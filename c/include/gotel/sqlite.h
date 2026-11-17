/** @file sqlite.h
 * @brief Public SQLite integration API for gotel.
 */
#pragma once

#include <gotel/gotel.h>
#include <sequel.h>
#include <sqlite3.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct { const char *name; const char *sql; } gotel_query_t;
typedef bool (*gotel_query_get_fn)(const void *, size_t, gotel_query_t *);
typedef struct { const void *context; size_t count; gotel_query_get_fn get; } gotel_query_catalog_t;
typedef struct gotel_sqlite gotel_sqlite_t;
typedef struct { const char *namespace_name; sequel_options_t sql; const sequel_analysis_options_t *analysis; } gotel_sqlite_options_t;
otlp_status_t gotel_sqlite_attach(gotel_host_t *, sqlite3 *, const char *, const gotel_query_t *, size_t, gotel_sqlite_t **);
otlp_status_t gotel_sqlite_attach_with_options(gotel_host_t *, sqlite3 *, const char *, const gotel_query_t *, size_t, const gotel_sqlite_options_t *, gotel_sqlite_t **);
otlp_status_t gotel_sqlite_attach_catalog_with_options(gotel_host_t *, sqlite3 *, const char *, const gotel_query_catalog_t *, const gotel_sqlite_options_t *, gotel_sqlite_t **);
void gotel_sqlite_detach(gotel_sqlite_t *);
int gotel_sqlite_prepare(gotel_sqlite_t *, const char *, sqlite3_stmt **);
int gotel_sqlite_prepare_v2(gotel_sqlite_t *, const char *, int, sqlite3_stmt **, const char **);
int gotel_sqlite_prepare_v3(gotel_sqlite_t *, const char *, int, unsigned, sqlite3_stmt **, const char **);
int gotel_sqlite_prepare_db(sqlite3 *, const char *, sqlite3_stmt **);
const gotel_query_t *gotel_query_find(const gotel_query_t *, size_t, const char *);

#ifdef __cplusplus
}
#endif
