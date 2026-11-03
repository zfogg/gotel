"""Native psycopg sync/async calls, including SQL Composable statements.

Pass the original bound cursor method. Parameters, return values, exceptions,
and cancellation are preserved. No psycopg import is required at module load.
"""

from __future__ import annotations

import sequel

from ._native import Adapter

_adapter = Adapter("postgresql", sequel.Dialect.POSTGRESQL)


def call_sync(operation, execute, /, statement, *args, namer=None, namespace="", policy=None, **kwargs):
    """Trace one native execution. policy contains Sequel inspection options."""
    return _adapter.sync(operation, execute, statement, args, kwargs, namer, namespace, policy)


async def call(operation, execute, /, statement, *args, namer=None, namespace="", policy=None, **kwargs):
    """Await one native execution without wrapping or consuming returned rows."""
    return await _adapter.asynchronous(operation, execute, statement, args, kwargs, namer, namespace, policy)
