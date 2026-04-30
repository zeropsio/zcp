# Pre-internal-testing fixes — plan (2026-04-30) — ARCHIVED

> **Status**: SHIPPED + ARCHIVED 2026-04-30 by branch
> `cleanup-pre-internal-testing` (P0-P7 complete).
>
> **Outcome**: 17/17 audit findings resolved or filed to `plans/backlog/`.
> Matrix simulator anomalies dropped 23 → 5 (-18); legacy `strategy`
> vocab WARN class fully eliminated (16 → 0); F9 narrow-scope briefing
> dropped from 35KB live-feedback shape to 24KB matrix demonstration
> (under 25KB cap).
>
> **Resolution table**: see
> `docs/audit-prerelease-internal-testing-2026-04-29.md` "Resolution
> status (2026-04-30)" section for the per-finding commit-hash map.
>
> **Backlog entries created**:
> - `plans/backlog/c3-failure-classification-async-events.md`
> - `plans/backlog/c9-recipe-git-push-scaffolding.md` (linked to existing
>   `plans/backlog/auto-wire-github-actions-secret.md`)
> - `plans/backlog/m1-glc-safety-net-identity-reset.md`
> - `plans/backlog/m2-stage-timing-validation-gate.md`
> - `plans/backlog/m4-aggregate-atom-placeholder-lint.md`
> - `plans/backlog/deploy-intent-resolver.md`
>
> **Reader contract.** Self-contained for a fresh Claude session opened with no
> prior context. Read this entire file before starting Phase 0.
>
> **Sister docs (authoritative for findings)**:
> - `docs/audit-prerelease-internal-testing-2026-04-29.md` — full audit with
>   17 verified findings (C1-C11, H1-H3, M1-M4) + 5 live-feedback findings
>   (F3-F8, F6/F7 already RESOLVED in dotnet recipe).
> - `internal/workflow/lifecycle_matrix_test.go` + `testdata/lifecycle-matrix.md`
>   — diagnostic matrix simulator. Re-run after each phase as drift gate.
>
> **This plan**: ships the pre-internal-testing fix-set in 7 phases, each
> independently committable, each gated by TDD + Codex PRE/POST review. The
> goal is a clean baseline before live agent testing starts.

---

## 1. Mental model recap

ZCP went through `deploy-strategy-decomposition` 2 weeks ago: split one
`DeployStrategy` enum into three orthogonal axes (`CloseDeployMode` /
`GitPushState` / `BuildIntegration`). Handler + types: clean. Atom corpus
+ eval scaffolding + spec text + a backward-compat shim in the strategy
gate: stale. Plus a handful of orthogonal HIGH bugs that surfaced via
live agent testing on `dotnet@9 + EF Core + Postgres` and matrix simulation.

The cleanup splits naturally into 6 substantive phases (after a setup
phase). Phase order respects: (a) BLOCKER vocab sweep first so all later
work runs against clean atoms; (b) UX-blocker fixes early so first test
session is usable; (c) structural separations (git-push lifecycle) last
because they touch async paths.

---

## 2. Phase tracker

| # | Phase | Status | Exit commit |
|---|---|---|---|
| P0 | Pre-flight — branch, baseline matrix sim, audit verification | DONE | `9669ebb5` |
| P1 | Vocabulary sweep (C1, C6, C7, C8, C11) — atomic commit, 40 files | DONE | `9e31c19b` |
| P2 | Subdomain eligibility unification (F8 root) | DONE | `287c821a` |
| P3 | Plan/spec alignment (H1, H2, C10) | DONE | `cfbd0793` |
| P4 | Git-push lifecycle separation (C2 root + C4) | DONE | `0ad55b35` |
| P5 | Response size dual-fix (H3 / F9 — atom aggregate + scope filter) | DONE | `7dcb1b46` (Lever A) + `c4140954` (Lever B) |
| P6 | UX bundle (C5+nohup-lint, F3 root, F5 root, M3, verify F5 follow-up) | DONE | `5d1eee18` (M3) + `7aacce24` (C5+lint) + `8ed3c365` (F3) + `d7486bb9` (F5) + `34d3403b` (verify F5 follow-up) |
| P7 | Verification + audit close + matrix sim final | DONE | `db05aa04` |

Update this table after each phase ships. Reference exit commit hash in the
table; full message body lives in the commit per `feedback_commits_as_llm_reflog.md`.

---

## 2.1 Root-cause depth review (2026-04-30)

Plan was put through a root-cause depth review (author + Codex independently).
Verdicts and Top 3 deepenings folded back into the relevant phases.

**Per-phase verdicts** (post-deepening):

| Phase | Pre-deepening | Post-deepening |
|---|---|---|
| P0 | ROOT | ROOT |
| P1 | DEEP-SYMPTOM | **ROOT** (added action-name lint + strategy-value lint) |
| P2 | DEEP-SYMPTOM | DEEP-SYMPTOM (canonical HTTP-route capability model is bigger refactor; deferred) |
| P3 | SYMPTOM (deliberate) | SYMPTOM (deliberate; H1 TODO names DeployIntent target) |
| P4 | DEEP-SYMPTOM | **ROOT** (added second git-push site at `deploy_local_git.go:215`) |
| P5 | DEEP-SYMPTOM | DEEP-SYMPTOM (Lever B safe per Codex atom audit; serviceScope flag deferred as future-proof) |
| P6 | DEEP-SYMPTOM | **ROOT** (F3 enrichment migrates 2 existing sites in same phase) |
| P7 | ROOT | ROOT (residuals categorized with backlog/plan ownership) |

Net: 5/7 ROOT, 2/7 DEEP-SYMPTOM, 0/7 pure SYMPTOM (P3 is deliberate per
author choice, documented in §3 below).

**Optional deepenings parked for future plans** (each ~0.5 day):
- Phase 3 C10 — `TestSpecXX_*` naming convention + lint requiring numbered
  spec invariants to have pin tests
- Phase 5 Lever B — explicit `serviceScope: work-session|project` axis
  flag (current corpus is safe; future-proof)
- Phase 2 — canonical "HTTP route eligibility" capability model in
  `ServiceMeta` (would unify subdomain + dev-server + verify-http paths)

## 3. Symptom-vs-root choices made up-front

Where deep analysis identified that the obvious patch is symptom-level, the
plan picks the root fix when effort is comparable. Recording the choice here
so future sessions know what was deliberate vs deferred.

| Finding | Choice | Rationale |
|---|---|---|
| **C2** | ROOT — remove auto-stamp from git-push handler; require explicit `record-deploy` | `record-deploy` action already exists; git-push currently bypasses it. Removing the bypass aligns sync vs async paths without new mechanism. |
| **F3** | ROOT — centralized "enrich platform 4xx with submitter-known data" layer | Pattern partly exists in `deploy_preflight.go:60-72` and `:126-159`. Centralization same effort as patch. |
| **F5** | ROOT — `workSessionState` struct in deploy/verify response | Envelope already computes `develop-closed-auto`. Patch = string change; root = small struct. |
| **C5** | BUNDLE — atom edit + corpus lint for `nohup`/`disown`/`& *$` | Catches the class. Same shape as axis K/L/M/N markers. |
| **H1** | SYMPTOM with TODO — extend `deployActionFor` to take ServiceSnapshot, branch on Mode/StageHostname | Root would be `DeployIntent` resolver (workflow/-side classifier) — too big for current scope, but symptom fix lays groundwork. Comment marks the structural target. |
| **H2** | SYMPTOM | Single atom edit. Root (atom command-snippet system) is generic refactor. |
| **C4** | SYMPTOM | Single atom axis change. |
| **C10** | SYMPTOM | Spec sweep + 1 pin test. Root (spec ↔ code automated drift detection) is its own initiative. |
| **M3** | SYMPTOM | 3 atom mode-axis additions. |

DEFERRED (not in this plan, queue for follow-up plans):
- C3 (failureClassification propagation into TimelineEvent)
- C9 (recipe git-push scaffolding) — see `plans/backlog/auto-wire-github-actions-secret.md`
- M1 (silent identity persistence in `.git/`)
- M2 (stage timing validation)
- M4 (aggregate-atom placeholder lint)
- DeployIntent resolver (would supersede H1's symptom fix)

---

## 4. Pre-flight (P0)

**Goal**: clean branch + reproducible baseline.

```sh
# 1. Branch from main
git checkout main && git pull --rebase origin main
git checkout -b cleanup-pre-internal-testing

# 2. Baseline matrix simulator
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
# Expected: PASS, "23 anomalies across 45 scenarios" (per audit doc, may be slightly different)
# Capture the markdown to compare later:
cp internal/workflow/testdata/lifecycle-matrix.md /tmp/matrix-baseline.md

# 3. Full test suite green
go test ./... -short -count=1
# Expected: PASS

# 4. Lint green
make lint-local
# Expected: PASS
```

If any of the above fails, STOP and resolve before starting P1. The plan
assumes a green starting state.

---

## 5. Phase blocks

Each phase has the same structure:

- **Scope**: which findings, which files
- **Why this is the fix**: short rationale (link to audit for detail)
- **Files**: explicit list — the LLM working the phase should not need to grep
- **Codex PRE-WORK**: brief template (read-only adversarial pass on the plan + intended changes)
- **Implementation**: TDD discipline (RED → GREEN → REFACTOR)
- **Verification**: tests + matrix simulator + manual checks
- **Codex POST-WORK**: brief template (read-only adversarial pass on the diff)
- **Commit shape**: subject + body skeleton

Codex helper invocation:
```sh
node /Users/macbook/.claude/plugins/cache/openai-codex/codex/1.0.4/scripts/codex-companion.mjs task "$PROMPT" --background
node /Users/macbook/.claude/plugins/cache/openai-codex/codex/1.0.4/scripts/codex-companion.mjs status <task-id>
node /Users/macbook/.claude/plugins/cache/openai-codex/codex/1.0.4/scripts/codex-companion.mjs result <task-id>
```

### Phase 1 — Vocabulary sweep (C1, C6, C7, C8, C11)

**Scope**: every reference to retired vocabulary from deploy-strategy
decomposition (~25 files), in one atomic commit.

**Why**: `deploy-strategy-decomposition` 2026-04-28 split `DeployStrategy →
{CloseDeployMode, GitPushState, BuildIntegration}`. Handler clean; atoms +
eval + tests + spec + tool gate stale. Agents reading these atoms call
`zerops_workflow action="strategy"` which the handler rejects with
`Unknown action`. Eval framework systematically teaches the dead API.

**Files** (verified by audit, current state confirmed 2026-04-29):

**Class-prevention lint (added 2026-04-30 per root-cause depth review)**:
The sweep alone is symptom-level — same drift can recur on the next refactor.
Add an atom-body lint to `internal/content/atoms_lint.go` that:
- Extracts every `zerops_workflow action="X"` pattern from atom bodies
- Verifies X is in the dispatcher accept list at `internal/tools/workflow.go:297`
- Build-time fail otherwise

Same lint also checks `zerops_deploy strategy="X"` values against the accept
list in `internal/tools/deploy_strategy_gate.go::validateDeployStrategyParam`.
This catches the class — any future action/strategy rename surfaces immediately.

Atoms — body uses dead `action="strategy"` or `strategies={}` syntax:
- `internal/content/atoms/develop-strategy-awareness.md` (line 23 — full rewrite of body, axes already correct)
- `internal/content/atoms/bootstrap-recipe-close.md` (line 25 area)
- `internal/content/atoms/develop-platform-rules-local.md` (lines 31-32 — replace `strategy=push-dev` with current vocab; keep `strategy=git-push` as that's still a valid `zerops_deploy` argument)

Atoms — IDs/titles/bodies use retired "Push-Dev" naming:
- `internal/content/atoms/develop-close-push-dev-{dev,simple,standard,local}.md`
- `internal/content/atoms/develop-push-dev-{deploy,workflow}-{container,local,dev,simple}.md`
- (Total 8 atoms — frontmatter axes already correct, only ID/title/body need rename)
- Decision: rename file slugs from `develop-(close-)?push-dev-*` to `develop-(close-)?close-mode-auto-*` to match the current `closeDeployMode=auto` axis. Update title leads. Sweep body prose.

Eval scaffolding:
- `internal/eval/instruction_variants.go` lines 48, 62 — variant prose teaches old API
- `internal/eval/scenarios/develop-strategy-unset-regression.md`
- `internal/eval/scenarios/strategy-push-git-setup.md`
- `internal/eval/scenarios/preseed/strategy-push-git-setup.sh`

Tests pinning the legacy text:
- `internal/eval/eval_test.go` line 318
- `internal/workflow/corpus_coverage_test.go` line 198
- `internal/workflow/bootstrap_outputs_test.go` lines 424, 660, 1083 (test comments still mention `DeployStrategy`, `StrategyConfirmed`)
- `internal/tools/workflow_phase5_test.go` line 249 (comment mentions `DeployStrategy`)
- `internal/workflow/router_test.go` line 226 (comment mentions deleted `migrateOldMeta`)

Tool gate:
- `internal/tools/deploy_strategy_gate.go` line 22 — DELETE the `case "push-dev"` from the accept list. Backward-compat shim that exists ONLY because atoms still emit it. CLAUDE.local.md forbids these.

Spec:
- `docs/spec-workflows.md` lines 914, 943, 962-963, 1103 — replace "push-dev" / "push-git" labels with "close-mode=auto" / "close-mode=git-push"

Plan:
- `plans/develop-flow-enhancements.md` — `git mv` to `plans/archive/` with header note "Phase 3 reverted, see workflow_close_test.go header for rationale" (this addresses C7). Phase 4 (mode expansion) IS implemented; archive is correct.

**Codex PRE-WORK** (2-3 min):
```
<task>
Adversarial review of the vocabulary sweep plan in plans/pre-internal-testing-fixes-2026-04-30.md Phase 1.
Open the listed files at the cited lines and verify:
1. Each file truly contains retired vocab as claimed
2. The proposed renames don't conflict with existing names (e.g. `develop-close-mode-auto-dev.md` doesn't already exist)
3. The 8-atom rename batch keeps reference integrity (any atom referencing renamed atoms via `references-atoms:` needs update)
4. The tool gate `case "push-dev"` removal is safe — verify no live test relies on the alias being accepted

Output: VERIFIED / RISK list with file:line. Don't write code.
</task>
```

**Implementation** (TDD):
1. RED: matrix simulator currently shows ~16 "legacy strategy vocab" anomalies. Capture as baseline.
2. GREEN: sweep all files. For renamed atoms, also update `references-atoms:` pointers in any atom that references them.
3. Re-run matrix simulator: anomaly count for "legacy strategy vocab" must be 0.
4. Re-run full test suite + lint.

**Verification**:
```sh
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
diff /tmp/matrix-baseline.md internal/workflow/testdata/lifecycle-matrix.md
# Expected: 16 fewer "legacy strategy vocab" warnings; no new FATAL/ERROR

# Verify zero retired vocab in atoms / eval / tests:
grep -rln 'action="strategy"\|strategies={\|strategy=push-dev\|"push-dev"' \
  internal/content/atoms/ internal/eval/ internal/workflow/ internal/tools/ docs/spec-workflows.md
# Expected: empty

go test ./... -short -count=1
make lint-local
```

**Codex POST-WORK** (2-3 min):
```
<task>
Adversarial review of the vocab sweep diff (HEAD vs HEAD~1).
Verify:
1. No retired vocab leaked through in any file
2. Atom rename batch preserved frontmatter integrity (axes unchanged)
3. references-atoms: pointers in other atoms were updated to renamed slugs
4. Test assertions that previously pinned the bug now pin the correct text
5. tool gate change broke no test

Output: VERIFIED or specific REGRESSION findings with file:line.
</task>
```

**Commit shape**:
```
sweep(deploy-strategy-vocab): retire action="strategy" + push-dev/push-git labels (~25 files)

Cleanup of language drift left after deploy-strategy-decomposition
(2026-04-28). Handler had been updated to action="close-mode" /
"git-push-setup" / "build-integration", but atoms, eval scaffolding,
test assertions, the tool gate's "push-dev" alias, the spec text, and
a stale plan all still emitted or expected the dead vocabulary.

Atomic so the corpus + tests + eval + spec land in a coherent state.

Verified by:
- matrix simulator anomaly count for "legacy strategy vocab"
  drops from 16 to 0
- grep for retired tokens across atoms/eval/spec returns empty
- full test suite + lint green

Findings closed: C1, C6, C7, C8, C11 (per audit-prerelease-2026-04-29).

Atom renames (slug → slug):
  develop-close-push-dev-dev → develop-close-mode-auto-dev
  develop-close-push-dev-simple → develop-close-mode-auto-simple
  develop-close-push-dev-standard → develop-close-mode-auto-standard
  develop-close-push-dev-local → develop-close-mode-auto-local
  develop-push-dev-deploy-container → develop-close-mode-auto-deploy-container
  ... (full list in commit body)
```

---

### Phase 2 — Subdomain eligibility unification (F8 root)

**Scope**: replace `modeEligibleForSubdomain` with a unified eligibility
predicate that consults HTTP signal (mode + runtime class + setup port +
live `ServiceStack.Ports[].HTTPSupport`).

**Why** (audit F8): live agent on `dotnet@9 dev mode` got
`warnings: ["auto-enable subdomain failed: Service stack is not http or https"]`.
Cause: `zsc noop` start = no HTTP listener yet → Zerops L7 router rejects.
The meta-NIL path (`deploy_subdomain.go:171-176`) ALREADY uses
`GetService().Ports[].HTTPSupport`. The meta-PRESENT path (`:117-129`) only
checks Mode. **Asymmetry between two paths solving the same problem.**

Hits every dev-mode dynamic-runtime first deploy across all recipes.

**Files**:
- `internal/tools/deploy_subdomain.go` — replace `modeEligibleForSubdomain` with
  unified `serviceEligibleForSubdomain(meta, serviceStack, resolvedSetup)`.
  Both meta-NIL and meta-PRESENT paths call the same function.
- `internal/tools/deploy_subdomain_test.go` — extend coverage:
  - dev + dynamic + zsc-noop start → NOT eligible (deferred hint, not warning)
  - dev + dynamic + real start + HTTPSupport=true → eligible
  - simple + implicit-webserver → eligible (no `start:` needed)
  - cross-deploy stage half → eligible (uses stage's HTTPSupport)

Optional: tweak the deferred-hint message to point at `zerops_dev_server
action=start hostname={hostname}` so the agent knows the next step.

**Codex PRE-WORK**:
```
<task>
Adversarial review of Phase 2 plan in plans/pre-internal-testing-fixes-2026-04-30.md.
Read internal/tools/deploy_subdomain.go end-to-end. Verify:
1. The unified predicate signature can be served by data both call sites
   already have access to (meta-nil path has serviceStack via GetService;
   meta-present path can fetch the same)
2. Resolved setup access — does maybeAutoEnableSubdomain currently parse
   zerops.yaml, or only consult meta? If not, what's the cost?
3. Test surface — is there a way to inject HTTPSupport into a fake
   ServiceStack in the existing test harness?

Output: VERIFIED / RISK list. Don't write code.
</task>
```

**Implementation**:
1. RED: add a test case that builds a meta-PRESENT envelope for dev+dynamic+zsc-noop and asserts the auto-enable does NOT trigger (current code triggers, the test fails).
2. GREEN: implement `serviceEligibleForSubdomain` consulting (mode, ports HTTPSupport, optional resolved setup). Both call sites switch to it.
3. REFACTOR: keep `modeEligibleForSubdomain` only as private helper if needed, otherwise delete.

**Verification**:
```sh
go test ./internal/tools -run 'TestMaybeAutoEnableSubdomain|TestModeEligible' -count=1 -v
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
# Matrix sim should not regress (this fix doesn't touch the corpus)
```

**Codex POST-WORK**:
```
<task>
Verify Phase 2 diff. Read internal/tools/deploy_subdomain.go before and after.
1. Unified predicate handles all 5 eligible-mode + 2 ineligible-mode cases
2. Asymmetry between meta-nil and meta-present paths is gone — same data
   source, same predicate
3. The dev+dynamic+zsc-noop case now defers cleanly with a constructive hint
4. No regression in the simple/standard/stage paths

Output: VERIFIED or REGRESSION list.
</task>
```

**Commit shape**:
```
fix(subdomain): unify auto-enable eligibility predicate (HTTP-signal-aware)

modeEligibleForSubdomain only checked topology Mode, so dev+dynamic
services with `zsc noop` start (no HTTP listener yet) tried auto-enable
post-deploy and got rejected by the platform with "Service stack is not
http or https". The meta-nil path at deploy_subdomain.go:171-176 already
consulted GetService().Ports[].HTTPSupport correctly — meta-present path
just didn't.

Replaces with serviceEligibleForSubdomain(meta, stack, setup) used by
both call sites. Includes the HTTPSupport check that meta-nil already had.

Failure mode also softened: the platform error is now caught and downgraded
to a deferred-hint pointing at zerops_dev_server action=start, instead of
surfacing as a warning the agent has to triage.

Findings closed: F8 (per audit-prerelease-2026-04-29).
```

---

### Phase 3 — Plan/spec alignment (H1, H2, C10)

**Scope**: three small fixes around standard-mode dev→stage promotion.

**H1** — `deployActionFor` extended to consult `ServiceSnapshot`:
- Files: `internal/workflow/build_plan.go` (extend signature), `build_plan_test.go` (add test)
- TODO comment on `deployActionFor`: "Future: replace with `DeployIntent` resolver
  (workflow-side ClassifyDeploy + setup picker). Symptom fix until that lands."
- For stage halves (`Mode=stage` AND `StageHostname` paired meta resolves to a dev
  half), emit `Args: {sourceService: <devHostname>, targetService: <hostname>, setup: "prod"}`.
- For everything else, current behavior: `Args: {targetService: <hostname>}`.

**H2** — `develop-first-deploy-promote-stage.md` body:
- Add `setup="prod"` to the `zerops_deploy` line. Mirror `develop-close-push-dev-standard.md:22`.

**C10** — spec D2b sweep:
- Files: `docs/spec-workflows.md` (D2b row), `CLAUDE.md` (D2b bullet under "Develop Flow / Work Session")
- Replace text describing "FirstDeployedAt guard" with the current "committed-code at workingDir" check. Cite `internal/tools/deploy_git_push.go:180-188` rationale comment.
- Add ONE pin test: `TestHandleGitPush_RefusesWithoutCommittedCode` (if not already present — check first).

**Codex PRE-WORK**:
```
<task>
Adversarial review of Phase 3 plan in plans/pre-internal-testing-fixes-2026-04-30.md.
Read internal/workflow/build_plan.go::deployActionFor and planDevelopActive.
Read internal/content/atoms/develop-first-deploy-promote-stage.md and
develop-close-push-dev-standard.md.
Read docs/spec-workflows.md D2b row + CLAUDE.md D2b bullet + the actual
guard at internal/tools/deploy_git_push.go:180-188.

Verify:
1. The H1 fix correctly identifies stage halves — what's the canonical
   way to detect "this is the stage half of a standard pair"?
   (pair-keyed meta, StageHostname comparison, Mode=stage check?)
2. H2 atom edit: confirm the rendered command will pass DM-2 + DM-3
   validation (cross-deploy with named setup="prod")
3. C10 spec text: read the rationale comment and confirm the new spec
   wording faithfully reflects it.

Output: VERIFIED / RISK list.
</task>
```

**Implementation**:
- TDD per finding. H1 first (most invasive), H2 second (single-atom),
  C10 third (spec text + 1 pin test).

**Verification**:
```sh
go test ./internal/workflow -run 'TestPlan|TestBuildPlan' -count=1 -v
go test ./internal/tools -run 'TestHandleGitPush' -count=1 -v
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
# Scenario 10.1 (standard pair) should now show correct cross-deploy plan in matrix output
```

**Codex POST-WORK**:
```
<task>
Verify Phase 3 diff. Three changes:
1. deployActionFor signature change — confirm all call sites updated
2. develop-first-deploy-promote-stage.md change — confirm setup="prod"
   added in the right place
3. spec D2b + CLAUDE.md D2b — confirm new text matches the actual handler
   behavior at deploy_git_push.go:180-188

Verify the H1 TODO comment names the structural target clearly so a
future ticket can pick it up without re-discovering DeployIntent.

Output: VERIFIED or specific findings.
</task>
```

**Commit shape**: 3 logical changes, **single commit OK** if Codex POST passes.
Otherwise split per finding.

---

### Phase 4 — Git-push lifecycle separation (C2 root)

**Scope**: remove auto-stamp from git-push handler. Require explicit
`record-deploy` call after agent observes `Status=ACTIVE` via
`zerops_events`.

**Why** (audit C2 root): `deploy_git_push.go:300` stamps `SucceededAt` on
push success → `RecordDeployAttempt` → `stampFirstDeployedAt` → next
envelope reports `Deployed=true`. For `BuildIntegration={webhook,actions}`
this happens BEFORE the async build runs. Agent verifies prematurely.

`record-deploy` action already exists for this purpose (`workflow_record_deploy.go`).
Atom `develop-record-external-deploy.md` documents the bridge. Git-push
just bypasses it. Removing the bypass aligns sync vs async paths without
any new mechanism.

**Files** — note: BOTH git-push success-stamp sites need the same fix (Codex
verified second site at `deploy_local_git.go:215` was missing from the
original draft):
- `internal/tools/deploy_git_push.go:354` — container git-push success stamp.
  Remove the `attempt.SucceededAt = time.Now()...` line; replace response
  with explicit nextStep guidance: "watch build via `zerops_events`; call
  `record-deploy` after `Status=ACTIVE`".
- `internal/tools/deploy_local_git.go:215-216` — local git-push success
  stamp (same shape as above). Same removal + nextStep guidance.
- Both files' `_test.go` companions — update tests; add
  `TestDeployGitPush_DoesNotStampDeployed` (container) and
  `TestDeployLocalGit_DoesNotStampDeployed` (local).
- Sync paths (`deploy_ssh.go:195`, `deploy_local.go:154`) DO stamp on
  success — confirmed correct (sync deploy IS the deploy). Don't touch.
- `internal/content/atoms/develop-close-mode-git-push.md` — update guidance
  to make the `record-deploy` step prominent (it's there at the bottom now)
- `internal/content/atoms/develop-build-observe.md` — already covers the
  events watch + record-deploy bridge; verify no edit needed
- BLOK 5's C4 fix (axis change to `[never-deployed]`) is a prerequisite
  for `develop-record-external-deploy` to fire when the agent needs it —
  Phase 6 must come AFTER Phase 4 if both ship in same release. Or fold C4
  into Phase 4 (small atom edit, related concern).

**Decision**: fold C4 into Phase 4 (one cohesive "git-push handoff cleanup").

**Codex PRE-WORK**:
```
<task>
Adversarial review of Phase 4 plan in plans/pre-internal-testing-fixes-2026-04-30.md.

Open:
- internal/tools/deploy_git_push.go:280-320 (the SucceededAt stamp area)
- internal/tools/workflow_record_deploy.go (the canonical bridge)
- internal/content/atoms/develop-close-mode-git-push.md
- internal/content/atoms/develop-record-external-deploy.md

Verify:
1. Removing the auto-stamp doesn't break any test that asserts
   "git-push response stamps Deployed". If yes, those tests need to be
   updated to assert "agent must call record-deploy" instead.
2. record-deploy axis fix (deployStates: [deployed] → [never-deployed]) —
   does the BUILT axis work correctly for the ASYNC case where the service
   is currently deployed=false? Or does the catch-22 actually apply
   differently than the audit claims?
3. Is there a synchronous git-push path (BuildIntegration=none) where
   removing the stamp WOULD break things? Per spec, BuildIntegration=none
   means "no ZCP-managed build" — which means the user has external CI
   doing the deploy. So stamping on git push is also wrong for that case
   (the build hasn't happened, just the push). Confirm.

Output: VERIFIED / RISK list.
</task>
```

**Implementation**:
1. RED: add `TestDeployGitPush_DoesNotStampDeployed` — fails because
   current code stamps.
2. GREEN: remove the stamp; update response to include `nextStep: "watch
   build via zerops_events; call record-deploy after Status=ACTIVE"`.
3. Update existing tests asserting the old behavior.
4. C4 inline: change `develop-record-external-deploy.md` axis from
   `deployStates: [deployed]` to `deployStates: [never-deployed]` so the
   atom fires when the agent actually needs it.
5. Update `develop-close-mode-git-push.md` guidance to make the
   `record-deploy` step prominent.

**Verification**:
```sh
go test ./internal/tools -run 'TestDeployGitPush|TestRecordDeploy' -count=1 -v
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
# develop-record-external-deploy should now appear in scenarios where
# deployStates=never-deployed AND buildIntegrations IN [webhook, actions]
```

**Codex POST-WORK**:
```
<task>
Verify Phase 4 diff.
1. deploy_git_push.go: stamp removed cleanly; response carries nextStep guidance
2. develop-record-external-deploy.md: axis flipped; body still makes sense
3. develop-close-mode-git-push.md: record-deploy step now prominent
4. No silent regression in BuildIntegration=none path

Specifically check: does the deploy response shape still satisfy the MCP
tool contract (response schema)? Removing a field could break it.

Output: VERIFIED or specific findings.
</task>
```

**Commit shape**:
```
fix(git-push): require explicit record-deploy after async build (remove auto-stamp)

The git-push deploy handler stamped FirstDeployedAt + SucceededAt
immediately on `git push` success. For BuildIntegration in
{webhook, actions}, the actual Zerops build is still async — the agent
would observe Deployed=true before the build had run. Verify against
stale state, retry, meanwhile real build progressing.

The record-deploy action already exists for exactly this bridge; the
develop-record-external-deploy atom documents how to use it. The git-push
handler was bypassing both.

Aligns sync vs async paths: now BOTH require record-deploy as the
"deploy landed and is observable" signal. Synchronous record-deploy is
fine because zerops_deploy returns synchronously for BuildIntegration=none
and for direct push deploys (where the deploy IS the deploy, not a
trigger for one).

Plus: develop-record-external-deploy axis was deployStates=[deployed],
which made the atom fire only AFTER the very state it was trying to
stamp. Catch-22 fixed by flipping to [never-deployed].

Findings closed: C2 (root), C4 (per audit-prerelease-2026-04-29).
```

---

### Phase 5 — Response size dual-fix (H3 / F9)

**Scope**: two independent levers that together drop the typical
multi-service `start workflow=develop` response from 35KB toward <10KB.

**Why** (audit H3 + live feedback F9): user reported 35KB on
`start workflow=develop` with 4 services. Atomic calls fine. Cause:

1. Of 80 atoms, only 3 use `multiService: aggregate` — the rest render
   N times for N matching services.
2. `Synthesize` filters atoms against `env.Services` (full project), not
   `env.WorkSession.Services` (work-session scope).

**Lever A — atom aggregate conversion**:

Files (the 14 candidates with `closeDeployModes:` axis that fire
per-service-identical):
- `internal/content/atoms/develop-build-observe.md`
- `internal/content/atoms/develop-close-mode-auto.md`
- `internal/content/atoms/develop-close-mode-git-push.md`
- `internal/content/atoms/develop-close-mode-manual.md`
- `internal/content/atoms/develop-strategy-awareness.md` (if it survived
  Phase 1 — it should have been rewritten to current vocab there)
- `internal/content/atoms/develop-strategy-review.md`
- The 8 `develop-close-mode-auto-*` / `develop-close-mode-auto-deploy-*` /
  `develop-close-mode-auto-workflow-*` atoms (renamed in Phase 1)

For each: add `multiService: aggregate` to frontmatter; rewrite body
prose so per-host content sits inside `{services-list:TEMPLATE}` and
shared prose is outside.

Pattern reference: `internal/content/atoms/develop-first-deploy-execute.md`,
`develop-first-deploy-promote-stage.md`, `develop-first-deploy-verify.md`.

**Lever B — Synthesize scope filter**:

Files:
- `internal/workflow/synthesize.go` — in `Synthesize`, when
  `env.WorkSession != nil`, narrow per-service axis matching to
  `env.Services` filtered to hostnames in `env.WorkSession.Services`.
- `internal/workflow/synthesize_test.go` — add coverage:
  - WorkSession with 1-host scope; project has 4 services; only the
    1 in scope contributes to per-service axis matches.
  - WorkSession=nil (e.g. idle phase) — current behavior preserved
    (full env.Services).

**Codex PRE-WORK**:
```
<task>
Adversarial review of Phase 5 plan in plans/pre-internal-testing-fixes-2026-04-30.md.

Read:
- internal/workflow/synthesize.go (especially the per-service axis match
  loop around line 65)
- internal/workflow/envelope.go (WorkSession + WorkSessionSummary shape)
- internal/content/atoms/develop-first-deploy-execute.md (aggregate-mode
  reference pattern)

Verify:
1. The 14 candidate atoms are GENUINELY per-service-identical except for
   {hostname} substitution — i.e. their text doesn't carry per-service
   detail that would justify N renders.
2. The aggregate-mode rewrite preserves all guidance — converting prose
   to `{services-list:TEMPLATE}` doesn't drop content the agent needs.
3. Lever B (scope filter) — does narrowing env.Services break any atom
   that NEEDS to see the full project (e.g. a "you have managed services
   in this project" atom that should fire even when scope is just one
   runtime)? List any candidate atoms.
4. Compaction-safety: per-pass invariant says Synthesize is byte-deterministic.
   The scope filter must not introduce non-determinism.

Output: VERIFIED / RISK list. Specific atom IDs that need careful handling.
</task>
```

**Implementation**:
1. Lever A first (atom-by-atom, per-PR-able even if shipped together).
   For each atom: add aggregate axis, rewrite body, run matrix sim, check
   the relevant scenario produces the new aggregated form.
2. Lever B second.
   - RED: add test that builds an envelope with project=4 services,
     scope=1 service; assert atoms with per-service axes render once
     for the 1 scope hostname only.
   - GREEN: implement scope filter in Synthesize.

**Verification**:
```sh
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
# Anomaly count for "briefing > 25KB" should drop sharply
# Scenario 10.2 (mixed runtimes 32KB) should drop into single digits

go test ./internal/workflow -run 'TestSynthesize|TestBuildPlan' -count=1 -v
go test ./... -short -count=1
```

**Codex POST-WORK**:
```
<task>
Verify Phase 5 diff. Two areas:
1. 14 atom rewrites — sample 3-4 random ones; verify aggregate-mode body
   is well-formed and the rendered output (visible in matrix sim
   testdata/lifecycle-matrix.md) is sensible.
2. Synthesize scope filter — verify the WorkSession=nil path is unchanged
   (idle phase still sees full env.Services); verify the WorkSession-set
   path narrows correctly without dropping atoms that legitimately need
   the wider view.

Cross-reference: how does the response size on scenario 10.2 compare
before vs after?

Output: VERIFIED + size delta numbers, or REGRESSION findings.
</task>
```

**Commit shape**: 2 logical levers, **2 commits** (lever A first, lever B
second). Lever A can ship without Lever B (still helps), but Lever B
without Lever A misses most of the gain.

---

### Phase 6 — UX bundle (C5 + nohup lint, F3 root, F5 root, M3)

**Scope**: bundle of small UX fixes plus one corpus lint addition.

**C5 + corpus lint**:
- Files: `internal/content/atoms/develop-first-deploy-asset-pipeline-container.md`
  (rewrite the `nohup ... &` block to use `zerops_dev_server action=start`),
  + `internal/content/atoms_lint.go` (add `axis-hot-shell` lint flagging
  `nohup`/`disown`/`& *$` outside marked anti-pattern paragraphs).
- Marker convention: `<!-- axis-hot-shell-keep: anti-pattern -->` like
  axis K/L/M/N markers, allowing the 3 atoms that legitimately call out
  the anti-pattern to keep their text.

**F3 root (centralized API enrichment)** — CHANGED 2026-04-30 per root-cause
depth review: refactor the 2 existing sites in this phase, not as follow-up.
Otherwise the "centralization" is just a third parallel pattern and the
class isn't actually addressed.

- New `internal/platform/error_enrichment.go` (or extend `errors.go`) with
  an `EnrichPlatformError(err *PlatformError, hints map[string][]string)`
  helper that appends to APIMeta.Metadata.
- `internal/platform/zerops_validate.go::ValidatePreDeployContent` — on
  `zeropsYamlSetupNotFound`, parse YAML locally and append `availableSetups`.
- **MIGRATE existing sites in same phase** (Codex confirmed they're parallel
  patterns):
  - `internal/tools/deploy_preflight.go:60-72` (setup choices ad hoc) →
    use new helper
  - `internal/tools/deploy_preflight.go:126-159` (env-ref choices) →
    use new helper
- Net effect: ONE canonical enrichment pattern; every API-error site goes
  through it.

**F5 root (workSessionState in deploy/verify response)**:
- Files: `internal/tools/deploy_local.go::sessionAnnotations` returns a
  structured `WorkSessionState` (Open/AutoClosed/None + ClosedAt + CloseReason
  + AutoCloseProgress). Same change in `deploy_local_git.go`,
  `deploy_git_push.go`, `deploy_batch.go`, `deploy_ssh.go`.
- Mirror the envelope's `develop-closed-auto` semantics so the agent gets
  the same lifecycle signal as `action="status"` would return.
- Update the response shape in the MCP tool schema if applicable.

**M3 (local-stage in 3 atom mode axes)**:
- `internal/content/atoms/develop-close-push-dev-local.md` (or its renamed
  Phase 1 successor) — add `local-stage` to modes
- `internal/content/atoms/develop-dynamic-runtime-start-local.md` — add
  `local-stage` to modes (also missing `local-only` per audit M3 detail)
- `internal/content/atoms/develop-ready-to-deploy.md` — add `local-stage`
  to modes

**Codex PRE-WORK**:
```
<task>
Adversarial review of Phase 6 plan in plans/pre-internal-testing-fixes-2026-04-30.md.

Three substantive changes; verify each:

1. C5 + nohup lint: read internal/content/atoms_lint.go and the existing
   axis K/L/M/N marker convention. Confirm a new "axis-hot-shell" lint
   fits the same shape. Does the matrix simulator already detect the
   nohup pattern (it has a heuristic check) — if yes, this lint formalizes
   it; check for overlap.

2. F3 root: read internal/platform/zerops_errors.go::reclassifyValidationError
   + internal/platform/errors.go::APIMetaItem. Verify the proposed
   EnrichPlatformError helper signature works with the existing error
   wrapping pattern. Identify any other API errors (env-var-not-found,
   type-not-in-catalog, hostname-collision) that should adopt it once it
   lands.

3. F5 root: read internal/workflow/work_session.go (close-state shape)
   + internal/workflow/compute_envelope.go::develop-closed-auto. Confirm
   the proposed WorkSessionState struct mirrors the envelope vocabulary.
   Check the MCP tool schema — does adding a structured field break any
   client that expects a string?

Output: VERIFIED / RISK / overlap notes.
</task>
```

**Implementation**: TDD per finding. Order: M3 (smallest), C5+lint,
F3 root, F5 root.

**Verification**:
```sh
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
# nohup lint anomaly count should drop to 0 (after C5 fix + lint marker on
# the 3 anti-pattern explanation atoms)

go test ./internal/platform -count=1 -v
go test ./internal/tools -run 'TestSessionAnnotations|TestDeploy' -count=1 -v
go test ./... -short -count=1
make lint-local
```

**Codex POST-WORK**:
```
<task>
Verify Phase 6 diff. Four logical changes:
1. C5 atom rewrite — verify zerops_dev_server invocation has correct args
2. nohup lint — verify the marker convention works on the 3 anti-pattern
   explanation atoms (no false positives)
3. F3 enrichment — verify EnrichPlatformError preserves error type/code/
   suggestion + appends APIMeta correctly
4. F5 workSessionState — verify the new struct is populated correctly in
   all 4 deploy paths; envelope still returns the same data as before

Output: VERIFIED or specific findings.
</task>
```

**Commit shape**: 3 commits (C5+lint, F3 root, F5 root). M3 can fold into
any of them or ship as fourth single-line commit per file.

---

### Phase 7 — Verification + audit close

**Scope**: final sweep, audit doc update, plan archive.

**Tasks**:
1. Re-run matrix simulator. Diff `testdata/lifecycle-matrix.md` against
   pre-Phase-1 baseline. Confirm: legacy strategy vocab anomalies = 0;
   nohup-pattern anomalies = 0; briefing-size > 25KB anomalies = 0
   (or close to 0; export-active alone may still be ~25KB which is OK
   per design).
2. Update `docs/audit-prerelease-internal-testing-2026-04-29.md`:
   - Mark each finding RESOLVED with the exit commit hash from this plan.
   - **Categorize residual anomalies** (added 2026-04-30 per Codex review):
     each remaining matrix-sim anomaly OR audit finding NOT closed by this
     plan must point at:
     - DEFERRED: an existing `plans/backlog/<slug>.md` entry, OR
     - PLANNED: a `plans/<slug>-DATE.md` follow-up plan path, OR
     - ACCEPTED: explicit one-line rationale why it stays open
   - Required `plans/backlog/` entries to create (one file each per
     `plans/backlog/README.md` convention):
     - `c3-failure-classification-async-events.md`
     - `c9-recipe-git-push-scaffolding.md` (already exists as
       `auto-wire-github-actions-secret.md` — link, don't duplicate)
     - `m1-glc-safety-net-identity-reset.md`
     - `m2-stage-timing-validation-gate.md`
     - `m4-aggregate-atom-placeholder-lint.md`
     - `deploy-intent-resolver.md` (the structural target H1's TODO names)
3. `git mv plans/pre-internal-testing-fixes-2026-04-30.md
        plans/archive/pre-internal-testing-fixes-2026-04-30.md`
4. Update `plans/backlog/` if any new deferred ideas surfaced during
   implementation (each as its own slug-named file per
   `plans/backlog/README.md` conventions).
5. Run full pre-release gate:
   ```sh
   go test ./... -race -count=1
   make lint-local
   ```
6. Final Codex SHIP-WITH-NOTES round on the cumulative branch diff.

**Codex final round**:
```
<task>
Cumulative review of branch cleanup-pre-internal-testing vs main.

Scope: this branch ships P1-P6 of plans/pre-internal-testing-fixes-2026-04-30.md.
Verify the cumulative diff is internally consistent:
1. Vocabulary sweep (P1) holds — no Phase 2-6 work re-introduced retired
   tokens
2. Cross-phase contracts not violated (e.g. P5 atom aggregates don't
   re-introduce per-service drift; P6 lint doesn't false-positive on
   P5-rewritten atoms)
3. Matrix simulator end state: enumerate residual anomalies; is each a
   known DEFERRED item or a regression?

Output: SHIP / SHIP-WITH-NOTES / HOLD verdict + any blocking findings.
</task>
```

If SHIP or SHIP-WITH-NOTES: merge to main via PR, mark plan COMPLETE.

If HOLD: address findings; rerun.

---

## 6. Cross-phase invariants

These hold throughout all phases; if a phase touches them, document why.

- **No backward-compat shims** (CLAUDE.local.md) — don't add new tolerated
  legacy values. Phase 1 explicitly removes the existing one (push-dev in
  strategy gate).
- **No new atoms with retired vocab** — every atom edit must use current
  vocabulary (closeDeployMode, gitPushState, buildIntegration; not
  strategy/push-dev/push-git as labels — strategy=git-push as a deploy arg
  remains valid).
- **Matrix simulator stays runnable** between phases. Re-run after each
  phase; capture output diff.
- **TDD per phase**: failing test before implementation. CLAUDE.md hard rule.
- **Atomic commits within phase scope** — split only by logical concern,
  per `feedback_commits_as_llm_reflog.md`.
- **English everywhere** in code, comments, docs, commits (CLAUDE.md).

---

## 7. Exit criteria for the plan

- All findings in scope marked RESOLVED in audit doc with commit hashes.
- Matrix simulator: legacy-vocab anomalies = 0; nohup-pattern anomalies = 0.
- Full test suite green with `-race`.
- `make lint-local` green.
- Plan file moved to `plans/archive/`.
- New backlog entries (if any) filed in `plans/backlog/`.

---

## 8. Rollback discipline

If a phase POST-WORK Codex flags a regression after the commit lands but
before the next phase starts: investigate, follow-up commit (NOT amend).
Per `feedback_commits_as_llm_reflog.md`, future LLM sessions reconstruct
context from the log; rewriting history erases the failure trace.

If a phase reveals the audit's claim was wrong (the second-pass
verification hit 0 refuted; this is unlikely but possible): document in
the commit body, update the audit, proceed.

---

## 9. Codex usage discipline

Per the recent export-buildFromGit phase pattern (commits 0f49e172,
e63d5ddc, etc.):

- **PRE-WORK round**: before each phase implementation, send Codex an
  adversarial review of the plan-as-written + intended changes. Read-only.
  Use the templates above. ~3 min per phase.
- **PER-EDIT round** (optional, for invasive changes like Phase 1 sweep
  or Phase 5 engine change): mid-implementation review of a substantive
  change before continuing.
- **POST-WORK round**: after each phase commit, send Codex a verification
  pass on the diff. ~3 min per phase.
- **FINAL round**: cumulative review on Phase 7. ~5 min.

Total Codex time across plan: ~50-60 min wall-clock, parallelizable with
implementation work.

If Codex flags risks pre-work, address them BEFORE coding. If post-work,
land a follow-up commit. Never wave them off.

---

## 10. Open questions to resolve before P0

If you're starting a fresh session and the answer to any of these isn't
obvious, ASK the user before P0:

1. Should the 8-atom rename in Phase 1 use `develop-close-mode-auto-*`
   slugs (matches the axis), or a different naming? Audit suggests the
   former; confirm.
2. F3 enrichment: should Phase 6 also refactor the 2 existing sites
   (`deploy_preflight.go:60-72`, `:126-159`) to use the new helper, or
   leave them alone (just add the helper + use it for the new case)?
   Smaller scope vs systematic.
3. Phase 5 Lever A: are all 14 listed atoms actually per-service-identical?
   Codex PRE-WORK should verify; if any have legitimate per-service
   prose, exclude them (don't force aggregate where it loses content).
