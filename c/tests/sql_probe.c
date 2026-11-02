#include <gotel/sql.h>
#include <gotel/testing.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CHECK(call)                                                                                                    \
  do {                                                                                                                 \
    if (!(call)) {                                                                                                     \
      fprintf(stderr, "line %d: %s\n", __LINE__, #call);                                                               \
      return 3;                                                                                                        \
    }                                                                                                                  \
  } while (0)
static const gotel_operation_t operations[] = {{"test.sql", false}};

int main(int argc, char **argv) {
  if (argc != 2)
    return 2;
  CHECK(SEQUEL_INPUT_MAX == (size_t)1024 * 1024 * 1024);
  CHECK(SEQUEL_TEXT_MAX == SEQUEL_INPUT_MAX && SEQUEL_SUMMARY_MAX == SEQUEL_INPUT_MAX);
  gotel_options_t options = {0};
  options.application_name = "test";
  options.exporter.service_name = "sql-test";
  options.exporter.endpoint = argv[1];
  options.exporter.flush_timeout_ms = 1000;
  options.exporter.batch_ms = 1;
  options.signals = GOTEL_TRACES;
  options.operations = operations;
  options.operation_count = 1;
  options.realtime_sample_rate = 1;
  options.monotonic_ns = gotel_test_monotonic_ns;
  options.realtime_ns = gotel_test_realtime_ns;
  gotel_test_clock_set(1000000000, 1700000000000000000ULL);
  gotel_host_t *host = NULL;
  CHECK(gotel_create(&options, &host) == OTLP_OK);
  const char *queries[] = {"SELECT 'LITERAL_SECRET';", "SELECT 'LITERAL_SECRET", "SELECT (1", "SELECT )",
                           "SELECT /* LITERAL_SECRET"};
  for (size_t i = 0; i < sizeof(queries) / sizeof(*queries); ++i) {
    for (int omit = 0; omit < 2; ++omit) {
      sequel_options_t policy = {.omit_query_text = omit};
      sequel_metadata_t metadata;
      sequel_inspect(queries[i], &policy, &metadata);
      gotel_scope_t scope;
      gotel_begin(host, &scope, 0, NULL);
      otlp_span_set_attribute_int(scope.span, "test.case", (int64_t)i);
      otlp_span_set_attribute_bool(scope.span, "test.omit", omit);
      gotel_sql_attributes(scope.span, &metadata, "sqlite", NULL, 0);
      sequel_metadata_free(&metadata);
      /* Metadata was freed: attribute setters must own their strings. */
      gotel_sql_diagnostics(scope.span, queries[i], SEQUEL_SQLITE);
      gotel_end(&scope);
    }
  }
  /* More than the former 1 MiB input and 32 KiB output limits, without a
   * maximum-sized allocation in the adapter. */
  size_t padding = 1024 * 1024 + 1, identifier = 40000;
  char *large = malloc(padding + identifier + 16);
  CHECK(large);
  memset(large, ' ', padding);
  memcpy(large + padding, "SELECT ", 7);
  memset(large + padding + 7, 'a', identifier);
  strcpy(large + padding + 7 + identifier, ";");
  sequel_metadata_t metadata;
  CHECK(sequel_inspect(large, NULL, &metadata) == SEQUEL_OK);
  CHECK(strlen(metadata.text) > 32768 && !metadata.truncated);
  gotel_scope_t scope;
  gotel_begin(host, &scope, 0, NULL);
  otlp_span_set_attribute_int(scope.span, "test.case", 5);
  gotel_sql_attributes(scope.span, &metadata, "sqlite", NULL, 0);
  gotel_sql_diagnostics(scope.span, large, SEQUEL_SQLITE);
  sequel_metadata_free(&metadata);
  sequel_metadata_free(&metadata);
  free(large);
  gotel_end(&scope);
  /* Metadata-only APIs must not diagnose truncation or dereference NULL text. */
  sequel_options_t truncated_policy = {.max_query_text_bytes = 8};
  CHECK(sequel_inspect("SELECT (column) FROM records", &truncated_policy, &metadata) == SEQUEL_OK);
  CHECK(metadata.truncated);
  gotel_begin(host, &scope, 0, NULL);
  otlp_span_set_attribute_int(scope.span, "test.case", 6);
  gotel_sql_attributes(scope.span, &metadata, "sqlite", NULL, 0);
  sequel_metadata_free(&metadata);
  gotel_end(&scope);
  metadata = (sequel_metadata_t){0};
  gotel_begin(host, &scope, 0, NULL);
  otlp_span_set_attribute_int(scope.span, "test.case", 7);
  gotel_sql_attributes(scope.span, &metadata, "sqlite", NULL, 0);
  gotel_end(&scope);
  CHECK(gotel_drain(host) == OTLP_OK);
  gotel_destroy(host);
  return 0;
}
