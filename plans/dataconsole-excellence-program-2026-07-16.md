# Data Console Excellence — program plan (rev 2)

Date: 2026-07-16 · Branch: `feat/managed-data-console` (continues S0-S7) · Status: **draft — gated on Karel**
Rev 2 incorporates the Codex review (request-changes): exact-value wire contract added,
S8 minimized, S9 re-scoped + moved behind the audit, DOM characterization harness moved
before the SPA refactor, rebaseline gate defined, live-profile no-skip gate, embedded-path
mutation verification, spec promotion moved before P4, UI-slice parallelism claim corrected.

## 0. Charter

**Outcome.** Every managed-service family gets a browser that (a) does everything its
excellence contract promises, (b) shares one presentation language with the other
families, (c) is pinned by a per-engine conformance suite so it cannot silently
regress, and (d) has a repeatable audit + improvement loop so future engines and
future polish enter through a defined machine, not ad-hoc patches.

**Non-goals.** No DDL (create/drop tables/indices/buckets), no multi-user console,
no public exposure, no production-posture work (spec §9 reserved), no SPA framework
or build step (the no-build discipline stays unless a Design decision overturns it).

**Operating rules** (repo discipline + program-specific):
- TDD mandatory per slice (RED→GREEN→REFACTOR); behavior-preserving refactors get
  characterization tests FIRST, then refactor under them.
- Fable orchestrates; implementation slices go to fresh subagents with self-contained briefs.
- Every plan/design output routes through Codex for a second opinion.
- **Observed verification only**: no capability is reported working unless driven
  against a real engine (live tier) with the result observed; offline fakes prove
  provider/request contracts, never engine semantics.
- **Evidence before proposals**: the P1 live audit produces the defect list; static
  findings below stay labelled `code-confirmed` vs `suspected` until observed live.
- **Enforcement over instruction**: every "keep in lockstep" comment becomes a drift
  pin (test or generator) during this program.
- **The spec is the design authority.** Approved contracts and decisions are promoted
  into `spec-dataconsole.md` at approval time (S12/S13), BEFORE implementation waves.
  Implementation briefs cite the spec, never this plan.

## 1. Evidence base — static audit (3 parallel explorations, 2026-07-16)

### 1.1 What is healthy (preserve, don't churn)

- Code-isolation island + adapter allowlist, AST + depguard pinned (spec §3).
- Caller-bound write posture: single choke point, dual-client test, uniform `ErrReadOnly` (spec §5).
- **Capability payload is server-owned**: `/api/services` ships per-service `actions[]`
  (`provider.ServiceActions`, connection-free); the SPA renders affordances only from it
  and branches on **zero** family/engine strings (grep-confirmed). Dispatch is
  `node.kind` + content-type driven.
- Action-ID contract drift guard exists (`contract_test.go`: `dist/contract.js` is
  generated from Go action IDs; dist JS may reference only `ACTION.*`).
- Route table is action-typed (`server.go:86-107`): route ↔ ActionID ↔ mutating flag in one
  place (note: `action` and `mutating` are dual-owned per route — S23 pins their coherence).
- All five family factories registered (`studio_console.go:67-73`); `dcseed` seeds all 12 engines.
- Live testbed exists: project `zcp-eval-clean` runs 10 managed services
  (pg@18 STOPPED, object-storage, typesense@30.2, valkey@7.2, clickhouse@25.3,
  meilisearch@1.44, nats@2.12, qdrant@1.12, elasticsearch@9.2, kafka@3.9).
  Missing engines: mariadb (provisioned 2026-07-16 as `mariadb:single@10.6` — the only
  catalog version; live-verified), mysql (**NOT a Zerops managed type** — live catalog has
  no mysql; console support stays dialect-parity-only via the shared myDialect), rabbitmq
  (platform-DEPRECATED in favor of NATS → not-yet forever, offline pins suffice) +
  shared-storage (not-yet).

### 1.2 Engine × operation matrix (implemented today; caps: tabular 100/1000, kv 200/1000, object 200/1000, document 200/1000, typesense hard 250)

| Engine | list | browse | pagination | query | edit | insert | delete item | upload | rename | TTL |
|---|---|---|---|---|---|---|---|---|---|---|
| pg / mariadb / mysql | schemas→tables | grid | offset+1 | RO tx | cell (PK+optimistic) | row | row (PK) | — | — | — |
| clickhouse | ✓ | ✓ | offset | readonly=1 | view-only | view-only | view-only | — | — | — |
| valkey | SCAN `:`‑tree | str→blob, coll→grid | cursor/offset mix | — | str=SET, hash=HSET, list=LSET*, set=SREM+SADD, zset=ZADD | — | HDEL/SREM/ZREM; **list unsupported** | — | — | EXPIRE/PERSIST |
| object (S3) | prefix tree | head-slice blob | StartAfter | — | WriteBlob | — | leaf only | ✓ | copy+delete (single key) | — |
| elasticsearch | indices | docs | `from` offset (≤ ~10k window) | — | upsert by `_id` | — | ✓ | — | — | — |
| meilisearch | indexes | docs | offset | — | PUT `[body]` — **path id ignored, pk authoritative** | — | ✓ | — | — | — |
| typesense | collections | search page | 1-based page | — | upsert by body id | — | ✓ | — | — | — |
| qdrant | collections | scroll | opaque offset | — | view-only | — | view-only | — | — | — |
| kafka / nats | topics/streams | **metadata summary only** | none | — | view-only | — | view-only | — | — | — |

\* LSET implemented server-side but unreachable (see D-03).

### 1.3 Defect register (static; verify live in P1 before fixing unless marked hotfix)

| ID | Sev | Status | Finding | Evidence |
|---|---|---|---|---|
| D-01 | HIGH | code-confirmed → **hotfix** | zset edit is a silent no-op that toasts "Saved": SPA always sends the row's OLD score (`keyColScore`), server ignores `Value` for zset | `app.js:658`, `dc-rows.js:15-18`, `kv.go:370-378` |
| D-02 | HIGH | code-confirmed → **hotfix** | KV key-column cells are editable; editing a hash FIELD cell issues `HSET key oldfield <typed>` — overwrites the value with the field-name text | `app.js:607-613`, `kv.go:347-351` |
| D-03 | MED | code-confirmed | redis list grid silently view-only (`RowKeyCols` nil only for list) though LSET works; list delete honestly unsupported — no UI signal | `kv.go:257,352-360,411` |
| D-04 | MED | suspected | tabular cell edit sends `newValue` as string always; typed columns (int/bool/json) risk optimistic-concurrency miss / encode error | `app.js:660`, `tabular.go:301-316` |
| D-05 | MED | code-confirmed | editing a document's id/pk field creates a duplicate instead of updating (meili pk authoritative; typesense body id; es/qdrant path id) | `meilisearch.go:97-107`, `typesense.go:86` |
| D-06 | LOW | code-confirmed | truncated JSON preview is malformed (raw byte head-slice of pretty JSON); guarded view-only so not editable | `document.go:190-193` |
| D-07 | LOW | latent | object rename/delete are single-key; prefix (folder) semantics unhandled — currently unexposed on containers | `object.go:210,223` |
| D-08 | LOW | code-confirmed | KV collections (hash/list/set/zset) have no whole-key delete affordance though `kv.Delete` exists; only string keys deletable | `app.js:438` wiring |
| D-09 | INFO | code-confirmed | `types.go:23` comment "stream — no provider (dropped)" is stale (factory registered, provider live); `SupportFor` gives rabbitmq not-yet while kafka/nats are view-only — decide, then document | `types.go:23`, `family.go:34-46` |
| D-10 | HIGH | code-confirmed (systemic) | exact-value transport: `Rows [][]any` / `RowKey map[string]any` / `expectedOld` flow through JSON into JS numbers — BIGINT > 2^53, DECIMAL, exact PKs can corrupt on display and on every edit/delete round-trip | `types.go:100-131`, browser JSON semantics |

### 1.4 UX inconsistency register (root cause: **no shared presentation contract** — providers freely shape tree/columns/content-type; SPA renders faithfully but divergently; `app.js` has zero tests)

| ID | Finding |
|---|---|
| U-01 | Two grid renderers: `renderGrid` (editable, PK 🔑, tooltips, Load-more) vs `runQuery` inline table (none of that, silent 2000 cap) |
| U-02 | Read-only *posture* is invisible (cells just don't respond); read-only *no-key* shows a label — two different silences for "can't edit" |
| U-03 | Tree meta chips only for object (size; provider's modified/etag dropped); other families bare names; icons kind-only so families are visually indistinguishable |
| U-04 | Stream topic opens as a JSON `<pre>` indistinguishable from a document blob — metadata masquerades as content |
| U-05 | Document edit = raw JSON textarea without validation; tabular edit = structured inline cell — unrelated experiences for "edit a record" |
| U-06 | Cell edit has no visible affordance (hover outline only) vs blob's explicit Save button |
| U-07 | KV IA: TTL bar detached at pane bottom; no insert-entry affordance; per-type asymmetries (D-03/D-08) |
| U-08 | Extension cards vs SPA rail: two pickers, different visuals, different gating (cards offer Browse on not-yet services; rail disables them) |
| U-09 | Pagination UX: tree "Load more…" vs grid "Load more rows…" vs query nothing |
| U-10 | Loading state absent for tree expansion; empty state rendered 3 ways; errors surface inline / inline-no-VPN / toast depending on operation |
| U-11 | Dead paste-token auth UI (unreachable in both real paths) |
| U-12 | Embedded panel tab always generic "Data Console" (no service name) |
| U-13 | No search/filter affordance for document family — ES/Meili/Typesense (search engines!) browsable only by paging ids |
| U-14 | Mutation feedback lies about state: "Saved" toasts on enqueue — meili writes are async tasks, ES visibility is refresh-dependent, S3 rename is copy-then-delete (partial failure possible) |

### 1.5 Verification debt register

| ID | Debt |
|---|---|
| T-01 | No live/e2e tier for dataconsole at all; 9 provider tests `t.Skip` pointing at "see e2e" that was never written; document family has zero engine tests |
| T-02 | Only KV runs against a real engine offline (miniredis); tabular uses a fake SQL driver; object/stream success paths skipped |
| T-03 | `app.js` (~840 lines, the whole DOM layer) untested; no DOM/headless harness; JS deps don't exist yet (no test package/lockfile, node invoked directly) |
| T-04 | Broker ALLOW/MUTATING is a hand-maintained copy of server routes ("Keep in lockstep") — exact parity today, no test pins it; `consolePanel` ASSETS similarly hand-copies `index.html` refs |
| T-05 | Error envelope proven through one object fake; per-provider sentinel mapping never executed end-to-end |
| T-06 | Read-only posture per engine (pg RO tx, ch readonly=1, valkey deny) proven via fakes only |
| T-07 | `zcpadapter` env mapping pinned against mock envs, never against live service envs |
| T-08 | `dcseed` automated nowhere; static fixture names unsuitable for repeatable mutating runs (no per-run namespace, no teardown) |
| T-09 | Pagination correctness untested for object/document/stream |

## 2. Method — the operating system for this program

Instantiates the guided/Matt Pocock approach (nine-phase workflow, second-brain
synthesis + `mattpocock/skills/engineering`) with the repo's own institutions:

| Pocock concept | This program |
|---|---|
| wayfinder (large work → shared decision map + tickets) | this plan: phases, decision log, slice register |
| grill-with-docs / domain-modeling | §1.2 matrix + P2 family excellence contracts = the domain model |
| improve-codebase-architecture | §1 static audit + P3 convergence design |
| to-spec (conversation → durable spec) | P2/P3 outputs promoted into `spec-dataconsole.md` AT approval, before P4 |
| to-tickets (tracer bullets, blocking edges) | §9 slice register, S-numbered, dependency-ordered |
| implement (TDD + review from tickets) | fresh subagent per slice, brief = the slice section, repo TDD mandate |
| prototype (material uncertainty) | P3 UI-canon mock before committing the SPA refactor |
| diagnosing-bugs | defects root-caused (problem-solving skill) before fixes |
| code-review two-axis | review checks repo standards AND excellence-contract compliance |
| observed-verification | verified-composite below; live drive + screenshots, never self-attestation |
| enforcement-over-instruction | drift pins: route parity generated, assets derived+asserted, matrix pinned |
| zero-context-handoff | audit files + briefs + this plan live in `plans/`; every slice resumable |
| progressive disclosure | this plan is the router; per-engine detail lives in audit files |
| risk-calibrated autonomy | mechanical slices run autonomous; taste/contract/scope decisions gate on Karel |

**Verified-composite** (every execution slice must show all four):
1. offline suite green (`go test ./internal/dataconsole/... -short` + node suites),
2. live tier green for touched engines (over VPN against `zcp-eval-clean`; see live profiles in S10a),
3. looks-right: screenshot of the touched view vs the UX canon — **mutation UX must be
   verified on the embedded path** (standalone is view-only by construction): VS Code
   webview walk, a mocked-broker DOM harness, or host-reviewed embedded screenshots,
4. independent review (code-review / Codex), reviewer ≠ implementer.

**The long-term loop** (what "dlouhodobě vylepšovat" means mechanically): any future
finding enters as audit evidence → red conformance case or canon delta → slice brief →
subagent → verified-composite → matrix/spec update. A new engine onboards as:
`Classify` + factory + seeder + conformance suite pass + audit run → support
tier is **earned by the suite**, never asserted.

## 3. P0 — corruption hotfix (start immediately; minimal by design)

**S8 — KV edit corruption hotfix** (D-01 + D-02 ONLY).
Seam: SPA edit-payload builders + cell interactivity; server contract UNCHANGED
(no `TablePage` field additions — that is DD-4/S14 territory).
RED: miniredis cases (zset score edit persists the typed score) + node unit tests
for the payload builders (key-column cells non-interactive; zset sends typed value
as `Score`). Preserve: command-allowlist safety; uniform `ErrReadOnly`.
D-03/D-08 (list semantics, whole-key delete) are explicitly DEFERRED to the P4 KV
slice — they are capability polish, not corruption.

## 4. P1 — live audit sweep (the instrument, then the evidence)

**S10a — live conformance harness skeleton.** Data-plane suite (providers speak
pg/redis/s3/http directly): lives inside the island, **descriptor-driven and
core-free** (neutral `ConnectionDescriptor` input from env / gitignored config —
never a core import; adapter live-env verification is a SEPARATE case set outside
the island). Tier taxonomy: mutating-data-plane suite — reconcile with
`spec-testing-architecture.md` (new build tag or an explicit e2e-tier amendment;
decide in-slice, document in the spec). **Two run profiles**: `partial` (local dev:
absent engines skip-with-reason, listed in the run report) and `full` (release gate:
required engine manifest — any absent/skipped engine FAILS; engine versions captured).
Retry policy: reachability/setup only, never semantic assertions.
Runbook: `zcli vpn up` → start `db` → seed → run.

**S10b — fixture/seeder library.** Promote `dcseed` seed funcs into a reusable,
island-safe fixture package (CLI shim stays): per-run namespaces, deterministic
teardown, recovery cleanup after interrupted runs, no `t.Parallel()` against shared
services. Baseline conformance framework (case skeleton per family) lands HERE,
pre-P4 — each P4 slice then adds its family's cases (S24 shrinks to a close-out pin).

**S11 prep — testbed completion:** provision mariadb + mysql into `zcp-eval-clean`
BEFORE the audit baseline (they are claimed full-support; support must be earnable).
rabbitmq/shared-storage stay unprovisioned (not-yet; offline honesty pins).

**S11 — the audit run** (orchestration; per-family audit passes + one synthesis):
for each of the 12 live services, drive EVERY advertised action via the API
(scripted), walk the UI per family — read paths via standalone puppeteer,
**mutation UX via the embedded path** (webview or mocked-broker harness; see §2),
screenshots of list/browse/edit/error/empty states. Output per family:
`plans/dataconsole-audit/<engine>.md` (observed / inferred / open).
**Exit artifact — the rebaseline report**: updated defect register (confirm/refute
D-04..D-10, harvest new), updated matrix, triage of any new corruption finding,
and a REWRITTEN wave-2 slice register. **Hard gate: Karel approves the rebaseline
before P2 scope is fixed.**

## 5. P2 — Define: family excellence contracts (gated: taste)

One contract per family (tabular, kv, object, document, stream, +file/unknown
honesty), written from audit evidence, ~1 page each. Each fixes: canonical
operations (with per-engine deltas), canonical view (tree shape, grid columns,
meta chips), canonical states (loading / empty / error / unreachable-VPN /
read-only-posture / view-only-no-key), canonical interactions (pagination, edit
affordance visibility, confirm + post-mutation feedback), **mutation-state
semantics: `accepted` vs `applied` vs `visible`** (meili tasks, ES refresh, S3
copy+delete — no "Saved" toast on mere enqueue), honest-label rules, non-goals.
Acceptance criteria BECOME the conformance suite's case list (the contract is the
test list). **Promoted into `spec-dataconsole.md` §7 upon approval — before P4.**

## 6. P3 — Design: convergence decisions (gated)

| ID | Decision | Options (→ recommendation) |
|---|---|---|
| DD-1 | Conformance substrate | offline layer (miniredis, fake SQL, recorded HTTP fixtures) proves provider/request CONTRACTS; live VPN tier proves ENGINE SEMANTICS (async, read-only enforcement, pagination windows, versions) → **hybrid, with layers named honestly**; option: selectively containerize high-risk engines later (parked, not rejected on principle); tier-taxonomy reconciliation happens in S10a |
| DD-2 | Document search (U-13) | NOT one uniform op. (a) bounded simple text search (server-built query) for es/meili/typesense; (b) engine-native advanced query; (c) qdrant payload FILTER as a separately-labelled thing or deferred → **(a) + qdrant deferred**; contract must pin query/body limits, timeout, cursor, plain-text results (no engine highlight HTML), route-security acceptance (§8) |
| DD-3 | Stream family target (U-04, D-09) | honest labelled metadata card now (partitions/streams; consumer-group counts best-effort with explicit "unavailable") → **yes**; message PEEK is a SEPARATE future decision+slice (non-consuming reads, no durable consumers, partition/sequence rules, byte/message caps, binary handling); rabbitmq stays not-yet |
| DD-4 | Presentation contract shape (U-01..U-10) | server-declared, SPA-rendered. Open sub-decisions: real typed `NodeMeta` struct vs discriminated meta; **per-`Column` editability + reason** (page-level list cannot express KV set rename / list edit-no-delete / identity columns); row-operation metadata where delete availability differs; ONE grid renderer, ONE state-banner, ONE meta-chip renderer, pagination canon → design doc + UI mock before refactor |
| DD-5 | Create/insert affordances | **deferred to post-audit** (S13): document create needs per-engine id/overwrite/async contract; KV create needs key type/collision/TTL semantics; S3 "new folder" is false semantics (prefixes) — decide per family on audit evidence |
| DD-6 | Edit-identity guard (D-05) | invariant: **"path identity is immutable during Save; payload/path identity mismatch is rejected"** (meili resolves its dynamic pk; typesense preserves `id`; ES `_id` outside `_source`); NOT conflated with SQL PK edits (different semantics — decided separately); explicit duplicate/copy is a possible later op, never hidden in Save → **refuse** |
| DD-7 | SPA test harness (T-03) | jsdom + node:test needs real tooling work: test-only package.json + lockfile, pinned Node in CI, `npm ci`, required CI command. **Characterization slice S15a lands BEFORE the refactor** (tests are the safety net, not cargo delivered with the change); jsdom assertions + small semantic snapshots are complementary |
| DD-8 | Testbed completion | **RESOLVED 2026-07-16**: mariadb:single@10.6 provisioned (only catalog version); mysql does not exist as a Zerops managed type → dialect-parity coverage is the ceiling, spec §6 should say so; rabbitmq platform-deprecated → not provisioned (offline classification pins) |
| DD-9 | Value-fidelity wire contract (D-10 + audit) | **RESOLVED 2026-07-16 (Karel: widen to all four).** One value-fidelity contract, not just bigint. Covers: (a) **bigint/int64** as string end-to-end incl. PK/rowKey/`expectedOld` — server `decode()` gains `UseNumber()`, T-AUD-02; (b) **timestamp** full sub-second precision in the read serialization, T-AUD-01; (c) **S3 content-type** carried on write+upload — the one `WriteBlob` provider-interface change, OBJ-AUD-01; (d) **no-TTL sentinel** (negative/null, not 0), KV-AUD-02; (e) **insert-key echo** in `Applied`, T-AUD-03. Promoted to `spec-dataconsole.md` before S28 builds. Conformance cases (boundary values) land with each. |

Each resolved decision gets a short decision record in this file (status, chosen
option, why, rejected alternatives) AND its durable outcome goes to the spec at S13
approval.

## 7. P4 — Execute: convergence waves (scope fixed by the S11 rebaseline + S12/S13)

**Gate outcome 2026-07-16 (Karel): the safety + honesty fixes are FOLDED INTO P4** — no
separate P0.5 wave. S26/S27/S28 are P4 slices run alongside the presentation work in one
combined wave. Exception taken as orchestrator: **S26** (CRITICAL collection-clobber
KV-AUD-01 + HIGH meili silent-loss DOC-AUD-01 — pure correctness, zero contract/design
dependency) was already in flight and lands now, filed under P4; a data-destroying bug does
not wait behind the design phase. S27/S28 wait for S12/S13 (their taxonomy + the value-wire
contract shape them).

Sequencing law (corrected): **backend halves may run in parallel; ALL UI work
serializes behind S15** (single `app.js` surface — sibling UI slices are NOT
independent). Briefs are written at wave start, one per slice, self-contained.

- **S26 mutation honesty** (T-1) — refuse cross-type `WriteBlob` clobber (KV-AUD-01),
  meili task-poll (DOC-AUD-01), honest object delete (OBJ-AUD-02), real `Applied.Affected`
  (KV-AUD-06). In flight; server/provider-side, no interface change. RED = the audit's live
  reproductions.
- **S27 error-envelope contract** (T-2) — `service`+`family` on body-addressed routes
  (KV-AUD-04), upstream status-class mapping not flat 502 + `413` for over-cap (DOC-AUD-02),
  JSON 401 (KV-AUD-08), 404-vs-422 consistency (KV-AUD-09), nats/kafka nonexistent parity
  (STR-AUD-02). Depends: S12 (the honest-error taxonomy the contracts define).
- **S28 value-fidelity contract = DD-9 applied** (T-3) — bigint string transport
  (T-AUD-02), timestamp full-precision (T-AUD-01), S3 content-type on write incl. the
  `WriteBlob` signature change (OBJ-AUD-01), no-TTL sentinel (KV-AUD-02), insert-key echo
  (T-AUD-03). Depends: S13 (DD-9 resolved) + spec promotion of the contract. Subsumes the
  old S9 (tabular exact-value edits fold in here — D-04 refuted as framed, so no standalone
  string-cast slice).
- **S14 presentation contract, server side** — DD-4/DD-9 schema (may split: DTO schema
  vs per-provider metadata population); stale-comment cleanup (D-09 doc half).
- **S15a DOM harness + characterization** — jsdom tooling (DD-7), characterization
  tests of CURRENT rendering + untrusted-data/XSS cases; lands before any renderer change.
- **S15 SPA unification** — one grid renderer (query view reuses it), state canon,
  meta chips, pagination canon, edit-affordance visibility (U-02/U-06), honest
  mutation-state feedback UI (U-14). May split: renderer unification / states+meta+
  pagination migration. Depends: S14, S15a, DD-4 mock approved.
- **S16 document search** — split: search contract+route (backend, after S23) /
  engine adapters / UI (after S15). Route-security acceptance per §8.
- **S17 create/insert affordances** — per DD-5 outcome; split per family (document
  create / KV add-entry+whole-key delete (D-08) / KV list semantics (D-03)).
- **S18 stream honesty** — labelled metadata card per DD-3; peek = separate future slice.
- **S19 document edit semantics** — split: identity guard (DD-6) / async
  accepted-applied-visible handling (U-14 backend) / truncation fix (D-06:
  boundary-truncate or fetch-full-on-edit).
- **S20 object polish** — meta chips population (with S14), folder semantics honesty
  (D-07); UI bits after S15.
- **S21 view-only excellence** — split: clickhouse (query console on the unified
  renderer) / qdrant (structured payload rendering); both keep honest view-only labels.
- **S22 picker unification** — extension cards adopt rail gating + one visual language
  (U-08, U-12); dead auth UI removal (U-11) can ride along or split if oversized.

## 8. Foundations + close-out guardrails

- **S23 route/assets parity — FOUNDATION, lands in wave 1** (before any new route):
  broker ALLOW/MUTATING **generated from the Go route table** (the owner), parity
  test both directions; action↔mutating dual ownership on each route pinned coherent;
  `consolePanel` ASSETS **derived from `index.html`** (HTML owns its refs; the panel
  rewrites and asserts none missed — Go does not own HTML assets).
  **New-route security acceptance** (standing, applies to every route added later,
  incl. `/api/search`): bearer coverage, correct mutating classification, write-token
  gate before decode/provider access, broker parity, sanitized errors, resource
  bounds (query length, result size, timeout), XSS-safe rendering.
- **S24 close-out conformance pin** (shrunk by design): matrix-completeness check —
  every declared capability has a conformance case; per-provider error-envelope +
  read-only posture + pagination cases complete (T-05, T-06, T-09); adapter live-env
  pin (T-07); `full`-profile run green on the release manifest.
- **S25 reconciliation + archive** — spec already carries contracts/decisions (S12/S13);
  here: final matrix refresh in spec §6 (support tiers earned by suite), CLAUDE.md map
  line if a new cross-cutting trap emerged, audit files trimmed to durable findings,
  this plan → `plans/archive/`.

## 9. Slice register

| ID | Title | Phase | Depends | Gate |
|---|---|---|---|---|
| S8 | KV edit corruption hotfix (D-01/D-02 only) | P0 | — | autonomous |
| S10a | live harness + run profiles | P1 | — | autonomous |
| S10b | fixture/seeder lib + case skeleton | P1 | S10a | autonomous |
| S23 | route/assets parity generation + security acceptance | found. | — | autonomous |
| S11p | testbed: provision mariadb+mysql | P1 | — | autonomous |
| S11 | audit sweep (per-family) + rebaseline report | P1 | S10a, S10b, S11p | ✅ **approved 2026-07-16** |
| S26 | mutation honesty (clobber guard, meili poll, honest delete) | P4 | S11 | in flight → review |
| S12 | family excellence contracts → spec §7 | P2 | S11 | **Karel approves** |
| S13 | decisions DD-1..DD-9 + UI canon mock → spec | P3 | S12 | **Karel approves** |
| S27 | error-envelope contract | P4 | S12 | review |
| S28 | value-fidelity contract (DD-9 applied; subsumes old S9) | P4 | S13 | review |
| S14 | presentation contract server side | P4 | S13 | review |
| S15a | DOM harness + characterization/XSS | P4 | S13 | review |
| S15 | SPA unification | P4 | S14, S15a | looks-right → Karel |
| S16 | document search (backend → UI) | P4 | S13, S23; UI after S15 | review |
| S17 | create/insert per family | P4 | S13; UI after S15 | review |
| S18 | stream honesty card | P4 | S13; UI after S15 | review |
| S19 | document edit semantics (identity/async/truncation) | P4 | S13 | review |
| S20 | object polish | P4 | S14; UI after S15 | review |
| S21 | clickhouse / qdrant view excellence | P4 | S15 | review |
| S22 | picker unification | P4 | S15 | looks-right → Karel |
| S24 | close-out conformance pin (full profile) | P5 | P4 done | review |
| S25 | reconciliation + archive | P5 | all | **Karel approves** |

Wave 1 = S8 + S10a + S23 + S11p in parallel → S10b → S11 → rebaseline gate →
S12 → S13 → wave 2 (backend halves parallel; UI serialized behind S15).

## 10. Risks

- **Audit invalidates wave-2 scope** — absorbed by design: the S11 rebaseline REWRITES
  the register before S12; wave-2 briefs are not written earlier.
- **Exact-value regressions during convergence** (D-10) — DD-9 lands as contract before
  any grid/edit slice; conformance cases include boundary values (2^53+, decimals).
- **SPA refactor blast radius** (S15) — bounded by S15a characterization tests landing
  first + DD-4 mock approval + no framework migration.
- **Live tier flakiness** — two profiles: `partial` skips-with-reason and reports;
  `full` fails on any absent/skipped engine (release gate). Retries only on
  reachability, never semantics.
- **Fixture pollution of the shared testbed** — per-run namespaces + teardown +
  recovery cleanup (S10b); no `t.Parallel()` against shared services.
- **Island erosion via test plumbing** — conformance suite + seeder are
  descriptor-driven and core-free; adapter live-env checks live outside the island.
- **Program stall** — every phase produces a durable artifact (audit files, contracts
  in spec, decision records); any session resumes from `plans/` + spec with zero chat
  context.

## 11. Promotion contract (what outlives this plan)

Durable homes: family excellence contracts + presentation/value-wire contracts →
`spec-dataconsole.md` (§6 refresh + new §7) **at S12/S13 approval**; conformance
suite + drift pins → tests (the behavior home); audit distillate → spec notes or
deleted (transient); this plan → archive at S25. CLAUDE.md gains at most one trap
line if the program surfaces a new cross-cutting trap.
