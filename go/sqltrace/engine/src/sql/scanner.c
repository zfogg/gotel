/* Keep numbers only for the structural parser's row bounds. No such buffer is
 * exported as query text. Both modes scan the entire input before truncation. */
static bool sanitize(const char *s, size_t n, gotel_sql_dialect_t dialect, bool numbers, output *b) {
  bool space = false;
  int arrays = 0;
  bool clickhouse_insert = false;
  for (size_t i = 0; i < n;) {
    unsigned char c = (unsigned char)s[i];
    if (isspace(c)) {
      space = true;
      ++i;
      continue;
    }
    if ((i + 1 < n &&
         ((s[i] == '-' && s[i + 1] == '-') || (dialect == GOTEL_SQL_CLICKHOUSE && s[i] == '/' && s[i + 1] == '/'))) ||
        ((dialect == GOTEL_SQL_MYSQL || dialect == GOTEL_SQL_CLICKHOUSE) && s[i] == '#')) {
      while (i < n && s[i] != '\n')
        ++i;
      space = true;
      continue;
    }
    if (i + 1 < n && s[i] == '/' && s[i + 1] == '*') {
      int depth = 1;
      i += 2;
      while (i < n && depth) {
        if (i + 1 < n && s[i] == '/' && s[i + 1] == '*') {
          if (++depth > 64)
            return false;
          i += 2;
        } else if (i + 1 < n && s[i] == '*' && s[i + 1] == '/') {
          --depth;
          i += 2;
        } else
          ++i;
      }
      if (depth)
        return false;
      space = true;
      continue;
    }
    if (space && b->n)
      text(b, " ");
    space = false;
    if (c == '$') {
      size_t j = dollar_tag_end(s, n, i, dialect);
      if (j) {
        size_t tag = j - i + 1, k = j + 1;
        while (k + tag <= n && memcmp(s + k, s + i, tag))
          ++k;
        if (k + tag > n)
          return false;
        text(b, "?");
        i = k + tag;
        continue;
      }
    }
    if (dialect == GOTEL_SQL_CLICKHOUSE && clickhouse_quote(s, n, i)) {
      size_t end = clickhouse_quoted_end(s, n, i);
      if (!end)
        return false;
      if (clickhouse_quote(s, n, i) == 0x98)
        text(b, "?");
      else
        put(b, s + i, end - i);
      i = end;
      continue;
    }
    size_t quote = i;
    if (i + 1 < n && strchr("EeBbXxNn", c) && s[i + 1] == '\'')
      quote = i + 1;
    else if (i + 2 < n && (c == 'u' || c == 'U') && s[i + 1] == '&' && s[i + 2] == '\'')
      quote = i + 2;
    else if (dialect == GOTEL_SQL_MYSQL && c == '_') {
      size_t j = i + 1;
      while (j < n && part((unsigned char)s[j]))
        ++j;
      if (j < n && s[j] == '\'')
        quote = j;
    }
    if (s[quote] == '\'' || (dialect == GOTEL_SQL_MYSQL && c == '"')) {
      size_t end = quoted(s, n, quote, s[quote], true);
      if (!end)
        return false;
      text(b, "?");
      i = end;
      continue;
    }
    bool bracket = dialect == GOTEL_SQL_GENERIC || dialect == GOTEL_SQL_SQLITE;
    if (c == '"' || c == '`' || (c == '[' && bracket)) {
      size_t end = quoted(s, n, i, c == '[' ? ']' : (char)c, dialect == GOTEL_SQL_CLICKHOUSE);
      if (!end)
        return false;
      put(b, s + i, end - i);
      i = end;
      continue;
    }
    if (!bracket) {
      if (c == '[')
        ++arrays;
      else if (c == ']' && --arrays < 0)
        return false;
    }
    if (dialect == GOTEL_SQL_CLICKHOUSE && c == '{') {
      size_t parameter = clickhouse_parameter(s, n, i);
      if (!parameter)
        return false;
      text(b, "?");
      i = parameter;
      continue;
    }
    size_t end = i;
    if (c == '?' || (c == '$' && i + 1 < n && isdigit((unsigned char)s[i + 1]))) {
      end = i + 1;
      while (end < n && isdigit((unsigned char)s[end]))
        ++end;
    } else if ((c == ':' || c == '@' || c == '$') && i + 1 < n && letter((unsigned char)s[i + 1]) &&
               !(c == ':' && i && s[i - 1] == ':')) {
      end = i + 2;
      while (end < n && part((unsigned char)s[end]) &&
             !(dialect == GOTEL_SQL_CLICKHOUSE && clickhouse_quote(s, n, end)))
        ++end;
      /* SQLite permits Tcl variable suffixes; remove those rather than emit values. */
      if (c == '$' && dialect == GOTEL_SQL_SQLITE) {
        while (end + 2 < n && s[end] == ':' && s[end + 1] == ':' && letter((unsigned char)s[end + 2])) {
          end += 3;
          while (end < n && part((unsigned char)s[end]))
            ++end;
        }
        if (end < n && s[end] == '(') {
          while (end < n && s[end] != ')')
            ++end;
          if (end == n)
            return false;
          ++end;
        }
      }
    } else if (c == '%' && i + 1 < n && s[i + 1] == 's')
      end = i + 2;
    else if (c == '%' && i + 1 < n && s[i + 1] == '(') {
      size_t j = i + 2;
      while (j + 1 < n && !(s[j] == ')' && s[j + 1] == 's'))
        ++j;
      if (j + 1 < n)
        end = j + 2;
    }
    if (end > i) {
      text(b, "?");
      i = end;
      continue;
    }
    if (isdigit(c) || (c == '.' && i + 1 < n && isdigit((unsigned char)s[i + 1]))) {
      end = dialect_number_end(s, n, i, dialect);
      if (numbers)
        put(b, s + i, end - i);
      else
        text(b, "?");
      i = end;
      continue;
    }
    if (letter(c) || c >= 128) {
      end = i + 1;
      while (end < n && part((unsigned char)s[end]) &&
             !(dialect == GOTEL_SQL_CLICKHOUSE && clickhouse_quote(s, n, end)))
        ++end;
      if (dialect == GOTEL_SQL_CLICKHOUSE) {
        if (equal(s + i, end - i, "INSERT"))
          clickhouse_insert = true;
        /* INSERT FORMAT payloads are not SQL. Never scan or export their bytes.
         * Reject rather than treating CSV/JSON/binary data as identifiers. */
        if (clickhouse_insert && equal(s + i, end - i, "FORMAT"))
          return false;
      }
      if (equal(s + i, end - i, "TRUE") || equal(s + i, end - i, "FALSE") || equal(s + i, end - i, "NULL"))
        text(b, "?");
      else
        put(b, s + i, end - i);
      i = end;
      continue;
    }
    if (c == ';')
      clickhouse_insert = false;
    put(b, s + i, 1);
    ++i;
  }
  return arrays == 0;
}
static size_t tokenize(const char *s, token *t, size_t cap, gotel_sql_dialect_t dialect) {
  size_t n = strlen(s), count = 0;
  for (size_t i = 0; i < n;) {
    if (isspace((unsigned char)s[i])) {
      ++i;
      continue;
    }
    if (count == cap)
      return 0;
    size_t start = i;
    bool word = false;
    char c = s[i];
    if (dialect == GOTEL_SQL_CLICKHOUSE && clickhouse_quote(s, n, i))
      i = clickhouse_quoted_end(s, n, i);
    else if (c == '"' || c == '`' || (c == '[' && (dialect == GOTEL_SQL_SQLITE || dialect == GOTEL_SQL_GENERIC)))
      i = quoted(s, n, i, c == '[' ? ']' : c, dialect == GOTEL_SQL_CLICKHOUSE);
    else if (letter((unsigned char)c) || (unsigned char)c >= 128) {
      word = true;
      ++i;
      while (i < n && part((unsigned char)s[i]))
        ++i;
    } else if (isdigit((unsigned char)c))
      i = dialect_number_end(s, n, i, dialect);
    else
      ++i;
    if (i <= start)
      return 0;
    t[count++] = (token){s + start, i - start, word, start && isspace((unsigned char)s[start - 1]),
                         dialect == GOTEL_SQL_CLICKHOUSE};
  }
  return count;
}
