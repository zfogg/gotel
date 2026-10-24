#ifndef GOTEL_UTF8_H
#define GOTEL_UTF8_H
#include <stdbool.h>
#include <stddef.h>
bool gotel_utf8_valid(const char *text, size_t length);
size_t gotel_utf8_prefix(const char *text, size_t length, size_t limit);
#endif
