#include <gotel/testing.h>
#include <stdio.h>
#include <string.h>
static const gotel_operation_t operations[] = {{"test.database.operation", false}, {"test.cleanup", false}};
typedef struct {
  int metadata;
  const char *name;
  const char *sql;
} application_query_t;
static bool application_query_get(const void *context, size_t index, gotel_query_t *out) {
  const application_query_t *queries = context;
  out->name = queries[index].name;
  out->sql = queries[index].sql;
  return true;
}
static int advance_progress(void *context) {
  (void)context;
  return gotel_test_clock_advance(100) == OTLP_OK ? 0 : 1;
}
#define CHECK(call)                                                                                                    \
  do {                                                                                                                 \
    if (!(call)) {                                                                                                     \
      fprintf(stderr, "line %d: %s\n", __LINE__, #call);                                                               \
      return 3;                                                                                                        \
    }                                                                                                                  \
  } while (0)
int main(int argc, char **argv) {
  if (argc != 3)
    return 2;
  gotel_options_t o = {0};
  o.application_name = "test";
  o.disable_span_name_prefix = !strcmp(argv[2], "unprefixed");
  o.exporter.service_name = "sqlite-test";
  o.exporter.endpoint = argv[1];
  o.exporter.flush_timeout_ms = 1000;
  o.exporter.batch_ms = 1;
  o.exporter.queue_capacity = 64;
  o.signals = GOTEL_ALL;
  o.operations = operations;
  o.operation_count = 2;
  o.realtime_sample_rate = 1;
  o.monotonic_ns = gotel_test_monotonic_ns;
  o.realtime_ns = gotel_test_realtime_ns;
  gotel_test_clock_set(1000000000, 1700000000000000000ULL);
  gotel_host_t *h = NULL;
  CHECK(gotel_create(&o, &h) == OTLP_OK);
  sqlite3 *db = NULL;
  CHECK(sqlite3_open(":memory:", &db) == SQLITE_OK);
  CHECK(sqlite3_exec(db,
                     "CREATE TABLE accounts(id INTEGER, token TEXT); INSERT INTO accounts VALUES(1,'LITERAL_SECRET');",
                     NULL, NULL, NULL) == SQLITE_OK);
  static const application_query_t queries[] = {
      {1, "FindAccount", "SELECT id FROM accounts WHERE token='LITERAL_SECRET' AND id=?1"},
      {2, "DeferredFinalize", "SELECT id FROM accounts UNION ALL SELECT 99"},
      {3, "EnableForeignKeys", "PRAGMA foreign_keys=ON;"}};
  sqlite3_progress_handler(db, 1, advance_progress, NULL);
  gotel_sqlite_options_t options = {.namespace_name = "fixture"};
  options.sql.omit_query_text = !strcmp(argv[2], "omit");
  if (!strcmp(argv[2], "truncate"))
    options.sql.max_query_text_bytes = 12;
  gotel_sqlite_t *adapter = NULL;
  gotel_query_catalog_t invalid = {queries, 3, NULL};
  CHECK(gotel_sqlite_attach_catalog_with_options(h, db, "test.storage", &invalid, &options, &adapter) ==
        OTLP_ERR_INVALID_ARGUMENT);
  const application_query_t duplicates[] = {{1, "First", "SELECT 1"}, {2, "Second", "SELECT 1"}};
  invalid = (gotel_query_catalog_t){duplicates, 2, application_query_get};
  CHECK(gotel_sqlite_attach_catalog_with_options(h, db, "test.storage", &invalid, &options, &adapter) ==
        OTLP_ERR_INVALID_ARGUMENT);
  gotel_query_catalog_t catalog = {queries, 3, application_query_get};
  CHECK(gotel_sqlite_attach_catalog_with_options(h, db, "test.storage", &catalog, &options, &adapter) == OTLP_OK);
  catalog.context = NULL; /* The adapter copied the catalog itself. */
  gotel_scope_t scope;
  gotel_begin(h, &scope, 0, NULL);
  CHECK(sqlite3_exec(db, queries[2].sql, NULL, NULL, NULL) == SQLITE_OK);
  sqlite3_stmt *statement = NULL;
  CHECK(sqlite3_prepare_v2(db, queries[0].sql, -1, &statement, NULL) == SQLITE_OK);
  for (int i = 0; i < 2; ++i) {
    CHECK(sqlite3_bind_int(statement, 1, 1) == SQLITE_OK);
    CHECK(sqlite3_step(statement) == SQLITE_ROW);
    CHECK(sqlite3_reset(statement) == SQLITE_OK);
  }
  CHECK(sqlite3_finalize(statement) == SQLITE_OK);
  CHECK(sqlite3_prepare_v2(db, queries[1].sql, -1, &statement, NULL) == SQLITE_OK);
  CHECK(sqlite3_step(statement) == SQLITE_ROW);
  CHECK(sqlite3_exec(db, "SELECT 'BOUND_SECRET', 789, x'4142', NULL", NULL, NULL, NULL) == SQLITE_OK);
  gotel_end(&scope);
  gotel_begin(h, &scope, 1, NULL);
  CHECK(gotel_test_clock_advance(250) == OTLP_OK);
  CHECK(sqlite3_finalize(statement) == SQLITE_OK);
  gotel_end(&scope);
  CHECK(sqlite3_close(db) == SQLITE_OK);
  CHECK(gotel_drain(h) == OTLP_OK);
  gotel_destroy(h);
  return 0;
}
