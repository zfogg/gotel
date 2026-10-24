add_library(gotel-testing STATIC ${PROJECT_SOURCE_DIR}/src/testing.c)
add_library(gotel::testing ALIAS gotel-testing)
target_link_libraries(gotel-testing PUBLIC gotel::gotel)
set_target_properties(gotel-testing PROPERTIES POSITION_INDEPENDENT_CODE ON)

if(GOTEL_BUILD_TESTS)
  find_package(Threads REQUIRED)
  find_package(Python3 REQUIRED COMPONENTS Interpreter)
  if(GOTEL_WITH_GRPC)
    execute_process(COMMAND ${Python3_EXECUTABLE} -c "import grpc, h2"
      RESULT_VARIABLE _grpc_python_status ERROR_QUIET)
    if(NOT _grpc_python_status EQUAL 0)
      message(FATAL_ERROR "gRPC tests require: python -m pip install -r c/tests/requirements-grpc.txt")
    endif()
    add_executable(gotel-grpc-probe grpc_probe.c)
    target_include_directories(gotel-grpc-probe PRIVATE ${otlp_c_SOURCE_DIR}/src)
    target_link_libraries(gotel-grpc-probe PRIVATE gotel::gotel)
    add_test(NAME gotel-grpc COMMAND ${Python3_EXECUTABLE}
      ${CMAKE_CURRENT_SOURCE_DIR}/grpc_test.py $<TARGET_FILE:gotel-grpc-probe>)
  endif()
  add_executable(gotel-snapshot-test snapshot_test.c)
  target_link_libraries(gotel-snapshot-test PRIVATE gotel::testing)
  add_test(NAME gotel-watchdog-snapshot COMMAND gotel-snapshot-test)

  add_executable(gotel-env-test env_test.c)
  if(GOTEL_WITH_GRPC)
    target_compile_definitions(gotel-env-test PRIVATE GOTEL_WITH_GRPC=1)
  endif()
  target_link_libraries(gotel-env-test PRIVATE gotel::gotel)
  target_include_directories(gotel-env-test PRIVATE
    ${PROJECT_SOURCE_DIR}/src ${PROJECT_BINARY_DIR}/otlp-tls ${otlp_c_SOURCE_DIR}/src)
  foreach(case defaults endpoints identity transport disabled validation headers grpc)
    add_test(NAME gotel-env-${case} COMMAND gotel-env-test ${case})
  endforeach()

  add_executable(gotel-test test.c)
  target_link_libraries(gotel-test PRIVATE gotel::testing Threads::Threads)
  target_compile_definitions(gotel-test PRIVATE
    GOTEL_HTTP_FIXTURES="${CMAKE_CURRENT_SOURCE_DIR}/../../testdata/http_names.tsv")
  add_test(NAME gotel-unit COMMAND gotel-test)
  foreach(case http validation sampling context registry concurrency testing)
    add_test(NAME gotel-${case} COMMAND gotel-test --case ${case})
  endforeach()

  add_executable(gotel-sql-probe sql_probe.c)
  target_link_libraries(gotel-sql-probe PRIVATE gotel::gotel)
  add_test(NAME gotel-sql COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/sql_test.py $<TARGET_FILE:gotel-sql-probe>)
  add_test(NAME gotel-sanitize COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/sql_sanitize_test.py $<TARGET_FILE:gotel-sql-probe>)
  add_test(NAME gotel-format COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/sql_format_test.py $<TARGET_FILE:gotel-sql-probe>)
  add_executable(gotel-format-api-test sql_format_api_test.c)
  target_link_libraries(gotel-format-api-test PRIVATE gotel::gotel)
  add_test(NAME gotel-format-api COMMAND gotel-format-api-test)

  add_executable(gotel-resource-probe resource_probe.c)
  target_link_libraries(gotel-resource-probe PRIVATE gotel::testing)
  add_test(NAME gotel-resource COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/resource_test.py $<TARGET_FILE:gotel-resource-probe>)

  if(GOTEL_WITH_SQLITE)
    add_executable(gotel-sqlite-probe sqlite_probe.c)
    target_link_libraries(gotel-sqlite-probe PRIVATE gotel::testing)
    add_test(NAME gotel-sqlite COMMAND ${Python3_EXECUTABLE}
      ${CMAKE_CURRENT_SOURCE_DIR}/sqlite_test.py $<TARGET_FILE:gotel-sqlite-probe>)
  endif()

  add_executable(gotel-tls-probe tls_probe.c)
  target_include_directories(gotel-tls-probe PRIVATE ${otlp_c_SOURCE_DIR}/src)
  target_link_libraries(gotel-tls-probe PRIVATE gotel::gotel)
  add_test(NAME gotel-tls COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/tls.py $<TARGET_FILE:gotel-tls-probe>)
  add_test(NAME gotel-python-testing COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/test_testing.py)
  add_test(NAME gotel-wire COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/wire.py $<TARGET_FILE:gotel-test> ${GOTEL_WITH_SQLITE} ${CMAKE_BUILD_TYPE})

  if(WIN32)
    get_property(_gotel_tests DIRECTORY PROPERTY TESTS)
    foreach(_test IN LISTS _gotel_tests)
      set_property(TEST ${_test} APPEND PROPERTY ENVIRONMENT_MODIFICATION
        "PATH=path_list_prepend:$<TARGET_FILE_DIR:gotel>"
        "PATH=path_list_prepend:$<TARGET_FILE_DIR:otlp_c>"
        "PATH=path_list_prepend:$<TARGET_FILE_DIR:${GOTEL_UTF8PROC_TARGET}>"
        "PATH=path_list_prepend:$<TARGET_FILE_DIR:${GOTEL_PCRE2_TARGET}>")
    endforeach()
  endif()
endif()
