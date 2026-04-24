# Dev-Server Canonical Primitive — Implementation Plan

> **SUPERSEDED** (archived 2026-04-24). The canonical-primitive install
> shipped across DS-01…DS-04 commits; the atom-contract portion (per-topic
> `TestDevServerAtomContract`) is replaced by the unified authoring
> contract in `plans/atom-authoring-contract.md`. Runtime-class guidance
> for agents now lives in the atom corpus; `docs/spec-workflows.md §8 O4`
> is self-contained and no longer points at this plan.

> **Scope**: Establish `zerops_dev_server` as the canonical primitive for dev-server lifecycle in container env, and harness background task primitive (`Bash run_in_background=true` in Claude Code) as the canonical primitive in local env. Atoms prescribe these patterns directly; the LLM follows the prescription. Code stops asserting runtime state it doesn't actually check. Specs codify the new invariant.
>
> **Framing**: prescriptive, not defensive. We tell the LLM what to do. When guidance is correct and complete, the problematic patterns (raw SSH backgrounding, false "NOT running" claims, 300s hangs) do not arise.
>
> **Supersedes**: `plans/friction-root-causes.md` §2 P1.1 (Delete the "Dev server NOT running" branch). That deletion is subsumed here as part of the full canonical-primitive installation.
>
> **Companion specs** (authoritative):
> - `docs/spec-workflows.md` — §4 develop flow, §8 invariants (O family). O4 gets rewritten.
> - `docs/spec-knowledge-distribution.md` — §3 axes, §8 one-fact-one-home. Atom prose duplication at §3 gets retired.
> - `docs/spec-local-dev.md` — §X local-env model. Already consistent.
> - `CLAUDE.md` — Conventions: single path, fix-at-source, no fallbacks.

---

## 0. Non-Negotiable Constraints

1. **Prescription, not proscription**: atoms tell the LLM what to do. They do NOT say "don't use raw SSH" — they say "use `zerops_dev_server`". When the prescription is clear, drift doesn't happen. (Anti-pattern phrases may still appear in contract tests to catch regressions, but never in atom bodies.)
2. Every workflow-aware response flows through `ComputeEnvelope → BuildPlan → Synthesize` (invariant KD-01, P1).
3. `BuildPlan` and `Synthesize` stay pure — byte-identical output for byte-equal envelopes (KD-02/03).
4. Single path per behavior. After this plan lands, there is exactly ONE atom-level pattern per (env, runtime-class, mode) tuple for dev-server lifecycle.
5. TDD at every affected layer. Tests first (RED) before code (GREEN).
6. Specs reflect reality. Spec edits ship in the same commits as the behavior edits.
7. No new fallbacks, no backward-compat shims. Old patterns get replaced, not alternated.

---

## 1. The Canonical Prescriptions

### 1.1 Container environment

Dev-server lifecycle is owned by `zerops_dev_server`:

```
zerops_dev_server action=start   hostname={h} command={cmd} port={p} healthPath={path}
zerops_dev_server action=status  hostname={h} port={p} healthPath={path}
zerops_dev_server action=stop    hostname={h} port={p}
zerops_dev_server action=restart hostname={h} command={cmd} port={p} healthPath={path}
zerops_dev_server action=logs    hostname={h} logLines={n}
```

Worker services (no HTTP port): `zerops_dev_server action=start noHttpProbe=true` — lifecycle via post-spawn liveness check, not HTTP probe.

No atom emits raw `ssh {host} "cmd &"` or `ssh {host} "nohup cmd &"` for dev-server lifecycle. SSH is still correct for one-shot commands (git ops, framework CLI, curl localhost) — these remain raw-SSH.

### 1.2 Local environment

Dev server runs on the user's machine. Lifecycle is owned by the harness's background task primitive. In Claude Code, that is `Bash run_in_background=true`:

```
Bash run_in_background=true  command="npm run dev"
Bash                         command="curl -s -o /dev/null -w '%{http_code}' http://localhost:3000/"
BashOutput                   bash_id={id}          # tail output
KillBash                     shell_id={id}         # stop
```

ZCP does not spawn processes on the user's local machine. The atoms teach the harness primitive; ZCP contributes by delivering managed-service env through `zerops_env generate-dotenv` and subdomain/URL info through `zerops_discover`.

### 1.3 Triage pattern (env-agnostic)

Before deploying, verifying, or iterating on a runtime service:

1. **Determine expectation** from service type + mode:
   - Implicit-webserver runtime (`php-nginx`, `php-apache`, `nginx`, `static`): platform-owned. Runtime is live on deploy. Nothing to start, nothing to restart. Fix code → deploy → verify.
   - Dynamic runtime, `mode=dev`: `zsc noop` convention. Dev server process is agent-owned.
   - Dynamic runtime, `mode=simple` or `mode=stage`: real `run.start`. Platform auto-starts on deploy with `healthCheck` gate.
2. **Check current state**:
   - Container env: `zerops_dev_server action=status` on the dev service.
   - Local env: `curl -s -o /dev/null -w '%{http_code}' http://localhost:{port}{path}`.
3. **Act on delta**:
   - Expected live, is live → proceed.
   - Expected live, not live → start via the canonical primitive.
   - Live but 5xx → read logs/response body, diagnose. Do NOT restart — restart does not fix bugs.
   - After any redeploy → container is new; previous process is gone; start the dev server again.

### 1.4 Code-side honesty

`DeployResult.Message` and `NextActions` stay agnostic of runtime-class and strategy. The post-deploy message reports what the platform told us (build status, timing, stale warnings filtered by pipeline start) and points at the next right tool — it does not assert process liveness that the code did not check.

---

## 2. Invariants

### Newly established

- **DS-01**: Post-deploy `DeployResult.Message` and `NextActions` are runtime-class-agnostic and strategy-agnostic. They never reference `NeedsManualStart` or `IsImplicitWebServerType` for message construction. Enforced by `TestDeployPostMessageHonesty` (AST scan).
- **DS-02**: Every container-env develop atom whose scope covers dev-server lifecycle (mode=dev/simple/standard + runtime=dynamic/implicit-webserver/static) either prescribes `zerops_dev_server` or explicitly prescribes "no manual start" (implicit-webserver, static, simple-mode). Enforced by `TestDevServerAtomContract`.
- **DS-03**: Every local-env develop atom whose scope covers dev-server lifecycle prescribes harness background task primitive (`Bash run_in_background=true`). Enforced by same test.
- **DS-04**: `zerops_dev_server` is the sole authoritative container-env tool for dev-server lifecycle. Raw `ssh {host} "{cmd} &"`, `ssh {host} "nohup ..."`, and `ssh {host} "setsid ..."` do not appear in any atom body. Enforced by `TestDevServerAtomContract`.

### Rewritten

- **O4** (`spec-workflows.md:1094`): from *"Dev server started manually via SSH after every deploy (container, dynamic runtimes)"* to:

  > **O4**: Dev-server lifecycle in develop workflow is owned by `zerops_dev_server` (container env) or harness background task primitive (local env). Platform auto-starts only for `simple` / `stage` modes and implicit-webserver / static runtimes. Agents never hand-roll SSH backgrounding for dev-server lifecycle in container env.

### Preserved

- O1 (deploy blocks until build completes).
- O3 (subdomain auto-enable).
- KD-01 / KD-02 / KD-03 (envelope pipeline purity).
- KD-11 (ServiceMeta is single persistent state).
- KD-12 (corpus coverage).

---

## 3. Workstreams

Seven workstreams, ordered by dependency. W1 through W3 can run in parallel; W4 depends on W2; W5 depends on W2 + W4; W6 depends on everything; W7 coordinates.

```
                      W1 (code cleanup) ─┐
                      W2 (atom rewrites) ┼─ W5 (contract test) ─┐
                      W3 (spec update) ──┤                      │
                                         W4 (scenarios) ────────┼─ W6 (integration) ─ W7 (release)
```

---

### W1 — Code cleanup

**Problem**: `deploy_poll.go:42-50` and `next_actions.go:28-50` branch on `NeedsManualStart` / `IsImplicitWebServerType` to build post-deploy messages that claim things the code did not actually check. The heuristic maps service type → "needs manual start" without consulting `run.start`; a dynamic service with a real `run.start` (e.g. `gunicorn app.py`) gets the "NOT running" message though the platform auto-started it.

**Design**:

- Post-deploy `DeployResult.Message` collapses to one neutral line:
  ```go
  result.Message = fmt.Sprintf("Successfully deployed to %s. Run zerops_verify for runtime state.", result.TargetService)
  if result.SourceService == result.TargetService {
      result.Message += " New container replaced old — prior SSH sessions are gone."
  }
  ```
- `deploySuccessNextActions` returns `nextActionDeploySuccess` unconditionally.
- Delete `NeedsManualStart` (in `internal/ops/deploy_validate.go:378-387`) and its test `TestNeedsManualStart` (in `deploy_validate_test.go:592-623`). No non-UX consumers.
- **Keep** `IsImplicitWebServerType` — legitimate consumers at `deploy_validate.go:41` (run.start requirement) and `workflow_checks_generate.go:103` (port/healthCheck validation).
- **Keep** runtime-type constants (`runtimePHPApach`, `runtimePHPNginx`, `runtimeNginx`, `runtimeStatic`) — consumed by `IsImplicitWebServerType`.
- **Keep** `checkStartupDetected` (`verify_checks.go:94`) — honest log-based signal, independent of the deleted heuristic.

**Files** (≤5 per phase per CLAUDE.md):
- `internal/tools/deploy_poll.go` — collapse switch.
- `internal/tools/next_actions.go` — collapse conditional.
- `internal/ops/deploy_validate.go` — delete `NeedsManualStart`.
- `internal/ops/deploy_validate_test.go` — delete `TestNeedsManualStart`.
- `internal/tools/deploy_poll_test.go` — add `TestPollDeployBuild_ActiveStatus_NeutralMessage`.
- `internal/tools/next_actions_test.go` — rewrite `TestDeploySuccessNextActions` to assert unified next-action.
- `internal/tools/deploy_ssh_test.go:549-556,604-606` — replace "NOT running" assertions with "Successfully deployed" + absence of runtime-class claims.
- `internal/tools/deploy_post_message_honesty_test.go` (new, ~60 LOC) — AST scan ensuring no `NeedsManualStart` consumer resurrects.

**TDD**:
1. RED: `TestPollDeployBuild_ActiveStatus_NeutralMessage` — mock ACTIVE; assert message contains `"Successfully deployed"` AND does not contain `"NOT running"`, `"idle start"`, `"auto-start"`, `"Built-in webserver"`.
2. RED: `TestDeployPostMessageHonesty` (new) — AST-scan `deploy_poll.go` + `next_actions.go` for identifiers `NeedsManualStart` inside message-construction code. Expect zero hits.
3. RED: flip existing "NOT running" assertions in `deploy_ssh_test.go` to assert absence.
4. GREEN: edit + delete.

**File delta**: -70 LOC.

---

### W2 — Atom corpus: prescriptive rewrites

**Problem**: Six atoms emit raw-SSH dev-server start patterns. These belong to a pre-`zerops_dev_server` era and teach the exact pattern the tool was built to replace (300 s hang on stdio retention).

**Design**: rewrite each atom to prescribe the canonical primitive. Body is shorter (tool API is tighter than raw SSH incantations) and directly actionable.

**Atoms to rewrite (container env, canonical = `zerops_dev_server`)**:

1. `develop-dynamic-runtime-start-container.md` — core start atom. Rewrite to prescribe `zerops_dev_server action=start hostname={hostname} command="{run.start}" port={run.ports[0].port} healthPath={run.ports[0].healthPath or "/"}`.

2. `develop-close-push-dev-dev.md` — close task in dev-only mode. Rewrite flow: `zerops_deploy` → `zerops_dev_server action=start` → `zerops_verify`.

3. `develop-close-push-dev-standard.md` — close task in standard pair. Rewrite: `zerops_deploy dev` → `zerops_dev_server action=start` → `zerops_verify dev` → `zerops_deploy sourceService=dev targetService=stage` → `zerops_verify stage`.

4. `develop-push-dev-workflow-dev.md` — iteration loop. Rewrite: edit mount → `zerops_dev_server action=restart` → probe → repeat.

5. `develop-manual-deploy.md` — manual strategy. Rewrite "Start manually via SSH" to "Start via `zerops_dev_server action=start`".

**Atom to rewrite (local env, canonical = harness background task)**:

6. `develop-dynamic-runtime-start-local.md` — currently tells the agent to SSH into a remote container in local env. That model contradicts local-env semantics (`spec-local-dev.md` — dev server runs on user's machine). Rewrite: local dev server runs on user's machine via harness background task primitive. If the harness is Claude Code: `Bash run_in_background=true command="npm run dev"`. Probe via `curl localhost:{port}`. Kill via harness.

**Atoms to update (minor wording)**:

7. `develop-checklist-dev-mode.md` — replace "agent starts the server over SSH" with "agent starts the server via `zerops_dev_server action=start`".

8. `develop-platform-rules-container.md` — in the "SSH is for running processes" bullet, split long-running vs one-shot: long-running (dev server, Vite HMR, workers) → `zerops_dev_server`; one-shot (artisan/bun/npm commands, git ops, curl localhost) → direct SSH.

9. `develop-platform-rules-local.md` — add a bullet: "Long-running dev servers start via your harness's background task primitive (e.g. `Bash run_in_background=true` in Claude Code), not raw backgrounding in a terminal the tool does not own."

10. `bootstrap-runtime-classes.md` — Dynamic class bullet: update "the real server starts over SSH after each deploy" to "the real server starts via `zerops_dev_server` (container) or harness background task (local) after each deploy".

**New atom**:

11. `develop-dev-server-triage.md` (new, priority 2, `phases: [develop-active]`) — env-agnostic triage atom codifying the expectation → check → action pattern from §1.3. Two implementation sub-blocks (container + local) with the concrete commands. Rendered once per develop-active brief (priority 2 so it comes early, before the specific runtime-start atoms).

**Files**:
- `internal/content/atoms/develop-dynamic-runtime-start-container.md` (rewrite)
- `internal/content/atoms/develop-dynamic-runtime-start-local.md` (rewrite)
- `internal/content/atoms/develop-close-push-dev-dev.md` (rewrite)
- `internal/content/atoms/develop-close-push-dev-standard.md` (rewrite)
- `internal/content/atoms/develop-push-dev-workflow-dev.md` (rewrite)
- `internal/content/atoms/develop-manual-deploy.md` (rewrite)
- `internal/content/atoms/develop-checklist-dev-mode.md` (update)
- `internal/content/atoms/develop-platform-rules-container.md` (update)
- `internal/content/atoms/develop-platform-rules-local.md` (update)
- `internal/content/atoms/bootstrap-runtime-classes.md` (update)
- `internal/content/atoms/develop-dev-server-triage.md` (new)

**TDD**: atoms are markdown; their tests are `atoms_test.go` (frontmatter validation, placeholder whitelist) + `corpus_coverage_test.go` + the new contract test in W5.

**File delta**: ~+300 LOC new body, -250 LOC old body (net ~+50 LOC). New atom ~80 LOC.

---

### W3 — Spec update

**Problem**: `spec-workflows.md` codifies O4 as "Dev server started manually via SSH"; 5+ prose sites repeat the claim. `spec-knowledge-distribution.md:200-208` directly duplicates the content of an atom body inside the spec (drift surface).

**Design**:

1. `docs/spec-workflows.md`:
   - Line 1094 (O4 invariant): rewrite per §2 above.
   - Lines 210, 547, 763, 846, 885: update prose to reference `zerops_dev_server` / harness background task.
2. `docs/spec-knowledge-distribution.md`:
   - Lines 200-208: remove the duplicated atom body; replace with a pointer: *"See `develop-dynamic-runtime-start-container.md` for the prescribed pattern."* (One-fact-one-home per KD-11.)
3. `docs/spec-local-dev.md`: no change; already consistent.

**Files**:
- `docs/spec-workflows.md`
- `docs/spec-knowledge-distribution.md`

**TDD**: specs have no runtime tests; consistency enforced by the contract test in W5. Commit message explicitly cites O4 rewrite.

**File delta**: ~+30 LOC / −60 LOC (net −30 LOC after duplication removal).

---

### W4 — Scenario coverage

**Problem**: zero scenarios today assert dev-server lifecycle patterns. The canonical prescription needs eval coverage so future drift fails at scenario time.

**Design**: two new scenarios + snapshot updates where needed.

**New scenarios**:

1. `internal/eval/scenarios/develop-dev-server-container.md` — container env + push-dev + dev mode + dynamic runtime. Agent is expected to call `zerops_dev_server action=start` (or `restart`) at least once after the first deploy. `forbiddenPatterns`: raw `ssh ... "npm run` / `nohup ... &` / `setsid` in tool calls. `mustCallTools`: `zerops_dev_server`.

2. `internal/eval/scenarios/develop-dev-server-local.md` — local env + dev mode + dynamic runtime. Agent uses harness background task primitive (Bash run_in_background). `forbiddenPatterns`: `zerops_dev_server` (wrong env) and raw `ssh` for starting the server.

**Existing scenarios — snapshot check**:

None of the scenarios in `internal/workflow/scenarios_test.go` pin the specific post-deploy Message text (only primary-tool and workflow-enter assertions). No snapshot update required unless a specific test surfaces during implementation.

**Files**:
- `internal/eval/scenarios/develop-dev-server-container.md` (new)
- `internal/eval/scenarios/develop-dev-server-local.md` (new)

**TDD**: scenarios run under `zcp eval`. Include in gate runs post-merge.

**File delta**: +80 LOC (two scenarios).

---

### W5 — Contract test

**Problem**: atom-level prescriptions need a mechanical guardrail. A future edit that reintroduces raw-SSH dev-server patterns into an atom body must fail at test time.

**Design**: new test file `internal/workflow/dev_server_atom_contract_test.go` modeled after `subdomain_atom_contract_test.go`. For each rewritten atom:
- `phraseRequired` on the prescribed command (e.g. `zerops_dev_server action=start` for container atoms; `Bash run_in_background=true` for the local atom).
- `phraseAbsent` on regression-marker patterns (e.g. `ssh -o StrictHostKeyChecking=no` appearing in a dev-server context — matched by nearest-surrounding phrase like `"cd /var/www && npm"`).

**Entries**:

```go
rules := []rule{
    {
        atomID:       "develop-dynamic-runtime-start-container",
        required:     []string{"zerops_dev_server action=start"},
        forbidden:    []string{`cd /var/www && {start-command}`, `nohup`, `setsid`, `\"cmd &\"`},
    },
    {
        atomID:       "develop-dynamic-runtime-start-local",
        required:     []string{"Bash run_in_background=true"},
        forbidden:    []string{`ssh -o StrictHostKeyChecking=no`, `ssh {hostname}`},
    },
    {
        atomID:       "develop-close-push-dev-dev",
        required:     []string{"zerops_dev_server"},
        forbidden:    []string{`NEW SSH session`, `ssh {hostname} "cd /var/www`},
    },
    {
        atomID:       "develop-close-push-dev-standard",
        required:     []string{"zerops_dev_server"},
        forbidden:    []string{`NEW SSH session`, `ssh {hostname} "cd /var/www`},
    },
    {
        atomID:       "develop-push-dev-workflow-dev",
        required:     []string{"zerops_dev_server"},
        forbidden:    []string{`Restart the server over SSH`, `ssh {hostname} "cd /var/www`},
    },
    {
        atomID:       "develop-manual-deploy",
        required:     []string{"zerops_dev_server"},
        forbidden:    []string{`Start manually via SSH`},
    },
    {
        atomID:       "develop-dev-server-triage",
        required:     []string{"zerops_dev_server action=status", "Bash run_in_background=true"},
    },
    {
        atomID:       "bootstrap-runtime-classes",
        required:     []string{"zerops_dev_server"},
    },
}
```

**Files**:
- `internal/workflow/dev_server_atom_contract_test.go` (new, ~140 LOC)

**TDD**: this IS the RED test for W2 — rules fail until atoms are rewritten. When all atoms land, it goes green. Matches friction plan P2.5's intent without depending on a generic framework — same pattern as subdomain-atom-contract-test.

**File delta**: +140 LOC.

---

### W6 — Integration + verification

**Gates before merge**:

1. `go test ./... -count=1 -race`: green.
2. `make lint-local`: green.
3. Full atom-corpus coverage matrix (`corpus_coverage_test.go`) stays green — no regression in axis conjunctions.
4. Contract tests green: `TestDevServerAtomContract`, `TestDeployPostMessageHonesty`, existing `TestSubdomainAtomContract`.
5. Scenario evaluation in ZCP container:
   ```
   zcp eval scenario --file internal/eval/scenarios/develop-dev-server-container.md
   zcp eval scenario --file internal/eval/scenarios/develop-dev-server-local.md
   ```
6. Regression scenarios continue to pass:
   - `develop-first-deploy-branch.md`
   - `develop-add-endpoint.md`
   - `greenfield-nodejs-todo.md`
   - `greenfield-laravel-weather.md`

**Invariant checklist**:

- [ ] DS-01 — post-deploy message honesty enforced by `TestDeployPostMessageHonesty`.
- [ ] DS-02 — container atoms prescribe `zerops_dev_server` enforced by `TestDevServerAtomContract`.
- [ ] DS-03 — local atoms prescribe harness task enforced by `TestDevServerAtomContract`.
- [ ] DS-04 — raw-SSH dev-server patterns absent from atom corpus enforced by `TestDevServerAtomContract`.
- [ ] O4 rewritten in `spec-workflows.md`.
- [ ] `spec-knowledge-distribution.md:200-208` duplication removed.

---

### W7 — Release

Per `CLAUDE.local.md` release process:

1. `git pull --rebase origin main`.
2. `go test ./... -count=1 -race`: green.
3. `make lint-local`: green.
4. `make release` (minor bump).
5. Watch GitHub Actions run (`gh run watch`). If it fails on `-race` or the strict linter, fix root cause, `make release-patch`.

Release commit message cites the plan + supersession:

```
feat(dev-server): canonicalize zerops_dev_server as dev-server primitive

Dev-server lifecycle in develop workflow is owned by zerops_dev_server
(container env) or harness background task primitive (local env).
Atoms prescribe; code asserts only what it checks; specs codify O4
rewrite.

Supersedes friction-root-causes P1.1.
See plans/dev-server-canonical-primitive.md.
```

---

## 4. File Budget Summary

| Area | Added | Edited | Deleted | Net LOC |
|---|---|---|---|---|
| Code (`internal/ops`, `internal/tools`) | +60 (honesty test) | +10 (message) | −70 (`NeedsManualStart`, 3 test sites) | −70 |
| Atoms | +80 (triage), +60 (new bodies) | ~−40 (compression of rewrites) | 0 | +100 |
| Specs | +30 (O4 rewrite + prose) | — | −60 (duplication removal) | −30 |
| Scenarios | +80 (two new) | 0 | 0 | +80 |
| Contract test | +140 | 0 | 0 | +140 |
| **Total** | **+450** | — | **−200** | **+220** |

~22 files touched. 11 atoms, 6 test files, 2 specs, 2 scenarios, 1 new contract test.

---

## 5. Execution Order + Tasks

1. **T1**: Write plan (this file).
2. **T2**: W5 RED — create `dev_server_atom_contract_test.go` with rules for all future atoms. Test fails initially. Commit RED.
3. **T3**: W1 RED — add `TestPollDeployBuild_ActiveStatus_NeutralMessage` + `TestDeployPostMessageHonesty`. Flip `deploy_ssh_test.go` assertions. Commit RED.
4. **T4**: W1 GREEN — collapse `deploy_poll.go` switch, collapse `next_actions.go`, delete `NeedsManualStart`. Commit GREEN.
5. **T5**: W2 GREEN (batch 1/3) — rewrite `develop-dynamic-runtime-start-container.md` + `develop-dynamic-runtime-start-local.md`. Run contract test. Commit.
6. **T6**: W2 GREEN (batch 2/3) — rewrite `develop-close-push-dev-{dev,standard}.md` + `develop-push-dev-workflow-dev.md` + `develop-manual-deploy.md`. Commit.
7. **T7**: W2 GREEN (batch 3/3) — update `develop-checklist-dev-mode.md`, `develop-platform-rules-{container,local}.md`, `bootstrap-runtime-classes.md`. Create `develop-dev-server-triage.md`. Commit. Contract test goes fully green.
8. **T8**: W3 — spec updates (`spec-workflows.md`, `spec-knowledge-distribution.md`). Commit.
9. **T9**: W4 — create two scenario files. Commit.
10. **T10**: W6 — full gate run; fix any fallouts. Commit fixes.
11. **T11**: W7 — release.

---

## 6. Rollback Strategy

Per-phase commits preserve clean revert ranges. If post-merge eval regresses:

| Phase | Rollback impact |
|---|---|
| T4 (code) | `NeedsManualStart` returns, post-deploy message reintroduces the dishonest "NOT running" claim |
| T5-T7 (atoms) | Specific atoms revert to raw-SSH prescriptions |
| T8 (specs) | O4 reverts to "via SSH" wording |
| T9 (scenarios) | Coverage gap returns |

Each commit is independently revertable. `TestDevServerAtomContract` gates the atom-corpus commits.

---

## 7. Open Questions

1. **Harness portability**: the local atom mentions "Claude Code's `Bash run_in_background=true`". If ZCP ever targets a non-Claude harness, the atom needs generalization. Today: ZCP is Claude-primary; accept the specific reference with a generic-framing preamble.

2. **Triage atom priority**: proposed priority 2 so it renders early. If rendered size grows to >20 KB, revisit — may demote to priority 3 and gate more aggressively.

3. **`develop-dynamic-runtime-start-local.md` — keep or delete?** Current content is actively wrong (prescribes SSH-to-remote in local env). Rewriting to prescribe harness background task is a clean fit. **Decision**: rewrite.

4. **Local-env scenario — harness assumption**: `develop-dev-server-local.md` scenario asserts `Bash run_in_background=true` call. This assertion is Claude-Code-specific. If eval runs on a non-Claude harness, the assertion needs generalization. Today: acceptable.

---

## 8. Evidence Index

### Code anchors (current state)

- `internal/tools/deploy_poll.go:42-50` — three-branch message switch to be collapsed.
- `internal/tools/next_actions.go:28-50` — `deploySuccessNextActions` conditional to be collapsed.
- `internal/ops/deploy_validate.go:378-387` — `NeedsManualStart` to be deleted.
- `internal/ops/deploy_validate_test.go:592-623` — `TestNeedsManualStart` to be deleted.
- `internal/tools/deploy_ssh_test.go:549-556,604-606` — "NOT running" assertions to be flipped.
- `internal/tools/next_actions_test.go:46-104` — `TestDeploySuccessNextActions` to be simplified.
- `internal/ops/deploy_validate.go:41` — `IsImplicitWebServerType` consumer (KEEP).
- `internal/tools/workflow_checks_generate.go:103` — `IsImplicitWebServerType` consumer (KEEP).
- `internal/ops/verify_checks.go:94` — `checkStartupDetected` (KEEP — honest signal).

### Existing tool

- `internal/tools/dev_server.go` — `zerops_dev_server` registration (no changes needed).
- `internal/ops/dev_server.go`, `dev_server_lifecycle.go`, `dev_server_start.go` — tool implementation (no changes needed).
- `internal/content/workflows/recipe/principles/dev-server-contract.md` — recipe-side authoritative contract (reference model for develop atom).
- `internal/workflow/subdomain_atom_contract_test.go` — pattern reference for W5.

### Spec anchors (to update)

- `docs/spec-workflows.md:1094` — O4 invariant.
- `docs/spec-workflows.md:210,547,763,846,885` — prose mentions.
- `docs/spec-knowledge-distribution.md:200-208` — duplicated atom body.

### Atom anchors (to rewrite/update)

- `internal/content/atoms/develop-dynamic-runtime-start-container.md`
- `internal/content/atoms/develop-dynamic-runtime-start-local.md`
- `internal/content/atoms/develop-close-push-dev-{dev,standard}.md`
- `internal/content/atoms/develop-push-dev-workflow-dev.md`
- `internal/content/atoms/develop-manual-deploy.md`
- `internal/content/atoms/develop-checklist-dev-mode.md`
- `internal/content/atoms/develop-platform-rules-{container,local}.md`
- `internal/content/atoms/bootstrap-runtime-classes.md`

---

## 9. Scope Exclusions (intentional)

- **Asset-pipeline atoms** (`develop-first-deploy-asset-pipeline-{container,local}.md`) — service-specific Vite/Laravel knowledge. By design, covered by recipe-local `CLAUDE.md` (enforced via `TestCheckCLAUDEMdExists`) + `zerops_knowledge` lookups. Not a develop-atom concern. No edits in this plan.
- **Local-exec backend for `zerops_dev_server`** — explicitly rejected during design. Local dev processes use harness primitive. Registration stays container-only.
- **Recipe pipeline** (`internal/content/workflows/recipe/*`) — already uses `zerops_dev_server` canonically. No edits.
- **`RuntimeStartClass` envelope field** — mode + runtime-class axes suffice. No new envelope field.
- **Atom-code contract framework (friction plan P2.5)** — not needed here; a specific `dev_server_atom_contract_test.go` matches the scope, following `subdomain_atom_contract_test.go` pattern.
