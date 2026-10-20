-- name: GetTradebotShortGateProfitLockState :one
SELECT * FROM tradebot_short_gate_profit_lock_states
WHERE scope_id = sqlc.arg('scopeID');

-- name: UpsertTradebotShortGateProfitLockState :one
INSERT INTO tradebot_short_gate_profit_lock_states (
    scope_id, config_hash, state, last_bar, revision
) VALUES (
    sqlc.arg('scope_id'), sqlc.arg('config_hash'), sqlc.arg('state'), sqlc.arg('last_bar'), 1
)
ON CONFLICT (scope_id) DO UPDATE SET
    config_hash = EXCLUDED.config_hash,
    state = EXCLUDED.state,
    last_bar = EXCLUDED.last_bar,
    revision = tradebot_short_gate_profit_lock_states.revision + 1,
    updated_at = now()
RETURNING *;

-- name: GetTradebotPortfolioProfitLockState :one
SELECT * FROM tradebot_portfolio_profit_lock_states
WHERE scope_id = sqlc.arg('scopeID');

-- name: UpsertTradebotPortfolioProfitLockState :one
INSERT INTO tradebot_portfolio_profit_lock_states (
    scope_id, config_hash, state, last_bar, revision
) VALUES (
    sqlc.arg('scope_id'), sqlc.arg('config_hash'), sqlc.arg('state'), sqlc.arg('last_bar'), 1
)
ON CONFLICT (scope_id) DO UPDATE SET
    config_hash = EXCLUDED.config_hash,
    state = EXCLUDED.state,
    last_bar = EXCLUDED.last_bar,
    revision = tradebot_portfolio_profit_lock_states.revision + 1,
    updated_at = now()
RETURNING *;
