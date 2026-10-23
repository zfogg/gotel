#include <gotel/testing.h>
#include <stdlib.h>
#include <string.h>
static const gotel_operation_t operations[] = {{"test.resource", false}};
int main(int argc, char **argv) {
  if (argc != 3)
    return 2;
  gotel_options_t o = {0};
  o.exporter.service_name = "resource-test";
  o.exporter.endpoint = argv[1];
  o.exporter.flush_timeout_ms = 1000;
  o.exporter.batch_ms = 1;
  o.exporter.queue_capacity = 64;
  o.signals = GOTEL_ALL;
  o.operations = operations;
  o.operation_count = 1;
  o.realtime_sample_rate = 1;
  o.monotonic_ns = gotel_test_monotonic_ns;
  o.realtime_ns = gotel_test_realtime_ns;
  gotel_test_clock_set(1000000000, 1700000000000000000ULL);
  char redacted[] = "[REDACTED]", host_name[] = "explicit-host";
  const char *arguments[] = {"demo.exe", "--token", redacted, "résumé", ""};
  otlp_resource_attr_t attrs[] = {{"host.name", {.type = OTLP_VALUE_STRING, .v.string_val = host_name}},
                                  {"process.pid", {.type = OTLP_VALUE_INT64, .v.int64_val = 42}},
                                  {"service.version", {.type = OTLP_VALUE_STRING, .v.string_val = "test-version"}},
                                  {"deployment.environment.name", {.type = OTLP_VALUE_STRING, .v.string_val = "test"}}};
  if (!strcmp(argv[2], "custom")) {
    o.argv = arguments;
    o.argc = 5;
    o.exporter.resource_attributes = attrs;
    o.exporter.n_resource_attributes = 4;
  }
  if (!strcmp(argv[2], "omit"))
    o.omit_command_args = true;
  if (!strcmp(argv[2], "disabled"))
    o.disable_resource_detection = true;
  if (!strcmp(argv[2], "invalid"))
    o.argc = -1;
  gotel_host_t *h = NULL;
  otlp_status_t status = gotel_create(&o, &h);
  if (!strcmp(argv[2], "invalid"))
    return status == OTLP_ERR_INVALID_ARGUMENT && h == NULL ? 0 : 3;
  if (status != OTLP_OK)
    return 3;
  memset(redacted, 'X', sizeof(redacted) - 1);
  memset(host_name, 'X', sizeof(host_name) - 1);
  gotel_scope_t s;
  gotel_begin(h, &s, 0, NULL);
  gotel_end(&s);
  gotel_log(h, OTLP_SEVERITY_INFO, "resource log", NULL, 0, NULL);
  status = gotel_drain(h);
  gotel_destroy(h);
  return status == OTLP_OK ? 0 : 4;
}
