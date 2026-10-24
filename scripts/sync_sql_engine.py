"""Bundle the single C SQL implementation into independently buildable packages."""
import argparse
import hashlib
from pathlib import Path

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--check", action="store_true")
args = parser.parse_args()
sources = sorted((root / "c/src/sql").glob("*.c")) + [
    root / "c/src/utf8.c", root / "c/src/utf8.h", root / "c/include/gotel/sql.h",
]
changed = []
def write(path, data):
    if path.exists() and path.read_bytes() == data:
        return
    changed.append(str(path.relative_to(root)))
    if not args.check:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)

digest = hashlib.sha256()
for source in sources:
    data = source.read_text(encoding="utf-8").replace("\r\n", "\n").encode()
    relative = source.relative_to(root / "c")
    digest.update(relative.as_posix().encode() + b"\0" + data)
    for bundle in (root / "go/sqltrace/engine", root / "python/vendor/engine"):
        write(bundle / relative, data)
for name in ("utf8proc.c", "utf8proc.h", "UTF8PROC_LICENSE.md"):
    write(root / "python/vendor" / name, (root / "go/sqltrace" / name).read_bytes())
for source in (root / "go/sqltrace/utf8proc_vendor").glob("*"):
    if source.is_file():
        write(root / "python/vendor/utf8proc_vendor" / source.name, source.read_bytes())
# Include test corpora too: the nested Go module is testable from a module ZIP.
for source in (root / "testdata").glob("*"):
    if source.is_file():
        write(root / "go/testdata" / source.name, source.read_bytes())
for name in ("sql_edge_cases.json", "sql_reference.json", "stonkpile_pi5.json", "sql_clickhouse.json"):
    write(root / "go/sqltrace/testdata/shared" / name, (root / "c/tests" / name).read_bytes())
write(root / "python/LICENSE", (root / "LICENSE").read_bytes())
write(root / "go/LICENSE", (root / "LICENSE").read_bytes())
bridge = root / "go/sqltrace/summary_native.c"
write(bridge, (f"/* Shared SQL source SHA256: {digest.hexdigest()} */\n"
    '#define GOTEL_SQL_STANDALONE 1\n'
    '#include "engine/src/sql/main.c"\n'
    '#include "engine/src/utf8.c"\n').encode())
if args.check and changed:
    raise SystemExit("Run python scripts/sync_sql_engine.py: " + ", ".join(changed))
