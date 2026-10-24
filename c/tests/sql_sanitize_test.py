"""Exercise the exported C sanitizer independently of SQL summarization."""
import json
import random
import subprocess
import sys
from pathlib import Path

root = Path(__file__).parent
probe = sys.argv[1]


def run(cases):
    lines = [f"{c['dialect']} {c.get('limit', 0)} 0 {int(c.get('keep', False))} {c['query'].hex()}\n"
             for c in cases]
    result = subprocess.run([probe, '--sanitize'], input=''.join(lines), text=True,
                            capture_output=True, check=True, timeout=90)
    output = result.stdout.splitlines()
    assert len(output) == len(cases), (len(output), len(cases), result.stderr)
    rows = []
    for line in output:
        status, truncated, encoded = line.split(' ', 2)
        rows.append((int(status), bool(int(truncated)), bytes.fromhex(encoded).decode('utf-8')))
    return rows


reference = json.loads((root / 'sql_reference.json').read_text(encoding='utf-8'))
reference += json.loads((root / 'stonkpile_pi5.json').read_text(encoding='utf-8'))
cases = [{'dialect': c['dialect'], 'query': c['query'].encode()} for c in reference]
for c, (status, truncated, text) in zip(reference, run(cases)):
    assert (status == 0) == c['safe'], (c, status)
    assert text == c['text'] and not truncated, (c, text, truncated)

syntax = [
    (2, b"SELECT E'secret\\'tail', U&'d\\0061ta', B'1010', X'DEAD'", 'SELECT ?, ?, ?, ?'),
    (2, b'SELECT $private$secret\n/* hidden */$private$, $1::text', 'SELECT ?, ?::text'),
    (4, b'SELECT _utf8mb4\'secret\', "private", 0xDEAD # comment\nFROM t', 'SELECT ?, ?, ? FROM t'),
    (1, b'SELECT [odd name] FROM [a table] WHERE x=:name AND y=@token AND z=?12',
     'SELECT [odd name] FROM [a table] WHERE x=? AND y=? AND z=?'),
    (3, "SELECT ['秘密', 123, [TRUE]], value[456] FROM résumé".encode(),
     'SELECT [?, ?, [?]], value[?] FROM résumé'),
    (2, b'SELECT * FROM t WHERE id IN ($1, $2, $3) AND x IN (SELECT id FROM y)',
     'SELECT * FROM t WHERE id IN (?) AND x IN (SELECT id FROM y)'),
    (2, b'WITH c AS (SELECT CASE WHEN x > 123 THEN \'secret\' ELSE NULL END FROM t) SELECT * FROM c',
     'WITH c AS (SELECT CASE WHEN x > ? THEN ? ELSE ? END FROM t) SELECT * FROM c'),
    (0, b'SELECT 0xCAFE, 0b101, .5, 1_000.25e-2, TRUE, false, null',
     'SELECT ?, ?, ?, ?, ?, ?, ?'),
    (2, b'SELECT /* outer /* inner secret */ hidden */ id -- secret\nFROM t', 'SELECT id FROM t'),
    (2, b'SELECT ARRAY[\'secret\', 123], tags[456], $1::text[]',
     'SELECT ARRAY[?, ?], tags[?], ?::text[]'),
]
for dialect, query, expected in syntax:
    for keep in (False, True):
        status, _, text = run([{'dialect': dialect, 'query': query, 'keep': keep}])[0]
        want = expected if not keep else expected.replace('IN (?)', 'IN (?, ?, ?)')
        assert status == 0 and text == want, (query, keep, text, want)

multi = [
    (b"SELECT 1; UPDATE t SET x='SECRET'; DELETE FROM t WHERE id=2",
     'SELECT ?; UPDATE t SET x=?; DELETE FROM t WHERE id=?'),
    (b"SELECT ';' AS semi; SELECT 2", 'SELECT ? AS semi; SELECT ?'),
    (b"SELECT 1;/* line one ;\n line two */SELECT 2", 'SELECT ?; SELECT ?'),
    (b"SELECT 1; -- hidden ;\nSELECT 2", 'SELECT ?; SELECT ?'),
]
for query, expected in multi:
    status, _, text = run([{'dialect': 2, 'query': query}])[0]
    assert status == 0 and text == expected, (query, text, expected)
for query in (b"SELECT 1; SELECT 'SECRET", b'SELECT 1; /* SECRET'):
    status, _, text = run([{'dialect': 2, 'query': query}])[0]
    assert status != 0 and not text

for bad in [b"SELECT 'secret", b'SELECT /* secret', b'SELECT $tag$secret$other$',
            b'SELECT "secret', b'SELECT E\'secret\\', b'SELECT [secret',
            b'SELECT "\xc0\xaf"', b'SELECT "\xed\xa0\x80"', b'SELECT "\xf4\x90\x80\x80"']:
    for dialect in range(5):
        status, truncated, text = run([{'dialect': dialect, 'query': bad, 'limit': 8}])[0]
        assert status != 0 and not truncated and not text, (bad, dialect, status, text)

long_query = b'SELECT ' + b', '.join(b'col' + str(i).encode() for i in range(9000)) + b" FROM t WHERE token='SECRET'"
assert len(long_query) > 32768
for limit in [0, 1, 2, 7, 32767, 32768, 32769, len(long_query)]:
    status, truncated, text = run([{'dialect': 2, 'query': long_query, 'limit': limit}])[0]
    effective = limit or 32768
    assert status == 0 and len(text.encode()) <= effective
    assert truncated == (len(long_query) - len('SECRET') - 1 > effective)
    assert 'SECRET' not in text

utf8_query = 'SELECT "日本😀é" FROM résumé WHERE x=123'.encode()
utf8_safe = 'SELECT "日本😀é" FROM résumé WHERE x=?'.encode()
for limit in range(1, len(utf8_query) + 1):
    status, truncated, text = run([{'dialect': 1, 'query': utf8_query, 'limit': limit}])[0]
    assert status == 0 and len(text.encode()) <= limit and utf8_safe.startswith(text.encode())
    assert truncated == (len(utf8_safe) > limit)

rng = random.Random(847291)
generated = []
for i in range(3000):
    payload = 'SECRET_' + ''.join(rng.choice('abcXYZ0123 /*$?\n\t') for _ in range(rng.randrange(1, 130)))
    generated.append({'dialect': i % 5, 'query': ("SELECT '" + payload + "' FROM t WHERE id=" + str(i)).encode()})
for c, (status, _, text) in zip(generated, run(generated)):
    assert status == 0 and 'SECRET' not in text and text.endswith('WHERE id=?'), (c, text)

print(f'{len(reference)} historical/live cases, {len(syntax)} syntax cases, 3000 secret mutations, '
      'five-dialect malformed inputs, 32 KiB and UTF-8 boundaries passed')
