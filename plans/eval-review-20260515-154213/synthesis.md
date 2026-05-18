# Eval Review Synthesis — 20260515-154213 (26 sessions, 24 with usable data)

## Headline

Three patterns dominate the run and each one is a **shared substrate problem**, not a per-handler defect. (1) `zerops_workflow action="status"` and the launch-active envelope conflate "any in-flight thing on this project" with "your current task," repeatedly derailing agents working on unrelated requests; the same envelope shape also fails to communicate state transitions (drift-baseline still in effect, classify accepted/rejected) so agents can't tell what their last call actually did. (2) The container boot-shim claims `/var/www/{hostname}/` is unconditionally mounted, but in reality the mount only materializes after a bootstrap-provision step closes — and **every guidance surface downstream of the shim** (classify-prompt grep tables, env-discovery instructions, develop-loop pre-flight) assumes the mount exists. Three of five existing/develop sessions and four of five launch-production sessions burnt a tool call discovering this. (3) The pipeline through `internal/tools/*` that converts agent intent into platform calls is structurally **path-blind**: handlers happily accept inputs valid for one variant of a workflow (existing-project, dev-only, push-mode-CI) and then emit guidance, error wording, and example shapes from a different variant. The most visible symptoms are `SERVICE_NOT_FOUND: not bootstrapped` on three lifecycle handlers (means "no ZCP metadata," reads as "service missing"), `setup=` rejections without setup-name discovery, classify-prompt response showing `workflow="export"` examples while user is in `launch-production`, and the four-bucket env classifier silently accepting nonsense.

All three root causes have the property that fixing the **loudest leaf** (e.g. rewording the error string, retitling the atom) leaves the trunk intact and the same defect-class will re-surface on the next handler added.

---

## Root cause 1: Workflow `status` envelope conflates project-ambient state with current-task scope, and provides no transition marker

**Evidence:** 9 sessions across 4 groups.
- `cross-deploy-stage-promote-from-dev` — got `launch-active` for an unrelated `api-prod` workflow mid promote-from-dev task; "don't get pulled into resuming it"
- `export-buildfromgit-self-snapshot` — two stale launch-active workflows surfaced over the export user wanted; "launches outrank bootstraps"
- `greenfield-node-postgres-dev-stage` — final status returned launch-active for someone else's prior workflow after the develop session auto-closed; took two reads to understand
- `launch-production-dev-only` — stale per-session JSON state survived `action="start"`; agent had to physically `rm` files under `.zcp/state/launch-production/`
- `launch-production-from-standard-pair` — after an errored `action="classify"`, status returned `ready-to-launch`; agent could not tell whether classifications had been recorded
- `launch-production-new-project-push-mode` — in-flight `eval-zcp-prod` launch surfaced when user wanted `myapp-prod`; agent guessed re-calling `start` would replace it
- `launch-production-pipeline-configured` — user said "launched"; status said `ready-to-launch` from a stale record; agent had to trust the user
- `launch-production-pipeline-not-configured` — same staleness + `ambiguousChoices` vs sticky `targetProjectName` ambiguity
- `launch-production-existing-with-webhook` — `existingProjectId` accepted at discover, then `ready-to-launch` returned new-project guidance because the envelope never branched on path

**Symptoms it explains:**
- Stale launch-active records derail unrelated tasks (cross-deploy, export, develop)
- Drift-gate session files persist across `action="start"` (drift-loop)
- Errored mutating calls leave status looking like they succeeded (no `lastTransition` field)
- `ambiguousChoices` vs sticky `targetProjectName` — neither is authoritative
- Existing-project path silently accepted at discover, then ignored downstream
- "status before mutate" not surfaced as invariant in CLAUDE.md route table

**Why is this a root cause, not a trigger?** The status envelope's purpose (per `docs/spec-workflows.md` P4 invariant) is "supported recovery primitive after context compaction." But its current shape — `kind: launch-active` + sticky `targetProjectName` + ambiguousChoices + guidance string that points at the **most recent** active workflow on the project — encodes **project-ambient state** and offers it as **resumption guidance**. The compaction-recovery use case wants the answer to "what was *this* agent doing"; the leak use case wants "is any unrelated launch in flight." The envelope answers neither cleanly. The drift-gate session-file persistence (`workflow_launch_production.go:244`) is the same defect class one layer deeper: state captured for one phase survives a phase that should have invalidated it. The author intent (compaction-resume) drifted from the actual delivery (project-wide active-workflow scanner).

**Proposed structural fix:**
- Layer: `internal/tools/launch_status_recovery.go` envelope shape + `internal/tools/launch_state.go` lifecycle + `internal/tools/workflow_launch_production.go:240-244` baseline-write site.
- Shape: Split the envelope into two fields: `currentSession` (this PID's last touched workflow if any; null for ambient queries) and `projectActive` (array of unrelated active workflows with `relevance: "ambient" | "compaction-resume-candidate"`). Add `lastTransition: {action, accepted: bool, at}` so agents reading the envelope after a classify or start call see whether the call advanced anything. `action="start"` on launch-production must clear the prior session's drift baseline atomically (not preserve-on-refusal as today).
- Invariant restored: Status returns task-scoped answers by default; ambient state is opt-in and labelled. Mutation accept/reject leaves a transition marker in the envelope. `action="start"` is idempotent — re-calling supersedes the prior session deterministically.
- Blast radius: `internal/tools/launch_state.go` readActive*/find functions; the CLAUDE.md route table gains a "status-first for launch-production" line; atoms `launch-status-recovery.md`, `launch-ready-to-launch.md`, possibly a new `launch-fresh-start-supersedes.md`.
- Pinned by: `TestStatusRecovery_AmbientLaunchesNotMixedWithSessionCurrent`, `TestLaunchStart_ClearsPriorDriftBaseline`, `TestStatusEnvelope_CarriesLastTransitionMarker`, `TestStatusEnvelope_ExistingProjectPathBranchesGuidance`.

**Risks / non-obvious consequences:** Some atoms currently lean on the "status returns any in-flight launch" behaviour as a recovery hint after compaction. If the envelope hides ambient launches by default, real compaction-resume on launch-production needs to surface them — the `relevance` tag is load-bearing. Atomic baseline supersede on `action="start"` must be careful not to discard audit-log entries that are append-only for compliance.

---

## Root cause 2: `/var/www/{hostname}/` mount claim is unconditionally true in the boot-shim but conditional in reality, and every downstream guidance assumes the shim

**Evidence:** 7 sessions across 3 groups.
- `cross-deploy-stage-promote-from-dev` — `ls /var/www/` returned only `CLAUDE.md`; wasted Glob on `**/zerops.yaml`
- `develop-add-managed-dep-to-existing` — `/var/www/appdev` did not exist before bootstrap close fired
- `git-push-setup-with-cicd-method-prompt` — same; `autoMounts` in response confirmed mounts populate post-provision
- `launch-production-dev-only` — empty mount, classify-prompt's grep-the-source flow unusable
- `launch-production-existing-with-webhook` — fallback to classify-by-name worked but undocumented
- `launch-production-laravel-showcase` — service-scoped discover omits project-level envs that classify-prompt actually needs
- `launch-production-new-project-push-mode` + `launch-production-pipeline-skip` — both classified env vars from naming alone with no fallback path in atoms

**Symptoms it explains:**
- Wasted Glob/Read calls hunting for service code that isn't there yet
- Classify-prompt agents present classifications as if verified when they only saw names
- `develop-add-managed-dep` agent reasoned "directory doesn't exist == broken environment"
- The grep-source-with-ripgrep-tables guidance in classify atoms reads as authoritative but isn't applicable
- Service-scoped `zerops_discover hostname=X` silently omits project-level envs — half a dozen sessions had to re-call without hostname

**Why is this a root cause, not a trigger?** The boot-shim line at `internal/content/templates/claude_container.md:3` ("Service code SSHFS-mounted at `/var/www/{hostname}/`; mount IS the service's runtime filesystem") asserts an unconditional invariant. The actual behaviour is that mounts materialize **only after** the bootstrap/adopt workflow's provision step writes `autoMounts` and the SSHFS daemon picks them up — a transition the agent's never been told about. The author intent (give agents a fast Read/Edit/Write path to service code) is sound; the drift is that no version of the shim acknowledges the precondition. Classify-prompt and develop atoms then **chain off the shim's invariant**: they prescribe `rg`/`grep` over `/var/www/{hostname}/` with detailed per-language tables, with no fallback branch. So the same defect — "guidance assumes a precondition the shim promised unconditionally" — propagates to every classify/develop/discover atom that touches source code.

Fixing the shim alone is symptom-level; fixing the seven atoms that depend on it is one-axis. The trunk fix is: (a) make the shim conditional and accurate, (b) introduce a single "mount-availability check" guidance primitive the dependent atoms reference, (c) give `zerops_discover` and classify-prompt response shapes a structured `sourceAccessible: bool` field so agents don't have to probe via Glob.

**Proposed structural fix:**
- Layer: `internal/content/templates/claude_container.md` boot-shim line (truth); `internal/tools/discover.go` response carries `sourceMountAvailable` per service; `internal/tools/workflow_launch_production.go` classify-prompt response carries `sourceMountAvailable` and `projectEnvsIncluded`; atoms `launch-classify-platform-envs.md` + `export-classify-envs.md` + `bootstrap-env-var-discovery.md` reshape with explicit "if mount unavailable" fallback path.
- Shape: Boot-shim line becomes "Service code SSHFS-mounted at `/var/www/{hostname}/` **after the service has been bootstrapped or adopted**; mount IS the service's runtime filesystem when present, otherwise `/var/www/{hostname}/` does not exist." Discover and classify-prompt responses carry an explicit boolean. Classify-prompt's example block leads with the no-mount fast-path (classify-from-name + surface-rationale-to-user), grep-tables move to "if mount available" subsection.
- Invariant restored: One source of truth for mount availability; every guidance surface reads the same signal; agents never have to probe via Glob/ls to discover an environmental precondition.
- Blast radius: Boot-shim, two atom rewrites (classify-platform-envs, export-classify-envs), one atom addition (`bootstrap-mount-availability.md`?), discover-tool schema, classify-prompt envelope field. ~8 files.
- Pinned by: `TestClaudeContainerShim_MountClaimIsConditional`, `TestDiscoverResponse_CarriesSourceMountAvailable`, `TestClassifyPromptResponse_CarriesSourceMountFlag`, `TestAtomAuthoringLint_ClassifyAtomsCoverNoMountFallback`.

**Risks / non-obvious consequences:** Atoms that currently embed grep-tables verbatim will get longer if both branches are surfaced inline; consider an indirection ("see bootstrap-mount-availability.md") to keep classify-prompt scannable. The eval-seed could also provision mounts so the realistic-case path is exercised — orthogonal to the fix.

---

## Root cause 3: Lifecycle and deploy handlers are path-blind — accept inputs valid for one variant, emit guidance/errors/examples from another

**Evidence:** 11 sessions across 4 groups.
- `cross-deploy-stage-promote-from-dev` — `setup="prod"` (yaml block) vs hostname-shaped neighbours; `SSH_DEPLOY_FAILED` doesn't surface available setup names
- `git-push-setup-with-cicd-method-prompt` — `INVALID_ZEROPS_YML: setup not found`, `build-integration` returns workflow file defaulting to dev-half when user said stage; raw-zcli example only under GitLab in `ci-cd.md`
- `delivery-git-push-actions-setup` — walkthrough text chains three orthogonal actions (close-mode / git-push-setup / build-integration) as if prerequisite
- `export-buildfromgit-self-snapshot` — walkthrough ordering implies sequence dependency; close-mode (often what user wants) missing from walkthrough entirely
- `git-push-setup-with-cicd-method-prompt` + `export-buildfromgit-self-snapshot` — `SERVICE_NOT_FOUND: not bootstrapped` (verified at `workflow_git_push_setup.go:81`, `workflow_build_integration.go:78`, `workflow_close_mode.go:95`) reads as "service missing" when it means "no ZCP metadata"
- `launch-production-pipeline-not-configured` — agent reached for `build-integration` for prod-side CI (wrong scope) because nothing in error wording tells them prod-side wiring lives at a different phase
- `launch-production-from-standard-pair` — classify-prompt response embeds `export-classify-envs.md` content showing `workflow="export"` examples (confirmed: `internal/content/atoms/export-classify-envs.md:22,95` carry the example, and launch-production's classify atom is content-shared with export)
- `launch-production-existing-with-webhook` — existing-project path discover/scope accepts `existingProjectId` then ready-to-launch returns new-project guidance
- `launch-production-existing-project-token` — CLAUDE.md route phrase "deploy dev/stage to prod" matches existing-project intent but routes to new-project workflow
- `classic-rust-postgres-standard` — workflow develop hint drops `setup=` for self-deploy on multi-setup recipe yaml, although it correctly includes setup for cross-deploy hint below
- `classic-php-mariadb-standard` — knowledge template ships bit-identical envVariables in dev/prod blocks; preflight rejects

**Symptoms it explains:**
- `SERVICE_NOT_FOUND: not bootstrapped` (3 identical sites)
- `setup=` rejection without setup-name discovery (2 sites + workflow hint dropping setup)
- Walkthroughs framed as chains when actions are orthogonal
- `build-integration` defaulting to dev-half when stage intent
- classify-prompt example using `workflow="export"` while user is in `launch-production`
- existing-project path half-built, accepted but ignored downstream
- knowledge template ships preflight-violating defaults

**Why is this a root cause, not a trigger?** The handlers in `internal/tools/workflow_{git_push_setup,build_integration,close_mode,export,launch_production}.go` are designed as **independent imperative entry points** — each accepts inputs and emits its own response. The shape ought to be: every handler reads a path discriminator (new-project vs existing-project; push-mode vs marketplace-action; dev-half vs stage-half; self-deploy vs cross-deploy; bootstrap-fresh vs adopt) and **emits guidance, errors, and examples branched on that discriminator**. Today, multiple handlers either don't compute the discriminator at all (build-integration defaults to dev-half regardless of session intent) or compute it for validation but not for response shape (launch-production accepts existingProjectId then ignores it in guidance). The classify-prompt atom content-sharing between export and launch-production is the same defect-class shifted one layer up: the corpus author wrote one body of atoms for both workflows, and the example tokens drifted to the workflow that exercised them first.

The shared root: **the response shape per handler is computed from a single canonical template, not from `(handlerName, pathDiscriminator)`**. Every fix at the leaf level (rewording an error, retitling an atom, fixing one example) papers over the next handler that hits the same shape.

**Proposed structural fix:**
- Layer: `internal/tools/workflow_{git_push_setup,build_integration,close_mode,launch_production}.go` response builders; `internal/content/atoms/*` classify/setup atoms with workflow-scoped variants; error code library in `internal/platform/errors.go` (one new code for "no ZCP metadata") .
- Shape: Introduce explicit path-discriminator type in `internal/topology/` (peer to existing `Mode`, `BuildIntegration`, etc.) — call it `WorkflowPath` with values like `NewProject`, `ExistingProject`, `PushModeCI`, `MarketplaceActionCI`, `DevHalfTarget`, `StageHalfTarget`. Each handler computes its path once at entry and threads it through response assembly. Error wire shape gains `path` field. Atom corpus authoring lint forbids `workflow="..."` example tokens that don't match the atom's declared workflow scope (axis addition to `internal/content/atoms_lint.go`). Replace `SERVICE_NOT_FOUND: not bootstrapped` with a dedicated code (`ERR_SERVICE_NO_ZCP_METADATA`) carrying explicit Recovery pointing at `bootstrap action=start workflow=bootstrap route=adopt`. Setup-name handlers preflight-read the source service's `zerops.yaml` and either default smarter or surface available setup names in the error payload.
- Invariant restored: A handler's response is a function of `(input, computedPath)` — atoms, errors, examples, walkthrough text branch on the path. Path is named, typed, lint-pinned. The "lifecycle handlers reference dev-half" rule becomes a typed invariant rather than scattered prose. Setup-name discovery becomes a single preflight helper.
- Blast radius: ~15-20 files. Topology type addition + 4 handler response builders + 3 error sites + 2 atom rewrites + 1 new lint axis + setup-name preflight helper + atom corpus axes lint. Spec-workflows.md gains §4.x on workflow paths.
- Pinned by: `TestWorkflowPath_ComputedAtHandlerEntry_*` (per handler), `TestErrorWire_NoZCPMetadataCarriesAdoptRecovery`, `TestBuildIntegration_DefaultsTo_StageHalfWhenIntent_Stage`, `TestAtomsLint_ExampleTokensMatchAtomWorkflowScope`, `TestDeploySSH_SurfacesAvailableSetupsOnSetupMismatch`.

**Risks / non-obvious consequences:** Adding `WorkflowPath` to topology is wide-and-long — every handler touches it. Worth the structural investment because today's path-blindness is the source of the largest cluster of cross-session findings, but careful: don't model paths the system doesn't actually branch on. The "lifecycle handlers reference dev-half" invariant may need a one-off escape for stage-deploy CI workflows specifically (build-integration's `action=actions` target hint); model it explicitly rather than as a default.

---

## Appendix A: Other actionable findings (not in top 3)

- **[atoms]** envClassifications four-bucket model has no slot for ZCP-provisioned-regenerate-per-project envs (`GIT_TOKEN`, `ZCP_API_KEY`) — every launch-production session guessed. Add fifth bucket. — Note: `launch-classify-platform-envs.md` already auto-handles `ZCP_API_KEY` by drop, but agents see it in the row table anyway because content-sharing with export-classify is leaky (see RC3). Verify whether the auto-handle list actually triggers in launch flow.
- **[atoms]** Plan-schema nesting (`bootstrapMode`/`stageHostname` inside `runtime`) — every classic session flagged; JSON example in `bootstrap-classic-plan-dynamic.md` needs prominence over prose.
- **[atoms]** Dev-mode `start: zsc noop --silent` + dev-server `command` semantic ("what `start:` would be if real") is a pitfall cluster across classic-bun/go/python/rust; current atoms exist but dev_server `command` arg description (`internal/tools/dev_server.go`) muddles literal-vs-real.
- **[tools]** `zerops_deploy` cross-deploy `setup=` parameter should default-smart by reading available setup names from source service zerops.yaml (or list them on error) — folded into RC3 but worth standalone if RC3 split.
- **[atoms]** Subdomain auto-enable: fan-out across non-deployed services trips `serviceStackIsNotHttp`; recovery hint on `http_root: fail` should be service-scoped, not generic "enable subdomain everywhere." [fullstack-multi-runtime]
- **[atoms]** Build-failure classifier mislabels readiness-fail as `failedPhase: init`, `category: start` when init commands all succeeded ([php-mariadb-standard]). `deploy_failure*.go` pattern library extension.
- **[atoms]** `zerops_workflow workflow="export"` discoverability — `export-buildfromgit-self-snapshot` agent never routed there for "snapshot this project with secrets"; CLAUDE.md route table doesn't mention export for snapshot intent.
- **[tool-desc]** `closeMode` schema annotation has terse example value (`{hostname:value}`); concrete `{"appdev":"git-push"}` is one-line fix in `workflow.go:46`.
- **[atoms]** Self-deploy destruction warning (DM-2) is framed around "source destroyed"; static+Node-build agents need the "clean unwanted files at end of buildCommands" canonical pattern surfaced.
- **[tool-desc]** Phase-consumption of launch-production inputs (`productionProjectName`, `region`, `skipPipelineSetup`) undocumented in tool description — caused phase-mismatch guessing in `fsp` and `pipeline-skip`.
- **[atoms]** Adopt-pair discover plan shape (`isExisting: true` + `resolution: EXISTS` + `bootstrapMode: standard`) has no end-to-end example in `bootstrap-adopt-discover.md`; three sessions assembled by inference.
- **[guidance]** Recipe vs classic route-pick lacks "recipe is template vs demo" signal — `nodejs-hello-world` exact-fit on infra but gutted by agent because demo code wasn't theirs. Atom + recipe-response shape.

---

## Appendix B: Eval-infra observations

- **2 of 26 sessions failed in seed phase** (`existing-simple-mode-node-add-endpoint`: `wait for api: process ... failed: unknown`; `existing-standard-appdev-only-reminders`: cleanup-cancel during seed teardown). Both pre-agent. Pattern suggests either eval-zcp resource contention from parallel runs or seed-poll status-classification gap. Worth checking whether `eval/behavioral/internal/seed.go` retries on `unknown` and whether teardown is serialized.
- **1 of 26 sessions (`recipe-laravel-minimal-standard`) lost its retrospective** — clean success, no self-review.md. Transcript shows 4 `system.init` events in one file, suggesting runner restarts mid-task that confused the retrospective resume's session_id keying. `eval/behavioral/flow-eval.sh` retrospective-fire path needs hardening against mid-task restarts.
- **`AskUserQuestion` permission-denied** twice in eval (`recipe-laravel-minimal`, `pipeline-skip`) — host-tool issue, not ZCP. Either allowlist in eval harness or pre-pick choices in scenario prompts.
- **ToolSearch `select:` short-name vs `mcp__zerops__`-prefix ambiguity** — three sessions reported (`classic-php`, `classic-static`, `greenfield-website-from-brief`). Bootstrap tool-preload atom should disambiguate; not a ZCP-owned tool but ZCP guidance can pin the form.
