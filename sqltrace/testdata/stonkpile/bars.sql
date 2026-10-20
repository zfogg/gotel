-- name: UpsertBar :exec
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, source, asset_type)
VALUES (sqlc.arg('symbol'), sqlc.arg('timeframe'), sqlc.arg('ts'), sqlc.arg('open'), sqlc.arg('high'), sqlc.arg('low'), sqlc.arg('close'), sqlc.arg('volume'), sqlc.arg('source'), sqlc.arg('asset_type'))
ON CONFLICT (symbol, timeframe, ts) DO UPDATE
SET open      = EXCLUDED.open,
    high      = EXCLUDED.high,
    low       = EXCLUDED.low,
    close     = EXCLUDED.close,
    volume    = EXCLUDED.volume,
    source    = EXCLUDED.source,
    asset_type = EXCLUDED.asset_type
WHERE (bars.open, bars.high, bars.low, bars.close, bars.volume, bars.source, bars.asset_type)
    IS DISTINCT FROM (EXCLUDED.open, EXCLUDED.high, EXCLUDED.low, EXCLUDED.close, EXCLUDED.volume, EXCLUDED.source, EXCLUDED.asset_type);

-- name: UpsertBarsBatch :exec
INSERT INTO bars (symbol, timeframe, ts, open, high, low, close, volume, source, asset_type)
SELECT unnest(sqlc.arg(symbols)::text[]),
       unnest(sqlc.arg(timeframes)::text[]),
       unnest(sqlc.arg(timestamps)::timestamptz[]),
       unnest(sqlc.arg(opens)::double precision[]),
       unnest(sqlc.arg(highs)::double precision[]),
       unnest(sqlc.arg(lows)::double precision[]),
       unnest(sqlc.arg(closes)::double precision[]),
       unnest(sqlc.arg(volumes)::double precision[]),
       unnest(sqlc.arg(sources)::text[]),
       unnest(sqlc.arg(asset_types)::text[])
ON CONFLICT (symbol, timeframe, ts) DO UPDATE
SET open       = EXCLUDED.open,
    high       = EXCLUDED.high,
    low        = EXCLUDED.low,
    close      = EXCLUDED.close,
    volume     = EXCLUDED.volume,
    source     = EXCLUDED.source,
    asset_type = EXCLUDED.asset_type
WHERE (bars.open, bars.high, bars.low, bars.close, bars.volume, bars.source, bars.asset_type)
    IS DISTINCT FROM (EXCLUDED.open, EXCLUDED.high, EXCLUDED.low, EXCLUDED.close, EXCLUDED.volume, EXCLUDED.source, EXCLUDED.asset_type);

-- name: GetBars :many
SELECT symbol, timeframe, ts,
       open::float8 AS open,
       high::float8 AS high,
       low::float8 AS low,
       close::float8 AS close,
       volume::float8 AS volume,
       source, asset_type
FROM bars
WHERE symbol = sqlc.arg('symbol')
  AND timeframe = sqlc.arg('timeframe')
  AND ts >= sqlc.arg('from')
  AND ts <= sqlc.arg('through')
ORDER BY ts ASC;

-- name: GetBarsBatch :many
SELECT symbol, timeframe, ts,
       open::float8 AS open,
       high::float8 AS high,
       low::float8 AS low,
       close::float8 AS close,
       volume::float8 AS volume,
       source, asset_type
FROM bars
WHERE symbol = ANY(sqlc.arg('symbols')::text[])
  AND timeframe = sqlc.arg('timeframe')
  AND ts >= sqlc.arg('from')
  AND ts <= sqlc.arg('through')
ORDER BY symbol, ts ASC;

-- name: GetBarsColumnarBatch :many
-- Aggregate each bounded series separately; sorting an entire multi-symbol
-- panel can spill despite every individual series fitting in work_mem.
SELECT requested.symbol::text AS symbol, aggregated.*
FROM (SELECT unnest(sqlc.arg(symbols)::text[]) AS symbol,
             unnest(sqlc.arg(froms)::timestamptz[]) AS from_ts) AS requested
CROSS JOIN LATERAL (
    SELECT array_agg(ts ORDER BY ts)::timestamptz[] AS timestamps,
           array_agg(open ORDER BY ts)::float8[] AS opens,
           array_agg(high ORDER BY ts)::float8[] AS highs,
           array_agg(low ORDER BY ts)::float8[] AS lows,
           array_agg(close ORDER BY ts)::float8[] AS closes,
           array_agg(volume ORDER BY ts)::float8[] AS volumes,
           array_agg(source ORDER BY ts)::text[] AS sources,
           array_agg(asset_type ORDER BY ts)::text[] AS asset_types
    FROM (
        SELECT ts, open::float8 AS open, high::float8 AS high,
               low::float8 AS low, close::float8 AS close,
               volume::float8 AS volume, source, asset_type
        FROM bars
        WHERE bars.symbol = requested.symbol
          AND timeframe = sqlc.arg(timeframe)
          AND ts >= requested.from_ts
          AND ts <= sqlc.arg(through)
        ORDER BY ts ASC
        LIMIT sqlc.arg(page_limit)::int
    ) AS recent
    HAVING count(*) > 0
) AS aggregated
ORDER BY requested.symbol;

-- name: GetPriceBarsColumnarBatch :many
-- Aggregate each bounded series separately; sorting an entire multi-symbol
-- panel can spill despite every individual series fitting in work_mem.
SELECT requested.symbol::text AS symbol, aggregated.*
FROM (SELECT unnest(sqlc.arg(symbols)::text[]) AS symbol,
             unnest(sqlc.arg(froms)::timestamptz[]) AS from_ts) AS requested
CROSS JOIN LATERAL (
    SELECT array_agg(ts ORDER BY ts)::timestamptz[] AS timestamps,
           array_agg(open ORDER BY ts)::float8[] AS opens,
           array_agg(high ORDER BY ts)::float8[] AS highs,
           array_agg(low ORDER BY ts)::float8[] AS lows,
           array_agg(close ORDER BY ts)::float8[] AS closes,
           array_agg(volume ORDER BY ts)::float8[] AS volumes
    FROM (
        SELECT ts, open::float8 AS open, high::float8 AS high,
               low::float8 AS low, close::float8 AS close,
               volume::float8 AS volume
        FROM bars
        WHERE bars.symbol = requested.symbol
          AND timeframe = sqlc.arg(timeframe)
          AND ts >= requested.from_ts
          AND ts <= sqlc.arg(through)
        ORDER BY ts ASC
        LIMIT sqlc.arg(page_limit)::int
    ) AS recent
    HAVING count(*) > 0
) AS aggregated
ORDER BY requested.symbol;

-- name: GetRecentPriceBarsColumnarBatch :many
-- Aggregate each bounded series separately; sorting an entire multi-symbol
-- panel can spill despite every individual series fitting in work_mem.
SELECT requested.symbol::text AS symbol, aggregated.*
FROM (SELECT DISTINCT unnest(sqlc.arg(symbols)::text[]) AS symbol) AS requested
CROSS JOIN LATERAL (
    SELECT array_agg(ts ORDER BY ts)::timestamptz[] AS timestamps,
           array_agg(open ORDER BY ts)::float8[] AS opens,
           array_agg(high ORDER BY ts)::float8[] AS highs,
           array_agg(low ORDER BY ts)::float8[] AS lows,
           array_agg(close ORDER BY ts)::float8[] AS closes,
           array_agg(volume ORDER BY ts)::float8[] AS volumes
    FROM (
        SELECT ts, open::float8 AS open, high::float8 AS high,
               low::float8 AS low, close::float8 AS close,
               volume::float8 AS volume
        FROM bars
        WHERE bars.symbol = requested.symbol
          AND timeframe = sqlc.arg(timeframe)
          AND ts <= sqlc.arg(through)
        ORDER BY ts DESC
        LIMIT sqlc.arg(bar_limit)::int
    ) AS recent
    HAVING count(*) > 0
) AS aggregated
ORDER BY requested.symbol;

-- name: CountBars :one
SELECT count(*)
FROM bars
WHERE symbol = sqlc.arg('symbol')
  AND timeframe = sqlc.arg('timeframe')
  AND ts >= sqlc.arg('from')
  AND ts <= sqlc.arg('through');

-- name: BarRange :one
SELECT min(ts)::timestamptz AS first_ts, max(ts)::timestamptz AS last_ts
FROM bars
WHERE symbol = sqlc.arg('symbol')
  AND timeframe = sqlc.arg('timeframe');

-- name: BarRangeBatch :many
SELECT requested.symbol::text AS symbol,
       first_bar.ts::timestamptz AS first_ts,
       last_bar.ts::timestamptz AS last_ts
FROM unnest(sqlc.arg('symbols')::text[]) AS requested(symbol)
CROSS JOIN LATERAL (
    SELECT ts
    FROM bars
    WHERE bars.symbol = requested.symbol
      AND bars.timeframe = sqlc.arg('timeframe')
    ORDER BY ts ASC
    LIMIT 1
) first_bar
CROSS JOIN LATERAL (
    SELECT ts
    FROM bars
    WHERE bars.symbol = requested.symbol
      AND bars.timeframe = sqlc.arg('timeframe')
    ORDER BY ts DESC
    LIMIT 1
) last_bar
ORDER BY requested.symbol;

-- name: ListBarTimestamps :many
SELECT ts
FROM bars
WHERE symbol = sqlc.arg('symbol')
  AND timeframe = sqlc.arg('timeframe')
  AND ts >= sqlc.arg('from')
  AND ts <= sqlc.arg('through')
ORDER BY ts ASC;

-- name: LatestMarketMapBars :many
-- Returns the two most recent closes and latest volume for every requested
-- symbol. The API uses these rows to calculate the live treemap color and size.
-- Use bounded backward primary-key scans instead of ranking every historical
-- bar for the requested universe. The bars PK is (symbol, timeframe, ts).
SELECT requested.symbol::text AS symbol,
       latest.ts::timestamptz AS bar_at,
       latest.close::float8 AS price,
       coalesce(previous.close::float8, 0)::float8 AS previous,
       latest.volume::float8 AS volume
FROM unnest(sqlc.arg(symbols)::text[]) AS requested(symbol)
JOIN LATERAL (
    SELECT bars.ts, bars.close, bars.volume
    FROM bars
    WHERE bars.symbol = requested.symbol
      AND bars.timeframe = sqlc.arg(map_timeframe)
    ORDER BY bars.ts DESC
    LIMIT 1
) latest ON true
LEFT JOIN LATERAL (
    SELECT bars.close
    FROM bars
    WHERE bars.symbol = requested.symbol
      AND bars.timeframe = sqlc.arg(map_timeframe)
      AND bars.ts < latest.ts
    ORDER BY bars.ts DESC
    LIMIT 1
) previous ON true;

-- name: GetHistoryBarsPage :many
SELECT symbol, timeframe, ts,
       open::float8 AS open,
       high::float8 AS high,
       low::float8 AS low,
       close::float8 AS close,
       volume::float8 AS volume,
       CASE WHEN octet_length(source) <= 262144 THEN source ELSE '' END::text AS source, asset_type,
       octet_length(source) > 262144 AS oversized
FROM bars
WHERE symbol = sqlc.arg('symbol')
  AND timeframe = sqlc.arg('timeframe')
  AND ts >= sqlc.arg('from')
  AND ts <= sqlc.arg('through')
  AND ts > sqlc.arg(after_ts)::timestamptz
ORDER BY ts ASC
LIMIT 512;

-- name: PreviousBarClose :one
SELECT close::float8 FROM bars WHERE symbol = sqlc.arg(symbol) AND timeframe = sqlc.arg(timeframe)
  AND ts < sqlc.arg(watermark) ORDER BY ts DESC LIMIT 1;

-- name: BarCloseAtOrBefore :one
SELECT ts, close::float8 FROM bars WHERE symbol = sqlc.arg(symbol) AND timeframe = sqlc.arg(timeframe)
  AND ts <= sqlc.arg(watermark) ORDER BY ts DESC LIMIT 1;

-- name: TouchIngestedBarSeries :exec
INSERT INTO bar_series_usage (symbol, timeframe, last_used_at, last_ingested_at)
SELECT DISTINCT symbol, sqlc.arg(timeframe), now(), now()
FROM unnest(sqlc.arg(symbols)::text[]) AS input(symbol)
ON CONFLICT (symbol, timeframe) DO UPDATE
SET last_used_at = CASE
        WHEN bar_series_usage.last_used_at < now() - interval '5 minutes' THEN now()
        ELSE bar_series_usage.last_used_at
    END,
    last_ingested_at = now()
WHERE bar_series_usage.last_used_at < now() - interval '5 minutes'
   OR bar_series_usage.last_ingested_at IS NULL
   OR bar_series_usage.last_ingested_at < now() - interval '5 minutes';

-- name: TouchReadBarSeries :exec
INSERT INTO bar_series_usage (symbol, timeframe, last_used_at)
SELECT DISTINCT symbol, sqlc.arg(timeframe), now()
FROM unnest(sqlc.arg(symbols)::text[]) AS input(symbol)
ON CONFLICT (symbol, timeframe) DO UPDATE
SET last_used_at = CASE
        WHEN bar_series_usage.last_used_at < now() - interval '5 minutes' THEN now()
        ELSE bar_series_usage.last_used_at
    END
WHERE bar_series_usage.last_used_at < now() - interval '5 minutes';

-- name: BarsRelationSize :one
SELECT pg_total_relation_size('public.bars'::regclass)::bigint;

-- name: SeedBarSeriesUsage :exec
INSERT INTO bar_series_usage (symbol, timeframe, last_used_at, last_ingested_at)
SELECT symbol, timeframe, max(ts), max(ts)
FROM bars
WHERE NOT EXISTS (SELECT 1 FROM bar_series_usage LIMIT 1)
GROUP BY symbol, timeframe
ON CONFLICT (symbol, timeframe) DO NOTHING;

-- name: LeastRecentlyUsedBarSeries :one
SELECT symbol, timeframe
FROM bar_series_usage
WHERE NOT (symbol = ANY(sqlc.arg(protected_symbols)::text[]))
ORDER BY last_used_at ASC, symbol, timeframe
LIMIT 1;

-- name: DeleteBarsSeries :execrows
DELETE FROM bars
WHERE symbol = sqlc.arg(symbol) AND timeframe = sqlc.arg(timeframe);

-- name: DeleteBarSeriesUsage :exec
DELETE FROM bar_series_usage
WHERE symbol = sqlc.arg(symbol) AND timeframe = sqlc.arg(timeframe);

-- name: SelectBarExtremesBatch :many
WITH windows AS (
    SELECT ordinality - 1 AS window_index, symbol, from_ts, to_ts
    FROM unnest(sqlc.arg(symbols)::text[])
        WITH ORDINALITY AS symbols(symbol, ordinality)
    JOIN unnest(sqlc.arg(from_times)::timestamptz[])
        WITH ORDINALITY AS froms(from_ts, ordinality) USING (ordinality)
    JOIN unnest(sqlc.arg(to_times)::timestamptz[])
        WITH ORDINALITY AS tos(to_ts, ordinality) USING (ordinality)
)
SELECT w.window_index::integer,
       COALESCE(extremes.low, 0)::float8 AS low,
       COALESCE(extremes.high, 0)::float8 AS high,
       COALESCE(extremes.bar_count, 0)::bigint AS bar_count
FROM windows w
LEFT JOIN LATERAL (
    SELECT MIN(b.low::float8) AS low,
           MAX(b.high::float8) AS high,
           COUNT(b.ts) AS bar_count
    FROM bars b
    WHERE b.symbol = w.symbol
      AND b.timeframe = sqlc.arg(timeframe)
      AND b.ts >= w.from_ts
      AND b.ts <= w.to_ts
) extremes ON true
ORDER BY w.window_index;
