-- name: InsertBrokerAccountBaseline :exec
INSERT INTO broker_account_baselines
    (broker, account_id, baseline_equity, source, broker_base_value)
VALUES (sqlc.arg(broker), sqlc.arg(account_id), sqlc.arg(baseline_equity), sqlc.arg(source), sqlc.arg(broker_base_value))
ON CONFLICT (broker, account_id) DO NOTHING;

-- name: GetBrokerAccountBaseline :one
SELECT broker, account_id, baseline_equity, source, observed_at,
       COALESCE(broker_base_value, 0)::double precision AS broker_base_value,
       external_cash_flow
FROM broker_account_baselines
WHERE broker = sqlc.arg(broker) AND account_id = sqlc.arg(account_id);
