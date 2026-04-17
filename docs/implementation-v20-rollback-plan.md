# Recipe Workflow Rollback to v20 Substrate — Implementation Guide

**Audience**: Opus-level implementer starting cold. You do not need prior conversation context. This document is self-contained.

**Estimated effort**: one focused afternoon. ~28 files deleted wholesale, ~19 files reverted to an earlier commit, ~8 cherry-picks for tool-layer wins, test suite reconciled, one run of v25 to validate.

---

## TL;DR

The recipe workflow's content-check + dispatch-gate machinery has been incrementally expanded across 12 structural releases (v8.78 → v8.89) since the last A-grade run (v20, 2026-04-15). Every subsequent recipe run (v21 D, v22 B, v23 C, v24 C) has regressed on wall time, convergence, and content fidelity. The machinery is load-bearing in the wrong direction: it forces multi-round remediation loops, the agent works around check false-positives by degrading correct content, and the dispatch gates can be bypassed with a two-word acknowledgement. This guide rolls the recipe-side workflow back to the v20-era substrate while preserving genuine tool-layer improvements that landed in the same commits.

Rollback target: **commit `5022f8d`** (`fix(server): remove keepalive, add shutdown diagnostics and observability`). That was the last commit before `v8.78` landed. v20 ran against that tree state.

---

## The problem this rollback addresses

The check-and-gate model has a mechanical failure mode.

**The math.** With N independent content checks each at per-check writer-pass-rate P, probability of clean-on-first-write is P^N. v20 had ~10 checks. v24 has ~17. Even at an optimistic 95% per-check rate:

| Active checks | P(clean on first write) | P(needs ≥1 fix round) |
|---:|---:|---:|
| 10 (v20) | 60% | 40% |
| 14 (v22) | 49% | 51% |
| 17 (v24) | 42% | 58% |

Adding checks *mathematically* lowers the probability of clean shipping. The content-fix dispatch gate shipped in v8.81 (commit `69409b9`) makes each failure multi-minute instead of inline. Adding more checks without changing this geometry is anti-convergent by construction.

**The trajectory.** Wall-clock time of the canonical `nestjs-showcase` run:

```
v20  71 min   grade A-
v21 129 min   grade D   (after v8.78 5-check reform)
v22 103 min   grade B   (after v8.80 9-gate addition)
v23 119 min   grade C   (after v8.81-85, 5 more structural versions)
v24 126 min   grade C   (after v8.86-89, 4 more versions)
```

Monotone regression while the check surface roughly doubled and the dispatch/sub-agent orchestration roughly tripled. The direction of the intervention is clear.

**The observable failure modes in v24.**

1. **The ceremony tax.** The workflow state machine requires the agent to attest to 12 deploy substeps *in order*. A v24 agent who had completed all app work and wanted to mark `readmes` done got rejected with `expected sub-step "deploy-dev" (current), got "readmes"` and retroactively issued 13 rapid-fire `complete deploy` calls at 17:00–17:07 walking back through every substep. That is not correctness enforcement — it is sequence enforcement with no underlying invariant.
2. **Content degradation to satisfy checks.** The `content_reality` regex matches any `name.ext` bareword and flags it as a non-existent file path. When it fired on `res.json`, `Nest.js`, and `uri.html` (all real API/framework/directive names), the fix-subagent brief explicitly told the agent to *rewrite correct content to avoid the naive heuristic* — replacing `res.json` with "the response body parser" and so on. The checker was authoritative over the content even when wrong.
3. **The dispatch gate is a suggestion.** v8.81's `content_fix_dispatch_required` gate collapses when the agent emits "acknowledged inline deviation". That exact phrase appears once in v24's main session log and the writer subagent was entirely bypassed.
4. **Check reads the wrong surface.** `comment_ratio` reads the `zerops.yaml` *copy* embedded in README's IG#1 fenced block rather than the on-disk `zerops.yaml`. The v24 agent wrote a Python script at `/tmp/sync_yaml.py` to copy the real yaml *into* the README so the check would pass. When agents build sync tools to satisfy checks, the checks are the bug.
5. **Check reads the wrong state.** `scaffold_hygiene` inspects filesystem presence of `node_modules/dist/` on the dev container mount, not what git ships. The agent ran `sudo rm -rf /var/www/node_modules /var/www/dist` on all 3 live dev containers — breaking the running dev servers — to satisfy a check that was testing the wrong thing. A 37-minute recovery cluster followed (redeploy ×4, dev_server restart ×4, npm install, browser re-verify).
6. **The retry loop is the expected case.** v24 deploy step ran 5 full-step `complete` attempts (17:07 → 17:22) with fail counts 17 → 8 → 4 → 3 → 0, strictly decreasing but never converging in one round. Same pattern as v23 (23→11→5→4→2→0).

**The cure has been worse than the disease for five consecutive runs.** Rolling back restores a substrate where v20 demonstrated A-grade convergence, and re-frames the question: what of the post-v20 machinery was actually useful, and what was the problem?

## Load-bearing insight

v20 had seven named decorative-content drift classes (generic `.env` advice, predecessor-cloned gotchas, `_nginx.json` topology mismatch, CLAUDE.md contradicting README, watchdog declared but unimplemented, IG item leaning on neighbor, env-comment template phrasing). These are all *content polish* issues — the kind a human editor resolves in a 20-minute review pass. The response to them was to add automated token-level checks. Those checks false-positive. The agent routes around the false-positives by degrading correct content. Content quality over five runs has not matched v20 and the run has gotten slower each time.

**The invariant this guide is built on**: v20 drift classes are content-editorial problems, not content-check problems. They belong in a one-time human review pass on published content, not in a 17-check gate fired on every generate. Accepting that distinction is the rollback's first move.

If v25 on the rolled-back substrate reproduces v20's A-grade, the machinery was the problem. If it does not, the substrate has degraded elsewhere and the rollback has at least isolated where that is.

---

## What the rollback preserves vs reverts

Commits between v20 (2026-04-15) and the v24 run (2026-04-17, on v8.89) partition cleanly:

| Commit | Role | Action |
|---|---|---|
| `5022f8d` fix(server): keepalive / shutdown diagnostics | **v20 substrate** | Keep (baseline) |
| `57de8dd` feat(workflow): v8.78 — 5 new content checks + predecessor-floor rollback | Content reform | **REVERT** |
| `9d878c6` fix(deploy): git-push token check | Deploy tool fix | Keep |
| `aadf5b1` fix(deploy): git-push checks GIT_TOKEN before push | Deploy tool fix | Keep |
| `6a0bd7f` refactor(server): methodCallTool constant | Refactor | Keep |
| `c9f19b9` fix(cicd): explicit fine-grained token permissions | CICD | Keep |
| `2b62823` fix(cicd): GitHub permission naming | CICD | Keep |
| `58103ef` feat(workflow): v8.80 — scaffold_hygiene, writer dispatch gate, gotcha_depth_floor, bash_guard, pkill-self-kill | Mixed | **SPLIT** — revert checks/gate, keep `bash_guard.go` + pkill-self-kill classifier |
| `bd65081` feat(workflow): accept opus-4-7 in recipe start gate | Model gate | Keep (one line) |
| `69409b9` feat(workflow): v8.81 — content-fix dispatch gate, architecture_narrative, dev_start_contract, scaffold preambles | Dispatch + checks | **REVERT** |
| `26b0347` feat(workflow): v8.82 content rubric + v8.83 response-size fix | Mixed | **SPLIT** — revert v8.82 checks/preambles, keep v8.83 substep-response-size fix |
| `1c7e948` feat(workflow): v8.84 eager scope shift + v8.85 env-var model + self-shadow + preflight setup | Mixed | **SPLIT** — revert recipe.md eager/preamble bloat, keep `env_shadow.go` + preflight setup propagation; optionally keep `EagerAt` refactor (judgment call, see below) |
| `ebea67a` feat(workflow): DevelopMarker → WorkSession for develop flow | Unrelated refactor | Keep (entirely develop-flow, not recipe) |
| `5fa83e8` feat(workflow): v8.86 — writer self-verifying briefs, contract_spec, facts log, claude_md_folk check | Dispatch + checks | **MOSTLY REVERT** — keep `record_fact.go` as orphan tool (harmless, small, agents may call it) |

---

## Prerequisites

- Clean working tree: `git status` reports nothing uncommitted.
- You are on `main` (or a base branch that has `5022f8d` in its ancestry).
- `go test ./...` passes on the current HEAD (baseline).
- You can run `zcp sync pull recipes` if you need recipe files locally for manual verification (not strictly required).

---

## Execution

### Phase 0 — Branch

```bash
cd /Users/fxck/www/zcp
git checkout -b rollback-to-v20-substrate
```

### Phase 1 — Identify target commit

The v20 substrate is HEAD as of `5022f8d`. For any file that had no legitimate change on top of it, `git checkout 5022f8d -- <path>` restores it exactly. Verify:

```bash
git log -1 --format='%H %s' 5022f8d
# expect: 5022f8d... fix(server): remove keepalive, add shutdown diagnostics and observability
```

### Phase 2 — Delete check machinery (wholesale)

These files were added in post-v20 commits and contain nothing worth keeping. They are self-contained and can be deleted without touching anything else:

```bash
cd /Users/fxck/www/zcp

# v8.78 additions
rm internal/tools/workflow_checks_causal_anchor.go
rm internal/tools/workflow_checks_causal_anchor_test.go
rm internal/tools/workflow_checks_claude_consistency.go
rm internal/tools/workflow_checks_claude_consistency_test.go
rm internal/tools/workflow_checks_per_item.go
rm internal/tools/workflow_checks_per_item_test.go
rm internal/tools/workflow_checks_reality.go
rm internal/tools/workflow_checks_reality_test.go
rm internal/tools/workflow_checks_service_coverage.go
rm internal/tools/workflow_checks_service_coverage_test.go

# v8.80 additions (but keep bash_guard — see Phase 4)
rm internal/tools/workflow_checks_scaffold_hygiene.go
rm internal/tools/workflow_checks_scaffold_hygiene_test.go
rm internal/tools/workflow_checks_gotcha_depth_floor.go
rm internal/tools/workflow_checks_gotcha_depth_floor_test.go

# v8.81 additions
rm internal/tools/workflow_checks_architecture_narrative.go
rm internal/tools/workflow_checks_architecture_narrative_test.go
rm internal/workflow/recipe_content_fix_gate.go
rm internal/workflow/recipe_content_fix_gate_test.go

# v8.82 additions
rm internal/tools/workflow_checks_zerops_yml_depth.go
rm internal/tools/workflow_checks_zerops_yml_depth_test.go
rm internal/tools/workflow_checks_readme_container_ops.go
rm internal/tools/workflow_checks_readme_container_ops_test.go

# v8.86 additions (but keep record_fact — see Phase 4)
rm internal/tools/workflow_checks_claude_md_folk.go
rm internal/tools/workflow_checks_claude_md_folk_test.go
rm internal/workflow/recipe_writer_brief.go
rm internal/workflow/recipe_writer_brief_test.go
rm internal/workflow/recipe_contract_spec.go
rm internal/workflow/recipe_contract_spec_test.go
```

**Verify**: `go build ./...` will fail here — that's expected. Proceed to Phase 3.

### Phase 3 — Revert modified files to v20 state

These files were modified by post-v20 commits (registering checks, adding preambles, wiring the dispatch gate). Restore each to its `5022f8d` state:

```bash
git checkout 5022f8d -- internal/tools/workflow_checks_recipe.go
git checkout 5022f8d -- internal/tools/workflow_checks_finalize.go
git checkout 5022f8d -- internal/tools/workflow_checks_generate.go
git checkout 5022f8d -- internal/tools/workflow_checks.go
git checkout 5022f8d -- internal/content/workflows/recipe.md
git checkout 5022f8d -- internal/workflow/recipe_topic_registry.go
git checkout 5022f8d -- internal/workflow/recipe_topic_registry_test.go
git checkout 5022f8d -- internal/workflow/recipe_section_catalog.go
git checkout 5022f8d -- internal/workflow/engine_recipe.go
git checkout 5022f8d -- internal/workflow/recipe.go
git checkout 5022f8d -- internal/workflow/recipe_substeps.go
git checkout 5022f8d -- internal/workflow/recipe_substep_validators.go
git checkout 5022f8d -- internal/workflow/recipe_substep_validators_test.go
git checkout 5022f8d -- internal/workflow/recipe_plan_predicates.go
git checkout 5022f8d -- internal/workflow/recipe_guidance.go
git checkout 5022f8d -- internal/workflow/recipe_guidance_test.go
git checkout 5022f8d -- internal/workflow/briefing.go
git checkout 5022f8d -- internal/workflow/managed_types.go
git checkout 5022f8d -- internal/workflow/recipe_test.go
```

### Phase 4 — Cherry-pick back the tool-layer wins

These are genuine improvements that landed in mixed commits. Each is self-contained enough to port individually. Work in this order.

#### 4a. `bash_guard` middleware (from `58103ef` / v8.80)

Rejects `cd /var/www/<host> && <cmd>` patterns that main agent inadvertently ran zcp-side over SSHFS. Clean structured error + correction suggestion. Self-contained.

```bash
git checkout 58103ef -- internal/tools/bash_guard.go
git checkout 58103ef -- internal/tools/bash_guard_test.go
```

`bash_guard.go` exposes `CheckBashCommand` as a package-level function with no registrations on HEAD (pure middleware, called directly from the bash pre-hook). No porting step required — cherry-picking the two files is sufficient.

#### 4b. `pkill` self-kill classifier (from `58103ef` / v8.80)

Converts SSH exit-255 from `pkill -f nest` (which matches its own shell ancestor) into a structured "stopped cleanly, ssh dropped" success. Lives in `internal/ops/dev_server_lifecycle.go`.

```bash
# Inspect the v8.80 changes to the file
git show 58103ef -- internal/ops/dev_server_lifecycle.go

# Port: use the 58103ef version if other v8.80 changes in that file are self-contained
git checkout 58103ef -- internal/ops/dev_server_lifecycle.go
```

If `dev_server_lifecycle.go` at `58103ef` references constants or types defined in deleted files, revert that specific line/block. The pkill self-kill detection itself (`isSSHSelfKill` classifier, `pkill --ignore-ancestors` where supported) is the load-bearing code; keep that.

#### 4c. v8.83 substep response-size fix (from `26b0347`)

This is the 14×–63× reduction on 3 substeps (`feature-sweep-dev`, `feature-sweep-stage`, `readmes-complete`). It prevents 40 KB+ responses that persist to disk. Lives in `internal/workflow/recipe_guidance.go`.

**Surgical port**: the v8.83 fix is about the `subStepToTopic` switch cases and a terminal-substep branch in `buildGuide`. The v8.82 portion of the same commit adds eager topic wiring you want reverted.

```bash
# Inspect the two portions
git show 26b0347 -- internal/workflow/recipe_guidance.go
```

Apply only the v8.83 portion:
- Add `SubStepFeatureSweepDev` → `"feature-sweep-dev"` and `SubStepFeatureSweepStage` → `"feature-sweep-stage"` cases in `subStepToTopic`'s switch. (Both constants already exist at the v20 substrate in `internal/workflow/recipe_substeps.go` — no additions required.)
- Add the compact "all substeps complete" terminal branch in `buildGuide` (emits ~500 bytes instead of falling through to the 40 KB monolith).
- Add `buildSubStepMissingMappingNote` fallback for defensive degradation.

Do NOT port the `content-quality-overview` topic registration (that's v8.82 preamble bloat) or the eager-at-substep wiring (that's v8.84, handled separately in 4e).

#### 4d. `env_self_shadow` check (from `1c7e948` / v8.85)

This is the single content check worth keeping. It catches `key: ${key}` shapes in `run.envVariables` — a real, silent, high-impact bug class. Has been demonstrated to have no false positives (ignores composed strings like `postgres://${db_hostname}:5432/app` and legitimate renames like `DB_HOST: ${db_hostname}`).

```bash
git checkout 1c7e948 -- internal/ops/env_shadow.go
git checkout 1c7e948 -- internal/ops/env_shadow_test.go
```

Then register the check at generate-complete. The registration point is in `internal/tools/workflow_checks_generate.go` (which you reverted in Phase 3). Manually add the integration:

```bash
git show 1c7e948 -- internal/tools/workflow_checks_generate.go
```

Port ONLY the `env_self_shadow` integration (it's labeled clearly by function name and detail string referencing `env-var-model`). Do not port the `dev_start_contract` check that was also added in 1c7e948 — that belongs to v8.81 preamble bloat.

#### 4e. Pre-flight resolved-setup propagation (from `1c7e948` / v8.85)

Deploy tool improvement. `deployPreFlight` returns the resolved setup name; handlers pass `--setup=<resolved>` to zcli explicitly; unknown-setup errors enumerate available setups.

```bash
git checkout 1c7e948 -- internal/ops/deploy_validate.go
git checkout 1c7e948 -- internal/tools/deploy_preflight.go
git checkout 1c7e948 -- internal/tools/deploy_preflight_test.go
```

Then inspect `internal/tools/deploy_local.go` and `internal/tools/deploy_ssh.go`. The v8.85 changes were 14 and 16 lines respectively, mostly updating the setup description and threading the resolved name through. Port ONLY the setup description + resolved-setup threading; leave the rest at v20 substrate:

```bash
git show 1c7e948 -- internal/tools/deploy_local.go internal/tools/deploy_ssh.go
```

#### 4f. `EagerAt` topic-scope refactor (from `1c7e948` / v8.84) — judgment call

The refactor replaced `GuidanceTopic.Eager bool` with `EagerAt string`, letting topics attach to specific substeps instead of step-entry. This fixed the 50.9 KB deploy step-entry response-persist-to-disk issue.

**Arguments for keeping**: genuine response-hygiene fix, makes topic scoping more flexible, orthogonal to the check machinery.

**Arguments for reverting**: it's 187 lines rewriting `recipe_topic_registry.go`; the v20 substrate managed without it; if we keep it we need to port the single topic (`where-commands-run`) that was truly eager, and decide what to do with the migrated topics that no longer exist.

**Recommendation**: revert. v20 ran fine without this refactor. The deploy step-entry size issue that motivated v8.84 was largely a symptom of too many topics being flagged eager in the first place — by reverting to v20 substrate, you're removing most of those topic additions, and the step-entry size will return to v20 levels. If v25 proves the response size is again problematic, revisit this specifically.

If you keep it: cherry-pick the `EagerAt string` type change and `InjectEagerTopicsForSubStep` function, but drop all the *specific topic registrations* that came with it (they were added for checks we just deleted).

#### 4g. `record_fact` MCP tool (from `5fa83e8` / v8.86)

Small, self-contained tool that writes a structured fact record to `/tmp/zcp-facts-<sessionId>.jsonl`. Agents may or may not call it; harmless either way; worth keeping as a minor tool for future use.

```bash
git checkout 5fa83e8 -- internal/tools/record_fact.go
git checkout 5fa83e8 -- internal/tools/record_fact_test.go
```

You'll need to register it in the MCP server. The registration point is in `internal/server/server.go`:

```bash
git show 5fa83e8 -- internal/server/server.go internal/server/server_test.go
```

Port ONLY the `record_fact` registration — skip anything else in those files from that commit (there may be unrelated registration of writer-brief or contract-spec tools that you don't want).

### Phase 5 — Reconcile tests

After Phases 2–4, `go build ./...` should pass. `go test ./...` will fail. Expected failures:

1. **Topic registry meta-tests** (`internal/workflow/recipe_topic_registry_test.go`) — if you reverted this test file in Phase 3, it should match the reverted `recipe_topic_registry.go`. If you kept any `EagerAt` refactor work, you'll need to reconcile.

2. **Substep guide coverage tests** (`internal/workflow/recipe_substep_guide_coverage_test.go`) — check whether this file existed at `5022f8d`. If not, delete it:
   ```bash
   git log --all --pretty=format:'%h' -- internal/workflow/recipe_substep_guide_coverage_test.go | tail -1
   ```
   If the earliest commit is post-v20, delete the file. If it existed earlier, revert it:
   ```bash
   git checkout 5022f8d -- internal/workflow/recipe_substep_guide_coverage_test.go
   ```

3. **Integration tests** (`integration/`) — run them with verbose output and check for references to deleted check names or functions:
   ```bash
   go test -v ./integration/ 2>&1 | head -50
   ```
   Expect failures only in tests that exercise the content-fix dispatch gate or specific check outputs. Revert those test files to `5022f8d` or delete them if they were added post-v20.

4. **E2E tests** (`./e2e/ -tags e2e`) — skip unless you intend to run them; they hit real Zerops API.

General recipe for test reconciliation: each failing test is either (a) testing a file you deleted — delete the test too if it's a dedicated test file, otherwise revert it, or (b) testing a behavior you reverted — revert the test file to the same commit. Do not leave a test file present that references a deleted symbol.

```bash
# Sanity check: any imports of deleted packages?
grep -rn "workflow_checks_causal_anchor\|workflow_checks_reality\|workflow_checks_per_item" --include="*.go" .

# Any references to deleted types?
grep -rn "ContentFixGate\|WriterBrief\|ContractSpec" --include="*.go" internal/
```

Clear all findings before proceeding.

### Phase 6 — Build + test verification

```bash
go build -o bin/zcp ./cmd/zcp
go test ./... -count=1 -short
make lint-local
```

All three must pass. If any lint issue is about unused imports in files you touched, clean them up.

### Phase 7 — Commit

```bash
git add -A
git status  # review every deletion and modification

git commit -m "$(cat <<'EOF'
revert(workflow): roll back recipe checker machinery to v20 substrate

v20 (2026-04-15) was the last A-grade nestjs-showcase run. Wall time has
regressed monotonically across v21-v24 while ~6000 lines of content-check
and dispatch-gate machinery was added. Failure modes observed in v24:
5-round content-fix loops, content degraded to satisfy false-positive
checks (res.json → "response body parser"), dispatch gate bypassable via
two-word acknowledgement, agent-authored sync scripts to satisfy checks
that read the wrong surface, destructive hygiene ops on live dev
containers to satisfy checks that test the wrong state.

Revert:
- v8.78 5-check reform (causal_anchor, reality, per_item,
  service_coverage, claude_consistency)
- v8.80 scaffold_hygiene + gotcha_depth_floor + writer-subagent dispatch gate
- v8.81 content-fix dispatch gate, architecture_narrative,
  dev_start_contract, scaffold preambles
- v8.82 readme_container_ops, zerops_yml_comment_depth, content-quality
  eager topic
- v8.86 writer self-verifying briefs, contract_spec, claude_md_folk

Keep:
- v8.80 bash_guard middleware + pkill self-kill classifier
- v8.83 substep response-size fix (14x-63x reduction on 3 substeps)
- v8.85 env_self_shadow check (real bug class, no false positives)
- v8.85 pre-flight resolved-setup propagation
- v8.86 record_fact tool (orphan but harmless)
- All unrelated fixes (cicd, git-push, server refactor, develop-flow
  work-session refactor, opus-4-7 model gate)

~6100 lines removed, ~2000 lines of tool-layer wins preserved. Net -4000
lines in the direction of v20.

The first run on the rolled-back substrate (v25) is the validation.
A-grade or near-A at v20 wall times proves the machinery was the problem.
Failure at v25 proves substrate has degraded elsewhere and isolates where.

EOF
)"
```

---

## Verification — how to know you're done

**Local**:
1. `go build` produces a working `zcp` binary.
2. `go test ./... -count=1 -short` passes cleanly.
3. `make lint-local` reports 0 issues.
4. Grep confirms no residual references to deleted symbols:
   ```bash
   grep -rn "ContentFixGate\|WriterBrief\|ContractSpec\|ScaffoldHygiene\|ContentReality\|CausalAnchor\|ClaudeConsistency\|ServiceCoverage\|PerItemStandalone\|ArchitectureNarrative\|ZeropsYmlDepth\|ReadmeContainerOps\|GotchaDepthFloor\|ClaudeMdFolk" --include="*.go" .
   ```
5. The binary's MCP tool list should match v20's. Quick check:
   ```bash
   ./bin/zcp --list-tools 2>/dev/null | sort
   ```
   Compare against what the v20 session logs show under `"name":"mcp__zerops__*"`. Should see v20's original set: `workflow`, `deploy`, `discover`, `verify`, `env`, `subdomain`, `mount`, `logs`, `knowledge`, `import`, `dev_server`, `browser`, `manage`, `scale`, `delete` — **plus** `record_fact`, which is a post-v20 addition being deliberately retained by Phase 4g.

**File-count sanity**:
- Check files in `internal/tools/workflow_checks_*.go`: should be ~4 files (whatever v20 had — likely `workflow_checks.go`, `workflow_checks_generate.go`, `workflow_checks_finalize.go`, `workflow_checks_recipe.go`, `workflow_checks_predecessor_floor.go`). If you see any of the deleted check names listed above, revisit Phase 2.
- Check files in `internal/workflow/recipe_*.go`: should NOT include `recipe_content_fix_gate.go`, `recipe_writer_brief.go`, `recipe_contract_spec.go`.

**Smoke test**:
```bash
./bin/zcp version
# expected: something like v8.77-rollback (or whatever you set)
```

---

## Running v25

Follow the standard recipe-run procedure for `nestjs-showcase`. Capture the session logs as usual under `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v25/SESSIONS_LOGS/`.

**Expected results if the machinery was the problem**:
- Wall time in the 70-90 min range (matching v20's 71 min)
- Zero content-fix dispatch-gate failures (gate doesn't exist)
- Zero retry loops on `complete deploy` full-step (no content checks to fail)
- Subagent delegation pattern: scaffold ×3, feature ×1, README/CLAUDE writer ×1, env-comments writer ×1, code review ×1, close-step critical-fix ×1 — matching v20
- Content published with v20's characteristics: deep invariant gotchas, accurate YAML comment ratios on disk, no Python sync scripts, no `rm -rf node_modules` steps
- Close-step review finds the normal 2-3 CRIT/WRONG contract issues that scaffolds always produce — fixed cleanly by a single critical-fix subagent

**Expected results if substrate degraded elsewhere**:
- Close-to-v20 wall time but close-step finds more issues than usual (indicates tool-layer regression independent of checks)
- `zerops_dev_server` stability regression (indicates platform-layer change — investigate Zerops backend separately)
- Main agent re-invents patterns v20's guidance provided (indicates `recipe.md` lost something meaningful when reverted — reinstate the specific block)

---

## What to do AFTER v25 lands

**If v25 hits A-grade or near-A**:
- Do NOT propose a new content-check to fix the drift classes. v20 had them; the whole point of this rollback is accepting that check-driven drift-prevention has failed for 4+ runs straight.
- If you want better-than-v20 content quality, run a human editorial pass on the published content before publishing to `zeropsio/recipes`. One pass by a human editor catches all 7 v20 drift classes in 20 minutes.
- If you want to automate the editorial pass, it is a *single subagent that reviews the whole deliverable once and returns ship-or-one-round-of-fixes*. Not 17 checks each capable of firing a retry. The difference is about convergence guarantee.

**If v25 hits B or C**:
- Before proposing fixes: diff v25's main-session against v20's main-session at the minute-by-minute level. What specific tool calls does v25 make that v20 didn't? What does v20 do that v25 fails to reproduce? The diff tells you whether the substrate really reverted or whether something about v20's behavior was implicit in knowledge files that have since been regenerated/pulled.
- `zcp sync pull guides` and `zcp sync pull recipes` pulls docs from upstream — these may have been updated since v20. Compare against the v20-era snapshots if you can reconstruct them.

**If v25 fails outright**:
- The rollback isolated substrate-level degradation from check-machinery-level degradation. That's still valuable. Investigate the substrate regression independently.
- Do NOT reintroduce the check machinery as a fix. The checks addressed a symptom.

---

## What NOT to do, under any circumstances

1. **Do not add a new content check to "fix" anything v25 shows.** Every postmortem since v20 has added a check. The result has been five consecutive regressions. This is the pattern this rollback is breaking.
2. **Do not re-enable the content-fix dispatch gate.** It is anti-convergent by construction (it adds a multi-minute subagent round trip to each check failure). If you want a writer-subagent gate, the shape is v20's: writer subagent authors content, critical-fix subagent at close step redeploys on CRIT-only findings. Not a per-check content-fix loop.
3. **Do not expand `recipe.md` preambles.** They crossed 50 KB before v8.84 and caused response-persist-to-disk failures. The version you just reverted is appropriate for v20's substrate. If you must add content, replace existing content, don't append.
4. **Do not re-add the `content_reality` check.** It had a regex false-positive rate high enough that the agent's only recourse was degrading correct content. If you want to prevent decorative content, enforce it at editorial review, not at the token level.
5. **Do not assume a single A-grade run validates the rollback.** Wait for two consecutive clean runs before treating the substrate as the new baseline.

---

## Appendix A — exact file inventory

### Files deleted in Phase 2 (~28 files, ~5000 lines)

```
internal/tools/workflow_checks_causal_anchor.go
internal/tools/workflow_checks_causal_anchor_test.go
internal/tools/workflow_checks_claude_consistency.go
internal/tools/workflow_checks_claude_consistency_test.go
internal/tools/workflow_checks_per_item.go
internal/tools/workflow_checks_per_item_test.go
internal/tools/workflow_checks_reality.go
internal/tools/workflow_checks_reality_test.go
internal/tools/workflow_checks_service_coverage.go
internal/tools/workflow_checks_service_coverage_test.go
internal/tools/workflow_checks_scaffold_hygiene.go
internal/tools/workflow_checks_scaffold_hygiene_test.go
internal/tools/workflow_checks_gotcha_depth_floor.go
internal/tools/workflow_checks_gotcha_depth_floor_test.go
internal/tools/workflow_checks_architecture_narrative.go
internal/tools/workflow_checks_architecture_narrative_test.go
internal/tools/workflow_checks_zerops_yml_depth.go
internal/tools/workflow_checks_zerops_yml_depth_test.go
internal/tools/workflow_checks_readme_container_ops.go
internal/tools/workflow_checks_readme_container_ops_test.go
internal/tools/workflow_checks_claude_md_folk.go
internal/tools/workflow_checks_claude_md_folk_test.go
internal/workflow/recipe_content_fix_gate.go
internal/workflow/recipe_content_fix_gate_test.go
internal/workflow/recipe_writer_brief.go
internal/workflow/recipe_writer_brief_test.go
internal/workflow/recipe_contract_spec.go
internal/workflow/recipe_contract_spec_test.go
```

### Files reverted in Phase 3 (to `5022f8d`)

```
internal/tools/workflow_checks_recipe.go
internal/tools/workflow_checks_finalize.go
internal/tools/workflow_checks_generate.go
internal/tools/workflow_checks.go
internal/content/workflows/recipe.md
internal/workflow/recipe_topic_registry.go
internal/workflow/recipe_topic_registry_test.go
internal/workflow/recipe_section_catalog.go
internal/workflow/engine_recipe.go
internal/workflow/recipe.go
internal/workflow/recipe_substeps.go
internal/workflow/recipe_substep_validators.go
internal/workflow/recipe_substep_validators_test.go
internal/workflow/recipe_plan_predicates.go
internal/workflow/recipe_guidance.go
internal/workflow/recipe_guidance_test.go
internal/workflow/briefing.go
internal/workflow/managed_types.go
internal/workflow/recipe_test.go
```

### Files cherry-picked in Phase 4 (tool-layer wins)

```
# From 58103ef (v8.80)
internal/tools/bash_guard.go                  (wholesale)
internal/tools/bash_guard_test.go             (wholesale)
internal/ops/dev_server_lifecycle.go          (pkill self-kill portion only)

# From 26b0347 (v8.83 portion)
internal/workflow/recipe_guidance.go          (subStepToTopic switch cases + terminal branch only)

# From 1c7e948 (v8.85 portion)
internal/ops/env_shadow.go                    (wholesale)
internal/ops/env_shadow_test.go               (wholesale)
internal/ops/deploy_validate.go               (wholesale — 17 added lines, surgical)
internal/tools/deploy_preflight.go            (wholesale — 25 line diff, surgical)
internal/tools/deploy_preflight_test.go       (wholesale)
internal/tools/deploy_local.go                (setup description + resolved-setup propagation only)
internal/tools/deploy_ssh.go                  (setup description + resolved-setup propagation only)
internal/tools/workflow_checks_generate.go    (env_self_shadow integration only — on top of the Phase 3 revert)

# From 5fa83e8 (v8.86 portion)
internal/tools/record_fact.go                 (wholesale)
internal/tools/record_fact_test.go            (wholesale)
internal/server/server.go                     (record_fact registration only)
```

---

## Appendix B — commit details (chronological)

Listed oldest-first. `KEEP` / `REVERT` / `SPLIT` is the action taken by this guide.

```
5022f8d  KEEP     fix(server): remove keepalive, add shutdown diagnostics and observability
57de8dd  REVERT   feat(workflow): v8.78 load-bearing content reform — 5 new checks, predecessor-floor rollback
9d878c6  KEEP     fix(deploy): harden git-push token check — no leak, handle SSH errors
aadf5b1  KEEP     fix(deploy): git-push checks GIT_TOKEN before push, returns decision question
6a0bd7f  KEEP     refactor(server): extract methodCallTool constant, fix logLevel doc
c9f19b9  KEEP     fix(cicd): explicit fine-grained token permissions per path
2b62823  KEEP     fix(cicd): correct GitHub permission name — Secrets, not Actions secrets
58103ef  SPLIT    feat(workflow): v8.80 v21 post-mortem fixes — revert checks+gate, keep bash_guard + pkill self-kill
bd65081  KEEP     feat(workflow): accept claude-opus-4-7[1m] in recipe start gate
69409b9  REVERT   feat(workflow): v8.81 v22 post-mortem fixes — content-fix dispatch gate, architecture narrative, dev-start contract, scaffold preambles
26b0347  SPLIT    feat(workflow): v8.82 + v8.83 — revert v8.82 content rubric, keep v8.83 response-size fix
1c7e948  SPLIT    feat(workflow): v8.84 + v8.85 — revert recipe.md preamble bloat, keep env_shadow + preflight setup + (optionally) EagerAt refactor
ebea67a  KEEP     feat(workflow): replace DevelopMarker with per-PID WorkSession for develop flow (unrelated)
5fa83e8  SPLIT    feat(workflow): v8.86 — revert writer briefs + contract_spec + claude_md_folk, keep record_fact
```

---

## Appendix C — the check inventory being removed

For provenance, these are the checks that will no longer fire after rollback:

| Check | Added in | Observed failure mode |
|---|---|---|
| `<host>_causal_anchor` | v8.78 | Per-gotcha token-level Zerops-mechanism-naming requirement. Drove v24 content degradation ("response body parser" instead of `res.json`). |
| `<host>_content_reality` | v8.78 | Regex falsely flags barewords like `Nest.js`, `res.json`, `uri.html` as non-existent file paths. Drove ~5 min of content rewrite in every run. |
| `<host>_per_item_standalone` (IG) | v8.78 | Requires every IG item to have a causal anchor in prose. Drove rewrite cycles on decorative IG items. |
| `<host>_service_coverage` | v8.78 | Each managed service needs ≥1 gotcha. Rolled back in v8.79 from gate to informational. |
| `<host>_claude_readme_consistency` | v8.78 rewritten v8.80 | Regex-based forbidden-pattern matcher. Originally matched 0 content in v21 (dead); v8.80 rewrite caught real cases in v22 but drove main-context iteration. |
| `<host>_scaffold_hygiene` | v8.80 | Tests filesystem presence of `node_modules/dist` on the mount — not what git ships. In v24 drove `sudo rm -rf` on live dev containers. |
| `<host>_gotcha_depth_floor` | v8.80 | Per-role minimum gotcha count. Not observably harmful but not load-bearing either. |
| `content_fix_dispatch_required` | v8.81 | Dispatch gate for writer-subagent. Bypassable via "acknowledged inline deviation" (observed in v24). |
| `recipe_architecture_narrative` | v8.81 | Root README must name each codebase + contract. Informational; not destructive. |
| `<host>_dev_start_contract` | v8.81 | Fails generate if `run.start` references compiled output but `buildCommands` doesn't build. Caught 0 real issues in v23/v24. |
| `zerops_yml_comment_depth` | v8.82 | 35% reasoning-marker floor on zerops.yaml comments. Overlaps with env-import comment depth check (which remains). |
| `<host>_ig_integration_guide_causal_anchor` | v8.82 | Second layer on top of `per_item_standalone`. |
| `<host>_readme_container_ops` | v8.82 | Informational nudge to move SSHFS/fuser/ssh/chown mentions to CLAUDE.md. Info-only; harmless but meaningless. |
| `<host>_claude_md_folk` | v8.86 | Prevents specific folk-doctrine strings in CLAUDE.md. Added after v23's "execOnce burn" invention but essentially a hardcoded denylist. |

All are being removed. The `env_self_shadow` check (v8.85) is being kept.

The `knowledge_base_exceeds_predecessor` (predecessor-floor) check is already informational-only as of v8.79; it remains that way.

---

## Appendix D — what v20's behavior looked like

For calibration during v25 runs. From `docs/recipe-version-log.md` v20 entry:

- Wall: 70 min 52 s
- 294 assistant events, 177 tool calls
- Main Bash: 33 calls, 2.3 min total, 0 very-long, 7 errored
- Subagents (10): scaffold ×3, feature ×1, README/CLAUDE writer ×1, yaml-block updater ×1, generate-time fix ×2, code review ×1, close-step critical-fix ×1
- MCP tool mix: 34 workflow, 12 guidance, 11 deploy, 10 dev_server, 2 browser (deploy + close), 6 verify, 4 subdomain, 4 knowledge, 3 mount, 3 logs
- Content: 349/231/267 README lines, 7/6/6 gotchas, 6/5/5 IG items, 99/83/106 CLAUDE.md lines
- Close: 3 CRIT found, 1 critical-fix subagent dispatched (rebuilt + redeployed + re-verified), spotless after fixes

If v25's session looks materially different from this in any dimension (e.g. no critical-fix subagent dispatched, response sizes higher than v20, missing browser walks), that's a concrete signal something didn't revert properly — grep the session log against this envelope before reaching for new machinery.

---

## Final principle

The substrate that produced v20 had known-acceptable content drift and was three versions shorter on wall time than any run since. The machinery added to fix the drift has produced strictly-monotone regression across five runs. Rolling back is the smallest move that changes direction. Any follow-up that adds machinery to fix something v25 exposes is the pattern this rollback is meant to break.
