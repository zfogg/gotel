"""Real HTTP/protobuf export, signal selection, names and bind-value exclusion."""
import http.server
import subprocess
import sys
import threading
import struct

def fields(data):
    """Decode protobuf framing without a generated-code dependency."""
    result = {}
    offset = 0
    def varint():
        nonlocal offset
        value = shift = 0
        while True:
            assert offset < len(data) and shift < 70
            byte = data[offset]
            offset += 1
            value |= (byte & 127) << shift
            if byte < 128:
                return value
            shift += 7
    while offset < len(data):
        tag = varint()
        key, wire = tag >> 3, tag & 7
        assert key
        if wire == 0:
            value = varint()
        elif wire == 1:
            value = data[offset:offset+8]
            assert len(value) == 8
            offset += 8
        elif wire == 2:
            size = varint()
            value = data[offset:offset+size]
            assert len(value) == size
            offset += size
        elif wire == 5:
            value = data[offset:offset+4]
            assert len(value) == 4
            offset += 4
        else:
            raise AssertionError(wire)
        result.setdefault(key, []).append(value)
    return result

def records(requests, path):
    output = []
    for p, data in requests:
        if p != path:
            continue
        for resource in fields(data).get(1, []):
            for scope in fields(resource).get(2, []):
                output.extend(fields(scope).get(2, []))
    return [fields(record) for record in output]

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
        assert {p for p, _ in server.records} == paths, [p for p, _ in server.records]
        payload = b''.join(body for _, body in server.records)
        assert b'BOUND_SECRET' not in payload
        if signals & 1:
            assert b'test.cli' in payload
            if len(sys.argv) < 3 or sys.argv[2] == 'ON':
                assert b'test.storage.SelectSecret' in payload
                assert b'test.storage.UnknownQuery' in payload
            spans = records(server.records, '/v1/traces')
            root = next(s for s in spans if s[5] == [b'test.cli'])
            assert len(root[1][0]) == 16 and len(root[2][0]) == 8
            assert all(struct.unpack('<Q', s[8][0])[0] >= struct.unpack('<Q', s[7][0])[0] for s in spans)
            if signals & 2:
                log = next(r for r in records(server.records, '/v1/logs') if b'wire message' in r[5][0])
                assert log[9] == root[1] and log[10] == root[2]
        if signals & 2:
            assert b'wire message' in payload
        if signals & 4:
            assert b'test.cli.count' in payload
            metrics = records(server.records, '/v1/metrics')
            metric = next(m for m in metrics if m[1] == [b'test.cli.count'])
            point = fields(fields(metric[7][0])[1][0])
            assert struct.unpack('<d', point[4][0])[0] == 1.0
            if signals == 4:
                metric = next(m for m in metrics if m[1] == [b'test.child.count'])
                point = fields(fields(metric[7][0])[1][0])
                assert struct.unpack('<d', point[4][0])[0] == 4000.0
    print('c-gotel OTLP wire tests passed')
finally:
    server.shutdown()
    server.server_close()
