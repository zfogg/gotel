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

from ._sqlanalysis import CacheStats as CacheStats
from ._sqlanalysis import attributes as _analysis_attributes
from ._sqlanalysis import configure_inspection_cache as configure_inspection_cache
from ._sqlanalysis import inspection_cache_stats as inspection_cache_stats
from .application import GotelApp
from .outcomes import Outcome, apply, classify

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
    return _analysis_attributes(sql, dialect, system, namespace, policy)


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
        bind_count: int | None = None,
        **policy,
    ):
        attrs = _attributes(sql, dialect, system, namespace, policy)
        if bind_count is not None:
            attrs["gotel.db.query.bind_count"] = bind_count
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
            outcome = Outcome.SUCCESS
            try:
                with suppress_instrumentation():
                    yield span
            except BaseException as exc:
                outcome = classify(exc)
                apply(span, outcome, exc)
                if outcome == Outcome.ERROR:
                    # Native driver messages can contain SQL/bind values. Keep
                    # validated SQLSTATE/numeric codes and positions only.
                    try:
                        state = getattr(exc, "sqlstate", None)
                        code = getattr(exc, "code", None)
                        if isinstance(state, str) and re.fullmatch(r"[0-9A-Z]{5}", state):
                            span.set_attribute("db.response.status_code", state)
                        elif isinstance(code, int) and 0 <= code <= 2147483647:
                            span.set_attribute("db.response.status_code", str(code))
                        position = getattr(getattr(exc, "diag", None), "statement_position", None)
                        if (
                            isinstance(position, str)
                            and position.isascii()
                            and position.isdigit()
                            and len(position) <= 10
                        ):
                            span.set_attribute("db.response.error.position", int(position))
                    except Exception:
                        pass  # A driver's diagnostic accessor cannot replace its original exception.
                raise
            finally:
                if outcome == Outcome.SUCCESS:
                    apply(span, outcome)
                _active.reset(token)
                self.duration.record(
                    time.monotonic() - started, {"db.system.name": system, "operation.outcome": outcome.value}
                )


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
        binds = parameters
        if executemany and isinstance(binds, (list, tuple)):
            binds = binds[0] if binds else ()
        bind_count = len(binds) if isinstance(binds, (dict, list, tuple)) else None
        scope = namer.query(
            execution_context.execution_options.get("gotel_query_name"),
            sql=statement,
            system=system,
            namespace=namespace,
            dialect=dialect,
            bind_count=bind_count,
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
