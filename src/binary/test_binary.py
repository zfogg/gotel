"""Black-box tests of the C executable; no Go toolchain required."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

BINARY = str(Path(sys.argv.pop(1)).resolve())


def run(*args, sql=b''):
    return subprocess.run([BINARY, *args], input=sql, capture_output=True, timeout=10)


class BinaryTests(unittest.TestCase):
    def test_stdout_dialects(self):
        for dialect in ('generic', 'postgresql', 'mysql', 'sqlite', 'duckdb', 'clickhouse'):
            with self.subTest(dialect=dialect):
                result = run('sql', 'format', '--dialect', dialect, sql=b'select a,b from t where id=42;')
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(b'a,', result.stdout)
                self.assertIn(b'id = 42', result.stdout)

    def test_nested_format_expansion(self):
        sql = b'select (' * 40 + b'select 1' + b')' * 40
        result = run('sql', 'format', sql=sql)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.count(b'select'), 41)
        self.assertEqual(result.stdout.count(b'('), 40)
        self.assertEqual(result.stdout.count(b')'), 40)

    def test_sanitize(self):
        result = run('sql', 'sanitize', "select * from t where name='secret' and id=42")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn(b'secret', result.stdout)
        self.assertNotIn(b'42', result.stdout)
        self.assertIn(b'?', result.stdout)

    def test_argument_and_pipe_agree(self):
        sql = "select a,b from t where x=1; -- tail\nselect 'keep  spaces';"
        for command in ('format', 'sanitize'):
            a = run('sql', command, sql)
            b = run('sql', command, sql=sql.encode())
            self.assertEqual(a.returncode, 0, a.stderr)
            self.assertEqual(b.returncode, 0, b.stderr)
            self.assertEqual(a.stdout, b.stdout)

    def test_files(self):
        cases = [
            ('postgresql', b"-- name: First :one\nselect   a, b  from   t; -- trailing\n\n-- Second\nselect   'x;  y', $$a;\n b$$;\n", b"-- name: First :one\nselect a, b from t; -- trailing\n\n-- Second\nselect 'x;  y', $$a;\n b$$;\n"),
            ('sqlite', b"\r\n  select   [a]  from t;\r\n\r\n  select   'b';\r\n", b"\r\n  select [a] from t;\r\n\r\n  select 'b';\r\n"),
            ('postgresql', b'/* outer\n /* inner */  end */\nselect   1;', b'/* outer\n /* inner */  end */\nselect 1;'),
            ('mysql', b"# comment\nselect   `a`, 'a  b'; # tail\nselect   2;", b"# comment\nselect `a`, 'a  b'; # tail\nselect 2;"),
            ('postgresql', b"select   'a'\n  'b';\n", b"select 'a'\n  'b';\n"),
            ('generic', b'', b''),
            ('generic', b' \t\n\n', b' \t\n\n'),
            ('generic', b'select   1;', b'select 1;'),
            ('postgresql', b"select  a::text, b->>'x', - -1, x != 2;", b"select a::text, b->>'x', - -1, x != 2;"),
            ('postgresql', 'select   \'日本語  café\';\r'.encode(), 'select \'日本語  café\';\r'.encode()),
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'queries with spaces.sql'
            for dialect, sql, expected in cases:
                with self.subTest(sql=sql):
                    path.write_bytes(sql)
                    path.chmod(0o640)
                    mode = path.stat().st_mode
                    for _ in range(2):
                        result = run('sql', 'format', '--dialect', dialect, '--edit-file', str(path))
                        self.assertEqual(result.returncode, 0, result.stderr)
                        self.assertEqual(result.stdout, b'')
                        self.assertEqual(path.read_bytes(), expected)
                        self.assertEqual(path.stat().st_mode, mode)
                    self.assertEqual(list(Path(directory).glob('*.tmp')), [])

    def test_errors_preserve_original(self):
        bad = [b"select 'unterminated", b'/* unfinished', b'select $$unfinished', b'select \0', b'select \xff', b'x' * (1024 * 1024 + 1)]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'bad.sql'
            for sql in bad:
                path.write_bytes(sql)
                result = run('sql', 'format', '--dialect', 'postgresql', '--edit-file', str(path))
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, b'')
                self.assertEqual(path.read_bytes(), sql)
                self.assertEqual(list(Path(directory).glob('*.tmp')), [])

    def test_bad_arguments(self):
        for args in [[], ['sql', 'unknown'], ['sql', 'format', '--dialect', 'unknown'], ['sql', 'sanitize', '--edit-file', 'x'], ['sql', 'format', '--edit-file', 'missing.sql'], ['sql', 'format', 'a', 'b'], ['sql', 'format', '--edit-file', 'x', 'select 1'], ['sql', 'format', '--edit-file', '']]:
            with self.subTest(args=args):
                self.assertNotEqual(run(*args).returncode, 0)

    def test_pipe_limit_and_nul(self):
        for sql in [b'x' * (1024 * 1024 + 1), b'select \0 1']:
            self.assertNotEqual(run('sql', 'format', sql=sql).returncode, 0)

    def test_help(self):
        for args in [('--help',), ('sql', 'format', '--help')]:
            result = run(*args)
            self.assertEqual(result.returncode, 0)
            self.assertIn(b'Usage:', result.stdout)

    @unittest.skipIf(os.name == 'nt', 'POSIX pseudo-terminal required')
    def test_interactive_stdin_does_not_wait(self):
        import pty
        master, slave = pty.openpty()
        try:
            for command in ('format', 'sanitize'):
                result = subprocess.run([BINARY, 'sql', command], stdin=slave, capture_output=True, timeout=3)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(b'Usage:', result.stderr)
            result = subprocess.run([BINARY, 'sql', 'format', 'select 1'], stdin=slave, capture_output=True, timeout=3)
            self.assertEqual(result.returncode, 0)
        finally:
            os.close(master)
            os.close(slave)

    @unittest.skipIf(os.name == 'nt', 'POSIX symlink permissions required')
    def test_symlinks_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            original = Path(directory) / 'original.sql'
            original.write_bytes(b'select   1;')
            link = Path(directory) / 'link.sql'
            link.symlink_to(original)
            self.assertNotEqual(run('sql', 'format', '--edit-file', str(link)).returncode, 0)
            self.assertEqual(original.read_bytes(), b'select   1;')


if __name__ == '__main__':
    unittest.main()
