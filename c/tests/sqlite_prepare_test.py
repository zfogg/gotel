"""Wire assertions for native SQLite preparation without duplicate successes."""

import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "testing/python"))
from gotel_testing import Collector, protobuf, records


def attrs(span):
    result = {}
    for entry in span.get(9, []):
        entry = protobuf(entry)
        value = protobuf(entry[2][0])
        if 1 in value:
            result[entry[1][0]] = value[1][0]
    return result


with Collector() as collector:
    for mode in ("normal", "omit"):
        collector.clear()
        subprocess.run(
            [sys.argv[1], f"http://127.0.0.1:{collector.server_port}/v1/traces", mode],
            check=True,
            timeout=10,
        )
        payload = b"".join(body for _, body in collector.records)
        for secret in (b"LITERAL_SECRET", b"FAIL_SECRET", b"DETACHED_SECRET", b"UNATTACHED_SECRET"):
            assert secret not in payload
        spans = records(collector.records, "/v1/traces")
        assert len(spans) == 4, [(span.get(5), attrs(span)) for span in spans]
        root = next(span for span in spans if span[5] == [b"fixture.operation"])
        good = [span for span in spans if span[5] == [b"fixture.storage.Good"]]
        assert len(good) == 1, "prepare success duplicated PROFILE"
        failures = [span for span in spans if attrs(span).get(b"operation.outcome") == b"error"]
        assert len(failures) == 2
        for span in failures:
            assert span[1] == root[1] and span[4] == root[2]
            attributes = attrs(span)
            assert attributes[b"error.type"] == b"sqlite.prepare"
            assert attributes[b"db.operation.name"] == b"PREPARE"
            assert attributes[b"db.namespace"] == b"fixture"
            assert b"db.query.text" not in attributes if mode == "omit" else True
            assert protobuf(span[15][0])[3] == [2]
print("SQLite prepare errors, byte bounds, tails, privacy, detach/close and PROFILE-only success passed")
