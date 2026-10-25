# gotel

SQL layout formatting is provided by [sequel](https://github.com/zfogg/sequel):
`sequel.FormatSQL(dialect, query)`
returns formatted text and an error (the original text on error).
`sequel.FormatSQLOrOriginal` is the tolerant presentation helper. Both preserve
comments, quoted text, identifiers, and bind markers; neither executes SQL or
substitutes values. Formatting supports PostgreSQL, DuckDB, MySQL, SQLite, ClickHouse and a
generic dialect, with a 1 MiB input limit and 64 nesting levels. It is a layout
formatter, not a SQL validator. Telemetry formats only the sanitized template in
`db.query.text`; span names and `db.query.summary` remain concise.

[Latest release](https://github.com/zfogg/gotel/releases/latest) ·
[All releases](https://github.com/zfogg/gotel/releases)

When a request crosses a browser, an HTTP service, a database, and a native
process, “how long did it take?” is only the first question. You also need to
know which operation ran, where it spent time, and whether its trace, logs, and
metrics describe the same work. gotel provides reusable OpenTelemetry
instrumentation for those boundaries in Go, C, Python, and TypeScript.

Your application still chooses its service name and telemetry backend. gotel
supplies stable operation names, resource attributes, safe database metadata,
and OTLP export helpers. Send the resulting telemetry to an OpenTelemetry
Collector or another OTLP-compatible backend.

| Your application | Start here | What it adds |
| --- | --- | --- |
| Go service | [Go packages](#go-services) | SDK setup, HTTP and database spans, logs, metrics, and resource detection |
| Python application | [Python package](python/) | uv package, SDK lifecycle, HTTP/database/Redis/Temporal adapters, and shared SQL inspection |
| Native C application | [gotel](#c---gotel) | Trace scopes, correlated logs, operation metrics, HTTP names, and SQLite spans |
| Browser application | [TypeScript package](#typescript-browser-applications) | Fetch spans with bounded names and an optional browser provider |

Use only the pieces your application needs. Where the languages overlap, they
share conventions: Go, C, and Python compile the **same C SQL scanner** for query
summaries and sanitized query text.

## Optional application-name span prefix

Existing span names keep their application prefix by default. Disable it at
SDK setup to emit `http.server.GET /items` or `storage.Find` instead of
`myapp.http.server.GET /items` or `myapp.storage.Find`. This changes exported
span names only: service resources, instrumentation scopes, metrics, attributes,
span IDs and parent relationships stay intact. Already unprefixed names and
names belonging to other applications are unchanged. Final HTTP route renames
are covered as well as ordinary operation spans.

- **Go:** set `otelhost.Config{ApplicationName: "myapp", DisableSpanNamePrefix: true}`.
  `ApplicationName` defaults to `ServiceName`. For gotel's custom retention
  processor, set `traceprocessor.Config.DisableSpanNamePrefix`; it uses the
  configured `Application`. Other custom exporters can use
  `gotel.WithoutApplicationPrefix(exporter, "myapp")`.
- **Python:** set `otelhost.Config(application_name="myapp", disable_span_name_prefix=True)`.
  `application_name` defaults to the resource's `service.name`.
- **TypeScript:** pass `disableSpanNamePrefix: true` to `startBrowserTelemetry`;
  the existing `prefix` identifies the application. Custom exporters can use
  `withoutApplicationPrefix(exporter, "myapp")`.
- **C:** set `options.disable_span_name_prefix = true` and
  `options.application_name = "myapp"` before `gotel_create`. The application
  name defaults to `options.exporter.service_name`. Operation, watchdog snapshot
  and SQLite span names follow the same policy; operation metric names retain
  their catalog names.

When service names vary by process (for example `myapp-api` and `myapp-worker`),
set the shared application name explicitly. Prefix removal matches the exact
`myapp.` boundary once; it does not remove component names such as `storage`.

## Repository layout and migration

Language libraries live in `c/`, `go/`, `python/`, and `typescript/`. The
TypeScript package manifest remains at the repository root for existing Git
package consumers. Python is a uv project; see [its guide](python/README.md).

The Go module is now `github.com/zfogg/gotel/go`; update imports to that prefix.
Its immutable module tags are `go/vX.Y.Z`, paired with the repository's `vX.Y.Z`
release. C consumers use `add_subdirectory(gotel/c)`, link `gotel::gotel`, and
replace `CGOTEL_*` options with `GOTEL_*`. Native files are named `libgotel` /
`gotel.dll`; release archives are named `gotel-vX.Y.Z-<os>-<arch>-<linkage>.zip`.
These are breaking packaging changes; prior releases retain their original paths.

SQL processing and the standalone CLI are owned by [sequel](https://github.com/zfogg/sequel).
CMake fetches an immutable sequel commit; Go imports `github.com/zfogg/sequel/go`,
and Python depends on the `sequel` distribution at the same revision. SQL processing callers import sequel directly; gotel tracing
adapters use it internally. Sequel owns the native implementation, bindings,
regression corpora and formatting/sanitization fuzz tests.

## Development commands

Python tooling is managed by uv in `python/`: Ruff handles linting, import sorting,
and formatting; ty checks types in the library, tests, and build configuration.
Run these targets from the repository root with GNU Make and uv installed:

| Command | Purpose |
| --- | --- |
| `make sync` | Install the locked Python development environment and all adapters |
| `make format` | Sort Python imports and apply Ruff formatting |
| `make lint` | Check Python lint rules and formatting without changing files |
| `make typecheck` | Run ty against Python source, tests |
| `make test-python` | Run Python tests |
| `make test-go` | Run Go tests and vet |
| `make test-typescript` | Run TypeScript type checks and tests |
| `make test` | Run Python, Go, and TypeScript tests |
| `make check` | Run Python lint/type checks and all three language test targets |
| `make test-c` | Build and test an already configured `c/build` |

Install TypeScript dependencies with `pnpm install --frozen-lockfile` before the
TypeScript targets. C requires its native dependencies and initial CMake
configuration; see [the C guide](c/README.md). Without Make, the Python equivalents
are `uv run --directory python --all-extras ruff format src tests`,
`uv run --directory python --all-extras ruff check src tests`, and
`uv run --directory python --all-extras ty check`. `make format` also sorts imports.

## Go services

Install the module in your Go application:

```sh
go get github.com/zfogg/gotel/go@latest
```

[`otelhost`](go/otelhost/) gives a service one place to configure OpenTelemetry
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

Import `github.com/zfogg/gotel/go/otelhost` for that example. If your service
already configures the OpenTelemetry SDK, keep its providers and use gotel's
instrumentation packages independently.

### Give HTTP operations useful names

[`httptrace`](go/httptrace/) works with `otelhttp` and `net/http` without requiring
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

Import `github.com/zfogg/gotel/go/httptrace`. Go `ServeMux` supplies the resolved
route through `Request.Pattern`. For another router, give `RouteMiddleware` a
callback that returns its route template, or call `SetServerRoute` on the
existing span. Neither hook creates another span or wraps the response writer.

### See a database call without exposing its values

[`sqltrace`](go/sqltrace/) keeps the application operation name separate from SQL
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

Import `github.com/zfogg/gotel/go/sqltrace` and
`github.com/zfogg/gotel/go/gormtrace`. [`pgxtrace`](go/pgxtrace/) covers pgx queries,
COPY, batches, prepare, and connect. For `database/sql`, use
`sqltrace.QueryContext`, `QueryRowContext`, or `ExecContext`; they end spans at
the appropriate point in the rows' lifecycle. `sqltrace.StartSpan` covers
custom database operations, and [`dbmetrics`](go/dbmetrics/) provides the
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

SQL parsing lives in [sequel](https://github.com/zfogg/sequel) and its Go binding
compiles it through cgo. **`go get` does not run CMake.** Building an
application that imports `sqltrace` or its adapters requires `CGO_ENABLED=1`
and a C compiler. The Go package bundles pinned utf8proc source; no system
utf8proc installation is needed. From this repository, inspect generated sqlc
queries without running the application with
`go run github.com/zfogg/sequel/cmd/sqlsummary -dir PATH -largest 10`.

[`mcptrace`](go/mcptrace/) instruments the official Go MCP SDK with connected
request/tool spans, counters, and latency histograms. It bounds method/tool
labels and omits arguments, results, and error contents.

Other Go packages include [`redistrace`](go/redistrace/),
[`temporaltrace`](go/temporaltrace/), [`slogadapter`](go/slogadapter/), and shared
span/shutdown helpers in the root package.

## C - gotel

[`gotel`](c/) is a pure-C telemetry host for native applications. It
provides named trace scopes, correlated logs, cumulative operation metrics,
resource detection, bounded HTTP names, and optional SQLite query spans over
OTLP/HTTP protobuf, with native OTLP/gRPC support enabled by default. It needs
no Go runtime or C++ SDK. The SQLite adapter
attaches to an existing connection; your application keeps using ordinary
SQLite calls. SQL inspection also works when SQLite support is off.

Add it to an existing CMake project and link its target:

```cmake
add_subdirectory(gotel/c)
target_link_libraries(my-app PRIVATE gotel::gotel)
```

Or build the C library on its own with Clang and Ninja:

```sh
cmake -S c -B c/build -G Ninja -DCMAKE_C_COMPILER=clang
cmake --build c/build
```

The default is a static library; `-DBUILD_SHARED_LIBS=ON` selects a shared
library. SQLite is enabled by default; `-DGOTEL_WITH_SQLITE=OFF` excludes its
adapter. A parent project can pass its utf8proc and PCRE2 targets, or CMake
resolves them. The [C library guide](c/README.md) has the target names,
platform build details, and full lifecycle. For native gRPC, set
`options.protocol = "grpc"` (or
`OTEL_EXPORTER_OTLP_PROTOCOL=grpc` when applying environment configuration).
Use an origin such as `https://collector.example.com:4317`, without HTTP signal
paths. The gRPC transport uses nghttp2 with the existing verified TLS
implementation; it does not start library threads. Build with
`-DGOTEL_WITH_GRPC=OFF` to omit gRPC support and its HTTP/2 dependency.

The integration follows your application's own loop: declare a fixed operation
catalog, create a host, call `gotel_begin`/`gotel_end` around work, and call
`gotel_tick` periodically from **one** worker or event loop to export. The
library does not start a background thread. Stop producers, drain, and destroy
the host on shutdown. The catalog bounds metric cardinality; unsampled calls
still contribute to operation metrics. A copied context can cross a worker
queue and become a child span without sharing a live mutable scope.

For SQL, [`sequel_inspect`](https://github.com/zfogg/sequel/blob/main/include/sequel.h) returns sanitized
text, a structural summary, the outer operation, truncation status, and the
number of nonempty top-level statements. `sequel_sanitize` exposes the same
scanner with a caller-sized output buffer:

```c
#include <sequel.h>

sequel_options_t policy = {.dialect = SEQUEL_POSTGRESQL};
sequel_metadata_t query;
if (sequel_inspect("SELECT * FROM items WHERE id=$1; UPDATE audit SET seen=true",
                      &policy, &query) == SEQUEL_OK) {
  /* query.statement_count is 2; query.text contains no bind values. */
}
```

The scanner handles all statements, including those after semicolons and
interleaved comments; semicolons inside quoted text do not add a statement.
Both C entry points fail closed if a later statement contains malformed
quoting. The SQLite adapter attaches `db.query.text`, `db.query.summary`,
`db.operation.name`, and statement/bind counts to spans while retaining your
application-owned span name. Its SQL implementation and language bindings live in sequel; gotel owns the
span attributes and collection policy.

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

Run Go tests with `go -C go test ./...` and browser tests with `pnpm test`.
`pnpm build` generates the ignored TypeScript `dist/` directory; package
packing builds it automatically and includes the compiled files. For C,
enable `GOTEL_BUILD_TESTS=ON` during configure and run
`ctest --test-dir c/build --output-on-failure`. CI builds and tests the
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
[C library guide](c/README.md), the [SQL fixtures](go/sqltrace/testdata/),
and [LICENSE](LICENSE) for further detail.

## Releases

Push a `vMAJOR.MINOR.PATCH` tag to run the
[release workflow](.github/workflows/release.yml) and build a GitHub Release.
Run that workflow manually with the intended tag first to build and test its
assets without publishing them.
The tag must match the CMake and TypeScript package versions. The release
contains tested C static and shared libraries for x64 and ARM64 on Linux,
macOS, and Windows, with public headers and linking notes in separate ZIPs;
one ZIP of the compiled
TypeScript package; and SHA-256 checksums.
Each ZIP has a use-focused `README.md`; the GitHub Release notes summarize
the C linking requirements and TypeScript installation steps.
The C archives retain external dependencies such as OpenSSL, SQLite, PCRE2,
and utf8proc. Read the [C linking guide](c/RELEASE.md) for static and
shared libraries, including platform libraries and DLL, dylib, or `.so`
placement required at run time.

For Go, the Git tag **is** the release artifact:
`go get github.com/zfogg/gotel/go@vMAJOR.MINOR.PATCH` retrieves the matching source and
builds it with the consumer's toolchain. A compiled Go `.a` would be tied to
a toolchain and is not useful as a normal Go module distribution. For
TypeScript installation, prefer npm registry publication when configured or a
Git dependency. The release ZIP contains `dist/`, `package.json`, the README,
and the license for direct inspection or unpacking. CI uses `npm pack` while
assembling the ZIP, but does not upload the intermediate `.tgz`.


After publishing, update the known consumers: **ascii-chat, spaniel, ratio,
stonkpile, and beanaries**. Beanaries consumes both Python and TypeScript; update
its uv and pnpm manifests and lockfiles together. See the
[consumer rollout list](.github/RELEASE_NOTES.md#consumer-updates) and
[release skill](.agents/skills/gotel-release/SKILL.md#update-consumers).

## SQL extraction into sequel

SQL processing now lives in [sequel](https://github.com/zfogg/sequel). C consumers
include `<sequel.h>` and call `sequel_inspect`, `sequel_sanitize`, or
`sequel_format`, checking `SEQUEL_OK`. Go callers import
`github.com/zfogg/sequel/go`; Python callers import `sequel`. Processing wrappers
were removed from gotel. Gotel retains database span naming, telemetry metadata,
collection policy and instrumentation. Existing Go tracing policy types alias
sequel's types; callers obtain default policies from `sequel.DefaultSanitizeOptions`.

The old executable is now `sequel format` / `sequel sanitize`; both accept stdin,
a string, or file paths, with optional `--embedded` and `--edit-file`.
All three gotel dependencies pin the same sequel commit. Release archives include
sequel's public header, libraries and licenses; gotel's Python wheel is pure Python.
