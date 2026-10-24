"""Own the standard OTel SDK lifecycle, with OTEL_* environment configuration."""

from __future__ import annotations

import asyncio
import logging
import os
import platform
import sys
import threading
import time
from dataclasses import dataclass, field
from typing import Any, TypedDict

from opentelemetry import metrics, trace
from opentelemetry._logs import get_logger_provider, set_logger_provider
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

_start_lock = threading.Lock()
_installed_global = False


class _ExporterOptions(TypedDict, total=False):
    endpoint: str
    timeout: float
    headers: dict[str, str]


@dataclass
class Config:
    service_name: str | None = None
    endpoint: str | None = None
    protocol: str | None = None
    resource_attributes: dict[str, Any] = field(default_factory=dict)
    signals: tuple[str, ...] = ("traces", "metrics", "logs")
    headers: dict[str, str] | None = None
    certificate_file: str | None = None
    timeout: float | None = None
    trace_exporter: Any = None
    metric_exporter: Any = None
    log_exporter: Any = None
    span_processors: tuple[Any, ...] = ()
    install_global: bool = True
    attach_logging_handler: bool = True


class Runtime:
    def __init__(self):
        self.tracer_provider = None
        self.meter_provider = None
        self.logger_provider = None
        self.logging_handler = None
        self._closed = False

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        deadline = time.monotonic() + timeout_millis / 1000
        success = True
        for provider in (self.tracer_provider, self.meter_provider, self.logger_provider):
            if provider is not None:
                remaining = max(0, int((deadline - time.monotonic()) * 1000))
                success = bool(provider.force_flush(remaining)) and success
        return success

    def shutdown(self):
        if self._closed:
            return
        self._closed = True
        if self.logging_handler is not None:
            logging.getLogger().removeHandler(self.logging_handler)
            self.logging_handler.close()
        errors = []
        for provider in (self.logger_provider, self.meter_provider, self.tracer_provider):
            if provider is not None:
                try:
                    provider.shutdown()
                except Exception as exc:
                    errors.append(exc)
        if errors:
            raise ExceptionGroup("gotel shutdown failed", errors)

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.shutdown()

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await asyncio.to_thread(self.shutdown)


def _exporter(config: Config, signal: str):
    custom = getattr(config, {"traces": "trace_exporter", "metrics": "metric_exporter", "logs": "log_exporter"}[signal])
    if custom is not None:
        return custom
    protocol = config.protocol or os.getenv(
        f"OTEL_EXPORTER_OTLP_{signal.upper()}_PROTOCOL", os.getenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
    )
    if protocol not in ("http/protobuf", "grpc"):
        raise ValueError(f"unsupported OTLP protocol: {protocol}")
    kwargs: _ExporterOptions = {}
    if config.timeout is not None:
        kwargs["timeout"] = config.timeout
    if config.endpoint is not None:
        kwargs["endpoint"] = (
            config.endpoint.rstrip("/") + f"/v1/{signal}" if protocol == "http/protobuf" else config.endpoint
        )
    if config.headers is not None:
        kwargs["headers"] = config.headers
    if config.certificate_file is not None:
        if protocol != "http/protobuf":
            raise ValueError("certificate_file is for HTTP; configure gRPC TLS with OTEL exporter environment settings")
    if protocol == "http/protobuf":
        from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

        factory = {"traces": OTLPSpanExporter, "metrics": OTLPMetricExporter, "logs": OTLPLogExporter}[signal]
        return factory(certificate_file=config.certificate_file, **kwargs)

    from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter as GRPCLogExporter
    from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter as GRPCMetricExporter
    from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter as GRPCSpanExporter

    grpc_factory = {"traces": GRPCSpanExporter, "metrics": GRPCMetricExporter, "logs": GRPCLogExporter}[signal]
    return grpc_factory(**kwargs)


def start(config: Config | None = None) -> Runtime:
    global _installed_global
    config = config or Config()
    with _start_lock:
        installing = (
            config.install_global and bool(config.signals) and os.getenv("OTEL_SDK_DISABLED", "false").lower() != "true"
        )
        if installing and _installed_global:
            raise RuntimeError("gotel global providers are already installed; reuse the runtime")
        if installing and (
            ("traces" in config.signals and not isinstance(trace.get_tracer_provider(), trace.ProxyTracerProvider))
            or ("metrics" in config.signals and isinstance(metrics.get_meter_provider(), MeterProvider))
            or ("logs" in config.signals and isinstance(get_logger_provider(), LoggerProvider))
        ):
            raise RuntimeError("OTel providers already configured; use adapters or install_global=False")
        runtime = _start(config)
        if installing:
            _installed_global = True
        return runtime


def _start(config: Config) -> Runtime:
    config = config or Config()
    if set(config.signals) - {"traces", "metrics", "logs"}:
        raise ValueError("signals must contain only traces, metrics, logs")
    runtime = Runtime()
    if os.getenv("OTEL_SDK_DISABLED", "false").lower() == "true":
        return runtime
    attributes = {
        "host.name": platform.node(),
        "os.type": platform.system().lower(),
        "process.pid": os.getpid(),
        "process.executable.path": sys.executable,
        "process.runtime.name": platform.python_implementation(),
        "process.runtime.version": platform.python_version(),
    }
    attributes.update(config.resource_attributes)
    if config.service_name is not None:
        attributes["service.name"] = config.service_name
    resource = Resource.create(attributes)
    try:
        if "traces" in config.signals:
            runtime.tracer_provider = TracerProvider(resource=resource, shutdown_on_exit=False)
            for processor in config.span_processors:
                runtime.tracer_provider.add_span_processor(processor)
            runtime.tracer_provider.add_span_processor(BatchSpanProcessor(_exporter(config, "traces")))
        if "metrics" in config.signals:
            reader = PeriodicExportingMetricReader(_exporter(config, "metrics"))
            runtime.meter_provider = MeterProvider(resource=resource, metric_readers=[reader], shutdown_on_exit=False)
        if "logs" in config.signals:
            runtime.logger_provider = LoggerProvider(resource=resource, shutdown_on_exit=False)
            runtime.logger_provider.add_log_record_processor(BatchLogRecordProcessor(_exporter(config, "logs")))
            runtime.logging_handler = LoggingHandler(logger_provider=runtime.logger_provider)
        if config.install_global:
            if runtime.tracer_provider is not None:
                trace.set_tracer_provider(runtime.tracer_provider)
            if runtime.meter_provider is not None:
                metrics.set_meter_provider(runtime.meter_provider)
            if runtime.logger_provider is not None:
                set_logger_provider(runtime.logger_provider)
        if config.attach_logging_handler and runtime.logging_handler is not None:
            logging.getLogger().addHandler(runtime.logging_handler)
    except Exception:
        runtime.shutdown()
        raise
    return runtime
