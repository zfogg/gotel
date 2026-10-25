#ifndef GOTEL_SQL_H
#define GOTEL_SQL_H
#include <otlp-c/otlp.h>
#include <sequel.h>
#ifdef __cplusplus
extern "C" {
#endif
void gotel_sql_attributes(otlp_span_t *span, const sequel_metadata_t *metadata, const char *system,
                          const char *namespace_name, size_t bind_count);
/* Preserve a custom collection byte limit when formatting expands SQL.
 * Zero uses SEQUEL_TEXT_MAX. */
void gotel_sql_attributes_with_limit(otlp_span_t *span, const sequel_metadata_t *metadata, const char *system,
                                     const char *namespace_name, size_t bind_count, size_t max_text_bytes);
#ifdef __cplusplus
}
#endif
#endif
