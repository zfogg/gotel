#ifndef GOTEL_TESTING_H
#define GOTEL_TESTING_H
#include <gotel/gotel.h>
#ifdef __cplusplus
extern "C" {
#endif
/* Optional test support: link gotel::testing. Never linked into production by
 * default. No assertion macros or test framework dependencies. */

/* Clocks are thread-local. Bind each producer thread independently, or supply
 * your own callbacks. Advancing either clock past UINT64_MAX is rejected. These
 * clocks control gotel sampling/measurement, not otlp-c's network backoff clock. */
void gotel_test_clock_set(uint64_t monotonic_ns, uint64_t realtime_ns);
otlp_status_t gotel_test_clock_advance(uint64_t nanoseconds);
uint64_t gotel_test_monotonic_ns(void);
uint64_t gotel_test_realtime_ns(void);

/* Script is borrowed through the host's lifetime (or until replaced). Only the
 * exporter owner accesses it. Exhaustion uses fallback_status (normally 200).
 * This transport bypasses HTTP I/O; use the Python collector for wire assertions. */
typedef struct {
  const int *statuses;
  size_t count, position;
  int fallback_status;
} gotel_test_script_t;
otlp_status_t gotel_test_transport(gotel_host_t *host, gotel_test_script_t *script);
/* Create a normal host, then install the no-I/O transport. */
otlp_status_t gotel_test_create(const gotel_options_t *options, gotel_host_t **output);

/* Thread-safe event recorder. Install gotel_test_event_record as the exporter
 * callback with the recorder as context. Stop/join producers before destroying. */
typedef struct gotel_test_events gotel_test_events_t;
typedef struct {
  uint64_t queue_full, batches, retries, dropped, partial_success, flush_failed;
} gotel_test_event_counts_t;
gotel_test_events_t *gotel_test_events_create(void);
void gotel_test_events_destroy(gotel_test_events_t *events);
void gotel_test_event_record(void *context, const otlp_event_t *event);
gotel_test_event_counts_t gotel_test_events_snapshot(const gotel_test_events_t *events);
#ifdef __cplusplus
}
#endif
#endif
