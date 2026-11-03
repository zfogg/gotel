#include <gotel/sql.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* Diagnostic messages are library-owned explanations, never SQL snippets. */
static void json_string(char **out, const char *value) {
  *(*out)++ = '"';
  for (const unsigned char *p = (const unsigned char *)value; *p; ++p) {
    if (*p == '"' || *p == '\\') {
      *(*out)++ = '\\';
      *(*out)++ = (char)*p;
    } else if (*p < 32) {
      *out += sprintf(*out, "\\u%04x", (unsigned)*p);
    } else {
      *(*out)++ = (char)*p;
    }
  }
  *(*out)++ = '"';
  **out = 0;
}

#include "sql_analysis.inc"

void gotel_sql_diagnostics(otlp_span_t *span, const char *sql, sequel_dialect_t dialect) {
  if (!span)
    return;
  sequel_diagnostic_t d = {0};
  sequel_diagnose(sql, dialect, &d);
  otlp_value_t item = {.type = OTLP_VALUE_STRING};
  if (d.code == SEQUEL_DIAG_NONE) {
    otlp_span_set_attribute_array(span, "db.query.diagnostics", &item, 0);
    return;
  }
  /* Sixfold escaping bounds all three fixed-size diagnostic strings. */
  char json[6 * (sizeof(d.message) + sizeof(d.explanation) + sizeof(d.hint)) + 1024];
  const char *severity = d.severity == SEQUEL_WARNING ? "warning" : d.severity == SEQUEL_NOTE ? "note" : "error";
  const char *code = sequel_diagnostic_code_name(d.code);
  int n = snprintf(json, sizeof(json),
                   "{\"code\":\"%s\",\"severity\":\"%s\",\"offset\":%zu,\"end_offset\":%zu,"
                   "\"line\":%zu,\"column\":%zu,\"end_line\":%zu,\"end_column\":%zu,"
                   "\"incomplete\":%s,\"statement_complete\":%s,\"message\":",
                   code ? code : "unknown", severity, d.offset, d.end_offset, d.line, d.column, d.end_line,
                   d.end_column, d.incomplete ? "true" : "false", d.statement_complete ? "true" : "false");
  char *cursor = json + n;
  json_string(&cursor, d.message);
  cursor += sprintf(cursor, ",\"explanation\":");
  json_string(&cursor, d.explanation);
  cursor += sprintf(cursor, ",\"hint\":");
  json_string(&cursor, d.hint);
  strcpy(cursor, "}");
  item.v.string_val = json;
  otlp_span_set_attribute_array(span, "db.query.diagnostics", &item, 1);
}

void gotel_sql_attributes(otlp_span_t *span, const sequel_metadata_t *m, const char *system, const char *ns,
                          size_t binds) {
  gotel_sql_attributes_with_limit(span, m, system, ns, binds, SEQUEL_TEXT_MAX);
}
void gotel_sql_attributes_with_limit(otlp_span_t *span, const sequel_metadata_t *m, const char *system, const char *ns,
                                     size_t binds, size_t max_text_bytes) {
  if (!span || !m)
    return;
  /* Processed/truncated text cannot stand in for the original SQL. */
  gotel_sql_diagnostics(span, "", SEQUEL_GENERIC);
  otlp_span_set_attribute_string(span, "db.query.summary", m->summary ? m->summary : "SQL");
  if (m->text && *m->text) {
    char *formatted = NULL;
    size_t length = 0;
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
    if (!m->truncated && sequel_format(query, dialect, NULL, 0, &length) == SEQUEL_OK && length <= limit) {
      formatted = malloc(length + 1);
      if (formatted && sequel_format(query, dialect, formatted, length + 1, &length) == SEQUEL_OK)
        query = formatted;
    }
    otlp_span_set_attribute_string(span, "db.query.text", query);
    free(formatted);
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
