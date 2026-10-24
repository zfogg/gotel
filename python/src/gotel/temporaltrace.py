"""Name adaptation around Temporal's replay-aware OTel interceptor."""

from opentelemetry.trace import Tracer


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
    def __init__(self, application):
        self.application = application
        self.delegate = application.tracer("temporal")

    def start_span(self, raw, *args, **kwargs):
        return self.delegate.start_span(self.application.qualify(name(raw)), *args, **kwargs)

    def start_as_current_span(self, raw, *args, **kwargs):
        return self.delegate.start_as_current_span(self.application.qualify(name(raw)), *args, **kwargs)


def interceptor(*, application, always_create_workflow_spans: bool = False):
    """Register once on Client.connect; its workers inherit the same interceptor."""
    from temporalio.contrib.opentelemetry import TracingInterceptor

    return TracingInterceptor(tracer=_Tracer(application), always_create_workflow_spans=always_create_workflow_spans)
