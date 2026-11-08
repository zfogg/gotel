//! Axum 0.8 middleware with matched-route names and W3C parent extraction.
//!
//! Install with `Router::layer(axum::middleware::from_fn_with_state(tracer, trace))`
//! after registering routes. Unmatched paths use `unmatched`, never raw URIs.
//! Response spans include streaming body completion, errors and cancellation.
use super::observer::Observer;
use crate::{Application, NamingError, Operation, Outcome};
use axum::{
    body::{Body, Bytes},
    extract::{MatchedPath, Request, State},
    middleware::Next,
    response::Response,
};
use http_body::{Body as HttpBody, Frame, SizeHint};
use opentelemetry::{
    propagation::{Extractor, TextMapPropagator},
    trace::{FutureExt, SpanKind, TraceContextExt},
    Context, KeyValue,
};
use opentelemetry_sdk::propagation::TraceContextPropagator;
use std::{
    pin::Pin,
    task::{Context as TaskContext, Poll},
};

/// Clonable state for Axum's `from_fn_with_state` middleware.
#[derive(Clone)]
pub struct Tracer {
    observer: Observer,
}
impl Tracer {
    /// Use the supplied application's providers without changing globals.
    pub fn new(app: &Application) -> Result<Self, NamingError> {
        Ok(Self {
            observer: Observer::new(
                app,
                "http.server",
                "http.server.request.duration",
                "gotel.http.server.request.count",
            )?,
        })
    }
}
struct Headers<'a>(&'a axum::http::HeaderMap);
impl Extractor for Headers<'_> {
    fn get(&self, key: &str) -> Option<&str> {
        self.0.get(key)?.to_str().ok()
    }
    fn keys(&self) -> Vec<&str> {
        self.0.keys().map(|key| key.as_str()).collect()
    }
}
/// Trace one matched request and its complete response body. Request headers,
/// URI values and bodies are not recorded. Valid incoming trace context wins.
pub async fn trace(State(tracer): State<Tracer>, request: Request, next: Next) -> Response {
    let parent = TraceContextPropagator::new()
        .extract_with_context(&Context::current(), &Headers(request.headers()));
    let method = super::method(request.method().as_str());
    let route = request
        .extensions()
        .get::<MatchedPath>()
        .map(|path| path.as_str())
        .unwrap_or("unmatched");
    let labels = vec![
        KeyValue::new("http.request.method", method),
        KeyValue::new("http.route", route.to_owned()),
    ];
    let op = tracer.observer.start(
        &parent,
        format!(
            "{} {route}",
            tracer.observer.names.name(method).expect("known method")
        ),
        SpanKind::Server,
        labels.clone(),
        labels,
    );
    let response = next.run(request).with_context(op.context().clone()).await;
    let code = response.status();
    op.context().span().set_attribute(KeyValue::new(
        "http.response.status_code",
        i64::from(code.as_u16()),
    ));
    let outcome = if code.is_server_error() {
        Outcome::Error
    } else {
        Outcome::Success
    };
    let (parts, body) = response.into_parts();
    if body.is_end_stream() {
        op.finish(
            outcome,
            (outcome == Outcome::Error).then_some("http.status"),
        );
        return Response::from_parts(parts, body);
    }
    Response::from_parts(
        parts,
        Body::new(ObservedBody {
            inner: body,
            operation: Some(op),
            outcome,
        }),
    )
}
pin_project_lite::pin_project! {
    struct ObservedBody {
        #[pin] inner: Body,
        operation: Option<Operation>,
        outcome: Outcome,
    }
}
impl HttpBody for ObservedBody {
    type Data = Bytes;
    type Error = axum::Error;
    fn poll_frame(
        self: Pin<&mut Self>,
        cx: &mut TaskContext<'_>,
    ) -> Poll<Option<Result<Frame<Bytes>, axum::Error>>> {
        let mut this = self.project();
        let _guard = this
            .operation
            .as_ref()
            .map(|op| op.context().clone().attach());
        let result = this.inner.as_mut().poll_frame(cx);
        let failed = matches!(&result, Poll::Ready(Some(Err(_))));
        if failed
            || matches!(&result, Poll::Ready(None))
            || (result.is_ready() && this.inner.is_end_stream())
        {
            if let Some(op) = this.operation.take() {
                let outcome = if failed {
                    Outcome::Error
                } else {
                    *this.outcome
                };
                op.finish(
                    outcome,
                    (outcome == Outcome::Error).then_some(if failed {
                        "http.body"
                    } else {
                        "http.status"
                    }),
                );
            }
        }
        result
    }
    fn is_end_stream(&self) -> bool {
        self.inner.is_end_stream()
    }
    fn size_hint(&self) -> SizeHint {
        self.inner.size_hint()
    }
}
