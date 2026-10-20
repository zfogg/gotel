-- name: ListTradebotFillsForAccount :many
SELECT
    f.fill_id,
    f.intent_id,
    f.scope_id,
    f.broker_order_id,
    f.symbol,
    f.action,
    f.qty,
    f.price,
    f.decision_price,
    f.decision_to_fill_slippage_bps,
    f.initial_entry_price,
    f.initial_stop_price,
    f.initial_risk_per_share,
    COALESCE(f.initial_entry_filled_at, i.decision_bar) AS initial_entry_filled_at,
    f.realized_pnl,
    f.filled_at,
    COALESCE(f.position_cycle_id, i.position_cycle_id) AS position_cycle_id,
    f.gross_pnl,
    f.net_pnl,
    f.total_costs,
    f.cost_breakdown,
    f.pnl_complete,
    f.missing_costs,
    i.account_id,
    i.broker,
    i.asset_class,
    i.timeframe,
    i.strategy AS trade_strategy,
    i.signal_id,
    i.direction,
    i.reason,
    s.ts AS signal_ts,
    s.created_at AS signal_created_at,
    s.direction AS signal_direction,
    s.conviction AS signal_conviction,
    s.entry AS signal_entry,
    s.stop AS signal_stop,
    s.target AS signal_target,
    s.qty AS signal_qty,
    s.rationale AS signal_rationale,
    s.source AS signal_source,
    s.asset_type AS signal_asset_type,
    s.timeframe AS signal_timeframe,
    COALESCE(s.source_votes, '[]'::jsonb) AS signal_source_votes,
    st.name AS signal_strategy
FROM tradebot_fill_events f
JOIN tradebot_intents i ON i.intent_id = f.intent_id
LEFT JOIN signals s ON s.id = i.signal_id
LEFT JOIN strategies st ON st.id = s.strategy_id
WHERE i.broker = sqlc.arg(broker)
  AND i.account_id = sqlc.arg(account_id)
  AND i.asset_class = sqlc.arg(asset_class)
  AND i.timeframe = sqlc.arg(timeframe)
  AND i.mode IN ('canonical', 'exit_only', 'manage_only')
  AND (sqlc.narg(filled_from)::timestamptz IS NULL OR f.filled_at >= sqlc.narg(filled_from))
  AND (sqlc.narg(filled_to)::timestamptz IS NULL OR f.filled_at <= sqlc.narg(filled_to))
  AND (cardinality(sqlc.arg(symbols)::text[]) = 0 OR f.symbol = ANY(sqlc.arg(symbols)::text[]))
ORDER BY f.filled_at DESC, f.fill_id DESC;

-- name: CountTradebotFillsForAccount :one
-- Keep Bot Portfolios tab badges independent from the selected tab. This is
-- deliberately a narrow indexed count rather than loading the full durable
-- fill ledger simply to render a badge.
SELECT count(*)
FROM tradebot_fill_events f
JOIN tradebot_intents i ON i.intent_id = f.intent_id
WHERE i.broker = sqlc.arg(broker)
  AND i.account_id = sqlc.arg(account_id)
  AND i.asset_class = sqlc.arg(asset_class)
  AND i.timeframe = sqlc.arg(timeframe)
  AND i.mode IN ('canonical', 'exit_only', 'manage_only')
  AND (sqlc.narg(filled_from)::timestamptz IS NULL OR f.filled_at >= sqlc.narg(filled_from))
  AND (sqlc.narg(filled_to)::timestamptz IS NULL OR f.filled_at <= sqlc.narg(filled_to))
  AND (cardinality(sqlc.arg(symbols)::text[]) = 0 OR f.symbol = ANY(sqlc.arg(symbols)::text[]));

-- name: ListTradebotAccountScopes :many
-- Reporting may run on an operator workstation that deliberately has no broker
-- credentials. A single durable scope is safe to report read-only; ambiguity
-- must remain an error rather than silently mixing accounts.
SELECT DISTINCT broker, account_id
FROM tradebot_intents
WHERE asset_class = sqlc.arg(asset_class)
  AND timeframe = sqlc.arg(timeframe)
  AND mode IN ('canonical', 'exit_only', 'manage_only')
ORDER BY broker, account_id;

-- name: ListTradebotIntentsForAccountSession :many
SELECT tradebot_intents.intent_id, tradebot_intents.symbol, tradebot_intents.action,
       tradebot_intents.direction, tradebot_intents.qty, tradebot_intents.decision_bar,
       tradebot_intents.stop_price, tradebot_intents.target_price, tradebot_intents.reason,
       tradebot_intents.status, tradebot_intents.broker_order_id, tradebot_intents.avg_fill_price,
       tradebot_intents.terminal_at, tradebot_intents.position_cycle_id,
       COALESCE(s.portfolio_hash, ''),
       COALESCE(s.strategy_config_hash, ''),
       COALESCE(s.strategy_evaluation_version, 0)
FROM tradebot_intents
LEFT JOIN signals s ON s.id = tradebot_intents.signal_id
WHERE tradebot_intents.broker = sqlc.arg(broker)
  AND tradebot_intents.account_id = sqlc.arg(account_id)
  AND tradebot_intents.asset_class = sqlc.arg(asset_class)
  AND tradebot_intents.timeframe = sqlc.arg(timeframe)
  AND tradebot_intents.mode IN ('canonical', 'exit_only', 'manage_only')
  AND tradebot_intents.decision_bar >= sqlc.arg(decision_from)
  AND tradebot_intents.decision_bar <= sqlc.arg(decision_to)
ORDER BY tradebot_intents.decision_bar, tradebot_intents.intent_id;
