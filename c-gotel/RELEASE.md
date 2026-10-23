# Linking a prebuilt c-gotel archive

This archive contains the Release-build static `c-gotel` and `otlp-c` libraries,
their public headers, and their licenses. The archive name identifies the
operating system and CPU architecture. Use a compatible C toolchain and point
its include path at `include/`.

The archives do **not** combine all third-party dependencies. Link PCRE2 8-bit,
utf8proc, OpenSSL SSL/Crypto, and SQLite3, plus the platform system libraries
required by those packages. The SQLite adapter is enabled in these builds;
define `CGOTEL_WITH_SQLITE=1` when using it. On Windows, the archive is built
with the MSVC-compatible Clang toolchain and vcpkg libraries for the archive's
CPU architecture (`x64-windows` or `arm64-windows`).

For the easiest dependency setup, build the matching Git tag from source with
`add_subdirectory(c-gotel)` and link `gotel::c-gotel`. The included `README.md`
documents that route and the API.
