"""ClickHouse API contracts, including privacy, layout and bounded output.

These are lexical/metadata tests; they do not claim server execution coverage.
"""
import json
import re
import subprocess
import sys
from pathlib import Path

cases = json.loads(Path(__file__).with_name("sql_clickhouse.json").read_text(encoding="utf-8"))
# The C API accepts NUL-terminated strings; Go/Python additionally reject embedded
# NUL before crossing that boundary. That binding-only case cannot reach C.
cases = [c for c in cases if "\x00" not in c["query"]]
assert sum(c["safe"] for c in cases) >= 60
assert sum(not c["safe"] for c in cases) >= 10


def probe(queries, mode=None, limit=0):
    args = [sys.argv[1]] + ([mode] if mode else [])
    data = "".join(f"5 {limit} 0 0 {q.encode().hex()}\n" for q in queries)
    result = subprocess.run(args, input=data, text=True, capture_output=True, check=True, timeout=60)
    lines = result.stdout.splitlines()
    assert len(lines) == len(queries), result.stderr
    return [line.split(" ") for line in lines]


queries = [c["query"] for c in cases]
metadata = probe(queries)
formatted = probe(queries, "--format")
valid = []
for case, result, layout in zip(cases, metadata, formatted):
    status, truncated, count, op, text, summary = result
    operation, text, summary = [bytes.fromhex(s).decode() for s in (op, text, summary)]
    format_status, length, output = layout
    output = bytes.fromhex(output).decode()
    if not case["safe"]:
        assert int(status) != 0 and text == "" and int(count) == 0, (case["name"], result)
        assert int(format_status) != 0 and output == "", (case["name"], layout)
        continue
    assert int(status) == 0 and truncated == "0", (case["name"], result)
    assert operation == case["operation"], (case["name"], operation)
    assert int(count) == (2 if case["name"] == "multiple statements" else 1), case["name"]
    assert all(fragment in summary for fragment in case["contains"]), (case["name"], summary)
    assert "SECRET" not in text + summary, (case["name"], result)
    assert int(format_status) == 0 and int(length) == len(output.encode()), (case["name"], layout)
    # Formatting may change whitespace only, never literals, parameters or comments.
    assert re.sub(r"\s", "", output) == re.sub(r"\s", "", case["query"]), case["name"]
    valid.append((case, output, result))

for (case, output, before), after, layout in zip(
    valid, probe([v[1] for v in valid]), probe([v[1] for v in valid], "--format")
):
    assert after[0:4] == before[0:4], (case["name"], before, after)
    assert re.sub(r"\s", "", bytes.fromhex(after[5]).decode()) == re.sub(
        r"\s", "", bytes.fromhex(before[5]).decode()
    ), (case["name"], before, after)
    assert re.sub(r"\s", "", bytes.fromhex(after[4]).decode()) == re.sub(
        r"\s", "", bytes.fromhex(before[4]).decode()
    ), case["name"]
    assert int(layout[0]) == 0 and bytes.fromhex(layout[2]).decode() == output, case["name"]

for limit in (1, 7, 32):
    for case, result in zip(cases, probe(queries, limit=limit)):
        assert len(bytes.fromhex(result[4])) <= limit, (case["name"], limit)
        assert "SECRET" not in bytes.fromhex(result[4] + result[5]).decode(), case["name"]
        if not case["safe"]:
            assert int(result[0]) != 0 and not result[4], case["name"]

print(f"ClickHouse: {len(valid)} accepted, {len(cases)-len(valid)} rejected; round trips and byte caps passed")
