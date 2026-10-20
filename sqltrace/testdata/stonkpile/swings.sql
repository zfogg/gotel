-- name: UpsertTradeableMove :exec
INSERT INTO tradeable_moves (symbol, timeframe, strategy_name, direction, entry_ts, exit_ts, entry_px, exit_px, bars, max_pnl, stop_estimate, max_r, regime)
VALUES (sqlc.arg('symbol'), sqlc.arg('timeframe'), sqlc.arg('strategy_name'), sqlc.arg('direction'), sqlc.arg('entry_ts'), sqlc.arg('exit_ts'), sqlc.arg('entry_px'), sqlc.arg('exit_px'), sqlc.arg('bars'), sqlc.arg('max_pnl'), sqlc.arg('stop_estimate'), sqlc.arg('max_r'), sqlc.arg('regime'))
ON CONFLICT (symbol, timeframe, strategy_name, entry_ts, exit_ts) DO UPDATE
SET exit_px     = EXCLUDED.exit_px,
    bars        = EXCLUDED.bars,
    max_pnl     = EXCLUDED.max_pnl,
    stop_estimate = EXCLUDED.stop_estimate,
    max_r       = EXCLUDED.max_r,
    regime      = EXCLUDED.regime;

-- name: UpsertTradeableMovesJSON :exec
INSERT INTO tradeable_moves (
    symbol, timeframe, strategy_name, direction, entry_ts, exit_ts,
    entry_px, exit_px, bars, max_pnl, stop_estimate, max_r, regime
)
SELECT input.symbol, input.timeframe, input.strategy_name, input.direction,
       input.entry_ts, input.exit_ts, input.entry_px, input.exit_px,
       input.bars, input.max_pnl, input.stop_estimate, input.max_r, input.regime
FROM jsonb_to_recordset(sqlc.arg(moves)::jsonb) AS input(
    symbol text,
    timeframe text,
    strategy_name text,
    direction text,
    entry_ts timestamptz,
    exit_ts timestamptz,
    entry_px double precision,
    exit_px double precision,
    bars integer,
    max_pnl double precision,
    stop_estimate double precision,
    max_r double precision,
    regime text
)
ON CONFLICT (symbol, timeframe, strategy_name, entry_ts, exit_ts) DO UPDATE
SET exit_px = EXCLUDED.exit_px,
    bars = EXCLUDED.bars,
    max_pnl = EXCLUDED.max_pnl,
    stop_estimate = EXCLUDED.stop_estimate,
    max_r = EXCLUDED.max_r,
    regime = EXCLUDED.regime;

-- name: GetMovesBySymbolTimeframe :many
SELECT id, symbol, timeframe, strategy_name, direction, entry_ts, exit_ts, entry_px, exit_px, bars, max_pnl, stop_estimate, max_r, regime, created_at
FROM tradeable_moves
WHERE symbol = sqlc.arg('symbol')
  AND timeframe = sqlc.arg('timeframe')
  AND strategy_name = sqlc.arg('strategy_name')
ORDER BY entry_ts ASC;

-- name: GetMovesInWindow :many
SELECT id, symbol, timeframe, strategy_name, direction, entry_ts, exit_ts, entry_px, exit_px, bars, max_pnl, stop_estimate, max_r, regime, created_at
FROM tradeable_moves
WHERE symbol = sqlc.arg('symbol')
  AND timeframe = sqlc.arg('timeframe')
  AND strategy_name = sqlc.arg('strategy_name')
  AND entry_ts >= sqlc.arg('entry_ts')
  AND exit_ts <= sqlc.arg('exit_ts')
ORDER BY entry_ts ASC;

-- name: AllMovesForStrategy :many
SELECT id, symbol, timeframe, strategy_name, direction, entry_ts, exit_ts, entry_px, exit_px, bars, max_pnl, stop_estimate, max_r, regime, created_at
FROM tradeable_moves
WHERE strategy_name = sqlc.arg('strategyName')
ORDER BY entry_ts ASC;

-- name: MovesByRegime :many
SELECT regime,
       count(*)                                AS move_count,
       coalesce(sum(max_pnl), 0)               AS total_max_pnl,
       coalesce(avg(max_pnl), 0)               AS avg_max_pnl,
       coalesce(avg(max_r), 0)                 AS avg_max_r
FROM tradeable_moves
WHERE strategy_name = sqlc.arg('strategyName')
GROUP BY regime
ORDER BY regime;

-- name: MovesByStrategyTimeframe :many
SELECT id, symbol, timeframe, strategy_name, direction, entry_ts, exit_ts, entry_px, exit_px, bars, max_pnl, stop_estimate, max_r, regime, created_at
FROM tradeable_moves
WHERE strategy_name = sqlc.arg('strategy_name')
  AND timeframe = sqlc.arg('timeframe')
  AND symbol = ANY(sqlc.arg('symbols')::text[])
ORDER BY entry_ts ASC;

-- name: GetSwingEvaluation :one
SELECT evaluated_at
FROM swing_evaluations
WHERE strategy_name = sqlc.arg('strategy_name')
  AND symbol = sqlc.arg('symbol')
  AND timeframe = sqlc.arg('timeframe');

-- name: UpsertSwingEvaluation :exec
INSERT INTO swing_evaluations (strategy_name, symbol, timeframe, evaluated_at)
VALUES (sqlc.arg('strategy_name'), sqlc.arg('symbol'), sqlc.arg('timeframe'), sqlc.arg('evaluated_at'))
ON CONFLICT (strategy_name, symbol, timeframe) DO UPDATE
SET evaluated_at = EXCLUDED.evaluated_at;
