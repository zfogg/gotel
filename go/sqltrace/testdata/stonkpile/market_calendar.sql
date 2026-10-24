-- Market calendar queries

-- name: GetMarketCalendar :one
SELECT is_open FROM market_calendar WHERE date = sqlc.arg('date') AND market = sqlc.arg('market');

-- name: GetMarketCalendarHours :one
SELECT open_time::text, close_time::text FROM market_calendar WHERE date = sqlc.arg('date') AND market = sqlc.arg('market');

-- name: UpsertMarketCalendar :exec
INSERT INTO market_calendar (date, market, timezone, open_time, close_time, is_open)
VALUES (sqlc.arg('date'), sqlc.arg('market'), sqlc.arg('timezone'), sqlc.arg('open_time'), sqlc.arg('close_time'), sqlc.arg('is_open'))
ON CONFLICT (date, market) DO UPDATE SET
    open_time = EXCLUDED.open_time,
    close_time = EXCLUDED.close_time,
    is_open = EXCLUDED.is_open;
-- name: UpsertNYSEMarketCalendar :exec
INSERT INTO market_calendar (date, market, timezone, open_time, close_time, is_open)
VALUES (sqlc.arg(date)::text::date, 'NYSE', 'America/New_York', sqlc.arg(open_time)::text::time, sqlc.arg(close_time)::text::time, true)
ON CONFLICT (date, market) DO UPDATE SET open_time = EXCLUDED.open_time, close_time = EXCLUDED.close_time, is_open = EXCLUDED.is_open;
