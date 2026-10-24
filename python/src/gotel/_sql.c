#define PY_SSIZE_T_CLEAN
#include <Python.h>
#include <gotel/sql.h>

static PyObject *inspect_sql(PyObject *self, PyObject *args) {
  const char *sql;
  int dialect, omit, keep;
  Py_ssize_t limit;
  if (!PyArg_ParseTuple(args, "sinpp", &sql, &dialect, &limit, &omit, &keep)) return NULL;
  if (limit < 0 || limit > GOTEL_SQL_TEXT_MAX) {
    PyErr_SetString(PyExc_ValueError, "query text limit must be between 0 and 32768");
    return NULL;
  }
  gotel_sql_options_t options = {0};
  options.dialect = (gotel_sql_dialect_t)dialect;
  options.max_query_text_bytes = (size_t)limit;
  options.omit_query_text = omit;
  options.keep_in_lists = keep;
  gotel_sql_metadata_t *metadata = PyMem_Malloc(sizeof(*metadata));
  if (!metadata) return PyErr_NoMemory();
  otlp_status_t status = gotel_sql_inspect(sql, &options, metadata);
  if (status != OTLP_OK) {
    PyMem_Free(metadata);
    PyErr_SetString(PyExc_ValueError, "SQL inspection rejected input");
    return NULL;
  }
  PyObject *result = Py_BuildValue("sssOn", metadata->text, metadata->summary, metadata->operation,
                                  metadata->truncated ? Py_True : Py_False,
                                  (Py_ssize_t)metadata->statement_count);
  PyMem_Free(metadata);
  return result;
}

static PyObject *format_sql(PyObject *self, PyObject *args) {
  const char *sql;
  int dialect;
  if (!PyArg_ParseTuple(args, "si", &sql, &dialect)) return NULL;
  size_t length = 0;
  if (gotel_sql_format(sql, (gotel_sql_dialect_t)dialect, NULL, 0, &length) != OTLP_OK) {
    PyErr_SetString(PyExc_ValueError, "SQL formatting rejected input");
    return NULL;
  }
  char *out = PyMem_Malloc(length + 1);
  if (!out) return PyErr_NoMemory();
  otlp_status_t status = gotel_sql_format(sql, (gotel_sql_dialect_t)dialect, out, length + 1, &length);
  PyObject *result = NULL;
  if (status == OTLP_OK) result = PyUnicode_DecodeUTF8(out, (Py_ssize_t)length, "strict");
  else PyErr_SetString(PyExc_ValueError, "SQL formatting rejected input");
  PyMem_Free(out);
  return result;
}

static PyMethodDef methods[] = {
  {"inspect", inspect_sql, METH_VARARGS, "Inspect using the shared C engine."},
  {"format", format_sql, METH_VARARGS, "Format without sanitizing."},
  {NULL, NULL, 0, NULL}
};
static struct PyModuleDef module = {PyModuleDef_HEAD_INIT, "_sql", NULL, -1, methods};
PyMODINIT_FUNC PyInit__sql(void) { return PyModule_Create(&module); }
