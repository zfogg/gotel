#include <gotel/clickhouse.h>
#include <string.h>

void gotel_clickhouse_begin(gotel_host_t *host, gotel_scope_t *scope, size_t operation, const gotel_context_t *parent,
                            const char *statement, const gotel_clickhouse_options_t *options) {
  if (!scope)
    return;
  gotel_begin(host, scope, operation, parent);
  if (!scope->active || !scope->span)
    return;
  sequel_analysis_options_t policy = {0};
  if (options && options->analysis)
    policy = *options->analysis;
  else {
    policy.format = true;
    policy.omit_source_maps = true;
    policy.omit_bind_descriptors = true;
  }
  policy.dialect = SEQUEL_CLICKHOUSE;
  sequel_analysis_t analysis = {0};
  sequel_analyze(statement, &policy, &analysis);
  gotel_sql_analysis_attributes_with_limit(scope->span, &analysis, "clickhouse",
                                           options ? options->namespace_name : NULL, options ? options->bind_count : 0,
                                           policy.max_query_text_bytes);
  sequel_analysis_free(&analysis);
  if (options && options->server_address)
    gotel_attr_string(scope, "server.address", options->server_address);
  if (options && options->server_port)
    gotel_attr_int(scope, "server.port", options->server_port);
}

void gotel_clickhouse_end(gotel_scope_t *scope, int result_code, gotel_outcome_t outcome) {
  if (!scope || !scope->active)
    return;
  gotel_attr_int(scope, "db.response.status_code", result_code);
  gotel_outcome(scope, outcome, outcome == GOTEL_OUTCOME_ERROR ? "clickhouse.query" : NULL);
  gotel_end(scope);
}

int gotel_clickhouse_call(gotel_host_t *host, size_t operation, const gotel_context_t *parent, const char *statement,
                          const gotel_clickhouse_options_t *options, gotel_clickhouse_call_t call, void *userdata,
                          gotel_clickhouse_classify_t classify) {
  if (!call)
    return -1;
  gotel_scope_t scope;
  gotel_clickhouse_begin(host, &scope, operation, parent, statement, options);
  int result = call(userdata);
  gotel_outcome_t outcome = classify      ? classify(result, userdata)
                            : result == 0 ? GOTEL_OUTCOME_SUCCESS
                                          : GOTEL_OUTCOME_ERROR;
  gotel_clickhouse_end(&scope, result, outcome);
  return result;
}
