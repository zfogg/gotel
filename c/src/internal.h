#pragma once

#include "tls.h"
#include <gotel/gotel.h>
#include <stdatomic.h>
typedef struct {
  atomic_flag lock;
  otlp_metric_t *value;
  bool recorded;
} gotel_instrument_t;

typedef struct {
  atomic_uint_fast64_t count, errors, total_ns;
} gotel_measurement_t;

struct gotel_host {
  gotel_options_t options;
  gotel_tls_store *tls;
  otlp_env_storage_t *signal_env;
  otlp_exporter_t *exporter;
  otlp_tracer_t *tracer;
  char application_name[224];
  gotel_measurement_t *measurements;
  gotel_instrument_t *instruments;
  size_t instrument_count;
  atomic_uint_fast64_t sequence, rate_window, retained_window;
  uint64_t last_metrics_ns, epoch_ns;
  uint64_t watchdog_poll_ns, watchdog_report_ns;
  void (*span_observer)(void *context, gotel_host_t *host, const otlp_span_t *span);
  void *span_observer_context;
};

/** Submit a completed span to the host exporter. */
void gotel_emit_span(gotel_host_t *host, otlp_span_t *span);

/** Observe, submit and free a span whose end timestamp is already set. */
otlp_status_t gotel_submit_span(gotel_host_t *host, otlp_span_t *span);

/** Populate the host resource with detected process and system metadata. */
otlp_status_t gotel_resource_detect(gotel_host_t *host);

/** Apply signal-specific exporter headers. */
otlp_status_t gotel_signal_headers(gotel_host_t *host);

/** Configure the selected exporter transport protocol. */
otlp_status_t gotel_protocol_configure(gotel_host_t *host);

/** Apply the configured application prefix to a span name. */
const char *gotel_span_name(gotel_host_t *host, const char *name);
