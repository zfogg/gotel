#define _POSIX_C_SOURCE 200809L
#include <ctype.h>
#include <gotel/gotel.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static uint64_t clock_ns(clockid_t which) {
  struct timespec value;
  if (clock_gettime(which, &value) != 0)
    return 0;
  return (uint64_t)value.tv_sec * 1000000000u + (uint64_t)value.tv_nsec;
}
static uint64_t monotonic_ns(void) { return clock_ns(CLOCK_MONOTONIC); }
static uint64_t realtime_ns(void) { return clock_ns(CLOCK_REALTIME); }

static int hex_byte(const char *text) {
  int high = isdigit((unsigned char)text[0])
                 ? text[0] - '0'
                 : tolower((unsigned char)text[0]) - 'a' + 10;
  int low = isdigit((unsigned char)text[1])
                ? text[1] - '0'
                : tolower((unsigned char)text[1]) - 'a' + 10;
  if (high < 0 || high > 15 || low < 0 || low > 15)
    return -1;
  return (high << 4) | low;
}

static bool parent_from_env(gotel_context_t *parent) {
  const char *header = getenv("TRACEPARENT");
  if (!header || strlen(header) != 55 || strncmp(header, "00-", 3) ||
      header[35] != '-' || header[52] != '-')
    return false;
  for (size_t i = 0; i < sizeof(parent->trace_id); ++i) {
    int value = hex_byte(header + 3 + i * 2);
    if (value < 0)
      return false;
    parent->trace_id[i] = (uint8_t)value;
  }
  for (size_t i = 0; i < sizeof(parent->span_id); ++i) {
    int value = hex_byte(header + 36 + i * 2);
    if (value < 0)
      return false;
    parent->span_id[i] = (uint8_t)value;
  }
  int flags = hex_byte(header + 53);
  if (flags < 0)
    return false;
  parent->sampled = (flags & 1) != 0;
  parent->valid = true;
  return true;
}

int main(void) {
  static const gotel_operation_t operations[] = {
      {"demo.worker.count_words", false}};
  gotel_options_t options = {0};
  options.exporter.service_name = "gotel-demo-worker";
  options.exporter.queue_capacity = 64;
  options.exporter.batch_ms = 1;
  options.exporter.flush_timeout_ms = 5000;
  options.signals = GOTEL_ALL;
  options.realtime_sample_rate = 1;
  options.monotonic_ns = monotonic_ns;
  options.realtime_ns = realtime_ns;
  options.operations = operations;
  options.operation_count = 1;
  gotel_env_storage_t environment;
  if (gotel_options_apply_env(&options, &environment, "http://spaniel:4318") !=
      OTLP_OK)
    return 1;
  gotel_host_t *host = NULL;
  if (gotel_create(&options, &host) != OTLP_OK)
    return 1;

  gotel_context_t parent = {0};
  bool has_parent = parent_from_env(&parent);
  gotel_scope_t scope;
  gotel_begin(host, &scope, 0, has_parent ? &parent : NULL);
  size_t words = 0;
  bool in_word = false;
  int ch;
  while ((ch = getchar()) != EOF) {
    if (isspace((unsigned char)ch)) {
      in_word = false;
    } else if (!in_word) {
      ++words;
      in_word = true;
    }
  }
  gotel_attr_int(&scope, "demo.word_count", (int64_t)words);
  gotel_log(host, OTLP_SEVERITY_INFO, "word count complete", __FILE__, __LINE__,
            __func__);
  gotel_end(&scope);
  gotel_tick(host);
  otlp_status_t status = gotel_drain(host);
  gotel_destroy(host);
  if (status != OTLP_OK)
    return 1;
  printf("%zu\n", words);
  return 0;
}
