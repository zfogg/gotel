-- name: UpsertTradebotConfiguration :one
INSERT INTO tradebot_configurations (
    config_hash, asset_class, timeframe, direction,
    strategy_params, execution_profile, management_policy, strategy_execution_policies, universe
) VALUES (
    sqlc.arg('config_hash'), sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('strategy_params'), sqlc.arg('execution_profile'), sqlc.arg('management_policy'),
    COALESCE(sqlc.arg(strategy_execution_policies)::jsonb, '{}'::jsonb),
    COALESCE(sqlc.arg(universe)::jsonb, '[]'::jsonb)
)
ON CONFLICT (config_hash) DO UPDATE SET universe = EXCLUDED.universe
RETURNING *;

-- name: UpsertTradebotConfigurationsBatch :many
INSERT INTO tradebot_configurations (
    config_hash, asset_class, timeframe, direction,
    strategy_params, execution_profile, management_policy, strategy_execution_policies, universe
)
SELECT unnest(sqlc.arg(config_hashes)::text[]),
       unnest(sqlc.arg(asset_classes)::text[]),
       unnest(sqlc.arg(timeframes)::text[]),
       unnest(sqlc.arg(directions)::text[]),
       unnest(sqlc.arg(strategy_params)::jsonb[]),
       unnest(sqlc.arg(execution_profiles)::jsonb[]),
       unnest(sqlc.arg(management_policies)::jsonb[]),
       unnest(
           CASE
               WHEN COALESCE(cardinality(sqlc.arg(strategy_execution_policies)::jsonb[]), 0) = 0
                   THEN array_fill('{}'::jsonb, ARRAY[cardinality(sqlc.arg(config_hashes)::text[])])
               ELSE array_replace(sqlc.arg(strategy_execution_policies)::jsonb[], NULL, '{}'::jsonb)
           END
       ),
       unnest(
           CASE
               WHEN COALESCE(cardinality(sqlc.arg(universes)::jsonb[]), 0) = 0
                   THEN array_fill('[]'::jsonb, ARRAY[cardinality(sqlc.arg(config_hashes)::text[])])
               ELSE array_replace(sqlc.arg(universes)::jsonb[], NULL, '[]'::jsonb)
           END
       )
ON CONFLICT (config_hash) DO UPDATE SET universe = EXCLUDED.universe
RETURNING *;

-- name: GetTradebotConfiguration :one
SELECT * FROM tradebot_configurations WHERE id = sqlc.arg('id');

-- name: GetTradebotConfigurationByHash :one
SELECT * FROM tradebot_configurations WHERE config_hash = sqlc.arg('configHash');

-- name: ListTradebotConfigurationsByHashPrefix :many
SELECT * FROM tradebot_configurations
WHERE config_hash LIKE sqlc.arg(hash_prefix)::text || '%'
ORDER BY created_at DESC
LIMIT 2;

-- name: GetActiveTradebotConfiguration :one
SELECT c.*
FROM tradebot_config_selections s
JOIN tradebot_configurations c ON c.id = s.configuration_id
WHERE s.asset_class = sqlc.arg('asset_class')
  AND s.timeframe = sqlc.arg('timeframe')
  AND s.direction = sqlc.arg('direction')
  AND s.effective_from <= sqlc.arg('effective_from')
  AND (s.effective_to IS NULL OR s.effective_to > sqlc.arg('effective_from'))
ORDER BY s.effective_from DESC
LIMIT 1;

-- name: GetCurrentActiveTradebotConfiguration :one
SELECT c.*
FROM tradebot_config_selections s
JOIN tradebot_configurations c ON c.id = s.configuration_id
WHERE s.asset_class = sqlc.arg('asset_class') AND s.timeframe = sqlc.arg('timeframe') AND s.direction = sqlc.arg('direction')
  AND s.effective_to IS NULL
ORDER BY s.effective_from DESC
LIMIT 1;

-- name: CreateOptimizerRun :one
INSERT INTO optimizer_runs (
    asset_class, timeframe, direction, mode, universe, universe_hash,
    data_watermark, code_sha, deterministic_seed, search_space_version,
    strategy_evaluation_version, promote, learn, candidates_total, portfolio_source
) VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('mode'), sqlc.arg('universe'), sqlc.arg('universe_hash'), sqlc.arg('data_watermark'), sqlc.arg('code_sha'), sqlc.arg('deterministic_seed'), sqlc.arg('search_space_version'), sqlc.arg('strategy_evaluation_version'), sqlc.arg('promote'), sqlc.arg('learn'), sqlc.arg('candidates_total'),
          COALESCE(NULLIF(sqlc.arg(portfolio_source)::text, ''), 'settings'))
RETURNING *;

-- name: GetOptimizerRun :one
SELECT * FROM optimizer_runs WHERE id = sqlc.arg('id');

-- name: GetActiveOptimizerRunForScope :one
SELECT * FROM optimizer_runs
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND direction = sqlc.arg('direction')
  AND status IN ('pending', 'running')
ORDER BY started_at DESC
LIMIT 1;

-- name: GetActiveOptimizerRun :one
SELECT * FROM optimizer_runs
WHERE status IN ('pending', 'running')
ORDER BY started_at DESC
LIMIT 1;

-- name: ListOptimizerRuns :many
SELECT * FROM optimizer_runs
WHERE (sqlc.arg(asset_class)::text = '' OR asset_class = sqlc.arg(asset_class)::text)
  AND (sqlc.arg(timeframe)::text = '' OR timeframe = sqlc.arg(timeframe)::text)
  AND (sqlc.arg(direction)::text = '' OR direction = sqlc.arg(direction)::text)
ORDER BY started_at DESC
LIMIT sqlc.arg(result_limit);

-- name: UpdateOptimizerRunProgress :one
UPDATE optimizer_runs
SET status = 'running',
    stage = sqlc.arg('stage'),
    progress = sqlc.arg('progress'),
    candidates_completed = sqlc.arg('candidates_completed'),
    candidates_total = sqlc.arg('candidates_total'),
    current_candidate = sqlc.arg('current_candidate'),
    error = '',
    finished_at = NULL,
    revision = revision + 1
WHERE id = sqlc.arg('id')
  AND status IN ('pending', 'running')
RETURNING *;

-- name: CompleteOptimizerRun :one
UPDATE optimizer_runs
SET status = sqlc.arg('status'),
    stage = sqlc.arg('stage'),
    progress = CASE WHEN sqlc.arg('status') = 'completed' THEN 100 ELSE progress END,
    error = sqlc.arg('error'),
    finished_at = now(),
    revision = revision + 1
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: UpdateOptimizerStrategyProgress :one
UPDATE optimizer_runs
SET status = 'running',
    stage = 'strategy_matrix',
    progress = sqlc.arg('progress'),
    strategy_backtests_completed = sqlc.arg('strategy_backtests_completed'),
    strategy_backtests_total = sqlc.arg('strategy_backtests_total'),
    strategy_backtests_reused = sqlc.arg('strategy_backtests_reused'),
    current_candidate = sqlc.arg('current_candidate'),
    current_strategy = sqlc.arg('current_strategy'),
    current_fold = sqlc.arg('current_fold'),
    revision = revision + 1
WHERE id = sqlc.arg('id')
  AND status IN ('pending', 'running')
RETURNING *;

-- name: UpsertOptimizerCandidateResult :one
INSERT INTO optimizer_candidate_results (
    run_id, configuration_id, rank, stage, eligible, score,
    realized_return, marked_return, total_r, r_per_day, profit_per_trade, sharpe,
    max_drawdown, max_drawdown_dollars, peak_locked_pct, positive_fold_pct, trades, fees,
    metrics, equity_curve, rejection_reasons, paired_utility,
    paired_fold_win_pct, evolutionary_success, center_configuration_id
) VALUES (
    sqlc.arg('run_id'), sqlc.arg('configuration_id'), sqlc.arg('rank'), sqlc.arg('stage'), sqlc.arg('eligible'), sqlc.arg('score'),
    sqlc.arg('realized_return'), sqlc.arg('marked_return'), sqlc.arg('total_r'), sqlc.arg('r_per_day'), sqlc.arg('profit_per_trade'), sqlc.arg('sharpe'),
    sqlc.arg('max_drawdown'), sqlc.arg('max_drawdown_dollars'), sqlc.arg('peak_locked_pct'), sqlc.arg('positive_fold_pct'), sqlc.arg('trades'), sqlc.arg('fees'),
    sqlc.arg('metrics'), sqlc.arg('equity_curve'), sqlc.arg('rejection_reasons'), sqlc.arg('paired_utility'), sqlc.arg('paired_fold_win_pct'), sqlc.arg('evolutionary_success'), sqlc.arg('center_configuration_id')
)
ON CONFLICT (run_id, configuration_id) DO UPDATE SET
    rank = EXCLUDED.rank,
    stage = EXCLUDED.stage,
    eligible = EXCLUDED.eligible,
    score = EXCLUDED.score,
    realized_return = EXCLUDED.realized_return,
    marked_return = EXCLUDED.marked_return,
    total_r = EXCLUDED.total_r,
    r_per_day = EXCLUDED.r_per_day,
    profit_per_trade = EXCLUDED.profit_per_trade,
    sharpe = EXCLUDED.sharpe,
    max_drawdown = EXCLUDED.max_drawdown,
    max_drawdown_dollars = EXCLUDED.max_drawdown_dollars,
    peak_locked_pct = EXCLUDED.peak_locked_pct,
    positive_fold_pct = EXCLUDED.positive_fold_pct,
    trades = EXCLUDED.trades,
    fees = EXCLUDED.fees,
    metrics = EXCLUDED.metrics,
    equity_curve = EXCLUDED.equity_curve,
    rejection_reasons = EXCLUDED.rejection_reasons,
    paired_utility = EXCLUDED.paired_utility,
    paired_fold_win_pct = EXCLUDED.paired_fold_win_pct,
    evolutionary_success = EXCLUDED.evolutionary_success,
    center_configuration_id = EXCLUDED.center_configuration_id,
    updated_at = now()
RETURNING *;

-- name: UpsertOptimizerFoldResult :exec
INSERT INTO optimizer_fold_results (
    run_id, configuration_id, fold_index,
    train_from, train_to, test_from, test_to,
    realized_return, marked_return, total_r, sharpe, max_drawdown, max_drawdown_dollars,
    peak_locked_pct, trades, fees, forced_liquidation, metrics, equity_curve
) VALUES (
    sqlc.arg('run_id'), sqlc.arg('configuration_id'), sqlc.arg('fold_index'),
    sqlc.arg('train_from'), sqlc.arg('train_to'), sqlc.arg('test_from'), sqlc.arg('test_to'),
    sqlc.arg('realized_return'), sqlc.arg('marked_return'), sqlc.arg('total_r'), sqlc.arg('sharpe'), sqlc.arg('max_drawdown'), sqlc.arg('max_drawdown_dollars'),
    sqlc.arg('peak_locked_pct'), sqlc.arg('trades'), sqlc.arg('fees'), sqlc.arg('forced_liquidation'), sqlc.arg('metrics'), sqlc.arg('equity_curve')
)
ON CONFLICT (run_id, configuration_id, fold_index) DO UPDATE SET
    realized_return = EXCLUDED.realized_return,
    marked_return = EXCLUDED.marked_return,
    total_r = EXCLUDED.total_r,
    sharpe = EXCLUDED.sharpe,
    max_drawdown = EXCLUDED.max_drawdown,
    max_drawdown_dollars = EXCLUDED.max_drawdown_dollars,
    peak_locked_pct = EXCLUDED.peak_locked_pct,
    trades = EXCLUDED.trades,
    fees = EXCLUDED.fees,
    forced_liquidation = EXCLUDED.forced_liquidation,
    metrics = EXCLUDED.metrics,
    equity_curve = EXCLUDED.equity_curve;

-- name: SetOptimizerCandidateRepresentative :exec
UPDATE optimizer_candidate_results
SET representative_strategy_id = sqlc.arg('representative_strategy_id'),
    strategy_rank_snapshot_id = sqlc.arg('strategy_rank_snapshot_id'),
    updated_at = now()
WHERE run_id = sqlc.arg('run_id') AND configuration_id = sqlc.arg('configuration_id');

-- name: ListOptimizerCandidateResultsFull :many
SELECT r.*, c.config_hash, c.strategy_params, c.execution_profile, c.management_policy,
       p.parent_configuration_id, p.generation, p.proposal_kind,
       p.mutation_vector, p.normalized_distance, p.changed_parameters
FROM optimizer_candidate_results r
JOIN tradebot_configurations c ON c.id = r.configuration_id
LEFT JOIN optimizer_candidate_proposals p
  ON p.run_id = r.run_id AND p.configuration_id = r.configuration_id
WHERE r.run_id = sqlc.arg('runID')
ORDER BY r.eligible DESC, r.rank ASC, r.configuration_id;

-- name: ListOptimizerCandidateResults :many
-- Dashboard and selection callers do not need the persisted full-resolution
-- candidate curve. The leading dashboard curves are rebuilt from fold rows and
-- downsampled separately; returning [] here avoids pulling every candidate's
-- potentially multi-megabyte JSONB curve through Postgres and Go.
SELECT
    r.run_id, r.configuration_id, r.rank, r.stage, r.eligible, r.score,
    r.realized_return, r.marked_return, r.total_r, r.r_per_day,
    r.profit_per_trade, r.max_drawdown, r.max_drawdown_dollars, r.peak_locked_pct,
    r.positive_fold_pct, r.trades, r.fees, r.metrics,
    '[]'::jsonb AS equity_curve, r.rejection_reasons, r.updated_at,
    r.paired_utility, r.paired_fold_win_pct, r.evolutionary_success,
    r.center_configuration_id, r.sharpe,
    r.representative_strategy_id, r.strategy_rank_snapshot_id,
    representative_strategy.name AS representative_strategy,
    c.config_hash, c.strategy_params, c.execution_profile, c.management_policy,
    c.strategy_execution_policies,
    p.parent_configuration_id, p.generation, p.proposal_kind,
    p.mutation_vector, p.normalized_distance, p.changed_parameters
FROM optimizer_candidate_results r
JOIN tradebot_configurations c ON c.id = r.configuration_id
LEFT JOIN strategies representative_strategy ON representative_strategy.id = r.representative_strategy_id
LEFT JOIN optimizer_candidate_proposals p
  ON p.run_id = r.run_id AND p.configuration_id = r.configuration_id
WHERE r.run_id = sqlc.arg('runID')
ORDER BY r.eligible DESC, r.rank ASC, r.configuration_id;

-- name: GetOptimizerSearchState :one
SELECT state.*, config.config_hash, config.strategy_params,
       config.execution_profile, config.management_policy
FROM optimizer_search_states state
JOIN tradebot_configurations config ON config.id = state.center_configuration_id
WHERE state.asset_class = sqlc.arg('asset_class') AND state.timeframe = sqlc.arg('timeframe') AND state.direction = sqlc.arg('direction');

-- name: EnsureOptimizerSearchState :one
INSERT INTO optimizer_search_states (
    asset_class, timeframe, direction, center_configuration_id,
    preset, radii, direction_bias, locked_parameters, elite_archive,
    last_probed_generation
) VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('center_configuration_id'), sqlc.arg('preset'), sqlc.arg('radii'), sqlc.arg('direction_bias'), sqlc.arg('locked_parameters'), sqlc.arg('elite_archive'), sqlc.arg('last_probed_generation'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    asset_class = optimizer_search_states.asset_class
RETURNING *;

-- name: UpsertOptimizerSearchState :one
INSERT INTO optimizer_search_states (
    asset_class, timeframe, direction, center_configuration_id,
    generation, preset, radii, direction_bias, locked_parameters,
    elite_archive, stagnation_count, last_probed_generation,
    last_learned_watermark, last_learned_run_id
) VALUES (
    sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('center_configuration_id'), sqlc.arg('generation'), sqlc.arg('preset'), sqlc.arg('radii'), sqlc.arg('direction_bias'), sqlc.arg('locked_parameters'),
    sqlc.arg('elite_archive'), sqlc.arg('stagnation_count'), sqlc.arg('last_probed_generation'), sqlc.arg('last_learned_watermark'), sqlc.arg('last_learned_run_id')
)
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    center_configuration_id = EXCLUDED.center_configuration_id,
    generation = EXCLUDED.generation,
    preset = EXCLUDED.preset,
    radii = EXCLUDED.radii,
    direction_bias = EXCLUDED.direction_bias,
    locked_parameters = EXCLUDED.locked_parameters,
    elite_archive = EXCLUDED.elite_archive,
    stagnation_count = EXCLUDED.stagnation_count,
    last_probed_generation = EXCLUDED.last_probed_generation,
    last_learned_watermark = EXCLUDED.last_learned_watermark,
    last_learned_run_id = EXCLUDED.last_learned_run_id,
    revision = optimizer_search_states.revision + 1,
    updated_at = now()
RETURNING *;

-- name: InsertOptimizerCandidateProposal :one
INSERT INTO optimizer_candidate_proposals (
    run_id, configuration_id, parent_configuration_id, generation,
    proposal_kind, mutation_vector, normalized_distance, changed_parameters
) VALUES (sqlc.arg('run_id'), sqlc.arg('configuration_id'), sqlc.arg('parent_configuration_id'), sqlc.arg('generation'), sqlc.arg('proposal_kind'), sqlc.arg('mutation_vector'), sqlc.arg('normalized_distance'), sqlc.arg('changed_parameters'))
ON CONFLICT (run_id, configuration_id) DO UPDATE SET
    parent_configuration_id = EXCLUDED.parent_configuration_id,
    generation = EXCLUDED.generation,
    proposal_kind = EXCLUDED.proposal_kind,
    mutation_vector = EXCLUDED.mutation_vector,
    normalized_distance = EXCLUDED.normalized_distance,
    changed_parameters = EXCLUDED.changed_parameters
RETURNING *;

-- name: ListOptimizerCandidateProposals :many
SELECT * FROM optimizer_candidate_proposals
WHERE run_id = sqlc.arg('runID')
ORDER BY configuration_id;

-- name: InsertOptimizerSearchEvent :one
INSERT INTO optimizer_search_events (
    asset_class, timeframe, direction, event_type, run_id,
    configuration_id, generation, revision, details
) VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('event_type'), sqlc.arg('run_id'), sqlc.arg('configuration_id'), sqlc.arg('generation'), sqlc.arg('revision'), sqlc.arg('details'))
RETURNING *;

-- name: ListOptimizerSearchEvents :many
SELECT * FROM optimizer_search_events
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND direction = sqlc.arg('direction')
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: ListOptimizerFoldResults :many
SELECT * FROM optimizer_fold_results
WHERE run_id = sqlc.arg('run_id') AND configuration_id = sqlc.arg('configuration_id')
ORDER BY fold_index;

-- name: ListTradebotConfigSelections :many
SELECT s.*, c.config_hash, c.strategy_params, c.execution_profile, c.management_policy
FROM tradebot_config_selections s
JOIN tradebot_configurations c ON c.id = s.configuration_id
WHERE (sqlc.arg(asset_class)::text = '' OR s.asset_class = sqlc.arg(asset_class)::text)
  AND (sqlc.arg(timeframe)::text = '' OR s.timeframe = sqlc.arg(timeframe)::text)
  AND (sqlc.arg(direction)::text = '' OR s.direction = sqlc.arg(direction)::text)
ORDER BY s.effective_from DESC
LIMIT sqlc.arg(result_limit);

-- name: CloseActiveTradebotConfigSelection :exec
UPDATE tradebot_config_selections
SET effective_to = sqlc.arg('effective_to')
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND direction = sqlc.arg('direction')
  AND effective_to IS NULL;

-- name: InsertTradebotConfigSelection :one
INSERT INTO tradebot_config_selections (
    configuration_id, asset_class, timeframe, direction,
    source_run_id, effective_from, promotion_reason
) VALUES (sqlc.arg('configuration_id'), sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('source_run_id'), sqlc.arg('effective_from'), sqlc.arg('promotion_reason'))
RETURNING *;

-- name: GetOptimizerScopeState :one
SELECT * FROM optimizer_scope_state
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND direction = sqlc.arg('direction');

-- name: ListOptimizerScopeStates :many
SELECT * FROM optimizer_scope_state
ORDER BY asset_class, timeframe, direction;

-- name: EnsureOptimizerScopeState :one
INSERT INTO optimizer_scope_state (asset_class, timeframe, direction)
VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    asset_class = optimizer_scope_state.asset_class
RETURNING *;

-- name: SetOptimizerBootstrapState :one
INSERT INTO optimizer_scope_state (
    asset_class, timeframe, direction,
    bootstrap_status, bootstrap_run_id, bootstrap_data_watermark,
    bootstrap_universe_hash, bootstrap_error
) VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('bootstrap_status'), sqlc.arg('bootstrap_run_id'), sqlc.arg('bootstrap_data_watermark'), sqlc.arg('bootstrap_universe_hash'), sqlc.arg('bootstrap_error'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    bootstrap_status = EXCLUDED.bootstrap_status,
    bootstrap_run_id = EXCLUDED.bootstrap_run_id,
    bootstrap_data_watermark = EXCLUDED.bootstrap_data_watermark,
    bootstrap_universe_hash = EXCLUDED.bootstrap_universe_hash,
    bootstrap_error = EXCLUDED.bootstrap_error,
    bootstrap_updated_at = now(),
    revision = optimizer_scope_state.revision + 1,
    updated_at = now()
RETURNING *;

-- name: GetPreviousTradebotConfigSelection :one
SELECT * FROM tradebot_config_selections
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND direction = sqlc.arg('direction')
  AND effective_to IS NOT NULL
ORDER BY effective_from DESC
LIMIT 1;

-- name: UpsertOptimizerScopeState :one
INSERT INTO optimizer_scope_state (
    asset_class, timeframe, direction, paused,
    challenger_config_id, consecutive_wins, challenger_watermark, next_run_at
) VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('paused'), sqlc.arg('challenger_config_id'), sqlc.arg('consecutive_wins'), sqlc.arg('challenger_watermark'), sqlc.arg('next_run_at'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    paused = EXCLUDED.paused,
    challenger_config_id = EXCLUDED.challenger_config_id,
    consecutive_wins = EXCLUDED.consecutive_wins,
    challenger_watermark = EXCLUDED.challenger_watermark,
    next_run_at = EXCLUDED.next_run_at,
    revision = optimizer_scope_state.revision + 1,
    updated_at = now()
RETURNING *;

-- name: SetOptimizerScopePaused :one
INSERT INTO optimizer_scope_state (asset_class, timeframe, direction, paused)
VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('paused'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    paused = EXCLUDED.paused,
    revision = optimizer_scope_state.revision + 1,
    updated_at = now()
RETURNING *;

-- name: TouchOptimizerScopeState :one
INSERT INTO optimizer_scope_state (asset_class, timeframe, direction)
VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    revision = optimizer_scope_state.revision + 1,
    updated_at = now()
RETURNING *;

-- name: ResetOptimizerChallengerState :one
INSERT INTO optimizer_scope_state (asset_class, timeframe, direction)
VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    challenger_config_id = NULL,
    consecutive_wins = 0,
    challenger_watermark = NULL,
    revision = optimizer_scope_state.revision + 1,
    updated_at = now()
RETURNING *;

-- name: GetOptimizerHistoryBackfill :one
SELECT * FROM optimizer_history_backfills
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND universe_hash = sqlc.arg('universe_hash');

-- name: StartOptimizerHistoryBackfill :one
INSERT INTO optimizer_history_backfills (
    asset_class, timeframe, universe_hash, status,
    requested_from, requested_to, symbols_total
) VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('universe_hash'), 'running', sqlc.arg('requested_from'), sqlc.arg('requested_to'), sqlc.arg('symbols_total'))
ON CONFLICT (asset_class, timeframe, universe_hash) DO UPDATE SET
    status = 'running',
    requested_from = EXCLUDED.requested_from,
    requested_to = EXCLUDED.requested_to,
    symbols_total = EXCLUDED.symbols_total,
    symbols_fetched = 0,
    bars_stored = 0,
    error = '',
    started_at = now(),
    finished_at = NULL
RETURNING *;

-- name: CompleteOptimizerHistoryBackfill :one
UPDATE optimizer_history_backfills SET
    status = sqlc.arg('status'),
    symbols_fetched = sqlc.arg('symbols_fetched'),
    bars_stored = sqlc.arg('bars_stored'),
    error = sqlc.arg('error'),
    finished_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: CreateSignalRun :one
INSERT INTO signal_runs (
    configuration_id, effective_from, range_start, range_end
) VALUES (sqlc.arg('configuration_id'), sqlc.arg('effective_from'), sqlc.arg('range_start'), sqlc.arg('range_end'))
RETURNING *;

-- name: CompleteSignalRun :exec
UPDATE signal_runs
SET status = sqlc.arg('status'), signals_written = sqlc.arg('signals_written'), error = sqlc.arg('error'), finished_at = now()
WHERE id = sqlc.arg('id');

-- name: UpsertConfiguredSignal :one
INSERT INTO signals (
    strategy_id, symbol, ts, direction, conviction, entry, stop, target,
    rationale, source, asset_type, timeframe, qty, source_votes, configuration_id, signal_run_id,
    portfolio_hash, strategy_config_hash, strategy_evaluation_version
) VALUES (
    sqlc.arg('strategy_id'), sqlc.arg('symbol'), sqlc.arg('ts'), sqlc.arg('direction'), sqlc.arg('conviction'), sqlc.arg('entry'), sqlc.arg('stop'), sqlc.arg('target'),
    sqlc.arg('rationale'), sqlc.arg('source'), sqlc.arg('asset_type'), sqlc.arg('timeframe'), sqlc.arg('qty'),
    coalesce(sqlc.arg(source_votes)::jsonb, '[]'::jsonb), sqlc.arg('configuration_id'), sqlc.arg('signal_run_id'), sqlc.arg('portfolio_hash'), sqlc.arg('strategy_config_hash'), sqlc.arg('strategy_evaluation_version')
)
ON CONFLICT (configuration_id, strategy_id, symbol, ts, timeframe, portfolio_hash, strategy_config_hash, strategy_evaluation_version)
    WHERE configuration_id IS NOT NULL
DO UPDATE SET
    direction = EXCLUDED.direction,
    conviction = EXCLUDED.conviction,
    entry = EXCLUDED.entry,
    stop = EXCLUDED.stop,
    target = EXCLUDED.target,
    rationale = EXCLUDED.rationale,
    source = EXCLUDED.source,
    asset_type = EXCLUDED.asset_type,
    qty = EXCLUDED.qty,
    signal_run_id = EXCLUDED.signal_run_id,
    source_votes = EXCLUDED.source_votes
RETURNING id;
