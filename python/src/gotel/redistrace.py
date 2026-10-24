"""Per-client Redis spans. Commands are bounded; keys and values are never read."""

from __future__ import annotations

import inspect
from contextlib import contextmanager
from functools import wraps

from opentelemetry import trace
from opentelemetry.instrumentation.utils import is_instrumentation_enabled, suppress_instrumentation

_COMMANDS = frozenset(
    "GET SET DEL EXISTS EXPIRE TTL INCR DECR INCRBY DECRBY MGET MSET HGET HSET HDEL HGETALL "
    "LPUSH RPUSH LPOP RPOP LRANGE SADD SREM SMEMBERS ZADD ZREM ZRANGE PING PUBLISH "
    "EVAL EVALSHA SCAN UNLINK SETEX SETNX HINCRBY ZCARD ZCOUNT XADD XREAD".split()
)


def instrument(client, *, application):
    if getattr(client, "_gotel_redis_remove", None):
        return client._gotel_redis_remove
    original = client.execute_command
    original_pipeline = getattr(client, "pipeline", None)

    @contextmanager
    def operation(args):
        if not is_instrumentation_enabled():
            yield
            return
        command = args[0] if args else "OTHER"
        if isinstance(command, bytes):
            command = command.decode("ascii", errors="replace")
        command = command.upper() if isinstance(command, str) else "OTHER"
        if command not in _COMMANDS and command != "PIPELINE":
            command = "OTHER"
        with application.tracer("redis").start_as_current_span(
            application.qualify(f"storage.redis.{command.lower()}"),
            kind=trace.SpanKind.CLIENT,
            attributes={"db.system.name": "redis", "db.operation.name": command},
            record_exception=False,
            set_status_on_exception=False,
        ) as span:
            try:
                with suppress_instrumentation():
                    yield
            except BaseException as exc:
                span.set_attribute("error.type", type(exc).__qualname__)
                span.set_status(trace.Status(trace.StatusCode.ERROR))
                raise

    if inspect.iscoroutinefunction(original):

        @wraps(original)
        async def execute(*args, **kwargs):
            with operation(args):
                return await original(*args, **kwargs)
    else:

        @wraps(original)
        def execute(*args, **kwargs):
            with operation(args):
                return original(*args, **kwargs)

    client.execute_command = execute

    if original_pipeline is not None:

        @wraps(original_pipeline)
        def pipeline(*args, **kwargs):
            instance = original_pipeline(*args, **kwargs)
            original_execute = instance.execute
            if inspect.iscoroutinefunction(original_execute):

                @wraps(original_execute)
                async def execute_pipeline(*args, **kwargs):
                    with operation(("PIPELINE",)):
                        return await original_execute(*args, **kwargs)
            else:

                @wraps(original_execute)
                def execute_pipeline(*args, **kwargs):
                    with operation(("PIPELINE",)):
                        return original_execute(*args, **kwargs)

            instance.execute = execute_pipeline
            return instance

        client.pipeline = pipeline

    def remove():
        if getattr(client, "_gotel_redis_remove", None) is remove:
            client.execute_command = original
            if original_pipeline is not None:
                client.pipeline = original_pipeline
            del client._gotel_redis_remove

    client._gotel_redis_remove = remove
    return remove
