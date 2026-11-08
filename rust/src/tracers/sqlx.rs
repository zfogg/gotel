//! SQLx 0.8 `Executor` adapter for pools, connections and transactions.
//!
//! Native queries and typed query builders continue using their ordinary methods:
//! `query.bind(value).fetch_one(tracer.wrap(&parent, "Read", &pool)).await`.
//! Streaming spans stay open until EOF, error or drop. Parameters are never taken
//! from the query to inspect them. Native transactions remain application-owned;
//! wrap `&mut *transaction` as required by SQLx's executor trait.
use super::SqlPolicy;
use crate::{Application, NamingError, Operation, Outcome, SqlTracer};
use futures_core::{future::BoxFuture, stream::BoxStream, Stream};
use opentelemetry::{trace::FutureExt, Context};
use sqlx::{Database, Describe, Either, Error, Execute};
use std::{
    fmt,
    marker::PhantomData,
    pin::Pin,
    sync::Arc,
    task::{Context as TaskContext, Poll},
};

/// Driver-specific SQL tracer. Clones share only providers and metric handles.
pub struct Tracer<DB: Database> {
    tracer: Arc<SqlTracer>,
    policy: SqlPolicy,
    marker: PhantomData<fn() -> DB>,
}
impl<DB: Database> Clone for Tracer<DB> {
    fn clone(&self) -> Self {
        Self {
            tracer: self.tracer.clone(),
            policy: self.policy,
            marker: PhantomData,
        }
    }
}
impl<DB: Database> Tracer<DB> {
    /// Select dialect from SQLx's database type, never from caller-supplied SQL.
    pub fn new(app: &Application, prefix: &str, namespace: &str) -> Result<Self, NamingError> {
        let dialect = match DB::NAME {
            "PostgreSQL" => sequel::Dialect::PostgreSql,
            "MySQL" => sequel::Dialect::MySql,
            "SQLite" => sequel::Dialect::Sqlite,
            _ => sequel::Dialect::Generic,
        };
        Ok(Self {
            tracer: Arc::new(app.database(prefix, dialect, namespace)?),
            policy: SqlPolicy::default(),
            marker: PhantomData,
        })
    }
    /// Configure text collection, optional metadata and native resource budgets.
    pub fn with_policy(mut self, policy: SqlPolicy) -> Self {
        self.policy = policy;
        self
    }
    /// Wrap one native executor with an explicit parent and source-owned operation.
    pub fn wrap<'c, E: sqlx::Executor<'c, Database = DB>>(
        &self,
        parent: &Context,
        operation: &'static str,
        executor: E,
    ) -> Executor<E> {
        Executor {
            inner: executor,
            tracer: self.tracer.clone(),
            parent: parent.clone(),
            operation,
            policy: self.policy,
        }
    }
}
/// A native SQLx executor with analysis and stream/future lifetime instrumentation.
pub struct Executor<E> {
    inner: E,
    tracer: Arc<SqlTracer>,
    parent: Context,
    operation: &'static str,
    policy: SqlPolicy,
}
impl<E> fmt::Debug for Executor<E> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("GotelExecutor").finish_non_exhaustive()
    }
}
fn finish<T>(operation: Operation, result: &Result<T, Error>) {
    operation.finish(
        if result.is_ok() {
            Outcome::Success
        } else {
            Outcome::Error
        },
        result.as_ref().err().map(|_| "sqlx::Error"),
    );
}
fn future<'a, T: 'a>(
    op: Operation,
    future: BoxFuture<'a, Result<T, Error>>,
) -> BoxFuture<'a, Result<T, Error>> {
    Box::pin(async move {
        let result = future.with_context(op.context().clone()).await;
        finish(op, &result);
        result
    })
}
fn stream<'a, T: 'a>(
    op: Operation,
    inner: BoxStream<'a, Result<T, Error>>,
) -> BoxStream<'a, Result<T, Error>> {
    Box::pin(ObservedStream {
        inner,
        operation: Some(op),
    })
}
pin_project_lite::pin_project! {
    struct ObservedStream<S> { #[pin] inner: S, operation: Option<Operation> }
}
impl<S: Stream<Item = Result<T, Error>>, T> Stream for ObservedStream<S> {
    type Item = Result<T, Error>;
    fn poll_next(self: Pin<&mut Self>, cx: &mut TaskContext<'_>) -> Poll<Option<Self::Item>> {
        let this = self.project();
        let _guard = this
            .operation
            .as_ref()
            .map(|op| op.context().clone().attach());
        let result = this.inner.poll_next(cx);
        if let Poll::Ready(Some(Err(_))) = &result {
            if let Some(op) = this.operation.take() {
                op.finish(Outcome::Error, Some("sqlx::Error"));
            }
        } else if let Poll::Ready(None) = result {
            if let Some(op) = this.operation.take() {
                op.finish(Outcome::Success, None);
            }
        }
        result
    }
    fn size_hint(&self) -> (usize, Option<usize>) {
        self.inner.size_hint()
    }
}
impl<'c, E: sqlx::Executor<'c>> sqlx::Executor<'c> for Executor<E> {
    type Database = E::Database;
    fn execute<'e, 'q: 'e, Q>(
        self,
        query: Q,
    ) -> BoxFuture<'e, Result<<Self::Database as Database>::QueryResult, Error>>
    where
        'c: 'e,
        Q: 'q + Execute<'q, Self::Database>,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, query.sql()));
        future(op, self.inner.execute(query))
    }
    fn execute_many<'e, 'q: 'e, Q>(
        self,
        query: Q,
    ) -> BoxStream<'e, Result<<Self::Database as Database>::QueryResult, Error>>
    where
        'c: 'e,
        Q: 'q + Execute<'q, Self::Database>,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, query.sql()));
        stream(op, self.inner.execute_many(query))
    }
    fn fetch<'e, 'q: 'e, Q>(
        self,
        query: Q,
    ) -> BoxStream<'e, Result<<Self::Database as Database>::Row, Error>>
    where
        'c: 'e,
        Q: 'q + Execute<'q, Self::Database>,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, query.sql()));
        stream(op, self.inner.fetch(query))
    }
    fn fetch_many<'e, 'q: 'e, Q>(
        self,
        query: Q,
    ) -> BoxStream<
        'e,
        Result<
            Either<<Self::Database as Database>::QueryResult, <Self::Database as Database>::Row>,
            Error,
        >,
    >
    where
        'c: 'e,
        Q: 'q + Execute<'q, Self::Database>,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, query.sql()));
        stream(op, self.inner.fetch_many(query))
    }
    fn fetch_all<'e, 'q: 'e, Q>(
        self,
        query: Q,
    ) -> BoxFuture<'e, Result<Vec<<Self::Database as Database>::Row>, Error>>
    where
        'c: 'e,
        Q: 'q + Execute<'q, Self::Database>,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, query.sql()));
        future(op, self.inner.fetch_all(query))
    }
    fn fetch_one<'e, 'q: 'e, Q>(
        self,
        query: Q,
    ) -> BoxFuture<'e, Result<<Self::Database as Database>::Row, Error>>
    where
        'c: 'e,
        Q: 'q + Execute<'q, Self::Database>,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, query.sql()));
        future(op, self.inner.fetch_one(query))
    }
    fn fetch_optional<'e, 'q: 'e, Q>(
        self,
        query: Q,
    ) -> BoxFuture<'e, Result<Option<<Self::Database as Database>::Row>, Error>>
    where
        'c: 'e,
        Q: 'q + Execute<'q, Self::Database>,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, query.sql()));
        future(op, self.inner.fetch_optional(query))
    }
    fn prepare_with<'e, 'q: 'e>(
        self,
        sql: &'q str,
        parameters: &'e [<Self::Database as Database>::TypeInfo],
    ) -> BoxFuture<'e, Result<<Self::Database as Database>::Statement<'q>, Error>>
    where
        'c: 'e,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, sql));
        future(op, self.inner.prepare_with(sql, parameters))
    }
    fn describe<'e, 'q: 'e>(
        self,
        sql: &'q str,
    ) -> BoxFuture<'e, Result<Describe<Self::Database>, Error>>
    where
        'c: 'e,
    {
        let op = self
            .tracer
            .start(&self.parent, self.policy.query(self.operation, sql));
        future(op, self.inner.describe(sql))
    }
}
