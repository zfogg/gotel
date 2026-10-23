# gotel

OpenTelemetry building blocks in three languages:

- **Go** (`sqltrace`, `httptrace`, `otelhost`, and adapters) instruments
  applications and emits traces, logs, metrics, and resource attributes.
  `sqltrace` uses the shared C implementation for SQL summaries, operation
  detection, and sanitized `db.query.text`.
- **C** (`c-gotel/`) instruments native applications. Its SQL scanner is the
  common implementation used by both C and Go; the C library can be built
  static or shared with CMake.
- **TypeScript** (`@zfogg/gotel`) instruments browser requests and gives them
  stable, route-like HTTP span names.

The default `db.query.text` limit is **32 KiB** in C and Go. Applications can
choose a smaller cap, omit query text, or request uncapped text through the Go
API (subject to the 1 MiB SQL input limit). Sanitization removes literals and
comments before applying the cap; summaries have a separate 4 KiB limit.

## C library

`c-gotel/` provides native trace scopes, structured log correlation, bounded
operation metrics, resource detection, HTTP naming, and SQLite query spans.
Applications create a host with an operation catalog, call `gotel_begin` and
`gotel_end` around work, and call `gotel_tick` from their own loop or worker to
export OTLP/HTTP protobuf. The library does not start a background thread.
The SQLite adapter attaches to an existing connection; it does not replace
SQLite calls. SQL inspection itself works without SQLite.

The public [C SQL API](c-gotel/include/gotel/sql.h) has two entry points:
`gotel_sql_inspect` returns the sanitized text, structural summary, outer
operation, truncation flag, and top-level statement count;
`gotel_sql_sanitize` sanitizes text independently and supports a caller-sized
output buffer. Both scan every statement, including ones after a semicolon,
and fail closed if a later string or comment is malformed. C and Go emit
`gotel.db.query.statement_count`; a normal single-statement call reports `1`.
Semicolons inside strings, quoted identifiers, and comments do not count.

```c
#include <gotel/sql.h>

gotel_sql_options_t policy = {.dialect = GOTEL_SQL_POSTGRESQL};
gotel_sql_metadata_t query;
if (gotel_sql_inspect("SELECT * FROM items WHERE id=$1; UPDATE audit SET seen=true",
                      &policy, &query) == OTLP_OK) {
  /* query.statement_count is 2; query.text contains no bind values. */
}
```

The SQL implementation lives in `c-gotel/src/sql/`: `scanner.c` handles
quoting, comments, placeholders and literals; `summary.c` builds structural
summaries; `api.c` applies limits and returns metadata; and `common.c` contains
shared helpers. `main.c` includes those files as one translation unit. CMake
builds `main.c` for the native library; cgo includes that same file for Go.
This keeps SQL behavior and security fixes in one place.

Build the C library with CMake and Clang:

```sh
cmake -S c-gotel -B c-gotel/build -G Ninja -DCMAKE_C_COMPILER=clang
cmake --build c-gotel/build
```

`BUILD_SHARED_LIBS=ON` selects a shared library; static is the default.
`CGOTEL_WITH_SQLITE=OFF` removes the optional SQLite adapter. A parent CMake
project can use `add_subdirectory(gotel/c-gotel)` and link `gotel::c-gotel`.
The library accepts existing utf8proc and PCRE2 targets, as ascii-chat does;
otherwise CMake resolves those dependencies. See the
[C library guide](c-gotel/README.md) for the lifecycle, adapters, and tests.

The `@zfogg/gotel` TypeScript package shares browser HTTP span naming and fetch
instrumentation between applications. Same-origin requests use route-like paths;
external requests retain their host and non-default port. Query strings and
fragments never enter span names. The application prefix is required explicitly.

```ts
import { createBrowserFetchInstrumentation } from '@zfogg/gotel'

const fetchInstrumentation = createBrowserFetchInstrumentation({
  prefix: 'myapp',
  ignoreUrls: [/\/telemetry\//],
  propagateTraceHeaderCorsUrls: [globalThis.location.origin],
})
```

`httptrace` shares HTTP naming between applications and instrumentation libraries:
`myapp.http.client.GET example.com/path` and
`myapp.http.server.GET /items/{id}`. It preserves every HTTP method, including
extension methods, and leaves existing destination attributes unchanged.
Client names retain the host, port and escaped path, excluding URL credentials,
query parameters and fragments. Server names use resolved route templates;
unresolved requests use `unknown` instead of raw per-request URLs.

```go
names := httptrace.Namer{Prefix: "myapp"}
transport := otelhttp.NewTransport(http.DefaultTransport,
    otelhttp.WithSpanNameFormatter(names.ClientSpanName))
handler := otelhttp.NewHandler(names.RouteMiddleware(nil)(mux), "myapp",
    otelhttp.WithSpanNameFormatter(names.ServerSpanName))
```

The package depends only on `net/http` and the OTel API, not Chi, Gin, Echo or
`otelhttp`. Its formatters work directly with `otelhttp` options. Go ServeMux
uses `Request.Pattern`; other routers supply a callback to `RouteMiddleware`
that returns the final route template after the handler runs. Install that
middleware inside HTTP instrumentation. `SetServerRoute` also updates an
existing span directly when an application's middleware has additional work.
Neither route hook creates a second span or wraps the response writer, so
streaming, flushing and WebSocket upgrades retain their existing behavior.

`sqltrace` provides:

- stable application prefixes such as `spaniel.storage.GetStats`;
- sqlc `-- name:` extraction and context-carried explicit names;
- sanitized SQL templates in `db.query.text` (never bind values);
- the real bind argument count in `gotel.db.query.bind_count`;
- the number of nonempty SQL statements in `gotel.db.query.statement_count`;
- standard database system, namespace, operation, and summary attributes;
- dialect-tolerant sanitization for PostgreSQL, DuckDB, SQLite, and MySQL syntax;
- automatic bounded SQL operation-and-table summaries, independent of span names.

SQL summaries, operation detection, and query-text sanitization compile the same C parser used by
`c-gotel` through cgo. `go get` only fetches the module; building a Go program
that imports `sqltrace` (or its database adapters) requires cgo and a C compiler.
The Go build compiles bundled utf8proc source and does not invoke CMake or
require a system utf8proc installation. The bundled utf8proc source is pinned to
commit `e5e799221b45bbb90f5fdc5c69b6b8dfbf017e78` (MIT license in
`sqltrace/UTF8PROC_LICENSE.md`).

`gormtrace` and `pgxtrace` apply that contract at the driver boundary so every
query is covered. `pgxtrace` covers queries, COPY, batches, prepare, and connect;
`gormtrace` supports configurable CLIENT/INTERNAL spans. `sqltrace.StartSpan`
covers custom operations, while `sqltrace.QueryContext`, `QueryRowContext`, and
`ExecContext` provide lifecycle-safe `database/sql` instrumentation. `dbmetrics`
provides the standard `db.client.operation.duration` histogram and observer
composition. `otelhost` optionally configures OTLP over
gRPC or HTTP/protobuf, resources, propagation, logs, metrics, traces, headers,
test exporters, and coordinated shutdown. The root package contains shared span
lifecycle, correlation, slog fan-out, standard-log routing, and shutdown helpers.

```go
namer := sqltrace.MustNamer("myapp.storage")
db.Use(gormtrace.New(gormtrace.Config{
    Scope: "myapp/storage",
    Namer: namer,
    System: "postgresql",
    Namespace: "myapp",
	Dialect: sqltrace.DialectPostgreSQL,
}))

ctx := namer.WithName(ctx, "ListWidgets")
db.WithContext(ctx).Find(&widgets)
```

Sanitization is dialect-aware and fail-closed. It handles PostgreSQL escape,
Unicode, dollar-quoted, bit, and hex strings; nested comments; MySQL escapes and
comments; common placeholder forms; numeric/boolean/null literals; optional
`IN`-list collapsing; and a UTF-8-safe query-text cap. Bind values are never
rendered into telemetry.

Applications with stricter policies can set `SanitizeOptions.OmitQueryText` and
still retain stable names, operation classification, and bind counts. v0.3 emits
current semantic-convention attributes only: migrate `db.system` to
`db.system.name` and `db.query.parameter.count` to
`gotel.db.query.bind_count` before upgrading.

Since v0.5, `db.query.summary` is a structural SQL skeleton, independent of span
names. For example: `SELECT … FROM signals LEFT JOIN LATERAL (SELECT … FROM
bars WHERE … LIMIT 1) AS latest ON signals.id = latest.signal_id WHERE … ORDER BY … LIMIT ?`. Recursive/materialized CTE
definitions and references stay scoped instead of being flattened into tables.
JOIN kinds, scalar/EXISTS subqueries, UNION/INTERSECT/EXCEPT (including ALL),
grouping, ordering, window modifiers, writes, conflicts and locking stay visible.
Function-only calls become `SELECT prepare_signal_grading`.

Since v0.5.1, join `ON` predicates, `USING (keys)` and `MERGE … ON` conditions retain their
identifiers, operators, boolean structure and nested query skeletons. Source
aliases are preserved as `AS alias` so qualified join keys remain meaningful.
All predicate literals (including numeric constants) and bind placeholders become
`?`; argument values are never interpolated.
Table-function `WITH ORDINALITY` is preserved, including its source/column aliases.
PostgreSQL/DuckDB arrays and subscripts are expressions, not bracket-quoted
identifiers: their literal elements/indices are sanitized in both attributes.

`…` means omitted expressions (including projection lists and WHERE/HAVING
predicates); `?` also means a dynamic row bound. Standalone integer LIMIT/OFFSET/FETCH
counts, including nested `LIMIT 1`, are retained. Parameter values are never
interpolated, and computed bounds become `?`. This policy is deliberately
different from `db.query.text`, which still sanitizes **all** numeric literals.
Identifiers remain visible, so do not embed sensitive data in identifier names.

Summaries omit sqlc names, preserve quoted/schema-qualified
identifiers and repeated joins, and cap the result at 4096 UTF-8 bytes. They are computed
from the complete sanitized statement before query-text truncation, cached with
the other metadata, and remain available when `OmitQueryText` is enabled.
They describe SQL structure, not execution plans or tables accessed inside a
stored function. Unknown syntax falls back to a recognized operation or `SQL`.
Connection and batch container spans do not emit a fictitious query summary;
batch query events and prepared statements carry their actual SQL summaries.

Inspect the largest *runtime* sqlc statements without executing the application:

```sh
go run ./cmd/sqlsummary -dir ../stonkpile/internal/db -largest 10
```

Use `-json` for structured output and `-out report.txt` to save the report. Ranking
is by generated SQL byte size, not source file size or estimated query cost.

## C applications

[`c-gotel`](c-gotel/README.md) provides a pure-C telemetry host, shared HTTP naming,
named SQLite tracing, context, logs and cumulative operation metrics over OTLP/HTTP.
