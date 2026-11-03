"""Native transaction/savepoint and pool context-manager lifetimes.

Wrap the driver's actual transaction()/pool.connection() context manager. The
driver remains responsible for commit/rollback and its exception semantics.
Queries inside inherit the lifetime span; lifecycle calls suppress SQL wrappers.
"""

from __future__ import annotations

import time
from contextlib import asynccontextmanager, contextmanager

from opentelemetry import metrics, trace
from opentelemetry.instrumentation.utils import suppress_instrumentation

from .application import GotelApp
from .outcomes import Outcome, apply, classify
from .sqltrace import storage_prefix


@contextmanager
def _operation(application: GotelApp, system, namespace, name):
    attrs = {"db.system.name": system, "db.operation.name": name}
    if namespace:
        attrs["db.namespace"] = namespace
    started = time.monotonic()
    result = Outcome.SUCCESS
    with application.tracer("storage").start_as_current_span(
        f"{storage_prefix(system)}.{name}",
        kind=trace.SpanKind.CLIENT,
        attributes=attrs,
        record_exception=False,
        set_status_on_exception=False,
    ) as span:
        try:
            yield span
        except BaseException as error:
            result = classify(error)
            apply(span, result, error)
            raise
        finally:
            if result == Outcome.SUCCESS:
                apply(span, result)
            metrics.get_meter("gotel/dbtrace").create_histogram("db.client.operation.duration", unit="s").record(
                time.monotonic() - started,
                {"db.system.name": system, "db.operation.name": name, "operation.outcome": result.value},
            )


@contextmanager
def transaction(manager, *, application: GotelApp, system="postgresql", namespace="", savepoint=False):
    """Trace a sync native transaction from enter through commit/rollback.

    Set savepoint=True when wrapping a driver's nested transaction manager.
    Native suppression and returned manager values are preserved exactly.
    """
    lifetime, begin, commit, rollback = (
        ("SavepointTransaction", "Savepoint", "ReleaseSavepoint", "RollbackSavepoint")
        if savepoint
        else ("Transaction", "BeginTransaction", "CommitTransaction", "RollbackTransaction")
    )
    with _operation(application, system, namespace, lifetime):
        with _operation(application, system, namespace, begin), suppress_instrumentation():
            value = manager.__enter__()
        try:
            yield value
        except BaseException as error:
            with _operation(application, system, namespace, rollback), suppress_instrumentation():
                suppressed = manager.__exit__(type(error), error, error.__traceback__)
            if not suppressed:
                raise
        else:
            with _operation(application, system, namespace, commit), suppress_instrumentation():
                manager.__exit__(None, None, None)


@asynccontextmanager
async def transaction_async(manager, *, application: GotelApp, system="postgresql", namespace="", savepoint=False):
    """Async native transaction lifetime, preserving cancellation and cleanup."""
    lifetime, begin, commit, rollback = (
        ("SavepointTransaction", "Savepoint", "ReleaseSavepoint", "RollbackSavepoint")
        if savepoint
        else ("Transaction", "BeginTransaction", "CommitTransaction", "RollbackTransaction")
    )
    with _operation(application, system, namespace, lifetime):
        with _operation(application, system, namespace, begin), suppress_instrumentation():
            value = await manager.__aenter__()
        try:
            yield value
        except BaseException as error:
            with _operation(application, system, namespace, rollback), suppress_instrumentation():
                suppressed = await manager.__aexit__(type(error), error, error.__traceback__)
            if not suppressed:
                raise
        else:
            with _operation(application, system, namespace, commit), suppress_instrumentation():
                await manager.__aexit__(None, None, None)


@contextmanager
def connection(manager, *, application: GotelApp, system="postgresql", namespace=""):
    """Trace pool wait, connection lifetime and release using the native manager."""
    with _operation(application, system, namespace, "PoolConnection"):
        with _operation(application, system, namespace, "PoolAcquire") as span, suppress_instrumentation():
            started = time.monotonic()
            try:
                value = manager.__enter__()
            finally:
                span.set_attribute("db.client.connection.acquire_wait_seconds", time.monotonic() - started)
        try:
            yield value
        except BaseException as error:
            with _operation(application, system, namespace, "PoolRelease"), suppress_instrumentation():
                suppressed = manager.__exit__(type(error), error, error.__traceback__)
            if not suppressed:
                raise
        else:
            with _operation(application, system, namespace, "PoolRelease"), suppress_instrumentation():
                manager.__exit__(None, None, None)


@asynccontextmanager
async def connection_async(manager, *, application: GotelApp, system="postgresql", namespace=""):
    """Async pool lifetime, measuring unsuccessful acquisition waits as well."""
    with _operation(application, system, namespace, "PoolConnection"):
        with _operation(application, system, namespace, "PoolAcquire") as span, suppress_instrumentation():
            started = time.monotonic()
            try:
                value = await manager.__aenter__()
            finally:
                span.set_attribute("db.client.connection.acquire_wait_seconds", time.monotonic() - started)
        try:
            yield value
        except BaseException as error:
            with _operation(application, system, namespace, "PoolRelease"), suppress_instrumentation():
                suppressed = await manager.__aexit__(type(error), error, error.__traceback__)
            if not suppressed:
                raise
        else:
            with _operation(application, system, namespace, "PoolRelease"), suppress_instrumentation():
                await manager.__aexit__(None, None, None)
