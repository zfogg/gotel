-- name: InsertMarketEventOccurrence :one
INSERT INTO market_event_occurrences (
    rule_id, rule_name, trigger_id, trigger_label, symbol,
    metric_type, timeframe, observed_at,
    previous_value, current_value, threshold_value,
    title, message, sound
)
VALUES (
    sqlc.arg(rule_id), sqlc.arg(rule_name), sqlc.arg(trigger_id), sqlc.arg(trigger_label), sqlc.arg(symbol),
    sqlc.arg(metric_type), sqlc.arg(timeframe), sqlc.arg(observed_at),
    sqlc.arg(previous_value), sqlc.arg(current_value), sqlc.arg(threshold_value),
    sqlc.arg(title), sqlc.arg(message), sqlc.arg(sound)
)
ON CONFLICT (rule_id, trigger_id, symbol, observed_at) DO NOTHING
RETURNING id;

-- name: ClaimPendingMarketEventOccurrences :many
WITH candidates AS (
    SELECT id
    FROM market_event_occurrences
    WHERE sent_at IS NULL
      AND rule_id = ANY(sqlc.arg(rule_ids)::text[])
      AND (
          delivery_claimed_at IS NULL OR
          delivery_claimed_at < now() - interval '5 minutes'
      )
    ORDER BY observed_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT sqlc.arg(limit_count)
)
UPDATE market_event_occurrences AS occurrence
SET delivery_claim_id = sqlc.arg(claim_id),
    delivery_claimed_at = now(),
    updated_at = now()
FROM candidates
WHERE occurrence.id = candidates.id
RETURNING occurrence.id, occurrence.rule_id, occurrence.rule_name,
          occurrence.trigger_id, occurrence.trigger_label, occurrence.symbol,
          occurrence.metric_type, occurrence.timeframe, occurrence.observed_at,
          occurrence.previous_value, occurrence.current_value, occurrence.threshold_value,
          occurrence.title, occurrence.message, occurrence.sound,
          occurrence.delivery_attempts, occurrence.last_error, occurrence.created_at;

-- name: MarkMarketEventOccurrenceSent :exec
UPDATE market_event_occurrences
SET sent_at = now(),
    delivery_attempts = delivery_attempts + 1,
    delivery_claim_id = NULL,
    delivery_claimed_at = NULL,
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND delivery_claim_id = sqlc.arg(claim_id)
  AND sent_at IS NULL;

-- name: MarkMarketEventOccurrenceFailed :exec
UPDATE market_event_occurrences
SET delivery_attempts = delivery_attempts + 1,
    delivery_claim_id = NULL,
    delivery_claimed_at = NULL,
    last_error = sqlc.arg(last_error),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND delivery_claim_id = sqlc.arg(claim_id)
  AND sent_at IS NULL;

-- name: GetMarketEventOccurrence :one
SELECT id, rule_id, trigger_id, symbol, observed_at, sent_at,
       delivery_attempts, last_error
FROM market_event_occurrences
WHERE id = sqlc.arg(id);
