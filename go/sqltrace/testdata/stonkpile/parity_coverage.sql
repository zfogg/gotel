-- name: RegisterParityCoverageScope :one
INSERT INTO tradebot_parity_coverage_scopes(account_id,broker,asset_class,timeframe,monitored_from)
VALUES(sqlc.arg(account_id),sqlc.arg(broker),sqlc.arg(asset_class),sqlc.arg(timeframe),sqlc.arg(monitored_from))
ON CONFLICT(account_id,broker,asset_class,timeframe) DO UPDATE SET account_id=EXCLUDED.account_id
RETURNING monitored_from;

-- name: EnsureParityCoverageSession :exec
INSERT INTO tradebot_parity_coverage_sessions(account_id,broker,asset_class,timeframe,session_open,session_close)
VALUES(sqlc.arg(account_id),sqlc.arg(broker),sqlc.arg(asset_class),sqlc.arg(timeframe),sqlc.arg(session_open),sqlc.arg(session_close))
ON CONFLICT DO NOTHING;

-- name: ListDailyParityCoverageCycles :many
SELECT r.receipt_id,r.watermark,r.started_at,r.finished_at,r.error,a.result AS audit
FROM tradebot_cycle_receipts r
LEFT JOIN tradebot_parity_cycle_audits a USING(receipt_id)
WHERE r.account_id=sqlc.arg(account_id) AND r.broker=sqlc.arg(broker)
 AND r.asset_class='equity' AND r.timeframe='1d'
 AND r.started_at>=sqlc.arg(started_from) AND r.started_at<sqlc.arg(started_through)
ORDER BY r.started_at,r.receipt_id;

-- name: ListParitySnapshotsForCycle :many
SELECT s.snapshot_id FROM tradebot_parity_snapshots s
JOIN tradebot_cycle_receipts r ON r.account_id=s.account_id AND r.broker=s.broker
 AND r.asset_class=s.asset_class AND r.timeframe=s.timeframe AND r.watermark=s.watermark
WHERE r.receipt_id=sqlc.arg(receipt_id)
 AND s.created_at>=r.started_at AND s.created_at<=r.finished_at
ORDER BY s.snapshot_id;

-- name: ListPendingDailyParityCycleAudits :many
SELECT r.receipt_id FROM tradebot_cycle_receipts r
LEFT JOIN tradebot_parity_cycle_audits a USING(receipt_id)
WHERE r.asset_class='equity' AND (r.timeframe='1d' OR (
 r.phase='market_gate' AND r.result->>'message'='market closed' AND EXISTS (
 SELECT 1 FROM tradebot_parity_coverage_sessions s WHERE s.account_id=r.account_id AND s.broker=r.broker
 AND s.asset_class=r.asset_class AND s.timeframe=r.timeframe AND r.watermark>=s.session_open AND r.watermark<s.session_close
))) AND r.finished_at IS NOT NULL
 AND a.receipt_id IS NULL AND EXISTS (
 SELECT 1 FROM tradebot_parity_coverage_scopes c WHERE c.account_id=r.account_id AND c.broker=r.broker
 AND c.asset_class=r.asset_class AND c.timeframe=r.timeframe AND r.started_at>=c.monitored_from
)
ORDER BY r.started_at,r.receipt_id LIMIT sqlc.arg(batch_limit);

-- name: SaveParityCycleAudit :exec
INSERT INTO tradebot_parity_cycle_audits(receipt_id,result) VALUES(sqlc.arg(receipt_id),sqlc.arg(result))
ON CONFLICT DO NOTHING;

-- name: ListParityCoverageSessions :many
SELECT * FROM tradebot_parity_coverage_sessions
WHERE account_id=sqlc.arg(account_id) AND broker=sqlc.arg(broker)
 AND asset_class=sqlc.arg(asset_class) AND timeframe=sqlc.arg(timeframe)
ORDER BY session_open;

-- name: SaveParityCoverageSession :exec
UPDATE tradebot_parity_coverage_sessions SET checked_at=sqlc.arg(checked_at),report=sqlc.arg(report)
WHERE account_id=sqlc.arg(account_id) AND broker=sqlc.arg(broker)
 AND asset_class=sqlc.arg(asset_class) AND timeframe=sqlc.arg(timeframe) AND session_open=sqlc.arg(session_open);

-- name: ListParityCoverageBoundaries :many
SELECT s.snapshot_id,s.watermark,s.created_at,b.expected_hash,b.received_hash,b.expected_count,b.received_count,b.completed_at,a.result AS audit
FROM tradebot_parity_snapshots s
LEFT JOIN tradebot_parity_batch_receipts b USING(snapshot_id)
LEFT JOIN tradebot_parity_boundary_audits a USING(snapshot_id)
WHERE s.account_id=sqlc.arg(account_id) AND s.broker=sqlc.arg(broker)
 AND s.asset_class=sqlc.arg(asset_class) AND s.timeframe=sqlc.arg(timeframe)
 AND s.watermark >= sqlc.arg(watermark_from) AND s.watermark <= sqlc.arg(watermark_through)
ORDER BY s.watermark,s.snapshot_id;

-- name: ListParityCoverageReceipts :many
SELECT r.*,a.result AS audit FROM tradebot_cycle_receipts r
LEFT JOIN tradebot_parity_cycle_audits a USING(receipt_id)
WHERE r.account_id=sqlc.arg(account_id) AND r.broker=sqlc.arg(broker)
 AND r.asset_class=sqlc.arg(asset_class) AND r.timeframe=sqlc.arg(timeframe)
 AND ((r.watermark >= sqlc.arg(watermark_from) AND r.watermark <= sqlc.arg(watermark_through))
 OR (r.watermark IS NULL AND r.started_at >= sqlc.arg(watermark_from) AND r.started_at <= sqlc.arg(receipts_through)))
ORDER BY r.started_at,r.receipt_id;

-- name: ListParityCoverageFills :many
SELECT f.fill_id,f.filled_at FROM tradebot_fill_events f JOIN tradebot_intents i USING(intent_id)
WHERE i.account_id=sqlc.arg(account_id) AND i.broker=sqlc.arg(broker)
 AND i.asset_class=sqlc.arg(asset_class) AND i.timeframe=sqlc.arg(timeframe)
 AND i.mode IN ('canonical','exit_only','manage_only')
 AND f.filled_at >= sqlc.arg(filled_from) AND f.filled_at < sqlc.arg(filled_through)
ORDER BY f.filled_at,f.fill_id;

-- name: ListPendingParityBoundaryAudits :many
-- Rank fresh evidence and revision rechecks independently within each account session.
-- A busy fresh stream must not leave corrected historical verdicts starved.
WITH pending AS (
SELECT s.snapshot_id,s.watermark,a.snapshot_id IS NOT NULL AS is_recheck,
 row_number() OVER (PARTITION BY s.account_id,s.broker,s.asset_class,s.timeframe,c.session_open,(a.snapshot_id IS NOT NULL) ORDER BY s.watermark,s.snapshot_id) AS scope_rank
FROM tradebot_parity_snapshots s
JOIN tradebot_parity_coverage_sessions c ON c.account_id=s.account_id AND c.broker=s.broker
 AND c.asset_class=s.asset_class AND c.timeframe=s.timeframe
 AND s.watermark>=c.session_open AND s.watermark<c.session_close
LEFT JOIN tradebot_parity_boundary_audits a USING(snapshot_id)
WHERE (a.snapshot_id IS NULL OR (
 a.result->>'status' IN ('unmatched','unverifiable')
 AND COALESCE(a.result->>'revision','') <> sqlc.arg(engine_revision)::text
)) AND EXISTS (
 SELECT 1 FROM tradebot_cycle_receipts r WHERE r.account_id=s.account_id AND r.broker=s.broker
 AND r.asset_class=s.asset_class AND r.timeframe=s.timeframe AND r.watermark=s.watermark
 AND r.finished_at IS NOT NULL AND r.error=''
)
)
SELECT snapshot_id FROM pending ORDER BY scope_rank,is_recheck,watermark,snapshot_id LIMIT sqlc.arg(batch_limit);

-- name: SaveParityBoundaryAudit :exec
INSERT INTO tradebot_parity_boundary_audits(snapshot_id,result) VALUES(sqlc.arg(snapshot_id),sqlc.arg(result))
ON CONFLICT (snapshot_id) DO UPDATE SET
 result = EXCLUDED.result || jsonb_build_object('previousAudit',jsonb_build_object(
   'checkedAt',tradebot_parity_boundary_audits.checked_at,'result',tradebot_parity_boundary_audits.result)),
 checked_at = now()
WHERE tradebot_parity_boundary_audits.result->>'status' IN ('unmatched','unverifiable')
 AND COALESCE(EXCLUDED.result->>'revision','') <> ''
 AND COALESCE(tradebot_parity_boundary_audits.result->>'revision','') <> EXCLUDED.result->>'revision';
