#include <gotel/sql.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
static void hex(const char *s) {
  for (; *s; ++s)
    printf("%02x", (unsigned char)*s);
}
static int nibble(char c) {
  if (c >= '0' && c <= '9')
    return c - '0';
  if (c >= 'a' && c <= 'f')
    return c - 'a' + 10;
  if (c >= 'A' && c <= 'F')
    return c - 'A' + 10;
  return -1;
}
int main(void) {
  char *line = malloc(2097200), *query = malloc(1048600);
  if (!line || !query)
    return 2;
  while (fgets(line, 2097200, stdin)) {
    int dialect = 0, omit = 0, keep = 0;
    size_t limit = 0;
    int offset = 0;
    if (sscanf(line, "%d %zu %d %d %n", &dialect, &limit, &omit, &keep, &offset) != 4)
      return 2;
    size_t n = strcspn(line + offset, "\r\n");
    if (n % 2)
      return 2;
    for (size_t i = 0; i < n; i += 2) {
      int high = nibble(line[offset + i]), low = nibble(line[offset + i + 1]);
      if (high < 0 || low < 0)
        return 2;
      query[i / 2] = (char)(high * 16 + low);
    }
    query[n / 2] = 0;
    gotel_sql_metadata_t metadata;
    gotel_sql_options_t options = {.dialect = (gotel_sql_dialect_t)dialect,
                                   .max_query_text_bytes = limit,
                                   .omit_query_text = omit != 0,
                                   .keep_in_lists = keep != 0};
    otlp_status_t status = gotel_sql_inspect(query, &options, &metadata);
    printf("%d %d ", (int)status, (int)metadata.truncated);
    hex(metadata.operation);
    putchar(' ');
    hex(metadata.text);
    putchar(' ');
    hex(metadata.summary);
    putchar('\n');
  }
  free(line);
  free(query);
  return 0;
}
