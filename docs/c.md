# Gotel C API

Gotel provides bounded OpenTelemetry operation naming, traces, logs, metrics and
instrumentation on top of otlp-c. Include `<gotel/gotel.h>` and link `gotel`.
The [main header](gotel_8h.html), [SQL attributes](sql_8h.html) and
[test support](testing_8h.html) references cover all public C interfaces.

## Ownership and threading

Initialize a `gotel_options_t` with a fixed operation/metric catalog. Options and
exporter strings are copied during `gotel_create`; catalogs and their names must
remain alive until destruction. Producer operations can run on multiple threads,
but a single owner must call `gotel_tick` and `gotel_drain`. No background thread
is started. Stop and join producers before draining and destroying the host.

```c
gotel_scope_t scope = {0};
gotel_begin(host, &scope, operation_index, NULL);
gotel_attr_string(&scope, "app.component", "worker");
/* Perform the operation; call gotel_error on failure. */
gotel_end(&scope);
```

Explicit parent contexts can cross queue/thread boundaries by value. Copied span
snapshots carry partial observations without accessing a live span on another
thread. Exporter event callbacks must be thread-safe and must not reenter the
exporter. Watchdog callbacks must be bounded, nonblocking and release locks.

## Configuration and adapters

Apply standard `OTEL_*` environment variables with `gotel_options_apply_env`,
then apply explicit application overrides before creation. Environment storage
must remain alive through host creation. TLS verification is always enabled.
SQLite APIs are available when built with `GOTEL_WITH_SQLITE`; the adapter owns
SQLite's single tracing callback slot and must detach before host destruction.

SQL formatting, sanitization and inspection are provided by
[Sequel](https://zfogg.github.io/sequel/c/). Gotel attaches their sanitized
metadata to OpenTelemetry spans without reading bind values or expanded SQL.

Optional deterministic clocks, no-I/O transport scripts and event recorders
require `gotel::testing`; they are not linked into production by default.
See the repository's C guide for dependency setup, HTTP/gRPC transport options
and complete lifecycle examples.
