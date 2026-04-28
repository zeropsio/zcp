# Plan: Deploy strategy decomposition — orthogonal CloseDeployMode + GitPushState + BuildIntegration (2026-04-28)

> **Reader contract.** Self-contained for a fresh Claude session.
> Read end-to-end before starting Phase 0.
>
> **Sister plans (precursors)**:
> - `plans/archive/atom-corpus-hygiene-followup-2-2026-04-27.md` — atom corpus
>   hygiene cycle 3 (axis K/L/M/N enforcement, content trim)
> - `plans/engine-atom-rendering-improvements-2026-04-27.md` — E1-E5 engine
>   tickets (multiService:aggregate, failureClassification, axis lints)
>
> **This plan**: implements the structural refactor surfaced by the
> 2026-04-28 user dialogue + multi-round Codex investigation. Replaces the
> conflated `DeployStrategy + PushGitTrigger` topology with three orthogonal
> dimensions, introduces the `IsPushSource` predicate, splits the tool API,
> rewrites the push-git atom corpus, and gates auto-close by close mode.
> Fixes 19 of 21 root problems identified across handler / envelope / atom /
> test / spec layers (2 deferred to follow-up plans).

## 1. Problem

The current model `DeployStrategy ∈ {push-dev, push-git, manual} × PushGitTrigger ∈ {webhook, actions, unset}` conflates **three independent concerns**:

1. **What dev workflow auto-does at close** (deploy mechanism choice)
2. **Whether git-push capability is configured** (orthogonal — could exist for ad-hoc release pushes regardless of close action)
3. **What ZCP-managed build integration responds to git pushes at the remote** (utility — orthogonal; users may have their own external CI/CD that ZCP doesn't track)

This conflation manifests as 21 root problems documented across multi-round Codex investigation (2026-04-28 dialogue):

| # | Layer | Severity | Problem | Citation |
|---|---|---|---|---|
| R1 | Atom body | 🔴 | `develop-manual-deploy.md` shows `zerops_deploy` calls — contradicts spec D7 "agent informs only" | atom L14-17 vs `spec-workflows.md:788` |
| R2 | Atom corpus | 🔴 | `record-deploy` action is orphan — no atom mentions it | `workflow_record_deploy.go` vs grep empty |
| R3 | Envelope | 🔴 | `Deployed` field has split semantic (push-dev=build landed; push-git=git push succeeded) | `compute_envelope.go:206-210`, `compute_envelope.go:262-272`, `deploy_git_push.go:215-229` (Codex PRE-WORK 2026-04-28: stale `:243-256` citation corrected) |
| R4 | Tool path | 🔴 | Subdomain auto-enable SKIP for entire push-git path | `deploy_ssh.go:195-202` vs `deploy_git_push.go:215-232` |
| R5 | Tool path | 🔴 | Pre-flight harness softer for git-push (env vars, deployFiles, setup-match skip) | `deploy_ssh.go:139-158` |
| R6 | Handler | 🟡 | Container handler missing `trackTriggerMissingWarning` (local has it) | `deploy_local_git.go:212-296` vs container |
| R7 | Tool wire | 🟡 | Tool param `"git-push"` vs meta `"push-git"` — terminology drift | `deployStrategyGitPush` vs `topology.StrategyPushGit` |
| R8 | Topology | 🟡 | `trigger=actions` is conceptually push-dev (zcli push) hidden under push-git label | `strategy-push-git-trigger-actions.md:75` |
| R9 | Topology | 🟡 | `push-git+unset` ≡ `manual` runtime — duplicate state | Codex Q3 |
| R10 | Topology | 🟡 | `failureClassification` structurally absent in TimelineEvent for async builds | `events.go:28` |
| R11 | Atom narrative | 🟡 | No migration guidance (push-dev → push-git, etc.) | grep `develop-strategy-review.md` |
| R12 | Recipes | 🟡 | Recipes have ZERO push-git scaffolding | grep `internal/recipe/` |
| R13 | Tool path | 🟡 | Stale events hint directs agent to `zerops_deploy` for async build failure → loses context | `events.go:94` |
| R14 | Tool path | 🟡 | Spec D2a invariant ("first deploy must omit strategy") not tool-enforced | `develop-first-deploy-intro.md:24` vs `deploy_git_push.go:147` |
| R15-R18 | (duplicates of R9-R12) | — | Surfaced from different angles in earlier rounds | — |
| R19 | Atom body | 🟢 | "CI/CD" vs "trigger" terminology drift | `develop-push-git-deploy.md:33` |
| R20 | Atom body | 🟢 | Circular `{stage-hostname}` reference in `develop-push-git-deploy-local.md:35` | atom L35 |
| R21 | Atom body | 🟢 | Dead "First time setup" section in `develop-push-git-deploy.md` (atom has `deployStates:[deployed]`) | atom L14-23 |

The architectural drift propagates through:
- **Handler permissiveness**: `handleGitPush` accepts any `targetService`, no source-of-push validation
- **Envelope mis-propagation**: strategy=push-git on both halves of standard pair, both render `develop-push-git-deploy` atom (broken stage-half rendering)
- **Atom corpus omissions**: push-git atoms missing `modes:` filter; manual atom contradicts spec; record-deploy orphan
- **Tool API conflation**: `action=strategy` bundles 3 distinct setup operations
- **Auto-close gate too aggressive**: fires for manual mode where ZCP has no completion criterion
- **Subdomain auto-enable + pre-flight harness silently skipped** for git-push
- **Recipes have ZERO push-git scaffolding** (discovery gap)
- **"CI/CD" framing implies ZCP owns user's CI/CD** (false totalizing claim)

## 2. Goal

Three orthogonal dimensions in topology vocabulary; clean tool API decomposition; atom corpus restructured around the new model; handler validation enforces source-of-push semantics; auto-close gated by `CloseDeployMode`. 19/21 root problems resolved; 2 deferred (R10 failureClassification structural, R12 recipe push-git presence) as separate follow-up plans.

## 3. Mental model — three dimensions + lifecycle

### 3.1 Three orthogonal dimensions

```
┌─────────────────────────────────────────────────────────┐
│ DIMENSION 1 — CloseDeployMode (per-pair, ServiceMeta)   │
│ What dev workflow auto-does at close.                   │
│   unset    = not chosen yet                             │
│   auto     = direct push (zcli) — current push-dev      │
│   git-push = commit + push to external remote           │
│   manual   = ZCP yields; tools remain callable          │
│                                                         │
│ → drives "what fires at close" + atom rendering         │
│ → auto-close fires only when ∈ {auto, git-push}         │
│ → manual + unset = workflow stays open until explicit   │
└─────────────────────────────────────────────────────────┘
                       ┴ INDEPENDENT
┌─────────────────────────────────────────────────────────┐
│ DIMENSION 2 — GitPushState (per-pair, ServiceMeta)      │
│ Is git-push capability set up?                          │
│   unconfigured = not set up                             │
│   configured   = ready (GIT_TOKEN/.netrc/credentials,   │
│                  RemoteURL known)                       │
│   broken       = setup attempted but artifact damaged   │
│   unknown      = adopted/migrated, needs probe          │
│                                                         │
│ + RemoteURL string  (cache; runtime source = git origin)│
│                                                         │
│ → orthogonal — may exist regardless of CloseDeployMode  │
└─────────────────────────────────────────────────────────┘
                       ┴ INDEPENDENT (with prereq)
┌─────────────────────────────────────────────────────────┐
│ DIMENSION 3 — BuildIntegration (per-pair, ServiceMeta)  │
│ ZCP-managed integration that fires on remote git push.  │
│   none    = ZCP hasn't configured anything (user may    │
│             have their own external CI; ZCP doesn't     │
│             track)                                      │
│   webhook = Zerops dashboard OAuth (Zerops pulls+builds)│
│   actions = GitHub Actions runs zcli push (mechanically │
│             push-dev from CI)                           │
│                                                         │
│ Prerequisite: GitPushState == configured                │
│ → orthogonal vs CloseDeployMode (could be set even      │
│   when CloseDeployMode=auto, for ad-hoc release pushes) │
│ → fires on ANY git push hitting remote (not just        │
│   ZCP-initiated)                                        │
│ → UTILITY framing: ZCP just helps wire these specific   │
│   integrations; users may keep/define independent CI/CD │
└─────────────────────────────────────────────────────────┘
```

### 3.2 The `IsPushSource(Mode)` predicate

Topology-level derived predicate. Resolves "which hostname is the source for git-push operations" in pair scenarios.

```go
// internal/topology/types.go
func IsPushSource(m Mode) bool {
    switch m {
    case ModeStandard,    // dev half of standard pair
         ModeSimple,      // single-service container
         ModeLocalStage,  // local CWD as source, paired with Zerops stage
         ModeLocalOnly:   // local CWD as source, no Zerops link
        return true
    }
    return false  // ModeStage is build target, not source; ModeDev is invalid combo with push-git
}
```

Used by:
- `handleGitPush` — reject `targetService` that isn't a push source (with remediation pointing at correct dev hostname)
- Atom corpus `modes:` filter — push-git deploy/close atoms render only for push-source snapshots
- `compute_envelope` — could choose not to propagate `CloseDeployMode=git-push` as actionable to ModeStage snapshots (alternative: use modes filter only; envelope stays pure)

### 3.3 Interaction matrix

| CloseDeployMode | GitPushState | BuildIntegration | What happens at close | User CLI git push effect |
|---|---|---|---|---|
| `auto` | (any) | (any) | zcli push direct → build → deploy → verify | (independent — ZCP doesn't see) |
| `git-push` | configured | none | commit + push → archived at remote, no build | archived |
| `git-push` | configured | webhook | commit + push → Zerops auto-builds | Zerops builds |
| `git-push` | configured | actions | commit + push → Actions runs zcli push | Actions builds |
| `git-push` | NOT configured | (any) | ERROR: setup git-push first (returns setup atom) | (n/a) |
| `manual` | configured | webhook | nothing auto; workflow stays open | webhook builds |
| `manual` | (any) | (any) | nothing auto; user orchestrates via own hooks | depends on user setup |
| `unset` | (any) | (any) | guidance: choose CloseDeployMode | (n/a) |

### 3.4 Lifecycle scenarios

#### Scenario A: Fresh service → CloseDeployMode=auto (default path)

1. Bootstrap creates service: `{CloseDeployMode: unset, GitPushState: unconfigured, BuildIntegration: none}`
2. Develop workflow first run prompts CloseDeployMode choice
3. User picks `auto`: `action=close-mode closeMode={X:auto}`
4. Develop iterations: agent calls `zerops_deploy` → push-dev path → build → verify
5. At close: deploy+verify success per scope → auto-close fires → workflow ends

#### Scenario B: User switches auto → git-push (with setup)

1. Service has `CloseDeployMode=auto, GitPushState=unconfigured`
2. Agent: `action=close-mode closeMode={X:git-push}`
3. Handler detects `GitPushState != configured` → returns:
   - **Option a (strict)**: error "git-push not configured; run `action=git-push-setup` first" with setup-atom guidance
   - **Option b (chained, RECOMMENDED)**: succeeds the close-mode write but returns guidance pointing at git-push-setup as next-step
4. Agent walks through `action=git-push-setup` (prompts for repo URL, GIT_TOKEN if container env)
5. Successful setup → `GitPushState=configured, RemoteURL=<url>`
6. Next close: git-push path executes (commit + push)

**Decision pinned**: Option b (chained) — `close-mode` write succeeds but response carries setup guidance. Reasoning: separating state writes from setup actions; close-mode is a declared intent independent of capability availability.

#### Scenario C: CI/CD setup with prereq chain

1. Service has `CloseDeployMode=auto, GitPushState=unconfigured, BuildIntegration=none`
2. Agent: `action=build-integration service=X integration=webhook`
3. Handler checks `GitPushState != configured` → composes response:
   - Phase A: synthesize git-push-setup guidance (atom)
   - Phase B: synthesize webhook-setup guidance (atom)
   - Returns combined response: "Step 1: configure git-push (prereq); Step 2: webhook setup"
4. Agent walks through both
5. End state: `GitPushState=configured, BuildIntegration=webhook`
6. **Note**: BuildIntegration is set independent of CloseDeployMode. User must separately set CloseDeployMode=git-push if they want auto-close to push to remote. Otherwise `auto` close still works (zcli push), and user manually pushes for releases (which fires webhook).

#### Scenario D: Manual mode lifecycle

1. User sets `CloseDeployMode=manual` for service X
2. Develop session runs: agent edits, calls `zerops_deploy` (still works — tool is permissive about manual at meta level), verifies
3. WorkSession tracks attempts
4. **At end of work**: auto-close DOES NOT fire (gated by CloseDeployMode ∈ {auto, git-push})
5. WorkSession.AutoCloseProgress returns null with annotation "auto-close not applicable in manual mode"
6. Agent options:
   - Explicitly close: `action=close workflow=develop`
   - Start new workflow: `action=start workflow=develop intent=Y` → **REQUIRES Phase 6 fix** (today auto-deletes old session per `workflow_develop.go:101-114`). After fix: returns guidance "current manual-mode session is open; pass `force=true` to discard, or close it explicitly first"
7. User's external orchestration (custom slash command, Claude hook, script) may invoke ZCP tools at any time; ZCP doesn't track those

#### Scenario E: Standard pair + git-push

1. Pair `laraveldev (ModeStandard) + laravelstage (ModeStage)` with `CloseDeployMode=git-push, GitPushState=configured, BuildIntegration=webhook`
2. Both halves' meta is pair-keyed (same record)
3. Envelope produces 2 snapshots: laraveldev (Mode=ModeStandard, IsPushSource=true) + laravelstage (Mode=ModeStage, IsPushSource=false)
4. Atom rendering:
   - `develop-close-mode-git-push.md` (modes filter: standard|simple|local-stage|local-only) → fires ONLY for laraveldev
   - `develop-build-observe.md` (BuildIntegration != none) → fires once per pair (or marked `multiService: aggregate`)
   - General develop atoms (platform-rules, etc.) → fire for both halves as today
5. Agent close: SSH laraveldev, commit `/var/www`, push via `zerops_deploy targetService=laraveldev strategy=git-push`
6. Webhook fires on remote → Zerops builds laravelstage → agent observes via `zerops_events serviceHostname=laravelstage`
7. Verify pass → auto-close

#### Scenario F: Migration of existing meta

Old meta on disk: `DeployStrategy=push-git, PushGitTrigger=webhook, StrategyConfirmed=true, FirstDeployedAt=2026-04-01`

`migrateOldMeta()` runs at meta load:
- `CloseDeployMode = git-push` (mapped from push-git)
- `CloseDeployModeConfirmed = true` (mapped from StrategyConfirmed)
- `GitPushState = configured` (heuristic: was push-git AND FirstDeployedAt set → assume setup ran successfully at some point)
- `RemoteURL = ""` (data lost; fill from `git remote get-url origin` probe on next push, or surface "RemoteURL unknown" in atoms)
- `BuildIntegration = webhook` (mapped from PushGitTrigger)
- `LastPushAt`/`LastBuildLandedAt`: NOT introduced (over-engineering, dropped)

Edge cases:
- Old meta has `DeployStrategy=""` (truly never set): all new fields stay at zero values (`CloseDeployMode=unset, GitPushState=unconfigured, BuildIntegration=none`)
- Old meta has `DeployStrategy=push-git` but `FirstDeployedAt=zero`: `GitPushState=unknown` (probe needed before next push)

Old fields stay in struct for one cycle to enable migration. Phase 10 deletes them after rest stable.

#### Scenario G: Local-only + BuildIntegration=webhook

1. User has local-only project (no Zerops runtime linked, e.g., adopted from existing remote-only setup)
2. User sets `BuildIntegration=webhook`: allowed but warning surfaces:
   > "BuildIntegration=webhook configured but no Zerops runtime is linked to this project. Webhook will fire on git pushes but no build target exists. Link a runtime first: `action=adopt-local targetService=<hostname>`."
3. Webhook config requires `serviceId` — for local-only, the webhook setup atom should refuse (no serviceId to use) OR direct user to link a runtime first. **Decision pinned**: refuse webhook setup explicitly for local-only; allow only after a runtime is linked. (Adjust intent: BuildIntegration field stays but webhook is unsetuppable for local-only; field remains `none` until runtime linked.)

## 4. User-confirmed decisions

| ID | Decision | Status |
|---|---|---|
| F1 | `CloseModeAuto` semantic = direct push only (explicit choice). Auto-close gated by `CloseDeployMode ∈ {auto, git-push}`; manual/unset stay open until explicit close. | ✅ CONFIRMED 2026-04-28 |
| F2 | `BuildIntegration` per-pair (matches meta keying) | ✅ RECOMMENDED (pending explicit user nod, defaulted in Phase 0) |
| F3 | `RemoteURL` source of truth = git origin (meta = cache + warning on mismatch) | ✅ RECOMMENDED |
| F5 | Local-only + BuildIntegration=webhook: refuse webhook setup until runtime linked (refined from "allow with warning" to "refuse with redirect") | ✅ REFINED PER SCENARIO G |
| Naming D1 | `CloseDeployMode` (parallel with `topology.Mode`) | ✅ RECOMMENDED |
| Naming D2 | `GitPushState` (enum, not bool) | ✅ RECOMMENDED |
| Naming D3 | `BuildIntegration` (utility framing, not "trigger") | ✅ CONFIRMED 2026-04-28 |
| Migration | Auto-migrate at meta load; old fields stay one cycle then deleted | ✅ RECOMMENDED |
| Drop LastBuildLandedAt | Over-engineering; verify is canonical "alive" signal; existing FirstDeployedAt + WorkSession suffices | ✅ CONFIRMED 2026-04-28 |
| Atom prereq chaining | Handler-side composition (not engine atom chaining) | ✅ DESIGN PINNED |

## 5. Baseline snapshot (2026-04-28)

### 5.1 Symbol blast radius (per Codex)

- **34 internal files / 173 hits** (symbol grep: `DeployStrategy`, `PushGitTrigger`, etc.)
- **40 atom files / 183 hits** (string grep: `push-git`, `push-dev`, `manual`)
- **101 internal files / 664 hits** (broader — includes test fixtures, docs)
- Estimated implementation: **~700-1200 LOC** across phases

### 5.2 Test fixtures inventory

- `internal/workflow/scenarios_test.go:809-862` — push-git scenarios (single-snapshot fixtures only; missing two-snapshot pair coverage that would expose double-render)
- `internal/workflow/corpus_coverage_test.go:634-708` — strategy/trigger matrix coverage
- `internal/workflow/synthesize_test.go:746-790` — strategy-setup synthesis
- `internal/workflow/bootstrap_outputs_test.go:424` — bootstrap writes empty DeployStrategy

### 5.3 Atom corpus inventory (push-git family)

Current 9 push-git atoms + 5 trigger atoms (state confirmed via `ls internal/content/atoms/`):
- `strategy-push-git-intro.md`, `strategy-push-git-push-{container,local}.md`, `strategy-push-git-trigger-{webhook,actions}.md`
- `develop-push-git-deploy{,-local}.md`, `develop-close-push-git-{container,local}.md`

After this plan: **12 atoms total** (4 setup + 4 develop close-mode + 1 build-observe + 3 narrative/migration). Net change: −2 atoms, 9 atoms heavily restructured, 5 net new.

## 6. Phased execution

> **Pacing rule**: 12 phases (0-11). Each phase ENTRY/WORK-SCOPE/EXIT criteria. Codex protocol per §7. Trackers per `phase-N-tracker.md` schema (created during execution).
>
> **Critical path discipline**: each phase ends with verify gate green (`make lint-local && go test ./... -short -race`). No phase begins until prior EXIT criteria satisfied. No "I'll fix it later" — broken state at phase boundary = stop, fix, retry.

### Phase 0 — Calibration

**ENTRY**: working tree clean; HEAD on main; this plan committed.

**WORK-SCOPE**:
1. Read this plan end-to-end. Walk §3 mental model + §4 decisions.
2. Verify env: `make setup` → `make lint-fast` green → `go test ./... -short` green.
3. Snapshot baseline:
   - `git rev-parse HEAD > plans/strategy-decomp/baseline-head.txt`
   - Probe atom corpus size: `wc -l internal/content/atoms/*.md > plans/strategy-decomp/baseline-atoms.txt`
   - List all `DeployStrategy`/`PushGitTrigger` call sites: `grep -rn "DeployStrategy\|PushGitTrigger\|StrategyPushGit\|StrategyPushDev\|StrategyManual" internal/ docs/ > plans/strategy-decomp/baseline-callsites.txt`
4. Init tracker dir: `plans/strategy-decomp/` with `phase-0-tracker.md`
5. **Codex PRE-WORK round** (mandatory): hand Codex this plan + ask "validate against current corpus state — any decision in §4 already invalidated by post-2026-04-27 commits? Any atom that would already render the proposed shape?"

**EXIT**:
- Baseline files committed to `plans/strategy-decomp/`
- Codex PRE-WORK APPROVE
- `phase-0-tracker.md` committed
- Verify gate green

### Phase 1 — Topology types + IsPushSource predicate + atom parser axes

**ENTRY**: Phase 0 EXIT satisfied.

**WORK-SCOPE**:
0. **Atom parser extension first** (per Codex final review — without this, Phase 8 atom parsing will FAIL on unknown frontmatter keys per `internal/workflow/atom.go:115`):
   - Add `closeDeployModes`, `gitPushStates`, `buildIntegrations` to valid frontmatter keys in `internal/workflow/atom.go::validFrontMatterKeys`
   - Add corresponding parsers (`parseCloseDeployModes`, `parseGitPushStates`, `parseBuildIntegrations`) following the `parseModes`/`parseTriggers` pattern at `atom.go:466,495`
   - Extend `KnowledgeAtom` struct with new axis slices
   - Update `Synthesize` filter logic in `internal/workflow/synthesize.go` to consider new axes
   - Add atom-lint hooks in `internal/content/atoms_lint.go` for the new axes
   - Pin parser tests for the new axes
1. Add to `internal/topology/types.go`:
   ```go
   type CloseDeployMode string
   const (
       CloseModeUnset    CloseDeployMode = "unset"
       CloseModeAuto     CloseDeployMode = "auto"
       CloseModeGitPush  CloseDeployMode = "git-push"
       CloseModeManual   CloseDeployMode = "manual"
   )

   type GitPushState string
   const (
       GitPushUnconfigured GitPushState = "unconfigured"
       GitPushConfigured   GitPushState = "configured"
       GitPushBroken       GitPushState = "broken"
       GitPushUnknown      GitPushState = "unknown"
   )

   type BuildIntegration string
   const (
       BuildIntegrationNone     BuildIntegration = "none"
       BuildIntegrationWebhook  BuildIntegration = "webhook"
       BuildIntegrationActions  BuildIntegration = "actions"
   )

   func IsPushSource(m Mode) bool {
       switch m {
       case ModeStandard, ModeSimple, ModeLocalStage, ModeLocalOnly:
           return true
       }
       return false
   }
   ```
2. Add `internal/topology/types_test.go` (or extend existing) pinning predicate truth table for all 6 Mode values × IsPushSource result.
3. Add `internal/topology/doc.go` doc-comment update if exists; explain orthogonality.
4. **DO NOT** remove old `DeployStrategy`, `StrategyPushGit`, etc. yet — they coexist for migration.
5. Verify gate green.
6. Commit (split as 2 commits for clarity):
   - `atom(P1): extend parser to support closeDeployModes/gitPushStates/buildIntegrations axes`
   - `topology(P1): add CloseDeployMode + GitPushState + BuildIntegration + IsPushSource predicate`

**EXIT**:
- New topology types compile
- IsPushSource truth table pinned
- Old types still present (zero churn elsewhere)
- Atom parser accepts new frontmatter axis keys without rejecting valid atoms
- Atom-lint hooks for new axes present (rules can be empty stubs initially)
- `phase-1-tracker.md` committed

**Risk**: LOW. Pure addition.

### Phase 2 — ServiceMeta migration

**ENTRY**: Phase 1 EXIT satisfied.

**WORK-SCOPE**:
1. Extend `ServiceMeta` in `internal/workflow/service_meta.go`:
   ```go
   type ServiceMeta struct {
       // ... existing identity/lifecycle fields ...
       
       // NEW: 3 orthogonal dimensions
       CloseDeployMode          topology.CloseDeployMode  `json:"closeDeployMode,omitempty"`
       CloseDeployModeConfirmed bool                       `json:"closeDeployModeConfirmed,omitempty"`
       GitPushState             topology.GitPushState      `json:"gitPushState,omitempty"`
       RemoteURL                string                     `json:"remoteUrl,omitempty"`
       BuildIntegration         topology.BuildIntegration  `json:"buildIntegration,omitempty"`
       
       // EXISTING (deprecated, deleted in Phase 10):
       DeployStrategy    topology.DeployStrategy   `json:"deployStrategy,omitempty"`
       PushGitTrigger    topology.PushGitTrigger   `json:"pushGitTrigger,omitempty"`
       StrategyConfirmed bool                      `json:"strategyConfirmed,omitempty"`
       
       // FirstDeployedAt UNCHANGED — semantic stays loose ("any successful deploy attempt")
   }
   ```
2. Implement `migrateOldMeta(meta *ServiceMeta)` in `service_meta.go`:
   - Maps old DeployStrategy → CloseDeployMode (push-dev→auto, push-git→git-push, manual→manual, ""→unset)
   - Maps old PushGitTrigger → BuildIntegration (webhook→webhook, actions→actions, unset/""→none)
   - Maps StrategyConfirmed → CloseDeployModeConfirmed
   - Heuristic GitPushState: was push-git + FirstDeployedAt set → configured; was push-git + no FirstDeployedAt → unknown; else unconfigured
   - RemoteURL stays empty (not in old meta; fills on next push or probe)
3. Call `migrateOldMeta` from **ALL** meta read paths — not just `ReadServiceMeta`. Per Codex audit: `parseMeta` is also called by `ListServiceMetas` and downstream `ManagedRuntimeIndex` (`internal/workflow/service_meta.go:218,240,268,123`). If migration only hooks `ReadServiceMeta`, `ListServiceMetas` returns un-migrated metas → router/envelope sees mixed state → silent corruption. Hook `parseMeta` itself (single point of integration).
4. Update `bootstrap_outputs.go` to write new fields with their defaults.
5. Update `service_meta_test.go`: pin migrateOldMeta() truth table for all 4×3 combinations of (DeployStrategy × PushGitTrigger).
6. Update existing tests that reference old fields — read fields on both old and new accessors during one-cycle window.
7. Verify gate green.
8. Commit: `meta(P2): add CloseDeployMode/GitPushState/BuildIntegration + migrateOldMeta`

**EXIT**:
- New fields persist + load correctly
- migrateOldMeta truth table pinned
- All existing tests pass with auto-migration
- Bootstrap writes new defaults
- `phase-2-tracker.md` committed

**Risk**: MEDIUM. Migration is the load-bearing piece. Test coverage critical.

### Phase 3 — Envelope + router

**ENTRY**: Phase 2 EXIT satisfied.

**WORK-SCOPE**:
1. Update `compute_envelope.go::buildOneSnapshot`:
   - Read new fields (CloseDeployMode, GitPushState, BuildIntegration) from meta
   - Add to ServiceSnapshot via new fields
   - Existing `Strategy`/`Trigger` fields stay (read from migrated meta) — deleted in Phase 10
2. Update `router.go::Route`:
   - Hint generation uses CloseDeployMode (was DeployStrategy)
   - Dominant-strategy detection (line 169) uses CloseDeployMode
3. Update `envelope.go::ServiceSnapshot` struct:
   - New fields: CloseDeployMode, GitPushState, BuildIntegration, RemoteURL
   - Old `Strategy`/`Trigger` stay (one cycle)
4. Update `synthesize.go::SynthesizeStrategySetup` if needed (axes match new fields).
5. Update `scenarios_test.go`: extend fixture for `develop-active/git-push/standard/container` to TWO snapshots (dev + stage) so future double-render bugs would be caught. Pin: only ONE rendering of `develop-close-mode-git-push.md` with dev hostname in body.
6. Verify gate green; pin coverage tests pass.
7. Commit: `envelope(P3): propagate new dimensions; router uses CloseDeployMode; pin two-snapshot fixture`

**EXIT**:
- ServiceSnapshot exposes new fields
- Router hints use CloseDeployMode
- Two-snapshot fixture pins single-render with dev hostname
- `phase-3-tracker.md` committed

**Risk**: MEDIUM. Affects atom rendering surface — must keep all existing scenarios green.

### Phase 4 — Handler updates

**ENTRY**: Phase 3 EXIT satisfied.

**WORK-SCOPE**:
1. `internal/tools/deploy_git_push.go::handleGitPush`:
   - **Add (R-source)**: `IsPushSource(meta.RoleFor(targetService))` validation → if false, return `ErrInvalidParameter` with remediation:
     ```
     "git-push target must be source-of-push (dev half of pair, simple, or local CWD); '{stage}' is the build target. 
     Push from '{dev-hostname}' instead: zerops_deploy targetService='{dev}' strategy='git-push'"
     ```
   - **Add (R-state)**: `meta.GitPushState != GitPushConfigured` → return `ErrPrerequisiteMissing`:
     ```
     "git-push not configured for service '{X}'. Run zerops_workflow action='git-push-setup' service='{X}' first."
     ```
   - **Add (R5)**: env-var pre-flight validation (currently skipped per `deploy_ssh.go:139` comment "no zerops.yaml needed" which is misleading — yaml IS validated, but env vars are not). Validate env-var references via `ops.ValidateEnvVars` (extend if needed).
   - **Add (R6)**: `trackTriggerMissingWarning` parity — if push succeeds but `BuildIntegration=none`, surface warning identical to `deploy_local_git.go:212`.
   - On success: stamp `LastPushAt` (existing FirstDeployedAt path stays as-is; `LastPushAt` field is NOT added — see scenario F note that we dropped split). Actually: stamping stays as today (RecordDeployAttempt → stampFirstDeployedAt).
2. `internal/tools/deploy_local_git.go::handleLocalGitPush`:
   - **Add (R-state)**: same `GitPushState != configured` pre-flight (consistent with container).
3. `internal/tools/deploy_ssh.go` (push-dev path):
   - No structural change — synchronous build observation already stamps state correctly.
   - Soft warning: if user-passed `strategy="git-push"` doesn't match `meta.CloseDeployMode=git-push`, log informationally (not blocking).
4. **Codex PER-EDIT round** (MANDATORY for handler changes — HIGH-risk).
5. Update handler tests + integration tests for new gates.
6. Verify gate green.
7. Commit: `handler(P4): IsPushSource validation + GitPushState pre-flight + env-var pre-flight + trackTriggerMissingWarning parity`

**EXIT**:
- handleGitPush rejects ModeStage targetService with remediation
- handleGitPush rejects unconfigured GitPushState with setup pointer
- env-var validation runs for git-push path
- trackTriggerMissingWarning fires for both env handlers
- Codex PER-EDIT APPROVE
- `phase-4-tracker.md` committed

**Risk**: HIGH. Handler is the actual path agents hit. Regression risk significant.

### Phase 5 — Tool API split

**ENTRY**: Phase 4 EXIT satisfied.

**WORK-SCOPE**:
1. Add new actions in `internal/tools/workflow.go`:
   - `action="close-mode" closeMode={hostname:auto|git-push|manual|unset}` → handler: `handleCloseMode`
   - `action="git-push-setup" service=hostname` → handler: `handleGitPushSetup` (interactive walkthrough, sets GitPushState=configured on success)
   - `action="build-integration" service=hostname integration=webhook|actions|none` → handler: `handleBuildIntegration` (auto-runs git-push-setup if `GitPushState != configured`)
2. Implement `handleCloseMode` in new file `internal/tools/workflow_close_mode.go`:
   - Validates value
   - Writes meta.CloseDeployMode + CloseDeployModeConfirmed=true
   - If switching TO `git-push` and `GitPushState != configured`, response includes setup-guidance pointer
3. Implement `handleGitPushSetup` in new file `internal/tools/workflow_git_push_setup.go`:
   - Env-aware (container = GIT_TOKEN setup, local = origin URL check)
   - Synthesizes `setup-git-push-{container,local}` atom
   - On confirmed setup: writes `GitPushState=configured, RemoteURL=...`
4. Implement `handleBuildIntegration` in new file `internal/tools/workflow_build_integration.go`:
   - Pre-check `GitPushState`
   - If unconfigured: composes git-push-setup THEN build-integration setup atoms in single response (handler-side prereq chaining)
   - On user confirm: writes `BuildIntegration=...`
5. **REMOVE `action=strategy`** from `workflow.go` — pre-prod, no compat alias.
6. Update tests: `workflow_test.go`, `workflow_strategy_test.go` (rename or replace), integration tests.
7. **Codex PER-EDIT round** (MANDATORY).
8. Verify gate green.
9. Commit: `api(P5): split action=strategy into close-mode + git-push-setup + build-integration; prereq chaining`

**EXIT**:
- 3 new actions registered + tested
- Old action=strategy removed
- Build-integration auto-chains git-push-setup when needed
- Codex PER-EDIT APPROVE
- `phase-5-tracker.md` committed

**Risk**: HIGH. API surface change. All callers (atoms, recipes, docs) must align in subsequent phases.

### Phase 6 — Auto-close gate by CloseDeployMode + handleDevelopBriefing manual-mode behavior

**ENTRY**: Phase 5 EXIT satisfied.

**WORK-SCOPE**:
1. Update `internal/workflow/work_session.go::AutoCloseProgressOf` (or equivalent):
   - Add gate: `if meta.CloseDeployMode != CloseModeAuto && meta.CloseDeployMode != CloseModeGitPush { return autoCloseDisabled }`
   - For `manual` or `unset`: return progress with `autoCloseEnabled: false` + annotation "manual close mode; explicit action=close required" or "close mode unset; pick via action=close-mode"
2. Update `WorkSession.AutoCloseProgress` shape if needed (add `enabled` field).
3. Update auto-close fire logic — only check completion criteria when gate passes.
4. Update `deploy_local.go` + `deploy_git_push.go` response wrappers — `sessionAnnotations` returns the new shape.
5. **(Per Codex final review) Address `handleDevelopBriefing` auto-delete behavior**: today, calling `action=start workflow=develop intent=Y` while session is active **deletes the old work session and creates a fresh one** for the same PID (`internal/tools/workflow_develop.go:101,109,114`). This contradicts the manual-mode "stays open until explicit close" semantic. Update logic:
   - If current session's services have `CloseDeployMode ∈ {manual, unset}`: do NOT auto-delete; return guidance "current session is in manual/unset mode; explicitly close it via `action=close` before starting a new intent, OR if you intend to discard, pass `force=true`"
   - For other close modes (auto/git-push): existing auto-delete behavior is fine (workflow has clear completion criterion; new intent is OK to supersede)
6. Test: scenarios with manual mode service should NOT auto-close even after deploy+verify success; new-intent on manual session should NOT auto-delete.
7. Verify gate green.
8. Commit: `workflow(P6): auto-close gated by CloseDeployMode + handleDevelopBriefing respects manual mode`

**EXIT**:
- Auto-close fires only for auto/git-push close modes
- manual/unset workflows stay open until explicit close
- handleDevelopBriefing prompts (not silently deletes) for manual/unset mode
- AutoCloseProgress messaging differentiated
- `phase-6-tracker.md` committed

**Risk**: MEDIUM-HIGH. Behavioral change visible in develop close UX + new-intent UX. Codex POST-WORK round mandatory.

### Phase 7 — record-deploy enhancement + atoms

**ENTRY**: Phase 6 EXIT satisfied.

**WORK-SCOPE**:
1. Update `internal/tools/workflow_record_deploy.go::handleRecordDeploy`:
   - On successful stamp (stamped=true), call `maybeAutoEnableSubdomain` (if applicable for the meta's mode)
   - Returns `subdomainAccessEnabled: true/false` in response
2. Document atom: create `develop-record-external-deploy.md`:
   - Front matter: `phases: [develop-active]`, `deployStates: [deployed]` (or perhaps fires on async build observation triggers — TBD axis)
   - Body: explains record-deploy as canonical bridge for external/async build observation. Surfaces it for `BuildIntegration=webhook|actions` scenarios when agent has confirmed via zerops_events that build landed.
3. Update existing atoms that should reference record-deploy (e.g., `develop-build-observe.md` from Phase 8).
4. Verify gate green.
5. Commit: `record-deploy(P7): also auto-enable subdomain + atom guidance`

**EXIT**:
- record-deploy auto-enables subdomain for first-deploy-eligible modes
- New atom `develop-record-external-deploy.md` in corpus
- `phase-7-tracker.md` committed

**Risk**: LOW.

### Phase 8 — Atom corpus restructure + corpus-wide stage-leakage audit

**ENTRY**: Phase 7 EXIT satisfied. Atom parser already supports new axes (Phase 1).

**WORK-SCOPE**: Substantial atom rewrite. Estimated 12 atoms touched + corpus-wide audit.

**0. Corpus-wide stage-leakage audit FIRST** (per Codex final review — `develop-close-push-dev-local.md:6` has `modes: [dev, stage]` showing this isn't unique to push-git family):
   - Grep ALL atoms for `strategies:` axis without `modes:` filter
   - For each: evaluate whether the atom should restrict to `IsPushSource` modes
   - Document findings in `plans/strategy-decomp/atom-leakage-audit.md` before any atom edits
   - Apply `modes:` filter additions to non-push-git atoms that need them
   - Codex POST-WORK review of audit results

#### Setup atoms (4):
1. **NEW** `setup-git-push-container.md`: GIT_TOKEN, .netrc safety, repo URL, commit pipeline. Front matter: `phases: [strategy-setup]`, `environments: [container]`, `gitPushStates: [unconfigured, broken, unknown]`.
2. **NEW** `setup-git-push-local.md`: user git credentials, repo URL. Front matter: `phases: [strategy-setup]`, `environments: [local]`, `gitPushStates: [...]`.
3. **NEW** `setup-build-integration-webhook.md`: Zerops dashboard OAuth walkthrough. UTILITY framing — explicitly mention "ZCP-managed integration; you can have other CI/CD independently".
4. **NEW** `setup-build-integration-actions.md`: GitHub Actions workflow file. Same framing as webhook.

#### Develop atoms (4):
5. **NEW** `develop-close-mode-auto.md`: close = auto/zcli push direct (current push-dev close). Front matter: `phases: [develop-active]`, `closeDeployModes: [auto]`, `modes: [standard, simple]` (IsPushSource).
6. **NEW** `develop-close-mode-git-push.md`: close = git-push. Front matter: `phases: [develop-active]`, `closeDeployModes: [git-push]`, `modes: [standard, simple, local-stage, local-only]` (IsPushSource).
7. **NEW** `develop-close-mode-manual.md`: extension slot framing. **NO `zerops_deploy` commands in body** — just acknowledges tools remain callable. Front matter: `closeDeployModes: [manual]`.
8. **NEW** `develop-build-observe.md`: async build observability via zerops_events. Front matter: `closeDeployModes: [git-push]` + `buildIntegrations: [webhook, actions]`.

#### Discovery / migration atoms (3 + refresh existing):
9. **REFRESH** `develop-strategy-review.md`: list current state across all 3 dimensions (CloseDeployMode + GitPushState + BuildIntegration). Replace single-strategy framing.
10. **NEW** `develop-promote-to-git-push.md`: migration narrative for switching auto → git-push (covers prereq chain).
11. **NEW** `develop-add-build-integration.md`: guidance for adding CI/CD without changing close mode (orthogonality).

#### Atoms to DELETE (after restructure):
- `develop-push-git-deploy.md` (replaced by close-mode-git-push)
- `develop-push-git-deploy-local.md` (replaced)
- `develop-close-push-git-container.md` (replaced)
- `develop-close-push-git-local.md` (replaced)
- `develop-manual-deploy.md` (replaced by close-mode-manual)
- `develop-close-manual.md` (consider merge into close-mode-manual)
- `strategy-push-git-intro.md` (functionality split into setup atoms)
- `strategy-push-git-push-{container,local}.md` (replaced by setup-git-push-*)
- `strategy-push-git-trigger-{webhook,actions}.md` (replaced by setup-build-integration-*)

#### Atom front-matter axis additions:
- `closeDeployModes:` axis (parallel to existing `strategies:`)
- `gitPushStates:` axis
- `buildIntegrations:` axis
- Engine `internal/workflow/atom.go::parseAtom` extended; `internal/content/atoms_lint.go` extended.

#### Tests:
- `corpus_coverage_test.go`: update fixtures for new axis values
- `scenarios_test.go`: pin renderings for new combos
- `atom_test.go`: front-matter parser tests

#### Codex involvement:
- **PER-EDIT MANDATORY** for atoms with HIGH risk (close-mode atoms, build-integration setup atoms — load-bearing for agent guidance)
- **POST-WORK round**: verify no Axis K/L/M/N regressions per recent atom hygiene work

**EXIT**:
- 12 new/refreshed atoms in corpus
- 9 old atoms removed
- All scenario tests pass
- Atom lint passes
- Codex PER-EDIT + POST-WORK APPROVE
- `phase-8-tracker.md` committed (this phase may need sub-trackers per atom)

**Risk**: HIGH. Largest LOC delta. Atom semantics are user-facing.

### Phase 9 — Misc fixes

**ENTRY**: Phase 8 EXIT satisfied.

**WORK-SCOPE** — small focused fixes deferred from earlier rounds:
1. **R13 — Stale events hint**: `internal/ops/events.go:94` — update hint to point at `zerops_logs` and `zerops_verify` for build context (not `zerops_deploy` which would loop).
2. **R14 — D2a invariant tool enforcement**: add explicit FirstDeployedAt check in `handleGitPush` if not already covered by GitPushState pre-flight. Decision: existing committed-code check + GitPushState already cover; no additional enforcement needed.
3. **R7 — Tool wire vs meta terminology**: deploy tool param value `"git-push"` matches meta value `"git-push"` after this refactor (CloseDeployMode uses `"git-push"`; tool param can keep `"git-push"`). Confirm consistency in `deploy_strategy_gate.go`.
4. **Spec updates**: `docs/spec-workflows.md` — update §4.3 strategy section to reflect new model. Update §8 invariants D2a, D7, S1 et al.
5. **CLAUDE.md updates**: add new invariants for CloseDeployMode auto-close gate, IsPushSource predicate, BuildIntegration utility framing.
6. Verify gate green.
7. Commit: `misc(P9): events hint fix + spec/CLAUDE updates`

**EXIT**:
- Events hint corrected
- Spec + CLAUDE.md aligned with new model
- `phase-9-tracker.md` committed

**Risk**: LOW.

### Phase 10 — Old field removal + cleanup

**ENTRY**: Phases 0-9 EXIT satisfied; ALL existing tests pass with new model.

**WORK-SCOPE**:
1. Remove deprecated fields from `ServiceMeta`: `DeployStrategy`, `PushGitTrigger`, `StrategyConfirmed`.
2. Remove `migrateOldMeta()` function (one cycle done).
3. Remove deprecated topology constants: `topology.StrategyPushDev`, `StrategyPushGit`, `StrategyManual`, `PushGitTrigger`, `TriggerUnset`, `TriggerWebhook`, `TriggerActions` (or rename — see naming decisions).
4. Sweep for stragglers via grep — should return zero hits.
5. Remove deprecated `ServiceSnapshot.Strategy/Trigger` fields.
6. Remove old atoms (already done in Phase 8 — confirm).
7. Update `bootstrap_outputs.go::test 424` — assertion now checks new field.
8. Verify gate green; lint passes; full test suite passes.
9. Commit: `cleanup(P10): remove deprecated DeployStrategy/PushGitTrigger fields and constants`

**EXIT**:
- Zero references to old field names
- Zero references to old topology constants
- Test suite all green
- `phase-10-tracker.md` committed

**Risk**: LOW (after rest stable). HIGH if executed before Phases 0-9 stabilize.

### Phase 11 — SHIP

**ENTRY**: Phase 10 EXIT satisfied.

**WORK-SCOPE**:
1. Re-run full test suite: `go test ./... -race -count=1`.
2. Re-run lint: `make lint-local`.
3. Composition re-score on atom corpus (vs Phase 0 baseline) — atom byte counts, scenario coverage delta.
4. **Codex FINAL-VERDICT round**: hand Codex the final state + delta from Phase 0 baseline. Ask for SHIP / SHIP-WITH-NOTES / NOSHIP verdict.
5. Update `CHANGELOG.md` if exists; otherwise create release notes summary.
6. **DO NOT** call `make release` — user controls release timing per CLAUDE.local.md.
7. Archive plan: `git mv plans/deploy-strategy-decomposition-2026-04-28.md plans/archive/`.
8. Commit: `PLAN COMPLETE: deploy strategy decomposition — SHIP`.

**EXIT**:
- Codex SHIP verdict recorded
- Plan archived
- All gates green
- `phase-11-tracker.md` committed

**Target SHIP outcome**: clean SHIP. SHIP-WITH-NOTES acceptable if R10 (failureClassification structural) or R12 (recipe push-git) explicitly deferred to follow-up plan with cross-link.

## 7. Codex collaboration protocol

Inherits cycle 1 §10 (atom-corpus-hygiene plan) protocol; phase-specific:

| Phase | Codex round | Mandatory? | Scope |
|---|---|---|---|
| 0 | PRE-WORK | Yes | Validate plan against current corpus |
| 1 | (none) | No | LOW risk |
| 2 | POST-WORK | Yes | Migration truth table review |
| 3 | POST-WORK | Yes | Envelope/router cross-check |
| 4 | PER-EDIT | **Yes** | HIGH risk handler changes |
| 5 | PER-EDIT | **Yes** | HIGH risk API change |
| 6 | POST-WORK | Yes | Auto-close gate semantic |
| 7 | (none) | No | LOW risk |
| 8 | PER-EDIT + POST-WORK | **Yes** | HIGH risk atom corpus |
| 9 | (none) | No | LOW risk |
| 10 | POST-WORK | Yes | Cleanup verification |
| 11 | FINAL-VERDICT | **Yes** | SHIP gate |

Estimated Codex rounds: ~8-10 across plan execution. Heavy on Phases 4, 5, 8.

## 8. Acceptance criteria (G1-G8 ship gates)

- **G1**: All 12 phases (0-11) closed per §6 EXIT criteria.
- **G2**: Full test suite green (`go test ./... -race -count=1` + `make lint-local`).
- **G3**: 19 of 21 root problems addressed (per §1 table). 2 deferred (R10 failureClassification; R12 recipe push-git) explicitly cross-linked to follow-up plan stubs.
- **G4**: Codex FINAL-VERDICT = SHIP or SHIP-WITH-NOTES.
- **G5**: Two-snapshot fixture for `develop-active/git-push/standard/container` pins single-render with dev hostname (regression guard for original P1 problem).
- **G6**: All atom front-matter axes lint-clean (Axis K/L/M/N from prior cycles).
- **G7**: spec-workflows.md + CLAUDE.md aligned with new model.
- **G8**: Zero references to deprecated `DeployStrategy/PushGitTrigger/StrategyConfirmed` in `internal/`.

## 9. Out of scope (deferred to follow-up plans)

1. **R10 — failureClassification structural for async builds**: extending `TimelineEvent` to carry failureClassification field requires engine work in `internal/ops/events.go` + classifier integration. Separate plan: `plans/timeline-event-failureclass-2026-05-XX.md` (TBD).

2. **R12 — Recipes' push-git presence**: adding push-git scaffolding to recipes is recipe-engine work + per-recipe template authoring. Recipes currently default to push-dev with `strategy: unset` post-bootstrap; user/agent picks during develop. Separate plan if/when prioritized.

3. **CI/CD provider expansion**: only webhook + actions supported. GitLab CI, Bitbucket Pipelines, Jenkins integration would extend BuildIntegration enum but is separate scope.

4. **Polling-based build observation**: relying on agent calling `record-deploy` after observing events. Engine-side polling for async build completion is a separate UX enhancement.

## 10. Anti-patterns + risks

- **Don't merge phases**: each phase ends in clean state for verifiability. Combining Phase 4 (handler) and Phase 5 (API) loses the safety of incremental verification.
- **Don't skip Codex PER-EDIT rounds on Phases 4, 5, 8**: HIGH-risk changes need second eyes. The cost of a missed atom contradiction or handler regression compounds across subsequent phases.
- **Don't leave migrateOldMeta() in code after Phase 10**: it's transitional infrastructure; permanence creates dual-path debt.
- **Don't add `LastBuildLandedAt` or `LastPushAt` later**: explicitly dropped as over-engineering. Verify is the canonical "alive" signal; FirstDeployedAt covers "has been deployed at all". If a future need for these fields surfaces, it must justify adding new state instead of using verify/events.
- **Don't conflate trigger=actions with true push-git**: atom guidance must be precise — webhook = Zerops pulls; actions = user CI runs zcli push (mechanically push-dev). Same BuildIntegration field, different mechanics.
- **Don't break the orthogonality framing**: BuildIntegration=webhook is valid even with CloseDeployMode=auto (user manually pushes for releases, webhook fires). Atoms must not imply "CI/CD requires git-push close mode".
- **Don't auto-flip CloseDeployMode**: setting BuildIntegration=webhook does NOT change CloseDeployMode. They're independent dimensions.
- **Don't claim "no build will fire" when BuildIntegration=none**: user may have external CI/CD that ZCP doesn't track. Atom language: "no ZCP-managed integration" not "no build will fire".
- **Don't use `t.Parallel()` in tests touching meta files**: meta is filesystem state, race-prone.
- **Don't commit phase EXIT without verify gate green**: rebuild discipline is the load-bearing safety net.
- **Don't skip the two-snapshot fixture in Phase 3**: it's the regression guard for the original double-render bug. Without it, future atom edits could re-introduce the same defect.
- **Don't add atom frontmatter axes in Phase 8 without parser support from Phase 1**: `internal/workflow/atom.go:115` has closed valid frontmatter keys; unknown axes cause parse errors that break ALL atom rendering. Parser extension is Phase 1 prereq.
- **Don't only hook `migrateOldMeta` to `ReadServiceMeta`**: `ListServiceMetas` calls `parseMeta` directly (`service_meta.go:240`); router/envelope use `ManagedRuntimeIndex` over the list (`service_meta.go:123`). Hook `parseMeta` (single integration point) so migration covers all read paths.
- **Don't make auto-close depend on `FirstDeployedAt`**: current readiness uses LATEST deploy+verify attempts (`work_session.go:398`). Adding FirstDeployedAt as a gate would mean services that lost their first-deploy stamp (rare but possible) could never auto-close again.
- **Don't audit only push-git atoms for stage leakage**: `develop-close-push-dev-local.md:6` has `modes: [dev, stage]` — pattern exists corpus-wide. Phase 8 audit must cover ALL atoms with `strategies:` filter and no `modes:` axis, not just the push-git family.

## 11. First moves for fresh instance

**Step 0 — prereq verification**:
1. `git status` → clean tree
2. `git rev-parse HEAD` → main branch
3. `make lint-fast` → green
4. `go test ./... -short` → green

**Step 1 — read context**:
1. This plan end-to-end (top to bottom; do not skim).
2. `plans/archive/atom-corpus-hygiene-followup-2-2026-04-27.md` — sister plan structure + Codex protocol patterns.
3. `internal/topology/types.go` — current topology state.
4. `internal/workflow/service_meta.go` — current ServiceMeta shape.
5. `internal/tools/workflow_strategy.go` — current action=strategy handler (to be replaced).
6. `internal/tools/deploy_git_push.go` — current handleGitPush (to be hardened).
7. `internal/content/atoms/develop-push-git-deploy.md` + `develop-close-push-git-container.md` — atoms to be replaced.
8. CLAUDE.md + CLAUDE.local.md — project conventions.

**Step 2 — initialize tracker dir**: `mkdir -p plans/strategy-decomp/` with `phase-0-tracker.md`.

**Step 3 — Phase 0 PRE-WORK Codex round** validating the plan against current corpus state. Phase 0 starts only after APPROVE.

**Step 4 — Begin Phase 1** (topology types + IsPushSource).

## 12. Open questions / TBD

1. **Atom front-matter axis name**: should new axes be `closeDeployModes` (long but parallel) or `closeModes` (shorter)? Same for `gitPushStates`/`buildIntegrations`. Decision: long-form for clarity (matches existing `strategies`, `triggers`). Confirm in Phase 1.

2. **Mixed-mode workflow auto-close**: WorkSession scope contains both auto and manual services. Behavior: ANY service in scope being `manual` blocks auto-close. Document in Phase 6.

3. **Mid-flow CloseDeployMode change**: user sets close-mode mid-workflow. Behavior: handler reads CloseDeployMode fresh at evaluation time (not cached). Workflow that started in manual mode can transition to auto mode mid-flight. Document in Phase 6.

4. **GitPushState=broken recovery**: when does state flip back to configured? Options: explicit re-run of git-push-setup, or auto-probe on next push. Decision: explicit re-setup; don't auto-recover (silent recovery hides intent).

5. **RemoteURL mismatch policy**: meta.RemoteURL = "https://github.com/foo/bar" but git origin = "git@github.com:foo/bar.git". Behavior: warn but don't block. Always trust git origin (runtime source of truth).

## 13. Provenance

Drafted 2026-04-28 after multi-round 2026-04-28 user dialogue + Codex investigation:

1. **Round 1**: User asked for atom precision review on push-git/CI-CD. Initial diagnosis identified P1-P11 atom-level issues.
2. **Round 2**: User confirmed P1+P2 (source-of-push) as fundamental. Codex ground-truthed code state; verdict (C) Broken contract across 5 layers.
3. **Round 3**: User requested system-level expansion. Identified R12-R21 root problems (subdomain skip, pre-flight skip, manual contradiction, record-deploy orphan, etc.). 21 total root problems mapped.
4. **Round 4**: User answered Q-A (manual = extension slot), Q-B (git-push + CI/CD orthogonal + linked), Q-C (Deployed = consider holistically). Architecture refined to 3 orthogonal dimensions. Codex stress-tested and surfaced 5 design ambiguities.
5. **Round 5**: User answered F1 (auto-close gate by CloseDeployMode), CI/CD framing as utility (not totalizing), dropped LastBuildLandedAt as over-engineering.
6. **Round 6**: Initial plan draft synthesizing all decisions into 12 implementable phases.
7. **Round 7 (Codex final stress-test)**: 7-scenario walkthrough surfaced 4 corrections folded into this plan:
   - Phase 1 must include atom parser axis extensions (else Phase 8 atom parse fails)
   - Phase 2 migration must hook ALL read paths (not just `ReadServiceMeta`); preferred integration point: `parseMeta`
   - Phase 6 must address `handleDevelopBriefing` auto-delete behavior for manual mode
   - Phase 8 must audit ALL atoms for stage leakage (not just push-git family)

Co-authored: deep dialogue 2026-04-28 between user (Karel) + Claude + Codex (5 background investigations: pushgit-recon, pushgit-routing, system-interaction, architecture-validate, final-walkthrough).
