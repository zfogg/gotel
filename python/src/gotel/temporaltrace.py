"""Name adaptation around Temporal's replay-aware OTel interceptor."""

from contextlib import contextmanager

from opentelemetry import trace
from opentelemetry.trace import Tracer

from .outcomes import Outcome, apply, classify


class _SafeSpan(trace.Span):
    """Restrict SDK error/status text while retaining SDK span ownership."""

    def __init__(self, delegate):
        self.delegate = delegate
        self.error = None
        self.status = None

    def get_span_context(self):
        return self.delegate.get_span_context()

    def is_recording(self):
        return self.delegate.is_recording()

    def set_attribute(self, key, value):
        return self.delegate.set_attribute(key, value)

    def set_attributes(self, attributes):
        return self.delegate.set_attributes(attributes)

    def add_event(self, name, attributes=None, timestamp=None):
        return self.delegate.add_event(name, attributes, timestamp)

    def add_link(self, context, attributes=None):
        return self.delegate.add_link(context, attributes)

    def update_name(self, name):
        return self.delegate.update_name(name)

    def set_status(self, status, description=None):
        self.status = status.status_code if isinstance(status, trace.Status) else status

    def record_exception(self, exception, attributes=None, timestamp=None, escaped=False):
        self.error = exception

    def finish(self, error=None):
        error = error or self.error
        result = classify(error)
        if error is not None:
            from temporalio.exceptions import ApplicationError, ApplicationErrorCategory, CancelledError

            if isinstance(error, ApplicationError) and error.category == ApplicationErrorCategory.BENIGN:
                result = Outcome.SUCCESS
            elif isinstance(error, CancelledError):
                result = Outcome.CANCELLED
        elif self.status == trace.StatusCode.ERROR:
            result = Outcome.ERROR
        apply(self.delegate, result, error)

    def end(self, end_time=None):
        self.finish()
        return self.delegate.end(end_time)


def name(raw: str) -> str:
    operation, separator, kind = raw.partition(":")
    family = {
        "RunWorkflow": "workflow",
        "StartWorkflow": "workflow",
        "StartActivity": "activity.schedule",
        "RunActivity": "activity.execute",
        "RunChildWorkflow": "workflow.child",
        "StartChildWorkflow": "workflow.child",
    }.get(operation, operation.lower())
    return f"temporal.{family}" + (f".{kind}" if separator else "")


class _Tracer(Tracer):
    def __init__(self, application, execution_links=None):
        self.application = application
        self.delegate = application.tracer("temporal")
        self.execution_links = execution_links

    def _options(self, raw, kwargs):
        if raw.startswith("RunActivity:"):
            from temporalio import activity

            if activity.in_activity():
                attrs = dict(kwargs.get("attributes") or {})
                attrs.update(activity_attributes(activity.info()))
                kwargs["attributes"] = attrs
        if raw.startswith(("RunActivity:", "RunWorkflow:")):
            links: list[trace.Link] = list(kwargs.get("links") or ())
            parent = trace.get_current_span(kwargs.get("context")).get_span_context()
            if parent.is_valid:
                links.append(trace.Link(parent))
            if self.execution_links is not None:
                links.extend(self.execution_links())
            kwargs["links"] = links[:16]
        # Temporal's SDK remains the replay/lifecycle owner. Disable automatic
        # exception events: driver/activity errors may contain payload values.
        kwargs["record_exception"] = False
        return kwargs

    def start_span(self, raw, *args, **kwargs):
        return _SafeSpan(
            self.delegate.start_span(self.application.qualify(name(raw)), *args, **self._options(raw, kwargs))
        )

    @contextmanager
    def start_as_current_span(self, raw, *args, **kwargs):
        kwargs["set_status_on_exception"] = False
        with self.delegate.start_as_current_span(
            self.application.qualify(name(raw)), *args, **self._options(raw, kwargs)
        ) as span:
            safe = _SafeSpan(span)
            try:
                yield safe
            except BaseException as error:
                safe.finish(error)
                raise
            else:
                safe.finish()


def interceptor(*, application, always_create_workflow_spans: bool = False, execution_links=None):
    """Register once on Client.connect; its workers inherit the same interceptor."""
    from temporalio.contrib.opentelemetry import TracingInterceptor

    from ._temporalworkflow import WorkflowInterceptor

    class Interceptor(TracingInterceptor):
        def workflow_interceptor_class(self, input):
            super().workflow_interceptor_class(input)
            return WorkflowInterceptor

    return Interceptor(
        tracer=_Tracer(application, execution_links), always_create_workflow_spans=always_create_workflow_spans
    )


def activity_attributes(info):
    """Safe retry/attempt/delay metadata; no inputs or heartbeat payloads.

    Current-attempt schedule time measures queue delay separately from original
    schedule delay, which includes retry backoff. Negative clock skew is clamped.
    """
    attrs = {
        "temporal.activity.attempt": info.attempt,
        "temporal.activity.retry": info.attempt > 1,
        "temporal.activity.id": info.activity_id,
        "temporal.workflow.id": info.workflow_id or "",
        "temporal.workflow.run_id": info.workflow_run_id or "",
        "temporal.activity.schedule_to_start_seconds": max(
            0.0, (info.started_time - info.scheduled_time).total_seconds()
        ),
    }
    current = getattr(info, "current_attempt_scheduled_time", None)
    if current is not None:
        attrs["temporal.activity.attempt_schedule_to_start_seconds"] = max(
            0.0, (info.started_time - current).total_seconds()
        )
    retry = getattr(info, "retry_policy", None)
    if retry is not None:
        attrs["temporal.activity.retry.maximum_attempts"] = retry.maximum_attempts
    return attrs
