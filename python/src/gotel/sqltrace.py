"""Safe SQL metadata from the shared C scanner, plus application-owned spans."""

from __future__ import annotations

import re
import time
from collections.abc import Awaitable, Callable
from contextlib import contextmanager
from contextvars import ContextVar
from typing import ParamSpec, TypeVar

import sequel
from opentelemetry import metrics, trace
from opentelemetry.instrumentation.utils import is_instrumentation_enabled, suppress_instrumentation

from .application import GotelApp

_operation: ContextVar[str | None] = ContextVar("gotel.sql.operation", default=None)
_active: ContextVar[bool] = ContextVar("gotel.sql.active", default=False)
_batch: ContextVar[bool] = ContextVar("gotel.sql.batch", default=False)

_P = ParamSpec("_P")
_T = TypeVar("_T")


def storage_prefix(system: str) -> str:
    """Canonical span namespace; database semantic attributes keep their own names."""
    system = system.strip().lower()
    if system in {"postgresql", "postgres"}:
        system = "postgres"
    if not re.fullmatch(r"[a-z][a-z0-9_]*", system):
        raise ValueError("Invalid database system")
    return f"storage.{system}"


def _validate_name(operation: str) -> str:
    if not isinstance(operation, str) or not re.fullmatch(r"[A-Za-z][A-Za-z0-9_]*", operation):
        raise ValueError("Query name must match [A-Za-z][A-Za-z0-9_]*")
    return operation


@contextmanager
def name(operation: str):
    """Name one database execution without creating an additional span."""
    token = _operation.set(_validate_name(operation))
    try:
        yield
    finally:
        _operation.reset(token)


async def call(operation: str, execute: Callable[_P, Awaitable[_T]], /, *args: _P.args, **kwargs: _P.kwargs) -> _T:
    """Execute one async database call under a static, task-local query name."""
    with name(operation):
        token = _batch.set(getattr(execute, "__name__", "") in {"commit", "flush", "refresh", "get", "run_sync"})
        try:
            return await execute(*args, **kwargs)
        finally:
            _batch.reset(token)


def call_sync(operation: str, execute: Callable[_P, _T], /, *args: _P.args, **kwargs: _P.kwargs) -> _T:
    """Synchronous counterpart of call; arguments and results are unchanged."""
    with name(operation):
        return execute(*args, **kwargs)


def _attributes(sql, dialect, system, namespace, policy):
    attrs = {"db.system.name": system}
    if namespace:
        attrs["db.namespace"] = namespace
    try:
        query = sequel.inspect(sql, dialect=dialect, **policy)
    except (sequel.SQLInspectionError, TypeError):
        return attrs  # No raw SQL, exception text, or partial template on failure.
    attrs.update({"db.query.summary": query.summary, "gotel.db.query.statement_count": query.statement_count})
    if query.operation:
        attrs["db.operation.name"] = query.operation
    if query.text:
        # Keep the complete bounded template when layout would expand past the
        # collection cap. Formatting must not introduce silent truncation.
        limit = policy.get("max_query_text_bytes", 32768) or 32768
        formatted = query.text if query.truncated else sequel.format_sql_or_original(query.text, dialect=dialect)
        attrs["db.query.text"] = formatted if len(formatted.encode()) <= limit else query.text
    if query.truncated:
        attrs["gotel.db.query.truncated"] = True
    return attrs


class Namer:
    def __init__(self, prefix: str, *, tracer_provider=None, meter_provider=None):
        self.application = GotelApp(prefix, tracer_provider=tracer_provider)
        meter = metrics.get_meter(prefix, meter_provider=meter_provider)
        self.duration = meter.create_histogram("db.client.operation.duration", unit="s")

    @contextmanager
    def name(self, operation: str):
        with name(operation):
            yield

    @contextmanager
    def query(
        self,
        operation: str | None = None,
        *,
        sql: str,
        system: str,
        namespace: str = "",
        dialect: sequel.Dialect = sequel.Dialect.GENERIC,
        **policy,
    ):
        attrs = _attributes(sql, dialect, system, namespace, policy)
        operation = operation or _operation.get() or "Query"
        if _batch.get():
            # One ORM flush can write several tables. Keep those query identities
            # separate without incorporating values, object ids or SQL hashes.
            verb = attrs.get("db.operation.name", "SQL").title()
            match = re.search(
                r'\b(?:FROM|INTO|UPDATE|TABLE)\s+["`]?([A-Za-z_][A-Za-z0-9_.]*)',
                attrs.get("db.query.summary", ""),
                re.IGNORECASE,
            )
            table = "" if match is None else "".join(part.title() for part in match[1].split(".")[-1].split("_"))
            operation += verb + table
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
    engine,
    *,
    namer: Namer,
    system: str,
    namespace: str = "",
    dialect: sequel.Dialect = sequel.Dialect.GENERIC,
    **policy,
):
    """Instrument one sync/async engine. Returns an idempotent removal callback.

    Disable the upstream SQLAlchemy instrumentor for this engine before attaching.
    The span covers cursor execution, including failures, rather than result consumption.
    """
    from sqlalchemy import event
    from sqlalchemy.orm import Session

    engine = getattr(engine, "sync_engine", engine)
    if getattr(engine, "_gotel_remove", None):
        return engine._gotel_remove

    def orm_execute(state):
        if not state.is_relationship_load:
            return
        bind = state.session.get_bind(**state.bind_arguments)
        if getattr(bind, "engine", bind) is not engine:
            return
        base = state.execution_options.get("gotel_query_name") or _operation.get()
        if not base:
            base = state.bind_mapper.class_.__name__
        keys = [part.key for part in state.loader_strategy_path.path if hasattr(part, "key")]
        suffix = "".join(word.title() for key in keys for word in key.split("_"))
        state.update_execution_options(gotel_query_name=base + "Load" + suffix)

    def before(conn, cursor, statement, parameters, execution_context, executemany):
        if _active.get() or not is_instrumentation_enabled():
            return
        scope = namer.query(
            execution_context.execution_options.get("gotel_query_name"),
            sql=statement,
            system=system,
            namespace=namespace,
            dialect=dialect,
            **policy,
        )
        batch_token = _batch.set(
            _batch.get() or execution_context.isinsert or execution_context.isupdate or execution_context.isdelete
        )
        try:
            scope.__enter__()
        finally:
            _batch.reset(batch_token)
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
    event.listen(Session, "do_orm_execute", orm_execute)

    def remove():
        if getattr(engine, "_gotel_remove", None) is remove:
            for name, callback in listeners:
                event.remove(engine, name, callback)
            event.remove(Session, "do_orm_execute", orm_execute)
            del engine._gotel_remove

    engine._gotel_remove = remove
    return remove
