"""Synthetic no-network SQLite unified-analysis policy assertions."""

import json
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "testing/python"))
from gotel_testing import protobuf, records


def attributes(span):
    result = {}
    for value in span.get(9, []):
        pair = protobuf(value)
        data = protobuf(pair[2][0])
        if 1 in data:
            result[pair[1][0].decode()] = data[1][0].decode()
        elif 3 in data:
            result[pair[1][0].decode()] = data[3][0]
    return result


with tempfile.TemporaryDirectory() as directory:
    output = Path(directory) / "sqlite.pb"
    for mode in ("default", "analysis", "policy", "truncate", "memory", "work", "output", "cancel"):
        subprocess.run([sys.argv[1], mode, str(output)], check=True, timeout=10)
        payload = output.read_bytes()
        assert not any(secret in payload for secret in (b"PRIVATE_LITERAL", b"PRIVATE_BIND", b"PREPARE_SECRET"))
        spans = records([("/v1/traces", payload)], "/v1/traces")
        assert len(spans) == 3
        query = next(span for span in spans if span[5] == [b"storage.sqlite.Lookup"])
        failure = next(span for span in spans if span[5] == [b"storage.sqlite.PrepareFailure"])
        attrs = attributes(query)
        assert attrs["gotel.db.query.bind_count"] == 1
        assert attributes(failure)["operation.outcome"] == "error"
        if mode in {"memory", "work", "output", "cancel"}:
            reason = "cancelled" if mode == "cancel" else "analysis_limit"
            for span in (query, failure):
                values = attributes(span)
                assert values["gotel.db.query.text_reason"] == reason, (mode, values)
                assert "db.query.text" not in values and "gotel.db.query.fingerprint" not in values
                assert "gotel.db.query.source_map" not in values and "gotel.db.query.binds" not in values
        else:
            assert "sqlite" in attrs["gotel.db.query.fingerprint"]
            assert attrs["gotel.db.query.analysis_status"] == "ok"
            if mode == "policy":
                assert "db.query.text" not in attrs and attrs["gotel.db.query.text_reason"] == "policy"
                assert "gotel.db.query.source_map" not in attrs
            else:
                assert "db.query.text" in attrs and "gotel.db.query.text_reason" not in attrs
            if mode == "default":
                assert "gotel.db.query.binds" not in attrs and "gotel.db.query.source_map" not in attrs
            else:
                binds = json.loads(attrs["gotel.db.query.binds"])
                assert len(binds) == 1 and binds[0]["slot"] == 1
                assert not any("name" in bind or "value" in bind for bind in binds)
            if mode == "analysis":
                assert json.loads(attrs["gotel.db.query.source_map"])["schema_version"] == 1
            if mode == "truncate":
                assert len(attrs["db.query.text"].encode()) <= 12
print("SQLite unified analysis, defaults, copied policy, safe binds/maps, budgets and cancellation passed")
