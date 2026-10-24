# Production SQL regression provenance

`stonkpile_pi5.json` contains 24 self-contained PostgreSQL query fixtures
captured from the read-only Spaniel MCP at `http://pi5:8345/mcp` on 2026-10-22.
Each retains its trace URL, observed `db.query.text` and `db.query.summary`, the
original SQL from stonkpile's generated SQL constants, and the corresponding
Go gotel inspection. C must match both Go fields exactly; the test also verifies
that those golden fields equal the observed production attributes.

There are eight short queries (49–67 sanitized characters), eight medium queries
(1,012–1,180), and eight long queries (1,484–3,246). Original SQL ranges from 86
to 3,755 bytes. Cases include advisory locks, nested EXISTS, DISTINCT ON, typed
JSON recordsets, multi-CTE writes, row locking and conflict updates.

Source was `zfogg/stonkpile` at checkout HEAD
`2a1f1c5aefdf2e4223d5241a1774532249725a29`; fixture bodies are authoritative
snapshots, and relative source paths point to the matching sqlc-generated
constants. No query bind values were read or copied. Production inspection found
197 distinct query names, 194 matching local query constants and 193 exact
Go/source/live attribute pairs. `ListParitySnapshotsForCycle` had a text mismatch
with the local source and was excluded rather than relabeled as an exact match.

The five failures initially exposed by these fixtures were typed-recordset alias
whitespace (two queries), missing row-lock clauses (two queries), and missing
DISTINCT ON structure (one query). The fixed cases are asserted by
`gotel-sql`, including Linux ASan/UBSan runs.
