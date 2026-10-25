# Changelog

## Unreleased

## v0.12.0

- Extract SQL formatting, sanitization, summaries, native code, language bindings,
  CLI and engine regression tests into sequel. Gotel's CMake, Go and Python
  adapters consume the same pinned sequel revision.
- Remove standalone SQL-processing APIs from gotel. Import sequel directly for
  those operations; gotel retains span naming, metadata and instrumentation.
- Package sequel headers, libraries and licenses in C release archives. Gotel's
  Python distribution is now pure Python with a native sequel dependency.

## v0.11.0

- Add canonical SQL namespaces `storage.postgres`, `storage.sqlite`,
  `storage.duckdb`, and `storage.clickhouse`, independent of application names.
  Go pgx/GORM adapters use database namespaces by default; explicit custom
  namers remain supported. Transaction spans use the same database namespace.
- Add Python `sqltrace.call`, `call_sync`, and task-local `name` for static
  names at each database execution site, plus `storage_prefix`. Statement
  execution options can override names. Eager relationship loads and writes
  to different tables within a flush receive distinct descriptive suffixes.
- Redis command and pipeline spans use `storage.redis.GET` and
  `storage.redis.PIPELINE` in both Go and Python; scopes and metric labels
  retain their existing identities.


## v0.10.1

- Make application-name span prefixes optional across C, Go, Python and
  TypeScript. Defaults preserve existing names; disabling removes one exact
  application prefix from exported names without changing service identity,
  instrumentation scopes, metrics, attributes, or trace relationships.
- Expose Python `GotelApp`, retaining `Application` as a compatibility alias.
- Document coordinated Python and TypeScript updates for Beanaries alongside
  the other four consumers.

- Publish releases from annotated tags without conflicting with shallow checkout
  refs when verifying the nested Go module tag.

## v0.10.0

- Add Go `mcptrace` middleware for the official MCP SDK: connected request/tool
  spans, request counters, duration histograms, bounded method/tool labels,
  protocol/tool error and panic handling, without recording payloads.
- Add the uv-managed `gotel` Python package with SDK lifecycle, application
  operations, HTTP, SQLAlchemy, Redis/pipeline, and Temporal adapters, correlated
  logging, and an in-memory test kit.
- Extend the shared C SQL engine with a ClickHouse dialect, typed parameters,
  escaped identifiers, clause summaries, and formatting. Reject `INSERT FORMAT`
  payload inspection rather than exposing non-SQL data.
- Move the native library to `c/`, rename C targets/options/artifacts to gotel,
  and move the Go module to `go/` (`github.com/zfogg/gotel/go`, `go/vX.Y.Z` tags).
  These packaging changes require consumer migrations.
- Bundle checked copies of the shared SQL engine into the standalone Go module
  and Python source package; add Python wheel/sdist release assets and CI.
- Add the standalone C `gotel` executable with `sql format --dialect`,
  `sql sanitize`, and conservative `--edit-file` formatting. Build to
  `build/gotel` (`build/gotel.exe` on Windows), without a Go toolchain.
- Preserve comments, literals, bind placeholders, indentation, blank lines,
  line endings, and permissions during conservative file edits. Reject malformed
  input before replacement; support Unicode SQL arguments and paths on Windows
  and clean temporary files after failed writes.
- Handle ClickHouse line comments, numeric heredoc tags, Unicode quotes,
  hexadecimal floats, typed parameters, arrays, and dialect-specific clauses
  without exposing literal or comment contents in sanitized telemetry.
- Grow SQL layout buffers for deeply nested queries while preserving the
  size-measurement and all-or-nothing caller-buffer contracts.
- Keep short boolean groups inline while indenting longer nested predicates
  across separate lines for readability.
- Keep complete bounded Python SQL templates when indentation would exceed the
  collection limit, and name FastAPI spans correctly for routes registered
  after instrumentation.
- Name browser fetch spans correctly for URL-plus-options requests and default
  GET calls, and unregister fetch instrumentation when telemetry shuts down.

## v0.9.0

- Add shared C and Go SQL formatting APIs for PostgreSQL, DuckDB, MySQL, SQLite,
  and generic SQL, preserving comments, literals, identifiers, and bind markers.
- Format sanitized `db.query.text` while retaining concise span names and query
  summaries. Indent CTEs, joins, nested predicates, CASE expressions, and windows.
- Add a shared corpus of real Stonkpile and Spaniel queries, exact layout tests,
  execution-equivalence checks, mutation tests, fuzzing, and sanitizer coverage.

## v0.8.5

- Add native OTLP/gRPC export for traces, metrics, and logs with HTTP/2 flow control, verified TLS, trailer statuses, and RetryInfo backpressure.
- Enable gRPC support by default and include it in static/shared releases; static ZIPs include the nghttp2 archive and all ZIPs include its license.
- Normalize pinned dependency checks across Git line endings and use the configured Python interpreter for gRPC test peers.

## v0.7.0

- Add the shared `Application` facade, resource builder, HTTP client/server bundle, background and cleanup contexts, safe error recording, and trace test kit.
- Add reusable Temporal and Redis instrumentation with application-prefixed workflow, activity, and command names.
- Add a complete browser telemetry provider/resource/fetch factory for TypeScript applications.

## v0.6.0

- Add router-independent HTTP client/server span naming with application prefixes,
  all HTTP methods, unchanged destination attributes, escaped client paths and
  bounded server route templates.
- Add route-finalization middleware and a direct existing-span hook without
  adding router dependencies, duplicate spans or response-writer wrappers.

## v0.5.0

- Render scoped CTE definitions, nested queries, typed/LATERAL joins and set
  operations instead of flattening them into a list of physical tables.
- Retain LIMIT/OFFSET/FETCH bounds at their original scope: fixed integer counts
  remain visible; placeholders and expressions become `?`, without interpolation.
- Include grouping, ordering, window modifiers, conflicts, writes and locking;
  raise the structural summary budget to 4096 UTF-8 bytes.
- Keep query-text sanitization unchanged and classify outer operations separately.
- Add a sqlsummary CLI for ranking and summarizing generated sqlc statements.

## v0.4.2

- Respect non-recursive CTE definition scope, summarize parenthesized join
  groups, and retain functions reached through CTEs and scalar subqueries.
- Accept platform line endings in the SQL corpus snapshot.

## v0.4.1

- Preserve Unknown span naming for empty SQL and operation-name fallbacks for
  SQL outside the summary grammar.

## v0.4.0

- Generate SQL operation-and-table summaries instead of repeating span names.
- Resolve CTEs, joins, subqueries, set operations and write dependencies, with
  function names for table-free queries and bounded, literal-free output.
- Apply summaries consistently to SQL, GORM, pgx, COPY, batch query events and
  prepared statements; classify CTE statements by their outer SQL operation.
- Cover all 540 Stonkpile named queries in a portable, versioned SQL corpus,
  plus focused complex-query, privacy, adapter and all-dialect fuzz regressions.
- Replace the DuckDB-only summary parser with a shared token-based structural
  summarizer; no database connection or native parser dependency is required.

## v0.3.0

- Add lifecycle-safe `database/sql` query, query-row, and exec adapters.
- Add reusable standard database duration metrics and observer composition.
- Add a query-text-disable privacy policy and a bounded SQL metadata cache.
- Honor context-carried stable names across manual and `database/sql` spans.
- Emit only current `db.system.name` and `gotel.db.query.bind_count` attributes.
- Expand sanitizer fuzzing across PostgreSQL, DuckDB, SQLite, MySQL, and generic SQL.
- Expand GORM callback, benchmark, race, and cross-platform CI coverage.

## v0.2.2

- Improve sanitizer correctness for dialect-specific quoting, placeholders,
  comments, literals, malformed input, UTF-8 truncation, and `IN` lists.

## v0.2.1

- Add incremental adapter fixes and documentation after the v0.2 release.

## v0.2.0

- Make SQL sanitization dialect-aware, fail-closed, bounded, and fuzz-tested.
- Add `gotel.db.query.bind_count` while temporarily dual-emitting the legacy key.
- Add pgx batch, prepare, connect, server, SQLSTATE, error, row, and observation telemetry.
- Add GORM span-kind, dialect, error, row, and collection telemetry.
- Add `sqltrace.StartSpan` for direct `database/sql` operations.
- Add validated namer constructors and improved cached caller naming.
- Add the optional `otelhost` SDK/bootstrap package and cross-platform CI.
