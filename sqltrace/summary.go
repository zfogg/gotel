package sqltrace

/*
#cgo CFLAGS: -I${SRCDIR}/../c-gotel/include -DUTF8PROC_STATIC
#define CGOTEL_SQL_STANDALONE 1
#include <stdlib.h>
#include <gotel/sql.h>
*/
import "C"

import (
	"strings"
	"unsafe"
)

// Dialect selects SQL quoting, placeholder, and sanitization rules.
type Dialect string

const (
	DialectGeneric         Dialect = "generic"
	DialectPostgreSQL      Dialect = "postgresql"
	DialectDuckDB          Dialect = "duckdb"
	DialectSQLite          Dialect = "sqlite"
	DialectMySQL           Dialect = "mysql"
	DefaultMaxSummaryBytes         = 4096
)

func nativeDialect(d Dialect) C.gotel_sql_dialect_t {
	switch strings.ToLower(string(d)) {
	case "sqlite":
		return C.GOTEL_SQL_SQLITE
	case "postgresql":
		return C.GOTEL_SQL_POSTGRESQL
	case "duckdb":
		return C.GOTEL_SQL_DUCKDB
	case "mysql":
		return C.GOTEL_SQL_MYSQL
	default:
		return C.GOTEL_SQL_GENERIC
	}
}

func inspectNative(d Dialect, query string) (string, string, bool) {
	if strings.IndexByte(query, 0) >= 0 {
		return "", "", false
	}
	sql := C.CString(query)
	defer C.free(unsafe.Pointer(sql))
	options := C.gotel_sql_options_t{dialect: nativeDialect(d), omit_query_text: true}
	var metadata C.gotel_sql_metadata_t
	if C.gotel_sql_inspect(sql, &options, &metadata) != C.OTLP_OK {
		return "", "", false
	}
	return C.GoString(&metadata.summary[0]), C.GoString(&metadata.operation[0]), true
}

// Summary returns a bounded SQL skeleton from the shared c-gotel parser.
func Summary(dialect Dialect, query string) string {
	summary, _, ok := inspectNative(dialect, query)
	if !ok || summary == "" {
		return "SQL"
	}
	return summary
}

// statementOperation uses the same parser as Summary, including CTE handling.
func statementOperation(dialect Dialect, statement string) string {
	_, operation, ok := inspectNative(dialect, statement)
	if !ok {
		return ""
	}
	return operation
}
