"""Shared native driver execution; never interpolate or inspect bind values."""

from __future__ import annotations

from contextlib import nullcontext

import sequel
from opentelemetry.instrumentation.utils import is_instrumentation_enabled

from . import sqltrace


def statement_text(statement: object, execute: object) -> str:
    if isinstance(statement, str):
        return statement
    if isinstance(statement, bytes):
        return statement.decode("utf-8", errors="strict")
    # psycopg.sql.Composable renders identifiers with connection-specific quoting.
    # It is never called on parameter values and never changes the driver's input.
    render = getattr(statement, "as_string", None)
    if callable(render):
        owner = getattr(execute, "__self__", None)
        rendered = render(getattr(owner, "connection", None))
        if isinstance(rendered, str):
            return rendered
    return ""


class Adapter:
    def __init__(self, system: str, dialect: sequel.Dialect):
        self.system = system
        self.dialect = dialect

    def _scope(self, operation, execute, statement, namer, namespace, policy, args=(), kwargs=None):
        if sqltrace._active.get() or not is_instrumentation_enabled():
            return nullcontext()
        try:
            sql = statement_text(statement, execute)
        except Exception:
            # Instrumentation must not alter driver failures or successful calls.
            sql = ""
        parameters = (
            args[0]
            if args
            else next((value for key, value in (kwargs or {}).items() if key in {"params", "parameters", "args"}), None)
        )
        many = getattr(execute, "__name__", "") == "executemany"
        if many and isinstance(parameters, (list, tuple)):
            parameters = parameters[0] if parameters else None
        count = len(parameters) if isinstance(parameters, (list, tuple, dict)) else None
        return (namer or sqltrace.Namer(sqltrace.storage_prefix(self.system))).query(
            operation,
            sql=sql,
            system=self.system,
            namespace=namespace,
            dialect=self.dialect,
            bind_count=count,
            **(policy or {}),
        )

    def sync(self, operation, execute, statement, args, kwargs, namer, namespace, policy):
        if sqltrace._active.get() or not is_instrumentation_enabled():
            return execute(statement, *args, **kwargs)
        with self._scope(operation, execute, statement, namer, namespace, policy, args, kwargs):
            return execute(statement, *args, **kwargs)

    async def asynchronous(self, operation, execute, statement, args, kwargs, namer, namespace, policy):
        if sqltrace._active.get() or not is_instrumentation_enabled():
            return await execute(statement, *args, **kwargs)
        with self._scope(operation, execute, statement, namer, namespace, policy, args, kwargs):
            return await execute(statement, *args, **kwargs)
