# Phase 0 tracker — Calibration

Started: 2026-04-28
Closed: TBD (pending Codex PRE-WORK round outcome + user go for Phase 1)

> Phase contract per `plans/deploy-strategy-decomposition-2026-04-28.md`
> §6 Phase 0. EXIT criteria: baseline files committed, Codex PRE-WORK
> APPROVE (or NEEDS-REVISION → revise plan in-place), tracker committed,
> verify gate green.

## Plan reference

- Plan SHA: `1172e427` (`plan(deploy-decomp): draft 12-phase deploy-strategy decomposition`)
- Plan file: `plans/deploy-strategy-decomposition-2026-04-28.md` (807 lines)
- Sister plans (precursors):
  - `plans/archive/atom-corpus-hygiene-followup-2-2026-04-27.md`
  - `plans/engine-atom-rendering-improvements-2026-04-27.md`

## Environment verification

| check | command | result |
|---|---|---|
| working tree | `git status --short` | clean (only plan untracked → committed in `1172e427`) |
| HEAD branch | `git rev-parse --abbrev-ref HEAD` | `main` |
| fast lint | `make lint-fast` | 0 issues (~3s) |
| short test suite | `go test ./... -short -count=1` | all packages PASS (~10s wall) |

## Baseline snapshot

Captured on 2026-04-28 at HEAD `1172e427`:

| artifact | path | content |
|---|---|---|
| HEAD commit | `plans/strategy-decomp/baseline-head.txt` | `1172e427646227dfbd48529903678b7072eed377` |
| atom corpus line counts | `plans/strategy-decomp/baseline-atoms.txt` | 77 atoms, 2854 total lines (incl. 21 push-git/manual/strategy atoms slated for restructure) |
| deprecated symbol call sites | `plans/strategy-decomp/baseline-callsites.txt` | 281 hits across `internal/` + `docs/` for `DeployStrategy\|PushGitTrigger\|StrategyPushGit\|StrategyPushDev\|StrategyManual` |

## Pre-Codex sanity checks (Claude-side, before PRE-WORK round)

Plan-cited claims spot-checked against current HEAD before launching Codex:

| claim | location | result |
|---|---|---|
| §10 anti-pattern: `develop-close-push-dev-local.md:6` has stage leakage | atom L6 | PASS — `modes: [dev, stage]` confirmed |
| Phase 6 §5: `workflow_develop.go` auto-deletes session on new intent | L114 | PASS — `_ = workflow.DeleteWorkSession(engine.StateDir(), os.Getpid())` confirmed |
| Phase 2 §3: `parseMeta` is single deserialization point | service_meta.go L218-220 | PASS — comment "Single deserialization path — both ReadServiceMeta and ListServiceMetas use this" |
| §5.2: `scenarios_test.go` push-git/standard/container fixture is single-snapshot | L820-822 | PASS — single `ServiceSnapshot` with hostname `appdev`, no pair half |
| Phase 1 §0: parser axes need extending (`parseModes`/`parseTriggers` exist) | atom.go L466,495 | PASS — both helpers exist; new axes (closeDeployModes/gitPushStates/buildIntegrations) absent |

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| Phase 0 PRE-WORK plan validation | PRE-WORK | DONE — NEEDS-REVISION (1 citation amendment) → effective APPROVE after in-place fix | `codex-round-p0-prework.md` | TBD (this Phase 0 EXIT commit) |

### PRE-WORK round outcome (2026-04-28)

- **Q1 (§4 decision invalidation)**: ALL DECISIONS VALID. Codex inspected
  post-2026-04-27 commits (E1/E2/E3, axis K/L/M/N enforcement, push-git
  atom split). No decision-text invalidation. R10 deferral language
  remains accurate (E2's `failureClassification` lives at deploy-response
  level; `TimelineEvent` still lacks the field). G5 ship gate remains
  pending (E1 covered StrategyUnset two-pair aggregate, NOT push-git
  dev-hostname single-render — Phase 3 must add the fixture).
- **Q2 (atoms already rendering proposed shape)**: NONE FOUND. Partial
  mechanics in `strategy-push-git-trigger-{actions,webhook}.md` show
  zcli/webhook plumbing but no orthogonality framing.
- **Bonus 1 (5 file:line spot-checks)**: 4/5 PASS. **1 FAIL** on §1 R3
  row — citation `compute_envelope.go:243-256` is stale; correct
  citation set is `compute_envelope.go:206-210` (Deployed assignment),
  `:262-272` (DeriveDeployed body), `deploy_git_push.go:215-229`
  (push-git stamps RecordDeployAttempt on git push success, not on
  build landing).
- **Bonus 2 (parser ordering safety)**: CONFIRMED. `validAtomFrontmatterKeys`
  is closed at `internal/workflow/atom.go:108-135`;
  `validateAtomFrontmatter` rejects unknown keys at L250-266. Phase 1.0
  parser extension MUST land before Phase 8 atom restructure.

### Amendment applied in-place

```diff
-| R3 | Envelope | 🔴 | `Deployed` field has split semantic (push-dev=build landed; push-git=git push succeeded) | `compute_envelope.go:243-256` |
+| R3 | Envelope | 🔴 | `Deployed` field has split semantic (push-dev=build landed; push-git=git push succeeded) | `compute_envelope.go:206-210`, `compute_envelope.go:262-272`, `deploy_git_push.go:215-229` (Codex PRE-WORK 2026-04-28: stale `:243-256` citation corrected) |
```

Per §5 Phase 0 EXIT contract + §10.5 work-economics rule (sister plan):
single concern, addressed in-place, consumer (Phase 1+ executor) identified
→ no further Codex round required. Effective verdict: APPROVE.

## Sub-pass work units

| # | sub-pass | initial state | final state | commit | notes |
|---|---|---|---|---|---|
| 1 | env verification (lint-fast + go test -short) | unverified | DONE — green | n/a | no code change required |
| 2 | commit plan file | uncommitted | committed `1172e427` | `1172e427` | plan was untracked at session start |
| 3 | create `plans/strategy-decomp/` + baseline files | dir absent | DONE | TBD | baseline-head/atoms/callsites |
| 4 | Phase 0 PRE-WORK Codex round | not run | DONE — NEEDS-REVISION (1 citation fix) | TBD (this commit) | output captured in `codex-round-p0-prework.md` |
| 5 | apply R3 citation amendment to plan | unrevised | DONE — surgical in-place edit | TBD (this commit) | only §1 R3 row touched; no decision-text change |
| 6 | commit baseline + tracker + plan amendment + Codex artifact | uncommitted | PENDING | TBD | single commit closes Phase 0 |
| 7 | verify gate post-snapshot + amendment | green pre-snapshot | PENDING (re-run before commit) | TBD | guards against drift |

## Phase 0 EXIT (§6)

- [x] Baseline `baseline-head.txt`, `baseline-atoms.txt`, `baseline-callsites.txt` written.
- [x] Codex PRE-WORK consumed: NEEDS-REVISION → 1 amendment applied in-place → effective APPROVE per §10.5 work-economics.
- [ ] `phase-0-tracker.md` committed (this commit).
- [ ] Verify gate green at commit time (re-run lint-fast + go test ./... -short before commit).
- [ ] User explicit go to enter Phase 1 (per session instruction: pause after PRE-WORK APPROVE).

## §15.2 EXIT enforcement (inherited from sister-plan schema)

- [ ] Every sub-pass row above has non-empty final state at close time.
- [ ] Every row that took action cites a commit hash.
- [ ] Codex round outcome cited.
- [ ] `Closed:` set to absolute date.

## Notes for Phase 1 entry

1. Phase 1 is LOW risk pure addition (§6 risk classification). Two commits:
   - `atom(P1): extend parser to support closeDeployModes/gitPushStates/buildIntegrations axes`
   - `topology(P1): add CloseDeployMode + GitPushState + BuildIntegration + IsPushSource predicate`
2. Phase 1.0 atom parser extension MUST land before Phase 1 topology types (per the plan's structural ordering — without parser support, any future atom touching the new axes triggers `validFrontMatterKeys` rejection that fails ALL atom rendering).
3. Old types (`DeployStrategy`, `StrategyPushGit`, etc.) stay in the topology vocabulary through Phase 9 — Phase 10 deletes them. This protects migration in Phase 2.
4. `IsPushSource(Mode)` truth table to pin in Phase 1 step 2: 6 Mode values × {true, false} = 6 rows. Per plan §3.2:
   - `ModeStandard`/`ModeSimple`/`ModeLocalStage`/`ModeLocalOnly` → true
   - `ModeStage`/`ModeDev` → false (ModeStage is build target; ModeDev is invalid combo with push-git)
5. The session was paused after Phase 0 EXIT per user instruction. Phase 1 begins ONLY after explicit user go.
