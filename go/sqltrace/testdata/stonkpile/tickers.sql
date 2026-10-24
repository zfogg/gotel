-- name: UpsertSECTicker :exec
INSERT INTO sec_tickers (cik, ticker, title, fetched_at)
VALUES (sqlc.arg('cik'), sqlc.arg('ticker'), sqlc.arg('title'), now())
ON CONFLICT (cik, ticker) DO UPDATE
SET title      = CASE WHEN EXCLUDED.title != '' THEN EXCLUDED.title ELSE sec_tickers.title END,
    fetched_at = EXCLUDED.fetched_at;

-- name: ListSECTickers :many
SELECT cik, ticker, title, fetched_at, created_at
FROM sec_tickers
ORDER BY ticker;

-- name: GetSECTicker :one
SELECT cik, ticker, title, fetched_at, created_at
FROM sec_tickers
WHERE ticker = sqlc.arg('ticker');

-- name: DeleteSECTickersByCIK :exec
DELETE FROM sec_tickers WHERE cik = sqlc.arg('cik');

-- name: UpsertTickerBacktestContext :one
INSERT INTO ticker_backtest_contexts (
    fingerprint, generation, timeframe, trade_symbols, signal_context_symbols
)
VALUES (sqlc.arg('fingerprint'), sqlc.arg('generation'), sqlc.arg('timeframe'), sqlc.arg('trade_symbols'), sqlc.arg('signal_context_symbols'))
ON CONFLICT (fingerprint) DO UPDATE
SET generation = EXCLUDED.generation,
    timeframe = EXCLUDED.timeframe,
    trade_symbols = EXCLUDED.trade_symbols,
    signal_context_symbols = EXCLUDED.signal_context_symbols
RETURNING id;

-- name: UpsertTickerBacktestResult :exec
INSERT INTO ticker_backtest_results
    (cik, ticker, strategy, timeframe, from_date, to_date,
     trades, win_rate, expect_r, profit_factor, total_r, sharpe,
     max_dd_pct, max_dd_dollars, final_equity, generation,
     soxx_20d_pct, vixy_20d_pct, momentum_ema, momentum_ratio, unrealized_pnl,
     network_score, network_profit_trades, network_profit_r, network_loss_r,
     network_net_r, network_profit_pnl, network_net_pnl,
     semi_affinity_score, semi_affinity_events,
     semi_affinity_smh_rate, semi_affinity_soxx_rate,
     semi_affinity_soxq_rate, semi_affinity_xsd_rate,
     semi_affinity_psi_rate, semi_affinity_ftxl_rate,
     semi_leadership_trend_pct, portfolio_context_id, updated_at)
VALUES (sqlc.arg('cik'), sqlc.arg('ticker'), sqlc.arg('strategy'), sqlc.arg('timeframe'), sqlc.arg('from_date'), sqlc.arg('to_date'), sqlc.arg('trades'), sqlc.arg('win_rate'), sqlc.arg('expect_r'), sqlc.arg('profit_factor'), sqlc.arg('total_r'), sqlc.arg('sharpe'), sqlc.arg('max_dd_pct'), sqlc.arg('max_dd_dollars'), sqlc.arg('final_equity'), sqlc.arg('generation'), sqlc.arg('soxx_20d_pct'), sqlc.arg('vixy_20d_pct'), sqlc.arg('momentum_ema'), sqlc.arg('momentum_ratio'), sqlc.arg('unrealized_pnl'), sqlc.arg('network_score'), sqlc.arg('network_profit_trades'), sqlc.arg('network_profit_r'), sqlc.arg('network_loss_r'), sqlc.arg('network_net_r'), sqlc.arg('network_profit_pnl'), sqlc.arg('network_net_pnl'), sqlc.arg('semi_affinity_score'), sqlc.arg('semi_affinity_events'), sqlc.arg('semi_affinity_smh_rate'), sqlc.arg('semi_affinity_soxx_rate'), sqlc.arg('semi_affinity_soxq_rate'), sqlc.arg('semi_affinity_xsd_rate'), sqlc.arg('semi_affinity_psi_rate'), sqlc.arg('semi_affinity_ftxl_rate'), sqlc.arg('semi_leadership_trend_pct'), sqlc.arg('portfolio_context_id'), now())
ON CONFLICT (
    ticker, strategy, timeframe, from_date, to_date, generation,
    portfolio_context_id
) DO UPDATE
SET trades          = EXCLUDED.trades,
    win_rate        = EXCLUDED.win_rate,
    expect_r        = EXCLUDED.expect_r,
    profit_factor   = EXCLUDED.profit_factor,
    total_r         = EXCLUDED.total_r,
    sharpe          = EXCLUDED.sharpe,
    max_dd_pct      = EXCLUDED.max_dd_pct,
    max_dd_dollars  = EXCLUDED.max_dd_dollars,
    final_equity    = EXCLUDED.final_equity,
    soxx_20d_pct    = EXCLUDED.soxx_20d_pct,
    vixy_20d_pct    = EXCLUDED.vixy_20d_pct,
    momentum_ema    = EXCLUDED.momentum_ema,
    momentum_ratio  = EXCLUDED.momentum_ratio,
    unrealized_pnl  = EXCLUDED.unrealized_pnl,
    network_score         = EXCLUDED.network_score,
    network_profit_trades = EXCLUDED.network_profit_trades,
    network_profit_r      = EXCLUDED.network_profit_r,
    network_loss_r        = EXCLUDED.network_loss_r,
    network_net_r         = EXCLUDED.network_net_r,
    network_profit_pnl    = EXCLUDED.network_profit_pnl,
    network_net_pnl       = EXCLUDED.network_net_pnl,
    semi_affinity_score     = EXCLUDED.semi_affinity_score,
    semi_affinity_events    = EXCLUDED.semi_affinity_events,
    semi_affinity_smh_rate  = EXCLUDED.semi_affinity_smh_rate,
    semi_affinity_soxx_rate = EXCLUDED.semi_affinity_soxx_rate,
    semi_affinity_soxq_rate = EXCLUDED.semi_affinity_soxq_rate,
    semi_affinity_xsd_rate  = EXCLUDED.semi_affinity_xsd_rate,
    semi_affinity_psi_rate  = EXCLUDED.semi_affinity_psi_rate,
    semi_affinity_ftxl_rate = EXCLUDED.semi_affinity_ftxl_rate,
    semi_leadership_trend_pct = EXCLUDED.semi_leadership_trend_pct,
    portfolio_context_id = EXCLUDED.portfolio_context_id,
    updated_at      = EXCLUDED.updated_at;

-- name: UpdateTickerBacktestNetworkScore :exec
UPDATE ticker_backtest_results
SET network_score = sqlc.arg('network_score'), updated_at = now()
WHERE ticker = sqlc.arg('ticker') AND strategy = sqlc.arg('strategy') AND timeframe = sqlc.arg('timeframe') AND from_date = sqlc.arg('from_date') AND to_date = sqlc.arg('to_date') AND generation = sqlc.arg('generation');

-- name: ListTickerBacktestResults :many
SELECT r.id, r.cik, r.ticker, r.strategy, r.timeframe, r.from_date, r.to_date,
       r.trades, r.win_rate, r.expect_r, r.profit_factor, r.total_r, r.sharpe,
       r.max_dd_pct, r.max_dd_dollars, r.final_equity, r.generation,
       r.soxx_20d_pct, r.vixy_20d_pct,
       r.momentum_ema, r.momentum_ratio, r.unrealized_pnl, r.network_score,
       r.network_profit_trades, r.network_profit_r, r.network_loss_r,
       r.network_net_r, r.network_profit_pnl, r.network_net_pnl,
       r.semi_affinity_score, r.semi_affinity_events,
       r.semi_affinity_smh_rate, r.semi_affinity_soxx_rate,
       r.semi_affinity_soxq_rate, r.semi_affinity_xsd_rate,
       r.semi_affinity_psi_rate, r.semi_affinity_ftxl_rate,
       r.semi_leadership_trend_pct,
       COALESCE(r.portfolio_context_id, 0)::bigint AS portfolio_context_id,
       COALESCE(pc.trade_symbols, '{}')::text[] AS portfolio_symbols,
       COALESCE(pc.signal_context_symbols, '{}')::text[] AS signal_context_symbols,
       r.created_at, r.updated_at,
       t.title AS company_name
FROM ticker_backtest_results r
LEFT JOIN sec_tickers t ON t.cik = r.cik AND t.ticker = r.ticker
LEFT JOIN ticker_backtest_contexts pc ON pc.id = r.portfolio_context_id
WHERE r.generation = (SELECT generation FROM ticker_backtest_results GROUP BY generation ORDER BY COUNT(*) DESC LIMIT 1)
ORDER BY r.total_r DESC;

-- name: GetCurrentGeneration :one
SELECT COALESCE(MAX(generation), 0)::int AS generation
FROM ticker_backtest_results;

-- name: CountTickerBacktestResultsForGeneration :one
SELECT COUNT(*) FROM ticker_backtest_results
WHERE generation = sqlc.arg('generation');

-- name: SelectPriorWindowMomentum :many
WITH selected_generation AS (
    SELECT r.generation
    FROM ticker_backtest_results r
    WHERE r.strategy LIKE sqlc.narg('strategy')
      AND r.timeframe = sqlc.narg('timeframe')
    GROUP BY r.generation
    -- Prefer the newest coherent cross-section. Tiny ad-hoc generations are
    -- diagnostics, not a market-wide selector; the historical generation with
    -- the most rows must not permanently eclipse every newer weekly run.
    ORDER BY (COUNT(DISTINCT r.ticker) >= 2 AND COUNT(*) >= 4) DESC,
             r.generation DESC,
             COUNT(*) DESC
    LIMIT 1
)
SELECT DISTINCT ON (r.ticker)
       r.ticker,
       COALESCE(r.momentum_ema, 0)::float8 AS momentum_ema,
       COALESCE(r.momentum_ratio, 0)::float8 AS momentum_ratio,
       r.trades::float8 AS trades
FROM ticker_backtest_results r
JOIN selected_generation g USING (generation)
WHERE r.strategy LIKE sqlc.narg('strategy')
  AND r.strategy NOT LIKE '%_3m_now'
  AND r.timeframe = sqlc.narg('timeframe')
  AND r.to_date <= sqlc.narg('from_date')
  AND r.ticker = ANY(sqlc.narg('symbols')::text[])
ORDER BY r.ticker, r.to_date DESC, r.from_date DESC, r.id DESC;

-- name: ListUnprocessedTickers :many
SELECT s.ticker, s.cik, s.title
FROM sec_tickers s
WHERE s.ticker ~ '^[A-Z]{1,5}$'
  AND (s.last_ingest_failed_at IS NULL OR s.last_ingest_failed_at < NOW() - INTERVAL '30 days')
  AND NOT EXISTS (
    SELECT 1 FROM ticker_backtest_results r
    WHERE r.ticker = s.ticker AND r.generation = sqlc.arg('generation')
  )
ORDER BY random()
LIMIT sqlc.arg('limit');

-- name: MarkTickerIngestFailed :exec
UPDATE sec_tickers SET last_ingest_failed_at = NOW() WHERE ticker = sqlc.arg('ticker');

-- name: CountSECTickers :one
SELECT COUNT(*) FROM sec_tickers;

-- name: CountTickerBacktestResults :one
SELECT COUNT(*) FROM ticker_backtest_results;

-- name: ListTickerBacktestResultsNeedingMomentum :many
SELECT r.id, r.cik, r.ticker, r.strategy, r.timeframe,
       r.from_date, r.to_date, r.generation,
       r.trades, t.title AS company_name
FROM ticker_backtest_results r
JOIN sec_tickers t ON t.cik = r.cik AND t.ticker = r.ticker
WHERE r.momentum_ema = 0 AND r.momentum_ratio = 0 AND r.trades > 0
ORDER BY r.ticker, r.from_date
LIMIT sqlc.arg('limit');

-- name: SelectDynamicPortfolio :many
-- Composite score across all quarterly backtest windows (3m, walking back 2yr).
-- Only symbols with positive total_r (net profitable) across qualifying windows
-- are included. minWindows = 2 ensures multi-window consistency.
-- When regimeGate is true, windows from bearish SOXX regimes are downweighted
-- so tickers that only profited during bull runs don't dominate the ranking.
SELECT r.ticker, COALESCE(MAX(s.title), '')::text AS company_name,
       ROUND(SUM(r.total_r))::float8 AS total_r,
       AVG(r.win_rate) AS win_rate,
       AVG(r.sharpe) AS sharpe,
       MAX(r.max_dd_pct) AS max_dd_pct,
       ROUND(SUM(r.final_equity))::float8 AS final_equity,
       SUM(r.trades)::bigint AS trades,
       ROUND(SUM(r.total_r * r.win_rate * CASE
           WHEN @regime_gate::bool = false THEN 1.0
           WHEN r.soxx_20d_pct IS NULL THEN 0.7
           WHEN r.soxx_20d_pct < -0.05 THEN 0.3
           WHEN r.soxx_20d_pct < 0 THEN 0.7
           ELSE 1.0
       END))::float8 AS score,
       COUNT(*)::bigint AS windows
FROM ticker_backtest_results r
LEFT JOIN sec_tickers s ON s.ticker = r.ticker
WHERE r.generation = (SELECT generation FROM ticker_backtest_results GROUP BY generation ORDER BY COUNT(*) DESC LIMIT 1)
  AND r.strategy LIKE @strategy
  AND r.timeframe = @timeframe
  AND r.trades >= @trades
GROUP BY r.ticker
HAVING COUNT(*) >= @min_windows::int AND SUM(r.total_r) > 0
ORDER BY score DESC
LIMIT @max_results;

-- name: SelectDynamicPortfolioDetailed :many
-- Extended portfolio query with regime context, momentum, and time-range filtering.
-- Returns per-ticker aggregate stats plus regime breakdown (bull/bear/chop R)
-- so the CLI can show which regimes a symbol prints in.
-- Note: momentum filter (EMAd and MOM R > 0) was removed 2026-07-27 after
-- psql analysis showed momentum destroys value on 100% of equity symbols
-- (avg R = -0.491, 16.5% win rate). Mean reversion (avg R = +0.462, 64.4% win
-- rate) is the correct leading indicator for early-capture portfolio selection.
SELECT r.ticker, COALESCE(MAX(s.title), '')::text AS company_name,
       SUM(r.total_r)::double precision AS total_r,
       AVG(r.win_rate) AS win_rate,
       AVG(r.sharpe) AS sharpe,
       MAX(r.max_dd_pct)::double precision AS max_dd_pct,
       SUM(r.final_equity)::double precision AS final_equity,
       SUM(r.trades) AS trades,
       -- Momentum (averaged across windows)
       COALESCE(AVG(r.momentum_ema), 0)::double precision AS avg_momentum_ema,
       COALESCE(AVG(r.momentum_ratio), 0)::double precision AS avg_momentum_ratio,
       -- Regime context (averaged across windows)
       COALESCE(AVG(r.soxx_20d_pct), 0)::double precision AS avg_soxx_trend,
       COALESCE(AVG(r.vixy_20d_pct), 0)::double precision AS avg_vixy_trend,
       COALESCE(SUM(r.network_score), 0)::double precision AS network_score,
       COALESCE(SUM(r.network_profit_trades), 0)::bigint AS network_profit_trades,
       COALESCE(SUM(r.network_profit_r), 0)::double precision AS network_profit_r,
       COALESCE(SUM(r.network_loss_r), 0)::double precision AS network_loss_r,
       COALESCE(SUM(r.network_net_r), 0)::double precision AS network_net_r,
       COALESCE(SUM(r.network_profit_pnl), 0)::double precision AS network_profit_pnl,
       COALESCE(SUM(r.network_net_pnl), 0)::double precision AS network_net_pnl,
       -- Score with regime gating plus causal contribution to other symbols.
       (SUM(r.total_r * r.win_rate * CASE
           WHEN @regime_gate::bool = false THEN 1.0
           WHEN r.soxx_20d_pct IS NULL THEN 0.7
           WHEN r.soxx_20d_pct < -0.05 THEN 0.3
           WHEN r.soxx_20d_pct < 0 THEN 0.7
           ELSE 1.0
       END) + COALESCE(SUM(r.network_net_r), 0))::double precision AS score,
       COUNT(*) AS windows,
       -- Per-regime R breakdown
       SUM(CASE WHEN r.soxx_20d_pct > 0.05 THEN r.total_r ELSE 0 END)::double precision AS bull_r,
       SUM(CASE WHEN r.soxx_20d_pct < -0.05 THEN r.total_r ELSE 0 END)::double precision AS bear_r,
       SUM(CASE WHEN r.soxx_20d_pct >= -0.05 AND r.soxx_20d_pct <= 0.05 THEN r.total_r ELSE 0 END)::double precision AS chop_r,
       -- Window date range
       MIN(r.from_date)::text AS earliest_window,
       MAX(r.to_date)::text AS latest_window,
       SUM(r.final_equity - 100000.0)::double precision AS realized_pnl,
       SUM(r.unrealized_pnl)::double precision AS unrealized_pnl,
       SUM(r.final_equity + r.unrealized_pnl - 100000.0)::double precision AS marked_balance
FROM ticker_backtest_results r
LEFT JOIN sec_tickers s ON s.ticker = r.ticker
WHERE r.generation = (SELECT generation FROM ticker_backtest_results GROUP BY generation ORDER BY COUNT(*) DESC LIMIT 1)
  AND r.strategy LIKE @strategy
  AND r.timeframe = @timeframe
  AND r.trades >= @trades
  AND r.from_date >= @from_date
  AND r.to_date <= @to_date
GROUP BY r.ticker
HAVING COUNT(*) >= @min_windows::int
   AND (SUM(r.total_r) > 0 OR COALESCE(SUM(r.network_net_r), 0) > 0)
ORDER BY score DESC
LIMIT @max_results;

-- name: SelectDynamicPortfolioAsOf :many
-- Same as SelectDynamicPortfolio but filters by to_date <= @as_of_date instead
-- of using the latest generation. This allows walk-forward backtests to query
-- what the dynamic portfolio WOULD have been at any historical date without
-- lookahead bias. Each window in ticker_backtest_results represents a specific
-- 3-month period ending at to_date, so to_date <= cutoff gives us only windows
-- that would have been known at that cutoff.
SELECT r.ticker, COALESCE(MAX(s.title), '')::text AS company_name,
       ROUND(SUM(r.total_r))::float8 AS total_r,
       AVG(r.win_rate) AS win_rate,
       AVG(r.sharpe) AS sharpe,
       MAX(r.max_dd_pct) AS max_dd_pct,
       ROUND(SUM(r.final_equity))::float8 AS final_equity,
       SUM(r.trades)::bigint AS trades,
       ROUND(SUM(r.total_r * r.win_rate * CASE
           WHEN @regime_gate::bool = false THEN 1.0
           WHEN r.soxx_20d_pct IS NULL THEN 0.7
           WHEN r.soxx_20d_pct < -0.05 THEN 0.3
           WHEN r.soxx_20d_pct < 0 THEN 0.7
           ELSE 1.0
       END))::float8 AS score,
       COUNT(*)::bigint AS windows
FROM ticker_backtest_results r
LEFT JOIN sec_tickers s ON s.ticker = r.ticker
WHERE r.to_date <= @as_of_date::date
  AND r.strategy LIKE @strategy
  AND r.timeframe = @timeframe
  AND r.trades >= @trades
GROUP BY r.ticker
HAVING COUNT(*) >= @min_windows::int AND SUM(r.total_r) > 0
ORDER BY score DESC
LIMIT @max_results;

-- name: SelectDynamicPortfolioAsOfDetailed :many
-- Extended version of SelectDynamicPortfolioAsOf with regime/momentum context.
-- Momentum filter intentionally omitted (destroys value, proven by 136/136
-- equity symbols having negative momentum expectancy). Mean reversion is the
-- correct leading indicator for early-capture symbol selection.
SELECT r.ticker, COALESCE(MAX(s.title), '')::text AS company_name,
       SUM(r.total_r)::double precision AS total_r,
       AVG(r.win_rate) AS win_rate,
       AVG(r.sharpe) AS sharpe,
       MAX(r.max_dd_pct)::double precision AS max_dd_pct,
       SUM(r.final_equity)::double precision AS final_equity,
       SUM(r.trades) AS trades,
       COALESCE(AVG(r.momentum_ema), 0)::double precision AS avg_momentum_ema,
       COALESCE(AVG(r.momentum_ratio), 0)::double precision AS avg_momentum_ratio,
       COALESCE(AVG(r.soxx_20d_pct), 0)::double precision AS avg_soxx_trend,
       COALESCE(AVG(r.vixy_20d_pct), 0)::double precision AS avg_vixy_trend,
       COALESCE(SUM(r.network_score), 0)::double precision AS network_score,
       COALESCE(SUM(r.network_profit_trades), 0)::bigint AS network_profit_trades,
       COALESCE(SUM(r.network_profit_r), 0)::double precision AS network_profit_r,
       COALESCE(SUM(r.network_loss_r), 0)::double precision AS network_loss_r,
       COALESCE(SUM(r.network_net_r), 0)::double precision AS network_net_r,
       COALESCE(SUM(r.network_profit_pnl), 0)::double precision AS network_profit_pnl,
       COALESCE(SUM(r.network_net_pnl), 0)::double precision AS network_net_pnl,
       (SUM(r.total_r * r.win_rate * CASE
           WHEN @regime_gate::bool = false THEN 1.0
           WHEN r.soxx_20d_pct IS NULL THEN 0.7
           WHEN r.soxx_20d_pct < -0.05 THEN 0.3
           WHEN r.soxx_20d_pct < 0 THEN 0.7
           ELSE 1.0
       END) + COALESCE(SUM(r.network_net_r), 0))::double precision AS score,
       COUNT(*) AS windows,
       SUM(CASE WHEN r.soxx_20d_pct > 0.05 THEN r.total_r ELSE 0 END)::double precision AS bull_r,
       SUM(CASE WHEN r.soxx_20d_pct < -0.05 THEN r.total_r ELSE 0 END)::double precision AS bear_r,
       SUM(CASE WHEN r.soxx_20d_pct >= -0.05 AND r.soxx_20d_pct <= 0.05 THEN r.total_r ELSE 0 END)::double precision AS chop_r,
       MIN(r.from_date)::text AS earliest_window,
       MAX(r.to_date)::text AS latest_window,
       SUM(r.final_equity - 100000.0)::double precision AS realized_pnl,
       SUM(r.unrealized_pnl)::double precision AS unrealized_pnl,
       SUM(r.final_equity + r.unrealized_pnl - 100000.0)::double precision AS marked_balance
FROM ticker_backtest_results r
LEFT JOIN sec_tickers s ON s.ticker = r.ticker
WHERE r.to_date <= @as_of_date::date
  AND r.strategy LIKE @strategy
  AND r.timeframe = @timeframe
  AND r.trades >= @trades
GROUP BY r.ticker
HAVING COUNT(*) >= @min_windows::int
   AND (SUM(r.total_r) > 0 OR COALESCE(SUM(r.network_net_r), 0) > 0)
ORDER BY score DESC
LIMIT @max_results;

-- name: ListTickerBacktestResultsByTicker :many
-- Per-window results for a specific ticker, for drill-down view.
SELECT r.id, r.cik, r.ticker, r.strategy, r.timeframe, r.from_date, r.to_date,
       r.trades, r.win_rate, r.expect_r, r.profit_factor, r.total_r, r.sharpe,
       r.max_dd_pct, r.max_dd_dollars, r.final_equity, r.generation,
       r.soxx_20d_pct, r.vixy_20d_pct,
       r.momentum_ema, r.momentum_ratio, r.unrealized_pnl,
       COALESCE(r.portfolio_context_id, 0)::bigint AS portfolio_context_id,
       COALESCE(pc.trade_symbols, '{}')::text[] AS portfolio_symbols,
       COALESCE(pc.signal_context_symbols, '{}')::text[] AS signal_context_symbols,
       t.title AS company_name
FROM ticker_backtest_results r
JOIN sec_tickers t ON t.cik = r.cik AND t.ticker = r.ticker
LEFT JOIN ticker_backtest_contexts pc ON pc.id = r.portfolio_context_id
WHERE r.ticker = sqlc.arg('ticker')
  AND r.generation = (SELECT generation FROM ticker_backtest_results GROUP BY generation ORDER BY COUNT(*) DESC LIMIT 1)
ORDER BY r.from_date;

-- name: SelectDynamicPortfolioV2 :many
-- V2: with mean reversion boost weight and lookback period for dynamic portfolio scoring.
-- Joins ticker_backtest_results with recent mean_reversion signal performance to boost
-- symbols showing positive MR turnaround. The mrWeight parameter controls how much
-- the MR signal influences the final score (0 = disabled, 1.0 = full boost).
-- mrLookback is the lookback window in days for MR signal performance.
SELECT r.ticker, COALESCE(MAX(s.title), '')::text AS company_name,
       SUM(r.total_r)::double precision AS total_r,
       AVG(r.win_rate) AS win_rate,
       AVG(r.sharpe) AS sharpe,
       MAX(r.max_dd_pct)::double precision AS max_dd_pct,
       SUM(r.final_equity)::double precision AS final_equity,
       SUM(r.trades) AS trades,
       COALESCE(AVG(r.momentum_ema), 0)::double precision AS avg_momentum_ema,
       COALESCE(AVG(r.momentum_ratio), 0)::double precision AS avg_momentum_ratio,
       COALESCE(AVG(r.soxx_20d_pct), 0)::double precision AS avg_soxx_trend,
       COALESCE(AVG(r.vixy_20d_pct), 0)::double precision AS avg_vixy_trend,
       -- Base score with regime gating
       SUM(r.total_r * r.win_rate * CASE
           WHEN sqlc.narg('regime_gate')::bool = false THEN 1.0
           WHEN r.soxx_20d_pct IS NULL THEN 0.7
           WHEN r.soxx_20d_pct < -0.05 THEN 0.3
           WHEN r.soxx_20d_pct < 0 THEN 0.7
           ELSE 1.0
       END)::double precision AS base_score,
       -- Mean reversion boost (NULL if no MR data)
       COALESCE(MAX(mr.avg_r), 0)::float8 AS mr_avg_r,
       COALESCE(MAX(mr.win_rate), 0)::float8 AS mr_win_rate,
       -- Final score: own performance + MR boost + causal profit/loss on other
       -- symbols. network_net_r is in the same risk-unit scale as total_r and
       -- includes losing beneficiary trades, preventing noisy breadth symbols
       -- from receiving one-sided credit.
       (SUM(r.total_r * r.win_rate * CASE
           WHEN sqlc.narg('regime_gate')::bool = false THEN 1.0
           WHEN r.soxx_20d_pct IS NULL THEN 0.7
           WHEN r.soxx_20d_pct < -0.05 THEN 0.3
           WHEN r.soxx_20d_pct < 0 THEN 0.7
           ELSE 1.0
        END)::double precision +
        COALESCE(MAX(mr.avg_r) * sqlc.narg('mr_weight')::float8, 0)::double precision +
        COALESCE(SUM(r.network_net_r), 0)::double precision)::numeric AS score,
       COUNT(*) AS windows,
       SUM(CASE WHEN r.soxx_20d_pct > 0.05 THEN r.total_r ELSE 0 END)::double precision AS bull_r,
       SUM(CASE WHEN r.soxx_20d_pct < -0.05 THEN r.total_r ELSE 0 END)::double precision AS bear_r,
       SUM(CASE WHEN r.soxx_20d_pct >= -0.05 AND r.soxx_20d_pct <= 0.05 THEN r.total_r ELSE 0 END)::double precision AS chop_r,
       MIN(r.from_date)::text AS earliest_window,
       MAX(r.to_date)::text AS latest_window,
       SUM(r.final_equity - 100000.0)::double precision AS realized_pnl,
       SUM(r.unrealized_pnl)::double precision AS unrealized_pnl,
       SUM(r.final_equity + r.unrealized_pnl - 100000.0)::double precision AS marked_balance
FROM ticker_backtest_results r
LEFT JOIN sec_tickers s ON s.ticker = r.ticker
LEFT JOIN (
    SELECT sig.symbol,
           COALESCE(AVG(out.r_multiple), 0)::float8 AS avg_r,
           COALESCE(AVG((out.r_multiple > 0)::int), 0)::float8 AS win_rate
    FROM signals sig
    JOIN signal_outcomes out ON out.signal_id = sig.id
    JOIN strategies st ON st.id = sig.strategy_id
    WHERE st.name = 'mean_reversion'
      AND sig.asset_type = 'equity'
      AND sig.direction IN ('long', 'short')
      AND sig.ts >= NOW() - (MAKE_INTERVAL(days => sqlc.narg('mr_lookback')))
    GROUP BY sig.symbol
    HAVING COUNT(out.id) >= 5
) mr ON mr.symbol = r.ticker
WHERE r.generation = (SELECT generation FROM ticker_backtest_results GROUP BY generation ORDER BY COUNT(*) DESC LIMIT 1)
  AND r.strategy LIKE sqlc.narg('strategy')
  AND r.timeframe = sqlc.narg('timeframe')
  AND r.trades >= sqlc.narg('trades')
  AND r.from_date >= sqlc.narg('from_date')
  AND r.to_date <= sqlc.narg('to_date')
GROUP BY r.ticker
HAVING COUNT(*) >= sqlc.narg('min_windows')::int
  AND (SUM(r.total_r) > 0 OR COALESCE(SUM(r.network_net_r), 0) > 0)
  -- Require at least one positive momentum indicator (EMAd or MOM R)
  AND (COALESCE(AVG(r.momentum_ema), 0) > 0 OR COALESCE(AVG(r.momentum_ratio), 0) > 0)
ORDER BY score DESC
LIMIT sqlc.narg('max_results');

-- name: SelectDynamicPortfolioV4 :many
-- V4 applies momentum-transition and trade-count leading indicators before
-- the final LIMIT. Every source row is scoped to one deterministic generation
-- and timeframe, and the current/prior windows use deterministic ordering.
WITH selected_generation AS (
    SELECT r.generation
    FROM ticker_backtest_results r
    WHERE r.strategy LIKE sqlc.narg('strategy')
      AND r.timeframe = sqlc.narg('timeframe')
    GROUP BY r.generation
    -- Prefer the newest coherent cross-section. Tiny ad-hoc generations are
    -- diagnostics, not a market-wide selector; the historical generation with
    -- the most rows must not permanently eclipse every newer weekly run.
    ORDER BY (COUNT(DISTINCT r.ticker) >= 2 AND COUNT(*) >= 4) DESC,
             r.generation DESC,
             COUNT(*) DESC
    LIMIT 1
),
scoped AS (
    SELECT r.*
    FROM ticker_backtest_results r
    JOIN selected_generation g USING (generation)
    WHERE r.strategy LIKE sqlc.narg('strategy')
      AND r.timeframe = sqlc.narg('timeframe')
      AND r.from_date >= sqlc.narg('from_date')
      AND r.to_date <= sqlc.narg('to_date')
),
current_window AS (
    SELECT DISTINCT ON (r.ticker)
           r.ticker, r.id, r.from_date, r.to_date, r.trades,
           r.win_rate, r.momentum_ema, r.momentum_ratio,
           r.soxx_20d_pct, r.vixy_20d_pct,
           r.semi_affinity_events,
           r.semi_affinity_smh_rate, r.semi_affinity_soxx_rate,
           r.semi_affinity_soxq_rate, r.semi_affinity_xsd_rate,
           r.semi_affinity_psi_rate, r.semi_affinity_ftxl_rate,
           r.semi_leadership_trend_pct
    FROM scoped r
    ORDER BY r.ticker, r.to_date DESC, r.from_date DESC, r.id DESC
),
affinity_medians AS (
    SELECT
        percentile_cont(0.5) WITHIN GROUP (ORDER BY semi_affinity_smh_rate)
            FILTER (WHERE semi_affinity_events >= 20 AND semi_affinity_smh_rate IS NOT NULL) AS smh,
        percentile_cont(0.5) WITHIN GROUP (ORDER BY semi_affinity_soxx_rate)
            FILTER (WHERE semi_affinity_events >= 20 AND semi_affinity_soxx_rate IS NOT NULL) AS soxx,
        percentile_cont(0.5) WITHIN GROUP (ORDER BY semi_affinity_soxq_rate)
            FILTER (WHERE semi_affinity_events >= 20 AND semi_affinity_soxq_rate IS NOT NULL) AS soxq,
        percentile_cont(0.5) WITHIN GROUP (ORDER BY semi_affinity_xsd_rate)
            FILTER (WHERE semi_affinity_events >= 20 AND semi_affinity_xsd_rate IS NOT NULL) AS xsd
    FROM current_window
),
affinity_shrunk AS (
    SELECT c.*,
           (c.semi_affinity_events * c.semi_affinity_smh_rate + 20 * m.smh) /
               (c.semi_affinity_events + 20) AS shrunk_smh,
           (c.semi_affinity_events * c.semi_affinity_soxx_rate + 20 * m.soxx) /
               (c.semi_affinity_events + 20) AS shrunk_soxx,
           (c.semi_affinity_events * c.semi_affinity_soxq_rate + 20 * m.soxq) /
               (c.semi_affinity_events + 20) AS shrunk_soxq,
           (c.semi_affinity_events * c.semi_affinity_xsd_rate + 20 * m.xsd) /
               (c.semi_affinity_events + 20) AS shrunk_xsd
    FROM current_window c
    CROSS JOIN affinity_medians m
),
affinity_ranked AS (
    SELECT a.ticker,
           (
               0.45 * percent_rank() OVER (ORDER BY a.shrunk_smh) +
               0.35 * percent_rank() OVER (ORDER BY a.shrunk_soxx) +
               0.10 * percent_rank() OVER (ORDER BY a.shrunk_soxq) +
               0.10 * percent_rank() OVER (ORDER BY a.shrunk_xsd)
           )::double precision AS semi_affinity_score
    FROM affinity_shrunk a
    WHERE a.semi_affinity_events >= 20
      AND a.shrunk_smh IS NOT NULL
      AND a.shrunk_soxx IS NOT NULL
      AND a.shrunk_soxq IS NOT NULL
      AND a.shrunk_xsd IS NOT NULL
),
current_features AS (
    SELECT c.*, a.semi_affinity_score
    FROM current_window c
    LEFT JOIN affinity_ranked a USING (ticker)
),
prior_window AS (
    SELECT c.ticker,
           p.momentum_ema AS prior_momentum_ema,
           p.momentum_ratio AS prior_momentum_ratio,
           p.trades AS prior_trades
    FROM current_window c
    LEFT JOIN LATERAL (
        SELECT r.momentum_ema, r.momentum_ratio, r.trades
        FROM ticker_backtest_results r
        JOIN selected_generation g USING (generation)
        WHERE r.ticker = c.ticker
          AND r.strategy LIKE sqlc.narg('strategy')
          AND r.timeframe = sqlc.narg('timeframe')
          AND r.from_date >= sqlc.narg('from_date')
          AND r.to_date <= sqlc.narg('to_date')
          AND r.to_date <= c.from_date
          AND r.id <> c.id
        ORDER BY r.to_date DESC, r.from_date DESC, r.id DESC
        LIMIT 1
    ) p ON true
),
macro AS (
    SELECT COALESCE(r.soxx_20d_pct, 0)::float8 AS soxx,
           COALESCE(r.vixy_20d_pct, 0)::float8 AS vixy
    FROM scoped r
    WHERE r.trades >= 5
      AND r.soxx_20d_pct IS NOT NULL
    ORDER BY r.to_date DESC, r.from_date DESC, r.id DESC
    LIMIT 1
),
mr AS (
    SELECT sig.symbol,
           COALESCE(AVG(out.r_multiple), 0)::float8 AS avg_r,
           COALESCE(AVG((out.r_multiple > 0)::int), 0)::float8 AS win_rate
    FROM signals sig
    JOIN signal_outcomes out ON out.signal_id = sig.id
    JOIN strategies st ON st.id = sig.strategy_id
    WHERE st.name = 'mean_reversion'
      AND sig.asset_type = 'equity'
      AND sig.direction IN ('long', 'short')
      AND sig.ts >= sqlc.narg('as_of')::timestamptz -
          MAKE_INTERVAL(days => sqlc.narg('mr_lookback')::int)
      AND sig.ts <= sqlc.narg('as_of')::timestamptz
      AND out.graded_at <= sqlc.narg('as_of')::timestamptz
    GROUP BY sig.symbol
    HAVING COUNT(out.id) >= 5
),
base AS (
    SELECT r.ticker, COALESCE(MAX(s.title), '')::text AS company_name,
           SUM(r.total_r)::double precision AS total_r,
           AVG(r.win_rate)::double precision AS win_rate,
           AVG(r.sharpe)::double precision AS sharpe,
           MAX(r.max_dd_pct)::double precision AS max_dd_pct,
           SUM(r.final_equity)::double precision AS final_equity,
           SUM(r.trades)::bigint AS trades,
           COALESCE(AVG(r.momentum_ema), 0)::double precision AS avg_momentum_ema,
           COALESCE(AVG(r.momentum_ratio), 0)::double precision AS avg_momentum_ratio,
           COALESCE(AVG(r.soxx_20d_pct), 0)::double precision AS avg_soxx_trend,
           COALESCE(AVG(r.vixy_20d_pct), 0)::double precision AS avg_vixy_trend,
           COALESCE(SUM(r.network_score), 0)::double precision AS network_score,
           COALESCE(SUM(r.network_profit_trades), 0)::bigint AS network_profit_trades,
           COALESCE(SUM(r.network_profit_r), 0)::double precision AS network_profit_r,
           COALESCE(SUM(r.network_loss_r), 0)::double precision AS network_loss_r,
           COALESCE(SUM(r.network_net_r), 0)::double precision AS network_net_r,
           COALESCE(SUM(r.network_profit_pnl), 0)::double precision AS network_profit_pnl,
           COALESCE(SUM(r.network_net_pnl), 0)::double precision AS network_net_pnl,
           (
               SUM(r.total_r * r.win_rate * CASE
                   WHEN sqlc.narg('regime_gate')::bool = false THEN 1.0
                   WHEN r.soxx_20d_pct IS NULL THEN 0.7
                   WHEN r.soxx_20d_pct < -0.05 THEN 0.3
                   WHEN r.soxx_20d_pct < 0 THEN 0.7
                   ELSE 1.0
               END)::double precision +
               COALESCE(MAX(mr.avg_r) * sqlc.narg('mr_weight')::float8, 0) +
               COALESCE(SUM(r.network_net_r), 0)
           )::double precision AS base_score,
           COUNT(*)::bigint AS windows,
           SUM(CASE WHEN r.soxx_20d_pct > 0.05 THEN r.total_r ELSE 0 END)::double precision AS bull_r,
           SUM(CASE WHEN r.soxx_20d_pct < -0.05 THEN r.total_r ELSE 0 END)::double precision AS bear_r,
           SUM(CASE WHEN r.soxx_20d_pct >= -0.05 AND r.soxx_20d_pct <= 0.05 THEN r.total_r ELSE 0 END)::double precision AS chop_r,
           MIN(r.from_date)::text AS earliest_window,
           MAX(r.to_date)::text AS latest_window,
           SUM(r.final_equity - 100000.0)::double precision AS realized_pnl,
           SUM(r.unrealized_pnl)::double precision AS unrealized_pnl,
           SUM(r.final_equity + r.unrealized_pnl - 100000.0)::double precision AS marked_balance
    FROM scoped r
    LEFT JOIN sec_tickers s ON s.ticker = r.ticker
    LEFT JOIN mr ON mr.symbol = r.ticker
    WHERE r.trades >= GREATEST(
        sqlc.narg('min_trades')::int,
        sqlc.narg('min_trades_ext')::int
    )
    GROUP BY r.ticker
    HAVING COUNT(*) >= GREATEST(
        sqlc.narg('min_windows')::int,
        sqlc.narg('min_windows_ext')::int
    )
      AND (SUM(r.total_r) > 0 OR COALESCE(SUM(r.network_net_r), 0) > 0)
),
contextual AS (
    SELECT b.*, c.trades AS current_trades,
           c.win_rate AS current_win_rate,
           c.momentum_ema AS current_momentum_ema,
           c.momentum_ratio AS current_momentum_ratio,
           c.semi_affinity_score,
           c.semi_affinity_events,
           c.semi_affinity_smh_rate,
           c.semi_affinity_soxx_rate,
           c.semi_affinity_soxq_rate,
           c.semi_affinity_xsd_rate,
           c.semi_affinity_psi_rate,
           c.semi_affinity_ftxl_rate,
           c.semi_leadership_trend_pct,
           p.prior_trades, p.prior_momentum_ema, p.prior_momentum_ratio,
           COALESCE(m.soxx, 0) AS macro_soxx,
           COALESCE(m.vixy, 0) AS macro_vixy,
           CASE
             WHEN p.prior_momentum_ema IS NULL OR p.prior_momentum_ratio IS NULL THEN ''
             WHEN p.prior_momentum_ema > 0 AND p.prior_momentum_ratio > 0
              AND c.momentum_ema < 0 AND c.momentum_ratio < 0 THEN 'CULL'
             WHEN p.prior_momentum_ema < 0 AND p.prior_momentum_ratio < 0
              AND c.momentum_ema < 0 AND c.momentum_ratio < 0 THEN 'DROP'
             WHEN p.prior_momentum_ema > 0 AND p.prior_momentum_ratio > 0
              AND c.momentum_ema > 0 AND c.momentum_ratio > 0
              AND NOT (
                  (COALESCE(m.soxx, 0) > 0 AND COALESCE(m.vixy, 0) < -1.0)
                  OR
                  (COALESCE(m.soxx, 0) > -0.05 AND COALESCE(m.soxx, 0) < 0.05
                   AND COALESCE(m.vixy, 0) > 1.0)
              ) THEN 'WATCH'
             ELSE ''
           END::text AS transition_action
    FROM base b
    JOIN current_features c USING (ticker)
    LEFT JOIN prior_window p USING (ticker)
    LEFT JOIN macro m ON true
),
dynamic_components AS (
    SELECT c.*,
           COALESCE(
               (c.realized_pnl - MIN(c.realized_pnl) OVER ()) /
               NULLIF(MAX(c.realized_pnl) OVER () - MIN(c.realized_pnl) OVER (), 0),
               0.5
           )::double precision AS dyn_pnl,
           COALESCE(
               (c.current_momentum_ema - MIN(c.current_momentum_ema) OVER ()) /
               NULLIF(
                   MAX(c.current_momentum_ema) OVER () -
                   MIN(c.current_momentum_ema) OVER (),
                   0
               ),
               0.5
           )::double precision AS dyn_ema,
           GREATEST(
               0,
               LEAST(COALESCE(c.current_momentum_ratio, 0), 2.0) - 0.5
           )::double precision AS dyn_momentum,
           GREATEST(COALESCE(c.current_win_rate, 0), 0.1)::double precision
               AS dyn_win_rate
    FROM contextual c
),
dynamic_scored AS (
    SELECT d.*,
           CASE
             -- Match the backtest-pane DYN vetoes: a formerly profitable
             -- symbol whose recent EMA or momentum ratio has broken down
             -- must rank below a healthy symbol. Network value is kept
             -- separate below so a measured catalyst is not discarded.
             WHEN d.realized_pnl > 0
              AND (d.current_momentum_ema <= 0 OR d.current_momentum_ratio < 0.8)
               THEN -ABS(
                   d.dyn_pnl * 0.30 +
                   d.dyn_ema * 0.35 +
                   d.dyn_momentum * 0.15 +
                   d.dyn_win_rate * 0.15
               ) * 0.5
             ELSE
                 d.dyn_pnl * 0.30 +
                 d.dyn_ema * 0.35 +
                 d.dyn_momentum * 0.15 +
                 d.dyn_win_rate * 0.15
           END::double precision AS dynamic_score
    FROM dynamic_components d
),
ranked AS (
    SELECT d.*,
           (
               -- Own-trade momentum describes whether this symbol's trading
               -- season is improving; it must not erase causal profit the
               -- symbol still creates in other names' confluence clusters.
               (d.base_score - d.network_net_r) *
               CASE d.transition_action
                 WHEN 'WATCH' THEN 0.8
                 WHEN 'CULL' THEN 0.3
                 WHEN 'DROP' THEN 0.2
                 ELSE 1.0
               END *
               -- DYN is intentionally bounded. It can promote a healthy
               -- emerging season and suppress a decaying one, but it cannot
               -- overwhelm measured network contribution.
               GREATEST(
                   0.5,
                   LEAST(1.25, 0.75 + d.dynamic_score * 0.5)
               ) *
                CASE
                  WHEN d.prior_trades > 0 AND d.current_trades::float8 / d.prior_trades < 0.8 THEN 1.6
                  WHEN d.prior_trades > 0 AND d.current_trades::float8 / d.prior_trades > 2.0 THEN 0.3
                  ELSE 1.0
                END +
                -- Marginal beneficiary R overlaps across contributors and is
                -- not fully portable after taking a top-N slice. Preserve its
                -- sign and ordering while discounting double-counted scale.
                SIGN(d.network_net_r) * SQRT(ABS(d.network_net_r))
           )::double precision AS score,
           CASE
             WHEN d.prior_trades > 0 AND d.current_trades::float8 / d.prior_trades < 0.8
               -- Lower recent activity was the strongest positive leading
               -- bucket in chronological validation. Do not call this DROP:
               -- DROP already means persistently negative own momentum.
               THEN CONCAT_WS('/', NULLIF(d.transition_action, ''), 'QUIET')
             WHEN d.prior_trades > 0 AND d.current_trades::float8 / d.prior_trades > 2.0
               THEN CONCAT_WS('/', NULLIF(d.transition_action, ''), 'SURGE')
             ELSE d.transition_action
           END::text AS momentum_transition
    FROM dynamic_scored d
),
eligible AS (
    SELECT r.*
    FROM ranked r
    -- CULL/DROP are adverse own-symbol transitions. They earn a portfolio
    -- slot only when measured cluster attribution proves that they remain a
    -- useful catalyst for other symbols. PART# is a binary evidence floor;
    -- larger counts and SLA rank do not increase score.
    WHERE r.semi_affinity_events >= 20
      AND (
          r.transition_action NOT IN ('CULL', 'DROP')
          OR r.network_net_r > 0
      )
)
SELECT ticker, company_name, total_r, win_rate, sharpe, max_dd_pct,
       final_equity, trades, score, dynamic_score, windows, momentum_transition,
       avg_momentum_ema, avg_momentum_ratio, avg_soxx_trend, avg_vixy_trend,
       network_score, network_profit_trades, network_profit_r, network_loss_r,
       network_net_r, network_profit_pnl, network_net_pnl,
       semi_affinity_score, semi_affinity_events,
       semi_affinity_smh_rate, semi_affinity_soxx_rate,
       semi_affinity_soxq_rate, semi_affinity_xsd_rate,
       semi_affinity_psi_rate, semi_affinity_ftxl_rate,
       semi_leadership_trend_pct,
       bull_r, bear_r, chop_r, earliest_window, latest_window,
       realized_pnl, unrealized_pnl, marked_balance
FROM eligible
ORDER BY score DESC, ticker ASC
LIMIT sqlc.narg('max_results');

-- name: SelectDynamicPortfolioV3 :many
-- V3: with enhanced regime/momentum context and optional symbol filtering.
-- Extends V2 with additional minTrades and minWindows parameters for stricter
-- filtering. This allows the caller to require more trades and more windows
-- for inclusion in the dynamic portfolio, useful for higher-confidence selections.
-- The mrWeight and mrLookback parameters work the same as V2 for MR boost.
SELECT r.ticker, COALESCE(MAX(s.title), '')::text AS company_name,
       SUM(r.total_r)::double precision AS total_r,
       AVG(r.win_rate) AS win_rate,
       AVG(r.sharpe) AS sharpe,
       MAX(r.max_dd_pct)::double precision AS max_dd_pct,
       SUM(r.final_equity)::double precision AS final_equity,
       SUM(r.trades) AS trades,
       COALESCE(AVG(r.momentum_ema), 0)::double precision AS avg_momentum_ema,
       COALESCE(AVG(r.momentum_ratio), 0)::double precision AS avg_momentum_ratio,
       COALESCE(AVG(r.soxx_20d_pct), 0)::double precision AS avg_soxx_trend,
       COALESCE(AVG(r.vixy_20d_pct), 0)::double precision AS avg_vixy_trend,
       COALESCE(SUM(r.network_score), 0)::double precision AS network_score,
       COALESCE(SUM(r.network_profit_trades), 0)::bigint AS network_profit_trades,
       COALESCE(SUM(r.network_profit_r), 0)::double precision AS network_profit_r,
       COALESCE(SUM(r.network_loss_r), 0)::double precision AS network_loss_r,
       COALESCE(SUM(r.network_net_r), 0)::double precision AS network_net_r,
       COALESCE(SUM(r.network_profit_pnl), 0)::double precision AS network_profit_pnl,
       COALESCE(SUM(r.network_net_pnl), 0)::double precision AS network_net_pnl,
       -- Base score with regime gating
       SUM(r.total_r * r.win_rate * CASE
           WHEN sqlc.narg('regime_gate')::bool = false THEN 1.0
           WHEN r.soxx_20d_pct IS NULL THEN 0.7
           WHEN r.soxx_20d_pct < -0.05 THEN 0.3
           WHEN r.soxx_20d_pct < 0 THEN 0.7
           ELSE 1.0
       END)::double precision AS base_score,
       -- Mean reversion boost (NULL if no MR data)
       COALESCE(MAX(mr.avg_r), 0)::float8 AS mr_avg_r,
       COALESCE(MAX(mr.win_rate), 0)::float8 AS mr_win_rate,
       -- Final score: own performance + MR boost + causal profit/loss on other
       -- symbols. network_net_r includes losing beneficiary trades so breadth
       -- activity alone cannot dominate the ranking.
       (SUM(r.total_r * r.win_rate * CASE
           WHEN sqlc.narg('regime_gate')::bool = false THEN 1.0
           WHEN r.soxx_20d_pct IS NULL THEN 0.7
           WHEN r.soxx_20d_pct < -0.05 THEN 0.3
           WHEN r.soxx_20d_pct < 0 THEN 0.7
           ELSE 1.0
        END)::double precision +
        COALESCE(MAX(mr.avg_r) * sqlc.narg('mr_weight')::float8, 0)::double precision +
        COALESCE(SUM(r.network_net_r), 0)::double precision)::numeric AS score,
       COUNT(*) AS windows,
       SUM(CASE WHEN r.soxx_20d_pct > 0.05 THEN r.total_r ELSE 0 END)::double precision AS bull_r,
       SUM(CASE WHEN r.soxx_20d_pct < -0.05 THEN r.total_r ELSE 0 END)::double precision AS bear_r,
       SUM(CASE WHEN r.soxx_20d_pct >= -0.05 AND r.soxx_20d_pct <= 0.05 THEN r.total_r ELSE 0 END)::double precision AS chop_r,
       MIN(r.from_date)::text AS earliest_window,
       MAX(r.to_date)::text AS latest_window,
       SUM(r.final_equity - 100000.0)::double precision AS realized_pnl,
       SUM(r.unrealized_pnl)::double precision AS unrealized_pnl,
       SUM(r.final_equity + r.unrealized_pnl - 100000.0)::double precision AS marked_balance
FROM ticker_backtest_results r
LEFT JOIN sec_tickers s ON s.ticker = r.ticker
LEFT JOIN (
    SELECT sig.symbol,
           COALESCE(AVG(out.r_multiple), 0)::float8 AS avg_r,
           COALESCE(AVG((out.r_multiple > 0)::int), 0)::float8 AS win_rate
    FROM signals sig
    JOIN signal_outcomes out ON out.signal_id = sig.id
    JOIN strategies st ON st.id = sig.strategy_id
    WHERE st.name = 'mean_reversion'
      AND sig.asset_type = 'equity'
      AND sig.direction IN ('long', 'short')
      AND sig.ts >= NOW() - (MAKE_INTERVAL(days => sqlc.narg('mr_lookback')))
    GROUP BY sig.symbol
    HAVING COUNT(out.id) >= 5
) mr ON mr.symbol = r.ticker
WHERE r.generation = (SELECT generation FROM ticker_backtest_results GROUP BY generation ORDER BY COUNT(*) DESC LIMIT 1)
  AND r.strategy LIKE sqlc.narg('strategy')
  AND r.timeframe = sqlc.narg('timeframe')
  AND r.trades >= sqlc.narg('trades')
  AND r.from_date >= sqlc.narg('from_date')
  AND r.to_date <= sqlc.narg('to_date')
GROUP BY r.ticker
HAVING COUNT(*) >= sqlc.narg('min_windows_ext')::int
  AND (SUM(r.total_r) > 0 OR COALESCE(SUM(r.network_net_r), 0) > 0)
ORDER BY score DESC
LIMIT sqlc.narg('max_results');
