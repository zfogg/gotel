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
  gotel_measurement_t *measurements;
  gotel_instrument_t *instruments;
  size_t instrument_count;
  atomic_uint_fast64_t sequence, rate_window;
  uint64_t last_metrics_ns, epoch_ns;
  uint64_t watchdog_poll_ns, watchdog_report_ns;
};
void gotel_emit_span(gotel_host_t *host, otlp_span_t *span);
otlp_status_t gotel_resource_detect(gotel_host_t *host);
otlp_status_t gotel_signal_headers(gotel_host_t *host);
otlp_status_t gotel_protocol_configure(gotel_host_t *host);
