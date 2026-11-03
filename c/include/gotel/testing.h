/** @file testing.h
 * @brief Public Gotel test support.
 */
#pragma once

#include <gotel/gotel.h>
#ifdef __cplusplus
extern "C" {
#endif

/** Optional test support: link gotel::testing. Never linked into production by
 * default. No assertion macros or test framework dependencies. */

/** Clocks are thread-local. Bind each producer thread independently, or supply
 * your own callbacks. Advancing either clock past UINT64_MAX is rejected. These
 * clocks control gotel sampling/measurement, not otlp-c's network backoff clock. */
void gotel_test_clock_set(uint64_t monotonic_ns, uint64_t realtime_ns);

/** Advance both thread-local clocks, rejecting integer overflow. */
otlp_status_t gotel_test_clock_advance(uint64_t nanoseconds);

/** Return the current thread-local monotonic test clock. */
uint64_t gotel_test_monotonic_ns(void);

/** Return the current thread-local realtime test clock. */
uint64_t gotel_test_realtime_ns(void);

/** Script is borrowed through the host's lifetime (or until replaced). Only the
 * exporter owner accesses it. Exhaustion uses fallback_status (normally 200).
 * This transport bypasses HTTP I/O; use the Python collector for wire assertions. */
typedef struct {
  const int *statuses;
  size_t count, position;
  int fallback_status;
} gotel_test_script_t;

/** Install the borrowed scripted transport on a host. */
otlp_status_t gotel_test_transport(gotel_host_t *host, gotel_test_script_t *script);

/** Create a normal host, then install the no-I/O transport. */
otlp_status_t gotel_test_create(const gotel_options_t *options, gotel_host_t **output);

/** Owned offline trace capture. Only the optional gotel::testing library links
 * the serializer. Captures contain actual completed span attributes, events,
 * links, timestamps and exporter resource metadata, before queue acceptance;
 * they do not prove delivery. No redaction beyond application instrumentation
 * is performed. Treat the bytes as potentially sensitive local test artifacts. */
typedef struct gotel_test_capture gotel_test_capture_t;

/** Allocate at most max_bytes retained protobuf bytes (must be nonzero).
 * Serialization uses additional temporary memory proportional to one span. */
gotel_test_capture_t *gotel_test_capture_create(size_t max_bytes);

/** Install capture and disable all network export on host. Call before any
 * producers/tick owner start. One capture can accept concurrent submissions
 * from multiple hosts. Attachment does not transfer ownership; stop producers
 * and detach or destroy all attached hosts before destroying capture. */
otlp_status_t gotel_test_capture_attach(gotel_host_t *host, gotel_test_capture_t *capture);

/** Remove capture after all host producers stop. Null transport stays enabled.
 * Safe if no capture is installed. Never detach during concurrent submission. */
void gotel_test_capture_detach(gotel_host_t *host);

/** Borrow a complete ExportTraceServiceRequest protobuf buffer until destroy.
 * Call after all attached producers stop. Empty capture returns NULL/0. An
 * encoding/allocation/byte-limit failure is sticky and returns no partial data;
 * callers must check the status before asserting on capture contents. */
otlp_status_t gotel_test_capture_read(const gotel_test_capture_t *capture, const uint8_t **data, size_t *bytes);

/** Free capture after detaching or destroying its hosts. NULL is safe. */
void gotel_test_capture_destroy(gotel_test_capture_t *capture);

/** Thread-safe event recorder. Install gotel_test_event_record as the exporter
 * callback with the recorder as context. Stop/join producers before destroying. */
typedef struct gotel_test_events gotel_test_events_t;

typedef struct {
  uint64_t queue_full, batches, retries, dropped, partial_success, flush_failed;
} gotel_test_event_counts_t;

/** Allocate a thread-safe event recorder. */
gotel_test_events_t *gotel_test_events_create(void);

/** Release the recorder after its producers have stopped. */
void gotel_test_events_destroy(gotel_test_events_t *events);

/** Record an exporter event using the recorder callback context. */
void gotel_test_event_record(void *context, const otlp_event_t *event);

/** Copy the recorder event counters. */
gotel_test_event_counts_t gotel_test_events_snapshot(const gotel_test_events_t *events);

#ifdef __cplusplus
}
#endif
