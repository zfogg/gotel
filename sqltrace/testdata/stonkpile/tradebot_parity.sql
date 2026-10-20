-- name: InsertTradebotParitySnapshot :exec
INSERT INTO tradebot_parity_snapshots (
  snapshot_id, account_id, broker, asset_class, timeframe, watermark,
  equity, cash, universe, signals, positions, execution_profile, profile_hash,
  strategy_config, strategy_selection_id, execution_policy, policy_context,
  execution_prices, portfolio_hash, strategy_config_hash,
  strategy_evaluation_version
) VALUES (
  sqlc.arg('snapshot_id'), sqlc.arg('account_id'), sqlc.arg('broker'), sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('watermark'),
  sqlc.arg('equity'), sqlc.arg('cash'), sqlc.arg('universe'), sqlc.arg('signals'), sqlc.arg('positions'), sqlc.arg('execution_profile'), sqlc.arg('profile_hash'),
  sqlc.arg('strategy_config'), sqlc.narg('strategy_selection_id'), sqlc.arg('execution_policy')::jsonb,
  sqlc.arg('policy_context')::jsonb, sqlc.arg('execution_prices')::jsonb,
  sqlc.arg('portfolio_hash'), sqlc.arg('strategy_config_hash'),
  sqlc.arg('strategy_evaluation_version')
)
ON CONFLICT (snapshot_id) DO NOTHING;

-- name: InsertTradebotParityBatchExpectation :exec
INSERT INTO tradebot_parity_batch_receipts (snapshot_id, expected_hash, expected_count)
VALUES (sqlc.arg('snapshot_id'), sqlc.arg('expected_hash'), sqlc.arg('expected_count'))
ON CONFLICT (snapshot_id) DO NOTHING;

-- name: RecordTradebotParityBatchReceipt :exec
WITH snapshot AS (
  SELECT snapshot_id
  FROM tradebot_parity_snapshots
  WHERE account_id = sqlc.arg('account_id')
    AND broker = sqlc.arg('broker')
    AND asset_class = sqlc.arg('asset_class')
    AND timeframe = sqlc.arg('timeframe')
    AND watermark = sqlc.arg('watermark')
  ORDER BY created_at DESC, snapshot_id DESC
  LIMIT 1
)
UPDATE tradebot_parity_batch_receipts r
SET received_hash = sqlc.arg('received_hash'),
    received_count = sqlc.arg('received_count'),
    received_at = now()
FROM snapshot
WHERE r.snapshot_id = snapshot.snapshot_id;

-- name: ListTradebotParityBatchReceiptsForAccount :many
SELECT r.*
FROM tradebot_parity_batch_receipts r
JOIN tradebot_parity_snapshots s ON s.snapshot_id = r.snapshot_id
WHERE s.account_id = sqlc.arg('account_id')
  AND s.asset_class = sqlc.arg('asset_class')
  AND s.timeframe = sqlc.arg('timeframe')
  AND s.watermark >= sqlc.arg('from_watermark')::timestamptz
  AND s.watermark <= sqlc.arg('through_watermark')::timestamptz
ORDER BY s.watermark DESC, s.created_at DESC, s.snapshot_id DESC;

-- name: CompleteTradebotParityBatchReceipt :exec
UPDATE tradebot_parity_batch_receipts r
SET completed_at = COALESCE(r.completed_at, now())
FROM tradebot_parity_snapshots s
WHERE s.snapshot_id = r.snapshot_id
 AND s.account_id = sqlc.arg('account_id') AND s.broker = sqlc.arg('broker')
 AND s.asset_class = sqlc.arg('asset_class') AND s.timeframe = sqlc.arg('timeframe')
 AND s.watermark = sqlc.arg('watermark')
 AND r.received_hash = r.expected_hash AND r.received_count = r.expected_count;

-- name: InsertTradebotParityContract :exec
INSERT INTO tradebot_parity_contracts (
  asset_class, timeframe, watermark, evaluation_hash, strategy_config_hash,
  portfolio_hash, profile_hash, strategy_version, identity
) VALUES (
  sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('watermark'),
  sqlc.arg('evaluation_hash'), sqlc.arg('strategy_config_hash'),
  sqlc.arg('portfolio_hash'), sqlc.arg('profile_hash'),
  sqlc.arg('strategy_version'), sqlc.arg('identity')::jsonb
)
ON CONFLICT DO NOTHING;

-- name: ListTradebotParityFills :many
SELECT f.fill_id, i.asset_class, i.timeframe, i.reason, owner.decision_bar, f.symbol,
  owner.direction, owner.action, f.action, f.qty,
  COALESCE(f.initial_entry_price, f.price) AS entry_price, f.price AS exit_price,
  COALESCE(f.initial_entry_filled_at, owner.decision_bar) AS entry_at,
  f.filled_at AS exit_at, f.net_pnl, c.identity, e.result,
  owner.profile_hash, p.execution_profile,
  COALESCE(jsonb_array_length(p.universe), 0)::integer AS symbol_count,
  owner.intent_id, owner.qty::float8 AS requested_qty,
  COALESCE(owner.stop_price, 0)::float8 AS stop_price,
  COALESCE(owner.target_price, 0)::float8 AS target_price,
  COALESCE(p.snapshot_id, '')::text AS snapshot_id,
  COALESCE(p.policy_context->'signalEvaluation'->>'version', '') = '1' AS strict_decision,
  owner.reference_price::float8 AS reference_price,
  x.identity AS exit_identity, x.inputs AS exit_inputs,
  i.intent_id AS exit_intent_id, i.qty::float8 AS exit_qty,
  i.decision_bar AS exit_decision_bar, f.broker_order_id,
  COALESCE(f.position_cycle_id, i.position_cycle_id, '')::text AS position_cycle_id
FROM tradebot_fill_events f
JOIN tradebot_intents i ON i.intent_id = f.intent_id
JOIN tradebot_intents owner ON owner.intent_id = CASE
  WHEN f.action LIKE 'open%' THEN i.intent_id ELSE COALESCE(f.position_cycle_id, i.position_cycle_id) END
  AND owner.account_id = i.account_id AND owner.broker = i.broker
  AND owner.asset_class = i.asset_class AND owner.timeframe = i.timeframe
JOIN signals s ON s.id = owner.signal_id
JOIN LATERAL (SELECT identity, evaluation_hash FROM tradebot_parity_contracts c
  WHERE c.asset_class = i.asset_class AND c.timeframe = i.timeframe AND c.watermark = owner.decision_bar
  AND c.strategy_config_hash = s.strategy_config_hash AND c.portfolio_hash = s.portfolio_hash
  AND c.strategy_version = s.strategy_evaluation_version
  AND c.created_at <= owner.created_at
  ORDER BY c.created_at DESC LIMIT 1) c ON true
LEFT JOIN LATERAL (SELECT snapshot_id, execution_profile, universe, policy_context FROM tradebot_parity_snapshots p
  WHERE p.account_id = owner.account_id AND p.broker = owner.broker
  AND p.asset_class = owner.asset_class AND p.timeframe = owner.timeframe AND p.watermark = owner.decision_bar
  AND p.profile_hash = c.identity->>'profileHash' AND p.portfolio_hash = s.portfolio_hash
  AND p.strategy_config_hash = s.strategy_config_hash AND p.strategy_evaluation_version = s.strategy_evaluation_version
  AND p.created_at <= owner.created_at ORDER BY p.created_at DESC LIMIT 1) p ON true
LEFT JOIN tradebot_parity_exit_evaluations x
  ON x.intent_id = i.intent_id AND f.action NOT LIKE 'open%'
LEFT JOIN LATERAL (SELECT result, matched FROM tradebot_backtest_parity_evidence e
  WHERE e.fill_id = f.fill_id
    AND e.evaluation_hash = COALESCE(x.identity->>'hash', c.evaluation_hash)
  ORDER BY (report_id LIKE 'protection-execution:%' OR report_id LIKE 'batch:%') DESC,
    CASE WHEN report_id LIKE 'protection-execution:%' OR report_id LIKE 'batch:%'
      THEN COALESCE((result->>'engineVersion')::integer, 0) ELSE 0 END DESC,
    (report_id LIKE 'protection-execution:%') DESC,
    matched DESC, checked_at DESC, report_id
  LIMIT 1) e ON true
WHERE i.mode IN ('canonical', 'exit_only', 'manage_only')
  AND (cardinality(sqlc.arg('fill_ids')::text[]) = 0 OR f.fill_id = ANY(sqlc.arg('fill_ids')::text[]))
  AND (NOT sqlc.arg('pending_only')::boolean OR COALESCE(e.matched, false) = false OR e.result->>'reportId' LIKE 'protection:%')
ORDER BY f.filled_at DESC, f.fill_id
LIMIT 5000;

-- name: ListPendingParityOrders :many
SELECT i.intent_id, i.asset_class, i.timeframe, i.reason, i.decision_bar,
       i.symbol, i.direction, i.qty::float8 AS requested_qty,
       COALESCE(i.stop_price, 0)::float8 AS stop_price,
       COALESCE(i.target_price, 0)::float8 AS target_price,
       c.identity, p.snapshot_id, i.reference_price::float8 AS reference_price,
       i.profile_hash, p.execution_profile,
       COALESCE(jsonb_array_length(p.universe), 0)::integer AS symbol_count,
       COALESCE(p.policy_context->'signalEvaluation'->>'version', '') = '1' AS strict_decision
FROM tradebot_intents i
JOIN signals s ON s.id = i.signal_id
JOIN LATERAL (
    SELECT identity, evaluation_hash
    FROM tradebot_parity_contracts c
    WHERE c.asset_class = i.asset_class AND c.timeframe = i.timeframe
      AND c.watermark = i.decision_bar
      AND c.strategy_config_hash = s.strategy_config_hash
      AND c.portfolio_hash = s.portfolio_hash
      AND c.strategy_version = s.strategy_evaluation_version
      AND c.created_at <= i.created_at
    ORDER BY c.created_at DESC
    LIMIT 1
) c ON true
JOIN LATERAL (
    SELECT snapshot_id, execution_profile, universe, policy_context
    FROM tradebot_parity_snapshots p
    WHERE p.account_id = i.account_id AND p.broker = i.broker
      AND p.asset_class = i.asset_class AND p.timeframe = i.timeframe
      AND p.watermark = i.decision_bar
      AND p.profile_hash = c.identity->>'profileHash'
      AND p.strategy_config_hash = s.strategy_config_hash
      AND p.portfolio_hash = s.portfolio_hash
      AND p.created_at <= i.created_at
    ORDER BY p.created_at DESC
    LIMIT 1
) p ON true
LEFT JOIN tradebot_parity_order_decisions d
  ON d.intent_id = i.intent_id AND d.evaluation_hash = c.evaluation_hash
WHERE i.mode = 'canonical'
  AND i.action IN ('open_long', 'open_short')
  AND i.broker_order_id IS NOT NULL AND i.broker_order_id <> ''
  AND d.intent_id IS NULL
ORDER BY i.created_at DESC
LIMIT 100;

-- name: InsertInitialParityBaseline :execrows
INSERT INTO tradebot_parity_initial_baseline(singleton, configuration_hash, engine_hash)
VALUES (true, sqlc.arg(configuration_hash), sqlc.arg(engine_hash))
ON CONFLICT DO NOTHING;

-- name: LockInitialParityBaseline :one
SELECT cutoff, completed_at, configuration_hash, engine_hash
FROM tradebot_parity_initial_baseline
WHERE singleton
FOR UPDATE;

-- name: EnrollInitialParityBaselineFills :exec
INSERT INTO tradebot_parity_initial_baseline_fills(fill_id)
SELECT f.fill_id
FROM tradebot_fill_events f
WHERE f.filled_at <= sqlc.arg(cutoff)
  AND NOT EXISTS (
      SELECT 1 FROM tradebot_backtest_parity_evidence e
      WHERE e.fill_id = f.fill_id AND e.matched
  );

-- name: ListInitialParityBaselineFillsPage :many
SELECT f.fill_id, i.asset_class, i.timeframe, i.reason, f.symbol,
       COALESCE(o.direction, i.direction)::text AS direction,
       f.action, f.qty::float8 AS qty,
       COALESCE(f.initial_entry_price, f.price)::float8 AS entry_price,
       f.price::float8 AS exit_price,
       COALESCE(f.initial_entry_filled_at, o.decision_bar, f.filled_at) AS entry_at,
       f.filled_at AS exit_at, (f.net_pnl IS NOT NULL)::boolean AS has_net_pnl,
       COALESCE(f.net_pnl, 0)::float8 AS net_pnl
FROM tradebot_parity_initial_baseline_fills b
JOIN tradebot_fill_events f USING(fill_id)
JOIN tradebot_intents i ON i.intent_id = f.intent_id
LEFT JOIN tradebot_intents o
  ON o.intent_id = CASE WHEN f.action LIKE 'open%' THEN i.intent_id
       ELSE COALESCE(f.position_cycle_id, i.position_cycle_id) END
  AND o.account_id = i.account_id AND o.broker = i.broker
WHERE b.result IS NULL
  AND f.fill_id > sqlc.arg(after_fill_id)
  AND NOT EXISTS (
      SELECT 1 FROM tradebot_backtest_parity_evidence e
      WHERE e.fill_id = f.fill_id AND e.matched
  )
ORDER BY f.fill_id
LIMIT 500;

-- name: SaveInitialParityBaselineResultsJSON :exec
UPDATE tradebot_parity_initial_baseline_fills b
SET result = value.value, checked_at = now()
FROM jsonb_each(sqlc.arg(results)::jsonb) AS value
WHERE b.fill_id = value.key AND b.result IS NULL;

-- name: ListInitialParityBaselineResults :many
SELECT fill_id, result
FROM tradebot_parity_initial_baseline_fills
WHERE fill_id = ANY(sqlc.arg(fill_ids)::text[]);

-- name: CompleteInitialParityBaselineIfReady :exec
UPDATE tradebot_parity_initial_baseline
SET completed_at = now()
WHERE singleton AND completed_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM tradebot_parity_initial_baseline_fills b
      WHERE b.result IS NULL
        AND NOT EXISTS (
            SELECT 1 FROM tradebot_backtest_parity_evidence e
            WHERE e.fill_id = b.fill_id AND e.matched
        )
  );

-- name: PutInitialParityBaselineScope :one
UPDATE tradebot_parity_initial_baseline
SET scope_contracts = CASE
    WHEN scope_contracts ? sqlc.arg(scope_key)::text THEN scope_contracts
    ELSE scope_contracts || jsonb_build_object(sqlc.arg(scope_key)::text, sqlc.arg(candidate)::jsonb)
END
WHERE singleton
RETURNING (scope_contracts -> sqlc.arg(scope_key)::text)::text;

-- name: SaveTradebotBacktestParityEvidence :exec
INSERT INTO tradebot_backtest_parity_evidence (
  fill_id, evaluation_hash, report_id, matched, result
) VALUES (
  sqlc.arg('fill_id'), sqlc.arg('evaluation_hash'), sqlc.arg('report_id'),
  sqlc.arg('matched'), sqlc.arg('result')::jsonb
)
ON CONFLICT (fill_id, evaluation_hash, report_id) DO UPDATE
SET matched = EXCLUDED.matched, result = EXCLUDED.result, checked_at = now()
WHERE NOT tradebot_backtest_parity_evidence.matched;

-- name: LatestTradebotParitySnapshot :one
SELECT * FROM tradebot_parity_snapshots
WHERE account_id = sqlc.arg('account_id') AND asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe')
ORDER BY watermark DESC
LIMIT 1;

-- name: GetTradebotParitySnapshot :one
SELECT * FROM tradebot_parity_snapshots WHERE snapshot_id = sqlc.arg('snapshotID');

-- name: GetTradebotParitySnapshotJSON :one
SELECT jsonb_build_object(
  'snapshotId', snapshot_id, 'accountId', account_id, 'broker', broker,
  'assetClass', asset_class,
  'timeframe', timeframe, 'watermark', watermark, 'equity', equity, 'cash', cash,
  'universe', universe, 'signals', signals, 'positions', positions,
  'executionProfile', execution_profile, 'profileHash', profile_hash,
  'strategyConfig', strategy_config, 'strategySelectionId', strategy_selection_id,
  'executionPolicy', execution_policy,
  'policyContext', policy_context,
  'executionPrices', execution_prices,
  'portfolioHash', portfolio_hash,
  'strategyConfigHash', strategy_config_hash,
  'strategyEvaluationVersion', strategy_evaluation_version,
  'createdAt', created_at
)
FROM tradebot_parity_snapshots
WHERE snapshot_id = sqlc.arg('snapshotID');

-- name: LatestTradebotParitySnapshotJSON :one
SELECT jsonb_build_object(
  'snapshotId', snapshot_id, 'accountId', account_id, 'broker', broker,
  'assetClass', asset_class,
  'timeframe', timeframe, 'watermark', watermark, 'equity', equity, 'cash', cash,
  'universe', universe, 'signals', signals, 'positions', positions,
  'executionProfile', execution_profile, 'profileHash', profile_hash,
  'strategyConfig', strategy_config, 'strategySelectionId', strategy_selection_id,
  'executionPolicy', execution_policy,
  'policyContext', policy_context,
  'executionPrices', execution_prices,
  'portfolioHash', portfolio_hash,
  'strategyConfigHash', strategy_config_hash,
  'strategyEvaluationVersion', strategy_evaluation_version,
  'createdAt', created_at
)
FROM tradebot_parity_snapshots
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe')
ORDER BY watermark DESC, created_at DESC, snapshot_id DESC
LIMIT 1;

-- name: ListTradebotParitySnapshotsJSONForAccount :many
SELECT jsonb_build_object(
  'snapshotId', snapshot_id, 'accountId', account_id, 'broker', broker,
  'assetClass', asset_class,
  'timeframe', timeframe, 'watermark', watermark, 'equity', equity, 'cash', cash,
  'universe', universe, 'signals', signals, 'positions', positions,
  'executionProfile', execution_profile, 'profileHash', profile_hash,
  'strategyConfig', strategy_config, 'strategySelectionId', strategy_selection_id,
  'executionPolicy', execution_policy,
  'policyContext', policy_context,
  'executionPrices', execution_prices,
  'portfolioHash', portfolio_hash,
  'strategyConfigHash', strategy_config_hash,
  'strategyEvaluationVersion', strategy_evaluation_version,
  'createdAt', created_at
)
FROM tradebot_parity_snapshots
WHERE account_id = sqlc.arg('account_id')
  AND asset_class = sqlc.arg('asset_class')
  AND timeframe = sqlc.arg('timeframe')
  AND watermark >= sqlc.arg('from_watermark')::timestamptz
  AND watermark <= sqlc.arg('through_watermark')::timestamptz
ORDER BY watermark DESC, created_at DESC, snapshot_id DESC;

-- name: ListTradebotParitySnapshotsJSON :many
SELECT jsonb_build_object(
  'snapshotId', snapshot_id, 'accountId', account_id, 'broker', broker,
  'assetClass', asset_class, 'timeframe', timeframe, 'watermark', watermark,
  'equity', equity, 'cash', cash, 'universe', universe, 'signals', signals,
  'positions', positions, 'executionProfile', execution_profile,
  'profileHash', profile_hash, 'strategyConfig', strategy_config,
  'strategySelectionId', strategy_selection_id,
  'executionPolicy', execution_policy,
  'policyContext', policy_context,
  'executionPrices', execution_prices,
  'portfolioHash', portfolio_hash,
  'strategyConfigHash', strategy_config_hash,
  'strategyEvaluationVersion', strategy_evaluation_version,
  'createdAt', created_at
)
FROM tradebot_parity_snapshots
WHERE account_id = sqlc.arg('account_id')
  AND broker = sqlc.arg('broker')
  AND asset_class = sqlc.arg('asset_class')
  AND timeframe = sqlc.arg('timeframe')
  AND watermark >= sqlc.arg('from_watermark')::timestamptz
  AND watermark < sqlc.arg('through_watermark')::timestamptz
ORDER BY watermark, created_at, snapshot_id;

-- name: GetOrCreateTradebotReplayAnchor :one
INSERT INTO tradebot_replay_anchors (
  lineage_key, asset_class, timeframe, profile_hash, strategy_config,
  direction_overlay, universe, replay_from, first_watermark
) VALUES (
  sqlc.arg('lineage_key'), sqlc.arg('asset_class'), sqlc.arg('timeframe'),
  sqlc.arg('profile_hash'), sqlc.arg('strategy_config')::jsonb,
  sqlc.arg('direction_overlay'), sqlc.arg('universe')::jsonb,
  COALESCE(
    -- Existing immutable anchors must not rescan snapshot history on every
    -- decision. Keep the conflict handler for concurrent first-time creators.
    (
      SELECT existing.replay_from
      FROM tradebot_replay_anchors AS existing
      WHERE existing.lineage_key = sqlc.arg('lineage_key')
    ),
    (
      SELECT existing.replay_from
      FROM tradebot_replay_anchors AS existing
      WHERE existing.lineage_key = sqlc.arg('legacy_lineage_key')
    ),
    (
      SELECT date_trunc('day', min(watermark) - interval '3 months')
      FROM tradebot_parity_snapshots
      WHERE asset_class = sqlc.arg('asset_class')
        AND timeframe = sqlc.arg('timeframe')
        AND profile_hash = sqlc.arg('profile_hash')
        AND strategy_config = sqlc.arg('strategy_config')::jsonb
        AND universe = sqlc.arg('universe')::jsonb
        AND watermark <= sqlc.arg('watermark')::timestamptz
    ),
    date_trunc('day', sqlc.arg('watermark')::timestamptz - interval '3 months')
  ),
  sqlc.arg('watermark')
)
ON CONFLICT (lineage_key) DO UPDATE
SET lineage_key = EXCLUDED.lineage_key
RETURNING lineage_key, replay_from, seeded_through;

-- name: MarkTradebotReplayAnchorSeeded :exec
UPDATE tradebot_replay_anchors
SET seeded_through = CASE
  WHEN seeded_through IS NULL OR seeded_through < sqlc.arg('seeded_through')::timestamptz
    THEN sqlc.arg('seeded_through')::timestamptz
  ELSE seeded_through
END
WHERE lineage_key = sqlc.arg('lineage_key');

-- name: SaveParityOrderDecision :exec
INSERT INTO tradebot_parity_order_decisions(intent_id,evaluation_hash,snapshot_id,matched,result)
VALUES(sqlc.arg(intent_id),sqlc.arg(evaluation_hash),sqlc.arg(snapshot_id),sqlc.arg(matched),sqlc.arg(result)::jsonb)
ON CONFLICT(intent_id,evaluation_hash) DO UPDATE SET matched=EXCLUDED.matched,
 result=EXCLUDED.result,checked_at=now()
WHERE NOT tradebot_parity_order_decisions.matched;

-- name: GetParityOrderDecision :one
SELECT result FROM tradebot_parity_order_decisions
WHERE intent_id=sqlc.arg(intent_id) AND evaluation_hash=sqlc.arg(evaluation_hash);

-- name: SaveTradebotSubmittedLimitPrice :exec
UPDATE tradebot_intents SET submitted_limit_price=sqlc.arg(price)::text::numeric
WHERE intent_id=sqlc.arg(intent_id) AND submitted_limit_price IS NULL;

-- name: ListTradebotParitySnapshotIDsForSession :many
SELECT snapshot_id FROM tradebot_parity_snapshots
WHERE asset_class = sqlc.arg(asset_class)
  AND timeframe = sqlc.arg(timeframe)
  AND watermark >= sqlc.arg(watermark_from)
  AND watermark <= sqlc.arg(watermark_through)
ORDER BY watermark, account_id, created_at, snapshot_id;
