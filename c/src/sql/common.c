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

/* Typed ClickHouse parameters are opaque lexical tokens, including nested types.
 * Ignore parameter names/types in telemetry; never inspect the supplied values. */
static size_t clickhouse_parameter(const char *s, size_t n, size_t i) {
  size_t j = i + 1;
  if (j >= n || !letter((unsigned char)s[j])) return 0;
  while (j < n && part((unsigned char)s[j])) ++j;
  while (j < n && isspace((unsigned char)s[j])) ++j;
  if (j == n || s[j++] != ':') return 0;
  size_t start = j, depth = 0;
  for (; j < n; ++j) {
    if (s[j] == '\'' || s[j] == '"' || s[j] == '`') {
      size_t end = quoted(s, n, j, s[j], true);
      if (!end) return 0;
      j = end - 1;
    } else if (s[j] == '(') {
      if (++depth > 64) return 0;
    } else if (s[j] == ')') {
      if (!depth) return 0;
      --depth;
    } else if (s[j] == '}') {
      return !depth && j > start ? j + 1 : 0;
    } else if (s[j] == '{' || s[j] == ';') return 0;
  }
  return 0;
}
