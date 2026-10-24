#ifndef CGOTEL_SQL_H
#define CGOTEL_SQL_H
#ifdef CGOTEL_SQL_STANDALONE
#include <stdbool.h>
#include <stddef.h>
typedef enum { OTLP_OK = 0, OTLP_ERR_NULL, OTLP_ERR_INVALID_ARGUMENT, OTLP_ERR_OVERFLOW, OTLP_ERR_NOMEM } otlp_status_t;
#else
#include <otlp-c/otlp.h>
#endif
#ifdef __cplusplus
extern "C" {
#endif
#define GOTEL_SQL_TEXT_MAX 32768
#define GOTEL_SQL_SUMMARY_MAX 32768
typedef enum {
  GOTEL_SQL_GENERIC,
  GOTEL_SQL_SQLITE,
  GOTEL_SQL_POSTGRESQL,
  GOTEL_SQL_DUCKDB,
  GOTEL_SQL_MYSQL
} gotel_sql_dialect_t;
typedef struct {
  gotel_sql_dialect_t dialect;
  /* Zero uses 32768. Values above 32768 are rejected. */
  size_t max_query_text_bytes;
  bool omit_query_text;
  bool keep_in_lists;
} gotel_sql_options_t;
typedef struct {
  char text[GOTEL_SQL_TEXT_MAX + 1];
  char summary[GOTEL_SQL_SUMMARY_MAX + 1];
  char operation[32];
  bool truncated;
  size_t statement_count;
} gotel_sql_metadata_t;
/* Dialect-independent inspection, usable without SQLite. Never reads bind
 * values. Malformed quoting fails closed: text is empty and summary is SQL.
 * Input is limited to 1 MiB and nesting to 64 levels. Output is UTF-8 bounded. */
otlp_status_t gotel_sql_inspect(const char *sql, const gotel_sql_options_t *options, gotel_sql_metadata_t *out);
/* Sanitizes without the fixed metadata text cap. capacity includes the NUL
 * terminator; limit is the maximum emitted bytes (zero uses 32768). */
otlp_status_t gotel_sql_sanitize(const char *sql, gotel_sql_dialect_t dialect, bool collapse_in_lists, size_t limit,
                                 char *out, size_t capacity, bool *truncated);
/* Layout-only SQL formatting. Comments, literals, identifiers and bind markers
 * are preserved verbatim. No SQL is executed or sanitized. Input is UTF-8,
 * limited to 1 MiB / 65536 tokens / 64 nesting levels. Not a SQL validator.
 * length receives the output byte count (excluding NUL). Pass out=NULL and
 * capacity=0 to measure. On failure a supplied output buffer is emptied. */
otlp_status_t gotel_sql_format(const char *sql, gotel_sql_dialect_t dialect, char *out, size_t capacity,
                               size_t *length);
#ifndef CGOTEL_SQL_STANDALONE
void gotel_sql_attributes(otlp_span_t *span, const gotel_sql_metadata_t *metadata, const char *system,
                          const char *namespace_name, size_t bind_count);
/* Preserve a custom collection byte limit when formatting expands SQL.
 * Zero uses GOTEL_SQL_TEXT_MAX. */
void gotel_sql_attributes_with_limit(otlp_span_t *span, const gotel_sql_metadata_t *metadata, const char *system,
                                     const char *namespace_name, size_t bind_count, size_t max_text_bytes);
#endif
#ifdef __cplusplus
}
#endif
#endif
