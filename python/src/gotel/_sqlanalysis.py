"""Unified native SQL analysis and a byte-bounded, observable inspection LRU."""

from __future__ import annotations

import copy
import json
import sys
from collections import OrderedDict
from dataclasses import asdict, dataclass
from threading import Lock
from typing import Any

import sequel

MAX_ANALYSIS_ATTRIBUTE_BYTES = 16 << 10
DEFAULT_INSPECTION_CACHE_BYTES = 8 << 20


@dataclass(frozen=True)
class CacheStats:
    """Consistent reset-relative counters and retained-byte accounting.

    hits/misses count lookups, evictions count budget-driven removals, bypasses
    count oversized results, entries counts retained keys, bytes is accounted
    memory and max_bytes is the configured budget. Zero max_bytes disables
    retention; allocator bookkeeping is excluded.
    """

    hits: int
    misses: int
    evictions: int
    bypasses: int
    entries: int
    bytes: int
    max_bytes: int


def _size(value: Any, seen: set[int] | None = None) -> int:
    seen = set() if seen is None else seen
    if id(value) in seen:
        return 0
    seen.add(id(value))
    result = sys.getsizeof(value)
    if isinstance(value, dict):
        result += sum(_size(key, seen) + _size(item, seen) for key, item in value.items())
    elif isinstance(value, (tuple, list)):
        result += sum(_size(item, seen) for item in value)
    return result


class _Cache:
    def __init__(self, max_bytes: int):
        self.lock = Lock()
        self.entries: OrderedDict[tuple, tuple[dict, int]] = OrderedDict()
        self.max_bytes = max_bytes
        self.bytes = self.hits = self.misses = self.evictions = self.bypasses = 0

    def configure(self, max_bytes: int) -> None:
        if max_bytes < 0:
            raise ValueError("max_bytes must be nonnegative")
        with self.lock:
            self.entries.clear()
            self.max_bytes = max_bytes
            self.bytes = self.hits = self.misses = self.evictions = self.bypasses = 0

    def stats(self) -> CacheStats:
        with self.lock:
            return CacheStats(
                self.hits, self.misses, self.evictions, self.bypasses, len(self.entries), self.bytes, self.max_bytes
            )

    def get(self, key: tuple) -> dict | None:
        with self.lock:
            entry = self.entries.get(key)
            if entry is None:
                self.misses += 1
                return None
            self.hits += 1
            self.entries.move_to_end(key)
            return copy.deepcopy(entry[0])

    def put(self, key: tuple, value: dict) -> None:
        # Includes raw SQL in the key, dictionary keys/values, result arrays,
        # tuple/list allocation and a conservative OrderedDict node allowance.
        size = _size((key, value)) + 256
        with self.lock:
            if size > self.max_bytes or self.max_bytes == 0:
                self.bypasses += 1
                return
            old = self.entries.pop(key, None)
            if old:
                self.bytes -= old[1]
            while self.bytes + size > self.max_bytes:
                _, (_, removed) = self.entries.popitem(last=False)
                self.bytes -= removed
                self.evictions += 1
            self.entries[key] = (copy.deepcopy(value), size)
            self.bytes += size


_cache = _Cache(DEFAULT_INSPECTION_CACHE_BYTES)


def configure_inspection_cache(max_bytes: int = DEFAULT_INSPECTION_CACHE_BYTES) -> None:
    """Set retention budget and clear entries/counters atomically; zero disables."""
    _cache.configure(max_bytes)


def inspection_cache_stats() -> CacheStats:
    """Return a consistent process-wide inspection cache snapshot."""
    return _cache.stats()


def _json(value: Any) -> str | None:
    result = json.dumps(value, separators=(",", ":"), ensure_ascii=True)
    return result if len(result.encode()) <= MAX_ANALYSIS_ATTRIBUTE_BYTES else None


def _analyze(sql: str, dialect, policy: dict) -> dict:
    attrs: dict[str, Any] = {"db.query.diagnostics": []}
    options = dict(policy)
    if "include_binds" in options:
        options["omit_bind_descriptors"] = not options.pop("include_binds")
    if "include_source_maps" in options:
        options["omit_source_maps"] = not options.pop("include_source_maps")
    options["keep_in_lists"] = not options.pop("collapse_in_lists", True)
    options.setdefault("format", True)
    options.setdefault("omit_bind_descriptors", True)
    options.setdefault("omit_source_maps", True)
    try:
        query = sequel.analyze(sql, dialect=dialect, **options)
    except (sequel.SQLInspectionError, TypeError, ValueError):
        try:
            original_length = len(sql.encode()) if isinstance(sql, str) else 0
        except UnicodeError:
            original_length = 0
        attrs.update(
            {
                "gotel.db.query.text_reason": "analysis_error",
                "gotel.db.query.analysis_status": "error",
                "gotel.db.query.original_length": original_length,
                "gotel.db.query.diagnostic_schema_version": sequel.DIAGNOSTIC_SCHEMA_VERSION,
            }
        )
        return attrs
    attrs.update(
        {
            "db.query.summary": query.summary or "SQL",
            "gotel.db.query.statement_count": query.statement_count,
            "gotel.db.query.analysis_status": query.status,
            "gotel.db.query.original_length": query.original_length,
            "gotel.db.query.diagnostic_schema_version": sequel.DIAGNOSTIC_SCHEMA_VERSION,
        }
    )
    diagnostics: list[str] = []
    diagnostic_bytes = 0
    for item in query.diagnostics:
        try:
            canonical = sequel.canonicalize_diagnostic(
                item, source_length=query.original_length, version=query.schema_version
            )
        except ValueError:
            attrs["gotel.db.query.diagnostic_validation_error"] = True
            continue
        encoded = json.dumps(asdict(canonical), separators=(",", ":"))
        if len(diagnostics) == 64 or diagnostic_bytes + len(encoded.encode()) > MAX_ANALYSIS_ATTRIBUTE_BYTES:
            break
        diagnostics.append(encoded)
        diagnostic_bytes += len(encoded.encode())
    attrs["db.query.diagnostics"] = diagnostics
    if len(diagnostics) < len(query.diagnostics):
        attrs["gotel.db.query.diagnostics_omitted"] = True
    if query.fingerprint:
        attrs["gotel.db.query.fingerprint"] = query.fingerprint
    if query.operation:
        attrs["db.operation.name"] = query.operation
    if query.text:
        limit = (
            options.get("max_query_text_bytes", sequel.DEFAULT_MAX_QUERY_TEXT_BYTES)
            or sequel.DEFAULT_MAX_QUERY_TEXT_BYTES
        )
        display = "sanitized"
        text = query.text
        if query.formatted and (limit < 0 or len(query.formatted.encode()) <= limit):
            text, display = query.formatted, "formatted"
        attrs["db.query.text"] = text
        if not options["omit_source_maps"]:
            # Bound traversal and serialization before asdict can copy a large
            # native map. The encoded byte check still handles large integers.
            segments = len(query.original_to_sanitized.segments) + len(query.sanitized_to_formatted.segments)
            envelope = (
                None
                if segments > MAX_ANALYSIS_ATTRIBUTE_BYTES // 96
                else _json(
                    {
                        "schema_version": query.schema_version,
                        "original_to_sanitized": asdict(query.original_to_sanitized),
                        "sanitized_to_formatted": asdict(query.sanitized_to_formatted),
                        "display": display,
                    }
                )
            )
            if envelope is None:
                attrs["gotel.db.query.source_map_omitted"] = True
            else:
                attrs["gotel.db.query.source_map"] = envelope
    else:
        attrs["gotel.db.query.text_reason"] = query.omission_reason or "empty"
    if not options.get("omit_bind_descriptors", True):
        descriptors = (
            None
            if len(query.binds) > MAX_ANALYSIS_ATTRIBUTE_BYTES // 96
            else _json([asdict(item) for item in query.binds])
        )
        if descriptors is not None:
            attrs["gotel.db.query.binds"] = descriptors
    if query.truncated:
        attrs["gotel.db.query.truncated"] = True
    return attrs


def attributes(sql, dialect, system, namespace, policy):
    """Build safe attributes; system/namespace and bind values are not cached."""
    # A cancellation callback is request-specific and must never be bypassed by
    # cached success. Unknown/unhashable policy objects also skip retention.
    # Retain only plain immutable policy scalars; user objects can own arbitrary
    # allocations that sys.getsizeof cannot account for recursively.
    cacheable = (
        policy.get("cancelled") is None
        and type(sql) is str
        and all(type(value) in {bool, int, str, type(None)} for value in policy.values())
    )
    key = (sql, dialect, tuple(sorted(policy.items())))
    if cacheable:
        try:
            hash(key)
        except TypeError:
            cacheable = False
    result = _cache.get(key) if cacheable else None
    if result is None:
        result = _analyze(sql, dialect, policy)
        if cacheable and result.get("gotel.db.query.analysis_status") not in {"error", "cancelled"}:
            _cache.put(key, result)
    result["db.system.name"] = system
    if namespace:
        result["db.namespace"] = namespace
    return result
