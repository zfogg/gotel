"""Safe SQL metadata from the shared C scanner, plus application-owned spans."""

from __future__ import annotations

import time
from contextlib import contextmanager
from contextvars import ContextVar
from dataclasses import dataclass
from enum import IntEnum

from opentelemetry import metrics, trace
from opentelemetry.instrumentation.utils import is_instrumentation_enabled, suppress_instrumentation

from . import _sql
from .application import Application


class Dialect(IntEnum):
    GENERIC = 0
    SQLITE = 1
    POSTGRESQL = 2
    DUCKDB = 3
    MYSQL = 4
    CLICKHOUSE = 5


class SQLInspectionError(ValueError):
    pass


@dataclass(frozen=True)
class Query:
    text: str
    summary: str
    operation: str
    truncated: bool
    statement_count: int


def inspect(
    sql: str,
    *,
    dialect: Dialect = Dialect.GENERIC,
    max_query_text_bytes: int = 32768,
    omit_query_text: bool = False,
    keep_in_lists: bool = False,
) -> Query:
    try:
        return Query(*_sql.inspect(sql, int(Dialect(dialect)), max_query_text_bytes, omit_query_text, keep_in_lists))
    except ValueError as exc:
        raise SQLInspectionError("SQL inspection rejected input") from exc


def format_sql(sql: str, *, dialect: Dialect = Dialect.GENERIC) -> str:
    return _sql.format(sql, int(Dialect(dialect)))


def format_sql_or_original(sql: str, *, dialect: Dialect = Dialect.GENERIC) -> str:
    try:
        return format_sql(sql, dialect=dialect)
    except ValueError:
        return sql


_operation: ContextVar[str | None] = ContextVar("gotel.sql.operation", default=None)
_active: ContextVar[bool] = ContextVar("gotel.sql.active", default=False)


def _attributes(sql, dialect, system, namespace, policy):
    attrs = {"db.system.name": system}
    if namespace:
        attrs["db.namespace"] = namespace
    try:
        query = inspect(sql, dialect=dialect, **policy)
    except (SQLInspectionError, TypeError):
        return attrs  # No raw SQL, exception text, or partial template on failure.
    attrs.update({"db.query.summary": query.summary, "gotel.db.query.statement_count": query.statement_count})
    if query.operation:
        attrs["db.operation.name"] = query.operation
    if query.text:
        # Keep the complete bounded template when layout would expand past the
        # collection cap. Formatting must not introduce silent truncation.
        limit = policy.get("max_query_text_bytes", 32768) or 32768
        formatted = query.text if query.truncated else format_sql_or_original(query.text, dialect=dialect)
        attrs["db.query.text"] = formatted if len(formatted.encode()) <= limit else query.text
    if query.truncated:
        attrs["gotel.db.query.truncated"] = True
    return attrs


class Namer:
    def __init__(self, prefix: str, *, tracer_provider=None, meter_provider=None):
        self.application = Application(prefix, tracer_provider=tracer_provider)
        meter = metrics.get_meter(prefix, meter_provider=meter_provider)
        self.duration = meter.create_histogram("db.client.operation.duration", unit="s")

    @contextmanager
    def name(self, operation: str):
        token = _operation.set(operation)
        try:
            yield
        finally:
            _operation.reset(token)

    @contextmanager
    def query(
        self,
        operation: str,
        *,
        sql: str,
        system: str,
        namespace: str = "",
        dialect: Dialect = Dialect.GENERIC,
        **policy,
    ):
        attrs = _attributes(sql, dialect, system, namespace, policy)
        started = time.monotonic()
        # Database exceptions frequently contain bind values. Record only the type.
        with self.application.tracer("sql").start_as_current_span(
            self.application.qualify(operation),
            kind=trace.SpanKind.CLIENT,
            attributes=attrs,
            record_exception=False,
            set_status_on_exception=False,
        ) as span:
            token = _active.set(True)
            try:
                with suppress_instrumentation():
                    yield span
            except BaseException as exc:
                span.set_attribute("error.type", type(exc).__qualname__)
                span.set_status(trace.Status(trace.StatusCode.ERROR))
                raise
            finally:
                _active.reset(token)
                self.duration.record(time.monotonic() - started, {"db.system.name": system})


def instrument_sqlalchemy(
    engine, *, namer: Namer, system: str, namespace: str = "", dialect: Dialect = Dialect.GENERIC, **policy
):
    """Instrument one sync/async engine. Returns an idempotent removal callback.

    Disable the upstream SQLAlchemy instrumentor for this engine before attaching.
    The span covers cursor execution, including failures, rather than result consumption.
    """
    from sqlalchemy import event

    engine = getattr(engine, "sync_engine", engine)
    if getattr(engine, "_gotel_remove", None):
        return engine._gotel_remove

    def before(conn, cursor, statement, parameters, execution_context, executemany):
        if _active.get() or not is_instrumentation_enabled():
            return
        scope = namer.query(
            _operation.get() or "Query", sql=statement, system=system, namespace=namespace, dialect=dialect, **policy
        )
        scope.__enter__()
        execution_context._gotel_scope = scope

    def finish(execution_context, error=None):
        scope = getattr(execution_context, "_gotel_scope", None)
        if scope is not None:
            execution_context._gotel_scope = None
            scope.__exit__(type(error) if error else None, error, error.__traceback__ if error else None)

    def after(conn, cursor, statement, parameters, execution_context, executemany):
        finish(execution_context)

    def failed(exception_context):
        finish(exception_context.execution_context, exception_context.original_exception)

    listeners = [("before_cursor_execute", before), ("after_cursor_execute", after), ("handle_error", failed)]
    for name, callback in listeners:
        event.listen(engine, name, callback)

    def remove():
        if getattr(engine, "_gotel_remove", None) is remove:
            for name, callback in listeners:
                event.remove(engine, name, callback)
            del engine._gotel_remove

    engine._gotel_remove = remove
    return remove
