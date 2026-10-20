-- name: InsertEquityBorrowSnapshot :one
INSERT INTO equity_borrow_snapshots (
  account_id, symbol, borrow_status, error_code, error_message,
  available_qty, fee_per_share, total_fee, quoted_at, quote_expires_at,
  trading_date, observed_at, source, raw_response
) VALUES (
  sqlc.arg('account_id'), sqlc.arg('symbol'), sqlc.arg('borrow_status'), sqlc.arg('error_code'), sqlc.arg('error_message'), sqlc.arg('available_qty'), sqlc.arg('fee_per_share'), sqlc.arg('total_fee'), sqlc.arg('quoted_at'), sqlc.arg('quote_expires_at'), sqlc.arg('trading_date'), sqlc.arg('observed_at'), sqlc.arg('source'), sqlc.arg('raw_response')
)
RETURNING *;

-- name: GetLatestEquityBorrowSnapshot :one
SELECT *
FROM equity_borrow_snapshots
WHERE account_id = sqlc.arg('account_id') AND symbol = sqlc.arg('symbol')
ORDER BY observed_at DESC, id DESC
LIMIT 1;

-- name: GetEquityBorrowSnapshotAt :one
SELECT *
FROM equity_borrow_snapshots
WHERE account_id = sqlc.arg('account_id') AND symbol = sqlc.arg('symbol') AND observed_at <= sqlc.arg('observed_at')
ORDER BY observed_at DESC, id DESC
LIMIT 1;

-- name: GetAnyEquityBorrowSnapshotAt :one
SELECT *
FROM equity_borrow_snapshots
WHERE symbol = sqlc.arg('symbol') AND observed_at <= sqlc.arg('observed_at')
ORDER BY observed_at DESC, id DESC
LIMIT 1;

-- name: ListLatestEquityBorrowSnapshots :many
SELECT DISTINCT ON (symbol) *
FROM equity_borrow_snapshots
WHERE account_id = sqlc.arg('account_id') AND symbol = ANY(sqlc.arg(symbols)::text[])
ORDER BY symbol, observed_at DESC, id DESC;

-- name: GetEquityBorrowSnapshotFingerprint :one
SELECT
  count(*)::int AS observation_count,
  md5(COALESCE(string_agg(
    concat_ws('|',
      id::text, account_id, symbol, borrow_status, error_code, error_message,
      available_qty::text, fee_per_share::text, total_fee::text,
      quoted_at::text, quote_expires_at::text, trading_date::text,
      observed_at::text, source, raw_response::text
    ), E'\n' ORDER BY observed_at, id
  ), '')) AS fingerprint
FROM equity_borrow_snapshots
WHERE account_id = sqlc.arg(account_id)
  AND observed_at >= sqlc.arg(observed_from)::timestamptz
  AND observed_at < sqlc.arg(observed_through)::timestamptz;

-- name: UpsertLocateRequest :one
INSERT INTO locate_requests (
  request_id, broker_locate_id, account_id, symbol, position_cycle_id,
  intent_id, quote_observed_at, requested_qty, located_qty, limit_price,
  located_price, total_fee, all_or_none, status, rejection_reason,
  idempotency_key, expires_at, raw_response
) VALUES (
  sqlc.arg('request_id'), sqlc.arg('broker_locate_id'), sqlc.arg('account_id'), sqlc.arg('symbol'), sqlc.arg('position_cycle_id'), sqlc.arg('intent_id'), sqlc.arg('quote_observed_at'), sqlc.arg('requested_qty'), sqlc.arg('located_qty'), sqlc.arg('limit_price'),
  sqlc.arg('located_price'), sqlc.arg('total_fee'), sqlc.arg('all_or_none'), sqlc.arg('status'), sqlc.arg('rejection_reason'), sqlc.arg('idempotency_key'), sqlc.arg('expires_at'), sqlc.arg('raw_response')
)
ON CONFLICT (request_id) DO UPDATE SET
  broker_locate_id = EXCLUDED.broker_locate_id,
  located_qty = EXCLUDED.located_qty,
  located_price = EXCLUDED.located_price,
  total_fee = EXCLUDED.total_fee,
  status = EXCLUDED.status,
  rejection_reason = EXCLUDED.rejection_reason,
  expires_at = EXCLUDED.expires_at,
  raw_response = EXCLUDED.raw_response,
  updated_at = now()
RETURNING *;

-- name: GetLocateRequestByIdempotencyKey :one
SELECT *
FROM locate_requests
WHERE idempotency_key = sqlc.arg('idempotencyKey');

-- name: GetLocateRequest :one
SELECT *
FROM locate_requests
WHERE request_id = sqlc.arg('requestID');

-- name: GetLatestConsumedLocateForSymbol :one
SELECT *
FROM locate_requests
WHERE account_id = sqlc.arg('account_id') AND symbol = sqlc.arg('symbol') AND consumed_at IS NOT NULL
ORDER BY consumed_at DESC, created_at DESC
LIMIT 1;

-- name: MarkLocateConsumed :exec
UPDATE locate_requests
SET consumed_at = now(), broker_order_id = sqlc.arg('broker_order_id'), updated_at = now()
WHERE broker_locate_id = sqlc.arg('broker_locate_id') AND consumed_at IS NULL;

-- name: ListUnnotifiedPaidLocateFailures :many
SELECT *
FROM locate_requests
WHERE total_fee > 0
  AND consumed_at IS NULL
  AND broker_order_id IS NULL
  AND failure_notified_at IS NULL
  AND updated_at < now() - interval '2 minutes'
ORDER BY updated_at
LIMIT sqlc.arg('limit');

-- name: MarkLocateFailureNotified :exec
UPDATE locate_requests
SET failure_notified_at = now(), updated_at = now()
WHERE request_id = sqlc.arg('requestID') AND failure_notified_at IS NULL;

-- name: InsertExecutionCostEvent :one
INSERT INTO execution_cost_events (
  event_id, source_ref, account_id, symbol, position_cycle_id, intent_id,
  broker_order_id, fill_id, kind, amount, currency, effective_at, raw_response
) VALUES (
  sqlc.arg('event_id'), sqlc.arg('source_ref'), sqlc.arg('account_id'), sqlc.arg('symbol'), sqlc.arg('position_cycle_id'), sqlc.arg('intent_id'), sqlc.arg('broker_order_id'), sqlc.arg('fill_id'), sqlc.arg('kind'), sqlc.arg('amount'), sqlc.arg('currency'), sqlc.arg('effective_at'), sqlc.arg('raw_response')
)
ON CONFLICT (event_id) DO UPDATE SET event_id = EXCLUDED.event_id
RETURNING *;

-- name: HasMatchingLocateCost :one
SELECT EXISTS (
  SELECT 1
  FROM execution_cost_events
  WHERE account_id = sqlc.arg('account_id')
    AND kind = 'locate_fee'
    AND symbol = sqlc.arg('symbol')
    AND amount = sqlc.arg('amount')
    AND effective_at BETWEEN sqlc.arg(effective_at)::timestamptz - interval '36 hours'
                         AND sqlc.arg(effective_at)::timestamptz + interval '36 hours'
) AS found;

-- name: ListUnallocatedExecutionCosts :many
SELECT *
FROM execution_cost_events
WHERE account_id = sqlc.arg('account_id')
  AND position_cycle_id = sqlc.arg('position_cycle_id')
  AND allocated_amount < amount
ORDER BY effective_at, event_id;

-- name: AllocateExecutionCost :exec
UPDATE execution_cost_events
SET allocated_amount = LEAST(amount, allocated_amount + sqlc.arg('allocated_amount'))
WHERE event_id = sqlc.arg('event_id');

-- name: SumExecutionCostsForPosition :one
SELECT
  COALESCE(sum(amount), 0)::numeric AS total,
  COALESCE(sum(amount) FILTER (WHERE kind = 'commission'), 0)::numeric AS commission,
  COALESCE(sum(amount) FILTER (WHERE kind = 'crypto_fee'), 0)::numeric AS crypto_fee,
  COALESCE(sum(amount) FILTER (WHERE kind = 'locate_fee'), 0)::numeric AS locate_fee,
  COALESCE(sum(amount) FILTER (WHERE kind = 'borrow_fee'), 0)::numeric AS borrow_fee,
  COALESCE(sum(amount) FILTER (WHERE kind = 'regulatory_fee'), 0)::numeric AS regulatory_fee,
  COALESCE(sum(amount) FILTER (WHERE kind = 'exchange_fee'), 0)::numeric AS exchange_fee,
  COALESCE(sum(amount) FILTER (WHERE kind = 'other'), 0)::numeric AS other
FROM execution_cost_events
WHERE account_id = sqlc.arg('account_id') AND position_cycle_id = sqlc.arg('position_cycle_id');
