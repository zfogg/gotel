# Changelog

## v0.2.0

- Make SQL sanitization dialect-aware, fail-closed, bounded, and fuzz-tested.
- Add `gotel.db.query.bind_count` while temporarily dual-emitting the legacy key.
- Add pgx batch, prepare, connect, server, SQLSTATE, error, row, and observation telemetry.
- Add GORM span-kind, dialect, error, row, and collection telemetry.
- Add `sqltrace.StartSpan` for direct `database/sql` operations.
- Add validated namer constructors and improved cached caller naming.
- Add the optional `otelhost` SDK/bootstrap package and cross-platform CI.
