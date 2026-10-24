package main

/*
#cgo CFLAGS: -DGOTEL_SQL_STANDALONE=1 -DUTF8PROC_STATIC -I${SRCDIR}/../../c/include -I${SRCDIR}/../../go/sqltrace
#include <stdlib.h>
#include <gotel/sql.h>
int edit_sql(const char *, int, char *, size_t);
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

func transform(sql, command, dialect string, edit bool) (string, error) {
	if len(sql) > 1024*1024 || strings.ContainsRune(sql, 0) {
		return "", fmt.Errorf("SQL exceeds 1 MiB or contains NUL")
	}
	dialects := map[string]C.gotel_sql_dialect_t{"generic": C.GOTEL_SQL_GENERIC, "sqlite": C.GOTEL_SQL_SQLITE, "postgresql": C.GOTEL_SQL_POSTGRESQL, "duckdb": C.GOTEL_SQL_DUCKDB, "mysql": C.GOTEL_SQL_MYSQL, "clickhouse": C.GOTEL_SQL_CLICKHOUSE}
	d, ok := dialects[dialect]
	if !ok {
		return "", fmt.Errorf("unsupported SQL dialect %q", dialect)
	}
	input := C.CString(sql)
	defer C.free(unsafe.Pointer(input))
	buf := make([]byte, len(sql)*8+1024)
	out := (*C.char)(unsafe.Pointer(&buf[0]))
	var status C.otlp_status_t
	if edit {
		if C.edit_sql(input, C.int(d), out, C.size_t(len(buf))) != 0 {
			return "", fmt.Errorf("cannot safely format SQL")
		}
	} else if command == "format" {
		var length C.size_t
		status = C.gotel_sql_format(input, d, out, C.size_t(len(buf)), &length)
	} else {
		var truncated C.bool
		status = C.gotel_sql_sanitize(input, d, true, C.size_t(len(buf)-1), out, C.size_t(len(buf)), &truncated)
		if bool(truncated) {
			return "", fmt.Errorf("sanitized SQL was truncated")
		}
	}
	if status != C.OTLP_OK {
		return "", fmt.Errorf("invalid or unsupported SQL (status %d)", status)
	}
	return C.GoString(out), nil
}
