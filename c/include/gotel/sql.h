/** @file sql.h
 * @brief Public Gotel SQL span attributes.
 */
#pragma once

#include <otlp-c/otlp.h>
#include <sequel.h>
#ifdef __cplusplus
extern "C" {
#endif

/** Analyze original SQL into db.query.diagnostics, independently of query-text
 * suppression. Clean SQL emits an empty array; issues are JSON string array
 * entries containing source ranges and explanations, never SQL or bind values.
 * Call after gotel_sql_attributes, which initializes an empty diagnostics array
 * because processed or truncated text cannot describe the original SQL. */
void gotel_sql_diagnostics(otlp_span_t *span, const char *sql, sequel_dialect_t dialect);

/** Attach inspected SQL metadata and initialize an empty diagnostics array. */
void gotel_sql_attributes(otlp_span_t *span, const sequel_metadata_t *metadata, const char *system,
                          const char *namespace_name, size_t bind_count);

/** Preserve a custom collection byte limit when formatting expands SQL.
 * Zero uses SEQUEL_TEXT_MAX. */
void gotel_sql_attributes_with_limit(otlp_span_t *span, const sequel_metadata_t *metadata, const char *system,
                                     const char *namespace_name, size_t bind_count, size_t max_text_bytes);

#ifdef __cplusplus
}
#endif
