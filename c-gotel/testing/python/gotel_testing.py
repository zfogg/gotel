"""Framework-independent, standard-library-only OTLP/HTTP test helpers.

Collector binds loopback, uses an ephemeral port, never resolves DNS, and must
be closed (prefer a context manager). Snapshots retain successful requests;
attempts retain failures too. Scripts and capture are synchronized across threads.
"""
from collections import deque
from dataclasses import dataclass
import http.server
import socketserver
import threading
import time


def protobuf(data):
    """Decode protobuf wire fields, retaining repeats and opaque byte values.

    Raises ValueError on malformed/truncated input, invalid tags, oversized
    varints, or unsupported groups. Unknown fields of supported types are kept.
    """
    fields, offset = {}, 0
    def varint():
        nonlocal offset
        value = 0
        for shift in range(0, 70, 7):
            if offset >= len(data):
                raise ValueError('truncated varint')
            byte = data[offset]
            offset += 1
            if shift == 63 and byte > 1:
                raise ValueError('varint exceeds uint64')
            value |= (byte & 127) << shift
            if byte < 128:
                return value
        raise ValueError('unterminated varint')
    while offset < len(data):
        tag = varint()
        key, wire = tag >> 3, tag & 7
        if not 0 < key < 2**29:
            raise ValueError('invalid field number')
        if wire == 0:
            value = varint()
        elif wire in (1, 2, 5):
            size = varint() if wire == 2 else {1: 8, 5: 4}[wire]
            if size > len(data) - offset:
                raise ValueError('truncated field')
            value = bytes(data[offset:offset + size])
            offset += size
        else:
            raise ValueError('unsupported wire type')
        fields.setdefault(key, []).append(value)
    return fields


def records(requests, path):
    """Flatten Export{Trace,Logs,Metrics}ServiceRequest scope records."""
    output = []
    for request_path, payload in requests:
        if request_path != path:
            continue
        for resource in protobuf(payload).get(1, []):
            for scope in protobuf(resource).get(2, []):
                output.extend(protobuf(record) for record in protobuf(scope).get(2, []))
    return output


def service_names(requests):
    names = set()
    for _, payload in requests:
        for resource in protobuf(payload).get(1, []):
            for attribute in protobuf(protobuf(resource)[1][0]).get(1, []):
                entry = protobuf(attribute)
                if entry[1] == [b'service.name']:
                    names.add(protobuf(entry[2][0])[1][0].decode('utf-8'))
    return names


@dataclass(frozen=True)
class Response:
    status: int = 200
    body: bytes = b''
    headers: tuple = ()


class _Server(http.server.ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False
    def server_bind(self):
        socketserver.TCPServer.server_bind(self)
        self.server_name = 'localhost'
        self.server_port = self.server_address[1]


class _Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.connection.settimeout(2)
        try:
            length = int(self.headers['Content-Length'])
            if not 0 <= length <= self.server.collector.max_body:
                raise ValueError('invalid body size')
            body = self.rfile.read(length)
            if len(body) != length:
                raise ValueError('truncated body')
        except (ValueError, TypeError, OSError):
            self.send_error(400)
            return
        owner = self.server.collector
        with owner._condition:
            script = owner._scripts.get(self.path)
            reply = script.popleft() if script else Response()
            owner._attempts.append((self.path, body, reply.status))
            if 200 <= reply.status < 300:
                owner._records.append((self.path, body))
            owner._condition.notify_all()
        self.send_response(reply.status)
        self.send_header('Content-Type', 'application/x-protobuf')
        self.send_header('Content-Length', str(len(reply.body)))
        for key, value in reply.headers:
            self.send_header(key, value)
        self.end_headers()
        if reply.body:
            self.wfile.write(reply.body)
    def log_message(self, *args):
        pass


class Collector:
    def __init__(self, max_body=16 * 1024 * 1024):
        self.max_body = max_body
        self._condition = threading.Condition()
        self._records, self._attempts, self._scripts = [], [], {}
        self._server = _Server(('127.0.0.1', 0), _Handler)
        self._server.collector = self
        self._thread = threading.Thread(target=self._server.serve_forever,
                                        kwargs={'poll_interval': .01}, daemon=True)
        self._thread.start()
    @property
    def server_port(self):
        return self._server.server_port
    @property
    def url(self):
        return f'http://127.0.0.1:{self.server_port}'
    def endpoint(self, signal='traces'):
        if signal not in ('traces', 'logs', 'metrics'):
            raise ValueError('unknown signal')
        return self.url + '/v1/' + signal
    @property
    def records(self):
        with self._condition:
            return list(self._records)
    @property
    def attempts(self):
        with self._condition:
            return list(self._attempts)
    def clear(self):
        """Clear capture and response scripts after producers have stopped."""
        with self._condition:
            self._records.clear()
            self._attempts.clear()
            self._scripts.clear()
    def script(self, path, responses):
        """Set per-path responses; exhaustion returns empty protobuf HTTP 200."""
        responses = [Response(r) if isinstance(r, int) else r for r in responses]
        if any(not 100 <= r.status <= 599 for r in responses):
            raise ValueError('invalid HTTP status')
        with self._condition:
            self._scripts[path] = deque(responses)
    def wait_for(self, predicate, timeout=5):
        """Wait on successful-request snapshots; return False on deadline."""
        deadline = time.monotonic() + timeout
        with self._condition:
            while not predicate(list(self._records)):
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return False
                self._condition.wait(remaining)
        return True
    def close(self):
        if self._thread.is_alive():
            self._server.shutdown()
            self._thread.join(timeout=3)
        self._server.server_close()
    def __enter__(self):
        return self
    def __exit__(self, *args):
        self.close()
