-- name: InsertParityInput :exec
INSERT INTO tradebot_parity_inputs (input_hash, payload)
VALUES (sqlc.arg('input_hash'), sqlc.arg('payload'))
ON CONFLICT (input_hash) DO NOTHING;

-- name: GetParityInput :one
SELECT payload FROM tradebot_parity_inputs WHERE input_hash = sqlc.arg('input_hash');

-- name: GetParityInputs :many
SELECT input_hash,payload FROM tradebot_parity_inputs
WHERE input_hash=ANY(sqlc.arg('input_hashes')::text[]);

-- name: GetSignalEvaluationFingerprint :one
SELECT canonical_config FROM signal_strategy_config_snapshots
WHERE strategy_config_hash = sqlc.arg('strategy_config_hash');

-- name: ListPresentParityInputs :many
SELECT input_hash FROM tradebot_parity_inputs
WHERE input_hash = ANY(sqlc.arg('input_hashes')::text[]);

-- name: InsertParityInputs :exec
INSERT INTO tradebot_parity_inputs (input_hash, payload)
SELECT unnest(sqlc.arg('input_hashes')::text[]), unnest(sqlc.arg('payloads')::bytea[])
ON CONFLICT (input_hash) DO NOTHING;
