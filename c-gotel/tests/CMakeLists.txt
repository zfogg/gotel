add_library(c-gotel-testing STATIC ${PROJECT_SOURCE_DIR}/src/testing.c)
add_library(gotel::testing ALIAS c-gotel-testing)
target_link_libraries(c-gotel-testing PUBLIC gotel::c-gotel)
set_target_properties(c-gotel-testing PROPERTIES POSITION_INDEPENDENT_CODE ON)

if(CGOTEL_BUILD_TESTS)
  find_package(Threads REQUIRED)
  find_package(Python3 REQUIRED COMPONENTS Interpreter)
  if(CGOTEL_WITH_GRPC)
    execute_process(COMMAND ${Python3_EXECUTABLE} -c "import grpc, h2"
      RESULT_VARIABLE _grpc_python_status ERROR_QUIET)
    if(NOT _grpc_python_status EQUAL 0)
      message(FATAL_ERROR "gRPC tests require: python -m pip install -r c-gotel/tests/requirements-grpc.txt")
    endif()
    add_executable(c-gotel-grpc-probe grpc_probe.c)
    target_include_directories(c-gotel-grpc-probe PRIVATE ${otlp_c_SOURCE_DIR}/src)
    target_link_libraries(c-gotel-grpc-probe PRIVATE gotel::c-gotel)
    add_test(NAME c-gotel-grpc COMMAND ${Python3_EXECUTABLE}
      ${CMAKE_CURRENT_SOURCE_DIR}/grpc_test.py $<TARGET_FILE:c-gotel-grpc-probe>)
  endif()
  add_executable(c-gotel-snapshot-test snapshot_test.c)
  target_link_libraries(c-gotel-snapshot-test PRIVATE gotel::testing)
  add_test(NAME c-gotel-watchdog-snapshot COMMAND c-gotel-snapshot-test)

  add_executable(c-gotel-env-test env_test.c)
  if(CGOTEL_WITH_GRPC)
    target_compile_definitions(c-gotel-env-test PRIVATE CGOTEL_WITH_GRPC=1)
  endif()
  target_link_libraries(c-gotel-env-test PRIVATE gotel::c-gotel)
  target_include_directories(c-gotel-env-test PRIVATE
    ${PROJECT_SOURCE_DIR}/src ${PROJECT_BINARY_DIR}/otlp-tls ${otlp_c_SOURCE_DIR}/src)
  foreach(case defaults endpoints identity transport disabled validation headers grpc)
    add_test(NAME c-gotel-env-${case} COMMAND c-gotel-env-test ${case})
  endforeach()

  add_executable(c-gotel-test test.c)
  target_link_libraries(c-gotel-test PRIVATE gotel::testing Threads::Threads)
  target_compile_definitions(c-gotel-test PRIVATE
    GOTEL_HTTP_FIXTURES="${CMAKE_CURRENT_SOURCE_DIR}/../../testdata/http_names.tsv")
  add_test(NAME c-gotel-unit COMMAND c-gotel-test)
  foreach(case http validation sampling context registry concurrency testing)
    add_test(NAME c-gotel-${case} COMMAND c-gotel-test --case ${case})
  endforeach()

  add_executable(c-gotel-sql-probe sql_probe.c)
  target_link_libraries(c-gotel-sql-probe PRIVATE gotel::c-gotel)
  add_test(NAME c-gotel-sql COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/sql_test.py $<TARGET_FILE:c-gotel-sql-probe>)
  add_test(NAME c-gotel-sanitize COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/sql_sanitize_test.py $<TARGET_FILE:c-gotel-sql-probe>)
  add_test(NAME c-gotel-format COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/sql_format_test.py $<TARGET_FILE:c-gotel-sql-probe>)
  add_executable(c-gotel-format-api-test sql_format_api_test.c)
  target_link_libraries(c-gotel-format-api-test PRIVATE gotel::c-gotel)
  add_test(NAME c-gotel-format-api COMMAND c-gotel-format-api-test)

  add_executable(c-gotel-resource-probe resource_probe.c)
  target_link_libraries(c-gotel-resource-probe PRIVATE gotel::testing)
  add_test(NAME c-gotel-resource COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/resource_test.py $<TARGET_FILE:c-gotel-resource-probe>)

  if(CGOTEL_WITH_SQLITE)
    add_executable(c-gotel-sqlite-probe sqlite_probe.c)
    target_link_libraries(c-gotel-sqlite-probe PRIVATE gotel::testing)
    add_test(NAME c-gotel-sqlite COMMAND ${Python3_EXECUTABLE}
      ${CMAKE_CURRENT_SOURCE_DIR}/sqlite_test.py $<TARGET_FILE:c-gotel-sqlite-probe>)
  endif()

  add_executable(c-gotel-tls-probe tls_probe.c)
  target_include_directories(c-gotel-tls-probe PRIVATE ${otlp_c_SOURCE_DIR}/src)
  target_link_libraries(c-gotel-tls-probe PRIVATE gotel::c-gotel)
  add_test(NAME c-gotel-tls COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/tls.py $<TARGET_FILE:c-gotel-tls-probe>)
  add_test(NAME c-gotel-python-testing COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/test_testing.py)
  add_test(NAME c-gotel-wire COMMAND ${Python3_EXECUTABLE}
    ${CMAKE_CURRENT_SOURCE_DIR}/wire.py $<TARGET_FILE:c-gotel-test> ${CGOTEL_WITH_SQLITE} ${CMAKE_BUILD_TYPE})

  if(WIN32)
    get_property(_gotel_tests DIRECTORY PROPERTY TESTS)
    foreach(_test IN LISTS _gotel_tests)
      set_property(TEST ${_test} APPEND PROPERTY ENVIRONMENT_MODIFICATION
        "PATH=path_list_prepend:$<TARGET_FILE_DIR:c-gotel>"
        "PATH=path_list_prepend:$<TARGET_FILE_DIR:otlp_c>"
        "PATH=path_list_prepend:$<TARGET_FILE_DIR:${CGOTEL_UTF8PROC_TARGET}>"
        "PATH=path_list_prepend:$<TARGET_FILE_DIR:${CGOTEL_PCRE2_TARGET}>")
    endforeach()
  endif()
endif()
