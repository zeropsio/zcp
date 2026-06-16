# launch-production — source-of-truth + multi-runtime redesign

**Date:** 2026-05-20
**Status:** Draft (Codex pass integrated — ready for Karel review)
**Triggered by:** Session log `.zcp/manual/l1.txt` — production project was launched from the **recipe-template repo** instead of the user's actual code.

---

## TL;DR

`launch-production` today silently builds prod from `git remote get-url origin` inside the source container. For projects bootstrapped from a Zerops recipe, that remote points at `zerops-recipe-apps/<slug>` — the public template. Three orthogonal defects collapse into one disaster: (a) no `GitPushState=configured` gate, (b) `buildFromGit` URL provenance never verified, (c) the bundle composer is hard-coded for **one** runtime + N managed deps. Result: user's actual modified code (delivered to dev via `zcli push` tarball upload — never pushed to any git remote) is invisible to production; only one runtime per launch call.

Fix: require `GitPushState=configured` on every promoted runtime, refactor `BuildLaunch` to compose N runtimes from a `Promotables []PromotableEntry` input, derive the prod-setup template from `setup: stage` when present, chain agents into `git-push-setup` (one per runtime that needs it) before publish.

---

## The bug, concretely

### Session evidence (`.zcp/manual/l1.txt`)

```
User: "spus produkci"
Agent: starts launch-production workflow → targetService=appdev
Workflow: scope-prompt → classify-prompt → ready-to-launch → launched
Result: prod project laravel-showcase-agent-prod created,
        import.yaml carried buildFromGit: github.com/zerops-recipe-apps/laravel-showcase-app
User: "diky cemu jsi to vyubildil? jak jsi tam dostal ten kod?"
Agent: "RemoteURL z appdev ServiceMeta"
        ↑ wrong — ServiceMeta.RemoteURL is empty when gitPushState=unconfigured
        the URL came from /var/www/.git/config (recipe-bootstrap clone), not meta
```

ServiceMeta state at launch time (both `appdev` and `workerstage`):

```
gitPushState: "unconfigured"      ← agent never ran git-push-setup
buildIntegration: "none"          ← no CI integration
bootstrapSession: ""              ← adopted, not freshly bootstrapped
firstDeployedAt: "2026-05-19T..." ← code DID deploy (via zcli push tarball)
```

The user delivered code via `zcli push` (tarball upload, leaves `.git/config` alone). The recipe-bootstrap left `.git/config` pointing at the recipe template. `launch-production` SSH-read the container, found the template URL, used it as `buildFromGit:` in the prod import. Prod cloned the template and built from there — none of the user's modifications made it.

### Three orthogonal defects

| # | Defect | File:line | Root | Impact |
|---|---|---|---|---|
| **B1** | Source-of-truth gate missing | `workflow_launch_production.go:788-793` — only checks `source.RepoURL != ""` | No `GitPushState=configured` check; reads live `.git/config` without provenance verification | **Production builds from a remote the user does not own.** Recipe template wins. |
| **B2** | Pair-fixation in composer | `internal/ops/bundle/launch.go:90-112` — single `runtimeEntry` + N managed deps | `TargetHostname` is one string; loop is over managed deps only | User can't promote `appdev/appstage + workerstage` in one launch. Forces serial launches → hostname conflicts on managed deps in the second call. |
| **B3** | Wrong prod-setup template preference | `launch_prod_setup_derive.go:83-103` — prefers `setup: dev` → `setup: stage` → first non-prod | Inverts the user's articulated model (stage is the validated last-known-good; dev is iteration) | Prod setup mirrors dev (debug, hot-reload) instead of stage (build-once, immutable artifact). |

### Why the export workflow already gets this right

`internal/tools/workflow_export.go:226-228`:

```go
if meta.GitPushState != topology.GitPushConfigured {
    return gitPushSetupChainResponse(ctx, input.TargetService, bundle, "GitPushState != configured", envOpts, corpus), nil, nil
}
```

Export refuses to publish without `GitPushState=configured` AND chains the agent into the `git-push-setup` action. Launch-production has the same publish semantics (same `buildFromGit` field, same need for an OWNED remote) but lacks the gate. **The fix is to bring launch-production to parity with export.**

---

## Design

### D1 — Source-of-truth gate (P-LP-10, new invariant)

**Promise:** for every runtime promoted by `launch-production`, the `buildFromGit:` URL in the production import.yaml comes from a ServiceMeta whose `GitPushState == GitPushConfigured` AND matches the live container's `git remote get-url origin`. No silent fallback to live `.git/config` written by the recipe-bootstrap.

**Per-runtime check structure** (helper `validateLaunchSourceControl(ctx, deps, stateDir, runtime)`, fed from `LaunchSourceControlCheck` carrier):

| # | Check | Phase |
|---|---|---|
| 1 | `meta.GitPushState == GitPushConfigured` | P1 |
| 2 | `meta.RemoteURL != ""` | P1 |
| 3 | live `git remote get-url origin` on `/var/www` of the **push hostname** (dev half for pairs) equals `meta.RemoteURL` | P1 |
| 4 | bundle's `RepoURL` input equals `meta.RemoteURL` (composer boundary defensive) | P1 |
| 5 | live HEAD on push hostname is known (non-empty `git rev-parse HEAD`) | P3 |
| 6 | live working tree is clean (`git status --porcelain` empty, minus allowlist for ZCP-managed paths) | P3 |
| 7 | `git ls-remote <RemoteURL> HEAD` (default branch) equals local HEAD | P3 |

**Two enforcement sites — different audit semantics:**

1. **Read-side gate** (between scope-prompt and classify-prompt): blocks the workflow forward-flow. **No audit entry** (Codex correction: read-side gate fires on every poll — auditing every refusal would spam `launch-audit-log.json`).
2. **Publish-side gate** (inside `executeLaunchMutation` + `executeExistingProjectMutation`): same helper called with `auditFail(...)` wrapped. Drift between read-side OK and publish-side fail = real publish refusal that operators want logged.

**Chain response** generalizes `gitPushSetupChainResponse` from export. For each runtime failing check 1-4, surface:
- `status: "source-control-required"` (NOT `failed` — this is recoverable, not terminal)
- Per-runtime blocker payload with the structured next call: `zerops_workflow action="git-push-setup" service=<pushHostname> remoteUrl=<owned URL>`
- After git-push-setup, a chained next call: `zerops_deploy targetService=<pushHostname> strategy="git-push" branch="main"` (so the agent actually delivers code to the configured remote — Codex insight: `git-push-setup` alone proves wiring but not push)
- Final step: re-call launch.

**Reason this works:** `git-push-setup confirm` writes `meta.RemoteURL` + `meta.GitPushState = configured` together (`workflow_git_push_setup.go:189-190`). Check 3 (live origin == meta) closes the case where a remote was set up but later someone manually rewrote `.git/config`. Check 4 is defensive at the composer boundary. Reusing meta closes the recipe-template loophole structurally — there's no API surface where a recipe URL can leak in.

**Pinning tests (P1):**
- `TestHandleLaunchProduction_GitPushUnconfigured_BlocksBeforeClassify`
- `TestHandleLaunchProduction_LiveRemoteMismatch_Blocks`
- `TestHandleLaunchProduction_PublishSourceControlRefusal_AppendsAudit`
- `TestHandleLaunchProduction_ReadSideGateDoesNotAudit`
- `TestBuildLaunch_RepoURLFromMetaNotSSH`

### D2 — Multi-runtime composition

**Input reshape.** `WorkflowInput.TargetService` (single string) → `WorkflowInput.Promotables []LaunchPromotableInput` (NEW launch-specific field — does not overload TargetService, which keeps its meaning for export/adopt). Each entry:

```go
type LaunchPromotableInput struct {
    Hostname              string `json:"hostname"`              // user-supplied (dev OR stage half accepted)
    ProdHostname          string `json:"prodHostname,omitempty"` // optional override for prod runtime name; default derived
    ProdSetupNameOverride string `json:"prodSetupNameOverride,omitempty"`
}
```

**Internal resolved shape** (post-normalization, fed to composer):

```go
type resolvedLaunchRuntime struct {
    ChoiceHostname string  // what user typed, e.g. "appstage"
    PushHostname   string  // canonical dev-half (source of code), e.g. "appdev"
    ProdHostname   string  // production runtime name in import.yaml
    StageHostname  string  // template source for setup picker, e.g. "appstage"
    ServiceType    string
    Meta           workflow.ServiceMeta
}
```

**Production hostname derivation (Codex insight — important).** Today the launch handler normalizes stage→dev and writes `TargetHostname=appdev` into the prod import.yaml, producing oddities like `pipeline-not-configured-appdev`. The prod runtime should be named per its production role:

- `appstage/appdev` (standard pair) → `app` (drop `-dev`/`-stage` suffix)
- `workerstage` (simple) → `worker`
- Fall back to `ChoiceHostname` (no transform) when no recognizable suffix
- `ProdHostname` input override always wins

**Composer reshape.** `BuildLaunch` accepts `Runtimes []LaunchRuntimeInput` instead of single TargetHostname/RepoURL/SetupName. Loops, emits one entry per runtime, dedupes managed deps:

```go
for _, r := range inputs.Runtimes {
    services = append(services, runtimeEntryFromInput(r))
}
for _, m := range dedupeManagedHostnames(inputs.ManagedServices) {
    services = append(services, managedEntryWithRules(m, true, keepNonHASet[m.Hostname]))
}
```

`runtimeEntryFromInput` carries `buildFromGit: r.RepoURL` from the meta-resolved value (D1 check 4 guards), per-runtime `zeropsSetup`, `minContainers`, and the prod cpu mode.

**Multi-pick scope prompt.** `gatherLaunchSourceContext` keeps pair-collapse but the returned shape changes from `AvailableRuntimes + SuggestedRuntime` (single) → `AvailablePromotables + SuggestedPromotables []runtimeChoice` (plural). The atom `launch-scope-prompt` updates: when user asks "everything", select all USER runtimes + all managed deps. Don't collapse to one pair.

**SourceSnapshot reshape.** `SourceSnapshot` becomes `Snapshots []RuntimeSnapshot` keyed by `PushHostname` (per-runtime GitCommitSHA + ZeropsYAMLSHA256 + ProjectEnvsDigest shared across runtimes). P-LP-3 drift check fires per-runtime; any drift refuses the whole publish.

**Pipeline check reshape.** `pipelineCheckInputs.RuntimeHostname` (single) → `pipelineCheckInputs.Runtimes []pipelineRuntimeInput`. `executeLaunchPipelineCheck` verifies every imported runtime's integration status. P-LP-8 (pipeline issues = warn, not fail) preserved per-runtime.

**Pinning tests:**
- `TestBuildLaunchBundle_MultipleRuntimes_SharedManagedDepsOnce`
- `TestBuildLaunchBundle_PerRuntimeBuildFromGitFromPromotable`
- `TestHandleLaunchProduction_MultiRuntime_AllPromoted`
- `TestGatherLaunchSourceContext_EverythingSelectsAllRuntimes`
- `TestExecuteLaunchPipelineCheck_MultipleRuntimes`
- `TestResolveLaunchRuntime_ProdHostnameDerivation`
- `TestPromotables_ZeroLengthRefused`

### D3 — Setup template preference

Reorder `pickProdSetupTemplate` priority (Codex correction — three tiers, not two):

1. **Existing explicit `prod` setup** — if the user already wrote one, don't override it. The whole derive-prod-from-source flow only fires when no `prod` block exists.
2. **Stage setup for the pair** — `setup: stage`, `setup: appstage`, or the pair's stage-half-derived name.
3. **Dev setup** — fallback only for runtimes with no stage half (simple/dev/local-only modes).

Rationale: stage is the validated last-known-good copy that runs on Zerops; dev's setup encodes iteration-only patterns (hot-reload watchers, debug logging, source-mounted volumes) that don't survive the recipe build environment.

**For multi-runtime:** when promoting `workerstage` (simple, no pair), there is no `setup: stage` block named `workerstage-stage` — `pickSetupName` handles this via the hostname-match priority (already implemented). D3 only changes the **fallback** when none of the hostname-derived candidates exist.

**Pinning test:** `TestPickProdSetupTemplate_PrefersStageOverDev`.

### D4 — Dev-code-via-push enforcement (extends D1 in P3)

User's articulated flow: "kód brát z toho, co je na dev, který by se měl pushnout do gitu — to je standardní flow."

`git-push-setup` confirm (`workflow_git_push_setup.go:181`) writes `RemoteURL` + flips `GitPushState=configured`, but does NOT verify that the user's working tree is up-to-date on the remote. Users could configure git-push, never run a push, then launch — and prod builds from an empty/stale remote.

**Don't hide push inside launch mutation.** Push writes to an external git remote, may require credentials, and is an explicit user-owned step. Use the existing chain pattern: blocker atom tells the agent to chain `git-push-setup` → `zerops_deploy strategy="git-push"` → re-call launch. Never auto-push inside the launch handler.

P3 adds checks 5-7 to the same `validateLaunchSourceControl` helper:
- **Check 5**: SSH read `git rev-parse HEAD` on push hostname (must be non-empty).
- **Check 6**: SSH read `git status --porcelain` — empty (clean). Allowlist for ZCP-managed paths (e.g. transient `.zcp/` state) defined in topology constant.
- **Check 7**: `git ls-remote <RemoteURL> <default-branch>` — local HEAD SHA must equal the remote-side HEAD SHA. This is the proof bit.

Failure surfaces as the same chain response but with a different blocker ID per failure shape (`dev-tree-dirty` / `head-not-pushed` / `remote-head-mismatch`) and a different chained call (`zerops_deploy strategy="git-push"` vs `git commit -A && zerops_deploy strategy="git-push"`).

**Semantic note (Codex insight — open):** "validated on stage" is not actually enforceable today. ZCP has no durable record of "stage was built from commit X" for tarball/zcli-push deploys. The enforceable near-term rule is: **prod builds the exact pushed dev HEAD.** A stronger "prod builds the exact commit currently validated on stage" rule needs either commit tracking at deploy time (record SHA per service stack in ServiceMeta on every deploy) OR a required dev-to-stage git-push close before launch.

P3 ships the near-term rule. Stronger rule = open question for Karel (see below).

**Pinning tests (P3):**
- `TestLaunchSourceGate_DirtyWorkingTreeBlocks`
- `TestLaunchSourceGate_HeadNotOnRemoteBlocks`
- `TestLaunchSourceGate_RemoteHeadMismatchBlocks`
- `TestLaunchSourceGate_AllChecksPassPromotes`

---

## Phasing

| Phase | Files changed | Tests added | Risk |
|---|---|---|---|
| **P1 — Gate B1** | `workflow_launch_production.go` (gate), `launch_source_read.go` (drop SSH RepoURL read), new `launch_git_push_gate.go` (chain response), new atom `launch-git-push-setup-required.md` | `TestLaunchSourceValidation_RefusesUnconfiguredGitPush` + 2 chain tests | Breaks every existing launch test (they all assume live SSH URL works). Fixture migration: each test gets a `meta.RemoteURL + GitPushState=configured`. ~30 test-data updates. |
| **P2 — Multi-runtime composition** | `internal/ops/bundle/inputs.go` (Promotables), `internal/ops/bundle/launch.go` (loop), `workflow_launch_production.go` (input wiring), `launch_source_context.go` (multi-select prompt) | `TestBuildLaunch_MultipleRuntimes*` + multi-runtime e2e | Wire shape breaks: `targetService` → `targetServices` (input field reshape). No backward compat per CLAUDE.md (pre-prod). |
| **P3 — Setup preference + dev-push enforcement** | `launch_prod_setup_derive.go` (reorder), new `launch_preflight_git.go`, atom `launch-dev-clean-pushed.md` | `TestPickProdSetupTemplate_PrefersStage` + 3 preflight tests | SSH-heavy preflight on every launch — extends latency. Mitigation: parallel `git status` reads when multiple runtimes are promoted. |
| **P4 — Spec + atom corpus** | `docs/spec-launch-production-platform-spike.md` (P-LP-10 + P-LP-11), `docs/spec-workflows.md` (§9 + invariants table), atoms `launch-scope-prompt.md`, `launch-classify-prompt.md` (multi-runtime language), CLAUDE.md bullet | Atom lint passes, golden file regen | Atom guidance drift across phases — write specs FIRST, atoms once per phase, regen goldens at phase boundary. |

Each phase commits independently. P1 ships the security fix; P2-P4 stack on it.

---

## New invariants (P-LP-10, P-LP-11)

To add to `docs/spec-workflows.md §9.1`:

| Anchor | Promise |
|---|---|
| **P-LP-10** | The `buildFromGit:` URL in every production import.yaml runtime entry comes from `ServiceMeta.RemoteURL` of a meta with `GitPushState == GitPushConfigured`. NEVER from a live SSH read of `/var/www/.git/config`. Pinned by `TestBuildLaunch_RepoURLFromMetaNotSSH` + `TestLaunchSourceValidation_RefusesUnconfiguredGitPush`. |
| **P-LP-11** | Every promoted runtime's dev-half working tree MUST be clean AND HEAD MUST be reachable on the configured `RemoteURL` at publish time. Pinned by `TestLaunchPreFlight_*`. |

---

## Open questions for Karel (must answer before P1 starts)

1. **Production runtime hostnames.** What should the prod-side names be?
   - `appstage/appdev` → `app`? Or keep `appstage` (so users still see "the stage I promoted")? Or `appdev`-like-today?
   - `workerstage` → `worker`?
   - Does this affect ServiceMeta lookups post-launch (the prod project's metas would be keyed by the new names)?
2. **`buildFromGit` ref pinning.** Does Zerops' `buildFromGit:` clone the default branch only, or can it pin a branch/tag/SHA? If only default-branch, D4 check 7 must verify HEAD-of-default-branch (not arbitrary local HEAD).
3. **"Stage was validated" signal.** Today ZCP records nothing durable per-service-stack about "what commit was last built and deployed." Is the near-term P3 rule ("prod builds the exact pushed dev HEAD") sufficient, or do we want to add per-stack `LastDeployedCommitSHA` to ServiceMeta first and gate prod on "matches stage's last successful deploy"?
4. **Monorepo / shared-codebase workers.** When two `Promotables` share the same source repo (common pattern: `app` + `worker` from the same monorepo), should one `git-push-setup` proof satisfy both, or require per-runtime proof? Today `meta.GitPushState` is per-pair-keyed-meta — so workerstage gets its own meta with its own check. Could dedupe by RemoteURL.
5. **Existing-project additive launch.** Should the existing-project path (where the user passes `existingProjectId + existingProdToken`) stay supported as today, or fold into "multi-runtime single launch is the primary"? Folding eliminates the second-launch hostname-conflict trap.
6. **Hard refusal on dirty working tree.** Confirm: P3 should hard-block when `git status --porcelain` is non-empty? Soft-warn would defeat the source-of-truth promise. Codex agrees with hard-block; Karel makes the call.
7. **Multi-select scope prompt default.** Pre-check "all USER runtimes + all managed deps" by default, OR pre-check the user's likely primary and let them add more? Codex recommends "all-default when user says 'everything'".

---

## Codex review pass — what landed

- **Diagnosis confirmed end-to-end** with file:line evidence (`recipe_templates_import.go:278` writes recipe URL; `service_git_init.go:40` preserves existing .git; `deploy_ssh.go:194` has a comment explicitly saying buildFromGit/upstream clone carries `.git/` into runtime; `zcli push` is tarball-only, never rewrites origin).
- **Gate refined**: not just `GitPushState=configured` — 4 checks in P1 (config + remoteUrl + live-origin-matches-meta + composer-input-matches-meta) close the loophole structurally.
- **Audit semantics split**: read-side gate doesn't audit (would spam every poll); publish-side does.
- **Promotable input shape** keeps `TargetService` for export/adopt unchanged, adds launch-specific `Promotables[]` field — no overloading.
- **Production hostname derivation** identified as separate concern (today `appdev` ends up as prod runtime name; should be `app` or user-supplied `ProdHostname`).
- **Setup template priority** corrected to 3 tiers (existing `prod` wins → stage → dev fallback).
- **Push proof** stays explicit — chain `git-push-setup` + `zerops_deploy strategy=git-push`, don't hide inside launch mutation.
- **"Validated on stage" semantic** flagged as needing durable per-stack commit tracking — out of scope for P1-P3, candidate for backlog.

---

## Final acceptance criteria — Karel-confirmed answers (2026-05-20 round 2)

| # | Decision | Detail |
|---|---|---|
| 1 | Prod hostname derivation | `appstage/appdev → app`, `workerstage → worker`. Drop suffixy `-dev`/`-stage`/`-worker`. `prodHostname` override always wins. |
| 2 | `buildFromGit` ref pinning | **NOT SUPPORTED by platform** — schema je `type: string`, jen URL. Platforma kloní default branch. P3 check 7 = single-shot validation at publish time. Pinning na SHA = backlog/feature-request směrem Zerops. |
| 3 | LastDeployedCommitSHA v ServiceMeta | **Ano**, P3 přidá; enables "prod builds exact pushed dev HEAD" rule. |
| 4 | Multi-runtime composition | **Varianta D — smart hybrid**. Defensive default (1 runtime + infra) když runtimes mají separate `meta.RemoteURL`; batch all když monorepo (sdílený RemoteURL). Agent vždy AskUserQuestion. |
| 5 | Existing-project additive | **Clean project = primary, existing-project = explicit prompt**. `MergeStrategy map[hostname]=skip/replace` per promotable. Replace vyžaduje `confirmDestructive` (extends diagnose-before-destruct invariant). |
| 6 | Dirty tree | **Hard block**. Soft-warn defeats source-of-truth promise. |
| 7 | Multi-select default | Smart heuristic: monorepo → all pre-selected; separate repos → primary + infra pre-selected, agent vyhodnotí + ptá. |

## Disconnects identified at user-flow cross-check (D1-D6)

| # | Disconnect | Fix v které fázi |
|---|---|---|
| D1 | `WorkflowInput.SkipBuildIntegration []string` chybí — warn-blocker se vrací na každý re-call bez ack mechanism | P1 |
| D2 | `launch-source-control-required.md` musí mít per-blocker-id user-friendly mapping + sekvenční discipline pro multi-promotable chains | P1 (atom rewrite) |
| D3 | `sourceContext` chybí `mode` + `recommendation` text — agent dnes nemá heuristic input pro batch/defensive výběr | P2 |
| D4 | existing-project ack mechanism (`MergeStrategy` map) + composer respekt + AskUserQuestion shape | P4 |
| D5 | git-push-setup walkthrough atom je develop-flow-styled; launch potřebuje "kód do prod" framing | P1 (atom wording v launch-source-control-required.md) |
| D6 | Live ImportServices behavior na duplicate hostname (replace vs error?) — verify proti eval-zcp před P4 | P4 e2e |

## Phase split — final

| Fáze | Co | Komity | Verify gate |
|---|---|---|---|
| **P1** | Source-control gate (checks 1-3), `source-control-required` status, chain-out blockers, audit asymmetry, `SkipBuildIntegration` input, atom `launch-source-control-required.md` (per-blocker-id table), ~30 happy-path test fixtures dostane `GitPushState=configured` seed | 1 commit | `go test -race ./... -count=1` + `make lint-local` |
| **P2** | `Promotables []LaunchPromotableInput`, `BuildLaunch` loop, dedupeManagedHostnames, prod hostname derivation, multi-pick scope context (`mode` + `recommendation`), per-runtime SourceSnapshot, pipeline check multi-runtime | 1 commit | same |
| **P3** | Push proof checks 4-5 (clean working tree, ls-remote HEAD == local HEAD), `pickProdSetupTemplate` 3-tier priority, `LastDeployedCommitSHA` field stamped from deploy_git_push, drift detection includes commit SHA | 1 commit | same |
| **P4** | `MergeStrategy` field, `existing-project-conflict-prompt` status, `launch-existing-project-conflict.md` atom, composer honors merge strategy, confirmDestructive integration on replace, ImportServices duplicate-hostname behavior verified | 1 commit | + live e2e |
| **P5** | `launch-scope-prompt.md` rewrite (multi-runtime + prereq), `launch-intro.md` update, spec `§10.1` + `§10.3` P-LP-10/P-LP-11, CLAUDE.md bullet | 1 commit | atom lint + golden regen |
