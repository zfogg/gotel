#pragma once

#include <stdbool.h>
#include <stddef.h>

/** Return whether the explicit byte range is valid UTF-8. */
bool gotel_utf8_valid(const char *text, size_t length);

/** Return the longest complete UTF-8 prefix within the byte limit. */
size_t gotel_utf8_prefix(const char *text, size_t length, size_t limit);
