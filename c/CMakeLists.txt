cmake_minimum_required(VERSION 3.24)
option(GOTEL_PREFER_CLANG "Prefer clang/clang++ when no compiler was selected" ON)
if(DEFINED CMAKE_C_COMPILER AND NOT DEFINED CMAKE_CXX_COMPILER AND "$ENV{CXX}" STREQUAL "")
  get_filename_component(_gotel_c_compiler_name "${CMAKE_C_COMPILER}" NAME_WE)
  if(_gotel_c_compiler_name STREQUAL "clang-cl")
    set(CMAKE_CXX_COMPILER "${CMAKE_C_COMPILER}" CACHE FILEPATH "C++ compiler")
  endif()
endif()
if(GOTEL_PREFER_CLANG AND NOT DEFINED CMAKE_C_COMPILER AND NOT CMAKE_GENERATOR MATCHES "Visual Studio")
  find_program(GOTEL_CLANG clang)
  find_program(GOTEL_CLANGXX clang++)
  if(GOTEL_CLANG AND GOTEL_CLANGXX)
    set(CMAKE_C_COMPILER "${GOTEL_CLANG}" CACHE FILEPATH "C compiler")
    set(CMAKE_CXX_COMPILER "${GOTEL_CLANGXX}" CACHE FILEPATH "C++ compiler")
  endif()
endif()
project(gotel VERSION 0.13.0 LANGUAGES C CXX)
option(GOTEL_BUILD_DOCS "Enable API documentation and publication targets" OFF)
if(GOTEL_BUILD_DOCS)
  include(../docs/targets.cmake)
endif()

include(FetchContent)
option(BUILD_SHARED_LIBS "Build shared libraries" OFF)
option(GOTEL_BUILD_TESTS "Build gotel unit and wire tests" OFF)
set(GOTEL_CXX_STANDARD 27 CACHE STRING "C++ standard for the facade; use 26 with current compilers")
option(GOTEL_WITH_SQLITE "Build SQLite trace adapter" ON)
set(GOTEL_SQLITE_TARGET "" CACHE STRING "Existing SQLite3 CMake target")

set(UTF8PROC_ENABLE_TESTING OFF CACHE BOOL "Disable utf8proc tests" FORCE)
set(UTF8PROC_INSTALL OFF CACHE BOOL "Disable utf8proc installation" FORCE)
add_subdirectory(vendor/utf8proc EXCLUDE_FROM_ALL)
set_target_properties(utf8proc PROPERTIES POSITION_INDEPENDENT_CODE ON)

include(cmake/TextDependencies.cmake)

# SQL processing is owned by sequel; allow an existing target or FetchContent override.
if(NOT TARGET sequel::sequel)
  FetchContent_Declare(sequel
    GIT_REPOSITORY https://github.com/zfogg/sequel.git
    GIT_TAG f0865335e0c90b872ad2d4b40d2590509439f1fe)
  FetchContent_MakeAvailable(sequel)
endif()

# gotel uses otlp-c's internal attribute and TLS helpers. Keep otlp-c in
# the same link image even when gotel itself is a shared library.
set(_gotel_shared ${BUILD_SHARED_LIBS})
set(BUILD_SHARED_LIBS OFF)
FetchContent_Declare(otlp_c
  GIT_REPOSITORY https://github.com/riboseinc/otlp-c.git
  GIT_TAG 0798449d66331c3953b8ba9354164cfbf2f3bdaf
)

# Do not let the dependency define a generic docs target in its consumer.
set(_gotel_disable_doxygen "${CMAKE_DISABLE_FIND_PACKAGE_Doxygen}")
set(CMAKE_DISABLE_FIND_PACKAGE_Doxygen TRUE)
FetchContent_MakeAvailable(otlp_c)
set_target_properties(otlp_c PROPERTIES POSITION_INDEPENDENT_CODE ON)
set(BUILD_SHARED_LIBS ${_gotel_shared})
set(CMAKE_DISABLE_FIND_PACKAGE_Doxygen "${_gotel_disable_doxygen}")

# otlp-c v1.1.19 calls Sleep in exporter translation units without
# including its declaration. Supply the Windows headers for that pinned version.
if(WIN32)
  # SO_ERROR can be zero while a nonblocking Winsock connect is still pending.
  # Wait for writability before considering it connected (Docker port forwarding
  # reliably exposes the race). Keep the pinned upstream source cache untouched.
  file(READ "${otlp_c_SOURCE_DIR}/src/platform_win.c" _gotel_platform)
  set(_gotel_connect_anchor "if (!s->connecting)\n\t\treturn OTLP_OK;")
  string(FIND "${_gotel_platform}" "${_gotel_connect_anchor}" _gotel_connect_position)
  if(_gotel_connect_position EQUAL -1)
    message(FATAL_ERROR "Pinned otlp-c Windows connect implementation changed")
  endif()
  string(REPLACE "${_gotel_connect_anchor}" "${_gotel_connect_anchor}
    fd_set writable, failed;
    FD_ZERO(&writable); FD_ZERO(&failed);
    FD_SET(s->s, &writable); FD_SET(s->s, &failed);
    struct timeval wait = {0, 0};
    int ready = select(0, NULL, &writable, &failed, &wait);
    if (ready == SOCKET_ERROR) return OTLP_ERR_CONNECT;
    if (!ready) return OTLP_ERR_WOULDBLOCK;
" _gotel_platform "${_gotel_platform}")
  file(WRITE "${CMAKE_CURRENT_BINARY_DIR}/otlp_platform_win.c" "${_gotel_platform}")
  get_target_property(_gotel_otlp_sources otlp_c SOURCES)
  list(REMOVE_ITEM _gotel_otlp_sources src/platform_win.c)
  set_property(TARGET otlp_c PROPERTY SOURCES "${_gotel_otlp_sources}")
  target_sources(otlp_c PRIVATE "${CMAKE_CURRENT_BINARY_DIR}/otlp_platform_win.c")
  if(MSVC)
    target_compile_options(otlp_c PRIVATE /FI${CMAKE_CURRENT_SOURCE_DIR}/src/otlp_windows.h)
  else()
    target_compile_options(otlp_c PRIVATE -include ${CMAKE_CURRENT_SOURCE_DIR}/src/otlp_windows.h)
  endif()
endif()

include(cmake/TLS.cmake)
include(cmake/GRPC.cmake)
add_subdirectory(src)

option(GOTEL_BUILD_TEST_SUPPORT "Build framework-independent test helpers" ${GOTEL_BUILD_TESTS})
if(GOTEL_BUILD_TESTS)
  if(NOT GOTEL_BUILD_TEST_SUPPORT)
    message(FATAL_ERROR "GOTEL_BUILD_TESTS requires GOTEL_BUILD_TEST_SUPPORT")
  endif()
  enable_testing()
endif()
if(GOTEL_BUILD_TEST_SUPPORT)
  add_subdirectory(tests)
endif()
