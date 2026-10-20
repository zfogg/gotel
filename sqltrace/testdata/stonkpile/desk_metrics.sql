-- name: UpsertIVSnapshot :exec
INSERT INTO option_iv_snapshots (symbol, day, iv, spot, option_symbol, strike, expiry, delta)
VALUES (sqlc.arg('symbol'), sqlc.arg('day'), sqlc.arg('iv'), sqlc.arg('spot'), sqlc.arg('option_symbol'), sqlc.arg('strike'), sqlc.arg('expiry'), sqlc.arg('delta'))
ON CONFLICT (symbol, day) DO UPDATE
SET iv            = EXCLUDED.iv,
    spot          = EXCLUDED.spot,
    option_symbol = EXCLUDED.option_symbol,
    strike        = EXCLUDED.strike,
    expiry        = EXCLUDED.expiry,
    delta         = EXCLUDED.delta;

-- name: RecentIVSnapshots :many
SELECT symbol, day, iv, spot, option_symbol, strike, expiry, delta
FROM option_iv_snapshots
WHERE symbol = sqlc.arg('symbol') AND day >= sqlc.arg('day')
ORDER BY day ASC;

-- name: UpsertDeskSnapshot :exec
INSERT INTO desk_equity_snapshots (mandate, day, equity, cash, unrealized_pl, positions)
VALUES (sqlc.arg('mandate'), sqlc.arg('day'), sqlc.arg('equity'), sqlc.arg('cash'), sqlc.arg('unrealized_pl'), sqlc.arg('positions'))
ON CONFLICT (mandate, day) DO UPDATE
SET equity        = EXCLUDED.equity,
    cash          = EXCLUDED.cash,
    unrealized_pl = EXCLUDED.unrealized_pl,
    positions     = EXCLUDED.positions;

-- name: DeskSnapshots :many
SELECT mandate, day, equity, cash, unrealized_pl, positions
FROM desk_equity_snapshots
WHERE mandate = sqlc.arg('mandate')
ORDER BY day ASC;

-- name: AllDeskSnapshots :many
SELECT mandate, day, equity, cash, unrealized_pl, positions
FROM desk_equity_snapshots
ORDER BY mandate, day ASC;

-- name: UpsertDeskFill :exec
INSERT INTO desk_fills (order_id, mandate, asset_class, symbol, side, qty, avg_price, filled_at, order_class, client_order_id, target_price, account_id, fill_id, tax_complete)
VALUES (sqlc.arg('order_id'), sqlc.arg('mandate'), sqlc.arg('asset_class'), sqlc.arg('symbol'), sqlc.arg('side'), sqlc.arg('qty'), sqlc.arg('avg_price'), sqlc.arg('filled_at'), sqlc.arg('order_class'), sqlc.arg('client_order_id'), sqlc.arg('target_price'), sqlc.arg('account_id'), sqlc.arg('fill_id'), sqlc.arg('tax_complete'))
ON CONFLICT (order_id) DO UPDATE
SET qty        = EXCLUDED.qty,
    avg_price  = EXCLUDED.avg_price,
    filled_at  = EXCLUDED.filled_at,
    target_price = EXCLUDED.target_price,
    account_id = EXCLUDED.account_id,
    fill_id = EXCLUDED.fill_id,
    tax_complete = EXCLUDED.tax_complete;

-- name: UpsertDeskFills :exec
INSERT INTO desk_fills (order_id, mandate, asset_class, symbol, side, qty, avg_price, filled_at, order_class, client_order_id, target_price, account_id, fill_id, tax_complete)
SELECT
  p.order_id, p.mandate, p.asset_class, p.symbol, p.side, p.qty, p.avg_price,
  p.filled_at, p.order_class, p.client_order_id, p.target_price, p.account_id,
  p.fill_id, p.tax_complete
FROM jsonb_to_recordset(sqlc.arg(fills)::jsonb) AS p(
  order_id text,
  mandate text,
  asset_class text,
  symbol text,
  side text,
  qty double precision,
  avg_price double precision,
  filled_at timestamptz,
  order_class text,
  client_order_id text,
  target_price double precision,
  account_id text,
  fill_id text,
  tax_complete boolean
)
ON CONFLICT (order_id) DO UPDATE
SET qty          = EXCLUDED.qty,
    avg_price    = EXCLUDED.avg_price,
    filled_at    = EXCLUDED.filled_at,
    target_price = EXCLUDED.target_price,
    account_id   = EXCLUDED.account_id,
    fill_id      = EXCLUDED.fill_id,
    tax_complete = EXCLUDED.tax_complete;

-- name: DeskFills :many
SELECT order_id, mandate, asset_class, symbol, side, qty, avg_price, filled_at, order_class, client_order_id, target_price, account_id, fill_id, tax_complete
FROM desk_fills
WHERE (sqlc.arg('mandate')::text = '' OR mandate = sqlc.arg('mandate')) AND (sqlc.arg('asset_class')::text = '' OR asset_class = sqlc.arg('asset_class'))
ORDER BY filled_at DESC
LIMIT sqlc.arg('limit');
-- name: LatestDeskEquity :one
SELECT equity::float8 FROM desk_equity_snapshots WHERE mandate = sqlc.arg(mandate) ORDER BY day DESC LIMIT 1;
