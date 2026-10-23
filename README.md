# gotel

[Latest release](https://github.com/zfogg/gotel/releases/latest) ·
[All releases](https://github.com/zfogg/gotel/releases)

When a request crosses a browser, an HTTP service, a database, and a native
process, “how long did it take?” is only the first question. You also need to
know which operation ran, where it spent time, and whether its trace, logs, and
metrics describe the same work. gotel provides reusable OpenTelemetry
instrumentation for those boundaries in Go, C, and TypeScript.

Your application still chooses its service name and telemetry backend. gotel
supplies stable operation names, resource attributes, safe database metadata,
and OTLP export helpers. Send the resulting telemetry to an OpenTelemetry
Collector or another OTLP-compatible backend.

| Your application | Start here | What it adds |
| --- | --- | --- |
| Go service | [Go packages](#go-services) | SDK setup, HTTP and database spans, logs, metrics, and resource detection |
| Native C application | [c-gotel](#c---c-gotel) | Trace scopes, correlated logs, operation metrics, HTTP names, and SQLite spans |
| Browser application | [TypeScript package](#typescript-browser-applications) | Fetch spans with bounded names and an optional browser provider |

Use only the pieces your application needs. Where the languages overlap, they
share conventions: Go and C compile the **same C SQL scanner** for query
summaries and sanitized query text.

## Go services

Install the module in your Go application:

```sh
go get github.com/zfogg/gotel@latest
```

[`otelhost`](otelhost/) gives a service one place to configure OpenTelemetry
providers, OTLP export, resources, propagation, and shutdown. It can export
traces, metrics, and logs over gRPC or HTTP/protobuf. For example:

```go
runtime, err := otelhost.Start(ctx, otelhost.Config{
    ServiceName: "catalog-api",
    Endpoint:    "http://localhost:4318",
    Protocol:    otelhost.ProtocolHTTPProtobuf,
})
if err != nil {
    return err
}
defer runtime.Shutdown(context.Background())
```

Import `github.com/zfogg/gotel/otelhost` for that example. If your service
already configures the OpenTelemetry SDK, keep its providers and use gotel's
instrumentation packages independently.

### Give HTTP operations useful names

[`httptrace`](httptrace/) works with `otelhttp` and `net/http` without requiring
a particular router. A request to `/items/123` can appear as
`catalog.http.server.GET /items/{id}` instead of creating a new span name for
every item. Client names retain the destination host, port, and path while
excluding credentials, query strings, and fragments. Numeric IDs, UUIDs, and
long hex IDs in client paths become `{id}`.

```go
names := httptrace.Namer{Prefix: "catalog"}
client := &http.Client{Transport: names.Transport(http.DefaultTransport)}
handler := names.Handler(mux, "catalog")
```

Import `github.com/zfogg/gotel/httptrace`. Go `ServeMux` supplies the resolved
route through `Request.Pattern`. For another router, give `RouteMiddleware` a
callback that returns its route template, or call `SetServerRoute` on the
existing span. Neither hook creates another span or wraps the response writer.

### See a database call without exposing its values

[`sqltrace`](sqltrace/) keeps the application operation name separate from SQL
structure. A `ListWidgets` call can have a stable span name such as
`catalog.storage.ListWidgets` alongside `db.query.text`, a structural
`db.query.summary`, the database system and namespace, and bind and statement
counts. `gotel.db.query.statement_count` is usually `1` but records multiple
statements sent in one call. Bind **values** are never added to those attributes.

For a GORM application with an existing `db` connection:

```go
namer := sqltrace.MustNamer("catalog.storage")
err := db.Use(gormtrace.New(gormtrace.Config{
    Scope:     "catalog/storage",
    Namer:     namer,
    System:    "postgresql",
    Namespace: "catalog",
    Dialect:   sqltrace.DialectPostgreSQL,
}))
if err != nil {
    return err
}

queryCtx := namer.WithName(ctx, "ListWidgets")
err = db.WithContext(queryCtx).Find(&widgets).Error
```

Import `github.com/zfogg/gotel/sqltrace` and
`github.com/zfogg/gotel/gormtrace`. [`pgxtrace`](pgxtrace/) covers pgx queries,
COPY, batches, prepare, and connect. For `database/sql`, use
`sqltrace.QueryContext`, `QueryRowContext`, or `ExecContext`; they end spans at
the appropriate point in the rows' lifecycle. `sqltrace.StartSpan` covers
custom database operations, and [`dbmetrics`](dbmetrics/) provides the
`db.client.operation.duration` histogram.

The sanitized query template removes literals, comments, and bind
placeholders. It understands common PostgreSQL, SQLite, DuckDB, and MySQL
quoting and placeholder forms, including multiple statements and comments
between them. Malformed quoting fails closed. Query text defaults to a **32 KiB
UTF-8-safe cap**; applications can choose a smaller cap, omit the text, or use
the Go API's uncapped option within the 1 MiB input limit. Omission still
leaves operation names, summaries, and counts available. Summaries also have a
**32 KiB UTF-8-safe cap** and show SQL shape: CTEs, joins, nested selects, writes, grouping,
ordering, and row bounds. It is a structural description, not a query plan.
Identifiers can appear in summaries, so avoid putting secrets in identifier
names.

SQL parsing lives in [`c-gotel/src/sql/`](c-gotel/src/sql/) and is compiled
into the Go package through cgo. **`go get` does not run CMake.** Building an
application that imports `sqltrace` or its adapters requires `CGO_ENABLED=1`
and a C compiler. The Go package bundles pinned utf8proc source; no system
utf8proc installation is needed. From this repository, inspect generated sqlc
queries without running the application with
`go run ./cmd/sqlsummary -dir PATH -largest 10`.

Other Go packages include [`redistrace`](redistrace/),
[`temporaltrace`](temporaltrace/), [`slogadapter`](slogadapter/), and shared
span/shutdown helpers in the root package.

## C - c-gotel

[`c-gotel`](c-gotel/) is a pure-C telemetry host for native applications. It
provides named trace scopes, correlated logs, cumulative operation metrics,
resource detection, bounded HTTP names, and optional SQLite query spans over
OTLP/HTTP protobuf. It needs no Go runtime or C++ SDK. The SQLite adapter
attaches to an existing connection; your application keeps using ordinary
SQLite calls. SQL inspection also works when SQLite support is off.

Add it to an existing CMake project and link its target:

```cmake
add_subdirectory(gotel/c-gotel)
target_link_libraries(my-app PRIVATE gotel::c-gotel)
```

Or build the C library on its own with Clang and Ninja:

```sh
cmake -S c-gotel -B c-gotel/build -G Ninja -DCMAKE_C_COMPILER=clang
cmake --build c-gotel/build
```

The default is a static library; `-DBUILD_SHARED_LIBS=ON` selects a shared
library. SQLite is enabled by default; `-DCGOTEL_WITH_SQLITE=OFF` excludes its
adapter. A parent project can pass its utf8proc and PCRE2 targets, or CMake
resolves them. The [C library guide](c-gotel/README.md) has the target names,
platform build details, and full lifecycle.

The integration follows your application's own loop: declare a fixed operation
catalog, create a host, call `gotel_begin`/`gotel_end` around work, and call
`gotel_tick` periodically from **one** worker or event loop to export. The
library does not start a background thread. Stop producers, drain, and destroy
the host on shutdown. The catalog bounds metric cardinality; unsampled calls
still contribute to operation metrics. A copied context can cross a worker
queue and become a child span without sharing a live mutable scope.

For SQL, [`gotel_sql_inspect`](c-gotel/include/gotel/sql.h) returns sanitized
text, a structural summary, the outer operation, truncation status, and the
number of nonempty top-level statements. `gotel_sql_sanitize` exposes the same
scanner with a caller-sized output buffer:

```c
#include <gotel/sql.h>

gotel_sql_options_t policy = {.dialect = GOTEL_SQL_POSTGRESQL};
gotel_sql_metadata_t query;
if (gotel_sql_inspect("SELECT * FROM items WHERE id=$1; UPDATE audit SET seen=true",
                      &policy, &query) == OTLP_OK) {
  /* query.statement_count is 2; query.text contains no bind values. */
}
```

The scanner handles all statements, including those after semicolons and
interleaved comments; semicolons inside quoted text do not add a statement.
Both C entry points fail closed if a later statement contains malformed
quoting. The SQLite adapter attaches `db.query.text`, `db.query.summary`,
`db.operation.name`, and statement/bind counts to spans while retaining your
application-owned span name. Its SQL implementation is split across
`scanner.c`, `summary.c`, `api.c`, and `common.c`, included by `src/sql/main.c`
for both CMake and cgo.

C resource detection adds host, OS, process, executable, command arguments,
and SDK information to traces, logs, and metrics;
`telemetry.sdk.language` is `c`. `gotel_options_apply_env` supports standard
`OTEL_*` service, resource, endpoint, header, timeout, and enablement settings.
Applications can supply redacted command arguments or omit them. For a remote
HTTPS backend, use a local OpenTelemetry Collector to handle TLS forwarding.
The C `gotel_log(host, severity, message)` macro captures source locations in
Debug builds and omits them in Release builds. Existing six-argument calls
also omit source locations in Release builds; `gotel_log_at` explicitly opts in.

## TypeScript browser applications

[`@zfogg/gotel`](typescript/) instruments browser `fetch` requests and gives
them names that stay useful as URLs change. A same-origin request can become
`web.http.client.GET /items/{id}`; an external request retains its destination
host and non-default port. Query strings and fragments do not enter span
names. The package also offers a browser provider with a service resource and
OTLP trace exporter.

```sh
pnpm add @zfogg/gotel
```

With the package's OpenTelemetry peer dependencies installed, start the
browser provider once during application initialization:

```ts
import { startBrowserTelemetry } from '@zfogg/gotel'

startBrowserTelemetry({
  prefix: 'web',
  serviceName: 'catalog-web',
  traceExportUrl: '/api/telemetry/v1/traces',
  fetch: { ignoreUrls: [/\/api\/telemetry\//] },
})
```

That URL must accept OTLP/HTTP protobuf. `ignoreUrls` keeps the export request
out of its own fetch traces. If your application already owns its provider,
use `createBrowserFetchInstrumentation` instead. You can supply an exporter,
for example an `AuthenticatedSpanExporter` that drops queued spans after
logout. Call `shutdownBrowserTelemetry(serviceName)` when your application
tears down the provider.

## Development and reference

Run Go tests with `go test ./...` and browser tests with `pnpm test`.
`pnpm build` generates the ignored TypeScript `dist/` directory; package
packing builds it automatically and includes the compiled files. For C,
enable `CGOTEL_BUILD_TESTS=ON` during configure and run
`ctest --test-dir c-gotel/build --output-on-failure`. CI builds and tests the
C library as both static and shared on Windows, Linux, and macOS, with an
additional Linux build without the SQLite adapter. The Go and TypeScript tests
also run in CI. The [shared HTTP naming cases](testdata/http_names.tsv) run
against all three implementations so route naming stays consistent.

For an end-to-end starting point, run the
[full-stack example](examples/full-stack/README.md). Docker Compose starts a
Spaniel collector, a browser app instrumented with TypeScript, a Go HTTP API,
and a C worker process. A browser runner makes a real request; a verifier then
checks Spaniel for a connected trace containing spans from all three service
names. The example shows trace context passing through HTTP and a process
boundary, and gives you a local waterfall to inspect. See the
[C library guide](c-gotel/README.md), the [SQL fixtures](sqltrace/testdata/),
and [LICENSE](LICENSE) for further detail.

## Releases

Push a `vMAJOR.MINOR.PATCH` tag to run the
[release workflow](.github/workflows/release.yml) and build a GitHub Release.
Run that workflow manually with the intended tag first to build and test its
assets without publishing them.
The tag must match the CMake and TypeScript package versions. The release
contains tested C static archives for x64 and ARM64 on Linux, macOS, and
Windows, with public headers and linking notes; one ZIP of the compiled
TypeScript package; and SHA-256 checksums.
The C archives retain external dependencies such as OpenSSL, SQLite, PCRE2,
and utf8proc. See [C release linking notes](c-gotel/RELEASE.md).

For Go, the Git tag **is** the release artifact:
`go get github.com/zfogg/gotel@vMAJOR.MINOR.PATCH` retrieves the matching source and
builds it with the consumer's toolchain. A compiled Go `.a` would be tied to
a toolchain and is not useful as a normal Go module distribution. For
TypeScript installation, prefer npm registry publication when configured or a
Git dependency. The release ZIP contains `dist/`, `package.json`, the README,
and the license for direct inspection or unpacking. CI uses `npm pack` while
assembling the ZIP, but does not upload the intermediate `.tgz`.
