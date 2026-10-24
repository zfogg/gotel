-- name: EnqueueParityVerificationJob :exec
INSERT INTO tradebot_parity_verification_jobs(job_key,intent_id,evaluation_hash,payload,priority)
VALUES(sqlc.arg(job_key),sqlc.arg(intent_id),sqlc.arg(evaluation_hash),sqlc.arg(payload)::jsonb,sqlc.arg(priority))
ON CONFLICT(job_key) DO NOTHING;

-- name: GetParityVerificationJob :one
SELECT * FROM tradebot_parity_verification_jobs WHERE job_key=sqlc.arg(job_key);

-- name: RequeueHistoricalClosingWindows :execrows
-- Recover obsolete terminal closing windows across rolling upgrades. Running
-- leases remain owned, and attempts remain monotonic to fence old workers.
UPDATE tradebot_parity_verification_jobs j
SET payload=j.payload || jsonb_build_object('supersededHistoricalWindowVerdict',
      COALESCE(j.payload->'supersededHistoricalWindowVerdict',j.result),'verificationRetryBase',j.attempts),
    state='queued', result=NULL, lease_until=NULL,
    next_attempt_at=now(), updated_at=now()
WHERE j.state='completed' AND j.payload->'Trade'->>'Close'='true'
  AND j.result->>'status'='unmatched'
  AND COALESCE((j.result->>'engineVersion')::integer,0)<sqlc.arg(minimum_engine_version)::integer
  AND EXISTS (SELECT 1 FROM tradebot_parity_replay_batches r WHERE r.batch_id=j.result->>'reportId');

-- name: RequeueCompatibleOpeningReplay :execrows
UPDATE tradebot_parity_verification_jobs j
SET payload=j.payload || jsonb_build_object('supersededOpeningEngineVerdict',
 COALESCE(j.payload->'supersededOpeningEngineVerdict',j.result),'verificationRetryBase',j.attempts),
 state='queued',result=NULL,lease_until=NULL,next_attempt_at=now(),updated_at=now()
WHERE j.state='completed' AND j.payload->'Trade'->>'Close' IS DISTINCT FROM 'true'
 AND j.payload->>'StrictDecision'='true' AND j.result->>'status'='unverifiable'
 AND j.result->>'explanation' LIKE 'pinned engine %cannot be executed%'
 AND j.payload->'Identity'->>'engineVersion'=sqlc.arg(engine_version)::text
 AND j.payload->'Identity'->>'engineHash'=sqlc.arg(engine_hash)::text;

-- name: ListParityVerificationJobsByKeys :many
SELECT * FROM tradebot_parity_verification_jobs WHERE job_key=ANY(sqlc.arg(job_keys)::text[]);

-- name: ClaimParityVerificationJobs :many
WITH next_batch AS (
 SELECT payload FROM tradebot_parity_verification_jobs
 WHERE (state IN ('queued','retry') AND next_attempt_at<=now()) OR (state='running' AND lease_until<=now())
 ORDER BY priority + CASE WHEN payload ? 'supersededHistoricalWindowVerdict' THEN 2 ELSE 0 END,
 CASE WHEN payload ? 'supersededHistoricalWindowVerdict' THEN created_at END DESC,created_at,job_key FOR UPDATE SKIP LOCKED LIMIT 1
), ready AS (
 SELECT j.job_key FROM tradebot_parity_verification_jobs j CROSS JOIN next_batch b
 WHERE ((j.state IN ('queued','retry') AND j.next_attempt_at<=now()) OR (j.state='running' AND j.lease_until<=now()))
 AND (j.payload ? 'supersededHistoricalWindowVerdict')=(b.payload ? 'supersededHistoricalWindowVerdict')
 AND COALESCE(j.payload->'Trade'->>'Close','false')=COALESCE(b.payload->'Trade'->>'Close','false')
 AND CASE WHEN j.payload->'Trade'->>'Close'='true' THEN
   j.payload->>'AssetClass'=b.payload->>'AssetClass' AND j.payload->>'Timeframe'=b.payload->>'Timeframe'
   AND left(j.payload->'Trade'->>'ExitAt',10)=left(b.payload->'Trade'->>'ExitAt',10)
 ELSE COALESCE(j.payload->>'SnapshotID','')=COALESCE(b.payload->>'SnapshotID','') END
 ORDER BY j.priority + CASE WHEN j.payload ? 'supersededHistoricalWindowVerdict' THEN 2 ELSE 0 END,
 CASE WHEN j.payload ? 'supersededHistoricalWindowVerdict' THEN j.created_at END DESC,j.created_at,j.job_key FOR UPDATE OF j SKIP LOCKED LIMIT sqlc.arg(batch_limit)
)
UPDATE tradebot_parity_verification_jobs j SET state='running',attempts=attempts+1,
 lease_until=now()+interval '20 minutes',updated_at=now()
FROM ready WHERE j.job_key=ready.job_key RETURNING j.*;

-- name: CompleteParityVerificationJob :exec
UPDATE tradebot_parity_verification_jobs SET state='completed',result=sqlc.arg(result)::jsonb,
 lease_until=NULL,updated_at=now()
WHERE job_key=sqlc.arg(job_key) AND state='running' AND attempts=sqlc.arg(attempts);

-- name: RetryParityVerificationJob :exec
UPDATE tradebot_parity_verification_jobs SET state='retry',last_error=sqlc.arg(last_error),
 next_attempt_at=sqlc.arg(next_attempt_at)::timestamptz,lease_until=NULL,updated_at=now()
WHERE job_key=sqlc.arg(job_key) AND state='running' AND attempts=sqlc.arg(attempts);
