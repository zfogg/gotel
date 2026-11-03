package sql

import (
	"container/list"
	"strings"
	"sync"
	"unsafe"

	sequel "github.com/zfogg/sequel/go"
)

// DefaultInspectionCacheBytes bounds retained SQL keys and analysis metadata.
const DefaultInspectionCacheBytes int64 = 8 << 20

// CacheStats is a consistent snapshot of the inspection LRU. Bytes includes
// retained keys, results, and entry overhead; it excludes allocator bookkeeping.
type CacheStats struct {
	// Hits counts successful lookups since the last configuration/reset.
	Hits uint64
	// Misses counts absent keys; cancelled analysis bypasses lookup.
	Misses uint64
	// Evictions counts entries removed to admit a newer result.
	Evictions uint64
	// Bypasses counts results too large for the retention budget.
	Bypasses uint64
	// Entries is the number of retained query/policy results.
	Entries int
	// Bytes is the conservative retained-allocation estimate in bytes.
	Bytes int64
	// MaxBytes is the configured budget; zero disables retention.
	MaxBytes int64
}

type inspectionCacheEntry struct {
	key   inspectionCacheKey
	value inspectionCacheValue
	bytes int64
}

type inspectionCache struct {
	mu      sync.Mutex
	stats   CacheStats
	entries map[inspectionCacheKey]*list.Element
	order   *list.List
}

var inspections = newInspectionCache(DefaultInspectionCacheBytes)

func newInspectionCache(maxBytes int64) *inspectionCache {
	return &inspectionCache{stats: CacheStats{MaxBytes: max(0, maxBytes)}, entries: make(map[inspectionCacheKey]*list.Element), order: list.New()}
}

// ConfigureInspectionCache replaces the byte budget and clears retained entries
// and counters. Zero disables retention. Concurrent inspection remains safe.
func ConfigureInspectionCache(maxBytes int64) {
	inspections.mu.Lock()
	defer inspections.mu.Unlock()
	inspections.stats = CacheStats{MaxBytes: max(0, maxBytes)}
	inspections.entries = make(map[inspectionCacheKey]*list.Element)
	inspections.order.Init()
}

// InspectionCacheStats returns the process-wide SQL inspection cache snapshot.
func InspectionCacheStats() CacheStats { return inspections.snapshot() }

func (c *inspectionCache) snapshot() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *inspectionCache) get(key inspectionCacheKey) (inspectionCacheValue, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		c.stats.Misses++
		return inspectionCacheValue{}, false
	}
	c.stats.Hits++
	c.order.MoveToFront(element)
	return element.Value.(inspectionCacheEntry).value, true
}

func inspectionBytes(key inspectionCacheKey, value inspectionCacheValue) int64 {
	// Include strings inside fixed diagnostic fields and both the map key and
	// list entry. Shared string backing storage may be counted twice, safely.
	size := int64(unsafe.Sizeof(inspectionCacheEntry{})+unsafe.Sizeof(list.Element{})+unsafe.Sizeof(key)+128) +
		int64(len(key.statement)+len(key.dialect)+len(value.formattedSQL)+len(value.operation)+len(value.summary)+len(value.simple)+len(value.sanitized.Text)+len(value.fingerprint)+len(value.status)+len(value.reason)+len(value.sourceMap)+len(value.binds))
	size += int64(cap(value.diagnostics)) * int64(unsafe.Sizeof(sequel.Diagnostic{}))
	for _, d := range value.diagnostics {
		size += int64(len(d.Message) + len(d.Explanation) + len(d.Hint) + len(d.Code) + len(d.Severity))
	}
	return size
}

func (c *inspectionCache) put(key inspectionCacheKey, value inspectionCacheValue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	size := inspectionBytes(key, value)
	if size > c.stats.MaxBytes || c.stats.MaxBytes == 0 {
		c.stats.Bypasses++
		return
	}
	// Detach small substrings from arbitrarily large caller allocations.
	key.statement = strings.Clone(key.statement)
	key.dialect = Dialect(strings.Clone(string(key.dialect)))
	value.simple = strings.Clone(value.simple)
	if old, ok := c.entries[key]; ok {
		c.remove(old)
	}
	for c.stats.Bytes+size > c.stats.MaxBytes {
		c.remove(c.order.Back())
		c.stats.Evictions++
	}
	element := c.order.PushFront(inspectionCacheEntry{key: key, value: value, bytes: size})
	c.entries[key] = element
	c.stats.Bytes += size
	c.stats.Entries++
}

func (c *inspectionCache) remove(element *list.Element) {
	entry := element.Value.(inspectionCacheEntry)
	delete(c.entries, entry.key)
	c.order.Remove(element)
	c.stats.Bytes -= entry.bytes
	c.stats.Entries--
}
