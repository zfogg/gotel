import shutil
from pathlib import Path

import pytest
import sequel

from gotel.querynames import CConfig, CExecutor, NamingError, PythonConfig, Registry, check_c, check_python


def write(tmp_path: Path, name: str, source: str) -> Path:
    path = tmp_path / name
    path.write_text(source, encoding="utf-8")
    return path


def test_python_checker_imports_scopes_sql_literals_and_catalog(tmp_path):
    path = write(
        tmp_path,
        "queries.py",
        """
from gotel import sqltrace as sql
from sqlalchemy import text as statement
QUERY = "SELECT id FROM widgets WHERE id=:id"
async def read(db):
    return await sql.call("ReadWidget", db.execute, statement(QUERY), {"id": 123})
async def delete(db):
    with sql.name("DeleteWidget"):
        return await db.execute("DELETE FROM widgets WHERE id=:id")
""",
    )
    report = check_python([path])
    report.raise_for_issues()
    assert report.count == 2
    assert [(site.name, site.method) for site in report.sites] == [
        ("ReadWidget", "db.execute"),
        ("DeleteWidget", "db.execute"),
    ]


@pytest.mark.parametrize(
    ("body", "code"),
    [
        ('db.execute("SELECT 1")', "unnamed"),
        ('sql.call(operation, db.execute, "SELECT 1")', "unnamed"),
        ('sql.call("Read", db.execute, runtime_sql)', "dynamic_sql"),
        ('db.execute("SELECT ?", sql.name("Decoy"))', "unnamed"),
        ('sql.call("Read", db.execute, "SELECT 1"); sql.call("Other", db.execute, "SELECT 2")', "identity"),
        ('sql.call("Read", db.execute, "SELECT 1"); sql.call("Read", db.execute, "DELETE FROM widgets")', "identity"),
    ],
)
def test_python_checker_rejects_unsafe_boundaries(tmp_path, body, code):
    path = write(
        tmp_path, "queries.py", "from gotel import sqltrace as sql\ndef run(db, operation, runtime_sql):\n    " + body
    )
    report = check_python([path])
    assert code in {issue.code for issue in report.issues}
    assert "SELECT" not in "\n".join(map(str, report.issues))


def test_python_dynamic_sites_still_have_unique_static_names(tmp_path):
    path = write(
        tmp_path,
        "queries.py",
        """
from gotel import sqltrace as sql
async def run(db, expression):
    await sql.call("Read", db.execute, expression)
    await sql.call("Read", db.execute, expression)
""",
    )
    report = check_python([path], PythonConfig(allow_dynamic_sql=True))
    assert [issue.code for issue in report.issues] == ["duplicate_site"]


def test_python_checker_lexical_shadowing_and_awaited_methods(tmp_path):
    path = write(
        tmp_path,
        "queries.py",
        """
from gotel import sqltrace as sql
OPERATION = "GlobalName"
def run(db, OPERATION):
    sql.call(OPERATION, db.execute, "SELECT 1")
def other(db):
    OPERATION = "LocalName"
    sql.call(OPERATION, db.execute, "SELECT id FROM widgets")
async def lookup(db, request, expression):
    request.session.get("non_database_lookup")
    await validate(request.session.get("also_not_database"))
    await sql.call("Read", db.get, expression)
""",
    )
    config = PythonConfig(allow_dynamic_sql=True, awaited_executors=("get",))
    report = check_python([path], config)
    assert report.count == 3
    assert [issue.code for issue in report.issues] == ["unnamed"]
    assert [site.name for site in report.sites] == [None, "LocalName", "Read"]


def test_python_same_line_sites_are_still_distinct(tmp_path):
    path = write(
        tmp_path,
        "queries.py",
        """
from gotel import sqltrace as sql
def run(db):
    sql.call("Read", db.execute, "SELECT 1"); sql.call("Read", db.execute, "SELECT 2")
""",
    )
    assert "duplicate_site" in {issue.code for issue in check_python([path]).issues}


def test_registry_bounds_and_identity():
    registry = Registry(1)
    registry.observe("Read", "SELECT 1")
    registry.observe("Read", "SELECT 2")
    with pytest.raises(NamingError, match="different definition"):
        registry.observe("Read", "DELETE FROM widgets")
    with pytest.raises(NamingError, match="capacity"):
        registry.observe("Delete", "DELETE FROM widgets")
    with pytest.raises(NamingError, match="manual review"):
        registry.observe("Bad", "SELECT 'private-secret")


@pytest.mark.skipif(shutil.which("clang") is None, reason="Clang is required for the real C AST adapter")
def test_c_checker_resolves_macros_catalog_aliases_and_forwarded_sql(tmp_path):
    path = write(
        tmp_path,
        "queries.c",
        """
typedef struct {const char *name; const char *sql;} gotel_query_t;
int execute(void *db, const char *sql);
#define QUERY(name, sql) {#name, sql},
static const gotel_query_t queries[] = {
  QUERY(Read, "SELECT id FROM widgets WHERE id=?") QUERY(Delete, "DELETE FROM widgets WHERE id=?")};
enum { ReadIndex=0, DeleteIndex=1 };
static int forward(void *db, const char *sql) { return execute(db, sql); }
int run(void *db) {const char *sql=queries[ReadIndex].sql; forward(db,sql);
  return execute(db,queries[DeleteIndex].sql);}
""",
    )
    report = check_c([path], CConfig(executors=(CExecutor("execute"),)))
    report.raise_for_issues()
    assert report.count == 2


@pytest.mark.skipif(shutil.which("clang") is None, reason="Clang is required for the real C AST adapter")
def test_c_checker_rejects_unnamed_and_dynamic_calls(tmp_path):
    path = write(
        tmp_path,
        "queries.c",
        """
int execute(void *db, const char *sql);
int run(void *db, const char *sql) {execute(db, "SELECT 'private-value'"); return execute(db,sql);}
""",
    )
    report = check_c([path], CConfig(executors=(CExecutor("execute"),)))
    assert {issue.code for issue in report.issues} == {"unnamed", "dynamic_sql"}
    assert "private-value" not in "\n".join(map(str, report.issues))


def test_registry_dialect_is_part_of_identity():
    registry = Registry()
    registry.observe("Read", "SELECT 1", dialect=sequel.Dialect.SQLITE)
    with pytest.raises(NamingError, match="different definition"):
        registry.observe("Read", "SELECT 1", dialect=sequel.Dialect.POSTGRESQL)
