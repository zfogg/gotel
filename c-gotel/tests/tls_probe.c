#include "platform.h"
#include <gotel/gotel.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static uint64_t mono(void) {
  uint64_t n = 0;
  otlp_platform_now_mono_nano(&n);
  return n;
}
static uint64_t real(void) {
  uint64_t n = 0;
  otlp_platform_now_unix_nano(&n);
  return n;
}
int main(int argc, char **argv) {
  if (argc != 4)
    return 2;
  static const gotel_operation_t operations[] = {{"test.tls.export", false}};
  gotel_options_t o = {0};
  o.operations = operations;
  o.operation_count = 1;
  o.monotonic_ns = mono;
  o.realtime_ns = real;
  o.signals = GOTEL_ALL;
  o.exporter.service_name = "c-gotel-tls";
  o.exporter.endpoint = argv[1];
  o.tls_ca_file = *argv[2] ? argv[2] : NULL;
  o.exporter.connect_timeout_ms = 1000;
  o.exporter.read_timeout_ms = 1000;
  o.exporter.flush_timeout_ms = 5000;
  o.exporter.backoff_initial_ms = 5;
  o.exporter.backoff_max_ms = 10;
  o.exporter.max_retries = 1;
  gotel_host_t *host = NULL;
  otlp_status_t rc = gotel_create(&o, &host);
  int success = atoi(argv[3]);
  if (rc != OTLP_OK)
    return success ? 3 : 0;
  gotel_scope_t scope;
  gotel_begin(host, &scope, 0, NULL);
  char *large = malloc(70001);
  if (!large)
    return 5;
  memset(large, 'x', 70000);
  large[70000] = 0;
  gotel_log(host, OTLP_SEVERITY_INFO, large);
  free(large);
  gotel_end(&scope);
  (void)gotel_drain(host);
  otlp_exporter_stats_t stats = {0};
  gotel_stats(host, &stats);
  int passed = success ? (stats.sent == 1 && stats.sent_logs == 1 && stats.sent_metrics == 4)
                       : (stats.sent == 0 && stats.sent_logs == 0 && stats.sent_metrics == 0 && stats.network_err > 0);
  if (!passed)
    fprintf(stderr, "sent=%llu logs=%llu metrics=%llu network_err=%llu\n", (unsigned long long)stats.sent,
            (unsigned long long)stats.sent_logs, (unsigned long long)stats.sent_metrics,
            (unsigned long long)stats.network_err);
  gotel_destroy(host);
  return passed ? 0 : 4;
}
