#include <gotel/testing.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define CHECK(x)                                                                                                       \
  do {                                                                                                                 \
    if (!(x)) {                                                                                                        \
      fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #x);                                                          \
      exit(1);                                                                                                         \
    }                                                                                                                  \
  } while (0)
static gotel_span_snapshot_t blocked;
static bool snapshot(void *context, gotel_watchdog_snapshot_t *out) {
  (void)context;
  out->blocked_threads = 2;
  out->wait_ns = 3000000000ULL;
  out->parent = blocked.context;
  out->parent_snapshot = blocked;
  return true;
}
int main(void) {
  static const gotel_operation_t ops[] = {{"test.root", false}, {"test.blocked", false}, {"test.deadlock", false}};
  static const gotel_metric_t metrics[] = {{"test.deadlocked.threads", "{thread}", OTLP_METRIC_GAUGE, NULL, 0}};
  gotel_options_t o = {0};
  o.exporter.service_name = "snapshot-test";
  o.signals = GOTEL_ALL;
  o.monotonic_ns = gotel_test_monotonic_ns;
  o.realtime_ns = gotel_test_realtime_ns;
  o.operations = ops;
  o.operation_count = 3;
  o.metrics = metrics;
  o.metric_count = 1;
  o.watchdog.snapshot = snapshot;
  o.watchdog.operation = 2;
  o.watchdog.metric = 0;
  gotel_host_t *h = NULL;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  gotel_scope_t root, parent;
  gotel_begin(h, &root, 0, NULL);
  gotel_begin(h, &parent, 1, NULL);
  blocked = gotel_current_span_snapshot();
  CHECK(!strcmp(blocked.name, "test.blocked"));
  CHECK(blocked.start_unix_ns == gotel_test_realtime_ns());
  CHECK(!memcmp(blocked.context.span_id, parent.context.span_id, 8));
  CHECK(!memcmp(blocked.parent_span_id, root.context.span_id, 8));
  otlp_span_t *live = parent.span;
  CHECK(gotel_test_clock_advance(3000000000ULL) == OTLP_OK);
  CHECK(gotel_tick(h) == OTLP_OK);
  CHECK(parent.active && parent.span == live);
  CHECK(!memcmp(gotel_current_span_snapshot().context.span_id, parent.context.span_id, 8));
  otlp_exporter_stats_t stats;
  CHECK(gotel_stats(h, &stats) == OTLP_OK && stats.emitted == 2);
  CHECK(gotel_test_clock_advance(1000000000ULL) == OTLP_OK);
  CHECK(gotel_tick(h) == OTLP_OK);
  CHECK(gotel_stats(h, &stats) == OTLP_OK && stats.emitted == 2);
  // A resumed owner may still mutate and finish the real span exactly once.
  gotel_attr_string(&parent, "resumed", "true");
  gotel_end(&parent);
  CHECK(!parent.active && !parent.span);
  gotel_end(&parent);
  gotel_end(&root);
  CHECK(gotel_stats(h, &stats) == OTLP_OK && stats.emitted == 4);
  CHECK(gotel_publish_span_snapshot(NULL, &blocked) == OTLP_ERR_NULL);
  CHECK(gotel_publish_span_snapshot(h, NULL) == OTLP_ERR_NULL);
  blocked.context.sampled = false;
  CHECK(gotel_publish_span_snapshot(h, &blocked) == OTLP_OK);
  CHECK(gotel_stats(h, &stats) == OTLP_OK && stats.emitted == 4);
  gotel_destroy(h);
  return 0;
}
