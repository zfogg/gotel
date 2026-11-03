import asyncio
from concurrent.futures import ThreadPoolExecutor
from dataclasses import replace
from datetime import UTC, datetime, timedelta

import pytest
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.sdk.trace.sampling import ALWAYS_OFF, Decision
from temporalio.common import RetryPolicy
from temporalio.exceptions import ApplicationError, ApplicationErrorCategory, CancelledError
from temporalio.testing import ActivityEnvironment
from temporalio.worker import ActivityInboundInterceptor, ExecuteActivityInput

from gotel import sampling, temporaltrace
from gotel.outcomes import Outcome, completion
from gotel.testing import Capture


def test_sampling_root_cap_parent_decision_and_late_critical_attribute():
    now = [0.0]
    policy = sampling.Policy(
        ALWAYS_OFF, realtime=lambda name, attrs: True, realtime_rate=1, realtime_max_per_second=3, clock=lambda: now[0]
    )

    def decide(index):
        return policy.should_sample(None, index + 1, "frame").decision == Decision.RECORD_AND_SAMPLE

    with ThreadPoolExecutor(max_workers=8) as threads:
        assert sum(threads.map(decide, range(100))) == 3
    now[0] = 1.0
    assert decide(100)
    parent = trace.SpanContext(1, 2, False, trace.TraceFlags(trace.TraceFlags.SAMPLED))
    context = trace.set_span_in_context(trace.NonRecordingSpan(parent))
    assert policy.should_sample(context, 1, "frame").decision == Decision.RECORD_AND_SAMPLE
    with pytest.raises(ValueError):
        sampling.Policy(ALWAYS_OFF, realtime_rate=float("nan"))


def test_retained_observation_is_independent_linked_and_bounded():
    exporter = InMemorySpanExporter()
    provider = TracerProvider(sampler=sampling.Policy(ALWAYS_OFF), shutdown_on_exit=False)
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    try:
        tracer = provider.get_tracer("test")
        dropped = tracer.start_span("ordinary")
        dropped.set_attribute("operation.class", "failure")
        retainer = sampling.Retainer(tracer, max_per_second=2, clock=lambda: 0.0)
        context = trace.set_span_in_context(dropped)
        assert retainer.observe("failure", Outcome.ERROR, context=context, duration_ns=1234)
        assert retainer.observe("failure", Outcome.ERROR, context=context)
        assert not retainer.observe("failure", Outcome.ERROR, context=context)
        assert not retainer.observe("success", Outcome.SUCCESS, context=context)
        dropped.end()
        spans = exporter.get_finished_spans()
        assert len(spans) == 2
        for span in spans:
            assert span.parent is None
            assert span.context.trace_id != dropped.get_span_context().trace_id
            assert span.links[0].context == dropped.get_span_context()
        assert spans[0].end_time is not None and spans[0].start_time is not None
        assert spans[0].end_time - spans[0].start_time == 1234
    finally:
        provider.shutdown()


@pytest.mark.parametrize(
    "kind,expected", [("error", "error"), ("benign", "success"), ("cancel", "cancelled"), ("budget", "deferred")]
)
async def test_temporal_actual_sdk_activity_outcomes_attempts_and_privacy(kind, expected):
    error = {
        "error": RuntimeError("PRIVATE activity payload"),
        "benign": ApplicationError("PRIVATE activity payload", category=ApplicationErrorCategory.BENIGN),
        "cancel": CancelledError("PRIVATE activity payload"),
        "budget": TimeoutError("PRIVATE activity payload"),
    }[kind]

    class Next(ActivityInboundInterceptor):
        def __init__(self):
            pass

        async def execute_activity(self, input):
            raise error

    env = ActivityEnvironment()
    now = datetime.now(UTC)
    env.info = replace(
        env.info,
        attempt=3,
        scheduled_time=now,
        started_time=now + timedelta(seconds=3),
        current_attempt_scheduled_time=now + timedelta(seconds=1),
        retry_policy=RetryPolicy(maximum_attempts=5),
    )
    prior = trace.SpanContext(1, 2, False)
    with Capture("") as capture:
        tracing = temporaltrace.interceptor(
            application=capture.application, execution_links=lambda: [trace.Link(prior)]
        )
        inbound = tracing.intercept_activity(Next())
        input = ExecuteActivityInput(fn=lambda: None, args=[], executor=None, headers={})

        async def run():
            if kind == "budget":
                with completion(Outcome.DEFERRED):
                    return await inbound.execute_activity(input)
            return await inbound.execute_activity(input)

        with pytest.raises(type(error)) as caught:
            await env.run(run)
        assert caught.value is error
        assert len(capture.spans) == 1
        span = capture.spans[0]
        assert span.attributes["temporal.activity.attempt"] == 3
        assert span.attributes["temporal.activity.retry"] is True
        assert span.attributes["temporal.activity.schedule_to_start_seconds"] == 3.0
        assert span.attributes["temporal.activity.attempt_schedule_to_start_seconds"] == 2.0
        assert span.attributes["operation.outcome"] == expected
        assert (span.status.status_code == trace.StatusCode.ERROR) == (kind == "error")
        assert ("error.type" in span.attributes) == (kind == "error")
        assert len(span.links) == 1 and span.links[0].context == prior
        capture.require_no_values("PRIVATE activity payload")


async def test_temporal_async_cancellation_has_no_exception_event():
    with Capture("") as capture:
        tracing = temporaltrace.interceptor(application=capture.application)
        with pytest.raises(asyncio.CancelledError):
            with tracing.tracer.start_as_current_span("RunActivity:Cancelled"):
                raise asyncio.CancelledError("PRIVATE")
        assert capture.spans[0].attributes["operation.outcome"] == "cancelled"
        capture.require_no_values("PRIVATE")


def test_workflow_metadata_uses_sdk_completed_observation(monkeypatch):
    from types import SimpleNamespace

    from temporalio import workflow
    from temporalio.contrib.opentelemetry import TracingWorkflowInboundInterceptor

    from gotel._temporalworkflow import WorkflowInterceptor

    observed = []
    monkeypatch.setattr(workflow, "info", lambda: SimpleNamespace(attempt=4, workflow_id="workflow", run_id="run"))
    monkeypatch.setattr(
        TracingWorkflowInboundInterceptor,
        "_completed_span",
        lambda self, name, *args, **kwargs: observed.append((name, kwargs)),
    )
    wrapped = object.__new__(WorkflowInterceptor)
    wrapped._completed_span("RunWorkflow:Refresh", additional_attributes={"existing": True})
    assert len(observed) == 1
    attrs = observed[0][1]["additional_attributes"]
    assert attrs["temporal.workflow.attempt"] == 4
    assert attrs["temporal.workflow.retry"] is True
    assert attrs["temporal.workflow.run_id"] == "run"
    assert attrs["existing"] is True
