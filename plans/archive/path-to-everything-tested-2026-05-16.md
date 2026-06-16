# Path to "naprosto vše otestováno + ZCP správně funguje pro vše"

**Origin**: Karel 2026-05-16 ultrathink. End-state: ZCP s žádnými známými bugy + behavioral matrix pokrývající všechny real-world varianty + Phase 2 (v9.92) released. Nekonečný rozpočet času/tokenů. Toto je autoritativní roadmap.

---

## Aktuální stav (snapshot 2026-05-16 12:30)

### Hotovo
- Phase 2 (v9.92) implementace: 8 phases (0+0.5, 1a-c, 2a-d) → 13 commits
- Phase 2 hotfix external-secret `pickRandom` → literal `REPLACE_ME` (1 commit)
- Live Go tests (4 testy proti reálné platformě) → 1 commit
- Routing fix CLAUDE.md (AUTHOR vs USE recipe split) → 1 commit
- Tier-1 behavioral scenarios #1, #3, #4 → 2 commits
- Backlog files (verification framework, Tier-2 token injection) → 2 commits
- **17 unpushed commits** na `main`, push paused per Karel

### Behavioral matrix status
| # | Scenario | Stav | Detail |
|---|---|---|---|
| 1 | kanban-laravel-minimal-dev-only | ✅ PASS post-fix | 8:57; recipe-route via bootstrap; agent delivered live URL |
| 4 | landing-page-static-simple | ✅ PASS | 3:19; simple-mode + nginx + no deps |
| 3 | api-node-postgres-classic-dev | ⚠️ classifier bug | user-sim NEPROBĚHL (2ms terminatedBy=unknown); agent asked text-question, scenario terminated |
| 2 | kanban-standard-pair | ⏳ neimplementováno | Recipe + standard topology |
| 5 | adopt-existing-standard-pair | ⏳ neimplementováno | Adopt route |
| 6-12 | Tier 2-3 | ⏳ neimplementováno | Chain / token-injected / failure recovery |

### Atom guidance gaps surfaced (14 known findings)
Z retros napříč #1, #3, #4:

1. Recipe slug naming: user-facing `zerops-laravel-minimal` ≠ corpus `laravel-minimal`
2. Empty mount `.git` blocks composer scaffold
3. APP_KEY needs `base64:` prefix (not raw `<@generateRandomString>`)
4. Verify `info` level wording reads alarming (OOM advisory ≠ failure)
5. Deferred-tool batch-load tax (sequential ToolSearch)
6. `build.base` for static = nodejs builder, NOT nginx
7. `IMPORT_HAS_PROJECT` rejection wording counter-intuitive
8. Subdomain auto on first deploy — agent reflex calls separate tool
9. Work session "gated by close-mode" reads as error
10. Two-phase bootstrap (route-menu → route-commit) buried in docstring
11. Recipe slug = identity, no "use scaffold but build feature X" knob (Aleš scope)
12. `emit-yaml shape=workspace` doesn't honor `bootstrapMode=dev` (Aleš scope)
13. `zerops_import` no `projectId` param (tool design)
14. AskUserQuestion harness error needs documented fallback pattern

### Phase 2 specific backlogs (already documented)
- `plans/backlog/launch-production-setup-gate-error-ux.md`
- `plans/backlog/launch-first-deploy-failed-recovery-hint.md`
- `plans/backlog/post-run-platform-verification-framework.md`
- `plans/backlog/tier-2-token-injected-scenarios.md`

### Out-of-my-scope (Aleš's territory per CLAUDE.local.md)
- `internal/recipe/` (recipe engine)
- `internal/tools/workflow_recipe.go`
- `internal/tools/workflow_checks_recipe.go`
- `internal/content/workflows/recipe/`
- `docs/zcprecipator*/`

I CAN touch `internal/content/atoms/`, `internal/content/templates/`, `internal/tools/workflow.go` (non-recipe parts), and the bulk of ZCP code.

---

## Sprint plan

Pět sprintů, total estimate ~5-8 sessions (Karel-paced). Each sprint má clear deliverables + success criteria. Sprints jsou serial — dependency chain.

### Sprint 1 — Framework hardening + critical atom polish (1-2 sessions)

**Goal**: Behavioral matrix runs reliably + Tier-1 fully green + 4 highest-impact atom fixes land.

#### S1.1 — Fix usersim classifier bug

**Problem**: scenario #3 (api-node-postgres-classic-dev) terminated v 1:05 s `userSim.terminatedBy=unknown`, `totalWallTime=2.11ms`. Agent's first turn ended s text question after `AskUserQuestion` returned error. Classifier should verdict=Waiting → spawn user-sim. Místo toho user-sim nikdy nespustil.

**Trace plan**:
1. Read `internal/eval/usersim.go::ClassifyTranscriptTail` full rule table
2. Manually classify transcript at `eval/behavioral/runs/20260516-112630/api-node-postgres-classic-dev/transcript.jsonl` (13 lines) — what verdict should fire?
3. Identify mismatch — likely Rule 3 (post-tool done-marker) firing falsely, OR result event with unusual stopReason

**Fix approach**:
- Add unit test reproducing the transcript pattern (text-question post tool_use + AskUserQuestion error result)
- Tighten verdict rules — text-question markers (`?`, modal phrases) should override post-tool defaults
- Re-run scenario #3 → user-sim should engage → persona reply → agent continues

**Success**: scenario #3 reaches deploy (`api` service ACTIVE in eval-zcp), retro mentions classification was clean.

#### S1.2 — Critical atom fixes (highest impact, 4 atoms)

Fix top-4 from atom gaps list:

##### S1.2.1 — APP_KEY `base64:` prefix
Touch `internal/content/atoms/develop-first-deploy-laravel.md` (or create it). Add explicit warning: `<@generateRandomString(<32>)>` emits raw ASCII; Laravel needs `base64:` prefix. Either:
- Set APP_KEY AFTER first deploy via `php artisan key:generate --show`
- OR set upfront as `base64:<32-byte-base64-string>`

Pin via atom-lint axis.

##### S1.2.2 — Recipe slug naming
Touch atom that fires on recipe-route entry. Document that corpus slugs DON'T have `zerops-` prefix even though user-facing names might. Suggested fallback: if user-named slug not in corpus, try removing `zerops-` prefix; if still fails, list available slugs.

Better: `zerops_workflow` action=start workflow=bootstrap route=recipe slug=... should ACCEPT a wider slug-shape (with alias map).

Scope: prefer atom fix; alias map is bigger Aleš-scope work.

##### S1.2.3 — Deferred-tool batch-load pattern
Touch top-level CLAUDE.md (claude_shared.md) OR add explicit atom `setup-deferred-tool-batch-load.md`. Pattern:

```
At first turn, batch-load all ZCP tools you'll need:
ToolSearch query="select:mcp__zerops__zerops_workflow,mcp__zerops__zerops_discover,mcp__zerops__zerops_knowledge,mcp__zerops__zerops_import,mcp__zerops__zerops_env,mcp__zerops__zerops_mount,mcp__zerops__zerops_deploy,mcp__zerops__zerops_verify,mcp__zerops__zerops_logs,mcp__zerops__zerops_subdomain"
```

Saves N-1 round-trips on a typical 10-tool flow.

##### S1.2.4 — Static `build.base` counter-intuition
Touch atom related to classic-scaffold for static stacks. Explicit: static-only services still need a builder base (nodejs@22 default). The `nginx` base is RUN-time only, not BUILD-time. Add example.

#### S1.3 — Re-run Tier-1

Re-run #1, #3, #4 with atom fixes. Verify retros mention the now-fixed surfaces as "smooth" instead of "friction". Document any new findings (compounding atom polish often surfaces second-tier issues).

**Sprint 1 done criterion**:
- All 3 Tier-1 scenarios PASS reliably
- Classifier bug fix has unit test pin
- 4 atom fixes land + atom-lint green
- Re-run retros show cleaner agent paths
- Lint-local clean; race tests clean

### Sprint 2 — Tier-1 completion + Phase 2 backlog promotion (1-2 sessions)

**Goal**: Full Tier-1 coverage (5/5 scenarios) + Phase 2 backlog fixes land + v9.92 release-ready.

#### S2.1 — Tier-1 scenarios #2 + #5

##### #2: kanban-laravel-standard-pair
Variant of #1 — same Laravel recipe, but standard pair (dev + stage). Tests cross-deploy from dev to stage post-bootstrap. Persona accepts stage slot. Watch agent handle recipe's default dev/stage pair shape.

##### #5: adopt-existing-standard-pair
Pre-state: existing services on eval-zcp (Node+postgres pair) provisioned MANUALLY (not via bootstrap). Persona: "mám standard pair appdev+appstage+db v Zerops dashboardu, nastav mi ZCP integration". Agent runs `route=adopt`. Tests adopt flow with isExisting=true, FirstDeployedAt stamp from current ACTIVE status.

#### S2.2 — Phase 2 backlog promotion

##### S2.2.1 — `setup: prod` gate error UX
Per `plans/backlog/launch-production-setup-gate-error-ux.md`:
- Parse source yaml setup names
- Include available setups in error message
- Add `ProdSetupNameOverride` to WorkflowInput (early Phase 6b promo)
- Composer reads override before defaulting to "prod"

##### S2.2.2 — First-deploy-failed Recovery hint
Per `plans/backlog/launch-first-deploy-failed-recovery-hint.md`:
- Structured Recovery on blocker pointing at `zerops_logs source=build`
- Diagnostic field with appVersion shape (pipelineStart, containerCreationStart)
- Retry-via-push suggestion in nextSteps

#### S2.3 — Re-run launch-production scenarios

Re-run live Go Test 4 (`TestLive_LaunchProduction_FullCycle`) to verify Phase 2 backlog fixes still produce ACTIVE service. Re-run any behavioral launch scenarios from existing eval matrix.

**Sprint 2 done criterion**:
- 5/5 Tier-1 scenarios PASS
- Phase 2 backlog A + B fixes land + tests pin them
- `make lint-local` green; race tests green
- All Phase 2 commits + new fixes ready for push (still paused per Karel)

### Sprint 3 — Tier-2 framework + scenarios (1-2 sessions)

**Goal**: Token-injection framework operational + Tier-2 scenarios (chained, real-token) running.

#### S3.1 — Framework: ENV-var injection

Per `plans/backlog/tier-2-token-injected-scenarios.md`:
- Modify `eval/behavioral/flow-eval.sh` to pass `ZCP_E2E_*` env vars through SSH
- Add `requiredEnvVars:` frontmatter to scenario schema + fail-fast parser
- Add token-shape sentinel scan post-pull (`ghp_*`, ZerropsToken patterns)
- Test with dummy `ZCP_E2E_LAUNCH_KEY=fake-token` — fail-fast works

#### S3.2 — Framework: Post-run platform verification

Per `plans/backlog/post-run-platform-verification-framework.md`:
- `verification:` block in scenario YAML (expectedServices, subdomainProbe, noFailedProcesses, noTokenLeak)
- Runner invokes between retro + cleanup
- Findings → `verification.json` alongside `self-review.md`
- Exit code combines retro + verification

#### S3.3 — Tier-2 scenarios

Write + run with new framework support:
- #6 develop-loop-after-bootstrap (chain — no tokens)
- #7 git-push-setup-then-actions (needs GITHUB_PAT)
- #9 launch-with-existing-cicd (Karel's specific case — LaunchKey + GITHUB_PAT)

Each scenario includes `verification:` block + `requiredEnvVars:`.

**Sprint 3 done criterion**:
- Framework changes pin'd by tests
- Tier-2 scenarios 6, 7, 9 PASS w/ verification
- Token-shape sentinel scan green on all retros (no leaks)

### Sprint 4 — Tier-3 + remaining atom polish (1-2 sessions)

**Goal**: Failure-mode + edge-case coverage + atom corpus polished.

#### S4.1 — Tier-3 scenarios

- #10 launch-to-existing-prod-project (Phase 2c path, existing-project token)
- #11 launch-failure-build-stuck (forced failure → diagnostic gather)
- #12 resume-after-compaction (action=status recovery primitive)

#### S4.2 — Remaining atom polish (6 atoms from gap list)

Fix gaps 4-10 from atom gap list:
- Verify `info` level wording (atom + handler text)
- `IMPORT_HAS_PROJECT` rejection guidance
- Subdomain auto-on-first-deploy atom
- Work session close-mode wording
- Two-phase bootstrap pattern atom
- AskUserQuestion harness fallback documented

#### S4.3 — Full matrix re-run

Re-run all 12 scenarios in one go. Catch regressions from atom polish + Phase 2 backlog fixes.

**Sprint 4 done criterion**:
- 12/12 scenarios PASS w/ verification
- All 10 atom guidance gaps polished + atom-lint green
- Verify info wording in handler matches polished atoms

### Sprint 5 — Aleš-scope handoff + release v9.92 (1 session)

**Goal**: Things I cannot fix documented for Aleš + push 17+ commits + cut release.

#### S5.1 — Aleš handoff document

`plans/ales-handoff-2026-05-XX.md` capturing:

1. `zerops_recipe` MCP tool description in `internal/recipe/handlers.go:321` — add explicit AUTHOR-only marker. Current: "zcprecipator3 recipe engine". Proposed: "zcprecipator3 recipe-AUTHORING engine. For end-user DEPLOYMENT of an existing recipe use zerops_workflow workflow=bootstrap route=recipe slug=...". This is defense-in-depth to my routing fix.

2. Recipe slug = customizable starting point (gap #11). Allow user to inject feature description (e.g. "build a Kanban using laravel-minimal recipe") without forcing recipe-author phases.

3. `emit-yaml shape=workspace` honor `bootstrapMode=dev` (gap #12). Currently always emits dev+stage; should respect bootstrapMode from plan.

4. `zerops_import` `projectId` parameter (gap #13). Currently uses ambient ZCP-container-bound project; should accept explicit override.

For each item: surface + impact + recommended fix scope. Karel decides if he forwards to Aleš or backlog.

#### S5.2 — Final lint + race + push

- `make lint-local` clean
- `go test -race ./... -count=1` clean
- Run full Tier-1+2+3 matrix one final time
- Surface results to Karel for explicit GO
- `git push origin main` (~25-30 commits across all sprints)
- `make release` → v9.92 tag (or whatever version Karel decides)
- Watch CI green via `./scripts/ci.sh run list --workflow Release --limit 3` → `watch <id>`

**Sprint 5 done criterion**:
- All 12 scenarios PASS final run
- Push successful
- CI release green
- Aleš handoff doc landed in `plans/`
- Karel GO signal recorded

---

## Success criteria (whole roadmap)

| Layer | Criterion |
|---|---|
| Unit tests | `go test ./... -short -count=1` PASS |
| Race tests | `go test -race ./... -count=1` PASS |
| Lint | `make lint-local` 0 issues |
| Live Go tests | All 4 PASS proti reálné platformě |
| Behavioral matrix | 12/12 scenarios PASS w/ verification |
| Atom corpus | atom-lint green, no known gaps from retros |
| Token safety | No `ghp_*` / `YJQTh.*` shapes in any artifact |
| Release | v9.92 tag pushed, CI green |
| Documentation | Aleš handoff doc + backlog files cover all unfixed items |

---

## Anti-patterns to avoid

- **Skip classifier bug** — without fix, scenario #3 + similar text-question paths unreliable. Sprint 1 priority.
- **Defer Aleš-scope items** indefinitely — write handoff so Karel can forward when convenient.
- **Add new atom complexity** — atom polish should SIMPLIFY agent guidance, not add edge-cases. Each atom edit should reduce retro friction count.
- **Premature push** — Karel explicitly paused. Wait for explicit GO after full matrix green.
- **Compounding scope** — if Sprint reveals new issues, decide: fix-now (small + on-path) vs backlog (out-of-scope). Don't blob-grow sprints.

---

## Compact + new-session commands

### Compact command

```
/compact 17 unpushed commits (Phase 2 v9.92 + Tier-1 behavioral matrix + routing fix). Sprint 1 priority: fix usersim.go classifier bug bypassing user-sim on text-question post-tool turns (scenario #3 terminated in 1:05 with userSim.totalWallTime=2.11ms). Plan: plans/path-to-everything-tested-2026-05-16.md.
```

### Post-compact command (paste this in new session)

```
Read /Users/macbook/Documents/Zerops-MCP/zcp/plans/path-to-everything-tested-2026-05-16.md.

Status: 17 unpushed commits on main; routing fix proven via kanban
scenario re-run; Tier-1 matrix 2/5 verified (#1, #4 PASS; #3
framework-bug-blocked; #2, #5 not started). Atom gap inventory in
plan §Atom guidance gaps surfaced.

Begin Sprint 1 per plan §Sprint 1. Sequence:

1. S1.1: investigate classifier bug
   - Read internal/eval/usersim.go::ClassifyTranscriptTail full
     rule table
   - Trace transcript at eval/behavioral/runs/20260516-112630/
     api-node-postgres-classic-dev/transcript.jsonl
   - Identify which rule mis-fires for text-question post-tool
   - Add unit test reproducing the failing pattern
   - Fix rule logic
   - Re-run scenario #3 to verify

2. S1.2: 4 critical atom fixes
   - APP_KEY base64-prefix
   - Recipe slug naming alias
   - Deferred-tool batch-load pattern
   - Static build.base counter-intuition

3. S1.3: Re-run Tier-1 #1, #3, #4

Each S1.X completes when tests + lint green. Surface findings as you go.

Constraints (from CLAUDE.local.md):
- Don't touch internal/recipe/, internal/tools/workflow_recipe.go,
  internal/tools/workflow_checks_recipe.go,
  internal/content/workflows/recipe/, docs/zcprecipator*/
- These are Aleš's scope — findings → mention, never act

After Sprint 1 done criterion met, commit progress and Karel will
trigger Sprint 2.

DO NOT push to origin/main until Karel explicit GO after full
matrix green per plan §Sprint 5.

Use TaskCreate to track per-sprint progress. Use Agent (subagent_type=
codex:codex-rescue) for parallel deep-dive if needed (e.g.
classifier-bug second-opinion review).
```

---

## Estimate

- **Sprint 1**: 1-2 sessions, ~600-1000 LOC + framework work
- **Sprint 2**: 1-2 sessions, ~400-600 LOC scenarios + Phase 2 fixes
- **Sprint 3**: 1-2 sessions, ~500 LOC framework + 3 scenarios
- **Sprint 4**: 1-2 sessions, ~300 LOC scenarios + atom polish
- **Sprint 5**: 1 session, handoff doc + push + release

**Total: 5-9 sessions** to reach "vše otestováno + ZCP správně funguje pro vše" end state.

Karel's "nekonečný rozpočet" makes this feasible. Iterative + each Sprint produces verifiable artifact before moving on.
