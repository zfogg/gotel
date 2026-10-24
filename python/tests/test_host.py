import logging
import subprocess
import sys

import pytest
from opentelemetry.sdk._logs.export import InMemoryLogRecordExporter
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from gotel import GotelApp
from gotel.otelhost import Config, start


def test_traces_logs_flush_shutdown():
    from opentelemetry.sdk._logs import LoggingHandler

    traces = InMemorySpanExporter()
    logs = InMemoryLogRecordExporter()
    with start(
        Config(
            service_name="beanaries-api",
            signals=("traces", "logs"),
            trace_exporter=traces,
            log_exporter=logs,
            install_global=False,
            attach_logging_handler=False,
        )
    ) as runtime:
        app = GotelApp("beanaries", tracer_provider=runtime.tracer_provider)
        logger = logging.getLogger("gotel.host.test")
        handler = LoggingHandler(logger_provider=runtime.logger_provider)
        logger.addHandler(handler)
        try:
            with app.operation("Run"):
                logger.warning("hello")
        finally:
            logger.removeHandler(handler)
        assert runtime.force_flush()
        assert traces.get_finished_spans()[0].resource.attributes["service.name"] == "beanaries-api"
        assert logs.get_finished_logs()[0].log_record.trace_id == traces.get_finished_spans()[0].context.trace_id
    runtime.shutdown()


def test_disabled_and_environment(monkeypatch):
    monkeypatch.setenv("OTEL_SDK_DISABLED", "true")
    with start() as runtime:
        assert runtime.tracer_provider is None
    monkeypatch.delenv("OTEL_SDK_DISABLED")
    monkeypatch.setenv("OTEL_SERVICE_NAME", "from-environment")
    with start(Config(signals=("traces",), trace_exporter=InMemorySpanExporter(), install_global=False)) as runtime:
        assert runtime.tracer_provider.resource.attributes["service.name"] == "from-environment"


def test_invalid_protocol():
    with pytest.raises(ValueError):
        start(Config(protocol="invalid"))


def test_global_lifecycle_is_once_per_process():
    subprocess.run(
        [
            sys.executable,
            "-c",
            """
from gotel.otelhost import Config, start
from gotel import GotelApp
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
exporter = InMemorySpanExporter()
runtime = start(Config(signals=('traces',), trace_exporter=exporter))
with GotelApp('global').operation('Run'):
    pass
assert runtime.force_flush()
assert exporter.get_finished_spans()[0].name == 'global.Run'
runtime.shutdown()
try:
    start(Config(signals=('traces',)))
except RuntimeError:
    pass
else:
    raise AssertionError('global providers were replaced')
""",
        ],
        check=True,
    )


def test_public_app_name_and_compatibility():
    from gotel import Application, GotelApp
    from gotel.application import Application as LegacyApplication

    assert GotelApp.__name__ == "GotelApp"
    assert Application is LegacyApplication is GotelApp
    assert GotelApp("beanaries").qualify("scraper.Fetch") == "beanaries.scraper.Fetch"
    assert GotelApp("beanaries").qualify("beanaries.scraper.Fetch") == "beanaries.scraper.Fetch"
