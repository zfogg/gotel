set(_docs_root "${CMAKE_CURRENT_LIST_DIR}/..")
get_filename_component(_docs_root "${_docs_root}" ABSOLUTE)
file(READ "${_docs_root}/docs/site.json" _docs_config)
string(JSON _docs_python GET "${_docs_config}" python_project)
string(JSON _docs_repo GET "${_docs_config}" repository)
find_program(DOCS_UV uv REQUIRED)
find_package(Doxygen 1.9 REQUIRED)
set(DOCS_OUTPUT_DIR "${CMAKE_CURRENT_BINARY_DIR}/site" CACHE PATH "Generated documentation site")
set(DOCS_PUBLISH_REF main CACHE STRING "Remote Git ref deployed by docs-publish")
configure_file("${_docs_root}/docs/Doxyfile.in" "${CMAKE_CURRENT_BINARY_DIR}/Doxyfile" @ONLY)
set(_docs_command "${DOCS_UV}" run --project "${_docs_root}/${_docs_python}" --group docs --locked python "${_docs_root}/scripts/docs.py")
add_custom_target(docs-c
  COMMAND "${CMAKE_COMMAND}" -E make_directory "${DOCS_OUTPUT_DIR}/c"
  COMMAND "${DOXYGEN_EXECUTABLE}" "${CMAKE_CURRENT_BINARY_DIR}/Doxyfile"
  WORKING_DIRECTORY "${_docs_root}" VERBATIM)
foreach(language IN ITEMS python go)
  add_custom_target(docs-${language}
    COMMAND ${_docs_command} ${language} --output "${DOCS_OUTPUT_DIR}"
    WORKING_DIRECTORY "${_docs_root}" VERBATIM USES_TERMINAL)
endforeach()
string(JSON _docs_typescript ERROR_VARIABLE _docs_no_typescript GET "${_docs_config}" languages typescript)
set(_docs_languages docs-c docs-python docs-go)
if(NOT _docs_no_typescript)
  add_custom_target(docs-typescript
    COMMAND ${_docs_command} typescript --output "${DOCS_OUTPUT_DIR}"
    WORKING_DIRECTORY "${_docs_root}" VERBATIM USES_TERMINAL)
  list(APPEND _docs_languages docs-typescript)
endif()
add_custom_target(docs-site
  COMMAND ${_docs_command} site --output "${DOCS_OUTPUT_DIR}"
  DEPENDS ${_docs_languages} WORKING_DIRECTORY "${_docs_root}" VERBATIM)
add_custom_target(docs-check
  COMMAND ${_docs_command} verify --output "${DOCS_OUTPUT_DIR}"
  DEPENDS docs-site WORKING_DIRECTORY "${_docs_root}" VERBATIM)
add_custom_target(docs DEPENDS docs-check)
add_custom_target(docs-publish
  COMMAND ${_docs_command} publish --ref "${DOCS_PUBLISH_REF}"
  WORKING_DIRECTORY "${_docs_root}" VERBATIM USES_TERMINAL)
