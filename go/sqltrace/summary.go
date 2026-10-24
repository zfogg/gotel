package sqltrace

/*
#cgo CFLAGS: -I${SRCDIR}/engine/include -DUTF8PROC_STATIC
#define GOTEL_SQL_STANDALONE 1
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
	DialectClickHouse      Dialect = "clickhouse"
	DefaultMaxSummaryBytes         = 32 * 1024
)

func nativeDialect(d Dialect) C.gotel_sql_dialect_t {
	switch strings.ToLower(string(d)) {
	case "sqlite":
		return C.GOTEL_SQL_SQLITE
	case "postgresql":
		return C.GOTEL_SQL_POSTGRESQL
	case "duckdb":
		return C.GOTEL_SQL_DUCKDB
	case "clickhouse":
		return C.GOTEL_SQL_CLICKHOUSE
	case "mysql":
		return C.GOTEL_SQL_MYSQL
	default:
		return C.GOTEL_SQL_GENERIC
	}
}

func inspectNative(d Dialect, query string) (string, string, bool) {
	summary, operation, _, _, ok := inspectNativeWithOptions(query, SanitizeOptions{Dialect: d, OmitQueryText: true})
	return summary, operation, ok
}

func inspectNativeWithOptions(query string, policy SanitizeOptions) (string, string, SanitizedQuery, int, bool) {
	if strings.IndexByte(query, 0) >= 0 {
		return "", "", SanitizedQuery{}, 0, false
	}
	sql := C.CString(query)
	defer C.free(unsafe.Pointer(sql))
	limit := policy.MaxQueryTextBytes
	if limit == 0 {
		limit = DefaultMaxQueryTextBytes
	}
	largeLimit := limit < 0 || limit > C.GOTEL_SQL_TEXT_MAX
	options := C.gotel_sql_options_t{dialect: nativeDialect(policy.Dialect),
		omit_query_text: C.bool(policy.OmitQueryText || largeLimit),
		keep_in_lists:   C.bool(!policy.CollapseInLists)}
	if !largeLimit {
		options.max_query_text_bytes = C.size_t(limit)
	}
	var metadata C.gotel_sql_metadata_t
	if C.gotel_sql_inspect(sql, &options, &metadata) != C.OTLP_OK {
		return "", "", SanitizedQuery{}, 0, false
	}
	safe := SanitizedQuery{Text: C.GoString(&metadata.text[0]), Truncated: bool(metadata.truncated)}
	if largeLimit && !policy.OmitQueryText {
		var ok bool
		safe, ok = sanitizeNative(query, policy)
		if !ok {
			return "", "", SanitizedQuery{}, 0, false
		}
	}
	return C.GoString(&metadata.summary[0]), C.GoString(&metadata.operation[0]), safe, int(metadata.statement_count), true
}

func sanitizeNative(query string, policy SanitizeOptions) (SanitizedQuery, bool) {
	if strings.IndexByte(query, 0) >= 0 || len(query) > 1024*1024 {
		return SanitizedQuery{}, false
	}
	limit := policy.MaxQueryTextBytes
	if limit == 0 {
		limit = DefaultMaxQueryTextBytes
	}
	if limit < 0 || limit > len(query) {
		limit = len(query)
	}
	if limit == 0 {
		limit = 1
	}
	buf := make([]byte, limit+1)
	sql := C.CString(query)
	defer C.free(unsafe.Pointer(sql))
	var truncated C.bool
	status := C.gotel_sql_sanitize(sql, nativeDialect(policy.Dialect), C.bool(policy.CollapseInLists),
		C.size_t(limit), (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)), &truncated)
	if status != C.OTLP_OK {
		return SanitizedQuery{}, false
	}
	return SanitizedQuery{Text: C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), Truncated: bool(truncated)}, true
}

// Summary returns a bounded SQL skeleton from the shared gotel parser.
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
