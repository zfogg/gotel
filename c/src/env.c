#include "internal.h"
#include "exporter_internal.h"
#include "env_config.h"
#include <ctype.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const char *env(const char *name) {
  const char *value = getenv(name);
  return value && *value ? value : NULL;
}

otlp_status_t gotel_options_set_endpoint(gotel_options_t *o, gotel_env_storage_t *s, const char *base) {
  if (!o || !s)
    return OTLP_ERR_NULL;
  if (!base || !*base) {
    o->exporter.endpoint = NULL;
    o->exporter.metrics_endpoint = NULL;
    o->exporter.logs_endpoint = NULL;
    return OTLP_OK;
  }
  if ((strncmp(base, "http://", 7) && strncmp(base, "https://", 8)) || strpbrk(base, "?#@"))
    return OTLP_ERR_INVALID_ARGUMENT;
  size_t n = strlen(base);
  while (n && base[n - 1] == '/')
    --n;
  char *buffers[] = {s->exporter.endpoint, s->exporter.metrics_endpoint, s->exporter.logs_endpoint};
  bool grpc = o->protocol && !strcmp(o->protocol, "grpc");
  const char *paths[] = {grpc ? "" : "/v1/traces", grpc ? "" : "/v1/metrics", grpc ? "" : "/v1/logs"};
  const char **fields[] = {&o->exporter.endpoint, &o->exporter.metrics_endpoint, &o->exporter.logs_endpoint};
  for (size_t i = 0; i < 3; ++i) {
    if (n + strlen(paths[i]) >= sizeof(s->exporter.endpoint))
      return OTLP_ERR_OVERFLOW;
    char endpoint[sizeof(s->exporter.endpoint)];
    memcpy(endpoint, base, n);
    strcpy(endpoint + n, paths[i]);
    otlp_status_t status = otlp_env_apply_signal_endpoint(&o->exporter, endpoint, buffers[i], fields[i]);
    if (status != OTLP_OK)
      return status;
  }
  return OTLP_OK;
}

otlp_status_t gotel_options_apply_env(gotel_options_t *o, gotel_env_storage_t *s, const char *fallback) {
  if (!o || !s)
    return OTLP_ERR_NULL;
  memset(s, 0, sizeof(*s));
  o->use_environment = true;
  const char *disabled = env("OTEL_SDK_DISABLED");
  if (disabled && strlen(disabled) == 4 && tolower((unsigned char)disabled[0]) == 't' &&
      tolower((unsigned char)disabled[1]) == 'r' && tolower((unsigned char)disabled[2]) == 'u' &&
      tolower((unsigned char)disabled[3]) == 'e') {
    o->signals = 0;
    return OTLP_OK;
  }
  const otlp_resource_attr_t *resources = o->exporter.resource_attributes;
  size_t count = o->exporter.n_resource_attributes;
  const char *protocol = env("OTEL_EXPORTER_OTLP_PROTOCOL");
  if (protocol) {
    if (!strcmp(protocol, "http/protobuf"))
      o->protocol = "http/protobuf";
#ifdef GOTEL_WITH_GRPC
    else if (!strcmp(protocol, "grpc"))
      o->protocol = "grpc";
#endif
    else
      return OTLP_ERR_INVALID_ARGUMENT;
  }
  /* Mixed per-signal protocols are not supported by this host API. Fail explicitly. */
  const char *protocol_names[] = {"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL",
                                 "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"};
  for (size_t i = 0; i < 3; ++i) {
    const char *value = env(protocol_names[i]);
    if (value && strcmp(value, o->protocol ? o->protocol : "http/protobuf"))
      return OTLP_ERR_INVALID_ARGUMENT;
  }
  otlp_status_t status = otlp_exporter_opts_apply_env(&o->exporter, &s->exporter);
  if (status != OTLP_OK)
    return status;
  const char *base = env("OTEL_EXPORTER_OTLP_ENDPOINT");
  if (base || (fallback && *fallback)) {
    status = gotel_options_set_endpoint(o, s, base ? base : fallback);
    if (status != OTLP_OK)
      return status;
  }
  const char *names[] = {"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
                         "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"};
  char *buffers[] = {s->exporter.endpoint, s->exporter.metrics_endpoint, s->exporter.logs_endpoint};
  const char **fields[] = {&o->exporter.endpoint, &o->exporter.metrics_endpoint, &o->exporter.logs_endpoint};
  for (size_t i = 0; i < 3; ++i) {
    status = otlp_env_apply_signal_endpoint(&o->exporter, env(names[i]), buffers[i], fields[i]);
    if (status != OTLP_OK)
      return status;
  }
  size_t total = s->exporter.n_attrs;
  if (count > 64 || total > 64 - count)
    return OTLP_ERR_OVERFLOW;
  for (size_t i = 0; i < total; ++i)
    s->resources[i] = s->exporter.attrs[i];
  for (size_t i = 0; i < count; ++i) {
    bool overridden = false;
    for (size_t j = 0; j < total; ++j)
      overridden |= !strcmp(resources[i].key, s->resources[j].key);
    if (!overridden)
      s->resources[total++] = resources[i];
  }
  o->exporter.resource_attributes = s->resources;
  o->exporter.n_resource_attributes = total;
  if (!env("OTEL_SERVICE_NAME")) {
    for (size_t i = 0; i < total; ++i)
      if (!strcmp(s->resources[i].key, "service.name") && s->resources[i].value.type == OTLP_VALUE_STRING)
        o->exporter.service_name = s->resources[i].value.v.string_val;
  } else {
    for (size_t i = 0; i < total; ++i)
      if (!strcmp(s->resources[i].key, "service.name"))
        s->resources[i].value = (otlp_value_t){.type = OTLP_VALUE_STRING, .v.string_val = o->exporter.service_name};
  }
  if (env("OTEL_EXPORTER_OTLP_TIMEOUT"))
    o->exporter.flush_timeout_ms = o->exporter.read_timeout_ms;
  if (env("OTEL_EXPORTER_OTLP_CERTIFICATE"))
    o->tls_ca_file = env("OTEL_EXPORTER_OTLP_CERTIFICATE");
  return OTLP_OK;
}

otlp_status_t gotel_protocol_configure(gotel_host_t *h) {
  if (!h->options.protocol || !strcmp(h->options.protocol, "http/protobuf"))
    return OTLP_OK;
#ifdef GOTEL_WITH_GRPC
  if (!strcmp(h->options.protocol, "grpc")) {
    static const char *paths[] = {
      "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
      "/opentelemetry.proto.collector.metrics.v1.MetricsService/Export",
      "/opentelemetry.proto.collector.logs.v1.LogsService/Export"};
    const char *endpoints[] = {h->options.exporter.endpoint, h->options.exporter.metrics_endpoint,
                              h->options.exporter.logs_endpoint};
    for (size_t i = 0; i < 3; ++i) {
      struct otlp_http_url url;
      const char *endpoint = endpoints[i] ? endpoints[i] : endpoints[0];
      if (!endpoint)
        endpoint = "https://localhost:4317";
      otlp_status_t status = otlp_http_parse_url(endpoint, &url);
      if (status != OTLP_OK || strcmp(url.path, "/"))
        return OTLP_ERR_INVALID_ARGUMENT;
      url.grpc = 1;
      strcpy(url.path, paths[i]);
      h->exporter->sig[i].url = url;
    }
    return OTLP_OK;
  }
#endif
  return OTLP_ERR_INVALID_ARGUMENT;
}

otlp_status_t gotel_signal_headers(gotel_host_t *h) {
  if (!h->options.use_environment)
    return OTLP_OK;
  h->signal_env = calloc(3, sizeof(*h->signal_env));
  if (!h->signal_env)
    return OTLP_ERR_NOMEM;
  const char *names[] = {"OTEL_EXPORTER_OTLP_TRACES_HEADERS", "OTEL_EXPORTER_OTLP_METRICS_HEADERS",
                         "OTEL_EXPORTER_OTLP_LOGS_HEADERS"};
  for (size_t i = 0; i < 3; ++i) {
    const char *value = env(names[i]);
    if (!value)
      continue;
    otlp_exporter_opts_t opts = {0};
    otlp_status_t status = otlp_env_apply_otlp_headers(&opts, value, &h->signal_env[i]);
    if (status != OTLP_OK)
      return status;
    for (size_t j = 0; j < opts.n_http_headers; ++j)
      if (strpbrk(opts.http_headers[j].name, "\r\n") || strpbrk(opts.http_headers[j].value, "\r\n"))
        return OTLP_ERR_INVALID_ARGUMENT;
    h->exporter->sig[i].gotel_headers = opts.http_headers;
    h->exporter->sig[i].gotel_header_count = opts.n_http_headers;
    h->exporter->sig[i].gotel_headers_set = true;
  }
  return OTLP_OK;
}
