#define GOTEL_SQL_STANDALONE 1
#include "../../c/src/sql/main.c"
#include "../../c/src/utf8.c"
#include "../../go/sqltrace/utf8proc.c"

/* Preserve every token and every gap containing a newline. Only collapse
 * existing horizontal gaps between tokens; never join previously separate
 * tokens or insert spaces inside dialect-specific operators. */
int edit_sql(const char *sql, int dialect, char *out, size_t capacity) {
  size_t n = strlen(sql);
  if (!gotel_utf8_valid(sql, n)) return 1;
  format_token *ts = calloc(n < TOKEN_MAX ? n + 1 : TOKEN_MAX, sizeof(*ts));
  if (!ts) return 1;
  size_t count = format_scan(sql, n, (gotel_sql_dialect_t)dialect, ts);
  if (count == SIZE_MAX) { free(ts); return 1; }
  output b = {out, 0, capacity};
  const char *previous = sql;
  for (size_t i = 0; i < count; ++i) {
    token t = ts[i].t;
    size_t gap = (size_t)(t.p - previous);
    if (gap) {
      if (i == 0 || memchr(previous, '\n', gap) || memchr(previous, '\r', gap))
        put(&b, previous, gap);
      else
        text(&b, " ");
    }
    emit_token(&b, t);
    previous = t.p + t.n;
  }
  put(&b, previous, n - (size_t)(previous - sql));
  free(ts);
  return b.overflow;
}
