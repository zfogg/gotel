#ifdef NDEBUG
#undef NDEBUG
#endif
#include <assert.h>
#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#endif
#include <gotel/clickhouse.h>
#include <gotel/testing.h>
#include <stdio.h>

typedef struct {
  int calls, result;
  gotel_context_t parent;
} native_t;

static int native_call(void *userdata) {
  native_t *native = userdata;
  ++native->calls;
  gotel_context_t current = gotel_current_context();
  assert(current.valid && current.sampled);
  assert(gotel_test_clock_advance(5000000) == OTLP_OK);
  return native->result;
}

static gotel_outcome_t classify_cancelled(int result, void *userdata) {
  assert(result == 42 && userdata);
  return GOTEL_OUTCOME_CANCELLED;
}

int main(void) {
#ifdef _WIN32
  _setmode(_fileno(stdout), _O_BINARY);
#endif
  static const gotel_operation_t operations[] = {{"storage.clickhouse.Query", false}, {"fixture.Owner", false}};
  gotel_options_t options = {0};
  options.exporter.service_name = "clickhouse-test";
  options.operations = operations;
  options.operation_count = 2;
  options.signals = GOTEL_TRACES | GOTEL_METRICS;
  options.monotonic_ns = gotel_test_monotonic_ns;
  options.realtime_ns = gotel_test_realtime_ns;
  gotel_test_clock_set(1000000000, 1700000000000000000ULL);
  gotel_host_t *host = NULL;
  assert(gotel_test_create(&options, &host) == OTLP_OK);
  gotel_test_capture_t *capture = gotel_test_capture_create(1024 * 1024);
  assert(capture && gotel_test_capture_attach(host, capture) == OTLP_OK);
  gotel_scope_t parent;
  gotel_begin(host, &parent, 1, NULL);
  native_t native = {0};
  sequel_analysis_options_t policy = {.dialect = SEQUEL_SQLITE, .format = true};
  gotel_clickhouse_options_t query_options = {.namespace_name = "analytics",
                                              .server_address = "localhost",
                                              .server_port = 9000,
                                              .bind_count = 1,
                                              .analysis = &policy};
  const char *queries[] = {"SELECT {id:UInt64}, 'PRIVATE_LITERAL'", "SELECT 'PRIVATE_UNCLOSED",
                           "SELECT 'PRIVATE_HIDDEN'", "SELECT 'PRIVATE_LIMIT'"};
  for (int i = 0; i < 4; ++i) {
    policy.omit_query_text = i == 2;
    policy.max_work_units = i == 3 ? 1 : 0;
    native.result = i == 1 ? 62 : 0;
    int result = gotel_clickhouse_call(host, 0, NULL, queries[i], &query_options, native_call, &native, NULL);
    assert(result == native.result && native.calls == i + 1);
    assert(gotel_current_context().span_id[0] == parent.context.span_id[0]);
  }
  policy.max_work_units = 0;
  native.result = 42;
  assert(gotel_clickhouse_call(host, 0, NULL, "SELECT 1", &query_options, native_call, &native, classify_cancelled) ==
         42);
  assert(native.calls == 5);
  assert(gotel_clickhouse_call(host, 0, NULL, "SELECT 1", NULL, NULL, NULL, NULL) == -1);
  assert(native.calls == 5);
  gotel_scope_t stream;
  gotel_clickhouse_begin(host, &stream, 0, NULL, "SELECT 1", NULL);
  assert(gotel_test_clock_advance(1000000) == OTLP_OK);
  gotel_clickhouse_end(&stream, 99, GOTEL_OUTCOME_DEFERRED);
  gotel_clickhouse_end(&stream, 99, GOTEL_OUTCOME_ERROR);
  gotel_end(&parent);
  gotel_destroy(host);
  const uint8_t *data = NULL;
  size_t size = 0;
  assert(gotel_test_capture_read(capture, &data, &size) == OTLP_OK && size);
  fwrite(data, 1, size, stdout);
  gotel_test_capture_destroy(capture);
}
