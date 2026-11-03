package sql

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	sequel "github.com/zfogg/sequel/go"
)

func TestUnifiedAnalysisReasonsAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name, statement, reason string
		options                 sequel.AnalysisOptions
	}{
		{"empty", "", "empty", sequel.AnalysisOptions{}},
		{"policy", "SELECT 'never-export-secret'", "policy", sequel.AnalysisOptions{OmitQueryText: true}},
		{"malformed", "SELECT 'never-export-secret", "malformed", sequel.AnalysisOptions{}},
		{"budget", "SELECT 'never-export-secret'", "analysis_limit", sequel.AnalysisOptions{MaxWorkUnits: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := InspectAnalysis(Namer{}, "Read", tc.statement, nil, tc.options)
			if m.QueryTextReason != tc.reason || m.SanitizedSQL != "" {
				t.Fatalf("reason=%q text=%q", m.QueryTextReason, m.SanitizedSQL)
			}
			b, _ := json.Marshal(m.Attributes("postgresql", "test"))
			if strings.Contains(string(b), "never-export-secret") {
				t.Fatal("literal escaped into transport")
			}
			if m.OriginalLength != len(tc.statement) || m.DiagnosticSchemaVersion != 1 {
				t.Fatal("missing original/schema metadata")
			}
		})
	}
}

func TestUnifiedFingerprintMapAndBinds(t *testing.T) {
	options := sequel.AnalysisOptions{Dialect: DialectPostgreSQL, Format: true}
	sql := "-- leading\r\nSELECT '🔒secret', id FROM widgets WHERE id=$1 OR parent=$1"
	m := InspectAnalysis(Namer{}, "Read", sql, []any{99}, options)
	other := InspectAnalysis(Namer{}, "Read", strings.Replace(sql, "🔒secret", "changed", 1), []any{123}, options)
	if m.Fingerprint == "" || m.Fingerprint != other.Fingerprint {
		t.Fatal("literal values changed query identity")
	}
	var envelope sourceMapEnvelope
	if err := json.Unmarshal([]byte(m.SourceMap), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OriginalToSanitized.FromLength != len(sql) {
		t.Fatal("wrong original map length")
	}
	if envelope.Display == "formatted" && envelope.SanitizedToFormatted.ToLength != len(m.formattedSQL) {
		t.Fatal("map does not describe transported text")
	}
	var binds []sequel.BindDescriptor
	if err := json.Unmarshal([]byte(m.BindDescriptors), &binds); err != nil {
		t.Fatal(err)
	}
	if len(binds) != 2 || binds[0].Slot != binds[1].Slot {
		t.Fatalf("bind identity lost: %+v", binds)
	}
	if err := sequel.CheckBinds(binds, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := sequel.CheckBinds(binds, 0, 2); err == nil {
		t.Fatal("wrong argument count accepted")
	}
}

func TestCancellationDoesNotPoisonCache(t *testing.T) {
	ConfigureInspectionCache(DefaultInspectionCacheBytes)
	t.Cleanup(func() { ConfigureInspectionCache(DefaultInspectionCacheBytes) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := InspectAnalysis(Namer{}, "Read", "SELECT 1", nil, sequel.AnalysisOptions{Context: ctx})
	if m.AnalysisStatus != "cancelled" || m.SanitizedSQL != "" {
		t.Fatalf("cancelled=%+v", m)
	}
	m = InspectAnalysis(Namer{}, "Read", "SELECT 1", nil, sequel.AnalysisOptions{})
	if m.AnalysisStatus != "ok" || m.SanitizedSQL == "" {
		t.Fatal("cancelled result poisoned cache")
	}
}

func TestByteCacheBudgetAndOversizeBypass(t *testing.T) {
	value := inspectionCacheValue{operation: "SELECT"}
	unit := inspectionBytes(inspectionCacheKey{statement: "SELECT 1"}, value)
	cache := newInspectionCache(2 * unit)
	for _, sql := range []string{"SELECT 1", "SELECT 2", "SELECT 3"} {
		cache.put(inspectionCacheKey{statement: sql}, value)
	}
	if _, ok := cache.get(inspectionCacheKey{statement: "SELECT 1"}); ok {
		t.Fatal("LRU did not evict")
	}
	cache.put(inspectionCacheKey{statement: strings.Repeat("x", int(unit*3))}, value)
	stats := cache.snapshot()
	if stats.Bytes > stats.MaxBytes || stats.Entries != 2 || stats.Evictions != 1 || stats.Bypasses != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestDiagnosticsTransportBounds(t *testing.T) {
	m := InspectAnalysis(Namer{}, "Invalid", strings.Repeat("SELECT );", 100), nil, sequel.AnalysisOptions{})
	var diagnostics []string
	omitted := false
	for _, attr := range m.Attributes("postgresql", "") {
		if string(attr.Key) == "db.query.diagnostics" {
			diagnostics = attr.Value.AsStringSlice()
		}
		if string(attr.Key) == "gotel.db.query.diagnostics_omitted" {
			omitted = attr.Value.AsBool()
		}
	}
	if len(diagnostics) == 0 || len(diagnostics) > 64 || !omitted {
		t.Fatalf("count=%d omitted=%t", len(diagnostics), omitted)
	}
	bytes := 0
	for _, d := range diagnostics {
		bytes += len(d)
	}
	if bytes > MaxAnalysisAttributeBytes {
		t.Fatal("diagnostics exceeded transport byte budget")
	}
}

func TestCacheConcurrentConfigureAndReads(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			for range 20 {
				Inspect(Namer{}, "Read", "SELECT 1", nil)
				_ = InspectionCacheStats()
				ConfigureInspectionCache(4096)
			}
		})
	}
	wg.Wait()
	ConfigureInspectionCache(DefaultInspectionCacheBytes)
}
