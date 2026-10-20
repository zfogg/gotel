-- name: UpsertStrategy :one
INSERT INTO strategies (name, kind, params, version)
VALUES (sqlc.arg('name'), sqlc.arg('kind'), sqlc.arg('params'), sqlc.arg('version'))
ON CONFLICT (name, version) DO UPDATE
SET kind = EXCLUDED.kind
RETURNING id, name, kind, params, version, created_at;

-- name: UpsertStrategiesBatch :many
INSERT INTO strategies (name, kind, params, version)
SELECT unnest(sqlc.arg(names)::text[]),
       unnest(sqlc.arg(kinds)::text[]),
       unnest(sqlc.arg(params_json)::text[])::jsonb,
       unnest(sqlc.arg(versions)::integer[])
ON CONFLICT (name, version) DO UPDATE
SET kind = EXCLUDED.kind
RETURNING id, name, kind, params, version, created_at;

-- name: GetStrategy :one
SELECT id, name, kind, params, version, created_at
FROM strategies
WHERE id = sqlc.arg('id');

-- name: GetStrategyByName :one
SELECT id, name, kind, params, version, created_at
FROM strategies
WHERE name = sqlc.arg('name') AND version = sqlc.arg('version');

-- name: ListStrategies :many
SELECT id, name, kind, params, version, created_at
FROM strategies
ORDER BY name, version;
