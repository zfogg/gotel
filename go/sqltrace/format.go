package sqltrace

/*
#cgo CFLAGS: -I${SRCDIR}/engine/include
#define GOTEL_SQL_STANDALONE 1
#include <stdlib.h>
#include <gotel/sql.h>
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

// FormatSQL formats SQL layout using gotel. It preserves comments, literals,
// identifiers and bind placeholders verbatim. It does not validate or execute
// SQL, interpolate parameters, or sanitize sensitive values. Input is limited
// to 1 MiB, 65536 tokens and 64 nesting levels. Errors return the original SQL.
func FormatSQL(dialect Dialect, query string) (string, error) {
	if len(query) > 1024*1024 || strings.IndexByte(query, 0) >= 0 {
		return query, fmt.Errorf("SQL exceeds 1 MiB or contains NUL")
	}
	switch dialect {
	case "", DialectGeneric, DialectPostgreSQL, DialectDuckDB, DialectSQLite, DialectMySQL:
	default:
		return query, fmt.Errorf("unsupported SQL dialect %q", dialect)
	}
	sql := C.CString(query)
	defer C.free(unsafe.Pointer(sql))
	var length C.size_t
	status := C.gotel_sql_format(sql, nativeDialect(dialect), nil, 0, &length)
	if status != C.OTLP_OK {
		return query, fmt.Errorf("format SQL: native status %d", status)
	}
	buf := make([]byte, int(length)+1)
	status = C.gotel_sql_format(sql, nativeDialect(dialect), (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)), &length)
	if status != C.OTLP_OK {
		return query, fmt.Errorf("format SQL: native status %d", status)
	}
	return string(buf[:int(length)]), nil
}

// FormatSQLOrOriginal is for presentation paths that must tolerate incomplete
// or unsupported input. Editors should use FormatSQL to surface errors.
func FormatSQLOrOriginal(dialect Dialect, query string) string {
	formatted, _ := FormatSQL(dialect, query)
	return formatted
}
