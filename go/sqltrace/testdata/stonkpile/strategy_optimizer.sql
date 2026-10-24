-- name: GetOptimizerStrategyBacktest :one
SELECT backtest.*, strategy.name AS strategy_name
FROM optimizer_strategy_backtests backtest
JOIN strategies strategy ON strategy.id = backtest.strategy_id
WHERE backtest.configuration_id = sqlc.arg(configuration_id)
  AND backtest.strategy_id = sqlc.arg(strategy_id)
  AND backtest.asset_class = sqlc.arg(asset_class)
  AND backtest.timeframe = sqlc.arg(timeframe)
  AND backtest.direction = sqlc.arg(direction)
  AND backtest.train_from = sqlc.arg(train_from)
  AND backtest.train_to = sqlc.arg(train_to)
  AND backtest.test_from = sqlc.arg(test_from)
  AND backtest.test_to = sqlc.arg(test_to)
  AND backtest.data_watermark = sqlc.arg(data_watermark)
  AND backtest.universe_hash = sqlc.arg(universe_hash)
  AND backtest.execution_hash = sqlc.arg(execution_hash)
  AND backtest.evaluation_version = sqlc.arg(evaluation_version);

-- name: GetOptimizerStrategyBacktestByID :one
SELECT backtest.*, strategy.name AS strategy_name
FROM optimizer_strategy_backtests backtest
JOIN strategies strategy ON strategy.id = backtest.strategy_id
WHERE backtest.id = sqlc.arg('id');

-- name: UpsertOptimizerStrategyBacktest :one
INSERT INTO optimizer_strategy_backtests (
    configuration_id, strategy_id, asset_class, timeframe, direction,
    train_from, train_to, test_from, test_to, data_watermark,
    universe_hash, execution_hash, evaluation_version, status, starting_equity,
    realized_final, marked_final, open_pnl, realized_return, marked_return,
    expectancy, profit_factor, total_r, trade_sharpe, max_drawdown, max_drawdown_dollars,
    peak_locked_pct, peak_gross_dollars, trades, fees, forced_liquidation,
    borrow_complete, tax_complete, metrics, equity_curve, error
) VALUES (
    sqlc.arg('configuration_id'), sqlc.arg('strategy_id'), sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'),
    sqlc.arg('train_from'), sqlc.arg('train_to'), sqlc.arg('test_from'), sqlc.arg('test_to'), sqlc.arg('data_watermark'),
    sqlc.arg('universe_hash'), sqlc.arg('execution_hash'), sqlc.arg('evaluation_version'), sqlc.arg('status'), sqlc.arg('starting_equity'),
    sqlc.arg('realized_final'), sqlc.arg('marked_final'), sqlc.arg('open_pnl'), sqlc.arg('realized_return'), sqlc.arg('marked_return'),
    sqlc.arg('expectancy'), sqlc.arg('profit_factor'), sqlc.arg('total_r'), sqlc.arg('trade_sharpe'), sqlc.arg('max_drawdown'), sqlc.arg('max_drawdown_dollars'),
    sqlc.arg('peak_locked_pct'), sqlc.arg('peak_gross_dollars'), sqlc.arg('trades'), sqlc.arg('fees'), sqlc.arg('forced_liquidation'),
    sqlc.arg('borrow_complete'), sqlc.arg('tax_complete'), sqlc.arg('metrics'), sqlc.arg('equity_curve'), sqlc.arg('error')
)
ON CONFLICT (
    configuration_id, strategy_id, asset_class, timeframe, direction,
    train_from, train_to, test_from, test_to, data_watermark,
    universe_hash, execution_hash, evaluation_version
) DO UPDATE SET
    status = EXCLUDED.status,
    starting_equity = EXCLUDED.starting_equity,
    realized_final = EXCLUDED.realized_final,
    marked_final = EXCLUDED.marked_final,
    open_pnl = EXCLUDED.open_pnl,
    realized_return = EXCLUDED.realized_return,
    marked_return = EXCLUDED.marked_return,
    expectancy = EXCLUDED.expectancy,
    profit_factor = EXCLUDED.profit_factor,
    total_r = EXCLUDED.total_r,
    trade_sharpe = EXCLUDED.trade_sharpe,
    max_drawdown = EXCLUDED.max_drawdown,
    max_drawdown_dollars = EXCLUDED.max_drawdown_dollars,
    peak_locked_pct = EXCLUDED.peak_locked_pct,
    peak_gross_dollars = EXCLUDED.peak_gross_dollars,
    trades = EXCLUDED.trades,
    fees = EXCLUDED.fees,
    forced_liquidation = EXCLUDED.forced_liquidation,
    borrow_complete = EXCLUDED.borrow_complete,
    tax_complete = EXCLUDED.tax_complete,
    metrics = EXCLUDED.metrics,
    equity_curve = EXCLUDED.equity_curve,
    error = EXCLUDED.error,
    updated_at = now()
RETURNING *;

-- name: ListOptimizerStrategyBacktestsForRanking :many
SELECT backtest.*, strategy.name AS strategy_name
FROM optimizer_strategy_backtests backtest
JOIN strategies strategy ON strategy.id = backtest.strategy_id
WHERE backtest.configuration_id = sqlc.arg(configuration_id)
  AND backtest.asset_class = sqlc.arg(asset_class)
  AND backtest.timeframe = sqlc.arg(timeframe)
  AND backtest.direction = sqlc.arg(direction)
  AND backtest.data_watermark = sqlc.arg(data_watermark)
  AND backtest.universe_hash = sqlc.arg(universe_hash)
  AND backtest.evaluation_version = sqlc.arg(evaluation_version)
  AND backtest.status = 'completed'
ORDER BY strategy.name, backtest.test_from;

-- name: UpsertOptimizerStrategyRankSnapshot :one
INSERT INTO optimizer_strategy_rank_snapshots (
    configuration_id, asset_class, timeframe, direction,
    data_watermark, universe_hash, score_version
) VALUES (sqlc.arg('configuration_id'), sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('data_watermark'), sqlc.arg('universe_hash'), sqlc.arg('score_version'))
ON CONFLICT (
    configuration_id, asset_class, timeframe, direction,
    data_watermark, universe_hash, score_version
) DO UPDATE SET configuration_id = EXCLUDED.configuration_id
RETURNING *;

-- name: UpsertOptimizerStrategyRankEntry :one
INSERT INTO optimizer_strategy_rank_entries (
    snapshot_id, strategy_id, rank, score, eligible,
    marked_return, total_r, marked_profit_per_trade,
    expectancy, profit_factor, max_drawdown, max_drawdown_dollars, trade_sharpe,
    open_pnl_return, peak_locked_pct, positive_fold_pct, trades,
    backtest_ids, score_components, rejection_reasons
) VALUES (
    sqlc.arg('snapshot_id'), sqlc.arg('strategy_id'), sqlc.arg('rank'), sqlc.arg('score'), sqlc.arg('eligible'),
    sqlc.arg('marked_return'), sqlc.arg('total_r'), sqlc.arg('marked_profit_per_trade'),
    sqlc.arg('expectancy'), sqlc.arg('profit_factor'), sqlc.arg('max_drawdown'), sqlc.arg('max_drawdown_dollars'), sqlc.arg('trade_sharpe'),
    sqlc.arg('open_pnl_return'), sqlc.arg('peak_locked_pct'), sqlc.arg('positive_fold_pct'), sqlc.arg('trades'),
    sqlc.arg('backtest_ids'), sqlc.arg('score_components'), sqlc.arg('rejection_reasons')
)
ON CONFLICT (snapshot_id, strategy_id) DO UPDATE SET
    rank = EXCLUDED.rank,
    score = EXCLUDED.score,
    eligible = EXCLUDED.eligible,
    marked_return = EXCLUDED.marked_return,
    total_r = EXCLUDED.total_r,
    marked_profit_per_trade = EXCLUDED.marked_profit_per_trade,
    expectancy = EXCLUDED.expectancy,
    profit_factor = EXCLUDED.profit_factor,
    max_drawdown = EXCLUDED.max_drawdown,
    max_drawdown_dollars = EXCLUDED.max_drawdown_dollars,
    trade_sharpe = EXCLUDED.trade_sharpe,
    open_pnl_return = EXCLUDED.open_pnl_return,
    peak_locked_pct = EXCLUDED.peak_locked_pct,
    positive_fold_pct = EXCLUDED.positive_fold_pct,
    trades = EXCLUDED.trades,
    backtest_ids = EXCLUDED.backtest_ids,
    score_components = EXCLUDED.score_components,
    rejection_reasons = EXCLUDED.rejection_reasons
RETURNING *;

-- name: GetLatestOptimizerStrategyRankSnapshot :one
SELECT *
FROM optimizer_strategy_rank_snapshots
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND direction = sqlc.arg('direction')
ORDER BY data_watermark DESC, id DESC
LIMIT 1;

-- name: GetOptimizerStrategyRankSnapshot :one
SELECT *
FROM optimizer_strategy_rank_snapshots
WHERE id = sqlc.arg('id');

-- name: GetOptimizerStrategyRankSnapshotForConfiguration :one
SELECT *
FROM optimizer_strategy_rank_snapshots
WHERE configuration_id = sqlc.arg('configuration_id')
  AND asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND direction = sqlc.arg('direction')
ORDER BY data_watermark DESC, id DESC
LIMIT 1;

-- name: ListOptimizerStrategyRankEntries :many
SELECT entry.*, strategy.name AS strategy_name
FROM optimizer_strategy_rank_entries entry
JOIN strategies strategy ON strategy.id = entry.strategy_id
WHERE entry.snapshot_id = sqlc.arg('snapshotID')
ORDER BY entry.eligible DESC, entry.rank, strategy.name;

-- name: GetCurrentTradebotStrategySelection :one
SELECT selection.*, strategy.name AS strategy_name, config.config_hash
FROM tradebot_strategy_selections selection
LEFT JOIN strategies strategy ON strategy.id = selection.strategy_id
JOIN tradebot_configurations config ON config.id = selection.configuration_id
WHERE selection.asset_class = sqlc.arg('asset_class')
  AND selection.timeframe = sqlc.arg('timeframe')
  AND selection.direction = sqlc.arg('direction')
  AND selection.effective_to IS NULL
ORDER BY selection.effective_from DESC
LIMIT 1;

-- name: GetEffectiveTradebotStrategySelection :one
SELECT selection.*, strategy.name AS strategy_name, config.config_hash
FROM tradebot_strategy_selections selection
LEFT JOIN strategies strategy ON strategy.id = selection.strategy_id
JOIN tradebot_configurations config ON config.id = selection.configuration_id
WHERE selection.asset_class = sqlc.arg('asset_class')
  AND selection.timeframe = sqlc.arg('timeframe')
  AND selection.direction = sqlc.arg('direction')
  AND selection.effective_from <= sqlc.arg('effective_from')
  AND (selection.effective_to IS NULL OR selection.effective_to > sqlc.arg('effective_from'))
ORDER BY selection.effective_from DESC
LIMIT 1;

-- name: ListTradebotStrategySelections :many
SELECT selection.*, strategy.name AS strategy_name, config.config_hash
FROM tradebot_strategy_selections selection
LEFT JOIN strategies strategy ON strategy.id = selection.strategy_id
JOIN tradebot_configurations config ON config.id = selection.configuration_id
WHERE (sqlc.arg(asset_class)::text = '' OR selection.asset_class = sqlc.arg(asset_class)::text)
  AND (sqlc.arg(timeframe)::text = '' OR selection.timeframe = sqlc.arg(timeframe)::text)
  AND (sqlc.arg(direction)::text = '' OR selection.direction = sqlc.arg(direction)::text)
ORDER BY selection.effective_from DESC
LIMIT sqlc.arg(result_limit);

-- name: GetLatestTradebotStrategySelectionForConfiguration :one
SELECT selection.*, strategy.name AS strategy_name, config.config_hash
FROM tradebot_strategy_selections selection
LEFT JOIN strategies strategy ON strategy.id = selection.strategy_id
JOIN tradebot_configurations config ON config.id = selection.configuration_id
WHERE selection.configuration_id = sqlc.arg('configuration_id')
  AND selection.asset_class = sqlc.arg('asset_class')
  AND selection.timeframe = sqlc.arg('timeframe')
  AND selection.direction = sqlc.arg('direction')
ORDER BY selection.effective_from DESC
LIMIT 1;

-- name: CloseActiveTradebotStrategySelection :exec
UPDATE tradebot_strategy_selections
SET effective_to = sqlc.arg('effective_to')
WHERE asset_class = sqlc.arg('asset_class') AND timeframe = sqlc.arg('timeframe') AND direction = sqlc.arg('direction')
  AND effective_to IS NULL;

-- name: InsertTradebotStrategySelection :one
INSERT INTO tradebot_strategy_selections (
    rank_snapshot_id, configuration_id, strategy_id,
    asset_class, timeframe, direction, status, score, rank,
    evaluation_watermark, effective_from, selection_reason, execution_policy
) VALUES (
    sqlc.arg('rank_snapshot_id'), sqlc.arg('configuration_id'), sqlc.arg('strategy_id'),
    sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('status'), sqlc.arg('score'), sqlc.arg('rank'),
    sqlc.arg('evaluation_watermark'), sqlc.arg('effective_from'), sqlc.arg('selection_reason'), sqlc.arg('execution_policy')
)
RETURNING *;

-- name: SetOptimizerStrategyChallenger :one
INSERT INTO optimizer_scope_state (
    asset_class, timeframe, direction,
    strategy_challenger_id, strategy_consecutive_wins,
    strategy_challenger_watermark, last_strategy_rank_snapshot_id
) VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('strategy_challenger_id'), sqlc.arg('strategy_consecutive_wins'), sqlc.arg('strategy_challenger_watermark'), sqlc.arg('last_strategy_rank_snapshot_id'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    strategy_challenger_id = EXCLUDED.strategy_challenger_id,
    strategy_consecutive_wins = EXCLUDED.strategy_consecutive_wins,
    strategy_challenger_watermark = EXCLUDED.strategy_challenger_watermark,
    last_strategy_rank_snapshot_id = EXCLUDED.last_strategy_rank_snapshot_id,
    revision = optimizer_scope_state.revision + 1,
    updated_at = now()
RETURNING *;

-- name: MarkTradebotStrategySelectionApplied :one
INSERT INTO optimizer_scope_state (
    asset_class, timeframe, direction,
    last_applied_strategy_selection_id, last_strategy_applied_at,
    last_strategy_signal_watermark
) VALUES (sqlc.arg('asset_class'), sqlc.arg('timeframe'), sqlc.arg('direction'), sqlc.arg('last_applied_strategy_selection_id'), now(), sqlc.arg('last_strategy_signal_watermark'))
ON CONFLICT (asset_class, timeframe, direction) DO UPDATE SET
    last_applied_strategy_selection_id = EXCLUDED.last_applied_strategy_selection_id,
    last_strategy_applied_at = EXCLUDED.last_strategy_applied_at,
    last_strategy_signal_watermark = EXCLUDED.last_strategy_signal_watermark,
    revision = optimizer_scope_state.revision + 1,
    updated_at = now()
RETURNING *;

-- name: ListTradebotOpenPositionOwners :many
SELECT strategy, count(*)::integer AS positions
FROM tradebot_positions
WHERE timeframe = sqlc.arg(timeframe)
  AND direction = sqlc.arg(direction)
  AND scope_id LIKE '%' || sqlc.arg(asset_class)::text || '%'
GROUP BY strategy
ORDER BY strategy;

-- name: ListTradebotPositionStrategyAllocations :many
SELECT DISTINCT strategy, configuration_id, direction
FROM tradebot_positions
WHERE timeframe = sqlc.arg(timeframe)
  AND scope_id LIKE '%' || sqlc.arg(asset_class)::text || '%'
  AND configuration_id IS NOT NULL
ORDER BY configuration_id, strategy, direction;
