-- name: UpsertSignal :one
WITH upserted AS (
INSERT INTO signals (configuration_id, strategy_id, symbol, ts, direction, conviction, entry, stop, target, rationale, source, asset_type, timeframe, qty, source_votes, portfolio_hash, strategy_config_hash, strategy_evaluation_version, signal_payload)
VALUES (NULLIF(sqlc.arg(configuration_id)::bigint, 0), sqlc.arg('strategy_id'), sqlc.arg('symbol'), sqlc.arg('ts'), sqlc.arg('direction'), sqlc.arg('conviction'), sqlc.arg('entry'), sqlc.arg('stop'), sqlc.arg('target'), sqlc.arg('rationale'), sqlc.arg('source'), sqlc.arg('asset_type'), sqlc.arg('timeframe'), sqlc.arg('qty'), coalesce(sqlc.arg('source_votes'), '[]'::jsonb), sqlc.arg('portfolio_hash'), sqlc.arg('strategy_config_hash'), sqlc.arg('strategy_evaluation_version'), sqlc.arg('signal_payload'))
ON CONFLICT (configuration_id, strategy_id, symbol, ts, timeframe, portfolio_hash, strategy_config_hash, strategy_evaluation_version)
    WHERE configuration_id IS NOT NULL
DO UPDATE
SET direction  = EXCLUDED.direction,
    conviction = EXCLUDED.conviction,
    entry      = EXCLUDED.entry,
    stop       = EXCLUDED.stop,
    target     = EXCLUDED.target,
    rationale  = EXCLUDED.rationale,
    source     = EXCLUDED.source,
    asset_type = EXCLUDED.asset_type,
    qty        = EXCLUDED.qty,
    source_votes = EXCLUDED.source_votes,
    signal_payload = EXCLUDED.signal_payload
WHERE (signals.direction, signals.conviction, signals.entry, signals.stop, signals.target,
       signals.rationale, signals.source, signals.asset_type, signals.qty, signals.source_votes, signals.signal_payload)
 IS DISTINCT FROM
      (EXCLUDED.direction, EXCLUDED.conviction, EXCLUDED.entry, EXCLUDED.stop, EXCLUDED.target,
       EXCLUDED.rationale, EXCLUDED.source, EXCLUDED.asset_type, EXCLUDED.qty, EXCLUDED.source_votes, EXCLUDED.signal_payload)
RETURNING id, strategy_id, symbol, ts, direction, conviction, entry, stop, target, rationale, source, created_at, asset_type, timeframe, qty, source_votes, portfolio_hash, strategy_config_hash, strategy_evaluation_version, signal_payload
)
SELECT * FROM upserted
UNION ALL
SELECT s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target,
       s.rationale, s.source, s.created_at, s.asset_type, s.timeframe, s.qty, s.source_votes,
       s.portfolio_hash, s.strategy_config_hash, s.strategy_evaluation_version, s.signal_payload
FROM signals s
WHERE NOT EXISTS (SELECT 1 FROM upserted)
  AND s.configuration_id = COALESCE(NULLIF(sqlc.arg(configuration_id)::bigint, 0),
      (SELECT id FROM tradebot_configurations WHERE config_hash = 'legacy:' || sqlc.arg('asset_type')::text || ':' || sqlc.arg('timeframe')::text))
  AND s.strategy_id = sqlc.arg('strategy_id') AND s.symbol = sqlc.arg('symbol') AND s.ts = sqlc.arg('ts') AND s.timeframe = sqlc.arg('timeframe')
  AND s.portfolio_hash = sqlc.arg('portfolio_hash') AND s.strategy_config_hash = sqlc.arg('strategy_config_hash') AND s.strategy_evaluation_version = sqlc.arg('strategy_evaluation_version');
-- name: UpsertSignalsBatch :execrows
INSERT INTO signals (configuration_id, strategy_id, symbol, ts, direction, conviction, entry, stop, target, rationale, source, asset_type, timeframe, qty, source_votes, portfolio_hash, strategy_config_hash, strategy_evaluation_version, signal_payload)
SELECT unnest(sqlc.arg(configuration_ids)::bigint[]),
       unnest(sqlc.arg(strategy_ids)::bigint[]),
       unnest(sqlc.arg(symbols)::text[]),
       unnest(sqlc.arg(timestamps)::timestamptz[]),
       unnest(sqlc.arg(directions)::text[]),
       unnest(sqlc.arg(convictions)::double precision[]),
       unnest(sqlc.arg(entries)::double precision[]),
       unnest(sqlc.arg(stops)::double precision[]),
       unnest(sqlc.arg(targets)::double precision[]),
       nullif(unnest(sqlc.arg(rationales)::text[]), ''),
       nullif(unnest(sqlc.arg(sources)::text[]), ''),
       unnest(sqlc.arg(asset_types)::text[]),
       unnest(sqlc.arg(timeframes)::text[]),
       nullif(unnest(sqlc.arg(qtys)::double precision[]), 0),
       unnest(sqlc.arg(source_votes)::jsonb[]),
       unnest(sqlc.arg(portfolio_hashes)::text[]),
       unnest(sqlc.arg(strategy_config_hashes)::text[]),
       unnest(sqlc.arg(strategy_evaluation_versions)::int[]),
       unnest(sqlc.arg(signal_payloads)::jsonb[])
ON CONFLICT (configuration_id, strategy_id, symbol, ts, timeframe, portfolio_hash, strategy_config_hash, strategy_evaluation_version)
    WHERE configuration_id IS NOT NULL
DO UPDATE
SET direction  = EXCLUDED.direction,
    conviction = EXCLUDED.conviction,
    entry      = EXCLUDED.entry,
    stop       = EXCLUDED.stop,
    target     = EXCLUDED.target,
    rationale  = EXCLUDED.rationale,
    source     = EXCLUDED.source,
    asset_type = EXCLUDED.asset_type,
    qty        = EXCLUDED.qty,
    source_votes = EXCLUDED.source_votes,
    signal_payload = EXCLUDED.signal_payload
WHERE (signals.direction, signals.conviction, signals.entry, signals.stop, signals.target,
       signals.rationale, signals.source, signals.asset_type, signals.qty, signals.source_votes, signals.signal_payload)
 IS DISTINCT FROM
      (EXCLUDED.direction, EXCLUDED.conviction, EXCLUDED.entry, EXCLUDED.stop, EXCLUDED.target,
       EXCLUDED.rationale, EXCLUDED.source, EXCLUDED.asset_type, EXCLUDED.qty, EXCLUDED.source_votes, EXCLUDED.signal_payload);

-- name: UpsertSignalPortfolioSnapshot :exec
INSERT INTO signal_portfolio_snapshots (
    portfolio_hash, hash_version, timeframe, asset_type,
    tradeable_symbols, context_symbols, canonical_snapshot
) VALUES (sqlc.arg('portfolio_hash'), sqlc.arg('hash_version'), sqlc.arg('timeframe'), sqlc.arg('asset_type'), sqlc.arg('tradeable_symbols'), sqlc.arg('context_symbols'), sqlc.arg('canonical_snapshot'))
ON CONFLICT (portfolio_hash) DO NOTHING;

-- name: UpsertSignalStrategyConfigSnapshot :exec
INSERT INTO signal_strategy_config_snapshots (
    strategy_config_hash, hash_version, strategy_evaluation_version, canonical_config
) VALUES (sqlc.arg('strategy_config_hash'), sqlc.arg('hash_version'), sqlc.arg('strategy_evaluation_version'), sqlc.arg('canonical_config'))
ON CONFLICT (strategy_config_hash) DO NOTHING;

-- name: GetSignal :one
SELECT id, strategy_id, symbol, ts, direction, conviction, entry, stop, target, rationale, source, created_at, asset_type, timeframe, qty, source_votes
FROM signals
WHERE id = sqlc.arg('id');

-- name: ListSignals :many
SELECT id, strategy_id, symbol, ts, direction, conviction, entry, stop, target, rationale, source, created_at, asset_type, timeframe, qty, source_votes
FROM signals
WHERE strategy_id = sqlc.arg('strategy_id')
  AND symbol = sqlc.arg('symbol')
  AND ts >= sqlc.arg('from')
  AND ts <= sqlc.arg('through')
ORDER BY ts ASC;

-- name: UpsertSignalOutcome :one
INSERT INTO signal_outcomes (signal_id, graded_at, r_multiple, hit, horizon_days)
VALUES (sqlc.arg('signal_id'), sqlc.arg('graded_at'), sqlc.arg('r_multiple'), sqlc.arg('hit'), sqlc.arg('horizon_days'))
ON CONFLICT (signal_id, horizon_days) DO UPDATE
SET graded_at  = EXCLUDED.graded_at,
    r_multiple = EXCLUDED.r_multiple,
    hit        = EXCLUDED.hit
RETURNING id, signal_id, graded_at, r_multiple, hit, horizon_days;

-- name: BatchUpsertSignalOutcome :exec
INSERT INTO signal_outcomes (signal_id, graded_at, r_multiple, hit, horizon_days)
VALUES (sqlc.arg('signal_id'), sqlc.arg('graded_at'), sqlc.arg('r_multiple'), sqlc.arg('hit'), sqlc.arg('horizon_days'))
ON CONFLICT (signal_id, horizon_days) DO UPDATE
SET graded_at  = EXCLUDED.graded_at,
    r_multiple = EXCLUDED.r_multiple,
    hit        = EXCLUDED.hit;

-- name: GetSignalOutcome :one
SELECT id, signal_id, graded_at, r_multiple, hit, horizon_days
FROM signal_outcomes
WHERE signal_id = sqlc.arg('signal_id') AND horizon_days = sqlc.arg('horizon_days');

-- name: ListUngradedSignalsAtHorizon :many
-- Keep each scheduled grading activity bounded. Later pipeline runs pick up
-- the remaining oldest signals instead of monopolizing the shared DB pool.
SELECT s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target, s.rationale, s.source, s.created_at, s.asset_type, s.timeframe, s.qty, s.source_votes
FROM signals s
JOIN signal_grading_pending p ON p.signal_id = s.id AND p.horizon_days = sqlc.arg('horizon_days')
WHERE p.retry_at <= now()
  AND p.strategy_id = sqlc.arg('strategy_id')
  AND p.symbol = sqlc.arg('symbol')
  AND p.ts <= sqlc.arg('ts')
ORDER BY p.retry_at ASC, p.ts ASC, p.signal_id ASC
LIMIT 25;

-- name: ListUngradedSignalsAtHorizonBatch :many
SELECT s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target, s.rationale, s.source, s.created_at, s.asset_type, s.timeframe, s.qty, s.source_votes
FROM signals s
LEFT JOIN signal_outcomes o ON o.signal_id = s.id AND o.horizon_days = sqlc.arg('horizon_days')
WHERE o.id IS NULL
  AND s.strategy_id = ANY(sqlc.arg('strategy_ids')::bigint[])
  AND s.symbol = ANY(sqlc.arg('symbols')::text[])
  AND s.ts <= sqlc.arg('through')
ORDER BY s.ts ASC;

-- name: ListOutcomesForStrategyByHorizon :many
SELECT o.signal_id, o.graded_at, o.r_multiple, o.hit, o.horizon_days, o.exit_reason, o.mae_r, o.mfe_r, s.symbol, s.ts, s.direction, s.entry, s.stop
FROM signal_outcomes o
JOIN signals s ON s.id = o.signal_id
WHERE s.strategy_id = sqlc.arg('strategyID')
ORDER BY s.ts ASC;

-- name: StrategyAggByDirectionHorizon :many
SELECT s.direction, o.horizon_days,
       count(*)                                AS graded,
       sum(case when o.hit then 1 else 0 end)  AS hits,
       coalesce(sum(o.r_multiple), 0)           AS sum_r,
       sum(case when o.exit_reason = 'stop' then 1 else 0 end)     AS stops,
       sum(case when o.exit_reason = 'target' then 1 else 0 end)   AS targets,
       sum(case when o.exit_reason = 'timed_out' then 1 else 0 end) AS timedouts,
       coalesce(sum(o.mae_r), 0)  AS sum_mae,
       coalesce(sum(o.mfe_r), 0)  AS sum_mfe
FROM signal_outcomes o
JOIN signals s ON s.id = o.signal_id
WHERE o.strategy_id = sqlc.arg('strategyID')
GROUP BY s.direction, o.horizon_days
ORDER BY s.direction, o.horizon_days;

-- name: StrategyAggByDirectionHorizonBatch :many
SELECT o.strategy_id, s.direction, o.horizon_days,
       count(*)                                AS graded,
       sum(case when o.hit then 1 else 0 end)  AS hits,
       coalesce(sum(o.r_multiple), 0)           AS sum_r,
       sum(case when o.exit_reason = 'stop' then 1 else 0 end)     AS stops,
       sum(case when o.exit_reason = 'target' then 1 else 0 end)   AS targets,
       sum(case when o.exit_reason = 'timed_out' then 1 else 0 end) AS timedouts,
       coalesce(sum(o.mae_r), 0)  AS sum_mae,
       coalesce(sum(o.mfe_r), 0)  AS sum_mfe
FROM signal_outcomes o
JOIN signals s ON s.id = o.signal_id
WHERE o.strategy_id = ANY(sqlc.arg('strategy_ids')::bigint[])
GROUP BY o.strategy_id, s.direction, o.horizon_days
ORDER BY o.strategy_id, s.direction, o.horizon_days;

-- name: RecentOutcomesForStrategyBounded :many
SELECT o.signal_id, o.graded_at, o.r_multiple, o.hit, o.horizon_days, o.exit_reason, o.mae_r, o.mfe_r, s.symbol, s.ts, s.direction
FROM signal_outcomes o
JOIN signals s ON s.id = o.signal_id
WHERE o.strategy_id = sqlc.arg('strategy_id')
ORDER BY s.ts DESC
LIMIT sqlc.arg('limit');

-- name: RecentOutcomesByStrategy :many
SELECT o.signal_id, o.graded_at, o.r_multiple, o.hit, o.horizon_days, o.exit_reason, o.mae_r, o.mfe_r, s.symbol, s.ts, s.direction
FROM signal_outcomes o
JOIN signals s ON s.id = o.signal_id
WHERE s.strategy_id = sqlc.arg('strategy_id') AND o.horizon_days = sqlc.arg('horizon_days')
ORDER BY s.ts DESC
LIMIT sqlc.arg('limit');

-- name: OutcomesByStrategyAndSymbol :many
SELECT o.signal_id, o.r_multiple, o.hit, o.exit_reason, o.mae_r, o.mfe_r, s.symbol, s.direction
FROM signal_outcomes o
JOIN signals s ON s.id = o.signal_id
WHERE s.strategy_id = sqlc.arg('strategy_id') AND o.horizon_days = sqlc.arg('horizon_days')
ORDER BY s.ts DESC;

-- name: GetLastEvaluation :one
SELECT evaluated_at FROM signal_evaluations
WHERE strategy_id = sqlc.arg('strategy_id') AND symbol = sqlc.arg('symbol') AND timeframe = sqlc.arg('timeframe');

-- name: GetLastEvaluations :many
SELECT strategy_id, symbol, timeframe, evaluated_at FROM signal_evaluations;

-- name: UpsertEvaluation :exec
INSERT INTO signal_evaluations (strategy_id, symbol, timeframe, evaluated_at)
VALUES (sqlc.arg('strategy_id'), sqlc.arg('symbol'), sqlc.arg('timeframe'), sqlc.arg('evaluated_at'))
ON CONFLICT (strategy_id, symbol, timeframe)
    WHERE configuration_id IS NULL
DO UPDATE
SET evaluated_at = EXCLUDED.evaluated_at;

-- name: UpsertEvaluationsBatch :exec
INSERT INTO signal_evaluations (strategy_id, symbol, timeframe, evaluated_at)
SELECT unnest(sqlc.arg(strategy_ids)::bigint[]),
       unnest(sqlc.arg(symbols)::text[]),
       unnest(sqlc.arg(timeframes)::text[]),
       unnest(sqlc.arg(evaluated_ats)::timestamptz[])
ON CONFLICT (strategy_id, symbol, timeframe)
    WHERE configuration_id IS NULL
DO UPDATE
SET evaluated_at = EXCLUDED.evaluated_at;

-- name: ListSignalsFiltered :many
SELECT s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target,
       s.rationale, s.source, s.created_at, s.asset_type, s.timeframe, s.qty, s.source_votes,
       st.name AS strategy_name
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.ts >= sqlc.arg('from') AND s.ts <= sqlc.arg('through')
  AND (sqlc.arg('asset_type') = '' OR s.asset_type = sqlc.arg('asset_type'))
  AND (sqlc.arg('symbols_csv') = '' OR s.symbol = ANY(string_to_array(sqlc.arg('symbols_csv'), ',')))
  AND (sqlc.arg('timeframe') = '' OR s.timeframe = sqlc.arg('timeframe'))
  AND (sqlc.arg('strategy_names_csv') = '' OR st.name = ANY(string_to_array(sqlc.arg('strategy_names_csv'), ',')))
ORDER BY s.ts DESC
LIMIT sqlc.arg('limit');

-- name: ListSignalsFilteredByStrategyID :many
SELECT s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target,
       s.rationale, s.source, s.created_at, s.asset_type, s.timeframe, s.qty, s.source_votes,
       st.name AS strategy_name
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.strategy_id = sqlc.arg(strategy_id)
  AND s.ts >= sqlc.arg(signal_from) AND s.ts <= sqlc.arg(signal_to)
  AND (sqlc.arg(asset_type)::text = '' OR s.asset_type = sqlc.arg(asset_type)::text)
  AND (sqlc.arg(symbols)::text = '' OR s.symbol = ANY(string_to_array(sqlc.arg(symbols)::text, ',')))
  AND (sqlc.arg(timeframe)::text = '' OR s.timeframe = sqlc.arg(timeframe)::text)
ORDER BY s.ts DESC
LIMIT sqlc.arg(row_limit);

-- name: ConvergenceBars :many
SELECT s.ts, s.symbol, COUNT(DISTINCT st.name) AS strategy_count,
       ARRAY_AGG(DISTINCT st.name ORDER BY st.name) AS strategies
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.ts >= sqlc.arg('from') AND s.ts <= sqlc.arg('through')
  AND s.direction <> 'flat' AND s.direction <> ''
  AND (sqlc.arg('timeframe') = '' OR s.timeframe = sqlc.arg('timeframe'))
GROUP BY s.ts, s.symbol
HAVING COUNT(DISTINCT st.name) >= sqlc.arg('min_strategy_count')::int
ORDER BY s.ts;

-- name: StrategyConfluenceBars :many
-- Finds timestamps where the same strategy fires on 2+ symbols
-- within the same asset class, bucketed into sqlc.arg('bucket_seconds')-second time windows.
WITH bucketed AS (
  SELECT
    (date_trunc('minute', s.ts) - (EXTRACT(minute FROM s.ts)::int % (sqlc.arg('bucket_seconds')::int / 60)) * interval '1 minute')::timestamptz AS bucket,
    st.name AS strategy,
    s.asset_type,
    s.symbol,
    s.direction,
    s.conviction
  FROM signals s
  JOIN strategies st ON st.id = s.strategy_id
  WHERE s.ts >= sqlc.arg('from') AND s.ts <= sqlc.arg('through')
    AND s.direction <> 'flat' AND s.direction <> ''
    AND (sqlc.arg('timeframe') = '' OR s.timeframe = sqlc.arg('timeframe'))
),
clustered AS (
  SELECT bucket, strategy, asset_type,
         COUNT(DISTINCT symbol) AS symbol_count,
         MODE() WITHIN GROUP (ORDER BY direction) AS majority_dir,
         AVG(conviction) AS avg_conviction
  FROM bucketed
  GROUP BY bucket, strategy, asset_type
  HAVING COUNT(DISTINCT symbol) >= 2
)
SELECT c.bucket AS ts, c.symbol_count,
       c.strategy, c.asset_type,
       c.majority_dir AS direction, c.avg_conviction AS conviction,
       b.symbol
FROM clustered c
JOIN bucketed b ON b.bucket = c.bucket AND b.strategy = c.strategy AND b.asset_type = c.asset_type
ORDER BY c.symbol_count DESC, c.bucket, b.symbol;

-- name: SignalsByBar :many
SELECT s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target,
       s.rationale, s.source, s.created_at, s.asset_type, s.timeframe, s.qty, s.source_votes,
       st.name AS strategy_name
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.direction <> 'flat'
  AND s.ts = ANY(sqlc.arg('bar_times')::timestamptz[])
  AND s.symbol = ANY(sqlc.arg('symbols')::text[]);

-- name: ListOutcomesFiltered :many
SELECT o.signal_id, o.graded_at, o.r_multiple, o.hit, o.horizon_days, o.exit_reason, o.mae_r, o.mfe_r,
       s.symbol, s.ts, s.direction, s.entry, s.stop, s.conviction, s.asset_type, s.timeframe,
       st.name AS strategy_name
FROM signal_outcomes o
JOIN signals s ON s.id = o.signal_id
JOIN strategies st ON st.id = s.strategy_id
WHERE s.ts >= sqlc.arg('from') AND s.ts <= sqlc.arg('through')
  AND (sqlc.arg('asset_type') = '' OR s.asset_type = sqlc.arg('asset_type'))
  AND (sqlc.arg('symbols_csv') = '' OR s.symbol = ANY(string_to_array(sqlc.arg('symbols_csv'), ',')))
  AND (sqlc.arg('timeframe') = '' OR s.timeframe = sqlc.arg('timeframe'))
  AND (sqlc.arg('strategy_names_csv') = '' OR st.name = ANY(string_to_array(sqlc.arg('strategy_names_csv'), ',')))
ORDER BY s.ts DESC
LIMIT sqlc.arg('limit');

-- name: UpsertSignalNote :one
INSERT INTO signal_notes (signal_id, symbol, summary, stance, confidence, catalysts, sources)
VALUES (sqlc.arg('signal_id'), sqlc.arg('symbol'), sqlc.arg('summary'), sqlc.arg('stance'), sqlc.arg('confidence'), sqlc.arg('catalysts'), sqlc.arg('sources'))
ON CONFLICT (signal_id) DO UPDATE
SET symbol     = EXCLUDED.symbol,
    summary    = EXCLUDED.summary,
    stance     = EXCLUDED.stance,
    confidence = EXCLUDED.confidence,
    catalysts  = EXCLUDED.catalysts,
    sources    = EXCLUDED.sources,
    created_at = now()
RETURNING signal_id, symbol, summary, stance, confidence, catalysts, sources, created_at;

-- name: GetSignalNote :one
SELECT signal_id, symbol, summary, stance, confidence, catalysts, sources, created_at
FROM signal_notes
WHERE signal_id = sqlc.arg('signalID');

-- name: GetSignalNotesByIDs :many
SELECT signal_id, symbol, summary, stance, confidence, catalysts, sources, created_at
FROM signal_notes
WHERE signal_id = ANY(sqlc.arg('signal_ids')::bigint[]);

-- name: GetSignalOutcomesBySignalIDs :many
SELECT signal_id, r_multiple, hit, horizon_days, exit_reason
FROM signal_outcomes
WHERE signal_id = ANY(sqlc.arg('signal_ids')::bigint[]);

-- name: LatestDirectionalSignal :one
SELECT id, strategy_id, symbol, ts, direction, conviction, entry, stop, target, rationale, source, created_at, timeframe, qty, source_votes, configuration_id
FROM signals
WHERE strategy_id = sqlc.arg('strategy_id')
  AND symbol = sqlc.arg('symbol')
  AND direction <> 'flat' AND direction <> ''
  AND timeframe = sqlc.arg('timeframe')
ORDER BY ts DESC
LIMIT 1;

-- name: LatestDirectionalSignalsForCombos :many
WITH requested_combos AS (
    SELECT unnest(sqlc.arg(strategy_ids)::bigint[]) AS strategy_id,
           unnest(sqlc.arg(symbols)::text[]) AS symbol,
           unnest(sqlc.arg(timeframes)::text[]) AS timeframe
), requested AS (
    SELECT DISTINCT strategy_id, symbol, timeframe FROM requested_combos
)
SELECT
  s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target,
  s.rationale, s.source, s.created_at, s.timeframe, s.qty, s.source_votes, s.configuration_id
FROM requested r
JOIN LATERAL (
  SELECT * FROM signals s
  WHERE s.strategy_id = r.strategy_id AND s.symbol = r.symbol AND s.timeframe = r.timeframe
    AND s.direction <> 'flat' AND s.direction <> ''
  ORDER BY s.ts DESC
  LIMIT 1
) s ON true
ORDER BY s.strategy_id, s.symbol, s.timeframe;

-- name: LatestDirectionalSignalsByStrategy :many
SELECT DISTINCT ON (symbol, timeframe)
  id, strategy_id, symbol, ts, direction, conviction, entry, stop, target, rationale, source, created_at, timeframe, qty, source_votes, configuration_id
FROM signals
WHERE strategy_id = sqlc.arg('strategyID')
  AND direction <> 'flat' AND direction <> ''
ORDER BY symbol, timeframe, ts DESC;

-- name: LatestDirectionalSignalsForScopedCombos :many
WITH requested AS (
    SELECT unnest(sqlc.arg(strategy_ids)::bigint[]) AS strategy_id,
           unnest(sqlc.arg(symbols)::text[]) AS symbol,
           unnest(sqlc.arg(timeframes)::text[]) AS timeframe,
           unnest(sqlc.arg(strategy_config_hashes)::text[]) AS strategy_config_hash
)
SELECT s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target,
  s.rationale, s.source, s.created_at, s.timeframe, s.qty, s.source_votes, s.configuration_id
FROM requested r
JOIN LATERAL (
  SELECT s.id, s.strategy_id, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target,
    s.rationale, s.source, s.created_at, s.timeframe, s.qty, s.source_votes, s.configuration_id
  FROM signals s
  WHERE s.portfolio_hash = sqlc.arg(portfolio_hash)
    -- Scoped callers require nonempty provenance. State the partial-index
    -- predicate explicitly: generic prepared plans cannot infer it from $1.
    AND s.portfolio_hash <> ''
    AND s.strategy_evaluation_version = sqlc.arg(evaluation_version)
    AND s.strategy_config_hash = r.strategy_config_hash
    AND s.strategy_id = r.strategy_id
    AND s.symbol = r.symbol
    AND s.timeframe = r.timeframe
    AND s.direction <> 'flat' AND s.direction <> ''
  ORDER BY s.ts DESC, s.id DESC
  LIMIT 1
) s ON true;

-- name: LatestDirectionalSignalsBySymbol :many
SELECT DISTINCT ON (s.strategy_id, s.timeframe)
  s.id, s.strategy_id, st.name as strategy_name, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target, s.rationale, s.source, s.created_at, s.timeframe, s.qty, s.source_votes, s.configuration_id
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = sqlc.arg('symbol')
  AND s.timeframe = sqlc.arg('timeframe')
  AND s.direction <> 'flat' AND s.direction <> ''
ORDER BY s.strategy_id, s.timeframe, s.ts DESC;

-- name: RecentDirectionalSignals :many
SELECT st.name as strategy_name, s.symbol, s.direction, s.conviction, s.entry, s.stop, s.target, s.ts, s.created_at, s.timeframe, s.id, s.qty, s.rationale
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.direction <> 'flat'
  AND s.timeframe = sqlc.arg('timeframe')
  AND (s.notified IS NULL OR s.notified != sqlc.arg('notified'))
ORDER BY s.ts DESC;

-- name: LatestConfluenceRuleRationale :one
SELECT s.rationale
FROM strategies st
-- Bound each strategy version's indexed scan before selecting the newest
-- overall. A joined LIMIT otherwise sorts every historical matching signal.
CROSS JOIN LATERAL (
  SELECT rationale, ts
  FROM signals
  WHERE strategy_id = st.id
    AND asset_type = 'equity'
    AND timeframe = sqlc.arg('timeframe')
    AND rationale LIKE '%SOXX=%'
  ORDER BY ts DESC
  LIMIT 1
) s
WHERE st.name = 'strategy_confluence'
ORDER BY s.ts DESC
LIMIT 1;

-- name: MarkSignalsNotified :exec
UPDATE signals SET notified = sqlc.arg('notified') WHERE id = ANY(sqlc.arg('signal_ids')::bigint[]);

-- name: SetReplaySignalConfiguration :exec
-- Used only inside the always-rollback historical TradeBot audit transaction.
UPDATE signals
SET configuration_id = sqlc.arg('configuration_id')
WHERE strategy_id = sqlc.arg('strategy_id')
  AND source = sqlc.arg('source')
  AND ts = sqlc.arg('ts')
  AND direction = sqlc.arg('direction');

-- name: BatchLatestSignalsBySymbol :many
-- Fetches latest directional signals for multiple symbols and timeframes.
-- Returns one row per (symbol, strategy, timeframe) combination.
SELECT DISTINCT ON (s.symbol, s.strategy_id, s.timeframe)
  s.id, s.strategy_id, st.name as strategy_name, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target, s.rationale, s.source, s.created_at, s.timeframe, s.qty, s.source_votes, s.configuration_id
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = ANY(sqlc.arg('symbols')::text[])
  AND s.timeframe = ANY(sqlc.arg('timeframes')::text[])
  AND s.direction <> 'flat'
  AND (Cardinality(sqlc.arg('strategy_names')::text[]) = 0 OR st.name = ANY(sqlc.arg('strategy_names')::text[]))
ORDER BY s.symbol, s.strategy_id, s.timeframe, s.ts DESC, s.id DESC;

-- name: BatchLatestSignalsAtBySymbol :many
-- Historical/no-write parity audit form of BatchLatestSignalsBySymbol.
-- The cutoff also makes delayed/retried completed-bar execution deterministic:
-- signals generated after the immutable decision bar cannot shadow that bar.
SELECT DISTINCT ON (s.symbol, s.strategy_id, s.timeframe)
  s.id, s.strategy_id, st.name as strategy_name, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target, s.rationale, s.source, s.created_at, s.timeframe, s.qty, s.source_votes, s.configuration_id
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = ANY(sqlc.arg('symbols')::text[])
  AND s.timeframe = ANY(sqlc.arg('timeframes')::text[])
  AND s.direction <> 'flat'
  AND (Cardinality(sqlc.arg('strategy_names')::text[]) = 0 OR st.name = ANY(sqlc.arg('strategy_names')::text[]))
  AND s.ts <= sqlc.arg('ts')
ORDER BY s.symbol, s.strategy_id, s.timeframe, s.ts DESC, s.id DESC;

-- name: BatchLatestScopedSignalsAtBySymbol :many
-- Canonical execution query: scope before DISTINCT so a newer foreign-scope
-- row can never shadow the current portfolio/configuration signal.
SELECT DISTINCT ON (s.symbol, s.strategy_id, s.timeframe)
  s.id, s.strategy_id, st.name as strategy_name, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target, s.rationale, s.source, s.created_at, s.asset_type, s.timeframe, s.qty, s.source_votes, s.configuration_id,
  s.portfolio_hash, s.strategy_config_hash, s.strategy_evaluation_version
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = ANY(sqlc.arg(symbols)::text[])
  AND s.timeframe = ANY(sqlc.arg(timeframes)::text[])
  AND s.direction <> 'flat'
  AND (Cardinality(sqlc.arg(strategy_names)::text[]) = 0 OR st.name = ANY(sqlc.arg(strategy_names)::text[]))
  AND s.portfolio_hash = sqlc.arg(portfolio_hash)::text
  AND s.strategy_config_hash = ANY(sqlc.arg(strategy_config_hashes)::text[])
  AND s.strategy_evaluation_version = sqlc.arg(strategy_evaluation_version)::int
  AND s.ts <= sqlc.arg(as_of)::timestamptz
ORDER BY s.symbol, s.strategy_id, s.timeframe, s.ts DESC, s.id DESC;

-- name: BatchBestSignalBySymbol :many
-- Fetches the single best signal per symbol (highest timeframe, most recent).
-- Used by the exit phase which only needs the best signal per symbol.
SELECT DISTINCT ON (s.symbol)
  s.id, s.strategy_id, st.name as strategy_name, s.symbol, s.ts, s.direction, s.conviction, s.entry, s.stop, s.target, s.rationale, s.source, s.created_at, s.timeframe, s.qty, s.source_votes, s.configuration_id
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = ANY(sqlc.arg('symbols')::text[])
  AND s.timeframe = ANY(sqlc.arg('timeframes')::text[])
  AND s.direction <> 'flat'
ORDER BY s.symbol, s.timeframe DESC, s.ts DESC;

-- name: RecentMarketMapSignals :many
-- Fetches directional signals created in the requested wall-clock window.
-- Market-map linger rules are applied by the API because each timeframe has a
-- different lifetime.
SELECT s.id,
       st.name AS strategy_name,
       s.symbol,
       coalesce(s.asset_type, '') AS asset_type,
       s.timeframe,
       s.direction,
       coalesce(s.conviction, 0)::float8 AS conviction,
       coalesce(s.entry, 0)::float8 AS entry,
       coalesce(s.stop, 0)::float8 AS stop,
       coalesce(s.target, 0)::float8 AS target,
       coalesce(s.qty, 0)::float8 AS qty,
       coalesce(s.rationale, '') AS rationale,
       coalesce(s.source, '') AS source,
       s.ts,
       s.created_at
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
  WHERE s.symbol = ANY(sqlc.arg(symbols)::text[])
    AND s.created_at >= sqlc.arg(created_from)
    AND s.created_at <= sqlc.arg(created_to)
    -- Active map signals must be fresh in both processing time and market time.
    -- The market-time predicate lets Postgres use idx_signals_ts_tf instead of
    -- scanning historical backfills by their unindexed created_at timestamp.
    AND s.ts >= sqlc.arg(created_from)
    AND s.ts <= sqlc.arg(created_to)
    AND (sqlc.arg(timeframe)::text = '' OR s.timeframe = sqlc.arg(timeframe)::text)
  AND s.direction <> 'flat'
  AND s.direction <> ''
ORDER BY s.created_at DESC, s.id DESC;

-- name: MarketChartSignals :many
-- Fetches directional signals whose market timestamp is inside the visible
-- chart-history window. This deliberately differs from the market-map query,
-- which uses created_at to implement live notification linger.
SELECT s.id,
       st.name AS strategy_name,
       s.symbol,
       coalesce(s.asset_type, '') AS asset_type,
       s.timeframe,
       s.direction,
       coalesce(s.conviction, 0)::float8 AS conviction,
       coalesce(s.entry, 0)::float8 AS entry,
       coalesce(s.stop, 0)::float8 AS stop,
       coalesce(s.target, 0)::float8 AS target,
       coalesce(s.qty, 0)::float8 AS qty,
       coalesce(s.rationale, '') AS rationale,
       coalesce(s.source, '') AS source,
       s.ts,
       s.created_at
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = sqlc.arg(symbol)
  AND s.ts >= sqlc.arg(signal_from)
  AND s.ts <= sqlc.arg(signal_to)
  AND (sqlc.arg(timeframe)::text = '' OR s.timeframe = sqlc.arg(timeframe)::text)
  AND s.direction <> 'flat'
  AND s.direction <> ''
ORDER BY s.ts DESC, s.id DESC;

-- name: SelectMeanReversionPerformance :many
-- Recent mean_reversion signal performance for dynamic portfolio scoring.
-- Used as a leading indicator: when MR avg_r turns positive after a bad period,
-- the symbol is typically 1-2 weeks away from a broader breakout run.
-- Returns avg_r, win_rate, total_r, and outcome count per symbol for the
-- mean_reversion strategy over the lookback window.
SELECT sig.symbol,
       COUNT(out.id)::int AS outcomes,
       COALESCE(AVG(out.r_multiple), 0)::float8 AS avg_r,
       COALESCE(AVG((out.r_multiple > 0)::int), 0)::float8 AS win_rate,
       COALESCE(SUM(out.r_multiple), 0)::float8 AS total_r
FROM signals sig
JOIN signal_outcomes out ON out.signal_id = sig.id
JOIN strategies s ON s.id = sig.strategy_id
WHERE s.name = 'mean_reversion'
  AND sig.asset_type = 'equity'
  AND sig.direction IN ('long', 'short')
  AND sig.ts >= sqlc.arg(since)::timestamptz
  AND sig.ts < sqlc.arg(until)::timestamptz
GROUP BY sig.symbol
HAVING COUNT(out.id) >= sqlc.arg(min_outcomes)::int
ORDER BY avg_r DESC;

-- name: SelectStrategyPerformance :many
-- Recent performance for all base strategies for dynamic portfolio scoring.
-- Returns avg_r, win_rate, total_r, and outcome count per symbol for strategies:
-- mean_reversion, breakout, vwap_reversion, trailing_stop.
-- Used to assess recent regime performance and identify turnaround candidates.
SELECT s.name AS strategy_name,
       sig.symbol,
       COUNT(out.id)::int AS outcomes,
       COALESCE(AVG(out.r_multiple), 0)::float8 AS avg_r,
       COALESCE(AVG((out.r_multiple > 0)::int), 0)::float8 AS win_rate,
       COALESCE(SUM(out.r_multiple), 0)::float8 AS total_r
FROM signals sig
JOIN signal_outcomes out ON out.signal_id = sig.id
JOIN strategies s ON s.id = sig.strategy_id
WHERE s.name IN ('mean_reversion', 'breakout', 'vwap_reversion', 'trailing_stop')
  AND sig.asset_type = 'equity'
  AND sig.direction IN ('long', 'short')
  AND sig.ts >= sqlc.arg(since)::timestamptz
  AND sig.ts < sqlc.arg(until)::timestamptz
GROUP BY s.name, sig.symbol
HAVING COUNT(out.id) >= sqlc.arg(min_outcomes)::int
ORDER BY s.name, sig.symbol;

-- name: GetHistorySignalsPage :many
-- Fetches directional signals whose market timestamp is inside the visible
-- chart-history window. This deliberately differs from the market-map query,
-- which uses created_at to implement live notification linger.
SELECT s.id,
       st.name AS strategy_name,
       s.symbol,
       coalesce(s.asset_type, '') AS asset_type,
       s.timeframe,
       s.direction,
       coalesce(s.conviction, 0)::float8 AS conviction,
       coalesce(s.entry, 0)::float8 AS entry,
       coalesce(s.stop, 0)::float8 AS stop,
       coalesce(s.target, 0)::float8 AS target,
       coalesce(s.qty, 0)::float8 AS qty,
       CASE WHEN (octet_length(coalesce(s.rationale, '')) + octet_length(coalesce(s.source, ''))) > 262144 THEN '' ELSE coalesce(s.rationale, '') END::text AS rationale,
       CASE WHEN (octet_length(coalesce(s.rationale, '')) + octet_length(coalesce(s.source, ''))) > 262144 THEN '' ELSE coalesce(s.source, '') END::text AS source,
       s.ts,
       s.created_at,
       (octet_length(coalesce(s.rationale, '')) + octet_length(coalesce(s.source, ''))) > 262144 AS oversized
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = sqlc.arg(symbol)
  AND s.ts >= sqlc.arg(signal_from)
  AND s.ts <= sqlc.arg(signal_to)
  AND (sqlc.arg(timeframe)::text = '' OR s.timeframe = sqlc.arg(timeframe)::text)
  AND s.direction <> 'flat'
  AND s.direction <> ''
  AND (s.ts, s.id) < (sqlc.arg(before_ts)::timestamptz, sqlc.arg(before_id)::bigint)
ORDER BY s.ts DESC, s.id DESC
LIMIT 128;

-- name: ClosestSignalForFill :one
SELECT s.entry::float8, s.stop::float8, s.target::float8, s.conviction::float8, s.timeframe, st.name
FROM signals s JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = sqlc.arg(symbol) AND s.direction = sqlc.arg(direction)
  AND s.entry > 0 AND s.stop > 0
  AND s.ts >= (sqlc.arg(filled_at)::timestamptz - INTERVAL '5 minutes')
  AND s.ts <= (sqlc.arg(filled_at)::timestamptz + INTERVAL '5 minutes')
ORDER BY ABS(EXTRACT(EPOCH FROM (s.ts - sqlc.arg(filled_at)::timestamptz))) LIMIT 1;

-- name: MaxDirectionalSignalID :one
SELECT COALESCE(MAX(s.id), 0)::bigint FROM signals s JOIN strategies st ON st.id = s.strategy_id
WHERE st.name = sqlc.arg(strategy) AND s.direction <> 'flat';

-- name: FindConfiguredSignalID :one
SELECT id FROM signals WHERE strategy_id = sqlc.arg(strategy_id) AND symbol = sqlc.arg(symbol)
  AND ts = sqlc.arg(ts) AND timeframe = sqlc.arg(timeframe) AND configuration_id = sqlc.arg(configuration_id)
  AND portfolio_hash = sqlc.arg(portfolio_hash) AND strategy_config_hash = sqlc.arg(strategy_config_hash)
  AND strategy_evaluation_version = sqlc.arg(strategy_evaluation_version)
ORDER BY id DESC LIMIT 1;

-- name: SignalDigestFiltered :many
SELECT s.symbol, s.direction, COUNT(*)::bigint AS signal_count,
       ROUND(AVG(s.conviction)::numeric, 2)::float8 AS avg_conviction,
       ROUND(MAX(s.conviction)::numeric, 2)::float8 AS max_conviction, s.asset_type
FROM signals s JOIN strategies st ON st.id = s.strategy_id
WHERE s.ts >= sqlc.arg(from_at)
  AND (sqlc.narg(to_at)::timestamptz IS NULL OR s.ts <= sqlc.narg(to_at)::timestamptz)
  AND s.direction IN ('long', 'short')
  AND (sqlc.arg(timeframe)::text = '' OR s.timeframe = sqlc.arg(timeframe)::text)
  AND (sqlc.arg(asset_type)::text = '' OR s.asset_type = sqlc.arg(asset_type)::text)
  AND (sqlc.arg(strategy)::text = '' OR st.name = sqlc.arg(strategy)::text)
  AND (cardinality(sqlc.arg(symbols)::text[]) = 0 OR s.symbol = ANY(sqlc.arg(symbols)::text[]))
  AND (sqlc.arg(direction)::text = '' OR s.direction = sqlc.arg(direction)::text)
GROUP BY s.symbol, s.direction, s.asset_type
ORDER BY avg_conviction DESC, signal_count DESC;

-- name: DeferSignalGradingPending :exec
UPDATE signal_grading_pending
SET retry_at = now() + interval '1 hour'
WHERE signal_id = sqlc.arg(signal_id)
  AND horizon_days = sqlc.arg(horizon_days);

-- name: PrepareSignalGrading :one
SELECT prepare_signal_grading(
    sqlc.arg(strategy_id),
    sqlc.arg(symbol),
    sqlc.arg(horizon_days),
    sqlc.arg(before_at)
);

-- name: UpsertSignalOutcomesJSON :exec
INSERT INTO signal_outcomes (
    signal_id, graded_at, r_multiple, hit, horizon_days, exit_reason, mae_r, mfe_r
)
SELECT input.signal_id, input.graded_at, input.r_multiple, input.hit,
       input.horizon_days, input.exit_reason, input.mae_r, input.mfe_r
FROM jsonb_to_recordset(sqlc.arg(outcomes)::jsonb) AS input(
    signal_id bigint,
    graded_at timestamptz,
    r_multiple double precision,
    hit boolean,
    horizon_days integer,
    exit_reason text,
    mae_r double precision,
    mfe_r double precision
)
ON CONFLICT (signal_id, horizon_days) DO UPDATE
SET graded_at = EXCLUDED.graded_at,
    r_multiple = EXCLUDED.r_multiple,
    hit = EXCLUDED.hit,
    exit_reason = EXCLUDED.exit_reason,
    mae_r = EXCLUDED.mae_r,
    mfe_r = EXCLUDED.mfe_r;

-- name: LockCurrentGradingSignals :many
SELECT id, direction,
       COALESCE(entry, 0)::float8 AS entry,
       COALESCE(stop, 0)::float8 AS stop,
       COALESCE(target, 0)::float8 AS target
FROM signals
WHERE id = ANY(sqlc.arg(signal_ids)::bigint[])
ORDER BY id
FOR SHARE;
