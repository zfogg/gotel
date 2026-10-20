-- name: PrepareParityProtection :exec
INSERT INTO tradebot_parity_protection_captures
(capture_id,account_id,broker,asset_class,timeframe,symbol,position_cycle_id,identity,inputs)
VALUES (sqlc.arg(capture_id),sqlc.arg(account_id),sqlc.arg(broker),sqlc.arg(asset_class),sqlc.arg(timeframe),sqlc.arg(symbol),sqlc.arg(position_cycle_id),sqlc.arg(identity)::jsonb,sqlc.arg(inputs)::jsonb)
ON CONFLICT (capture_id) DO NOTHING;

-- name: BindParityProtection :execrows
UPDATE tradebot_parity_protection_captures
SET broker_order_id=sqlc.arg(broker_order_id),bound_order=sqlc.arg(bound_order)::jsonb
WHERE capture_id=sqlc.arg(capture_id) AND account_id=sqlc.arg(account_id) AND broker=sqlc.arg(broker)
AND broker_order_id IS NULL;

-- name: GetParityProtection :one
SELECT * FROM tradebot_parity_protection_captures
WHERE account_id=sqlc.arg(account_id) AND broker=sqlc.arg(broker) AND broker_order_id=sqlc.arg(broker_order_id);

-- name: GetPreparedParityProtection :one
SELECT * FROM tradebot_parity_protection_captures
WHERE account_id=sqlc.arg(account_id) AND broker=sqlc.arg(broker) AND capture_id=sqlc.arg(capture_id);
