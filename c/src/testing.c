#include "internal.h"
#include <gotel/testing.h>
#include <stdlib.h>
static _Thread_local uint64_t mono = UINT64_C(1000000000);
static _Thread_local uint64_t real = UINT64_C(1700000000000000000);
void gotel_test_clock_set(uint64_t monotonic_ns, uint64_t realtime_ns) {
  mono = monotonic_ns;
  real = realtime_ns;
}
otlp_status_t gotel_test_clock_advance(uint64_t ns) {
  if (UINT64_MAX - mono < ns || UINT64_MAX - real < ns)
    return OTLP_ERR_OVERFLOW;
  mono += ns;
  real += ns;
  return OTLP_OK;
}
uint64_t gotel_test_monotonic_ns(void) { return mono; }
uint64_t gotel_test_realtime_ns(void) { return real; }
static int scripted_status(void *context) {
  gotel_test_script_t *script = context;
  return script->position < script->count ? script->statuses[script->position++] : script->fallback_status;
}
otlp_status_t gotel_test_transport(gotel_host_t *host, gotel_test_script_t *script) {
  if (!host || (script && ((!script->statuses && script->count) || script->position > script->count ||
                           script->fallback_status < 100 || script->fallback_status > 599)))
    return OTLP_ERR_INVALID_ARGUMENT;
  if (script)
    for (size_t i = 0; i < script->count; ++i)
      if (script->statuses[i] < 100 || script->statuses[i] > 599)
        return OTLP_ERR_INVALID_ARGUMENT;
  otlp_exporter_set_null_transport(host->exporter, true);
  otlp_exporter_set_null_transport_status_fn(host->exporter, script ? scripted_status : NULL, script);
  return OTLP_OK;
}
struct gotel_test_events {
  atomic_uint_fast64_t counts[6];
};
gotel_test_events_t *gotel_test_events_create(void) {
  gotel_test_events_t *events = calloc(1, sizeof(*events));
  if (events)
    for (size_t i = 0; i < 6; ++i)
      atomic_init(&events->counts[i], 0);
  return events;
}
void gotel_test_events_destroy(gotel_test_events_t *events) { free(events); }
void gotel_test_event_record(void *context, const otlp_event_t *event) {
  gotel_test_events_t *events = context;
  if (events && event && event->code >= OTLP_EVT_QUEUE_FULL && event->code <= OTLP_EVT_SYNC_FLUSH_FAILED)
    atomic_fetch_add_explicit(&events->counts[event->code - 1], 1, memory_order_relaxed);
}
gotel_test_event_counts_t gotel_test_events_snapshot(const gotel_test_events_t *events) {
  if (!events)
    return (gotel_test_event_counts_t){0};
  return (gotel_test_event_counts_t){atomic_load(&events->counts[0]), atomic_load(&events->counts[1]),
                                     atomic_load(&events->counts[2]), atomic_load(&events->counts[3]),
                                     atomic_load(&events->counts[4]), atomic_load(&events->counts[5])};
}

otlp_status_t gotel_test_create(const gotel_options_t *options, gotel_host_t **output) {
  otlp_status_t result = gotel_create(options, output);
  if (result != OTLP_OK)
    return result;
  return gotel_test_transport(*output, NULL);
}
