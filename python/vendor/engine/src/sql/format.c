/* Preserve lexical contents; only whitespace outside lexical tokens changes.
 * Keeping the formatter independent of a grammar also supports driver/sqlc
 * parameters and sanitized telemetry templates that aren't executable SQL. */
typedef struct {
  token t;
  bool gap, newline;
  int kind; /* 0 punctuation, 1 word, 2 quoted/parameter, 3 line comment, 4 block comment */
  size_t matching;
  bool multiline;
  size_t flat_width;
} format_token;

static size_t format_scan(const char *s, size_t n, gotel_sql_dialect_t d, format_token *ts) {
  size_t count = 0, i = 0;
  bool clickhouse_insert = false;
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
    if ((end < n && ((c == '-' && s[end] == '-') || (d == GOTEL_SQL_CLICKHOUSE && c == '/' && s[end] == '/'))) ||
        (c == '#' && (d == GOTEL_SQL_MYSQL || d == GOTEL_SQL_CLICKHOUSE))) {
      while (end < n && s[end] != '\n' && (d == GOTEL_SQL_CLICKHOUSE || s[end] != '\r'))
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
      if (d == GOTEL_SQL_CLICKHOUSE && clickhouse_quote(s, n, i)) {
        end = clickhouse_quoted_end(s, n, i);
        if (!end)
          return SIZE_MAX;
        kind = 2;
      }
      if (c == '{' && d == GOTEL_SQL_CLICKHOUSE) {
        end = clickhouse_parameter(s, n, i);
        if (!end)
          return SIZE_MAX;
        kind = 2;
      }
      size_t quote = i;
      if (i + 1 < n && strchr("EeBbXxNn", c) && s[i + 1] == '\'')
        quote = i + 1;
      else if (i + 2 < n && (c == 'u' || c == 'U') && s[i + 1] == '&' && s[i + 2] == '\'')
        quote = i + 2;
      else if (d == GOTEL_SQL_MYSQL && c == '_') {
        size_t q = i + 1;
        while (q < n && part((unsigned char)s[q]))
          ++q;
        if (q < n && s[q] == '\'')
          quote = q;
      }
      if (quote != i) {
        end = quoted(s, n, quote, '\'', d == GOTEL_SQL_MYSQL || c == 'e' || c == 'E');
        if (!end)
          return SIZE_MAX;
        kind = 2;
      }
      /* PostgreSQL dollar strings must be recognized before $parameters. */
      if (c == '$' &&
          (d == GOTEL_SQL_POSTGRESQL || d == GOTEL_SQL_DUCKDB || d == GOTEL_SQL_CLICKHOUSE || d == GOTEL_SQL_GENERIC)) {
        size_t tag = dollar_tag_end(s, n, i, d);
        if (tag) {
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
        while (end < n && part((unsigned char)s[end]) && !(d == GOTEL_SQL_CLICKHOUSE && clickhouse_quote(s, n, end)))
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
      if (!kind && c == ':' && end < n && (s[end] == ':' || s[end] == '=')) {
        ++end;
        kind = 5;
      }
      if (!kind && strchr("~!#%^&|+-*/<>=@", c) && !(c == '@' && end < n && letter((unsigned char)s[end]))) {
        while (end < n && strchr("~!#%^&|+-*/<>=@", s[end])) {
          if (end + 1 < n &&
              ((s[end] == '/' && (s[end + 1] == '*' || (d == GOTEL_SQL_CLICKHOUSE && s[end + 1] == '/'))) ||
               (s[end] == '-' && s[end + 1] == '-')))
            break;
          ++end;
        }
        /* Separate trailing unary signs in >=-1 and x+-y. PostgreSQL custom
         * operators containing special operator characters stay opaque. */
        bool custom = false;
        for (size_t k = i; k < end; ++k)
          if (strchr("~!#%^&|@", s[k]))
            custom = true;
        if (!custom)
          while (end > i + 1 && (s[end - 1] == '+' || s[end - 1] == '-'))
            --end;
        kind = 5;
      }
      if (!kind &&
          (c == '\'' || c == '"' || c == '`' || (c == '[' && (d == GOTEL_SQL_SQLITE || d == GOTEL_SQL_GENERIC)))) {
        bool escape =
            d == GOTEL_SQL_MYSQL || d == GOTEL_SQL_CLICKHOUSE ||
            (c == '\'' && i && (s[i - 1] == 'e' || s[i - 1] == 'E') && (i == 1 || !part((unsigned char)s[i - 2])));
        end = quoted(s, n, i, c == '[' ? ']' : (char)c, escape);
        if (!end)
          return SIZE_MAX;
        kind = 2;
      } else if (!kind && isdigit(c)) {
        end = dialect_number_end(s, n, i, d);
      } else if (!kind && (letter(c) || c >= 128 || c == '$' || c == '?' || c == ':' || c == '@')) {
        while (end < n && part((unsigned char)s[end]) && !(d == GOTEL_SQL_CLICKHOUSE && clickhouse_quote(s, n, end)))
          ++end;
        kind = letter(c) || c >= 128 ? 1 : 0;
      }
    }
    if (d == GOTEL_SQL_CLICKHOUSE && kind == 1) {
      if (equal(s + start, end - start, "INSERT"))
        clickhouse_insert = true;
      if (clickhouse_insert && equal(s + start, end - start, "FORMAT"))
        return SIZE_MAX;
    }
    if (kind == 0 && c == ';')
      clickhouse_insert = false;
    ts[count] = (format_token){{s + start, end - start, kind == 1, false, d == GOTEL_SQL_CLICKHOUSE},
                               gap,
                               newline,
                               kind == 5 ? 0 : kind,
                               0,
                               false,
                               0};
    ts[count].flat_width = (count ? ts[count - 1].flat_width : 0) + end - start + 1;
    ++count;
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

/* Clause state belongs to a parenthesis level; function arguments must not
 * accidentally inherit SELECT-list or WHERE-list comma/bool rules. */
enum { F_NONE, F_SELECT, F_FROM, F_CONDITION, F_ORDER, F_SET, F_VALUES, F_WITH, F_OTHER, F_ON };
typedef struct {
  size_t base, closing, header_end;
  int clause;
  bool active, pending, multiline, between, deleting, inserting, values_seen, window;
} format_frame;
typedef struct {
  size_t depth, base;
  bool condition, multiline;
} format_case;

static size_t format_next(format_token *ts, size_t n, size_t i) {
  while (i < n && ts[i].kind >= 3)
    ++i;
  return i;
}
static bool format_list(int c) { return c == F_SELECT || c == F_ORDER || c == F_SET || c == F_VALUES; }
static size_t format_body(format_frame f) {
  return f.base + (format_list(f.clause) || f.clause == F_CONDITION || f.clause == F_ON ? 1 : 0);
}
static void format_space(output *b) {
  if (b->n && b->p[b->n - 1] != ' ' && b->p[b->n - 1] != '\n')
    text(b, " ");
}
static bool format_join(format_token *ts, size_t n, size_t i) {
  token t = ts[i].t;
  if (kw(t, "JOIN"))
    return true;
  if (!(kw(t, "LEFT") || kw(t, "RIGHT") || kw(t, "FULL") || kw(t, "INNER") || kw(t, "CROSS") || kw(t, "NATURAL")))
    return false;
  for (size_t j = i + 1; j < n && j < i + 4; ++j) {
    if (kw(ts[j].t, "JOIN"))
      return true;
    if (!(kw(ts[j].t, "OUTER") || kw(ts[j].t, "LEFT") || kw(ts[j].t, "RIGHT") || kw(ts[j].t, "FULL") ||
          kw(ts[j].t, "INNER")))
      break;
  }
  return false;
}
static bool format_comparison(token t) {
  return equal(t.p, t.n, "=") || equal(t.p, t.n, "!=") || equal(t.p, t.n, "<>") || equal(t.p, t.n, "<") ||
         equal(t.p, t.n, ">") || equal(t.p, t.n, "<=") || equal(t.p, t.n, ">=");
}
static bool format_spaced_operator(format_token *ts, size_t i, size_t previous) {
  token t = ts[i].t;
  if (format_comparison(t))
    return true;
  if (!(equal(t.p, t.n, "+") || equal(t.p, t.n, "-") || equal(t.p, t.n, "*") || equal(t.p, t.n, "/") ||
        equal(t.p, t.n, "//") || equal(t.p, t.n, "%") || equal(t.p, t.n, "||")))
    return false;
  if (previous == SIZE_MAX)
    return false;
  token p = ts[previous].t;
  if (sym(p, ')') || sym(p, ']') || ts[previous].kind == 2 || isdigit((unsigned char)*p.p))
    return true;
  return p.word && !(kw(p, "SELECT") || kw(p, "DISTINCT") || kw(p, "THEN") || kw(p, "ELSE") || kw(p, "WHEN") ||
                     kw(p, "BY") || kw(p, "AND") || kw(p, "OR") || kw(p, "NOT") || kw(p, "WHERE") || kw(p, "SET") ||
                     kw(p, "VALUES") || kw(p, "RETURNING") || kw(p, "AS"));
}
static bool format_layout(format_token *ts, size_t count, output *b) {
  /* Match groups once, then propagate multiline constructs out through their
   * containing function calls. This avoids repeatedly scanning nested SQL. */
  size_t opens[65] = {0}, level = 0;
  for (size_t i = 0; i < count; ++i) {
    token t = ts[i].t;
    if (ts[i].kind == 0 && t.n == 1 && strchr("([{", *t.p)) {
      if (level == 64)
        return false;
      opens[++level] = i;
      if (i && kw(ts[i - 1].t, "OVER"))
        ts[i].multiline = true;
    } else if (ts[i].kind == 0 && t.n == 1 && strchr(")]}", *t.p)) {
      if (!level)
        return false;
      size_t start = opens[level--];
      if (*ts[start].t.p != (*t.p == ')' ? '(' : *t.p == ']' ? '[' : '{'))
        return false;
      ts[start].matching = i;
      ts[i].matching = start;
      ts[start].multiline |= ts[i].flat_width - ts[start].flat_width > 70;
      if (level && ts[start].multiline)
        ts[opens[level]].multiline = true;
    } else if (level && (kw(t, "SELECT") || kw(t, "WITH") || kw(t, "CASE") || ts[i].kind == 3)) {
      ts[opens[level]].multiline = true;
    }
  }
  if (level)
    return false;
  format_frame frames[65] = {{0}};
  format_case cases[64];
  size_t depth = 0, case_count = 0, line_indent = 0;
  size_t previous = SIZE_MAX;
  bool after_comparison = false, join_head = false;
  for (size_t i = 0; i < count; ++i) {
    format_token f = ts[i];
    token t = f.t;
    bool open = f.kind == 0 && t.n == 1 && strchr("([{", *t.p);
    bool close = f.kind == 0 && t.n == 1 && strchr(")]}", *t.p);
    bool line = false;
    size_t indent = format_body(frames[depth]);
    if (close) {
      line = frames[depth].multiline;
      indent = frames[depth].closing;
      --depth;
    }
    format_frame *ctx = &frames[depth];
    size_t next = f.kind >= 3 ? i + 1 : format_next(ts, count, i + 1);
    bool head = ctx->pending && i <= ctx->header_end;
    bool select = kw(t, "SELECT");
    bool with = kw(t, "WITH") && !ctx->active;
    bool modifier = format_join(ts, count, i) && !join_head;
    bool from = kw(t, "FROM") && ctx->active && !head;
    bool condition =
        ((t.clickhouse && kw(t, "PREWHERE")) || kw(t, "WHERE") || kw(t, "HAVING") || kw(t, "QUALIFY")) && ctx->active;
    bool order =
        (kw(t, "ORDER") || kw(t, "GROUP") || kw(t, "PARTITION")) && next < count && kw(ts[next].t, "BY") && ctx->active;
    bool set = kw(t, "SET") && ctx->active;
    bool values =
        kw(t, "VALUES") && ctx->clause != F_SELECT && ((!ctx->active) || (ctx->inserting && !ctx->values_seen));
    bool returning = (kw(t, "RETURNING") || (t.clickhouse && (kw(t, "SETTINGS") || kw(t, "FORMAT")))) && ctx->active;
    bool on = kw(t, "ON") && ctx->clause == F_FROM && !head;
    bool conflict = kw(t, "ON") && next < count && (kw(ts[next].t, "CONFLICT") || kw(ts[next].t, "DUPLICATE"));
    bool bound = (kw(t, "LIMIT") || kw(t, "OFFSET")) && ctx->active;
    bool setop = (kw(t, "UNION") || kw(t, "INTERSECT") || kw(t, "EXCEPT")) && ctx->active;
    bool logical = (kw(t, "AND") || kw(t, "OR")) && (ctx->clause == F_CONDITION || ctx->clause == F_ON) &&
                   (!case_count || cases[case_count - 1].depth != depth);
    if (kw(t, "BETWEEN"))
      ctx->between = true;
    bool between_and = kw(t, "AND") && ctx->between;
    if (between_and) {
      ctx->between = false;
      logical = false;
    }
    bool case_when = case_count && cases[case_count - 1].depth == depth && (kw(t, "WHEN") || kw(t, "ELSE"));
    bool case_end = case_count && cases[case_count - 1].depth == depth && kw(t, "END");
    format_case *case_ctx = case_count && cases[case_count - 1].depth == depth ? &cases[case_count - 1] : NULL;
    if (case_ctx && kw(t, "WHEN")) {
      case_ctx->condition = true;
      case_ctx->multiline = false;
      size_t nested = 0;
      for (size_t j = i + 1; j < count; ++j) {
        if (sym(ts[j].t, '(') || sym(ts[j].t, '[') || sym(ts[j].t, '{'))
          j = ts[j].matching;
        else if (kw(ts[j].t, "CASE"))
          ++nested;
        else if (kw(ts[j].t, "END") && nested)
          --nested;
        else if (!nested && kw(ts[j].t, "THEN")) {
          case_ctx->multiline |= ts[j].flat_width - ts[i].flat_width > 60;
          break;
        } else if (!nested && (kw(ts[j].t, "AND") || kw(ts[j].t, "OR")))
          case_ctx->multiline = true;
      }
    }
    bool nested_case = kw(t, "CASE") && case_count && previous != SIZE_MAX &&
                       (kw(ts[previous].t, "THEN") || kw(ts[previous].t, "ELSE"));
    bool spaced_operator = format_spaced_operator(ts, i, previous);
    bool keyword_group = open && previous != SIZE_MAX &&
                         (kw(ts[previous].t, "AS") || kw(ts[previous].t, "IN") || kw(ts[previous].t, "EXISTS") ||
                          kw(ts[previous].t, "OVER") || kw(ts[previous].t, "FILTER") || kw(ts[previous].t, "USING") ||
                          kw(ts[previous].t, "ON") || kw(ts[previous].t, "AND") || kw(ts[previous].t, "OR") ||
                          kw(ts[previous].t, "NOT"));
    if (select || with || modifier || (from && !ctx->deleting) || condition || order || set || values || returning ||
        conflict || bound || setop) {
      line = true;
      indent = ctx->base;
    } else if (on || logical) {
      line = true;
      indent = ctx->base + 1;
    } else if (ctx->pending && !head && f.kind < 3 && !close) {
      line = true;
      indent = format_body(*ctx);
    }
    if (case_when) {
      line = true;
      indent = cases[case_count - 1].base + 1;
    }
    if (case_ctx && case_ctx->condition && case_ctx->multiline && !case_when &&
        (kw(t, "AND") || kw(t, "OR") || kw(t, "THEN"))) {
      line = true;
      indent = case_ctx->base + (kw(t, "THEN") ? 1 : 2);
      if (between_and)
        line = false;
    }
    if (case_end) {
      line = true;
      indent = cases[--case_count].base;
    }
    if (nested_case) {
      line = true;
      indent = cases[case_count - 1].base + 2;
    }
    if (f.kind >= 3) {
      /* Keep inline comments inline, especially those following semicolons.
       * Their contents (including trailing spaces) are never trimmed. */
      line = f.newline || (i && ts[i - 1].kind == 3);
      indent = line_indent;
    } else if (i && ts[i - 1].kind == 3) {
      line = true;
    }
    /* PostgreSQL concatenates adjacent quoted strings only across a newline.
     * Preserve such newlines, including those beside intervening comments. */
    if (f.newline && (f.kind == 2 || (i && ts[i - 1].kind == 2)))
      line = true;
    if (line) {
      format_line(b, indent);
      line_indent = indent;
    } else if (!close && !sym(t, ',') && !sym(t, ';') &&
               ((f.gap && !(previous != SIZE_MAX && ts[previous].kind == 0 && ts[previous].t.n == 1 &&
                            strchr("([{", *ts[previous].t.p))) ||
                after_comparison || spaced_operator || keyword_group ||
                (previous != SIZE_MAX && sym(ts[previous].t, ',')))) {
      format_space(b);
    }
    emit_token(b, t);
    after_comparison = spaced_operator;
    if (f.kind >= 3)
      continue;
    if (!head)
      ctx->pending = false;
    if (kw(t, "CASE")) {
      if (case_count == 64)
        return false;
      cases[case_count++] = (format_case){depth, line_indent};
    }
    if (case_ctx && (kw(t, "THEN") || kw(t, "ELSE")))
      case_ctx->condition = false;
    if (select) {
      ctx->active = true;
      ctx->deleting = false;
      ctx->clause = F_SELECT;
      ctx->pending = true;
      ctx->header_end = i;
      size_t j = next;
      if (j < count && (kw(ts[j].t, "DISTINCT") || kw(ts[j].t, "ALL"))) {
        ctx->header_end = j;
        size_t k = format_next(ts, count, j + 1);
        if (kw(ts[j].t, "DISTINCT") && k < count && kw(ts[k].t, "ON")) {
          k = format_next(ts, count, k + 1);
          if (k < count && sym(ts[k].t, '('))
            ctx->header_end = ts[k].matching;
        }
      }
    } else if (with) {
      ctx->active = true;
      ctx->clause = F_WITH;
    } else if (!ctx->active && (kw(t, "UPDATE") || kw(t, "INSERT") || kw(t, "DELETE") || kw(t, "MERGE"))) {
      ctx->active = true;
      ctx->deleting = kw(t, "DELETE");
      ctx->inserting = kw(t, "INSERT");
      ctx->clause = F_OTHER;
    } else if (from || modifier) {
      ctx->clause = F_FROM;
    } else if (condition || on) {
      ctx->clause = on ? F_ON : F_CONDITION;
      ctx->pending = !on;
      ctx->header_end = i;
      ctx->between = false;
    } else if (order || set || values || returning) {
      if (values) {
        ctx->active = true;
        ctx->values_seen = true;
      }
      ctx->clause = set ? F_SET : values ? F_VALUES : F_ORDER;
      ctx->pending = true;
      ctx->header_end = order ? next : i;
      if (order && ctx->window) {
        /* Short single-expression window clauses read naturally inline.
         * Count only commas at this level; function arguments are opaque. */
        size_t end = next;
        bool list = false;
        for (size_t j = next + 1; j < count; ++j) {
          token item = ts[j].t;
          if (sym(item, ')') || kw(item, "ORDER") || kw(item, "ROWS") || kw(item, "RANGE") || kw(item, "GROUPS"))
            break;
          if (sym(item, ','))
            list = true;
          if (sym(item, '(') || sym(item, '[') || sym(item, '{'))
            j = ts[j].matching;
          end = j;
        }
        if (!list && ts[end].flat_width - ts[i].flat_width <= 60)
          ctx->pending = false;
      }
    } else if (bound || setop || conflict) {
      ctx->clause = F_OTHER;
    } else if (sym(t, ',')) {
      ctx->pending = format_list(ctx->clause) || ctx->clause == F_WITH || ctx->clause == F_FROM ||
                     (ctx->multiline && ctx->clause == F_NONE);
      ctx->header_end = i;
    } else if (sym(t, ';')) {
      ctx->active = false;
      ctx->deleting = false;
      ctx->inserting = false;
      ctx->values_seen = false;
      ctx->clause = F_NONE;
      ctx->pending = true;
      ctx->header_end = i;
    }
    if (modifier)
      join_head = !kw(t, "JOIN");
    else if (kw(t, "JOIN"))
      join_head = false;
    if (open) {
      size_t closing = head ? ctx->base : format_body(*ctx);
      bool window = previous != SIZE_MAX && kw(ts[previous].t, "OVER");
      bool case_condition = case_ctx && case_ctx->condition && case_ctx->multiline;
      if (case_condition)
        closing = case_ctx->base + 2;
      bool subquery = next < count && (kw(ts[next].t, "SELECT") || kw(ts[next].t, "WITH") || kw(ts[next].t, "UPDATE") ||
                                       kw(ts[next].t, "DELETE"));
      bool boolean = false;
      if ((ctx->clause == F_CONDITION || ctx->clause == F_ON || case_condition) &&
          (previous == SIZE_MAX || !ts[previous].t.word || kw(ts[previous].t, "AND") || kw(ts[previous].t, "OR") ||
           kw(ts[previous].t, "NOT") || kw(ts[previous].t, "WHERE") || kw(ts[previous].t, "HAVING") ||
           kw(ts[previous].t, "ON"))) {
        for (size_t j = i + 1; j < ts[i].matching; ++j) {
          if (ts[j].kind == 0 && ts[j].t.n == 1 && strchr("([{", *ts[j].t.p))
            j = ts[j].matching;
          else if (kw(ts[j].t, "AND") || kw(ts[j].t, "OR"))
            boolean = true;
        }
      }
      bool multiline = ts[i].multiline || subquery || boolean;
      if (ctx->clause == F_WITH)
        closing = ctx->base;
      frames[++depth] = (format_frame){.base = closing + (multiline && !boolean ? 1 : 0),
                                       .closing = closing,
                                       .clause = boolean ? F_CONDITION : F_NONE,
                                       .active = boolean || window,
                                       .pending = multiline,
                                       .header_end = i,
                                       .multiline = multiline,
                                       .window = window};
    }
    previous = i;
  }
  return !case_count && !b->overflow;
}

otlp_status_t gotel_sql_format(const char *sql, gotel_sql_dialect_t dialect, char *out, size_t capacity,
                               size_t *length) {
  if (out && capacity)
    *out = 0;
  if (!sql || !length)
    return OTLP_ERR_NULL;
  *length = 0;
  if (dialect < GOTEL_SQL_GENERIC || dialect > GOTEL_SQL_CLICKHOUSE || (!out && capacity) || (out && !capacity))
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
  if (!format_layout(ts, count, &b)) {
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
