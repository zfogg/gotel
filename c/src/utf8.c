#include "utf8.h"
#include <utf8proc.h>

bool gotel_utf8_valid(const char *text, size_t length) {
  size_t i = 0;
  while (i < length) {
    utf8proc_int32_t codepoint;
    utf8proc_ssize_t n =
        utf8proc_iterate((const utf8proc_uint8_t *)text + i, (utf8proc_ssize_t)(length - i), &codepoint);
    if (n < 1)
      return false;
    i += (size_t)n;
  }
  return true;
}

size_t gotel_utf8_prefix(const char *text, size_t length, size_t limit) {
  size_t i = 0;
  while (i < length && i < limit) {
    utf8proc_int32_t codepoint;
    utf8proc_ssize_t n =
        utf8proc_iterate((const utf8proc_uint8_t *)text + i, (utf8proc_ssize_t)(length - i), &codepoint);
    if (n < 1 || (size_t)n > limit - i)
      break;
    i += (size_t)n;
  }
  return i;
}
