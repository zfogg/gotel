## SQL processing moved to sequel in v0.12.0

Import [sequel](https://github.com/zfogg/sequel) directly for SQL formatting,
sanitization, raw inspection and summaries. The C API uses `<sequel.h>` and
`sequel_*` names; Go imports `github.com/zfogg/sequel/go`; Python imports `sequel`.
Gotel's tracing adapters use those bindings internally. Standalone processing
wrappers were removed from gotel, and its Python package is now pure Python.

The sequel CLI exposes `format` and `sanitize`, accepting stdin, SQL strings or
multiple paths. Both support `--embedded` and `--edit-file`; sanitization can
remove comments, replace literals and bind markers, or do both.

## C libraries

The 12 C ZIPs cover static and shared `gotel` builds for x64 and ARM64 on
Linux, macOS, and Windows. Each ZIP contains public headers, licenses, and a
`README.md` specific to its linkage type. Archive names identify the OS, CPU,
and `static` or `shared` linkage. Use a compatible C toolchain and point its
include path at `include/`.

Native OTLP/gRPC support is included in all C releases. Select it with
`options.protocol = "grpc"`; HTTP/protobuf remains the default export protocol.
Static ZIPs contain `gotel`, `otlp-c`, `nghttp2`, `sequel`, and `sequel_utf8proc` archives. Shared ZIPs contain the
`gotel` `.so`/`.dylib` and its versioned names, or on Windows `gotel.dll`
and its `.lib` import library. Shared archives also include the matching sequel library. `otlp-c` and nghttp2 are linked into the shared library, so neither needs a
separate runtime library. The nghttp2 license is included in both ZIP types.
The shared ZIPs do not bundle third-party runtime libraries: install matching
PCRE2, utf8proc, OpenSSL, and SQLite libraries and make their `.so`, `.dylib`,
or `.dll` files available to the system loader. On Windows, place the DLLs
beside the application or on `PATH`; on Linux and macOS, configure the normal
loader paths or runpath. The included shared `README.md` gives CMake and Make
examples and commands to inspect exact binary dependencies.

The static archives do **not** combine all third-party dependencies. Link PCRE2
8-bit, utf8proc, OpenSSL SSL/Crypto, and SQLite3. The SQLite adapter is enabled;
define `GOTEL_WITH_SQLITE=1` when compiling your application. Linux may need
`-pthread` and, with static OpenSSL, `-ldl` or other libraries reported by
`pkg-config --static --libs openssl`. macOS needs `Security.framework` and
`CoreFoundation.framework`. Windows needs `Shell32.lib`, `Advapi32.lib`,
`Crypt32.lib`, `Ws2_32.lib`, and `User32.lib` with the vcpkg OpenSSL build.
On Windows, use the MSVC-compatible Clang toolchain and vcpkg libraries for
the archive's architecture (`x64-windows` or `arm64-windows`).

Each C ZIP has a `README.md` with CMake and GNU Make examples for all three
operating systems, plus `API.md` for the source-build route and C API. To have
CMake resolve the dependencies for you, build the matching tag from source
with `add_subdirectory(gotel/c)` and link `gotel::gotel`.

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
`go get github.com/zfogg/gotel/go@vX.Y.Z` with this release's version. No
compiled Go archive is published.

Python assets contain the pure Python `gotel` wheel and source distribution.
Install with uv; the pinned sequel dependency builds its C extension when a
matching native wheel is unavailable. GitHub publication does not publish to
PyPI or npm. Go consumers use matching immutable `go/vX.Y.Z` tags.



## Consumer updates

Update all known gotel consumers after publishing a release:

| Repository | Integration | Dependency files |
| --- | --- | --- |
| ascii-chat | C | `deps/ascii-chat-deps/gotel` submodule |
| spaniel | Go | `go.mod`, `go.sum` |
| ratio | Go and TypeScript | `go.mod`, `go.sum`, `web/package.json` and its lockfile |
| stonkpile | Go and TypeScript | `go.mod`, `go.sum`, `web/package.json` and its lockfile |
| beanaries | Python and TypeScript | `apps/backend/pyproject.toml`, `apps/backend/uv.lock`, `apps/web/package.json`, `pnpm-lock.yaml` |

For Beanaries, update both the uv Git source (`python/` subdirectory) and the
TypeScript Git dependency to the published release commit, regenerate both
lockfiles, and verify browser → API → Keycloak and Temporal worker traces.
The [gotel-release skill](https://github.com/zfogg/gotel/blob/main/.agents/skills/gotel-release/SKILL.md)
describes the coordinated rollout and validation steps.
