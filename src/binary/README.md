# gotel binary

Build from the repository checkout with Go 1.26+ and a C compiler (CGO enabled):

```sh
cd src/binary
go build -o gotel .
go test ./...
```

The binary embeds the existing shared C SQL engine and needs no server or runtime
SQL dependencies. Its source references the engine and bundled utf8proc in this
checkout; build it from a complete checkout rather than `go install ...@version`.

```sh
echo 'select a,b from users where id=42' | ./gotel sql format --dialect postgresql
./gotel sql sanitize "select * from users where id=42"
./gotel sql format --dialect postgresql --edit-file queries.sql
```

`format` and `sanitize` read a single SQL argument or stdin and write to stdout.
Dialects: `generic` (default), `postgresql`, `sqlite`, `mysql`, `duckdb`, `clickhouse`.
Input is limited to 1 MiB. Invalid quoting, invalid UTF-8, embedded NULs and lexer
limits cause an error before an edited file is written. Formatting is lexical,
not SQL validation.

`--edit-file PATH` uses conservative formatting: existing horizontal gaps between
tokens become one space. Every existing newline, blank line, indentation, comment,
literal and trailing whitespace is preserved byte for byte, including CRLF and
missing final newlines. It handles multiple statements without splitting on
semicolons, which may occur inside comments and literals. It does not reflow
clauses like stdout formatting. The completed output is written to a temporary
sibling and renamed over the original, retaining permission bits. Symlinks and
nonregular files are rejected. Replacing a file changes its inode; other hard links
continue to refer to the original contents.
