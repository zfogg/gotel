use crate::*;
#[cfg(any(
    feature = "db-rusqlite",
    feature = "db-sqlx",
    all(feature = "http-axum", feature = "http-reqwest")
))]
use opentelemetry::trace::SpanKind;
#[cfg(feature = "http-axum")]
use opentelemetry::trace::TraceContextExt;
#[cfg(any(
    feature = "db-rusqlite",
    feature = "db-sqlx",
    feature = "http-axum",
    feature = "http-reqwest"
))]
use opentelemetry::Context;
use opentelemetry::Value;
use opentelemetry_sdk::{
    logs::SdkLoggerProvider,
    metrics::SdkMeterProvider,
    trace::{InMemorySpanExporter, SdkTracerProvider, SpanData},
};

pub(super) fn fixture() -> (Application, InMemorySpanExporter) {
    let exporter = InMemorySpanExporter::default();
    let app = Application::with_providers(
        "adapter",
        SdkTracerProvider::builder()
            .with_batch_exporter(exporter.clone())
            .build(),
        SdkMeterProvider::builder().build(),
        SdkLoggerProvider::builder().build(),
    )
    .unwrap();
    (app, exporter)
}
pub(super) fn attributes(span: &SpanData, key: &str) -> Value {
    span.attributes
        .iter()
        .find(|a| a.key.as_str() == key)
        .unwrap_or_else(|| panic!("missing {key} on {}", span.name))
        .value
        .clone()
}
pub(super) fn assert_private(spans: &[SpanData]) {
    assert!(!format!("{spans:?}").contains("PRIVATE"));
}

#[cfg(feature = "http-reqwest")]
#[tokio::test]
async fn reqwest_transport_timeout_and_dropped_future_keep_native_errors() {
    use std::{future::Future, time::Duration};
    let (app, exporter) = fixture();
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    // Keep the listener alive without sending headers: the native request times out.
    let native = reqwest::Client::builder()
        .no_proxy()
        .timeout(Duration::from_millis(25))
        .build()
        .unwrap();
    let client = super::reqwest::Client::new(native.clone(), &app).unwrap();
    let request = native
        .get(format!("http://{address}/PRIVATE_PATH?key=PRIVATE_QUERY"))
        .build()
        .unwrap();
    let error = client.execute(&Context::new(), request).await.unwrap_err();
    assert!(error.is_timeout());
    assert!(error.url().unwrap().path().contains("PRIVATE_PATH"));
    let request = native
        .get(format!("http://{address}/PRIVATE_PATH"))
        .build()
        .unwrap();
    let parent = Context::new();
    let mut pending = Box::pin(client.execute(&parent, request));
    let mut cx = std::task::Context::from_waker(futures::task::noop_waker_ref());
    assert!(pending.as_mut().poll(&mut cx).is_pending());
    drop(pending);
    drop(listener);
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    assert_eq!(spans.len(), 2);
    assert_eq!(
        attributes(&spans[0], "operation.outcome"),
        Value::from("error")
    );
    assert_eq!(
        attributes(&spans[1], "operation.outcome"),
        Value::from("cancelled")
    );
    app.shutdown().unwrap();
}

#[cfg(feature = "http-axum")]
#[tokio::test]
async fn axum_unmatched_status_body_errors_and_dropped_streams() {
    use axum::{
        body::{Body, Bytes},
        http::Request,
        response::Response,
        routing::get,
        Router,
    };
    use tower::ServiceExt;
    let (app, exporter) = fixture();
    let router = Router::new()
        .route(
            "/failure",
            get(|| async {
                (
                    axum::http::StatusCode::INTERNAL_SERVER_ERROR,
                    "PRIVATE_BODY",
                )
            }),
        )
        .route(
            "/stream-error",
            get(|| async {
                Response::new(Body::from_stream(futures::stream::once(async {
                    Err::<Bytes, _>(std::io::Error::other("PRIVATE_STREAM_ERROR"))
                })))
            }),
        )
        .route(
            "/pending",
            get(|| async {
                Response::new(Body::from_stream(futures::stream::pending::<
                    Result<Bytes, std::io::Error>,
                >()))
            }),
        )
        .layer(axum::middleware::from_fn_with_state(
            super::axum::Tracer::new(&app).unwrap(),
            super::axum::trace,
        ));
    for (path, status) in [
        ("/PRIVATE_UNMATCHED?secret=PRIVATE_QUERY", 404),
        ("/failure", 500),
    ] {
        let response = router
            .clone()
            .oneshot(Request::get(path).body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(response.status(), status);
        axum::body::to_bytes(response.into_body(), 1024)
            .await
            .unwrap();
    }
    let response = router
        .clone()
        .oneshot(Request::get("/stream-error").body(Body::empty()).unwrap())
        .await
        .unwrap();
    let error = axum::body::to_bytes(response.into_body(), 1024)
        .await
        .unwrap_err();
    assert!(error.to_string().contains("PRIVATE_STREAM_ERROR"));
    let response = router
        .oneshot(Request::get("/pending").body(Body::empty()).unwrap())
        .await
        .unwrap();
    drop(response);
    assert!(!Context::current().span().span_context().is_valid());
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    for (name, outcome) in [
        ("http.server.GET unmatched", "success"),
        ("http.server.GET /failure", "error"),
        ("http.server.GET /stream-error", "error"),
        ("http.server.GET /pending", "cancelled"),
    ] {
        let span = spans.iter().find(|s| s.name == name).unwrap();
        assert_eq!(attributes(span, "operation.outcome"), Value::from(outcome));
    }
    app.shutdown().unwrap();
}

#[cfg(feature = "db-rusqlite")]
#[test]
fn rusqlite_native_results_errors_and_diagnostics() {
    let (app, exporter) = fixture();
    let db = super::rusqlite::Connection::new(
        rusqlite::Connection::open_in_memory().unwrap(),
        &app,
        "storage.sqlite",
        "test",
    )
    .unwrap();
    let parent = app
        .operation(&Context::new(), "Parent", SpanKind::Internal)
        .unwrap();
    db.execute_batch(
        parent.context(),
        "Schema",
        "CREATE TABLE items (value TEXT)",
    )
    .unwrap();
    assert_eq!(
        db.execute(
            parent.context(),
            "Insert",
            "INSERT INTO items VALUES (?)",
            ["PRIVATE_VALUE"]
        )
        .unwrap(),
        1
    );
    let row: String = db
        .query_row(
            parent.context(),
            "Read",
            "SELECT value FROM items WHERE value = ?",
            ["PRIVATE_VALUE"],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(row, "PRIVATE_VALUE");
    let error = db
        .execute(
            parent.context(),
            "Malformed",
            "SELECT 'PRIVATE_UNCLOSED",
            [],
        )
        .unwrap_err();
    assert!(matches!(error, rusqlite::Error::SqlInputError { .. }));
    let missing = db
        .query_row(
            parent.context(),
            "Missing",
            "SELECT value FROM items WHERE 0",
            [],
            |row| row.get::<_, String>(0),
        )
        .unwrap_err();
    assert!(matches!(missing, rusqlite::Error::QueryReturnedNoRows));
    parent.finish(Outcome::Success, None);
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    let read = spans
        .iter()
        .find(|s| s.name == "storage.sqlite.Read")
        .unwrap();
    let parent = spans.iter().find(|s| s.name == "adapter.Parent").unwrap();
    assert_eq!(read.parent_span_id, parent.span_context.span_id());
    assert_eq!(
        attributes(read, "db.query.diagnostics"),
        Value::Array(opentelemetry::Array::String(vec![]))
    );
    let bad = spans
        .iter()
        .find(|s| s.name == "storage.sqlite.Malformed")
        .unwrap();
    assert!(!bad
        .attributes
        .iter()
        .any(|a| a.key.as_str() == "db.query.text"));
    let Value::Array(opentelemetry::Array::String(diagnostics)) =
        attributes(bad, "db.query.diagnostics")
    else {
        panic!("typed diagnostics missing");
    };
    assert!(diagnostics[0].as_str().contains("unterminated_string"));
    assert_private(&spans);
    let native = db.into_inner();
    assert_eq!(
        native
            .query_row("SELECT count(*) FROM items", [], |row| row.get::<_, i64>(0))
            .unwrap(),
        1
    );
    app.shutdown().unwrap();
}

#[cfg(feature = "db-sqlx")]
#[tokio::test]
async fn sqlx_queries_typed_results_transactions_streams_and_cancellation() {
    use futures::StreamExt;
    use sqlx::{Executor as _, Row};
    let (app, exporter) = fixture();
    let pool = sqlx::sqlite::SqlitePoolOptions::new()
        .max_connections(1)
        .connect("sqlite::memory:")
        .await
        .unwrap();
    let tracer = super::sqlx::Tracer::<sqlx::Sqlite>::new(&app, "storage.sqlite", "test").unwrap();
    let parent = app
        .operation(&Context::new(), "Parent", SpanKind::Internal)
        .unwrap();
    sqlx::query("CREATE TABLE items (value TEXT)")
        .execute(tracer.wrap(parent.context(), "Schema", &pool))
        .await
        .unwrap();
    let mut transaction = pool.begin().await.unwrap();
    let inserted = sqlx::query("INSERT INTO items VALUES (?)")
        .bind("PRIVATE_VALUE")
        .execute(tracer.wrap(parent.context(), "Insert", &mut *transaction))
        .await
        .unwrap();
    assert_eq!(inserted.rows_affected(), 1);
    transaction.commit().await.unwrap();
    let value: (String,) = sqlx::query_as("SELECT value FROM items WHERE value = ?")
        .bind("PRIVATE_VALUE")
        .fetch_one(tracer.wrap(parent.context(), "Read", &pool))
        .await
        .unwrap();
    assert_eq!(value.0, "PRIVATE_VALUE");
    let mut rows = sqlx::query("SELECT value FROM items").fetch(tracer.wrap(
        parent.context(),
        "Stream",
        &pool,
    ));
    assert_eq!(
        rows.next().await.unwrap().unwrap().get::<String, _>(0),
        "PRIVATE_VALUE"
    );
    assert!(rows.next().await.is_none());
    drop(rows);
    let rows = sqlx::query("SELECT value FROM items").fetch(tracer.wrap(
        parent.context(),
        "Dropped",
        &pool,
    ));
    drop(rows);
    let missing = sqlx::query("SELECT value FROM items WHERE 0")
        .fetch_one(tracer.wrap(parent.context(), "Missing", &pool))
        .await
        .err()
        .expect("missing row");
    assert!(matches!(missing, sqlx::Error::RowNotFound));
    let bad = tracer
        .wrap(parent.context(), "Malformed", &pool)
        .execute("SELECT 'PRIVATE_UNCLOSED")
        .await
        .unwrap_err();
    assert!(matches!(bad, sqlx::Error::Database(_)));
    let rows = tracer
        .wrap(parent.context(), "All", &pool)
        .fetch_all("SELECT value FROM items")
        .await
        .unwrap();
    assert_eq!(rows.len(), 1);
    assert!(tracer
        .wrap(parent.context(), "Optional", &pool)
        .fetch_optional("SELECT value FROM items WHERE 0")
        .await
        .unwrap()
        .is_none());
    let mut statements = tracer
        .wrap(parent.context(), "Many", &pool)
        .execute_many("SELECT 1; SELECT 2");
    while let Some(result) = statements.next().await {
        result.unwrap();
    }
    drop(statements);
    let mut results = tracer
        .wrap(parent.context(), "Mixed", &pool)
        .fetch_many("SELECT 1");
    while let Some(result) = results.next().await {
        result.unwrap();
    }
    drop(results);
    let statement = tracer
        .wrap(parent.context(), "Prepare", &pool)
        .prepare("SELECT ?")
        .await
        .unwrap();
    drop(statement);
    let description = tracer
        .wrap(parent.context(), "Describe", &pool)
        .describe("SELECT 1")
        .await
        .unwrap();
    assert_eq!(description.columns().len(), 1);
    let hidden = tracer.clone().with_policy(super::SqlPolicy {
        omit_query_text: true,
        ..Default::default()
    });
    let row = hidden
        .wrap(parent.context(), "Hidden", &pool)
        .fetch_one("SELECT 'PRIVATE_LITERAL'")
        .await
        .unwrap();
    assert_eq!(row.get::<String, _>(0), "PRIVATE_LITERAL");
    let bounded = tracer.with_policy(super::SqlPolicy {
        max_work_units: 1,
        ..Default::default()
    });
    bounded
        .wrap(parent.context(), "Budget", &pool)
        .execute("SELECT 'PRIVATE_LIMIT'")
        .await
        .unwrap();
    parent.finish(Outcome::Success, None);
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    for operation in ["Hidden", "Budget"] {
        let span = spans
            .iter()
            .find(|s| s.name == format!("storage.sqlite.{operation}"))
            .unwrap();
        assert!(!span
            .attributes
            .iter()
            .any(|a| a.key.as_str() == "db.query.text"));
        assert_eq!(
            attributes(span, "operation.outcome"),
            Value::from("success")
        );
    }
    assert_eq!(
        attributes(
            spans
                .iter()
                .find(|s| s.name == "storage.sqlite.Dropped")
                .unwrap(),
            "operation.outcome"
        ),
        Value::from("cancelled")
    );
    assert_eq!(
        attributes(
            spans
                .iter()
                .find(|s| s.name == "storage.sqlite.Missing")
                .unwrap(),
            "operation.outcome"
        ),
        Value::from("error")
    );
    let parent = spans.iter().find(|s| s.name == "adapter.Parent").unwrap();
    for span in spans
        .iter()
        .filter(|s| s.name.starts_with("storage.sqlite."))
    {
        assert_eq!(span.parent_span_id, parent.span_context.span_id());
    }
    pool.close().await;
    app.shutdown().unwrap();
}

#[cfg(all(feature = "http-axum", feature = "http-reqwest"))]
#[tokio::test]
async fn http_client_server_propagation_route_privacy_and_native_body() {
    use axum::{routing::get, Router};
    let (app, exporter) = fixture();
    let state = super::axum::Tracer::new(&app).unwrap();
    let router = Router::new()
        .route("/users/{id}", get(|| async { "PRIVATE_RESPONSE" }))
        .layer(axum::middleware::from_fn_with_state(
            state,
            super::axum::trace,
        ));
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let (stop, stopped) = tokio::sync::oneshot::channel();
    let server = tokio::spawn(async move {
        axum::serve(listener, router)
            .with_graceful_shutdown(async {
                let _ = stopped.await;
            })
            .await
            .unwrap();
    });
    let native = reqwest::Client::builder().no_proxy().build().unwrap();
    let client = super::reqwest::Client::new(native.clone(), &app).unwrap();
    let parent = app
        .operation(&Context::new(), "Parent", SpanKind::Internal)
        .unwrap();
    let request = native
        .get(format!(
            "http://{address}/users/PRIVATE_ID?token=PRIVATE_QUERY"
        ))
        .header("authorization", "PRIVATE_CREDENTIAL")
        .build()
        .unwrap();
    let response = client.execute(parent.context(), request).await.unwrap();
    assert_eq!(response.status(), 200);
    assert_eq!(response.text().await.unwrap(), "PRIVATE_RESPONSE");
    parent.finish(Outcome::Success, None);
    stop.send(()).unwrap();
    server.await.unwrap();
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    let client = spans.iter().find(|s| s.name == "http.client.GET").unwrap();
    let server = spans
        .iter()
        .find(|s| s.name == "http.server.GET /users/{id}")
        .unwrap();
    assert_eq!(server.parent_span_id, client.span_context.span_id());
    assert_eq!(
        server.span_context.trace_id(),
        client.span_context.trace_id()
    );
    assert_eq!(
        attributes(server, "http.response.status_code"),
        Value::I64(200)
    );
    app.shutdown().unwrap();
}
