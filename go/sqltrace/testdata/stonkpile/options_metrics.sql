-- name: UpsertOptionMetricsCache :exec
INSERT INTO option_metrics_cache (
    underlying, day, spot,
    pc_vol_ratio, pc_vol_total, pc_oi_ratio, pc_oi_total,
    skew, skew_ok,
    delta_oi, delta_oi_pct, delta_oi_tilt, today_oi, yesterday_oi,
    contract_count, fetched_at
) VALUES (sqlc.arg('underlying'),sqlc.arg('day'),sqlc.arg('spot'),sqlc.arg('pc_vol_ratio'),sqlc.arg('pc_vol_total'),sqlc.arg('pc_oi_ratio'),sqlc.arg('pc_oi_total'),sqlc.arg('skew'),sqlc.arg('skew_ok'),sqlc.arg('delta_oi'),sqlc.arg('delta_oi_pct'),sqlc.arg('delta_oi_tilt'),sqlc.arg('today_oi'),sqlc.arg('yesterday_oi'),sqlc.arg('contract_count'),sqlc.arg('fetched_at'))
ON CONFLICT (underlying, day) DO UPDATE SET
    spot            = EXCLUDED.spot,
    pc_vol_ratio    = EXCLUDED.pc_vol_ratio,
    pc_vol_total    = EXCLUDED.pc_vol_total,
    pc_oi_ratio     = EXCLUDED.pc_oi_ratio,
    pc_oi_total     = EXCLUDED.pc_oi_total,
    skew            = EXCLUDED.skew,
    skew_ok         = EXCLUDED.skew_ok,
    delta_oi        = EXCLUDED.delta_oi,
    delta_oi_pct    = EXCLUDED.delta_oi_pct,
    delta_oi_tilt   = EXCLUDED.delta_oi_tilt,
    today_oi        = EXCLUDED.today_oi,
    yesterday_oi    = EXCLUDED.yesterday_oi,
    contract_count  = EXCLUDED.contract_count,
    fetched_at      = EXCLUDED.fetched_at;

-- name: LatestOptionMetricsByUnderlying :many
SELECT c.underlying, c.day, c.spot,
       c.pc_vol_ratio, c.pc_vol_total, c.pc_oi_ratio, c.pc_oi_total,
       c.skew, c.skew_ok,
       c.delta_oi, c.delta_oi_pct, c.delta_oi_tilt, c.today_oi, c.yesterday_oi,
       c.contract_count, c.fetched_at
FROM option_metrics_cache c
INNER JOIN (
    SELECT underlying, MAX(day) AS max_day
    FROM option_metrics_cache
    WHERE underlying = ANY(sqlc.arg('underlyings')::text[])
    GROUP BY underlying
) latest ON c.underlying = latest.underlying AND c.day = latest.max_day;
