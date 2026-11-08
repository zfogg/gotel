//! Optional adapters for native HTTP, SQL and Redis libraries.
//!
//! Enable only the Cargo features required by your application. Providers belong
//! to the supplied [`crate::Application`]; adapters never replace globals.
#![doc = include_str!("../../ADAPTERS.md")]
#[cfg(any(feature = "db-sqlx", feature = "db-rusqlite"))]
mod policy;
#[cfg(any(feature = "db-sqlx", feature = "db-rusqlite"))]
pub use policy::SqlPolicy;
#[cfg(feature = "http-axum")]
pub mod axum;
#[cfg(feature = "redis")]
pub mod redis;
#[cfg(all(test, feature = "redis"))]
mod redis_tests;
#[cfg(feature = "http-reqwest")]
pub mod reqwest;
#[cfg(feature = "db-rusqlite")]
pub mod rusqlite;
#[cfg(feature = "db-sqlx")]
pub mod sqlx;
#[cfg(all(
    test,
    any(
        feature = "http-axum",
        feature = "http-reqwest",
        feature = "db-rusqlite",
        feature = "db-sqlx",
        feature = "redis"
    )
))]
mod tests;

#[cfg(any(feature = "http-axum", feature = "http-reqwest", feature = "redis"))]
mod observer;

#[cfg(any(feature = "http-axum", feature = "http-reqwest"))]
fn method(method: &str) -> &'static str {
    match method {
        "GET" => "GET",
        "POST" => "POST",
        "PUT" => "PUT",
        "PATCH" => "PATCH",
        "DELETE" => "DELETE",
        "HEAD" => "HEAD",
        "OPTIONS" => "OPTIONS",
        "CONNECT" => "CONNECT",
        "TRACE" => "TRACE",
        _ => "OTHER",
    }
}
