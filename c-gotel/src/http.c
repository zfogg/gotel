#include <ctype.h>
#include <gotel/gotel.h>
#include <stdio.h>
#include <string.h>
static bool identifier(const char *s, size_t n) {
  bool digits = n > 0, hex = n >= 24, uuid = n == 36;
  for (size_t i = 0; i < n; ++i) {
    if (!isdigit((unsigned char)s[i]))
      digits = false;
    if (!isxdigit((unsigned char)s[i]))
      hex = false;
    if (uuid && ((i == 8 || i == 13 || i == 18 || i == 23) ? s[i] != '-' : !isxdigit((unsigned char)s[i])))
      uuid = false;
  }
  return digits || hex || uuid;
}
otlp_status_t gotel_http_name(char *out, size_t cap, const char *prefix, bool server, const char *method,
                              const char *target) {
  if (!out || !cap || !prefix || !method || !*method || !target)
    return OTLP_ERR_INVALID_ARGUMENT;
  out[0] = 0;
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
      const char *part = identifier(s, len) ? "{id}" : s;
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
