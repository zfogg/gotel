-- name: UpsertSignalOutcomeFeature :exec
INSERT INTO signal_outcome_features (
    signal_id, horizon_bars, decision_time, available_at, graded_at,
    symbol, source, cluster_id, regime_state, breadth_participants,
    breadth_eligible, r_multiple, mae_r, mfe_r, exit_reason, graded
) VALUES (sqlc.arg('signal_id'), sqlc.arg('horizon_bars'), sqlc.arg('decision_time'), sqlc.arg('available_at'), sqlc.arg('graded_at'), sqlc.arg('symbol'), sqlc.arg('source'), sqlc.arg('cluster_id'), sqlc.arg('regime_state'), sqlc.arg('breadth_participants'), sqlc.arg('breadth_eligible'), sqlc.arg('r_multiple'), sqlc.arg('mae_r'), sqlc.arg('mfe_r'), sqlc.arg('exit_reason'), sqlc.arg('graded'))
ON CONFLICT (signal_id, horizon_bars) DO UPDATE SET
    available_at = EXCLUDED.available_at,
    graded_at = EXCLUDED.graded_at,
    symbol = EXCLUDED.symbol,
    source = EXCLUDED.source,
    cluster_id = EXCLUDED.cluster_id,
    regime_state = EXCLUDED.regime_state,
    breadth_participants = EXCLUDED.breadth_participants,
    breadth_eligible = EXCLUDED.breadth_eligible,
    r_multiple = EXCLUDED.r_multiple,
    mae_r = EXCLUDED.mae_r,
    mfe_r = EXCLUDED.mfe_r,
    exit_reason = EXCLUDED.exit_reason,
    graded = EXCLUDED.graded;

-- name: ListSourceOutcomeFeaturesBefore :many
SELECT signal_id, horizon_bars, decision_time, available_at, graded_at,
       symbol, source, cluster_id, regime_state, breadth_participants,
       breadth_eligible, r_multiple, mae_r, mfe_r, exit_reason, graded
FROM signal_outcome_features
WHERE source = sqlc.arg('source')
  AND available_at < sqlc.arg('available_before')
  AND available_at >= sqlc.arg('available_from')
  AND (sqlc.arg('horizon_bars')::integer = 0 OR horizon_bars = sqlc.arg('horizon_bars'))
  AND graded = true
ORDER BY available_at ASC, signal_id ASC, horizon_bars ASC;
