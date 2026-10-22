#include <gotel/gotel.h>
#include <stdatomic.h>
typedef struct {
  atomic_uint_fast64_t count, errors, total_ns;
} gotel_measurement_t;
struct gotel_host {
  gotel_options_t options;
  otlp_exporter_t *exporter;
  otlp_tracer_t *tracer;
  gotel_measurement_t *measurements;
  atomic_uint_fast64_t sequence, rate_window;
  uint64_t last_metrics_ns, epoch_ns;
};
void gotel_emit_span(gotel_host_t *host, otlp_span_t *span);
