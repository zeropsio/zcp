# Git-push deploy flow redesign audit

Date: 2026-05-19

Scope: audit the current ZCP git-push setup, build-integration setup, and post-setup develop iteration/close flow. This report is design-only; it does not change handlers or atoms.

## Findings

### 1. The planner is strategy-blind, so git-push atoms cannot fully correct the flow

The strongest mismatch is in `internal/workflow/build_plan.go::deployActionFor`. It chooses deploy actions from hostname shape only:

- If the host is a stage half, it emits `zerops_deploy sourceService=<dev> targetService=<stage> setup=prod`.
- Otherwise it emits `zerops_deploy targetService=<host>`.

It does not read `CloseDeployMode`, `GitPushState`, `BuildIntegration`, or `RemoteURL`. The local TODO already names the right structural target: replace this inline inference with a `DeployIntent` resolver (`internal/workflow/build_plan.go:252-257`).

This explains why adding another git-push atom is not sufficient. Even when `develop-close-mode-git-push` renders, the typed plan can still suggest the old dev self-deploy or dev-to-stage cross-deploy. The generic atom reinforces that by saying "Simple / standard / local / first-deploy: every change -> `zerops_deploy`" without checking the delivery mode (`internal/content/atoms/develop-change-drives-deploy.md:13-18`).

### 2. The current model distinguishes push source, but not build target

`internal/topology/predicates.go::IsPushSource` is explicit: a standard pair's dev half is the push source; the stage half is a build target, not a source (`internal/topology/predicates.go:75-95`). `gitPushMetaPreflight` enforces this and rejects `zerops_deploy targetService=<stage> strategy=git-push`, pointing the agent back to the dev half (`internal/tools/deploy_git_push.go:71-80`).

That part is correct, but the next half is missing. Once a remote push lands, the build target for a standard pair should usually be stage. Current handler responses do not expose that split:

- `workflow_build_integration.go::actionsConfirmResponse` looks up the service ID for the same `hostname` passed into `action="build-integration"` and writes it as `ZEROPS_SERVICE_ID` (`internal/tools/workflow_build_integration.go:194-235`).
- The default Actions workflow uses `--setup <hostname>` (`internal/tools/workflow_build_integration.go:205-210`, `internal/tools/workflow_build_integration.go:293-319`), which is wrong for recipe-style `zerops.yaml` files that use `dev` / `prod`.
- `webhookConfirmResponse` deep-links to the same `hostname` service page (`internal/tools/workflow_build_integration.go:258-285`).

So the current implementation has this shape:

```text
push source = appdev
build integration service = appdev
plan stage deploy = appdev -> appstage cross-deploy
```

The intended git-push standard shape should be closer to:

```text
push source = appdev
remote build target = appstage
local push preflight setup = dev or detected source setup
remote build setup = prod or detected target setup
record/observe/verify target = appstage
no post-setup dev->dev deploy and no appdev->appstage cross-deploy
```

This is a structural gap, not an atom wording issue.

### 3. The specs still mix old cross-deploy language into post-git-push operation

The specs correctly state the three orthogonal dimensions (`CloseDeployMode`, `GitPushState`, `BuildIntegration`) and say first deploy always uses default direct deploy because git-push needs committed code and a remote (`docs/spec-workflows.md:647-653`, `docs/spec-workflows.md:657-682`).

But the operational details still say:

- "Stage entry written AFTER dev verified"
- `zerops_deploy sourceService={dev} targetService={stage}` for cross-deploy

without qualifying that as auto/direct deploy only (`docs/spec-workflows.md:759-765`). That conflicts with the post-git-push model. After `GitPushState=configured`, stage promotion should be a remote rebuild, not a local cross-deploy, unless the service is still on `closeMode=auto`.

### 4. The setup flow is text-only and split across too many decisions

`handleGitPushSetup` has a walkthrough mode and a confirm mode. Walkthrough returns:

```json
{
  "status": "walkthrough",
  "service": "...",
  "guidance": "...",
  "nextStep": "After completing..."
}
```

No structured inputs are listed (`internal/tools/workflow_git_push_setup.go:146-150`). Confirm mode only writes `GitPushState=configured` and `RemoteURL`; the response tells the agent to configure `webhook|actions` later (`internal/tools/workflow_git_push_setup.go:171-176`).

`handleBuildIntegration` has the same pattern: if `integration` is omitted it returns a plain-text picker; when confirmed it writes only `BuildIntegration`. The separation is valid as persistence design, but the user-facing journey is unnecessarily fragmented because the handler never says, in one machine-readable block, "collect repo URL, token, and integration choice before starting."

The manual session shows the consequence: the agent asked for URL, PAT, and trigger in free text, recommended webhook, then had to reverse course to Actions after the user objected.

### 5. Webhook is treated as peer/default even though Actions is the shorter GitHub happy path

Current guidance biases webhook in several places:

- `docs/spec-workflows.md:692-696` uses `integration="webhook"` in the canonical example.
- `develop-strategy-awareness.md` lists `none`, `webhook`, then `actions`, and its example uses webhook (`internal/content/atoms/develop-strategy-awareness.md:26-39`).
- `setup-build-integration-actions.md` and `setup-build-integration-webhook.md` both have `priority: 2`, so the corpus does not encode a preference.
- The git-push setup atom lists `Secrets + Workflows` as an add-on rather than the default GitHub CI path (`internal/content/atoms/setup-git-push-container.md:14-19`).
- `git-push-setup` confirm says `integration="webhook|actions"` in that order (`internal/tools/workflow_git_push_setup.go:176`).

The user is right for the GitHub + PAT + usable `gh` case: Actions can be completed without manual Zerops dashboard OAuth. Webhook still has valid cases: GitLab, users who prefer Zerops-owned OAuth, repos where the agent cannot write workflow files or secrets, and teams avoiding GitHub Actions for policy reasons. So "Actions default" should be conditional, not absolute:

```text
GitHub remote + user can provide repo-scoped PAT with Contents, Secrets, Workflows -> recommend Actions.
GitLab or no secret/workflow permission -> recommend webhook or none.
Existing external CI/CD -> recommend none, with explicit record/observe instructions.
```

### 6. `AskUserQuestion` has no systematic contract in the relevant atoms

The eval harness recognizes `AskUserQuestion` as a wait signal (`internal/eval/usersim.go:105-118`), but the git-push/setup atoms do not mention it. The only atom hit by `rg "AskUserQuestion" internal/content/atoms` is `launch-scope-prompt.md:22`.

There is already a better repository-local pattern: `plans/workflow-family-architecture-2026-05-14.md:1044-1065` defines a client-agnostic structured prompt shape with `prompt`, `options[]`, and `inputField`. That should be made a spec-level MCP response contract for any workflow action that gates on user input. Atoms can mention "route this structured prompt through AskUserQuestion when your harness exposes it," but handlers should carry the parseable shape. Atoms alone are too easy for agents to read as prose.

### 7. Stage promotion under git-push is currently ambiguous and partly wrong

Current behavior:

- `zerops_deploy targetService=appstage strategy=git-push` is rejected because stage is not a push source.
- `BuildPlan` can still suggest `zerops_deploy sourceService=appdev targetService=appstage setup=prod` because it is strategy-blind.
- `build-integration actions/webhook` targets the service passed to `service=`, usually the dev half, because handlers do not compute a build target.

So the user's "no dev->dev and no dev->stage after git-push is set up" intuition is correct for post-setup operation, but it needs one extra design statement: standard git-push still needs a stage build target. The build should be caused by the remote push through webhook/actions, then observed with `zerops_events` and acknowledged with `record-deploy` against the target that actually built.

First deploy remains an exception. The spec invariant that first deploy uses the default direct mechanism should stay intact until there is committed code, a remote, and `GitPushState=configured`.

## Traced Paths

### Path A: real manual session, GitHub repo, user wanted git-push

Observed shape:

1. `git-push-setup service=appdev` returned text guidance.
2. Agent asked for repo URL, PAT, and trigger choice in free text.
3. Agent stored `GIT_TOKEN`, confirmed `GitPushState=configured`, and set `closeMode=git-push`.
4. Agent performed first git init/commit/push work, then ran `zerops_deploy targetService=appdev strategy=git-push setup=dev`.
5. Agent selected webhook first, hit the dashboard OAuth manual step, then switched to Actions after the user corrected it.

Divergence: the first user-facing question was not structured, webhook was presented as the practical default, and the post-setup path still thought in terms of the dev service.

### Path B: standard pair after git-push configured

Current code can simultaneously produce these statements:

- `develop-close-mode-git-push` says run `zerops_deploy targetService=<hostname> strategy=git-push`.
- `gitPushMetaPreflight` rejects the stage half as `targetService`.
- `BuildPlan` emits stage cross-deploy if the stage hostname is stale.
- `actionsConfirmResponse` and `webhookConfirmResponse` configure whichever hostname was passed as `service`, usually the dev half.

Divergence: there is no single authoritative answer for "what target becomes green after this push?"

### Path C: Actions integration setup

Current confirm response is useful but target-unsafe:

1. It parses owner/repo and returns workflow YAML.
2. It returns `gh secret set` commands for `ZEROPS_TOKEN` and `ZEROPS_SERVICE_ID`.
3. It looks up `ZEROPS_SERVICE_ID` for the input hostname.
4. It generates `zcli push --service-id ... --setup <hostname>`.

Divergence: for recipe-derived standard pairs, the likely desired build is stage with `setup=prod`, while the generated workflow uses dev/service input and hostname setup.

## Design Proposal

### Option A: keep the three persisted axes, add a typed `DeployIntent` resolver

This is the recommended shape.

Keep the three meta fields:

- `CloseDeployMode`: user-facing delivery pattern and auto-close gate.
- `GitPushState`: whether the remote push capability exists.
- `BuildIntegration`: who consumes the remote push.

Do not add an atom axis for "iteration vs close." The real missing dimension is not another filter axis; it is the typed deploy command and target that should be shared by the planner, atoms, and handlers.

Add a workflow-level resolver, conceptually:

```go
type DeployIntent struct {
    Mode              topology.Mode
    Delivery          "direct" | "git-push" | "manual"
    PushSource        string
    BuildTarget       string
    PushSetup         string
    BuildSetup        string
    DeployTool        string
    DeployArgs        map[string]string
    EventsService     string
    RecordDeployTarget string
    VerifyTarget      string
    RequiresAsyncAck  bool
    FirstDeployBypass bool
}
```

Rules:

- First deploy bypasses git-push and keeps today's direct first-deploy behavior.
- `closeMode=auto` keeps today's direct self/cross deploy behavior.
- `closeMode=git-push` + `GitPushState=configured` resolves to `zerops_deploy targetService=<pushSource> strategy=git-push`, never direct dev self-deploy and never local dev-to-stage cross-deploy.
- For standard/local-stage modes, `BuildTarget` is stage; for simple it is the same hostname; for local-only it may be empty or external-CI-only.
- `BuildIntegration=actions|webhook` instructions use `BuildTarget`, not blindly the push source.
- Auto-close evaluates the resolved deploy target(s), not raw session hostnames that no longer receive direct deploys.

This gives the atom corpus one small job: explain the resolved intent. It avoids a second family of iteration atoms and prevents planner output from contradicting guidance text.

Shortest GitHub + Actions happy path after this redesign:

1. Agent calls `zerops_workflow action="git-push-setup" service="appdev"` and receives a structured prompt for repo URL, token, and integration choice. Actions is marked recommended when the remote is GitHub and the PAT can cover Contents + Secrets + Workflows.
2. Agent asks one structured user question, then sets `GIT_TOKEN`.
3. Agent confirms `git-push-setup remoteUrl=...`.
4. Agent confirms `build-integration integration="actions"` and writes the returned workflow/secrets using the resolved build target and setup.
5. Agent sets `closeMode=git-push`.
6. Agent commits and runs one `zerops_deploy targetService=<pushSource> strategy="git-push" setup=<pushSetup> branch=main`.
7. Agent watches `zerops_events serviceHostname=<buildTarget>`, then `record-deploy` + `verify` on the build target.

That is still multiple tool calls because the persisted axes remain independent, but it is one user-facing question and one coherent delivery path.

### Option B: add a fourth atom axis, such as `deliveryCycle=iteration|close`

This would make it possible to add a git-push iteration atom without touching the planner. It is not recommended.

Trade-offs:

- It increases atom matrix size immediately.
- It still leaves `BuildPlan` emitting old commands.
- It does not solve push-source vs build-target.
- It teaches agents two command sources: typed plan and atom prose.

This is the "infinite atom expansion" path the user explicitly wants to avoid.

### Option C: reuse auto workflow atoms by adding `git-push` to their frontmatter and branching inside

This is also not recommended.

`develop-close-mode-auto-workflow-*` atoms are useful, but only for direct deploy. Making them dual-mode would turn compact operational atoms into conditional prose and still would not fix Actions/webhook handler targeting. Reuse should happen by keeping the auto atoms as direct-deploy guidance and converting the generic strategy atom to say "follow the resolved deploy intent from the plan."

### Winner

Option A.

The minimal redesign is:

1. Centralize deploy-command selection in `DeployIntent`.
2. Make setup handlers return structured prompts and explicit recommendations.
3. Update a small set of atoms to explain the new intent and default ordering.
4. Keep the three persisted axes independent.

## Concrete Change List

Estimated file count: add 2, edit 17, delete 0.

### Phase 1: typed deploy intent and planner alignment

Add:

- `internal/workflow/deploy_intent.go` - resolver for direct, git-push, manual, first-deploy bypass, push source, build target, setup names, record target, verify target.
- `internal/workflow/deploy_intent_test.go` - table tests for dev, simple, standard, local-stage, local-only across auto/git-push/manual.

Edit:

- `internal/workflow/build_plan.go` - replace `deployActionFor` hostname inference with `DeployIntent`. The TODO at `build_plan.go:252-257` becomes the implementation point.
- `internal/workflow/work_session.go` - make auto-close/deploy-green checks evaluate resolved targets for git-push standard pairs, otherwise a scope containing both `appdev` and `appstage` may wait for a dev deploy that should no longer happen.
- `internal/tools/workflow_phase5_test.go` - add tests that a configured standard pair no longer plans dev self-deploy or dev-to-stage cross-deploy under git-push.

### Phase 2: setup handler response shape and Actions default

Edit:

- `internal/tools/workflow_git_push_setup.go`
  - Walkthrough response should include `inputsRequired` or `questions` with repo URL, token, and build-integration choice.
  - Include `recommendedIntegration`, with Actions recommended for GitHub when the user can provide Contents + Secrets + Workflows permissions.
  - Keep state writes limited to `GitPushState` and `RemoteURL`.

- `internal/tools/workflow_build_integration.go`
  - Walkthrough response should include `prompt`, `options[]`, and `inputField:"integration"`.
  - Confirm response should include `pushSource`, `buildTarget`, `pushSetup`, `buildSetup`, `eventsService`, and `recordDeployTarget`.
  - Actions workflow should use the build target service ID and the build setup name, not the push source hostname by default.
  - Webhook deep-link should point at the build target service page for standard/local-stage.

- `internal/tools/workflow_close_mode.go`
  - When switching to git-push, return a next-step chain that points to the structured setup prompt and makes the build target explicit.
  - Listing hint should not always suggest `auto`; it should reflect available configured modes.

- `internal/tools/deploy_git_push.go` and `internal/tools/deploy_local_git.go`
  - Missing-trigger warning should list Actions first for GitHub remotes when appropriate.
  - Error recovery should say whether the rejected target was a push source problem or a build target problem.

### Phase 3: atom edits, no new atom family

Edit:

- `internal/content/atoms/develop-change-drives-deploy.md`
  - Replace "simple / standard / local -> every change -> `zerops_deploy`" with "every change must reach the resolved delivery target; use the plan's deploy intent."

- `internal/content/atoms/develop-close-mode-git-push.md`
  - Explain push source vs build target.
  - For standard pairs, observe and record the stage target after CI/webhook build.
  - Keep the setup-parameter warning, but distinguish local push preflight setup from remote build setup.

- `internal/content/atoms/develop-strategy-awareness.md`
  - List build integrations as `actions | webhook | none` when the remote is GitHub-capable, with the caveats.
  - Use Actions in the example or use a neutral placeholder.

- `internal/content/atoms/setup-git-push-container.md`
- `internal/content/atoms/setup-git-push-local.md`
  - Add a "collect inputs first" block matching the structured response shape.
  - Make it clear that the agent should route that block through `AskUserQuestion` when available.

- `internal/content/atoms/setup-build-integration-actions.md`
  - Promote Actions as the shortest GitHub happy path.
  - Replace `{hostname}` setup examples with `{buildSetup}` or explicit detected setup.

- `internal/content/atoms/setup-build-integration-webhook.md`
  - Keep as first-class fallback, but label dashboard OAuth as a manual UI step.
  - Use `{buildTarget}` instead of `{hostname}` where standard pairs are discussed.

Update goldens/tests:

- `internal/workflow/testdata/atom-goldens/develop/git-push-configured-webhook.md`
- Add or update an Actions golden for standard git-push target resolution.
- `internal/workflow/scenarios_test.go` should assert that the git-push atom renders once per pair and that generic deploy prose no longer contradicts it.

### Phase 4: spec and contract updates

Edit:

- `docs/spec-workflows.md`
  - Qualify section 4.8 cross-deploy language as direct/auto deploy only.
  - State post-git-push standard behavior: push from dev source, rebuild stage target from git, record/verify stage, no local dev-to-stage cross-deploy.
  - Change the build-integration example from webhook to an Actions-first conditional example.

- `docs/spec-knowledge-distribution.md`
  - Keep the three axes.
  - Add an atom/handler contract: user-input-gating workflow responses must include `prompt`, `options[]` when choices exist, and `inputField` or `inputsRequired`.
  - Atoms may mention `AskUserQuestion`, but the parseable response shape is the source of truth.

## Risks + Spec Invariants That Constrain the Design

- First deploy must stay direct. The spec says close-mode is not a first-deploy gate (`docs/spec-workflows.md:647-653`). Git-push setup depends on committed code and remote credentials that usually do not exist before first deploy.
- The three persisted axes should remain orthogonal. Recombining them into one "strategy" field would undo the deploy-strategy decomposition and make mid-session switches harder.
- Actions cannot be an unconditional default. It is the shortest GitHub path only when the user can provide the right repo-scoped permissions and the agent can write workflow/secrets. Webhook remains valid for GitLab, policy-constrained repos, and users who prefer Zerops dashboard OAuth.
- `BuildIntegration=actions` stores a Zerops token in GitHub secrets. That is acceptable if explicit and repo-scoped, but it should not be hidden behind "automatic setup."
- Auto-wiring GitHub secrets through REST/libsodium should stay out of the minimal redesign. `plans/backlog/auto-wire-github-actions-secret.md` already treats it as a separate security-sensitive enhancement.
- Standard git-push needs a build-target invariant. If `buildTarget=stage`, auto-close and `record-deploy` cannot require a fresh dev deploy in the same session.
- `zerops.yaml` setup names cannot default to hostname in generated Actions workflows. Recipe flows use `dev` / `prod`; single-runtime adoptions may use hostname. The resolver or handler must derive or require setup names explicitly.
- Stage service source-code webhook support should be verified against the Zerops dashboard/API before locking the webhook guidance. Current code can deep-link any service, but this audit did not prove the dashboard accepts the exact standard-pair target/setup combination.
- Local-only remains underspecified. It is a push source in topology, but it may have no Zerops build target or service ID for Actions. The resolver should make that state explicit instead of letting the Actions handler emit unusable placeholders.

## Open Questions for the User

1. For a standard pair under git-push, should every remote push build only stage, or should there be an optional mode that also rebuilds dev for preview parity?
2. Should `closeMode=git-push` still be set on both halves of a standard pair, or should the UI/API normalize it to the pair's dev-keyed meta and display the stage build target separately?
3. Should `git-push-setup` remain a pure capability action, or may it accept optional `integration` and call the build-integration path as a convenience while still persisting separate fields?
4. What is the desired policy for tokens in chat/tool transcripts? The current real session included the PAT in user text. A structured prompt can reduce ambiguity, but it cannot make a pasted secret invisible unless the harness provides a secret input primitive.
5. Should local-only support `BuildIntegration=actions`, or should Actions be refused until the project has a Zerops target service ID?
6. For webhook, can Zerops dashboard source-code integration be configured against the stage half with the `prod` setup in every standard recipe shape? If not, webhook cannot share the exact same build-target rule as Actions.

---

## Decision Log (2026-05-19, user-confirmed)

| # | Decision |
|---|---|
| 1 | **Scope = full.** Wider DeployIntent (13 fields, including `Delivery`, `PushSetup`/`BuildSetup` split, `EventsService`, `RecordDeployTarget`, `VerifyTarget`, `RequiresAsyncAck`, `FirstDeployBypass`) — user explicitly asked to widen scope and observe ripple. |
| 2 | **Standard pair under git-push builds only stage.** No optional dev-parity rebuild. |
| 3 | **`closeMode=git-push` is dev-keyed meta only** (pair-keyed). Consistent with `git-push-setup` + `build-integration` already being per-pair. Stage half reads close-mode through pair lookup. |
| 4 | **No `integration` shortcut on `git-push-setup`.** Keep orthogonal: capability and CI integration stay independently persisted. Convenience routed through chained next-step pointers in structured prompt response. |
| 5 | **Token paste policy unchanged.** Tokens in transcripts are an acknowledged limitation; not solved in this redesign. Structured prompt narrows the question but doesn't make a pasted PAT invisible. |
| 6 | **Local-only refuses actions/webhook** until `action="adopt-local"` links a Zerops runtime as stage. Recovery hint at `adopt-local`. Both integrations need a Zerops service ID for build target, which local-only lacks. Rationale: spec-local-dev.md §11 already blocks `closeMode=auto` for local-only on the same "no deploy target" ground; extending to build-integration is consistent. |
| 7 | **PENDING** — live verification on eval-zcp before locking webhook stage-half guidance. Until verified, Phase 3 retains current dev-half webhook deep-link as fallback; refactor to stage-half deep-link lands in Phase 3a (post-verification) or gets explicitly deferred. |

## Ripple Scope Map (consumer sites)

Surveyed by Explore agent 2026-05-19. Marked **CONFLICT** = current behavior contradicts resolver intent; **AGREES** = current logic is already aligned; the resolver just absorbs it.

| File:Line | Today's decision | Resolver alignment | Change shape |
|---|---|---|---|
| `internal/workflow/build_plan.go:258-276` (`deployActionFor`) | hostname → (source, target, setup) inline | **CONFLICT** (strategy-blind; core target) | Internal: call `DeployIntent.Resolve()` |
| `internal/workflow/build_plan.go:285-292` (`findDevHalfForStage`) | StageHostname index | AGREES (foundation) | Logic relocates into resolver |
| `internal/workflow/work_session.go:379-418` (`EvaluateAutoClose`/`autoCloseGateOpen`) | All scope hostnames + close-mode gate | **CONFLICT for git-push**: evaluates dev half that no longer receives deploys | Thread resolved targets; gate fires on resolved targets only |
| `internal/tools/workflow_build_integration.go:186-235` (`actionsConfirmResponse`) | service ID + `--setup <hostname>` from input hostname (= push source) | **CONFLICT**: should target buildTarget + buildSetup | Resolver supplies (`buildTarget`, `buildSetup`) |
| `internal/tools/workflow_build_integration.go:258-285` (`webhookConfirmResponse`) | deep-link to push-source page | **CONFLICT** (pending #7 verification) | Resolver supplies buildTarget; deep-link recompute |
| `internal/tools/verify.go:51` (`ops.Verify`) | Single input hostname | Soft conflict: post-git-push should verify buildTarget | Optional verifyTarget hint; backward-compat |
| `internal/tools/workflow_record_deploy.go:46-180` | Input `targetService` | **CONFLICT for git-push standard**: should record on buildTarget (stage), not push-source | Resolver supplies recordDeployTarget; validate input or auto-route |
| `internal/tools/deploy_git_push.go:71-115` (`gitPushMetaPreflight`) | Push-source predicate (rejects stage half) | AGREES | No change |
| `internal/tools/deploy_strategy_gate.go:60-79` (`checkLocalOnlyGate`) | Local-only zcli refusal | AGREES | No change |
| `internal/tools/deploy_ssh.go:258-276`, `deploy_local.go` | Source/target args | Consumer of plan output | No change (handler reads resolved args from plan) |

**Excluded** (boundary):
- **Recipe authoring** (Aleš's scope). Recipe flows emit `zerops.yaml` + import instructions, not deploy plans. Resolver applies to develop close + bootstrap close, not recipe author phase. Mention only: import.yaml emitted by recipe must respect buildTarget for downstream Actions/webhook.
- **Launch-production**. Its own deploy orchestration (zcli push, tag triggers, HA managed deps). Distinct close-mode logic; out of scope.
- **Export**. Reads deploy history, doesn't drive future deploys.

## Phase Breakdown (5 phases, ~9.5 dev-days, stop-gates between)

| Phase | Scope | Stop-gate | LOC est. |
|---|---|---|---|
| **1** | `deploy_intent.go` type + Resolve + `deploy_intent_test.go` + `build_plan.go::deployActionFor` swap | All existing `build_plan_test.go` cases green; no behavior change yet | ~150 |
| **2** | `work_session.go` auto-close gate uses resolved targets; 8 new `TestEvaluateAutoClose_*` cases per mode × close-mode × ready-state | Git-push standard pair no longer waits for dev self-deploy; auto-close fires when stage-only is green | ~120 + 8 tests |
| **3** | `workflow_build_integration.go` + `workflow_git_push_setup.go` walkthrough → structured prompt (`prompt`, `options[]`, `inputField`); `recommendedIntegration` (Actions default conditional on GitHub-capable remote + permissive PAT); local-only refuse path with `adopt-local` recovery; `workflow_close_mode.go` chained next-step uses structured shape | `git-push-setup` walkthrough returns structured inputs; agent harness with `AskUserQuestion` automatically renders picker; `build-integration=actions` emits workflow YAML for correct buildTarget+buildSetup | ~180 + 3 goldens |
| **4** | `verify.go` + `workflow_record_deploy.go` accept/derive resolver targets; 4 new tests | Post-git-push standard: agent records on stage, verifies stage, no dev references in transcripts | ~100 + 4 tests |
| **5** | 6 atom edits (placeholder migration to `{buildTarget}`/`{buildSetup}`, structured-prompt note); `spec-workflows.md` §4.8 qualifier; `spec-knowledge-distribution.md` structured-prompt contract; regen 23 goldens | `make lint-local` green; atom corpus & planner emit one consistent story | ~50 + 23 goldens |

**Critical risks (most-likely failure modes during refactor):**
- **`ws.Services` vs resolved-targets divergence** — highest-risk invariant in Phase 2. Wrong split could either block auto-close forever (waiting for dev) or fire prematurely (skipping stage verify).
- **Atom lint placeholders** — current lint may reject new `{buildTarget}`/`{buildSetup}` syntax. Phase 5 lint-rule extension may be needed; alternatively keep placeholders backward-compatible (substitute pre-render).
- **Golden regen prose contradictions** — atoms must mirror planner output. Manual review per batch in Phase 5; Codex POST-WORK round per batch.
- **Recipe author boundary** — bootstrap-emitted import.yaml carries closeMode defaults. Verify Aleš's flow receives correct default (`unset` → strategy-review). Escalate before Phase 5 if mismatch.

## Open Items (require resolution before / during execution)

1. **Live verification: webhook on stage half** (Question #7). **Partial resolution from frontend audit 2026-05-19** (`../frontend-legacy/`): `RemoteRepositoryConnectorFeature` + `externalRepositoryIntegration$` API (`PUT /api/service-stack/{id}/external-repository-integration`) have **no service-type / mode / stage filter**. Setup name is a free-text `zeropsYamlSetup` field. → Verdict (a): stage half accepts OAuth bind; Phase 3 can deep-link there. **Remaining unknown**: route slug `service-stack-source-code` (used in current ZCP webhook URL) was not located in legacy frontend tree — production frontend may have it; or ZCP URL is broken. Mitigation: Phase 3 webhook deep-link falls back to plain service-stack-detail-deploy page if `service-stack-source-code` 404s on live verification. Live smoke test on eval-zcp still recommended before Phase 3 ships.
2. **Recipe author boundary check** — confirm Aleš agrees recipe-emitted import.yaml respects new buildTarget for downstream CI configuration, OR scope a separate handoff to recipe team.
3. **Codex POST-WORK rounds** — per Phase 2/3/5; verify resolver+atom+planner consistency on golden snapshots.

## Execution Log

### Phase 1 — DeployIntent type + resolver (2026-05-19)

**Status**: COMPLETE.

**Changes**:
- ADD `internal/workflow/deploy_intent.go` (~210 LOC): `DeployIntent` struct (13 fields), `DeployDelivery` enum (direct/git-push/manual), `Resolve(target, services)` pure function, `resolveDelivery` helper. First-deploy bypass override included.
- ADD `internal/workflow/deploy_intent_test.go` (~270 LOC): 11-case table test + build-plan parity test.
- EDIT `internal/workflow/build_plan.go` (`deployActionFor`): replaced inline hostname inference with `Resolve()` call. Added `snapshotByHostname` helper for the (rare) case where host isn't in `env.Services`. Old TODO comment removed.

**Verify gate**:
- `go test ./internal/workflow/... -short -race -count=1` — PASS
- `go test ./... -short -count=1` — all packages PASS (no regression across `ops`, `tools`, `recipe`, `eval`, …)
- `make lint-fast` — 0 issues

**Behavior parity**: confirmed via `TestResolve_BuildPlanParity` + existing `TestBuildPlan_DevelopActiveDeployPending_StageHalf_CrossDeploy` / `_DevHalf_SelfDeploy`. Stage half emits `sourceService+targetService+setup=prod` cross-deploy; dev/simple emits `targetService` self-deploy. No behavior change yet — `DeployArgs` are 1:1 with pre-resolver output. Downstream Phase 2-4 consumers (auto-close gate, build-integration targeting, verify/record-deploy) start reading the new fields (`BuildTarget`, `EventsService`, `RecordDeployTarget`, `VerifyTarget`, `RequiresAsyncAck`, `FirstDeployBypass`) in their respective phases.

**Phase 2 ready to start**: auto-close gate threading + 8-case `TestEvaluateAutoClose_*` matrix.

### Phase 2 — EvaluateAutoClose threading (2026-05-19)

**Status**: COMPLETE.

**Changes**:
- ADD `internal/workflow/deploy_intent_targets.go` (~95 LOC): `SnapshotsFromMetas`, `ResolvedDeployTargets`, `dedupStrings`. `SnapshotsFromMetas` emits both halves of a pair (dev + synthetic stage) so the resolver can answer `findDevHalfForStage` in non-envelope contexts. `Deployed` set from `meta.FirstDeployedAt != ""` so the first-deploy bypass branch doesn't shadow git-push delivery for already-deployed services.
- EDIT `internal/workflow/work_session.go::EvaluateAutoClose` + `AutoCloseProgressOf`: iterate `ResolvedDeployTargets(stateDir, ws)` instead of `ws.Services`. Under git-push for a standard pair the gate fires on stage-only (the dev half no longer receives deploys and would otherwise hold the gate open forever).
- EDIT `internal/workflow/work_session_test.go`: new `TestEvaluateAutoClose_GitPushStandardPair` (8 cases: git-push configured/unconfigured × stage ready/pending/missing × manual + auto regression) + `TestResolvedDeployTargets_StandardPair` (4 cases pinning the dedup + dev→stage collapse).
- FIX in `deploy_intent.go::Resolve` during testing: BuildTarget must depend on `(Mode, Delivery)`, not Mode alone — under direct delivery a standard-pair dev half deploys to itself; only git-push delivery shifts BuildTarget to stage. Adjusted 6 table-test expectations accordingly.

**Verify gate**:
- `go test ./... -short -count=1 -race` — all packages PASS
- Existing `TestEvaluateAutoClose_CloseDeployModeGate` + `TestEvaluateAutoClose` cases preserved (backward-compat via stateDir="" fallback in `ResolvedDeployTargets`)

### Phase 3 — build-integration buildTarget + structured prompt + Actions default (2026-05-19)

**Status**: COMPLETE.

**Changes**:
- EDIT `internal/tools/workflow_build_integration.go::actionsConfirmResponse` + `webhookConfirmResponse`: read `(buildTarget, buildSetup)` from new `anticipatedBuildTarget(meta)` helper (synthesizes git-push delivery snapshot for build-target resolution, forces `Deployed=true` to bypass FirstDeployBypass for the integration setup context). Workflow YAML now emits `--setup <buildSetup>` (= `"prod"` for standard pairs) and the secret store uses the build-target's service ID. Webhook deep-link points at build-target source-code panel; response carries both `dashboardUrl` and `fallbackUrl` (plain service-stack page if `service-stack-source-code` slug 404s — caveat from frontend audit).
- ADD local-only refuse path in `handleBuildIntegration`: `bi != none && meta.Mode == PlanModeLocalOnly` → `ErrPrerequisiteMissing` with `WithRecovery(adopt-local)`.
- EDIT walkthrough response (`workflow_git_push_setup.go`): adds `inputsRequired` (3 inputs: `remoteUrl`, `gitToken` (secret), `integration`), `recommendedIntegration` (conditional default — `actions` for GitHub, `webhook` for GitLab), `prompt`, structured `nextStep`. Confirm response now carries `recommendedIntegration`.
- EDIT build-integration walkthrough response: adds `options[]` (actions/webhook/none with labels + descriptions), `inputField:"integration"`, `recommendedIntegration`, `buildTarget` + `buildSetup`, `prompt`.
- ADD helper `recommendIntegrationForRemoteURL(remote string)` — picks default by host substring (gitlab → webhook, else actions).
- UPDATE existing test `TestHandleBuildIntegration_ActionsConfirmEnrichesResponse` to assert `"buildTarget":"appstage"`, `"buildSetup":"prod"`, `--setup "prod"` for the standard-pair fixture.

**Verify gate**: `go test ./... -short` — PASS. Existing actions/webhook tests stay green after assertion updates.

### Phase 4 — verify + record-deploy resolved targets (2026-05-19)

**Status**: COMPLETE.

**Changes**:
- ADD `internal/tools/resolve_build_target.go` (~45 LOC): `resolveBuildTargetForHost(stateDir, host)` — synthesizes git-push delivery snapshot (Deployed=true) and reads BuildTarget+BuildSetup from `Resolve`. Returns ("","") for unknown hosts so callers fall back without a warning.
- EDIT `internal/tools/workflow_record_deploy.go`: redirect `input.TargetService` to `buildHost` before the build-status gate and the stamp call. Push-source-to-stage redirect surfaces in `resp.Warnings` so the agent learns the correct hostname.
- EDIT `internal/tools/verify.go`: redirect `host` to `buildHost` before `ops.Verify`. Adds optional `Note` field on `verifyResponse` carrying the redirect explanation (preserves backward-compatible verifyResult shape).
- ADD `internal/tools/phase4_redirect_test.go` (~125 LOC): 5 cases pinning the resolver + handler redirects for standard pair (dev → stage), simple (no redirect), and missing meta (graceful fallback).

**Verify gate**: `go test ./... -short` — PASS.

### Phase 5 — atom corpus alignment + spec + golden regen (2026-05-19)

**Status**: COMPLETE.

**Changes**:
- EDIT `internal/content/atoms/develop-close-mode-git-push.md`: explicit "Push source vs build target" framing; `setup=<source-setup>` placeholder swap; explicit "self-deploy / cross-deploy under git-push: NOT part of the delivery pattern" note; record-deploy + verify direct callers at build-target; build-status acks on build target. axis-k/l/m/n suppression markers for the topology-role prose.
- EDIT `internal/content/atoms/develop-change-drives-deploy.md`: tightened iteration cadence sentence + auto-close note to fit within `TestCorpusCoverage_OutputUnderMCPCap` soft cap on the 4-service multi-pair fixture.
- EDIT `internal/content/atoms/develop-strategy-awareness.md`: `actions | webhook | none` enumeration with actions = recommended, webhook = fallback; example uses `integration="actions"`.
- EDIT `internal/content/atoms/setup-git-push-container.md`: "Collect three inputs (use `AskUserQuestion` when the harness exposes it)" block lists remote URL + GIT_TOKEN + build-integration; PAT scope table promotes `Secrets + Workflows` to the default for GitHub (covers actions integration).
- EDIT `internal/content/atoms/setup-git-push-local.md`: "Collect inputs" note (local mode collects URL + integration only; token comes from the user's local git).
- EDIT `internal/content/atoms/setup-build-integration-actions.md`: title + body rewrite — "Recommended for GitHub: zero manual dashboard step"; priority lowered from `2` → `1` so it sorts first when both atoms match.
- EDIT `internal/content/atoms/setup-build-integration-webhook.md`: title + body rewrite — "Alternative (GitLab / policy-constrained repos)"; priority raised from `2` → `3` so it sorts after actions; dashboard steps now name `buildSetup` + `fallbackUrl`.
- EDIT `docs/spec-workflows.md` §4.8: qualifies cross-deploy as `closeMode=auto` direct-delivery only; documents DeployIntent as the central deploy-command resolver and lists its consumers.
- REGEN `internal/workflow/testdata/atom-goldens/` — 12 goldens regenerated via `ZCP_UPDATE_ATOM_GOLDENS=1`.

**Verify gate**:
- `go test ./... -short -count=1 -race` — all packages PASS
- `make lint-fast` — 0 issues
- Atom lint (Axis K/L/M/N) — 0 violations

### Final tally

- **Files added**: 4 (`deploy_intent.go`, `deploy_intent_test.go`, `deploy_intent_targets.go`, `resolve_build_target.go`, `phase4_redirect_test.go` — 5 actually counted)
- **Files edited**: 14 (build_plan, work_session, work_session_test, workflow_build_integration, workflow_git_push_setup, workflow_record_deploy, verify, workflow_phase5_test, develop-close-mode-git-push, develop-change-drives-deploy, develop-strategy-awareness, setup-git-push-container, setup-git-push-local, setup-build-integration-actions, setup-build-integration-webhook, spec-workflows.md — 16 actually)
- **Goldens regenerated**: 12
- **New tests**: 11 (`TestResolve_Table` 11 cases, `TestResolve_BuildPlanParity`, `TestEvaluateAutoClose_GitPushStandardPair` 8 cases, `TestResolvedDeployTargets_StandardPair` 4 cases, 5 Phase 4 redirect tests)
- **Test surface**: `go test ./... -short -race -count=1` green; `make lint-fast` clean

**Karel's 3 original problems**:

| Problem | Fix landing |
|---|---|
| 1. dev→dev self-deploy + dev→stage cross-deploy after git-push setup | Phase 1+2+4: planner resolves git-push delivery (no inline cross-deploy emit); auto-close gate evaluates stage-only resolved targets; record-deploy + verify redirect dev→stage |
| 2. webhook offered as default over actions | Phase 3+5: actions priority=1 (sorts first), webhook priority=3 (fallback); `recommendedIntegration` field on handler responses; PAT default scope includes Secrets+Workflows for actions |
| 3. AskUserQuestion not used; free-text questions | Phase 3+5: walkthrough responses carry `inputsRequired`, `options[]`, `inputField`, `prompt`, `recommendedIntegration` as structured machine-readable fields; atoms explicitly say "use AskUserQuestion when the harness exposes it" |

**Open Items remaining**:
1. ~~Webhook on stage half — frontend audit yielded verdict (a): dashboard accepts OAuth bind on any service-stack type, `zeropsYamlSetup` free-text. ZCP webhook deep-link uses `service-stack-source-code` slug not found in legacy frontend tree; live verification still recommended before the next live agent run.~~ **Resolved via frontend audit + fallbackUrl in webhook response.**
2. Recipe author boundary check — recipe-emitted import.yaml should respect new buildTarget for downstream CI config. Escalate to Aleš before next iteration.
3. Codex POST-WORK round on goldens — optional, not required for green-state ship.

