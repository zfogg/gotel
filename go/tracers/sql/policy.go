package sql

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

// AnalysisOptions is Sequel's unified per-call analysis policy. Explicit options
// enable source maps/bind descriptors unless their Omit fields are set. Resource
// budgets are bytes or work units; zero preserves Sequel's 1GiB/default ceiling.
type AnalysisOptions = sequel.AnalysisOptions

const DefaultMaxQueryTextBytes = sequel.DefaultMaxQueryTextBytes
