#include "internal.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
struct gotel_sqlite {
  gotel_host_t *host;
  sqlite3 *db;
  const char *prefix;
  const gotel_query_t *queries;
  size_t count;
};
static int traced(unsigned event, void *data, void *statement, void *extra) {
  gotel_sqlite_t *a = data;
  if (event == SQLITE_TRACE_CLOSE) {
    free(a);
    return 0;
  }
  if (event != SQLITE_TRACE_PROFILE)
    return 0;
  const char *sql = sqlite3_sql(statement);
  const char *name = "UnknownQuery";
  const gotel_query_t *query = gotel_query_find(a->queries, a->count, sql);
  if (query)
    name = query->name;
  char span_name[256];
  int n = snprintf(span_name, sizeof(span_name), "%s.%s", a->prefix, name);
  if (n < 0 || (size_t)n >= sizeof(span_name))
    return 0;
  gotel_host_t *h = a->host;
  otlp_span_t *span = NULL;
  gotel_context_t current = gotel_current_context();
  if ((h->options.signals & GOTEL_TRACES) && !(current.valid && !current.sampled))
    span = otlp_tracer_start_span(h->tracer, span_name);
  if (span) {
    uint64_t duration = extra ? *(sqlite3_uint64 *)extra : 0;
    otlp_span_set_kind(span, OTLP_SPAN_KIND_CLIENT);
    otlp_span_set_attribute_string(span, "db.system.name", "sqlite");
    otlp_span_set_attribute_string(span, "db.query.summary", name);
    otlp_span_set_attribute_int(span, "gotel.db.query.bind_count", sqlite3_bind_parameter_count(statement));
    gotel_context_t parent = gotel_current_context();
    if (parent.valid && parent.sampled) {
      otlp_span_set_trace_id(span, parent.trace_id);
      otlp_span_set_parent_span_id(span, parent.span_id);
    }
    /* PROFILE supplies duration, including early reset/finalize. Backdate the
     * completed span rather than holding mutable span state across user code. */
    otlp_span_mark_end(span);
    uint64_t end = h->options.realtime_ns();
    otlp_span_set_end_time(span, end);
    otlp_span_set_start_time(span, end >= duration ? end - duration : 0);
    otlp_exporter_emit(h->exporter, span);
    otlp_span_free(span);
  }
  if (h->options.signals & GOTEL_METRICS) {
    otlp_metric_t *m = otlp_metric_create(OTLP_METRIC_HISTOGRAM, "db.client.operation.duration", "s", NULL, NULL, 0);
    if (m) {
      otlp_metric_set_attribute_string(m, "db.system.name", "sqlite");
      otlp_metric_set_attribute_string(m, "db.query.summary", span_name);
      otlp_metric_mark_time(m);
      otlp_metric_record(m, extra ? (double)*(sqlite3_uint64 *)extra / 1e9 : 0);
      otlp_exporter_emit_metric(h->exporter, m);
      otlp_metric_free(m);
    }
  }
  return 0;
}
otlp_status_t gotel_sqlite_attach(gotel_host_t *h, sqlite3 *db, const char *prefix, const gotel_query_t *queries,
                                  size_t count, gotel_sqlite_t **out) {
  if (!out)
    return OTLP_ERR_NULL;
  *out = NULL;
  if (!h || !db || !prefix || !*prefix || (!queries && count))
    return OTLP_ERR_INVALID_ARGUMENT;
  for (size_t i = 0; i < count; ++i) {
    if (!queries[i].name || !*queries[i].name || !queries[i].sql)
      return OTLP_ERR_INVALID_ARGUMENT;
    for (size_t j = 0; j < i; ++j)
      if (!strcmp(queries[i].sql, queries[j].sql))
        return OTLP_ERR_INVALID_ARGUMENT;
  }
  gotel_sqlite_t *a = calloc(1, sizeof(*a));
  if (!a)
    return OTLP_ERR_NOMEM;
  *a = (gotel_sqlite_t){h, db, prefix, queries, count};
  if (sqlite3_trace_v2(db, SQLITE_TRACE_PROFILE | SQLITE_TRACE_CLOSE, traced, a) != SQLITE_OK) {
    free(a);
    return OTLP_ERR_INVALID_ARGUMENT;
  }
  *out = a;
  return OTLP_OK;
}
void gotel_sqlite_detach(gotel_sqlite_t *a) {
  if (!a)
    return;
  sqlite3_trace_v2(a->db, 0, NULL, NULL);
  free(a);
}
