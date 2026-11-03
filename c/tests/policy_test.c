#include <assert.h>
#include <gotel/testing.h>
#include <string.h>

int main(void) {
  gotel_context_t parent = {.valid = true, .sampled = true};
  parent.trace_id[0] = 1;
  parent.span_id[0] = 2;
  gotel_context_t inherited = gotel_operation_parent(parent, GOTEL_PARENT_INHERIT);
  assert(inherited.valid && !inherited.realtime && !memcmp(parent.trace_id, inherited.trace_id, 16));
  gotel_context_t bounded = gotel_operation_parent(parent, GOTEL_PARENT_BOUNDED);
  assert(bounded.valid && bounded.realtime && !parent.realtime);
  assert(!gotel_operation_parent(parent, GOTEL_PARENT_INDEPENDENT).valid);
  static const gotel_operation_t operations[] = {
      {"fixture.owner", false}, {"fixture.frame", true}, {"fixture.error", false}};
  gotel_options_t options = {0};
  options.exporter.service_name = "policy";
  options.operations = operations;
  options.operation_count = 3;
  options.signals = GOTEL_TRACES;
  options.realtime_sample_rate = 0;
  options.monotonic_ns = gotel_test_monotonic_ns;
  options.realtime_ns = gotel_test_realtime_ns;
  gotel_test_clock_set(1000000000, 1700000000000000000ULL);
  gotel_host_t *host = NULL;
  assert(gotel_test_create(&options, &host) == OTLP_OK);
  gotel_scope_t owner, frame, retained;
  gotel_begin(host, &owner, 0, NULL);
  bounded = gotel_operation_parent(owner.context, GOTEL_PARENT_BOUNDED);
  gotel_begin(host, &frame, 1, &bounded);
  assert(frame.context.sampled == owner.context.sampled);
  gotel_end(&frame);
  parent.sampled = false;
  for (int i = 0; i < 10; ++i) {
    assert(gotel_begin_retained(host, &retained, 2, &parent));
    assert(retained.context.sampled && memcmp(retained.context.trace_id, parent.trace_id, 16));
    gotel_outcome(&retained, GOTEL_OUTCOME_DEFERRED, NULL);
    assert(!retained.failed);
    gotel_outcome(&retained, GOTEL_OUTCOME_ERROR, "fixture");
    assert(retained.failed);
    gotel_outcome(&retained, GOTEL_OUTCOME_CANCELLED, NULL);
    assert(!retained.failed);
    gotel_end(&retained);
  }
  assert(!gotel_begin_retained(host, &retained, 2, &parent));
  assert(!retained.active);
  assert(!gotel_begin_retained(host, &retained, 1, &parent));
  assert(gotel_test_clock_advance(1000000000) == OTLP_OK);
  assert(gotel_begin_retained(host, &retained, 2, &parent));
  gotel_end(&retained);
  gotel_end(&owner);
  assert(gotel_drain(host) == OTLP_OK);
  gotel_destroy(host);
}
