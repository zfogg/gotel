set(_gotel_sources gotel.c http.c sql.c resource.c utf8.c)
if(BUILD_SHARED_LIBS)
  add_library(c-gotel SHARED ${_gotel_sources})
else()
  add_library(c-gotel STATIC ${_gotel_sources})
endif()

add_library(gotel::c-gotel ALIAS c-gotel)
target_compile_features(c-gotel PUBLIC c_std_11)
set_target_properties(c-gotel PROPERTIES
  POSITION_INDEPENDENT_CODE ON
  WINDOWS_EXPORT_ALL_SYMBOLS ON
  VERSION ${PROJECT_VERSION}
  SOVERSION 0
)
target_compile_definitions(c-gotel PRIVATE CGOTEL_VERSION="${PROJECT_VERSION}")
target_link_libraries(c-gotel PRIVATE ${CGOTEL_UTF8PROC_TARGET} ${CGOTEL_PCRE2_TARGET})
target_include_directories(c-gotel PRIVATE
  ${PROJECT_BINARY_DIR}/otlp-tls
  ${otlp_c_SOURCE_DIR}/src
)
if(WIN32)
  target_link_libraries(c-gotel PRIVATE shell32 advapi32)
endif()
target_include_directories(c-gotel PUBLIC ${PROJECT_SOURCE_DIR}/include)
target_link_libraries(c-gotel PUBLIC otlp-c::otlp_c)

if(CGOTEL_WITH_SQLITE)
  find_package(SQLite3 REQUIRED)
  target_sources(c-gotel PRIVATE sqlite.c named.c)
  if(TARGET SQLite3::SQLite3)
    target_link_libraries(c-gotel PUBLIC SQLite3::SQLite3)
  else()
    target_link_libraries(c-gotel PUBLIC SQLite::SQLite3)
  endif()
  target_compile_definitions(c-gotel PUBLIC CGOTEL_WITH_SQLITE=1)
endif()
