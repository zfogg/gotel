#include <gotel/testing.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#ifdef _WIN32
#include <windows.h>
#else
#include <pthread.h>
#endif
#define CHECK(x)                                                                                                       \
  do {                                                                                                                 \
    if (!(x)) {                                                                                                        \
      fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #x);                                                          \
      exit(1);                                                                                                         \
    }                                                                                                                  \
  } while (0)
static const gotel_operation_t ops[] = {{"test.cli", false}, {"test.frame", true}, {"test.child", false}};
static const double metric_bounds[] = {1, 10, 100};
static const gotel_metric_t metrics[] = {
    {"test.bytes", "By", OTLP_METRIC_COUNTER, NULL, 0},
    {"test.depth", "{item}", OTLP_METRIC_GAUGE, NULL, 0},
    {"test.size", "By", OTLP_METRIC_HISTOGRAM, metric_bounds, 3},
};
static gotel_options_t options(void) {
  gotel_options_t o = {0};
  o.exporter.service_name = "c-gotel-test";
  o.exporter.endpoint = "http://127.0.0.1:1/v1/traces";
  o.exporter.flush_timeout_ms = 100;
  o.exporter.batch_ms = 1;
  o.exporter.queue_capacity = 64;
  o.signals = GOTEL_ALL;
  o.realtime_sample_rate = 0.01;
  o.monotonic_ns = gotel_test_monotonic_ns;
  o.realtime_ns = gotel_test_realtime_ns;
  o.operations = ops;
  o.operation_count = 3;
  o.metrics = metrics;
  o.metric_count = 3;
  return o;
}
static void names(void) {
  char b[256];
  CHECK(gotel_http_name(b, sizeof(b), "app", false, "GET",
                        "https://user:secret@example.com:8443/a/123?q=secret#frag") == OTLP_OK);
  CHECK(!strcmp(b, "app.http.client.GET example.com:8443/a/{id}"));
  CHECK(gotel_http_name(b, sizeof(b), "app.", true, "PROPFIND", "") == OTLP_OK);
  CHECK(!strcmp(b, "app.http.server.PROPFIND unknown"));
  CHECK(gotel_http_name(b, sizeof(b), "app", true, "GET", "/items/{id}") == OTLP_OK);
  CHECK(!strcmp(b, "app.http.server.GET /items/{id}"));
  CHECK(gotel_http_name(b, sizeof(b), "", false, "POST", "http://[::1]:80/%2Fabc") == OTLP_OK);
  CHECK(!strcmp(b, "http.client.POST [::1]:80/%2Fabc"));
  CHECK(gotel_http_name(b, 4, "app", false, "GET", "http://example.com") == OTLP_ERR_OVERFLOW);
  CHECK(!*b);
  CHECK(gotel_http_name(b, sizeof(b), "app", false, "GET", "secret") == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(gotel_http_name(b, sizeof(b), "app", true, "GET", "/path?secret") == OTLP_ERR_INVALID_ARGUMENT);
  const char *ids[] = {"123", "ABCDEF1234567890abcdef12", "550e8400-e29b-41d4-a716-446655440000"};
  for (size_t i = 0; i < sizeof(ids) / sizeof(*ids); ++i) {
    char url[256];
    snprintf(url, sizeof(url), "https://example.com/items/%s", ids[i]);
    CHECK(gotel_http_name(b, sizeof(b), "app", false, "GET", url) == OTLP_OK);
    CHECK(!strcmp(b, "app.http.client.GET example.com/items/{id}"));
  }
  CHECK(gotel_http_name(b, sizeof(b), "app", false, "GET",
                        "https://example.com/550e8400-e29b-01d4-a716-446655440000") == OTLP_OK);
  CHECK(strstr(b, "550e8400-e29b-01d4-a716-446655440000"));
  CHECK(gotel_http_name(b, sizeof(b), "app", false, "GET",
                        "https://example.com/550e8400-e29b-41d4-0716-446655440000") == OTLP_OK);
  CHECK(strstr(b, "550e8400-e29b-41d4-0716-446655440000"));
  CHECK(gotel_http_name(b, sizeof(b), "app", false, "GET", "https://example.com/日本/１２３/42") == OTLP_OK);
  CHECK(!strcmp(b, "app.http.client.GET example.com/日本/１２３/{id}"));
  CHECK(gotel_http_name(b, sizeof(b), "app", true, "GET", "/\xc0\xaf") == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(!*b);
}
static void validation(void) {
  unsigned signals = 99;
  CHECK(gotel_parse_signals("traces,logs", &signals) == OTLP_OK && signals == 3);
  CHECK(gotel_parse_signals("metrics", &signals) == OTLP_OK && signals == 4);
  CHECK(gotel_parse_signals("none", &signals) == OTLP_OK && signals == 0);
  CHECK(gotel_parse_signals("traces,", &signals) != OTLP_OK);
  CHECK(gotel_parse_signals("logs,logs", &signals) != OTLP_OK);
  CHECK(gotel_parse_signals("", &signals) != OTLP_OK);
  CHECK(gotel_parse_signals("traces,wat", &signals) != OTLP_OK);
  gotel_host_t *h = (void *)1;
  gotel_options_t o = options();
  o.realtime_sample_rate = NAN;
  CHECK(gotel_test_create(&o, &h) != OTLP_OK && h == NULL);
  o = options();
  o.exporter.queue_capacity = 3;
  CHECK(gotel_test_create(&o, &h) != OTLP_OK && h == NULL);
  o = options();
  o.signals = 8;
  CHECK(gotel_test_create(&o, &h) != OTLP_OK);
  o = options();
  o.metrics = NULL;
  CHECK(gotel_test_create(&o, &h) == OTLP_ERR_INVALID_ARGUMENT);
  gotel_metric_t bad[] = {metrics[0], metrics[0]};
  o.metrics = bad;
  o.metric_count = 2;
  CHECK(gotel_test_create(&o, &h) == OTLP_ERR_INVALID_ARGUMENT);
  double unsorted[] = {2, 1};
  bad[0] = (gotel_metric_t){"bad", "s", OTLP_METRIC_HISTOGRAM, unsorted, 2};
  o.metric_count = 1;
  CHECK(gotel_test_create(&o, &h) == OTLP_ERR_INVALID_ARGUMENT);
  unsorted[1] = INFINITY;
  CHECK(gotel_test_create(&o, &h) == OTLP_ERR_INVALID_ARGUMENT);
}
static void sampling(void) {
  gotel_options_t o = options();
  gotel_host_t *h = NULL;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  size_t sampled = 0;
  for (int i = 0; i < 10000; ++i) {
    gotel_scope_t s, c;
    gotel_begin(h, &s, 1, NULL);
    if (s.span)
      ++sampled;
    gotel_begin(h, &c, 2, NULL);
    CHECK((s.span != NULL) == (c.span != NULL));
    if (s.span)
      CHECK(!memcmp(s.context.trace_id, c.context.trace_id, 16));
    gotel_end(&c);
    gotel_end(&s);
    CHECK(!gotel_current_context().valid);
  }
  CHECK(sampled > 60 && sampled < 140);
  gotel_destroy(h);
  o.realtime_sample_rate = 1;
  o.realtime_max_per_second = 2;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  for (int i = 0; i < 3; ++i) {
    gotel_scope_t s;
    gotel_begin(h, &s, 1, NULL);
    CHECK((s.span != NULL) == (i < 2));
    gotel_end(&s);
  }
  CHECK(gotel_test_clock_advance(1000000000) == OTLP_OK);
  gotel_scope_t s;
  gotel_begin(h, &s, 1, NULL);
  CHECK(s.span);
  gotel_end(&s);
  gotel_destroy(h);
  o.realtime_sample_rate = 0;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  gotel_scope_t root, frame;
  gotel_begin(h, &root, 0, NULL);
  CHECK(root.span);
  gotel_begin(h, &frame, 1, NULL);
  CHECK(!frame.span);
  gotel_end(&frame);
  gotel_end(&root);
  gotel_destroy(h);
}
static void context_and_signals(void) {
  gotel_options_t o = options();
  gotel_host_t *h = NULL;
  gotel_scope_t root, child;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  gotel_begin(h, &root, 0, NULL);
  gotel_context_t c = gotel_current_context();
  gotel_end(&root);
  gotel_begin(h, &child, 2, &c);
  CHECK(!memcmp(c.trace_id, child.context.trace_id, 16));
  gotel_error(&child, "test.failure", "test error");
  CHECK(child.failed);
  gotel_end(&child);
  gotel_end(&child);
  gotel_destroy(h);
  o.signals = GOTEL_METRICS;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  gotel_begin(h, &root, 0, NULL);
  CHECK(!root.span);
  gotel_end(&root);
  gotel_log(h, OTLP_SEVERITY_INFO, "not exported", NULL, 0, NULL);
  otlp_exporter_stats_t stats;
  CHECK(gotel_stats(h, &stats) == OTLP_OK);
  CHECK(stats.emitted == 0);
  gotel_destroy(h);
}
#ifdef CGOTEL_WITH_SQLITE
static void sqlite_test(gotel_host_t *h) {
  sqlite3 *db = NULL;
  CHECK(sqlite3_open(":memory:", &db) == SQLITE_OK);
  static const gotel_query_t q[] = {{"SelectSecret", "SELECT ? UNION ALL SELECT ?"}};
  gotel_sqlite_t *a = NULL;
  CHECK(gotel_sqlite_attach(h, db, "test.storage", q, 1, &a) == OTLP_OK);
  sqlite3_stmt *s = NULL;
  CHECK(sqlite3_prepare_v2(db, q[0].sql, -1, &s, NULL) == SQLITE_OK);
  CHECK(sqlite3_bind_text(s, 1, "BOUND_SECRET", -1, SQLITE_STATIC) == SQLITE_OK);
  CHECK(sqlite3_step(s) == SQLITE_ROW);
  CHECK(sqlite3_reset(s) == SQLITE_OK);
  CHECK(sqlite3_step(s) == SQLITE_ROW);
  CHECK(sqlite3_finalize(s) == SQLITE_OK);
  CHECK(sqlite3_exec(db, "SELECT 42", NULL, NULL, NULL) == SQLITE_OK);
  gotel_sqlite_detach(a);
  CHECK(sqlite3_exec(db, "SELECT 43", NULL, NULL, NULL) == SQLITE_OK);
  CHECK(sqlite3_close(db) == SQLITE_OK);
}
#endif
static void registry(void) {
#ifdef CGOTEL_WITH_SQLITE
  static const gotel_query_t q[] = {{"One", "SELECT 1"}, {"Two", "SELECT 2"}};
  CHECK(gotel_query_find(q, 2, "SELECT 2") == &q[1]);
  CHECK(gotel_query_find(q, 2, "SELECT 3") == NULL);
  CHECK(gotel_query_find(NULL, 0, "SELECT 1") == NULL);
  gotel_options_t o = options();
  gotel_host_t *h = NULL;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  sqlite3 *db = NULL;
  CHECK(sqlite3_open(":memory:", &db) == SQLITE_OK);
  gotel_sqlite_t *a = NULL;
  const gotel_query_t duplicate[] = {{"One", "SELECT 1"}, {"Other", "SELECT 1"}};
  CHECK(gotel_sqlite_attach(h, db, "app.storage", duplicate, 2, &a) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(a == NULL);
  CHECK(gotel_sqlite_attach(h, db, "app.storage", q, 2, &a) == OTLP_OK);
  CHECK(sqlite3_exec(db, "SELECT 1", NULL, NULL, NULL) == SQLITE_OK);
  /* CLOSE frees the callback attachment without requiring wrapper close APIs. */
  CHECK(sqlite3_close(db) == SQLITE_OK);
  gotel_destroy(h);
#endif
}
#ifdef _WIN32
static DWORD WINAPI produce(void *arg)
#else
static void *produce(void *arg)
#endif
{
  gotel_host_t *h = arg;
  gotel_test_clock_set(7, 9);
  CHECK(gotel_test_clock_advance(11) == OTLP_OK);
  CHECK(gotel_test_monotonic_ns() == 18 && gotel_test_realtime_ns() == 20);
  for (int i = 0; i < 1000; ++i) {
    gotel_scope_t s;
    CHECK(gotel_metric_record(h, 0, 1) == OTLP_OK);
    CHECK(gotel_metric_record(h, 2, 10) == OTLP_OK);
    gotel_begin(h, &s, 2, NULL);
    CHECK(gotel_current_context().valid);
    gotel_end(&s);
  }
  return 0;
}
static void concurrent(gotel_host_t *h) {
  uint64_t before = gotel_test_monotonic_ns();
#ifdef _WIN32
  HANDLE threads[4];
  for (int i = 0; i < 4; ++i) {
    threads[i] = CreateThread(NULL, 0, produce, h, 0, NULL);
    CHECK(threads[i] != NULL);
  }
  CHECK(WaitForMultipleObjects(4, threads, TRUE, 10000) == WAIT_OBJECT_0);
  for (int i = 0; i < 4; ++i)
    CloseHandle(threads[i]);
#else
  pthread_t threads[4];
  for (int i = 0; i < 4; ++i)
    CHECK(pthread_create(&threads[i], NULL, produce, h) == 0);
  for (int i = 0; i < 4; ++i)
    CHECK(pthread_join(threads[i], NULL) == 0);
#endif
  CHECK(!gotel_current_context().valid);
  CHECK(gotel_test_monotonic_ns() == before);
}
static void concurrency(void) {
  gotel_options_t o = options();
  gotel_host_t *h = NULL;
  o.signals = GOTEL_METRICS;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  concurrent(h);
  gotel_destroy(h);
}
static void wire(const char *endpoint, unsigned signals) {
  gotel_test_events_t *events = gotel_test_events_create();
  CHECK(events != NULL);
  gotel_options_t o = options();
  o.exporter.endpoint = endpoint;
  o.exporter.flush_timeout_ms = 2000;
  o.exporter.backoff_initial_ms = 10;
  o.exporter.backoff_max_ms = 20;
  o.exporter_event = gotel_test_event_record;
  o.exporter_event_context = events;
  o.signals = signals;
  gotel_host_t *h = NULL;
  CHECK(gotel_create(&o, &h) == OTLP_OK);
  CHECK(gotel_metric_record(h, 0, 20) == OTLP_OK);
  CHECK(gotel_metric_record(h, 0, 30) == OTLP_OK);
  CHECK(gotel_metric_record(h, 1, 7) == OTLP_OK);
  CHECK(gotel_metric_record(h, 1, 2) == OTLP_OK);
  CHECK(gotel_metric_record(h, 2, 5) == OTLP_OK);
  CHECK(gotel_metric_record(h, 2, 50) == OTLP_OK);
  CHECK(gotel_metric_record(h, 3, 1) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(gotel_metric_record(h, 0, -1) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(gotel_metric_record(h, 1, NAN) == OTLP_ERR_INVALID_ARGUMENT);
  gotel_scope_t root;
  gotel_begin(h, &root, 0, NULL);
  gotel_log(h, OTLP_SEVERITY_INFO, "wire message", __FILE__, __LINE__, __func__);
  CHECK(gotel_test_clock_advance(5000000) == OTLP_OK);
  gotel_error(&root, "fixture.error", "wire failure");
  gotel_end(&root);
  gotel_log(h, OTLP_SEVERITY_INFO, "wire outside", __FILE__, __LINE__, __func__);
  for (int severity = OTLP_SEVERITY_TRACE; severity <= OTLP_SEVERITY_FATAL4; ++severity)
    gotel_log(h, (otlp_severity_t)severity, "wire severity", __FILE__, __LINE__, __func__);
#ifdef CGOTEL_WITH_SQLITE
  sqlite_test(h);
#endif
  if (signals == GOTEL_METRICS)
    concurrent(h);
  CHECK(gotel_drain(h) == OTLP_OK);
  gotel_test_event_counts_t counts = gotel_test_events_snapshot(events);
  CHECK(counts.retries ==
        ((signals & GOTEL_TRACES ? 1 : 0) + (signals & GOTEL_LOGS ? 1 : 0) + (signals & GOTEL_METRICS ? 1 : 0)));
  CHECK(counts.batches >= counts.retries);
  gotel_destroy(h);
  gotel_test_events_destroy(events);
}
static void testing_support(void) {
  gotel_test_clock_set(10, 20);
  CHECK(gotel_test_clock_advance(5) == OTLP_OK);
  CHECK(gotel_test_monotonic_ns() == 15 && gotel_test_realtime_ns() == 25);
  gotel_test_clock_set(UINT64_MAX - 1, 20);
  CHECK(gotel_test_clock_advance(2) == OTLP_ERR_OVERFLOW);
  CHECK(gotel_test_monotonic_ns() == UINT64_MAX - 1 && gotel_test_realtime_ns() == 20);
  gotel_test_clock_set(1000000000, UINT64_C(1700000000000000000));
  gotel_options_t o = options();
  o.signals = GOTEL_TRACES;
  o.exporter.queue_capacity = 2;
  gotel_test_events_t *events = gotel_test_events_create();
  CHECK(events);
  o.exporter_event = gotel_test_event_record;
  o.exporter_event_context = events;
  gotel_host_t *h = NULL;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  gotel_test_script_t bad = {.count = 1, .fallback_status = 200};
  CHECK(gotel_test_transport(h, &bad) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(gotel_test_transport(NULL, NULL) == OTLP_ERR_INVALID_ARGUMENT);
  for (int i = 0; i < 10; ++i) {
    gotel_scope_t scope;
    gotel_begin(h, &scope, 0, NULL);
    gotel_end(&scope);
  }
  otlp_exporter_stats_t stats;
  CHECK(gotel_stats(h, &stats) == OTLP_OK);
  CHECK(stats.emitted + stats.dropped_full == 10 && stats.dropped_full > 0);
  gotel_test_event_counts_t counts = gotel_test_events_snapshot(events);
  CHECK(counts.queue_full == stats.dropped_full);
  CHECK(gotel_drain(h) == OTLP_OK);
  CHECK(gotel_stats(h, &stats) == OTLP_OK && stats.sent == stats.emitted);
  gotel_destroy(h);
  gotel_test_events_destroy(events);
}
int main(int argc, char **argv) {
  if (argc == 3 && !strcmp(argv[1], "--case")) {
    if (!strcmp(argv[2], "http"))
      names();
    else if (!strcmp(argv[2], "validation"))
      validation();
    else if (!strcmp(argv[2], "sampling"))
      sampling();
    else if (!strcmp(argv[2], "context"))
      context_and_signals();
    else if (!strcmp(argv[2], "registry"))
      registry();
    else if (!strcmp(argv[2], "testing"))
      testing_support();
    else if (!strcmp(argv[2], "concurrency"))
      concurrency();
    else
      CHECK(false);
    return 0;
  }
  if (argc > 1) {
    wire(argv[1], argc > 2 ? (unsigned)atoi(argv[2]) : GOTEL_ALL);
    return 0;
  }
  testing_support();
  names();
  validation();
  sampling();
  context_and_signals();
  registry();
  concurrency();
#ifdef CGOTEL_WITH_SQLITE
  gotel_options_t o = options();
  gotel_host_t *h = NULL;
  CHECK(gotel_test_create(&o, &h) == OTLP_OK);
  sqlite_test(h);
  otlp_exporter_stats_t stats;
  CHECK(gotel_stats(h, &stats) == OTLP_OK);
  CHECK(stats.emitted == 3);
  gotel_destroy(h);
#endif
  puts("c-gotel unit tests passed");
  return 0;
}
