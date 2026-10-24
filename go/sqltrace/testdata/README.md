# SQL summary regression corpus

`stonkpile/*.sql` is a snapshot of all 43 sqlc query files (540 named queries)
from github.com/zfogg/stonkpile at commit
`c8217e8103c39873eb78f378823d8d9a3c44bc95`, copied on 2026-10-20.
It is test input only and is never embedded in gotel applications.

`stonkpile_summaries.txt` records each named query's expected summary.
Review changed SQL and summaries before regenerating it with:

```sh
go test ./sqltrace -run TestStonkpileQuerySummaries -update-summaries
```

Focused tests separately establish correctness for the complex constructs;
the full corpus keeps coverage independent of a sibling Stonkpile checkout.
