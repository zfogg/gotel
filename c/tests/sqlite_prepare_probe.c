#include <gotel/testing.h>
#include <stdio.h>
#include <string.h>

#define CHECK(value)                                                                                                   \
  do {                                                                                                                 \
    if (!(value)) {                                                                                                    \
      fprintf(stderr, "line %d: %s\n", __LINE__, #value);                                                              \
      return 1;                                                                                                        \
    }                                                                                                                  \
  } while (0)

int main(int argc, char **argv) {
  CHECK(argc == 3);
  gotel_options_t options = {0};
  static const gotel_operation_t operations[] = {{"fixture.operation", false}};
  options.application_name = "fixture";
  options.exporter.service_name = "prepare-fixture";
  options.exporter.endpoint = argv[1];
  options.exporter.queue_capacity = 64;
  options.exporter.batch_ms = 1;
  options.exporter.flush_timeout_ms = 1000;
  options.signals = GOTEL_ALL;
  options.operations = operations;
  options.operation_count = 1;
  options.monotonic_ns = gotel_test_monotonic_ns;
  options.realtime_ns = gotel_test_realtime_ns;
  gotel_test_clock_set(1000000000, 1700000000000000000ULL);
  gotel_host_t *host = NULL;
  CHECK(gotel_create(&options, &host) == OTLP_OK);
  sqlite3 *db = NULL;
  CHECK(sqlite3_open(":memory:", &db) == SQLITE_OK);
  sqlite3_stmt *statement = NULL;
  CHECK(gotel_sqlite_prepare_db(db, "SELECT 1", &statement) == SQLITE_OK);
  CHECK(sqlite3_finalize(statement) == SQLITE_OK);
  static const gotel_query_t queries[] = {{"Good", "SELECT 'LITERAL_SECRET'"},
                                          {"Missing", "SELECT 'FAIL_SECRET' FROM missing_table"}};
  gotel_sqlite_options_t sql_options = {.namespace_name = "fixture"};
  sql_options.sql.omit_query_text = !strcmp(argv[2], "omit");
  gotel_sqlite_t *attachment = NULL, *duplicate = NULL;
  CHECK(gotel_sqlite_attach_with_options(host, db, "fixture.storage", queries, 2, &sql_options, &attachment) ==
        OTLP_OK);
  CHECK(gotel_sqlite_attach(host, db, "duplicate", NULL, 0, &duplicate) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(!duplicate);
  gotel_scope_t scope;
  gotel_begin(host, &scope, 0, NULL);
  CHECK(gotel_sqlite_prepare_db(db, queries[0].sql, &statement) == SQLITE_OK);
  CHECK(sqlite3_step(statement) == SQLITE_ROW);
  CHECK(sqlite3_step(statement) == SQLITE_DONE);
  CHECK(sqlite3_finalize(statement) == SQLITE_OK);
  CHECK(gotel_sqlite_prepare(attachment, queries[1].sql, &statement) == SQLITE_ERROR);
  CHECK(!statement && sqlite3_errcode(db) == SQLITE_ERROR);
  /* The exact byte bound has no NUL terminator and must not read past input. */
  const char bounded[] = {'S', 'E', 'L', 'E', 'C', 'T', ' ', '\''};
  const char *tail = NULL;
  CHECK(gotel_sqlite_prepare_v3(attachment, bounded, sizeof(bounded), SQLITE_PREPARE_PERSISTENT, &statement, &tail) ==
        SQLITE_ERROR);
  CHECK(!statement && tail >= bounded && tail <= bounded + sizeof(bounded));
  CHECK(gotel_sqlite_prepare_v2(attachment, "SELECT 2; SELECT 3", -1, &statement, &tail) == SQLITE_OK);
  CHECK(tail && !strcmp(tail, " SELECT 3"));
  CHECK(sqlite3_finalize(statement) == SQLITE_OK); /* never stepped: no PROFILE span */
  gotel_end(&scope);
  gotel_sqlite_detach(attachment);
  CHECK(gotel_sqlite_prepare_db(db, "SELECT 'DETACHED_SECRET' FROM no_table", &statement) == SQLITE_ERROR);
  CHECK(gotel_sqlite_attach(host, db, "fixture.storage", queries, 2, &attachment) == OTLP_OK);
  CHECK(sqlite3_close(db) == SQLITE_OK); /* CLOSE removes registry entry */
  CHECK(sqlite3_open(":memory:", &db) == SQLITE_OK);
  CHECK(gotel_sqlite_prepare_db(db, "SELECT 'UNATTACHED_SECRET' FROM no_table", &statement) == SQLITE_ERROR);
  CHECK(sqlite3_close(db) == SQLITE_OK);
  CHECK(gotel_sqlite_prepare(NULL, "SELECT 1", &statement) == SQLITE_MISUSE);
  CHECK(gotel_drain(host) == OTLP_OK);
  gotel_destroy(host);
  return 0;
}
