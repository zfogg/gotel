import asyncio

import pytest
from sqlalchemy import text
from sqlalchemy.ext.asyncio import create_async_engine

from gotel import sqltrace
from gotel.testing import Capture


@pytest.mark.parametrize(
    "system,expected",
    [("postgresql", "postgres"), ("postgres", "postgres"), ("sqlite", "sqlite"), ("clickhouse", "clickhouse")],
)
def test_database_namespace(system, expected):
    assert sqltrace.storage_prefix(system) == f"storage.{expected}"


@pytest.mark.parametrize("system", ["", "postgres.tenant", "sql customer", "1postgres"])
def test_invalid_database_namespace(system):
    with pytest.raises(ValueError, match="Invalid database system"):
        sqltrace.storage_prefix(system)


@pytest.mark.parametrize("operation", ["", "user@example.com", "List Builds", "List.Builds", 123])
def test_invalid_static_name(operation):
    with pytest.raises(ValueError, match="Query name must match"):
        with sqltrace.name(operation):
            pass


async def test_call_names_survive_sqlalchemy_greenlet_bridge():
    engine = create_async_engine("sqlite+aiosqlite://")
    try:
        with Capture() as capture:
            sqltrace.instrument_sqlalchemy(
                engine, namer=sqltrace.Namer("storage.sqlite", tracer_provider=capture.provider), system="sqlite"
            )
            async with engine.connect() as conn:
                await sqltrace.call("ListBuilds", conn.execute, text("SELECT 1"))
            assert [s.name for s in capture.spans] == ["storage.sqlite.ListBuilds"]
    finally:
        await engine.dispose()


async def test_statement_name_overrides_call_name():
    engine = create_async_engine("sqlite+aiosqlite://")
    try:
        with Capture() as capture:
            sqltrace.instrument_sqlalchemy(
                engine, namer=sqltrace.Namer("storage.sqlite", tracer_provider=capture.provider), system="sqlite"
            )
            async with engine.connect() as conn:
                await sqltrace.call("Outer", conn.execute, text("SELECT 1").execution_options(gotel_query_name="Inner"))
            assert [s.name for s in capture.spans] == ["storage.sqlite.Inner"]
    finally:
        await engine.dispose()


async def test_concurrent_names_are_isolated():
    with Capture() as capture:
        namer = sqltrace.Namer("storage.postgres", tracer_provider=capture.provider)

        async def execute():
            await asyncio.sleep(0)
            with namer.query(sql="SELECT 1", system="postgresql"):
                pass

        await asyncio.gather(sqltrace.call("ListBuilds", execute), sqltrace.call("ListProjects", execute))
        assert {s.name for s in capture.spans} == {"storage.postgres.ListBuilds", "storage.postgres.ListProjects"}


async def test_failed_call_restores_outer_name():
    with Capture() as capture:
        namer = sqltrace.Namer("storage.clickhouse", tracer_provider=capture.provider)

        async def fail():
            with namer.query(sql="SELECT 'private-value'", system="clickhouse"):
                raise ValueError("private-value")

        with sqltrace.name("Outer"):
            with pytest.raises(ValueError):
                await sqltrace.call("Inner", fail)
            with namer.query(sql="SELECT 1", system="clickhouse"):
                pass
        assert [s.name for s in capture.spans] == ["storage.clickhouse.Inner", "storage.clickhouse.Outer"]
        capture.require_no_values("private-value")


def test_sync_call_restores_name_and_return_value():
    with Capture() as capture:
        namer = sqltrace.Namer("storage.postgres", tracer_provider=capture.provider)

        def execute(value):
            with namer.query(sql="SELECT 1", system="postgresql"):
                return value

        assert sqltrace.call_sync("ReadValue", execute, 7) == 7
        assert [s.name for s in capture.spans] == ["storage.postgres.ReadValue"]


async def test_flush_distinguishes_tables():
    with Capture() as capture:
        namer = sqltrace.Namer("storage.postgres", tracer_provider=capture.provider)

        async def flush():
            for sql in ["INSERT INTO projects VALUES (1)", "INSERT INTO builds VALUES (2)"]:
                with namer.query(sql=sql, system="postgresql"):
                    pass

        await sqltrace.call("PersistProject", flush)
        assert [s.name for s in capture.spans] == [
            "storage.postgres.PersistProjectInsertProjects",
            "storage.postgres.PersistProjectInsertBuilds",
        ]


async def test_eager_relationship_queries_have_separate_names():
    from sqlalchemy import ForeignKey, insert, select
    from sqlalchemy.ext.asyncio import async_sessionmaker
    from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship, selectinload

    class Base(DeclarativeBase):
        pass

    class Project(Base):
        __tablename__ = "projects"
        id: Mapped[int] = mapped_column(primary_key=True)
        builds: Mapped[list["Build"]] = relationship()

    class Build(Base):
        __tablename__ = "builds"
        id: Mapped[int] = mapped_column(primary_key=True)
        project_id: Mapped[int] = mapped_column(ForeignKey("projects.id"))

    engine = create_async_engine("sqlite+aiosqlite://")
    try:
        async with engine.begin() as connection:
            await connection.run_sync(Base.metadata.create_all)
            await connection.execute(insert(Project).values(id=1))
        with Capture() as capture:
            remove = sqltrace.instrument_sqlalchemy(
                engine, namer=sqltrace.Namer("storage.sqlite", tracer_provider=capture.provider), system="sqlite"
            )
            async with async_sessionmaker(engine)() as session:
                await sqltrace.call(
                    "ListProjects", session.execute, select(Project).options(selectinload(Project.builds))
                )
            assert [s.name for s in capture.spans] == [
                "storage.sqlite.ListProjects",
                "storage.sqlite.ListProjectsLoadBuilds",
            ]
            remove()
    finally:
        await engine.dispose()
