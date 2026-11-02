"""Validate structured SQL diagnostics on actual OTLP wire envelopes."""

import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "testing/python"))
from gotel_testing import Collector, protobuf, records


def any_value(data):
    value = protobuf(data)
    if 1 in value:
        return value[1][0].decode()
    if 2 in value:
        return bool(value[2][0])
    if 3 in value:
        return value[3][0]
    if 5 in value:
        return [any_value(item) for item in protobuf(value[5][0]).get(1, [])]
    raise AssertionError(value)


with Collector() as collector:
    subprocess.run(
        [sys.argv[1], f"http://127.0.0.1:{collector.server_port}/v1/traces"],
        check=True,
        timeout=30,
    )
    payload = b"".join(body for _, body in collector.records)
    assert b"LITERAL_SECRET" not in payload
    spans = records(collector.records, "/v1/traces")
    assert len(spans) == 13, len(spans)
    expected = {
        1: "unterminated_string",
        2: "unclosed_group",
        3: "unmatched_close",
        4: "unterminated_comment",
    }
    for span in spans:
        attributes = {}
        for item in span.get(9, []):
            kv = protobuf(item)
            attributes[kv[1][0].decode()] = any_value(kv[2][0])
        case = attributes["test.case"]
        diagnostics = attributes["db.query.diagnostics"]
        assert isinstance(diagnostics, list)
        if case in (0, 5, 6, 7):
            assert diagnostics == [], diagnostics
        else:
            assert len(diagnostics) == 1
            diagnostic = json.loads(diagnostics[0])
            assert diagnostic["code"] == expected[case], diagnostic
            assert diagnostic["severity"] == ("error" if case == 3 else "warning")
            assert diagnostic["incomplete"] is (case != 3)
            assert diagnostic["statement_complete"] is False
            for name in (
                "offset",
                "end_offset",
                "line",
                "column",
                "end_line",
                "end_column",
            ):
                assert isinstance(diagnostic[name], int) and diagnostic[name] >= 0
            for name in ("message", "explanation", "hint"):
                assert isinstance(diagnostic[name], str) and diagnostic[name]
        if attributes.get("test.omit"):
            assert "db.query.text" not in attributes
        if case == 5:
            assert len(attributes["db.query.text"]) > 32768
            assert "gotel.db.query.truncated" not in attributes
print("C SQL diagnostic arrays, malformed and incomplete input, omission, ownership and large defaults passed")
