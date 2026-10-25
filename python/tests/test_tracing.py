import asyncio

import pytest
import sequel
from opentelemetry import trace

from gotel import correlation, sqltrace
from gotel.logging import add_trace_context
from gotel.testing import Capture


async def test_async_context_and_exceptions():
    with Capture("beanaries") as capture:
        application = capture.application

        @application.traced("scraper.FetchProject")
        async def child():
            await asyncio.sleep(0)
            assert correlation()["trace_id"]

        with application.operation("scraper.Run") as parent:
            await asyncio.gather(child(), child())
            assert add_trace_context(None, None, {}) == correlation()
        for span in capture.spans[:2]:
            assert span.parent.span_id == parent.get_span_context().span_id
        with pytest.raises(ValueError), application.operation("scraper.Fail"):
            raise ValueError("failed")
        assert capture.spans[-1].status.status_code == trace.StatusCode.ERROR
        capture.require_no_duplicate_spans()
    assert correlation() == {}


def test_sync_decorator():
    with Capture() as capture:

        @capture.application.traced("Calculate")
        def calculate(value):
            return value + 1

        assert calculate(3) == 4
        capture.require_span("test.Calculate")
        span = capture.application.tracer("custom").start_span("test.AlreadyQualified")
        span.end()
        capture.require_span("test.AlreadyQualified")


def test_database_error_privacy():
    with Capture("beanaries") as capture:
        storage = sqltrace.Namer("beanaries.storage", tracer_provider=capture.provider)
        with (
            pytest.raises(ValueError),
            storage.query(
                "InsertBuilds",
                sql="INSERT INTO builds VALUES ('secret-value')",
                system="clickhouse",
                dialect=sequel.Dialect.CLICKHOUSE,
            ),
        ):
            raise ValueError("driver error: secret-value")
        capture.require_span("beanaries.storage.InsertBuilds")
        capture.require_no_values("secret-value")
        assert capture.spans[0].status.status_code == trace.StatusCode.ERROR


def test_database_formatting_keeps_complete_bounded_template():
    with Capture() as capture:
        namer = sqltrace.Namer("test.storage", tracer_provider=capture.provider)
        sql = "SELECT a,b,c,d,e FROM t"
        with namer.query("List", sql=sql, system="postgresql", max_query_text_bytes=23):
            pass
        assert capture.spans[0].attributes["db.query.text"] == sql
        assert not capture.spans[0].attributes.get("gotel.db.query.truncated", False)


async def test_sqlalchemy_async_success_failure_and_names():
    from sqlalchemy import text
    from sqlalchemy.ext.asyncio import create_async_engine

    engine = create_async_engine("sqlite+aiosqlite://")
    try:
        with Capture("beanaries") as capture:
            namer = sqltrace.Namer("beanaries.storage", tracer_provider=capture.provider)
            remove = sqltrace.instrument_sqlalchemy(engine, namer=namer, system="sqlite", dialect=sequel.Dialect.SQLITE)
            assert sqltrace.instrument_sqlalchemy(engine, namer=namer, system="sqlite") is remove
            async with engine.connect() as conn:
                with namer.name("ListBuilds"):
                    assert (await conn.execute(text("SELECT 'secret-value'"))).scalar() == "secret-value"
                with pytest.raises(Exception):
                    await conn.execute(text("SELECT * FROM missing WHERE id='secret-value'"))
                assert (await conn.execute(text("SELECT 1"))).scalar() == 1
            assert len(capture.spans) == 3
            capture.require_span("beanaries.storage.ListBuilds")
            capture.require_no_values("secret-value")
            assert capture.spans[1].status.status_code == trace.StatusCode.ERROR
            remove()
            remove()
            assert trace.get_current_span().get_span_context().is_valid is False
    finally:
        await engine.dispose()
