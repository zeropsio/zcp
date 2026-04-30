# Rename `CloseDeployMode` → `DeliveryMode`

**Surfaced**: 2026-04-30 — close-mode framing sweep (commit `57c91749`).
The textual sweep reframed `CloseDeployMode` from "what action=close
does" to "develop session's delivery pattern + auto-close gate". The
field name itself still encodes the OLD model: `Close` + `Deploy` +
`Mode` reads as "the mode for the deploy at close" — exactly the
aspirational dispatcher framing the sweep just removed. Every
documentation pass has to spend a paragraph explaining "ignore the
name, here's what it actually does."

**Why deferred**: the textual sweep just shipped; we want at least one
testing cycle with the corrected docs/atoms before deciding the rename
is worth the blast radius. The field name still works in code (no
runtime bug); the misalignment is purely cognitive. Premature rename
risks Round-N drift being indistinguishable from rename churn.

**Trigger to promote**: any of —
1. Internal-testing teammates ask "why is it called `Close`*Deploy*Mode
   if close doesn't deploy?" more than twice in distinct sessions.
2. A future audit round flags new drift caused by the misleading name
   (e.g. an atom author re-introduces "close runs deploy" framing
   because the field name pulled them there).
3. Another deploy-axis rename ships (e.g. `GitPushState` →
   `GitDelivery`), making the bundled rename cheaper to land together.

## Sketch

Single sweeping rename, phased to keep each phase independently
verifiable per the 5-file phase cap convention:

**Phase 1 — Topology constants + serialization:**
- `internal/topology/types.go`: `CloseDeployMode` → `DeliveryMode`,
  `CloseModeAuto/GitPush/Manual/Unset` → `Delivery{Auto,GitPush,
  Manual,Unset}`. Keep string values (`"auto"`, `"git-push"`, …)
  byte-equal so on-disk `ServiceMeta.json` stays readable across
  versions WITHIN a single migration window.
- `internal/topology/aliases.go` + `predicates.go`: rename matching
  helpers.
- `internal/topology/types_test.go`: rename tests.
- `internal/runtime/meta.go`: rename `ServiceMeta.CloseDeployMode` +
  `CloseDeployModeConfirmed` field → `DeliveryMode` +
  `DeliveryModeConfirmed`. JSON tag stays `closeDeployMode` /
  `closeDeployModeConfirmed` for one release if migration shim is
  desired (delete in Phase 5).
- Verify: `go build ./...` + `go test ./internal/topology
  ./internal/runtime`.

**Phase 2 — Atom frontmatter axis:**
- `internal/workflow/atom.go::ParseAtom`: rename `closeDeployModes`
  field → `deliveryModes`. Accept both during transition.
- `internal/content/atoms/*.md` (~15 atoms): rename
  `closeDeployModes:` → `deliveryModes:` in YAML frontmatter.
- `internal/content/atoms_lint.go`: update axis-name validator.
- Verify: `go test ./internal/content ./internal/workflow`.

**Phase 3 — Handlers + ops:**
- `internal/tools/workflow.go::handleCloseMode` → `handleDeliveryMode`.
- `internal/tools/workflow_close_mode.go`: rename file + handler +
  input/output struct fields.
- `internal/ops/close_mode.go` (if exists): rename.
- `internal/workflow/build_plan.go`, `compute_envelope.go`,
  `synthesize.go`: rename references.
- The MCP action name itself: `action="close-mode"` →
  `action="delivery-mode"`. This is a tool-API change but per
  CLAUDE.local.md "no backward-compat constraints" we ship it clean.
- Verify: `go test ./internal/tools ./internal/ops ./internal/workflow`.

**Phase 4 — Spec + intro + audit docs:**
- `docs/spec-workflows.md` — every `closeDeployMode` / `close-mode` /
  `CloseDeployMode` mention.
- `docs/spec-knowledge-distribution.md`, `docs/spec-work-session.md`,
  `docs/spec-architecture.md`.
- `docs/intro-zerops-and-zcp.md` (the team intro added in `aa7a5db9`).
- `docs/audit-prerelease-internal-testing-2026-04-29.md`.
- `CLAUDE.md` — "Deploy config is three orthogonal dimensions" bullet.
- Verify: docs render coherently; spell-check sweep.

**Phase 5 — Eval + tests + cleanup:**
- `internal/eval/instruction_variants.go` (already on the C1 sweep
  list — bundle the rename if C1 hasn't shipped yet).
- `internal/eval/scenarios/*.md`.
- Strip the JSON-tag migration shim from Phase 1 if any.
- Strip the `closeDeployModes:` frontmatter accept-both shim from
  Phase 2.
- Re-run matrix simulator + scenario tests.

## Risks

- **API rename is user-visible**: agents that already know
  `action="close-mode"` will hit "Unknown action". Acceptable per
  pre-production constraint, but timing matters — don't ship mid-
  internal-testing-session. Wait for a clean week.
- **JSON-tag change in `ServiceMeta`**: old on-disk metas with
  `closeDeployMode` key won't deserialize into `deliveryMode` field
  unless the JSON tag stays the same OR a one-shot migration runs at
  engine boot. Prefer keeping JSON tags identical and renaming only
  Go-side fields, OR ship a migration helper that rewrites
  `.zcp/state/services/*.json` once.
- **Atom axis name `closeDeployModes` is referenced by lint rules
  AND by the matrix simulator**. Both need to learn `deliveryModes`.
- **The sister concept `CloseDeployModeConfirmed` boolean** also
  needs renaming. Don't leave a `DeliveryMode` + `CloseDeployModeConfirmed`
  Frankenstein — that would be the exact "half-removed feature" anti-
  pattern from the global CLAUDE.md.

## Refs

- Originating sweep: commit `57c91749` "docs(close-mode): reframe as
  delivery pattern, not close-handler dispatcher"
- Related lint backlog entry:
  `plans/backlog/close-mode-handler-dispatch-drift-lint.md` — would
  also need its banned-phrase regexes updated post-rename.
- Handler ground truth confirming the name is misleading:
  `internal/tools/workflow.go:873-902`.
- CLAUDE.local.md "Engineering Priority" — "No backward-compatibility
  constraints. Rename types, change signatures…"
