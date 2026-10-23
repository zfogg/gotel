#include "../utf8.h"
#include <ctype.h>
#include <gotel/sql.h>
#include <stdio.h>
#include <stdint.h>
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

// clang-format off
#include "common.c"
#include "scanner.c"
#include "summary.c"
#include "format.c"
#include "api.c"
// clang-format on
