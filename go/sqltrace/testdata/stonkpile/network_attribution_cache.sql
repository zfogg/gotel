-- name: GetNetworkAttributionCache :one
UPDATE network_attribution_cache
SET last_accessed_at = now()
WHERE cache_key = sqlc.arg('cacheKey')
RETURNING id, cache_key, asset_type, timeframe, range_from, range_to,
          payload, payload_bytes, workload_units, created_at, last_accessed_at;

-- name: UpsertNetworkAttributionCache :one
INSERT INTO network_attribution_cache (
    cache_key, asset_type, timeframe, range_from, range_to,
    payload, payload_bytes, workload_units
)
VALUES (sqlc.arg('cache_key'), sqlc.arg('asset_type'), sqlc.arg('timeframe'), sqlc.arg('range_from'), sqlc.arg('range_to'), sqlc.arg('payload'), sqlc.arg('payload_bytes'), sqlc.arg('workload_units'))
ON CONFLICT (cache_key)
DO UPDATE SET
    asset_type = EXCLUDED.asset_type,
    timeframe = EXCLUDED.timeframe,
    range_from = EXCLUDED.range_from,
    range_to = EXCLUDED.range_to,
    payload = EXCLUDED.payload,
    payload_bytes = EXCLUDED.payload_bytes,
    workload_units = EXCLUDED.workload_units,
    last_accessed_at = now()
RETURNING id, cache_key, asset_type, timeframe, range_from, range_to,
          payload, payload_bytes, workload_units, created_at, last_accessed_at;

-- name: ListNetworkAttributionCacheForPrune :many
SELECT id, payload_bytes, workload_units
FROM network_attribution_cache
ORDER BY last_accessed_at DESC, created_at DESC, id DESC;

-- name: DeleteNetworkAttributionCache :exec
DELETE FROM network_attribution_cache
WHERE id = sqlc.arg('id');
