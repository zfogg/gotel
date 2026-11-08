/** @file clickhouse.h
 * @brief ClickHouse query scopes for native C and HTTP transports.
 */
#pragma once

#include <gotel/gotel.h>
#include <gotel/sql.h>

#ifdef __cplusplus
extern "C" {
#endif

/** Borrowed per-call identity and privacy options. SQL is always analyzed with
 * the ClickHouse dialect. Never supply credentials in server_address. */
typedef struct {
  const char *namespace_name;                /**< Stable database name; not a connection URL. */
  const char *server_address;                /**< Optional hostname only, without credentials. */
  unsigned server_port;                      /**< Optional native/HTTP port; zero omits it. */
  size_t bind_count;                         /**< Supplied argument count only; never argument values. */
  const sequel_analysis_options_t *analysis; /**< Optional per-call native budgets/privacy policy. */
} gotel_clickhouse_options_t;

/** Begin a registered, source-named query operation. Template/analysis options
 * are needed only during this call; sanitized attributes own their storage.
 * Scopes follow gotel_begin's same-thread/LIFO lifetime rules. Keep a streaming
 * query open through EOF or explicit cleanup. Analysis failure never prevents
 * the caller from executing its native query. NULL host creates an inactive scope. */
void gotel_clickhouse_begin(gotel_host_t *host, gotel_scope_t *scope, size_t operation, const gotel_context_t *parent,
                            const char *statement, const gotel_clickhouse_options_t *options);

/** Record the native result code and explicit outcome, then finish once. Native
 * exception messages and response bodies are never accepted or exported. */
void gotel_clickhouse_end(gotel_scope_t *scope, int result_code, gotel_outcome_t outcome);

/** Native call adapter. The caller owns connection/arguments/results inside
 * userdata; Gotel passes the same pointer once and returns the integer unchanged. */
typedef int (*gotel_clickhouse_call_t)(void *userdata);

/** Optional result classification. It affects telemetry only, never native codes.
 * Supply this for clients whose success code is not zero or for resumable work. */
typedef gotel_outcome_t (*gotel_clickhouse_classify_t)(int result_code, void *userdata);

/** Trace one synchronous native/HTTP call, including the callback's response
 * consumption. A non-NULL call is required; NULL returns -1 without execution.
 * Default classification is zero=success, otherwise error. For HTTP, the
 * callback must account for server errors even when transport/HTTP status is
 * successful; Gotel never reads or replaces a libcurl/native response callback. */
int gotel_clickhouse_call(gotel_host_t *host, size_t operation, const gotel_context_t *parent, const char *statement,
                          const gotel_clickhouse_options_t *options, gotel_clickhouse_call_t call, void *userdata,
                          gotel_clickhouse_classify_t classify);

#ifdef __cplusplus
}
#endif
