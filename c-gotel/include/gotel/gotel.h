#ifndef CGOTEL_H
#define CGOTEL_H
#include <otlp-c/otlp.h>
#ifdef __cplusplus
extern "C" {
#endif

typedef struct gotel_host gotel_host_t;
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
  otlp_exporter_opts_t exporter;
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
  uint8_t trace_id[16], span_id[8];
  bool valid, sampled, realtime;
} gotel_context_t;
typedef struct gotel_scope {
  gotel_host_t *host;
  otlp_span_t *span;
  gotel_context_t context;
  struct gotel_scope *previous;
  uint64_t started_ns;
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
void gotel_attr_string(gotel_scope_t *scope, const char *key, const char *value);
void gotel_attr_int(gotel_scope_t *scope, const char *key, int64_t value);
void gotel_log(gotel_host_t *host, otlp_severity_t severity, const char *body, const char *file, int line,
               const char *function);
/* Safe bounded HTTP naming. Returns overflow rather than truncating identities.
 * Client URL excludes userinfo/query/fragment and normalizes common ID segments.
 * Server target is a caller-supplied route template, never a raw request path. */
otlp_status_t gotel_http_name(char *output, size_t capacity, const char *prefix, bool server, const char *method,
                              const char *target);
void gotel_http_attributes(gotel_scope_t *scope, const char *method, int status);
#ifdef CGOTEL_WITH_SQLITE
#include <sqlite3.h>
typedef struct {
  const char *name;
  const char *sql;
} gotel_query_t;
typedef struct gotel_sqlite gotel_sqlite_t;
/* Explicitly owns sqlite3_trace_v2's single callback slot. Attach once before
 * use; detach after statements finish and before close/host destruction.
 * Queries/prefix must remain alive through detach. Close automatically detaches.
 * No expanded SQL is read. */
otlp_status_t gotel_sqlite_attach(gotel_host_t *host, sqlite3 *db, const char *prefix, const gotel_query_t *queries,
                                  size_t count, gotel_sqlite_t **out);
void gotel_sqlite_detach(gotel_sqlite_t *attachment);
const gotel_query_t *gotel_query_find(const gotel_query_t *queries, size_t count, const char *sql);
#endif
#ifdef __cplusplus
}
#endif
#endif
