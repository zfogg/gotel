import json
from pathlib import Path

import pytest

from gotel import sqltrace
from gotel.sqltrace import Dialect, SQLInspectionError


@pytest.mark.parametrize("dialect", list(Dialect))
def test_literals(dialect):
    query = sqltrace.inspect("SELECT * FROM users WHERE email='secret@example.com' AND id=987654", dialect=dialect)
    assert "secret" not in query.text + query.summary
    assert "987654" not in query.text + query.summary
    assert query.operation == "SELECT"


@pytest.mark.parametrize(
    "sql",
    [
        "SELECT argMax(name, created_at) FROM builds FINAL PREWHERE project_id={project:UInt64} SETTINGS max_threads=8",
        "SELECT * FROM t ARRAY JOIN arr AS item WHERE item='secret-value' LIMIT 3 BY project_id",
        "SELECT * FROM t GLOBAL ANY LEFT JOIN s ON t.id=s.id SAMPLE 0.1",
        "INSERT INTO builds (id, name) SETTINGS max_partitions_per_insert_block=10000 VALUES (123, 'secret-value')",
        "SELECT $tag$secret-value$tag$, {x:Tuple(String, Array(UInt64))}, "
        "['secret-value', 123] # secret-value\n FROM t",
        'SELECT `escaped\\`identifier`, "escaped\\"identifier" FROM t WHERE name=\'secret-value\'',
    ],
)
def test_clickhouse(sql):
    query = sqltrace.inspect(sql, dialect=Dialect.CLICKHOUSE)
    assert "secret-value" not in query.text + query.summary
    formatted = sqltrace.format_sql(sql, dialect=Dialect.CLICKHOUSE)
    assert sqltrace.format_sql(formatted, dialect=Dialect.CLICKHOUSE) == formatted


def test_clickhouse_final_is_not_alias():
    query = sqltrace.inspect(
        "SELECT * FROM builds FINAL PREWHERE id=42 SETTINGS max_threads=8", dialect=Dialect.CLICKHOUSE
    )
    assert "FROM builds FINAL PREWHERE" in query.summary
    assert "AS FINAL" not in query.summary


@pytest.mark.parametrize(
    "sql",
    [
        "SELECT 'unterminated",
        "SELECT {name:String",
        "SELECT {x:Array(UInt64}",
        "INSERT INTO t FORMAT CSV\nsecret-value,123",
        'INSERT INTO t FORMAT JSONEachRow {"password":"secret-value"}',
        "SELECT 1; INSERT INTO t FORMAT CSV\nsecret-value",
        "SELECT 1\x00secret-value",
        "SELECT 'broken" + "x" * 40000,
    ],
    ids=lambda value: str(len(value)),
)
def test_fail_closed(sql):
    with pytest.raises(SQLInspectionError):
        sqltrace.inspect(sql, dialect=Dialect.CLICKHOUSE, max_query_text_bytes=8)


def test_utf8_limits_omission_and_multiple_statements():
    query = sqltrace.inspect('SELECT "日本語", 123; DELETE FROM users WHERE id=42', max_query_text_bytes=12)
    assert len(query.text.encode()) <= 12
    assert query.truncated
    assert query.statement_count == 2
    assert query.operation == ""
    assert sqltrace.inspect("SELECT 'secret'", omit_query_text=True).text == ""


def test_shared_corpus():
    root = Path(__file__).resolve().parents[2]
    for case in json.loads((root / "c/tests/sql_reference.json").read_text(encoding="utf-8")):
        if not case["safe"]:
            with pytest.raises(SQLInspectionError):
                sqltrace.inspect(case["query"], dialect=Dialect(case["dialect"]))
            continue
        query = sqltrace.inspect(case["query"], dialect=Dialect(case["dialect"]))
        assert query.text == case["text"]
        assert query.summary == case["summary"]
        assert query.operation == case["operation"]
