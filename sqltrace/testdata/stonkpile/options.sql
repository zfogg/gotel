-- name: UpsertOptionBar :exec
INSERT INTO option_bars (symbol, underlying, timeframe, ts, open, high, low, close, volume, trade_count, vwap)
VALUES (sqlc.arg('symbol'), sqlc.arg('underlying'), sqlc.arg('timeframe'), sqlc.arg('ts'), sqlc.arg('open'), sqlc.arg('high'), sqlc.arg('low'), sqlc.arg('close'), sqlc.arg('volume'), sqlc.arg('trade_count'), sqlc.arg('vwap'))
ON CONFLICT (symbol, timeframe, ts) DO UPDATE
SET open        = EXCLUDED.open,
    high        = EXCLUDED.high,
    low         = EXCLUDED.low,
    close       = EXCLUDED.close,
    volume      = EXCLUDED.volume,
    trade_count = EXCLUDED.trade_count,
    vwap        = EXCLUDED.vwap;

-- name: OptionBarsByUnderlying :many
SELECT symbol, underlying, timeframe, ts, open, high, low, close, volume, trade_count, vwap
FROM option_bars
WHERE underlying = sqlc.arg('underlying') AND timeframe = sqlc.arg('timeframe') AND ts >= sqlc.arg('from') AND ts <= sqlc.arg('through')
ORDER BY ts ASC;

-- name: UpsertOptionOISnapshot :exec
INSERT INTO option_oi_snapshots (symbol, underlying, day, open_interest)
VALUES (sqlc.arg('symbol'), sqlc.arg('underlying'), sqlc.arg('day'), sqlc.arg('open_interest'))
ON CONFLICT (symbol, day) DO UPDATE
SET open_interest = EXCLUDED.open_interest;

-- name: RecentOptionOISnapshots :many
SELECT symbol, underlying, day, open_interest
FROM option_oi_snapshots
WHERE underlying = sqlc.arg('underlying') AND day >= sqlc.arg('day')
ORDER BY day ASC;

-- name: UpsertIVTermSnapshot :exec
INSERT INTO option_iv_term (underlying, day, dte_bucket, iv, strike, expiry, delta)
VALUES (sqlc.arg('underlying'), sqlc.arg('day'), sqlc.arg('dte_bucket'), sqlc.arg('iv'), sqlc.arg('strike'), sqlc.arg('expiry'), sqlc.arg('delta'))
ON CONFLICT (underlying, day, dte_bucket) DO UPDATE
SET iv     = EXCLUDED.iv,
    strike = EXCLUDED.strike,
    expiry = EXCLUDED.expiry,
    delta  = EXCLUDED.delta;

-- name: RecentIVTermSnapshots :many
SELECT underlying, day, dte_bucket, iv, strike, expiry, delta
FROM option_iv_term
WHERE underlying = sqlc.arg('underlying') AND day >= sqlc.arg('day')
ORDER BY day ASC, dte_bucket ASC;
-- name: OptionBarsFingerprintRows :many
SELECT symbol, underlying, timeframe, ts, open::text AS open, high::text AS high,
       low::text AS low, close::text AS close, volume::text AS volume,
       trade_count::text AS trade_count, COALESCE(vwap::text, '')::text AS vwap
FROM option_bars WHERE underlying = ANY(sqlc.arg(underlyings)::text[]) AND timeframe = '1d'
  AND ts >= sqlc.arg(from_at) AND ts < sqlc.arg(to_at)
ORDER BY underlying, symbol, ts;

-- name: OptionOIFingerprintRows :many
SELECT symbol, underlying, day, open_interest::text AS open_interest
FROM option_oi_snapshots WHERE underlying = ANY(sqlc.arg(underlyings)::text[])
  AND day >= sqlc.arg(from_at)::date AND day < sqlc.arg(to_at)::date
ORDER BY underlying, symbol, day;
