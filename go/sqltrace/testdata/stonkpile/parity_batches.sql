-- name: FindParityBatchAnchor :one
SELECT snapshot_id FROM tradebot_parity_snapshots
WHERE account_id=sqlc.arg(account_id) AND broker=sqlc.arg(broker)
 AND asset_class=sqlc.arg(asset_class) AND timeframe=sqlc.arg(timeframe)
 AND watermark<=sqlc.arg(before_at)::timestamptz
 AND watermark>=sqlc.arg(before_at)::timestamptz-interval '15 minutes'
 AND created_at<sqlc.arg(before_at)::timestamptz
ORDER BY watermark DESC,created_at DESC,snapshot_id DESC LIMIT 1;

-- name: SaveParityReplayBatch :exec
INSERT INTO tradebot_parity_replay_batches(batch_id,account_id,broker,asset_class,timeframe,snapshot_id,through_at,replay)
VALUES(sqlc.arg(batch_id),sqlc.arg(account_id),sqlc.arg(broker),sqlc.arg(asset_class),sqlc.arg(timeframe),sqlc.arg(snapshot_id),sqlc.arg(through_at),sqlc.arg(replay)::jsonb)
ON CONFLICT(batch_id) DO NOTHING;

-- name: FirstParityBatchContractChange :one
SELECT min(watermark)::timestamptz AS changed_at
FROM tradebot_parity_snapshots
WHERE account_id=sqlc.arg(account_id) AND broker=sqlc.arg(broker)
 AND asset_class=sqlc.arg(asset_class) AND timeframe=sqlc.arg(timeframe)
 AND watermark>sqlc.arg(from_at)::timestamptz AND watermark<=sqlc.arg(through_at)::timestamptz
 AND (profile_hash<>sqlc.arg(profile_hash) OR strategy_config_hash<>sqlc.arg(strategy_config_hash)
 OR portfolio_hash<>sqlc.arg(portfolio_hash) OR strategy_evaluation_version<>sqlc.arg(strategy_version));
