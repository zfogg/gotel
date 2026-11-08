//! Official ClickHouse Rust client query, cursor and insert instrumentation.
//!
//! Analysis sees only the original query template, before native binding.
//! Supplied values, inserted rows, credentials and server messages are omitted.
//! The official 0.15 driver requires Rust 1.89 when this feature is enabled.
use super::SqlPolicy;
use crate::{Application, NamingError, Operation, Outcome, Query as SqlQuery, SqlTracer};
use clickhouse::{error::Result, Row, RowOwned, RowRead, RowWrite};
use opentelemetry::{
    propagation::{Injector, TextMapPropagator},
    trace::FutureExt,
    Context,
};
use opentelemetry_sdk::propagation::TraceContextPropagator;
use std::sync::Arc;

/// Clonable wrapper preserving the native client's transport and configuration.
#[derive(Clone)]
pub struct Client {
    inner: clickhouse::Client,
    tracer: Arc<SqlTracer>,
    policy: SqlPolicy,
}
impl Client {
    /// Wrap the original client, with source-owned names and database identity.
    pub fn new(
        inner: clickhouse::Client,
        app: &Application,
        prefix: &str,
        namespace: &str,
    ) -> std::result::Result<Self, NamingError> {
        Ok(Self {
            inner,
            tracer: Arc::new(app.database(prefix, sequel::Dialect::ClickHouse, namespace)?),
            policy: SqlPolicy::default(),
        })
    }
    /// Set query text, bounded metadata and native analysis budget policies.
    pub fn with_policy(mut self, policy: SqlPolicy) -> Self {
        self.policy = policy;
        self
    }
    /// Start a query from an original template, before native interpolation. Drop
    /// without execution records cancellation. Use source-owned operation names.
    pub fn query(&self, parent: &Context, operation: &'static str, template: &str) -> Query {
        let operation = self
            .tracer
            .start(parent, self.policy.query(operation, template));
        Query {
            inner: propagated(self.inner.clone(), operation.context()).query(template),
            operation,
        }
    }
    /// Begin a native typed insertion. Supply a separate application-owned SQL
    /// template for analysis; the native table and row payloads are never inspected.
    pub fn insert<T: Row>(
        &self,
        parent: &Context,
        query: SqlQuery<'_>,
        table: &str,
    ) -> impl std::future::Future<Output = Result<Insert<T>>> {
        let operation = self.tracer.start(
            parent,
            query.with_analysis(self.policy.query(query.operation, query.statement).analysis),
        );
        let client = propagated(self.inner.clone(), operation.context());
        let table = table.to_owned();
        async move {
            match client
                .insert(&table)
                .with_context(operation.context().clone())
                .await
            {
                Ok(inner) => Ok(Insert {
                    inner,
                    operation: Some(operation),
                }),
                Err(error) => {
                    finish(operation, true);
                    Err(error)
                }
            }
        }
    }
    /// Return the configured native client without changing it.
    pub fn into_inner(self) -> clickhouse::Client {
        self.inner
    }
}
fn propagated(mut client: clickhouse::Client, context: &Context) -> clickhouse::Client {
    #[derive(Default)]
    struct Headers(Vec<(String, String)>);
    impl Injector for Headers {
        fn set(&mut self, key: &str, value: String) {
            self.0.push((key.to_owned(), value));
        }
    }
    let mut headers = Headers::default();
    TraceContextPropagator::new().inject_context(context, &mut headers);
    for (key, value) in headers.0 {
        client = client.with_header(key, value);
    }
    client
}
fn finish(operation: Operation, failed: bool) {
    operation.finish(
        if failed {
            Outcome::Error
        } else {
            Outcome::Success
        },
        failed.then_some("clickhouse::error::Error"),
    );
}

/// A native query builder with one owned SQL observation. Values are forwarded
/// directly to ClickHouse; no formatted/bound SQL is used for telemetry.
pub struct Query {
    inner: clickhouse::query::Query,
    operation: Operation,
}
impl Query {
    /// Bind a positional value using the native escaping/serialization behavior.
    pub fn bind(mut self, value: impl clickhouse::sql::Bind) -> Self {
        self.inner = self.inner.bind(value);
        self
    }
    /// Bind a typed server parameter without exposing its name or value to analysis.
    pub fn param(mut self, name: &str, value: impl serde::Serialize) -> Self {
        self.inner = self.inner.param(name, value);
        self
    }
    /// Forward a native per-query setting without recording it.
    pub fn with_setting(mut self, name: impl Into<String>, value: impl Into<String>) -> Self {
        self.inner = self.inner.with_setting(name, value);
        self
    }
    /// Execute and consume the complete native response, returning its original error.
    pub async fn execute(self) -> Result<()> {
        let result = self
            .inner
            .execute()
            .with_context(self.operation.context().clone())
            .await;
        finish(self.operation, result.is_err());
        result
    }
    /// Fetch an owned typed row with native not-found/decoding errors.
    pub async fn fetch_one<T: RowOwned + RowRead>(self) -> Result<T> {
        let result = self
            .inner
            .fetch_one()
            .with_context(self.operation.context().clone())
            .await;
        finish(self.operation, result.is_err());
        result
    }
    /// Fetch at most one owned row; native `None` remains successful.
    pub async fn fetch_optional<T: RowOwned + RowRead>(self) -> Result<Option<T>> {
        let result = self
            .inner
            .fetch_optional()
            .with_context(self.operation.context().clone())
            .await;
        finish(self.operation, result.is_err());
        result
    }
    /// Collect native typed rows without changing ordering or decoding behavior.
    pub async fn fetch_all<T: RowOwned + RowRead>(self) -> Result<Vec<T>> {
        let result = self
            .inner
            .fetch_all()
            .with_context(self.operation.context().clone())
            .await;
        finish(self.operation, result.is_err());
        result
    }
    /// Stream native rows, retaining the observation through EOF/error/drop.
    pub fn fetch<T: Row>(self) -> Result<Cursor<T>> {
        let _guard = self.operation.context().clone().attach();
        match self.inner.fetch() {
            Ok(inner) => Ok(Cursor {
                inner,
                operation: Some(self.operation),
            }),
            Err(error) => {
                finish(self.operation, true);
                Err(error)
            }
        }
    }
}

/// A native typed cursor, including borrowed-row decoding.
pub struct Cursor<T> {
    inner: clickhouse::query::RowCursor<T>,
    operation: Option<Operation>,
}
impl<T: Row> Cursor<T> {
    /// Advance once; return original native values/errors. Exhaustion finishes
    /// successfully, and dropping an unfinished cursor records cancellation.
    pub async fn next(&mut self) -> Result<Option<T::Value<'_>>>
    where
        T: RowRead,
    {
        let context = self
            .operation
            .as_ref()
            .map(|op| op.context().clone())
            .unwrap_or_default();
        let result = self.inner.next().with_context(context).await;
        if !matches!(&result, Ok(Some(_))) {
            if let Some(operation) = self.operation.take() {
                finish(operation, result.is_err());
            }
        }
        result
    }
}

/// A native typed insert. Call `end` to commit; dropping retains native abort
/// semantics and records cancellation. Gotel never serializes inserted rows.
pub struct Insert<T: Row> {
    inner: clickhouse::insert::Insert<T>,
    operation: Option<Operation>,
}
impl<T: Row> Insert<T> {
    /// Write the native row unchanged, recording failures without their messages.
    pub async fn write(&mut self, row: &T::Value<'_>) -> Result<()>
    where
        T: RowWrite,
    {
        let context = self
            .operation
            .as_ref()
            .map(|op| op.context().clone())
            .unwrap_or_default();
        let result = self.inner.write(row).with_context(context).await;
        if result.is_err() {
            if let Some(operation) = self.operation.take() {
                finish(operation, true);
            }
        }
        result
    }
    /// Finish the original insertion and return its native completion/error.
    pub async fn end(mut self) -> Result<()> {
        let context = self
            .operation
            .as_ref()
            .map(|op| op.context().clone())
            .unwrap_or_default();
        let result = self.inner.end().with_context(context).await;
        if let Some(operation) = self.operation.take() {
            finish(operation, result.is_err());
        }
        result
    }
}
