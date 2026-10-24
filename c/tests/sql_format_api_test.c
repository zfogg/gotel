#include <gotel/sql.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CHECK(expr)                                                                                                    \
  do {                                                                                                                 \
    if (!(expr)) {                                                                                                     \
      fprintf(stderr, "failed line %d: %s\n", __LINE__, #expr);                                                        \
      return 1;                                                                                                        \
    }                                                                                                                  \
  } while (0)
int main(void) {
  const char *sql = "SELECT a,b FROM t WHERE id=$1; -- trailing  ";
  char out[256] = "old";
  size_t length = 999;
  CHECK(gotel_sql_format(NULL, GOTEL_SQL_POSTGRESQL, out, sizeof(out), &length) == OTLP_ERR_NULL);
  CHECK(!*out);
  CHECK(gotel_sql_format(sql, GOTEL_SQL_POSTGRESQL, out, sizeof(out), NULL) == OTLP_ERR_NULL);
  CHECK(gotel_sql_format(sql, (gotel_sql_dialect_t)-1, out, sizeof(out), &length) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(gotel_sql_format(sql, GOTEL_SQL_POSTGRESQL, NULL, 1, &length) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(gotel_sql_format(sql, GOTEL_SQL_POSTGRESQL, out, 0, &length) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(gotel_sql_format(sql, GOTEL_SQL_POSTGRESQL, NULL, 0, &length) == OTLP_OK);
  size_t required = length;
  CHECK(required > strlen(sql));
  CHECK(gotel_sql_format(sql, GOTEL_SQL_POSTGRESQL, out, required, &length) == OTLP_ERR_OVERFLOW);
  CHECK(!*out && length == required);
  CHECK(gotel_sql_format(sql, GOTEL_SQL_POSTGRESQL, out, required + 1, &length) == OTLP_OK);
  CHECK(length == strlen(out) && length == required);
  CHECK(strstr(out, "$1") && strstr(out, "-- trailing  "));
  CHECK(gotel_sql_format("SELECT \xff", GOTEL_SQL_POSTGRESQL, out, sizeof(out), &length) == OTLP_ERR_INVALID_ARGUMENT);
  CHECK(!*out);
  CHECK(gotel_sql_format("", GOTEL_SQL_POSTGRESQL, out, 1, &length) == OTLP_OK && length == 0 && !*out);
  char nested[512] = "SELECT ";
  size_t used = strlen(nested);
  for (int i = 0; i < 40; ++i) {
    memcpy(nested + used, "(SELECT ", 8);
    used += 8;
  }
  nested[used++] = '1';
  for (int i = 0; i < 40; ++i)
    nested[used++] = ')';
  nested[used] = 0;
  CHECK(gotel_sql_format(nested, GOTEL_SQL_POSTGRESQL, NULL, 0, &length) == OTLP_OK);
  CHECK(length > strlen(nested) * 8 + 1024);
  required = length;
  CHECK(gotel_sql_format(nested, GOTEL_SQL_POSTGRESQL, out, sizeof(out), &length) == OTLP_ERR_OVERFLOW);
  CHECK(!*out && length == required);
  char *expanded = malloc(required + 1);
  CHECK(expanded);
  CHECK(gotel_sql_format(nested, GOTEL_SQL_POSTGRESQL, expanded, required + 1, &length) == OTLP_OK);
  CHECK(strlen(expanded) == required);
  CHECK(gotel_sql_format(expanded, GOTEL_SQL_POSTGRESQL, NULL, 0, &length) == OTLP_OK);
  CHECK(length == required);
  free(expanded);
  return 0;
}
