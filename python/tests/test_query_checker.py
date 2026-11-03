import shutil
from concurrent.futures import ThreadPoolExecutor
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
        ('sql.call("Read", db.execute, "SELECT 1"); sql.call("Other", db.execute, "SELECT 1")', "identity"),
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


def test_python_comprehension_lambda_and_global_writes_cannot_authorize_names(tmp_path):
    path = write(
        tmp_path,
        "queries.py",
        """
from gotel import sqltrace as sql
OPERATION = "LooksStatic"
def mutate(value):
    global OPERATION
    OPERATION = value
def run(db, names):
    [sql.call(OPERATION, db.execute, "SELECT 1") for OPERATION in names]
    invoke = lambda OPERATION: sql.call(OPERATION, db.execute, "SELECT 1")
    sql.call(OPERATION, db.execute, "SELECT 1")
""",
    )
    report = check_python([path])
    assert report.count == 3
    assert [issue.code for issue in report.issues] == ["unnamed"] * 3


def test_registry_bounds_and_identity():
    registry = Registry(1)
    registry.observe("Read", "SELECT 1")
    registry.observe("Read", "SELECT 2")
    with pytest.raises(NamingError, match="different shape"):
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
    with pytest.raises(NamingError, match="different shape"):
        registry.observe("Read", "SELECT 1", dialect=sequel.Dialect.POSTGRESQL)


@pytest.mark.parametrize(
    ("left", "right"),
    [
        ("SELECT value FROM alerts WHERE key='fill'", "SELECT value FROM alerts WHERE key=$1"),
        (
            "UPDATE runs SET status='completed',error_message='' WHERE id=$1",
            "UPDATE runs SET status='failed',error_message=$1 WHERE id=$2",
        ),
        (
            "SELECT(pg_stat_get_live_tuples('public.bars'::regclass)*256)::bigint",
            "SELECT(pg_stat_get_live_tuples('public.signals'::regclass)*4096)::bigint",
        ),
        ("SELECT 'two spaces'", "SELECT 'two  spaces'"),
        ("SELECT $1,$1", "SELECT $1,$2"),
        ("SELECT $1", "SELECT $2"),
        ("SELECT $$\n-- name: One :one\n$$", "SELECT $$\n-- name: Two :one\n$$"),
    ],
)
def test_static_python_definitions_preserve_fixed_semantics(tmp_path, left, right):
    source = "from gotel import sqltrace as sql\ndef run(db):\n"
    source += f"    sql.call('One', db.execute, {left!r})\n    sql.call('Two', db.execute, {right!r})\n"
    report = check_python([write(tmp_path, "queries.py", source)], PythonConfig(dialect=sequel.Dialect.POSTGRESQL))
    report.raise_for_issues()
    assert report.count == 2


def test_static_python_sqlc_metadata_and_explicit_shape_policy(tmp_path):
    source = "from gotel import sqltrace as sql\ndef run(db):\n"
    source += "    sql.call('One', db.execute, '-- name: One :one\\nSELECT 1')\n"
    source += "    sql.call('Two', db.execute, '-- name: Two :one\\nSELECT 1')\n"
    path = write(tmp_path, "queries.py", source)
    assert "identity" in {issue.code for issue in check_python([path]).issues}
    source = source.replace("Two :one\\nSELECT 1", "Two :one\\nSELECT 2")
    path.write_text(source)
    check_python([path]).raise_for_issues()
    report = check_python([path], PythonConfig(definition_identity="shape"))
    assert "identity" in {issue.code for issue in report.issues}


def test_runtime_shape_and_definition_observations_are_separate():
    registry = Registry()
    registry.observe("Fill", "SELECT 'fill'")
    registry.observe("Alert", "SELECT 'alert'")
    fingerprint = sequel.fingerprint("SELECT 1")
    registry.observe_fingerprint("One", fingerprint)
    registry.observe_fingerprint("Two", fingerprint)
    with pytest.raises(NamingError, match="different shape"):
        registry.observe("Fill", "DELETE FROM alerts")
    registry = Registry()
    registry.observe_definition("One", "-- name: One :one\nSELECT 'same'")
    with pytest.raises(NamingError, match="duplicates the definition"):
        registry.observe_definition("Two", "-- name: Two :one\nSELECT 'same'")
    registry.observe_definition("Two", "SELECT 'different'")
    with pytest.raises(NamingError, match="different source definition"):
        registry.observe_definition("One", "SELECT 'changed'")
    permissive = Registry(check_duplicate_definitions=False)
    permissive.observe_definition("One", "SELECT 1")
    permissive.observe_definition("Two", "SELECT 1")
    with pytest.raises(NamingError, match="different source definition"):
        permissive.observe_definition("One", "SELECT 2")


def test_runtime_definition_registry_concurrent_and_retains_no_source():
    registry = Registry(2)

    def observe(index):
        name = "One" if index % 2 == 0 else "Two"
        value = "PRIVATE_ONE" if index % 2 == 0 else "PRIVATE_TWO"
        registry.observe_definition(name, f"SELECT '{value}'")

    with ThreadPoolExecutor(max_workers=4) as pool:
        list(pool.map(observe, range(40)))
    retained = repr(registry.__dict__)
    assert "PRIVATE_ONE" not in retained and "PRIVATE_TWO" not in retained and "SELECT" not in retained


@pytest.mark.skipif(shutil.which("clang") is None, reason="Clang is required for the real C AST adapter")
def test_c_definition_identity_retains_literals_and_catalog_membership(tmp_path):
    path = write(
        tmp_path,
        "queries.c",
        """
typedef struct {const char *name; const char *sql;} gotel_query_t;
static const gotel_query_t queries[]={
 {"Fill", "SELECT 'fill'"}, {"Alert", "SELECT 'alert'"},
 {"Spaces", "SELECT 'two spaces'"}, {"MoreSpaces", "SELECT 'two  spaces'"}};
int execute(void *db,const char *sql);
int run(void *db){execute(db,queries[0].sql); return execute(db,"SELECT 'unregistered'");}
""",
    )
    report = check_c([path], CConfig(executors=(CExecutor("execute"),)))
    assert [issue.code for issue in report.issues] == ["unnamed"]
    shape = check_c([path], CConfig(executors=(CExecutor("execute"),), definition_identity="shape"))
    assert "identity" in {issue.code for issue in shape.issues}


@pytest.mark.skipif(shutil.which("clang") is None, reason="Clang is required for the real C AST adapter")
def test_c_duplicate_exact_bodies_ignore_only_sqlc_metadata(tmp_path):
    path = write(
        tmp_path,
        "queries.c",
        """
typedef struct {const char *name; const char *sql;} gotel_query_t;
static const gotel_query_t queries[]={
 {"One", "-- name: One :one\\nSELECT 'same'"}, {"Two", "-- name: Two :one\\nSELECT 'same'"}};
""",
    )
    report = check_c([path])
    assert [issue.code for issue in report.issues] == ["identity"]
