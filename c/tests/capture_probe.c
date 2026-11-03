#include <gotel/testing.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#include <windows.h>
#else
#include <pthread.h>
#endif

#define CHECK(value)                                                                                                   \
  do {                                                                                                                 \
    if (!(value)) {                                                                                                    \
      fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #value);                                                      \
      exit(1);                                                                                                         \
    }                                                                                                                  \
  } while (0)

static const gotel_operation_t operations[] = {
    {"capture.root", false}, {"capture.child", false}, {"capture.hot", true}};

static gotel_host_t *create(const char *service) {
  gotel_options_t options = {0};
  options.exporter.service_name = service;
  options.exporter.endpoint = "http://127.0.0.1:1/v1/traces";
  options.exporter.queue_capacity = 1024;
  options.exporter.flush_timeout_ms = 100;
  options.signals = GOTEL_TRACES;
  options.operations = operations;
  options.operation_count = 3;
  options.monotonic_ns = gotel_test_monotonic_ns;
  options.realtime_ns = gotel_test_realtime_ns;
  gotel_host_t *host = NULL;
  CHECK(gotel_test_create(&options, &host) == OTLP_OK);
  return host;
}

static void emit(gotel_host_t *host) {
  gotel_test_clock_set(UINT64_C(1000000000), UINT64_C(1700000000000000000));
  gotel_scope_t root, child, dropped;
  gotel_begin(host, &root, 0, NULL);
  gotel_begin(host, &child, 1, NULL);
  gotel_attr_string(&child, "capture.string", "quoted \"value\"\nutf8 \xF0\x9F\x98\x80");
  gotel_attr_int(&child, "capture.integer", 42);
  CHECK(otlp_span_add_event(child.span, "capture.event", UINT64_C(1700000000000000001)) == OTLP_OK);
  CHECK(otlp_span_set_event_attribute_string(child.span, "event.key", "event.value") == OTLP_OK);
  CHECK(otlp_span_add_link(child.span, root.context.trace_id, root.context.span_id) == OTLP_OK);
  CHECK(otlp_span_set_link_attribute_string(child.span, "link.key", "link.value") == OTLP_OK);
  gotel_error(&child, "capture.failure", "stable fixture failure");
  gotel_end(&child);
  gotel_context_t independent = {0};
  gotel_begin(host, &dropped, 2, &independent);
  CHECK(!dropped.context.sampled);
  gotel_end(&dropped);
  gotel_end(&root);
}

#ifdef _WIN32
static DWORD WINAPI produce(void *context) {
#else
static void *produce(void *context) {
#endif
  for (unsigned i = 0; i < 10; ++i)
    emit(context);
  return 0;
}

static void concurrent(gotel_host_t *host) {
#ifdef _WIN32
  HANDLE threads[4];
  for (unsigned i = 0; i < 4; ++i) {
    threads[i] = CreateThread(NULL, 0, produce, host, 0, NULL);
    CHECK(threads[i] != NULL);
  }
  CHECK(WaitForMultipleObjects(4, threads, TRUE, 10000) == WAIT_OBJECT_0);
  for (unsigned i = 0; i < 4; ++i)
    CHECK(CloseHandle(threads[i]));
#else
  pthread_t threads[4];
  for (unsigned i = 0; i < 4; ++i)
    CHECK(pthread_create(&threads[i], NULL, produce, host) == 0);
  for (unsigned i = 0; i < 4; ++i)
    CHECK(pthread_join(threads[i], NULL) == 0);
#endif
}

int main(int argc, char **argv) {
  CHECK(argc == 3);
  CHECK(gotel_test_capture_create(0) == NULL);
  CHECK(gotel_test_capture_attach(NULL, NULL) == OTLP_ERR_NULL);
  gotel_test_capture_detach(NULL);
  gotel_test_capture_destroy(NULL);
  bool overflow = !strcmp(argv[1], "overflow");
  gotel_test_capture_t *capture = gotel_test_capture_create(overflow ? 1 : 1024 * 1024);
  CHECK(capture != NULL);
  const uint8_t *data = (const uint8_t *)1;
  size_t bytes = 1;
  CHECK(gotel_test_capture_read(NULL, &data, &bytes) == OTLP_ERR_NULL);
  CHECK(data == NULL && bytes == 0);
  CHECK(gotel_test_capture_read(capture, &data, &bytes) == OTLP_OK);
  CHECK(data == NULL && bytes == 0);
  CHECK(gotel_test_capture_read(capture, NULL, &bytes) == OTLP_ERR_NULL);
  CHECK(gotel_test_capture_read(capture, &data, NULL) == OTLP_ERR_NULL);
  gotel_host_t *host = create("capture-one");
  CHECK(gotel_test_capture_attach(host, capture) == OTLP_OK);
  CHECK(gotel_test_capture_attach(host, capture) == OTLP_ERR_INVALID_ARGUMENT);
  if (!strcmp(argv[1], "concurrent"))
    concurrent(host);
  else
    emit(host);
  if (!strcmp(argv[1], "snapshot")) {
    gotel_scope_t scope;
    gotel_begin(host, &scope, 0, NULL);
    gotel_span_snapshot_t snapshot = gotel_current_span_snapshot();
    CHECK(gotel_test_clock_advance(1000000000) == OTLP_OK);
    CHECK(gotel_publish_span_snapshot(host, &snapshot) == OTLP_OK);
    gotel_end(&scope);
  }
  CHECK(gotel_drain(host) == OTLP_OK);
  gotel_test_capture_detach(host);
  gotel_test_capture_detach(host);
  CHECK(gotel_test_capture_read(capture, &data, &bytes) == (overflow ? OTLP_ERR_OVERFLOW : OTLP_OK));
  if (overflow) {
    CHECK(data == NULL && bytes == 0);
    // A failed capture cannot be reused to accidentally assert on a suffix.
    CHECK(gotel_test_capture_attach(host, capture) == OTLP_OK);
    emit(host);
    CHECK(gotel_test_capture_read(capture, &data, &bytes) == OTLP_ERR_OVERFLOW);
    gotel_destroy(host);
  } else {
    size_t before_detached = bytes;
    emit(host);
    CHECK(gotel_test_capture_read(capture, &data, &bytes) == OTLP_OK && bytes == before_detached);
    CHECK(gotel_drain(host) == OTLP_OK);
    gotel_destroy(host);
    gotel_host_t *second = create("capture-two");
    CHECK(gotel_test_capture_attach(second, capture) == OTLP_OK);
    emit(second);
    CHECK(gotel_drain(second) == OTLP_OK);
    gotel_destroy(second); // Host destruction ends attachment lifetime too.
    CHECK(gotel_test_capture_read(capture, &data, &bytes) == OTLP_OK && bytes > before_detached);
    FILE *output = fopen(argv[2], "wb");
    CHECK(output != NULL);
    CHECK(fwrite(data, 1, bytes, output) == bytes);
    CHECK(fclose(output) == 0);
  }
  gotel_test_capture_destroy(capture);
  return 0;
}
