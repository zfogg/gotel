"""Exercise real protobuf exporters against an isolated loopback receiver."""

import logging
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import ExportLogsServiceRequest
from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2 import ExportMetricsServiceRequest
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import ExportTraceServiceRequest

from gotel import GotelApp
from gotel.otelhost import Config, start


def test_otlp_three_signals():
    requests = {}

    class Receiver(BaseHTTPRequestHandler):
        def do_POST(self):
            body = self.rfile.read(int(self.headers["content-length"]))
            requests.setdefault(self.path, []).append(body)
            self.send_response(200)
            self.send_header("content-length", "0")
            self.end_headers()

        def log_message(self, format: str, *args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Receiver)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        with start(
            Config(
                service_name="beanaries-test",
                endpoint=f"http://127.0.0.1:{server.server_port}",
                install_global=False,
                timeout=2,
            )
        ) as runtime:
            app = GotelApp("beanaries", tracer_provider=runtime.tracer_provider)
            counter = runtime.meter_provider.get_meter("test").create_counter("test.operations")
            with app.operation("Test") as span:
                logging.getLogger("gotel.otlp.test").warning("correlated message")
                counter.add(1)
                trace_id = span.get_span_context().trace_id
            assert runtime.force_flush(5000)
        traces = ExportTraceServiceRequest.FromString(requests["/v1/traces"][0])
        logs = ExportLogsServiceRequest.FromString(requests["/v1/logs"][0])
        metrics = ExportMetricsServiceRequest.FromString(requests["/v1/metrics"][0])
        exported = traces.resource_spans[0].scope_spans[0].spans[0]
        assert exported.name == "beanaries.Test"
        record = logs.resource_logs[0].scope_logs[0].log_records[0]
        assert int.from_bytes(record.trace_id, "big") == trace_id
        assert metrics.resource_metrics[0].scope_metrics[0].metrics[0].name == "test.operations"
    finally:
        server.shutdown()
        server.server_close()
        thread.join()
