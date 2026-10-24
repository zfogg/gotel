-- name: CreateWebNotification :one
INSERT INTO web_notifications (title, content, kind, duration_ms, priority)
VALUES (sqlc.arg(title), sqlc.arg(content), sqlc.arg(kind), sqlc.arg(duration_ms), sqlc.arg(priority))
RETURNING id, title, content, kind, duration_ms, priority, created_at;

-- name: ListWebNotifications :many
SELECT id, title, content, kind, duration_ms, priority, created_at, count(*) OVER () AS total_count
FROM web_notifications
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(limit_count) OFFSET sqlc.arg(offset_count);
