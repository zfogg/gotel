"""Explicit completion semantics; errors and cancellation are never rewritten."""

from __future__ import annotations

import asyncio
from contextlib import contextmanager
from contextvars import ContextVar
from enum import StrEnum

from opentelemetry import trace


class Outcome(StrEnum):
    """Bounded completion values; these do not replace returned driver errors."""

    SUCCESS = "success"
    ERROR = "error"
    CANCELLED = "cancelled"
    DEFERRED = "deferred"


_override: ContextVar[Outcome | None] = ContextVar("gotel.outcome", default=None)


@contextmanager
def completion(value: Outcome):
    """Explicit telemetry outcome for application-owned resumable work.

    The owner decides whether work is safely deferred; this scope never suppresses
    exceptions or treats arbitrary timeouts as resumable.
    """
    token = _override.set(Outcome(value))
    try:
        yield
    finally:
        _override.reset(token)


def classify(error: BaseException | None) -> Outcome:
    """Classify using an explicit owner override or native cancellation type."""
    if error is None:
        return Outcome.SUCCESS
    override = _override.get()
    if override is not None:
        return override
    if isinstance(error, asyncio.CancelledError):
        return Outcome.CANCELLED
    return Outcome.ERROR


def apply(span: trace.Span, value: Outcome, error: BaseException | None = None) -> None:
    """Set completion/status without ending a span or retaining error messages."""
    span.set_attribute("operation.outcome", value.value)
    if value == Outcome.DEFERRED:
        span.add_event("work_budget_exhausted")
    if value == Outcome.ERROR:
        span.set_status(trace.Status(trace.StatusCode.ERROR))
        if error is not None:
            span.set_attribute("error.type", type(error).__qualname__)
