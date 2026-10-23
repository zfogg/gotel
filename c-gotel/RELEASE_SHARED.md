# Linking a prebuilt shared c-gotel library

This ZIP contains a Release-build shared `c-gotel` library, its public headers,
and licenses. `otlp-c` is compiled into `c-gotel`; there is no separate
`otlp-c` runtime library. Match the operating system and CPU architecture in
the ZIP name. The SQLite adapter is enabled; define `CGOTEL_WITH_SQLITE=1`
when compiling code that uses its API.

The ZIP does not bundle PCRE2 8-bit, utf8proc, OpenSSL, or SQLite runtime
libraries. Install compatible versions of those dependencies for the same
architecture. Some may be linked into `c-gotel` by the build toolchain, so
inspect the actual binary before shipping: `ldd lib/libc-gotel.so` on Linux,
`otool -L lib/libc-gotel.dylib` on macOS, or
`dumpbin /dependents lib\c-gotel.dll` on Windows. These commands also show
the exact names your deployment must provide.

| Platform | Library to link | Files needed at run time |
| --- | --- | --- |
| Linux | `libc-gotel.so` | `libc-gotel.so.0.8` and its dependency `.so` files, plus libc and pthread from the OS. Keep the included versioned `.so` names together. |
| macOS | `libc-gotel.dylib` | `libc-gotel.0.8.dylib` and its dependency `.dylib` files, plus `Security.framework`, `CoreFoundation.framework`, and libSystem from macOS. Keep the included versioned `.dylib` names together. |
| Windows | `c-gotel.lib` import library | `c-gotel.dll` and its dependent DLLs, including matching vcpkg PCRE2, utf8proc, SQLite3, and OpenSSL DLLs where dynamically linked. Windows supplies `Shell32.dll`, `Advapi32.dll`, `Crypt32.dll`, `Ws2_32.dll`, `User32.dll`, and the C runtime. |

On Windows, use MSVC-compatible Clang and the matching vcpkg triplet
(`x64-windows` or `arm64-windows`). On Linux and macOS, use a compatible
Clang-based toolchain. The Windows import library is for linking and does not
replace the DLL at run time. Do not copy a DLL or dylib from another CPU
architecture. Distribute dependency libraries according to their own licenses.

## CMake

Use this `CMakeLists.txt` with an `example.c` that calls the public API.
`CGOTEL_ROOT` is the extracted directory containing `lib/` and `include/`.

```cmake
cmake_minimum_required(VERSION 3.24)
project(gotel_example LANGUAGES C)

set(CGOTEL_ROOT "" CACHE PATH "Extracted c-gotel release directory")
if(NOT CGOTEL_ROOT)
  message(FATAL_ERROR "Set CGOTEL_ROOT")
endif()

add_library(gotel_prebuilt SHARED IMPORTED)
if(WIN32)
  set_target_properties(gotel_prebuilt PROPERTIES
    IMPORTED_IMPLIB "${CGOTEL_ROOT}/lib/c-gotel.lib"
    IMPORTED_LOCATION "${CGOTEL_ROOT}/lib/c-gotel.dll")
elseif(APPLE)
  set_target_properties(gotel_prebuilt PROPERTIES
    IMPORTED_LOCATION "${CGOTEL_ROOT}/lib/libc-gotel.dylib")
else()
  set_target_properties(gotel_prebuilt PROPERTIES
    IMPORTED_LOCATION "${CGOTEL_ROOT}/lib/libc-gotel.so")
endif()

add_executable(example example.c)
target_include_directories(example PRIVATE "${CGOTEL_ROOT}/include")
target_compile_definitions(example PRIVATE CGOTEL_WITH_SQLITE=1)
target_link_libraries(example PRIVATE gotel_prebuilt)
if(WIN32)
  add_custom_command(TARGET example POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_if_different
      "${CGOTEL_ROOT}/lib/c-gotel.dll" $<TARGET_FILE_DIR:example>)
else()
  set_target_properties(example PROPERTIES BUILD_RPATH "${CGOTEL_ROOT}/lib")
endif()
```

Install the dependency packages before configuring. For Windows, place their
runtime DLLs beside `example.exe` or on `PATH`; CMake's copy command above
copies only `c-gotel.dll`. For Linux and macOS, make the dependency shared
libraries available through the platform loader's normal search paths or
configure an appropriate runpath. The archive's versioned shared-library
names must stay in `lib/` alongside the unversioned linker name.

## GNU Make on Linux

```make
CGOTEL_ROOT ?= /path/to/extracted/c-gotel-directory
CC = clang
CFLAGS += -I$(CGOTEL_ROOT)/include -DCGOTEL_WITH_SQLITE=1
LDFLAGS += -L$(CGOTEL_ROOT)/lib -Wl,-rpath,$(CGOTEL_ROOT)/lib
LDLIBS += -lc-gotel

example: example.c
	$(CC) $(CFLAGS) $< -o $@ $(LDFLAGS) $(LDLIBS)
```

## GNU Make on macOS

```make
CGOTEL_ROOT ?= /path/to/extracted/c-gotel-directory
CC = clang
CFLAGS += -I$(CGOTEL_ROOT)/include -DCGOTEL_WITH_SQLITE=1
LDFLAGS += -L$(CGOTEL_ROOT)/lib -Wl,-rpath,$(CGOTEL_ROOT)/lib
LDLIBS += -lc-gotel

example: example.c
	$(CC) $(CFLAGS) $< -o $@ $(LDFLAGS) $(LDLIBS)
```

## GNU Make on Windows

Run GNU Make from a Visual Studio developer environment with `clang-cl` on
`PATH`. Install matching vcpkg runtime dependencies and put their DLLs on
`PATH` or beside the executable.

```make
CGOTEL_ROOT ?= C:/path/to/extracted/c-gotel-directory

example.exe: example.c
	clang-cl /nologo /I"$(CGOTEL_ROOT)/include" /DCGOTEL_WITH_SQLITE=1 $< \
  /link /OUT:$@ "$(CGOTEL_ROOT)/lib/c-gotel.lib"
	copy "$(CGOTEL_ROOT)/lib/c-gotel.dll" .
```

For dependency resolution and platform-specific builds from source, use
`add_subdirectory(c-gotel)` and link `gotel::c-gotel`. See `API.md` for the
source-build route and the C API.
