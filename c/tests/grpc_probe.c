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
  if (argc != 3)
    return 2;
  static const gotel_operation_t operations[] = {{"test.grpc.export", false}};
  static const otlp_http_header_t headers[] = {{"authorization", "Bearer test-token"}};
  gotel_options_t o = {0};
  o.operations = operations;
  o.operation_count = 1;
  o.monotonic_ns = mono;
  o.realtime_ns = real;
  o.signals = GOTEL_ALL;
  o.protocol = "grpc";
  o.exporter.service_name = "gotel-grpc";
  o.exporter.endpoint = argv[1];
  o.exporter.http_headers = headers;
  o.exporter.n_http_headers = 1;
  o.tls_ca_file = *argv[2] ? argv[2] : NULL;
  o.exporter.connect_timeout_ms = 500;
  o.exporter.read_timeout_ms = 500;
  o.exporter.flush_timeout_ms = 3000;
  o.exporter.backoff_initial_ms = 5;
  o.exporter.backoff_max_ms = 100;
  o.exporter.max_retries = 1;
  gotel_env_storage_t storage;
  if (gotel_options_apply_env(&o, &storage, NULL) != OTLP_OK)
    return 3;
  gotel_host_t *host = NULL;
  otlp_status_t status = gotel_create(&o, &host);
  if (status != OTLP_OK) {
    printf("{\"create\":%d}\n", status);
    return 0;
  }
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
  printf("{\"create\":0,\"traces\":%llu,\"logs\":%llu,\"metrics\":%llu,\"rejected\":%llu,\"network\":%llu}\n",
         (unsigned long long)stats.sent, (unsigned long long)stats.sent_logs, (unsigned long long)stats.sent_metrics,
         (unsigned long long)stats.rejected_spans, (unsigned long long)stats.network_err);
  gotel_destroy(host);
  return 0;
}
