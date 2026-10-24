"""Invalidate cgo's package cache when shared C sources outside sqltrace change."""
import argparse
import hashlib
import re
from pathlib import Path

root = Path(__file__).resolve().parents[1]
paths = sorted((root / 'c-gotel/src/sql').glob('*.c')) + [
    root / 'c-gotel/src/utf8.c', root / 'c-gotel/src/utf8.h', root / 'c-gotel/include/gotel/sql.h',
]
digest = hashlib.sha256()
for path in paths:
    digest.update(path.relative_to(root).as_posix().encode() + b'\0')
    digest.update(path.read_text(encoding='utf-8').encode())
bridge = root / 'sqltrace/summary_native.c'
source = bridge.read_text(encoding='utf-8')
marker = f'/* Shared SQL source SHA256: {digest.hexdigest()} */'
updated = re.sub(r'/\* Shared SQL source SHA256: [0-9a-f]+ \*/', marker, source)
if updated == source and marker not in source:
    updated = marker + '\n' + source
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--check', action='store_true')
args = parser.parse_args()
if args.check:
    if updated != source:
        raise SystemExit('Shared C sources changed: run python scripts/sync_sql_engine.py')
else:
    bridge.write_text(updated, encoding='utf-8', newline='\n')
