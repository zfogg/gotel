"""Native ClickHouse sync/async tracing for asynch and clickhouse clients."""

from __future__ import annotations

import inspect
from functools import wraps

import sequel

from ._native import Adapter

_adapter = Adapter("clickhouse", sequel.Dialect.CLICKHOUSE)


def call_sync(operation, execute, /, statement, *args, namer=None, namespace="", policy=None, **kwargs):
    """Trace one native sync query, forwarding arguments/results/errors unchanged.

    namer defaults to storage.clickhouse. policy is a Sequel options dictionary.
    namespace is an optional stable database name. Bind values are never retained.
    """
    return _adapter.sync(operation, execute, statement, args, kwargs, namer, namespace, policy)


async def call(operation, execute, /, statement, *args, namer=None, namespace="", policy=None, **kwargs):
    """Async call_sync; native cancellation propagates unchanged."""
    return await _adapter.asynchronous(operation, execute, statement, args, kwargs, namer, namespace, policy)


def instrument(client, *, namer=None, namespace="", policy=None):
    """Instrument native query methods; return an idempotent removal callback.

    Attach once before use and remove after active calls finish. ORM suppression
    prevents duplicate spans. Insert APIs accept table/data instead of SQL and
    should use call/call_sync explicitly with an application-owned safe template.
    """
    previous = getattr(client, "_gotel_clickhouse_remove", None)
    if previous is not None:
        return previous
    originals = {}

    def wrap(execute):
        if inspect.iscoroutinefunction(execute):

            @wraps(execute)
            async def asynchronous(*args, **kwargs):
                statement = args[0] if args else kwargs.get("query", kwargs.get("cmd", ""))
                if args:
                    return await call(None, execute, *args, namer=namer, namespace=namespace, policy=policy, **kwargs)
                with _adapter._scope(None, execute, statement, namer, namespace, policy, (), kwargs):
                    return await execute(**kwargs)

            return asynchronous

        @wraps(execute)
        def synchronous(*args, **kwargs):
            statement = args[0] if args else kwargs.get("query", kwargs.get("cmd", ""))
            if args:
                return call_sync(None, execute, *args, namer=namer, namespace=namespace, policy=policy, **kwargs)
            with _adapter._scope(None, execute, statement, namer, namespace, policy, (), kwargs):
                return execute(**kwargs)

        return synchronous

    try:
        for method in ("execute", "executemany", "query", "command", "query_df", "query_arrow", "query_np"):
            execute = getattr(client, method, None)
            if callable(execute):
                originals[method] = (method in vars(client), execute)
                setattr(client, method, wrap(execute))
    except Exception:
        for method, (owned, execute) in originals.items():
            if owned:
                setattr(client, method, execute)
            else:
                delattr(client, method)
        raise

    def remove():
        if getattr(client, "_gotel_clickhouse_remove", None) is not remove:
            return
        for method, (owned, execute) in originals.items():
            if owned:
                setattr(client, method, execute)
            else:
                delattr(client, method)
        delattr(client, "_gotel_clickhouse_remove")

    client._gotel_clickhouse_remove = remove
    return remove


def cursor_factory(base, *, namer=None, namespace="", policy=None):
    """Return an asynch-compatible Cursor subclass with an instrumented execute."""

    class Cursor(base):
        async def execute(self, query, *args, **kwargs):
            return await call(
                None, super().execute, query, *args, namer=namer, namespace=namespace, policy=policy, **kwargs
            )

        async def executemany(self, query, *args, **kwargs):
            return await call(
                None, super().executemany, query, *args, namer=namer, namespace=namespace, policy=policy, **kwargs
            )

    return Cursor
