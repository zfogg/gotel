-- name: ClaimOperatorOperation :execrows
INSERT INTO operator_operations (operation_id, request_hash, request_path, request_body)
VALUES (sqlc.arg(operation_id), sqlc.arg(request_hash), sqlc.arg(request_path), sqlc.arg(request_body))
ON CONFLICT DO NOTHING;

-- name: GetOperatorOperation :one
SELECT * FROM operator_operations WHERE operation_id = sqlc.arg(operation_id);

-- name: CompleteOperatorOperation :exec
UPDATE operator_operations
SET response_status = sqlc.arg(response_status), response_body = sqlc.arg(response_body),
    response_content_type = sqlc.arg(response_content_type)
WHERE operation_id = sqlc.arg(operation_id) AND response_status IS NULL;
