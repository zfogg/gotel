"""Check ClickHouse metadata on actual offline OTLP protobuf captures."""

import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "testing/python"))
from gotel_testing import protobuf, records


def value(data):
    fields = protobuf(data)
    if 1 in fields:
        return fields[1][0].decode()
    if 3 in fields:
        return fields[3][0]
    if 5 in fields:
        return [value(item) for item in protobuf(fields[5][0]).get(1, [])]
    raise AssertionError(fields)


payload = subprocess.check_output([sys.argv[1]], timeout=30)
assert b"PRIVATE" not in payload
spans = records([("/v1/traces", payload)], "/v1/traces")
assert len(spans) == 7, len(spans)
queries = spans[:6]
parent = spans[6]
for span in queries:
    assert span[4] == parent[2]
    attrs = {}
    for item in span.get(9, []):
        kv = protobuf(item)
        attrs[kv[1][0].decode()] = value(kv[2][0])
    assert attrs["db.system.name"] == "clickhouse"
    assert isinstance(attrs["db.query.diagnostics"], list)
    code = attrs["db.response.status_code"]
    if code == 62:
        assert attrs["operation.outcome"] == "error"
        assert "db.query.text" not in attrs
        assert (
            json.loads(attrs["db.query.diagnostics"][0])["code"]
            == "unterminated_string"
        )
    elif code == 42:
        assert attrs["operation.outcome"] == "cancelled"
    elif code == 99:
        assert attrs["operation.outcome"] == "deferred"
    else:
        assert attrs["operation.outcome"] == "success"
        reason = attrs.get("gotel.db.query.text_reason")
        if reason in ("policy", "analysis_limit"):
            assert "db.query.text" not in attrs
        else:
            assert attrs["gotel.db.query.fingerprint"].startswith(
                "sequel:v1:clickhouse:"
            )
            assert attrs["db.query.diagnostics"] == []
print(
    "ClickHouse C callbacks, scopes, diagnostics, privacy, budgets and completion passed"
)
