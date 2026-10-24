import httpx
import pytest
from fastapi import FastAPI

from gotel.httptrace import Namer
from gotel.testing import Capture


async def test_fastapi_resolved_route_and_unmatched():
    app = FastAPI()

    @app.get("/items/{item_id}")
    def item(item_id: str):
        return {"id": item_id}

    with Capture("beanaries") as capture:
        namer = Namer("beanaries")
        remove = namer.instrument_fastapi(app, tracer_provider=capture.provider)
        assert namer.instrument_fastapi(app) is remove
        async with httpx.AsyncClient(transport=httpx.ASGITransport(app), base_url="http://test") as client:
            assert (await client.get("/items/123?secret=abc")).status_code == 200
            assert (await client.get("/missing/456")).status_code == 404
        assert [span.name for span in capture.spans] == [
            "beanaries.http.server.GET /items/{item_id}",
            "beanaries.http.server.GET /{unmatched}",
        ]
        remove()
        remove()


async def test_httpx_async_failure_and_removal():
    async def fail(request):
        raise httpx.ConnectError("unavailable")

    with Capture("beanaries") as capture:
        async with httpx.AsyncClient(transport=httpx.MockTransport(fail)) as client:
            remove = Namer("beanaries").instrument_httpx(client, tracer_provider=capture.provider)
            with pytest.raises(httpx.ConnectError):
                await client.get("https://user:password@example.com:443/items/123?token=secret")
            assert capture.spans[0].name == "beanaries.http.client.GET example.com/items/{id}"
            remove()
            remove()


def test_httpx_sync():
    with Capture() as capture, httpx.Client(transport=httpx.MockTransport(lambda req: httpx.Response(200))) as client:
        remove = Namer("test").instrument_httpx(client, tracer_provider=capture.provider)
        client.get("https://example.com/items/123")
        capture.require_span("test.http.client.GET example.com/items/{id}")
        remove()
