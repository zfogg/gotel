# gotel binary

Build the standalone C executable from a complete repository checkout using
CMake 3.24+ and a C99 compiler. No Go toolchain or network download is required.
The binary embeds the shared C SQL engine and the bundled utf8proc sources.

```sh
cmake -S src/binary -B build/binary -DCMAKE_BUILD_TYPE=Release
cmake --build build/binary --config Release
ctest --test-dir build/binary -C Release --output-on-failure
```

Python 3 is used only for black-box tests. For a build without tests, configure
with `-DBUILD_TESTING=OFF`. Install with `cmake --install build/binary --config
Release --prefix /your/prefix`. With Visual Studio, the executable is under
`build/binary/Release/gotel.exe`; single-configuration generators put it directly
under `build/binary/`.

```sh
echo 'select a,b from users where id=42' | build/binary/gotel sql format --dialect postgresql
build/binary/gotel sql sanitize "select * from users where id=42"
build/binary/gotel sql format --dialect postgresql --edit-file queries.sql
```

`format` and `sanitize` accept one SQL argument or piped/redirected stdin and write
to stdout. Without an argument, interactive terminal input prints usage and exits
with an error immediately. Use `--` before an SQL argument starting with `-`.
Dialects: `generic` (default), `postgresql`, `sqlite`, `mysql`, `duckdb`, `clickhouse`.
Input reads are bounded to 1 MiB plus one byte to detect overflow. Invalid quoting,
invalid UTF-8, embedded NULs and lexer limits fail before edited files are written.
Formatting is lexical, not SQL validation.

`--edit-file PATH` conservatively collapses existing horizontal gaps between
tokens to one space. Newlines, blank lines, indentation, comments, literals and
trailing whitespace are preserved byte for byte, including CRLF and missing final
newlines. Multiple statements are lexed together, without splitting semicolons
inside quoted text or comments. This mode does not reflow clauses like stdout
formatting. The complete result is written and flushed to an exclusive temporary
sibling before replacing the original, retaining permission bits. Symlinks and
nonregular files are rejected. Replacement changes the inode; other hard links
retain their original contents.
