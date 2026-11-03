import asyncio
import sqlite3
from contextlib import asynccontextmanager, contextmanager

import pytest
from opentelemetry import trace

from gotel import dbtrace, psycopgtrace, sqltrace
from gotel.testing import Capture


def test_transaction_encompasses_real_query_and_commit_rollback():
    connection = sqlite3.connect(":memory:")
    connection.execute("CREATE TABLE events(value)")
    try:
        with Capture("") as capture:
            names = sqltrace.Namer("storage.postgres", tracer_provider=capture.provider)
            with dbtrace.transaction(connection, application=capture.application) as returned:
                assert returned is connection
                psycopgtrace.call_sync("Write", connection.execute, "INSERT INTO events VALUES (?)", (42,), namer=names)
            assert connection.execute("SELECT value FROM events").fetchone() == (42,)
            life = capture.require_span("storage.postgres.Transaction")
            query = capture.require_span("storage.postgres.Write")
            assert query.parent.span_id == life.context.span_id
            assert life.start_time <= query.start_time <= query.end_time <= life.end_time
            assert capture.require_span("storage.postgres.CommitTransaction").parent.span_id == life.context.span_id
            error = ValueError("PRIVATE body")
            with pytest.raises(ValueError) as caught, dbtrace.transaction(connection, application=capture.application):
                connection.execute("INSERT INTO events VALUES (99)")
                raise error
            assert caught.value is error
            assert connection.execute("SELECT COUNT(*) FROM events").fetchone() == (1,)
            failed = [span for span in capture.spans if span.name.endswith(".Transaction")][-1]
            assert failed.status.status_code == trace.StatusCode.ERROR
            capture.require_no_values("PRIVATE body")
    finally:
        connection.close()


def test_driver_suppression_and_failed_begin_do_not_leak_lifetime():
    calls = []

    @contextmanager
    def suppressed():
        calls.append("enter")
        try:
            yield "native"
        except ValueError:
            calls.append("suppressed")

    @contextmanager
    def failure():
        raise RuntimeError("PRIVATE acquisition")
        yield

    with Capture("") as capture:
        with dbtrace.transaction(suppressed(), application=capture.application):
            raise ValueError("PRIVATE body")
        assert calls == ["enter", "suppressed"]
        with pytest.raises(RuntimeError), dbtrace.connection(failure(), application=capture.application):
            pytest.fail("failed enter yielded")
        acquire = capture.require_span("storage.postgres.PoolAcquire")
        assert acquire.attributes["db.client.connection.acquire_wait_seconds"] >= 0
        assert len([span for span in capture.spans if span.name.endswith("PoolRelease")]) == 0
        capture.require_no_values("PRIVATE acquisition", "PRIVATE body")


async def test_async_pool_transaction_savepoint_and_cancellation_cleanup():
    calls = []

    @asynccontextmanager
    async def manager(label):
        calls.append((label, "enter"))
        try:
            yield label
        finally:
            calls.append((label, "exit"))

    with Capture("") as capture:
        async with dbtrace.connection_async(manager("pool"), application=capture.application):
            with pytest.raises(asyncio.CancelledError):
                async with dbtrace.transaction_async(manager("tx"), application=capture.application):
                    async with dbtrace.transaction_async(
                        manager("savepoint"), application=capture.application, savepoint=True
                    ):
                        raise asyncio.CancelledError()
        assert calls == [
            ("pool", "enter"),
            ("tx", "enter"),
            ("savepoint", "enter"),
            ("savepoint", "exit"),
            ("tx", "exit"),
            ("pool", "exit"),
        ]
        for family in ("Transaction", "SavepointTransaction"):
            span = capture.require_span(f"storage.postgres.{family}")
            assert span.attributes["operation.outcome"] == "cancelled"
            assert span.status.status_code != trace.StatusCode.ERROR
        capture.require_span("storage.postgres.RollbackSavepoint")
        capture.require_span("storage.postgres.PoolRelease")
