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

static bool cancelled(void *context) { return *(bool *)context; }

int main(int argc, char **argv) {
  CHECK(argc == 3);
  static const gotel_operation_t operations[] = {{"fixture.owner", false}};
  gotel_options_t options = {0};
  options.exporter.service_name = "sqlite-analysis";
  options.exporter.endpoint = "http://127.0.0.1:1/v1/traces";
  options.signals = GOTEL_TRACES;
  options.operations = operations;
  options.operation_count = 1;
  options.monotonic_ns = gotel_test_monotonic_ns;
  options.realtime_ns = gotel_test_realtime_ns;
  gotel_test_clock_set(1000000000, 1700000000000000000ULL);
  gotel_host_t *host = NULL;
  CHECK(gotel_test_create(&options, &host) == OTLP_OK);
  gotel_test_capture_t *capture = gotel_test_capture_create(1024 * 1024);
  CHECK(capture && gotel_test_capture_attach(host, capture) == OTLP_OK);
  bool stop = !strcmp(argv[1], "cancel");
  sequel_analysis_options_t analysis = {.format = true, .cancelled = cancelled, .cancel_userdata = &stop};
  if (!strcmp(argv[1], "memory"))
    analysis.max_memory_bytes = 1;
  if (!strcmp(argv[1], "work"))
    analysis.max_work_units = 1;
  if (!strcmp(argv[1], "output"))
    analysis.max_output_bytes = 1;
  gotel_sqlite_options_t sql_options = {.namespace_name = "fixture"};
  if (strcmp(argv[1], "default"))
    sql_options.analysis = &analysis;
  if (!strcmp(argv[1], "policy"))
    sql_options.sql.omit_query_text = true;
  if (!strcmp(argv[1], "truncate"))
    sql_options.sql.max_query_text_bytes = 12;
  sqlite3 *db = NULL;
  CHECK(sqlite3_open(":memory:", &db) == SQLITE_OK);
  static const gotel_query_t queries[] = {{"Lookup", "SELECT ?1 AS value, 'PRIVATE_LITERAL' AS token"},
                                          {"PrepareFailure", "SELECT 'PREPARE_SECRET' FROM missing_table"}};
  gotel_sqlite_t *attachment = NULL;
  CHECK(gotel_sqlite_attach_with_options(host, db, "storage.sqlite", queries, 2, &sql_options, &attachment) == OTLP_OK);
  analysis.max_memory_bytes = 1; /* attach copied the policy, not this pointer */
  gotel_scope_t owner;
  gotel_begin(host, &owner, 0, NULL);
  sqlite3_stmt *statement = NULL;
  CHECK(gotel_sqlite_prepare_db(db, queries[0].sql, &statement) == SQLITE_OK);
  CHECK(sqlite3_bind_text(statement, 1, "PRIVATE_BIND", -1, SQLITE_STATIC) == SQLITE_OK);
  CHECK(sqlite3_step(statement) == SQLITE_ROW);
  CHECK(!strcmp((const char *)sqlite3_column_text(statement, 0), "PRIVATE_BIND"));
  CHECK(sqlite3_step(statement) == SQLITE_DONE);
  CHECK(sqlite3_finalize(statement) == SQLITE_OK);
  CHECK(gotel_sqlite_prepare_db(db, queries[1].sql, &statement) == SQLITE_ERROR && !statement);
  gotel_end(&owner);
  gotel_sqlite_detach(attachment);
  CHECK(sqlite3_close(db) == SQLITE_OK);
  CHECK(gotel_drain(host) == OTLP_OK);
  gotel_test_capture_detach(host);
  const uint8_t *data = NULL;
  size_t bytes = 0;
  CHECK(gotel_test_capture_read(capture, &data, &bytes) == OTLP_OK);
  FILE *output = fopen(argv[2], "wb");
  CHECK(output && fwrite(data, 1, bytes, output) == bytes && fclose(output) == 0);
  gotel_test_capture_destroy(capture);
  gotel_destroy(host);
  return 0;
}
