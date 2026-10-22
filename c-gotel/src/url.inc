/* Replacement parser for the pinned otlp-c internal URL representation. */
otlp_status_t otlp_http_parse_url(const char *url, struct otlp_http_url *out) {
  if (!url || !out)
    return OTLP_ERR_NULL;
  memset(out, 0, sizeof(*out));
  for (const unsigned char *p = (const unsigned char *)url; *p; ++p)
    if (*p <= 32 || *p == 127 || *p == '@' || *p == '?' || *p == '#')
      return OTLP_ERR_INVALID_ARGUMENT;
  const char *host;
  if (!strncmp(url, "https://", 8)) {
    out->tls = 1;
    out->port = 443;
    host = url + 8;
  } else if (!strncmp(url, "http://", 7)) {
    out->port = 80;
    host = url + 7;
  } else
    return OTLP_ERR_INVALID_ARGUMENT;
  const char *end;
  if (*host == '[') {
    ++host;
    end = strchr(host, ']');
    if (!end || !memchr(host, ':', (size_t)(end - host)))
      return OTLP_ERR_INVALID_ARGUMENT;
  } else {
    end = host;
    while (*end && *end != ':' && *end != '/')
      ++end;
  }
  size_t len = (size_t)(end - host);
  if (!len || len >= sizeof(out->host))
    return OTLP_ERR_INVALID_ARGUMENT;
  memcpy(out->host, host, len);
  const char *p = *end == ']' ? end + 1 : end;
  if (*p == ':') {
    uint32_t port = 0;
    const char *start = ++p;
    while (*p >= '0' && *p <= '9') {
      port = port * 10 + (uint32_t)(*p++ - '0');
      if (port > 65535)
        return OTLP_ERR_INVALID_ARGUMENT;
    }
    if (p == start || !port)
      return OTLP_ERR_INVALID_ARGUMENT;
    out->port = (uint16_t)port;
  }
  if (*p && *p != '/')
    return OTLP_ERR_INVALID_ARGUMENT;
  if (strlen(p) >= sizeof(out->path))
    return OTLP_ERR_INVALID_ARGUMENT;
  strcpy(out->path, *p ? p : "/");
  return OTLP_OK;
}
