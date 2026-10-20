-- Tax account configuration and event ledger.

-- name: ListTaxBrokerActivities :many
SELECT account_id, broker, asset_class, activity_id, kind, symbol, amount, qty,
       split_ratio, price, side, order_id, occurred_at
FROM tax_broker_activities
WHERE account_id = sqlc.arg(account_id)
  AND broker = sqlc.arg(broker)
  AND asset_class = sqlc.arg(asset_class)
ORDER BY occurred_at, activity_id;

-- name: UpsertTaxBrokerActivity :exec
INSERT INTO tax_broker_activities (
  account_id, broker, asset_class, activity_id, kind, symbol, amount, qty,
  split_ratio, price, side, order_id, occurred_at
) VALUES (
  sqlc.arg(account_id), sqlc.arg(broker), sqlc.arg(asset_class),
  sqlc.arg(activity_id), sqlc.arg(kind), sqlc.arg(symbol), sqlc.arg(amount),
  sqlc.arg(qty), sqlc.arg(split_ratio), sqlc.arg(price), sqlc.arg(side),
  sqlc.arg(order_id), sqlc.arg(occurred_at)
)
ON CONFLICT (account_id, broker, asset_class, activity_id) DO UPDATE SET
  kind = EXCLUDED.kind,
  symbol = EXCLUDED.symbol,
  amount = EXCLUDED.amount,
  qty = EXCLUDED.qty,
  split_ratio = EXCLUDED.split_ratio,
  price = EXCLUDED.price,
  side = EXCLUDED.side,
  order_id = EXCLUDED.order_id,
  occurred_at = EXCLUDED.occurred_at;

-- name: GetTaxActivityCoverage :one
SELECT * FROM tax_activity_coverage
WHERE account_id = sqlc.arg(account_id)
  AND broker = sqlc.arg(broker)
  AND asset_class = sqlc.arg(asset_class);

-- name: UpsertTaxActivityCoverage :exec
INSERT INTO tax_activity_coverage (
  account_id, broker, asset_class, coverage_from, coverage_through,
  complete, last_full_rebuild_at
) VALUES (
  sqlc.arg(account_id), sqlc.arg(broker), sqlc.arg(asset_class),
  sqlc.arg(coverage_from), sqlc.arg(coverage_through),
  sqlc.arg(complete), sqlc.arg(last_full_rebuild_at)
)
ON CONFLICT (account_id, broker, asset_class) DO UPDATE SET
  coverage_from = EXCLUDED.coverage_from,
  coverage_through = EXCLUDED.coverage_through,
  complete = EXCLUDED.complete,
  last_full_rebuild_at = EXCLUDED.last_full_rebuild_at,
  updated_at = now();

-- name: EnsureTaxAccount :exec
INSERT INTO tax_accounts (account_id, broker, treatment, profile_key)
VALUES (sqlc.arg(account_id), sqlc.arg(broker), sqlc.arg(treatment), sqlc.arg(profile_key))
ON CONFLICT (account_id) DO UPDATE
SET broker = EXCLUDED.broker, treatment = EXCLUDED.treatment,
    profile_key = EXCLUDED.profile_key, updated_at = now();

-- name: InsertTaxLiabilityEvent :exec
INSERT INTO tax_liability_events (
    event_id, account_id, tax_year, event_type, source_type, source_id,
    desk_fill_order_id, tradebot_fill_id, symbol, tax_character,
    taxable_amount, federal_tax, state_tax, local_tax, niit_tax,
    liability_delta, held_delta, withdrawn_amount, effective_at,
    rule_version, complete, missing_data, note, raw_response
) SELECT
    sqlc.arg(event_id), sqlc.arg(account_id), sqlc.arg(tax_year), sqlc.arg(event_type), sqlc.arg(source_type), sqlc.arg(source_id),
    sqlc.narg(desk_fill_order_id), sqlc.narg(tradebot_fill_id), sqlc.narg(symbol), sqlc.narg(tax_character),
    sqlc.arg(taxable_amount), sqlc.arg(federal_tax), sqlc.arg(state_tax), sqlc.arg(local_tax), sqlc.arg(niit_tax),
    sqlc.arg(liability_delta), sqlc.arg(held_delta), sqlc.arg(withdrawn_amount), sqlc.arg(effective_at),
    sqlc.arg(rule_version), sqlc.arg(complete), sqlc.arg(missing_data), sqlc.arg(note), sqlc.arg(raw_response)
WHERE NOT (sqlc.arg(event_type) = 'realization' AND sqlc.arg(source_type) = 'tradebot_fill' AND EXISTS (
 SELECT 1 FROM tax_history_reconciliation WHERE account_id=sqlc.arg(account_id) AND checked_through >= sqlc.arg(effective_at)
))
ON CONFLICT (account_id, source_type, source_id, event_type) DO NOTHING;

-- name: GetTaxSharedSummary :one
SELECT account_summaries FROM tax_shared_summary WHERE tax_year = sqlc.arg(tax_year);

-- name: ListTaxSummaries :many
WITH filtered AS NOT MATERIALIZED (
    -- Let each aggregate read only its own columns, without spilling a shared
    -- materialized copy of the annual ledger (including broker payloads).
    SELECT account_id, liability_delta, held_delta, withdrawn_amount,
           federal_tax, state_tax, local_tax, niit_tax, taxable_amount,
           tax_character, complete, missing_data, effective_at
    FROM tax_liability_events WHERE tax_year = sqlc.arg(tax_year)
      AND (sqlc.arg(account_id)::text = '' OR account_id = sqlc.arg(account_id)::text)
), totals AS (
    SELECT account_id,
           sum(liability_delta)::double precision AS liability,
           greatest(0, sum(held_delta) - sum(withdrawn_amount))::double precision AS held,
           sum(withdrawn_amount)::double precision AS withdrawn,
           sum(federal_tax)::double precision AS federal, sum(state_tax)::double precision AS state,
           sum(local_tax)::double precision AS local, sum(niit_tax)::double precision AS niit,
           sum(taxable_amount) FILTER (WHERE tax_character IN ('ordinary','investment_ordinary'))::double precision AS ordinary,
           sum(taxable_amount) FILTER (WHERE tax_character = 'short_term')::double precision AS short_term,
           sum(taxable_amount) FILTER (WHERE tax_character = 'long_term')::double precision AS long_term,
           sum(taxable_amount) FILTER (WHERE tax_character = 'section_1256')::double precision AS section_1256,
           bool_and(complete)::boolean AS complete,
           sum(greatest(0, held_delta))::double precision AS reserve_added,
           sum(greatest(0, -held_delta))::double precision AS reserve_reduced,
           sum(greatest(0, taxable_amount))::double precision AS taxable_gains,
           sum(least(0, taxable_amount))::double precision AS taxable_losses,
           count(*)::bigint AS event_count,
           (max(effective_at AT TIME ZONE 'America/New_York') FILTER (WHERE 'forward_wash_sale_window'=ANY(missing_data)))::date + 31 AS pending_until
    FROM filtered GROUP BY account_id
), missing AS (
    SELECT account_id, array_agg(DISTINCT item)::text[] AS values
    FROM filtered, LATERAL unnest(missing_data) item
    GROUP BY account_id
)
SELECT a.account_id,
       COALESCE(t.liability, 0)::double precision AS estimated_liability,
       COALESCE(t.held, 0)::double precision AS held_tax,
       COALESCE(t.withdrawn, 0)::double precision AS withdrawn_ytd,
       COALESCE(t.federal, 0)::double precision AS federal,
       COALESCE(t.state, 0)::double precision AS state,
       COALESCE(t.local, 0)::double precision AS local,
       COALESCE(t.niit, 0)::double precision AS niit,
       COALESCE(t.ordinary, 0)::double precision AS ordinary,
       COALESCE(t.short_term, 0)::double precision AS short_term,
       COALESCE(t.long_term, 0)::double precision AS long_term,
       COALESCE(t.section_1256, 0)::double precision AS section_1256,
       COALESCE(t.complete, false)::boolean AS complete,
       COALESCE(m.values, '{}')::text[] AS missing_data,
       COALESCE(t.reserve_added, 0)::double precision AS reserve_added,
       COALESCE(t.reserve_reduced, 0)::double precision AS reserve_reduced,
       COALESCE(t.taxable_gains, 0)::double precision AS taxable_gains,
       COALESCE(t.taxable_losses, 0)::double precision AS taxable_losses,
       COALESCE(t.event_count, 0)::bigint AS event_count,
       (t.pending_until::timestamp AT TIME ZONE 'America/New_York')::timestamptz AS pending_until,
       h.checked_through
FROM tax_accounts a
LEFT JOIN totals t ON t.account_id = a.account_id
LEFT JOIN missing m ON m.account_id = a.account_id
LEFT JOIN tax_history_reconciliation h ON h.account_id=a.account_id
WHERE (sqlc.arg(account_id)::text = '' OR a.account_id = sqlc.arg(account_id)::text)
ORDER BY a.account_id;

-- name: TaxStateBefore :one
WITH filtered AS (
    SELECT e.taxable_amount, e.tax_character, e.complete, e.missing_data
    FROM tax_liability_events e
    WHERE (e.account_id=sqlc.arg(account_id) OR e.account_id IN (
      SELECT jsonb_object_keys(s.account_summaries) FROM tax_shared_summary s
      WHERE s.tax_year=sqlc.arg(tax_year) AND s.account_summaries ? sqlc.arg(account_id)
    )) AND e.tax_year=sqlc.arg(tax_year)
      AND (e.event_type='realization' OR e.source_type='annual_carryover') AND e.effective_at < sqlc.arg(before)
), missing AS (
    SELECT COALESCE(array_agg(DISTINCT item), '{}') values
    FROM filtered, LATERAL unnest(missing_data) item
)
SELECT
    COALESCE(sum(taxable_amount) FILTER (WHERE tax_character IN ('ordinary','investment_ordinary')),0)::double precision AS ordinary,
    COALESCE(sum(taxable_amount) FILTER (WHERE tax_character='short_term'),0)::double precision AS short_term,
    COALESCE(sum(taxable_amount) FILTER (WHERE tax_character='long_term'),0)::double precision AS long_term,
    COALESCE(sum(taxable_amount) FILTER (WHERE tax_character='section_1256'),0)::double precision AS section_1256,
    COALESCE(bool_and(complete), true)::boolean AS complete,
    (SELECT values FROM missing)::text[] AS missing_data
FROM filtered;

-- name: TaxInvestmentOrdinaryBefore :one
SELECT COALESCE(sum(taxable_amount),0)::double precision AS investment_ordinary
FROM tax_liability_events e
WHERE (e.account_id=sqlc.arg(account_id) OR e.account_id IN (
    SELECT jsonb_object_keys(s.account_summaries) FROM tax_shared_summary s
    WHERE s.tax_year=sqlc.arg(tax_year) AND s.account_summaries ? sqlc.arg(account_id)
)) AND e.tax_year=sqlc.arg(tax_year)
  AND e.tax_character='investment_ordinary' AND e.effective_at < sqlc.arg(before);

-- name: DailyTaxReport :one
SELECT
    (SELECT count(*) FROM desk_fills d
     WHERE d.account_id=sqlc.arg(account_id) AND d.filled_at >= sqlc.arg(from_at) AND d.filled_at < sqlc.arg(to_at))::bigint AS fills,
    (SELECT count(*) FROM tradebot_fill_events f
     JOIN tradebot_intents i ON i.intent_id = f.intent_id
     WHERE i.account_id=sqlc.arg(account_id) AND f.filled_at >= sqlc.arg(from_at) AND f.filled_at < sqlc.arg(to_at)
       AND f.action IN ('close_long', 'cover_short'))::bigint AS closed_trades,
    COALESCE((SELECT sum(taxable_amount) FROM tax_liability_events
     WHERE account_id=sqlc.arg(account_id) AND effective_at >= sqlc.arg(from_at) AND effective_at < sqlc.arg(to_at)
       AND event_type='realization'), 0)::double precision AS realized_pre_tax,
    COALESCE((SELECT sum(liability_delta) FROM tax_liability_events
     WHERE account_id=sqlc.arg(account_id) AND effective_at >= sqlc.arg(from_at) AND effective_at < sqlc.arg(to_at)
       AND event_type='realization'), 0)::double precision AS estimated_tax;

-- name: ListDailyFillAccountIDs :many
SELECT DISTINCT account_id
FROM desk_fills
WHERE filled_at >= sqlc.arg(from_at) AND filled_at < sqlc.arg(to_at)
  AND account_id <> ''
ORDER BY account_id;

-- name: ListDailyTradebotFillScopes :many
SELECT DISTINCT i.account_id, i.timeframe
FROM tradebot_fill_events f
JOIN tradebot_intents i ON i.intent_id = f.intent_id
WHERE f.filled_at >= sqlc.arg(from_at) AND f.filled_at < sqlc.arg(to_at)
  AND i.asset_class = sqlc.arg(asset_class)
  AND i.timeframe = ANY(sqlc.arg(timeframes)::text[])
  AND i.account_id <> ''
ORDER BY i.account_id, i.timeframe;

-- name: TaxHeldBalance :one
SELECT greatest(0, COALESCE(sum(held_delta), 0) - COALESCE(sum(withdrawn_amount), 0))::double precision AS held
FROM tax_liability_events WHERE account_id = sqlc.arg(account_id) AND tax_year = sqlc.arg(tax_year);

-- name: TaxHeldBalanceThrough :one
-- Replay retains estimated tax reserve across calendar years until an explicit
-- withdrawal. Live sizing must use that same cumulative spendable-capital
-- basis at its immutable decision watermark rather than silently releasing a
-- prior year's still-unpaid reserve on January 1.
SELECT greatest(0, COALESCE(sum(held_delta), 0) - COALESCE(sum(withdrawn_amount), 0))::double precision AS held
FROM tax_liability_events
WHERE account_id = sqlc.arg(account_id)
  AND effective_at <= sqlc.arg(as_of);

-- name: InsertTaxWithdrawal :exec
INSERT INTO tax_liability_events (
    event_id, account_id, tax_year, event_type, source_type, source_id,
    withdrawn_amount, effective_at, rule_version, complete, note
) VALUES (sqlc.arg(event_id), sqlc.arg(account_id), sqlc.arg(tax_year), 'withdrawal', 'cli', sqlc.arg(event_id),
    sqlc.arg(withdrawn_amount), now(), 'operator-withdrawal', true, sqlc.arg(note));
-- name: FreshTaxHistoryReconciliation :one
SELECT EXISTS(SELECT 1 FROM tax_history_reconciliation WHERE account_id = sqlc.arg(account_id)
  AND profile_hash = sqlc.arg(profile_hash) AND checked_through > sqlc.arg(checked_after))::bool;

-- name: FreshTaxSharedSummary :one
SELECT EXISTS(SELECT 1 FROM tax_shared_summary s WHERE profile_hash = sqlc.arg(profile_hash)
  AND s.checked_through > sqlc.arg(checked_after) AND s.tax_year = sqlc.arg(tax_year)
  AND NOT EXISTS(SELECT 1 FROM tax_liability_events e WHERE s.account_summaries ? e.account_id
    AND e.event_type = 'realization' AND e.effective_at > s.checked_through))::bool;
