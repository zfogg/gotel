# Development conventions

- Use `#pragma once` for project headers.
- Separate function declarations and their documentation blocks with blank lines.
- Format C sources and headers with `clang-format`, and Go sources with `gofmt`.
- Use `uv` for Python tooling; run Ruff and ty for Python changes.
- Format Rust with `cargo fmt`; run Rust tests and Clippy with warnings denied.
