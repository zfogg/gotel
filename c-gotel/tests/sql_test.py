"""Go-derived dialect corpus plus adversarial, bounded and mutation tests."""
import json, random, subprocess, sys
from pathlib import Path

cases=json.loads(Path(__file__).with_name('sql_reference.json').read_text(encoding='utf-8'))
production=json.loads(Path(__file__).with_name('stonkpile_pi5.json').read_text(encoding='utf-8'))
assert {c['category'] for c in production}=={'short','medium','long'}
for c in production:
    assert c['text']==c['observed_text'] and c['summary']==c['observed_summary']
    assert c['trace_url'].startswith('http://pi5:8345/traces/')
cases+=production
def inspect(cases):
    data=''.join(f"{c['dialect']} {c.get('limit',0)} {int(c.get('omit',False))} {int(c.get('keep',False))} {(c['raw'] if 'raw' in c else c['query'].encode()).hex()}\n" for c in cases)
    result=subprocess.run([sys.argv[1]],input=data,text=True,capture_output=True,check=True,timeout=60)
    lines=result.stdout.splitlines(); assert len(lines)==len(cases),(len(lines),len(cases),result.stderr)
    out=[]
    for line in lines:
        status,truncated,op,text,summary=line.split(' ')
        out.append(dict(status=int(status),truncated=bool(int(truncated)),operation=bytes.fromhex(op).decode(),
                        text=bytes.fromhex(text).decode(),summary=bytes.fromhex(summary).decode()))
    return out
errors=[]
index_query='CREATE INDEX IF NOT EXISTS idx_participants_session ON session_participants(session_id)'
index_result=inspect([dict(query=index_query,dialect=1)])[0]
assert index_result['status']==0
assert index_result['summary']=='CREATE INDEX idx_participants_session ON ...'
assert index_result['text']==index_query
for c,r in zip(cases,inspect(cases)):
    if (r['status']==0)!=c['safe'] or r['text']!=c['text'] or r['summary']!=c['summary'] or r['operation']!=c['operation']:
        errors.append((c,r))
if errors:
    for c,r in errors[:20]:print('QUERY',c.get('name',c['query'][:120]),'\nEXPECTED',c['summary'],'\nACTUAL',r)
    raise AssertionError(f'{len(errors)} mismatches in {len(cases)} Go reference cases')

rng=random.Random(81342)
mutations=[]
for i in range(2000):
    payload='SECRET_'+''.join(rng.choice('abcXYZ0123 \n\t/*$?') for _ in range(rng.randrange(1,150)))
    mutations.append(dict(dialect=i%5,query="SELECT '"+payload+"' FROM accounts WHERE id="+str(i)))
for c,r in zip(mutations,inspect(mutations)):
    assert r['status']==0 and 'SECRET' not in r['text']+r['summary'],(c,r)

limits=[dict(dialect=2,query='SELECT résumé FROM accounts',limit=i) for i in range(1,40)]
for c,r in zip(limits,inspect(limits)):
    assert len(r['text'].encode())<=c['limit']
    assert r['truncated']==(len(c['query'].encode())>c['limit'])
    assert r['summary']=='SELECT … FROM accounts'

for query in ('SELECT "日本😀é" FROM accounts', 'SELECT "𐀀é" FROM accounts'):
    boundaries=[dict(dialect=1,query=query,limit=i) for i in range(1,len(query.encode())+1)]
    for c,r in zip(boundaries,inspect(boundaries)):
        assert r['status']==0 and len(r['text'].encode())<=c['limit']
        assert query.startswith(r['text'])
invalid_utf8=[b'\xc0\xaf',b'\xed\xa0\x80',b'\xf4\x90\x80\x80',b'\xe2\x82',b'\x80']
for r in inspect([dict(dialect=1,raw=b'SELECT "'+bad+b'"',limit=1) for bad in invalid_utf8]):
    assert r['status']!=0 and r['text']=='' and r['summary']=='SQL'

malformed=["SELECT 'SECRET",'SELECT /* SECRET','SELECT $tag$SECRET$wrong$', 'SELECT "SECRET', 'SELECT E\'SECRET\\']
for r in inspect([dict(query=q,dialect=d) for q in malformed for d in range(5)]):
    assert r['status']!=0 and r['text']=='' and r['summary']=='SQL'
for r in inspect([dict(query="SELECT 'SECRET'",dialect=d,omit=True) for d in range(5)]):
    assert r['status']==0 and not r['text'] and r['summary']=='SELECT …'

# A malformed suffix must still fail closed beyond the text truncation budget.
for r in inspect([dict(query='SELECT '+('x,'*5000)+"'SECRET",dialect=d,limit=8) for d in range(5)]):
    assert r['status']!=0 and r['text']==''

# Deterministic malformed grammar exercises scanner/parser bounds under ASan.
noise=[]
alphabet="abcXYZ0123$@?:;.,[]()/*-+ '\"`\\\n"
for i in range(4000):
    noise.append(dict(query='SELECT '+''.join(rng.choice(alphabet) for _ in range(rng.randrange(1,200))),dialect=i%5))
for c,r in zip(noise,inspect(noise)):
    assert len(r['text'].encode())<=4096 and len(r['summary'].encode())<=4096
    if r['status']!=0:assert r['text']=='' and r['summary']=='SQL'
deep=[dict(query='SELECT '+('('*200)+'1'+(')'*200),dialect=d) for d in range(5)]
for r in inspect(deep):assert len(r['summary'].encode())<=4096
oversized=inspect([dict(query='SELECT '+('x'*1048576),dialect=0)])[0]
assert oversized['status']!=0 and oversized['text']==''
print(f'{len(cases)} Go/live parity cases, 2000 secret mutations, 4000 malformed grammar cases, UTF-8 and size limits passed')
