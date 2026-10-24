-- name: EnqueueParityAlertFill :exec
INSERT INTO tradebot_parity_alert_queue(fill_id) VALUES(sqlc.arg('fillID')) ON CONFLICT DO NOTHING;

-- name: ListPendingParityAlertFills :many
SELECT f.fill_id, i.account_id, i.asset_class, i.timeframe, f.symbol, f.action, f.qty, i.reason,
 COALESCE(f.position_cycle_id,i.position_cycle_id,i.intent_id)::text AS cycle_id
FROM tradebot_parity_alert_queue q JOIN tradebot_fill_events f USING(fill_id)
JOIN tradebot_intents i USING(intent_id)
WHERE q.handled_at IS NULL ORDER BY f.filled_at, f.fill_id;

-- name: HandleParityAlertFills :exec
UPDATE tradebot_parity_alert_queue SET handled_at=now() WHERE fill_id=ANY(sqlc.arg('fill_ids')::text[]);

-- name: ClaimParityAlert :execrows
INSERT INTO tradebot_parity_alert_receipts(alert_key) VALUES(sqlc.arg('alertKey')) ON CONFLICT DO NOTHING;

-- name: FinishParityAlert :exec
UPDATE tradebot_parity_alert_receipts SET delivered_at=now() WHERE alert_key=sqlc.arg('alertKey');

