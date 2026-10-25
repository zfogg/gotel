#include "exporter_internal.h"
#include "internal.h"
#include "span_internal.h"
#include <gotel/testing.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define CHECK(c)                                                                                                       \
  do {                                                                                                                 \
    if (!(c)) {                                                                                                        \
      fprintf(stderr, "line %d: %s\n", __LINE__, #c);                                                                  \
      return 1;                                                                                                        \
    }                                                                                                                  \
  } while (0)
static const struct otlp_attribute *find(const struct otlp_attribute *attrs, size_t n, const char *key) {
  for (size_t i = 0; i < n; ++i)
    if (!strcmp(attrs[i].key, key))
      return &attrs[i];
  return NULL;
}
int main(void) {
  char secret[] = "--password=fixture-value";
  const char *raw[] = {"C:/private/demo.exe", "client",           secret, "--token", "fixture-token", "-Pfixture",
                       "session-fixture",     "--unicode=résumé", ""};
  const char *expected[] = {"demo",       "client",     "--password", "--token",   "[REDACTED]",
                            "[REDACTED]", "[REDACTED]", "--unicode",  "[REDACTED]"};
  gotel_command_args_t args = {0};
  CHECK(gotel_command_args_init(&args, "demo", "client", 9, raw) == OTLP_OK);
  for (int i = 0; i < args.argc; ++i)
    CHECK(!strcmp(args.argv[i], expected[i]));
  memset(secret, 'X', sizeof(secret) - 1);
  CHECK(!strcmp(args.argv[2], "--password"));
  gotel_operation_t operation = {"cli", false};
  gotel_options_t options = {0};
  options.exporter.service_name = "command-test";
  options.exporter.endpoint = "http://127.0.0.1:1/v1/traces";
  options.exporter.queue_capacity = 64;
  options.exporter.batch_ms = 1;
  options.signals = GOTEL_TRACES;
  options.realtime_sample_rate = 1;
  options.operations = &operation;
  options.operation_count = 1;
  gotel_test_clock_set(1000000000, 1700000000000000000ULL);
  options.monotonic_ns = gotel_test_monotonic_ns;
  options.realtime_ns = gotel_test_realtime_ns;
  options.argc = args.argc;
  options.argv = args.argv;
  gotel_host_t *host = NULL;
  CHECK(gotel_test_create(&options, &host) == OTLP_OK);
  gotel_scope_t scope;
  gotel_begin(host, &scope, 0, NULL);
  CHECK(scope.span != NULL);
  CHECK(gotel_command_args_set_span(&scope, &args) == OTLP_OK);
  gotel_command_args_destroy(&args);
  CHECK(args.argc == 0 && args.argv == NULL);
  gotel_command_args_destroy(&args);
  size_t count = 0;
  const struct otlp_attribute *attrs = otlp_span_get_attrs(scope.span, &count);
  const struct otlp_attribute *span_args = find(attrs, count, "process.command_args");
  const struct otlp_attribute *resource_args =
      find(host->exporter->resource_attrs.items, host->exporter->resource_attrs.n, "process.command_args");
  CHECK(span_args && resource_args && span_args->type == OTLP_ATTR_ARRAY && resource_args->type == OTLP_ATTR_ARRAY);
  CHECK(span_args->v.array_val->n == 9 && resource_args->v.array_val->n == 9);
  for (int i = 0; i < 9; ++i) {
    CHECK(!strcmp(span_args->v.array_val->items[i].v.string_val, expected[i]));
    CHECK(!strcmp(resource_args->v.array_val->items[i].v.string_val, expected[i]));
  }
  CHECK(!strcmp(find(attrs, count, "process.command")->v.string_val, "demo"));
  CHECK(!strcmp(find(attrs, count, "cli.command.name")->v.string_val, "client"));
  gotel_end(&scope);
  CHECK(gotel_drain(host) == OTLP_OK);
  gotel_destroy(host);
  CHECK(gotel_command_args_init(&args, "demo", NULL, -1, raw) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(gotel_command_args_init(&args, "demo", NULL, 1, NULL) == OTLP_ERR_INVALID_ARGUMENT);
  const char *invalid[] = {"demo", NULL};
  CHECK(gotel_command_args_init(&args, "demo", NULL, 2, invalid) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(args.argv == NULL);
  CHECK(gotel_command_args_init(&args, "demo", NULL, 0, NULL) == OTLP_OK);
  CHECK(args.argv != NULL && args.argc == 0);
  gotel_scope_t unsampled = {0};
  CHECK(gotel_command_args_set_span(&unsampled, &args) == OTLP_OK);
  gotel_command_args_destroy(&args);
  const char *plain[] = {"/private/demo", "--password=secret", "session-value"};
  CHECK(gotel_command_args_init_ex(&args, "demo", NULL, 3, plain, false) == OTLP_OK);
  for (int i = 0; i < 3; ++i)
    CHECK(!strcmp(args.argv[i], plain[i]));
  gotel_command_args_destroy(&args);
  return 0;
}
