-- name: UpsertConfluenceRuleAlertState :one
INSERT INTO confluence_rule_alert_state (scope, state_hash)
VALUES (sqlc.arg('scope'), sqlc.arg('state_hash'))
ON CONFLICT (scope) DO UPDATE
SET state_hash = EXCLUDED.state_hash,
    updated_at = now()
WHERE confluence_rule_alert_state.state_hash IS DISTINCT FROM EXCLUDED.state_hash
RETURNING scope, state_hash, updated_at;

-- name: LatestConfluenceRuleAlertState :one
SELECT scope, state_hash, updated_at
FROM confluence_rule_alert_state
WHERE scope = sqlc.arg('scope');

-- name: BootstrapConfluenceRuleAlertState :exec
INSERT INTO confluence_rule_alert_state (scope, state_hash, updated_at)
SELECT sqlc.arg('target_scope'), legacy.state_hash, legacy.updated_at
FROM confluence_rule_alert_state AS legacy
WHERE legacy.scope = sqlc.arg('legacy_scope')
ON CONFLICT (scope) DO NOTHING;
