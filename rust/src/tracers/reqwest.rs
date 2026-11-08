//! reqwest 0.13 async HTTP client instrumentation and W3C propagation.
//!
//! Spans cover request execution through receipt of response headers. The original
//! response, including its unconsumed body, is returned. URLs, query strings,
//! credentials, bodies and error messages are not exported. Client names contain
//! only the method; arbitrary URL paths are never treated as route templates.
use super::observer::Observer;
use crate::{Application, NamingError, Outcome};
use opentelemetry::{
    propagation::{Injector, TextMapPropagator},
    trace::{FutureExt, SpanKind, TraceContextExt},
    Context, KeyValue,
};
use opentelemetry_sdk::propagation::TraceContextPropagator;

/// A clonable wrapper retaining the caller's reqwest client configuration.
#[derive(Clone)]
pub struct Client {
    inner: reqwest::Client,
    observer: Observer,
}
impl Client {
    /// Wrap an existing client without changing redirects, proxies, TLS or timeouts.
    pub fn new(inner: reqwest::Client, app: &Application) -> Result<Self, NamingError> {
        Ok(Self {
            inner,
            observer: Observer::new(
                app,
                "http.client",
                "http.client.request.duration",
                "gotel.http.client.request.count",
            )?,
        })
    }
    /// Build and execute a native request builder under the current context.
    pub async fn send(
        &self,
        request: reqwest::RequestBuilder,
    ) -> Result<reqwest::Response, reqwest::Error> {
        self.execute(&Context::current(), request.build()?).await
    }
    /// Execute once under an explicit parent, replacing propagation headers with
    /// this request span's W3C context. Bodies and response errors are unchanged.
    pub async fn execute(
        &self,
        parent: &Context,
        mut request: reqwest::Request,
    ) -> Result<reqwest::Response, reqwest::Error> {
        let method = super::method(request.method().as_str());
        let mut attributes = vec![
            KeyValue::new("http.request.method", method),
            KeyValue::new("url.scheme", request.url().scheme().to_owned()),
        ];
        if let Some(host) = request.url().host_str() {
            attributes.push(KeyValue::new("server.address", host.to_owned()));
        }
        if let Some(port) = request.url().port_or_known_default() {
            attributes.push(KeyValue::new("server.port", i64::from(port)));
        }
        let op = self.observer.start(
            parent,
            self.observer.names.name(method).expect("known method"),
            SpanKind::Client,
            attributes,
            vec![KeyValue::new("http.request.method", method)],
        );
        TraceContextPropagator::new()
            .inject_context(op.context(), &mut Headers(request.headers_mut()));
        let result = self
            .inner
            .execute(request)
            .with_context(op.context().clone())
            .await;
        match &result {
            Ok(response) => {
                op.context().span().set_attribute(KeyValue::new(
                    "http.response.status_code",
                    i64::from(response.status().as_u16()),
                ));
                let failed =
                    response.status().is_client_error() || response.status().is_server_error();
                op.finish(
                    if failed {
                        Outcome::Error
                    } else {
                        Outcome::Success
                    },
                    failed.then_some("http.status"),
                );
            }
            Err(_) => op.finish(Outcome::Error, Some("reqwest::Error")),
        }
        result
    }
}
struct Headers<'a>(&'a mut reqwest::header::HeaderMap);
impl Injector for Headers<'_> {
    fn set(&mut self, key: &str, value: String) {
        if let (Ok(key), Ok(value)) = (
            reqwest::header::HeaderName::from_bytes(key.as_bytes()),
            reqwest::header::HeaderValue::from_str(&value),
        ) {
            self.0.insert(key, value);
        }
    }
}
