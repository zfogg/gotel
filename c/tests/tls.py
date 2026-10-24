"""Real TLS 1.2/1.3 export and fail-closed verification, entirely on loopback."""
from pathlib import Path
import os
import shutil
import socketserver
import ssl
import subprocess
import sys
import tempfile
import threading
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'testing' / 'python'))
from gotel_testing import Collector
openssl = shutil.which('openssl') or r'C:\Program Files\Git\usr\bin\openssl.exe'
probe = sys.argv[1]
def run(endpoint, ca, success=True, env=None):
    subprocess.run([probe, endpoint, str(ca), '1' if success else '0'], check=True, timeout=20, env=env)
with tempfile.TemporaryDirectory(prefix='gotel-tls-') as directory:
    root = Path(directory)
    def command(*args):
        subprocess.run([openssl, *map(str, args)], cwd=root, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    command('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', 'ca.key', '-out', 'ca.pem',
            '-days', '2', '-subj', '/CN=gotel-test-root', '-addext', 'basicConstraints=critical,CA:TRUE')
    command('req', '-newkey', 'rsa:2048', '-nodes', '-keyout', 'server.key', '-out', 'server.csr', '-subj', '/CN=localhost')
    (root/'extensions').write_text('subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=critical,CA:FALSE\nextendedKeyUsage=serverAuth\n')
    command('x509', '-req', '-in', 'server.csr', '-CA', 'ca.pem', '-CAkey', 'ca.key', '-CAcreateserial',
            '-out', 'server.pem', '-days', '1', '-extfile', 'extensions')
    command('x509', '-req', '-in', 'server.csr', '-CA', 'ca.pem', '-CAkey', 'ca.key', '-CAcreateserial',
            '-out', 'expired.pem', '-days', '0', '-extfile', 'extensions')
    def context(version, certificate='server.pem'):
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.minimum_version = ctx.maximum_version = version
        ctx.load_cert_chain(root/certificate, root/'server.key')
        return ctx
    for version in [ssl.TLSVersion.TLSv1_2, ssl.TLSVersion.TLSv1_3]:
        with Collector(ssl_context=context(version)) as collector:
            for path in ('traces', 'logs', 'metrics'): collector.script('/v1/'+path, [503])
            run(collector.endpoint(), root/'ca.pem')
            assert {p for p, _ in collector.records} == {'/v1/traces', '/v1/logs', '/v1/metrics'}
            assert len(collector.attempts) == 6
            collector.clear()
            run(collector.endpoint(), '', False)
            assert not collector.attempts, 'untrusted certificate sent application data'
            env = dict(os.environ, SSL_CERT_FILE=str(root/'ca.pem'))
            run(collector.endpoint(), '', env=env)
            assert len(collector.records) == 3, 'default OpenSSL trust paths were not used'
            print(version.name, 'all signals, retry, untrusted root, default trust: passed')
    # A trusted chain is insufficient when the endpoint hostname does not match.
    (root/'extensions').write_text('subjectAltName=DNS:wrong.invalid\nbasicConstraints=critical,CA:FALSE\n')
    command('x509', '-req', '-in', 'server.csr', '-CA', 'ca.pem', '-CAkey', 'ca.key', '-CAcreateserial',
            '-out', 'wrong.pem', '-days', '1', '-extfile', 'extensions')
    for certificate in ['wrong.pem', 'expired.pem']:
        with Collector(ssl_context=context(ssl.TLSVersion.TLSv1_3, certificate)) as collector:
            run(collector.endpoint(), root/'ca.pem', False)
            assert not collector.attempts
    (root/'invalid.pem').write_text('invalid CA file')
    run('https://127.0.0.1:1/v1/traces', root/'invalid.pem', False)
    # A peer which accepts TCP but never responds to ClientHello must time out.
    class Stall(socketserver.BaseRequestHandler):
        def handle(self):
            self.request.settimeout(3)
            try:
                while self.request.recv(16384): pass
            except OSError: pass
    class Server(socketserver.ThreadingTCPServer): daemon_threads = True
    with Server(('127.0.0.1', 0), Stall) as server:
        thread = threading.Thread(target=server.serve_forever, kwargs={'poll_interval':.01}, daemon=True)
        thread.start()
        try: run(f'https://127.0.0.1:{server.server_address[1]}/v1/traces', root/'ca.pem', False)
        finally: server.shutdown(); thread.join()
print('TLS certificate mismatch/expiry/invalid bundle and handshake timeout: passed')

