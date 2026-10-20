-- Explicit, bounded read projections for the Dynamic Portfolio workspace.
-- name: RecordDynamicPortfolioResolution :exec
INSERT INTO dynamic_portfolio_resolution_observations(timeframe,resolved_at,activity,selection)
VALUES (sqlc.arg(timeframe),sqlc.arg(resolved_at),sqlc.arg(activity),sqlc.arg(selection))
ON CONFLICT (timeframe) DO UPDATE SET resolved_at=EXCLUDED.resolved_at,observed_at=now(),activity=EXCLUDED.activity,selection=EXCLUDED.selection
WHERE EXCLUDED.resolved_at >= dynamic_portfolio_resolution_observations.resolved_at;

-- name: DPWebWorkerResolution :one
SELECT selection,activity,resolved_at,observed_at
FROM dynamic_portfolio_resolution_observations WHERE timeframe=sqlc.arg(timeframe);

-- name: DPWebRevisions :many
SELECT jsonb_strip_nulls(to_jsonb(r) || jsonb_build_object('result', to_jsonb(rr))) AS data
FROM dynamic_portfolio_revisions r
LEFT JOIN dynamic_portfolio_revision_results rr ON rr.revision_id = r.id
WHERE r.timeframe = sqlc.arg(timeframe)
 AND r.created_at <= sqlc.arg(snapshot_at)::timestamptz
 AND (sqlc.arg(before_id)::bigint = 0 OR (r.created_at,r.id) < (sqlc.arg(before_at)::timestamptz,sqlc.arg(before_id)::bigint))
 AND (sqlc.arg(search)::text = '' OR r.id::text = sqlc.arg(search)::text
      OR to_char(r.created_at AT TIME ZONE 'UTC','YYYY-MM-DD') = sqlc.arg(search)::text)
ORDER BY r.created_at DESC,r.id DESC LIMIT sqlc.arg(page_size)::integer;

-- name: DPWebRevision :one
SELECT jsonb_strip_nulls(to_jsonb(r) || jsonb_build_object('result', to_jsonb(rr))) AS data
FROM dynamic_portfolio_revisions r
LEFT JOIN dynamic_portfolio_revision_results rr ON rr.revision_id = r.id
WHERE r.id = sqlc.arg(revision_id) AND r.timeframe = sqlc.arg(timeframe);

-- name: DPWebRevisionNeighbors :one
SELECT
 COALESCE((SELECT p.id FROM dynamic_portfolio_revisions p WHERE p.timeframe=r.timeframe AND (p.created_at,p.id)<(r.created_at,r.id) ORDER BY p.created_at DESC,p.id DESC LIMIT 1),0)::bigint AS older_id,
 COALESCE((SELECT p.id FROM dynamic_portfolio_revisions p WHERE p.timeframe=r.timeframe AND (p.created_at,p.id)>(r.created_at,r.id) ORDER BY p.created_at,p.id LIMIT 1),0)::bigint AS newer_id
FROM dynamic_portfolio_revisions r WHERE r.id=sqlc.arg(revision_id);

-- name: DPWebLatest :one
SELECT r.id FROM dynamic_portfolio_revisions r
JOIN dynamic_portfolio_revision_results rr ON rr.revision_id=r.id
WHERE r.timeframe=sqlc.arg(timeframe) AND r.status='active'
ORDER BY r.created_at DESC,r.id DESC LIMIT 1;

-- name: DPWebPeriods :many
SELECT jsonb_strip_nulls(to_jsonb(p)) AS data FROM dynamic_portfolio_revision_periods p
WHERE p.revision_id=sqlc.arg(revision_id) AND p.effective_from < sqlc.arg(before_at)::timestamptz
ORDER BY p.effective_from DESC,p.id DESC LIMIT 101;

-- name: DPWebPeriodAt :one
SELECT jsonb_strip_nulls(to_jsonb(p)) AS data FROM dynamic_portfolio_revision_periods p
WHERE p.revision_id=sqlc.arg(revision_id) AND p.effective_from<=sqlc.arg(at_time)::timestamptz
AND (p.effective_to IS NULL OR p.effective_to>sqlc.arg(at_time)::timestamptz)
ORDER BY p.effective_from DESC LIMIT 1;

-- name: DPWebPreviousDecision :one
SELECT jsonb_strip_nulls(to_jsonb(p)) AS data FROM dynamic_portfolio_revision_periods p
WHERE p.revision_id=sqlc.arg(revision_id) AND p.effective_from<sqlc.arg(at_time)::timestamptz
ORDER BY p.effective_from DESC LIMIT 1;

-- name: DPWebNextDecision :one
SELECT jsonb_strip_nulls(to_jsonb(p)) AS data FROM dynamic_portfolio_revision_periods p
WHERE p.revision_id=sqlc.arg(revision_id) AND p.effective_from>sqlc.arg(at_time)::timestamptz
ORDER BY p.effective_from LIMIT 1;

-- name: DPWebRuns :many
SELECT jsonb_strip_nulls(to_jsonb(r) || jsonb_build_object('deterministic_seed',r.deterministic_seed::text,'completed_contexts',
 (SELECT count(*) FROM portfolio_contexts c JOIN portfolio_context_results cr ON cr.context_id=c.id WHERE c.run_id=r.id))) AS data
FROM portfolio_research_runs r WHERE r.timeframe=sqlc.arg(timeframe)
AND r.created_at<=sqlc.arg(snapshot_at)::timestamptz
AND (sqlc.arg(before_id)::bigint=0 OR (r.created_at,r.id)<(sqlc.arg(before_at)::timestamptz,sqlc.arg(before_id)::bigint))
ORDER BY r.created_at DESC,r.id DESC LIMIT 26;

-- name: DPWebRun :one
SELECT jsonb_strip_nulls(to_jsonb(r) || jsonb_build_object('deterministic_seed',r.deterministic_seed::text,'completed_contexts',
 (SELECT count(*) FROM portfolio_contexts c JOIN portfolio_context_results cr ON cr.context_id=c.id WHERE c.run_id=r.id))) AS data
FROM portfolio_research_runs r WHERE r.id=sqlc.arg(run_id) AND r.timeframe=sqlc.arg(timeframe);

-- name: DPWebContexts :many
SELECT jsonb_strip_nulls(to_jsonb(c) || jsonb_build_object('result',to_jsonb(cr))) AS data
FROM portfolio_contexts c LEFT JOIN portfolio_context_results cr ON cr.context_id=c.id
WHERE c.run_id=sqlc.arg(run_id) AND c.id>sqlc.arg(after_id)::bigint
ORDER BY c.id LIMIT 51;

-- name: DPWebContext :one
SELECT jsonb_strip_nulls(to_jsonb(c) || jsonb_build_object('result',to_jsonb(cr))) AS data
FROM portfolio_contexts c LEFT JOIN portfolio_context_results cr ON cr.context_id=c.id
WHERE c.run_id=sqlc.arg(run_id) AND c.id=sqlc.arg(context_id);

-- name: DPWebWindows :many
SELECT to_jsonb(w) AS data FROM portfolio_context_window_results w
WHERE w.context_id=sqlc.arg(context_id) ORDER BY w.window_index LIMIT 1001;

-- name: DPWebSymbols :many
SELECT to_jsonb(s) AS data FROM portfolio_symbol_results s
WHERE s.context_id=sqlc.arg(context_id) ORDER BY s.ticker LIMIT 1001;

-- name: DPWebMarginals :many
SELECT to_jsonb(m) AS data FROM portfolio_paired_marginals m
WHERE m.run_id=sqlc.arg(run_id) AND (m.baseline_context_id=sqlc.arg(context_id) OR m.included_context_id=sqlc.arg(context_id))
ORDER BY m.id LIMIT 1001;

-- Only completed, matching canonical batch receipts are worker evidence. Do
-- not infer a revision ID from matching names or from today's configuration.
-- name: DPWebWorkerObservations :many
SELECT DISTINCT ON (s.account_id,s.broker)
 s.account_id,s.broker,s.snapshot_id,s.watermark,r.completed_at AS observed_at,
 s.portfolio_hash,s.universe,s.policy_context
FROM tradebot_parity_snapshots s JOIN tradebot_parity_batch_receipts r ON r.snapshot_id=s.snapshot_id
WHERE s.asset_class='equity' AND s.timeframe=sqlc.arg(timeframe)
 AND r.completed_at IS NOT NULL AND r.received_hash=r.expected_hash AND r.received_count=r.expected_count
ORDER BY s.account_id,s.broker,s.watermark DESC,s.created_at DESC,s.snapshot_id DESC LIMIT 51;
