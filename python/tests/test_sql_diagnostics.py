"""Diagnostics refer to original SQL, survive suppression and exclude values."""

import json

import pytest
import sequel

from gotel import sqltrace
from gotel.testing import Capture


@pytest.mark.parametrize(
    ("dialect", "sql", "code"),
    [
        (sequel.Dialect.SQLITE, "SELECT 'SECRET", "unterminated_string"),
        (sequel.Dialect.POSTGRESQL, "SELECT $tag$SECRET", "unterminated_string"),
        (sequel.Dialect.MYSQL, "SELECT `SECRET", "unterminated_identifier"),
        (sequel.Dialect.CLICKHOUSE, "SELECT {arg:String", "unterminated_parameter"),
        (sequel.Dialect.DUCKDB, "SELECT (1", "unclosed_group"),
        (sequel.Dialect.GENERIC, "SELECT /* SECRET", "unterminated_comment"),
    ],
)
def test_original_diagnostics(dialect, sql, code):
    with Capture() as capture:
        namer = sqltrace.Namer("storage.test", tracer_provider=capture.provider)
        with namer.query("Broken", sql=sql, system=dialect.name.lower(), dialect=dialect, omit_query_text=True):
            pass
        attrs = capture.spans[0].attributes
        assert "db.query.text" not in attrs
        diagnostics = attrs["db.query.diagnostics"]
        assert len(diagnostics) == 1
        assert "SECRET" not in diagnostics[0]
        diagnostic = json.loads(diagnostics[0])
        assert diagnostic["code"] == code
        assert diagnostic["line"] == 1 and diagnostic["column"] >= 1
        assert diagnostic["incomplete"]
        assert diagnostic["explanation"] and diagnostic["hint"]


def test_clean_diagnostics_are_empty_array():
    with Capture() as capture:
        namer = sqltrace.Namer("storage.test", tracer_provider=capture.provider)
        for sql in ("", "SELECT 'SECRET'", "SELECT FROM"):
            with namer.query("Clean", sql=sql, system="sqlite", dialect=sequel.Dialect.SQLITE):
                pass
        assert all(tuple(span.attributes["db.query.diagnostics"]) == () for span in capture.spans)


async def test_real_sqlalchemy_query_failure():
    from sqlalchemy import text
    from sqlalchemy.ext.asyncio import create_async_engine

    engine = create_async_engine("sqlite+aiosqlite://")
    try:
        with Capture() as capture:
            remove = sqltrace.instrument_sqlalchemy(
                engine,
                namer=sqltrace.Namer("storage.sqlite", tracer_provider=capture.provider),
                system="sqlite",
                dialect=sequel.Dialect.SQLITE,
            )
            async with engine.connect() as connection:
                with pytest.raises(Exception):
                    await connection.execute(text("SELECT 'SECRET"))
                await connection.execute(text("SELECT 1"))
            remove()
            assert len(capture.spans) == 2
            bad, good = capture.spans
            assert json.loads(bad.attributes["db.query.diagnostics"][0])["code"] == "unterminated_string"
            assert "db.query.text" not in bad.attributes
            assert tuple(good.attributes["db.query.diagnostics"]) == ()
            capture.require_no_values("SECRET")
    finally:
        await engine.dispose()
