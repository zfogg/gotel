package sqltrace

import sequel "github.com/zfogg/sequel/go"

type Dialect = sequel.Dialect

const (
	DialectGeneric         = sequel.DialectGeneric
	DialectPostgreSQL      = sequel.DialectPostgreSQL
	DialectDuckDB          = sequel.DialectDuckDB
	DialectSQLite          = sequel.DialectSQLite
	DialectMySQL           = sequel.DialectMySQL
	DialectClickHouse      = sequel.DialectClickHouse
	DefaultMaxSummaryBytes = sequel.DefaultMaxSummaryBytes
)

type SanitizeOptions = sequel.SanitizeOptions
type SanitizedQuery = sequel.SanitizedQuery

const DefaultMaxQueryTextBytes = sequel.DefaultMaxQueryTextBytes
