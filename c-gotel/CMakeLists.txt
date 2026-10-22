cmake_minimum_required(VERSION 3.21)
project(c-gotel VERSION 0.1.0 LANGUAGES C)
include(FetchContent)
option(CGOTEL_BUILD_TESTS "Build c-gotel unit and wire tests" OFF)
option(CGOTEL_WITH_SQLITE "Build SQLite trace adapter" ON)
FetchContent_Declare(otlp_c GIT_REPOSITORY https://github.com/riboseinc/otlp-c.git
  GIT_TAG 0798449d66331c3953b8ba9354164cfbf2f3bdaf)
# Do not let the dependency define a generic docs target in its consumer.
set(_gotel_disable_doxygen "${CMAKE_DISABLE_FIND_PACKAGE_Doxygen}")
set(CMAKE_DISABLE_FIND_PACKAGE_Doxygen TRUE)
FetchContent_MakeAvailable(otlp_c)
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
  if(MSVC AND NOT CMAKE_C_COMPILER_ID MATCHES "Clang")
    target_compile_options(otlp_c PRIVATE /FI${CMAKE_CURRENT_SOURCE_DIR}/src/otlp_windows.h)
  else()
    target_compile_options(otlp_c PRIVATE -include ${CMAKE_CURRENT_SOURCE_DIR}/src/otlp_windows.h)
  endif()
endif()
include(cmake/TLS.cmake)
add_library(c-gotel STATIC src/gotel.c src/http.c)
add_library(gotel::c-gotel ALIAS c-gotel)
target_compile_features(c-gotel PUBLIC c_std_11)
set_target_properties(c-gotel PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_include_directories(c-gotel PRIVATE ${otlp_c_SOURCE_DIR}/src)
target_include_directories(c-gotel PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)
target_link_libraries(c-gotel PUBLIC otlp-c::otlp_c)
if(CGOTEL_WITH_SQLITE)
  find_package(SQLite3 REQUIRED)
  target_sources(c-gotel PRIVATE src/sqlite.c src/named.c)
  if(TARGET SQLite3::SQLite3)
    target_link_libraries(c-gotel PUBLIC SQLite3::SQLite3)
  else()
    target_link_libraries(c-gotel PUBLIC SQLite::SQLite3)
  endif()
  target_compile_definitions(c-gotel PUBLIC CGOTEL_WITH_SQLITE=1)
endif()
option(CGOTEL_BUILD_TEST_SUPPORT "Build framework-independent test helpers" ${CGOTEL_BUILD_TESTS})
if(CGOTEL_BUILD_TEST_SUPPORT)
  add_library(c-gotel-testing STATIC src/testing.c)
  add_library(gotel::testing ALIAS c-gotel-testing)
  target_link_libraries(c-gotel-testing PUBLIC gotel::c-gotel)
  set_target_properties(c-gotel-testing PROPERTIES POSITION_INDEPENDENT_CODE ON)
endif()
if(CGOTEL_BUILD_TESTS)
  if(NOT CGOTEL_BUILD_TEST_SUPPORT)
    message(FATAL_ERROR "CGOTEL_BUILD_TESTS requires CGOTEL_BUILD_TEST_SUPPORT")
  endif()
  find_package(Threads REQUIRED)
  enable_testing()
  add_executable(c-gotel-test tests/test.c)
  target_link_libraries(c-gotel-test PRIVATE gotel::testing Threads::Threads)
  add_test(NAME c-gotel-unit COMMAND c-gotel-test)
  foreach(case http validation sampling context registry concurrency testing)
    add_test(NAME c-gotel-${case} COMMAND c-gotel-test --case ${case})
  endforeach()
  find_package(Python3 REQUIRED COMPONENTS Interpreter)
  add_executable(c-gotel-tls-probe tests/tls_probe.c)
  target_include_directories(c-gotel-tls-probe PRIVATE ${otlp_c_SOURCE_DIR}/src)
  target_link_libraries(c-gotel-tls-probe PRIVATE gotel::c-gotel)
  add_test(NAME c-gotel-tls COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/tests/tls.py $<TARGET_FILE:c-gotel-tls-probe>)
  add_test(NAME c-gotel-python-testing COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/tests/test_testing.py)
  add_test(NAME c-gotel-wire COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/tests/wire.py $<TARGET_FILE:c-gotel-test> ${CGOTEL_WITH_SQLITE})
endif()
