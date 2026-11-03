package sql

import (
	"encoding/json"

	sequel "github.com/zfogg/sequel/go"
)

const (
	// MaxAnalysisAttributeBytes bounds each optional map/bind transport attribute.
	MaxAnalysisAttributeBytes = 16 << 10
	// QueryTextReasonKey explains absent query text with a fixed safe enum.
	QueryTextReasonKey = "gotel.db.query.text_reason"
	// QueryFingerprintKey contains the versioned, dialect-scoped Sequel digest.
	QueryFingerprintKey = "gotel.db.query.fingerprint"
	// QueryAnalysisStatusKey distinguishes completed from failed analysis.
	QueryAnalysisStatusKey = "gotel.db.query.analysis_status"
	// QueryOriginalLengthKey measures original SQL in UTF-8 bytes, without SQL.
	QueryOriginalLengthKey = "gotel.db.query.original_length"
	// QueryDiagnosticSchemaVersionKey identifies Sequel's diagnostic wire schema.
	QueryDiagnosticSchemaVersionKey = "gotel.db.query.diagnostic_schema_version"
	// QuerySourceMapKey contains an opt-in bounded JSON provenance envelope.
	QuerySourceMapKey = "gotel.db.query.source_map"
	// QuerySourceMapOmittedKey marks a requested map exceeding transport bounds.
	QuerySourceMapOmittedKey = "gotel.db.query.source_map_omitted"
	// QueryBindsKey contains opt-in bounded marker metadata, never bind values.
	QueryBindsKey = "gotel.db.query.binds"
)

type sourceMapEnvelope struct {
	SchemaVersion        int              `json:"schema_version"`
	OriginalToSanitized  sequel.SourceMap `json:"original_to_sanitized"`
	SanitizedToFormatted sequel.SourceMap `json:"sanitized_to_formatted"`
	Display              string           `json:"display"`
}

func boundedJSON(value any) (string, bool) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > MaxAnalysisAttributeBytes {
		return "", false
	}
	return string(encoded), true
}

// InspectAnalysis extracts metadata using unified Sequel analysis, including
// per-call resource budgets and cancellation. Maps are transported with a 16KiB
// cap. Bind descriptors require OmitBindDescriptors=false and have the same cap.
// Input bind values are counted only; analysis never inspects or records them.
func InspectAnalysis(namer Namer, explicitName, statement string, args []any, options sequel.AnalysisOptions) Metadata {
	if options.Dialect == "" {
		options.Dialect = DialectGeneric
	}
	key := inspectionCacheKey{
		statement: statement, dialect: options.Dialect, maxBytes: options.MaxQueryTextBytes,
		collapse: !options.KeepInLists, omit: options.OmitQueryText,
		maxInput: options.MaxInputBytes, maxOutput: options.MaxOutputBytes,
		maxMemory: options.MaxMemoryBytes, maxWork: options.MaxWorkUnits,
		format: options.Format, omitMaps: options.OmitSourceMaps, omitBinds: options.OmitBindDescriptors,
	}
	// Cancellation is checked around lookup and polled during native misses.
	// Cancelled results never poison a later caller's key.
	cacheable := options.Context == nil || options.Context.Err() == nil
	var cached inspectionCacheValue
	var found bool
	if cacheable {
		cached, found = inspections.get(key)
	}
	if options.Context != nil && options.Context.Err() != nil {
		found, cacheable = false, false
	}
	if !found {
		a := sequel.Analyze(statement, options)
		cached.summary, cached.operation = a.Summary, a.Operation
		cached.sanitized = SanitizedQuery{Text: a.Text, Truncated: a.Truncated}
		cached.statementCount = a.StatementCount
		if a.Status != "ok" {
			cached.statementCount = 0
		}
		cached.simple = sequel.SQLCName(statement)
		cached.diagnostics = a.Diagnostics
		cached.fingerprint, cached.status, cached.reason = a.Fingerprint, a.Status, a.OmissionReason
		cached.originalLength, cached.schemaVersion = a.OriginalLength, sequel.DiagnosticSchemaVersion
		cached.formattedSQL = a.Text
		display := "sanitized"
		if a.Formatted != "" && (options.MaxQueryTextBytes <= 0 || len(a.Formatted) <= options.MaxQueryTextBytes) {
			cached.formattedSQL, display = a.Formatted, "formatted"
		}
		if !options.OmitSourceMaps && a.Text != "" {
			// Bound traversal before json.Marshal can allocate an oversized output.
			if len(a.OriginalToSanitized.Segments)+len(a.SanitizedToFormatted.Segments) <= MaxAnalysisAttributeBytes/96 {
				cached.sourceMap, cached.mapsPresent = boundedJSON(sourceMapEnvelope{a.SchemaVersion, a.OriginalToSanitized, a.SanitizedToFormatted, display})
			}
			cached.mapsOmitted = !cached.mapsPresent
		}
		if !options.OmitBindDescriptors && len(a.Binds) <= MaxAnalysisAttributeBytes/96 {
			cached.binds, cached.bindsPresent = boundedJSON(a.Binds)
		}
		if cached.summary == "" {
			cached.summary = "SQL"
		}
		if cacheable && a.Status != "cancelled" && a.Status != "error" {
			inspections.put(key, cached)
		}
	}
	return metadataFromInspection(namer, explicitName, len(args), cached)
}
