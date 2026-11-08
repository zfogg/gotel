//! Official duckdb-rs instrumentation using Sequel's DuckDB dialect.
//!
//! Execute and batch calls, typed row callbacks, and collected query results keep
//! native parameters/results/errors. Connection and transaction ownership remain
//! with the application; wrap `&*transaction` for transactional calls.
use super::SqlPolicy;
use crate::{Application, NamingError, SqlTracer};
use opentelemetry::Context;
use std::borrow::Borrow;

/// An owned or borrowed duckdb-rs connection with safe SQL telemetry.
pub struct Connection<C> {
    inner: C,
    tracer: SqlTracer,
    policy: SqlPolicy,
}
impl<C: Borrow<duckdb::Connection>> Connection<C> {
    /// Wrap a native connection with the actual DuckDB dialect and owned providers.
    pub fn new(
        inner: C,
        app: &Application,
        prefix: &str,
        namespace: &str,
    ) -> Result<Self, NamingError> {
        Ok(Self {
            inner,
            tracer: app.database(prefix, sequel::Dialect::DuckDb, namespace)?,
            policy: SqlPolicy::default(),
        })
    }
    /// Configure text collection, optional metadata and native analysis budgets.
    pub fn with_policy(mut self, policy: SqlPolicy) -> Self {
        self.policy = policy;
        self
    }
    /// Execute once, returning the native affected-row count without inspecting values.
    pub fn execute<P: duckdb::Params>(
        &self,
        parent: &Context,
        operation: &'static str,
        sql: &str,
        params: P,
    ) -> duckdb::Result<usize> {
        self.tracer
            .query(parent, self.policy.query(operation, sql), || {
                self.inner.borrow().execute(sql, params)
            })
    }
    /// Execute a batch without taking over native transaction or error semantics.
    pub fn execute_batch(
        &self,
        parent: &Context,
        operation: &'static str,
        sql: &str,
    ) -> duckdb::Result<()> {
        self.tracer
            .query(parent, self.policy.query(operation, sql), || {
                self.inner.borrow().execute_batch(sql)
            })
    }
    /// Fetch/decode one row with the original callback and native error unchanged.
    pub fn query_row<T, P: duckdb::Params, F: FnOnce(&duckdb::Row<'_>) -> duckdb::Result<T>>(
        &self,
        parent: &Context,
        operation: &'static str,
        sql: &str,
        params: P,
        row: F,
    ) -> duckdb::Result<T> {
        self.tracer
            .query(parent, self.policy.query(operation, sql), || {
                self.inner.borrow().query_row(sql, params, row)
            })
    }
    /// Collect decoded rows inside one query span, including callback/iteration
    /// errors. This method buffers results; native Arrow/appender APIs remain on
    /// the original connection and are not automatically instrumented.
    pub fn query_all<T, P: duckdb::Params, F: FnMut(&duckdb::Row<'_>) -> duckdb::Result<T>>(
        &self,
        parent: &Context,
        operation: &'static str,
        sql: &str,
        params: P,
        row: F,
    ) -> duckdb::Result<Vec<T>> {
        self.tracer
            .query(parent, self.policy.query(operation, sql), || {
                let mut statement = self.inner.borrow().prepare(sql)?;
                statement.query_map(params, row)?.collect()
            })
    }
    /// Return the original connection without closing it or committing a transaction.
    pub fn into_inner(self) -> C {
        self.inner
    }
}
