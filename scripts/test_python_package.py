"""Build an sdist/wheel and import the installed wheel outside the checkout."""
import os
from pathlib import Path
import subprocess
import tempfile
import zipfile


root = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix="gotel-python-package-") as directory:
    work = Path(directory)
    subprocess.run(["uv", "build", str(root / "python"), "--out-dir", str(work / "dist")], check=True)
    wheel, = (work / "dist").glob("*.whl")
    with zipfile.ZipFile(wheel) as archive:
        names = archive.namelist()
        assert not any(name.endswith((".pyd", ".so")) for name in names), names
        assert "gotel/py.typed" in names
    subprocess.run(["uv", "venv", str(work / "venv")], check=True)
    interpreter = work / "venv" / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    subprocess.run(["uv", "pip", "install", "--python", str(interpreter), str(wheel)], check=True)
    subprocess.run([str(interpreter), "-I", "-c", """
from gotel import Application
import sequel
from gotel.otelhost import Config, start
from gotel.testing import Capture
query = sequel.inspect("SELECT * FROM builds FINAL PREWHERE id={id:UInt64}", dialect=sequel.Dialect.CLICKHOUSE)
assert query.operation == 'SELECT' and '{id' not in query.text
assert 'FINAL PREWHERE' in query.summary
assert sequel.format_sql('SELECT 1')
with Capture('installed') as capture:
    with capture.application.operation('Test'):
        pass
    capture.require_span('installed.Test')
with start(Config(signals=(), install_global=False)):
    pass
print('Installed wheel SQL engine and tracing passed')
"""], cwd=work, check=True)
