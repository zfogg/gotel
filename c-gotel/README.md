# c-gotel

Pure-C OpenTelemetry policy and instrumentation, backed by pinned otlp-c.
No Go runtime, C++ SDK, background threads, or SQLite replacement calls.

## Build and test

```sh
cmake -S c-gotel -B c-gotel/build -G Ninja -DCMAKE_C_COMPILER=clang -DCGOTEL_BUILD_TESTS=ON
cmake --build c-gotel/build
ctest --test-dir c-gotel/build --output-on-failure
```

SQLite headers/libraries are required by default. Disable the adapter with
`-DCGOTEL_WITH_SQLITE=OFF` for consumers without SQLite. A consumer can use
`add_subdirectory(gotel/c-gotel)` and link `gotel::c-gotel`. FetchContent pins
otlp-c to a commit; `FETCHCONTENT_SOURCE_DIR_OTLP_C` supports offline caches.

## Host and operation scopes

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

## Export ownership

Producers enqueue telemetry; one caller owns `gotel_tick`. In a threaded
application run it periodically from a worker. An event-loop application can
call it from its own timer. Tick never creates a thread, but connection DNS
resolution can block: prefer numeric loopback collector addresses.
Stop and join producers, stop the tick owner, call `gotel_drain` for the
configured bounded flush, then `gotel_destroy`. Never free a host borrowed
by an active scope or SQLite connection. Diagnostics are opt-in through
`gotel_set_logger`; its callback must not recursively export its own logs.

The transport is OTLP/HTTP protobuf. For remote HTTPS backends, send to a local
OpenTelemetry Collector that handles TLS and forwards downstream. No telemetry
is sent merely by linking the library; the application controls host creation.

## HTTP

`gotel_http_name` implements the shared gotel naming convention:
`app.http.client.GET example.com:8443/items/{id}` and
`app.http.server.GET /items/{id}`. Client URLs omit userinfo, query and fragment;
integer, long hexadecimal and UUID path segments become `{id}`. A server must
supply a resolved route template, or the empty string for `unknown`.
The caller times its existing HTTP operation and sets the returned name on
its scope. `gotel_http_attributes` records method/status and client errors.

## Named SQLite queries

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
values, expanded SQL, raw SQL text, or connection file paths are exported.
Unknown SQL uses `UnknownQuery`. SQLite PROFILE supplies actual duration at
completion, reset or early finalize; the adapter emits a completed span and a
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
