-- name: GetAlertState :one
SELECT strategy, last_signal_id, last_pushover_at, last_email_at, paused
FROM alert_state WHERE strategy = sqlc.arg('strategy');

-- name: UpsertAlertState :exec
INSERT INTO alert_state (strategy, last_signal_id, last_pushover_at, last_email_at, paused, updated_at)
VALUES (sqlc.arg('strategy'), sqlc.arg('last_signal_id'), sqlc.arg('last_pushover_at'), sqlc.arg('last_email_at'), sqlc.arg('paused'), now())
ON CONFLICT (strategy) DO UPDATE
SET last_signal_id = GREATEST(alert_state.last_signal_id, EXCLUDED.last_signal_id),
    last_pushover_at = COALESCE(EXCLUDED.last_pushover_at, alert_state.last_pushover_at),
    last_email_at = COALESCE(EXCLUDED.last_email_at, alert_state.last_email_at),
    paused = EXCLUDED.paused,
    updated_at = now();

-- name: SetAlertPaused :exec
UPDATE alert_state SET paused = sqlc.arg('paused'), updated_at = now() WHERE strategy = sqlc.arg('strategy');

-- name: UnnotifiedCrossSignals :many
SELECT s.id, s.symbol, s.direction, s.conviction, s.entry, s.stop, s.target,
       s.ts, s.timeframe, s.rationale, s.asset_type, st.name as strategy_name
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE st.name = 'cross_strategy'
  AND s.direction <> 'flat' AND s.direction <> ''
  AND s.id > sqlc.arg('id')
ORDER BY s.id ASC;

-- name: UnnotifiedCrossSignalsSince :many
SELECT s.id, s.symbol, s.direction, s.conviction, s.entry, s.stop, s.target,
       s.ts, s.timeframe, s.rationale, s.asset_type, st.name as strategy_name
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE st.name = 'cross_strategy'
  AND s.direction <> 'flat' AND s.direction <> ''
  AND s.ts >= sqlc.arg('ts')
ORDER BY s.id ASC;

-- name: SignalDigest :many
SELECT s.symbol, s.direction, COUNT(*) as signal_count,
       ROUND(AVG(s.conviction)::numeric, 2) as avg_conviction,
       ROUND(MAX(s.conviction)::numeric, 2) as max_conviction,
       s.asset_type
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.ts >= sqlc.arg('from')
  AND s.direction IN ('long', 'short')
  AND (sqlc.arg('timeframe')::text = '' OR s.timeframe = sqlc.arg('timeframe'))
  AND (sqlc.arg('asset_type')::text = '' OR s.asset_type = sqlc.arg('asset_type'))
  AND (sqlc.arg('strategy_name')::text = '' OR st.name = sqlc.arg('strategy_name'))
GROUP BY s.symbol, s.direction, s.asset_type
ORDER BY avg_conviction DESC, signal_count DESC;

-- name: SymbolSignals :many
SELECT st.name, s.direction, s.conviction, s.timeframe,
       TO_CHAR(s.ts, 'HH24:MI') as time,
       COALESCE(s.entry, 0) as entry, COALESCE(s.stop, 0) as stop, COALESCE(s.target, 0) as target
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.symbol = sqlc.arg('symbol')
  AND s.direction IN ('long', 'short')
  AND s.ts >= sqlc.arg('ts')
ORDER BY s.ts DESC
LIMIT 50;

-- name: HighConvergenceSignals :many
SELECT s.ts, s.symbol, s.direction, s.asset_type, COUNT(DISTINCT st.name) as strategy_count,
       ARRAY_AGG(DISTINCT st.name) as strategies
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.ts >= sqlc.arg('from')
  AND s.direction IN ('long', 'short')
GROUP BY s.ts, s.symbol, s.direction, s.asset_type
HAVING COUNT(DISTINCT st.name) >= sqlc.arg('min_strategy_count')::bigint
ORDER BY COUNT(DISTINCT st.name) DESC, s.ts DESC;

-- name: DailyUnnotifiedSignals :many
SELECT s.id, s.symbol, s.direction, s.conviction, s.entry, s.stop, s.target,
       s.ts, s.timeframe, s.rationale, s.asset_type, st.name as strategy_name
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.direction <> 'flat'
  AND s.ts >= sqlc.arg('ts')
  AND (s.notified IS NULL OR s.notified != sqlc.arg('notified'))
  AND s.timeframe IN ('1h', '1d')
ORDER BY s.ts DESC;

-- name: DailyDigestSignals :many
SELECT s.id, s.symbol, s.direction, s.conviction, s.entry, s.stop, s.target,
       s.ts, s.timeframe, s.rationale, s.asset_type, st.name as strategy_name
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
WHERE s.direction IN ('long', 'short')
  AND s.ts >= sqlc.arg('ts')
  AND s.timeframe IN ('1h', '1d')
ORDER BY s.ts DESC;
