//! rusqlite 0.32 execute, batch and typed single-row instrumentation.
//!
//! Parameter objects and row callbacks are passed directly to rusqlite. Native
//! errors are returned intact. Wrap `&*transaction` to keep native transaction
//! ownership and commit/rollback semantics with the application.
use super::SqlPolicy;
use crate::{Application, NamingError, SqlTracer};
use opentelemetry::Context;
use std::borrow::Borrow;

/// An owned or borrowed SQLite connection using Gotel's shared SQL analysis.
pub struct Connection<C> {
    inner: C,
    tracer: SqlTracer,
    policy: SqlPolicy,
}
impl<C: Borrow<rusqlite::Connection>> Connection<C> {
    /// Create an adapter with the SQLite dialect forced by the actual driver.
    pub fn new(
        inner: C,
        app: &Application,
        prefix: &str,
        namespace: &str,
    ) -> Result<Self, NamingError> {
        Ok(Self {
            inner,
            tracer: app.database(prefix, sequel::Dialect::Sqlite, namespace)?,
            policy: SqlPolicy::default(),
        })
    }
    /// Configure text collection, optional metadata and native resource budgets.
    pub fn with_policy(mut self, policy: SqlPolicy) -> Self {
        self.policy = policy;
        self
    }
    /// Execute once with original parameters and affected-row count.
    pub fn execute<P: rusqlite::Params>(
        &self,
        parent: &Context,
        operation: &'static str,
        sql: &str,
        params: P,
    ) -> rusqlite::Result<usize> {
        self.tracer
            .query(parent, self.policy.query(operation, sql), || {
                self.inner.borrow().execute(sql, params)
            })
    }
    /// Execute a native SQL batch once, retaining rusqlite's transaction behavior.
    pub fn execute_batch(
        &self,
        parent: &Context,
        operation: &'static str,
        sql: &str,
    ) -> rusqlite::Result<()> {
        self.tracer
            .query(parent, self.policy.query(operation, sql), || {
                self.inner.borrow().execute_batch(sql)
            })
    }
    /// Fetch and decode a native row; callback/driver errors are unchanged.
    pub fn query_row<
        T,
        P: rusqlite::Params,
        F: FnOnce(&rusqlite::Row<'_>) -> rusqlite::Result<T>,
    >(
        &self,
        parent: &Context,
        operation: &'static str,
        sql: &str,
        params: P,
        row: F,
    ) -> rusqlite::Result<T> {
        self.tracer
            .query(parent, self.policy.query(operation, sql), || {
                self.inner.borrow().query_row(sql, params, row)
            })
    }
    /// Return the original connection, without closing or committing it.
    pub fn into_inner(self) -> C {
        self.inner
    }
}
