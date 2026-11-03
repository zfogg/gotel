package querynames

import (
	"fmt"
	"regexp"
	"sync"

	sequel "github.com/zfogg/sequel/go"
)

var versionedFingerprint = regexp.MustCompile(`^sequel:v1:(generic|postgresql|duckdb|sqlite|mysql|clickhouse):[a-f0-9]{64}$`)

// Registry checks runtime shapes and optional source definitions separately.
// Observe/ObserveFingerprint only enforce one shape per name: distinct source
// operations may legitimately share a redacted shape. ObserveDefinition compares
// literal-preserving source templates. SQL and argument values are never retained.
type Registry struct {
	mu                        sync.Mutex
	names                     map[string]registryIdentity
	definitions               map[string]string
	allowDuplicateDefinitions bool
	// MaxEntries is fixed by NewRegistry; callers must not mutate it concurrently.
	MaxEntries int
}

type registryIdentity struct{ fingerprint, definition string }

// RegistryConfig controls a runtime/source identity registry.
type RegistryConfig struct {
	// MaxEntries bounds distinct operation names; nonpositive values select 4096.
	MaxEntries int
	// AllowDuplicateDefinitions permits different names for the same exact source
	// body. It never disables same-name shape or source-definition consistency.
	AllowDuplicateDefinitions bool
}

// NewRegistry creates a bounded, concurrent identity registry. A nonpositive
// limit selects 4096 entries. Exhaustion returns an error rather than losing
// duplicate detection through eviction.
func NewRegistry(maxEntries int) *Registry {
	return NewRegistryWithConfig(RegistryConfig{MaxEntries: maxEntries})
}

// NewRegistryWithConfig configures exact-definition duplicate policy. Shape-only
// observations always allow distinct names sharing a fingerprint.
func NewRegistryWithConfig(config RegistryConfig) *Registry {
	if config.MaxEntries <= 0 {
		config.MaxEntries = 4096
	}
	return &Registry{names: map[string]registryIdentity{}, definitions: map[string]string{}, MaxEntries: config.MaxEntries, allowDuplicateDefinitions: config.AllowDuplicateDefinitions}
}

// Observe verifies a runtime query name and statement shape. It does not compare
// fixed literals across operations or claim cross-name duplicate definitions. Invalid SQL is
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

// ObserveFingerprint checks same-name shape consistency without reanalysis.
// A fingerprint cannot establish that distinct names have duplicate definitions.
func (r *Registry) ObserveFingerprint(name, fingerprint string) error {
	if !identifier.MatchString(name) {
		return fmt.Errorf("query name must be a static identifier")
	}
	if !versionedFingerprint.MatchString(fingerprint) {
		return fmt.Errorf("query identity unavailable; manual review required")
	}
	return r.observe(name, fingerprint, "")
}

// ObserveDefinition registers a source-owned SQL template, preserving literals,
// bind markers and comments except leading SQLC name directives/outer whitespace.
// It rejects conflicting source bodies for one name and, by default, duplicate
// exact bodies across names. Only digests are retained. Do not call this for SQL
// containing interpolated runtime values; use Observe/ObserveFingerprint there.
func (r *Registry) ObserveDefinition(name, statement string, dialect sequel.Dialect) error {
	if !identifier.MatchString(name) {
		return fmt.Errorf("query name must be a static identifier")
	}
	definition, fingerprint, err := sqlDefinitionIdentity(statement, dialect, DefinitionExact)
	if err != nil {
		return err
	}
	return r.observe(name, fingerprint, definition)
}

func (r *Registry) observe(name, fingerprint, definition string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	prior, exists := r.names[name]
	if prior.fingerprint != "" && prior.fingerprint != fingerprint {
		return fmt.Errorf("query name %s is reused for a different shape", name)
	}
	if definition != "" && prior.definition != "" && prior.definition != definition {
		return fmt.Errorf("query name %s is reused for a different source definition", name)
	}
	if definition != "" && !r.allowDuplicateDefinitions {
		if owner, ok := r.definitions[definition]; ok && owner != name {
			return fmt.Errorf("query %s duplicates the definition of %s", name, owner)
		}
	}
	if !exists && len(r.names) >= r.MaxEntries {
		return fmt.Errorf("query identity registry capacity exceeded")
	}
	prior.fingerprint = fingerprint
	if definition != "" {
		prior.definition = definition
		r.definitions[definition] = name
	}
	r.names[name] = prior
	return nil
}
