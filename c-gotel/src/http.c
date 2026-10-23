#define PCRE2_CODE_UNIT_WIDTH 8
#include "utf8.h"
#include <gotel/gotel.h>
#include <pcre2.h>
#include <stdio.h>
#include <string.h>
static otlp_status_t identifier(const char *s, size_t n, bool *result) {
  /* Keep the same anchored ASCII ID policy as Go's httptrace.boundedClientPath. */
  static const char pattern[] =
      "\\A(?:[0-9]+|[0-9a-f]{24,}|[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\\z";
  int error;
  PCRE2_SIZE offset;
  pcre2_code *code = pcre2_compile((PCRE2_SPTR)pattern, PCRE2_ZERO_TERMINATED, PCRE2_CASELESS, &error, &offset, NULL);
  if (!code)
    return OTLP_ERR_NOMEM;
  pcre2_match_data *data = pcre2_match_data_create_from_pattern(code, NULL);
  if (!data) {
    pcre2_code_free(code);
    return OTLP_ERR_NOMEM;
  }
  int matched = pcre2_match(code, (PCRE2_SPTR)s, n, 0, 0, data, NULL);
  pcre2_match_data_free(data);
  pcre2_code_free(code);
  if (matched < 0 && matched != PCRE2_ERROR_NOMATCH)
    return OTLP_ERR_INVALID_ARGUMENT;
  *result = matched >= 0;
  return OTLP_OK;
}
otlp_status_t gotel_http_name(char *out, size_t cap, const char *prefix, bool server, const char *method,
                              const char *target) {
  if (!out || !cap || !prefix || !method || !*method || !target)
    return OTLP_ERR_INVALID_ARGUMENT;
  out[0] = 0;
  if (!gotel_utf8_valid(prefix, strlen(prefix)) || !gotel_utf8_valid(method, strlen(method)) ||
      !gotel_utf8_valid(target, strlen(target)))
    return OTLP_ERR_INVALID_ARGUMENT;
  char safe[1024];
  size_t used = 0;
  if (server) {
    if (!*target)
      target = "unknown";
    if (strchr(target, '?') || strchr(target, '#'))
      return OTLP_ERR_INVALID_ARGUMENT;
    if (strlen(target) >= sizeof(safe))
      return OTLP_ERR_OVERFLOW;
    strcpy(safe, target);
  } else {
    const char *start = strstr(target, "://");
    if (!start)
      return OTLP_ERR_INVALID_ARGUMENT;
    start += 3;
    const char *end = start + strcspn(start, "/?#");
    const char *host = start;
    for (const char *s = start; s < end; ++s)
      if (*s == '@')
        host = s + 1;
    size_t n = (size_t)(end - host);
    if (!n || n >= sizeof(safe))
      return OTLP_ERR_INVALID_ARGUMENT;
    memcpy(safe, host, n);
    used = n;
    const char *s = end;
    while (*s && *s != '?' && *s != '#') {
      if (*s == '/') {
        if (used + 1 >= sizeof(safe))
          return OTLP_ERR_OVERFLOW;
        safe[used++] = *s++;
        continue;
      }
      size_t len = strcspn(s, "/?#");
      bool is_id;
      otlp_status_t status = identifier(s, len, &is_id);
      if (status != OTLP_OK)
        return status;
      const char *part = is_id ? "{id}" : s;
      size_t plen = part == s ? len : 4;
      if (used + plen >= sizeof(safe))
        return OTLP_ERR_OVERFLOW;
      memcpy(safe + used, part, plen);
      used += plen;
      s += len;
    }
    safe[used] = 0;
  }
  size_t pn = strlen(prefix);
  while (pn && prefix[pn - 1] == '.')
    --pn;
  int n = snprintf(out, cap, "%.*s%shttp.%s.%s %s", (int)pn, prefix, pn ? "." : "", server ? "server" : "client",
                   method, safe);
  if (n < 0 || (size_t)n >= cap) {
    out[0] = 0;
    return OTLP_ERR_OVERFLOW;
  }
  return OTLP_OK;
}
void gotel_http_attributes(gotel_scope_t *s, const char *method, int status) {
  gotel_attr_string(s, "http.request.method", method);
  if (status > 0)
    gotel_attr_int(s, "http.response.status_code", status);
  if (s && s->span)
    otlp_span_set_kind(s->span, OTLP_SPAN_KIND_CLIENT);
  if (status >= 400)
    gotel_error(s, "http.error", "HTTP request failed");
}
