use super::tests::{assert_private, attributes, fixture};
use crate::Outcome;
use opentelemetry::{
    trace::{SpanKind, TraceContextExt},
    Context, Value as OtelValue,
};
use redis::{Cmd, ConnectionLike, Pipeline, RedisFuture, RedisResult, Value};
use std::{
    future::Future,
    sync::{Arc, Mutex},
    task::Poll,
};

#[derive(Default)]
struct Native {
    calls: Arc<Mutex<Vec<Call>>>,
    fail: bool,
    pending: bool,
}
type Call = (Vec<u8>, usize, usize);
impl Native {
    fn response(&self) -> RedisResult<Value> {
        if self.fail {
            Err(redis::RedisError::from((
                redis::ErrorKind::Io,
                "PRIVATE_ERROR",
            )))
        } else {
            Ok(Value::BulkString(b"PRIVATE_VALUE".to_vec()))
        }
    }
}
impl redis::ConnectionLike for Native {
    fn req_packed_command(&mut self, cmd: &[u8]) -> RedisResult<Value> {
        self.calls.lock().unwrap().push((cmd.to_vec(), 0, 1));
        self.response()
    }
    fn req_packed_commands(
        &mut self,
        cmd: &[u8],
        offset: usize,
        count: usize,
    ) -> RedisResult<Vec<Value>> {
        self.calls
            .lock()
            .unwrap()
            .push((cmd.to_vec(), offset, count));
        (0..count).map(|_| self.response()).collect()
    }
    fn get_db(&self) -> i64 {
        7
    }
    fn supports_pipelining(&self) -> bool {
        false
    }
    fn is_open(&self) -> bool {
        true
    }
    fn check_connection(&mut self) -> bool {
        true
    }
}
impl redis::aio::ConnectionLike for Native {
    fn req_packed_command<'a>(&'a mut self, cmd: &'a Cmd) -> RedisFuture<'a, Value> {
        Box::pin(async move {
            self.calls
                .lock()
                .unwrap()
                .push((cmd.get_packed_command(), 0, 1));
            if self.pending {
                std::future::pending::<()>().await;
            }
            self.response()
        })
    }
    fn req_packed_commands<'a>(
        &'a mut self,
        pipeline: &'a Pipeline,
        offset: usize,
        count: usize,
    ) -> RedisFuture<'a, Vec<Value>> {
        Box::pin(async move {
            self.calls
                .lock()
                .unwrap()
                .push((pipeline.get_packed_pipeline(), offset, count));
            (0..count).map(|_| self.response()).collect()
        })
    }
    fn get_db(&self) -> i64 {
        7
    }
}
#[test]
fn synchronous_commands_and_packed_pipelines_are_transparent_and_private() {
    use redis::Commands;
    let (app, exporter) = fixture();
    let native = Native::default();
    let calls = native.calls.clone();
    let parent = app
        .operation(&Context::new(), "Parent", SpanKind::Internal)
        .unwrap();
    let mut connection = super::redis::Connection::new(native, &app, "cache.redis")
        .unwrap()
        .with_parent(parent.context().clone());
    let value: String = connection.get("PRIVATE_KEY").unwrap();
    assert_eq!(value, "PRIVATE_VALUE");
    let mut cmd = redis::cmd("get");
    cmd.arg("PRIVATE_OTHER_KEY");
    let bytes = cmd.get_packed_command();
    let response = connection.req_packed_command(&bytes).unwrap();
    assert_eq!(response, Value::BulkString(b"PRIVATE_VALUE".to_vec()));
    assert_eq!(calls.lock().unwrap()[1].0, bytes);
    let pipe = b"PRIVATE_NATIVE_PIPELINE";
    assert_eq!(connection.req_packed_commands(pipe, 2, 3).unwrap().len(), 3);
    assert_eq!(calls.lock().unwrap()[2], (pipe.to_vec(), 2, 3));
    redis::cmd("PRIVATE_UNKNOWN_COMMAND")
        .arg("PRIVATE_KEY")
        .query::<Value>(&mut connection)
        .unwrap();
    assert_eq!(connection.get_db(), 7);
    assert!(!connection.supports_pipelining());
    assert!(connection.is_open());
    assert!(connection.check_connection());
    parent.finish(Outcome::Success, None);
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    assert_eq!(
        spans.iter().filter(|s| s.name == "cache.redis.GET").count(),
        2
    );
    assert!(spans.iter().any(|s| s.name == "cache.redis.PIPELINE"));
    assert!(spans.iter().any(|s| s.name == "cache.redis.OTHER"));
    let parent = spans.iter().find(|s| s.name == "adapter.Parent").unwrap();
    for span in spans.iter().filter(|s| s.name.starts_with("cache.redis.")) {
        assert_eq!(span.parent_span_id, parent.span_context.span_id());
    }
    let native = connection.into_inner();
    assert_eq!(native.calls.lock().unwrap().len(), 4);
    app.shutdown().unwrap();
}
#[tokio::test]
async fn asynchronous_commands_pipelines_errors_and_cancellation() {
    use redis::AsyncCommands;
    let (app, exporter) = fixture();
    let mut connection =
        super::redis::Connection::new(Native::default(), &app, "cache.redis").unwrap();
    let value: String = connection.get("PRIVATE_KEY").await.unwrap();
    assert_eq!(value, "PRIVATE_VALUE");
    let mut pipeline = redis::pipe();
    pipeline.cmd("GET").arg("PRIVATE_KEY");
    let values: (String,) = pipeline.query_async(&mut connection).await.unwrap();
    assert_eq!(values.0, "PRIVATE_VALUE");
    let mut failed = super::redis::Connection::new(
        Native {
            fail: true,
            ..Default::default()
        },
        &app,
        "cache.redis",
    )
    .unwrap();
    let error = redis::cmd("GET")
        .arg("PRIVATE_KEY")
        .query_async::<String>(&mut failed)
        .await
        .unwrap_err();
    assert_eq!(error.kind(), redis::ErrorKind::Io);
    assert!(error.to_string().contains("PRIVATE_ERROR"));
    let mut pending = super::redis::Connection::new(
        Native {
            pending: true,
            ..Default::default()
        },
        &app,
        "cache.redis",
    )
    .unwrap();
    let mut cmd = redis::cmd("GET");
    cmd.arg("PRIVATE_KEY");
    let mut future = Box::pin(cmd.query_async::<String>(&mut pending));
    let mut cx = std::task::Context::from_waker(futures::task::noop_waker_ref());
    assert!(matches!(future.as_mut().poll(&mut cx), Poll::Pending));
    assert!(!Context::current().span().span_context().is_valid());
    drop(future);
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    let redis: Vec<_> = spans
        .iter()
        .filter(|s| s.name.starts_with("cache.redis."))
        .collect();
    assert_eq!(redis.len(), 4);
    assert_eq!(
        attributes(redis[2], "operation.outcome"),
        OtelValue::from("error")
    );
    assert_eq!(
        attributes(redis[3], "operation.outcome"),
        OtelValue::from("cancelled")
    );
    app.shutdown().unwrap();
}
