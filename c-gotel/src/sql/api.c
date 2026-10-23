static void utf8_cut(char *s, size_t limit) {
  size_t n = strlen(s);
  if (n <= limit)
    return;
  limit = gotel_utf8_prefix(s, n, limit);
  s[limit] = 0;
  while (limit && isspace((unsigned char)s[limit - 1]))
    s[--limit] = 0;
}
static void collapse_in(char *s) {
  size_t n = strlen(s);
  for (size_t i = 0; i + 2 < n; ++i) {
    if (!equal(s + i, 2, "IN") || (i && part((unsigned char)s[i - 1])) || (i + 2 < n && part((unsigned char)s[i + 2])))
      continue;
    size_t j = i + 2;
    while (j < n && isspace((unsigned char)s[j]))
      ++j;
    if (j == n || s[j] != '(')
      continue;
    size_t k = j + 1, count = 0;
    bool comma = false;
    for (;;) {
      while (k < n && isspace((unsigned char)s[k]))
        ++k;
      if (k == n || s[k] != '?')
        break;
      ++count;
      ++k;
      while (k < n && isspace((unsigned char)s[k]))
        ++k;
      if (k < n && s[k] == ',') {
        comma = true;
        ++k;
        continue;
      }
      comma = false;
      break;
    }
    if (count > 1 && !comma && k < n && s[k] == ')') {
      memmove(s + j + 3, s + k + 1, n - k);
      memcpy(s + j, "(?)", 3);
      n = strlen(s);
    }
  }
}
otlp_status_t gotel_sql_sanitize(const char *sql, gotel_sql_dialect_t dialect, bool collapse_in_lists, size_t limit,
                                 char *out, size_t capacity, bool *truncated) {
  if (!sql || !out || !truncated)
    return OTLP_ERR_NULL;
  *truncated = false;
  if (capacity)
    *out = 0;
  if (dialect < GOTEL_SQL_GENERIC || dialect > GOTEL_SQL_MYSQL || capacity == 0)
    return OTLP_ERR_INVALID_ARGUMENT;
  if (!limit)
    limit = GOTEL_SQL_TEXT_MAX;
  if (limit >= capacity)
    return OTLP_ERR_INVALID_ARGUMENT;
  size_t n = 0;
  while (n <= INPUT_MAX && sql[n])
    ++n;
  if (n > INPUT_MAX)
    return OTLP_ERR_OVERFLOW;
  if (!gotel_utf8_valid(sql, n))
    return OTLP_ERR_INVALID_ARGUMENT;
  char *safe = calloc(n + 1, 1);
  if (!safe)
    return OTLP_ERR_NOMEM;
  output text_output = {safe, 0, n + 1};
  otlp_status_t status = OTLP_ERR_INVALID_ARGUMENT;
  if (!sanitize(sql, n, dialect, false, &text_output))
    goto done;
  if (collapse_in_lists)
    collapse_in(safe);
  *truncated = strlen(safe) > limit;
  utf8_cut(safe, limit);
  memcpy(out, safe, strlen(safe) + 1);
  status = OTLP_OK;
done:
  free(safe);
  return status;
}
otlp_status_t gotel_sql_inspect(const char *sql, const gotel_sql_options_t *options, gotel_sql_metadata_t *out) {
  if (!out)
    return OTLP_ERR_NULL;
  memset(out, 0, sizeof(*out));
  strcpy(out->summary, "SQL");
  if (!sql)
    return OTLP_ERR_NULL;
  gotel_sql_options_t o = options ? *options : (gotel_sql_options_t){0};
  if (o.dialect > GOTEL_SQL_MYSQL || o.dialect < GOTEL_SQL_GENERIC || o.max_query_text_bytes > GOTEL_SQL_TEXT_MAX)
    return OTLP_ERR_INVALID_ARGUMENT;
  size_t n = 0;
  while (n <= INPUT_MAX && sql[n])
    ++n;
  if (n > INPUT_MAX)
    return OTLP_ERR_OVERFLOW;
  if (!gotel_utf8_valid(sql, n))
    return OTLP_ERR_INVALID_ARGUMENT;
  char *safe = calloc(n + 1, 1), *shape = calloc(n + 1, 1);
  token *tokens = calloc(n < TOKEN_MAX ? n + 1 : TOKEN_MAX, sizeof(*tokens));
  if (!safe || !shape || !tokens) {
    free(safe);
    free(shape);
    free(tokens);
    return OTLP_ERR_NOMEM;
  }
  output a = {safe, 0, n + 1}, b = {shape, 0, n + 1};
  otlp_status_t status = OTLP_ERR_INVALID_ARGUMENT;
  if (!sanitize(sql, n, o.dialect, false, &a) || !sanitize(sql, n, o.dialect, true, &b))
    goto done;
  size_t count = tokenize(shape, tokens, n < TOKEN_MAX ? n + 1 : TOKEN_MAX, o.dialect);
  if (!count) {
    status = *safe ? OTLP_ERR_OVERFLOW : OTLP_OK;
    goto done;
  }
  output summary = {out->summary, 0, sizeof(out->summary)};
  *out->summary = 0;
  for (size_t start = 0; start < count;) {
    size_t end = start, nesting = 0;
    for (; end < count; ++end) {
      if (sym(tokens[end], '('))
        ++nesting;
      else if (sym(tokens[end], ')') && nesting)
        --nesting;
      else if (!nesting && sym(tokens[end], ';'))
        break;
    }
    if (end > start) {
      ++out->statement_count;
      token *statement = tokens + start;
      size_t length = end - start;
      if (!*out->operation) {
        size_t op = 0;
        while (op < length && sym(statement[op], '('))
          ++op;
        if (kw(statement[0], "WITH"))
          for (op = 1; op < length; ++op) {
            if (sym(statement[op], '(')) {
              op = close_group(statement, length, op);
              continue;
            }
            if (kw(statement[op], "SELECT") || kw(statement[op], "INSERT") || kw(statement[op], "UPDATE") ||
                kw(statement[op], "DELETE") || kw(statement[op], "MERGE") || kw(statement[op], "VALUES"))
              break;
          }
        if (op < length && statement[op].word) {
          output p = {out->operation, 0, sizeof(out->operation)};
          upper(&p, statement[op]);
        }
      }
      if (summary.n)
        text(&summary, "; ");
      summarize(&summary, statement, length, 0);
    }
    start = end < count ? end + 1 : count;
  }
  if (!*out->summary)
    strcpy(out->summary, "SQL");
  if (summary.overflow) {
    utf8_cut(out->summary, GOTEL_SQL_SUMMARY_MAX - 3);
    strcat(out->summary, "…");
  }
  if (!o.keep_in_lists)
    collapse_in(safe);
  if (!o.omit_query_text) {
    size_t limit = o.max_query_text_bytes ? o.max_query_text_bytes : GOTEL_SQL_TEXT_MAX;
    out->truncated = strlen(safe) > limit;
    utf8_cut(safe, limit);
    strcpy(out->text, safe);
  }
  status = OTLP_OK;
done:
  free(safe);
  free(shape);
  free(tokens);
  return status;
}
#ifndef CGOTEL_SQL_STANDALONE
void gotel_sql_attributes(otlp_span_t *span, const gotel_sql_metadata_t *m, const char *system, const char *ns,
                          size_t binds) {
  if (!span || !m)
    return;
  otlp_span_set_attribute_string(span, "db.query.summary", m->summary);
  if (*m->text)
    otlp_span_set_attribute_string(span, "db.query.text", m->text);
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
#endif
