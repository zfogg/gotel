"""Route-aware server names and bounded, credential-free HTTP client names."""

from __future__ import annotations

import re
from urllib.parse import urlsplit

_ID = re.compile(r"^(?:\d+|[0-9a-fA-F]{16,}|[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12})$")
_METHODS = frozenset("GET HEAD POST PUT DELETE CONNECT OPTIONS TRACE PATCH".split())


class Namer:
    def __init__(self, prefix: str):
        self.prefix = prefix.strip().rstrip(".")

    def _name(self, suffix):
        return f"{self.prefix}.{suffix}" if self.prefix else suffix

    def server_name(self, method: str, route: str | None) -> str:
        return self._name(f"http.server.{method if method in _METHODS else 'OTHER'} {route or '/{unmatched}'}")

    def client_name(self, method: str, url: str) -> str:
        parsed = urlsplit(url)
        host = parsed.hostname or "unknown"
        if ":" in host:
            host = f"[{host}]"
        if parsed.port:
            host += f":{parsed.port}"
        path = "/".join("{id}" if _ID.fullmatch(segment) else segment for segment in parsed.path.split("/")) or "/"
        return self._name(f"http.client.{method if method in _METHODS else 'OTHER'} {host}{path}")

    def instrument_fastapi(self, app, *, tracer_provider=None, meter_provider=None, server_request_hook=None):
        from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor

        if getattr(app.state, "gotel_http_remove", None):
            return app.state.gotel_http_remove
        if getattr(app, "_is_instrumented_by_opentelemetry", False):
            raise ValueError("remove existing FastAPI instrumentation before installing gotel")
        namer = self

        # Route resolution happens inside the router, after the OTel server hook.
        # Wrap each route's ASGI application so renaming occurs after resolution,
        # including on exceptions, while the server span is still recording.
        wrapped = []
        for route in app.routes:
            original = route.app
            path = getattr(route, "path", None)

            def wrap(original, path):
                async def routed(scope, receive, send):
                    from opentelemetry import trace

                    span = trace.get_current_span()
                    if scope["type"] == "http":
                        span.update_name(namer.server_name(scope["method"], path))
                        span.set_attribute("http.route", path or "/{unmatched}")
                    await original(scope, receive, send)

                return routed

            route.app = wrap(original, path)
            wrapped.append((route, original))

        def hook(span, scope):
            span.update_name(self.server_name(scope.get("method", "OTHER"), None))
            if server_request_hook:
                server_request_hook(span, scope)

        FastAPIInstrumentor.instrument_app(
            app,
            tracer_provider=tracer_provider,
            meter_provider=meter_provider,
            server_request_hook=hook,
            exclude_spans=["receive", "send"],
        )

        def remove():
            if getattr(app.state, "gotel_http_remove", None) is remove:
                FastAPIInstrumentor.uninstrument_app(app)
                for route, original in wrapped:
                    route.app = original
                del app.state.gotel_http_remove

        app.state.gotel_http_remove = remove
        return remove

    def instrument_httpx(self, client, *, tracer_provider=None, meter_provider=None, request_hook=None):
        import httpx
        from opentelemetry.instrumentation.httpx import HTTPXClientInstrumentor

        if getattr(client, "_gotel_http_remove", None):
            return client._gotel_http_remove
        if getattr(client, "_is_instrumented_by_opentelemetry", False):
            raise ValueError("remove existing HTTPX instrumentation before installing gotel")

        def hook(span, request):
            method = request.method.decode() if isinstance(request.method, bytes) else request.method
            span.update_name(self.client_name(method, str(request.url)))
            if request_hook:
                request_hook(span, request)

        async def async_hook(span, request):
            hook(span, request)

        HTTPXClientInstrumentor.instrument_client(
            client,
            tracer_provider=tracer_provider,
            meter_provider=meter_provider,
            request_hook=async_hook if isinstance(client, httpx.AsyncClient) else hook,
        )

        def remove():
            if getattr(client, "_gotel_http_remove", None) is remove:
                HTTPXClientInstrumentor.uninstrument_client(client)
                del client._gotel_http_remove

        client._gotel_http_remove = remove
        return remove
