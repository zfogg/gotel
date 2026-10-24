from __future__ import annotations

import inspect
from contextlib import contextmanager
from functools import wraps

from opentelemetry import trace


class _Tracer(trace.Tracer):
    def __init__(self, application, delegate):
        self.application = application
        self.delegate = delegate

    def start_span(self, name, *args, **kwargs):
        return self.delegate.start_span(self.application.qualify(name), *args, **kwargs)

    def start_as_current_span(self, name, *args, **kwargs):
        return self.delegate.start_as_current_span(self.application.qualify(name), *args, **kwargs)


def correlation() -> dict[str, str]:
    context = trace.get_current_span().get_span_context()
    if not context.is_valid:
        return {}
    return {"trace_id": format(context.trace_id, "032x"), "span_id": format(context.span_id, "016x")}


class Application:
    def __init__(self, prefix: str, *, tracer_provider=None):
        self.prefix = prefix.strip().rstrip(".")
        self.tracer_provider = tracer_provider

    def qualify(self, name: str) -> str:
        name = name.strip()
        if name == self.prefix or name.startswith(self.prefix + "."):
            return name
        return f"{self.prefix}.{name}" if self.prefix else name

    def tracer(self, component: str = "operations"):
        return _Tracer(self, trace.get_tracer(self.qualify(component), tracer_provider=self.tracer_provider))

    @contextmanager
    def operation(self, name: str, *, attributes=None, kind=trace.SpanKind.INTERNAL, context=None, links=None):
        with self.tracer(name.split(".", 1)[0]).start_as_current_span(
            self.qualify(name),
            attributes=attributes,
            kind=kind,
            context=context,
            links=links,
        ) as span:
            yield span

    def traced(self, name: str):
        def decorate(function):
            if inspect.iscoroutinefunction(function):

                @wraps(function)
                async def asynchronous(*args, **kwargs):
                    with self.operation(name):
                        return await function(*args, **kwargs)

                return asynchronous

            @wraps(function)
            def synchronous(*args, **kwargs):
                with self.operation(name):
                    return function(*args, **kwargs)

            return synchronous

        return decorate
