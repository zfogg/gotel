-- name: UpsertVIXSnapshot :one
INSERT INTO vix_snapshots (date, level)
VALUES (sqlc.arg('date'), sqlc.arg('level'))
ON CONFLICT (date) DO UPDATE
SET level = EXCLUDED.level
RETURNING date, level, created_at;

-- name: ListVIXSnapshots :many
SELECT date, level
FROM vix_snapshots
WHERE date >= sqlc.arg('date')
ORDER BY date ASC;

-- name: GetLatestVIXSnapshot :one
SELECT date, level
FROM vix_snapshots
ORDER BY date DESC
LIMIT 1;
