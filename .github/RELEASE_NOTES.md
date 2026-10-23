## C static libraries

The six C ZIPs contain Release-build static `c-gotel` and `otlp-c` libraries,
their public headers, and their licenses for x64 and ARM64 on Linux, macOS,
and Windows. Each archive name identifies its operating system and CPU
architecture. Use a compatible C toolchain and point its include path at
`include/`.

These archives do **not** combine all third-party dependencies. Link PCRE2
8-bit, utf8proc, OpenSSL SSL/Crypto, and SQLite3. The SQLite adapter is enabled;
define `CGOTEL_WITH_SQLITE=1` when compiling your application. Linux may need
`-pthread` and, with static OpenSSL, `-ldl` or other libraries reported by
`pkg-config --static --libs openssl`. macOS needs `Security.framework` and
`CoreFoundation.framework`. Windows needs `Shell32.lib`, `Advapi32.lib`,
`Crypt32.lib`, `Ws2_32.lib`, and `User32.lib` with the vcpkg OpenSSL build.
On Windows, use the MSVC-compatible Clang toolchain and vcpkg libraries for
the archive's architecture (`x64-windows` or `arm64-windows`).

Each C ZIP has a `README.md` with CMake and GNU Make examples for all three
operating systems, plus `API.md` for the source-build route and C API. To have
CMake resolve the dependencies for you, build the matching tag from source
with `add_subdirectory(c-gotel)` and link `gotel::c-gotel`.

## TypeScript

`gotel-typescript-*.zip` is the single compiled `@zfogg/gotel` package asset.
Extract it, then from your browser application install the extracted directory
containing `package.json`, for example
`npm install /absolute/path/to/gotel-typescript-vX.Y.Z` or the corresponding
`pnpm add` command. npm does not install the ZIP directly. The archive's
`README.md` gives a browser setup example and lists the peer-dependency
requirements; `PROJECT.md` is the full project guide. The ZIP is not the full
Go/C/TypeScript source tree.

## Go

The version tag is the Go module release. Use
`go get github.com/zfogg/gotel@vX.Y.Z` with this release's version. No
compiled Go archive is published.
