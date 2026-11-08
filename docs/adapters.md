# Driver adapters, completion and retention

Native adapters preserve the original driver's arguments, results, exceptions and
cancellation. Query metadata goes through Gotel's SQL inspection path; parameters
are counted without recording their values. Exception messages are excluded.
Optional driver packages are installed by the application.

## Rust

`gotel::tracers` provides optional Cargo features for Axum (`http-axum`), reqwest
(`http-reqwest`), SQLx (`db-sqlx`), rusqlite (`db-rusqlite`), DuckDB (`db-duckdb`)
and redis-rs (`redis`).
See the [Rust adapter guide](../rust/ADAPTERS.md) for versions and examples.

Axum middleware extracts W3C context and names matched routes, including streaming
response completion and cancellation. The reqwest client injects W3C context and
returns original responses. SQLx implements the native executor trait, preserving
typed queries, streams and native transactions; rusqlite forwards execute/batch
and query-row calls. SQL spans use Sequel's sanitization, diagnostics and optional
bounded metadata, with configurable privacy and analysis budgets.

The Redis connection wrapper implements both native sync and async connection
traits, including pipelines and connection managers. It exports a bounded command
vocabulary and timing without command arguments, keys, values, scripts or error
messages. Adapters use application-owned providers, retain original driver
results/errors, and attach context only during synchronous calls or async polling.

## Python

```python
from gotel import clickhousetrace, dbtrace, psycopgtrace, sqltrace

names = sqltrace.Namer("storage.postgres")
psycopgtrace.call_sync("FindAccount", cursor.execute, statement, parameters,
                      namer=names, namespace="accounts")
await psycopgtrace.call("FindAccount", async_cursor.execute, statement, parameters,
                       namer=names)

# Works with native psycopg/asynch/SQLAlchemy context managers. The driver owns
# commit, rollback, error suppression and returned objects.
async with dbtrace.connection_async(pool.connection(), application=application):
    async with dbtrace.transaction_async(connection.transaction(), application=application):
        await psycopgtrace.call("WriteAccount", cursor.execute, statement, parameters)

# A native asynch-compatible subclass, including execute/executemany.
TracedCursor = clickhousetrace.cursor_factory(Cursor, namespace="analytics")
# Mutable clickhouse clients can instead be instrumented and later restored.
remove = clickhousetrace.instrument(client, namespace="analytics")
remove()
```

`policy` on native calls/factories is a dictionary of `Namer.query` inspection
options. `namer` can select an isolated tracer/meter provider. A psycopg Composable
is rendered with the bound cursor's connection only for inspection; the original
object is passed to the driver. Non-inspectable objects produce no fabricated SQL.
Both native adapters honor OpenTelemetry suppression and active SQL scopes.
Insert APIs taking table/data rather than a SQL statement should be traced with
an explicit application-owned query template, rather than treating data as SQL.

`dbtrace.transaction[_async]` records the entire native transaction manager
lifetime, including commit or rollback I/O. `savepoint=True` selects nested
savepoint lifecycle names. `dbtrace.connection[_async]` measures actual pool
acquire wait in seconds and holds a connection lifetime through release. Queries
inside inherit the lifetime context. Lifecycle spans do not claim SQL analysis.
Failed enter never calls exit; native suppression and error identity are retained.
SQLAlchemy's query listeners remain query-only: its commit event fires before
driver commit, so explicit transaction scopes are needed for complete timing.

`gotel.outcomes.Outcome` contains success/error/cancelled/deferred.
`completion(Outcome.DEFERRED)` is an explicit owner decision for resumable work;
it changes telemetry only and never suppresses a timeout or cancellation.

## Go

`outcome.WithWorkBudget` uses a private timeout cause. Only cancellation/deadline
errors caused by that budget become deferred; earlier caller cancellation,
ordinary deadlines and unrelated errors retain their semantics. `Apply` decorates
a span without ending it; `End` classifies and ends it. Neither records messages.
Deferred spans emit `work_budget_exhausted`. Successful database observers retain
the existing `ok` label (`not_found` for GORM), while `operation.outcome` uses
success/error/cancelled/deferred.

`pgx.Config.Outcome` and `gorm.Config.Outcome` accept an application-specific
classification callback. Original driver errors are unchanged. pgx implements
pool AcquireTracer/ReleaseTracer; acquire duration includes failed waits. Release
has no SDK caller context and is an independent instantaneous observation.

PostgreSQL SQLSTATE is extracted through driver error wrappers, including
`pgconn.PrepareError`. Telemetry retains the outer error type while leaving the
original caller error unchanged; database and wrapper messages are excluded.

```go
tx, err := tracer.Begin(ctx, pool) // or BeginTx with pgx.TxOptions
if err != nil { return err }
defer tx.Rollback(cleanupCtx)
if err := write(ctx, tx); err != nil {
    gotelpgx.MarkTransactionOutcome(tx, ctx, err)
    return err
}
return tx.Commit(ctx)
```

The transaction lifetime spans begin before acquisition and end once at commit
or rollback. All pgx Tx query methods inherit the lifetime while keeping the
caller's values, deadline and cancellation. Nested `Begin` creates a savepoint
lifetime. Deferred rollback after commit returns the driver's closed-transaction
error without another lifecycle span. The wrapper owns no background goroutine.
Callers must finish transactions, as required by pgx itself.
`WithinTransaction` optionally owns callback cleanup with a five-second detached
rollback context, including panic propagation. Use manual `Begin` plus
`MarkTransactionOutcome` to retain application-owned cleanup control.

`gorm.WrapTransactions` uses `DB.Conn` to measure acquisition separately from
BEGIN, holds the connection through completion and releases it once. Prepared
transaction wrappers are unwrapped for query parentage. Its matching
`MarkTransactionOutcome` accepts `tx.Statement.ConnPool`. Successful explicit
rollback remains a successful driver operation; owners may mark a callback error
on the lifetime separately. `Retry` is opt-in: positive maximum attempts and an
explicit retryable predicate bound retries; it never guesses idempotence.

GORM query spans retain an explicit operation parent from `Operations.Start`,
including an operation started before the transaction. A child span explicitly
supplied after `Begin` is also retained. An ordinary transaction call using the
original request context (or no active span) is parented to the transaction
lifetime. The native driver keeps the query span already created by the GORM
callback instead of replacing it a second time. Acquisition, BEGIN, COMMIT and
ROLLBACK remain transaction children; caller values, cancellation and work-budget
causes are forwarded unchanged.

## Critical and realtime sampling

Go `sampling.Policy` and Python `sampling.Policy` take an existing delegate,
critical/realtime predicates, a realtime sample rate in [0,1], and a positive
per-second cap. Critical predicates inspect names/attributes supplied at start.
Go `Matcher` extracts reusable name, Temporal-family and operation-class matching
while leaving business strings with the application. Realtime descendants retain
one valid parent's decision. Independent realtime roots receive rate/cap checks.
Injectable clocks support deterministic tests; policy state is safe to share.

Head sampling cannot be changed after a failure is discovered. `Retainer` emits
a separately capped independent error observation linked to the original dropped
context. It does not reconstruct missing spans or claim a complete original
trace. The tracer provider must use `Policy`, which recognizes the retained
observation marker. Successes and already-sampled operations are not retained.
Durations use Go `time.Duration` or Python `duration_ns`; metric labels never
contain trace IDs, query fingerprints, raw SQL or bind values.

## C

`gotel_sqlite_prepare[_v2/_v3]` uses an existing attachment's catalog, namespace
and privacy policy. Success creates no prepare span: PROFILE alone records
execution. Failure records a safe template/diagnostics and SQLite result code,
never `sqlite3_errmsg`. Explicit byte bounds and tail pointers match SQLite.
`gotel_sqlite_prepare_db` locates the attachment from a raw database pointer;
unattached connections call SQLite directly. Attach/detach/close must be serialized
with use of that connection. Duplicate attachment is rejected. CLOSE removes
the registry entry; borrowed catalog/prefix/namespace lifetimes are unchanged.

`gotel_sqlite_options_t.analysis` optionally points to unified Sequel options.
Attach copies the struct; callback/userdata pointers remain borrowed through
detach/close. The pointer itself is needed only during attach. NULL retains the
existing text/format policy and omits source maps and bind descriptors while
recording fingerprints, diagnostics and absence reasons. An explicit policy
selects input/output/memory/work budgets, maps, descriptors and cancellation;
zero limits preserve Sequel's defaults, including the 1 GiB input ceiling.
The adapter forces SQLite dialect. Legacy `sql.omit_query_text` remains a privacy
floor, and a nonzero legacy text limit further bounds the analysis display limit.
Resource failure/cancellation suppresses unsafe metadata and never alters native
SQLite results. Duration metrics use bounded operation/catalog names, without
fingerprints, maps, bind descriptors, query templates or SQL summaries as labels.

`gotel_operation_parent` is a pure copied-context helper: INHERIT preserves the
parent, INDEPENDENT returns an invalid root, and BOUNDED marks a valid parent as
owning one sampling decision across nested realtime stages. It allocates and
locks nothing. `gotel_begin_retained` accepts a non-realtime operation, inherits
sampled parents normally, and makes capped independent linked observations for
dropped parents. It returns false when capped/invalid; call `gotel_end` only after
true. The cap is `realtime_max_per_second`, defaulting to 10/s when zero.
`gotel_outcome` records explicit completion and updates error metrics/status.

## Temporal

Existing Go `New`/`ConfigureClient` and Python `interceptor` continue using the
SDK's replay-aware combined interceptor; register it once on the client. Activity
execution spans include attempt, retry flag, schedule-to-start seconds and IDs.
Original scheduling delay includes retry backoff. Python additionally records
current-attempt queue delay when the SDK supplies it. Negative clock skew is
clamped. `ExecutionLinks` (Go) / `execution_links` (Python) supplies bounded
prior-attempt/execution links; no process-global retry history is retained.
Payloads, heartbeat details, task tokens and exception/status messages are omitted.
Go `WorkflowExecutionLinks` accepts the SDK workflow context for explicit prior
workflow-run links. Workflow attempt/retry and execution IDs decorate existing
SDK observations; replay creates no additional observations. A schedule parent
link identifies scheduling lineage, not a previous retry. Applications must
provide prior-attempt/run contexts explicitly when those links are needed.
