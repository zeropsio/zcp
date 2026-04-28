# Phase 0 tracker — Calibration

Started: 2026-04-28
Closed: 2026-04-28 (Phase 0 EXIT commit pending below); Phase 1 paused awaiting user go per session instruction

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 0.
> EXIT criteria: baseline files committed, Codex PRE-WORK APPROVE (or
> NEEDS-REVISION amendments folded in), tracker committed, verify gate green.

## Plan reference

- Plan SHA at session start: `b743cda041c6082e125f87691fe7ebf42581d383`
- Plan file: `plans/export-buildfromgit-2026-04-28.md` (794 lines)
- Sister plan (precursor): `plans/archive/strategy-decomp/` (deploy-strategy-decomposition; SHIP-WITH-NOTES)
- Related: `plans/archive/atom-corpus-hygiene-followup-2-2026-04-27.md` (Axis K/L/M/N hygiene)

## Environment verification

| check | command | result |
|---|---|---|
| working tree | `git status` | clean (HEAD = `b743cda0`, 1 commit ahead of origin/main = the plan commit) |
| HEAD branch | `git rev-parse --abbrev-ref HEAD` | `main` |
| fast lint | `make lint-fast` | 0 issues |
| short test suite | `go test ./... -short -count=1` | all packages PASS (~10s wall) |

## Baseline snapshot

Captured on 2026-04-28 at HEAD `b743cda0`:

| artifact | path | content |
|---|---|---|
| HEAD commit | `plans/export-buildfromgit/baseline-head.txt` | `b743cda041c6082e125f87691fe7ebf42581d383` |
| target file LOC | `plans/export-buildfromgit/baseline-loc.txt` | export.md=229, ops/export.go=132, workflow_immediate.go=39 |
| symbol call sites | `plans/export-buildfromgit/baseline-callsites.txt` | 550 hits across `internal/` + `docs/` for `PhaseExportActive\|buildFromGit\|zeropsSetup` |
| import schema (live) | `plans/export-buildfromgit/import-schema.json` | 31103 bytes |
| zerops.yaml schema (live) | `plans/export-buildfromgit/zerops-schema.json` | 23839 bytes |

### Schema delta vs embedded testdata

- `internal/schema/testdata/zerops_yml_schema.json` — IDENTICAL to live (23839 bytes both).
- `internal/schema/testdata/import_yml_schema.json` — DIFFERS (live is 31103 bytes; embedded is 30901; +202 bytes drift).
- Phase 5 input: must decide whether to refresh the embedded testdata + re-cache or fetch fresh at runtime.

## Pre-Codex sanity checks (Claude-side)

Plan-cited claims spot-checked against current HEAD before launching Codex (memory rule: pre-baked claims need grep before trust):

| claim | location | result |
|---|---|---|
| `internal/tools/workflow.go:142` (router) → `synthesizeImmediateGuidance` at `:150` | actual L150 confirmed; router invocation L150 + L300 | **DRIFT** — plan says `:142`, actual `:150`. ±8 lines. |
| `internal/tools/workflow_immediate.go:35-38` returns `PhaseExportActive` for `"export"` | actual L36 returns `workflow.PhaseExportActive, true` | PASS (within range) |
| `internal/workflow/synthesize.go:524-526` — `SynthesizeImmediatePhase` | actual L524 confirmed | PASS |
| `internal/workflow/build_plan.go:43-48` — `PhaseExportActive` yields empty plan | actual L43-44 in `case PhaseStrategySetup, PhaseExportActive:` | PASS |
| `internal/workflow/router.go:209` — `workflow="export"` hint | actual L208-209 (Workflow + Hint) | PASS |
| `internal/workflow/scenarios_test.go:600` — S12 fixture | test fn at L591, S12 body extends through L617 | DRIFT — plan says `:600`, actual fn starts L591 |
| `internal/workflow/scenarios_test.go:882` — pin coverage closure | actual L881 | PASS (within ±1) |
| `internal/workflow/corpus_coverage_test.go:768` — export envelope | actual L766 (`Name: "export_active"`) | PASS (within ±2) |
| `internal/workflow/service_meta.go:47-48` — `RemoteURL` cache | actual L48 with documented "cache; runtime source of truth = git remote get-url origin" | PASS |
| `internal/schema/schema.go:37-49` — schema parsing infra | confirmed; `ParseImportYmlSchema` + `ParseZeropsYmlSchema` exist | PASS |

### Plan gaps surfaced by sanity check (pre-Codex)

1. **Standalone `zerops_export` MCP tool coexists.** `internal/tools/export.go:1-41` registers a `zerops_export` tool that wraps `ops.ExportProject` for raw YAML output. The plan only targets the `zerops_workflow workflow="export"` phase. Open question: does the standalone tool stay, get repurposed, or get removed? Routed to Codex agent A.

2. **Schema validator gap.** `internal/schema/validate.go` only has `ValidateZeropsYmlRaw` (unknown-field detection against extracted enums). There is NO full JSON Schema validator. Phase 5's `ValidateImportYAML` + `ValidateZeropsYAML` need either a third-party JSON Schema library or hand-rolled per-rule validation. Plan §5 may underestimate effort. Routed to Codex agent A.

3. **Chain pattern is INLINE, not a helper.** `internal/tools/workflow_close_mode.go:120-123` builds `setupPointers` inline; there is no `chainSetupGitPushGuidance(...)` function. Plan §6 Phase 3 ("Reuse existing `chainSetupGitPushGuidance(...)` pattern") needs to either extract a helper first OR copy the inline pattern. Routed to Codex agent B.

4. **All three new axes already wired.** `closeDeployModes` / `gitPushStates` / `buildIntegrations` are present in `validAtomFrontmatterKeys` (`internal/workflow/atom.go:131-133`) and have parsers (`parseCloseDeployModes` L502, `parseGitPushStates` L517+). No parser-extension Phase 1.0 needed (unlike sister plan).

5. **Filename change impacts corpus_coverage fixture.** Phase 4 changes `import.yaml` → `zerops-project-import.yaml`. The `export_active` fixture's `MustContain` includes the literal `"import.yaml"` (`corpus_coverage_test.go:778`). Phase 4 (or Phase 7 test work) must update this. Routed to Codex agent B.

## Codex rounds

Three parallel Codex agents launched (per plan §7 — "Phase 0 PRE-WORK can fan out: one agent challenges decisions, one agent independently audits the rendering pipeline, one agent samples classification on a real Laravel/Node app"):

| agent | scope | output target | status |
|---|---|---|---|
| A | §4 decisions + Q1/Q5 challenges + critical gap rulings (zerops_export coexistence, schema validator complexity) | `codex-round-p0-prework-decisions.md` | running |
| B | rendering pipeline + atom hygiene + multi-call handler state + chain pattern adequacy + e2e surface delta | `codex-round-p0-prework-rendering.md` | running |
| C | classification protocol field-test on Laravel/Node/Django + 7 failure modes (M1–M7) | `codex-round-p0-prework-classification.md` | running |

### Round outcomes

| agent | scope | duration | verdict |
|---|---|---|---|
| A | decisions / integrity / Q1+Q5 + critical gaps | ~268s | NEEDS-REVISION → effective APPROVE after amendments |
| B | rendering pipeline + atom hygiene + chain pattern | ~236s | CONDITIONAL-APPROVE (5 BLOCKING + 3 NON-BLOCKING) → effective APPROVE after amendments |
| C | classification protocol field-test (Laravel/Node/Django + M1–M7) | ~143s | NEEDS-REVISION → effective APPROVE after amendments |

**Convergent verdict**: NEEDS-REVISION across all three. After in-place amendments folded per sister-plan §10.5 work-economics rule, effective verdict converges to APPROVE.

### Amendments applied (13 total)

All folded in-place into `plans/export-buildfromgit-2026-04-28.md`. Full synthesis table at plan §13. Highlights:

1. §1 + §4: Added X9 row (standalone `zerops_export` retained as orthogonal raw-export surface).
2. §4: Added JSON Schema validator decision (vendor `github.com/santhosh-tekuri/jsonschema/v5`).
3. §3.4: Five surgical edits to classification protocol (provenance + framework reasoning + aliased imports + empty/sentinel handling + privacy-flag plain config).
4. §3.5 + §3.4: Phase B per-env review table mandatory before Phase C.
5. §5.2: Full entry-points citation hygiene refresh.
6. §6 Phase 2: Explicit gate that `ops.ExportBundle` must land before Phase 4 atoms reference its fields.
7. §6 Phase 3: Reframe `chainSetupGitPushGuidance(...)` (doesn't exist) as inline pattern at `workflow_close_mode.go:120-136`. Clarify per-request inputs are stateless.
8. §6 Phase 4: Explicit `priority: 1..6` for new atoms; drop `gitPushStates` axis from `export-publish-needs-setup.md` (silently never fires); use `{repoUrl}` not `{repoURL}`; axis-marker discipline for SSH-path prose.
9. §6 Phase 5: Acknowledge no JSON Schema lib vendored; vendor decision pinned; refresh embedded testdata (`import_yml_schema.json` is 202B behind live).
10. §6 Phase 7 + Phase 4: `corpus_coverage_test.go:766-779` MustContain update; S12 split (a/b/c/d).

No structural redesign required. Plan phase order is preserved.

## Phase 0 EXIT

- [x] Baseline files written (`baseline-head.txt`, `baseline-loc.txt`, `baseline-callsites.txt`).
- [x] Live JSON schemas cached (`import-schema.json`, `zerops-schema.json`).
- [x] Pre-Codex sanity checks done; 5 gaps routed to Codex agents.
- [x] Codex PRE-WORK consumed: 3 agents converged on NEEDS-REVISION → 13 in-place amendments → effective APPROVE.
- [x] All three round transcripts persisted.
- [x] Plan amendments folded in-place; new §13 synthesis section added.
- [x] `phase-0-tracker.md` finalized.
- [ ] Phase 0 EXIT commit (single commit closing Phase 0 — see commit hash below once made).
- [ ] User explicit go to enter Phase 1 (per session instruction: pause after PRE-WORK APPROVE).

### §15.2 EXIT enforcement (inherited from sister-plan schema)

- [x] Every action above has a non-empty final state.
- [x] Three Codex round transcripts cited (`codex-round-p0-prework-decisions.md`, `codex-round-p0-prework-rendering.md`, `codex-round-p0-prework-classification.md`).
- [x] Convergent verdict recorded; amendment count = 13.

## Notes for Phase 1 entry

1. Phase 1 is LOW risk topology types only (`ExportVariant`, `SecretClassification`).
2. Phase 1 commit shape per plan §6 Phase 1: `topology(P1): add ExportVariant + SecretClassification enums`.
3. **No parser-extension Phase 1.0 needed** (in contrast to sister plan) — `closeDeployModes` / `gitPushStates` / `buildIntegrations` are already in `validAtomFrontmatterKeys` (`internal/workflow/atom.go:131-133`).
4. The plan now pins 13 amendments — all are surgical, none invalidate phase ordering. Phase 1's `ExportVariant` + `SecretClassification` types remain unchanged from the original §6 Phase 1.
5. Phase 5 is now MEDIUM risk (was LOW-MEDIUM) due to JSON Schema lib vendor work. No change to phase order.
6. Session pause point: Phase 1 starts ONLY after explicit user go.
