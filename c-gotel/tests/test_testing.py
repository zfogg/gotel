"""Contract tests for the framework-independent Python helpers."""
from pathlib import Path
import sys
import struct
import threading
import unittest
import urllib.request
import urllib.error
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'testing' / 'python'))
from gotel_testing import Collector, Response, protobuf, records, service_names


def post(url, body=b'payload'):
    try:
        with urllib.request.urlopen(urllib.request.Request(url, body), timeout=2) as reply:
            return reply.status, reply.read(), reply.headers
    except urllib.error.HTTPError as reply:
        with reply:
            return reply.code, reply.read(), reply.headers


class WireDecoder(unittest.TestCase):
    def test_repeated_varints(self):
        self.assertEqual(protobuf(b'\x08\x01\x08\x02'), {1: [1, 2]})
    def test_fixed_and_unknown_fields(self):
        self.assertEqual(protobuf(b'\x09' + struct.pack('<Q', 42) + b'\x15abcd'),
                         {1: [struct.pack('<Q', 42)], 2: [b'abcd']})
    def test_uint64_max(self):
        self.assertEqual(protobuf(b'\x08' + b'\xff' * 9 + b'\x01'), {1: [2**64 - 1]})
    def test_empty(self):
        self.assertEqual(protobuf(b''), {})
        self.assertEqual(records([], '/v1/traces'), [])
        self.assertEqual(service_names([]), set())
    def test_malformed(self):
        for data in [b'\x00', b'\x0b', b'\x08', b'\x0a\x03ab', b'\x09abc',
                     b'\x15abc', b'\x08' + b'\x80' * 10, b'\x08' + b'\xff' * 10]:
            with self.subTest(data=data), self.assertRaises(ValueError):
                protobuf(data)


class Receiver(unittest.TestCase):
    def test_response_script_and_capture(self):
        with Collector() as collector:
            collector.script('/v1/traces', [503, Response(429, b'retry', (('Retry-After', '0'),))])
            self.assertEqual(post(collector.endpoint())[0], 503)
            code, body, headers = post(collector.endpoint())
            self.assertEqual((code, body, headers['Retry-After']), (429, b'retry', '0'))
            self.assertEqual(post(collector.endpoint())[0], 200)
            self.assertEqual(collector.records, [('/v1/traces', b'payload')])
            self.assertEqual(len(collector.attempts), 3)
            collector.clear()
            self.assertEqual(collector.attempts, [])
            self.assertEqual(post(collector.endpoint())[0], 200)
    def test_isolation(self):
        with Collector() as first, Collector() as second:
            first.script('/v1/logs', [403])
            self.assertNotEqual(first.server_port, second.server_port)
            self.assertEqual(post(first.endpoint('logs'))[0], 403)
            self.assertEqual(post(second.endpoint('logs'))[0], 200)
            self.assertEqual(first.records, [])
    def test_bounded_wait(self):
        with Collector() as collector:
            self.assertFalse(collector.wait_for(lambda items: bool(items), timeout=.01))
            thread = threading.Thread(target=post, args=(collector.endpoint(),))
            thread.start()
            self.assertTrue(collector.wait_for(lambda items: len(items) == 1))
            thread.join(timeout=3)
            self.assertFalse(thread.is_alive())
    def test_invalid_options(self):
        with Collector() as collector:
            with self.assertRaises(ValueError): collector.endpoint('invalid')
            with self.assertRaises(ValueError): collector.script('/v1/traces', [999])
    def test_body_limit(self):
        with Collector(max_body=1) as collector:
            self.assertEqual(post(collector.endpoint(), b'too large')[0], 400)
            self.assertEqual(collector.records, [])


if __name__ == '__main__':
    unittest.main()
