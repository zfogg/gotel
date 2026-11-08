use super::tests::{assert_private, attributes, fixture};
use crate::{Outcome, Query as SqlQuery};
use clickhouse::{
    test::{handlers, Mock},
    Row,
};
use opentelemetry::{
    trace::{SpanKind, TraceContextExt},
    Context, Value,
};
use serde::{Deserialize, Serialize};

#[derive(Row, Serialize, Deserialize, Debug, PartialEq)]
struct Record {
    value: String,
}
fn client(app: &crate::Application, mock: &Mock) -> super::clickhouse::Client {
    let native = clickhouse::Client::default().with_mock(mock);
    super::clickhouse::Client::new(native, app, "storage.clickhouse", "analytics").unwrap()
}

#[tokio::test]
async fn native_typed_queries_binding_cursors_and_server_errors() {
    let (app, exporter) = fixture();
    let mock = Mock::new();
    let client = client(&app, &mock);
    let parent = app
        .operation(&Context::new(), "Parent", SpanKind::Internal)
        .unwrap();
    mock.add(handlers::provide([Record {
        value: "PRIVATE_RESULT".into(),
    }]));
    let row = client
        .query(
            parent.context(),
            "Read",
            "SELECT value FROM items WHERE value = ?",
        )
        .bind("PRIVATE_BIND")
        .fetch_one::<Record>()
        .await
        .unwrap();
    assert_eq!(row.value, "PRIVATE_RESULT");
    mock.add(handlers::provide([Record {
        value: "PRIVATE_CURSOR".into(),
    }]));
    let mut cursor = client
        .query(parent.context(), "Stream", "SELECT value FROM items")
        .fetch::<Record>()
        .unwrap();
    assert_eq!(
        cursor.next().await.unwrap().unwrap().value,
        "PRIVATE_CURSOR"
    );
    assert!(cursor.next().await.unwrap().is_none());
    mock.add(handlers::provide(Vec::<Record>::new()));
    let missing = client
        .query(parent.context(), "Optional", "SELECT value FROM items")
        .fetch_optional::<Record>()
        .await
        .unwrap();
    assert!(missing.is_none());
    mock.add(handlers::provide(Vec::<Record>::new()));
    let error = client
        .query(parent.context(), "Missing", "SELECT value FROM items")
        .fetch_one::<Record>()
        .await
        .unwrap_err();
    assert!(matches!(error, clickhouse::error::Error::RowNotFound));
    let record = mock.add(handlers::record_ddl());
    client
        .query(parent.context(), "Execute", "SELECT 'PRIVATE_LITERAL'")
        .execute()
        .await
        .unwrap();
    assert!(record.query().await.contains("PRIVATE_LITERAL"));
    mock.add(handlers::failure(clickhouse::test::status::BAD_REQUEST));
    client
        .query(parent.context(), "Malformed", "SELECT 'PRIVATE_UNCLOSED")
        .execute()
        .await
        .unwrap_err();
    drop(client.query(parent.context(), "Dropped", "SELECT 1"));
    parent.finish(Outcome::Success, None);
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    let parent = spans.iter().find(|s| s.name == "adapter.Parent").unwrap();
    for span in spans
        .iter()
        .filter(|s| s.name.starts_with("storage.clickhouse."))
    {
        assert_eq!(span.parent_span_id, parent.span_context.span_id());
        assert_eq!(
            attributes(span, "db.system.name"),
            Value::from("clickhouse")
        );
    }
    let read = spans
        .iter()
        .find(|s| s.name == "storage.clickhouse.Read")
        .unwrap();
    assert!(attributes(read, "gotel.db.query.fingerprint")
        .as_str()
        .starts_with("sequel:v1:clickhouse:"));
    let bad = spans
        .iter()
        .find(|s| s.name == "storage.clickhouse.Malformed")
        .unwrap();
    assert!(!bad
        .attributes
        .iter()
        .any(|a| a.key.as_str() == "db.query.text"));
    let Value::Array(opentelemetry::Array::String(diagnostics)) =
        attributes(bad, "db.query.diagnostics")
    else {
        panic!("diagnostics missing")
    };
    assert!(diagnostics[0].as_str().contains("unterminated_string"));
    let dropped = spans
        .iter()
        .find(|s| s.name == "storage.clickhouse.Dropped")
        .unwrap();
    assert_eq!(
        attributes(dropped, "operation.outcome"),
        Value::from("cancelled")
    );
    assert!(!Context::current().span().span_context().is_valid());
    app.shutdown().unwrap();
}

#[tokio::test]
async fn native_insert_rows_and_privacy_limits() {
    let (app, exporter) = fixture();
    let mock = Mock::new();
    let client = client(&app, &mock);
    let rows = mock.add(handlers::record::<Record>());
    let parent = Context::new();
    let future = client.insert::<Record>(
        &parent,
        SqlQuery::new("Insert", "INSERT INTO items (value) VALUES (?)"),
        "items",
    );
    fn require_send<T: Send>(_: &T) {}
    require_send(&future);
    let mut insert = future.await.unwrap();
    insert
        .write(&Record {
            value: "PRIVATE_INSERT".into(),
        })
        .await
        .unwrap();
    insert.end().await.unwrap();
    let actual: Vec<Record> = rows.collect().await;
    assert_eq!(
        actual,
        vec![Record {
            value: "PRIVATE_INSERT".into()
        }]
    );
    let mut dropped = client
        .insert::<Record>(
            &parent,
            SqlQuery::new("Discard", "INSERT INTO items VALUES (?)"),
            "items",
        )
        .await
        .unwrap();
    dropped
        .write(&Record {
            value: "PRIVATE_DISCARD".into(),
        })
        .await
        .unwrap();
    drop(dropped);
    for (operation, policy) in [
        (
            "Hidden",
            super::SqlPolicy {
                omit_query_text: true,
                ..Default::default()
            },
        ),
        (
            "Budget",
            super::SqlPolicy {
                max_work_units: 1,
                ..Default::default()
            },
        ),
    ] {
        mock.add(handlers::record_ddl());
        client
            .clone()
            .with_policy(policy)
            .query(&parent, operation, "SELECT 'PRIVATE_LITERAL'")
            .execute()
            .await
            .unwrap();
    }
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    for operation in ["Hidden", "Budget"] {
        let span = spans
            .iter()
            .find(|s| s.name == format!("storage.clickhouse.{operation}"))
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
    let discarded = spans
        .iter()
        .find(|s| s.name == "storage.clickhouse.Discard")
        .unwrap();
    assert_eq!(
        attributes(discarded, "operation.outcome"),
        Value::from("cancelled")
    );
    app.shutdown().unwrap();
}

#[tokio::test]
async fn official_client_http_preserves_values_and_propagates_query_context() {
    use axum::{extract::State, http::HeaderMap, routing::post, Router};
    use std::sync::{Arc, Mutex};
    type Capture = Arc<Mutex<Option<(HeaderMap, String)>>>;
    let (app, exporter) = fixture();
    let captured = Arc::new(Mutex::new(None));
    async fn handle(
        State(captured): State<Capture>,
        headers: HeaderMap,
        body: String,
    ) -> &'static str {
        *captured.lock().unwrap() = Some((headers, body));
        ""
    }
    let router = Router::new()
        .route("/", post(handle))
        .with_state(captured.clone());
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    let (stop, stopped) = tokio::sync::oneshot::channel();
    let server = tokio::spawn(async move {
        axum::serve(listener, router)
            .with_graceful_shutdown(async {
                let _ = stopped.await;
            })
            .await
            .unwrap()
    });
    let native = clickhouse::Client::default()
        .with_url(format!("http://{address}"))
        .with_user("test")
        .with_password("PRIVATE_PASSWORD");
    let client =
        super::clickhouse::Client::new(native, &app, "storage.clickhouse", "analytics").unwrap();
    client
        .query(&Context::new(), "Execute", "SELECT ?")
        .bind("PRIVATE_BIND")
        .execute()
        .await
        .unwrap();
    stop.send(()).unwrap();
    server.await.unwrap();
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    let (headers, body) = captured.lock().unwrap().take().unwrap();
    assert!(body.contains("PRIVATE_BIND"));
    assert_eq!(headers.get("x-clickhouse-key").unwrap(), "PRIVATE_PASSWORD");
    assert!(headers
        .get("traceparent")
        .unwrap()
        .to_str()
        .unwrap()
        .contains(&spans[0].span_context.span_id().to_string()));
    app.shutdown().unwrap();
}
