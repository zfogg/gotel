-- name: StartTradebotCycleReceipt :exec
INSERT INTO tradebot_cycle_receipts(receipt_id, account_id, broker, asset_class, timeframe, watermark, started_at)
VALUES(sqlc.arg(receipt_id), sqlc.arg(account_id), sqlc.arg(broker), sqlc.arg(asset_class),
       sqlc.arg(timeframe), sqlc.narg(watermark), sqlc.arg(started_at));

-- name: FinishTradebotCycleReceipt :execrows
UPDATE tradebot_cycle_receipts
SET finished_at=sqlc.narg(finished_at), phase=sqlc.arg(phase), result=sqlc.arg(result), error=sqlc.arg(error)
WHERE receipt_id=sqlc.arg(receipt_id) AND finished_at IS NULL;

-- name: RecordTradebotCyclePhase :exec
INSERT INTO tradebot_cycle_phases(receipt_id, sequence, phase, observed_at, evidence)
VALUES(sqlc.arg(receipt_id), sqlc.arg(sequence), sqlc.arg(phase), sqlc.arg(observed_at), sqlc.arg(evidence));

-- name: GetTradebotCycleReceipt :one
SELECT * FROM tradebot_cycle_receipts WHERE receipt_id=sqlc.arg(receipt_id);

-- name: GetTradebotCycleReceiptForIntent :one
SELECT * FROM tradebot_cycle_receipts
WHERE result->'trades' @> jsonb_build_array(jsonb_build_object('intentId', sqlc.arg(intent_id)::text))
ORDER BY started_at, receipt_id LIMIT 1;

-- name: ListTradebotCyclePhases :many
SELECT * FROM tradebot_cycle_phases WHERE receipt_id=sqlc.arg(receipt_id) ORDER BY sequence;

-- name: ListTradebotCycleReceipts :many
SELECT * FROM tradebot_cycle_receipts
WHERE account_id=sqlc.arg(account_id) AND asset_class=sqlc.arg(asset_class) AND timeframe=sqlc.arg(timeframe)
  AND watermark >= sqlc.arg(watermark_from) AND watermark <= sqlc.arg(watermark_through)
ORDER BY started_at, receipt_id;
