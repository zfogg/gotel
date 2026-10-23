#include <ctype.h>
#include <gotel/sql.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define INPUT_MAX (1024 * 1024)
#define TOKEN_MAX 65536
typedef struct {
  const char *p;
  size_t n;
  bool word, space;
} token;
typedef struct {
  char *p;
  size_t n, cap;
  bool overflow;
} output;
static bool letter(unsigned char c) { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'); }
static bool part(unsigned char c) { return letter(c) || isdigit(c) || c == '$' || c >= 128; }
static bool equal(const char *s, size_t n, const char *word) {
  if (strlen(word) != n)
    return false;
  for (size_t i = 0; i < n; ++i)
    if (toupper((unsigned char)s[i]) != toupper((unsigned char)word[i]))
      return false;
  return true;
}
static bool kw(token t, const char *word) { return t.word && equal(t.p, t.n, word); }
static bool sym(token t, char c) { return t.n == 1 && *t.p == c; }
static void put(output *b, const char *s, size_t n) {
  if (n > b->cap - b->n - 1) {
    b->overflow = true;
    n = b->cap - b->n - 1;
  }
  memcpy(b->p + b->n, s, n);
  b->n += n;
  b->p[b->n] = 0;
}
static void text(output *b, const char *s) { put(b, s, strlen(s)); }
static void item(output *b, const char *s) {
  if (b->n && b->p[b->n - 1] != ' ')
    text(b, " ");
  text(b, s);
}
static void emit_token(output *b, token t) { put(b, t.p, t.n); }
static void upper(output *b, token t) {
  for (size_t i = 0; i < t.n; ++i) {
    char c = (char)toupper((unsigned char)t.p[i]);
    put(b, &c, 1);
  }
}
static size_t quoted(const char *s, size_t n, size_t i, char close, bool backslash) {
  for (++i; i < n; ++i) {
    if (backslash && s[i] == '\\') {
      if (++i == n)
        return 0;
      continue;
    }
    if (s[i] == close) {
      if (i + 1 < n && s[i + 1] == close) {
        ++i;
        continue;
      }
      return i + 1;
    }
  }
  return 0;
}
static size_t number_end(const char *s, size_t n, size_t i) {
  if (i + 1 < n && s[i] == '0' && strchr("xXbB", s[i + 1])) {
    i += 2;
    while (i < n && (isxdigit((unsigned char)s[i]) || s[i] == '_'))
      ++i;
    return i;
  }
  while (i < n && (isdigit((unsigned char)s[i]) || s[i] == '_' || s[i] == '.'))
    ++i;
  if (i < n && (s[i] == 'e' || s[i] == 'E')) {
    ++i;
    if (i < n && (s[i] == '+' || s[i] == '-'))
      ++i;
    while (i < n && (isdigit((unsigned char)s[i]) || s[i] == '_'))
      ++i;
  }
  return i;
}
/* Keep numbers only for the structural parser's row bounds. No such buffer is
 * exported as query text. Both modes scan the entire input before truncation. */
static bool sanitize(const char *s, size_t n, gotel_sql_dialect_t dialect, bool numbers, output *b) {
  bool space = false;
  int arrays = 0;
  for (size_t i = 0; i < n;) {
    unsigned char c = (unsigned char)s[i];
    if (isspace(c)) {
      space = true;
      ++i;
      continue;
    }
    if ((i + 1 < n && s[i] == '-' && s[i + 1] == '-') || (dialect == GOTEL_SQL_MYSQL && s[i] == '#')) {
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
      size_t j = i + 1;
      if (j < n && letter((unsigned char)s[j])) {
        ++j;
        while (j < n && (letter((unsigned char)s[j]) || isdigit((unsigned char)s[j])))
          ++j;
      }
      if (j < n && s[j] == '$') {
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
      size_t end = quoted(s, n, i, c == '[' ? ']' : (char)c, false);
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
    size_t end = i;
    if (c == '?' || (c == '$' && i + 1 < n && isdigit((unsigned char)s[i + 1]))) {
      end = i + 1;
      while (end < n && isdigit((unsigned char)s[end]))
        ++end;
    } else if ((c == ':' || c == '@' || c == '$') && i + 1 < n && letter((unsigned char)s[i + 1]) &&
               !(c == ':' && i && s[i - 1] == ':')) {
      end = i + 2;
      while (end < n && part((unsigned char)s[end]))
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
      end = number_end(s, n, i);
      if (numbers)
        put(b, s + i, end - i);
      else
        text(b, "?");
      i = end;
      continue;
    }
    if (letter(c) || c >= 128) {
      end = i + 1;
      while (end < n && part((unsigned char)s[end]))
        ++end;
      if (equal(s + i, end - i, "TRUE") || equal(s + i, end - i, "FALSE") || equal(s + i, end - i, "NULL"))
        text(b, "?");
      else
        put(b, s + i, end - i);
      i = end;
      continue;
    }
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
    if (c == '"' || c == '`' || (c == '[' && (dialect == GOTEL_SQL_SQLITE || dialect == GOTEL_SQL_GENERIC)))
      i = quoted(s, n, i, c == '[' ? ']' : c, false);
    else if (letter((unsigned char)c) || (unsigned char)c >= 128) {
      word = true;
      ++i;
      while (i < n && part((unsigned char)s[i]))
        ++i;
    } else if (isdigit((unsigned char)c))
      i = number_end(s, n, i);
    else
      ++i;
    if (i <= start)
      return 0;
    t[count++] = (token){s + start, i - start, word, start && isspace((unsigned char)s[start - 1])};
  }
  return count;
}
static size_t close_group(token *t, size_t n, size_t i) {
  size_t depth = 0;
  for (size_t j = i; j < n; ++j) {
    if (sym(t[j], '('))
      ++depth;
    else if (sym(t[j], ')') && --depth == 0)
      return j;
  }
  return n;
}
static bool identifier(token t) { return t.word || *t.p == '"' || *t.p == '`' || *t.p == '['; }
static size_t name(output *b, token *t, size_t n, size_t i) {
  if (i >= n || !identifier(t[i]))
    return i;
  emit_token(b, t[i++]);
  while (i + 1 < n && sym(t[i], '.') && identifier(t[i + 1])) {
    text(b, ".");
    emit_token(b, t[i + 1]);
    i += 2;
  }
  return i;
}
static bool boundary(token t) {
  const char *words[] = {"FROM",  "WHERE",     "GROUP",  "HAVING",    "ORDER", "LIMIT", "OFFSET",  "FETCH", "FOR",
                         "UNION", "INTERSECT", "EXCEPT", "RETURNING", "SET",   "USING", "QUALIFY", "WINDOW"};
  for (size_t i = 0; i < sizeof(words) / sizeof(*words); ++i)
    if (kw(t, words[i]))
      return true;
  return sym(t, ';') || sym(t, ')');
}
static size_t clause_end(token *t, size_t n, size_t i) {
  unsigned cases = 0;
  for (; i < n; ++i) {
    if (sym(t[i], '(')) {
      i = close_group(t, n, i);
      continue;
    }
    if (kw(t[i], "CASE")) {
      ++cases;
      continue;
    }
    if (kw(t[i], "END") && cases) {
      --cases;
      continue;
    }
    if (cases)
      continue;
    if (i && kw(t[i], "FROM") && kw(t[i - 1], "DISTINCT"))
      continue;
    if (i && kw(t[i], "GROUP") && kw(t[i - 1], "WITHIN"))
      continue;
    if (i + 1 < n && kw(t[i], "ON") && kw(t[i + 1], "CONFLICT"))
      break;
    if (boundary(t[i]))
      break;
  }
  return i;
}
static bool query_start(token t) {
  return kw(t, "SELECT") || kw(t, "WITH") || kw(t, "INSERT") || kw(t, "UPDATE") || kw(t, "DELETE") || kw(t, "MERGE") ||
         kw(t, "VALUES") || kw(t, "EXPLAIN");
}
static void summarize(output *, token *, size_t, unsigned);
static void nested(output *b, token *t, size_t n, unsigned depth) {
  if (depth > 64) {
    item(b, "…");
    return;
  }
  for (size_t i = 0; i < n; ++i)
    if (sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      if (i + 1 < end && query_start(t[i + 1])) {
        if (i && (kw(t[i - 1], "EXISTS") || kw(t[i - 1], "IN"))) {
          if (i > 1 && kw(t[i - 2], "NOT"))
            item(b, "NOT");
          item(b, kw(t[i - 1], "IN") ? "IN" : "EXISTS");
        }
        item(b, "(");
        summarize(b, t + i + 1, end - i - 1, depth + 1);
        text(b, ")");
      } else
        nested(b, t + i + 1, end - i - 1, depth + 1);
      i = end;
    }
}
static bool join_modifier(token t) {
  const char *words[] = {"NATURAL", "LEFT", "RIGHT", "FULL", "OUTER",     "INNER",
                         "CROSS",   "SEMI", "ANTI",  "ASOF", "POSITIONAL"};
  for (size_t i = 0; i < sizeof(words) / sizeof(*words); ++i)
    if (kw(t, words[i]))
      return true;
  return false;
}
static size_t join_at(token *t, size_t n, size_t i) {
  size_t j = i;
  while (j < n && join_modifier(t[j]))
    ++j;
  if (j < n && kw(t[j], "JOIN")) {
    ++j;
    if (j < n && kw(t[j], "LATERAL"))
      ++j;
    return j;
  }
  return i;
}
static void expression(output *b, token *t, size_t n, unsigned depth) {
  size_t started = b->n;
  for (size_t i = 0; i < n; ++i) {
    if (t[i].space && b->n > started && b->p[b->n - 1] != ' ')
      text(b, " ");
    if (sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      text(b, "(");
      if (i + 1 < end && query_start(t[i + 1]))
        summarize(b, t + i + 1, end - i - 1, depth + 1);
      else if (depth < 64)
        expression(b, t + i + 1, end - i - 1, depth + 1);
      text(b, ")");
      i = end;
    } else if (isdigit((unsigned char)*t[i].p) ||
               (sym(t[i], '.') && i + 1 < n && isdigit((unsigned char)*t[i + 1].p))) {
      text(b, "?");
      if (sym(t[i], '.'))
        ++i;
    } else
      emit_token(b, t[i]);
  }
}
static void relations(output *, token *, size_t, unsigned);
static size_t relation(output *b, token *t, size_t n, size_t i, unsigned depth) {
  while (i < n && (kw(t[i], "ONLY") || kw(t[i], "LATERAL"))) {
    upper(b, t[i++]);
    text(b, " ");
  }
  if (i >= n) {
    text(b, "…");
    return i;
  }
  if (sym(t[i], '(')) {
    size_t end = close_group(t, n, i);
    text(b, "(");
    if (i + 1 < end && query_start(t[i + 1]))
      summarize(b, t + i + 1, end - i - 1, depth + 1);
    else
      relations(b, t + i + 1, end - i - 1, depth + 1);
    text(b, ")");
    i = end < n ? end + 1 : n;
  } else {
    size_t next = name(b, t, n, i);
    if (next == i) {
      text(b, "…");
      return i + 1;
    }
    i = next;
    if (i < n && sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      text(b, "(…)");
      i = end < n ? end + 1 : n;
    }
  }
  if (i + 1 < n && kw(t[i], "WITH") && kw(t[i + 1], "ORDINALITY")) {
    text(b, " WITH ORDINALITY");
    i += 2;
  }
  if (i < n && kw(t[i], "AS"))
    ++i;
  if (i < n && identifier(t[i]) && !boundary(t[i]) && !kw(t[i], "ON") && !kw(t[i], "WHEN") && !kw(t[i], "WITH") &&
      join_at(t, n, i) == i) {
    text(b, " AS ");
    emit_token(b, t[i++]);
    if (i < n && sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      expression(b, t + i, end - i + (end < n), depth + 1);
      i = end < n ? end + 1 : n;
    }
  }
  return i;
}
static void relations(output *b, token *t, size_t n, unsigned depth) {
  if (depth > 64) {
    text(b, "…");
    return;
  }
  size_t i = relation(b, t, n, 0, depth);
  while (i < n) {
    size_t next = join_at(t, n, i);
    if (next > i) {
      text(b, " ");
      for (; i < next; ++i) {
        upper(b, t[i]);
        text(b, " ");
      }
      i = relation(b, t, n, i, depth);
    } else if (sym(t[i], ',')) {
      text(b, ", ");
      i = relation(b, t, n, i + 1, depth);
    } else if (kw(t[i], "ON") || kw(t[i], "USING")) {
      text(b, " ");
      upper(b, t[i++]);
      text(b, " ");
      size_t start = i;
      int cases = 0;
      for (; i < n; ++i) {
        if (sym(t[i], '(')) {
          i = close_group(t, n, i);
          continue;
        }
        if (sym(t[i], '[')) {
          size_t depth_square = 1;
          while (++i < n && depth_square) {
            if (sym(t[i], '['))
              ++depth_square;
            else if (sym(t[i], ']'))
              --depth_square;
          }
          if (i < n)
            --i;
          continue;
        }
        if (kw(t[i], "CASE"))
          ++cases;
        else if (kw(t[i], "END") && cases)
          --cases;
        if (!cases && (join_at(t, n, i) > i || sym(t[i], ',')))
          break;
      }
      expression(b, t + start, i - start, depth + 1);
    } else
      ++i;
  }
}
static void bound(output *b, token *t, size_t n) {
  if (n == 1 && kw(t[0], "ALL")) {
    text(b, "ALL");
    return;
  }
  bool valid = n == 1 || (n == 2 && (sym(t[0], '-') || sym(t[0], '+')));
  if (valid) {
    token v = t[n - 1];
    for (size_t i = 0; i < v.n; ++i)
      if (!isdigit((unsigned char)v.p[i]))
        valid = false;
  }
  if (valid) {
    for (size_t i = 0; i < n; ++i)
      emit_token(b, t[i]);
  } else
    text(b, "?");
}
static void summarize(output *b, token *t, size_t n, unsigned depth) {
  if (depth > 64) {
    text(b, "…");
    return;
  }
  if (!n)
    return;
  size_t i = 0;
  if (kw(t[0], "WITH")) {
    text(b, "WITH ");
    i = 1;
    if (i < n && kw(t[i], "RECURSIVE")) {
      text(b, "RECURSIVE ");
      ++i;
    }
    for (;;) {
      size_t next = name(b, t, n, i);
      if (next == i) {
        text(b, "SQL");
        return;
      }
      i = next;
      if (i < n && sym(t[i], '('))
        i = close_group(t, n, i) + 1;
      if (i >= n || !kw(t[i], "AS")) {
        text(b, " SQL");
        return;
      }
      ++i;
      text(b, " AS");
      if (i < n && kw(t[i], "NOT")) {
        text(b, " NOT");
        ++i;
      }
      if (i < n && kw(t[i], "MATERIALIZED")) {
        text(b, " MATERIALIZED");
        ++i;
      }
      if (i >= n || !sym(t[i], '(')) {
        text(b, " SQL");
        return;
      }
      size_t end = close_group(t, n, i);
      text(b, " (");
      summarize(b, t + i + 1, end - i - 1, depth + 1);
      text(b, ")");
      i = end < n ? end + 1 : n;
      if (i >= n || !sym(t[i], ','))
        break;
      text(b, ", ");
      ++i;
    }
    text(b, " ");
  }
  if (i >= n)
    return;
  if (kw(t[i], "EXPLAIN")) {
    text(b, "EXPLAIN");
    ++i;
    if (i < n && sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      for (size_t j = i + 1; j < end; ++j)
        if (kw(t[j], "ANALYZE")) {
          text(b, " ANALYZE");
          break;
        }
      i = end < n ? end + 1 : n;
    }
    while (i < n && (kw(t[i], "ANALYZE") || kw(t[i], "VERBOSE") || kw(t[i], "QUERY") || kw(t[i], "PLAN"))) {
      if (kw(t[i], "ANALYZE"))
        text(b, " ANALYZE");
      ++i;
    }
    if (i < n) {
      text(b, " ");
      summarize(b, t + i, n - i, depth + 1);
    }
    return;
  }
  token operation_token = t[i];
  if (sym(t[i], '(')) {
    size_t end = close_group(t, n, i);
    text(b, "(");
    summarize(b, t + i + 1, end - i - 1, depth + 1);
    text(b, ")");
    i = end < n ? end + 1 : n;
  } else {
    token op = t[i++];
    const char *operations[] = {"SELECT",   "INSERT", "UPDATE", "DELETE", "MERGE",    "COPY",    "CALL",
                                "TRUNCATE", "CREATE", "ALTER",  "DROP",   "VACUUM",   "ANALYZE", "PRAGMA",
                                "SHOW",     "SET",    "BEGIN",  "COMMIT", "ROLLBACK", "VALUES"};
    bool supported = false;
    for (size_t j = 0; j < sizeof(operations) / sizeof(*operations); ++j)
      if (kw(op, operations[j]))
        supported = true;
    if (!supported) {
      text(b, "SQL");
      return;
    }
    upper(b, op);
    if (kw(op, "SELECT")) {
      if (i < n && (kw(t[i], "DISTINCT") || kw(t[i], "ALL"))) {
        text(b, " ");
        upper(b, t[i++]);
        if (i < n && kw(t[i], "ON")) {
          item(b, "ON (…)");
          ++i;
          if (i < n && sym(t[i], '('))
            i = close_group(t, n, i) + 1;
        }
      }
      size_t end = clause_end(t, n, i);
      char function_text[4097] = {0};
      output functions = {function_text, 0, sizeof(function_text)};
      for (size_t j = i; j < end; ++j) {
        if (sym(t[j], '(')) {
          j = close_group(t, n, j);
          continue;
        }
        char fn[1024] = {0};
        output f = {fn, 0, sizeof(fn)};
        size_t next = name(&f, t, end, j);
        if (next > j && next < end && sym(t[next], '(') && !kw(t[j], "EXISTS") && !kw(t[j], "CAST")) {
          size_t close = close_group(t, n, next);
          if (functions.n)
            text(&functions, ", ");
          text(&functions, fn);
          text(&functions, close == next + 1 ? "()" : "(…)");
          j = next - 1;
        }
      }
      if (end < n && kw(t[end], "FROM"))
        item(b, "…");
      else if (functions.n)
        item(b, function_text);
      else
        item(b, "…");
      nested(b, t + i, end - i, depth + 1);
      for (size_t j = i; j < end; ++j) {
        if (sym(t[j], '(')) {
          j = close_group(t, n, j);
          continue;
        }
        if (kw(t[j], "OVER"))
          item(b, "OVER (…)");
        if (kw(t[j], "FILTER"))
          item(b, "FILTER (…)");
        if (j + 1 < end && kw(t[j], "WITHIN") && kw(t[j + 1], "GROUP"))
          item(b, "WITHIN GROUP (…)");
      }
      i = end;
    } else if (kw(op, "UPDATE")) {
      text(b, " ");
      size_t end = clause_end(t, n, i);
      relations(b, t + i, end - i, depth + 1);
      i = end;
    } else if (kw(op, "VALUES")) {
      item(b, "…");
      return;
    } else if (kw(op, "CALL") || kw(op, "COPY") || kw(op, "VACUUM") || kw(op, "ANALYZE") || kw(op, "TRUNCATE")) {
      if (i < n && kw(t[i], "TABLE"))
        ++i;
      text(b, " ");
      i = name(b, t, n, i);
      if (kw(op, "CALL"))
        return;
    }
  }
  while (i < n) {
    token v = t[i];
    if (sym(v, ';') || sym(v, ')'))
      break;
    if (kw(v, "UNION") || kw(v, "INTERSECT") || kw(v, "EXCEPT")) {
      text(b, " ");
      upper(b, t[i++]);
      while (i < n && (kw(t[i], "ALL") || kw(t[i], "DISTINCT") || kw(t[i], "BY") || kw(t[i], "NAME"))) {
        text(b, " ");
        upper(b, t[i++]);
      }
      text(b, " ");
      summarize(b, t + i, n - i, depth + 1);
      return;
    }
    if (kw(v, "FROM") || kw(v, "USING")) {
      if (kw(operation_token, "COPY"))
        break;
      text(b, " ");
      upper(b, v);
      text(b, " ");
      size_t end = clause_end(t, n, i + 1);
      relations(b, t + i + 1, end - i - 1, depth + 1);
      i = end;
      continue;
    }
    if (kw(v, "INTO")) {
      item(b, "INTO ");
      i = kw(t[0], "MERGE") ? relation(b, t, n, i + 1, depth + 1) : name(b, t, n, i + 1);
      if (i < n && sym(t[i], '('))
        i = close_group(t, n, i) + 1;
      continue;
    }
    if (kw(v, "SELECT") && kw(operation_token, "INSERT")) {
      text(b, " ");
      summarize(b, t + i, n - i, depth + 1);
      return;
    }
    if (kw(v, "LIMIT") || kw(v, "OFFSET")) {
      text(b, " ");
      upper(b, v);
      text(b, " ");
      size_t end = clause_end(t, n, i + 1), comma = i + 1;
      size_t bound_end = end;
      if (bound_end > i + 1 && (kw(t[bound_end - 1], "ROWS") || kw(t[bound_end - 1], "ROW")))
        --bound_end;
      while (comma < bound_end && !sym(t[comma], ',')) {
        if (sym(t[comma], '('))
          comma = close_group(t, bound_end, comma);
        if (comma < bound_end)
          ++comma;
      }
      bound(b, t + i + 1, (comma < bound_end ? comma : bound_end) - i - 1);
      if (comma < bound_end) {
        text(b, ", ");
        bound(b, t + comma + 1, bound_end - comma - 1);
      }
      nested(b, t + i + 1, bound_end - i - 1, depth + 1);
      i = end;
      continue;
    }
    if (kw(v, "FOR")) {
      item(b, "FOR");
      ++i;
      while (i < n) {
        bool matched = false;
        const char *locks[] = {"UPDATE", "NO", "KEY", "SHARE", "SKIP", "LOCKED", "NOWAIT"};
        for (size_t j = 0; j < sizeof(locks) / sizeof(*locks); ++j)
          if (kw(t[i], locks[j])) {
            item(b, locks[j]);
            matched = true;
            break;
          }
        if (!matched && boundary(t[i]))
          break;
        ++i;
      }
      continue;
    }
    if (kw(v, "FETCH")) {
      size_t end = clause_end(t, n, i + 1), j = i + 1;
      item(b, "FETCH");
      if (j < end && (kw(t[j], "FIRST") || kw(t[j], "NEXT"))) {
        text(b, " ");
        upper(b, t[j++]);
      } else
        item(b, "FIRST");
      size_t row = j;
      while (row < end && !kw(t[row], "ROW") && !kw(t[row], "ROWS"))
        ++row;
      text(b, " ");
      if (row > j)
        bound(b, t + j, row - j);
      else
        text(b, "1");
      item(b, row + 2 < end && kw(t[row + 1], "WITH") && kw(t[row + 2], "TIES") ? "ROWS WITH TIES" : "ROWS ONLY");
      i = end;
      continue;
    }
    if (kw(v, "ON") && i + 1 < n && kw(t[i + 1], "CONFLICT")) {
      i += 2;
      if (i < n && sym(t[i], '('))
        i = close_group(t, n, i) + 1;
      while (i < n && !kw(t[i], "DO") && !kw(t[i], "RETURNING"))
        ++i;
      if (i + 1 < n && kw(t[i], "DO")) {
        item(b, "ON CONFLICT DO ");
        upper(b, t[i + 1]);
        i += 2;
      }
      continue;
    }
    if (kw(v, "WHERE") || kw(v, "HAVING") || kw(v, "QUALIFY") || kw(v, "SET") || kw(v, "RETURNING") || kw(v, "GROUP") ||
        kw(v, "ORDER") || kw(v, "WINDOW")) {
      text(b, " ");
      upper(b, v);
      ++i;
      if (i < n && kw(t[i], "BY")) {
        text(b, " BY");
        ++i;
      }
      size_t end = clause_end(t, n, i);
      item(b, "…");
      nested(b, t + i, end - i, depth + 1);
      i = end;
      continue;
    }
    if (kw(v, "VALUES")) {
      item(b, "VALUES …");
      ++i;
      continue;
    }
    if (sym(v, '(')) {
      size_t end = close_group(t, n, i);
      nested(b, t + i, end - i + (end < n), depth + 1);
      i = end < n ? end + 1 : n;
      continue;
    }
    ++i;
  }
}
static void utf8_cut(char *s, size_t limit) {
  size_t n = strlen(s);
  if (n <= limit)
    return;
  while (limit && ((unsigned char)s[limit] & 0xc0) == 0x80)
    --limit;
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
  size_t op = 0;
  while (op < count && sym(tokens[op], '('))
    ++op;
  if (kw(tokens[0], "WITH"))
    for (op = 1; op < count; ++op) {
      if (sym(tokens[op], '(')) {
        op = close_group(tokens, count, op);
        continue;
      }
      if (kw(tokens[op], "SELECT") || kw(tokens[op], "INSERT") || kw(tokens[op], "UPDATE") ||
          kw(tokens[op], "DELETE") || kw(tokens[op], "MERGE") || kw(tokens[op], "VALUES"))
        break;
    }
  if (op < count && tokens[op].word) {
    output p = {out->operation, 0, sizeof(out->operation)};
    upper(&p, tokens[op]);
  }
  output summary = {out->summary, 0, sizeof(out->summary)};
  *out->summary = 0;
  summarize(&summary, tokens, count, 0);
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
}
