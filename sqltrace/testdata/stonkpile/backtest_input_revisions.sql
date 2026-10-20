-- name: ListBacktestInputRevisions :many
SELECT dataset, revision
FROM backtest_input_revisions
ORDER BY dataset;
