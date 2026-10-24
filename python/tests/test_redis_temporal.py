import fakeredis
import fakeredis.aioredis
import pytest

from gotel import redistrace, temporaltrace
from gotel.testing import Capture


async def test_redis_async():
    client = fakeredis.aioredis.FakeRedis()
    try:
        with Capture() as capture:
            remove = redistrace.instrument(client, application=capture.application)
            assert redistrace.instrument(client, application=capture.application) is remove
            await client.set("private-key", "private-value")
            assert await client.get("private-key") == b"private-value"
            async with client.pipeline() as pipeline:
                pipeline.set("private-key", "private-value")
                pipeline.get("private-key")
                assert await pipeline.execute() == [True, b"private-value"]
            capture.require_span("storage.redis.PIPELINE")
            capture.require_span("storage.redis.SET")
            capture.require_no_values("private-key", "private-value")
            remove()
            remove()
    finally:
        await client.aclose()


def test_redis_sync_and_failure():
    with Capture() as capture:
        client = fakeredis.FakeRedis()
        remove = redistrace.instrument(client, application=capture.application)
        client.set("key", "secret-value")
        with pytest.raises(Exception):
            client.incr("key")
        capture.require_no_values("secret-value", "key")
        assert len(capture.spans) == 2
        remove()
        client.close()


def test_temporal_uses_sdk_interceptor_and_names():
    from temporalio.contrib.opentelemetry import TracingInterceptor

    with Capture("beanaries") as capture:
        interceptor = temporaltrace.interceptor(application=capture.application)
        assert isinstance(interceptor, TracingInterceptor)
        with interceptor.tracer.start_as_current_span("StartActivity:FetchProject"):
            pass
        span = interceptor.tracer.start_span("RunActivity:FetchProject")
        span.end()
        assert [span.name for span in capture.spans] == [
            "beanaries.temporal.activity.schedule.FetchProject",
            "beanaries.temporal.activity.execute.FetchProject",
        ]
