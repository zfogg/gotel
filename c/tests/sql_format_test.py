"""Independent token oracle, real-world corpus, mutation and execution checks.

Install requirements-format.txt. The probe uses the same exported C API as
consumers. SQLGlot is a test-only lexer; DuckDB and SQLite execute before/after
samples against isolated, in-memory databases (never production databases).
"""
import json
import random
import re
import sqlite3
import subprocess
import sys
from pathlib import Path

import duckdb
import sqlglot
import sqlparse

ROOT = Path(__file__).resolve().parents[2]
fixture = json.loads((ROOT / 'testdata/sql_format.json').read_text(encoding='utf-8'))
cases = fixture['cases']
assert len(cases) >= 600
assert len({c.get('source') for c in cases if c.get('source')}) >= 40
dialects = {'generic': 0, 'sqlite': 1, 'postgresql': 2, 'duckdb': 3, 'mysql': 4, 'clickhouse': 5}


def format_many(inputs):
    data = ''.join(f"{dialects[c['dialect']]} {c.get('capacity', 0)} 0 0 {c['sql'].encode('utf-8').hex()}\n" for c in inputs)
    p = subprocess.run([sys.argv[1], '--format'], input=data, text=True,
                       capture_output=True, check=True, timeout=120)
    lines = p.stdout.splitlines()
    assert len(lines) == len(inputs), (len(lines), len(inputs), p.stderr)
    return [(int(a), int(b), bytes.fromhex(c).decode('utf-8'))
            for a, b, c in (line.split(' ', 2) for line in lines)]


def tokens(sql, dialect):
    dialect = 'postgres' if dialect == 'postgresql' else dialect
    return [(str(t.token_type), t.text, t.comments)
            for t in sqlglot.tokenize(sql, read=dialect if dialect != 'generic' else None)]


oracle_fallbacks = []
def preserved(before, after, dialect, name):
    # Stronger than AST equivalence: every non-whitespace byte must remain.
    assert re.sub(r'\s', '', before) == re.sub(r'\s', '', after), name
    try:
        original = tokens(before, dialect)
    except sqlglot.errors.TokenError:
        # SQLGlot itself cannot lex a few sqlc expressions. Keep the byte
        # invariant, exact placeholders/comments below, and report the gap.
        oracle_fallbacks.append(name)
    else:
        assert original == tokens(after, dialect), (name, before, after)
    # Comment and parameter counts/order, including trailing whitespace.
    def comments(sql):
        return [text.rstrip('\r\n') for kind, text in sqlparse.lexer.tokenize(sql)
                if kind in sqlparse.tokens.Comment]
    assert comments(before) == comments(after), (name, 'comments')
    for pattern in (r'\$\d+\b', r'\?\d*', r':\w+\b', r'%\(\w+\)s'):
        assert re.findall(pattern, before, re.S) == re.findall(pattern, after, re.S), (name, pattern)


outputs = format_many(cases)
valid_cases = []
for c, (status, length, out) in zip(cases, outputs):
    if not c['valid']:
        assert status != 0 and out == '', c['name']
        continue
    assert status == 0 and length == len(out.encode()), (c['name'], status)
    if 'expected' in c:
        assert out == c['expected'], (c['name'], 'layout', c['expected'], out)
    preserved(c['sql'], out, c['dialect'], c['name'])
    valid_cases.append({**c, 'sql': out})
for c, (status, _, out) in zip(valid_cases, format_many(valid_cases)):
    assert status == 0 and out == c['sql'], ('unstable', c['name'])

# Seeded generated statements cover combinations absent from handpicked cases.
rng = random.Random(20261023)
mutations = []
for i in range(1000):
    dialect = ('postgresql', 'mysql', 'sqlite', 'duckdb')[i % 4]
    marker = '$1' if dialect == 'postgresql' else '?'
    literal = "'" + ''.join(rng.choice('aZ09 ;?/*$\t') for _ in range(rng.randrange(1, 40))) + "'"
    gap = rng.choice([' ', '\t', '\n', '  '])
    sql = f"-- case {i}  \nselect{gap}id,{literal} as value from records /* keep {i} */ where id={marker} and active=1; -- end {i}  "
    mutations.append(dict(name=f'mutation/{i}', dialect=dialect, sql=sql))
formatted = format_many(mutations)
for c, (status, _, out) in zip(mutations, formatted):
    assert status == 0, c['name']
    preserved(c['sql'], out, c['dialect'], c['name'])
for (_, _, first), (status, _, second) in zip(formatted, format_many([{**c, 'sql': r[2]} for c, r in zip(mutations, formatted)])):
    assert status == 0 and first == second

# Buffer boundaries and documented resource limits fail without partial output.
for c, (status, _, out) in zip([
    dict(dialect='postgresql', sql='select a,b from t', capacity=1),
    dict(dialect='duckdb', sql='(' * 65 + ')' * 65),
    dict(dialect='postgresql', sql='x' * (1024 * 1024 + 1)),
    dict(dialect='sqlite', sql='select ' + ','.join('x' for _ in range(33000))),
], format_many([
    dict(dialect='postgresql', sql='select a,b from t', capacity=1),
    dict(dialect='duckdb', sql='(' * 65 + ')' * 65),
    dict(dialect='postgresql', sql='x' * (1024 * 1024 + 1)),
    dict(dialect='sqlite', sql='select ' + ','.join('x' for _ in range(33000))),
])):
    assert status != 0 and not out, c

execution_count = 0
for dialect, db in [('sqlite', sqlite3.connect(':memory:')), ('duckdb', duckdb.connect())]:
    db.execute('create table records(id integer, team varchar, active integer, secret varchar)')
    db.execute("insert into records values (1,'a',1,'s'),(2,'a',0,'t'),(3,'b',1,'u')")
    queries = [
        'select id,team from records where id=? and active=1',
        'with a as (select * from records where active=1) select team,count(*) from a group by team order by team',
        "select 'one  two', id, case when active=1 then 'yes' else 'no' end from records order by id",
        'select team,sum(active),row_number() over(order by team) from records group by team order by team',
        'select id from records where id between 1 and 3 and active=1 order by id desc limit 2',
        "select id /* preserved */ from records -- predicate\nwhere team='a' order by id",
    ]
    if dialect == 'duckdb':
        queries += ['select * exclude(secret) from records qualify row_number() over(partition by team order by id)=1 order by id',
                    'select team,count(*) from records group by all order by team',
                    "select {'x':[1,2]}, 9//2, id from records order by id"]
    for q, (status, _, out) in zip(queries, format_many([dict(dialect=dialect, sql=q) for q in queries])):
        assert status == 0
        params = [1] if '?' in q else []
        assert db.execute(q, params).fetchall() == db.execute(out, params).fetchall(), (q, out)
        execution_count += 1
    db.close()
print(f'PASS: {len(cases)} corpus cases, 1000 mutations, limits, {execution_count} execution comparisons; '
      f'{len(oracle_fallbacks)} SQLGlot input-lexer fallbacks: {sorted(set(oracle_fallbacks))}')
