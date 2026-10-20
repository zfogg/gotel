# gotel

Reusable OpenTelemetry building blocks for Go applications, with a focus on
safe, useful database spans.

`sqltrace` provides:

- stable application prefixes such as `spaniel.storage.GetStats`;
- sqlc `-- name:` extraction and context-carried explicit names;
- sanitized SQL templates in `db.query.text` (never bind values);
- the real bind argument count in `db.query.parameter.count`;
- standard database system, namespace, operation, and summary attributes;
- dialect-tolerant sanitization for PostgreSQL, DuckDB, SQLite, and MySQL syntax;
- optional DuckDB AST summaries for genuinely ad hoc SQL.

`gormtrace` and `pgxtrace` apply that contract at the driver boundary so every
query is covered. The root package contains shared span lifecycle, correlation,
slog fan-out, standard-log routing, and shutdown helpers.

```go
namer := sqltrace.Namer{Prefix: "myapp.storage"}
db.Use(gormtrace.New(gormtrace.Config{
    Scope: "myapp/storage",
    Namer: namer,
    System: "postgresql",
    Namespace: "myapp",
}))

ctx := namer.WithName(ctx, "ListWidgets")
db.WithContext(ctx).Find(&widgets)
```
