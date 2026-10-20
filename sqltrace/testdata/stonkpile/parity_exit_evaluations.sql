-- name: SaveParityExitEvaluation :exec
INSERT INTO tradebot_parity_exit_evaluations (intent_id,identity,inputs)
VALUES ($1,$2,$3) ON CONFLICT DO NOTHING;

-- name: GetParityExitEvaluation :one
SELECT identity,inputs FROM tradebot_parity_exit_evaluations WHERE intent_id=$1;

-- name: GetParityExitContract :one
SELECT identity FROM tradebot_parity_contracts
WHERE asset_class=$1 AND timeframe=$2 AND watermark=$3
 AND profile_hash=$4 AND portfolio_hash=$5 AND strategy_version=$6
 AND strategy_config_hash=ANY(sqlc.arg(config_hashes)::text[])
ORDER BY created_at DESC LIMIT 1;
