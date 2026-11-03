# SQL analysis and source-owned query names

Gotel uses Sequel's unified analysis in C, Go and Python. A successful query
exports a versioned, dialect-specific `gotel.db.query.fingerprint`. The SHA-256
digest excludes literal values, comments and whitespace while retaining token
boundaries, quoted identifiers and repeated bind relationships. It is a query
shape identity, not a database grammar verdict. Group it with database and
service identity; do not use it as a metric label.

When `db.query.text` is absent, `gotel.db.query.text_reason` explains why:

| Reason | Meaning |
| --- | --- |
| `policy` | Collection policy suppresses query text. |
| `malformed` | Lexical or delimiter analysis failed closed. |
| `analysis_limit` | A per-call input, output, memory or work budget was exceeded. |
| `analysis_error` | Analysis or configuration failed. |
| `cancelled` | The caller cancelled analysis. |
| `empty` | There is no SQL template to collect. |

`gotel.db.query.analysis_status` distinguishes `ok`, `malformed`, `limit`,
`cancelled` and `error`. A truncated template still has text and uses the existing
`gotel.db.query.truncated` boolean rather than an omission reason. Fingerprints
are absent when analysis fails; suppression by policy can retain a fingerprint.

`db.query.diagnostics` remains an array of JSON strings with canonical catalog
prose and original UTF-8 byte ranges. `gotel.db.query.original_length` and
`gotel.db.query.diagnostic_schema_version` let a receiver validate that metadata
without receiving original SQL. Transport keeps at most 64 diagnostics and
16 KiB of diagnostic JSON. `gotel.db.query.diagnostics_omitted=true` marks a
bounded prefix. A validation failure has its own
`gotel.db.query.diagnostic_validation_error` flag.

## Maps and bind descriptors

The compatibility inspection path does not generate source maps or bind
descriptors. Explicit analysis options enable them:

```go
options := &sequel.AnalysisOptions{
    Dialect: sequel.DialectPostgreSQL,
    Format: true,
    // OmitSourceMaps and OmitBindDescriptors are false in this explicit policy.
}
tracer := gotelpgx.New(gotelpgx.Config{Analysis: options})
```

The same `Analysis` option is available on GORM configuration and
`sql.SpanConfig`. `sql.InspectAnalysis` accepts the options by value. Adapter
calls inherit the operation context when the policy does not supply one.
Cancellation is checked before cache lookup and during native analysis, and
cancelled results are not cached.

```python
with namer.query(
    "ReadWidget", sql=statement, system="postgresql",
    include_source_maps=True, include_binds=True,
    max_work_units=100_000, max_memory_bytes=8 << 20,
):
    ...
```

Python also accepts Sequel's `omit_source_maps=False` and
`omit_bind_descriptors=False`. `max_input_bytes`, `max_output_bytes`,
`max_memory_bytes` and `max_work_units` are per-call analysis budgets. Zero uses
Sequel defaults: 1 GiB input/output/memory ceilings and unlimited work.
`max_query_text_bytes` controls template truncation independently from the total
output budget. There is no smaller hidden global SQL input limit.
Ordinary Python analysis/cancellation-callback failures produce `analysis_error`
without exception prose and do not prevent the database call. Task cancellation,
`KeyboardInterrupt` and `SystemExit` still propagate.

C callers zero-initialize `sequel_analysis_options_t`, set the corresponding
`omit_*` fields according to their policy, call `sequel_analyze[_n]`, then call
`gotel_sql_analysis_attributes`. Free the owning result with
`sequel_analysis_free` after attributes are attached. `_with_limit` preserves a
custom text cap when formatting expands the template. Attribute setters copy
their input; the analysis remains caller-owned.

When the caller already knows the byte length, prefer `sequel_analyze_n`: it
preserves that length even if cancellation happens immediately. The C
NUL-terminated wrapper polls cancellation while discovering length; an early
failure reports only the discovered prefix, which can be zero.

`gotel.db.query.source_map` is JSON:

```json
{
  "schema_version": 1,
  "original_to_sanitized": {"from_length": 0, "to_length": 0, "segments": []},
  "sanitized_to_formatted": {"from_length": 0, "to_length": 0, "segments": []},
  "display": "formatted"
}
```

The numeric maps contain no original text. Map diagnostics through the first map
and, when `display` is `formatted`, the second map. Their lengths describe the
actual transported text. Respect Sequel's `Exact` result: replacement/deletion
interiors are approximate anchors, not exact underlines. Never apply original
offsets directly to sanitized text. If the bounded map is unavailable, display
the original location and explanation without an underline.

Map and bind JSON are each bounded to 16 KiB, with a pre-serialization segment
count guard. Oversized maps set `gotel.db.query.source_map_omitted=true`.
`gotel.db.query.binds` contains occurrence ordinal, repeated-marker slot,
statement index, UTF-8 offsets and marker style. It never contains parameter
names, types or values. Sequel's `CheckBinds` / `check_binds` can validate supplied
slot counts without receiving values.

## Inspection cache

Go `sql.ConfigureInspectionCache(maxBytes)` and Python
`sqltrace.configure_inspection_cache(max_bytes)` set the process-wide LRU byte
budget, clear entries and reset counters atomically. The default is 8 MiB; zero
disables retention. Keys include raw SQL and every analysis policy field. The
byte estimate includes retained raw keys, strings, result arrays and entry
overhead; allocator bookkeeping is excluded. Oversized entries bypass retention.
Go clones retained raw-key substrings so a short query cannot retain an
arbitrarily large caller allocation.

`InspectionCacheStats()` / `inspection_cache_stats()` return hits, misses,
evictions, bypasses, entry count, accounted bytes and configured budget. Request
cancellation does not poison the shared cache. Transient analysis errors are not
retained. Returned metadata can be changed without mutating cached results.

## Query naming checks

`github.com/zfogg/gotel/go/querynames` extracts the type-aware checker originally
used by Ratio. Configure `Receivers`, `NamingFunctions`, `DefinitionFiles`,
`CatalogTypes`, `CallerTypes` and exclusions for the application's actual APIs.
The checker handles GORM receiver chains, local aliases, SQLC names in constant
SQL, duplicate definitions, overwritten contexts and caller-built scopes passed
into a generated catalog. It returns execution count plus actionable errors.
`Config.Context` cancels package loading and compiler subprocesses.

Static definition comparison defaults to exact source bodies. Its internal
digests preserve fixed SQL literals, bind-marker spelling/repetition, comments
and whitespace inside literals; only leading SQLC `-- name:` directives and
outer whitespace are excluded. For example, `WHERE key='fill'` and
`WHERE key=$1` are different definitions, as are `'completed'` and `'failed'`.
Bound argument values outside the SQL string are not part of a template's
definition. The comparison is conservative rather than an SQL equivalence
proof. Go `Config.DefinitionIdentity=DefinitionShape` and Python/C
`definition_identity="shape"` deliberately opt into redacted shape comparison
when that is the project's intended policy. These definition digests are lint
metadata and are never emitted as telemetry fingerprints.

`AllowCall` is a narrow per-call policy hook for verified decorators;
`UnchangedForwarder` proves unchanged receiver/SQL/variadic forwarding. Restrict
the receiver type and delegate field in the application before invoking that
proof. Do not exclude an entire executor file to admit one forwarding call.

Python `gotel.querynames.check_python(paths, PythonConfig(...))` uses the standard
AST, lexical names and import aliases. Configure wrappers, naming scopes and
executor methods. `awaited_executors` distinguishes asynchronous ORM calls from
ordinary mapping methods. `allow_dynamic_sql=True` requires a static operation
name while deferring SQL identity to runtime checks. `Report.sites` supplies
name, path, line and target executor for generated catalogs.

`check_c(paths, CConfig(...))` invokes Clang's typed AST. Supply the actual
`clang_args` (`-D`, `-I`, language standard) and `CExecutor` SQL argument indexes.
It resolves evaluated macro catalogs, struct/array members, local aliases and
same-translation-unit forwarding arguments. Compilation failures and unresolved
dynamic SQL require review. The library contains no project-specific C parser.

Both reports expose issues and a checked execution count. Python
`Report.raise_for_issues()` fails standard check commands. Applications should
also assert that the count is nonzero to catch accidental policy exclusions.

Go `querynames.NewRegistry(limit)` and Python `querynames.Registry(limit)` check
runtime name-to-fingerprint consistency for dynamic SQL. `Observe` / `observe`
and `ObserveFingerprint` / `observe_fingerprint` reject one name changing shape
and permit distinct names sharing a fingerprint. A redacted shape cannot prove
that different source operations have duplicate definitions.

`ObserveDefinition` / `observe_definition` explicitly registers a fixed,
literal-preserving source template. It rejects one name changing source body and
by default rejects different names for the same exact body. Go
`NewRegistryWithConfig(RegistryConfig{AllowDuplicateDefinitions:true})` or Python
`Registry(check_duplicate_definitions=False)` permits cross-name exact copies;
same-name consistency still applies. Python's duplicate-definition setting does
not restrict fingerprint-only observations. Use this source-definition API only
for source-owned templates, not SQL with interpolated runtime values.

Registries are concurrent and bounded, retain names/digests without SQL or
argument values, and report capacity exhaustion rather than evict identities.
Callers decide whether to surface naming errors in tests or telemetry; do not
replace driver results. Definition digests must never be exported as the
privacy-redacted `gotel.db.query.fingerprint` attribute.
