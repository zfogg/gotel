/* Preserve lexical contents; only whitespace outside lexical tokens changes.
 * Keeping the formatter independent of a grammar also supports driver/sqlc
 * parameters and sanitized telemetry templates that aren't executable SQL. */
typedef struct {
  token t;
  bool gap, newline;
  int kind; /* 0 punctuation, 1 word, 2 quoted, 3 line comment, 4 block comment */
} format_token;

static size_t format_scan(const char *s, size_t n, gotel_sql_dialect_t d, format_token *ts) {
  size_t count = 0, i = 0;
  while (i < n) {
    bool gap = false, newline = false;
    while (i < n && isspace((unsigned char)s[i])) {
      gap = true;
      newline |= s[i] == '\n' || s[i] == '\r';
      ++i;
    }
    if (i == n)
      break;
    if (count == TOKEN_MAX)
      return SIZE_MAX;
    size_t start = i, end = i + 1;
    int kind = 0;
    unsigned char c = (unsigned char)s[i];
    if ((c == '-' && end < n && s[end] == '-') || (c == '#' && d == GOTEL_SQL_MYSQL)) {
      while (end < n && s[end] != '\n' && s[end] != '\r')
        ++end;
      kind = 3;
    } else if (c == '/' && end < n && s[end] == '*') {
      int level = 1;
      end++;
      while (end < n && level) {
        if (end + 1 < n && s[end] == '/' && s[end + 1] == '*') {
          if (++level > 64)
            return SIZE_MAX;
          end += 2;
        } else if (end + 1 < n && s[end] == '*' && s[end + 1] == '/') {
          --level;
          end += 2;
        } else
          ++end;
      }
      if (level)
        return SIZE_MAX;
      kind = 4;
    } else {
      /* PostgreSQL dollar strings must be recognized before $parameters. */
      if (c == '$' && (d == GOTEL_SQL_POSTGRESQL || d == GOTEL_SQL_DUCKDB || d == GOTEL_SQL_GENERIC)) {
        size_t tag = i + 1;
        if (tag < n && letter((unsigned char)s[tag])) {
          ++tag;
          while (tag < n && (letter((unsigned char)s[tag]) || isdigit((unsigned char)s[tag])))
            ++tag;
        }
        if (tag < n && s[tag] == '$') {
          size_t len = tag - i + 1;
          end = tag + 1;
          while (end + len <= n && memcmp(s + end, s + i, len))
            ++end;
          if (end + len > n)
            return SIZE_MAX;
          end += len;
          kind = 2;
        }
      }
      if (!kind && c == '%' && end < n && s[end] == '(') {
        size_t marker = end + 1;
        while (marker < n && s[marker] != ')')
          ++marker;
        if (marker + 1 < n && s[marker + 1] == 's') {
          end = marker + 2;
          kind = 2;
        }
      }
      if (!kind && c == '$' && d == GOTEL_SQL_SQLITE) {
        while (end < n && part((unsigned char)s[end]))
          ++end;
        while (end + 2 < n && s[end] == ':' && s[end + 1] == ':' && letter((unsigned char)s[end + 2])) {
          end += 3;
          while (end < n && part((unsigned char)s[end]))
            ++end;
        }
        if (end < n && s[end] == '(') {
          while (end < n && s[end] != ')')
            ++end;
          if (end == n)
            return SIZE_MAX;
          ++end;
        }
        kind = 2;
      }
      if (!kind &&
          (c == '\'' || c == '"' || c == '`' || (c == '[' && (d == GOTEL_SQL_SQLITE || d == GOTEL_SQL_GENERIC)))) {
        bool escape = d == GOTEL_SQL_MYSQL || (c == '\'' && i && (s[i - 1] == 'e' || s[i - 1] == 'E') &&
                                               (i == 1 || !part((unsigned char)s[i - 2])));
        end = quoted(s, n, i, c == '[' ? ']' : (char)c, escape);
        if (!end)
          return SIZE_MAX;
        kind = 2;
      } else if (!kind && (letter(c) || c >= 128 || isdigit(c) || c == '$' || c == '?' || c == ':' || c == '@')) {
        while (end < n && part((unsigned char)s[end]))
          ++end;
        kind = letter(c) || c >= 128 ? 1 : 0;
      }
    }
    ts[count++] = (format_token){{s + start, end - start, kind == 1, false}, gap, newline, kind};
    i = end;
  }
  return count;
}

static void format_line(output *b, size_t indent) {
  if (b->n && b->p[b->n - 1] != '\n')
    text(b, "\n");
  if (b->n)
    for (size_t i = 0; i < indent; ++i)
      text(b, "  ");
}

otlp_status_t gotel_sql_format(const char *sql, gotel_sql_dialect_t dialect, char *out, size_t capacity,
                               size_t *length) {
  if (out && capacity)
    *out = 0;
  if (!sql || !length)
    return OTLP_ERR_NULL;
  *length = 0;
  if (dialect < GOTEL_SQL_GENERIC || dialect > GOTEL_SQL_MYSQL || (!out && capacity) || (out && !capacity))
    return OTLP_ERR_INVALID_ARGUMENT;
  size_t n = 0;
  while (n <= INPUT_MAX && sql[n])
    ++n;
  if (n > INPUT_MAX)
    return OTLP_ERR_OVERFLOW;
  if (!gotel_utf8_valid(sql, n))
    return OTLP_ERR_INVALID_ARGUMENT;
  format_token *ts = calloc(n < TOKEN_MAX ? n + 1 : TOKEN_MAX, sizeof(*ts));
  size_t cap = n * 8 + 1024;
  char *buf = calloc(cap, 1);
  if (!ts || !buf) {
    free(ts);
    free(buf);
    return OTLP_ERR_NOMEM;
  }
  otlp_status_t status = OTLP_ERR_INVALID_ARGUMENT;
  size_t count = format_scan(sql, n, dialect, ts);
  if (count == SIZE_MAX)
    goto done;
  output b = {buf, 0, cap};
  size_t depth = 0;
  int clauses[65] = {0}; /* 1 SELECT list, 2 other clauses */
  char groups[65] = {0};
  bool pending = false;
  for (size_t i = 0; i < count; ++i) {
    format_token f = ts[i];
    token t = f.t;
    bool close = f.kind == 0 && t.n == 1 && strchr(")]}", *t.p);
    bool open = f.kind == 0 && t.n == 1 && strchr("([{", *t.p);
    if (close) {
      if (!depth || groups[depth] != (*t.p == ')' ? '(' : *t.p == ']' ? '[' : '{'))
        goto done;
      --depth;
    }
    bool select = kw(t, "SELECT");
    bool clause = kw(t, "FROM") || kw(t, "WHERE") || kw(t, "HAVING") || kw(t, "QUALIFY") || kw(t, "LIMIT") ||
                  kw(t, "OFFSET") || kw(t, "RETURNING") || kw(t, "UNION") || kw(t, "EXCEPT") || kw(t, "INTERSECT") ||
                  kw(t, "JOIN") || ((kw(t, "GROUP") || kw(t, "ORDER")) && i + 1 < count && kw(ts[i + 1].t, "BY"));
    bool logical = (kw(t, "AND") || kw(t, "OR")) && clauses[depth] == 2;
    /* Only introduce breaks at structural boundaries; never split adjacent
     * lexical tokens (prefix strings, operators, casts, parameters, etc.). */
    if (select || (clause && clauses[depth]) || logical || (pending && f.kind < 3) || (i && ts[i - 1].kind == 3) ||
        f.newline) {
      format_line(&b, depth + (logical ? 1 : 0) + (pending && !select && !clause && !close ? 1 : 0));
    } else if (f.gap && b.n && b.p[b.n - 1] != '\n')
      text(&b, " ");
    if (f.kind < 3)
      pending = false;
    emit_token(&b, t);
    if (select) {
      clauses[depth] = 1;
      pending = true;
    } else if (clause && clauses[depth])
      clauses[depth] = 2;
    else if (sym(t, ',') && clauses[depth] == 1)
      pending = true;
    else if (sym(t, ';')) {
      clauses[depth] = 0;
      pending = true;
    }
    if (open) {
      if (depth == 64)
        goto done;
      groups[++depth] = *t.p;
      clauses[depth] = 0;
    }
  }
  if (depth || b.overflow) {
    status = b.overflow ? OTLP_ERR_OVERFLOW : OTLP_ERR_INVALID_ARGUMENT;
    goto done;
  }
  *length = b.n;
  if (out) {
    if (capacity <= b.n) {
      status = OTLP_ERR_OVERFLOW;
      goto done;
    }
    memcpy(out, b.p, b.n + 1);
  }
  status = OTLP_OK;
done:
  free(ts);
  free(buf);
  return status;
}
