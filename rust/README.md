# Gotel for Rust

[API documentation](https://zfogg.github.io/gotel/rust/)

Install from this repository (the Rust crate is in `rust/`):

```toml
[dependencies]
gotel = { git = "https://github.com/zfogg/gotel" }
```

Commit your Cargo.lock to pin the exact revision. Historical Gotel releases before
the Rust bindings were added do not contain this crate. Future release archives
include `gotel-rust-vVERSION.zip`; extract it and use a Cargo path dependency.

The crate uses the native OpenTelemetry Rust SDK, rather than the Gotel C library.
Sequel compiles its bundled C engine using a C compiler. You can instead link a
matching prebuilt Sequel SDK with `SEQUEL_LIB_DIR` and `SEQUEL_LINK_KIND`, as
described in [Sequel's Rust installation guide](https://github.com/zfogg/sequel/blob/main/rust/README.md).

```rust
# fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
# #[cfg(feature = "otlp")]
# {
use gotel::{Application, ApplicationConfig, Query};
use gotel::opentelemetry::Context;
use gotel::sequel::Dialect;

let app = Application::otlp(ApplicationConfig::new("example"))?;
let database = app.database("storage.postgres", Dialect::PostgreSql, "example")?;
let result = database.query(
    &Context::new(),
    Query::new("ReadWidget", "SELECT * FROM widgets WHERE id = $1")
        .with_bind_count(1),
    || Ok::<_, std::io::Error>(42), // Execute your real driver call here.
)?;
assert_eq!(result, 42);
app.shutdown()?;
# }
# Ok(())
# }
```

`Application::otlp` owns trace, metric and log providers and honors standard OTEL
configuration. It never installs global providers. Initialize and shut down its
blocking HTTP client outside an async runtime, or in a blocking task. Disable
default features to supply your own providers through `Application::with_providers`.

Query helpers return driver results and errors unchanged. SQL literals are
sanitized before export; malformed input omits query text and includes canonical
`db.query.diagnostics`. Diagnostics are always a string array, including an empty
array. Bind counts never include values. Optional source maps and bind descriptors
use bounded JSON attributes; native analysis budgets remain configurable.

For async drivers use `query_async`: context is attached only while polling, and
dropping unfinished work records cancellation. Explicit contexts preserve causal
parentage across concurrent tasks. `Operation` records success, error,
cancellation or deferred outcomes and ends exactly once. Error and panic messages
are never recorded automatically.

Use application-owned route templates and operation identifiers. HTTP client name
helpers remove URL credentials, query strings and fragments, and normalize common
numeric/UUID identifiers; arbitrary path segments must already be safe to export.
