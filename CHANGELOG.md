# Changelog

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
