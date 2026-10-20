-- name: InsertTradebotIntent :one
INSERT INTO tradebot_intents (
  intent_id, scope_id, mode, account_id, broker, asset_class, strategy,
  timeframe, symbol, signal_id, decision_bar, action, direction, qty,
  reference_price, stop_price, target_price, reason, policy, status,
  client_order_id, profile_hash, model_fill_price, configuration_id,
  position_cycle_id, strategy_selection_id, execution_policy, operator_operation_id
) VALUES (
  sqlc.arg('intent_id'), sqlc.arg('scope_id'), sqlc.arg('mode'), sqlc.arg('account_id'), sqlc.arg('broker'), sqlc.arg('asset_class'), sqlc.arg('strategy'),
  sqlc.arg('timeframe'), sqlc.arg('symbol'), sqlc.narg('signal_id'), sqlc.arg('decision_bar'), sqlc.arg('action'), sqlc.arg('direction'), sqlc.arg('qty'),
  sqlc.arg('reference_price'), sqlc.narg('stop_price'), sqlc.narg('target_price'), sqlc.arg('reason'), sqlc.arg('policy'), sqlc.arg('status'), sqlc.arg('client_order_id'), sqlc.arg('profile_hash'), sqlc.narg('model_fill_price'), sqlc.narg('configuration_id'), sqlc.narg('position_cycle_id'), sqlc.narg('strategy_selection_id'),
  COALESCE(sqlc.narg(execution_policy)::jsonb, '{}'::jsonb), NULLIF(sqlc.narg(operator_operation_id)::text, '')
)
ON CONFLICT (intent_id) DO UPDATE SET
  status = 'pending',
  broker_order_id = NULL,
  error = NULL,
  updated_at = now()
WHERE tradebot_intents.status = 'failed'
RETURNING intent_id;

-- name: GetTradebotIntent :one
SELECT * FROM tradebot_intents WHERE intent_id = sqlc.arg('intentID');

-- name: ListTradebotFillsForIntent :many
-- A missing signal-inventory lot can be reconstructed only when this durable
-- broker-fill provenance is unambiguous. Keep partial fills distinct so a
-- recovery never silently invents an aggregate entry price.
SELECT * FROM tradebot_fill_events
WHERE intent_id = sqlc.arg('intentID')
ORDER BY filled_at, fill_id;

-- name: MarkTradebotIntentSubmitted :exec
UPDATE tradebot_intents
SET status = 'submitted', broker_order_id = sqlc.arg('broker_order_id'), updated_at = now(), error = NULL
WHERE intent_id = sqlc.arg('intent_id');

-- name: MarkTradebotIntentFailed :exec
UPDATE tradebot_intents
SET status = 'failed', error = sqlc.arg('error'), updated_at = now()
WHERE intent_id = sqlc.arg('intent_id');

-- name: UpsertTradebotPosition :exec
INSERT INTO tradebot_positions (
  scope_id, symbol, strategy, timeframe, direction, qty, avg_entry_price,
  stop_price, target_price, trail_price, entry_intent_id, entry_signal_id,
  entry_bar, last_signal_id, last_bar, configuration_id, position_cycle_id,
  strategy_selection_id, execution_policy
) VALUES (
  sqlc.arg('scope_id'), sqlc.arg('symbol'), sqlc.arg('strategy'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('qty'), sqlc.arg('avg_entry_price'),
  sqlc.arg('stop_price'), sqlc.arg('target_price'), sqlc.arg('trail_price'), sqlc.arg('entry_intent_id'), sqlc.arg('entry_signal_id'),
  sqlc.arg('entry_bar'), sqlc.arg('last_signal_id'), sqlc.arg('last_bar'), sqlc.arg('configuration_id'), sqlc.arg('position_cycle_id'), sqlc.arg('strategy_selection_id'),
  COALESCE(sqlc.narg(execution_policy)::jsonb, '{}'::jsonb)
)
ON CONFLICT (scope_id, symbol) DO UPDATE SET
  strategy = EXCLUDED.strategy,
  timeframe = EXCLUDED.timeframe,
  direction = EXCLUDED.direction,
  qty = EXCLUDED.qty,
  avg_entry_price = EXCLUDED.avg_entry_price,
  stop_price = EXCLUDED.stop_price,
  target_price = EXCLUDED.target_price,
  trail_price = EXCLUDED.trail_price,
  entry_intent_id = EXCLUDED.entry_intent_id,
  entry_signal_id = EXCLUDED.entry_signal_id,
  entry_bar = EXCLUDED.entry_bar,
  last_signal_id = EXCLUDED.last_signal_id,
  last_bar = EXCLUDED.last_bar,
  configuration_id = EXCLUDED.configuration_id,
  position_cycle_id = EXCLUDED.position_cycle_id,
  strategy_selection_id = EXCLUDED.strategy_selection_id,
  execution_policy = EXCLUDED.execution_policy,
  trailing_peak = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id
      OR tradebot_positions.direction IS DISTINCT FROM EXCLUDED.direction
      OR (EXCLUDED.position_cycle_id IS NULL AND (
        tradebot_positions.entry_intent_id IS DISTINCT FROM EXCLUDED.entry_intent_id
        OR tradebot_positions.entry_bar IS DISTINCT FROM EXCLUDED.entry_bar)) THEN NULL
    ELSE tradebot_positions.trailing_peak END,
  initial_risk_per_share = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.initial_risk_per_share END,
  initial_target_price = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.initial_target_price END,
  favorable_peak = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.favorable_peak END,
  adaptive_profit_active = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN FALSE
    ELSE tradebot_positions.adaptive_profit_active END,
  adaptive_profit_stop = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.adaptive_profit_stop END,
  adaptive_state_bar = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.adaptive_state_bar END,
  updated_at = now();

-- name: UpsertTradebotPositions :exec
-- Reconcile the broker's complete open-position snapshot in one statement.
-- The JSON recordset preserves nullable ownership and policy fields while
-- avoiding one round trip per position.
INSERT INTO tradebot_positions (
  scope_id, symbol, strategy, timeframe, direction, qty, avg_entry_price,
  stop_price, target_price, trail_price, entry_intent_id, entry_signal_id,
  entry_bar, last_signal_id, last_bar, configuration_id, position_cycle_id,
  strategy_selection_id, execution_policy
)
SELECT
  p.scope_id, p.symbol, p.strategy, p.timeframe, p.direction, p.qty,
  p.avg_entry_price, p.stop_price, p.target_price, p.trail_price,
  p.entry_intent_id, p.entry_signal_id, p.entry_bar, p.last_signal_id,
  p.last_bar, p.configuration_id, p.position_cycle_id,
  p.strategy_selection_id, COALESCE(p.execution_policy, '{}'::jsonb)
FROM jsonb_to_recordset(sqlc.arg(positions)::jsonb) AS p(
  scope_id text,
  symbol text,
  strategy text,
  timeframe text,
  direction text,
  qty double precision,
  avg_entry_price double precision,
  stop_price double precision,
  target_price double precision,
  trail_price double precision,
  entry_intent_id text,
  entry_signal_id bigint,
  entry_bar timestamptz,
  last_signal_id bigint,
  last_bar timestamptz,
  configuration_id bigint,
  position_cycle_id text,
  strategy_selection_id bigint,
  execution_policy jsonb
)
ON CONFLICT (scope_id, symbol) DO UPDATE SET
  strategy = EXCLUDED.strategy,
  timeframe = EXCLUDED.timeframe,
  direction = EXCLUDED.direction,
  qty = EXCLUDED.qty,
  avg_entry_price = EXCLUDED.avg_entry_price,
  stop_price = EXCLUDED.stop_price,
  target_price = EXCLUDED.target_price,
  trail_price = EXCLUDED.trail_price,
  entry_intent_id = EXCLUDED.entry_intent_id,
  entry_signal_id = EXCLUDED.entry_signal_id,
  entry_bar = EXCLUDED.entry_bar,
  last_signal_id = EXCLUDED.last_signal_id,
  last_bar = EXCLUDED.last_bar,
  configuration_id = EXCLUDED.configuration_id,
  position_cycle_id = EXCLUDED.position_cycle_id,
  strategy_selection_id = EXCLUDED.strategy_selection_id,
  execution_policy = EXCLUDED.execution_policy,
  trailing_peak = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id
      OR tradebot_positions.direction IS DISTINCT FROM EXCLUDED.direction
      OR (EXCLUDED.position_cycle_id IS NULL AND (
        tradebot_positions.entry_intent_id IS DISTINCT FROM EXCLUDED.entry_intent_id
        OR tradebot_positions.entry_bar IS DISTINCT FROM EXCLUDED.entry_bar)) THEN NULL
    ELSE tradebot_positions.trailing_peak END,
  initial_risk_per_share = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.initial_risk_per_share END,
  initial_target_price = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.initial_target_price END,
  favorable_peak = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.favorable_peak END,
  adaptive_profit_active = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN FALSE
    ELSE tradebot_positions.adaptive_profit_active END,
  adaptive_profit_stop = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.adaptive_profit_stop END,
  adaptive_state_bar = CASE
    WHEN tradebot_positions.position_cycle_id IS DISTINCT FROM EXCLUDED.position_cycle_id THEN NULL
    ELSE tradebot_positions.adaptive_state_bar END,
  updated_at = now();

-- name: GetTradebotPosition :one
SELECT * FROM tradebot_positions WHERE scope_id = sqlc.arg('scope_id') AND symbol = sqlc.arg('symbol');

-- name: DeleteTradebotPosition :exec
DELETE FROM tradebot_positions WHERE scope_id = sqlc.arg('scope_id') AND symbol = sqlc.arg('symbol');

-- name: SetTradebotPositionTargetOrder :exec
UPDATE tradebot_positions
SET target_order_id = sqlc.arg('target_order_id'), target_order_updated_at = now(), updated_at = now()
WHERE scope_id = sqlc.arg('scope_id') AND symbol = sqlc.arg('symbol');

-- name: AdvanceTradebotAdaptiveProfitState :execrows
UPDATE tradebot_positions
SET initial_risk_per_share = COALESCE(initial_risk_per_share, sqlc.arg(initial_risk_per_share)),
    initial_target_price = COALESCE(initial_target_price, sqlc.narg(initial_target_price)),
    favorable_peak = sqlc.arg(favorable_peak),
    adaptive_profit_active = sqlc.arg(adaptive_profit_active),
    adaptive_profit_stop = sqlc.narg(adaptive_profit_stop),
    adaptive_state_bar = sqlc.arg(adaptive_state_bar),
    stop_price = sqlc.narg(stop_price),
    target_price = sqlc.narg(target_price),
    updated_at = now()
WHERE scope_id = sqlc.arg(scope_id)
  AND symbol = sqlc.arg(symbol)
  AND (adaptive_state_bar IS NULL OR adaptive_state_bar < sqlc.arg(adaptive_state_bar));

-- name: ListTradebotPositionsByScopePrefix :many
SELECT * FROM tradebot_positions
WHERE scope_id LIKE sqlc.arg(scope_prefix)::text || '%'
ORDER BY scope_id, symbol;

-- name: LatestFilledEntryIntentForSymbol :one
SELECT * FROM tradebot_intents i
WHERE i.account_id = sqlc.arg('account_id')
  AND i.asset_class = sqlc.arg('asset_class')
  AND i.symbol = sqlc.arg('symbol')
  AND i.action IN ('open_long', 'open_short')
  AND i.filled_qty > 0
  AND NOT (i.strategy = 'broker_reconcile' AND EXISTS (
    SELECT 1 FROM tradebot_intents owner
    WHERE owner.broker = i.broker AND owner.account_id = i.account_id
      AND owner.asset_class = i.asset_class AND owner.symbol = i.symbol
      AND owner.broker_order_id = i.broker_order_id AND owner.filled_qty > 0
      AND owner.action IN ('open_long', 'open_short')
      AND owner.action = i.action AND owner.strategy <> 'broker_reconcile'
  ))
ORDER BY COALESCE(i.terminal_at, i.updated_at) DESC, i.created_at DESC
LIMIT 1;

-- name: LatestFilledEntryIntentsForSymbols :many
SELECT DISTINCT ON (i.symbol) *
FROM tradebot_intents i
WHERE i.account_id = sqlc.arg(account_id)
  AND i.asset_class = sqlc.arg(asset_class)
  AND i.symbol = ANY(sqlc.arg(symbols)::text[])
  AND i.action IN ('open_long', 'open_short')
  AND i.filled_qty > 0
  AND NOT (i.strategy = 'broker_reconcile' AND EXISTS (
    SELECT 1 FROM tradebot_intents owner
    WHERE owner.broker = i.broker AND owner.account_id = i.account_id
      AND owner.asset_class = i.asset_class AND owner.symbol = i.symbol
      AND owner.broker_order_id = i.broker_order_id AND owner.filled_qty > 0
      AND owner.action IN ('open_long', 'open_short')
      AND owner.action = i.action AND owner.strategy <> 'broker_reconcile'
  ))
ORDER BY i.symbol, COALESCE(i.terminal_at, i.updated_at) DESC, i.created_at DESC;

-- name: LatestFilledEntryIntentForSymbolAt :one
SELECT * FROM tradebot_intents i
WHERE i.account_id = sqlc.arg(account_id)
  AND i.asset_class = sqlc.arg(asset_class)
  AND i.symbol = sqlc.arg(symbol)
  AND i.action IN ('open_long', 'open_short')
  AND i.filled_qty > 0
  AND NOT (i.strategy = 'broker_reconcile' AND EXISTS (
    SELECT 1 FROM tradebot_intents owner
    WHERE owner.broker = i.broker AND owner.account_id = i.account_id
      AND owner.asset_class = i.asset_class AND owner.symbol = i.symbol
      AND owner.broker_order_id = i.broker_order_id AND owner.filled_qty > 0
      AND owner.action IN ('open_long', 'open_short')
      AND owner.action = i.action AND owner.strategy <> 'broker_reconcile'
      AND COALESCE(owner.terminal_at, owner.updated_at) <= sqlc.arg(filled_at)
  ))
  AND COALESCE(i.terminal_at, i.updated_at) <= sqlc.arg(filled_at)
ORDER BY COALESCE(i.terminal_at, i.updated_at) DESC, i.created_at DESC
LIMIT 1;

-- name: TradebotEntryIntentExistsForBrokerOrder :one
SELECT EXISTS (
 SELECT 1 FROM tradebot_intents
 WHERE broker = sqlc.arg(broker) AND account_id = sqlc.arg(account_id)
   AND asset_class = sqlc.arg(asset_class)
   AND broker_order_id = sqlc.arg(broker_order_id)
   AND action IN ('open_long', 'open_short')
);

-- name: TradebotFillExistsForBrokerOrder :one
SELECT EXISTS (
  SELECT 1
  FROM tradebot_fill_events f
  JOIN tradebot_intents i ON i.intent_id = f.intent_id
  WHERE i.broker = sqlc.arg(broker)
    AND i.account_id = sqlc.arg(account_id)
    AND i.asset_class = sqlc.arg(asset_class)
    AND f.broker_order_id = sqlc.arg(broker_order_id)
);

-- name: TradebotFillExistsForBrokerOrders :many
SELECT DISTINCT f.broker_order_id
FROM tradebot_fill_events f
JOIN tradebot_intents i ON i.intent_id = f.intent_id
WHERE i.broker = sqlc.arg(broker)
  AND i.account_id = sqlc.arg(account_id)
  AND i.asset_class = sqlc.arg(asset_class)
  AND f.broker_order_id = ANY(sqlc.arg(broker_order_ids)::text[]);

-- name: ReclassifyTradebotProtectiveFillAsShortEntry :execrows
WITH updated_intent AS (
  UPDATE tradebot_intents i
  SET action = 'open_short',
      direction = 'short',
      qty = sqlc.arg(qty),
      reference_price = sqlc.arg(price),
      stop_price = NULL,
      target_price = NULL,
      reason = 'broker_orphan_short',
      filled_qty = sqlc.arg(qty),
      avg_fill_price = sqlc.arg(price),
      model_fill_price = sqlc.arg(price),
      terminal_at = sqlc.arg(filled_at),
      position_cycle_id = sqlc.arg(position_cycle_id),
      updated_at = now()
  FROM tradebot_fill_events f
  WHERE f.intent_id = i.intent_id
    AND i.broker = sqlc.arg(broker)
    AND i.account_id = sqlc.arg(account_id)
    AND i.asset_class = sqlc.arg(asset_class)
    AND f.broker_order_id = sqlc.arg(broker_order_id)
    AND i.action = 'close_long'
    AND i.reason IN ('stop', 'target')
  RETURNING i.intent_id
)
UPDATE tradebot_fill_events f
SET action = 'open_short',
    qty = sqlc.arg(qty),
    price = sqlc.arg(price),
    decision_price = sqlc.arg(price),
    decision_to_fill_slippage_bps = 0,
    realized_pnl = 0,
    gross_pnl = 0,
    net_pnl = 0,
    total_costs = 0,
    cost_breakdown = '{}'::jsonb,
    pnl_complete = true,
    missing_costs = '{}'::text[],
    filled_at = sqlc.arg(filled_at),
    position_cycle_id = sqlc.arg(position_cycle_id)
FROM updated_intent i
WHERE f.intent_id = i.intent_id;

-- name: EarliestTradebotFillAtForAccount :one
SELECT min(f.filled_at)::timestamptz
FROM tradebot_fill_events f
JOIN tradebot_intents i ON i.intent_id = f.intent_id
WHERE i.broker = sqlc.arg(broker)
  AND i.account_id = sqlc.arg(account_id)
  AND i.asset_class = sqlc.arg(asset_class);

-- name: GetTradebotBrokerActivityCursor :one
SELECT last_activity_at
FROM tradebot_broker_activity_cursors
WHERE broker = sqlc.arg(broker)
  AND account_id = sqlc.arg(account_id)
  AND asset_class = sqlc.arg(asset_class)
  AND activity_type = sqlc.arg(activity_type);

-- name: UpsertTradebotBrokerActivityCursor :exec
INSERT INTO tradebot_broker_activity_cursors (
  broker, account_id, asset_class, activity_type, last_activity_at
) VALUES (
  sqlc.arg(broker), sqlc.arg(account_id), sqlc.arg(asset_class),
  sqlc.arg(activity_type), sqlc.arg(last_activity_at)
)
ON CONFLICT (broker, account_id, asset_class, activity_type) DO UPDATE SET
  last_activity_at = GREATEST(
    tradebot_broker_activity_cursors.last_activity_at,
    EXCLUDED.last_activity_at
  ),
  updated_at = now();

-- name: GetTradebotBrokerActivityFetchAt :one
SELECT last_fetch_at
FROM tradebot_broker_activity_cursors
WHERE broker = sqlc.arg(broker)
  AND account_id = sqlc.arg(account_id)
  AND asset_class = sqlc.arg(asset_class)
  AND activity_type = sqlc.arg(activity_type);

-- name: MarkTradebotBrokerActivityFetch :exec
INSERT INTO tradebot_broker_activity_cursors (
  broker, account_id, asset_class, activity_type, last_activity_at, last_fetch_at
) VALUES (
  sqlc.arg(broker), sqlc.arg(account_id), sqlc.arg(asset_class),
  sqlc.arg(activity_type), to_timestamp(0), sqlc.arg(fetched_at)
)
ON CONFLICT (broker, account_id, asset_class, activity_type) DO UPDATE SET
  last_fetch_at = EXCLUDED.last_fetch_at,
  updated_at = now();

-- name: InsertTradebotFillEvent :exec
INSERT INTO tradebot_fill_events (
  fill_id, intent_id, scope_id, broker_order_id, symbol, action,
  qty, price, realized_pnl, filled_at, configuration_id, position_cycle_id,
  gross_pnl, net_pnl, total_costs, cost_breakdown, pnl_complete, missing_costs,
  strategy_selection_id, execution_policy
) VALUES (
  sqlc.arg(fill_id), sqlc.arg(intent_id), sqlc.arg(scope_id),
  sqlc.arg(broker_order_id), sqlc.arg(symbol), sqlc.arg(action),
  sqlc.arg(qty), sqlc.arg(price), sqlc.arg(realized_pnl), sqlc.arg(filled_at),
  sqlc.arg(configuration_id), sqlc.arg(position_cycle_id),
  sqlc.arg(gross_pnl), sqlc.arg(net_pnl), sqlc.arg(total_costs),
  sqlc.arg(cost_breakdown), sqlc.arg(pnl_complete),
  COALESCE(sqlc.narg(missing_costs)::text[], '{}'::text[]),
  sqlc.arg(strategy_selection_id),
  COALESCE(sqlc.narg(execution_policy)::jsonb, '{}'::jsonb)
)
ON CONFLICT (fill_id) DO NOTHING;

-- name: ListTradebotFillNotifications :many
SELECT f.fill_id, f.symbol, f.action, f.qty, f.price, f.realized_pnl,
       f.gross_pnl, f.net_pnl, f.total_costs, f.cost_breakdown,
       f.pnl_complete, f.missing_costs, f.filled_at, f.position_cycle_id,
       i.strategy, i.asset_class, i.timeframe, i.reason, i.account_id,
       f.execution_policy, f.decision_price, f.decision_to_fill_slippage_bps,
       COALESCE(tax.estimated_tax, 0)::double precision AS estimated_tax,
       COALESCE(tax.tax_complete, false) AS tax_complete
FROM tradebot_fill_events f
JOIN tradebot_intents i ON i.intent_id = f.intent_id
LEFT JOIN tradebot_fill_notification_receipts receipt ON receipt.fill_id = f.fill_id
LEFT JOIN LATERAL (
  SELECT sum(liability_delta)::double precision AS estimated_tax,
         bool_and(complete) AS tax_complete
  FROM tax_liability_events
  WHERE (tradebot_fill_id = f.fill_id OR desk_fill_order_id = f.broker_order_id)
    AND event_type <> 'withdrawal'
) tax ON true
WHERE i.asset_class = sqlc.arg(asset_class)
  AND (sqlc.arg(timeframe)::text = '' OR i.timeframe = sqlc.arg(timeframe))
  AND (receipt.fill_id IS NULL OR receipt.status = 'unhandled')
ORDER BY f.filled_at, f.fill_id
LIMIT sqlc.arg(limit_count);

-- name: ClaimTradebotFillNotifications :many
INSERT INTO tradebot_fill_notification_receipts (
  fill_id, status, claimed_at, attempt_count, last_error
)
SELECT fill_id, 'claimed', now(), 1, NULL
FROM unnest(sqlc.arg(fill_ids)::text[]) AS requested(fill_id)
ON CONFLICT (fill_id) DO UPDATE SET
  status = 'claimed',
  claimed_at = now(),
  handled_at = NULL,
  notified_at = NULL,
  delivery_state = NULL,
  attempt_count = tradebot_fill_notification_receipts.attempt_count + 1,
  last_error = NULL
WHERE tradebot_fill_notification_receipts.status = 'unhandled'
RETURNING fill_id;

-- name: MarkTradebotFillNotificationsHandled :exec
UPDATE tradebot_fill_notification_receipts
SET status = 'handled',
    delivery_state = 'sent',
    notified_at = now(),
    handled_at = now(),
    claimed_at = NULL,
    last_error = NULL
WHERE fill_id = ANY(sqlc.arg(fill_ids)::text[])
  AND status = 'claimed';

-- name: MarkTradebotFillNotificationsSuppressed :exec
INSERT INTO tradebot_fill_notification_receipts (
  fill_id, status, delivery_state, handled_at, attempt_count, last_error
)
SELECT fill_id, 'handled', 'suppressed', now(), 0, NULL
FROM unnest(sqlc.arg(fill_ids)::text[]) AS requested(fill_id)
ON CONFLICT (fill_id) DO UPDATE SET
  status = 'handled',
  delivery_state = 'suppressed',
  notified_at = NULL,
  handled_at = now(),
  claimed_at = NULL,
  last_error = NULL
WHERE tradebot_fill_notification_receipts.status = 'unhandled';

-- name: MarkTradebotFillNotificationsUnhandled :exec
UPDATE tradebot_fill_notification_receipts
SET status = 'unhandled',
    delivery_state = NULL,
    notified_at = NULL,
    handled_at = NULL,
    claimed_at = NULL,
    last_error = sqlc.arg(last_error)
WHERE fill_id = ANY(sqlc.arg(fill_ids)::text[])
  AND status = 'claimed';

-- name: GetTradebotFillNotificationReceipt :one
SELECT *
FROM tradebot_fill_notification_receipts
WHERE fill_id = sqlc.arg(fill_id);

-- name: UpsertTradebotShadowState :exec
INSERT INTO tradebot_shadow_state (
  scope_id, symbol, state, intent_count, divergence_count
) VALUES (sqlc.arg('scope_id'), sqlc.arg('symbol'), sqlc.arg('state'), sqlc.arg('intent_count'), sqlc.arg('divergence_count'))
ON CONFLICT (scope_id, symbol) DO UPDATE SET
  state = EXCLUDED.state,
  intent_count = EXCLUDED.intent_count,
  divergence_count = EXCLUDED.divergence_count,
  updated_at = now();

-- name: AcquireTradebotExecutionLease :one
INSERT INTO tradebot_execution_leases (
  lease_key, owner_id, acquired_at, heartbeat_at, expires_at
) VALUES (sqlc.arg('lease_key'), sqlc.arg('owner_id'), now(), now(), sqlc.arg('expires_at'))
ON CONFLICT (lease_key) DO UPDATE SET
  owner_id = EXCLUDED.owner_id,
  acquired_at = now(),
  heartbeat_at = now(),
  expires_at = EXCLUDED.expires_at
WHERE tradebot_execution_leases.expires_at <= now()
   OR tradebot_execution_leases.owner_id = EXCLUDED.owner_id
RETURNING lease_key;

-- name: HeartbeatTradebotExecutionLease :exec
UPDATE tradebot_execution_leases
SET heartbeat_at = now(), expires_at = sqlc.arg('expires_at')
WHERE lease_key = sqlc.arg('lease_key') AND owner_id = sqlc.arg('owner_id');

-- name: ReleaseTradebotExecutionLease :exec
DELETE FROM tradebot_execution_leases WHERE lease_key = sqlc.arg('lease_key') AND owner_id = sqlc.arg('owner_id');

-- name: ListNonterminalTradebotIntents :many
SELECT * FROM tradebot_intents
WHERE account_id = sqlc.arg(account_id)
  AND broker = sqlc.arg(broker)
  AND asset_class = sqlc.arg(asset_class)
  -- Shadow/pre-market intents use the sentinel broker order id "shadow" and
  -- must never block reconciliation of real broker orders.
  AND mode IN ('canonical', 'exit_only', 'manage_only')
  AND status IN ('new', 'pending', 'submitted', 'partially_filled')
ORDER BY created_at;

-- name: ReconcileTradebotIntent :exec
UPDATE tradebot_intents
SET status = sqlc.arg('status'), filled_qty = sqlc.arg('filled_qty'), avg_fill_price = sqlc.arg('avg_fill_price'),
    canceled_qty = sqlc.arg('canceled_qty'), terminal_at = sqlc.arg('terminal_at'), updated_at = now(), error = sqlc.arg('error')
WHERE intent_id = sqlc.arg('intent_id');

-- name: AdvanceTradebotTrailingPeak :one
-- Row locking makes concurrent observations a monotone extremum. The complete
-- identity guard rejects delayed observations from a closed/replaced cycle.
UPDATE tradebot_positions
SET trailing_peak = CASE WHEN direction = 'short'
      THEN LEAST(COALESCE(trailing_peak, sqlc.arg(price)::double precision), sqlc.arg(price)::double precision)
      ELSE GREATEST(COALESCE(trailing_peak, sqlc.arg(price)::double precision), sqlc.arg(price)::double precision) END,
    updated_at = now()
WHERE scope_id = sqlc.arg(scope_id) AND symbol = sqlc.arg(symbol)
  AND position_cycle_id IS NOT DISTINCT FROM sqlc.narg(position_cycle_id)::text
  AND (position_cycle_id IS NOT NULL OR (
    entry_intent_id IS NOT DISTINCT FROM sqlc.narg(entry_intent_id)::text
    AND entry_bar IS NOT DISTINCT FROM sqlc.narg(entry_bar)::timestamptz))
  AND direction = sqlc.arg(direction)
RETURNING trailing_peak;
-- name: SelectTradebotLongStopFills :many
SELECT f.fill_id, f.symbol, f.qty::float8 AS qty, f.price::float8 AS price,
       f.filled_at, COALESCE(f.initial_risk_per_share, 0)::float8 AS initial_risk_per_share,
       COALESCE(i.execution_policy->>'clusterId', '')::text AS cluster_id
FROM tradebot_fill_events f
JOIN tradebot_intents i ON i.intent_id = f.intent_id
WHERE i.broker = sqlc.arg(broker)
  AND i.account_id = sqlc.arg(account_id)
  AND i.asset_class = sqlc.arg(asset_class)
  AND i.timeframe = sqlc.arg(timeframe)
  AND f.action = 'close_long'
  AND lower(trim(i.reason)) = 'stop'
  AND f.filled_at >= sqlc.arg(from_at)
  AND f.filled_at < sqlc.arg(through_at)
ORDER BY f.filled_at, f.fill_id;

-- name: SelectTradebotStopWaveIntentSymbols :many
SELECT symbol
FROM tradebot_intents
WHERE broker = sqlc.arg(broker)
  AND account_id = sqlc.arg(account_id)
  AND asset_class = sqlc.arg(asset_class)
  AND timeframe = sqlc.arg(timeframe)
  AND action = 'open_short'
  AND reason = sqlc.arg(reason)
  AND decision_bar = sqlc.arg(decision_bar)
  AND status <> 'failed'
GROUP BY symbol;
