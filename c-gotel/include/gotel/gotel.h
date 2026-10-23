#ifndef CGOTEL_H
#define CGOTEL_H
#include <otlp-c/otlp.h>
#ifdef __cplusplus
extern "C" {
#endif

typedef struct gotel_host gotel_host_t;
typedef struct gotel_scope gotel_scope_t;
enum { GOTEL_TRACES = 1, GOTEL_LOGS = 2, GOTEL_METRICS = 4, GOTEL_ALL = 7 };
typedef struct {
  const char *name;
  bool realtime;
} gotel_operation_t;
typedef struct {
  const char *name, *unit;
  otlp_metric_type_t type;
  const double *bounds;
  size_t bound_count;
} gotel_metric_t;

typedef struct {
  uint8_t trace_id[16], span_id[8];
  bool valid, sampled, realtime;
} gotel_context_t;

/* Immutable metadata copied by the owning thread before it blocks. Contains no
 * live span pointers; publishing it never ends or mutates the original scope. */
typedef struct {
  gotel_context_t context;
  uint8_t parent_span_id[8];
  uint64_t start_unix_ns;
  char name[224];
} gotel_span_snapshot_t;

/* Optional lock-cycle watchdog, polled by the single tick owner on its own thread.
 * Snapshot must be bounded, nonblocking, and release all application locks before
 * returning. False means unavailable, not healthy. No signal-handler use. */
typedef struct {
  size_t blocked_threads;
  uint64_t wait_ns;
  bool limited;
  gotel_context_t parent; /* Copied context of a blocked operation, or zero for root. */
  gotel_span_snapshot_t parent_snapshot;
} gotel_watchdog_snapshot_t;
typedef struct {
  bool (*snapshot)(void *context, gotel_watchdog_snapshot_t *output);
  void *context;
  /* Runs on the tick owner after snapshot; must also be bounded/nonblocking. */
  void (*decorate)(void *context, gotel_scope_t *scope);
  size_t operation, metric; /* Non-realtime operation and GAUGE catalog indices. */
} gotel_watchdog_options_t;

typedef struct {
  otlp_exporter_opts_t exporter;
  /* NULL uses system roots. A PEM bundle replaces system roots. Verification is always enabled. */
  const char *tls_ca_file;
  unsigned signals;
  double realtime_sample_rate;
  uint32_t realtime_max_per_second;
  uint32_t metrics_interval_ms;
  uint64_t (*monotonic_ns)(void);
  uint64_t (*realtime_ns)(void);
  const gotel_operation_t *operations;
  size_t operation_count;
  const gotel_metric_t *metrics;
  size_t metric_count;
  /* Structured exporter events may run on producer or exporter threads.
   * Callbacks must be thread-safe and must not reenter the exporter. */
  otlp_event_fn exporter_event;
  void *exporter_event_context;
  /* Resource detection matches otelhost: host, OS, process and SDK identity.
   * Explicit exporter.resource_attributes override detected scalar attributes.
   * argv is copied at create; NULL discovers native process arguments. Supply
   * a redacted argv when command arguments may contain credentials. */
  int argc;
  const char *const *argv;
  bool omit_command_args;
  bool disable_resource_detection;
  /* Set by gotel_options_apply_env; enables signal-specific header overrides. */
  bool use_environment;
  gotel_watchdog_options_t watchdog;
} gotel_options_t;
/* Options are copied; exporter strings/resources are copied during create.
 * The operation catalog and its names must remain alive until destroy. No internal threads.
 * Stop and join all producers before drain/destroy. Tick/drain: one owner only. */
otlp_status_t gotel_create(const gotel_options_t *options, gotel_host_t **output);
void gotel_destroy(gotel_host_t *host);
/* Fixed catalogs bound cardinality. Record is thread-safe and allocates no memory;
 * tick/drain snapshot cumulative values. Counter values must be nonnegative.
 * Catalogs must remain alive until destroy. Histograms require sorted finite bounds. */
otlp_status_t gotel_metric_record(gotel_host_t *host, size_t metric, double value);
otlp_status_t gotel_tick(gotel_host_t *host);
otlp_status_t gotel_drain(gotel_host_t *host);
void gotel_set_logger(gotel_host_t *host, otlp_log_fn logger, void *context);
otlp_status_t gotel_stats(gotel_host_t *host, otlp_exporter_stats_t *stats);
otlp_status_t gotel_parse_signals(const char *text, unsigned *signals);

typedef struct {
  otlp_env_storage_t exporter;
  otlp_resource_attr_t resources[64];
} gotel_env_storage_t;
/* Apply standard OTEL environment variables over caller defaults. Storage must
 * live through gotel_create; create copies exporter strings and resources.
 * A base URL keeps its path prefix and gains /v1/<signal>. Apply explicit
 * application overrides after this call. Empty environment values are unset. */
otlp_status_t gotel_options_apply_env(gotel_options_t *options, gotel_env_storage_t *storage,
                                      const char *default_base_endpoint);
otlp_status_t gotel_options_set_endpoint(gotel_options_t *options, gotel_env_storage_t *storage,
                                         const char *base_endpoint);

typedef struct gotel_scope {
  gotel_host_t *host;
  otlp_span_t *span;
  gotel_context_t context;
  struct gotel_scope *previous;
  uint64_t started_ns;
  uint64_t started_unix_ns;
  uint8_t parent_span_id[8];
  size_t operation;
  bool active, attached, failed;
} gotel_scope_t;
/* An explicit parent can cross a queue/thread boundary by value. NULL inherits
 * the current thread scope. Realtime roots sample independently of CLI spans. */
void gotel_begin(gotel_host_t *host, gotel_scope_t *scope, size_t operation, const gotel_context_t *parent);
void gotel_end(gotel_scope_t *scope);
void gotel_current_error(const char *type, const char *message);
void gotel_error(gotel_scope_t *scope, const char *type, const char *message);
gotel_context_t gotel_current_context(void);
gotel_span_snapshot_t gotel_current_span_snapshot(void);
/* Publishes an explicitly marked partial observation with the original IDs and
 * observation time as its end. A later normal completion may update that span.
 * No attributes are read from the live span on another thread. */
otlp_status_t gotel_publish_span_snapshot(gotel_host_t *host, const gotel_span_snapshot_t *snapshot);
void gotel_attr_string(gotel_scope_t *scope, const char *key, const char *value);
void gotel_attr_int(gotel_scope_t *scope, const char *key, int64_t value);
/* Explicit source metadata is available to adapters that already capture the
 * original call site. The old six-argument symbol remains for existing binaries. */
void gotel_log_at(gotel_host_t *host, otlp_severity_t severity, const char *body, const char *file, int line,
                  const char *function);
void gotel_log(gotel_host_t *host, otlp_severity_t severity, const char *body, const char *file, int line,
               const char *function);

/* Three arguments capture the call site in development builds. The legacy
 * six-argument form accepts an existing location. Optimized builds omit both
 * forms' source locations so __FILE__ paths are not embedded by these calls.
 * Define CGOTEL_LOG_SOURCE_ENABLED explicitly to override the NDEBUG default. */
#ifndef CGOTEL_LOG_SOURCE_ENABLED
#ifdef NDEBUG
#define CGOTEL_LOG_SOURCE_ENABLED 0
#else
#define CGOTEL_LOG_SOURCE_ENABLED 1
#endif
#endif

#if CGOTEL_LOG_SOURCE_ENABLED
#define CGOTEL_LOG_AUTO(host, severity, body) gotel_log_at((host), (severity), (body), __FILE__, __LINE__, __func__)
#define CGOTEL_LOG_EXPLICIT(host, severity, body, file, line, function)                                                \
  gotel_log_at((host), (severity), (body), (file), (line), (function))
#else
#define CGOTEL_LOG_AUTO(host, severity, body) gotel_log_at((host), (severity), (body), NULL, 0, NULL)
#define CGOTEL_LOG_EXPLICIT(host, severity, body, file, line, function)                                                \
  gotel_log_at((host), (severity), (body), NULL, 0, NULL)
#endif
#define CGOTEL_LOG_DISPATCH(_1, _2, _3, _4, _5, _6, selected, ...) selected
#define gotel_log(...)                                                                                                 \
  CGOTEL_LOG_DISPATCH(__VA_ARGS__, CGOTEL_LOG_EXPLICIT, CGOTEL_LOG_INVALID, CGOTEL_LOG_INVALID, CGOTEL_LOG_AUTO)       \
  (__VA_ARGS__)
/* Safe bounded HTTP naming. Returns overflow rather than truncating identities.
 * Client URL excludes userinfo/query/fragment and normalizes common ID segments.
 * Server target is a caller-supplied route template, never a raw request path. */
otlp_status_t gotel_http_name(char *output, size_t capacity, const char *prefix, bool server, const char *method,
                              const char *target);
void gotel_http_attributes(gotel_scope_t *scope, const char *method, int status);
#ifdef CGOTEL_WITH_SQLITE
#include <gotel/sql.h>
#include <sqlite3.h>
typedef struct {
  const char *name;
  const char *sql;
} gotel_query_t;
typedef struct gotel_sqlite gotel_sqlite_t;
typedef struct {
  const char *namespace_name;
  gotel_sql_options_t sql;
} gotel_sqlite_options_t;
/* Explicitly owns sqlite3_trace_v2's single callback slot. Attach once before
 * use; detach after statements finish and before close/host destruction.
 * Queries/prefix must remain alive through detach. Close automatically detaches.
 * No expanded SQL is read. */
otlp_status_t gotel_sqlite_attach(gotel_host_t *host, sqlite3 *db, const char *prefix, const gotel_query_t *queries,
                                  size_t count, gotel_sqlite_t **out);
/* Options are copied; namespace_name has the same lifetime as prefix/queries.
 * The adapter always uses SQLite quoting; other SQL consumers use sql.h. */
otlp_status_t gotel_sqlite_attach_with_options(gotel_host_t *host, sqlite3 *db, const char *prefix,
                                               const gotel_query_t *queries, size_t count,
                                               const gotel_sqlite_options_t *options, gotel_sqlite_t **out);
void gotel_sqlite_detach(gotel_sqlite_t *attachment);
const gotel_query_t *gotel_query_find(const gotel_query_t *queries, size_t count, const char *sql);
#endif
#ifdef __cplusplus
}
#endif
#endif
