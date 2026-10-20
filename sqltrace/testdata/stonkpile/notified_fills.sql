-- Notified fills tracking queries

-- name: GetLastNotifiedFillTime :one
SELECT last_fill_ts FROM notified_fills WHERE key = 'fill' ORDER BY id DESC LIMIT 1;

-- name: UpdateNotifiedFillTime :exec
UPDATE notified_fills SET last_fill_ts = sqlc.arg('lastFillTs'), updated_at = now()
WHERE key = 'fill' AND last_fill_ts < sqlc.arg('lastFillTs');

-- name: InsertNotifiedFillTime :exec
INSERT INTO notified_fills (last_fill_ts, key) VALUES (sqlc.arg('lastFillTs'), 'fill')
ON CONFLICT DO NOTHING;

-- name: GetAlertTime :one
SELECT last_fill_ts FROM notified_fills WHERE key = sqlc.arg('key') ORDER BY id DESC LIMIT 1;

-- name: UpsertAlertTime :exec
INSERT INTO notified_fills (last_fill_ts, key) VALUES (sqlc.arg('last_fill_ts'), sqlc.arg('key'))
ON CONFLICT (key) DO UPDATE SET last_fill_ts = EXCLUDED.last_fill_ts, updated_at = now();

-- name: GetDailyLossAlertState :one
SELECT last_fill_ts, daily_loss_pct
FROM notified_fills
WHERE key = sqlc.arg('key');

-- name: UpsertDailyLossAlertState :exec
INSERT INTO notified_fills (last_fill_ts, key, daily_loss_pct)
VALUES (sqlc.arg('last_fill_ts'), sqlc.arg('key'), sqlc.arg('daily_loss_pct'))
ON CONFLICT (key) DO UPDATE SET
    last_fill_ts = EXCLUDED.last_fill_ts,
    daily_loss_pct = EXCLUDED.daily_loss_pct,
    updated_at = now();
