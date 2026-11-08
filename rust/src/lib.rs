//! Shared OpenTelemetry conventions for Rust applications.
//!
//! The application owns its providers; no global provider is replaced. Database
//! spans use Sequel's native analysis without executing or retaining original SQL.
#![doc = include_str!("../README.md")]
#![warn(missing_docs)]
#![doc(html_root_url = "https://zfogg.github.io/gotel/rust")]

mod application;
mod naming;
mod operation;
mod outcome;
mod sql;
pub mod tracers;

pub use application::*;
pub use naming::*;
/// OpenTelemetry API types used by Gotel's public interface.
pub use opentelemetry;
pub use operation::*;
pub use outcome::*;
/// The matching native SQL API, re-exported to keep analysis types aligned.
pub use sequel;
pub use sql::*;

#[cfg(test)]
mod tests;

#[cfg(all(test, feature = "otlp"))]
mod otlp_tests;
