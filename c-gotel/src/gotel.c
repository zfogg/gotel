#include "internal.h"
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static _Thread_local gotel_scope_t *current;
static bool power_of_two(size_t n) { return n && !(n & (n - 1)); }
otlp_status_t gotel_parse_signals(const char *text, unsigned *signals) {
  if (!text || !signals)
    return OTLP_ERR_NULL;
  unsigned value = 0;
  if (!strcmp(text, "none")) {
    *signals = 0;
    return OTLP_OK;
  }
  while (*text) {
    size_t n = strcspn(text, ",");
    unsigned bit = n == 6 && !memcmp(text, "traces", 6)    ? GOTEL_TRACES
                   : n == 4 && !memcmp(text, "logs", 4)    ? GOTEL_LOGS
                   : n == 7 && !memcmp(text, "metrics", 7) ? GOTEL_METRICS
                                                           : 0;
    if (!bit || (value & bit))
      return OTLP_ERR_INVALID_ARGUMENT;
    value |= bit;
    text += n;
    if (*text && !*++text)
      return OTLP_ERR_INVALID_ARGUMENT;
  }
  if (!value)
    return OTLP_ERR_INVALID_ARGUMENT;
  *signals = value;
  return OTLP_OK;
}
otlp_status_t gotel_create(const gotel_options_t *o, gotel_host_t **out) {
  if (!out)
    return OTLP_ERR_NULL;
  *out = NULL;
  if (!o || !o->monotonic_ns || !o->realtime_ns || !o->operations || !o->operation_count ||
      !isfinite(o->realtime_sample_rate) || o->realtime_sample_rate < 0 || o->realtime_sample_rate > 1 ||
      (o->signals & ~GOTEL_ALL) || (o->exporter.queue_capacity && !power_of_two(o->exporter.queue_capacity)))
    return OTLP_ERR_INVALID_ARGUMENT;
  for (size_t i = 0; i < o->operation_count; ++i)
    if (!o->operations[i].name || !*o->operations[i].name || strlen(o->operations[i].name) > 220)
      return OTLP_ERR_INVALID_ARGUMENT;
  if ((!o->metrics && o->metric_count) || o->metric_count > SIZE_MAX - o->operation_count)
    return OTLP_ERR_INVALID_ARGUMENT;
  for (size_t i = 0; i < o->metric_count; ++i) {
    const gotel_metric_t *m = &o->metrics[i];
    if (!m->name || !*m->name || strlen(m->name) > 220 || m->type < OTLP_METRIC_COUNTER ||
        m->type > OTLP_METRIC_HISTOGRAM || (!m->bounds && m->bound_count))
      return OTLP_ERR_INVALID_ARGUMENT;
    for (size_t j = 0; j < m->bound_count; ++j)
      if (!isfinite(m->bounds[j]) || (j && m->bounds[j] <= m->bounds[j - 1]))
        return OTLP_ERR_INVALID_ARGUMENT;
    for (size_t j = 0; j < i; ++j)
      if (!strcmp(m->name, o->metrics[j].name))
        return OTLP_ERR_INVALID_ARGUMENT;
  }
  if (o->watchdog.snapshot &&
      (o->watchdog.operation >= o->operation_count || o->watchdog.metric >= o->metric_count ||
       o->operations[o->watchdog.operation].realtime || o->metrics[o->watchdog.metric].type != OTLP_METRIC_GAUGE))
    return OTLP_ERR_INVALID_ARGUMENT;
  gotel_host_t *h = calloc(1, sizeof(*h));
  if (!h)
    return OTLP_ERR_NOMEM;
  h->options = *o;
  /* Build roots once per host, before requests can borrow them. HTTP-only hosts
   * may run on minimal systems without a CA store. Explicit invalid bundles fail. */
  otlp_status_t tls_status = gotel_tls_store_create(o->tls_ca_file, &h->tls);
  if (tls_status != OTLP_OK && o->tls_ca_file && *o->tls_ca_file) {
    free(h);
    return tls_status;
  }
  h->measurements = calloc(o->operation_count, sizeof(*h->measurements));
  h->exporter = otlp_exporter_create(&o->exporter);
  h->tracer = otlp_tracer_create(o->exporter.service_name, "c-gotel", CGOTEL_VERSION);
  if (!h->measurements || !h->exporter || !h->tracer) {
    gotel_destroy(h);
    return OTLP_ERR_NOMEM;
  }
  otlp_status_t header_status = gotel_signal_headers(h);
  if (header_status != OTLP_OK) {
    gotel_destroy(h);
    return header_status;
  }
  otlp_status_t resource_status = gotel_resource_detect(h);
  if (resource_status != OTLP_OK) {
    gotel_destroy(h);
    return resource_status;
  }
  h->instrument_count = o->operation_count + o->metric_count;
  h->instruments = calloc(h->instrument_count, sizeof(*h->instruments));
  if (!h->instruments) {
    gotel_destroy(h);
    return OTLP_ERR_NOMEM;
  }
  static const double seconds[] = {.0001, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10};
  for (size_t i = 0; i < h->instrument_count; ++i) {
    gotel_instrument_t *instrument = &h->instruments[i];
    atomic_flag_clear(&instrument->lock);
    if (i < o->operation_count) {
      char name[256];
      snprintf(name, sizeof(name), "%s.duration", o->operations[i].name);
      instrument->value =
          otlp_metric_create(OTLP_METRIC_HISTOGRAM, name, "s", NULL, seconds, sizeof(seconds) / sizeof(seconds[0]));
    } else {
      const gotel_metric_t *m = &o->metrics[i - o->operation_count];
      instrument->value = otlp_metric_create(m->type, m->name, m->unit, NULL, m->bounds, m->bound_count);
    }
    if (!instrument->value) {
      gotel_destroy(h);
      return OTLP_ERR_NOMEM;
    }
    otlp_metric_set_start_time(instrument->value, o->realtime_ns());
  }
  otlp_exporter_set_event_logger(h->exporter, o->exporter_event, o->exporter_event_context);
  atomic_init(&h->sequence, 0);
  atomic_init(&h->rate_window, 0);
  for (size_t i = 0; i < o->operation_count; ++i) {
    atomic_init(&h->measurements[i].count, 0);
    atomic_init(&h->measurements[i].errors, 0);
    atomic_init(&h->measurements[i].total_ns, 0);
  }
  memset(&h->options.exporter, 0, sizeof(h->options.exporter));
  h->epoch_ns = o->realtime_ns();
  h->last_metrics_ns = o->monotonic_ns();
  *out = h;
  return OTLP_OK;
}
void gotel_destroy(gotel_host_t *h) {
  if (!h)
    return;
  otlp_tracer_free(h->tracer);
  otlp_exporter_free(h->exporter);
  free(h->signal_env);
  if (h->instruments) {
    for (size_t i = 0; i < h->instrument_count; ++i)
      otlp_metric_free(h->instruments[i].value);
    free(h->instruments);
  }
  gotel_tls_store_free(h->tls);
  free(h->measurements);
  free(h);
}
static otlp_status_t record_instrument(gotel_host_t *h, size_t i, double value) {
  gotel_instrument_t *m = &h->instruments[i];
  while (atomic_flag_test_and_set_explicit(&m->lock, memory_order_acquire)) {
  }
  otlp_status_t result = otlp_metric_record(m->value, value);
  if (result == OTLP_OK)
    m->recorded = true;
  atomic_flag_clear_explicit(&m->lock, memory_order_release);
  return result;
}
otlp_status_t gotel_metric_record(gotel_host_t *h, size_t metric, double value) {
  if (!h || metric >= h->options.metric_count || !isfinite(value) ||
      (value < 0 && h->options.metrics[metric].type == OTLP_METRIC_COUNTER))
    return OTLP_ERR_INVALID_ARGUMENT;
  if (!(h->options.signals & GOTEL_METRICS))
    return OTLP_OK;
  return record_instrument(h, h->options.operation_count + metric, value);
}
static bool sample(gotel_host_t *h, uint64_t now) {
  uint64_t x = atomic_fetch_add(&h->sequence, 1) + UINT64_C(0x9e3779b97f4a7c15);
  x = (x ^ (x >> 30)) * UINT64_C(0xbf58476d1ce4e5b9);
  x = (x ^ (x >> 27)) * UINT64_C(0x94d049bb133111eb);
  x ^= x >> 31;
  if ((double)(x >> 11) / 9007199254740992.0 >= h->options.realtime_sample_rate)
    return false;
  uint64_t second = (now / UINT64_C(1000000000)) & UINT64_C(0xffffffff);
  uint64_t old = atomic_load(&h->rate_window);
  for (;;) {
    uint64_t used = (old >> 32) == second ? (old & UINT64_C(0xffffffff)) : 0;
    if (h->options.realtime_max_per_second && used >= h->options.realtime_max_per_second)
      return false;
    uint64_t next = (second << 32) | (used + 1);
    if (atomic_compare_exchange_weak(&h->rate_window, &old, next))
      return true;
  }
}
gotel_context_t gotel_current_context(void) { return current ? current->context : (gotel_context_t){0}; }
void gotel_begin(gotel_host_t *h, gotel_scope_t *s, size_t op, const gotel_context_t *explicit_parent) {
  memset(s, 0, sizeof(*s));
  if (!h || op >= h->options.operation_count)
    return;
  s->host = h;
  s->operation = op;
  s->active = true;
  s->started_ns = h->options.monotonic_ns();
  s->previous = current;
  s->attached = true;
  gotel_context_t parent = explicit_parent ? *explicit_parent : gotel_current_context();
  bool realtime = h->options.operations[op].realtime;
  bool inherited = parent.valid && (!realtime || parent.realtime);
  bool sampled = inherited ? parent.sampled : (!realtime || sample(h, s->started_ns));
  s->context.valid = true;
  s->context.sampled = sampled;
  s->context.realtime = realtime || (inherited && parent.realtime);
  if (sampled && (h->options.signals & GOTEL_TRACES)) {
    s->span = otlp_tracer_start_span(h->tracer, h->options.operations[op].name);
    if (s->span) {
      if (inherited) {
        otlp_span_set_trace_id(s->span, parent.trace_id);
        otlp_span_set_parent_span_id(s->span, parent.span_id);
      }
      otlp_context_t c = otlp_context_from_span(s->span);
      memcpy(s->context.trace_id, c.trace_id, 16);
      memcpy(s->context.span_id, c.span_id, 8);
    }
  }
  current = s;
}
void gotel_emit_span(gotel_host_t *h, otlp_span_t *span) {
  if (!span)
    return;
  otlp_span_mark_end(span);
  otlp_exporter_emit(h->exporter, span);
  otlp_span_free(span);
}
void gotel_end(gotel_scope_t *s) {
  if (!s || !s->active)
    return;
  gotel_host_t *h = s->host;
  uint64_t elapsed = h->options.monotonic_ns() - s->started_ns;
  if (h->options.signals & GOTEL_METRICS) {
    gotel_measurement_t *m = &h->measurements[s->operation];
    record_instrument(h, s->operation, (double)elapsed / 1e9);
    atomic_fetch_add(&m->count, 1);
    atomic_fetch_add(&m->total_ns, elapsed);
    if (s->failed)
      atomic_fetch_add(&m->errors, 1);
  }
  if (s->attached && current == s)
    current = s->previous;
  gotel_emit_span(h, s->span);
  s->span = NULL;
  s->active = false;
}
void gotel_current_error(const char *type, const char *message) { gotel_error(current, type, message); }
void gotel_error(gotel_scope_t *s, const char *type, const char *message) {
  if (!s || !s->active)
    return;
  s->failed = true;
  if (!s->span)
    return;
  otlp_span_set_status(s->span, OTLP_STATUS_CODE_ERROR, message);
  otlp_span_set_attribute_string(s->span, "error.type", type ? type : "unknown");
}
void gotel_attr_string(gotel_scope_t *s, const char *k, const char *v) {
  if (s && s->span && k && v)
    otlp_span_set_attribute_string(s->span, k, v);
}
void gotel_attr_int(gotel_scope_t *s, const char *k, int64_t v) {
  if (s && s->span && k)
    otlp_span_set_attribute_int(s->span, k, v);
}
void gotel_log(gotel_host_t *h, otlp_severity_t severity, const char *body, const char *file, int line,
               const char *function) {
  if (!h || !(h->options.signals & GOTEL_LOGS) || !body)
    return;
  otlp_log_record_t *r = otlp_log_record_create(severity, body);
  if (!r)
    return;
  static const char *const severity_names[] = {"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"};
  if (severity >= OTLP_SEVERITY_TRACE && severity <= OTLP_SEVERITY_FATAL4)
    otlp_log_record_set_severity_text(r, severity_names[((unsigned)severity - 1) / 4]);
  otlp_log_record_mark_timestamp(r);
  gotel_context_t c = gotel_current_context();
  if (c.valid && c.sampled) {
    otlp_log_record_set_trace_id(r, c.trace_id);
    otlp_log_record_set_span_id(r, c.span_id);
  }
  if (file)
    otlp_log_record_set_attribute_string(r, "code.file.path", file);
  if (function)
    otlp_log_record_set_attribute_string(r, "code.function.name", function);
  otlp_log_record_set_attribute_int(r, "code.line.number", line);
  otlp_exporter_emit_log(h->exporter, r);
  otlp_log_record_free(r);
}
static void export_metrics(gotel_host_t *h) {
  if (!(h->options.signals & GOTEL_METRICS))
    return;
  for (size_t i = 0; i < h->instrument_count; ++i) {
    gotel_instrument_t *m = &h->instruments[i];
    while (atomic_flag_test_and_set_explicit(&m->lock, memory_order_acquire)) {
    }
    if (m->recorded) {
      otlp_metric_set_time(m->value, h->options.realtime_ns());
      otlp_exporter_emit_metric(h->exporter, m->value);
    }
    atomic_flag_clear_explicit(&m->lock, memory_order_release);
  }
  for (size_t i = 0; i < h->options.operation_count; ++i) {
    gotel_measurement_t *m = &h->measurements[i];
    uint64_t count = atomic_load(&m->count);
    if (!count)
      continue;
    const char *operation = h->options.operations[i].name;
    char count_name[256], error_name[256], duration_name[256];
    snprintf(count_name, sizeof(count_name), "%s.count", operation);
    snprintf(error_name, sizeof(error_name), "%s.errors", operation);
    snprintf(duration_name, sizeof(duration_name), "%s.duration.sum", operation);
    otlp_metric_t *c = otlp_metric_create(OTLP_METRIC_COUNTER, count_name, "{operation}", NULL, NULL, 0);
    otlp_metric_t *e = otlp_metric_create(OTLP_METRIC_COUNTER, error_name, "{error}", NULL, NULL, 0);
    otlp_metric_t *d = otlp_metric_create(OTLP_METRIC_COUNTER, duration_name, "s", NULL, NULL, 0);
    otlp_metric_t *items[] = {c, e, d};
    double values[] = {(double)count, (double)atomic_load(&m->errors), (double)atomic_load(&m->total_ns) / 1e9};
    for (size_t j = 0; j < 3; ++j)
      if (items[j]) {
        otlp_metric_set_attribute_string(items[j], "operation.name", operation);
        otlp_metric_set_start_time(items[j], h->epoch_ns);
        otlp_metric_mark_time(items[j]);
        otlp_metric_record(items[j], values[j]);
        otlp_exporter_emit_metric(h->exporter, items[j]);
        otlp_metric_free(items[j]);
      }
  }
}
static void watchdog_poll(gotel_host_t *h, uint64_t now) {
  const gotel_watchdog_options_t *w = &h->options.watchdog;
  if (!w->snapshot || (h->watchdog_poll_ns && now - h->watchdog_poll_ns < 1000000000ULL))
    return;
  h->watchdog_poll_ns = now;
  gotel_watchdog_snapshot_t snapshot = {0};
  if (!w->snapshot(w->context, &snapshot))
    return;
  size_t blocked = snapshot.wait_ns >= 2000000000ULL ? snapshot.blocked_threads : 0;
  (void)gotel_metric_record(h, w->metric, (double)blocked);
  if (!blocked || (h->watchdog_report_ns && now - h->watchdog_report_ns < 30000000000ULL))
    return;
  h->watchdog_report_ns = now;
  gotel_scope_t scope = {0};
  gotel_begin(h, &scope, w->operation, &snapshot.parent);
  gotel_attr_int(&scope, "thread.deadlock.count", (int64_t)blocked);
  gotel_attr_int(&scope, "thread.deadlock.wait_ns", (int64_t)snapshot.wait_ns);
  gotel_attr_int(&scope, "thread.deadlock.snapshot_limited", snapshot.limited);
  gotel_attr_string(&scope, "thread.deadlock.detector", "mutex_wait_cycle");
  if (w->decorate)
    w->decorate(w->context, &scope);
  gotel_error(&scope, "thread.deadlock", "Mutex wait cycle persisted for at least two seconds");
  gotel_end(&scope);
}

otlp_status_t gotel_tick(gotel_host_t *h) {
  if (!h)
    return OTLP_ERR_NULL;
  uint64_t now = h->options.monotonic_ns();
  watchdog_poll(h, now);
  uint64_t interval = (uint64_t)(h->options.metrics_interval_ms ? h->options.metrics_interval_ms : 10000) * 1000000;
  if (now - h->last_metrics_ns >= interval) {
    export_metrics(h);
    h->last_metrics_ns = now;
  }
  gotel_tls_store *previous = gotel_tls_select(h->tls);
  otlp_status_t result = otlp_exporter_tick(h->exporter, 0);
  gotel_tls_select(previous);
  return result;
}
otlp_status_t gotel_drain(gotel_host_t *h) {
  if (!h)
    return OTLP_ERR_NULL;
  export_metrics(h);
  gotel_tls_store *previous = gotel_tls_select(h->tls);
  otlp_status_t result = otlp_exporter_flush(h->exporter);
  gotel_tls_select(previous);
  return result;
}
otlp_status_t gotel_stats(gotel_host_t *h, otlp_exporter_stats_t *out) {
  return h ? otlp_exporter_get_stats(h->exporter, out) : OTLP_ERR_NULL;
}

void gotel_set_logger(gotel_host_t *h, otlp_log_fn logger, void *context) {
  if (h)
    otlp_exporter_set_logger(h->exporter, logger, context);
}
