set(_gotel_sources gotel.c env.c http.c sql.c resource.c command.c utf8.c)
if(BUILD_SHARED_LIBS)
  add_library(gotel SHARED ${_gotel_sources})
else()
  add_library(gotel STATIC ${_gotel_sources})
endif()

add_library(gotel::gotel ALIAS gotel)
target_compile_features(gotel PUBLIC c_std_11)
set_target_properties(gotel PROPERTIES
  POSITION_INDEPENDENT_CODE ON
  WINDOWS_EXPORT_ALL_SYMBOLS ON
  VERSION ${PROJECT_VERSION}
  SOVERSION 0.8
)
target_compile_definitions(gotel PRIVATE GOTEL_VERSION="${PROJECT_VERSION}")
target_link_libraries(gotel PRIVATE ${GOTEL_UTF8PROC_TARGET} ${GOTEL_PCRE2_TARGET})
target_include_directories(gotel PRIVATE
  ${PROJECT_BINARY_DIR}/otlp-tls
  ${otlp_c_SOURCE_DIR}/src
)
if(WIN32)
  target_link_libraries(gotel PRIVATE shell32 advapi32)
endif()
target_include_directories(gotel PUBLIC ${PROJECT_SOURCE_DIR}/include)
target_link_libraries(gotel PUBLIC otlp-c::otlp_c sequel::sequel)
if(GOTEL_WITH_GRPC)
  target_compile_definitions(gotel PRIVATE GOTEL_WITH_GRPC=1)
endif()

if(GOTEL_WITH_SQLITE)
  if(GOTEL_SQLITE_TARGET)
    if(NOT TARGET "${GOTEL_SQLITE_TARGET}")
      message(FATAL_ERROR "GOTEL_SQLITE_TARGET must name an existing CMake target")
    endif()
    set(_gotel_sqlite_target "${GOTEL_SQLITE_TARGET}")
  else()
    find_package(SQLite3 REQUIRED)
    if(TARGET SQLite3::SQLite3)
      set(_gotel_sqlite_target SQLite3::SQLite3)
    elseif(TARGET SQLite::SQLite3)
      set(_gotel_sqlite_target SQLite::SQLite3)
    else()
      message(FATAL_ERROR "FindSQLite3 did not provide a SQLite3 CMake target")
    endif()
  endif()
  target_sources(gotel PRIVATE sqlite.c named.c)
  target_link_libraries(gotel PUBLIC ${_gotel_sqlite_target})
  target_compile_definitions(gotel PUBLIC GOTEL_WITH_SQLITE=1)
endif()
