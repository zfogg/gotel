-- name: UpsertSignalContract :one
INSERT INTO signal_contracts (
    strategy_id, strategy_name, strategy_kind, evaluation_version,
    signal_schema_version, asset_type, timeframe, portfolio_hash,
    strategy_config_hash, tradeable_symbols, context_symbols, canonical_contract
)
VALUES (
    sqlc.arg(strategy_id), sqlc.arg(strategy_name), sqlc.arg(strategy_kind),
    sqlc.arg(evaluation_version), sqlc.arg(signal_schema_version),
    sqlc.arg(asset_type), sqlc.arg(timeframe), sqlc.arg(portfolio_hash),
    sqlc.arg(strategy_config_hash), sqlc.arg(tradeable_symbols),
    sqlc.arg(context_symbols), sqlc.arg(canonical_contract)
)
ON CONFLICT (strategy_id, evaluation_version, signal_schema_version,
             asset_type, timeframe, portfolio_hash, strategy_config_hash)
DO UPDATE SET
    strategy_name = EXCLUDED.strategy_name,
    strategy_kind = EXCLUDED.strategy_kind,
    tradeable_symbols = EXCLUDED.tradeable_symbols,
    context_symbols = EXCLUDED.context_symbols,
    canonical_contract = EXCLUDED.canonical_contract
RETURNING id, strategy_id, strategy_name, strategy_kind, evaluation_version,
          signal_schema_version, asset_type, timeframe, portfolio_hash,
          strategy_config_hash, tradeable_symbols, context_symbols,
          canonical_contract, created_at;

-- name: GetSignalContract :one
SELECT id, strategy_id, strategy_name, strategy_kind, evaluation_version,
       signal_schema_version, asset_type, timeframe, portfolio_hash,
       strategy_config_hash, tradeable_symbols, context_symbols,
       canonical_contract, created_at
FROM signal_contracts
WHERE id = sqlc.arg('id');

-- name: GetSignalContractByIdentity :one
SELECT id, strategy_id, strategy_name, strategy_kind, evaluation_version,
       signal_schema_version, asset_type, timeframe, portfolio_hash,
       strategy_config_hash, tradeable_symbols, context_symbols,
       canonical_contract, created_at
FROM signal_contracts
WHERE strategy_name = sqlc.arg(strategy_name)
  AND evaluation_version = sqlc.arg(evaluation_version)
  AND signal_schema_version = sqlc.arg(signal_schema_version)
  AND asset_type = sqlc.arg(asset_type)
  AND timeframe = sqlc.arg(timeframe)
  AND portfolio_hash = sqlc.arg(portfolio_hash)
  AND strategy_config_hash = sqlc.arg(strategy_config_hash);

-- name: UpsertSignalCoverage :one
INSERT INTO signal_coverage (
    contract_id, desired_from, desired_to, generated_from, generated_to,
    status, last_success_at, last_error, last_run_id, updated_at
)
VALUES (sqlc.arg('contract_id'), sqlc.arg('desired_from'), sqlc.arg('desired_to'), sqlc.arg('generated_from'), sqlc.arg('generated_to'), sqlc.arg('status'), sqlc.arg('last_success_at'), sqlc.arg('last_error'), sqlc.arg('last_run_id'), now())
ON CONFLICT (contract_id)
DO UPDATE SET
    desired_from = EXCLUDED.desired_from,
    desired_to = EXCLUDED.desired_to,
    generated_from = CASE
        WHEN EXCLUDED.generated_from IS NULL THEN signal_coverage.generated_from
        WHEN signal_coverage.generated_from IS NULL THEN EXCLUDED.generated_from
        ELSE LEAST(signal_coverage.generated_from, EXCLUDED.generated_from)
    END,
    generated_to = CASE
        WHEN EXCLUDED.generated_to IS NULL THEN signal_coverage.generated_to
        WHEN signal_coverage.generated_to IS NULL THEN EXCLUDED.generated_to
        ELSE GREATEST(signal_coverage.generated_to, EXCLUDED.generated_to)
    END,
    status = EXCLUDED.status,
    last_success_at = EXCLUDED.last_success_at,
    last_error = EXCLUDED.last_error,
    last_run_id = EXCLUDED.last_run_id,
    updated_at = now()
RETURNING id, contract_id, desired_from, desired_to, generated_from,
          generated_to, status, last_success_at, last_error, last_run_id,
          updated_at;

-- name: GetSignalCoverage :one
SELECT id, contract_id, desired_from, desired_to, generated_from,
       generated_to, status, last_success_at, last_error, last_run_id,
       updated_at
FROM signal_coverage
WHERE contract_id = sqlc.arg('contractID');

-- name: UpsertSignalCoverageSegment :one
INSERT INTO signal_coverage_segments (
    contract_id, segment_from, segment_to, status, data_fingerprint,
    warmup_fingerprint, run_id, error_message, completed_at
)
VALUES (
    sqlc.arg(contract_id), sqlc.arg(segment_from), sqlc.arg(segment_to),
    sqlc.arg(status), sqlc.arg(data_fingerprint), sqlc.arg(warmup_fingerprint),
    sqlc.arg(run_id), sqlc.arg(error_message), sqlc.arg(completed_at)
)
ON CONFLICT (contract_id, segment_from, segment_to, data_fingerprint, warmup_fingerprint)
DO UPDATE SET
    status = EXCLUDED.status,
    run_id = EXCLUDED.run_id,
    error_message = EXCLUDED.error_message,
    completed_at = EXCLUDED.completed_at
RETURNING id, contract_id, segment_from, segment_to, status,
          data_fingerprint, warmup_fingerprint, run_id, error_message,
          created_at, completed_at;

-- name: ListSignalCoverageSegments :many
SELECT id, contract_id, segment_from, segment_to, status,
       data_fingerprint, warmup_fingerprint, run_id, error_message,
       created_at, completed_at
FROM signal_coverage_segments
WHERE contract_id = sqlc.arg(contract_id)
  AND segment_to > sqlc.arg(request_from)
  AND segment_from < sqlc.arg(request_to)
ORDER BY segment_from ASC, segment_to ASC, id ASC;

-- name: UpsertSignalContractEvaluation :exec
INSERT INTO signal_contract_evaluations (
    contract_id, strategy_id, symbol, timeframe, evaluated_at, updated_at
)
VALUES (sqlc.arg('contract_id'), sqlc.arg('strategy_id'), sqlc.arg('symbol'), sqlc.arg('timeframe'), sqlc.arg('evaluated_at'), now())
ON CONFLICT (contract_id, symbol, timeframe)
DO UPDATE SET evaluated_at = EXCLUDED.evaluated_at, updated_at = now();

-- name: GetSignalContractEvaluation :one
SELECT evaluated_at
FROM signal_contract_evaluations
WHERE contract_id = sqlc.arg('contract_id') AND symbol = sqlc.arg('symbol') AND timeframe = sqlc.arg('timeframe');

-- name: PruneStaleUnreferencedSignals :execrows
WITH candidates AS MATERIALIZED (
    SELECT s.id
    FROM signals s
    LEFT JOIN signal_contracts sc
      ON sc.strategy_id = s.strategy_id
     AND sc.asset_type = s.asset_type
     AND sc.timeframe = s.timeframe
     AND sc.portfolio_hash = s.portfolio_hash
     AND sc.strategy_config_hash = s.strategy_config_hash
     AND sc.evaluation_version = s.strategy_evaluation_version
    WHERE (
         s.ts < sqlc.arg(cutoff)
       OR s.strategy_evaluation_version <> sqlc.arg(current_evaluation_version)
       OR NOT EXISTS (
           SELECT 1
           FROM signal_contracts active_contract
           JOIN signal_coverage active_coverage
             ON active_coverage.contract_id = active_contract.id
           WHERE active_contract.strategy_id = s.strategy_id
             AND active_contract.asset_type = s.asset_type
             AND active_contract.timeframe = s.timeframe
             AND active_contract.portfolio_hash = s.portfolio_hash
             AND active_contract.strategy_config_hash = s.strategy_config_hash
             AND active_contract.evaluation_version = s.strategy_evaluation_version
             AND active_coverage.status IN ('running', 'complete')
       )
       OR EXISTS (
           SELECT 1
           FROM signal_contracts newer_contract
           JOIN signal_coverage newer_coverage
             ON newer_coverage.contract_id = newer_contract.id
           WHERE sc.id IS NOT NULL
             AND newer_contract.strategy_id = sc.strategy_id
             AND newer_contract.asset_type = sc.asset_type
             AND newer_contract.timeframe = sc.timeframe
             AND newer_contract.evaluation_version = sc.evaluation_version
             AND newer_contract.signal_schema_version = sc.signal_schema_version
             AND newer_contract.created_at > sc.created_at
             AND newer_coverage.status = 'complete'
       )
    )
      AND NOT EXISTS (
          SELECT 1 FROM tradebot_intents i WHERE i.signal_id = s.id OFFSET 0
      )
      AND NOT EXISTS (
          SELECT 1 FROM tradebot_positions p
          WHERE p.entry_signal_id = s.id OR p.last_signal_id = s.id
          OFFSET 0
      )
      AND NOT EXISTS (
          SELECT 1 FROM tradebot_inventory_states tis
          WHERE tis.last_signal_id = s.id OFFSET 0
      )
      AND NOT EXISTS (
          SELECT 1 FROM signal_notes n WHERE n.signal_id = s.id OFFSET 0
      )
      -- signal_outcomes and signal_outcome_features are derived from the
      -- signal and have ON DELETE CASCADE. They must not keep stale signal
      -- versions alive indefinitely; current/trade-referenced lineage is
      -- retained by the contract and trade/position predicates above.
    ORDER BY s.strategy_evaluation_version ASC, s.id ASC
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE OF s SKIP LOCKED
)
,
deleted_outcome_features AS (
    DELETE FROM signal_outcome_features f
    WHERE f.signal_id IN (SELECT id FROM candidates)
    RETURNING f.signal_id
),
deleted_outcomes AS (
    DELETE FROM signal_outcomes o
    WHERE o.signal_id IN (SELECT id FROM candidates)
    RETURNING o.signal_id
)
DELETE FROM signals s
USING candidates c
WHERE s.id = c.id
  -- Reference both derived-table deletes so the parent delete cannot race
  -- their FK cascades. The scalar counts are always nonnegative, including
  -- an empty derived table, and keep this one transaction set-based.
  AND (SELECT count(*) FROM deleted_outcome_features) >= 0
  AND (SELECT count(*) FROM deleted_outcomes) >= 0;

-- name: PruneLegacyUnreferencedSignals :execrows
WITH candidates AS MATERIALIZED (
    SELECT s.id
    FROM signals s
    -- Evaluation versions are monotonic and the current version is the
    -- highest version accepted by this binary. Keep the predicate a strict
    -- range so signals_evaluation_version_idx can seek directly to legacy
    -- rows; <> forces a scan of every current-version row first.
    WHERE s.strategy_evaluation_version < sqlc.arg(current_evaluation_version)
      AND NOT EXISTS (SELECT 1 FROM tradebot_intents i WHERE i.signal_id = s.id OFFSET 0)
      AND NOT EXISTS (
          SELECT 1 FROM tradebot_positions p
          WHERE p.entry_signal_id = s.id OR p.last_signal_id = s.id
          OFFSET 0
      )
      AND NOT EXISTS (
          SELECT 1 FROM tradebot_inventory_states tis
          WHERE tis.last_signal_id = s.id OFFSET 0
      )
      AND NOT EXISTS (SELECT 1 FROM signal_notes n WHERE n.signal_id = s.id OFFSET 0)
    ORDER BY s.strategy_evaluation_version ASC, s.id ASC
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE OF s SKIP LOCKED
)
,
deleted_outcome_features AS (
    DELETE FROM signal_outcome_features f
    WHERE f.signal_id IN (SELECT id FROM candidates)
    RETURNING f.signal_id
),
deleted_outcomes AS (
    DELETE FROM signal_outcomes o
    WHERE o.signal_id IN (SELECT id FROM candidates)
    RETURNING o.signal_id
)
DELETE FROM signals s
USING candidates c
WHERE s.id = c.id
  AND (SELECT count(*) FROM deleted_outcome_features) >= 0
  AND (SELECT count(*) FROM deleted_outcomes) >= 0;

-- name: ListStoredSignalsForContract :many
SELECT s.id, s.strategy_id, st.name AS strategy_name, s.symbol, s.ts,
       s.direction, s.conviction, s.entry, s.stop, s.target, s.rationale,
       s.source, s.created_at, s.asset_type, s.timeframe, s.qty,
       s.source_votes, s.portfolio_hash, s.strategy_config_hash,
       s.strategy_evaluation_version, s.signal_payload
FROM signals s
JOIN strategies st ON st.id = s.strategy_id
JOIN tradebot_configurations c ON c.id = s.configuration_id
WHERE s.strategy_id = sqlc.arg(strategy_id)
  AND s.symbol = ANY(sqlc.arg(symbols)::text[])
  AND s.ts >= sqlc.arg(signal_from)
  AND s.ts <= sqlc.arg(signal_to)
  AND s.timeframe = sqlc.arg(timeframe)
  AND s.portfolio_hash = sqlc.arg(portfolio_hash)
  AND s.strategy_config_hash = sqlc.arg(strategy_config_hash)
  AND s.strategy_evaluation_version = sqlc.arg(evaluation_version)
  AND c.config_hash NOT LIKE 'legacy:%'
  AND (s.signal_payload IS NULL OR s.signal_payload = 'null'::jsonb)
ORDER BY s.ts ASC, s.symbol ASC, s.id ASC;

-- name: ListStoredSignalPayloadsForContract :many
WITH page AS MATERIALIZED (
SELECT s.id, s.ts
FROM signals s JOIN tradebot_configurations c ON c.id = s.configuration_id
WHERE s.strategy_id = sqlc.arg(strategy_id)
  AND s.symbol = sqlc.arg(symbol)
  AND (s.ts, s.id) > (sqlc.arg(cursor_ts)::timestamptz, sqlc.arg(cursor_id)::bigint)
  AND s.ts <= sqlc.arg(signal_to)
  AND s.timeframe = sqlc.arg(timeframe)
  AND s.portfolio_hash = sqlc.arg(portfolio_hash)
  AND s.strategy_config_hash = sqlc.arg(strategy_config_hash)
  AND s.strategy_evaluation_version = sqlc.arg(evaluation_version)
  AND c.config_hash NOT LIKE 'legacy:%'
  AND s.signal_payload IS NOT NULL
  AND s.signal_payload <> 'null'::jsonb
  AND s.configuration_id IS NOT NULL
ORDER BY s.ts ASC, s.id ASC
LIMIT sqlc.arg(page_limit)
)
SELECT s.id, s.signal_payload, page.ts FROM page
JOIN signals s ON s.id = page.id
ORDER BY page.ts, page.id;
