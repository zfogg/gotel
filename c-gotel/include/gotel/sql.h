#ifndef CGOTEL_SQL_H
#define CGOTEL_SQL_H
#include <otlp-c/otlp.h>
#ifdef __cplusplus
extern "C" {
#endif
#define GOTEL_SQL_TEXT_MAX 4096
#define GOTEL_SQL_SUMMARY_MAX 4096
typedef enum {
  GOTEL_SQL_GENERIC,
  GOTEL_SQL_SQLITE,
  GOTEL_SQL_POSTGRESQL,
  GOTEL_SQL_DUCKDB,
  GOTEL_SQL_MYSQL
} gotel_sql_dialect_t;
typedef struct {
  gotel_sql_dialect_t dialect;
  /* Zero uses 4096. Values above 4096 are rejected. */
  size_t max_query_text_bytes;
  bool omit_query_text;
  bool keep_in_lists;
} gotel_sql_options_t;
typedef struct {
  char text[GOTEL_SQL_TEXT_MAX + 1];
  char summary[GOTEL_SQL_SUMMARY_MAX + 1];
  char operation[32];
  bool truncated;
} gotel_sql_metadata_t;
/* Dialect-independent inspection, usable without SQLite. Never reads bind
 * values. Malformed quoting fails closed: text is empty and summary is SQL.
 * Input is limited to 1 MiB and nesting to 64 levels. Output is UTF-8 bounded. */
otlp_status_t gotel_sql_inspect(const char *sql, const gotel_sql_options_t *options, gotel_sql_metadata_t *out);
void gotel_sql_attributes(otlp_span_t *span, const gotel_sql_metadata_t *metadata, const char *system,
                          const char *namespace_name, size_t bind_count);
#ifdef __cplusplus
}
#endif
#endif
