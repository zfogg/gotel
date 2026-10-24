# gotel for Python

A uv-managed package providing application-owned OpenTelemetry operation names,
OTLP traces/logs/metrics, and SQL inspection using gotel's shared C engine.
Requires Python 3.11 or newer. The distribution is `gotel`; imports use `gotel`.

```sh
cd python
uv sync --all-extras
uv run pytest
uv build
```

Install from a checkout with `uv add '../gotel/python[fastapi,httpx,sqlalchemy,redis,temporal]'`.
For an immutable Git dependency, use
`uv add 'gotel[fastapi,httpx,sqlalchemy,redis,temporal] @ git+https://github.com/zfogg/gotel.git@COMMIT#subdirectory=python'`.
Do not depend on a developer's checkout in production. Releases include wheels and
a source distribution; PyPI publication is separate. Source builds require a C
compiler, but no CMake, Go, system utf8proc, or native telemetry library.

## Development tooling

Ruff and ty are development dependencies in `pyproject.toml` and pinned by
`uv.lock`. Ruff enforces lint rules, import ordering, and consistent formatting;
ty checks the package, tests, and build script without running them. Native SQL
extension signatures are supplied in `_sql.pyi` so callers can be checked too.

From the repository root:

```sh
make sync        # Install all adapters and development tools from the lockfile
make format      # Sort Python imports and apply Ruff formatting
make lint        # Check Ruff rules and formatting without editing files
make typecheck   # Run ty
make test-python # Run pytest
make check       # Lint/types plus Python, Go, and TypeScript tests
```

`make check` also needs Go, pnpm, and installed TypeScript dependencies. C tests
are separate (`make test-c` after configuring `c/build`). Within `python/`, direct
commands are `uv run --all-extras ruff check src tests setup.py`,
`uv run --all-extras ruff format src tests setup.py`, and
`uv run --all-extras ty check`. CI runs both tools and checks formatting.

## Lifecycle

```python
from gotel import Application
from gotel.otelhost import Config, start

async def serve():
    async with start(Config(service_name="beanaries-api")) as runtime:
        application = Application("beanaries")
        with application.operation("scraper.FetchProject") as span:
            span.set_attribute("beanaries.project.source", "github")
            await fetch_project()
```

Call `start` once per process, after worker forks. The runtime owns all enabled
providers and shuts them down; the async context manager runs shutdown in a worker
thread. `force_flush(timeout_millis=30000)` uses a shared budget across signals.
Shutdown follows SDK/exporter timeouts; it is not a hard cancellation deadline.
Stopping telemetry does not permit replacing OTel's process-global providers.

`Config` selects signals (`traces`, `metrics`, `logs`), resources, endpoint,
protocol (`http/protobuf` or the optional `grpc` extra), headers, certificates,
exporter timeout, custom exporters, and additional span processors. Explicit
configuration overrides exporter environment defaults. An explicit HTTP endpoint
is a base URL; gotel appends `/v1/<signal>`. Otherwise standard signal-specific
`OTEL_EXPORTER_OTLP_*` settings apply. `OTEL_SDK_DISABLED=true` creates no providers.
`OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, sampler settings, batch settings,
and metric export intervals are handled by the standard SDK.

Host, OS, process, executable, Python runtime, and SDK resources are included.
Command arguments are not collected. Pass `install_global=False` for isolated
providers, and pass those providers explicitly to application/adapters. Existing
SDK installations can use gotel's adapters without starting a runtime.

The runtime supplies `logging_handler` and by default attaches it to the root
logger without changing levels or rendering. Set `attach_logging_handler=False`
to manage attachment yourself. `gotel.logging.add_trace_context` is a structlog
processor that adds `trace_id` and `span_id` to events inside an active span.

`@application.traced("scraper.FetchProject")` supports sync and async functions.
It does not wrap generators. Use an explicit `operation` context for iteration.
Names must be stable application operations; put varying identifiers in attributes.
These APIs return standard OTel spans and preserve async context propagation.

## HTTP

```python
from gotel.httptrace import Namer

names = Namer("beanaries")
remove_server = names.instrument_fastapi(app)
remove_client = names.instrument_httpx(client)
```

Install server instrumentation before serving; routes are resolved per request. It
names resolved routes as `beanaries.http.server.GET /items/{item_id}` and unmatched
requests as `beanaries.http.server.GET /{unmatched}`. Send/receive helper spans
are suppressed. Client names retain destination host, port, and path, replace
numeric/UUID/long-hex path segments with `{id}`, and exclude credentials, query,
and fragments. Arbitrary string slugs require application-owned route policies.
Names do not imply that every upstream HTTP attribute or exception is redacted;
configure SDK instrumentation capture policies for application-specific secrets.

Both adapters return idempotent removal callbacks and accept explicit providers
and request hooks. Remove existing instrumentation before installing gotel.

## Shared SQL inspection and formatting

```python
from gotel import sqltrace

query = sqltrace.inspect(
    "SELECT * FROM builds FINAL PREWHERE project_id={project:UInt64}",
    dialect=sqltrace.Dialect.CLICKHOUSE,
)
print(query.text, query.summary, query.operation, query.statement_count)
```

`Dialect` supports generic, SQLite, PostgreSQL, DuckDB, MySQL, and ClickHouse.
`inspect` never accepts bind values. It removes literals, comments, and parameter
markers and returns an immutable `Query`. It raises `SQLInspectionError` on unsafe
input. The default text cap is 32 KiB, adjustable with `max_query_text_bytes`;
`omit_query_text=True` retains summaries/counts. `keep_in_lists=True` retains
individual sanitized placeholders. Limits are UTF-8 aware; input is limited to
1 MiB and nesting to 64 levels.

`format_sql` preserves literals and comments and raises `ValueError` on rejection.
`format_sql_or_original` returns the original on rejection. Formatting is for
presentation, not privacy; telemetry formats only the sanitized template.

ClickHouse supports typed parameters, escaped identifiers, heredocs, arrays,
`FINAL`, `PREWHERE`, join modifiers, and `SETTINGS`. Like other dialects, summaries
describe selected structure rather than validate the entire grammar. `INSERT ...
FORMAT` is rejected for inspection because its payload is a separate data format,
not SQL. Adapters still emit timing and operation spans while omitting SQL metadata.
Pass only the SQL header for streamed/binary inserts. Identifiers can appear in
metadata and must not contain secrets.

## Database adapters

```python
storage = sqltrace.Namer("beanaries.storage")
remove = sqltrace.instrument_sqlalchemy(
    engine, namer=storage, system="clickhouse", namespace="beanaries",
    dialect=sqltrace.Dialect.CLICKHOUSE,
)

with storage.name("ListBuilds"):
    result = await session.execute(statement)

with storage.query("InsertBuilds", sql=sql, system="clickhouse",
                   dialect=sqltrace.Dialect.CLICKHOUSE):
    await cursor.execute(sql, parameters)
```

The adapter attaches to one sync or async SQLAlchemy engine. Disable existing
SQLAlchemy instrumentation before attaching. Install once per engine; the returned
callback removes listeners. Queries without a named operation use `Query`.
Spans cover cursor execution, including errors, rather than result consumption.
Manual query scopes suppress nested automatic instrumentation. Database spans
record error types without driver messages or exception events, which may contain
bind values. Duration is recorded in `db.client.operation.duration` (seconds).
Bind values are never recorded, and bind counts are not inferred from SQL.

## Redis and Temporal

```python
from gotel import redistrace, temporaltrace

remove_redis = redistrace.instrument(redis_client, application=application)
tracing = temporaltrace.interceptor(application=application)
# Client.connect(..., interceptors=[tracing])
```

Redis wraps sync/async client commands and emits bounded names such as
`beanaries.storage.redis.get`. Unknown commands use `other`; keys, values, and
exception messages are not exported. Pipelines receive one `storage.redis.pipeline` span around execution, without
recording queued commands or values.

Temporal uses the SDK's replay-aware interceptor, propagation, and workflow
sandbox support. Register it once on the client; workers inherit it. Scheduling
and execution have distinct names, such as `beanaries.temporal.activity.schedule.Fetch`
and `beanaries.temporal.activity.execute.Fetch`. The optional
`always_create_workflow_spans` flag follows the Temporal SDK's semantics.

## Tests and source ownership

`gotel.testing.Capture` provides isolated in-memory providers, an `application`,
captured `spans`, and assertions for names, duplicate identities, and private values.
Use separate captures per test; no global provider reset is needed.

The SQL implementation is authored only in `../c/`. Run
`python scripts/sync_sql_engine.py` from the repository root after changing it.
The checked bundles in `vendor/` and `../go/sqltrace/engine/` make source packages
self-contained; CI checks their byte-for-byte parity.
