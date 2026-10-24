"""In-process capture without installing process-global OTel providers."""

from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from .application import Application


class Capture:
    def __init__(self, prefix="test"):
        self.exporter = InMemorySpanExporter()
        self.provider = TracerProvider(shutdown_on_exit=False)
        self.provider.add_span_processor(SimpleSpanProcessor(self.exporter))
        self.application = Application(prefix, tracer_provider=self.provider)

    @property
    def spans(self):
        return self.exporter.get_finished_spans()

    def require_span(self, name):
        found = [span for span in self.spans if span.name == name]
        assert found, f"missing span {name!r}"
        return found[0]

    def require_no_duplicate_spans(self):
        identities = [(span.context.trace_id, span.context.span_id) for span in self.spans]
        assert len(set(identities)) == len(identities), "duplicate span identities"

    def require_bounded_names(self, *, forbidden=(), max_names=100):
        assert len({span.name for span in self.spans}) <= max_names
        for span in self.spans:
            assert not any(value in span.name for value in forbidden), span.name

    def require_no_values(self, *values):
        for span in self.spans:
            exported = repr((span.name, dict(span.attributes), span.events, span.status.description))
            assert not any(value in exported for value in values), "sensitive value in telemetry"

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.provider.shutdown()
