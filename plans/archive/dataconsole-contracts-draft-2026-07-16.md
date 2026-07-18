# Data Console — family excellence contracts (P2 draft)

Date: 2026-07-16 · Gate: **Karel approves before promotion** · Program:
`plans/dataconsole-excellence-program-2026-07-16.md` §5 (P2) · Evidence base:
`plans/dataconsole-audit/REBASELINE.md` + the five per-family audit files.

**What this is.** Six family excellence contracts — the durable definition of what
"excellent" means for each managed-service family, fixed from the live audit, for
Karel to review before promotion into `docs/spec-dataconsole.md` as a new **§7**.
Each contract states target *invariants* (what must be true), not implementation
(no slices, no code); some invariants are already met by landed work, some are not
— the conformance suite is what proves each, red or green.

**Promotion note (for the promoter, not part of the contract).** Inserting §7
shifts the existing spec §7 Install → §8, §8 Security → §9, §9 Non-goals → §10.
The value-fidelity wire contract (DD-9) and the presentation contract (DD-4) are
P3 design records that land alongside this at promotion; where a clause depends on
them it says so and does not pre-empt the decision.

---

## Preamble — the shared canon every family inherits

### P.1 The four systemic invariants (cross-family; each family states only its deltas)

The audit's dominant theme was not UX unevenness but **operations that reported
success while destroying, losing, or never applying data** (REBASELINE §2). These
four invariants bind every family; a family section restates only where its engines
diverge.

- **I-1 — Success never lies (accepted / applied / visible).** A `200` / `{"ok":true}`
  MUST mean the write is *applied* — durably accepted by the engine such that a
  subsequent authoritative read returns it — or MUST carry an explicit non-applied
  status (`accepted`-not-confirmed, or a typed error). It MUST NOT be returned on
  mere enqueue (DOC-AUD-01), on an idempotent no-op that changed nothing
  (OBJ-AUD-02, KV-AUD-06), or on a write that silently corrupted the value
  (T-AUD-02) or landed somewhere the caller did not name (D-05). *Applied* and
  *visible* are distinct where an engine's read-after-write lags (§P.4); the
  contract for each family states which of its reads are authoritative.

- **I-2 — Errors are honest and structured (T-2).** Every error — including the
  auth 401 (KV-AUD-08) — MUST be the JSON envelope
  `{code, status, message, service, family, action, requestId}`, carrying the
  shared sentinel `code` (§P.3) and populated `service`+`family` on *every* route,
  body-addressed mutations included (KV-AUD-04). Distinct conditions MUST map to
  distinct sentinels: a validation rejection is not an outage (DOC-AUD-02), a
  missing resource is not "unsupported" (KV-AUD-09), and the same condition across
  sibling engines maps to the same sentinel (STR-AUD-02). Messages stay sanitized —
  the raw driver cause never crosses the wire (spec §8.1).

- **I-3 — Values keep their fidelity (T-3 = DD-9).** A value read, displayed, and
  written back MUST round-trip exactly. The concrete rules live in the DD-9
  value-fidelity wire contract (P3), applied per family in each §Value fidelity:
  bigint/int64 as string end-to-end (T-AUD-02), timestamp full sub-second precision
  (T-AUD-01), S3 content-type carried on write (OBJ-AUD-01), no-TTL sentinel not
  `0` (KV-AUD-02), insert-key echo (T-AUD-03), truncated-blob true size
  (KV-AUD-05). This section references DD-9; it does not re-derive it.

- **I-4 — Create and identity are explicit and immutable (T-4).** A family that can
  create MUST offer an explicit, honest create path (KV cannot create a collection
  at all today — KV-AUD-03). Identity is immutable during an edit: a Save MUST NOT
  silently re-identify, relocate, or duplicate a record (D-05); a path/payload
  identity mismatch is refused, never guessed (DD-6). Re-identify/duplicate, where
  wanted, is a separate explicit op, never hidden inside Save.

### P.2 The contract IS the test list

Every normative **MUST** in a contract is an acceptance criterion, and every family
closes with a **Conformance cases** list that indexes them by finding ID. That list
is the family's slice of the per-engine conformance suite: a case exists for each
MUST, boundary values included (I-3). "Support tier" is therefore *earned by the
suite passing*, never asserted (program §2). A capability with no green case is not
"supported" — it is a claim.

### P.3 Shared sentinel taxonomy (the honest-error vocabulary)

The one typed error set (`provider/errors.go`), status + public code, referenced by
every family's §Error contract. It is the whole vocabulary — a family maps its
conditions onto these, never inventing a flat catch-all.

| Sentinel | HTTP | code | Fires when |
|---|---|---|---|
| `ErrInvalid` | 400 | `invalid` | Malformed request (bad JSON body, unparseable input) |
| `ErrReadOnly` | 403 | `read_only` | Write refused: no write token, view-only tier, or wrong token (no oracle — spec §5.1) |
| `ErrNotFound` | 404 | `not_found` | Addressed resource does not exist |
| `ErrNeedsConfirm` | 409 | `needs_confirm` | Write token present, `X-Confirm` absent |
| `ErrConflict` | 409 | `conflict` | Optimistic-concurrency miss (0 rows matched `expectedOld`) |
| `ErrWrongType` | 409 | `wrong_type` | Write would overwrite a different-shaped value (cross-type clobber) |
| `ErrTooLarge` | 413 | `too_large` | Body over the edit/transport guard |
| `ErrUnsupported` | 422 | `unsupported` | Operation not applicable to this family/shape (e.g. edit a no-PK table) |
| `ErrUpstream` | 502 | `upstream` | Sanitized upstream failure (validation *and* outage today — I-2 requires splitting these per family) |
| `ErrUnreachable` | 503 | `unreachable` | Genuine private-network reachability failure → VPN gate |
| `ErrTimeout` | 504 | `timeout` | Upstream accepted the request but did not confirm completion in time (async) |

### P.4 Shared state canon (one honest rendering each — fixes U-10/U-02)

Six states, one canonical rendering apiece. A family section names only the states
with family-specific content. Today these render three-ways-for-empty, silence for
read-only, and inline/toast/VPN split for errors (U-10, U-02); the canon collapses
each to one.

| State | Trigger | One canonical rendering |
|---|---|---|
| **loading** | request in flight (tree expand, table page, blob open) | a skeleton/spinner in the target pane; never a blank pane (U-10: tree-expand has no loading state today) |
| **empty** | valid container, zero children / table, zero rows | one empty-state ("Empty" / "No rows"), distinct from loading and error; the `nodes:null` vs `[]` wire difference is normalized before render (object-stream.md §5) |
| **error** | any sentinel (§P.3) | one inline error surface in the pane carrying the public message + `service`/`family`; a mutation may *also* toast, but a toast is never the sole error channel (U-10) |
| **unreachable-VPN** | `ErrUnreachable` (503) | the VPN gate card (`vpnGateReason`), shown ONLY on genuine reachability failure — never on a bad credential, which is `upstream` (errors.go `IsNetUnreachable`) |
| **read-only-posture** | caller has no write token (standalone tab, or unarmed session) | mutation affordances rendered but disabled with the posture reason; the rail's `read-only` badge (ui-walk.md). One rendering — never "cells silently don't respond" (U-02) |
| **view-only-no-key** | a specific opened table/collection whose `RowKeyCols` is empty (PK-less table, redis list) | cells locked + no row-delete button + a *visible* "view-only: no row key" label on that grid — distinct from posture and from the view-only tier (U-02, D-03) |

A **service-level view-only tier** (clickhouse / qdrant / stream) is distinct again:
a rail `view` badge on the whole service, no mutation affordances anywhere in it
(§Honest labels per family). Three different "cannot edit" conditions, three visibly
different renderings — never one shared silence.

### P.5 Shared presentation note (feeds DD-4)

Each §Canonical view states what server-declared metadata the family's tree/grid
MUST surface. The *mechanism* (typed `NodeMeta`, one grid renderer, one meta-chip
renderer, pagination canon) is DD-4's to finalize; these contracts pin the
*information* that must reach the user, not the widget. Where a provider already
emits a field the UI drops (object `modified`/`etag` — object-stream.md §3; KV redis
`type` — KV-AUD-07), surfacing it is a view MUST, not a new capability.

---

## §7.1 Tabular — postgresql, mariadb (full) · clickhouse (view-only)

**Canonical operations.** Browse (schemas → tables), arbitrary read-only SQL, and —
full tier only — cell edit, row insert, row delete, each optimistic-concurrency
guarded.

| Operation | pg / mariadb (full) | clickhouse (view-only) |
|---|---|---|
| Tree: schemas → tables | ✓ | ✓ |
| ReadTable (grid, offset pagination) | ✓ | ✓ |
| Query (engine-enforced read-only) | `READ ONLY` tx | `readonly=1` DSN |
| editCell / insertRow / deleteRow | ✓ | refused `read_only` (403), engine-gated before any SQL (tabular.md §1.3) |

Per-engine deltas the contract accepts as honest, not bugs: `dataType`
vocabulary is three distinct dialects (pg `information_schema` verbose / mariadb
bare / clickhouse-native `UInt64`) — a shared type badge reconciles three
vocabularies, not two (tabular.md §5). Numeric/decimal reads back as a JSON
*string* on pg+mariadb (protective for precision — §Value fidelity). Wrong-typed
writes are refused strictly on pg but silently coerced on mariadb (T-AUD-04) — the
contract does NOT promise uniform server-side type rejection across the family;
client validation cannot assume it. **mysql** is not a Zerops managed type (DD-8);
the family's mysql ceiling is dialect-parity via the shared `myDialect`, and spec §6
should say so — no live mysql conformance case can exist.

**Canonical view.** Tree: schema = `container`, table/view = `tabular`. Grid: the
provider's `Column{name, dataType, pk}`, PK column marked (`🔑`), row count shown
(the reference view — ui-walk.md). The Query result grid MUST reach parity with the
ReadTable grid's presentation, with one honest exception it MUST signal: query
results carry no `dataType`/`pk` and `rowKeyCols` is always null (Query is
read-only by contract), so a query grid MUST render as explicitly non-editable, not
as an editable grid that silently ignores clicks (tabular.md §4, U-01).

**Canonical states.** All six apply. **view-only-no-key**: a PK-less table (empty
`RowKeyCols`) renders as a locked, labelled grid — not observed live (every testbed
table had a PK) but a first-class rendering the contract requires (tabular.md §6).
**view-only tier**: clickhouse wears the service `view` badge; its refusal is
engine-level and fires even with a valid write token (tabular.md §1.3).

**Mutation-state semantics (I-1).** pg + mariadb writes are **synchronous and
transactional** — a `200` means *applied and committed*; *applied* ≡ *visible*
immediately (no async lag). No "Saved" is shown before the committing response
returns. clickhouse is view-only *because* its mutations are async (spec §6) — the
family does not expose an async-write state at all.

**Value fidelity (I-3 = DD-9).** The tabular slice, the DD-9 heartland:
- **bigint / int64** transported as a **string** end-to-end (read, edit `newValue`,
  `expectedOld`, `rowKey`, insert values). A bare JSON number corrupts by ±1 above
  2^53 server-side, before any browser (T-AUD-02, D-10 — measured 8-value table in
  tabular.md §2). String transport round-trips exactly.
- **timestamp** serialized at full sub-second precision; RFC3339-without-fraction
  makes every `now()`-column's `expectedOld` un-matchable → spurious 409 on every
  such edit (T-AUD-01).
- **insertRow** echoes the assigned key in `Applied` so insert-then-edit is possible
  for serial/identity PKs with no other unique column (T-AUD-03).
- decimal-as-string is preserved as-is (a renderer treating a numeric-looking column
  as a JS number would re-introduce the loss it avoids — tabular.md §4).

**Error contract (I-2).** Distinct conditions the family MUST keep distinct:
optimistic-conflict (`conflict`) vs read-only refusal (`read_only`) vs unsupported
shape (`unsupported`, e.g. edit on a no-PK table) vs upstream failure (`upstream`).
Two known collapses to resolve: table-not-found currently reads as generic
`upstream` alongside real outages (T-AUD-06), and delete-miss vs edit-conflict both
surface as `conflict` (T-AUD-05) — see Open Questions for whether these are worth
new sentinels or accepted as-is. Envelope carries `service`+`family` on the
body-addressed edit/insert/delete routes (KV-AUD-04, cross-family).

**Create / identity (I-4).** insertRow is the create path; it MUST echo the new
key (above). Cell edit keys on the immutable `rowKey`; editing a value never
re-identifies the row. Editing a **PK column** is distinct from document
id-immutability (DD-6) and is decided separately — the SQL PK edit semantics are an
open question, not folded into DD-6.

**Honest labels + non-goals.** pg/mariadb = full; clickhouse = view-only (honest
`view` badge, browse+query only). No DDL (create/drop/alter tables or indices) —
program non-goal. No cross-engine type-strictness guarantee (T-AUD-04). jsonb column
handling unverified (no fixture — tabular.md §6); a conformance case requires a jsonb
fixture before the family claims it.

**Conformance cases.** (1) bigint string round-trip at 2^53±k, insert+edit+delete,
pg [+mariadb once a bigint fixture exists] — T-AUD-02. (2) timestamp full-precision
read → same-value edit succeeds, `now()` column — T-AUD-01. (3) insertRow echoes
key; insert-then-edit by echoed key — T-AUD-03. (4) clickhouse edit/insert/delete
refused `read_only` even with a write token — tabular.md §1.3. (5) PK-less table
renders view-only-no-key — tabular.md §6. (6) query grid renders non-editable —
U-01. (7) error envelope carries service+family on edit/insert/delete — KV-AUD-04.
(8) conflict vs read_only vs unsupported vs upstream each distinct on their trigger —
I-2. (9) [open] table-not-found distinguishable from outage — T-AUD-06.

---

## §7.2 KV — valkey (full)

**Canonical operations.** Browse a virtual `:`-delimited key tree; per key: Stat
(type + TTL), read a string value, read a collection as a grid, edit one collection
entry, set/clear TTL, delete the whole key; create/overwrite a string value.
Single engine — deltas are **per redis type**, not per engine:

| Type | Grid columns | Entry edit | Entry delete | Notes |
|---|---|---|---|---|
| string | (blob, not a grid) | WriteBlob (SET) | whole-key delete | type-guarded against clobber (below) |
| hash | field / value | HSET on `value` | HDEL | `field` (key) column locked (D-02 fix) |
| list | index / value | LSET works server-side but **UI-locked** | unsupported | view-only-no-key: no safe row identity (D-03) |
| set | member | SREM+SADD (rename) | SREM | member is the key |
| zset | member / score | ZADD on `score` | ZREM | score edit; value-only write is refused (below) |

**Canonical view.** Tree: virtual `:` grouping = `container`; a collection
(hash/list/set/zset) = `tabular`; a string = `blob`. The tree MUST surface the redis
**type** per node — today all four collection types collapse to one `tabular` kind
and are visually indistinguishable until opened (KV-AUD-07, UI-AUD-04, U-03). Each
key MUST show a **TTL chip** reading its real state (§Value fidelity). Grid columns
per type as tabled above; `RowKeyCols` drives editability.

**Canonical states.** All six. **view-only-no-key**: a redis **list** is the live
example — empty `RowKeyCols`, every cell locked, no delete button, labelled (D-03,
kv.md §2a; a correct-by-omission choice today, but the *label* is the contract
delta). **read-only-posture**: the KV write-auth matrix is airtight (20/20 refusals,
kv.md §1) — the posture rendering is the only gap, not the enforcement.

**Mutation-state semantics (I-1).** Valkey commands are **synchronous** — a `200`
means applied. Two silent-success defects the contract forbids:
- WriteBlob (string SET) MUST refuse a cross-type overwrite of an existing
  hash/list/set/zset (`wrong_type`, 409) rather than clobbering the collection into
  a string — the CRITICAL live data-loss finding (KV-AUD-01, destroyed a
  `leaderboard` zset).
- zset SetEntry with a value but no score MUST refuse rather than silently writing
  score 0 (KV-AUD-10).
- `Applied.Affected` MUST carry the real command reply count, never a hardcoded 1 —
  a DeleteEntry of a nonexistent field must not report `affected:1` (KV-AUD-06).

**Value fidelity (I-3 = DD-9).** The KV slice:
- **no-TTL sentinel**: a key with no expiry MUST report a negative/null `ttlSeconds`,
  never `0` — today every never-expiring key (the common case) shows a false
  "TTL: 0s" (KV-AUD-02, cross-confirmed UI-AUD-02).
- **truncated-blob true size**: a truncated value MUST report its real byte size, not
  just a one-bit `Truncated` flag — the gap between shown and actual can reach ~63
  MiB with no signal (KV-AUD-05).
- binary string values round-trip byte-exact (verified incl. NUL — kv.md §1); no
  change needed there.

**Error contract (I-2).** Conditions the family MUST keep distinct: not-found
(`not_found`) vs unsupported-shape (`unsupported`) — today ReadTable on a missing key
returns `unsupported` where Stat correctly returns `not_found` (KV-AUD-09);
cross-type clobber (`wrong_type`) vs read-only (`read_only`); the auth 401 MUST be
the JSON envelope, not `text/plain` (KV-AUD-08). Body-addressed mutation envelopes
MUST carry `service`+`family` (KV-AUD-04 — first found here, fixes all families).

**Create / identity (I-4).** The family's sharpest T-4 gap: there is **no
collection-creation path at all** — `SetEntry` type-dispatches on the key's *current*
type, so a nonexistent key is `unsupported`, and a collection destroyed or deleted
cannot be recreated in-console (KV-AUD-03). The contract requires an explicit
create path per collection type (key name + type + first entry, TTL/collision
semantics per Open Questions), and the whole-key **collection delete** affordance the
server already supports but the UI never wires (D-08, kv.md §1). Entry identity: the
key column (hash field / set member / zset member) is immutable during a value/score
edit (the D-01/D-02 UI locks); a rename is the explicit SREM+SADD set op, never a
silent field-name overwrite.

**Honest labels + non-goals.** valkey = full (keydb classified but off-platform).
Redis **list** stays edit-locked and delete-unsupported by design (no safe entry
identity — D-03); the contract does not promise list editing. No pub/sub, no stream
(XADD) type, no server-side scripting.

**Conformance cases.** (1) WriteBlob on hash/list/set/zset refused `wrong_type`,
collection intact — KV-AUD-01. (2) zset value-only-no-score refused — KV-AUD-10. (3)
`Applied.Affected` = real reply count; delete-missing-field ≠ `affected:1` —
KV-AUD-06. (4) no-TTL key reports negative/null sentinel, renders "no expiry" —
KV-AUD-02. (5) truncated value reports true size — KV-AUD-05. (6) missing key →
`not_found` on both Stat and ReadTable — KV-AUD-09. (7) 401 is JSON envelope —
KV-AUD-08. (8) mutation envelope carries service+family — KV-AUD-04. (9) collection
create path exists per type — KV-AUD-03. (10) whole-key collection delete wired —
D-08. (11) tree node carries redis type; list renders view-only-no-key — KV-AUD-07,
D-03.

---

## §7.3 Object — object-storage / S3 (full)

**Canonical operations.** Browse a prefix tree; per object: Stat
(size/contentType/etag), read (head-sliced with inline preview), write, multipart
upload, rename, delete. Single engine — no per-engine deltas; the deltas are
**prefix (folder) vs leaf** semantics.

| Operation | Leaf (real S3 key) | Prefix (folder-shaped path) |
|---|---|---|
| Read / Stat | ✓ | 404 (no object literally named the prefix) |
| Write / Upload | ✓ | n/a |
| Rename | copy+delete, single key | 404 (correct — copy errors on missing source) |
| Delete | stat-then-remove, honest 404 on missing | honest 404 (not silent success) |

**Canonical view.** Prefix = `container`, object = `blob`. The richest family view
today — inline `<img>` for images, size chip on tree rows (ui-walk.md, the
presentation bar the others must meet). It MUST also surface the provider's
`modified` and `etag`, both emitted on the wire and both dropped by the UI today
(object-stream.md §3, U-03) — `etag` is the only stable version/identity hint the
family exposes. On blob open: `contentType` drives the image/text/binary branch and
`truncated` drives the head-slice badge.

**Canonical states.** loading / empty / error / read-only-posture apply.
**unreachable-VPN does NOT apply** — object is the one full family the UI does not
VPN-gate (`vpnGateFamily` excludes object; actions.go). No view-only-no-key (objects are
addressed by key, always). The blob viewer's binary branch ("Binary — use Download")
is a legitimate content state, but it MUST NOT be reachable for content the console
itself just wrote as text/image (see I-1/OBJ-AUD-01 below).

**Mutation-state semantics (I-1).** S3 PUT is synchronous — a `200` means applied.
Two honesty rules:
- **Delete** MUST report whether anything was deleted: stat-then-remove so a missing
  key returns `not_found`, not a `{"ok":true}` indistinguishable from a real
  deletion (OBJ-AUD-02, and it matches Rename's existing behavior on the same
  condition). Accepts a check-then-act race window, same as Rename's copy-then-remove.
- **Rename** is copy-then-delete (S3 has no atomic rename) — a partial failure
  (copy succeeds, delete fails) is possible and MUST surface honestly rather than
  reporting a clean rename; the two-step nature is a real accepted/applied nuance for
  this family (U-14).

**Value fidelity (I-3 = DD-9).** The object slice is the **content-type carry**:
console-originated writes MUST set the S3 Content-Type — from the multipart part's
declared MIME on upload, or sniffed/threaded on `PUT`. Today neither write path sets
it, so every console-written object degrades to `application/octet-stream`: an
uploaded image loses its inline preview, and a text file edited+saved once becomes
un-editable/un-previewable on next open ("Binary — use Download") — a
first-workflow, self-inflicted break (OBJ-AUD-01, HIGH). This requires the one
`WriteBlob` provider-interface signature change in DD-9 (carry content-type),
promoted before it is built against. Bytes themselves round-trip exact today
(verified `cmp`-clean — object-stream.md §1).

**Error contract (I-2).** Distinct conditions: not-found (`not_found`) on
missing/leaf and on prefix; read-only (`read_only`) under posture; malformed
multipart/JSON (`invalid`). Malformed `segs` MUST NOT silently degrade to a root
listing (OBJ-AUD-03) — it masks a client bug as ordinary output; it should surface
`invalid` (low severity, cross-family behavior in `parsePath`). Download-hardening
headers stay uniform (verified — object-stream.md §1; spec §8.1).

**Create / identity (I-4).** Write/upload to a new key is the create path — no
identity ambiguity (the key IS the identity). "New folder" is **false semantics**:
S3 prefixes are not objects, so a create-folder affordance MUST NOT pretend to make
an empty directory (DD-5). Rename is the explicit re-identify op (copy+delete);
identity is never mutated implicitly.

**Honest labels + non-goals.** object-storage = full. No folder-wide (recursive
prefix) rename or delete — single-key only, and folder-shaped paths are refused
honestly, not silently no-op'd (D-07 → OBJ-AUD-02). No empty-folder creation
(prefix semantics). No bucket-level admin.

**Conformance cases.** (1) console write/upload sets content-type; image previews +
text stays editable on re-open — OBJ-AUD-01. (2) delete of missing/already-gone key
→ `not_found`, not `{"ok":true}` — OBJ-AUD-02. (3) rename partial-failure surfaces
honestly — U-14. (4) tree surfaces size+modified; blob Stat surfaces etag — U-03,
object-stream.md §3. (5) malformed `segs` → `invalid`, not root listing —
OBJ-AUD-03. (6) folder-shaped rename/delete refused honestly, children untouched —
D-07. (7) unreachable-VPN state does not render for object — actions.go
`vpnGateFamily`.

---

## §7.4 Document — elasticsearch, meilisearch, typesense (full) · qdrant (view-only)

**Canonical operations.** Browse containers (index / collection) → documents by id;
read a document as pretty JSON; upsert and delete by id (full tier). qdrant is
view-only (vectors are not human-editable). Bounded search is a **planned** op, not
yet present (DD-2, U-13).

| Operation | es (full) | meili (full) | typesense (full) | qdrant (view-only) |
|---|---|---|---|---|
| Tree / List (paginate) | `from` offset | `offset` | 1-based `page` | opaque scroll token |
| ReadBlob (doc by id) | ✓ | ✓ | ✓ | ✓ (includes full `vector[]`) |
| Write / Delete (upsert by id) | ✓ | ✓ (async) | ✓ (sync) | refused `read_only` |
| Search / filter | none yet (DD-2) | none yet | none yet | payload-filter deferred (DD-2) |

The four pagination mechanics are genuinely different (document.md §6); a future
search contract cannot assume one cursor shape. **Upload** works server-side for the
document family though the UI never offers it (`WriteBlob` type-assertion is shared —
document.md §6): the contract MUST decide it is either a real feature or excluded at
the `Caps()`/provider level, not merely hidden client-side (Open Questions).

**Canonical view.** Container = `container`, document = `blob`. A document renders as
pretty JSON preserving the engine's own key order (`json.Indent`, never remarshalled
— document.md §6). **qdrant**: a single-point read dumps the full `vector[]` inline —
fine at 4 dims, a wall of numbers at real 384/768/1536-dim embeddings (UI-AUD-03,
promoted [VERIFIED] by document.md §6); the vectors view MUST summarize (dimension
count + payload) and collapse the raw vector. These engines are search engines with
**no search affordance** — the contract acknowledges browse-by-paging-ids as the v1
floor and names bounded search as the known next capability (DD-2), not a silent gap.

**Canonical states.** All six except view-only-no-key (documents are id-addressed).
**view-only tier**: qdrant wears the service `view` badge; writes refused
`read_only` with data verified unmodified (document.md §1). unreachable-VPN applies
(document family is VPN-gated — actions.go).

**Mutation-state semantics (I-1) — the family's defining contract.** The engines
differ fundamentally in write timing (measured, document.md §5):

| Engine | Write model | `200`/`ok:true` means | Applied-visible lag |
|---|---|---|---|
| es | sync index + near-real-time search | applied to primary (translog-durable) | ReadBlob ~0 ms; **List lags 0–750 ms** (refresh-cycle-bound) |
| meili | async task queue | **enqueued only** — MUST be resolved to applied | everything async ~0.9–1.35 s (floor, not ceiling) |
| typesense | fully synchronous upsert | applied | ~0 ms |
| qdrant | read-only | n/a | n/a |

Rules:
- **meili**: the console MUST poll the task to a terminal state before reporting
  success — a `2xx` on enqueue is not applied; a task that fails meili's async
  validation MUST surface as a typed error (`invalid`), and a task unconfirmed within
  the poll budget MUST surface as `timeout` (504), never a false "Saved". Without the
  poll, a rejected document vanishes with no error at any layer — the standout
  finding (DOC-AUD-01, HIGH).
- **es**: the open blob view (real-time `GET`) is authoritative and instant; the
  **tree/List** can read stale for up to a refresh interval. The UI MUST NOT present
  a just-written document as missing (or a just-deleted one as present) in the tree
  while the authoritative read disagrees — *applied* (blob) and *visible* (list) are
  distinct and the UI reflects the authoritative one (document.md §5).
- **typesense**: synchronous; `200` ≡ applied ≡ visible.

**Value fidelity (I-3 = DD-9).** The document slice is smaller (values are
engine-serialized JSON, preserved verbatim). Truncation honesty: a read over
`MaxInlineBytes` (16 MiB) is a raw byte-slice of pretty JSON → **invalid JSON** at a
200 status, signalled only by a header (D-06, DOC-AUD-03). The contract requires a
truncation that is either valid-JSON-safe (boundary-aware) or fetch-full-on-edit, and
never a silent 200 with a malformed body. Note the ES write ceiling (~10.6–12.5 MB)
sits below the 16 MiB read cap, so ES can never itself write a document large enough
to hit D-06 (DOC-AUD-03) — whether `MaxInlineBytes` is per-engine is an Open Question.

**Error contract (I-2) — the worst T-2 offender.** Today *every* non-2xx upstream
(400 validation, 409, 413, 500) flattens to one generic `502 upstream`, so a
"fix your payload" is indistinguishable from "the service is down"; and the console's
own 64 MiB body cap truncates via `io.LimitReader` → a `400 invalid` where the typed
`413 too_large` already exists (DOC-AUD-02). The contract requires: upstream status
*classes* mapped to distinct sentinels (validation → `invalid`/`unsupported`, not
`upstream`); over-cap bodies → `413 too_large`; and the meili async-failure
distinction above. Envelope carries `service`+`family` (KV-AUD-04).

**Create / identity (I-4) — id-routing, broken three ways (D-05, worse than
"duplicate").** The DD-6 invariant — *path identity is immutable during Save;
path/payload identity mismatch is refused* — is violated by all three writable
engines, differently (document.md §3): ES accepts+ignores a body `id` (stores at the
path) but rejects a reserved `_id`; **meili silently misroutes** to the body's
primaryKey (path 404s) or **silently drops** a document with no primaryKey (invisible
loss); **typesense silently misroutes** to the body id, or **auto-assigns** its own
id when omitted (e.g. `"5"`), echoed nowhere. The contract requires: a Save resolves
and honors the *path* id; a mismatching payload id is refused, not guessed; an
engine-assigned id (typesense) is echoed back. Duplicate/copy-to-new-id is a separate
explicit op. Document **create** (new id) semantics — overwrite vs conflict, per
engine async — is scoped by DD-5.

**Honest labels + non-goals.** es/meili/typesense = full; qdrant = view-only. No
search/filter in v1 (named, not hidden — DD-2). No vector editing. No index/collection
(schema) creation or mapping edits. Upload is not an advertised document op pending
the Open Question.

**Conformance cases.** (1) meili write polls to terminal; task failure → `invalid`,
unconfirmed → `timeout`, never false success — DOC-AUD-01. (2) es list-lag: UI shows
authoritative blob read, not stale tree — document.md §5. (3) path/payload id
mismatch refused on es/meili/typesense; typesense auto-id echoed — D-05/DD-6. (4)
qdrant write refused `read_only`, data unmodified; vector collapsed in view —
document.md §1, UI-AUD-03. (5) upstream classes mapped distinct (validation ≠ outage)
— DOC-AUD-02. (6) over-cap body → `413`, not `400` — DOC-AUD-02. (7) truncated read
is valid-JSON-safe or fetch-full — D-06. (8) envelope carries service+family —
KV-AUD-04. (9) [open] bounded search contract — DD-2. (10) phantom-next empty
terminal page tolerated by UI — DOC-AUD-04.

---

## §7.5 Stream — kafka, nats (view-only)

**Canonical operations.** Browse topics/streams; read a **metadata summary** per
topic/stream. No message consumption, no mutation. All writes refused (verified
20/20 across both engines — object-stream.md §5).

| Operation | kafka (`events`) | nats (`queue`) |
|---|---|---|
| Tree: list topics/streams (root) | ✓ (leaf per topic) | ✓ (leaf per stream) |
| List below root | short-circuits empty (no deeper browse) | same |
| Read summary (`ReadBlob`) | `{topic, partitions, partitionIds}` | `{stream, messages, firstSeq, lastSeq, bytes, subjects}` |
| Any mutation | refused | refused |
| Message peek | deferred (DD-3) | deferred (DD-3) |

**Canonical view — the family's defining fix.** A topic/stream summary MUST render
as a **labelled metadata card** (partitions / message count / subjects / seq range;
consumer-group counts best-effort with an explicit "unavailable" — DD-3), NOT as a
JSON `<pre>` byte-indistinguishable from a stored document (U-04, confirmed
object-stream.md §4 / ui-walk.md). Today the summary hits the exact same blob branch
as a real document, the only "this is not content" signal is the *absence* of a Save
button, and "messages: 30" shows with zero messages viewable — metadata masquerading
as content. The card MUST state plainly it is generated metadata, not stored data.
The `subjects` wildcard (`events.>`) MUST render literally, not unicode-escaped
(`events.>` — UI-AUD-02). Tree nodes carry no meta today (the barest in the
console — object-stream.md §4); the card is where the numbers belong.

**Canonical states.** loading / empty / error / unreachable-VPN apply (stream is
VPN-gated — actions.go). **read-only-posture and view-only-no-key do not apply as
edit states** — the whole family is the service-level **view-only tier**: a `view`
badge, no mutation affordance anywhere, by nature.

**Mutation-state semantics (I-1).** None — the family never writes. The refusal is
uniform and airtight; the only cleanup is cosmetic (the two internal refusal codes,
`read_only` for implemented-but-hardwired-RO methods vs `unsupported` for
never-implemented ones, are invisible to the SPA since no mutation is ever advertised
— object-stream.md §5; normalizing to one "view-only" sentinel is optional polish).

**Value fidelity (I-3).** The summary numbers MUST be the true upstream state
(verified — the console reports real partition/message counts even when inflated by
repeated seed runs; object-stream.md §4). No DD-9 value-transport concern (no
editable values). `subjects` rendering is the one fidelity item (UI-AUD-02).

**Error contract (I-2).** The family's T-2 fix is **sibling parity**: a nonexistent
topic/stream MUST map to the same sentinel on both engines — today nats correctly
returns `not_found` (404) but kafka returns `upstream` (502) for the identical
condition (STR-AUD-02). And since topic auto-create is disabled, a nonexistent-topic
read is a permanent client-caused condition, not a transient outage — mapping it to
`upstream` gives false "retry later" framing. **Stat** MUST verify existence: today
it returns 200 for a fabricated topic/stream, never calling upstream (STR-AUD-01) —
latent on the raw API (the SPA's only Stat caller is TTL-gated, which stream never
advertises) but a contract violation of I-1's "success means real".

**Create / identity (I-4).** None — no create, no identity mutation. Out of scope
by nature.

**Honest labels + non-goals.** kafka/nats = view-only. **rabbitmq** is
platform-deprecated (in favor of NATS) → not-yet, forever; offline classification
pins suffice (DD-8, program §1.1). **Message peek** (non-consuming reads, no durable
consumers, partition/sequence + byte/message caps, binary handling) is a separate
future decision and slice, explicitly NOT promised here (DD-3). The stale
`types.go:23` "no provider (dropped)" comment is factually wrong (provider is live)
and must be corrected (D-09).

**Conformance cases.** (1) topic/stream renders as a labelled metadata card, not a
document `<pre>`; states "generated metadata" — U-04. (2) `subjects` wildcard renders
literally — UI-AUD-02. (3) nonexistent topic/stream → same sentinel (`not_found`) on
kafka and nats — STR-AUD-02. (4) Stat verifies existence (no 200 for a fabricated
name) — STR-AUD-01. (5) all mutations refused on both engines — object-stream.md §5.
(6) rabbitmq classified not-yet — DD-8.

---

## §7.6 File / Unknown — honesty note (not-yet)

Not a driven family — a **classification honesty** contract. `shared-storage`
(`FamilyFile`) is mount-only with no network endpoint, and anything the platform
adds that the classifier does not recognize falls to `FamilyUnknown` (family.go).
Both are `SupportNotYet`.

**The whole contract:** a not-yet service MUST be surfaced honestly — labelled
"not yet supported", advertising **zero** read or mutating affordances
(`familyReadActionIDs`/`familyMutatingActionIDs` return nil; actions.go) — and MUST
NOT be mis-rendered as browsable or editable (spec §2 invariant 6, §6). A new
platform type appearing here is the signal to run the onboarding loop (classify +
provider + seeder + conformance suite), never to fake support. There is no view, no
state beyond the not-yet label, no mutation semantics, no value transport, and no
create/identity surface — by design.

**Conformance cases.** (1) `shared-storage` → `FamilyFile`, `SupportNotYet`, zero
actions — family.go. (2) an unclassified type → `FamilyUnknown`, `SupportNotYet`,
zero actions, not mis-rendered — spec §2.6, §6. (3) a not-yet service exposes no
enabled read or write action in `/api/services` — actions.go.

---

## Open questions for Karel (resolve before promotion)

1. **DD-9 wire shape is P3, referenced here.** These contracts cite DD-9's rules
   (string bigint, full-precision timestamp, content-type carry, no-TTL sentinel,
   insert-key echo, true truncated size) but do not fix the DTO shape (e.g. does
   `Applied` gain a `key` field for the insert echo; does `BlobMeta.Size` reach the
   wire; the `WriteBlob` content-type signature). Confirm DD-9 lands alongside this
   so §Value-fidelity clauses have a concrete referent at promotion.

2. **Tabular sub-sentinels (T-AUD-05, T-AUD-06).** Worth new distinctions, or
   accepted collapses? (a) delete-miss vs edit-conflict both `conflict` today — split
   into a delete-specific sentinel, or is "the row isn't in the state you expected"
   one honest condition? (b) table-not-found vs real outage both `upstream` — map
   not-found-relation to `not_found`, or accept the sanitization tradeoff? My lean:
   (a) accept as one `conflict`; (b) worth `not_found` for the commonest real mistake.

3. **Document upload — feature or dead surface?** `POST /api/upload` works for
   es/meili/typesense server-side but is UI-hidden and never in the document action
   set (document.md §6). Promote to a real "replace document from file" op, or exclude
   it at the provider/`Caps()` level so the capability matches the contract? My lean:
   exclude — "upload a file as a JSON document" has no clear meaning for these engines.

4. **`MaxInlineBytes` per-engine?** 16 MiB is family-wide, but ES's own write ceiling
   (~10.6–12.5 MB) sits below it, so ES can't reach the read-truncation path via its
   own writes (DOC-AUD-03). Make the cap per-engine-aware, or accept the asymmetry as
   documented?

5. **KV create semantics (KV-AUD-03, scoped by DD-5).** The contract requires a
   collection-create path; the *semantics* need a decision: key-name + type + first
   entry in one op? collision behavior (refuse vs overwrite — must not re-open the
   KV-AUD-01 clobber)? initial TTL? This is the create half of T-4 for KV and is
   currently unspecified beyond "must exist".

6. **SQL PK-column edit (deferred from DD-6).** DD-6 fixes document id-immutability;
   editing a tabular **primary-key** column is different (a real relational update
   with cascade/uniqueness implications). Confirm it stays out of DD-6 and gets its
   own decision, or is simply refused in v1.

7. **Stream refusal sentinel normalization (cosmetic).** Collapse the
   `read_only`-vs-`unsupported` split on stream mutation attempts to a single
   "view-only" signal, or leave it (invisible to the SPA today)? Low stakes; flagging
   for completeness.

---

## Resolutions (orchestrator, 2026-07-16 — Karel delegated completion)

Karel authorized autonomous completion, so the seven questions are resolved to their
leans / safe defaults and become binding for the implementation slices. This draft is
now the working spec; it promotes to `docs/spec-dataconsole.md §7` at S25.

1. **DD-9 lands alongside — CONFIRMED.** S28 (in flight) implements exactly this DTO
   shape: `Applied` gains the inserted key, `BlobMeta.Size` (true size) + `TTLSeconds`
   (nil = no expiry) reach the wire, `WriteBlob` gains a content-type parameter. The
   §Value-fidelity clauses have a concrete referent.
2. **Tabular sub-sentinels → (a) accept, (b) split.** delete-miss and edit-conflict stay
   one `conflict` ("the row isn't in the state you expected" is one honest condition);
   table/relation-not-found maps to `not_found`, split from real `upstream` outage. → S27.
3. **Document upload → EXCLUDE at `Caps()`.** "Upload a file as a JSON document" has no
   clear meaning for es/meili/typesense; drop `uploadObject` from the document action set
   so the capability matches the contract. → S14 (actions).
4. **`MaxInlineBytes` → accept the asymmetry, documented.** Keep the 16 MiB family-wide
   read cap; ES's lower write ceiling (DOC-AUD-03) is a documented known limitation, not
   worth per-engine cap machinery in v1.
5. **KV create semantics → explicit, collision-refusing.** Create takes key-name + type +
   an optional first entry in one op; a name collision REFUSES (`ErrConflict`, never
   overwrites — this preserves the KV-AUD-01 clobber guard); no initial TTL (persistent by
   default, TTL set separately). → S17.
6. **SQL PK-column edit → REFUSED in v1.** A primary-key edit is delete+recreate semantics
   (cascade/uniqueness implications), out of scope for inline edit. PK columns render
   non-editable; an EditCell targeting a PK column returns `ErrUnsupported`. Not folded
   into DD-6 (which is document identity). → S28/S14.
7. **Stream refusal → normalize to `read_only`.** View-only-tier mutation attempts return
   the one `read_only` "view-only" signal, not a `read_only`/`unsupported` split. → S27.
