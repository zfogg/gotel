#include <gotel/sql.h>
#include <string.h>

void gotel_sql_attributes(otlp_span_t *span, const sequel_metadata_t *m, const char *system, const char *ns,
                          size_t binds) {
  gotel_sql_attributes_with_limit(span, m, system, ns, binds, SEQUEL_TEXT_MAX);
}
void gotel_sql_attributes_with_limit(otlp_span_t *span, const sequel_metadata_t *m, const char *system, const char *ns,
                                     size_t binds, size_t max_text_bytes) {
  if (!span || !m)
    return;
  otlp_span_set_attribute_string(span, "db.query.summary", m->summary);
  if (*m->text) {
    char formatted[SEQUEL_TEXT_MAX + 1];
    size_t length;
    const char *query = m->text;
    sequel_dialect_t dialect = SEQUEL_GENERIC;
    if (system && !strcmp(system, "postgresql"))
      dialect = SEQUEL_POSTGRESQL;
    else if (system && !strcmp(system, "duckdb"))
      dialect = SEQUEL_DUCKDB;
    else if (system && !strcmp(system, "mysql"))
      dialect = SEQUEL_MYSQL;
    else if (system && !strcmp(system, "sqlite"))
      dialect = SEQUEL_SQLITE;
    size_t limit = max_text_bytes && max_text_bytes < SEQUEL_TEXT_MAX ? max_text_bytes : SEQUEL_TEXT_MAX;
    if (system && strcmp(system, "clickhouse") == 0)
      dialect = SEQUEL_CLICKHOUSE;
    if (!m->truncated && sequel_format(query, dialect, formatted, limit + 1, &length) == SEQUEL_OK)
      query = formatted;
    otlp_span_set_attribute_string(span, "db.query.text", query);
  }
  if (*m->operation)
    otlp_span_set_attribute_string(span, "db.operation.name", m->operation);
  if (m->truncated)
    otlp_span_set_attribute_bool(span, "gotel.db.query.truncated", true);
  if (system && *system)
    otlp_span_set_attribute_string(span, "db.system.name", system);
  if (ns && *ns)
    otlp_span_set_attribute_string(span, "db.namespace", ns);
  otlp_span_set_attribute_int(span, "gotel.db.query.bind_count", (int64_t)binds);
  otlp_span_set_attribute_int(span, "gotel.db.query.statement_count", (int64_t)m->statement_count);
}
