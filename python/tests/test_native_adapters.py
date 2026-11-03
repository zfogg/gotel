import asyncio
import sqlite3

import pytest
from opentelemetry import trace
from opentelemetry.instrumentation.utils import suppress_instrumentation

from gotel import clickhousetrace, psycopgtrace, sqltrace
from gotel.outcomes import Outcome, completion
from gotel.testing import Capture


def namer(capture, system="postgres"):
    return sqltrace.Namer(f"storage.{system}", tracer_provider=capture.provider)


def test_sync_executes_real_driver_preserves_results_and_values():
    connection = sqlite3.connect(":memory:")
    try:
        cursor = connection.cursor()
        with Capture("") as capture:
            names = namer(capture)
            returned = psycopgtrace.call_sync(
                "FindAccount",
                cursor.execute,
                "SELECT ? AS token, 'LITERAL_SECRET' AS literal",
                ("BIND_SECRET",),
                namer=names,
                namespace="fixture",
            )
            assert returned is cursor
            assert cursor.fetchone() == ("BIND_SECRET", "LITERAL_SECRET")
            span = capture.require_span("storage.postgres.FindAccount")
            assert span.attributes["db.namespace"] == "fixture"
            assert span.attributes["gotel.db.query.bind_count"] == 1
            assert span.attributes["operation.outcome"] == "success"
            capture.require_no_values("BIND_SECRET", "LITERAL_SECRET")
            with pytest.raises(sqlite3.OperationalError):
                psycopgtrace.call_sync("Missing", cursor.execute, "SELECT * FROM missing", namer=names)
            assert capture.spans[-1].attributes["operation.outcome"] == "error"
            assert capture.spans[-1].status.status_code == trace.StatusCode.ERROR
            assert not capture.spans[-1].events
    finally:
        connection.close()


def test_composable_receives_native_connection_and_original_input():
    class Composable:
        def as_string(self, context):
            assert context is cursor.connection
            return "SELECT %(token)s, 'LITERAL_SECRET'"

    class Cursor:
        connection = object()

        def execute(self, statement, parameters, *, prepare=False):
            assert statement is query
            assert parameters is binds
            assert prepare
            return sentinel

    cursor, query, binds, sentinel = Cursor(), Composable(), {"token": "BIND_SECRET"}, object()
    with Capture("") as capture:
        assert (
            psycopgtrace.call_sync("Lookup", cursor.execute, query, binds, prepare=True, namer=namer(capture))
            is sentinel
        )
        assert capture.spans[0].attributes["gotel.db.query.bind_count"] == 1
        capture.require_no_values("BIND_SECRET", "LITERAL_SECRET")


async def test_async_calls_are_task_local_and_cancelled_error_identity():
    gate = asyncio.Event()
    calls = []

    async def execute(statement, parameters):
        calls.append((statement, parameters))
        await gate.wait()
        return parameters

    with Capture("") as capture:
        names = namer(capture)
        tasks = [
            asyncio.create_task(psycopgtrace.call(operation, execute, "SELECT %s", (secret,), namer=names))
            for operation, secret in (("One", "PRIVATE_ONE"), ("Two", "PRIVATE_TWO"))
        ]
        await asyncio.sleep(0)
        tasks[0].cancel()
        with pytest.raises(asyncio.CancelledError):
            await tasks[0]
        gate.set()
        assert await tasks[1] == ("PRIVATE_TWO",)
        cancelled = capture.require_span("storage.postgres.One")
        assert cancelled.attributes["operation.outcome"] == "cancelled"
        assert cancelled.status.status_code != trace.StatusCode.ERROR
        assert capture.require_span("storage.postgres.Two").attributes["operation.outcome"] == "success"
        capture.require_no_values("PRIVATE_ONE", "PRIVATE_TWO")
        assert len(calls) == 2


def test_explicit_deferred_preserves_exception_and_suppression():
    error = TimeoutError("SECRET parameters")

    def fail(statement):
        raise error

    with Capture("") as capture:
        names = namer(capture)
        with completion(Outcome.DEFERRED), pytest.raises(TimeoutError) as caught:
            psycopgtrace.call_sync("Deferred", fail, "SELECT 1", namer=names)
        assert caught.value is error
        assert capture.spans[0].attributes["operation.outcome"] == "deferred"
        assert capture.spans[0].status.status_code != trace.StatusCode.ERROR
        with suppress_instrumentation(), pytest.raises(TimeoutError):
            psycopgtrace.call_sync("Suppressed", fail, "SELECT 1", namer=names)
        assert len(capture.spans) == 1
        capture.require_no_values("SECRET parameters")


def test_clickhouse_instrument_sync_keywords_nesting_removal():
    class Client:
        def query(self, query, parameters=None):
            return self.execute(query, parameters)

        def execute(self, query, parameters=None):
            assert query == "SELECT %(token)s"
            return parameters

    client = Client()
    with Capture("") as capture:
        names = namer(capture, "clickhouse")
        remove = clickhousetrace.instrument(client, namer=names, namespace="analytics")
        assert clickhousetrace.instrument(client, namer=names) is remove
        with sqltrace.name("Find"):
            assert client.query(query="SELECT %(token)s", parameters={"token": "SECRET"}) == {"token": "SECRET"}
        assert len(capture.spans) == 1
        assert capture.spans[0].name == "storage.clickhouse.Find"
        assert capture.spans[0].attributes["gotel.db.query.bind_count"] == 1
        with suppress_instrumentation():
            client.query(query="SELECT %(token)s")
        assert len(capture.spans) == 1
        remove()
        remove()
        assert "query" not in vars(client) and "execute" not in vars(client)
        client.query("SELECT %(token)s")
        assert len(capture.spans) == 1
        capture.require_no_values("SECRET")


async def test_clickhouse_cursor_factory_executemany_and_async_instrument():
    class Cursor:
        def __init__(self, connection):
            self.connection = connection

        async def execute(self, query, args=None, context=None):
            return args, context

        async def executemany(self, query, args):
            return await self.execute(query, args)

    with Capture("") as capture:
        names = namer(capture, "clickhouse")
        traced = clickhousetrace.cursor_factory(Cursor, namer=names)
        cursor = traced("connection")
        assert isinstance(cursor, Cursor)
        with sqltrace.name("InsertBatch"):
            assert await cursor.executemany("INSERT INTO events VALUES (%s,%s)", [(1, "SECRET")]) == (
                [(1, "SECRET")],
                None,
            )
        assert len(capture.spans) == 1
        assert capture.spans[0].attributes["gotel.db.query.bind_count"] == 2
        remove = clickhousetrace.instrument(cursor, namer=names)
        await cursor.execute(query="SELECT 1")
        assert len(capture.spans) == 2
        remove()
        capture.require_no_values("SECRET")
