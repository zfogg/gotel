package querynames

import (
	"fmt"
	"regexp"
	"sync"

	sequel "github.com/zfogg/sequel/go"
)

var versionedFingerprint = regexp.MustCompile(`^sequel:v1:(generic|postgresql|duckdb|sqlite|mysql|clickhouse):[a-f0-9]{64}$`)

// Registry checks dynamic executions against source-owned query identities.
// It retains names and canonical fingerprints, never SQL or arguments. Construct
// one per application/database boundary; MaxEntries bounds cardinality.
type Registry struct {
	mu     sync.Mutex
	names  map[string]string
	shapes map[string]string
	// MaxEntries is fixed by NewRegistry; callers must not mutate it concurrently.
	MaxEntries int
}

// NewRegistry creates a bounded, concurrent identity registry. A nonpositive
// limit selects 4096 entries. Exhaustion returns an error rather than losing
// duplicate detection through eviction.
func NewRegistry(maxEntries int) *Registry {
	if maxEntries <= 0 {
		maxEntries = 4096
	}
	return &Registry{names: map[string]string{}, shapes: map[string]string{}, MaxEntries: maxEntries}
}

// Observe verifies a runtime query name and statement shape. Invalid SQL is
// rejected; callers should record this error as a naming diagnostic, not fail a
// database operation that would otherwise execute. No input SQL is in errors.
func (r *Registry) Observe(name, statement string, dialect sequel.Dialect) error {
	if !identifier.MatchString(name) {
		return fmt.Errorf("query name must be a static identifier")
	}
	shape, err := sequel.Fingerprint(dialect, statement)
	if err != nil {
		return fmt.Errorf("query identity unavailable; manual review required")
	}
	return r.ObserveFingerprint(name, shape)
}

// ObserveFingerprint checks a versioned Sequel fingerprint without reanalysis.
func (r *Registry) ObserveFingerprint(name, fingerprint string) error {
	if !identifier.MatchString(name) {
		return fmt.Errorf("query name must be a static identifier")
	}
	if !versionedFingerprint.MatchString(fingerprint) {
		return fmt.Errorf("query identity unavailable; manual review required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.names[name]; ok {
		if prior != fingerprint {
			return fmt.Errorf("query name %s is reused for a different definition", name)
		}
		return nil
	}
	if prior, ok := r.shapes[fingerprint]; ok {
		return fmt.Errorf("query %s duplicates the definition of %s", name, prior)
	}
	if len(r.names) >= r.MaxEntries {
		return fmt.Errorf("query identity registry capacity exceeded")
	}
	r.names[name] = fingerprint
	r.shapes[fingerprint] = name
	return nil
}
