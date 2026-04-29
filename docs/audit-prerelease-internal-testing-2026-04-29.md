# Pre-Internal-Testing Audit — Lifecycle & Knowledge Delivery

**Date**: 2026-04-29
**Scope**: Mental-model walk + automated matrix simulation + Codex adversarial review of the full ZCP lifecycle (bootstrap → develop → close) before internal testing kicks off.
**Goal**: Catch the "first-day dumb errors" — places where the spec says one thing, the atoms say another, the tests pin a third, and an agent walks into a contradiction.

---

## What was checked

| Source | Method | Output |
|---|---|---|
| `docs/spec-workflows.md` (1300+ lines) | Read end-to-end | Mental model of two-phase lifecycle |
| `internal/workflow/{synthesize,build_plan,bootstrap_guide_assembly}.go` | Explore agent | Pipeline mapping (envelope → plan → atoms) |
| `internal/tools/workflow*.go` + `internal/ops/deploy_*.go` | Explore agent | Deploy-mode dispatch (close-mode / git-push / build-integration) |
| 80 atoms in `internal/content/atoms/*.md` | Matrix simulator | 45 scenarios × actual `Synthesize` output → markdown report |
| Same code, independent pass | Codex (gpt-5.3) | 13 findings with file:line citations |

The matrix simulator is permanent: `internal/workflow/lifecycle_matrix_test.go`, gated by `ZCP_RUN_MATRIX=1`. Output lands at `internal/workflow/testdata/lifecycle-matrix.md`. Re-run any time after atom edits to see the diff.

---

## Lifecycle mental model (verified)

### Phase 1 — Enter Evidence (once per service)

Bootstrap is **infrastructure-only** since v8.100+ (Option A). Three steps: `discover → provision → close`. The **four routes** of bootstrap don't 1:1 map to the user's "adopt / import / manual" mental model:

| User's word | Implementation route | Notes |
|---|---|---|
| **adopt** | `route="adopt"` | Existing services without ServiceMeta |
| **import** | `route="recipe"` (template-driven) | No `route="import"` — closest is `recipe` which fetches a curated import.yaml from the recipe corpus |
| **manual** | `route="classic"` | Free-form plan; agent constructs import.yaml from intent |
| (recovery) | `route="resume"` | Claim dead-PID session |

Bootstrap NEVER deploys app code — develop owns the first deploy and stamps `FirstDeployedAt`.

### Phase 2 — Develop (repeated per task)

Status pipeline: `ComputeEnvelope` (live API + meta + session) → `BuildPlan` (pure dispatch) → `Synthesize` (atom corpus filter+render) → `RenderStatus` (markdown). All three middle stages are pure functions over the envelope JSON — same input, byte-identical output (compaction-safety).

**Three close-modes** = what `action="close"` does:
- `auto` → `zerops_deploy` directly (zcli push)
- `git-push` → commit + push to configured remote (Zerops/CI builds)
- `manual` → ZCP yields, user owns

**Three orthogonal axes** (CLAUDE.md "Deploy config is three orthogonal dimensions"):
- `CloseDeployMode` (what close does)
- `GitPushState` (capability provisioned?)
- `BuildIntegration` (which CI shape)

**D2a invariant**: First deploy ALWAYS uses default self-deploy regardless of close-mode. Close-mode only kicks in after `FirstDeployedAt` stamps. ✅ Confirmed in `develop-first-deploy-intro.md:24` — atom explicitly tells agent "no `strategy` argument on first deploy".

---

## Findings — sorted by severity

### CRITICAL

#### C1 — Stale `action="strategy"` vocabulary across atoms + eval + tests

**Symptom**: agent reading `develop-strategy-awareness.md:23` calls `zerops_workflow action="strategy" strategies={"appdev":"push-dev"}`. Handler at `internal/tools/workflow.go:313` returns `Unknown action "strategy". Valid actions: …, close-mode, …`. The `default:` branch hint guides recovery — so it's not a crash — but every first-test session pays this round-trip and risks cascading confusion.

**Affected files** (all need rewrite, NOT alias-add — clean removal per global instructions):

| File | What's wrong | Fires in |
|---|---|---|
| `internal/content/atoms/develop-strategy-awareness.md` | Body says `strategy=push-dev/push-git/manual/unset` and `action="strategy"` | EVERY develop-active scenario where close-mode is set (15 of my 45 sims) |
| `internal/content/atoms/bootstrap-recipe-close.md:25` | Bootstrap close hands off to develop with `action="strategy"` instructions | Every recipe-route bootstrap close |
| `internal/content/atoms/develop-platform-rules-local.md` | Mixes `strategy=git-push` (still valid `zerops_deploy` arg) with `strategy=push-dev` (retired vocab) — confusing | Every local-env develop |
| `internal/eval/instruction_variants.go:48,62` | TWO instruction variants document `action="strategy" strategies={…}` to test agents | Every eval run uses one of the variants |
| `internal/eval/scenarios/develop-strategy-unset-regression.md:21,56` | Eval scenario expects old action | Eval-time false negatives |
| `internal/eval/scenarios/strategy-push-git-setup.md` | Same | Same |
| `internal/eval/scenarios/preseed/strategy-push-git-setup.sh` | Preseed script names old | Same |
| `internal/eval/eval_test.go:318` | Test ASSERTS the legacy text appears | Test passes only because atom is wrong |
| `internal/workflow/corpus_coverage_test.go:198` | MustContain assertion for `action="strategy"` | Same |
| `internal/workflow/bootstrap_outputs_test.go:660` | Same | Same |

**Plus 8-atom "push-dev" naming family** — frontmatter axes are correct (`closeDeployModes: [auto]`), but IDs/titles/bodies still use the retired "Push-Dev" label. An agent rendering the iteration cycle reads "### Push-Dev Deploy Strategy" as a section header even though the close-mode value is `auto`:

| Atom | Title (rendered to agent) |
|---|---|
| `develop-close-push-dev-dev.md` | "Close task — push-dev dev mode (no stage)" |
| `develop-close-push-dev-simple.md` | "Close task — push-dev simple mode" |
| `develop-close-push-dev-standard.md` | "Close task — push-dev standard mode" |
| `develop-close-push-dev-local.md` | "Close task — push-dev" |
| `develop-push-dev-deploy-container.md` | "Push-dev strategy — deploy via zerops_deploy" |
| `develop-push-dev-deploy-local.md` | "Push-dev strategy — deploy via zerops_deploy" |
| `develop-push-dev-workflow-dev.md` | "Push-dev iteration cycle (dev mode)" |
| `develop-push-dev-workflow-simple.md` | "Push-dev iteration cycle (simple mode)" |

Plus body mentions in `develop-static-workflow.md`, `develop-change-drives-deploy.md` (own search-and-replace).

And **`docs/spec-workflows.md` itself uses retired vocab** at lines 914, 943, 962-963, 1103 — "Deploy (push-dev)" / routing offering "push-dev/push-git" / atom name reference. Spec is source-of-truth; needs sweeping too.

**Fix size**: small per file (search-and-replace) but **wide blast radius** (~25 files including atoms + tests + eval + spec + atom file renames). Must touch all together so the corpus + tests + eval + spec are coherent. This is a textbook "single canonical path" sweep per the user's `feedback_single_path_engineering.md` memory.

**Why CRITICAL**: every internal test session — eval AND interactive — would observe this within minutes. Eval would be additionally polluted (false negatives, pass-because-broken tests).

---

### HIGH

#### H1 — Stage cross-deploy plan loses `sourceService` for standard mode

**Source**: Codex finding #1, `internal/workflow/build_plan.go:81`+244+246.

`planDevelopActive` iterates `WorkSession.Services`, finds first pending host, emits `Plan.Primary = zerops_deploy targetService=<host>` with NO `sourceService`. When the pending host is `appstage` in standard mode, the typed Plan tells the agent to `zerops_deploy targetService="appstage"` — a SELF-deploy of stage. But spec §4.6 + standard-mode atoms say stage is always a CROSS-deploy from dev (`sourceService="appdev" targetService="appstage" setup="prod"`).

**Failure mode**: agent follows Plan, hits DM-2 (self-deploy with stage's narrower deployFiles → ErrInvalidZeropsYml). Best case: agent re-reads atoms, recovers. Worst case: builds wrong artifact.

**Fix size**: structural — Plan needs to carry `sourceService` for stage halves. Probably needs `deployActionFor` to consult the matched ServiceSnapshot's `Mode` + paired Hostname.

#### H2 — `develop-first-deploy-promote-stage` atom omits `setup="prod"`

**Source**: Codex finding #2.

Compare:
- `develop-first-deploy-promote-stage.md:21`: `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"` ❌ no setup
- `develop-close-push-dev-standard.md:22`: `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}" setup="prod"` ✅

First standard-mode promotion to stage uses default setup name (likely "dev"), which is the wrong block (different `start`, `deployFiles`, `healthCheck`). Build/run will diverge from expectations.

**Fix size**: single atom edit + drift test.

#### H3 — Status response has no token cap

**Source**: Codex finding #3 + my matrix simulation.

`internal/tools/workflow_develop.go:214→222` synthesizes ALL matched atoms; `render.go:223` dumps every guidance item. The 28KB soft cap exists ONLY for sub-agent dispatch briefs (`internal/tools/workflow.go:80`), not for status responses.

Empirical from matrix:
- 12.1 export-active container: **27.9 KB** (6 atoms; export-classify-envs alone is 9.6 KB)
- 10.2 mixed runtimes (api + web + db): **32.8 KB** — over MCP soft limits
- 7.4 D2a edge case: 21 KB

**Failure mode**: complex multi-service projects bury the Plan/Next section under 30 KB of atom prose. LLM context-window pressure. Could miss the "use `setup=prod`" pointer in a wall of text.

**Fix size**: structural — needs same atom-id envelope spill mechanism that dispatch briefs have, applied to status.

---

### MEDIUM

#### M1 — Silent wrong-identity persistence on `.git/`

**Source**: Codex #4. `InitServiceGit` failure logged stderr-only at `internal/tools/workflow_bootstrap.go:204`. Deploy safety net at `internal/ops/deploy_ssh.go:199` only fires when `.git/` is MISSING (`test -d .git || (...)`). If a wrong `.git/` exists from another tool, identity reconfig is skipped and commits attribute to whatever was set previously.

**Fix size**: structural — either tighten the safety net to always re-set identity, or surface the InitServiceGit failure as a hard MOUNT_FAILED.

#### M2 — Stage entry timing is guidance only

**Source**: Codex #5. `internal/ops/deploy_validate.go:110+116+121` validates per-role shape but doesn't enforce the "stage entry written AFTER dev verified" sequence. If agent writes both upfront, deploy succeeds and the spec's safety story ("dev verified before stage exists") is broken without alarm.

**Fix size**: structural — would need session-scoped "have we seen a successful dev verify yet?" gate.

#### M3 — `local-stage` atom coverage gaps

**Source**: Codex #6. `local-stage` mode is subdomain-eligible (`deploy_subdomain.go:117`) and first-deploy supports it (`develop-first-deploy-execute.md:5`), but:
- `develop-close-push-dev-local.md:6` is `[dev, stage]` — local-stage agents won't see local close cadence
- `develop-dynamic-runtime-start-local.md:7` is `[dev, standard]` — no dev-server start atom for local-stage
- `develop-ready-to-deploy.md:5` excludes local-stage

**Fix size**: small — add `local-stage` to the missing axis lists.

#### M4 — Aggregate atoms can use global host outside `services-list:`

**Source**: Codex #7. `internal/workflow/synthesize.go:118` falls back to `globalHost`/`globalStage` for placeholder substitution OUTSIDE the `{services-list:…}` directive. Future aggregate atoms with stray placeholders re-introduce the wrong-host bug fixed in C2.

**Fix size**: structural — assert at corpus-load that aggregate atoms have NO bare `{hostname}`/`{stage-hostname}` outside a `services-list` directive.

---

### LOW (UX gaps, not bugs)

#### L1 — "import" terminology mismatch
`bootstrap-route-options.md:35` lists routes as adopt/recipe/classic/resume. User's mental model says "import". Agent translating user intent might try `route="import"` → engine rejects (`engine.go:339`). Add a synonym hint or rephrase atom to say "use `route=recipe` for import-yaml templates".

#### L2 — Plan doesn't encode "first deploy ignores close-mode"
Atoms cover D2a well (`develop-first-deploy-intro.md:24`), but Plan dispatch doesn't surface it as `Rationale`. Agent reading Plan alone (without atoms) might assume close-mode applies to first deploy.

#### L3 — Recipe collision detection only at plan submit
Discovery annotates collisions (`route.go:153`) but the agent can still pick recipe and learn at plan-commit that hostnames clash. Surface the conflict more loudly in the route discovery response.

---

### REFUTED (originally suspected, found NOT bugs)

| Original suspicion | Refutation |
|---|---|
| Multi-service axis collision (atom matched B but renders A) | Per-service binding works correctly — `synthesize.go:65→141` |
| Cross-deploy stage subdomain not auto-enabled | Auto-enable path is platform-side and pair-aware (`deploy_subdomain.go:39+41+117`) |
| `develop-strategy-review` never reaches the agent before auto-close | Auto-close refuses unset close-mode (`work_session.go:375`); the prompt is reachable |

---

## Drift findings — second pass (post-C1 sweep, more bugs of the same family)

After confirming C1 with the user, did a targeted hunt for OTHER places where recent refactors didn't propagate. Used: recent commits (`deploy-strategy-decomposition`, `dev-server-canonical-primitive`, `develop-flow-enhancements`, `plan-pipeline-repair`), the R1-R21 root-problem list from `plans/archive/deploy-strategy-decomposition-2026-04-28.md`, plus a Codex adversarial pass.

### HIGH

#### C2 — `git-push` deploy stamps `Deployed=true` on push success, not on async build land

**Source**: Codex finding #1, R3 from deploy-strategy-decomposition plan (was supposedly fixed — was not).

`internal/tools/deploy_git_push.go:300` sets `SucceededAt` immediately when `git push` transmits. `internal/workflow/work_session.go:220` then stamps `FirstDeployedAt`. `internal/workflow/compute_envelope.go:260` reports `Deployed: true`. For `BuildIntegration ∈ {webhook, actions}` the actual Zerops build is still ASYNC — it hasn't run yet.

**Failure mode**: agent observes `Deployed: true` after a push, runs `zerops_verify`, gets stale-deploy results (or no app at all), sees verify fail, retries — meanwhile the real build is still running. Auto-close gate may also fire prematurely if verify happens to pass against stale state.

**Fix size**: structural — gate the stamp on the async build event reaching `Status: ACTIVE` (per `develop-record-external-deploy` semantics). Or document that `git-push` close-mode requires explicit `record-deploy` after build observation, and pull the auto-stamp out of `deploy_git_push.go`.

#### C3 — Atoms reference `failureClassification` field on TimelineEvent that doesn't exist

**Source**: Codex finding #4, R10 from deploy-strategy-decomposition plan (DEFERRED — confirmed still missing).

`internal/ops/events.go:28` `TimelineEvent` carries only `FailReason string` + `Hint string`. No `failureClass` / `failureClassification`. But:
- `internal/ops/events.go:94` (the BUILD_FAILED hint) tells agent: *"Read failureClass + description on this event …"*
- `internal/content/atoms/develop-build-observe.md:24` repeats: *"Read the latest event's `failureClass` + `description` for the cause"*

Agent looks for the field, finds nothing, falls back to raw `FailReason` (which is what was supposed to be classified). Misses the structured diagnostic that the deploy-failure classifier produces for the SYNCHRONOUS path — async path silently has no classifier.

**Fix size**: medium — either propagate `FailureClass` from build events into `TimelineEvent`, OR rewrite the events hint + atom to read `failReason` + maybe regex-classify in the atom prose.

#### C10 — Spec invariant D2b is OUT OF DATE; handler has a different (better) guard

**Source**: spec `docs/spec-workflows.md:1100` D2b says: *"`handleGitPush` refuses with `PREREQUISITE_MISSING` when the target meta has no `FirstDeployedAt` — defense in depth against agents that ignore the atom guidance."*

Codex verification surfaced the actual current behavior at `internal/tools/deploy_git_push.go:180-188`:
```
// Pre-flight: the container must have a git repo with at least one
// commit at workingDir. A git push with nothing to transmit is either
// a user bug or a silent fallback we refuse to ship (an earlier
// design auto-committed everything when no commits existed; that
// masked "agent forgot to commit" failures). See plan phase A.2 —
// this replaces the old meta.IsDeployed() gate which false-positived
// on adopted services the platform had deployed before ZCP ever
// touched the meta.
committedOut, err := sshDeployer.ExecSSH(ctx, hostname, committedCodeCheckCmd(workingDir))
```

The handler INTENTIONALLY REPLACED the `FirstDeployedAt`/`meta.IsDeployed()` gate with a "committed code at workingDir" check — because the meta-based gate false-positived on adopted services the platform had deployed before ZCP ever wrote the meta. So:

1. The spec invariant D2b is FACTUALLY WRONG — handler does NOT refuse on no `FirstDeployedAt`. Spec needs updating to describe the committed-code check.
2. The CLAUDE.md invariant `D2b` (under "Develop Flow / Work Session") is similarly out of date.
3. There's NO documentation pointer from the new pre-flight back to the rationale comment, so the next person reading the spec will be confused why the code doesn't match.

**Failure mode**: agent reads spec, expects `PREREQUISITE_MISSING` on no `FirstDeployedAt`, doesn't get it. Or reads the wrong invariant text and misroutes a recovery flow.

**Fix size**: small — sweep spec D2b text + CLAUDE.md D2b bullet to describe the committed-code check; reference the rationale comment in `deploy_git_push.go:180-188`. Bonus: pin `TestHandleGitPush_RefusesWithoutCommittedCode` if not already present.

### MEDIUM

#### C4 — `develop-record-external-deploy` atom is gated on `deployStates: [deployed]` but its job is to STAMP that state

**Source**: Codex finding #5.

Frontmatter says `deployStates: [deployed]`. Body says: *"Without that record the service stays at `deployState=never-deployed` here … `record-deploy` is the canonical bridge: it records the deploy."*

Catch-22: the atom that explains how to escape `never-deployed` only fires AFTER you've already escaped. First-time external-deploy users (webhook-built services after first build) never see this guidance.

**Fix size**: small — change axis to `deployStates: [never-deployed]` plus `buildIntegrations: [webhook, actions]` (already there), OR drop the `deployStates` axis and rely on the build-integration filter alone.

#### C5 — Asset-pipeline atom uses raw SSH backgrounding for dev server (forbidden per dev-server-canonical-primitive)

**Source**: my matrix simulation flagged 4 atoms with `nohup`/`&`; 3 are legitimate anti-pattern explanations, **but** `internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:38` actually instructs:

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && nohup npm run dev > /tmp/vite.log 2>&1 &'
```

The dev-server-canonical-primitive plan (archived 2026-04-24) explicitly forbids agent-rolled SSH backgrounding for dev-server lifecycle in container env. Should route through `zerops_dev_server action=start`. Three sibling atoms (`develop-platform-rules-container.md:21`, `develop-dynamic-runtime-start-container.md:37`, `develop-dev-server-reason-codes.md:20`) explicitly call out `ssh "cmd &"` as anti-pattern — this atom violates exactly that rule.

**Fix size**: small — rewrite the Vite-start block to use `zerops_dev_server action=start hostname={hostname} command="npm run dev" port={vite-port} healthPath="/"`.

#### C6 — `develop-platform-rules-local.md:31-32` mixes current and retired `strategy` vocabulary

**Source**: my grep — already noted in C1, calling out separately because it's the only atom mixing BOTH valid (`zerops_deploy strategy="git-push"` arg) AND retired (`strategy=push-dev` field-naming) usages in adjacent table rows. Confusing for the agent because they look like the same vocabulary class.

```
| Deploy source | … `strategy=git-push` needs commits; `strategy=push-dev` ships the tree. |
| Git-push setup | Before `zerops_deploy strategy=git-push`, …; Default push-dev needs no git state. |
```

The first half (`strategy=git-push` as `zerops_deploy` argument) is correct. The second half (`strategy=push-dev` as conceptual label) is retired.

**Fix size**: small — replace the retired half with "default zcli push" or similar.

### LOW

#### C7 — Phase 3 of `develop-flow-enhancements.md` was actively reverted; plan still describes it as a goal

**Source**: `internal/tools/workflow_close_test.go:9-13` documents that the close-without-deploy guard was implemented and then REMOVED ("created friction on legitimate pivots, the agent worked around it via direct tools"). But `plans/develop-flow-enhancements.md:41` still has Phase 3 *Goal*: "`action=close workflow=develop` refuses when no successful deploy unless `force=true` given. Auto-close path untouched." — describing as an aspirational goal what was actively dismantled.

The `Force` field on `WorkflowInput` exists but was repurposed for `action="start" workflow="develop"` (discard-and-replace) — completely different semantics from what Phase 3 specified.

**Failure mode**: someone (human or LLM) reading the plan thinks the close guard is on the roadmap, tries to implement it, breaks `workflow_close_test.go`. Or reads the plan as authoritative spec and is confused why behavior differs.

**Fix size**: small — `git mv plans/develop-flow-enhancements.md plans/archive/develop-flow-enhancements.md` with a header note "Phase 3 reverted — see workflow_close_test.go for rationale". Phase 4 (mode expansion) IS implemented.

#### C8 — Plan body uses retired field names (`DeployStrategy`, `StrategyConfirmed`)

**Source**: `plans/develop-flow-enhancements.md` Phase 4 mechanism describes meta merge as: *"Keep `BootstrappedAt`, `DeployStrategy`, `StrategyConfirmed`, `BootstrapSession`."* These field names were retired in deploy-strategy decomposition; current names are `CloseDeployMode`, `CloseDeployModeConfirmed`. Plan-archive sweep would catch this when archiving (see C7).

Same drift exists in 3 test-file comments: `internal/tools/workflow_phase5_test.go:249`, `internal/workflow/bootstrap_outputs_test.go:424,1083`, plus `internal/workflow/router_test.go:226` mentions deleted `migrateOldMeta`.

**Fix size**: trivial — comment-only sweep.

#### C9 — Recipes have ZERO `git-push` scaffolding

**Source**: R12 from deploy-strategy-decomposition plan (DEFERRED — confirmed still empty). Grep for `git-push`/`GitPushState`/`setup-git-push` across `internal/recipe/` returns nothing.

**Failure mode**: user asks for a recipe-bootstrap of e.g. `laravel-minimal`, completes bootstrap, switches to `closeMode=git-push`, then has to figure out the git-push setup from scratch — recipe didn't pre-stage a remote URL placeholder, didn't mention `setup-git-push-{container,local}` atom, didn't add the `zerops_workflow action="git-push-setup"` walkthrough into recipe README.

**Fix size**: medium — add an optional `git-push-setup` post-bootstrap section to the recipe corpus generator (or document this as "agents follow the develop-strategy-review prompt naturally"). Same scope as the original deferred R12 ticket.

---

---

## Update — commit `41429da1` + live-agent feedback (2026-04-29 evening)

A live-agent test session completed in parallel with this audit. The user shipped commit `41429da1` "fix(workflows): build-integration handoff + git-push-setup call order" addressing the agent's first three friction points. Verified clean below; remaining feedback items folded into this report as F3-F8.

### Resolved by commit 41429da1 (verified clean)

| Feedback | Fix landed | Evidence |
|---|---|---|
| **F1** — git-push-setup walkthrough order misleading ("first deploy then confirm" — reality: deploy refuses with PREREQUISITE_MISSING until configured stamped) | `setup-git-push-{container,local}.md` reordered: GIT_TOKEN → action="git-push-setup" stamps state → commit + zerops_deploy. New atoms have ZERO retired-vocab leak (verified). | `setup-git-push-container.md` step 2 lead: *"Stamp git-push capability BEFORE the first push"*; explicit `PREREQUISITE_MISSING` mention |
| **F2** — Templating bug: *"set up from X instead of X"* on standalone ModeDev | `topology.PushSourceResult` enum (4 variants: OK / IsStageHalf / ModeUnsupported / UnknownHost) replaces boolean `IsPushSourceFor`; reason-specific remediation. | `predicates.go::PushSourceResult` + test `TestHandleGitPushSetup_StandaloneModeDevSurfacesModeUnsupported` (PASS) |
| **F4** — `build-integration=actions` only stamped flag, didn't deliver workflow YAML or secret guidance | `actionsConfirmResponse` returns full workflow YAML, prefilled `gh secret set` commands (env-aware: container reads `$ZCP_API_KEY`, local extracts via `jq` from `.mcp.json`), explicit `ZEROPS_TOKEN=ZCP_API_KEY` reuse hint, per-repo fine-grained PAT recommendation. | `workflow_build_integration.go::actionsConfirmResponse` + `actionsWorkflowYAML` + `ghSecretSetCommand` + tests (PASS) |
| Bonus: backlog management | New `plans/backlog/` folder + README documenting the deferred-ideas workflow. First entry `auto-wire-github-actions-secret.md` parks the GitHub-API zero-touch secret-creation Codex recommended. | `plans/backlog/README.md` |

All new atoms (`setup-git-push-{container,local}.md`, `setup-build-integration-actions.md`) have ZERO retired-vocab leak — confirmed via `grep -E 'action="strategy"|push-dev|push-git|StrategyConfirmed|DeployStrategy'` returning empty. Matrix simulator passes after lint sweep on `lifecycle_matrix_test.go`.

### NEW findings from live feedback (not in original audit)

| ID | Severity | Title |
|---|---|---|
| **F3** | MEDIUM | `zeropsYamlSetupNotFound` error has no recovery hint listing available setups (Codex root cause: missing submitter-side enrichment layer) |
| **F5** | LOW-MED | `workSessionNote: "No active develop session — deploy not tracked"` is identical for "session never opened" and "session auto-closed". Agent doesn't realise auto-close happened. (Codex: collapsed branch in `sessionAnnotations`; data IS persisted) |
| **F6** | RESOLVED | `## Gotchas` section added to `internal/knowledge/recipes/dotnet-hello-world.md` covering Razor `.cshtml` hot-reload (`dotnet watch run` vs runtime-compilation package) |
| **F7** | RESOLVED | Same Gotchas section above also covers EF Core `EnsureCreatedAsync` shared-DB trap (use migrations or `CREATE TABLE IF NOT EXISTS`) |
| **F8** | **HIGH** | Subdomain auto-enable triggers for dev-mode dynamic runtime even when start is `zsc noop` — Codex root cause: predicate is mode-only, ignores HTTP signal that meta-nil path already uses (`GetService().Ports[].HTTPSupport`). Affects every dev-mode dynamic-runtime first deploy across all recipes. |

#### F3 — `zeropsYamlSetupNotFound` recovery hint

Agent renamed setup blocks dev/prod after starting with hostname-matching block; pre-flight rejected `setup="remindersdev"` with `zeropsYamlSetupNotFound { name: ["remindersdev"] }`. The error structure is correct but doesn't include "available setups: dev, prod" — which the validator already knows because it parsed the file.

**Codex root cause** (2026-04-29):
- Platform error reclassified at `internal/platform/zerops_validate.go:115-124` (`reclassifyValidationError`) — only echoes the platform message; doesn't enrich.
- Submitter HAS the YAML at `internal/ops/deploy_validate_api.go:37-45` and parsed setup names available via `internal/ops/deploy_validate.go:224-239` (`SetupNames()`).
- Wire format CAN carry enrichment: `APIMetaItem.Metadata map[string][]string` at `internal/platform/errors.go:78-82`, forwarded to MCP at `internal/tools/errwire.go:136-144`.

**Structural gap**: no submitter-side diagnostic enrichment stage between platform error mapping and MCP wire response; locally-known context is discarded.

**PATCH fix** (small): in `ValidatePreDeployContent`, on `zeropsYamlSetupNotFound`, parse `yamlContent` and append `availableSetups` to `PlatformError.APIMeta`.

**ROOT-CAUSE fix** (medium): centralize the enrichment pattern. Same need exists for env-ref preflight (`deploy_preflight.go:126-159` already gathers live hostnames) and setup preflight (`deploy_preflight.go:60-72` prints available setups). One canonical "enrich platform 4xx with submitter-known choices" layer; every API-error site goes through it.

#### F5 — `workSessionNote` doesn't differentiate "no session" from "auto-closed"

**Codex root cause** (2026-04-29):
- `sessionAnnotations` (`internal/tools/deploy_local.go:191-195`) collapses ALL non-open states (read error, nil, ClosedAt-set) onto the same constant `noActiveSessionWarning` (`deploy_local.go:199`).
- BUT auto-close IS persisted: `ws.ClosedAt` + `CloseReasonAutoComplete` set at `internal/workflow/work_session.go:212-215` and `:277-280`. The data the agent needs IS on disk.
- Dead-PID cleanup eventually deletes the file (`work_session.go:479-505`) — auto-close vs gc-deleted are then indistinguishable, but the auto-close window is observable.

**Structural gap**: deploy/verify responses don't expose lifecycle terminal state, even though the workflow envelope already computes `develop-closed-auto` at `internal/workflow/compute_envelope.go:399-423`.

**PATCH fix** (small): in `sessionAnnotations`, branch on `ws.ClosedAt != ""` and emit a distinct note carrying `ClosedAt` + `CloseReason` (e.g. *"Develop session auto-closed at 2026-04-29T20:14:03Z (reason: auto-complete) — start a new one for this work."*).

**ROOT-CAUSE fix** (small): return a structured `workSessionState` object from deploy/verify responses, reusing the envelope's `develop-closed-auto` state semantics. Agents get the same lifecycle signal as `action="status"` without an extra round-trip.

#### F6 + F7 — Razor hot-reload + EF Core EnsureCreated → ADDRESSED in `dotnet-hello-world.md`

Both findings landed as a `## Gotchas` section in `internal/knowledge/recipes/dotnet-hello-world.md` (between title and §1). User direction: per-recipe gotchas section is NOT a structural requirement (only 4/36 recipes have one today — bun, laravel-minimal, laravel-showcase, nestjs-minimal). Add concrete verified gotchas as they surface; don't fabricate placeholders or enforce a contract that isn't ready.

CLAUDE.md Conventions section now carries the routing rule: **recipe-specific findings go in recipes, not atoms or audits**. Push via `zcp sync push recipes dotnet-hello-world` when ready.

Codex root-cause confirmed: structural gap candidate is "no required gotchas section in recipe authoring contract" (`docs/spec-content-surfaces.md:184-213` governs generated recipe surfaces but knowledge recipe markdown has no such contract). Defer that until coverage demands it.

#### F8 — Subdomain auto-enable fails for `zsc noop` start

After first `zerops_deploy remindersdev` (dev mode dotnet@9): `warnings: ["auto-enable subdomain failed: Service stack is not http or https"]`. Reason: `zsc noop` start = no HTTP listener yet → Zerops L7 router doesn't see a port → auto-enable rejected.

**Codex root cause** (2026-04-29):
- `maybeAutoEnableSubdomain` (`internal/tools/deploy_subdomain.go:63-67`) reads meta then checks `modeEligibleForSubdomain(meta.Mode)` (`:117-129`) — predicate is mode-only.
- Predicate has no way to know whether the deploy left an HTTP process running. `ServiceMeta` has no runtime-class or start-command field (`internal/workflow/service_meta.go:33-54`); `platform.ServiceStack` exposes `Ports`/`SubdomainAccess`, not start command (`internal/platform/types.go:23-37`).
- Crucially: the meta-NIL path at `deploy_subdomain.go:171-176` already uses `GetService().Ports[].HTTPSupport` — the right signal exists, just isn't used on the meta-PRESENT path.

**Structural gap**: no canonical "HTTP route eligibility" predicate combining topology mode, runtime class, YAML `run.ports.httpSupport`, and live process state. O3 (deploy-handler ownership) is incomplete, not wrong: ownership stands, eligibility gate needs to be stronger.

**PATCH fix** (medium): in `maybeAutoEnableSubdomain`, special-case the platform's "Service stack is not http or https" rejection (around `deploy_subdomain.go:77-81`) and downgrade to a deferred-dev-server hint instead of a warning.

**ROOT-CAUSE fix** (medium-large): replace `modeEligibleForSubdomain` with a shared eligibility service that ALSO consults the resolved `zerops.yaml` setup `run.ports[*].httpSupport` and/or `GetService().Ports[].HTTPSupport` (the meta-nil path already does this — generalize it). Severity: HIGH per Codex — affects every dev-mode dynamic-runtime first deploy across all recipes.

#### F9 ↔ H3 — VERY STRONG CONFIRMATION of response-size finding with concrete data

Live feedback: `start workflow=develop` returned **35 KB** of guidance with 4 services in scope. Atomic calls (close-mode, git-push-setup, build-integration) were ≤2 KB — those are FINE. The blow-up is concentrated in `start workflow=develop` and `complete step=*` where atoms multiply per-service.

**Concrete cause** (verified):
- Only **3 of 80 atoms** use `multiService: aggregate` mode (`develop-first-deploy-execute.md`, `develop-first-deploy-promote-stage.md`, `develop-first-deploy-verify.md`).
- **14 high-fire atoms** with `closeDeployModes:` axis render once per matching service. With 4 services × `closeDeployMode=auto`, atoms like `develop-push-dev-deploy-container`, `develop-close-push-dev-dev`, `develop-strategy-awareness` (yes, the broken one!) all fire 4× with byte-different bodies (hostname substitution).
- `Synthesize` (`workflow/synthesize.go`) filters atoms against `env.Services` (full project list), NOT against `env.WorkSession.Services` (the work-session scope). The agent observation matches: "scope=[remindersdev], nepotřebuju atom pro appdev".

**Two-dimensional fix**:

| Lever | Where | Effort |
|---|---|---|
| **A.** Convert 14 per-service-identical atoms to `multiService: aggregate` with `{services-list:...}` directive | Frontmatter edits in `internal/content/atoms/develop-{push-dev,close-push-dev,close-mode,strategy-*}*.md` | Small per atom; high-impact in aggregate |
| **B.** Filter `Synthesize` against `env.WorkSession.Services` when WorkSession is set, instead of `env.Services` | `workflow/synthesize.go::Synthesize` per-service axis matching loop (line 65); add scope filter helper | Small engine change; covers feedback's concrete ask |

A alone collapses ~14 atoms × 4 services = 56 renders → 14 (single render with services-list expansion). B narrows the multiplication base from "all project services" to "work-session scope" — typically 1-2 services. Combined, the 35KB scenario could drop to <10KB without losing any guidance.

Promote H3 from queue to **Round 2** based on the concrete impact data.

---

## Drift findings summary

| ID | Severity | Title | Touch |
|---|---|---|---|
| C1 | CRITICAL | Stale `action="strategy"` + push-dev/push-git titles | ~25 files |
| C2 | HIGH | git-push stamps Deployed=true before async build lands | structural |
| C3 | HIGH | Atoms reference `failureClassification` field that doesn't exist on TimelineEvent | medium |
| C10 | HIGH | Spec D2b guard not implemented in handleGitPush | small |
| C4 | MEDIUM | `record-deploy` atom unreachable when needed (catch-22 axes) | small |
| C5 | MEDIUM | Asset-pipeline atom uses raw `nohup ... &` over SSH (forbidden) | small |
| C6 | MEDIUM | `develop-platform-rules-local` mixes current+retired `strategy` vocab | small |
| C7 | LOW | `develop-flow-enhancements.md` Phase 3 reverted, plan still says ToDo | trivial (mv) |
| C8 | LOW | Plans + 3 test comments still use retired field names | trivial |
| C9 | LOW | Recipes have no git-push scaffolding (R12 deferred, still open) | medium |
| C11 | LOW | `deploy_strategy_gate.go:22` accepts retired `"push-dev"` value as backward-compat shim | small |

---

## Second-pass verification (2026-04-29 evening)

Per the user's "verify before claim" memory, every finding above was re-read against current disk state (`nl -ba <file> | sed -n 'A,Bp'` for exact line evidence). Independent Codex verification pass run in parallel.

| Finding | Status | Evidence (current file:line) |
|---|---|---|
| C1 | **VERIFIED** | `develop-strategy-awareness.md:23` literal `zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}`. Handler `workflow.go:311-315` lists valid actions explicitly excluding `strategy`. `bootstrap-recipe-close.md:25` same pattern. Push-dev family titles unchanged (8 atoms verified by direct title grep). |
| C2 | **VERIFIED** | Chain confirmed: `tools/deploy_git_push.go:300` `attempt.SucceededAt = time.Now()` runs synchronously after git push command return; `RecordDeployAttempt` (`work_session.go:220`) calls `stampFirstDeployedAt` when `SucceededAt != ""`; `compute_envelope.go:260` `DeriveDeployed` returns true when `meta.IsDeployed()`. No async-build observation between push and stamp. |
| C3 | **VERIFIED w/ refinement** | `TimelineEvent` (`internal/ops/events.go:29-41`) fields: Timestamp, Type, Action, Status, Service, Detail, Duration, User, ProcessID, **FailReason**, **Hint**. NO `failureClass`. The `DeployFailureClassification` type DOES exist (`topology/failure_class.go:55`) and is wired to sync deploy responses (`tools/deploy_poll.go:115`, `tools/deploy_local.go:148`, `tools/deploy_git_push.go:284`) but NOT into `TimelineEvent`. Hint at `events.go:94` plus `develop-build-observe.md:24` reference the missing field. Refinement: classifier exists; only the async/event path is unwired. |
| C4 | **VERIFIED** | `develop-record-external-deploy.md:7` `deployStates: [deployed]`; body line 9-11 says service "stays at deployState=never-deployed" without this. Atom can't fire when most needed. |
| C5 | **VERIFIED** | `develop-first-deploy-asset-pipeline-container.md:38` literal `'cd /var/www && nohup npm run dev > /tmp/vite.log 2>&1 &'` — instructive, not callout. 3 sibling atoms (`develop-platform-rules-container.md:21`, `develop-dynamic-runtime-start-container.md:37`, `develop-dev-server-reason-codes.md:20`) explicitly call this pattern an anti-pattern; this atom does it. |
| C6 | **VERIFIED** | `develop-platform-rules-local.md:31` mixes `strategy=git-push` (current valid `zerops_deploy` arg) and `strategy=push-dev` (retired vocab) in one row; line 32 says "Default push-dev needs no git state". |
| C7 | **VERIFIED** | `internal/tools/workflow_close_test.go:1-13` header doc says guard was added then removed ("created friction on legitimate pivots"). `plans/develop-flow-enhancements.md` still in active dir with Phase 3 listed as ToDo. `handleWorkSessionClose` (`workflow.go:873-892`) has zero `HasSuccessfulDeploy` / `Force` reads — guard truly absent in current code. |
| C8 | **VERIFIED** | `bootstrap_outputs_test.go:1083` literal: "BootstrappedAt, DeployStrategy, StrategyConfirmed, FirstDeployedAt — must survive". Plus `workflow_phase5_test.go:249`, `bootstrap_outputs_test.go:424`, `router_test.go:226` referencing deleted `migrateOldMeta`. |
| C9 | **VERIFIED** | `grep -rn 'git-push\|GitPushState\|setup-git-push\|configureGit' internal/recipe/` returns zero hits in non-test code. R12 deferred from deploy-strategy-decomposition; still empty. |
| C10 | **VERIFIED** | Spec D2b text confirmed in `docs/spec-workflows.md`. `tools/deploy_git_push.go` only checks `meta.IsPushSourceFor(targetService)` (line 68) — pair-key validation, not first-deploy gate. `deploy_strategy_gate.go` validates strategy parameter values only. No `FirstDeployedAt` read in either git-push handler file. |
| C11 (new) | **VERIFIED** | `deploy_strategy_gate.go:22` `case "", deployStrategyGitPush, "push-dev": return nil` — accepts retired `"push-dev"` as alias. Backward-compat shim of exactly the kind CLAUDE.local.md forbids. Likely added because atoms still emit it (C1/C6); proper fix is sweep atoms + delete the case. |
| H1 | **VERIFIED** | `deployActionFor` (`build_plan.go:242-249`) emits `Args: map[string]string{"targetService": host}` — no `sourceService`. `planDevelopActive` (`build_plan.go:81-92`) iterates pending hosts and calls `deployActionFor` regardless of Mode. Stage half pending → self-deploy of stage suggested. |
| H2 | **VERIFIED** | `develop-first-deploy-promote-stage.md:21` `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"` (no setup) vs `develop-close-push-dev-standard.md:22` `... setup="prod"`. Confirmed asymmetry. |
| H3 | **VERIFIED** | `tools/workflow_develop.go:214-226` chain: `Synthesize` → `BuildPlan` → `RenderStatus` → `textResult`. Zero size-check, zero overflow-envelope branch. Matrix sim shows 32KB for multi-service, 28KB for export-active. |
| M1 | **VERIFIED** | `tools/workflow_bootstrap.go:204-206` `fmt.Fprintf(os.Stderr, ...)` — log only, no error propagation. `ops/deploy_ssh.go:199-202` `(test -d .git \|\| (git init && git config ...))` — config only runs when `.git/` MISSING. Existing wrong-identity `.git/` persists. |
| M2 | **VERIFIED** | `ops/deploy_validate.go:110+116+121` — all per-role shape warnings (zsc-noop on stage, healthCheck on dev, readinessCheck on dev). No "stage entry exists before dev verified" sequencing check. |
| M3 | **VERIFIED** | `develop-close-push-dev-local.md:6` modes `[dev, stage]`; `develop-dynamic-runtime-start-local.md:7` modes `[dev, standard]`; `develop-ready-to-deploy.md:5` modes `[dev, simple, standard]`. None list `local-stage`. `local-stage` IS subdomain-eligible (`tools/deploy_subdomain.go:117`). |
| M4 | **VERIFIED** | `workflow/synthesize.go:118-122` aggregate-mode rendering uses `globalHost`/`globalStage` for placeholder substitution OUTSIDE the `{services-list:...}` directive. No corpus-load assertion that aggregate atoms have no bare `{hostname}` outside the directive. |

**Total: 17/17 findings VERIFIED. Zero REFUTED.**

The audit table holds as-written. The single addition during verification is **C11** (legacy `"push-dev"` accepted by the strategy gate) — a backward-compat shim that should be deleted as part of the C1 sweep, since the only reason it exists is to swallow the legacy vocab the C1 atoms still emit.

Codex independent verification pass run in parallel against the same finding list — folded into this table where it offered sharper file:line citations or refinements. No claim was REFUTED by either pass.

---

## Recommended runtime × recipe coverage matrix for internal testing

The 72 recipes form a 5-class × 3-mode × 2-env matrix; testing every cell is wasteful. These **9 scenarios** cover the cross-product of all interesting axes with one recipe each:

| # | Recipe slug | Runtime class | Mode | Env | Why |
|---|---|---|---|---|---|
| 1 | `bun-hello-world` | dynamic | simple | container | Newest dynamic runtime — exercise version detection |
| 2 | `nodejs-hello-world` | dynamic | simple | local | Most-common runtime in local env |
| 3 | `nextjs-ssr-hello-world` | dynamic (SSR) | simple | container | Asset-pipeline atoms (`develop-first-deploy-asset-pipeline-container`) |
| 4 | `vue-static-hello-world` | static | simple | container | Static-runtime path, `dist/` deployFiles |
| 5 | `php-hello-world` | implicit-webserver | simple | container | PHP — no `start:`, runtime-specific atoms |
| 6 | `laravel-minimal` | implicit-webserver + managed | standard | container | Multi-service, PHP+DB, dev/stage pair, env-var resolution |
| 7 | `nestjs-minimal` | dynamic + managed | standard | container | Multi-service Node + DB, cross-deploy |
| 8 | `vue-static-hello-world` | static | simple | local | Local-env static (no SSH) |
| 9 | `nodejs-hello-world` | dynamic | dev | container | `zsc noop` start + `zerops_dev_server` lifecycle |

This pulls every code path the matrix simulator exercises in synthetic mode into a real provision flow. After fixing the C1/H-tier bugs above, run these 9 against `eval-zcp` (per `CLAUDE.local.md`) before declaring a green internal-testing baseline.

---

## How to re-run the simulator

```sh
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
# → internal/workflow/testdata/lifecycle-matrix.md (45 scenarios, current 23 anomalies)
```

After every atom edit or BuildPlan change, re-run and diff `lifecycle-matrix.md` against the prior commit. Anomalies are tagged FATAL / ERROR / WARN at the bottom of the file.

To extend coverage, add a `matrixScenario` to the appropriate `*Scenarios()` builder in `lifecycle_matrix_test.go`. The harness will pick it up automatically.

---

## Suggested fix order

Group by "blocks first-day testing" vs "shows up in week 1" vs "queue".

### Round 1 — atomic vocabulary sweep (must land before any internal testing)
1. **C1** — full sweep: ~25 files. Atoms (rename file + body), atom file renames (`develop-(close-)?push-dev-*` → `develop-close-mode-auto-*`), body rewrites, eval scenarios + instruction variants, test `MustContain` updates, spec-workflows.md sweep. ONE atomic commit. Re-run matrix simulator to confirm zero "legacy strategy vocab" warnings.
2. **C8** — comment-only sweep in 3 test files + 1 router_test comment. Bundle with C1 commit.

### Round 2 — close fast-followups (land before first end-to-end recipe test)
3. **H2** — add `setup="prod"` to `develop-first-deploy-promote-stage.md`. Trivial.
4. **C10** — add `FirstDeployedAt` guard in `handleGitPush`. Pin with test.
5. **C4** — fix `record-deploy` atom axes (catch-22).
6. **C5** — rewrite asset-pipeline atom Vite-start block to use `zerops_dev_server`.
7. **C6** — fix `develop-platform-rules-local` mixed vocab.
8. **C7** — `git mv plans/develop-flow-enhancements.md plans/archive/` + status note. Triggers `make lint-local` plan-archival check if any.

### Round 3 — structural fixes (queue after first test session)
9. **H1** — `planDevelopActive` cross-deploy plan (need to thread `sourceService` through `deployActionFor`).
10. **H3** — status-response token cap (port dispatch-brief overflow envelope).
11. **C2** — gate `git-push` `Deployed` stamp on async build event. STRUCTURAL — needs build-completion observer. Possibly merge with C9 since both touch the git-push lifecycle.
12. **C3** — propagate `FailureClass` into `TimelineEvent` (or rewrite atom to read `failReason`).
13. **M3** — add `local-stage` to excluding atoms.
14. **L1, L2, L3** — small UX atom edits, batch.

### Round 4 — deferred / discovery-driven
15. **C9 / R12** — recipe git-push scaffolding. Open since deploy-strategy-decomposition; needs its own plan.
16. **M1, M2, M4** — structural defenses (silent identity, stage-timing validation, aggregate-atom placeholder lint). Queue for a focused pass after testing reveals which one bites first.

After Round 1+2 land, the user can start internal testing with confidence the agent isn't being told to call a non-existent action, the deploy hand-off is consistent, and the failure classifier guidance matches the actual data shape.
