# c-gotel

The optional watchdog exports a copied parent metadata snapshot alongside its
deadlock error child. Producers capture `gotel_current_span_snapshot()` before
blocking and return it in `gotel_watchdog_snapshot_t.parent_snapshot` together
with the matching parent context. No live span pointer crosses threads. The
snapshot retains the original IDs, name, ancestor and start time, uses observation
time as its end, and is marked `gotel.span.snapshot` and `gotel.span.incomplete`.
It is a partial observation, not a forced completion; the owner may still finish
normally. Collectors determine how later exports with the same span ID are stored.

Pure-C OpenTelemetry policy and instrumentation, backed by pinned otlp-c.
No Go runtime, C++ SDK, background threads, or SQLite replacement calls.

## Standard environment configuration

Call `gotel_options_apply_env(&options, &storage, default_base_url)` after setting
application defaults, then apply explicit application overrides before
`gotel_create`. Keep `gotel_env_storage_t` alive through create; exporter strings,
headers and resources are copied. `gotel_options_set_endpoint` converts a base
URL to all three signal URLs, retaining path prefixes.

Supported variables are `OTEL_SDK_DISABLED` (case-insensitive `true` disables all
signals), `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`,
`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`,
`OTEL_EXPORTER_OTLP_TIMEOUT` (positive milliseconds),
`OTEL_EXPORTER_OTLP_CERTIFICATE` (PEM CA bundle), and
`OTEL_EXPORTER_OTLP_PROTOCOL` (`http/protobuf`, or `grpc` with the optional native
transport enabled). The
`OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_{ENDPOINT,HEADERS}` variants override
their common settings. Signal endpoints are complete URLs; base endpoints gain
`/v1/<signal>`. Empty variables are unset. `OTEL_SERVICE_NAME` wins over
`service.name` in resource attributes; environment resource attributes override
matching application attributes while retaining other application attributes.

Run the downstream executable checks with
`python c-gotel/tests/ascii_chat_env.py /path/to/ascii-chat`. They exercise all
three wire signals, resource identity, headers, endpoint precedence, disablement,
timeouts, and invalid protocol/certificate configuration using synthetic media.

## Build and test

```sh
python -m pip install -r c-gotel/tests/requirements-grpc.txt
cmake -S c-gotel -B c-gotel/build -G Ninja -DCMAKE_C_COMPILER=clang -DCGOTEL_BUILD_TESTS=ON
cmake --build c-gotel/build
ctest --test-dir c-gotel/build --output-on-failure
```

SQLite headers/libraries are required by default. Disable the adapter with
`-DCGOTEL_WITH_SQLITE=OFF` for consumers without SQLite. A consumer can use
`add_subdirectory(gotel/c-gotel)` and link `gotel::c-gotel`. `BUILD_SHARED_LIBS=ON`
builds a shared c-gotel library; the default builds a static library. Both
forms are tested on Windows, Linux, and macOS. The library keeps the pinned otlp-c
implementation in its own link image because it uses otlp-c's internal TLS and
resource helpers. CMake accepts existing dependency targets through
`CGOTEL_UTF8PROC_TARGET`, `CGOTEL_PCRE2_TARGET`, and `CGOTEL_SQLITE_TARGET`;
otherwise it discovers SQLite3 and discovers or fetches utf8proc and PCRE2.
ascii-chat passes its existing targets and chooses
the static form for release builds. `FETCHCONTENT_SOURCE_DIR_OTLP_C` supports
offline caches.

Tagged releases provide separate platform-specific static and shared library
ZIPs. Both contain public headers; static ZIPs include `c-gotel`, `otlp-c`, and `nghttp2`
archives, while shared ZIPs contain `c-gotel` plus any platform import library.
Read each ZIP's `README.md` for external dependencies, toolchain requirements,
and the runtime libraries needed by shared builds.

## Host and operation scopes

Host creation detects resource identity once, using the same categories as Go
`otelhost.NewResource`: SDK, hostname, OS description/type, process PID, owner,
executable path/name, command arguments, and compiler/runtime description.
These attributes are resources on **every** trace, log, and metric envelope.
`process.command_args` is an OTLP string array. Explicit scalar
`exporter.resource_attributes` override detected values; environment resource
overrides remain opt-in through the existing exporter environment helper.

Set `argc`/`argv` to supply application-redacted command arguments; their values
are deep-copied during `gotel_create`. NULL `argv` discovers native arguments
on Windows, Linux, and macOS. `omit_command_args` suppresses detection of command
arguments, and `disable_resource_detection` disables all default detectors.
Service version and deployment environment remain caller-supplied attributes,
as in Go. `telemetry.sdk.language` is `c`; C reports its actual compiler
rather than claiming a Go runtime.

Declare a bounded array of `gotel_operation_t` identities, then initialize
`gotel_options_t` with exporter options, signal mask, sampling probability,
operation catalog, monotonic/realtime clock callbacks and metrics interval.
`gotel_create` returns a host. Call `gotel_begin(host, &scope, operation, NULL)`
and `gotel_end(&scope)` on every exit path. `gotel_error` records a failure;
`gotel_attr_string` and `gotel_attr_int` add bounded operation metadata.

Nested scopes inherit thread-local context. `gotel_current_context()` copies
IDs and the sampling decision by value; pass that copy to `gotel_begin` after
crossing a worker queue. Do not move a live mutable scope between threads.
End scopes in stack order. A realtime root samples independently of a CLI
parent, and its descendants share that decision. Probability and a per-host
per-second root budget bound frame/audio/packet trace volume. Metrics count
all executions, including unsampled ones.

Each catalog operation exports cumulative `<operation>.count`,
`<operation>.errors`, and `<operation>.duration.sum` (seconds). These are
counts and total duration, not percentile histograms. Cardinality is bounded
by the catalog, not user URLs, query parameters or client identifiers.

## Logs and source locations

Call `gotel_log(host, severity, message)` to emit a log. In Debug builds, the
macro records its file, line, and function automatically. The existing
six-argument form remains source-compatible for adapters that already have
that information. In Release builds (or whenever `NDEBUG` is defined), both
forms omit source locations so these calls do not compile file paths into the
application or send them to the collector. Other application code may still
contain its own file paths.

Use `gotel_log_at(host, severity, message, file, line, function)` when source
metadata is intentionally needed regardless of build type. Define
`CGOTEL_LOG_SOURCE_ENABLED` as `0` or `1` before including the header to
override the default for the macro forms.

## Export ownership

Producers enqueue telemetry; one caller owns `gotel_tick`. In a threaded
application run it periodically from a worker. An event-loop application can
call it from its own timer. Tick never creates a thread, but connection DNS
resolution can block: prefer numeric loopback collector addresses.
Stop and join producers, stop the tick owner, call `gotel_drain` for the
configured bounded flush, then `gotel_destroy`. Never free a host borrowed
by an active scope or SQLite connection. Diagnostics are opt-in through
`gotel_set_logger`; its callback must not recursively export its own logs.

The default transport is OTLP/HTTP protobuf, with verified HTTPS support.
No telemetry is sent merely by linking the library; the application controls
host creation.

## Native OTLP/gRPC

Native gRPC support is built by default using pure-C nghttp2. Disable it with
`-DCGOTEL_WITH_GRPC=OFF` to omit the HTTP/2 dependency. HTTP/protobuf remains the
default export protocol. Set `options.protocol = "grpc"`, or opt into environment
configuration with `OTEL_EXPORTER_OTLP_PROTOCOL=grpc`. A build without gRPC
rejects that selection. No upstream checkout is changed: the build stages and
checks the pinned otlp-c sources, retaining its queues, protobuf encoding,
batching, retry budgets, diagnostics, and caller-driven tick/drain lifecycle.

gRPC endpoints are origins, for example `http://127.0.0.1:4317` for plaintext
HTTP/2 or `https://collector.example.com:4317` for verified TLS. Omit `/v1/traces`
and other HTTP paths; non-root paths are rejected. With no endpoint configured,
gRPC uses `https://localhost:4317`. The endpoint helper and standard environment
base endpoint set all three origins without appending HTTP signal paths.
Signal-specific endpoint and header overrides work for gRPC as well. One host
uses one protocol; conflicting per-signal protocol environment settings are
rejected. Separate hosts can use different protocols.

The transport implements unary Export RPCs for traces, metrics and logs, gRPC
framing, HTTP/2 flow control, trailer status handling, bounded responses (4 MiB),
and RetryInfo backpressure (subject to the exporter's configured backoff cap).
Partial success is decoded by otlp-c and is not retried. Resource exhaustion is
retried only when the server supplies valid RetryInfo. Metadata names must be
lowercase gRPC keys; connection and reserved protocol headers cannot be overridden.
TLS reuses the existing verified OpenSSL trust store and requires ALPN `h2`.

This initial transport opens a connection per batch and runs one RPC at a time;
it does not multiplex batches, pool connections, compress messages, support
client certificates, or provide a general-purpose gRPC client. There are no new
library threads. DNS retains the existing synchronous resolution behavior.
Existing exporter HTTP diagnostic/status buckets represent mapped delivery
classes for gRPC (200 success, 503 retryable, 400 permanent), not wire HTTP status.

When building tests with gRPC enabled, install the test-only Python peers with
`python -m pip install -r c-gotel/tests/requirements-grpc.txt`. CTest exercises a
real grpcio server and malformed HTTP/2 peers, including large records, all three
signals, metadata, environment configuration, retries, partial success, invalid
frames/statuses, resets, GOAWAY, deadlines, TLS trust, identity and ALPN failure.

## HTTP

`gotel_http_name` implements the shared gotel naming convention with PCRE2 ID
matching and utf8proc validation:
`app.http.client.GET example.com:8443/items/{id}` and
`app.http.server.GET /items/{id}`. Client URLs omit userinfo, query and fragment;
integer, long hexadecimal and UUID path segments become `{id}`. A server must
supply a resolved route template, or the empty string for `unknown`.
The caller times its existing HTTP operation and sets the returned name on
its scope. `gotel_http_attributes` records method/status and client errors.

## Named SQLite queries

`gotel/sql.h` provides SQL inspection independently of the SQLite adapter and
does not require linking SQLite. It supports common SQL plus PostgreSQL,
DuckDB, SQLite, and MySQL quoting/placeholder rules. Its scanner removes comments
and replaces string/blob/numeric/boolean/null literals and bind placeholders.
It never uses expanded SQL or reads bind values. Malformed quoting fails closed.

`gotel_sql_inspect` returns distinct fields: `text` is the sanitized SQL template,
`summary` is a structural skeleton of CTEs, sources, aliases, joins, nested
queries, clauses and row bounds, `operation` is the first SQL verb, and
`statement_count` counts nonempty top-level statements in the call. Text
defaults to 32768 bytes, independently of the 32768-byte summary budget; both stay
on UTF-8 boundaries. Static integer row bounds may appear in the summary but
are sanitized in query text. Input is limited to 1 MiB and structural recursion
to 64 levels. This is a telemetry parser, not SQL syntax validation or execution.
`gotel_sql_sanitize` exposes the same fail-closed text scanner with a caller-sized
buffer; Go uses it for custom and uncapped query-text policies. Both APIs scan
every statement and reject a malformed later statement without emitting text.
Semicolons in quoted content or comments do not increase `statement_count`.
The implementation is split under `src/sql/`: `scanner.c`, `summary.c`,
`api.c`, and `common.c` are included by `main.c`. CMake compiles only `main.c`;
the Go cgo package includes the same file, so it runs the identical parser.

The SQLite adapter exports `db.query.text`, `db.query.summary`,
`db.operation.name`, `db.system.name`, the bind count, and
`gotel.db.query.statement_count` while retaining the
application-owned span name. The summary is SQL structure, never the query's
application name. `gotel_sqlite_attach_with_options` additionally accepts a
logical database namespace, text omission, a smaller text budget and IN-list
policy. Existing `gotel_sqlite_attach` calls use the safe default policy.

SQL regression tests include 665 cases generated from Go's tests and SQL command checks, 24 production
query examples from pi5 Spaniel/stonkpile (eight each short, medium and long),
2,000 secret-payload mutations, malformed grammar, and size/UTF-8 boundaries.
The checked-in JSON fixtures need no Go installation or live service at test
time. Regenerate the Go corpus with `go run ./c-gotel/tests/sql_reference`;
the same generator accepts input/output JSON paths for inspecting other queries.
Resource and SQLite tests decode real protobuf envelopes from local collectors.

```c
static const gotel_query_t queries[] = {
    {"LookupWidget", "SELECT name FROM widgets WHERE id = ?"},
};
gotel_sqlite_t *attachment = NULL;
gotel_sqlite_attach(host, db, "myapp.storage", queries, 1, &attachment);
/* Ordinary sqlite3_prepare_v2 / bind / step / reset / finalize calls. */
gotel_sqlite_detach(attachment);
sqlite3_close(db);
```

The immutable query registry in `src/named.c` resolves original SQL templates
by text, so separate prepared instances share stable operation names. No bind
values, expanded SQL, unsanitized SQL text, or connection file paths are exported.
Unknown SQL uses `UnknownQuery`. The adapter measures STMT-to-PROFILE elapsed
time with the host's monotonic clock, preserving sub-millisecond queries that
SQLite's built-in PROFILE timer rounds to zero. Statement starts retain their
parent context through reset or early finalize; the adapter emits a completed span and a
`db.client.operation.duration` histogram observation. Parent sampling is honored.
Prepare failures do not produce PROFILE callbacks; record these at existing
application error checks. The adapter does not infer execution errors from
PROFILE, where SQLite error state can describe a previous operation.

Attach once before using a connection, and detach after finishing statements.
It explicitly owns SQLite's single `sqlite3_trace_v2` callback slot: do not
replace another integration's callback. Normal close automatically frees the
attachment; do not also detach a pointer after close. Query descriptors and
prefix must outlive the attachment. Close/detach all connections before freeing
the host. The adapter does not wrap or change SQLite transaction behavior.

## Validation

CTest covers naming and sanitization, invalid options, sampling distribution,
parent decisions, rate limits, context restoration, query lookup, reset/finalize/
close lifecycle, and concurrent producers. A real HTTP receiver decodes protobuf
and checks signal filtering, span/log correlation, timestamps, cumulative metric
values, and exclusion of bound secrets. Tests run with ASan/UBSan on Linux.

## Bounded application metrics

Every operation emits a cumulative `.duration` histogram with explicit second
boundaries (100 microseconds through 10 seconds), alongside count/error/duration
sum counters. Histograms include unsampled operations.

Supply a fixed `gotel_metric_t` catalog through `options.metrics` / `metric_count`.
`gotel_metric_record(host, index, value)` adds to a counter, replaces a gauge,
or observes a histogram. Producers allocate no memory; each instrument serializes
recording and export snapshots with a short atomic lock. Tick/drain publishes
cumulative snapshots, including gauges recorded as zero. Unused instruments are
omitted. Catalog identities and sorted finite histogram bounds are validated
at creation. NaN/infinite observations and negative counter increments are rejected.
The exporter queue can drop a snapshot under pressure; subsequent cumulative
snapshots retain the measurements. Stop producers before drain/destroy.

## Reusable test support

Enable `CGOTEL_BUILD_TEST_SUPPORT=ON` and link `gotel::testing` from a test
executable. Production consumers leave the option off. The helper library has
no Criterion, application, socket, or assertion dependencies.

```c
#include <gotel/testing.h>
gotel_test_clock_set(1000000000, 1700000000000000000ULL);
options.monotonic_ns = gotel_test_monotonic_ns;
options.realtime_ns = gotel_test_realtime_ns;
gotel_test_events_t *events = gotel_test_events_create();
options.exporter_event = gotel_test_event_record;
options.exporter_event_context = events;
gotel_host_t *host = NULL;
/* Check every status using your framework of choice. */
gotel_test_create(&options, &host); /* no HTTP I/O */
const int statuses[] = {503, 200};
gotel_test_script_t script = {statuses, 2, 0, 200};
gotel_test_transport(host, &script);
/* Produce scopes/logs/metrics; advance fake measurement time as needed. */
gotel_drain(host);
gotel_test_event_counts_t counts = gotel_test_events_snapshot(events);
/* Inspect counts and gotel_stats(). */
gotel_destroy(host);
gotel_test_events_destroy(events);
```

Fake clocks are thread-local and affect gotel measurement/sampling intervals;
they do not replace otlp-c's network/backoff clock. Scripts are borrowed and must
outlive their host or replacement. Event counters are thread-safe and count
events, not affected items. Install callbacks before starting producers. Null
transport exercises queueing, signals, outcomes and retry state, but does not
validate HTTP or serialized payloads.

For actual wire assertions, add `c-gotel/testing/python` to the test's Python
module path and import `Collector`, `Response`, `protobuf`, `records`, and
`service_names` from `gotel_testing`. The standard-library-only collector binds
an ephemeral loopback port, supports response scripts and bounded condition waits,
and keeps successful-request snapshots separately from all attempts:

```python
with Collector() as collector:
    collector.script('/v1/traces', [503, Response(200)])
    # Run your executable using collector.url or collector.endpoint('traces').
    # Then inspect records(collector.records, '/v1/traces').
```

Clear capture/scripts only after producers stop. Use the context manager to close
servers; snapshots are copies safe to inspect while requests arrive. The wire
decoder rejects malformed/truncated input instead of silently accepting partial
messages. The support library and collector have their own contract tests.

## HTTPS

c-gotel exports OTLP/protobuf over HTTP or HTTPS using OpenSSL 1.1.1 or newer.
TLS 1.3 is enabled, with TLS 1.2 as the minimum. Certificate chains, validity
periods, and DNS/IP endpoint identities are verified; verification cannot be
disabled. DNS endpoints send SNI. `gotel_options_t.tls_ca_file` selects a PEM
bundle replacing system roots; NULL uses OpenSSL default paths plus native
Windows ROOT/macOS anchors. OpenSSL's `SSL_CERT_FILE`/`SSL_CERT_DIR` apply to
default paths. Custom bundles are read during create and may be released after
create; roots remain owned by the host through request destruction.

CMake reuses existing `OpenSSL::SSL`/`OpenSSL::Crypto` targets when embedded.
Standalone consumers must provide OpenSSL development headers/libraries.
There are no additional library threads. Nonblocking memory BIOs bridge the
existing platform sockets; tick/drain perform handshake and encrypted I/O.
TLS handshakes share the configured send/read inactivity deadline. HTTPS
connections are closed after each batch (plain HTTP retains upstream reuse).
The pinned upstream HTTP sources are adapted in the build directory, with
source hash checks, leaving shared dependency caches untouched.

`c-gotel-tls` tests real TLS 1.2 and TLS 1.3 collectors, all three signals,
70KB records across TLS records, transient failures, default/custom roots,
expired/untrusted/wrong-identity certificates, malformed bundles, and a stalled
handshake. Run `ctest --test-dir build --output-on-failure` after building tests.


## Watchdog callbacks

Set `options.watchdog.snapshot` to opt in to mutex-cycle monitoring. The callback
copies a bounded observation (cycle thread count, minimum wait age, and snapshot
limits); returning false skips an unavailable snapshot. It must never block or
retain application locks. Supply non-realtime operation and GAUGE metric catalog
indices. An optional `decorate` callback adds application-owned diagnostic
attributes to the watchdog span; it runs only after snapshot locks are released.
Callbacks and context must outlive the host.

Call `gotel_tick` from a worker independent of the threads being monitored. The
watchdog polls once per second, requires two seconds of wait age, records the
current blocked count, and emits a completed ERROR child span when a copied parent is available (otherwise a root span) with
`error.type=thread.deadlock` at most once every 30 seconds. No internal threads or
OS-specific APIs are introduced. It does not mutate another thread's active
scope, recover deadlocks, handle fatal signals, or survive a blocked tick owner.


## SQL formatting

`gotel_sql_format(sql, dialect, out, capacity, &length)` formats layout while
preserving comments, quoted contents, identifiers, and bind placeholders. It
never executes SQL or substitutes values. Measure with `out=NULL, capacity=0`,
then allocate `length+1` bytes. Input and output must not overlap. On errors,
the output is emptied. Input is limited to 1 MiB, 65536 tokens, and 64 nested
groups. This is intentionally a formatter, not a SQL syntax validator.

The SQLite adapter and `gotel_sql_attributes` format sanitized `db.query.text`
only; summaries and span names remain concise. The SQLite adapter preserves its
custom collection limit. Direct callers with custom limits can use
`gotel_sql_attributes_with_limit`; formatting failures retain sanitized text.

`testdata/sql_format.json` contains 694 cases, including named statements from
50 stonkpile SQL files with their source revision, Spaniel dashboard queries,
and synthetic dialect/comment/placeholder regressions. The C test suite uses
SQLGlot and sqlparse as independent lexical checks, 1000 deterministic mutations,
and SQLite/DuckDB execution comparisons. Install test dependencies with
`python -m pip install -r c-gotel/tests/requirements-format.txt`.
