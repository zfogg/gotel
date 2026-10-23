"""Real HTTP/protobuf export, signal selection, names and bind-value exclusion."""
from pathlib import Path
import subprocess
import sys
import struct
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'testing' / 'python'))
from gotel_testing import Collector, protobuf as fields, records

server = Collector()
try:
    endpoint = f'http://127.0.0.1:{server.server_port}/v1/traces'
    for signals, paths in [(7, {'/v1/traces', '/v1/logs', '/v1/metrics'}),
                           (1, {'/v1/traces'}), (2, {'/v1/logs'}),
                           (4, {'/v1/metrics'}), (0, set())]:
        server.clear()
        for path in paths:
            server.script(path, [503])
        subprocess.run([sys.argv[1], endpoint, str(signals)], check=True, timeout=15)
        assert {p for p, _ in server.records} == paths, [p for p, _ in server.records]
        payload = b''.join(body for _, body in server.records)
        assert b'BOUND_SECRET' not in payload
        assert b'SELECT ? UNION ALL SELECT ?' not in payload
        if signals & 1:
            assert b'test.cli' in payload
            if len(sys.argv) < 3 or sys.argv[2] == 'ON':
                assert b'test.storage.SelectSecret' in payload
                assert b'test.storage.UnknownQuery' in payload
            spans = records(server.records, '/v1/traces')
            root = next(s for s in spans if s[5] == [b'test.cli'])
            assert fields(root[15][0])[3] == [2]
            assert len(root[1][0]) == 16 and len(root[2][0]) == 8
            assert all(struct.unpack('<Q', s[8][0])[0] >= struct.unpack('<Q', s[7][0])[0] for s in spans)
            if signals & 2:
                log = next(r for r in records(server.records, '/v1/logs') if b'wire message' in r[5][0])
                assert log[9] == root[1] and log[10] == root[2]
                outside = next(r for r in records(server.records, '/v1/logs') if b'wire outside' in r[5][0])
                assert 9 not in outside and 10 not in outside
        if signals & 2:
            assert b'wire message' in payload
            logs = records(server.records, '/v1/logs')
            severity_logs = [r for r in logs if fields(r[5][0]).get(1) == [b'wire severity']]
            assert len(severity_logs) == 24
            labels = [b'TRACE', b'DEBUG', b'INFO', b'WARN', b'ERROR', b'FATAL']
            for severity in range(1, 25):
                log = next(r for r in severity_logs if r[2] == [severity])
                assert log[3] == [labels[(severity - 1) // 4]], (severity, log)
        if signals & 4:
            if len(sys.argv) < 3 or sys.argv[2] == 'ON':
                sql_metrics = [m for m in records(server.records, '/v1/metrics')
                               if m[1] == [b'db.client.operation.duration']]
                assert len(sql_metrics) == 3
                for metric in sql_metrics:
                    histogram = fields(metric[9][0])
                    assert histogram[2] == [1], 'SQLite observations must use delta temporality'
                    point = fields(histogram[1][0])
                    assert struct.unpack('<Q', point[4][0])[0] == 1
                    assert len(point[7][0]) == 15 * 8
            assert b'test.cli.count' in payload
            metrics = records(server.records, '/v1/metrics')
            def point(name, kind):
                metric = next(m for m in metrics if m[1] == [name.encode()])
                return fields(fields(metric[kind][0])[1][0])
            assert struct.unpack('<d', point('test.bytes', 7)[4][0])[0] == (4050 if signals == 4 else 50)
            assert struct.unpack('<d', point('test.depth', 5)[4][0])[0] == 2
            assert struct.unpack('<d', point('test.cli.errors', 7)[4][0])[0] == 1
            histogram = point('test.size', 9)
            assert struct.unpack('<Q', histogram[4][0])[0] == (4002 if signals == 4 else 2)
            assert struct.unpack('<d', histogram[5][0])[0] == (40055 if signals == 4 else 55)
            assert struct.unpack('<4Q', histogram[6][0]) == (0, 4001 if signals == 4 else 1, 1, 0)
            duration = point('test.cli.duration', 9)
            assert struct.unpack('<Q', duration[4][0])[0] == 1
            assert abs(struct.unpack('<d', duration[5][0])[0] - .005) < 1e-9
            metric = next(m for m in metrics if m[1] == [b'test.cli.count'])
            point = fields(fields(metric[7][0])[1][0])
            assert struct.unpack('<d', point[4][0])[0] == 1.0
            if signals == 4:
                metric = next(m for m in metrics if m[1] == [b'test.child.count'])
                point = fields(fields(metric[7][0])[1][0])
                assert struct.unpack('<d', point[4][0])[0] == 4000.0
    print('c-gotel OTLP wire tests passed')
finally:
    server.close()
