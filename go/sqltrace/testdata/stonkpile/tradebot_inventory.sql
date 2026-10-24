-- name: GetTradebotInventoryState :one
SELECT * FROM tradebot_inventory_states
WHERE scope_id = sqlc.arg('scope_id') AND symbol = sqlc.arg('symbol');

-- name: ListTradebotInventoryStatesByScopePrefix :many
SELECT * FROM tradebot_inventory_states
WHERE scope_id LIKE sqlc.arg(scope_prefix)::text || '%'
ORDER BY scope_id, symbol;

-- name: UpsertTradebotInventoryState :one
INSERT INTO tradebot_inventory_states (
    scope_id, symbol, strategy_selection_id, configuration_id,
	pressure, pending_reversal, confirmation_direction, confirmation_count,
	signal_exit_confirmation_direction, signal_exit_confirmation_count,
	signal_exit_confirmation_winning,
	last_signal_id, last_bar, halted, halt_reason, execution_policy,
    replay_lineage_key, replay_from, revision
) VALUES (
    sqlc.arg('scope_id'), sqlc.arg('symbol'), sqlc.arg('strategy_selection_id'), sqlc.arg('configuration_id'),
	sqlc.arg('pressure'), sqlc.arg('pending_reversal'), sqlc.arg('confirmation_direction'), sqlc.arg('confirmation_count'),
	sqlc.arg('signal_exit_confirmation_direction'), sqlc.arg('signal_exit_confirmation_count'), sqlc.arg('signal_exit_confirmation_winning'),
	sqlc.arg('last_signal_id'), sqlc.arg('last_bar'), sqlc.arg('halted'), sqlc.arg('halt_reason'), sqlc.arg('execution_policy'),
	sqlc.arg('replay_lineage_key'), sqlc.arg('replay_from'), 1
)
ON CONFLICT (scope_id, symbol) DO UPDATE SET
    strategy_selection_id = EXCLUDED.strategy_selection_id,
    configuration_id = EXCLUDED.configuration_id,
    pressure = EXCLUDED.pressure,
    pending_reversal = EXCLUDED.pending_reversal,
	confirmation_direction = EXCLUDED.confirmation_direction,
	confirmation_count = EXCLUDED.confirmation_count,
	signal_exit_confirmation_direction = EXCLUDED.signal_exit_confirmation_direction,
	signal_exit_confirmation_count = EXCLUDED.signal_exit_confirmation_count,
	signal_exit_confirmation_winning = EXCLUDED.signal_exit_confirmation_winning,
    last_signal_id = EXCLUDED.last_signal_id,
    last_bar = EXCLUDED.last_bar,
    halted = EXCLUDED.halted,
    halt_reason = EXCLUDED.halt_reason,
    execution_policy = EXCLUDED.execution_policy,
    replay_lineage_key = CASE
        WHEN EXCLUDED.replay_lineage_key <> '' THEN EXCLUDED.replay_lineage_key
        ELSE tradebot_inventory_states.replay_lineage_key
    END,
    replay_from = COALESCE(EXCLUDED.replay_from, tradebot_inventory_states.replay_from),
    revision = tradebot_inventory_states.revision + 1,
    updated_at = now()
RETURNING *;

-- name: ListOpenTradebotInventoryLots :many
SELECT * FROM tradebot_inventory_lots
WHERE scope_id = sqlc.arg('scope_id') AND symbol = sqlc.arg('symbol')
  AND status IN ('open', 'closing') AND remaining_qty > 0
ORDER BY entry_bar, lot_id;

-- name: ListOpenTradebotInventoryLotsForKeys :many
SELECT lots.* FROM tradebot_inventory_lots AS lots
JOIN jsonb_to_recordset(sqlc.arg(scope_symbols)::jsonb) AS keys(scope_id text, symbol text)
  ON keys.scope_id = lots.scope_id AND keys.symbol = lots.symbol
WHERE lots.status IN ('open', 'closing') AND lots.remaining_qty > 0
ORDER BY lots.scope_id, lots.symbol, lots.entry_bar, lots.lot_id;

-- name: GetTradebotInventoryLotForEntryIntent :one
SELECT * FROM tradebot_inventory_lots
WHERE scope_id = sqlc.arg('scope_id') AND symbol = sqlc.arg('symbol') AND entry_intent_id = sqlc.arg('entry_intent_id')
ORDER BY created_at DESC, lot_id DESC
LIMIT 1;

-- name: InsertTradebotInventoryLot :one
INSERT INTO tradebot_inventory_lots (
    lot_id, scope_id, symbol, strategy_selection_id, configuration_id,
    strategy, direction, remaining_qty, entry_qty, entry_price,
    stop_price, target_price, trail_price, entry_bar,
    entry_intent_id, entry_fill_id, protection_order_id, target_order_id,
    execution_policy, status
) VALUES (
    sqlc.arg('lot_id'), sqlc.arg('scope_id'), sqlc.arg('symbol'), sqlc.arg('strategy_selection_id'), sqlc.arg('configuration_id'),
    sqlc.arg('strategy'), sqlc.arg('direction'), sqlc.arg('remaining_qty'), sqlc.arg('entry_qty'), sqlc.arg('entry_price'),
    sqlc.arg('stop_price'), sqlc.arg('target_price'), sqlc.arg('trail_price'), sqlc.arg('entry_bar'),
    sqlc.arg('entry_intent_id'), sqlc.arg('entry_fill_id'), sqlc.arg('protection_order_id'), sqlc.arg('target_order_id'),
    sqlc.arg('execution_policy'), sqlc.arg('status')
)
ON CONFLICT (lot_id) DO UPDATE SET lot_id = EXCLUDED.lot_id
RETURNING *;

-- name: UpdateTradebotInventoryLotQuantity :one
UPDATE tradebot_inventory_lots
SET remaining_qty = sqlc.arg('remaining_qty'),
    status = sqlc.arg('status'),
    revision = revision + 1,
    updated_at = now()
WHERE lot_id = sqlc.arg('lot_id')
RETURNING *;

-- name: SetTradebotInventoryLotProtection :one
UPDATE tradebot_inventory_lots
SET protection_order_id = sqlc.arg('protection_order_id'),
    target_order_id = sqlc.arg('target_order_id'),
    revision = revision + 1,
    updated_at = now()
WHERE lot_id = sqlc.arg('lot_id')
RETURNING *;

-- name: HaltTradebotInventoryState :one
UPDATE tradebot_inventory_states
SET halted = true, halt_reason = sqlc.arg('halt_reason'),
    revision = revision + 1, updated_at = now()
WHERE scope_id = sqlc.arg('scope_id') AND symbol = sqlc.arg('symbol')
RETURNING *;
