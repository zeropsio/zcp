# Phase 1 tracker — F1 develop-first-deploy-scaffold-yaml drops

Started: 2026-04-27
Closed: 2026-04-27

Plan: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` §5 Phase 1.

## ENTRY check

- [x] Phase 0 EXIT met (commit `00459f02`; tracker `phase-0-tracker-v3.md` closed; round-2 Codex APPROVE).

## Phase 1 work units

| # | atom | line range | action | initial size | final size | delta | commit | codex round | notes |
|---|---|---|---|---:|---:|---:|---|---|---|
| 1 | `develop-first-deploy-scaffold-yaml.md` | L41-L45 (content-root tip block) | DROP | 1,438 B/render | 1,048 B/render | −390 B/render | (this commit) | SKIPPED (LOW-risk per plan §5 Phase 1 step 3) | tilde-extract / preserve detail; cross-link to `develop-deploy-modes` already at L24 + L39 |
| 2 | `develop-first-deploy-scaffold-yaml.md` | L47 (schema-fetch line) | DROP | included in row 1 measurement | included in row 1 | included in row 1 | (this commit) | SKIPPED (LOW-risk) | generic advice; agent has `zerops_knowledge` tool |

## Probe re-run (post-F1)

| Fixture | pre-F1 | post-F1 | delta | atom-fires |
|---|---:|---:|---:|---:|
| develop_first_deploy_standard_container | 20,643 B | 20,253 B | −390 B | 1× scaffold-yaml |
| develop_first_deploy_implicit_webserver_standard | 21,947 B | 21,557 B | −390 B | 1× scaffold-yaml |
| develop_first_deploy_two_runtime_pairs_standard | 22,394 B | 22,004 B | −390 B | 1× scaffold-yaml |
| develop_first_deploy_standard_single_service | 20,588 B | 20,198 B | −390 B | 1× scaffold-yaml |
| develop_simple_deployed_container | 16,085 B | 16,085 B | 0 B | 0× (envelopeDeployStates: [never-deployed] axis filters atom out of deployed envelopes) |

**Aggregate first-deploy slice reduction: −1,560 B** (plan §4.3 estimate was ~1,080 B; Phase 1 EXIT expected ~1,000-1,200 B; actual exceeds estimate because plan estimated 270 B/render but actual block was 390 B/render).

Per-atom delta on scaffold-yaml: 1,438 → 1,048 B (−390 B per render).

## Verify gate

- [x] `make lint-local` 0 issues post-F1.
- [x] `go test ./internal/content/... ./internal/workflow/... -short -count=1 -race` green post-F1.

## Phase 1 EXIT readiness (per §5 Phase 1 EXIT)

- [x] F1 atom edits committed.
- [x] Probe re-run shows expected ~1,000-1,200 B aggregate first-deploy slice reduction (actual: −1,560 B, exceeds estimate).
- [x] `phase-1-tracker-v3.md` committed.
