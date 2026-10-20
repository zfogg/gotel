# Changelog

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
