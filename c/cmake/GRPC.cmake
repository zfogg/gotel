option(GOTEL_WITH_GRPC "Enable native OTLP/gRPC (nghttp2)" ON)
set(GOTEL_NGHTTP2_TARGET "" CACHE STRING "Existing nghttp2 CMake target")
if(GOTEL_WITH_GRPC)
  if(NOT GOTEL_NGHTTP2_TARGET)
    # gotel only links nghttp2's static library for OTLP/gRPC.  Do not
    # configure nghttp2's optional applications, HPACK tools, examples, or
    # HTTP/3 support: those are the source of slow, irrelevant dependency
    # probes for ngtcp2/nghttp3, systemd, Jansson, libevent, libxml2, and
    # jemalloc.
    set(_gotel_nghttp2_disable_packages
      Libngtcp2
      Libnghttp3
      Systemd
      Jansson
      Libevent
      LibXml2
      Jemalloc
      # nghttp2 itself is TLS-agnostic; gotel's TLS.cmake already links the
      # parent project's OpenSSL::SSL and OpenSSL::Crypto targets directly.
      OpenSSL
    )
    set(_gotel_nghttp2_saved_disable_packages)
    foreach(_package IN LISTS _gotel_nghttp2_disable_packages)
      set(_gotel_nghttp2_saved_${_package}_disable "${CMAKE_DISABLE_FIND_PACKAGE_${_package}}")
      set(CMAKE_DISABLE_FIND_PACKAGE_${_package} TRUE)
    endforeach()

    set(_gotel_nghttp2_saved_enable_http3 "${ENABLE_HTTP3}")
    set(_gotel_nghttp2_saved_enable_app "${ENABLE_APP}")
    set(_gotel_nghttp2_saved_enable_hpack_tools "${ENABLE_HPACK_TOOLS}")
    set(_gotel_nghttp2_saved_enable_examples "${ENABLE_EXAMPLES}")
    set(ENABLE_HTTP3 OFF)
    set(ENABLE_APP OFF)
    set(ENABLE_HPACK_TOOLS OFF)
    set(ENABLE_EXAMPLES OFF)

    function(_gotel_fetch_nghttp2)
      set(ENABLE_LIB_ONLY ON)
      set(BUILD_STATIC_LIBS ON)
      set(BUILD_SHARED_LIBS OFF)
      set(BUILD_TESTING OFF)
      FetchContent_Declare(gotel_nghttp2
        GIT_REPOSITORY https://github.com/nghttp2/nghttp2.git
        GIT_TAG 43b29f991403b5419dd0bea2840581078ead9bf8)
      FetchContent_MakeAvailable(gotel_nghttp2)
    endfunction()
    _gotel_fetch_nghttp2()

    foreach(_package IN LISTS _gotel_nghttp2_disable_packages)
      set(CMAKE_DISABLE_FIND_PACKAGE_${_package} "${_gotel_nghttp2_saved_${_package}_disable}")
    endforeach()
    set(ENABLE_HTTP3 "${_gotel_nghttp2_saved_enable_http3}")
    set(ENABLE_APP "${_gotel_nghttp2_saved_enable_app}")
    set(ENABLE_HPACK_TOOLS "${_gotel_nghttp2_saved_enable_hpack_tools}")
    set(ENABLE_EXAMPLES "${_gotel_nghttp2_saved_enable_examples}")

    set_target_properties(nghttp2_static PROPERTIES POSITION_INDEPENDENT_CODE ON)
    set(GOTEL_NGHTTP2_TARGET nghttp2_static)
  endif()
  if(NOT TARGET "${GOTEL_NGHTTP2_TARGET}")
    message(FATAL_ERROR "GOTEL_NGHTTP2_TARGET must name an existing CMake target")
  endif()
  # Keep the original HTTP implementation behind a common request adapter.
  # TLS.cmake has already staged and hash-checked the pinned source/header.
  file(READ "${_stage}/http_client.c" _http1)
  string(REPLACE "otlp_http_request" "gotel_http1_request" _http1 "${_http1}")
  string(REPLACE "\"http_client.h\"" "\"http1_client.h\"" _http1 "${_http1}")
  file(WRITE "${_stage}/http_client.c" "${_http1}")
  file(READ "${_stage}/http_client.h" _api)
  string(FIND "${_api}" "/* Start a POST." _start)
  string(SUBSTRING "${_api}" ${_start} -1 _api)
  string(REPLACE "otlp_http_request" "gotel_http1_request" _api "${_api}")
  file(WRITE "${_stage}/http1_client.h"
    "#ifndef GOTEL_HTTP1_CLIENT_H\n#define GOTEL_HTTP1_CLIENT_H\n#include \"http_client.h\"\ntypedef struct gotel_http1_request gotel_http1_request_t;\n${_api}")
  file(READ "${_stage}/http_client.h" _api)
  string(REPLACE "int tls;" "int tls;\n int grpc;" _api "${_api}")
  file(WRITE "${_stage}/http_client.h" "${_api}")
  # Validate the pristine pinned source, not the staged copy. The staged file
  # is intentionally rewritten by this adapter and therefore is not
  # idempotent across repeated CMake configure runs.
  file(READ "${otlp_c_SOURCE_DIR}/src/env_config.c" _env)
  # Git may check out CRLF on Windows. Compare the pinned LF content on every host.
  string(REPLACE "\r\n" "\n" _env "${_env}")
  string(SHA256 _env_hash "${_env}")
  if(NOT _env_hash STREQUAL "dc8de0df1b5535d27c3854d7bd838bf80c64b1b0b7c9131212ec379214443db0")
    message(FATAL_ERROR "Pinned otlp-c environment implementation changed; review the gRPC adapter")
  endif()
  string(REPLACE "strcmp(value, \"http/protobuf\") != 0"
    "strcmp(value, \"http/protobuf\") != 0 && strcmp(value, \"grpc\") != 0" _env "${_env}")
  file(WRITE "${_stage}/env_config.c" "${_env}")
  target_sources(otlp_c PRIVATE "${CMAKE_CURRENT_SOURCE_DIR}/src/grpc.c")
  target_include_directories(otlp_c BEFORE PRIVATE "${_stage}")
  target_link_libraries(otlp_c PRIVATE ${GOTEL_NGHTTP2_TARGET})
  target_compile_definitions(otlp_c PRIVATE GOTEL_WITH_GRPC=1)
endif()
