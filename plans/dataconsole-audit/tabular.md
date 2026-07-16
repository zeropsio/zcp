# Tabular family — S11 live audit

Engines: `db` (postgresql:single@18, full support), `mariadb` (mariadb:single@10.6, full
support), `ch` (clickhouse:single@25.3, view-only). Driven entirely over HTTP against the
live console server (`/api/*`) — no code paths read besides the wire contract cited below.
All mutation testing used my own `audit_`-prefixed rows, inserted and deleted through the
same API surface; postgres also carries a pre-existing adopted app schema (Laravel tables +
a kanban-style `cards`/`attachments`/`products`/`users` app) alongside the `dcseed`
fixtures (`dc_customers`, `dc_orders`) — this gave real `bigint`/`boolean`/`numeric`
columns unavailable in the bare `dcseed` schema (see §Testbed note). Final sweep confirmed
zero residual audit rows and exact original row counts on every table touched
(`dc_customers`=3, `dc_orders`=2, `mariadb.users`=2, `mariadb.products`=1, `ch.events`=20).

Wire contract cited from: `internal/dataconsole/console/server/server.go` (routes, auth
middleware, error envelope), `server/server_writetoken_test.go` (write-token request
shape), `provider/types.go` (Page/TablePage/CellEdit/Applied), `provider/tabular/tabular.go`
+ `dialect.go` (provider semantics), `provider/errors.go` (sentinel→HTTP/code mapping),
`console/seed/tabular.go` (dcseed schema).

---

## 1. Verdict matrices

Legend: **passed** / **failed** / **blocked** / **not-run**. All [VERIFIED] unless flagged
[UNVERIFIED].

### 1.1 db (postgresql:single@18)

| Operation | Verdict | Evidence |
|---|---|---|
| Tree: root → schemas | passed | `GET /api/tree?service=db` → `{"nodes":[{"name":"public","kind":"container",...,"hasChildren":true}]}`, one schema |
| Tree: schema → tables | passed | `segs=["public"]` → 14 nodes, `kind:"tabular"`, `hasChildren:false` each |
| Tree pagination (limit=5, 14 tables) | passed | 3 pages: 5+5+4 rows, `nextCursor` "5"→"10"→"" (empty), exhausted correctly |
| ReadTable: dc_customers | passed | columns w/ dataType+pk; `rowKeyCols:["id"]` |
| ReadTable: dc_orders | passed | same shape |
| ReadTable: adopted-app tables (cards/attachments/products/users/cache/…) | passed | all 14 tables readable; all report a PK (see §5d) |
| ReadTable pagination (dc_customers, limit=2, 3 rows) | passed | page1 rows=2 nextCursor="2"; page2 rows=1 nextCursor="" (exhausted) |
| Query: valid SELECT | passed | returns rows; `columns[].dataType` always `""` (Query never populates it — see §5c) |
| Query: INSERT/UPDATE/DELETE/DDL/data-modifying-CTE, no write-token | passed (refused) | all six attempts → uniform `502 {"code":"upstream","status":502,"message":"upstream error"}` |
| Query: same mutating statements WITH valid write-token+confirm | passed (still refused) | identical 502 — proves `/api/query` is unconditionally read-only, not merely token-gated (route table carries no `mutating:true` for `/api/query`) |
| Query: lowercase/comment-obfuscated INSERT | passed (refused) | same 502 |
| editCell: text column | passed | `dc_customers.name`, round-tripped exactly |
| editCell: NULL round-trip | passed | `email: null` accepted, stored NULL, read back `null` |
| editCell: int column | passed | `dc_orders.customer_id` 1→2 |
| editCell: numeric/decimal, string-encoded newValue | passed | `"9876543.21"` round-tripped exactly |
| editCell: numeric/decimal, over-precision JSON number | passed (silently rounds) | `555.555` → stored `555.56` (column is `numeric(10,2)`) |
| editCell: boolean, JSON boolean | passed | `true`→`false` round-tripped |
| editCell: boolean, JSON-string `"true"` | passed | accepted, coerced correctly |
| editCell: timestamp, using the API's OWN prior read as expectedOld | **failed** | 409 conflict — see T-AUD-01 |
| editCell: timestamp, using full-precision expectedOld | passed | 200, proves the edit mechanism itself is sound; the read path is the fault |
| editCell: bigint, JSON **number** newValue, value > 2^53 | **failed (silent corruption)** | see T-AUD-02 |
| editCell: bigint, JSON **string** newValue, same value | passed | exact precision preserved |
| editCell: optimistic-concurrency conflict (stale expectedOld) | passed | 409 `{"code":"conflict",...}`, row unchanged |
| insertRow: full row | passed | — |
| insertRow: partial row (omit nullable cols) | passed | `dc_orders` row with only `status` set; `customer_id`/`total` landed NULL |
| insertRow: missing NOT NULL column | passed (refused) | 502 upstream, sanitized (no raw constraint text) |
| insertRow: wrong-type value (text into numeric, string into boolean) | passed (refused) | 502 upstream, sanitized |
| insertRow: JSON number into a text column | passed (refused) | pgx/Postgres rejects (contrast mariadb — §5d) |
| insertRow: response echoes assigned PK | **failed (gap)** | `Applied{statement, affected}` never carries the new row's key — see T-AUD-03 |
| deleteRow: existing PK | passed | `affected:1` |
| deleteRow: non-existent PK | passed | 409 `{"code":"conflict",...}` (same code as edit-conflict — see §5c) |
| PK-less table | not-run | every one of the 14 tables on `db` reports a PK (see §5d) |
| Unsupported action (Stat) | passed | `GET /api/stat` on a tabular service → 422 `{"code":"unsupported",...}` |
| Unknown table/schema | passed | 502 upstream (SQL-level "relation does not exist", sanitized — same code as any other query failure, see §5c) |
| Unknown service | passed | 404 `{"code":"not_found",...}` — cleanly distinguished from unknown-table |
| Malformed cursor | passed (lenient) | silently normalized to offset 0, no error (matches `parseOffset`'s documented fallback) |
| Malformed `segs` | passed (lenient) | silently degrades to `Segments:[]` — see §5c |
| Malformed JSON body | passed | 400 `{"code":"invalid",...}` |

### 1.2 mariadb (mariadb:single@10.6)

| Operation | Verdict | Evidence |
|---|---|---|
| Tree: root → schemas | passed | one node, `"mariadb"` (the connected database, not a MySQL-style multi-schema listing) |
| Tree: schema → tables | passed | 2 nodes: `products`, `users` |
| ReadTable: users | passed | `dataType` reported as bare `"int"`/`"varchar"` (no length/precision — contrast pg's `"character varying"`, see §5d) |
| ReadTable: products | passed | `price` (`decimal`) rendered as JSON **string** `"9.99"` (matches pg's numeric-as-string behavior) |
| ReadTable pagination (users, limit=1, 2 rows) | passed | page1 nextCursor="1"; page2 nextCursor="" (exhausted) |
| Query: valid SELECT | passed | — |
| Query: INSERT/UPDATE/DELETE/DDL, no write-token | passed (refused) | uniform 502 upstream, same envelope shape as pg |
| editCell: text column | passed | `users.name` |
| editCell: NULL round-trip | passed | `email: null` accepted |
| editCell: decimal, string-encoded newValue | passed | exact round-trip |
| editCell: decimal, over-precision JSON number | passed (silently rounds) | `555.555` → `555.56`, identical rounding to pg |
| editCell: optimistic-concurrency conflict | passed | 409 conflict |
| editCell: bigint | not-run | no bigint/int8 column exists anywhere in the mariadb fixture — see §4 Testbed note |
| editCell: boolean | not-run | no boolean-ish column in the mariadb fixture (MariaDB has no true BOOLEAN type; dcseed schema has none anyway) |
| editCell: timestamp | not-run | dcseed's mariadb schema (`users`, `products`) has **no timestamp column at all** |
| insertRow: full row | passed | — |
| insertRow: partial row | not directly tested | (users/products have few nullable columns; NULL round-trip covered via editCell instead) |
| insertRow: missing NOT NULL column | passed (refused) | 502 upstream |
| insertRow: **JSON number into a VARCHAR column** | **passed but flagged** | MariaDB silently coerced `12345` (number) into `email` → stored as string `"12345"`, **no error** — contrast pg's hard rejection of the identical shape. See T-AUD-04 / §5d |
| deleteRow: existing PK | passed | — |
| deleteRow: non-existent PK | passed | 409 conflict |
| PK-less table | not-run | both tables (`users`, `products`) have a PK |
| Unknown schema | passed | 502 upstream (same mechanism as pg's unknown-table) |

### 1.3 ch (clickhouse:single@25.3, view-only)

| Operation | Verdict | Evidence |
|---|---|---|
| Tree: root → schemas | passed | one node, `"db"` |
| Tree: schema → tables | passed | one node, `events` |
| ReadTable: events | passed | `dataType` uses ClickHouse-native names (`UInt64`, `String`, `DateTime`) — no normalization to a common vocabulary (§5d) |
| ReadTable pagination (limit=2, 20 rows, walked to exhaustion) | passed | 10 pages of 2 rows each; `nextCursor` "2","4",...,"18", then empty at page 10; total rows = 20 exactly |
| Query: valid SELECT | passed | — |
| Query: INSERT via query | passed (refused) | 502 upstream |
| Query: ALTER TABLE ADD COLUMN (DDL) via query | passed (refused) | 502 upstream |
| Query: `ALTER TABLE ... DELETE WHERE` (CH's async mutation syntax) via query | passed (refused) | 502 upstream — confirms `readonly=1` connection setting rejects CH's own mutation dialect too, not just standard INSERT/DDL |
| editCell (with **valid write-token + confirm**) | passed (refused) | 403 `{"code":"read_only",...}` — the engine-level `NoEdit`/`Support:view-only` gate fires before any SQL is attempted, independent of the route write-token check |
| insertRow (with valid write-token + confirm) | passed (refused) | 403 read_only, same as above |
| deleteRow (with valid write-token + confirm) | passed (refused) | 403 read_only, same as above |
| Unsupported action (Stat/node) | passed | 422 unsupported |
| PK-less table | not-run | `events` reports `id` as PK (ClickHouse's `ORDER BY id` implies `is_in_primary_key=1`); only one table exists on this engine, so no broader survey possible |

---

## 2. D-04 / D-10 verdict (feeds DD-9)

**D-10 (exact-value transport) — CONFIRMED, and the mechanism is more specific and more
severe than the plan's static citation.**

The plan's citation (`types.go:100-131`) pointed at the *output* shape (`Rows [][]any`)
and framed the risk as a browser/JS `JSON.parse` precision problem. Live testing found the
corruption happens **earlier and entirely server-side**, on the way *in*:

- `server.go`'s `decode()` uses a bare `json.NewDecoder(...).Decode(v)` with no
  `UseNumber()`. `CellEdit.NewValue`/`ExpectedOld` (`any`) and `InsertRow`'s `row
  map[string]any` (`types.go:119-125`) all decode a JSON number into a Go `float64` —
  standard, well-documented `encoding/json` behavior for values destined for an `interface{}`.
- That `float64` is what reaches `database/sql`/pgx as the bound parameter. For a `bigint`
  column, this reintroduces IEEE-754 double rounding *at the server*, independent of any
  browser.

**Exact measured mapping** (insert `attachments.size_bytes bigint`, then read back via
`size_bytes::text` — a raw SQL cast that bypasses the API's own JSON formatting, so this is
ground truth of what Postgres actually stored):

| Sent (JSON number) | Stored (Postgres, exact) | Delta |
|---|---|---|
| `9007199254740992` (2^53) | `9007199254740992` | 0 |
| `9007199254740993` (2^53+1) | `9007199254740992` | **−1** |
| `9007199254740994` (2^53+2) | `9007199254740994` | 0 |
| `9007199254740995` (2^53+3) | `9007199254740996` | **+1** |
| `1152921504606846977` (2^60+1) | `1152921504606846976` | **−1** |
| `4611686018427387905` (2^62+1) | `4611686018427387904` | **−1** |
| `9223372036854775807` (int64 max) | `9223372036854775807` | 0 (coincidental — see below) |
| `9223372036854775806` (int64 max − 1) | `9223372036854775807` | **+1** |

The int64-max row is a trap, not a counter-example: `float64(9223372036854775807)` itself
rounds up to `9223372036854775808` (2^63, one past int64 max). The driver evidently
saturating-clamps an out-of-range-high float back to `math.MaxInt64` before binding, so
`...806` and `...807` both silently collide onto the same stored value `...807`. Verified
by direct computation (`strconv`/Python `float()` — same IEEE-754 double, cross-checked
against the live Postgres result) — see scratchpad `float_check.py` reasoning reproduced
in T-AUD-02 below.

**The fix implied by this evidence is precise, not generic**: sending the value as a JSON
**string** avoids the bug entirely (verified: `"9007199254740993"` round-tripped exactly,
both as an `insertRow` value and as an `editCell` `newValue`) — string-encoding is safe
because Postgres/pgx parse decimal *text* directly for a text-format parameter, never
passing through a float64 intermediate. So DD-9's wire contract should mandate
**string-encoded transport specifically for bigint/int64-range values** (and PKs/rowKey/
expectedOld built from them) — not necessarily for every numeric type (see next point).

**D-10 side-finding — timestamp columns have the same disease via a different mechanism
(new, not in the plan's citation list): T-AUD-01.** `normalize()`
(`tabular.go:512-524`) formats `time.Time` via `t.Format(time.RFC3339)`, which has **no
fractional-second component**. A `timestamptz DEFAULT now()` column reliably carries
microsecond precision (`created_at`-style columns virtually always do), so the value the
API hands back to a caller is *never* what optimistic concurrency needs to match. Live
evidence: inserted row's `created` was actually `2026-07-16 15:52:00.934297+00`
server-side; the API's own read returned `"2026-07-16T17:52:00+02:00"` (zero fractional
seconds). Using that value as `expectedOld` on a subsequent `editCell` produced `409
conflict` (0 rows matched) even though nothing else had changed the row. Using the true
full-precision value as `expectedOld` succeeded immediately (`200`, `affected:1`),
isolating the fault to the read-side serialization, not the edit mechanism. **Net effect:
optimistic-concurrency edits on timestamp columns are permanently broken for any row whose
timestamp doesn't happen to fall on a whole second** — for `now()`-populated columns, that
is effectively always.

**D-04 (SPA always sends `newValue` as a string) — verdict: NOT uniformly a bug; direction
matters per type, and this audit found the *opposite* risk is real too.**

Tested string-encoded `newValue`/insert-values against every available typed column:

- **numeric/decimal**: string-encoded works perfectly on both pg and mariadb (`"9876543.21"`
  round-tripped exactly on both engines).
- **boolean**: string `"true"` accepted and correctly coerced by Postgres.
- **bigint**: string encoding is the *only safe* encoding (see D-10 above) — a JSON
  **number** is what corrupts it, not a string.
- So "D-04 sends everything as a string" is not itself the corruption source for the types
  tested here; if anything, string-encoding is protective for bigint. D-04 remains a real
  code-shape observation (worth confirming against the current `app.js:660` at implementation
  time) but the live evidence does not support treating it as a standalone defect — the
  *actual* risk is a client sending a bare JSON **number** for a bigint-range value, which a
  naive "editable cell shows the number, user edits the number, client re-serializes the
  number" UI flow would do by construction (independent of whatever `app.js:660` currently
  does for other types). DD-9 should specify: transport bigint-range integers as strings
  end-to-end (read *and* write), never as bare JSON numbers.

---

## 3. New defects (T-AUD-nn)

**T-AUD-01 — HIGH — timestamp optimistic-concurrency is broken by the read path's own
lossy serialization.** `internal/dataconsole/console/provider/tabular/tabular.go:519-520`
(`normalize()`, `time.RFC3339`). Evidence: §2 above (exact before/after values, isolated
failing vs. succeeding request). Any table with a `now()`-populated timestamp/timestamptz
column (both `dcseed`'s `dc_customers.created` and the adopted app's
`*_at`/`failed_at`/`created`/`updated_at` columns) is affected. Impact: every legitimate
edit attempt on such a column that follows the normal "read current value → edit → submit
expectedOld" flow will spuriously 409, indistinguishable from a real concurrent-edit
conflict.

**T-AUD-02 — HIGH — bigint (int64-range) values silently corrupt on write, server-side,
independent of any browser.** `internal/dataconsole/console/server/server.go:746-753`
(`decode()`, no `UseNumber()`) feeding `provider.CellEdit.NewValue`/`ExpectedOld` and
`InsertRow`'s `row map[string]any` (`provider/types.go:119-125`). Evidence: the 8-row
mapping table in §2, live-verified via raw-SQL `::text` casts that bypass the API's own
(also-imprecise) JSON formatting. This is D-10's most concrete instance and is worse than
"suspected": it requires no browser, no SPA, and no adversarial input — any bigint value
that happens to exceed 2^53 (file sizes, external IDs, Snowflake-style IDs, etc. — entirely
ordinary data) silently corrupts by ±1 or more on ordinary insert/edit through the audited
API surface.

**T-AUD-03 — MEDIUM — `insertRow` never returns the new row's key.**
`provider/types.go:127-131` (`Applied{Statement, Affected}` — no key/row echo).
Consequence, live-confirmed by necessity during this audit: after every `insertRow` call,
the only way to discover the assigned PK (needed for any subsequent `editCell`/`deleteRow`
on that row) is a separate read filtered on some other unique value the caller happens to
control. For an auto-increment/serial PK with no other unique column in the row (plausible
for many real tables), the caller has **no reliable way** to identify the just-inserted row
at all — not even "read the last row by insertion order" is safe under concurrent writers.
This blocks any "insert then immediately edit" UX (e.g., "add row" followed by inline
editing) from being implemented correctly.

**T-AUD-04 — LOW/MEDIUM — cross-engine type-strictness asymmetry on writes.** Live
evidence: `POST /api/row` with a **JSON number** (`12345`) bound to a VARCHAR column
(`mariadb.users.email`) succeeds silently (MariaDB/its driver coerces to the string
`"12345"`, no error); the identical shape against a Postgres `text` column
(`dc_customers.email`) is hard-rejected with `502 upstream`. A user (or a client bug) that
sends a wrong-typed value gets silently-accepted garbage on one engine and a clean refusal
on another, for the same conceptual mistake. Not a corruption bug per se (mariadb's
coercion is arguably "correct" MySQL semantics), but it means client-side type validation
cannot assume server-side rejection as a backstop uniformly across the tabular family.

**T-AUD-05 — LOW — delete-miss and edit-conflict are indistinguishable.** Both
`DeleteRow` (0 rows affected because the key doesn't exist) and `EditCell` (0 rows affected
because `expectedOld` is stale) return the identical `provider.ErrConflict` → `409
{"code":"conflict"}`. A caller cannot tell "this row was already deleted by someone else"
from "someone else changed a value I was about to overwrite" — both collapse to the same
generic message. `tabular.go:316`, `:370`.

**T-AUD-06 — LOW (UX) — all SQL execution errors collapse to the same generic 502,
including "table not found."** A typo'd table/schema name (`unknown table`/`unknown
schema` in §1.1/§1.2) produces the exact same `{"code":"upstream","status":502,"message":
"upstream error"}` as a NOT NULL violation, a type mismatch, a genuine transient DB error,
or (per the read-only proof in §1) a rejected write. `tabular.go` wraps every
`ExecContext`/`QueryContext` failure identically via `fmt.Errorf("tabular: X: %w",
provider.ErrUpstream)` with no sub-classification. This is a deliberate consequence of the
"never leak raw driver text" design (confirmed sound — no case in this audit leaked a raw
driver string, constraint name, or DSN fragment into a response body) but it does mean the
operator-facing error is maximally uninformative for the single most common real mistake
(typo, or a race where the table gets dropped elsewhere).

---

## 4. UX-relevant observations from payloads

- **Query results carry zero type metadata.** `provider.Column{Name: c}` — `dataType` and
  `pk` are always empty/false for `/api/query` results (`tabular.go:264-267`), unlike
  `ReadTable`. A query-result grid cannot right-align numbers, format booleans, or flag PK
  columns; it also can't offer editing (`rowKeyCols` is always `null` for query results, by
  design — Query is read-only by contract, so this is consistent, just worth the SPA
  knowing not to attempt it).
- **Numeric/decimal values are JSON strings; booleans and integers are native JSON.** Every
  `numeric`/`decimal` column (`dc_orders.total`, `products.price` on both engines) reads
  back as a JSON *string* (`"49.90"`), while `boolean` and `integer`/`bigint` columns read
  back as native JSON `true`/`false`/number. This asymmetry is actually protective (it's
  why decimal precision never broke in this audit) but a naive SPA cell renderer that treats
  "numeric-looking column → render as number, allow numeric input" would need to know this
  column is secretly a string to avoid re-introducing float precision loss on write.
- **`Applied.Statement` echoes the fully-rendered SQL** (quoted identifiers + bound
  placeholders, values redacted as `$1`/`?`) back to the caller on every successful
  mutation. Useful for operator transparency/debugging; also means the console is a
  (harmless, since it's parameterized and the caller already had write access) window into
  exact table/column identifiers and dialect quoting style — not a security issue given the
  caller already has write authority, but worth the SPA deciding whether to surface it.
- **A malformed/stale `segs` or `cursor` query param never surfaces as an error** — both
  silently degrade (`segs` → empty segment list, i.e. "list the root"; `cursor` → offset 0,
  i.e. "page 1"). This is intentional leniency in the code (`parsePath`/`parseOffset`
  discard their parse errors), but it means a client-side bug that corrupts a cursor/segs
  value produces *silent wrong behavior* (quietly jumps to a different tree level, or
  silently restarts pagination at page 1) rather than a visible error the developer would
  notice immediately.
- **ClickHouse column type names are not normalized** (`UInt64`, `String`, `DateTime`) vs.
  Postgres's `information_schema` names (`integer`, `character varying`,
  `timestamp with time zone`) vs. MariaDB's bare names (`int`, `varchar`, `decimal` — no
  length/precision suffix). A shared "type badge" renderer in the SPA has three different
  vocabularies to reconcile, not two.

---

## 5. Cross-engine asymmetries

| Axis | db (postgres) | mariadb | ch (clickhouse) |
|---|---|---|---|
| `dataType` naming | `information_schema` verbose (`character varying`, `timestamp with time zone`, `numeric`) | bare MySQL names, no length/precision (`varchar`, `int`, `decimal`) | ClickHouse-native (`UInt64`, `String`, `DateTime`) — a third vocabulary |
| PK detection | `table_constraints`/`key_column_usage` join | `key_column_usage` filtered on `constraint_name='PRIMARY'` | `system.columns.is_in_primary_key` — semantically PRIMARY KEY *or* ORDER BY key, not the same relational concept as pg/mariadb's PK |
| Numeric-as-string on read | yes | yes | n/a (view-only; not tested for a decimal column — `events` has none) |
| Wrong-type JSON number → text/varchar column | **hard rejected** (502) | **silently coerced** (succeeds, stores stringified value) | not-run (view-only) |
| Boolean-string coercion (`"true"` → boolean) | accepted | not-run (no boolean column in mariadb fixture) | not-run |
| Read-only enforcement mechanism | `sql.TxOptions{ReadOnly:true}` (engine-honoured READ ONLY transaction) | same (`BeginTx` + `ReadOnly:true`) | connection-level `readonly=1` DSN param, no transactions (`supportsReadOnlyTx()==false`) — verified both standard SQL writes *and* ClickHouse's own `ALTER ... DELETE`/`ALTER ... ADD COLUMN` mutation dialect are refused the same way |
| View-only gate | n/a (full support) | n/a (full support) | enforced at `Capabilities.ReadOnly` (`NoEdit:true`), checked first in every provider mutation method — refuses even with a valid write-token, before any SQL executes |
| Error envelope shape on failure | identical `{code,status,message,...}` structure | identical | identical — no engine-specific fields leak through anywhere in this audit |

---

## 6. Epistemics — blocked / not-run, and why

- **bigint editCell/insertRow on mariadb — not-run.** The `dcseed` mariadb schema
  (`users(id INT,...)`, `products(id INT,...)`) has no `bigint`/`BIGINT` column anywhere,
  and DDL is out of scope for this audit (stated non-goal in the program plan). The D-10
  bigint finding (T-AUD-02) is therefore Postgres-only live evidence; it is a server-side
  JSON-decode issue (`server.go`'s `decode()`), not an engine-specific one, so there is
  every reason to expect identical behavior on mariadb's `BIGINT` columns once a fixture
  exposes one — but this audit did not observe it directly on mariadb. **Recommendation for
  S9/testbed**: add a genuine `BIGINT`/`bigint` column to both the postgres and mariadb
  `dcseed` fixtures so this stops depending on the adopted app schema's incidental richness.
- **boolean editCell on mariadb — not-run.** Same root cause (no such column in the
  fixture); MariaDB's `BOOLEAN` is a `TINYINT(1)` alias, so even a fixture fix should
  account for that when asserting "boolean" semantics.
- **timestamp editCell on mariadb — not-run.** Neither `users` nor `products` has a
  timestamp column in the dcseed schema. The T-AUD-01 finding (timestamp truncation) is
  Postgres-only live evidence; the responsible code (`normalize()`'s `time.RFC3339`
  formatting) is engine-agnostic (shared by all three dialects in `tabular.go`), so the same
  defect almost certainly reproduces on mariadb/mysql `DATETIME`/`TIMESTAMP` columns and on
  ClickHouse `DateTime` reads — but this was not directly observed outside postgres.
- **json/jsonb editCell/insertRow — not-run on any engine.** No seeded or adopted-schema
  table on any of the three engines has a JSON/JSONB column. Read-path JSON-value handling
  (e.g. `SELECT '{"a":1}'::jsonb`) was not probed via literal either, since the ground rules
  prioritized the bigint/timestamp round-trips found to be actively broken, and a JSON-typed
  *column* test would still have required DDL to set up meaningfully (a literal-only probe
  would not exercise `ReadTable`'s column-type/PK detection or the edit path at all).
  **Recommendation**: same as above — S9's fixture work should add a jsonb column on
  postgres at minimum.
- **PK-less table (view-only-by-no-PK semantics) — not-run on any engine.** Surveyed all 14
  tables on `db` (dcseed's 2 plus the adopted app's Laravel + kanban tables) and both tables
  on `mariadb`: every one reports a non-empty PK. `ch.events` also reports a PK (ClickHouse's
  `ORDER BY id` satisfies `is_in_primary_key`). No PK-less table exists anywhere in this
  environment to exercise the "RowKeyCols empty ⇒ view-only" code path live.
- **UI/embedded-path mutation verification — out of scope for this pass.** This audit is
  API-only per its brief (plans/dataconsole-audit/tabular.md is the API half of S11); the
  embedded-path screenshot walk is a separate workstream (see `plans/dataconsole-audit/ui-walk.md`).
- **Everything else in the protocol (tree walk, ReadTable, pagination exhaustion on all
  three engines, Query read-only enforcement incl. data-modifying-CTE and write-token-present
  variants, editCell/insertRow/deleteRow on every available typed column, error paths,
  cross-engine asymmetries) is [VERIFIED] live** — see §1 matrices for the full per-operation
  breakdown.
