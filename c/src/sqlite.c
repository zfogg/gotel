#include "internal.h"
#include <gotel/sql.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
typedef struct statement_start {
  void *statement;
  uint64_t monotonic_ns;
  gotel_context_t parent;
  struct statement_start *next;
} statement_start_t;
struct gotel_sqlite {
  gotel_host_t *host;
  sqlite3 *db;
  const char *prefix;
  const gotel_query_t *queries;
  size_t count;
  gotel_sqlite_options_t options;
  statement_start_t *started;
};
static void clear_started(gotel_sqlite_t *a) {
  while (a->started) {
    statement_start_t *entry = a->started;
    a->started = entry->next;
    free(entry);
  }
}
static int traced(unsigned event, void *data, void *statement, void *extra) {
  gotel_sqlite_t *a = data;
  gotel_host_t *h = a->host;
  if (event == SQLITE_TRACE_CLOSE) {
    clear_started(a);
    free(a);
    return 0;
  }
  if (event == SQLITE_TRACE_STMT) {
    if (!(h->options.signals & (GOTEL_TRACES | GOTEL_METRICS)))
      return 0;
    // Trigger subprograms reuse the statement; retain the outer start.
    for (statement_start_t *entry = a->started; entry; entry = entry->next)
      if (entry->statement == statement)
        return 0;
    statement_start_t *entry = malloc(sizeof(*entry));
    if (entry) {
      *entry = (statement_start_t){statement, h->options.monotonic_ns(), gotel_current_context(), a->started};
      a->started = entry;
    }
    return 0;
  }
  if (event != SQLITE_TRACE_PROFILE)
    return 0;
  uint64_t finished_ns = h->options.monotonic_ns();
  uint64_t end = h->options.realtime_ns();
  uint64_t duration = extra ? *(sqlite3_uint64 *)extra : 0;
  gotel_context_t parent = gotel_current_context();
  statement_start_t **link = &a->started;
  while (*link && (*link)->statement != statement)
    link = &(*link)->next;
  if (*link) {
    statement_start_t *entry = *link;
    if (finished_ns >= entry->monotonic_ns)
      duration = finished_ns - entry->monotonic_ns;
    parent = entry->parent;
    *link = entry->next;
    free(entry);
  }
  const char *sql = sqlite3_sql(statement);
  const char *name = "UnknownQuery";
  const gotel_query_t *query = gotel_query_find(a->queries, a->count, sql);
  if (query)
    name = query->name;
  sequel_metadata_t metadata;
  sequel_inspect(sql, &a->options.sql, &metadata);
  char span_name[256];
  int n = snprintf(span_name, sizeof(span_name), "%s.%s", a->prefix, name);
  if (n < 0 || (size_t)n >= sizeof(span_name))
    return 0;
  otlp_span_t *span = NULL;
  if ((h->options.signals & GOTEL_TRACES) && !(parent.valid && !parent.sampled))
    span = otlp_tracer_start_span(h->tracer, gotel_span_name(h, span_name));
  if (span) {
    otlp_span_set_kind(span, OTLP_SPAN_KIND_CLIENT);
    gotel_sql_attributes_with_limit(span, &metadata, "sqlite", a->options.namespace_name,
                                    (size_t)sqlite3_bind_parameter_count(statement),
                                    a->options.sql.max_query_text_bytes);
    if (parent.valid && parent.sampled) {
      otlp_span_set_trace_id(span, parent.trace_id);
      otlp_span_set_parent_span_id(span, parent.span_id);
    }
    /* PROFILE's supplied nanoseconds have millisecond resolution. Time STMT to
     * PROFILE with the caller's monotonic clock, including reset/finalize. */
    otlp_span_mark_end(span);
    otlp_span_set_end_time(span, end);
    otlp_span_set_start_time(span, end >= duration ? end - duration : 0);
    otlp_exporter_emit(h->exporter, span);
    otlp_span_free(span);
  }
  if (h->options.signals & GOTEL_METRICS) {
    static const double bounds[] = {.0001, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10};
    otlp_metric_t *m = otlp_metric_create(OTLP_METRIC_HISTOGRAM, "db.client.operation.duration", "s", NULL, bounds,
                                          sizeof(bounds) / sizeof(bounds[0]));
    if (m) {
      otlp_metric_set_attribute_string(m, "db.system.name", "sqlite");
      otlp_metric_set_attribute_string(m, "db.query.summary", metadata.summary);
      otlp_metric_set_attribute_string(m, "gotel.db.query.name", span_name);
      otlp_metric_set_aggregation_temporality(m, OTLP_AGG_TEMP_DELTA);
      otlp_metric_set_start_time(m, end >= duration ? end - duration : 0);
      otlp_metric_set_time(m, end);
      otlp_metric_record(m, (double)duration / 1e9);
      otlp_exporter_emit_metric(h->exporter, m);
      otlp_metric_free(m);
    }
  }
  return 0;
}
otlp_status_t gotel_sqlite_attach(gotel_host_t *h, sqlite3 *db, const char *prefix, const gotel_query_t *queries,
                                  size_t count, gotel_sqlite_t **out) {
  return gotel_sqlite_attach_with_options(h, db, prefix, queries, count, NULL, out);
}
otlp_status_t gotel_sqlite_attach_with_options(gotel_host_t *h, sqlite3 *db, const char *prefix,
                                               const gotel_query_t *queries, size_t count,
                                               const gotel_sqlite_options_t *options, gotel_sqlite_t **out) {
  if (!out)
    return OTLP_ERR_NULL;
  *out = NULL;
  if (!h || !db || !prefix || !*prefix || (!queries && count))
    return OTLP_ERR_INVALID_ARGUMENT;
  if (options && options->sql.max_query_text_bytes > SEQUEL_TEXT_MAX)
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
  if (options)
    a->options = *options;
  a->options.sql.dialect = SEQUEL_SQLITE;
  if (sqlite3_trace_v2(db, SQLITE_TRACE_STMT | SQLITE_TRACE_PROFILE | SQLITE_TRACE_CLOSE, traced, a) != SQLITE_OK) {
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
  clear_started(a);
  free(a);
}
