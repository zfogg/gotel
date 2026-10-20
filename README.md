# gotel

Reusable OpenTelemetry building blocks for Go applications, with a focus on
safe, useful database spans.

`sqltrace` provides:

- stable application prefixes such as `spaniel.storage.GetStats`;
- sqlc `-- name:` extraction and context-carried explicit names;
- sanitized SQL templates in `db.query.text` (never bind values);
- the real bind argument count in `gotel.db.query.bind_count`;
- standard database system, namespace, operation, and summary attributes;
- dialect-tolerant sanitization for PostgreSQL, DuckDB, SQLite, and MySQL syntax;
- automatic bounded SQL operation-and-table summaries, independent of span names.

`gormtrace` and `pgxtrace` apply that contract at the driver boundary so every
query is covered. `pgxtrace` covers queries, COPY, batches, prepare, and connect;
`gormtrace` supports configurable CLIENT/INTERNAL spans. `sqltrace.StartSpan`
covers custom operations, while `sqltrace.QueryContext`, `QueryRowContext`, and
`ExecContext` provide lifecycle-safe `database/sql` instrumentation. `dbmetrics`
provides the standard `db.client.operation.duration` histogram and observer
composition. `otelhost` optionally configures OTLP over
gRPC or HTTP/protobuf, resources, propagation, logs, metrics, traces, headers,
test exporters, and coordinated shutdown. The root package contains shared span
lifecycle, correlation, slog fan-out, standard-log routing, and shutdown helpers.

```go
namer := sqltrace.MustNamer("myapp.storage")
db.Use(gormtrace.New(gormtrace.Config{
    Scope: "myapp/storage",
    Namer: namer,
    System: "postgresql",
    Namespace: "myapp",
	Dialect: sqltrace.DialectPostgreSQL,
}))

ctx := namer.WithName(ctx, "ListWidgets")
db.WithContext(ctx).Find(&widgets)
```

Sanitization is dialect-aware and fail-closed. It handles PostgreSQL escape,
Unicode, dollar-quoted, bit, and hex strings; nested comments; MySQL escapes and
comments; common placeholder forms; numeric/boolean/null literals; optional
`IN`-list collapsing; and a UTF-8-safe query-text cap. Bind values are never
rendered into telemetry.

Applications with stricter policies can set `SanitizeOptions.OmitQueryText` and
still retain stable names, operation classification, and bind counts. v0.3 emits
current semantic-convention attributes only: migrate `db.system` to
`db.system.name` and `db.query.parameter.count` to
`gotel.db.query.bind_count` before upgrading.

Since v0.4, `db.query.summary` describes the SQL instead of repeating the span
name. For example, a span named `stonkpile.storage.ListOutcomesForStrategyByHorizon`
has summary `SELECT signal_outcomes, signals`. CTEs (including recursive and
materialized CTEs), joins, lateral/scalar subqueries, set operations, windowed
queries and INSERT/UPDATE/DELETE/MERGE source queries resolve to physical tables.
Data-modifying CTEs retain their writes: `SELECT signals; INSERT signals`.
Function-only calls become `SELECT prepare_signal_grading`.

Summaries omit predicates, values, projection lists, aliases and sqlc names.
They preserve quoted/schema-qualified identifiers, show at most five targets
per operation, and cap the entire summary at 256 UTF-8 bytes. They are computed
from the complete sanitized statement before query-text truncation, cached with
the other metadata, and remain available when `OmitQueryText` is enabled.
They describe SQL structure, not execution plans or tables accessed inside a
stored function. Unknown syntax falls back to a recognized operation or `SQL`.
Connection and batch container spans do not emit a fictitious query summary;
batch query events and prepared statements carry their actual SQL summaries.
