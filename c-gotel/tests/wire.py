"""Real HTTP/protobuf export, signal selection, names and bind-value exclusion."""
import http.server
import subprocess
import sys
import threading

class Receiver(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        payload = self.rfile.read(int(self.headers['Content-Length']))
        self.server.records.append((self.path, payload))
        self.send_response(200)
        self.send_header('Content-Type', 'application/x-protobuf')
        self.send_header('Content-Length', '0')
        self.end_headers()
    def log_message(self, *args):
        pass

server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Receiver)
server.records = []
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
try:
    endpoint = f'http://127.0.0.1:{server.server_port}/v1/traces'
    for signals, paths in [(7, {'/v1/traces', '/v1/logs', '/v1/metrics'}),
                           (1, {'/v1/traces'}), (2, {'/v1/logs'}),
                           (4, {'/v1/metrics'}), (0, set())]:
        server.records.clear()
        subprocess.run([sys.argv[1], endpoint, str(signals)], check=True, timeout=15)
        assert {p for p, _ in server.records} == paths, server.records
        payload = b''.join(body for _, body in server.records)
        assert b'BOUND_SECRET' not in payload
        if signals & 1:
            assert b'test.cli' in payload
            assert b'test.storage.SelectSecret' in payload
            assert b'test.storage.UnknownQuery' in payload
        if signals & 2:
            assert b'wire message' in payload
        if signals & 4:
            assert b'gotel.operation.count' in payload
    print('c-gotel OTLP wire tests passed')
finally:
    server.shutdown()
    server.server_close()
