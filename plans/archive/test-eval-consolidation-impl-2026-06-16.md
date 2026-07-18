# Test + eval consolidation — implementation plan (2026-06-16)

Executes the audit in `plans/e2e-eval-surface-consolidation-2026-06-16.md`. Branch
`test-eval-consolidation`. Baseline `go build ./... && go test ./... -short` GREEN before start.

## Locked decisions (stated, not silent)

1. **eval-as-gate → keep observation-only.** Do NOT build an LLM-judge CI grader (large; contradicts
   the deliberate "local session is the grader" design). Build the manifest with a `gating` field so
   the capability is structurally present; document the path-to-hard-gate in the spec. Consequence:
   e2e stays the everything-net; it shrinks only by the redundancy removals already identified.
2. **Build tags 4 → 2.** Delete `probe`. Fold `live` → `api`. Final real-platform tiers:
   - `api` = read-mostly real platform (SDK contract + catalog/recipe audits + export composer audit).
   - `e2e` = mutating real-platform lifecycle.
   - default (untagged) = everything provable offline.
   - behavioral eval = agent-judgment (markdown + flow-eval runner; NOT a build tag).
3. **§8 leans:** `deploy_prepare_fail` KEEP (only `build_logs` merges); `subdomain_test`,
   `git_delivery_live`, `bootstrap_advanced` KEEP (doc-fix only where noted).
4. **Era-2 functional grader** (`internal/eval/scenarios/*.md` + `RunScenario` + `cmd/zcp/eval.go`
   functional subcommands + their `scenarios_test.go`): retire IFF cleanly separable from the live
   `RunBehavioralScenario` path in `internal/eval`. Agent investigates first; if entangled → STOP + report.

## Architectural target (the "do it better")

`docs/spec-testing-architecture.md` becomes the governing MAP — the test-surface analog of
"one home per knowledge". States: the placement pyramid (offline→`api`→`e2e`→eval), the rule
"every behavior has ONE verification home chosen by a rule not by convenience", the division of
labor (api/e2e = deterministic correctness; eval = non-deterministic agent navigation), and the
eval-as-gate decision + path. Plus a machine-enforced **scenario manifest** (front-matter:
id/behavior/canonical/overlaps/gating/last-reviewed) + lint, so drift can't be invisible again.

## Phases → agent batches → gates

Each batch = agents on DISJOINT packages; I run gates between batches. Gate =
`go build ./... && go vet -tags api,e2e ./... && go test ./... -short` (+ `make lint-local` at end).

**Batch A — pure deletions + hygiene (lowest risk, parallel, disjoint pkgs):**
- A-del: delete `logfetcher_probe{,2,3}_test.go` (atomic) + `internal/auth/auth_api_test.go` + 3 orphan
  funcs in `e2e/bootstrap_helpers_test.go`; drop `probe` from `make vet-tags` + `ci.yml`, retitle tag list.
- A-eval: delete Era-1/Era-2 dead scripts (`eval/functional/`, `eval/AGENT_PROMPT.md`, `eval/run.sh`,
  `eval/taxonomy.yaml`, `eval/scripts/*.py` except none-kept, dead `.sh`) + frozen outputs
  (`wave1-analysis.json`, `audits/usersim-*.md`). KEEP `build-deploy.sh`, `timeline.py`,
  `distill.sh`, `run-batch.sh`. Grep-verify zero refs before each delete.
- A-plans: `git rm` 3 shipped backlogs; `git mv` 3 roadmap plans → `plans/archive/`, repoint
  `internal/tools/live_export_test.go` provenance comment.

**Batch B — drift rewrites (parallel, disjoint files):**
- B-knowledge: `knowledge_quality_test.go` single-owner (export `serviceNormalizer` Keys() accessor,
  derive table, drop keydb/rabbitmq, fix count) + `instructions_test.go` add `AdoptionBootstrapping`.
- B-scenarios: rewrite stale eval scenarios (`git-push-setup-with-cicd-method-prompt`,
  `launch-with-existing-cicd`, `launch-to-existing-prod-project`, `launch-production-pipeline-not-configured`),
  fix `.netrc`/restart in `git-push-setup-then-actions`, dedup `delivery-git-push-actions-setup`,
  un-defer-or-delete `existing-simple-mode-add-endpoint`; EXTEND drift guard with semantic tokens.
- B-e2e-merge: merge `build_logs_test.go` → `deploy_failure_classification_e2::BuildPhase` (transplant
  asserts) + update `Makefile:163` run filter; rewrite `launch_baseline_test.go` to enum-canary-only +
  relocate `defaultAPIHost()` to `helpers_test.go`; delete `launch_existing_project_test.go`.

**Batch C — tag architecture + Era-2 + the spec (sequential, each touches shared config):**
- C-tagmove: move `update_test.go` + `orphan_cleanup_safety_test.go` to untagged (relocate
  `hasTestPrefix` to a non-tagged home if needed); verify they run in `go test ./...`.
- C-livefold: retag the 3 `live` tests → `api` (`live_export_test.go` + `live_test_harness.go` in
  internal/tools; `schema/live_audit_test.go`; `knowledge/recipes_live_audit_test.go`); remove `live`
  from `make vet-tags` + `ci.yml`.
- C-eval-grader: investigate + (if separable) retire Era-2 functional grader.
- C-spec: write `docs/spec-testing-architecture.md` + scenario manifest format + lint + CLAUDE.md pointer.

**Review:** Codex review of the scoped diff + a review workflow (correctness + did-we-lose-coverage +
plan-fidelity per-item). Address findings. Final `make lint-local` + `go test -race` on touched pkgs.

## Coverage NOT to lose (gate every merge/delete on these)

- `build_logs` `mode=ssh` + `buildLogsSource=build_container` + log-content asserts (transplant, don't drop).
- the `launch_baseline` live enum canary (unchecked-string-cast drift detection).
- `knowledge_quality` Phase-2 live port/env validation.
- `build-deploy.sh` (flow-eval ships the binary via it).
- the live catalog/composer audits (rehomed to `api`, not deleted).

---

## SHIPPED (2026-06-16) — per-item status + review

Branch `test-eval-consolidation` (uncommitted, atop the separate launch-finalization changeset).
Gates: `go build ./...` ✓ · `go vet -tags api,e2e ./...` ✓ · `go test ./... -short` ✓ ·
`go test -race` (eval/content/tools/platform) ✓ · `make lint-local` **0 issues** ✓.

| Item | Status | Note |
|---|---|---|
| Batch A — probe trio + auth stub + 3 orphan helpers + Makefile/ci | done | `probe` tag retired |
| Batch A — eval museum (Era-1 + Era-2 scripts, frozen outputs) | done | kept `build-deploy.sh`, `timeline.py` |
| Batch A — 3 shipped backlogs closed + 3 roadmap plans archived | done | provenance repointed |
| Batch B — knowledge_quality single-owner + instructions enum | done | derives from `ServiceNormalizerKeys()` (12 keys); review: clean |
| Batch B — 4 stale scenarios rewritten + drift guard extended | done | + a major scenario bug fixed in review (B2 below) |
| Batch B — build_logs→failure-classification merge | done | review found 1 dropped assert (`BUILD_FAILED`) → re-added |
| Batch B — launch_baseline→canary + launch_existing delete | done | review: enum canary preserved (clean); existing-project live decode covered by `TestAPI_GetUserInfo`+`TestAPI_ListProjects` |
| Batch C — `live`→`api` fold (4 files) + Makefile/ci | done | 4 tags → 2 |
| Batch C — update + orphan_cleanup_safety → untagged | **partial (corrected in review)** | only `orphan_cleanup_safety`+`hasTestPrefix` moved; `update_test` REVERTED to `e2e` (it needs creds — review BLOCKER) |
| Batch C — Era-2 functional grader retired | done | behavioral runner provably intact (review: clean); orphaned `Expectation`/`FollowUp` residue also deleted in review |
| Batch C — docs/spec-testing-architecture.md + manifest lint + CLAUDE.md | done | gating claim corrected to honest (see deviation below) |
| **eval-as-gate** | **deferred (deliberate)** | observation-only kept; see deviation |

### Review — two independent adversarial passes (Codex gpt-5.5 + 6-dimension workflow)

- **1 BLOCKER (workflow caught, Codex missed):** untagging `update_test` made `TestE2E_AsyncUpdate`
  run in CI's non-short `go test -race ./...` where it fails without creds (update goroutine starts
  after the auth gate). My `-short` gate masked it. **Fixed:** reverted to `e2e` tag + honest comment.
- **1 major (Codex caught, workflow missed):** `launch-with-existing-cicd.md` told the agent to delete
  the prod project via `zerops_delete` (which deletes a SERVICE, not a project). **Fixed:** rewritten
  to `action="reset"` (the shipped in-tool teardown).
- **Confirmed clean by both:** coverage-loss (build_logs transplant, launch_baseline canary, probe→contract,
  Era-2 no-live-loss), tag architecture, knowledge_quality single-owner, AdoptionBootstrapping, behavioral
  eval intact, command tree, drift+manifest guards.
- **Minors swept:** evalisms skip-predicate (broke on the live→api retag — re-anchored to filename),
  `BUILD_FAILED` assert, logfetcher stale cursor claim + dead helper, orphaned `Expectation` struct,
  ~9 stale doc-comments / fixture / README references to deleted artifacts.

### Deviation flagged (plan-fidelity)

- **Locked-decision #1 said "build the manifest with a `gating` field."** On reflection an inert parsed
  field nothing consumes is plumbing-without-a-consumer (CLAUDE.md anti-pattern). I did NOT add the field;
  instead the spec now states honestly that a hard gate needs BOTH a grader AND a `gating` field, both
  deferred. The review's spec-fidelity dimension flagged the original (false) "lever already present" claim;
  it's now corrected. **This is a conscious deviation from my own locked decision — surfaced, not silent.**

### Out-of-scope observation (NOT fixed — launch-finalization area)

- `internal/tools/workflow_git_push_setup.go` doc-comments (`:111`, `:277`, `:522`) say the handler
  "restarts the push-source", but it calls no `RestartService` (other comments `:553/:612/:616` correctly
  say no restart). The rewritten scenario is now MORE correct than the handler's own prose. Doc-vs-code
  drift in the git-push area — flagged for a separate pass.
