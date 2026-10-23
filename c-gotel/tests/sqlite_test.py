from pathlib import Path
import subprocess,sys
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'testing/python'))
from gotel_testing import Collector,protobuf,records
def attrs(span):
    result={}
    for kv in span.get(9,[]):
        kv=protobuf(kv);value=protobuf(kv[2][0]);result[kv[1][0].decode()]=value.get(1,value.get(2,value.get(3)))[0]
    return result
with Collector() as receiver:
    for mode in ('normal','truncate','omit'):
        receiver.clear()
        subprocess.run([sys.argv[1],f'http://127.0.0.1:{receiver.server_port}/v1/traces',mode],check=True,timeout=10)
        payload=b''.join(b for _,b in receiver.records)
        assert b'LITERAL_SECRET' not in payload and b'BOUND_SECRET' not in payload
        spans=records(receiver.records,'/v1/traces')
        root=next(s for s in spans if s[5]==[b'test.database.operation'])
        sql=[s for s in spans if s[5]==[b'test.storage.FindAccount']]
        assert len(sql)==2
        for s in sql:
            assert s[1]==root[1] and s[4]==root[2]
            a=attrs(s)
            assert a['db.query.summary']=='SELECT … FROM accounts WHERE …'.encode()
            assert a['db.query.summary']!=s[5][0]
            assert a['db.namespace']==b'fixture' and a['db.system.name']==b'sqlite'
            assert a['db.operation.name']==b'SELECT' and a['gotel.db.query.bind_count']==1
            if mode=='omit':assert 'db.query.text' not in a and 'gotel.db.query.truncated' not in a
            elif mode=='truncate':assert len(a['db.query.text'])<=12 and a['gotel.db.query.truncated']==1
            else:assert a['db.query.text']==b'SELECT id FROM accounts WHERE token=? AND id=?'
        unnamed=next(s for s in spans if s[5]==[b'test.storage.UnknownQuery'])
        assert attrs(unnamed)['db.query.summary']=='SELECT …'.encode()
        assert b'db.client.operation.duration' in payload
print('SQLite named/unnamed SQL, bind privacy, reset/finalize, namespace, truncation, omission and parentage passed')
