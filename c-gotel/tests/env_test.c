#if !defined(_WIN32) && !defined(_POSIX_C_SOURCE)
#define _POSIX_C_SOURCE 200809L
#endif
#include "exporter_internal.h"
#include "internal.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CHECK(x)                                                                                                       \
  do {                                                                                                                 \
    if (!(x)) {                                                                                                        \
      fprintf(stderr, "%s:%d: %s\n", __FILE__, __LINE__, #x);                                                          \
      exit(1);                                                                                                         \
    }                                                                                                                  \
  } while (0)
#define SAME(a, b) CHECK((a) && !strcmp((a), (b)))
static const char *variables[] = {"OTEL_SDK_DISABLED",
                                  "OTEL_SERVICE_NAME",
                                  "OTEL_RESOURCE_ATTRIBUTES",
                                  "OTEL_EXPORTER_OTLP_ENDPOINT",
                                  "OTEL_EXPORTER_OTLP_HEADERS",
                                  "OTEL_EXPORTER_OTLP_TIMEOUT",
                                  "OTEL_EXPORTER_OTLP_CERTIFICATE",
                                  "OTEL_EXPORTER_OTLP_PROTOCOL",
                                  "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
                                  "OTEL_EXPORTER_OTLP_METRICS_PROTOCOL",
                                  "OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
                                  "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
                                  "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
                                  "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
                                  "OTEL_EXPORTER_OTLP_TRACES_HEADERS",
                                  "OTEL_EXPORTER_OTLP_METRICS_HEADERS",
                                  "OTEL_EXPORTER_OTLP_LOGS_HEADERS"};
static void environment(const char *name, const char *value) {
#ifdef _WIN32
  CHECK(_putenv_s(name, value ? value : "") == 0);
#else
  CHECK((value ? setenv(name, value, 1) : unsetenv(name)) == 0);
#endif
}
static void clear(void) {
  for (size_t i = 0; i < sizeof(variables) / sizeof(*variables); ++i)
    environment(variables[i], NULL);
}
static uint64_t clock_ns(void) { return 1000000000; }
static gotel_options_t options(void) {
  static const gotel_operation_t operations[] = {{"env.test", false}};
  gotel_options_t o = {0};
  o.signals = GOTEL_ALL;
  o.exporter.endpoint = "http://127.0.0.1:1/v1/traces";
  o.exporter.service_name = "application";
  o.exporter.connect_timeout_ms = 123;
  o.exporter.read_timeout_ms = 456;
  o.exporter.flush_timeout_ms = 789;
  o.monotonic_ns = o.realtime_ns = clock_ns;
  o.operations = operations;
  o.operation_count = 1;
  o.disable_resource_detection = true;
  return o;
}
static void defaults(void) {
  gotel_env_storage_t s;
  gotel_options_t o = options();
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  SAME(o.exporter.endpoint, "http://127.0.0.1:1/v1/traces");
  SAME(o.exporter.service_name, "application");
  CHECK(o.signals == GOTEL_ALL && o.exporter.read_timeout_ms == 456);
  o = options();
  o.exporter.endpoint = NULL;
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK && !o.exporter.endpoint);
  for (size_t i = 0; i < sizeof(variables) / sizeof(*variables); ++i)
    environment(variables[i], "");
  o = options();
  CHECK(gotel_options_apply_env(&o, &s, "http://localhost:24318") == OTLP_OK);
  SAME(o.exporter.endpoint, "http://localhost:24318/v1/traces");
  SAME(o.exporter.metrics_endpoint, "http://localhost:24318/v1/metrics");
  SAME(o.exporter.logs_endpoint, "http://localhost:24318/v1/logs");
  CHECK(o.signals == GOTEL_ALL && o.exporter.flush_timeout_ms == 789);
}
static void endpoints(void) {
  gotel_env_storage_t s;
  gotel_options_t o = options();
  environment("OTEL_EXPORTER_OTLP_ENDPOINT", "https://example.com:443/collector///");
  environment("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://traces.example.com/custom");
  environment("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://logs.example.com/");
  CHECK(gotel_options_apply_env(&o, &s, "http://localhost:24318") == OTLP_OK);
  SAME(o.exporter.endpoint, "https://traces.example.com/custom");
  SAME(o.exporter.metrics_endpoint, "https://example.com:443/collector/v1/metrics");
  SAME(o.exporter.logs_endpoint, "http://logs.example.com/");
  environment("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://metrics.example.com/custom");
  o = options();
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  SAME(o.exporter.metrics_endpoint, "http://metrics.example.com/custom");
  CHECK(gotel_options_set_endpoint(&o, &s, "http://[::1]:24318/prefix/") == OTLP_OK);
  SAME(o.exporter.endpoint, "http://[::1]:24318/prefix/v1/traces");
  SAME(o.exporter.metrics_endpoint, "http://[::1]:24318/prefix/v1/metrics");
  SAME(o.exporter.logs_endpoint, "http://[::1]:24318/prefix/v1/logs");
  CHECK(gotel_options_set_endpoint(&o, &s, "") == OTLP_OK);
  CHECK(!o.exporter.endpoint && !o.exporter.metrics_endpoint && !o.exporter.logs_endpoint);
}
static const char *attribute(gotel_options_t *o, const char *key) {
  for (size_t i = 0; i < o->exporter.n_resource_attributes; ++i)
    if (!strcmp(o->exporter.resource_attributes[i].key, key))
      return o->exporter.resource_attributes[i].value.v.string_val;
  return NULL;
}
static void identity(void) {
  gotel_env_storage_t s;
  otlp_resource_attr_t attributes[] = {
      {"deployment.environment.name", {.type = OTLP_VALUE_STRING, .v.string_val = "default"}},
      {"service.version", {.type = OTLP_VALUE_STRING, .v.string_val = "1.2.3"}}};
  gotel_options_t o = options();
  o.exporter.resource_attributes = attributes;
  o.exporter.n_resource_attributes = 2;
  environment("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=test,service.name=resource-service");
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  SAME(o.exporter.service_name, "resource-service");
  SAME(attribute(&o, "deployment.environment.name"), "test");
  SAME(attribute(&o, "service.version"), "1.2.3");
  CHECK(o.exporter.n_resource_attributes == 3);
  environment("OTEL_SERVICE_NAME", "explicit-service");
  o = options();
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  SAME(o.exporter.service_name, "explicit-service");
  SAME(attribute(&o, "service.name"), "explicit-service");
}
static void transport(void) {
  gotel_env_storage_t s;
  gotel_options_t o = options();
  environment("OTEL_EXPORTER_OTLP_TIMEOUT", "321");
  environment("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf");
  environment("OTEL_EXPORTER_OTLP_CERTIFICATE", "/custom/ca.pem");
  environment("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Bearer token,x-key=a=b");
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  CHECK(o.exporter.connect_timeout_ms == 321 && o.exporter.read_timeout_ms == 321 &&
        o.exporter.flush_timeout_ms == 321);
  SAME(o.tls_ca_file, "/custom/ca.pem");
  CHECK(o.exporter.n_http_headers == 2);
  SAME(o.exporter.http_headers[0].name, "authorization");
  SAME(o.exporter.http_headers[0].value, "Bearer token");
  SAME(o.exporter.http_headers[1].value, "a=b");
}
static void disabled(void) {
  const char *values[] = {"true", "TRUE", "tRuE", "false", "1", "invalid", ""};
  for (size_t i = 0; i < sizeof(values) / sizeof(*values); ++i) {
    gotel_env_storage_t s;
    gotel_options_t o = options();
    environment("OTEL_SDK_DISABLED", values[i]);
    CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
    CHECK(o.signals == (i < 3 ? 0 : GOTEL_ALL));
  }
  environment("OTEL_SDK_DISABLED", "true");
  environment("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc");
  gotel_env_storage_t s;
  gotel_options_t o = options();
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK && !o.signals);
}
static void validation(void) {
  gotel_env_storage_t s;
  gotel_options_t o = options();
  CHECK(gotel_options_apply_env(NULL, &s, NULL) == OTLP_ERR_NULL);
  CHECK(gotel_options_apply_env(&o, NULL, NULL) == OTLP_ERR_NULL);
  CHECK(gotel_options_set_endpoint(NULL, &s, "http://localhost") == OTLP_ERR_NULL);
  CHECK(gotel_options_set_endpoint(&o, NULL, "http://localhost") == OTLP_ERR_NULL);
  const char *urls[] = {"ftp://collector", "http://user:secret@collector", "http://collector/?key=secret",
                        "http://collector/#fragment"};
  for (size_t i = 0; i < sizeof(urls) / sizeof(*urls); ++i)
    CHECK(gotel_options_set_endpoint(&o, &s, urls[i]) == OTLP_ERR_INVALID_ARGUMENT);
  const char *timeouts[] = {"-1", "0", "abc", "1ms", "4294967296"};
  for (size_t i = 0; i < sizeof(timeouts) / sizeof(*timeouts); ++i) {
    environment("OTEL_EXPORTER_OTLP_TIMEOUT", timeouts[i]);
    o = options();
    CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_ERR_INVALID_ARGUMENT);
  }
  clear();
  environment("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc");
#ifdef CGOTEL_WITH_GRPC
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  SAME(o.protocol, "grpc");
#else
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_ERR_INVALID_ARGUMENT);
#endif
  clear();
  environment("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "not-a-url");
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_ERR_INVALID_ARGUMENT);
  clear();
  char huge[2048];
  memset(huge, 'x', sizeof(huge) - 1);
  huge[sizeof(huge) - 1] = 0;
  memcpy(huge, "http://localhost/", 17);
  CHECK(gotel_options_set_endpoint(&o, &s, huge) == OTLP_ERR_OVERFLOW);
  memcpy(huge, "test=", 5);
  environment("OTEL_RESOURCE_ATTRIBUTES", huge);
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_ERR_OVERFLOW);
}
static void headers(void) {
  gotel_env_storage_t s;
  gotel_options_t o = options();
  environment("OTEL_EXPORTER_OTLP_HEADERS", "x-auth=base");
  environment("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "x-auth=trace");
  environment("OTEL_EXPORTER_OTLP_METRICS_HEADERS", "x-auth=metric");
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  gotel_host_t *h = NULL;
  CHECK(gotel_create(&o, &h) == OTLP_OK && h);
  CHECK(h->exporter->sig[0].gotel_headers_set && h->exporter->sig[1].gotel_headers_set);
  CHECK(!h->exporter->sig[2].gotel_headers_set);
  SAME(h->exporter->http_headers[0].value, "base");
  SAME(h->exporter->sig[0].gotel_headers[0].value, "trace");
  SAME(h->exporter->sig[1].gotel_headers[0].value, "metric");
  environment("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "x-auth=changed");
  memset(&s, 0, sizeof(s));
  SAME(h->exporter->http_headers[0].value, "base");
  SAME(h->exporter->sig[0].gotel_headers[0].value, "trace");
  gotel_destroy(h);
  environment("OTEL_EXPORTER_OTLP_LOGS_HEADERS", "x-auth=log");
  o = options();
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  h = NULL;
  CHECK(gotel_create(&o, &h) == OTLP_OK && h);
  CHECK(h->exporter->sig[2].gotel_headers_set);
  SAME(h->exporter->sig[2].gotel_headers[0].value, "log");
  gotel_destroy(h);
  // Applications that do not opt in retain explicit configuration.
  o = options();
  h = NULL;
  CHECK(gotel_create(&o, &h) == OTLP_OK && h);
  CHECK(!h->exporter->sig[0].gotel_headers_set && !h->exporter->sig[1].gotel_headers_set &&
        !h->exporter->sig[2].gotel_headers_set);
  gotel_destroy(h);
  clear();
  environment("OTEL_EXPORTER_OTLP_LOGS_HEADERS", "x-auth=bad\r\nInjected: value");
  o = options();
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  h = NULL;
  CHECK(gotel_create(&o, &h) == OTLP_ERR_INVALID_ARGUMENT && !h);
}
static void grpc_options(void) {
  gotel_env_storage_t s;
  gotel_options_t o = options();
  gotel_host_t *h = NULL;
  o.protocol = "unknown";
  CHECK(gotel_create(&o, &h) == OTLP_ERR_INVALID_ARGUMENT && !h);
  o.protocol = "grpc";
#ifdef CGOTEL_WITH_GRPC
  CHECK(gotel_options_set_endpoint(&o, &s, "http://127.0.0.1:4317") == OTLP_OK);
  SAME(o.exporter.endpoint, "http://127.0.0.1:4317");
  SAME(o.exporter.metrics_endpoint, o.exporter.endpoint);
  CHECK(gotel_create(&o, &h) == OTLP_OK);
  CHECK(h->exporter->sig[0].url.grpc && h->exporter->sig[0].url.port == 4317);
  SAME(h->exporter->sig[2].url.path, "/opentelemetry.proto.collector.logs.v1.LogsService/Export");
  gotel_destroy(h);
  h = NULL;
  o.exporter.endpoint = "http://127.0.0.1:4317/v1/traces";
  CHECK(gotel_create(&o, &h) == OTLP_ERR_INVALID_ARGUMENT && !h);
  o = options();
  o.protocol = "grpc";
  o.exporter.endpoint = NULL;
  CHECK(gotel_create(&o, &h) == OTLP_OK);
  CHECK(h->exporter->sig[0].url.grpc && h->exporter->sig[0].url.port == 4317 && h->exporter->sig[0].url.tls);
  gotel_destroy(h);
  environment("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc");
  environment("OTEL_EXPORTER_OTLP_ENDPOINT", "https://localhost:4317");
  environment("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "http://127.0.0.1:9000");
  o = options();
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_OK);
  SAME(o.exporter.endpoint, "https://localhost:4317");
  SAME(o.exporter.logs_endpoint, "http://127.0.0.1:9000");
  environment("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf");
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_ERR_INVALID_ARGUMENT);
#else
  CHECK(gotel_create(&o, &h) == OTLP_ERR_INVALID_ARGUMENT && !h);
  environment("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc");
  CHECK(gotel_options_apply_env(&o, &s, NULL) == OTLP_ERR_INVALID_ARGUMENT);
#endif
}
int main(int argc, char **argv) {
  CHECK(argc == 2);
  clear();
  if (!strcmp(argv[1], "defaults"))
    defaults();
  else if (!strcmp(argv[1], "endpoints"))
    endpoints();
  else if (!strcmp(argv[1], "identity"))
    identity();
  else if (!strcmp(argv[1], "transport"))
    transport();
  else if (!strcmp(argv[1], "disabled"))
    disabled();
  else if (!strcmp(argv[1], "validation"))
    validation();
  else if (!strcmp(argv[1], "headers"))
    headers();
  else if (!strcmp(argv[1], "grpc"))
    grpc_options();
  else
    CHECK(0);
  clear();
  return 0;
}
