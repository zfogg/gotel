"""Exercise native C export against grpcio, plus deliberately malformed HTTP/2 peers."""
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from pathlib import Path
import collections
import json
import os
import shutil
import socketserver
import subprocess
import sys
import tempfile
import threading
import time
import grpc
import h2.config
import h2.connection
import h2.events

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'testing' / 'python'))
from gotel_testing import protobuf

PROBE = sys.argv[1]
SERVICES = {
    'traces': 'opentelemetry.proto.collector.trace.v1.TraceService',
    'metrics': 'opentelemetry.proto.collector.metrics.v1.MetricsService',
    'logs': 'opentelemetry.proto.collector.logs.v1.LogsService',
}

def run(endpoint, ca='', **overrides):
    env = {k: v for k, v in os.environ.items() if not k.startswith('OTEL_')}
    env.update(overrides)
    result = subprocess.run([PROBE, endpoint, str(ca)], env=env, check=True,
                            capture_output=True, text=True, timeout=12)
    return json.loads(result.stdout)

@contextmanager
def collector(action=None, credentials=None):
    attempts = collections.defaultdict(list)
    lock = threading.Lock()
    pool = ThreadPoolExecutor(max_workers=8)
    server = grpc.server(pool)
    for signal, service in SERVICES.items():
        def export(request, context, signal=signal):
            with lock:
                attempts[signal].append((request, dict(context.invocation_metadata()), time.monotonic()))
                number = len(attempts[signal])
            assert protobuf(request), 'empty or malformed OTLP envelope'
            return action(signal, number, context) if action else b''
        handler = grpc.unary_unary_rpc_method_handler(export, request_deserializer=lambda x: x,
                                                       response_serializer=lambda x: x)
        server.add_generic_rpc_handlers((grpc.method_handlers_generic_handler(service, {'Export': handler}),))
    port = server.add_secure_port('127.0.0.1:0', credentials) if credentials else server.add_insecure_port('127.0.0.1:0')
    server.start()
    try:
        yield f'{"https" if credentials else "http"}://127.0.0.1:{port}', attempts
    finally:
        server.stop(0).wait()
        pool.shutdown(wait=True)

def successful(result):
    assert result['create'] == 0 and (result['traces'], result['logs'], result['metrics']) == (1, 1, 4), result

with collector() as (endpoint, attempts):
    successful(run(endpoint))
    assert set(attempts) == set(SERVICES)
    assert len(attempts['logs'][0][0]) > 70000, 'flow-controlled request was truncated'
    assert all(records[0][1]['authorization'] == 'Bearer test-token' for records in attempts.values())
    successful(run(endpoint, OTEL_EXPORTER_OTLP_PROTOCOL='grpc', OTEL_EXPORTER_OTLP_ENDPOINT=endpoint,
                   OTEL_EXPORTER_OTLP_TRACES_HEADERS='authorization=trace-token'))
    assert attempts['traces'][-1][1]['authorization'] == 'trace-token'
    assert attempts['logs'][-1][1]['authorization'] == 'Bearer test-token'

def unavailable(signal, number, context):
    if number == 1:
        context.abort(grpc.StatusCode.UNAVAILABLE, 'retry this batch')
    return b''
with collector(unavailable) as (endpoint, attempts):
    successful(run(endpoint))
    assert all(len(records) == 2 for records in attempts.values()), attempts
    assert all(records[0][0] == records[1][0] for records in attempts.values())

for code in (grpc.StatusCode.INVALID_ARGUMENT, grpc.StatusCode.UNAUTHENTICATED,
             grpc.StatusCode.RESOURCE_EXHAUSTED, grpc.StatusCode.INTERNAL):
    def reject(signal, number, context):
        context.abort(code, 'permanent error')
    with collector(reject) as (endpoint, attempts):
        result = run(endpoint)
        assert result['traces'] == result['logs'] == result['metrics'] == 0, result
        assert all(len(records) == 1 for records in attempts.values()), (code, {k: len(v) for k, v in attempts.items()})

def varint(n):
    result = bytearray()
    while n > 127:
        result.append((n & 127) | 128)
        n >>= 7
    result.append(n)
    return bytes(result)

def field(n, value):
    return varint(n * 8 + 2) + varint(len(value)) + value

for code in (grpc.StatusCode.UNAVAILABLE, grpc.StatusCode.RESOURCE_EXHAUSTED):
    duration = b'\x10' + varint(80000000)  # 80 ms, larger than normal jitter.
    info = field(1, duration)
    any_info = field(1, b'type.googleapis.com/google.rpc.RetryInfo') + field(2, info)
    details = b'\x08' + varint(code.value[0]) + field(3, any_info)
    def throttled(signal, number, context):
        if number == 1:
            context.set_trailing_metadata((('grpc-status-details-bin', details),))
            context.abort(code, 'backpressure')
        return b''
    with collector(throttled) as (endpoint, attempts):
        successful(run(endpoint))
        assert all(len(v) == 2 and v[1][2] - v[0][2] >= .07 for v in attempts.values())

def partial(signal, number, context):
    # ExportTraceServiceResponse.partial_success.rejected_spans = 1.
    return b'\x0a\x02\x08\x01' if signal == 'traces' else b''
with collector(partial) as (endpoint, attempts):
    result = run(endpoint)
    assert result['rejected'] == 1, result
    assert all(len(records) == 1 for records in attempts.values())

with collector(lambda signal, number, context: b'x' * (4 * 1024 * 1024 + 16)) as (endpoint, attempts):
    result = run(endpoint, OTEL_EXPORTER_OTLP_TIMEOUT='2000')
    assert result['traces'] == result['logs'] == result['metrics'] == 0, result
    assert all(len(records) == 1 for records in attempts.values()), {k: len(v) for k, v in attempts.items()}

# Real HTTP/2 peers can send responses grpcio correctly refuses to generate.
@contextmanager
def raw_collector(mode, tls_context=None):
    class Handler(socketserver.BaseRequestHandler):
        def handle(self):
            peer = self.request
            peer.settimeout(2)
            try:
                if tls_context:
                    peer = tls_context.wrap_socket(peer, server_side=True)
                connection = h2.connection.H2Connection(h2.config.H2Configuration(client_side=False))
                connection.initiate_connection()
                peer.sendall(connection.data_to_send())
                while True:
                    chunk = peer.recv(65536)
                    if not chunk:
                        return
                    for event in connection.receive_data(chunk):
                        if isinstance(event, h2.events.DataReceived):
                            connection.acknowledge_received_data(event.flow_controlled_length, event.stream_id)
                        if isinstance(event, h2.events.StreamEnded):
                            stream = event.stream_id
                            if mode == 'reset':
                                connection.reset_stream(stream, error_code=2)
                            elif mode == 'goaway':
                                connection.close_connection(error_code=0, last_stream_id=0)
                            elif mode == 'stall':
                                time.sleep(1)
                                return
                            elif mode in ('http404', 'http503'):
                                connection.send_headers(stream, [(':status', mode[4:])], end_stream=True)
                            else:
                                connection.send_headers(stream, [(':status', '200'), ('content-type',
                                    'application/json' if mode == 'content-type' else 'application/grpc')])
                                body = {'length': b'\0\0\0\0\x01', 'compression': b'\x01\0\0\0\0',
                                        'multiple': b'\0' * 10}.get(mode, b'\0' * 5)
                                connection.send_data(stream, body, end_stream=mode == 'missing-status')
                                if mode != 'missing-status':
                                    connection.send_headers(stream, [('grpc-status', '99' if mode == 'bad-status' else '0')],
                                                            end_stream=True)
                    outgoing = connection.data_to_send()
                    if outgoing:
                        peer.sendall(outgoing)
            except (OSError, h2.exceptions.H2Error):
                pass
            finally:
                if peer is not self.request:
                    peer.close()
    class Server(socketserver.ThreadingTCPServer):
        daemon_threads = True
    with Server(('127.0.0.1', 0), Handler) as server:
        thread = threading.Thread(target=server.serve_forever, kwargs={'poll_interval': .01}, daemon=True)
        thread.start()
        try:
            yield f'{"https" if tls_context else "http"}://127.0.0.1:{server.server_address[1]}'
        finally:
            server.shutdown()
            thread.join()

for mode in ('missing-status', 'bad-status', 'length', 'compression', 'multiple', 'content-type',
             'reset', 'goaway', 'http404', 'http503', 'stall'):
    with raw_collector(mode) as endpoint:
        result = run(endpoint)
        assert result['traces'] == result['logs'] == result['metrics'] == 0, (mode, result)

with tempfile.TemporaryDirectory(prefix='gotel-grpc-') as directory:
    root = Path(directory)
    openssl = shutil.which('openssl') or r'C:\Program Files\Git\usr\bin\openssl.exe'
    def command(*args):
        subprocess.run([openssl, *args], cwd=root, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    command('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', 'server.key', '-out', 'server.pem',
            '-days', '2', '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost,IP:127.0.0.1')
    credentials = grpc.ssl_server_credentials([((root/'server.key').read_bytes(), (root/'server.pem').read_bytes())])
    with collector(credentials=credentials) as (endpoint, attempts):
        successful(run(endpoint, root/'server.pem'))
        result = run(endpoint)
        assert result['traces'] == result['logs'] == result['metrics'] == 0 and result['network'] > 0, result
        successful(run(endpoint, SSL_CERT_FILE=str(root/'server.pem')))
        assert all(len(records) == 2 for records in attempts.values())
    import ssl
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(root/'server.pem', root/'server.key')
    # A trusted TLS server without h2 ALPN must fail before exporting telemetry.
    with raw_collector('normal', context) as endpoint:
        result = run(endpoint, root/'server.pem')
        assert result['traces'] == result['logs'] == result['metrics'] == 0, result
    command('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', 'wrong.key', '-out', 'wrong.pem',
            '-days', '2', '-subj', '/CN=wrong.invalid', '-addext', 'subjectAltName=DNS:wrong.invalid')
    credentials = grpc.ssl_server_credentials([((root/'wrong.key').read_bytes(), (root/'wrong.pem').read_bytes())])
    with collector(credentials=credentials) as (endpoint, attempts):
        result = run(endpoint, root/'wrong.pem')
        assert result['traces'] == result['logs'] == result['metrics'] == 0, result
        assert not attempts

print('native gRPC: signals, flow control, metadata, environment, retries/RetryInfo, partial success, malformed peers, timeouts, TLS/ALPN passed')
