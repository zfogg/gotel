"""Check the real C release ZIP contract using an existing native build."""
import json
from pathlib import Path
import sys
import tempfile
import zipfile

from package_release import ROOT, package_c


def check(build, linkage):
    cache = {}
    for line in (build / 'CMakeCache.txt').read_text(encoding='utf-8').splitlines():
        if '=' in line and not line.startswith(('#', '//')):
            key, value = line.split('=', 1)
            cache[key.split(':', 1)[0]] = value
    tag = 'v' + json.loads((ROOT / 'package.json').read_text(encoding='utf-8'))['version']
    with tempfile.TemporaryDirectory(prefix='gotel-package-') as directory:
        path = package_c(tag, build, Path(cache['otlp-c_SOURCE_DIR']), Path(directory), linkage,
                         nghttp2_source=Path(cache['nghttp2_SOURCE_DIR']))
        with zipfile.ZipFile(path) as archive:
            names = archive.namelist()
            assert any(n.endswith('/third-party/nghttp2-COPYING') for n in names), names
            assert any(n.endswith('/include/gotel/gotel.h') for n in names), names
            assert any(n.endswith('/include/sequel.h') for n in names), names
            assert any(n.endswith('/third-party/sequel-LICENSE') for n in names), names
            assert not any(n.endswith('/testing.h') for n in names), names
            if linkage == 'static':
                assert sum('/lib/' in n for n in names) == 5, names
                assert any(n.endswith(('/lib/libsequel.a', '/lib/sequel.lib')) for n in names), names
                assert any(n.endswith(('/lib/libnghttp2.a', '/lib/nghttp2.lib')) for n in names), names
            else:
                assert not any(n.endswith(('/lib/libnghttp2.a', '/lib/nghttp2.lib')) for n in names), names
            assert archive.testzip() is None
    print(f'{linkage} release ZIP: libraries, headers, and nghttp2 license passed')


if __name__ == '__main__':
    check(Path(sys.argv[1]), sys.argv[2])
