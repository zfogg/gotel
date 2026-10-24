"""Exercise gotel environment configuration through an ascii-chat binary.

Run: python c/tests/ascii_chat_env.py /path/to/ascii-chat
Uses synthetic video, loopback HTTP, and no camera or audio devices.
"""
import http.server
import os
from pathlib import Path
import subprocess
import sys
import threading
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'testing/python'))
from gotel_testing import protobuf, service_names


class Receiver(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers['Content-Length']))
        self.server.records.append((self.path, body, dict(self.headers)))
        time.sleep(self.server.delay)
        try:
            self.send_response(200)
            self.send_header('Content-Length', '0')
            self.end_headers()
        except OSError:
            pass

    def log_message(self, *args):
        pass


binary = str(Path(sys.argv[1]).resolve())
server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Receiver)
server.daemon_threads = True
server.records, server.delay = [], 0
threading.Thread(target=server.serve_forever, daemon=True).start()
url = f'http://127.0.0.1:{server.server_port}'
base_env = {k: v for k, v in os.environ.items() if not k.startswith(('OTEL_', 'ASCII_CHAT_OTEL_'))}
media = ['mirror', '--test-pattern', '--audio=false', '--snapshot', '--snapshot-delay', '0',
         '--width', '40', '--height', '12', '--splash-screen=false']


def run(label, environment=None, flags=(), paths=None, success=True):
    server.records = []
    result = subprocess.run([binary, '--no-check-update', *flags, *media],
                            env=dict(base_env, **(environment or {})), capture_output=True, timeout=20)
    assert (result.returncode == 0) == success, (label, result.stderr[-3000:])
    if paths is not None:
        assert {p for p, _, _ in server.records} == paths, (label, server.records)
    print(label, 'PASS', flush=True)
    return result


all_paths = {'/v1/traces', '/v1/logs', '/v1/metrics'}
try:
    run('standard base endpoint', {'OTEL_EXPORTER_OTLP_ENDPOINT': url}, paths=all_paths)
    run('base endpoint path prefix', {'OTEL_EXPORTER_OTLP_ENDPOINT': url + '/collector/'},
        paths={'/collector' + p for p in all_paths})
    run('ascii endpoint environment overrides standard',
        {'OTEL_EXPORTER_OTLP_ENDPOINT': 'http://127.0.0.1:1', 'ASCII_CHAT_OTEL_ENDPOINT': url}, paths=all_paths)
    run('CLI endpoint overrides environment',
        {'OTEL_EXPORTER_OTLP_ENDPOINT': 'http://127.0.0.1:1', 'ASCII_CHAT_OTEL_ENDPOINT': 'http://127.0.0.1:2'},
        flags=['--otel-endpoint', url], paths=all_paths)
    run('ascii signals environment overrides default',
        {'OTEL_EXPORTER_OTLP_ENDPOINT': url, 'ASCII_CHAT_OTEL_ENABLED_TELEMETRY': 'traces'}, paths={'/v1/traces'})
    run('SDK disabled', {'OTEL_EXPORTER_OTLP_ENDPOINT': url, 'OTEL_SDK_DISABLED': 'true'}, paths=set())
    run('empty variables are unset', {'OTEL_EXPORTER_OTLP_ENDPOINT': url, 'OTEL_SDK_DISABLED': '',
                                    'OTEL_SERVICE_NAME': '', 'OTEL_EXPORTER_OTLP_TIMEOUT': ''}, paths=all_paths)
    run('identity, resource attributes, headers and protocol', {
        'OTEL_EXPORTER_OTLP_ENDPOINT': url, 'OTEL_SERVICE_NAME': 'ascii-env-test',
        'OTEL_RESOURCE_ATTRIBUTES': 'deployment.environment.name=test,service.name=lower-priority',
        'OTEL_EXPORTER_OTLP_HEADERS': 'x-env-test=auth-token', 'OTEL_EXPORTER_OTLP_PROTOCOL': 'http/protobuf'},
        paths=all_paths)
    assert service_names([(p, b) for p, b, _ in server.records]) == {'ascii-env-test'}
    for _, body, headers in server.records:
        assert headers.get('x-env-test') == 'auth-token', headers
        for envelope in protobuf(body).get(1, []):
            attrs = {}
            for item in protobuf(protobuf(envelope)[1][0]).get(1, []):
                item = protobuf(item)
                attrs[item[1][0]] = protobuf(item[2][0])
            assert attrs[b'deployment.environment.name'][1] == [b'test']
            assert attrs[b'ascii_chat.mode'][1] == [b'mirror']
            assert b'service.version' in attrs
    run('resource service.name fallback', {'OTEL_EXPORTER_OTLP_ENDPOINT': url,
        'OTEL_RESOURCE_ATTRIBUTES': 'service.name=resource-service'}, paths=all_paths)
    assert service_names([(p, b) for p, b, _ in server.records]) == {'resource-service'}
    env = {'OTEL_EXPORTER_OTLP_ENDPOINT': 'http://127.0.0.1:1',
           'OTEL_EXPORTER_OTLP_HEADERS': 'x-env-test=base'}
    for signal in ('TRACES', 'LOGS', 'METRICS'):
        env[f'OTEL_EXPORTER_OTLP_{signal}_ENDPOINT'] = url + '/custom/' + signal.lower()
        env[f'OTEL_EXPORTER_OTLP_{signal}_HEADERS'] = 'x-env-test=' + signal.lower()
    run('signal-specific endpoints and headers', env,
        paths={'/custom/traces', '/custom/logs', '/custom/metrics'})
    for path, _, headers in server.records:
        assert headers.get('x-env-test') == path.rsplit('/', 1)[-1], (path, headers)
    run('unsupported protocol rejected', {'OTEL_EXPORTER_OTLP_ENDPOINT': url,
        'OTEL_EXPORTER_OTLP_PROTOCOL': 'grpc'}, success=False, paths=set())
    run('invalid timeout rejected', {'OTEL_EXPORTER_OTLP_ENDPOINT': url,
        'OTEL_EXPORTER_OTLP_TIMEOUT': '-1'}, success=False, paths=set())
    run('invalid certificate rejected', {'OTEL_EXPORTER_OTLP_ENDPOINT': url,
        'OTEL_EXPORTER_OTLP_CERTIFICATE': str(Path('missing-test-ca.pem').resolve())}, success=False, paths=set())
    server.delay = 2
    started = time.monotonic()
    run('standard timeout bounds stalled collector', {'OTEL_EXPORTER_OTLP_ENDPOINT': url,
        'OTEL_EXPORTER_OTLP_TIMEOUT': '200'})
    assert server.records, 'stalled receiver was never contacted'
    assert time.monotonic() - started < 1.8, 'standard timeout was overwritten'
finally:
    server.shutdown()
    server.server_close()
