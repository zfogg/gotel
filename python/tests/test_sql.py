import json
import re
from pathlib import Path

import pytest

from gotel import sqltrace
from gotel.sqltrace import Dialect, SQLInspectionError

CLICKHOUSE_CASES = json.loads(
    (Path(__file__).resolve().parents[2] / "c/tests/sql_clickhouse.json").read_text(encoding="utf-8")
)


@pytest.mark.parametrize("dialect", list(Dialect))
@pytest.mark.parametrize("sql", ["SELECT 1", "SELECT 'private-value'", "SELECT 987654"])
def test_short_scalar_telemetry_presentation(dialect, sql):
    attrs = sqltrace._attributes(sql, dialect, "clickhouse", "test", {})
    assert attrs["db.query.text"] == "SELECT ?"
    assert attrs["db.query.summary"] == "SELECT ?"
    assert attrs["db.operation.name"] == "SELECT"
    assert "private-value" not in str(attrs) and "987654" not in str(attrs)


@pytest.mark.parametrize("case", CLICKHOUSE_CASES, ids=lambda case: case["name"])
def test_clickhouse_shared_corpus(case):
    sql = case["query"]
    if not case["safe"]:
        for limit in (1, 7, 32, 32768):
            with pytest.raises(SQLInspectionError):
                sqltrace.inspect(sql, dialect=Dialect.CLICKHOUSE, max_query_text_bytes=limit)
        with pytest.raises(ValueError):
            sqltrace.format_sql(sql, dialect=Dialect.CLICKHOUSE)
        return
    before = sqltrace.inspect(sql, dialect=Dialect.CLICKHOUSE)
    assert before.operation == case["operation"]
    assert before.statement_count == (2 if case["name"] == "multiple statements" else 1)
    assert all(fragment in before.summary for fragment in case["contains"])
    assert "SECRET" not in before.text + before.summary
    formatted = sqltrace.format_sql(sql, dialect=Dialect.CLICKHOUSE)
    assert re.sub(r"\s", "", formatted) == re.sub(r"\s", "", sql)
    assert sqltrace.format_sql(formatted, dialect=Dialect.CLICKHOUSE) == formatted
    after = sqltrace.inspect(formatted, dialect=Dialect.CLICKHOUSE)
    assert (re.sub(r"\s", "", after.summary), after.operation, after.statement_count) == (
        re.sub(r"\s", "", before.summary),
        before.operation,
        before.statement_count,
    )
    assert re.sub(r"\s", "", after.text) == re.sub(r"\s", "", before.text)
    for limit in (1, 7, 32):
        bounded = sqltrace.inspect(sql, dialect=Dialect.CLICKHOUSE, max_query_text_bytes=limit)
        assert len(bounded.text.encode()) <= limit
        assert "SECRET" not in bounded.text + bounded.summary


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
        "SELECT 1 // privatevalue\nFROM t",
        "SELECT 1+// privatevalue\rFROM hidden\n2 FROM t",
        "SELECT $1tag$privatevalue$1tag$",
        "SELECT $123$privatevalue$123$",
        "SELECT \u2018privatevalue\u2019",
        "SELECT\u2018privatevalue\u2019",
        "SELECT \u2018privatevalue /* ; \u2019 FROM t",
    ],
)
def test_clickhouse_lexical_privacy(sql):
    query = sqltrace.inspect(sql, dialect=Dialect.CLICKHOUSE)
    assert "privatevalue" not in query.text + query.summary
    formatted = sqltrace.format_sql(sql, dialect=Dialect.CLICKHOUSE)
    assert "privatevalue" in formatted
    after = sqltrace.inspect(formatted, dialect=Dialect.CLICKHOUSE)
    assert "privatevalue" not in after.text + after.summary
    assert (after.summary, after.operation, after.statement_count) == (
        query.summary,
        query.operation,
        query.statement_count,
    )
    assert "".join(after.text.split()) == "".join(query.text.split())
    assert sqltrace.format_sql(formatted, dialect=Dialect.CLICKHOUSE) == formatted


@pytest.mark.parametrize("number", ["0x1.fp+3", "0XFF.8P-3", "0x1p+3", "0xF_F.8p2"])
def test_clickhouse_hex_float(number):
    sql = f"SELECT {number}"
    assert sqltrace.inspect(sql, dialect=Dialect.CLICKHOUSE).text == "SELECT ?"
    formatted = sqltrace.format_sql(sql, dialect=Dialect.CLICKHOUSE)
    assert number in formatted
    assert sqltrace.inspect(formatted, dialect=Dialect.CLICKHOUSE).text == "SELECT ?"


def test_clickhouse_unicode_identifiers_and_unclosed_literals():
    sql = "SELECT * FROM \u201cmy table\u201d WHERE x=\u2018privatevalue\u2019"
    query = sqltrace.inspect(sql, dialect=Dialect.CLICKHOUSE)
    assert "\u201cmy table\u201d" in query.text and "\u201cmy table\u201d" in query.summary
    for malformed in ["SELECT \u2018privatevalue", "SELECT $1tag$privatevalue"]:
        with pytest.raises(SQLInspectionError):
            sqltrace.inspect(malformed, dialect=Dialect.CLICKHOUSE)
        with pytest.raises(ValueError):
            sqltrace.format_sql(malformed, dialect=Dialect.CLICKHOUSE)


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
