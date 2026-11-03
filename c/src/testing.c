#include "exporter_internal.h"
#include "internal.h"
#include "otlp_messages.h"
#include <gotel/testing.h>
#include <stdlib.h>
#include <string.h>
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

struct gotel_test_capture {
  atomic_flag lock;
  uint8_t *data;
  size_t bytes, capacity;
  otlp_status_t status;
};

gotel_test_capture_t *gotel_test_capture_create(size_t max_bytes) {
  if (!max_bytes)
    return NULL;
  gotel_test_capture_t *capture = calloc(1, sizeof(*capture));
  if (!capture)
    return NULL;
  capture->data = malloc(max_bytes);
  if (!capture->data) {
    free(capture);
    return NULL;
  }
  capture->capacity = max_bytes;
  atomic_flag_clear(&capture->lock);
  return capture;
}

static void capture_span(void *context, gotel_host_t *host, const otlp_span_t *span) {
  gotel_test_capture_t *capture = context;
  while (atomic_flag_test_and_set_explicit(&capture->lock, memory_order_acquire)) {
  }
  if (capture->status == OTLP_OK) {
    struct otlp_pb_buf encoded;
    capture->status = otlp_pb_buf_init(&encoded, 0);
    if (capture->status == OTLP_OK) {
      size_t resource_count = 0;
      const struct otlp_attribute *resource = otlp_exporter_get_resource_attrs(host->exporter, &resource_count);
      capture->status =
          otlp_encode_export_trace_service_request(&encoded, host->exporter->service_name, host->exporter->schema_url,
                                                   resource, resource_count, "gotel", NULL, &span, 1);
      if (capture->status == OTLP_OK) {
        if (encoded.len > capture->capacity - capture->bytes)
          capture->status = OTLP_ERR_OVERFLOW;
        else {
          // Each encoded request contains only repeated ResourceSpans fields;
          // concatenation is one valid protobuf request, including mixed hosts.
          memcpy(capture->data + capture->bytes, encoded.data, encoded.len);
          capture->bytes += encoded.len;
        }
      }
      otlp_pb_buf_free(&encoded);
    }
  }
  atomic_flag_clear_explicit(&capture->lock, memory_order_release);
}

otlp_status_t gotel_test_capture_attach(gotel_host_t *host, gotel_test_capture_t *capture) {
  if (!host || !capture)
    return OTLP_ERR_NULL;
  if (host->span_observer)
    return OTLP_ERR_INVALID_ARGUMENT;
  otlp_status_t result = gotel_test_transport(host, NULL);
  if (result != OTLP_OK)
    return result;
  host->span_observer_context = capture;
  host->span_observer = capture_span;
  return OTLP_OK;
}

void gotel_test_capture_detach(gotel_host_t *host) {
  if (host) {
    host->span_observer = NULL;
    host->span_observer_context = NULL;
  }
}

otlp_status_t gotel_test_capture_read(const gotel_test_capture_t *capture, const uint8_t **data, size_t *bytes) {
  if (data)
    *data = NULL;
  if (bytes)
    *bytes = 0;
  if (!capture || !data || !bytes)
    return OTLP_ERR_NULL;
  if (capture->status != OTLP_OK)
    return capture->status;
  *data = capture->bytes ? capture->data : NULL;
  *bytes = capture->bytes;
  return OTLP_OK;
}

void gotel_test_capture_destroy(gotel_test_capture_t *capture) {
  if (capture) {
    free(capture->data);
    free(capture);
  }
}
