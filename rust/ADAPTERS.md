# Native library adapters

Enable only the integrations you use:

| Cargo feature | Rust module | Supported library |
| --- | --- | --- |
| `http-axum` | `gotel::tracers::axum` | Axum 0.8 |
| `http-reqwest` | `gotel::tracers::reqwest` | reqwest 0.13 |
| `db-sqlx` | `gotel::tracers::sqlx` | SQLx 0.8 (PostgreSQL, MySQL, SQLite) |
| `db-rusqlite` | `gotel::tracers::rusqlite` | rusqlite 0.32 |
| `db-duckdb` | `gotel::tracers::duckdb` | official duckdb-rs 1.20000.x (DuckDB 2.0) |
| `redis` | `gotel::tracers::redis` | redis-rs 1.8, sync and async |

```toml
[dependencies]
gotel = { git = "https://github.com/zfogg/gotel", features = ["http-axum", "http-reqwest", "db-sqlx", "redis"] }
```

Keep your native library dependencies at the compatible versions above. The SQLx
and rusqlite versions share a SQLite native dependency and preserve Gotel's Rust
1.88 minimum. Select SQLx's database/runtime/TLS features in your application.
Adapters use the providers on your `Application`, never process-global providers.
Apply one Gotel adapter at each I/O boundary to avoid duplicate observations.

## Axum server and reqwest client

```no_run
# #[cfg(all(feature = "http-axum", feature = "http-reqwest"))]
# fn install(app: &gotel::Application) -> Result<(), gotel::NamingError> {
use axum::{middleware, routing::get, Router};
use gotel::tracers;
let routes: Router = Router::new()
    .route("/users/{id}", get(|| async { "ok" }))
    .layer(middleware::from_fn_with_state(
        tracers::axum::Tracer::new(app)?, tracers::axum::trace,
    ));
let client = tracers::reqwest::Client::new(reqwest::Client::new(), app)?;
# let _ = (routes, client);
# Ok(())
# }
```

Pass a native `reqwest::RequestBuilder` to `client.send(builder).await`, or use
`client.execute(&parent, builder.build()?).await` for explicit parentage. Client
spans end when response headers arrive, returning the original response/body.
Axum spans include the response body's streaming lifetime. Dropped unfinished
bodies record cancellation; body failures record an error without its message.

Clients inject and servers extract W3C `traceparent`/`tracestate`. Axum names use
matched route templates; unmatched routes use `unmatched`. Client span names use
only the bounded HTTP method. Raw paths, queries, credentials, authorization
headers and request/response bodies are not exported. Client 4xx/5xx and server
5xx statuses are errors; server 4xx statuses retain successful execution semantics.

## SQLx

```no_run
# #[cfg(feature = "db-sqlx")]
# async fn read(app: &gotel::Application, pool: &sqlx::SqlitePool) -> Result<(), Box<dyn std::error::Error>> {
use gotel::{opentelemetry::Context, tracers};
let tracer = tracers::sqlx::Tracer::<sqlx::Sqlite>::new(app, "storage.sqlite", "example")?;
let parent = Context::current();
let row: (String,) = sqlx::query_as("SELECT name FROM users WHERE id = ?")
    .bind(42_i64)
    .fetch_one(tracer.wrap(&parent, "FindUser", pool)).await?;
# let _ = row;
# Ok(())
# }
```

The wrapper implements SQLx's native `Executor`. It supports prepared/typed
queries, execute, fetch-one/optional/all, and streaming. Spans last until stream
EOF, driver error or drop; cancellation never changes driver behavior. Use
`&mut *transaction` to execute inside a native transaction. Acquiring connections
and committing/rolling back remain owned by the application. Typed row decoding
performed by SQLx after executor completion remains outside the executor span.

## rusqlite

```no_run
# #[cfg(feature = "db-rusqlite")]
# fn read(app: &gotel::Application) -> Result<(), Box<dyn std::error::Error>> {
use gotel::{opentelemetry::Context, tracers};
let connection = tracers::rusqlite::Connection::new(
    rusqlite::Connection::open_in_memory()?, app, "storage.sqlite", "example",
)?;
let value: i64 = connection.query_row(&Context::current(), "ReadNumber",
    "SELECT ?", [42_i64], |row| row.get(0))?;
# assert_eq!(value, 42);
# Ok(())
# }
```

Use `execute`, `execute_batch` and `query_row`; each forwards native parameter and
callback objects unchanged. Wrap `&*transaction` to observe queries in a native
transaction without taking over its lifecycle. `into_inner` returns the original
connection without closing or committing it.

Both SQL adapters use Sequel analysis with driver-selected dialects, safe
`db.query.text`, fingerprints and canonical `db.query.diagnostics`, including an
empty array for clean input. Bind values and driver messages are never exported.
`with_policy(SqlPolicy { omit_query_text: true, ..Default::default() })` disables
text while retaining safe metadata. The same policy configures input/output,
memory/work budgets and opt-in source maps/bind descriptors. Analysis failures
do not prevent native query execution or replace native errors/results.

## DuckDB

```no_run
# #[cfg(feature = "db-duckdb")]
# fn read(app: &gotel::Application) -> Result<(), Box<dyn std::error::Error>> {
use gotel::{opentelemetry::Context, tracers};
let db = tracers::duckdb::Connection::new(
    duckdb::Connection::open_in_memory()?, app, "storage.duckdb", "analytics",
)?;
let rows: Vec<i64> = db.query_all(&Context::current(), "ReadRange",
    "SELECT i FROM range(?) AS t(i)", [3_i64], |row| row.get(0))?;
# assert_eq!(rows, vec![0, 1, 2]);
# Ok(())
# }
```

Enable `db-duckdb` and use the official `duckdb` crate at version `1.20000.x`.
The driver uses its normal native-library discovery/download configuration;
applications may enable its `bundled` feature to compile DuckDB themselves. A
downloaded/system shared library must remain available at runtime; follow the
driver's deployment instructions. Gotel does not change native library selection.

`execute`, `execute_batch`, `query_row` and `query_all` use the DuckDB dialect,
fingerprints, canonical diagnostics and the same `SqlPolicy` as the other SQL
adapters. `query_all` buffers results and includes native row-decoding errors in
the span. Arrow streams, appenders and calls made directly on the native connection
are not automatically instrumented. Wrap `&*transaction` to observe native
transactional queries; commit and rollback remain application-owned.

## Redis

```no_run
# #[cfg(feature = "redis")]
# async fn read(app: &gotel::Application) -> Result<(), Box<dyn std::error::Error>> {
use redis::AsyncCommands;
let native = redis::Client::open("redis://127.0.0.1/")?
    .get_multiplexed_async_connection().await?;
let mut connection = gotel::tracers::redis::Connection::new(native, app, "cache.redis")?;
let value: Option<String> = connection.get("user:42").await?;
let values: (Option<String>,) = redis::pipe().cmd("GET").arg("user:42")
    .query_async(&mut connection).await?;
# let _ = (value, values);
# Ok(())
# }
```

The same wrapper implements synchronous `ConnectionLike` for `redis::Commands`
and asynchronous `aio::ConnectionLike` for `AsyncCommands`, multiplexed
connections and connection managers. Native commands and pipelines are forwarded
once, preserving offsets/counts, responses and errors. Pipelines produce one
`PIPELINE` observation; fixed known command names are exported and unknown custom
commands use `OTHER`. Commands, scripts, keys, values, responses and error messages
are not exported. `with_parent(context)` selects explicit parentage; otherwise
each call uses its current context. Dropping pending async work records
cancellation, and `into_inner` restores the original native connection.

Durations use seconds. HTTP, SQL and Redis metrics use bounded operation/method
labels, without SQL text, fingerprints, keys, URLs or bind values.
