-- name: InsertMissedBacktestTrade :exec
INSERT INTO tradebot_missed_backtest_trades (
  account_id, asset_class, timeframe, strategy, symbol, direction,
  backtest_entry_time, price, target_price, stop_price
) VALUES (sqlc.arg('account_id'), sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('strategy'), sqlc.arg('symbol'), sqlc.arg('direction'), sqlc.arg('backtest_entry_time'), sqlc.arg('price'), sqlc.arg('target_price'), sqlc.arg('stop_price'))
ON CONFLICT (account_id, asset_class, timeframe, strategy, symbol, direction, backtest_entry_time)
DO UPDATE SET price = EXCLUDED.price, target_price = EXCLUDED.target_price,
  stop_price = EXCLUDED.stop_price
WHERE NOT tradebot_missed_backtest_trades.notified
  AND tradebot_missed_backtest_trades.notification_claimed_at IS NULL;

-- name: ListPendingMissedBacktestTrades :many
SELECT * FROM tradebot_missed_backtest_trades
WHERE account_id = sqlc.arg('account_id') AND asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND notified = FALSE
ORDER BY backtest_entry_time, symbol, direction;

-- name: MarkMissedBacktestTradesNotified :exec
UPDATE tradebot_missed_backtest_trades
SET notified = TRUE, notified_at = now()
WHERE id = ANY(sqlc.arg('trade_ids')::bigint[]) AND notified = FALSE;

-- name: ListFilledTradebotEntriesForParityWindow :many
SELECT i.*
FROM tradebot_intents i
WHERE i.account_id = sqlc.arg('account_id')
  AND i.asset_class = sqlc.arg('asset_class')
  AND i.timeframe = sqlc.arg('timeframe')
  AND i.action IN ('open_long', 'open_short')
  AND i.filled_qty > 0
  AND i.decision_bar >= sqlc.arg('decision_from')
  AND i.decision_bar <= sqlc.arg('decision_through')
ORDER BY i.decision_bar, i.symbol;

-- name: ListTradebotLifecycleIntentsForParityWindow :many
SELECT i.*
FROM tradebot_intents i
WHERE i.account_id = sqlc.arg('account_id')
  AND i.asset_class = sqlc.arg('asset_class')
  AND i.timeframe = sqlc.arg('timeframe')
  AND i.action IN ('open_long', 'open_short', 'close_long', 'cover_short')
  AND i.decision_bar >= sqlc.arg('decision_from')
  AND i.decision_bar <= sqlc.arg('decision_through')
ORDER BY i.decision_bar, i.symbol, i.action;

-- name: ListTradebotEntryIntentsForParityWindow :many
SELECT i.*
FROM tradebot_intents i
WHERE i.account_id = sqlc.arg('account_id')
  AND i.asset_class = sqlc.arg('asset_class')
  AND i.timeframe = sqlc.arg('timeframe')
  AND i.action IN ('open_long', 'open_short')
  AND i.decision_bar >= sqlc.arg('decision_from')
  AND i.decision_bar <= sqlc.arg('decision_through')
ORDER BY i.decision_bar, i.symbol;

-- name: UpsertTradebotParityDivergence :exec
INSERT INTO tradebot_parity_divergences (
  snapshot_id, account_id, asset_class, timeframe, watermark, symbol,
  direction, action, class, detail
) VALUES (
  sqlc.arg('snapshot_id'), sqlc.arg('account_id'), sqlc.arg('asset_class'),
  sqlc.arg('timeframe'), sqlc.arg('watermark'), sqlc.arg('symbol'),
  sqlc.arg('direction'), sqlc.arg('action'), sqlc.arg('class'),
  sqlc.arg('detail')::jsonb
)
ON CONFLICT (snapshot_id, symbol, direction, action) DO UPDATE
SET class = EXCLUDED.class, detail = EXCLUDED.detail, recorded_at = now();

-- name: ClaimPendingMissedBacktestTrades :many
WITH claimed AS (
 UPDATE tradebot_missed_backtest_trades
 SET notification_claimed_at = now()
 WHERE id IN (
  SELECT p.id FROM tradebot_missed_backtest_trades p
  WHERE p.account_id = sqlc.arg('account_id') AND p.asset_class = sqlc.arg('asset_class') AND p.timeframe = sqlc.arg('timeframe')
   AND NOT p.notified AND p.notification_claimed_at IS NULL
   AND p.id = ANY(sqlc.arg(eligible_ids)::bigint[])
   -- Replay can outlive the intent read that nominated these candidates.
   -- An admitted order is broker-state divergence, never a missed action.
   AND NOT EXISTS (
    SELECT 1 FROM tradebot_intents i
    WHERE i.account_id = p.account_id AND i.asset_class = p.asset_class
     AND i.timeframe = p.timeframe AND i.strategy = p.strategy
     AND i.symbol = p.symbol AND i.direction = p.direction
     AND i.action IN ('open_long', 'open_short')
     AND i.decision_bar = p.backtest_entry_time - sqlc.arg(bar_seconds)::double precision * interval '1 second'
   )
  ORDER BY p.backtest_entry_time, p.symbol, p.direction
  FOR UPDATE SKIP LOCKED
 )
 RETURNING *
)
SELECT * FROM claimed ORDER BY backtest_entry_time, symbol, direction;
