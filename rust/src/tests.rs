use super::*;
use opentelemetry::{
    trace::{Span, SpanKind, Status, TraceContextExt, Tracer},
    Context, Value,
};
use opentelemetry_sdk::{
    logs::SdkLoggerProvider,
    metrics::SdkMeterProvider,
    trace::{InMemorySpanExporter, SdkTracerProvider, SpanData},
};
use std::{future::Future, sync::Arc, task::Poll};

fn fixture() -> (Application, InMemorySpanExporter) {
    let exporter = InMemorySpanExporter::default();
    let provider = SdkTracerProvider::builder()
        .with_simple_exporter(exporter.clone())
        .build();
    let app = Application::with_providers(
        "fixture",
        provider,
        SdkMeterProvider::builder().build(),
        SdkLoggerProvider::builder().build(),
    )
    .unwrap();
    (app, exporter)
}
fn attr<'a>(span: &'a SpanData, key: &str) -> &'a Value {
    &span
        .attributes
        .iter()
        .find(|a| a.key.as_str() == key)
        .unwrap_or_else(|| panic!("missing {key}"))
        .value
}
fn has(span: &SpanData, key: &str) -> bool {
    span.attributes.iter().any(|a| a.key.as_str() == key)
}

#[derive(Debug)]
struct DriverError(Arc<str>);
impl std::fmt::Display for DriverError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}
impl std::error::Error for DriverError {}

#[test]
fn sql_parents_results_privacy_and_wire_metadata() {
    let (app, exporter) = fixture();
    let sql = app
        .database("storage.postgres", sequel::Dialect::PostgreSql, "test")
        .unwrap()
        .with_server("localhost", 5432);
    let parent = app
        .operation(&Context::new(), "Handle", SpanKind::Server)
        .unwrap();
    let query = Query::new("ReadWidget", "SELECT 'PRIVATE_LITERAL', $1 FROM widgets")
        .with_bind_count(1)
        .with_analysis(sequel::AnalysisOptions {
            format: true,
            ..Default::default()
        });
    let result = Arc::new(123);
    let returned = sql
        .query(parent.context(), query, || {
            let child = app
                .operation(&Context::current(), "Driver", SpanKind::Internal)
                .unwrap();
            child.finish(Outcome::Success, None);
            Ok::<_, DriverError>(result.clone())
        })
        .unwrap();
    assert!(Arc::ptr_eq(&result, &returned));
    parent.finish(Outcome::Success, None);
    let spans = exporter.get_finished_spans().unwrap();
    let query = spans
        .iter()
        .find(|s| s.name == "storage.postgres.ReadWidget")
        .unwrap();
    let parent = spans.iter().find(|s| s.name == "fixture.Handle").unwrap();
    let child = spans.iter().find(|s| s.name == "fixture.Driver").unwrap();
    assert_eq!(query.parent_span_id, parent.span_context.span_id());
    assert_eq!(child.parent_span_id, query.span_context.span_id());
    assert!(!format!("{spans:?}").contains("PRIVATE_LITERAL"));
    assert_eq!(attr(query, "operation.outcome"), &Value::from("success"));
    assert_eq!(query.status, Status::Ok);
    assert!(attr(query, "gotel.db.query.fingerprint")
        .as_str()
        .starts_with("sequel:v1:postgresql:"));
    let maps: serde_json::Value =
        serde_json::from_str(&attr(query, "gotel.db.query.source_map").as_str()).unwrap();
    assert_eq!(maps["display"], "formatted");
    let binds: serde_json::Value =
        serde_json::from_str(&attr(query, "gotel.db.query.binds").as_str()).unwrap();
    assert_eq!(binds[0]["style"], "numbered");
    assert_eq!(
        attr(query, "gotel.db.query.bind_arity_valid"),
        &Value::Bool(true)
    );
    assert_eq!(
        attr(query, "db.query.diagnostics"),
        &Value::Array(opentelemetry::Array::String(Vec::new()))
    );
    app.shutdown().unwrap();
}

#[test]
fn errors_outcomes_and_driver_identity_are_preserved() {
    let (app, exporter) = fixture();
    let sql = app
        .database("storage.postgres", sequel::Dialect::PostgreSql, "test")
        .unwrap();
    for outcome in [Outcome::Error, Outcome::Cancelled, Outcome::Deferred] {
        let secret: Arc<str> = "PRIVATE_ERROR_MESSAGE".into();
        let result = sql.query_with(
            &Context::new(),
            Query::new("Read", "SELECT 1"),
            || Err::<(), _>(DriverError(secret.clone())),
            |_| outcome,
        );
        assert!(Arc::ptr_eq(&result.unwrap_err().0, &secret));
    }
    let spans = exporter.get_finished_spans().unwrap();
    for (span, outcome) in spans
        .iter()
        .zip([Outcome::Error, Outcome::Cancelled, Outcome::Deferred])
    {
        assert_eq!(
            attr(span, "operation.outcome"),
            &Value::from(outcome.as_str())
        );
        assert_eq!(has(span, "error.type"), outcome == Outcome::Error);
        assert_eq!(span.status == Status::Ok, outcome != Outcome::Error);
    }
    assert!(!format!("{spans:?}").contains("PRIVATE_ERROR_MESSAGE"));
    assert!(spans[2]
        .events
        .iter()
        .any(|e| e.name == "work_budget_exhausted"));
}

#[test]
fn malformed_limits_policy_and_analysis_callbacks_do_not_block_execution() {
    let (app, exporter) = fixture();
    let sql = app
        .database("storage.sqlite", sequel::Dialect::Sqlite, "test")
        .unwrap();
    let panic_probe = || panic!("PRIVATE_ANALYSIS_CALLBACK");
    let cases = [
        (
            "SELECT 'PRIVATE_UNCLOSED",
            sequel::AnalysisOptions::default(),
            "malformed",
        ),
        (
            "SELECT 'PRIVATE_LIMIT'",
            sequel::AnalysisOptions {
                max_work_units: 1,
                ..Default::default()
            },
            "analysis_limit",
        ),
        (
            "SELECT 'PRIVATE_POLICY'",
            sequel::AnalysisOptions {
                omit_query_text: true,
                ..Default::default()
            },
            "policy",
        ),
        (
            "SELECT 'PRIVATE_CALLBACK'",
            sequel::AnalysisOptions {
                cancelled: Some(&panic_probe),
                ..Default::default()
            },
            "analysis_error",
        ),
    ];
    for (statement, policy, reason) in cases {
        assert_eq!(
            sql.query(
                &Context::new(),
                Query::new("Run", statement).with_analysis(policy),
                || Ok::<_, DriverError>(42)
            )
            .unwrap(),
            42
        );
        let spans = exporter.get_finished_spans().unwrap();
        let span = spans.last().unwrap();
        assert_eq!(
            attr(span, "gotel.db.query.text_reason"),
            &Value::from(reason)
        );
        assert!(!has(span, "db.query.text"));
        assert_eq!(attr(span, "operation.outcome"), &Value::from("success"));
    }
    let spans = exporter.get_finished_spans().unwrap();
    assert!(!format!("{spans:?}").contains("PRIVATE_"));
    if let Value::Array(opentelemetry::Array::String(diagnostics)) =
        attr(&spans[0], "db.query.diagnostics")
    {
        let diagnostic: serde_json::Value = serde_json::from_str(diagnostics[0].as_str()).unwrap();
        assert_eq!(diagnostic["code"], "unterminated_string");
        assert!(diagnostic["incomplete"].as_bool().unwrap());
    } else {
        panic!("diagnostic transport must be a typed string array");
    }
}

#[test]
fn transport_is_bounded_before_json_allocation() {
    let (app, _) = fixture();
    let sql = app
        .database("storage.postgres", sequel::Dialect::PostgreSql, "test")
        .unwrap();
    let text = format!(
        "SELECT {}",
        (1..1000)
            .map(|i| format!("${i}"))
            .collect::<Vec<_>>()
            .join(",")
    );
    let metadata = sql.inspect(Query::new("Large", &text).with_analysis(Default::default()));
    assert!(metadata
        .attributes
        .iter()
        .any(|a| a.key.as_str() == "gotel.db.query.source_map_omitted"
            && a.value == Value::Bool(true)));
    assert!(metadata
        .attributes
        .iter()
        .any(|a| a.key.as_str() == "gotel.db.query.binds_omitted" && a.value == Value::Bool(true)));
    assert!(!metadata
        .attributes
        .iter()
        .any(|a| a.key.as_str() == "gotel.db.query.binds"));
}

#[test]
fn async_poll_context_is_scoped_and_dropped_futures_are_cancelled() {
    let (app, exporter) = fixture();
    let sql = app
        .database("storage.postgres", sequel::Dialect::PostgreSql, "test")
        .unwrap();
    let future = {
        let local = std::cell::Cell::new(false);
        let callback = || local.get();
        sql.query_async(
            &Context::new(),
            Query::new("Cancelled", "SELECT 1").with_analysis(sequel::AnalysisOptions {
                cancelled: Some(&callback),
                ..Default::default()
            }),
            futures::future::pending::<Result<(), DriverError>>,
        )
    };
    fn require_send<T: Send>(_: &T) {}
    require_send(&future);
    let mut future = Box::pin(future);
    let mut cx = std::task::Context::from_waker(futures::task::noop_waker_ref());
    assert!(future.as_mut().poll(&mut cx).is_pending());
    assert!(!Context::current().span().span_context().is_valid());
    drop(future);
    let spans = exporter.get_finished_spans().unwrap();
    assert_eq!(spans.len(), 1);
    assert_eq!(
        attr(&spans[0], "operation.outcome"),
        &Value::from("cancelled")
    );
    assert_eq!(spans[0].status, Status::Ok);
    let joined = async {
        let a = app
            .operation(&Context::new(), "A", SpanKind::Server)
            .unwrap();
        let b = app
            .operation(&Context::new(), "B", SpanKind::Server)
            .unwrap();
        let driver = || async {
            let mut ready = false;
            futures::future::poll_fn(move |cx| {
                if ready {
                    Poll::Ready(())
                } else {
                    ready = true;
                    cx.waker().wake_by_ref();
                    Poll::Pending
                }
            })
            .await;
            let child = app
                .operation(&Context::current(), "Child", SpanKind::Internal)
                .unwrap();
            let id = child.context().span().span_context().trace_id();
            child.finish(Outcome::Success, None);
            Ok::<_, DriverError>(id)
        };
        let (ar, br) = futures::join!(
            sql.query_async(a.context(), Query::new("A", "SELECT 1"), driver),
            sql.query_async(b.context(), Query::new("B", "SELECT 2"), driver)
        );
        assert_eq!(ar.unwrap(), a.context().span().span_context().trace_id());
        assert_eq!(br.unwrap(), b.context().span().span_context().trace_id());
        a.finish(Outcome::Success, None);
        b.finish(Outcome::Success, None);
    };
    let mut joined = Box::pin(joined);
    while joined.as_mut().poll(&mut cx).is_pending() {}
    assert!(!Context::current().span().span_context().is_valid());
}

#[test]
fn panic_cleans_up_without_recording_messages() {
    let (app, exporter) = fixture();
    let sql = app
        .database("storage.postgres", sequel::Dialect::PostgreSql, "test")
        .unwrap();
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        sql.query(
            &Context::new(),
            Query::new("Panics", "SELECT 1"),
            || -> Result<(), DriverError> { panic!("PRIVATE_DRIVER_PANIC") },
        )
    }));
    assert!(result.is_err());
    assert!(!Context::current().span().span_context().is_valid());
    let spans = exporter.get_finished_spans().unwrap();
    assert_eq!(attr(&spans[0], "operation.outcome"), &Value::from("error"));
    assert!(!format!("{spans:?}").contains("PRIVATE_DRIVER_PANIC"));
}

#[test]
fn naming_and_http_url_privacy() {
    assert!(Namer::new("source-owned")
        .unwrap()
        .name("Read")
        .unwrap()
        .starts_with("source-owned."));
    assert!(Namer::new("PRIVATE SQL 'bad'").is_err());
    let name = http_name(
        "http.client",
        false,
        "GET",
        "https://user:PRIVATE_PASSWORD@example.com/orders/123?token=PRIVATE_QUERY#PRIVATE_FRAGMENT",
    )
    .unwrap();
    assert_eq!(name, "http.client.GET /orders/:id");
    assert!(!name.contains("PRIVATE"));
    let (app, _) = fixture();
    let sql = app
        .database("storage.postgres", sequel::Dialect::PostgreSql, "test")
        .unwrap();
    let metadata = sql.inspect(Query::new("PRIVATE LABEL 'SQL'", "SELECT 1"));
    assert_eq!(metadata.name, "storage.postgres.SQL");
    assert!(!format!("{metadata:?}").contains("PRIVATE LABEL"));
}

#[test]
fn constructors_do_not_install_a_global_provider() {
    let tracer = opentelemetry::global::tracer("global-before");
    let mut before = tracer.start("before");
    assert!(!before.is_recording());
    before.end();
    let (app, exporter) = fixture();
    let span = app
        .operation(&Context::new(), "Owned", SpanKind::Internal)
        .unwrap();
    span.finish(Outcome::Success, None);
    let mut after = opentelemetry::global::tracer("global-after").start("after");
    assert!(!after.is_recording());
    after.end();
    assert_eq!(exporter.get_finished_spans().unwrap().len(), 1);
}
