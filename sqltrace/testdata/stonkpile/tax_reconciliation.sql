-- name: TaxReconciliationFillExists :one
SELECT EXISTS (
    SELECT 1 FROM tax_lot_dispositions d WHERE d.account_id = sqlc.arg(account_id) AND d.closing_fill_id = sqlc.arg(fill_id)
    UNION ALL
    SELECT 1 FROM tax_lots l WHERE l.account_id = sqlc.arg(account_id) AND l.source_fill_id = sqlc.arg(fill_id)
) AS exists;

-- name: ListTaxLotsForUpdate :many
SELECT lot_id, account_id, symbol, asset_class, direction, remaining_qty::text AS remaining_qty,
       unit_basis::text AS unit_basis, opened_at, source_fill_id, complete
FROM tax_lots
WHERE account_id = sqlc.arg(account_id) AND symbol = sqlc.arg(symbol) AND remaining_qty > 0
ORDER BY opened_at, lot_id
FOR UPDATE;

-- name: UpdateTaxLotRemaining :exec
UPDATE tax_lots SET remaining_qty = sqlc.arg(remaining_qty)::text::numeric, updated_at = now()
WHERE lot_id = sqlc.arg(lot_id);

-- name: InsertTaxLot :exec
INSERT INTO tax_lots (
    lot_id, account_id, symbol, asset_class, direction, original_qty,
    remaining_qty, unit_basis, opened_at, source_fill_id, complete, missing_data
) VALUES (
    sqlc.arg(lot_id), sqlc.arg(account_id), sqlc.arg(symbol), sqlc.arg(asset_class), sqlc.arg(direction),
    sqlc.arg(original_qty)::text::numeric, sqlc.arg(remaining_qty)::text::numeric, sqlc.arg(unit_basis)::text::numeric,
    sqlc.arg(opened_at), sqlc.arg(source_fill_id), sqlc.arg(complete), sqlc.arg(missing_data)
) ON CONFLICT (lot_id) DO NOTHING;

-- name: InsertTaxLotDisposition :exec
INSERT INTO tax_lot_dispositions (
    disposition_id, lot_id, account_id, symbol, opening_fill_id, closing_fill_id,
    qty, proceeds, basis, taxable_gain, tax_character, opened_at, closed_at, complete, missing_data
) VALUES (
    sqlc.arg(disposition_id), sqlc.arg(lot_id), sqlc.arg(account_id), sqlc.arg(symbol),
    sqlc.arg(opening_fill_id), sqlc.arg(closing_fill_id), sqlc.arg(qty)::text::numeric,
    sqlc.arg(proceeds)::text::numeric, sqlc.arg(basis)::text::numeric, sqlc.arg(taxable_gain)::text::numeric,
    sqlc.arg(tax_character), sqlc.arg(opened_at), sqlc.arg(closed_at), sqlc.arg(complete), sqlc.arg(missing_data)
) ON CONFLICT (disposition_id) DO NOTHING;

-- name: InsertTaxLotLiability :exec
INSERT INTO tax_liability_events (
    event_id, account_id, tax_year, event_type, source_type, source_id,
    desk_fill_order_id, symbol, tax_character, taxable_amount, federal_tax,
    state_tax, local_tax, niit_tax, liability_delta, held_delta, effective_at,
    rule_version, complete, missing_data
) VALUES (
    sqlc.arg(event_id), sqlc.arg(account_id), sqlc.arg(tax_year), 'realization', 'tax_lot_disposition', sqlc.arg(source_id),
    (SELECT order_id FROM desk_fills WHERE order_id = sqlc.arg(desk_fill_order_id)), sqlc.arg(symbol),
    sqlc.arg(tax_character), sqlc.arg(taxable_amount)::text::numeric, sqlc.arg(federal_tax)::text::numeric,
    sqlc.arg(state_tax)::text::numeric, sqlc.arg(local_tax)::text::numeric, sqlc.arg(niit_tax)::text::numeric,
    sqlc.arg(liability_delta)::text::numeric, sqlc.arg(held_delta)::text::numeric, sqlc.arg(effective_at),
    sqlc.arg(rule_version), sqlc.arg(complete), sqlc.arg(missing_data)
) ON CONFLICT (account_id, source_type, source_id, event_type) DO NOTHING;

-- name: LoadTaxReconciliationState :one
SELECT
    COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'ordinary'), 0)::text AS ordinary,
    COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'short_term'), 0)::text AS short_term,
    COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'long_term'), 0)::text AS long_term,
    COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'section_1256'), 0)::text AS section_1256,
    greatest(0, COALESCE(sum(held_delta), 0) - COALESCE(sum(withdrawn_amount), 0))::text AS held
FROM tax_liability_events
WHERE account_id = sqlc.arg(account_id) AND tax_year = sqlc.arg(tax_year);

-- name: DeleteTaxHistoryReconciliation :exec
DELETE FROM tax_history_reconciliation WHERE account_id = sqlc.arg(account_id);

-- name: DeleteTaxRealizationEvents :exec
DELETE FROM tax_liability_events WHERE account_id = sqlc.arg(account_id) AND event_type = 'realization';

-- name: DeleteTaxLotDispositions :exec
DELETE FROM tax_lot_dispositions WHERE account_id = sqlc.arg(account_id);

-- name: DeleteTaxLots :exec
DELETE FROM tax_lots WHERE account_id = sqlc.arg(account_id);

-- name: DeleteRebuiltTaxLiabilityEvents :exec
DELETE FROM tax_liability_events
WHERE account_id = sqlc.arg(account_id)
  AND (
      (event_type = 'realization' AND effective_at <= sqlc.arg(as_of))
      OR source_type IN ('shared_tax_allocation', 'annual_carryover')
  );

-- name: RemovedSharedTaxAccountExists :one
SELECT EXISTS (
    SELECT 1
    FROM tax_shared_summary s,
         LATERAL jsonb_object_keys(s.account_summaries) account_id
    WHERE s.tax_year = sqlc.arg(tax_year)
      AND NOT (account_id = ANY(sqlc.arg(account_ids)::text[]))
) AS exists;

-- name: FirstTaxReplayYear :one
SELECT LEAST(
    sqlc.arg(first_year)::integer,
    COALESCE(min(tax_year), sqlc.arg(first_year)::integer)
)::integer
FROM tax_liability_events
WHERE account_id = ANY(sqlc.arg(account_ids)::text[])
  AND event_type <> 'realization'
  AND source_type <> 'annual_carryover'
  AND tax_year <= sqlc.arg(first_year)::integer;

-- name: LoadTaxNonRealizationYearState :one
SELECT COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'ordinary'), 0)::text AS ordinary,
       COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'short_term'), 0)::text AS short_term,
       COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'long_term'), 0)::text AS long_term,
       COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'section_1256'), 0)::text AS section_1256,
       COALESCE(sum(taxable_amount) FILTER (WHERE tax_character = 'investment_ordinary'), 0)::text AS investment_ordinary
FROM tax_liability_events
WHERE account_id = ANY(sqlc.arg(account_ids)::text[])
  AND tax_year = sqlc.arg(tax_year)
  AND event_type <> 'realization'
  AND source_type <> 'annual_carryover';

-- name: InsertTaxAnnualCarryover :exec
INSERT INTO tax_liability_events(
    event_id, account_id, tax_year, event_type, source_type, source_id,
    tax_character, taxable_amount, effective_at, rule_version,
    complete, missing_data, note
) VALUES (
    sqlc.arg(event_id), sqlc.arg(account_id), sqlc.arg(tax_year),
    'manual_adjustment', 'annual_carryover', sqlc.arg(source_id),
    sqlc.arg(tax_character), sqlc.arg(taxable_amount)::text::numeric,
    sqlc.arg(effective_at), sqlc.arg(rule_version), true, '{}',
    'Opening taxpayer capital-loss carryover'
);

-- name: InsertRebuiltTaxLiability :exec
INSERT INTO tax_liability_events(
    event_id, account_id, tax_year, event_type, source_type, source_id,
    symbol, tax_character, taxable_amount, federal_tax, state_tax, local_tax,
    niit_tax, liability_delta, held_delta, effective_at, rule_version,
    complete, missing_data, note
) VALUES (
    sqlc.arg(event_id), sqlc.arg(account_id), sqlc.arg(tax_year),
    'realization', sqlc.arg(source_type), sqlc.arg(source_id),
    sqlc.arg(symbol), sqlc.arg(tax_character), sqlc.arg(taxable_amount)::text::numeric,
    sqlc.arg(federal_tax)::text::numeric, sqlc.arg(state_tax)::text::numeric,
    sqlc.arg(local_tax)::text::numeric, sqlc.arg(niit_tax)::text::numeric,
    sqlc.arg(liability_delta)::text::numeric, sqlc.arg(liability_delta)::text::numeric,
    sqlc.arg(effective_at), sqlc.arg(rule_version), sqlc.arg(complete),
    sqlc.arg(missing_data), sqlc.arg(note)
);

-- name: LoadTaxHeldAndWithdrawn :one
SELECT COALESCE(sum(held_delta) FILTER (WHERE event_type = 'realization'), 0)::text AS held,
       COALESCE(sum(withdrawn_amount), 0)::text AS withdrawn
FROM tax_liability_events
WHERE account_id = sqlc.arg(account_id)
  AND tax_year = sqlc.arg(tax_year)
  AND effective_at <= sqlc.arg(as_of);

-- name: InsertSharedTaxAllocation :exec
INSERT INTO tax_liability_events(
    event_id, account_id, tax_year, event_type, source_type, source_id,
    held_delta, effective_at, rule_version, complete, missing_data, note
) VALUES (
    sqlc.arg(event_id), sqlc.arg(account_id), sqlc.arg(tax_year),
    'manual_adjustment', 'shared_tax_allocation', sqlc.arg(source_id),
    sqlc.arg(held_delta)::text::numeric, sqlc.arg(effective_at),
    sqlc.arg(rule_version), true, '{}',
    'Allocate combined conservative reserve across connected accounts'
);

-- name: UpsertTaxSharedSummary :exec
INSERT INTO tax_shared_summary(tax_year, account_summaries, checked_through, profile_hash)
VALUES (
    sqlc.arg(tax_year), sqlc.arg(account_summaries)::jsonb,
    sqlc.arg(checked_through), sqlc.arg(profile_hash)
)
ON CONFLICT(tax_year) DO UPDATE
SET account_summaries = excluded.account_summaries,
    checked_through = excluded.checked_through,
    profile_hash = excluded.profile_hash;

-- name: UpsertTaxHistoryReconciliation :exec
INSERT INTO tax_history_reconciliation(account_id, checked_through, profile_hash, fill_count)
VALUES (
    sqlc.arg(account_id), sqlc.arg(checked_through),
    sqlc.arg(profile_hash), sqlc.arg(fill_count)
)
ON CONFLICT(account_id) DO UPDATE
SET checked_through = excluded.checked_through,
    profile_hash = excluded.profile_hash,
    fill_count = excluded.fill_count;
