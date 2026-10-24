-- PostgreSQL advisory locks used to serialize work across processes.

-- name: TryAdvisoryLock :one
SELECT pg_try_advisory_lock(hashtextextended(sqlc.arg(lock_key), 0))::bool;

-- name: AcquireAdvisoryLock :exec
SELECT pg_advisory_lock(hashtextextended(sqlc.arg(lock_key), 0));

-- name: ReleaseAdvisoryLock :exec
SELECT pg_advisory_unlock(hashtextextended(sqlc.arg(lock_key), 0));

-- name: TryAdvisoryTransactionLock :one
SELECT pg_try_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key), 0))::bool;

-- name: AcquireAdvisoryTransactionLock :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(lock_key), 0));
