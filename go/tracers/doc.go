// Package tracers groups gotel's instrumentation adapters and trace processor.
//
// Import the adapter you need from a child package: gorm, http, mcp, pgx,
// redis, sql or temporal. The processor package provides whole-trace retention.
// For example, the HTTP adapter is github.com/zfogg/gotel/go/tracers/http.
package tracers
