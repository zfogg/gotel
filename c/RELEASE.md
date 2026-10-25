# Linking prebuilt gotel libraries

Choose the static or shared section to match the release ZIP you downloaded.
Each ZIP is named for its operating system, CPU architecture, and linkage type.
Both contain public headers, licenses, and this guide.

Native gRPC support is enabled by default and included in every release ZIP.
Select it with `options.protocol = "grpc"`; HTTP/protobuf remains the default
export protocol. Static ZIPs include nghttp2 for final linking; shared libraries
link nghttp2 statically and need no separate nghttp2 runtime. Source builds can
disable gRPC support with `GOTEL_WITH_GRPC=OFF`.
Rebuild consumers against the matching headers when changing gotel versions.

## Static libraries

This archive contains the Release-build static `gotel`, `otlp-c`, `nghttp2`, `sequel`, and `sequel_utf8proc` libraries,
their public headers, and their licenses. The archive name identifies the
operating system and CPU architecture. Use a compatible C toolchain and point
its include path at `include/`.

The archives do **not** combine all third-party dependencies. Link PCRE2 8-bit,
utf8proc, OpenSSL SSL/Crypto, and SQLite3. The SQLite adapter is enabled in
these builds; define `GOTEL_WITH_SQLITE=1` when compiling your application.
The system libraries used by this build are:

| Platform | System libraries |
| --- | --- |
| Linux | pthread (`-pthread`); libc is linked by the C toolchain. Static OpenSSL builds can need `dl` and other libraries reported by `pkg-config --static --libs openssl`. |
| macOS | `Security.framework` and `CoreFoundation.framework`; libSystem is linked by the toolchain. |
| Windows | `Shell32.lib`, `Advapi32.lib`, `Crypt32.lib`, `Ws2_32.lib`, and `User32.lib` (the last is required by vcpkg's static OpenSSL build). |

On Windows, use the MSVC-compatible Clang toolchain and matching vcpkg libraries
for the archive's architecture (`x64-windows` or `arm64-windows`). Link the
`gotel` archive before `otlp-c`, then the bundled `nghttp2` and external dependency libraries. The examples
below assume your application source is `example.c`; replace it with your own.

### CMake on Linux, macOS, and Windows

Use this `CMakeLists.txt` on any of the three platforms. Point `GOTEL_ROOT` at
the extracted directory containing `lib/` and `include/`.

```cmake
cmake_minimum_required(VERSION 3.24)
project(gotel_example LANGUAGES C)

set(GOTEL_ROOT "" CACHE PATH "Extracted gotel release directory")
if(NOT GOTEL_ROOT)
  message(FATAL_ERROR "Set GOTEL_ROOT to the extracted gotel directory")
endif()

if(WIN32)
  set(GOTEL_LIB "${GOTEL_ROOT}/lib/gotel.lib")
  set(OTLP_LIB "${GOTEL_ROOT}/lib/otlp_c.lib")
  set(NGHTTP2_LIB "${GOTEL_ROOT}/lib/nghttp2.lib")
  set(SEQUEL_LIB "${GOTEL_ROOT}/lib/sequel.lib")
  set(SEQUEL_UTF8PROC_LIB "${GOTEL_ROOT}/lib/sequel_utf8proc.lib")
else()
  set(GOTEL_LIB "${GOTEL_ROOT}/lib/libgotel.a")
  set(OTLP_LIB "${GOTEL_ROOT}/lib/libotlp_c.a")
  set(NGHTTP2_LIB "${GOTEL_ROOT}/lib/libnghttp2.a")
  set(SEQUEL_LIB "${GOTEL_ROOT}/lib/libsequel.a")
  set(SEQUEL_UTF8PROC_LIB "${GOTEL_ROOT}/lib/libsequel_utf8proc.a")
endif()

add_library(gotel_prebuilt STATIC IMPORTED)
set_target_properties(gotel_prebuilt PROPERTIES IMPORTED_LOCATION "${GOTEL_LIB}")
add_library(otlp_prebuilt STATIC IMPORTED)
set_target_properties(otlp_prebuilt PROPERTIES IMPORTED_LOCATION "${OTLP_LIB}")

find_library(PCRE2_8_LIBRARY NAMES pcre2-8 REQUIRED)
find_library(UTF8PROC_LIBRARY NAMES utf8proc REQUIRED)
find_package(SQLite3 REQUIRED)
find_package(OpenSSL REQUIRED)

add_executable(example example.c)
target_include_directories(example PRIVATE "${GOTEL_ROOT}/include")
target_compile_definitions(example PRIVATE GOTEL_WITH_SQLITE=1)
target_link_libraries(example PRIVATE
  gotel_prebuilt otlp_prebuilt "${NGHTTP2_LIB}" "${SEQUEL_LIB}" "${SEQUEL_UTF8PROC_LIB}"
  "${PCRE2_8_LIBRARY}" "${UTF8PROC_LIBRARY}"
  ${SQLite3_LIBRARIES} OpenSSL::SSL OpenSSL::Crypto
)
if(WIN32)
  target_link_libraries(example PRIVATE shell32 advapi32 crypt32 ws2_32 user32)
elseif(APPLE)
  target_link_libraries(example PRIVATE "-framework Security" "-framework CoreFoundation")
else()
  find_package(Threads REQUIRED)
  target_link_libraries(example PRIVATE Threads::Threads ${CMAKE_DL_LIBS})
endif()
```

On Linux:

```sh
sudo apt-get install clang cmake ninja-build libpcre2-dev libutf8proc-dev libsqlite3-dev libssl-dev
cmake -S . -B build -G Ninja -DCMAKE_C_COMPILER=clang \
  -DGOTEL_ROOT=/path/to/extracted/gotel-directory
cmake --build build
```

On macOS:

```sh
brew install cmake ninja llvm openssl@3 sqlite pcre2 utf8proc
cmake -S . -B build -G Ninja \
  -DCMAKE_C_COMPILER="$(brew --prefix llvm)/bin/clang" \
  -DGOTEL_ROOT=/path/to/extracted/gotel-directory \
  -DOPENSSL_ROOT_DIR="$(brew --prefix openssl@3)" \
  -DCMAKE_PREFIX_PATH="$(brew --prefix sqlite);$(brew --prefix pcre2);$(brew --prefix utf8proc)"
cmake --build build
```

On Windows, run this from PowerShell in a Visual Studio developer environment.
Use `arm64-windows` in place of `x64-windows` for the ARM64 ZIP:

```powershell
vcpkg install openssl:x64-windows sqlite3:x64-windows pcre2:x64-windows utf8proc:x64-windows
cmake -S . -B build -G Ninja `
  -DCMAKE_C_COMPILER=clang-cl `
  -DCMAKE_TOOLCHAIN_FILE="$env:VCPKG_ROOT/scripts/buildsystems/vcpkg.cmake" `
  -DVCPKG_TARGET_TRIPLET=x64-windows `
  -DGOTEL_ROOT="C:/path/to/extracted/gotel-directory"
cmake --build build
```

### GNU Make on Linux

```make
GOTEL_ROOT ?= /path/to/extracted/gotel-directory
CC = clang
CFLAGS += -I$(GOTEL_ROOT)/include -DGOTEL_WITH_SQLITE=1
LDLIBS = $(GOTEL_ROOT)/lib/libgotel.a $(GOTEL_ROOT)/lib/libotlp_c.a \
  $(GOTEL_ROOT)/lib/libnghttp2.a $(GOTEL_ROOT)/lib/libsequel.a $(GOTEL_ROOT)/lib/libsequel_utf8proc.a \
  -lpcre2-8 -lutf8proc -lsqlite3 -lssl -lcrypto -pthread -ldl

example: example.c
	$(CC) $(CFLAGS) $< -o $@ $(LDLIBS)
```

### GNU Make on macOS

```make
GOTEL_ROOT ?= /path/to/extracted/gotel-directory
CC = clang
CFLAGS += -I$(GOTEL_ROOT)/include -DGOTEL_WITH_SQLITE=1
LDFLAGS += -L$(shell brew --prefix openssl@3)/lib \
  -L$(shell brew --prefix sqlite)/lib -L$(shell brew --prefix pcre2)/lib \
  -L$(shell brew --prefix utf8proc)/lib
LDLIBS = $(GOTEL_ROOT)/lib/libgotel.a $(GOTEL_ROOT)/lib/libotlp_c.a \
  $(GOTEL_ROOT)/lib/libnghttp2.a $(GOTEL_ROOT)/lib/libsequel.a $(GOTEL_ROOT)/lib/libsequel_utf8proc.a \
  -lpcre2-8 -lutf8proc -lsqlite3 -lssl -lcrypto \
  -framework Security -framework CoreFoundation

example: example.c
	$(CC) $(CFLAGS) $< -o $@ $(LDFLAGS) $(LDLIBS)
```

### GNU Make on Windows

Run GNU Make from a Visual Studio developer environment with `clang-cl` on
`PATH`. Use vcpkg libraries for the same CPU architecture as the release ZIP.

```make
GOTEL_ROOT ?= C:/path/to/extracted/gotel-directory
VCPKG_ROOT ?= C:/vcpkg
TRIPLET ?= x64-windows
VCPKG_LIB = $(VCPKG_ROOT)/installed/$(TRIPLET)/lib

example.exe: example.c
	clang-cl /nologo /I"$(GOTEL_ROOT)/include" /DGOTEL_WITH_SQLITE=1 $< \
  /link /OUT:$@ /LIBPATH:"$(VCPKG_LIB)" \
  "$(GOTEL_ROOT)/lib/gotel.lib" "$(GOTEL_ROOT)/lib/otlp_c.lib" "$(GOTEL_ROOT)/lib/nghttp2.lib" "$(GOTEL_ROOT)/lib/sequel.lib" "$(GOTEL_ROOT)/lib/sequel_utf8proc.lib" \
  pcre2-8.lib utf8proc.lib sqlite3.lib libssl.lib libcrypto.lib \
  shell32.lib advapi32.lib crypt32.lib ws2_32.lib user32.lib
```

For the easiest dependency setup, build the matching Git tag from source with
`add_subdirectory(gotel)` and link `gotel::gotel`. The included `API.md`
documents that route and the API.

## Shared libraries

This ZIP contains a Release-build shared `gotel` library, its public headers,
and licenses. `otlp-c` is compiled into `gotel`; there is no separate
`otlp-c` runtime library. Match the operating system and CPU architecture in
the ZIP name. The SQLite adapter is enabled; define `GOTEL_WITH_SQLITE=1`
when compiling code that uses its API.

The ZIP does not bundle PCRE2 8-bit, utf8proc, OpenSSL, or SQLite runtime
libraries. Install compatible versions of those dependencies for the same
architecture. Some may be linked into `gotel` by the build toolchain, so
inspect the actual binary before shipping: `ldd lib/libgotel.so` on Linux,
`otool -L lib/libgotel.dylib` on macOS, or
`dumpbin /dependents lib\gotel.dll` on Windows. These commands also show
the exact names your deployment must provide.

| Platform | Library to link | Files needed at run time |
| --- | --- | --- |
| Linux | `libgotel.so` | `libgotel.so.0.8` and its dependency `.so` files, plus libc and pthread from the OS. Keep the included versioned `.so` names together. |
| macOS | `libgotel.dylib` | `libgotel.0.8.dylib` and its dependency `.dylib` files, plus `Security.framework`, `CoreFoundation.framework`, and libSystem from macOS. Keep the included versioned `.dylib` names together. |
| Windows | `gotel.lib` import library | `gotel.dll` and its dependent DLLs, including matching vcpkg PCRE2, utf8proc, SQLite3, and OpenSSL DLLs where dynamically linked. Windows supplies `Shell32.dll`, `Advapi32.dll`, `Crypt32.dll`, `Ws2_32.dll`, `User32.dll`, and the C runtime. |

On Windows, use MSVC-compatible Clang and the matching vcpkg triplet
(`x64-windows` or `arm64-windows`). On Linux and macOS, use a compatible
Clang-based toolchain. The Windows import library is for linking and does not
replace the DLL at run time. Do not copy a DLL or dylib from another CPU
architecture. Distribute dependency libraries according to their own licenses.

### CMake

Use this `CMakeLists.txt` with an `example.c` that calls the public API.
`GOTEL_ROOT` is the extracted directory containing `lib/` and `include/`.

```cmake
cmake_minimum_required(VERSION 3.24)
project(gotel_example LANGUAGES C)

set(GOTEL_ROOT "" CACHE PATH "Extracted gotel release directory")
if(NOT GOTEL_ROOT)
  message(FATAL_ERROR "Set GOTEL_ROOT")
endif()

add_library(gotel_prebuilt SHARED IMPORTED)
if(WIN32)
  set_target_properties(gotel_prebuilt PROPERTIES
    IMPORTED_IMPLIB "${GOTEL_ROOT}/lib/gotel.lib"
    IMPORTED_LOCATION "${GOTEL_ROOT}/lib/gotel.dll")
elseif(APPLE)
  set_target_properties(gotel_prebuilt PROPERTIES
    IMPORTED_LOCATION "${GOTEL_ROOT}/lib/libgotel.dylib")
else()
  set_target_properties(gotel_prebuilt PROPERTIES
    IMPORTED_LOCATION "${GOTEL_ROOT}/lib/libgotel.so")
endif()

add_executable(example example.c)
target_include_directories(example PRIVATE "${GOTEL_ROOT}/include")
target_compile_definitions(example PRIVATE GOTEL_WITH_SQLITE=1)
target_link_libraries(example PRIVATE gotel_prebuilt)
if(WIN32)
  add_custom_command(TARGET example POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_if_different
      "${GOTEL_ROOT}/lib/gotel.dll" $<TARGET_FILE_DIR:example>)
else()
  set_target_properties(example PROPERTIES BUILD_RPATH "${GOTEL_ROOT}/lib")
endif()
```

Install the dependency packages before configuring. For Windows, place their
runtime DLLs beside `example.exe` or on `PATH`; CMake's copy command above
copies only `gotel.dll`. For Linux and macOS, make the dependency shared
libraries available through the platform loader's normal search paths or
configure an appropriate runpath. The archive's versioned shared-library
names must stay in `lib/` alongside the unversioned linker name.

### GNU Make on Unix

This Makefile works on Linux and macOS.

```make
GOTEL_ROOT ?= /path/to/extracted/gotel-directory
CC = clang
CFLAGS += -I$(GOTEL_ROOT)/include -DGOTEL_WITH_SQLITE=1
LDFLAGS += -L$(GOTEL_ROOT)/lib -Wl,-rpath,$(GOTEL_ROOT)/lib
LDLIBS += -lgotel

example: example.c
	$(CC) $(CFLAGS) $< -o $@ $(LDFLAGS) $(LDLIBS)
```

### GNU Make on Windows

Run GNU Make from a Visual Studio developer environment with `clang-cl` on
`PATH`. Install matching vcpkg runtime dependencies and put their DLLs on
`PATH` or beside the executable.

```make
GOTEL_ROOT ?= C:/path/to/extracted/gotel-directory

example.exe: example.c
	clang-cl /nologo /I"$(GOTEL_ROOT)/include" /DGOTEL_WITH_SQLITE=1 $< \
  /link /OUT:$@ "$(GOTEL_ROOT)/lib/gotel.lib"
	copy "$(GOTEL_ROOT)/lib/gotel.dll" .
```

For dependency resolution and platform-specific builds from source, use
`add_subdirectory(gotel)` and link `gotel::gotel`. See `API.md` for the
source-build route and the C API.

SQL processing is provided by the bundled sequel libraries. Shared archives include
the matching sequel shared library; keep it beside gotel or on the runtime loader
path. Public gotel headers include `sequel.h`, shipped in every archive.
