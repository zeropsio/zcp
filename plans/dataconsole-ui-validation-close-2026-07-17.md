# Data Console UI/UX Validation Program — Close — 2026-07-17

Status: CLOSED — both fix rounds deployed to the container and verified live.
G5 definitive run (2026-07-17 evening, tag `g5definitive`, deployed build
through `fe69956d`): **47/47 scenarios PASS, exit 0, zero S1, zero non-info
S2** (the single S2-tagged record is a status:info scope note — the known
whole-collection-delete backlog item). The closing loop also caught and fixed
two last items live: the cell editor grew its row ~3px on open (flex wrapper
+ border-offset fix, TAB-3 now measures 0.0px in all three states) and a
deploy-process trap — the EMBEDDED SPA is served from the extension's
materialized media dir, so every SPA change requires the extension reinstall,
not just the binary swap. Supersedes
`plans/dataconsole-ui-validation-program-2026-07-17.md` (the program's
proposal; this is its outcome record).

## 1. Why this ran

The excellence program (`plans/archive/dataconsole-excellence-program-2026-07-16.md`)
verified the console at four emulated layers — Go unit, JS unit, jsdom DOM,
HTTP conformance — and still missed three bugs Karel found in a ten-minute
manual pass, all in the one layer never driven live: the assembled embedded
UI (real webview → host broker → server → engine). This program added that
missing lane.

## 2. What ran

Two lanes, one harness (`internal/dataconsole/uitest/`, `f90181d2`..`cddad87f`):

- **Lane F (functional)** — scripted per-family scenarios, three oracles each
  (O-UI: DOM assertion in the real webview; O-ENGINE: an independent SSH CLI —
  `psql`/`redis-cli`/`curl`/`mc` — never the console's own API; O-HONESTY: the
  UI's claim equals engine truth).
- **Lane X (UX)** — a full state-gallery screenshot sweep, then two independent
  critic passes scored against a 10-point rubric.

Gates and rough agent assignment (16 Sonnet agents, A1–A15, plus this
documentation pass):

| Gate | What ran | Agent(s) |
|---|---|---|
| G0 | Real UI proven drivable end-to-end incl. the native VS Code write-confirm modal | Fable, inline |
| G1 | Harness built; CORE-1 (write-mode lifecycle across a service switch) green with full evidence chain | A1 |
| G2 | Lane F fan-out: tabular (db+mariadb+ch), kv+object+stream+standalone, document (es+docs+search+vectors) | A2, A3, A4 |
| G3 | Lane X: gallery sweep, then independent visual + interaction critics | A5, then A9, A10 |
| G4 | Adjudicate S1/S2 findings → fix → redeploy → regression re-run, twice (round 1, then round 2 + adversarial-review follow-up) | A6, A7, A8, A11, A12a, A12b, A13, A14 |
| G5 | Final full-matrix green run + report gallery | A15 (in flight at this draft) |

37 scripted scenarios across the 5 family files (CORE-1..7, TAB-1..10,
KV-1..6, OBJ-1..5, DOC-1..6, STR-1..3), 44 evidence directories, ~530
screenshots, plus a separate 28-control exhaustive button audit
(`fc4da349`) that proved download/upload round-trip to disk on the real
container filesystem.

## 3. Findings and headline fixes

254 raw finding RECORDS across 9 agent JSONL files — a count of records, not
of defects: it includes per-engine re-confirmations of one root cause
(postgres AND mariadb; the DELETE framing bug alone was rediscovered by
CORE-1, KV-5, TAB-7, and three DOC-4 engines before being root-caused once),
superseded pre-fix observations, and informational spec-conformance notes.
The authoritative defect ledger is the commit list below — ~25 distinct
product defects/gaps fixed across the two rounds, plus the polish batch.
5 findings were retracted or superseded on adjudication as test-harness
artifacts, not product bugs (a bad assertion, a test-design error, a flaky
second-run false negative) — recorded honestly rather than dropped silently.

Headline S1/S2 fixes, by commit:

- **`d114668f`** — six live-audit fixes in one pass: the DELETE root cause
  (Node's `http.request` doesn't chunk a DELETE body without an explicit
  `Content-Length`, so every embedded delete — node/row/entry, every family —
  400'd as "Invalid request"); a tree-load generation guard (two in-flight
  root loads duplicated every node, and a stale generation's closures could
  target the previous service); Escape now cancels a cell edit instead of
  firing blur→commit; mutation affordances render only when enabled (no
  disabled-but-visible controls on view-only engines); per-type KV create
  fields (hash/zset creation was structurally impossible); action-aware
  `ErrConflict` copy.
- **`e0350867`** — meilisearch delete now waits for task completion (I-1):
  the UI previously reported "Deleted." while the document still existed.
- **`1f284597` + `5d9fc065`** — tabular engine rejections (SQL syntax, a
  wrong-type literal, a constraint violation) reclassify from `ErrUpstream`
  ("the platform broke") to `ErrInvalid` (an honest 400); the sanitized
  reason now crosses the wire via `provider.WithPublicDetail` instead of
  stopping at the server log.
- **`3a824246`** — the SPA renders that detail; sidebar status badges no
  longer crush to a single glyph at narrow widths.
- **`8d827d3b`** (round-2 UX batch, B1–B9) — the modal never loses typed
  input on a rejection (disables + "Working…" in flight, reopens with the
  typed values + inline error instead of discarding them); Escape/backdrop
  dismiss + a focus trap; every non-editable cell gets a click-to-view
  full-value path; a content generation guard mirroring the tree's; Enter
  submits multi-field forms; delete confirms name their target; an explicit
  NULL affordance; per-folder upload bars; qdrant Search gated on capability.
- **`cddad87f`** (adversarial-review follow-up, 7 fixes) — extended the
  generation guard to grid "Load more" and `/api/refresh`; a `modalEpoch` so
  a nested prompt-then-confirm flow (Rename, Set TTL) survives its outer
  modal's own completion; client-side validation failures now throw into the
  same inline-error path as a server rejection instead of silently
  discarding input; NULL vs `""` are distinct in the cell editor's
  unchanged-check; the focus trap excludes hidden/disabled controls and
  parks on the modal box when nothing is focusable; a single-folder bucket
  root keeps its own upload bar alongside the auto-expanded child's.

## 4. Deliberately not changed

- **Blur-commit on grid cell edit.** Flagged by the interaction critic
  (clicking away from an edited cell commits it, not just Enter) but
  adjudicated as the intended grid-editing idiom, not a bug — Escape is the
  distinct, working cancel path. Left as-is.
- **Standalone reach depth and discoverability.** DD-C scoped standalone Lane
  F coverage to a smoke test (CORE-3 + one render per family) rather than a
  full duplicate scenario matrix, since the broker/write path is the actual
  risk surface and the server + SPA are shared between reach paths. There is
  also still deliberately no in-UI "open in browser" button (spec §4.2,
  §4.3) — a developer reaches the standalone tab through code-server's own
  proxy, not a console-provided affordance. Both are spec-intentional.
- **mysql as a third tabular dialect.** DD-B relied on mariadb for dialect
  coverage instead of importing mysql into the eval project; tabular
  findings did not turn out to be dialect-shaped, so the recommendation
  stands as a revisit-if-needed, not a permanent close.

## 5. Follow-up backlog

- **meilisearch's `sanitizeTaskReason` → `PublicDetail` candidate.** Tabular's
  `engineErr` attaches its sanitized rejection reason via
  `provider.WithPublicDetail` so it crosses the wire (§7.2); meilisearch's
  own task-failure reason sanitizer does not yet do the same and still stops
  at the flat generic message. Not required for I-1/I-2 compliance today
  (the task-confirmation tail already makes success honest), but the same
  wire-detail treatment would be a small, consistent follow-up.
- **Whole-collection KV delete affordance.** Per-entry delete (a hash
  field/set/zset member) and whole-key delete (a string blob or a collection
  via the tree node's own delete) both exist; worth a dedicated check on
  whether a collection's own opened/expanded view needs a delete affordance
  distinct from the tree node's, or whether the tree-node path already
  covers it end to end.
- **mysql third-dialect coverage** — see DD-B above; still a deliberate skip,
  not a closed decision.
- **Insert-row required/optional field cues pending column-meta
  nullability.** The insert-row form reuses the raw SQL type name as every
  field's placeholder with no required-vs-optional distinction (flagged S4
  by the interaction critic). Needs a nullable flag added to `Column` before
  the SPA can render an honest cue — deferred as polish, not correctness.

## 6. Artifacts

- Harness: `internal/dataconsole/uitest/` (`run.js`, `gallery.js`,
  `button-audit.js`; committed as a permanent live-only tier, DD-A).
- Findings: `internal/dataconsole/uitest/findings/*.jsonl` (gitignored,
  local to the eval run).
- Evidence: `internal/dataconsole/uitest/evidence/<scenario>/*.png`
  (gitignored).
