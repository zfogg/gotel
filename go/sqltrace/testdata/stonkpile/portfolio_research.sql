-- name: CreatePortfolioResearchRun :one
INSERT INTO portfolio_research_runs (
    algorithm_version, strategy, timeframe, from_ts, to_ts,
    candidate_symbols, required_trade_symbols, signal_context_symbols,
    portfolio_size, trials, paired_swaps_per_symbol, deterministic_seed,
    evaluator_version, research_mode, decision_at, min_improvement
)
VALUES (
    'v6.3', sqlc.arg('strategy'), sqlc.arg('timeframe'), sqlc.arg('from_ts'), sqlc.arg('to_ts'),
    sqlc.arg('candidate_symbols'), sqlc.arg('required_trade_symbols'), sqlc.arg('signal_context_symbols'),
    sqlc.arg('portfolio_size'), sqlc.arg('trials'), sqlc.arg('paired_swaps_per_symbol'), sqlc.arg('deterministic_seed'),
    sqlc.arg('evaluator_version'),
    COALESCE(NULLIF(sqlc.arg(research_mode)::text, ''), 'live'),
    COALESCE(sqlc.narg(decision_at)::timestamptz, sqlc.arg('to_ts')),
    sqlc.arg(min_improvement)
)
RETURNING id;

-- name: FailSupersededPortfolioResearchRuns :exec
UPDATE portfolio_research_runs
SET status = 'failed', completed_at = now(),
    error_message = 'superseded by a retry of the same research cutoff'
WHERE status = 'running'
  AND timeframe = sqlc.arg(timeframe)
  AND to_ts = sqlc.arg(to_ts)
  AND deterministic_seed = sqlc.arg(deterministic_seed);

-- name: RecordPortfolioPromotionDecision :exec
UPDATE portfolio_research_runs
SET promotion_decision = sqlc.arg('promotion_decision')
WHERE id = sqlc.arg('id');

-- name: CompletePortfolioResearchRun :exec
UPDATE portfolio_research_runs
SET status = 'completed', completed_at = now(), error_message = ''
WHERE id = sqlc.arg('id');

-- name: FailPortfolioResearchRun :exec
UPDATE portfolio_research_runs
SET status = 'failed', completed_at = now(), error_message = sqlc.arg('error_message')
WHERE id = sqlc.arg('id');

-- name: ListLatestFailedPortfolioResearchRuns :many
WITH ranked AS (
    SELECT id, timeframe, research_mode, status, created_at, completed_at, error_message,
           row_number() OVER (PARTITION BY timeframe ORDER BY id DESC) AS timeframe_rank
    FROM portfolio_research_runs
    WHERE status IN ('failed', 'completed', 'running')
), latest AS (
    SELECT id, timeframe, research_mode, status, created_at, completed_at, error_message
    FROM ranked
    WHERE timeframe_rank = 1
)
SELECT id, timeframe, research_mode, created_at, completed_at, error_message
FROM latest
WHERE status = 'failed'
  AND completed_at IS NOT NULL
  -- Worker shutdowns and activity timeouts are expected handoff signals, not
  -- evidence of a broken research lane. Temporal retries or the next schedule
  -- run must get a chance before health reports a real failure.
  AND error_message NOT ILIKE '%context canceled%'
  AND error_message NOT ILIKE '%context deadline exceeded%'
  -- Require sustained failure across three consecutive attempts for this
  -- timeframe. A newer running or completed row naturally suppresses this.
  AND (
      SELECT count(*)
      FROM ranked recent
      WHERE recent.timeframe = latest.timeframe
        AND recent.timeframe_rank <= 3
        AND recent.status = 'failed'
        AND recent.error_message NOT ILIKE '%context canceled%'
        AND recent.error_message NOT ILIKE '%context deadline exceeded%'
  ) = 3
ORDER BY timeframe;

-- name: UpsertPortfolioContext :one
INSERT INTO portfolio_contexts (
    run_id, fingerprint, source, trade_symbols, signal_context_symbols,
    parent_context_id, swapped_in, swapped_out
)
VALUES (sqlc.arg('run_id'), sqlc.arg('fingerprint'), sqlc.arg('source'), sqlc.arg('trade_symbols'), sqlc.arg('signal_context_symbols'), sqlc.arg('parent_context_id'), sqlc.arg('swapped_in'), sqlc.arg('swapped_out'))
ON CONFLICT (run_id, fingerprint) DO UPDATE
SET source = EXCLUDED.source,
    trade_symbols = EXCLUDED.trade_symbols,
    signal_context_symbols = EXCLUDED.signal_context_symbols,
    parent_context_id = COALESCE(EXCLUDED.parent_context_id, portfolio_contexts.parent_context_id),
    swapped_in = EXCLUDED.swapped_in,
    swapped_out = EXCLUDED.swapped_out
RETURNING id;

-- name: UpsertPortfolioContextResult :exec
INSERT INTO portfolio_context_results (
    context_id, score, realized_final_equity, marked_final_equity,
    forced_liquidation_final_equity, total_r, profit_factor, max_dd_pct,
    trades, profitable_windows, windows
)
VALUES (sqlc.arg('context_id'), sqlc.arg('score'), sqlc.arg('realized_final_equity'), sqlc.arg('marked_final_equity'), sqlc.arg('forced_liquidation_final_equity'), sqlc.arg('total_r'), sqlc.arg('profit_factor'), sqlc.arg('max_dd_pct'), sqlc.arg('trades'), sqlc.arg('profitable_windows'), sqlc.arg('windows'))
ON CONFLICT (context_id) DO UPDATE
SET score = EXCLUDED.score,
    realized_final_equity = EXCLUDED.realized_final_equity,
    marked_final_equity = EXCLUDED.marked_final_equity,
    forced_liquidation_final_equity = EXCLUDED.forced_liquidation_final_equity,
    total_r = EXCLUDED.total_r,
    profit_factor = EXCLUDED.profit_factor,
    max_dd_pct = EXCLUDED.max_dd_pct,
    trades = EXCLUDED.trades,
    profitable_windows = EXCLUDED.profitable_windows,
    windows = EXCLUDED.windows;

-- name: UpsertPortfolioSymbolResult :exec
INSERT INTO portfolio_symbol_results (
    context_id, ticker, trades, wins, losses, win_rate,
    realized_pnl, unrealized_pnl, total_r, avg_r
)
VALUES (sqlc.arg('context_id'), sqlc.arg('ticker'), sqlc.arg('trades'), sqlc.arg('wins'), sqlc.arg('losses'), sqlc.arg('win_rate'), sqlc.arg('realized_pnl'), sqlc.arg('unrealized_pnl'), sqlc.arg('total_r'), sqlc.arg('avg_r'))
ON CONFLICT (context_id, ticker) DO UPDATE
SET trades = EXCLUDED.trades,
    wins = EXCLUDED.wins,
    losses = EXCLUDED.losses,
    win_rate = EXCLUDED.win_rate,
    realized_pnl = EXCLUDED.realized_pnl,
    unrealized_pnl = EXCLUDED.unrealized_pnl,
    total_r = EXCLUDED.total_r,
    avg_r = EXCLUDED.avg_r;

-- name: UpsertPortfolioContextWindowResult :exec
INSERT INTO portfolio_context_window_results (
    context_id, window_index, from_ts, to_ts,
    realized_final_equity, marked_final_equity,
    forced_liquidation_final_equity, total_r, profit_factor,
    max_dd_pct, trades, profitable
)
VALUES (sqlc.arg('context_id'), sqlc.arg('window_index'), sqlc.arg('from_ts'), sqlc.arg('to_ts'), sqlc.arg('realized_final_equity'), sqlc.arg('marked_final_equity'), sqlc.arg('forced_liquidation_final_equity'), sqlc.arg('total_r'), sqlc.arg('profit_factor'), sqlc.arg('max_dd_pct'), sqlc.arg('trades'), sqlc.arg('profitable'))
ON CONFLICT (context_id, window_index) DO UPDATE
SET from_ts = EXCLUDED.from_ts,
    to_ts = EXCLUDED.to_ts,
    realized_final_equity = EXCLUDED.realized_final_equity,
    marked_final_equity = EXCLUDED.marked_final_equity,
    forced_liquidation_final_equity = EXCLUDED.forced_liquidation_final_equity,
    total_r = EXCLUDED.total_r,
    profit_factor = EXCLUDED.profit_factor,
    max_dd_pct = EXCLUDED.max_dd_pct,
    trades = EXCLUDED.trades,
    profitable = EXCLUDED.profitable;

-- name: UpdatePortfolioContextForwardScore :exec
UPDATE portfolio_context_results
SET forward_score = sqlc.arg('forward_score')
WHERE context_id = sqlc.arg('context_id');

-- name: UpsertPortfolioSymbolForecast :exec
INSERT INTO portfolio_symbol_forecasts (
    run_id, ticker, historical_score, network_net_r,
    median_marginal_score, p20_marginal_score, marginal_positive_rate,
    marginal_samples, forward_score
)
VALUES (sqlc.arg('run_id'), sqlc.arg('ticker'), sqlc.arg('historical_score'), sqlc.arg('network_net_r'), sqlc.arg('median_marginal_score'), sqlc.arg('p20_marginal_score'), sqlc.arg('marginal_positive_rate'), sqlc.arg('marginal_samples'), sqlc.arg('forward_score'))
ON CONFLICT (run_id, ticker) DO UPDATE
SET historical_score = EXCLUDED.historical_score,
    network_net_r = EXCLUDED.network_net_r,
    median_marginal_score = EXCLUDED.median_marginal_score,
    p20_marginal_score = EXCLUDED.p20_marginal_score,
    marginal_positive_rate = EXCLUDED.marginal_positive_rate,
    marginal_samples = EXCLUDED.marginal_samples,
    forward_score = EXCLUDED.forward_score;

-- name: InsertPortfolioPairedMarginal :exec
INSERT INTO portfolio_paired_marginals (
    run_id, symbol, replaced_symbol, baseline_context_id, included_context_id,
    delta_score, delta_realized_equity, delta_marked_equity,
    delta_forced_equity, delta_total_r, delta_max_dd_pct
)
VALUES (sqlc.arg('run_id'), sqlc.arg('symbol'), sqlc.arg('replaced_symbol'), sqlc.arg('baseline_context_id'), sqlc.arg('included_context_id'), sqlc.arg('delta_score'), sqlc.arg('delta_realized_equity'), sqlc.arg('delta_marked_equity'), sqlc.arg('delta_forced_equity'), sqlc.arg('delta_total_r'), sqlc.arg('delta_max_dd_pct'))
ON CONFLICT (
    run_id, symbol, replaced_symbol, baseline_context_id, included_context_id
) DO UPDATE
SET delta_score = EXCLUDED.delta_score,
    delta_realized_equity = EXCLUDED.delta_realized_equity,
    delta_marked_equity = EXCLUDED.delta_marked_equity,
    delta_forced_equity = EXCLUDED.delta_forced_equity,
    delta_total_r = EXCLUDED.delta_total_r,
    delta_max_dd_pct = EXCLUDED.delta_max_dd_pct;

-- name: SelectBestPortfolioContextForRun :one
SELECT c.id, c.trade_symbols, c.signal_context_symbols, r.score, r.forward_score,
       r.realized_final_equity, r.marked_final_equity,
       r.forced_liquidation_final_equity, r.max_dd_pct
FROM portfolio_contexts c
JOIN portfolio_context_results r ON r.context_id = c.id
WHERE c.run_id = sqlc.arg('runID')
  AND c.source <> 'paired'
ORDER BY r.forward_score DESC, r.score DESC,
         r.forced_liquidation_final_equity DESC, c.fingerprint
LIMIT 1;

-- name: SelectBestDynamicPortfolioV5AsOf :one
SELECT c.id, c.trade_symbols, c.signal_context_symbols, r.forward_score,
       pr.id AS run_id
FROM portfolio_research_runs pr
JOIN portfolio_contexts c ON c.run_id = pr.id
JOIN portfolio_context_results r ON r.context_id = c.id
WHERE pr.status = 'completed'
  AND pr.timeframe = sqlc.arg(timeframe)
  AND pr.to_ts <= sqlc.arg(to_ts)
  AND c.source <> 'paired'
  AND r.forced_liquidation_final_equity > 100000
  AND r.max_dd_pct <= 20
  AND r.windows > 0
  AND r.profitable_windows * 5 >= r.windows * 3
ORDER BY pr.to_ts DESC, r.forward_score DESC, r.score DESC, c.fingerprint
LIMIT 1;

-- name: InsertDynamicPortfolioChallenger :one
INSERT INTO dynamic_portfolio_champions (
    run_id, context_id, timeframe, status, confirmations, score
)
SELECT sqlc.arg('run_id'), sqlc.arg('context_id'), timeframe, 'challenger', 1, sqlc.arg('score')
FROM portfolio_research_runs
WHERE id = sqlc.arg('run_id')
RETURNING id;

-- name: SelectLatestDynamicPortfolioChallenger :one
SELECT ch.id, ch.run_id, ch.context_id, ch.status, ch.confirmations, ch.score,
       ch.selected_at, ch.active_from, ch.active_to, c.fingerprint,
       c.trade_symbols, c.signal_context_symbols
FROM dynamic_portfolio_champions ch
JOIN portfolio_contexts c ON c.id = ch.context_id
WHERE ch.status = 'challenger'
  AND ch.timeframe = sqlc.arg('timeframe')
ORDER BY ch.selected_at DESC, ch.id DESC
LIMIT 1;

-- name: ConfirmDynamicPortfolioChallenger :exec
UPDATE dynamic_portfolio_champions
SET run_id = sqlc.arg(run_id),
    context_id = sqlc.arg(context_id),
    score = sqlc.arg(score),
    confirmations = confirmations + 1,
    status = CASE
        WHEN confirmations + 1 >= sqlc.arg(min_confirmations)
            THEN 'confirmed'
        ELSE status
    END
WHERE id = sqlc.arg(id);

-- name: RetireDynamicPortfolioChallengers :exec
UPDATE dynamic_portfolio_champions
SET status = 'retired', active_to = now()
WHERE status IN ('challenger', 'confirmed')
  AND timeframe = sqlc.arg('timeframe');

-- name: RetireActiveDynamicPortfolio :exec
UPDATE dynamic_portfolio_champions
SET status = 'retired', active_to = now()
WHERE status = 'active'
  AND timeframe = sqlc.arg('timeframe');

-- name: ActivateDynamicPortfolioChampion :exec
UPDATE dynamic_portfolio_champions
SET status = 'active', active_from = now(), active_to = NULL
WHERE id = sqlc.arg('id');

-- name: InsertDynamicPortfolioChangeNotification :exec
INSERT INTO dynamic_portfolio_change_notifications (
    champion_id, timeframe, added_symbols, removed_symbols, portfolio_symbols
)
VALUES (sqlc.arg('champion_id'), sqlc.arg('timeframe'), sqlc.arg('added_symbols'), sqlc.arg('removed_symbols'), sqlc.arg('portfolio_symbols'))
ON CONFLICT (champion_id) DO NOTHING;

-- name: SelectPendingDynamicPortfolioChangeNotification :one
SELECT id, champion_id, timeframe, added_symbols, removed_symbols,
       portfolio_symbols, created_at, attempts, last_error
FROM dynamic_portfolio_change_notifications
WHERE champion_id = sqlc.arg('championID')
  AND delivered_at IS NULL;

-- name: ListPendingDynamicPortfolioChangeNotificationChampionIDs :many
SELECT champion_id
FROM dynamic_portfolio_change_notifications
WHERE delivered_at IS NULL
ORDER BY created_at, id
LIMIT 25;

-- name: MarkDynamicPortfolioChangeNotificationDelivered :exec
UPDATE dynamic_portfolio_change_notifications
SET delivered_at = now(), attempts = attempts + 1, last_error = ''
WHERE id = sqlc.arg('id')
  AND delivered_at IS NULL;

-- name: RecordDynamicPortfolioChangeNotificationFailure :exec
UPDATE dynamic_portfolio_change_notifications
SET attempts = attempts + 1, last_error = sqlc.arg('last_error')
WHERE id = sqlc.arg('id')
  AND delivered_at IS NULL;

-- name: SelectActiveDynamicPortfolioV5 :one
SELECT ch.id AS champion_id, ch.run_id, ch.context_id, ch.score,
	   ch.confirmations, ch.selected_at, ch.active_from, pr.timeframe,
       c.trade_symbols, c.signal_context_symbols, c.fingerprint
FROM dynamic_portfolio_champions ch
JOIN portfolio_research_runs pr ON pr.id = ch.run_id
JOIN portfolio_contexts c ON c.id = ch.context_id
WHERE ch.status = 'active'
  AND ch.timeframe = sqlc.arg(timeframe)
  AND (ch.active_from IS NULL OR ch.active_from <= sqlc.arg(active_at))
  AND (ch.active_to IS NULL OR ch.active_to > sqlc.arg(active_at))
ORDER BY ch.active_from DESC NULLS LAST, ch.id DESC
LIMIT 1;

-- name: ListDynamicPortfolioV5History :many
SELECT ch.id AS champion_id, ch.run_id, ch.context_id,
       ch.active_from, ch.active_to,
       pr.timeframe, c.trade_symbols, c.signal_context_symbols,
       c.fingerprint
FROM dynamic_portfolio_champions ch
JOIN portfolio_research_runs pr ON pr.id = ch.run_id
JOIN portfolio_contexts c ON c.id = ch.context_id
WHERE ch.active_from IS NOT NULL
  AND pr.timeframe = sqlc.arg(timeframe)
  AND ch.active_from <= sqlc.arg(to_ts)
  AND (ch.active_to IS NULL OR ch.active_to > sqlc.arg(from_ts))
ORDER BY ch.active_from, ch.id;

-- name: SelectDynamicPortfolioChampionForActivation :one
SELECT ch.id AS champion_id, ch.run_id, ch.context_id,
       pr.timeframe, c.trade_symbols, c.signal_context_symbols,
       c.fingerprint
FROM dynamic_portfolio_champions ch
JOIN portfolio_research_runs pr ON pr.id = ch.run_id
JOIN portfolio_contexts c ON c.id = ch.context_id
WHERE ch.id = sqlc.arg('id');

-- name: AggregatePortfolioMarginalsForRun :many
SELECT symbol,
       COUNT(*)::bigint AS samples,
       AVG(delta_score)::double precision AS mean_delta_score,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY delta_score)::double precision
           AS median_delta_score,
       percentile_cont(0.2) WITHIN GROUP (ORDER BY delta_score)::double precision
           AS p20_delta_score,
       AVG((delta_score > 0)::int)::double precision AS positive_rate,
       stddev_pop(delta_score)::double precision AS score_dispersion,
       AVG(delta_forced_equity)::double precision AS mean_delta_forced_equity
FROM portfolio_paired_marginals
WHERE run_id = sqlc.arg('runID')
GROUP BY symbol
ORDER BY median_delta_score DESC, positive_rate DESC, symbol;

-- name: ListPortfolioCompanionEffects :many
SELECT symbol, replaced_symbol,
       COUNT(*)::bigint AS samples,
       AVG(delta_score)::double precision AS mean_delta_score,
       AVG(delta_forced_equity)::double precision AS mean_delta_forced_equity
FROM portfolio_paired_marginals
WHERE run_id = sqlc.arg('runID')
GROUP BY symbol, replaced_symbol
ORDER BY symbol, mean_delta_score DESC, replaced_symbol;

-- name: SelectLatestCompletedPortfolioResearchRun :one
SELECT id, timeframe, from_ts, to_ts, portfolio_size, trials,
       paired_swaps_per_symbol, deterministic_seed, research_mode, decision_at,
       min_improvement, promotion_decision, created_at, completed_at
FROM portfolio_research_runs
WHERE status = 'completed'
ORDER BY completed_at DESC NULLS LAST, id DESC
LIMIT 1;

-- name: SelectLatestCompletedPortfolioResearchRunForTimeframe :one
SELECT id, timeframe, from_ts, to_ts, portfolio_size, trials,
       paired_swaps_per_symbol, deterministic_seed, research_mode, decision_at,
       min_improvement, promotion_decision, created_at, completed_at
FROM portfolio_research_runs
WHERE status = 'completed'
  AND timeframe = sqlc.arg('timeframe')
ORDER BY completed_at DESC NULLS LAST, id DESC
LIMIT 1;

-- name: SelectCompletedPortfolioResearchRun :one
SELECT id, timeframe, from_ts, to_ts, portfolio_size, trials,
       paired_swaps_per_symbol, deterministic_seed, research_mode, decision_at,
       min_improvement, promotion_decision, created_at, completed_at
FROM portfolio_research_runs
WHERE id = sqlc.arg('id')
  AND status = 'completed';

-- name: ListDynamicPortfolioV5Contexts :many
SELECT c.id AS context_id, c.run_id, c.source, c.fingerprint,
       c.trade_symbols, c.signal_context_symbols,
       r.score, r.forward_score, r.realized_final_equity, r.marked_final_equity,
       r.forced_liquidation_final_equity, r.total_r, r.profit_factor,
       r.max_dd_pct, r.trades, r.profitable_windows, r.windows,
       COALESCE(ch.status, '')::text AS champion_status,
       COALESCE(ch.confirmations, 0)::int AS confirmations
FROM portfolio_contexts c
JOIN portfolio_context_results r ON r.context_id = c.id
LEFT JOIN dynamic_portfolio_champions ch ON ch.context_id = c.id
WHERE c.run_id = sqlc.arg('run_id')
  AND c.source <> 'paired'
ORDER BY
    CASE COALESCE(ch.status, '')
        WHEN 'active' THEN 0
        WHEN 'confirmed' THEN 1
        WHEN 'challenger' THEN 2
        ELSE 3
    END,
    r.forward_score DESC,
    r.score DESC,
    r.forced_liquidation_final_equity DESC,
    c.fingerprint
LIMIT sqlc.arg('limit');

-- name: ListDynamicPortfolioRevisionCandidates :many
WITH ranked AS (
    SELECT pr.id AS run_id, pr.timeframe, pr.from_ts AS evidence_from,
           pr.to_ts AS evidence_to, pr.decision_at,
           c.id AS context_id, c.trade_symbols, c.signal_context_symbols,
           c.fingerprint, r.forward_score, r.score,
           coverage.window_count, coverage.covered_days,
           row_number() OVER (
               PARTITION BY pr.id
               ORDER BY r.forward_score DESC, r.score DESC,
                        r.forced_liquidation_final_equity DESC, c.fingerprint
           ) AS run_rank
    FROM portfolio_research_runs pr
    JOIN portfolio_contexts c ON c.run_id = pr.id
    JOIN portfolio_context_results r ON r.context_id = c.id
    JOIN (
        SELECT context_id, COUNT(*)::int AS window_count,
               EXTRACT(EPOCH FROM (MAX(to_ts) - MIN(from_ts))) / 86400.0 AS covered_days
        FROM portfolio_context_window_results
        GROUP BY context_id
    ) coverage ON coverage.context_id = c.id
    WHERE pr.status = 'completed'
      AND pr.timeframe = sqlc.arg('timeframe')
      AND pr.to_ts <= pr.decision_at
      -- Paired contexts are complete fixed-size portfolios, not merely
      -- marginal rows. They must be eligible for timeline promotion when
      -- their own whole-portfolio robustness result passes the gates below.
      AND r.forced_liquidation_final_equity > 100000
      AND r.max_dd_pct <= 20
      AND r.windows > 0
      AND r.profitable_windows * 5 >= r.windows * 3
      -- A portfolio must have several persisted chronological windows spanning
      -- most of its research interval; one concentrated winning segment is not
      -- sufficient evidence for a dated DP decision.
      AND coverage.window_count >= CASE pr.timeframe
          WHEN '1m' THEN 3
          WHEN '5m' THEN 4
          WHEN '1h' THEN 6
          WHEN '1d' THEN 8
          ELSE 3
      END
      AND coverage.covered_days >= EXTRACT(EPOCH FROM (pr.to_ts - pr.from_ts)) / 86400.0 * 0.75
), best_per_decision AS (
    SELECT DISTINCT ON (decision_at)
           run_id, timeframe, evidence_from, evidence_to, decision_at,
           context_id, trade_symbols, signal_context_symbols, fingerprint,
           forward_score, score
    FROM ranked
    WHERE run_rank = 1
    ORDER BY decision_at, forward_score DESC, score DESC, run_id DESC
)
SELECT *
FROM best_per_decision
ORDER BY decision_at, run_id;

-- name: SelectActiveDynamicPortfolioRevision :one
SELECT id, timeframe, source_run_id, parent_revision_id, algorithm_version,
       status, fingerprint, decision_count, first_decision_at,
       last_decision_at, created_at
FROM dynamic_portfolio_revisions
WHERE timeframe = sqlc.arg('timeframe') AND status = 'active'
LIMIT 1;

-- name: SelectDynamicPortfolioRevision :one
SELECT id, timeframe, source_run_id, parent_revision_id, algorithm_version,
       status, fingerprint, decision_count, first_decision_at,
       last_decision_at, created_at
FROM dynamic_portfolio_revisions
WHERE id = sqlc.arg('id');

-- name: SupersedeActiveDynamicPortfolioRevision :exec
UPDATE dynamic_portfolio_revisions
SET status = 'superseded'
WHERE timeframe = sqlc.arg('timeframe') AND status = 'active';

-- name: ActivateDynamicPortfolioRevision :exec
WITH candidate AS (
    SELECT revision.id, revision.timeframe, revision.created_at
    FROM dynamic_portfolio_revisions revision
    WHERE revision.id = sqlc.arg(revision_id) AND revision.status = 'pending'
)
UPDATE dynamic_portfolio_revisions revision
SET status = CASE
    WHEN revision.id = candidate.id THEN 'active'
    ELSE 'superseded'
END
FROM candidate
WHERE revision.timeframe = candidate.timeframe
  AND (
      revision.id = candidate.id OR
      (revision.status IN ('active', 'pending') AND revision.created_at <= candidate.created_at)
  )
  AND NOT EXISTS (
      SELECT 1
      FROM dynamic_portfolio_revisions newer
      WHERE newer.timeframe = candidate.timeframe
        AND newer.status = 'active'
        AND newer.created_at > candidate.created_at
  );

-- name: InsertDynamicPortfolioRevision :one
INSERT INTO dynamic_portfolio_revisions (
    timeframe, source_run_id, parent_revision_id, fingerprint,
    decision_count, first_decision_at, last_decision_at
)
VALUES (sqlc.arg('timeframe'), sqlc.arg('source_run_id'), sqlc.arg('parent_revision_id'), sqlc.arg('fingerprint'), sqlc.arg('decision_count'), sqlc.arg('first_decision_at'), sqlc.arg('last_decision_at'))
RETURNING id;

-- name: InsertDynamicPortfolioRevisionPeriod :exec
INSERT INTO dynamic_portfolio_revision_periods (
    revision_id, source_run_id, context_id, effective_from, effective_to,
    evidence_from, evidence_to, selection_score, trade_symbols,
    context_symbols, fingerprint
)
VALUES (sqlc.arg('revision_id'), sqlc.arg('source_run_id'), sqlc.arg('context_id'), sqlc.arg('effective_from'), sqlc.arg('effective_to'), sqlc.arg('evidence_from'), sqlc.arg('evidence_to'), sqlc.arg('selection_score'), sqlc.arg('trade_symbols'), sqlc.arg('context_symbols'), sqlc.arg('fingerprint'));

-- name: ListLatestDynamicPortfolioRevisionHistory :many
SELECT p.id AS period_id, p.revision_id, p.source_run_id, p.context_id,
       p.effective_from, p.effective_to, r.timeframe,
       p.trade_symbols, p.context_symbols, p.fingerprint,
       p.evidence_from, p.evidence_to, p.selection_score
FROM dynamic_portfolio_revisions r
JOIN dynamic_portfolio_revision_periods p ON p.revision_id = r.id
WHERE r.timeframe = sqlc.arg(timeframe)
  AND r.status = 'active'
  AND p.effective_from <= sqlc.arg(to_ts)
  AND (p.effective_to IS NULL OR p.effective_to > sqlc.arg(from_ts))
ORDER BY p.effective_from, p.id;

-- name: ListDynamicPortfolioRevisionHistory :many
SELECT p.id AS period_id, p.revision_id, p.source_run_id, p.context_id,
       p.effective_from, p.effective_to, r.timeframe,
       p.trade_symbols, p.context_symbols, p.fingerprint,
       p.evidence_from, p.evidence_to, p.selection_score
FROM dynamic_portfolio_revisions r
JOIN dynamic_portfolio_revision_periods p ON p.revision_id = r.id
WHERE r.id = sqlc.arg(revision_id)
  AND r.timeframe = sqlc.arg(timeframe)
  AND p.effective_from <= sqlc.arg(to_ts)
  AND (p.effective_to IS NULL OR p.effective_to > sqlc.arg(from_ts))
ORDER BY p.effective_from, p.id;

-- name: ListRecentDynamicPortfolioRevisionPeriods :many
WITH recent AS (
    SELECT revision.id, revision.created_at
    FROM dynamic_portfolio_revisions revision
    WHERE revision.timeframe = sqlc.arg(timeframe)
    ORDER BY revision.created_at DESC, revision.id DESC
    LIMIT sqlc.arg(revision_limit)
)
SELECT p.id AS period_id, p.revision_id, p.source_run_id, p.context_id,
       p.effective_from, p.effective_to, r.timeframe,
       p.trade_symbols, p.context_symbols, p.fingerprint,
       p.evidence_from, p.evidence_to, p.selection_score
FROM recent
JOIN dynamic_portfolio_revisions r ON r.id = recent.id
JOIN dynamic_portfolio_revision_periods p ON p.revision_id = recent.id
ORDER BY recent.created_at DESC, p.effective_from, p.id;

-- name: ListDynamicPortfolioRevisions :many
SELECT r.id, r.timeframe, r.source_run_id, r.parent_revision_id,
       r.algorithm_version, r.status, r.fingerprint, r.decision_count,
       r.first_decision_at, r.last_decision_at, r.created_at,
       rr.evaluator_version, rr.from_ts, rr.to_ts, rr.starting_equity,
       rr.realized_final_equity, rr.marked_final_equity,
       rr.forced_liquidation_final_equity, rr.open_pnl, rr.total_r,
       rr.profit_factor, rr.sharpe, rr.sharpe_period, rr.max_dd_pct,
       rr.max_dd_dollars, rr.trades, rr.wins, rr.losses, rr.total_fees,
       rr.data_watermark, rr.created_at AS evaluated_at
FROM dynamic_portfolio_revisions r
LEFT JOIN dynamic_portfolio_revision_results rr ON rr.revision_id = r.id
WHERE r.timeframe = sqlc.arg('timeframe')
ORDER BY r.created_at DESC, r.id DESC
LIMIT sqlc.arg('limit');

-- name: UpsertDynamicPortfolioRevisionResult :exec
INSERT INTO dynamic_portfolio_revision_results (
    revision_id, evaluator_version, from_ts, to_ts, starting_equity,
    realized_final_equity, marked_final_equity,
    forced_liquidation_final_equity, open_pnl, total_r, profit_factor,
    sharpe, sharpe_period, max_dd_pct, max_dd_dollars, trades, wins,
    losses, total_fees, data_watermark, execution_profile
)
VALUES (
    sqlc.arg('revision_id'), sqlc.arg('evaluator_version'), sqlc.arg('from_ts'), sqlc.arg('to_ts'), sqlc.arg('starting_equity'), sqlc.arg('realized_final_equity'), sqlc.arg('marked_final_equity'), sqlc.arg('forced_liquidation_final_equity'), sqlc.arg('open_pnl'), sqlc.arg('total_r'), sqlc.arg('profit_factor'),
    sqlc.arg('sharpe'), sqlc.arg('sharpe_period'), sqlc.arg('max_dd_pct'), sqlc.arg('max_dd_dollars'), sqlc.arg('trades'), sqlc.arg('wins'), sqlc.arg('losses'), sqlc.arg('total_fees'), sqlc.arg('data_watermark'), sqlc.arg('execution_profile')
)
ON CONFLICT (revision_id) DO UPDATE
SET evaluator_version = EXCLUDED.evaluator_version,
    from_ts = EXCLUDED.from_ts,
    to_ts = EXCLUDED.to_ts,
    starting_equity = EXCLUDED.starting_equity,
    realized_final_equity = EXCLUDED.realized_final_equity,
    marked_final_equity = EXCLUDED.marked_final_equity,
    forced_liquidation_final_equity = EXCLUDED.forced_liquidation_final_equity,
    open_pnl = EXCLUDED.open_pnl,
    total_r = EXCLUDED.total_r,
    profit_factor = EXCLUDED.profit_factor,
    sharpe = EXCLUDED.sharpe,
    sharpe_period = EXCLUDED.sharpe_period,
    max_dd_pct = EXCLUDED.max_dd_pct,
    max_dd_dollars = EXCLUDED.max_dd_dollars,
    trades = EXCLUDED.trades,
    wins = EXCLUDED.wins,
    losses = EXCLUDED.losses,
    total_fees = EXCLUDED.total_fees,
    data_watermark = EXCLUDED.data_watermark,
    execution_profile = EXCLUDED.execution_profile,
    created_at = now();

-- name: ListDynamicPortfolioV5Symbols :many
WITH marginal AS (
    SELECT symbol,
           COUNT(*)::bigint AS samples,
           AVG(delta_score)::double precision AS mean_delta_score,
           percentile_cont(0.5) WITHIN GROUP (ORDER BY delta_score)::double precision
               AS median_delta_score,
           percentile_cont(0.2) WITHIN GROUP (ORDER BY delta_score)::double precision
               AS p20_delta_score,
           AVG((delta_score > 0)::int)::double precision AS positive_rate,
           stddev_pop(delta_score)::double precision AS score_dispersion,
           AVG(delta_forced_equity)::double precision AS mean_delta_forced_equity
    FROM portfolio_paired_marginals
    WHERE run_id = (
        SELECT run_id FROM portfolio_contexts WHERE id = sqlc.arg('contextID')
    )
    GROUP BY symbol
)
SELECT s.context_id, s.ticker, COALESCE(t.title, '')::text AS company_name,
       s.trades, s.wins, s.losses, s.win_rate, s.realized_pnl,
       s.unrealized_pnl, s.total_r, s.avg_r,
       COALESCE(m.samples, 0)::bigint AS marginal_samples,
       COALESCE(m.mean_delta_score, 0)::double precision AS mean_delta_score,
       COALESCE(m.median_delta_score, 0)::double precision AS median_delta_score,
       COALESCE(m.p20_delta_score, 0)::double precision AS p20_delta_score,
       COALESCE(m.positive_rate, 0)::double precision AS positive_rate,
       COALESCE(m.score_dispersion, 0)::double precision AS score_dispersion,
       COALESCE(m.mean_delta_forced_equity, 0)::double precision
           AS mean_delta_forced_equity
FROM portfolio_symbol_results s
LEFT JOIN LATERAL (
    SELECT title
    FROM sec_tickers
    WHERE ticker = s.ticker
    ORDER BY fetched_at DESC, cik
    LIMIT 1
) t ON true
LEFT JOIN marginal m ON m.symbol = s.ticker
WHERE s.context_id = sqlc.arg('contextID')
ORDER BY s.realized_pnl DESC, s.total_r DESC, s.ticker;

-- name: ListDynamicPortfolioV5Champions :many
SELECT f.run_id, f.ticker, COALESCE(t.title, '')::text AS company_name,
       f.forward_score, f.historical_score, f.network_net_r,
       f.median_marginal_score, f.p20_marginal_score,
       f.marginal_positive_rate, f.marginal_samples,
       COALESCE(AVG(s.realized_pnl), 0)::double precision AS context_realized_pnl,
       COALESCE(AVG(s.unrealized_pnl), 0)::double precision AS context_unrealized_pnl,
       COALESCE(AVG(s.total_r), 0)::double precision AS context_total_r,
       COALESCE(AVG(s.trades), 0)::double precision AS context_trades
FROM portfolio_symbol_forecasts f
LEFT JOIN LATERAL (
    SELECT title
    FROM sec_tickers
    WHERE ticker = f.ticker
    ORDER BY fetched_at DESC, cik
    LIMIT 1
) t ON true
LEFT JOIN portfolio_contexts c ON c.run_id = f.run_id
LEFT JOIN portfolio_symbol_results s
       ON s.context_id = c.id AND s.ticker = f.ticker
WHERE f.run_id = sqlc.arg('run_id')
  AND f.forward_score > 0
GROUP BY f.run_id, f.ticker, t.title, f.forward_score, f.historical_score,
         f.network_net_r, f.median_marginal_score, f.p20_marginal_score,
         f.marginal_positive_rate, f.marginal_samples
ORDER BY f.forward_score DESC, f.marginal_positive_rate DESC, f.ticker
LIMIT sqlc.arg('limit');
-- name: CurrentMacroRegimeAverages :one
SELECT COALESCE(AVG(soxx_20d_pct), 0)::float8 AS soxx, COALESCE(AVG(vixy_20d_pct), 0)::float8 AS vixy
FROM ticker_backtest_results WHERE strategy LIKE '%_3m_now' AND generation = 1;

-- name: NetworkScoresForFilledPortfolioPeriods :many
WITH fill_period AS (
    SELECT f.symbol, f.filled_at, period.period_id, period.context_id, period.evidence_to
    FROM (SELECT unnest(sqlc.arg(symbols)::text[]) AS symbol,
                 unnest(sqlc.arg(filled_ats)::timestamptz[]) AS filled_at) AS f
    LEFT JOIN LATERAL (
        SELECT p.id AS period_id, p.context_id, p.evidence_to
        FROM dynamic_portfolio_revision_periods p
        JOIN dynamic_portfolio_revisions revision ON revision.id = p.revision_id
        WHERE revision.timeframe = sqlc.arg(timeframe)
          AND p.effective_from <= f.filled_at
          AND (p.effective_to IS NULL OR p.effective_to > f.filled_at)
        ORDER BY p.effective_from DESC, p.id DESC LIMIT 1
    ) period ON true
), known_periods AS (
    SELECT symbol, COUNT(DISTINCT period_id) AS period_count, BOOL_AND(period_id IS NOT NULL) AS fully_known
    FROM fill_period GROUP BY symbol
), scored_period AS (
    SELECT DISTINCT ON (f.symbol) f.symbol, r.network_score
    FROM fill_period f
    JOIN known_periods known ON known.symbol = f.symbol AND known.period_count = 1 AND known.fully_known
    JOIN ticker_backtest_results r ON r.ticker = f.symbol AND r.to_date <= f.evidence_to::date
    JOIN ticker_backtest_contexts ticker_context ON ticker_context.id = r.portfolio_context_id
    JOIN portfolio_contexts portfolio_context ON portfolio_context.id = f.context_id
      AND ticker_context.trade_symbols = portfolio_context.trade_symbols
      AND ticker_context.signal_context_symbols = portfolio_context.signal_context_symbols
    WHERE r.strategy LIKE sqlc.arg(strategy_pattern) AND r.strategy NOT LIKE '%_3m_now'
      AND r.timeframe = sqlc.arg(timeframe)
    ORDER BY f.symbol, r.to_date DESC, r.from_date DESC, r.id DESC
)
SELECT symbol::text AS symbol, network_score::float8 AS network_score FROM scored_period;

-- name: WatchlistPortfolioRows :many
WITH scoped AS (
    SELECT r.* FROM ticker_backtest_results r
    WHERE r.ticker = ANY(sqlc.arg(tickers)::text[]) AND r.timeframe = sqlc.arg(timeframe)
      AND r.from_date >= sqlc.arg(from_date)::date AND r.to_date <= sqlc.arg(to_date)::date AND r.trades >= 5
      AND r.generation = (SELECT generation FROM ticker_backtest_results GROUP BY generation ORDER BY COUNT(*) DESC, generation DESC LIMIT 1)
), latest AS (
    SELECT DISTINCT ON (r.ticker) r.* FROM scoped r
    ORDER BY r.ticker, r.to_date DESC, r.from_date DESC, r.generation DESC, r.id DESC
), regime AS (
    SELECT r.ticker,
      SUM(CASE WHEN r.soxx_20d_pct > 0.05 THEN r.total_r ELSE 0 END)::float8 AS bull_r,
      SUM(CASE WHEN r.soxx_20d_pct < -0.05 THEN r.total_r ELSE 0 END)::float8 AS bear_r,
      SUM(CASE WHEN r.soxx_20d_pct BETWEEN -0.05 AND 0.05 THEN r.total_r ELSE 0 END)::float8 AS chop_r
    FROM scoped r GROUP BY r.ticker
)
SELECT r.ticker, COALESCE(s.title, '')::text AS company,
 r.total_r::float8, r.win_rate::float8, r.sharpe::float8, r.max_dd_pct::float8,
 r.final_equity::float8, r.trades::float8, r.momentum_ema::float8, r.momentum_ratio::float8,
 r.soxx_20d_pct::float8, r.vixy_20d_pct::float8, r.from_date, r.to_date,
 r.unrealized_pnl::float8, r.expect_r::float8, r.semi_affinity_score, r.semi_affinity_events,
 r.semi_affinity_smh_rate, r.semi_affinity_soxx_rate, r.semi_affinity_soxq_rate,
 r.semi_affinity_xsd_rate, r.semi_affinity_psi_rate, r.semi_affinity_ftxl_rate,
 r.semi_leadership_trend_pct, g.bull_r, g.bear_r, g.chop_r
FROM latest r JOIN regime g USING (ticker) LEFT JOIN sec_tickers s ON s.ticker = r.ticker ORDER BY r.ticker;

-- name: SelectPortfolioSearchCandidates :many
WITH selected_generation AS (
    SELECT generation
    FROM ticker_backtest_results AS results
    WHERE results.timeframe = sqlc.arg(timeframe)
      AND results.strategy LIKE 'strategy_confluence%'
    GROUP BY generation
    HAVING MAX(to_date) <= sqlc.arg(as_of)::date
    ORDER BY (COUNT(DISTINCT ticker) >= 2 AND COUNT(*) >= 4) DESC,
             generation DESC, COUNT(*) DESC
    LIMIT 1
), evidence AS (
    SELECT ticker,
           (COALESCE(SUM(network_net_r), 0) +
            GREATEST(SUM(total_r), 0) +
            LN(1 + SUM(trades)))::double precision AS score
    FROM ticker_backtest_results AS results
    WHERE results.generation = (SELECT generation FROM selected_generation)
      AND results.strategy LIKE 'strategy_confluence%'
      AND timeframe = sqlc.arg(timeframe)
    GROUP BY ticker
    UNION ALL
    SELECT f.ticker, MAX(f.forward_score)::double precision AS score
    FROM portfolio_symbol_forecasts f
    JOIN portfolio_research_runs pr ON pr.id = f.run_id
    WHERE pr.status = 'completed'
      AND pr.timeframe = sqlc.arg(timeframe)
      AND pr.to_ts <= sqlc.arg(as_of)
      AND f.forward_score > 0
    GROUP BY f.ticker
)
SELECT ticker
FROM evidence
GROUP BY ticker
ORDER BY MAX(score) DESC, ticker
LIMIT sqlc.arg(result_limit);

-- name: ListPortfolioSearchMarketUniverse :many
SELECT ticker
FROM sec_tickers
WHERE ticker ~ '^[A-Z][A-Z0-9.-]{0,9}$'
ORDER BY ticker;
