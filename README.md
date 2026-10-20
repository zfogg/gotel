# gotel

Reusable OpenTelemetry building blocks for Go applications, with a focus on
safe, useful database spans.

`sqltrace` provides:

- stable application prefixes such as `spaniel.storage.GetStats`;
- sqlc `-- name:` extraction and context-carried explicit names;
- sanitized SQL templates in `db.query.text` (never bind values);
- the real bind argument count in `gotel.db.query.bind_count` (plus the legacy
  `db.query.parameter.count` during the v0.2 transition);
- standard database system, namespace, operation, and summary attributes;
- dialect-tolerant sanitization for PostgreSQL, DuckDB, SQLite, and MySQL syntax;
- optional DuckDB AST summaries for genuinely ad hoc SQL.

`gormtrace` and `pgxtrace` apply that contract at the driver boundary so every
query is covered. `pgxtrace` covers queries, COPY, batches, prepare, and connect;
`gormtrace` supports configurable CLIENT/INTERNAL spans. `sqltrace.StartSpan`
covers direct `database/sql` calls. `otelhost` optionally configures OTLP over
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
