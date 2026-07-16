# S11 rebaseline — Data Console live audit synthesis

Date: 2026-07-16 · Gate: **Karel approves before P2 scope fixes** · Program:
`plans/dataconsole-excellence-program-2026-07-16.md`

Synthesis of four live family audits driven against the real console over VPN
(`zcp-eval-clean`, writes armed): `tabular.md` (db/mariadb/ch), `kv.md` (valkey),
`document.md` (es/meili/typesense/qdrant), `object-stream.md` (s3/kafka/nats), plus
`ui-walk.md` (standalone UI). Every claim below is live-verified with response evidence
in the cited per-family file.

## 1. Headline

The architecture held up: read paths, pagination, the caller-bound write posture, and
read-only enforcement (SQL `READ ONLY` tx, clickhouse `readonly=1`, view-only 403s, the
write-token/confirm/origin matrix) are **airtight on every engine** — proven even with a
valid write token presented. No security regression; the island and route-parity work from
wave 1 is sound.

But the audit surfaced **one dominant, cross-family failure theme the static read
under-weighted: the console reports success on operations that destroyed, lost, or never
applied data.** Seven distinct defects are instances of a "200 OK that lies." This — not
the UX unevenness — is the real reason "edit/delete doesn't work in some places": in
several places it *reports* working while doing nothing or doing damage. This reshapes the
program: a **safety + honesty wave (P0.5) now precedes the presentation refactor.**

Tally: **10 static predictions reconciled** (2 fixed in wave 1, 4 confirmed, 2 confirmed-
worse, 1 refuted-as-framed, 1 reframed) + **24 new live findings** (1 critical, 6 high,
7 medium, 10 low). Full evidence in the per-family files; the load-bearing ones below.

## 2. The four systemic themes (the synthesis that drives the rewrite)

**T-1 — "Success that lies" (the keystone).** Mutations report `{"ok":true}` / `200` while
the data is destroyed, silently dropped, or merely enqueued:
- KV-AUD-01 [CRIT] `PUT /api/blob` unconditional Redis SET clobbers a whole hash/list/set/
  zset into a string — 200 OK, collection gone (live-destroyed `leaderboard`).
- DOC-AUD-01 [HIGH] meilisearch write returns ok on task-enqueue; a doc that fails meili's
  async validation vanishes with no error at any layer (console never polls task status).
- OBJ-AUD-02 [MED] object delete returns `{"ok":true}` for a nonexistent/already-deleted
  key, indistinguishable from a real deletion (and disagrees with rename, which 404s).
- T-AUD-01 [HIGH] timestamp edits spuriously 409 (read path truncates sub-second precision).
- T-AUD-02 [HIGH] bigint values silently corrupt by ±1 on write (server float64 decode).
- KV-AUD-02 [HIGH] a no-TTL key reports `ttlSeconds:0`, rendered as "TTL: 0s" — a false
  fact about the data.
- KV-AUD-06 [LOW] `Applied.Affected` hardcoded to 1 regardless of the real Redis reply.
→ **The excellence contract's `accepted`/`applied`/`visible` semantics (was U-14, one line)
  is now the spine of the program, not a polish item.**

**T-2 — Error contract is flat and lossy.** A validation rejection is indistinguishable
from an outage; envelopes drop context:
- Every non-2xx upstream (400/401/403/409/…) collapses to one generic `502 upstream`
  (document + tabular + stream).
- KV-AUD-04 [MED, cross-family] mutating-route envelopes omit `service`+`family` because
  `withRouteContext` reads `service` from the URL query only, which body-addressed
  POST/PUT/DELETE never populate.
- STR-AUD-02 nats 404 vs kafka 502 for the same "nonexistent resource"; KV-AUD-09 ReadTable
  404-vs-422 inconsistency; KV-AUD-08 401 is plaintext not the JSON envelope; DOC-AUD-02 the
  64 MiB body cap truncates via `io.LimitReader` instead of returning the existing `413
  ErrTooLarge`.

**T-3 — Value/type fidelity is unspecified.** DD-9 must cover more than bigint: bigint
(T-AUD-02, string-transport is the proven fix), timestamp sub-second precision (T-AUD-01),
S3 content-type (OBJ-AUD-01), the no-TTL sentinel (KV-AUD-02), and the missing insert-result
echo (T-AUD-03). D-04 as originally framed is **refuted** — string-encoding is *protective*,
not the bug.

**T-4 — Create + identity are systematically missing.** KV has no collection-creation path
at all (KV-AUD-03: `SetEntry` type-dispatches on the *current* type, so a new key is
`ErrUnsupported` — which is why a clobbered collection can't be repaired). Document id-routing
is broken three different ways (D-05). These are one theme: the console can read and edit
existing shapes but cannot honestly *create* or *re-identify* them.

## 3. Static defect reconciliation

| ID | Was | Verdict | Evidence / new home |
|---|---|---|---|
| D-01 | HIGH zset no-op | **FIXED** (S8) | server contract re-confirmed correct by kv.md; UI lock lands the fix |
| D-02 | HIGH hash field overwrite | **FIXED** (S8) | kv.md confirms post-fix wire shape matches server |
| D-03 | MED list silently view-only | **CONFIRMED as-designed** | kv.md: LSET works server-side, UI deliberately locks list cells, no dead delete button. Not a bug — a create/edit *gap* → S17 |
| D-04 | MED tabular sends string | **REFUTED as framed** | tabular.md: string-encoding round-trips exactly and is the *safe* bigint transport; real risk is bare JSON number → folded into T-AUD-02/DD-9 |
| D-05 | MED doc id/pk duplicate | **CONFIRMED, worse (3 modes)** | document.md §3: es rejects `_id`/ignores `id` (correct); meili+typesense silently misroute to body-id, path 404s; typesense auto-assigns id when omitted → S19/DD-6 |
| D-06 | LOW truncated JSON malformed | **CONFIRMED** | document.md §2: 16 MiB mid-string cut, invalid JSON, 200 OK, only signal is a header → S19 |
| D-07 | LOW object folder rename/delete | **RESOLVED (no data loss) + reframed** | object-stream.md §2: children provably untouched; underlying issue is OBJ-AUD-02 (dishonest delete) not folder-specific |
| D-08 | LOW KV no whole-key collection delete | **PARTIALLY REFUTED** | kv.md: server deletes collections fine via `/api/node` + `/api/blob`; the gap is UI-wiring only → S17 |
| D-09 | INFO stale comment / rabbitmq | **CONFIRMED** doc cleanup | → S14 (comment) + S25 (spec §6 tier note) |
| D-10 | HIGH exact-value transport | **CONFIRMED, relocated server-side** | tabular.md §2 = T-AUD-02: `server.go decode()` lacks `UseNumber()`, corruption is server-in, not browser. 8-value mapping table proves ±1 at 2^53+ → DD-9/S9 |

## 4. New defect register (live-found, severity-ranked)

**CRITICAL**
- **KV-AUD-01** `PUT /api/blob` clobbers collections (unconditional SET, no type guard). Data loss, silent. `kv.md`.

**HIGH**
- **OBJ-AUD-01** object writes (`PUT /api/blob` + multipart `/api/upload`) never set S3 content-type → uploaded image loses inline preview; editing+saving a text file once makes it un-editable/un-previewable in the console ("Binary — use Download"). First-workflow bug. `object-stream.md`.
- **T-AUD-01** timestamp optimistic-concurrency permanently broken (`normalize()` RFC3339, no sub-second) → every `now()`-column edit spuriously 409. `tabular.md`.
- **T-AUD-02** bigint silent corruption server-side (`decode()` no `UseNumber()` → float64) → ±1 for any value > 2^53. `tabular.md`.
- **KV-AUD-02** no-TTL key reports `ttlSeconds:0` not a negative/null sentinel → SPA "no expiry" branch never fires. Cross-confirmed by `ui-walk.md` UI-AUD-02. `kv.md`.
- **KV-AUD-03** no collection-creation path (SetEntry type-dispatches on current type). `kv.md`.
- **DOC-AUD-01** meilisearch write silently fails while reporting ok (no task-status poll). `document.md`.
- **KV-AUD-10** zset `SetEntry` with a value but no score silently writes score 0 (`ZADD 0 member`) — same no-guard, API-only-footgun class as KV-AUD-01; belongs in the same guard slice. `kv.md`.

**MEDIUM**
- **KV-AUD-04** (cross-family) mutating-route error envelope drops `service`+`family` (`withRouteContext` reads query-only). `kv.md`.
- **DOC-AUD-02** error flattening: all non-2xx → generic 502; 64 MiB cap truncates via `io.LimitReader` instead of `413 ErrTooLarge`. `document.md`.
- **OBJ-AUD-02** object delete always `{"ok":true}` (idempotent S3) even for nonexistent keys; disagrees with rename's 404. `object-stream.md`.
- **STR-AUD-01** stream Stat/`GET /api/node` never verifies existence (200 for a fabricated topic/stream). `object-stream.md`.
- **STR-AUD-02** nonexistent-read misclassified within one family: nats 404, kafka 502 upstream. `object-stream.md`.
- **KV-AUD-05** truncated blobs never report true size (`BlobMeta.Size`/`TTLSeconds` computed then dropped in transport). `kv.md`.
- **T-AUD-03** `insertRow` never echoes the new row's key → blocks any correct insert-then-edit UX. `tabular.md`.

**LOW**
- **T-AUD-04** cross-engine write-strictness asymmetry (mariadb coerces JSON number→VARCHAR silently; pg rejects). `tabular.md`.
- **T-AUD-05** delete-miss and edit-conflict both surface as generic 409 (indistinguishable). `tabular.md`.
- **KV-AUD-06** `Applied.Affected` hardcoded to 1. **KV-AUD-07** tree nodes carry no type metadata (= UI-AUD-04, U-03). **KV-AUD-08** 401 plaintext not JSON envelope. **KV-AUD-09** ReadTable missing-key 422 not 404. `kv.md`.
- **OBJ-AUD-03** malformed `segs` silently degrades to root listing (discarded unmarshal error). `object-stream.md`.
- **DOC-AUD-03** ES write ceiling (~10.6–12.5 MB) sits below the 16 MiB read cap → D-06 unreachable via ES's own write path. **DOC-AUD-04** phantom next-page when doc count is an exact multiple of the limit (typesense + es). `document.md`.
- **UI-AUD-01** deep-link `#…&svc=<host>` doesn't select the service. **UI-AUD-02** NATS `subjects` renders unicode-escaped `events.>`. **UI-AUD-03** [now VERIFIED] qdrant ReadBlob dumps the full vector array. **UI-AUD-04** families visually indistinguishable in the tree. `ui-walk.md`.

**Confirmed-healthy (no churn):** read-only enforcement all engines; write-auth matrix (20/20 stream refusals, token/confirm/origin/bearer); pagination cursors; object image preview + size chips; tabular grid + PK marker; clickhouse/qdrant view-only gates; blob download-hardening headers uniform across families.

## 5. Rewritten wave plan (supersedes program §6-§9 pending Karel's approval)

The audit inserts a **P0.5 safety + honesty wave before P2/P3**, and sharpens DD-9. Rationale:
T-1/T-2 defects are correctness/data-loss, cheap to fix at the seam, and must not wait behind
the taste-gated presentation work. Contracts (P2) and the SPA refactor (P4) build on honest
server behavior.

### P0.5 — safety + honesty (server-side, mostly autonomous, TDD live-replayable)

- **S26 [was implicit] — mutation honesty + write guards** (T-1). Refuse `WriteBlob` SET on an
  existing non-string key (KV-AUD-01, uniform `ErrConflict`/typed refusal, never clobber);
  meili write polls task completion or returns an honest `accepted`-not-`applied` status
  (DOC-AUD-01); object delete reports whether anything was deleted (OBJ-AUD-02); `Applied.
  Affected` carries the real count (KV-AUD-06). RED = the exact live reproductions in the audit.
- **S27 — error-envelope contract** (T-2). `service`+`family` on body-addressed routes
  (KV-AUD-04); map upstream status classes instead of flattening to 502 (DOC-AUD-02); `413`
  for over-cap bodies; JSON envelope for 401 (KV-AUD-08); consistent 404-vs-422 (KV-AUD-09);
  nats/kafka nonexistent-read parity (STR-AUD-02). One typed error taxonomy, pinned per provider.
- **S28 — value-wire contract = DD-9, expanded + applied** (T-3). bigint string-transport end-
  to-end incl. PK/rowKey/expectedOld (T-AUD-02); timestamp full-precision serialization
  (T-AUD-01); S3 content-type carried on write + upload (OBJ-AUD-01, needs a `WriteBlob`
  signature change — the one interface change, promote to spec before building); no-TTL
  sentinel (KV-AUD-02); `insertRow` echoes the new key (T-AUD-03); truncated-blob true size
  (KV-AUD-05). Conformance cases land with each.

### P1.5 — create + identity (was DD-5, now evidence-scoped)

- **S17 (rescoped) — create/identity affordances**: KV collection-create (KV-AUD-03) + whole-
  key collection delete UI wiring (D-08); document id-immutability guard + honest reroute
  (D-05/DD-6); the create semantics per family the audit now specifies concretely.

### P2/P3/P4 unchanged in shape, sharpened by evidence

- Excellence contracts (S12) now write the `accepted`/`applied`/`visible` column from the
  measured **visibility table**: es list lags 0–750 ms (refresh-bound), meili everything async
  ~0.9–1.35 s (200 = enqueued), typesense synchronous, qdrant read-only. This is real data, not
  a guess.
- DD-3 (stream) evidence-backed: U-04 confirmed (metadata byte-indistinguishable from content);
  the honest metadata card is the fix.
- DD-4 presentation contract absorbs UI-AUD-02/03/04 + U-03 (tree type metadata KV-AUD-07,
  vector-array collapse, family glyphs, unicode-escape render).
- S15/S15a (SPA unify + DOM harness) and S23 (already landed) unchanged.

### Deferred / won't-fix-now (recorded honestly)
- DOC-AUD-03/04, T-AUD-04/05, OBJ-AUD-03, UI-AUD-01 → tracked, scheduled into the owning P4
  slice, not their own work.
- rabbitmq/shared-storage stay not-yet (offline pins); mysql not a Zerops type (DD-8 resolved).

## 6. Gate decisions (Karel, 2026-07-16)

1. **Wave order → FOLD INTO P4.** No separate front-loaded P0.5 wave. The safety + honesty
   fixes (S26/S27/S28) become P4 convergence slices, run alongside the presentation work, in
   one combined wave after P2 contracts + P3 design. Exception, taken as orchestrator: S26 (the
   CRITICAL collection-clobber KV-AUD-01 + the HIGH meili silent-loss DOC-AUD-01) was already
   in flight when this decision landed and has zero contract/design dependency; leaving a
   data-destroying bug live to wait behind the design phase is not defensible, so S26 lands now
   and is FILED under P4 (same code either way). No further pre-wave safety agents spawn; S27
   (error envelope) and S28 (value-wire) wait for the contracts/design that shape their taxonomy.
2. **DD-9 → WIDEN TO ALL FOUR (RESOLVED).** The value-wire contract covers bigint (string
   transport end-to-end) + timestamp full-precision + S3 content-type + no-TTL sentinel +
   insert-key echo, as one value-fidelity contract promoted to `spec-dataconsole.md`. Includes
   the `WriteBlob` content-type signature change (the one provider-interface change), promoted
   to the spec before S28 builds against it.

## 7. Next steps (post-gate, autonomous)

1. Land S26 (in flight) → integrate under P4.
2. Update the program plan: drop the standalone P0.5 wave; fold S26/S27/S28 into P4; record
   both decisions + the RESOLVED widened DD-9.
3. Proceed to **P2 — draft the six family excellence contracts** from this audit's evidence
   (the `accepted`/`applied`/`visible` visibility table, the honest-error taxonomy, the value-
   fidelity rules), for Karel's next owner gate. These + the P3 decision records are the "how"
   that shapes the P4 combined wave.

Fixtures restored (final dcseed pass, all 11 seeded ✓, leaderboard back to a zset); console
stopped; live config (credentials) deleted.
