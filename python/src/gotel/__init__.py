"""GotelApp-owned operation names on standard OpenTelemetry spans."""

from .application import GotelApp, correlation

__all__ = ["GotelApp", "Application", "correlation"]

# Compatibility for consumers of the already published Python API.
Application = GotelApp
