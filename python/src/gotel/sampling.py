"""Critical/realtime head policy and bounded independent retained observations."""

from __future__ import annotations

import math
import threading
import time
from collections.abc import Callable, Sequence

from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.sdk.trace.sampling import ALWAYS_OFF, ALWAYS_ON, Decision, Sampler, SamplingResult, TraceIdRatioBased

from .outcomes import Outcome, apply


class _Limiter:
    def __init__(self):
        self.lock = threading.Lock()
        self.started: float | None = None
        self.count = 0

    def allow(self, now: float, cap: int) -> bool:
        with self.lock:
            if self.started is None or now - self.started >= 1:
                self.started, self.count = now, 0
            if self.count >= cap:
                return False
            self.count += 1
            return True


class Policy(Sampler):
    """Predicates receive span name and start attributes. Late attributes cannot
    change a dropped head decision. Realtime children inherit one root decision.
    """

    def __init__(
        self,
        delegate: Sampler,
        *,
        critical=None,
        realtime=None,
        realtime_rate=0.0,
        realtime_max_per_second=1,
        clock: Callable[[], float] = time.monotonic,
    ):
        if not math.isfinite(realtime_rate) or not 0 <= realtime_rate <= 1:
            raise ValueError("realtime_rate must be finite and in [0,1]")
        if realtime_max_per_second < 1:
            raise ValueError("realtime cap must be positive")
        self.delegate, self.critical, self.realtime = delegate, critical, realtime
        self.ratio = TraceIdRatioBased(realtime_rate)
        self.cap, self.clock, self.limit = realtime_max_per_second, clock, _Limiter()

    def should_sample(self, parent_context, trace_id, name, kind=None, attributes=None, links=None, trace_state=None):
        args = parent_context, trace_id, name, kind, attributes, links, trace_state
        attrs = attributes or {}
        if attrs.get("gotel.observation.retained") is True or self.critical and self.critical(name, attrs):
            return ALWAYS_ON.should_sample(*args)
        if self.realtime and self.realtime(name, attrs):
            parent = trace.get_current_span(parent_context).get_span_context()
            if parent.is_valid:
                return (ALWAYS_ON if parent.trace_flags.sampled else ALWAYS_OFF).should_sample(*args)
            result = self.ratio.should_sample(*args)
            if result.decision == Decision.RECORD_AND_SAMPLE and not self.limit.allow(self.clock(), self.cap):
                return SamplingResult(Decision.DROP, trace_state=result.trace_state)
            return result
        return self.delegate.should_sample(*args)

    def get_description(self):
        return f"gotel.policy({self.delegate.get_description()})"


class Retainer:
    """Use with a tracer provider configured with Policy. Emits a bounded root
    with a link to the original dropped operation; it never resamples that span.
    """

    def __init__(self, tracer: trace.Tracer, *, max_per_second=10, clock: Callable[[], float] = time.monotonic):
        if max_per_second < 1:
            raise ValueError("retention cap must be positive")
        self.tracer, self.cap, self.clock, self.limit = tracer, max_per_second, clock, _Limiter()

    def observe(self, name: str, outcome: Outcome, *, context=None, duration_ns=0, attributes=None) -> bool:
        parent = trace.get_current_span(context).get_span_context()
        if outcome != Outcome.ERROR or parent.trace_flags.sampled or not self.limit.allow(self.clock(), self.cap):
            return False
        attrs = dict(attributes or {})
        attrs["gotel.observation.retained"] = True
        links: Sequence[trace.Link] = [trace.Link(parent)] if parent.is_valid else []
        end = time.time_ns()
        span = self.tracer.start_span(
            name,
            context=Context(),
            start_time=end - max(0, duration_ns),
            attributes=attrs,
            links=links,
            record_exception=False,
        )
        apply(span, outcome)
        span.end(end_time=end)
        return span.get_span_context().trace_flags.sampled
