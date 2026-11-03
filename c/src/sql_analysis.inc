/* Unified SQL transport. All JSON contains canonical prose or numeric metadata. */
#include <stdarg.h>

#define GOTEL_SQL_ATTRIBUTE_MAX (16u * 1024u)

typedef struct {
  char data[GOTEL_SQL_ATTRIBUTE_MAX + 1];
  size_t length;
  bool overflow;
} sql_json_t;

static void sql_json_add(sql_json_t *json, const char *format, ...) {
  if (json->overflow)
    return;
  va_list args;
  va_start(args, format);
  int n = vsnprintf(json->data + json->length, sizeof(json->data) - json->length, format, args);
  va_end(args);
  if (n < 0 || (size_t)n >= sizeof(json->data) - json->length) {
    json->overflow = true;
    return;
  }
  json->length += (size_t)n;
}

static void sql_map_json(sql_json_t *json, const sequel_source_map_t *map) {
  static const char *kinds[] = {"copy", "replace", "delete", "insert"};
  sql_json_add(json, "{\"from_length\":%zu,\"to_length\":%zu,\"segments\":[", map->from_length, map->to_length);
  for (size_t i = 0; i < map->count && !json->overflow; ++i) {
    const sequel_map_segment_t *s = &map->segments[i];
    if ((unsigned)s->kind >= sizeof(kinds) / sizeof(kinds[0])) {
      json->overflow = true;
      return;
    }
    sql_json_add(json, "%s{\"from_start\":%zu,\"from_end\":%zu,\"to_start\":%zu,\"to_end\":%zu,\"kind\":\"%s\"}",
                 i ? "," : "", s->from_start, s->from_end, s->to_start, s->to_end, kinds[s->kind]);
  }
  sql_json_add(json, "]}");
}

static char *sql_diagnostic_json(const sequel_diagnostic_t *d) {
  size_t capacity = 6 * (sizeof(d->message) + sizeof(d->explanation) + sizeof(d->hint)) + 1024;
  char *json = malloc(capacity);
  if (!json)
    return NULL;
  const char *severity = d->severity == SEQUEL_WARNING ? "warning" : d->severity == SEQUEL_NOTE ? "note" : "error";
  int n =
      snprintf(json, capacity,
               "{\"code\":\"%s\",\"severity\":\"%s\",\"offset\":%zu,\"end_offset\":%zu,"
               "\"line\":%zu,\"column\":%zu,\"end_line\":%zu,\"end_column\":%zu,"
               "\"incomplete\":%s,\"statement_complete\":%s,\"message\":",
               sequel_diagnostic_code_name(d->code), severity, d->offset, d->end_offset, d->line, d->column,
               d->end_line, d->end_column, d->incomplete ? "true" : "false", d->statement_complete ? "true" : "false");
  char *cursor = json + n;
  json_string(&cursor, d->message);
  cursor += sprintf(cursor, ",\"explanation\":");
  json_string(&cursor, d->explanation);
  cursor += sprintf(cursor, ",\"hint\":");
  json_string(&cursor, d->hint);
  strcpy(cursor, "}");
  return json;
}

static void sql_analysis_diagnostics(otlp_span_t *span, const sequel_analysis_t *a) {
  /* A transport bound does not change analysis validity or source ranges. */
  otlp_value_t items[64] = {0};
  char *strings[64] = {0};
  size_t count = 0, bytes = 0;
  for (size_t i = 0; i < a->diagnostic_count; ++i) {
    sequel_diagnostic_t canonical = {0};
    if (sequel_diagnostic_canonicalize(SEQUEL_DIAGNOSTIC_VERSION, a->original_length, &a->diagnostics[i], &canonical) !=
        SEQUEL_OK) {
      otlp_span_set_attribute_bool(span, "gotel.db.query.diagnostic_validation_error", true);
      continue;
    }
    if (count == sizeof(items) / sizeof(items[0]))
      break;
    char *value = sql_diagnostic_json(&canonical);
    if (!value)
      break;
    size_t length = strlen(value);
    if (bytes + length > GOTEL_SQL_ATTRIBUTE_MAX) {
      free(value);
      break;
    }
    strings[count] = value;
    items[count].type = OTLP_VALUE_STRING;
    items[count].v.string_val = value;
    bytes += length;
    ++count;
  }
  otlp_span_set_attribute_array(span, "db.query.diagnostics", items, count);
  if (count < a->diagnostic_count)
    otlp_span_set_attribute_bool(span, "gotel.db.query.diagnostics_omitted", true);
  for (size_t i = 0; i < count; ++i)
    free(strings[i]);
}

void gotel_sql_analysis_attributes(otlp_span_t *span, const sequel_analysis_t *a, const char *system, const char *ns,
                                   size_t binds) {
  gotel_sql_analysis_attributes_with_limit(span, a, system, ns, binds, SEQUEL_TEXT_MAX);
}

void gotel_sql_analysis_attributes_with_limit(otlp_span_t *span, const sequel_analysis_t *a, const char *system,
                                              const char *ns, size_t binds, size_t max_text_bytes) {
  if (!span || !a)
    return;
  const char *text = a->text ? a->text : "";
  const char *display = "sanitized";
  size_t limit = max_text_bytes ? max_text_bytes : SEQUEL_TEXT_MAX;
  if (a->formatted && *a->formatted && strlen(a->formatted) <= limit) {
    text = a->formatted;
    display = "formatted";
  }
  if (*text)
    otlp_span_set_attribute_string(span, "db.query.text", text);
  else
    otlp_span_set_attribute_string(span, "gotel.db.query.text_reason",
                                   a->omission_reason && *a->omission_reason ? a->omission_reason : "empty");
  otlp_span_set_attribute_string(span, "db.query.summary", a->summary && *a->summary ? a->summary : "SQL");
  otlp_span_set_attribute_string(span, "gotel.db.query.analysis_status", a->status ? a->status : "error");
  otlp_span_set_attribute_int(span, "gotel.db.query.original_length", (int64_t)a->original_length);
  otlp_span_set_attribute_int(span, "gotel.db.query.diagnostic_schema_version", SEQUEL_DIAGNOSTIC_VERSION);
  if (*a->fingerprint)
    otlp_span_set_attribute_string(span, "gotel.db.query.fingerprint", a->fingerprint);
  if (*a->operation)
    otlp_span_set_attribute_string(span, "db.operation.name", a->operation);
  if (a->truncated)
    otlp_span_set_attribute_bool(span, "gotel.db.query.truncated", true);
  if (system && *system)
    otlp_span_set_attribute_string(span, "db.system.name", system);
  if (ns && *ns)
    otlp_span_set_attribute_string(span, "db.namespace", ns);
  otlp_span_set_attribute_int(span, "gotel.db.query.bind_count", (int64_t)binds);
  otlp_span_set_attribute_int(span, "gotel.db.query.statement_count", (int64_t)a->statement_count);
  sql_analysis_diagnostics(span, a);
  if (*text && a->original_to_sanitized.segments) {
    sql_json_t json = {0};
    sql_json_add(&json, "{\"schema_version\":%u,\"original_to_sanitized\":", a->schema_version);
    sql_map_json(&json, &a->original_to_sanitized);
    sql_json_add(&json, ",\"sanitized_to_formatted\":");
    sql_map_json(&json, &a->sanitized_to_formatted);
    sql_json_add(&json, ",\"display\":\"%s\"}", display);
    if (json.overflow)
      otlp_span_set_attribute_bool(span, "gotel.db.query.source_map_omitted", true);
    else
      otlp_span_set_attribute_string(span, "gotel.db.query.source_map", json.data);
  }
  if (a->binds) {
    static const char *styles[] = {"positional", "numbered", "named", "typed"};
    sql_json_t json = {0};
    sql_json_add(&json, "[");
    for (size_t i = 0; i < a->bind_count && !json.overflow; ++i) {
      const sequel_bind_descriptor_t *b = &a->binds[i];
      if ((unsigned)b->style >= sizeof(styles) / sizeof(styles[0])) {
        json.overflow = true;
        break;
      }
      sql_json_add(
          &json,
          "%s{\"ordinal\":%zu,\"slot\":%zu,\"statement_index\":%zu,\"offset\":%zu,\"end_offset\":%zu,\"style\":\"%s\"}",
          i ? "," : "", b->ordinal, b->slot, b->statement_index, b->offset, b->end_offset, styles[b->style]);
    }
    sql_json_add(&json, "]");
    if (!json.overflow)
      otlp_span_set_attribute_string(span, "gotel.db.query.binds", json.data);
  }
}
