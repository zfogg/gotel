"""Verify resource types and values on the actual OTLP envelopes of every signal."""
from pathlib import Path
import subprocess,sys
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'testing/python'))
from gotel_testing import Collector,protobuf
def any_value(data):
    v=protobuf(data)
    if 1 in v:return v[1][0].decode()
    if 3 in v:return v[3][0]
    if 5 in v:return [any_value(x) for x in protobuf(v[5][0]).get(1,[])]
    raise AssertionError(v)
with Collector() as receiver:
    endpoint=f'http://127.0.0.1:{receiver.server_port}/v1/traces'
    for mode in ('auto','custom','omit','disabled','invalid'):
        receiver.clear()
        subprocess.run([sys.argv[1],endpoint,mode],check=True,timeout=10)
        if mode=='invalid':assert not receiver.records;continue
        resources={}
        for path,body in receiver.records:
            for envelope in protobuf(body)[1]:
                resource=protobuf(protobuf(envelope)[1][0])
                attrs={}
                for kv in resource.get(1,[]):
                    kv=protobuf(kv);key=kv[1][0].decode();assert key not in attrs,key
                    attrs[key]=any_value(kv[2][0])
                resources[path]=attrs
        assert set(resources)=={'/v1/traces','/v1/logs','/v1/metrics'},resources
        first=next(iter(resources.values()))
        assert all(r==first for r in resources.values()),'resource differs across signals'
        assert first['service.name']=='resource-test'
        if mode=='disabled':assert 'process.pid' not in first;continue
        for key in ('host.name','os.type','os.description','process.executable.name','process.executable.path',
                    'process.owner','process.runtime.name','process.runtime.description','telemetry.sdk.name',
                    'telemetry.sdk.language','telemetry.sdk.version'):
            assert isinstance(first[key],str) and first[key],(key,first)
        assert isinstance(first['process.pid'],int) and first['process.pid']>0
        if mode=='custom':
            assert first['process.command_args']==['demo.exe','--token','[REDACTED]','résumé','']
            assert first['process.pid']==42 and first['host.name']=='explicit-host'
            assert first['service.version']=='test-version' and first['deployment.environment.name']=='test'
        elif mode=='omit':assert 'process.command_args' not in first
        else:
            assert isinstance(first['process.command_args'],list)
            assert first['process.command_args'][-1]=='auto'
print('Resource detection, scalar overrides, argv arrays, omission and all-signal envelope parity passed')
