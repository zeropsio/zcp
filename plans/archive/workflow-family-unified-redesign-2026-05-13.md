# Workflow Family Unified Redesign — git-push + cicd + production + tokens

**Date:** 2026-05-13 (v4 — final after Karel SDK reveal + production push-mode redesign)
**Status:** APPROVED for Phase 0 — awaiting GO
**Scope:** Unified architecture covering git-push, CI/CD, production launch, and token flow — across export + launch-production + bootstrap/develop. Hard compatibility guarantee for existing dev/stage workflows. Parity across local + container modes.

---

## 1. Reality (validated against code + SDK)

### 1.1 Token landscape

| Token | Scope | Lives | ZCP holds value? |
|---|---|---|---|
| **Zerops API token** (`ZCP_API_KEY`) | per-project | Container env / `.mcp.json` / GH Secret `ZEROPS_TOKEN` (CI side) | env-read only |
| **GitHub fine-grained PAT** | per-repo | User's local `gh` auth; container's `GIT_TOKEN` project env | **never** |
| **launchKey** (transient) | account-wide | Handler in-memory only (`defer admin.Close()`) | 5–15 min window |
| **Existing-prod-project token** (NEW v4) | per-project (the prod project) | Handler in-memory only | session only, cleared post-mutation |

**Token reuse pattern** (shipped, `setup-build-integration-actions.md:60`): GH Secret `ZEROPS_TOKEN` value == project's `ZCP_API_KEY`. Shell-expand never crosses MCP wire.

### 1.2 Server-direct SDK calls (already in zerops-go, ZCP underuses)

| SDK call | What server returns | ZCP today | v4 plan |
|---|---|---|---|
| `GetServiceStackEnv` | items with `Type` (USER\|SYSTEM) + `Sensitive` + `Editable` | **drops** all 3 (`zerops_env.go:27-34`) | propagate full shape; SDK-driven env auto-class |
| `GetProjectEnv` | items with `Type` + `Sensitive` + `Editable` | drops same 3 | same |
| `GetProjectExport` | canonical project import yaml | **unused** | baseline for export; ZCP layers Variant + buildFromGit transforms |
| `GetServiceStackExport` | per-service canonical yaml | unused | optional reference for single-service export |
| `PostServiceStackZeropsYamlValidation` | server-authoritative pass/fail | **unused** (client-side JSON schema only) | replace `schema.ValidateImportYAML` for export + launch |
| `PostClientProjectImport` (via `ProjectAdminClient`) | new project + initial services | launch new-project path | unchanged |
| **`PostProjectServiceStackImport`** (NEW v4 use) | services imported into **existing** project | `zerops_env.go` calls for env mutation only; never for launch | enables existing-prod-project path |
| `PutServiceStackTriggerPipeline` | direct deploy w/o git pull | unused | post-launched first-deploy without waiting on tag push (optional v2) |

### 1.3 Cross-project surface — launchKey today

Two project modes after v4:

```
NEW PROD PROJECT (existing flow)         EXISTING PROD PROJECT (NEW v4 path)
─────────────────────────────             ─────────────────────────────
launchKey (account-wide, transient)       project-scoped token (user-supplied)
                                          → validate via GetUserInfo (single-project gate)
PostClientProjectImport (create)          PostProjectServiceStackImport (import-only)
GrantSelfRole (A.10)                      no role grant (user owns project)
DeleteProject recovery on failure         NO delete recovery (user-owned)
launchKey REVOKED post-launched           token cleared from handler memory
```

### 1.4 Git operations (env-split, unchanged)

- **Container** (`deploy_git_push.go`): `GIT_TOKEN` → `.netrc` with `trap rm + umask 077` → `git push` via SSH-exec. ZCP never reads `$GIT_TOKEN` value.
- **Local** (`deploy_local_git.go`): delegate to user's git auth (SSH keys, Keychain). ZCP never reads/writes credentials.

### 1.5 Known issues fixed by v4

| ID | Issue | Resolution |
|---|---|---|
| L1 | F19/F20/F21 composer divergence (object-storage, mode reject) | Server-driven yaml + SDK Type/Sensitive/Editable obviates client mapping |
| L3 | **P-LP-3 LATENT BUG** — `SourceSnapshot` written only at mutation, compared "against itself" | Phase 0.5: baseline write at ready-to-launch + compare at mutation |
| L4 | P-LP-1 grep gap (`fmt.Sprintf("%+v")` would leak) | Phase 5: `CredentialField` interface + AST sentinel + serialization fixture |
| L5 | `GitPushBroken`/`Unknown` orphan enum, referenced by setup atoms but never written | Phase 6: enum cleanup + atom frontmatter update |
| L6 | No GitHub Actions emit for prod project post-launched | Phase 6: `ComposeActionsHandoff(Prod)` single canonical path |
| L7 | No "generate-prod-token BEFORE revoke-launchKey" ordering | Phase 6: composer-level ordering pin |
| L10 | Auto-class env table launch-only | Phase 3: SDK-driven (Type/Sensitive/Editable) replaces table, both workflows |
| L11 | `launchLaunchedResponse` handler-composed | Phase 6: typed `ComposeLaunchTerminal` |
| L12 | No `IntegrationChoice` field needed | DROPPED — single push-mode path (N2) |

### 1.6 Production redesign substrate (Karel N2)

**Today:** import yaml runtime entries carry `buildFromGit: https://...`. Server pull-mode builds on first start + tag push triggers via dashboard-OAuth Path B pipeline integration.

**v4:** prod runtime entries get `startWithoutCode: true` + **no `buildFromGit`**. Service exists as infra shell. Deploy via GitHub Actions push-mode (`zcli push --service-id <prod-id> --setup prod` on tag push).

**Implications:**
- Path B pipeline integration **DROPPED** for prod (`launch_pipeline.go`, `PipelineConfigurations`, `pickPipelineAtomID`, `launch-pipeline-*.md` atoms).
- `IntegrationStatus` SDK type + `GetServiceStackIntegrationStatus` — kept available, unused post Phase 6.
- 3-way cicd choice (Actions/webhook/skip) **DROPPED** — single canonical path.
- `PutStandardServiceStackTriggerExternalRepositoryIntegration` — exists in SDK but **not used** (P-LP-7 stays).

---

## 2. Target unified architecture

### 2.1 Token policy formalized

**Rule 1 — Persistent shapes:** `ZerropsToken` (project-scoped) + `GitHubPAT` (fine-grained, per-repo). User-supplied, ZCP holds `ZerropsToken` only for **its own** project.

**Rule 2 — Token reuse default:** For any project's CI/CD, GH Secret `ZEROPS_TOKEN` value == project's `ZCP_API_KEY`. Shell-expand at user side.

**Rule 3 — Transient credentials:** `LaunchKey` (account-wide, new-project mutation) + `ExistingProdToken` (project-scoped, existing-project mutation). Both compile-time `CredentialField` (Phase 5).

**Rule 4 — Cross-project token handoff (push-mode, codex-corrected):**

Post-launched: user generates NEW project-scoped token on the prod project. Token reaches:
- GH Secret `ZEROPS_TOKEN` of source repo (for prod Actions workflow)
- Optionally local `.mcp.json` (debug; non-default)

ZCP never holds prod-token value. Stdin form mandatory:

```bash
# PREFERRED: stdin (no shell-transcript leak)
echo "$PROD_TOKEN" | gh secret set ZEROPS_TOKEN -R owner/repo
```

**Rule 5 — `GIT_TOKEN` (container only, unchanged):** set via `zerops_env action=set project=true variables=["GIT_TOKEN=<PAT>"]`; container shell consumes via `.netrc`.

### 2.2 Layer 0 — server-direct calls (NEW v4)

Where SDK already has authoritative behavior, ZCP wraps thinly:

| Server call | ZCP wrapper |
|---|---|
| `GetProjectExport(projectID)` | `ops.FetchProjectExportYAML` — baseline for export composer |
| `PostServiceStackZeropsYamlValidation(...)` | `ops.ValidateZeropsYAML` — replaces client-side `schema.ValidateImportYAML` for export + launch |
| `PostProjectServiceStackImport(projectID, yaml)` | `ops.ImportServicesIntoExistingProject` — existing-prod-project path |

### 2.3 4 ZCP layers (above Layer 0)

```
Layer 1   internal/ops/bundle/          Bundle.Compose(serverBaseline, variant, inputs) typed Variant
                                          - launch variant: NO buildFromGit, startWithoutCode:true
                                          - export variant: buildFromGit-self-snapshot, env-strip
Layer 1b  internal/ops/cicd/            ComposeActionsHandoff(target, owner/repo, serviceID, token,
                                          setup, trigger) — SHARED stage + prod
Layer 2   internal/ops/inventory/       ListServices + ENVS WITH Type/Sensitive/Editable preserved
Layer 3   internal/envclass/            SDK-driven: Type=SYSTEM→infra; Editable=false→drop;
                                          Type=USER+Sensitive→AutoSecret bias; USER else→PlainConfig
Layer 4   WorkflowState.Launch          + LaunchProjectMode (New|Existing) — promoted from side-channel
                                          (LaunchID stays as deterministic inner key)
```

**Dependency rule** (`internal/topology/architecture_test.go`): unchanged. `cicd/` peers with `bundle/`, both under `ops/`.

### 2.4 Production redesign (N2 — push-mode)

**Composed import yaml for production:**

```yaml
project:
  name: <prod-name>
  tags: [env:prod, source-project:<srcID>, managed-by:zcp-launch]
  envVariables: { /* classified per Layer 3 */ }
services:
  - hostname: <runtime>
    type: <stack-type>
    mode: HA
    priority: 10
    minContainers: 2
    verticalAutoscaling: { cpuMode: DEDICATED }
    zeropsSetup: prod
    startWithoutCode: true             # NEW v4 — no first-build wait
    # NO buildFromGit                  # NEW v4 — push-mode handoff
  - hostname: <db>
    type: postgresql@16
    mode: HA
    priority: 10
  # … managed services unchanged …
```

**Deploy via GitHub Actions** (composed by `ComposeActionsHandoff(Prod, ...)`):

```yaml
# .github/workflows/zerops-prod.yml — emitted by ZCP, user commits
on:
  push:
    tags: ['v*.*.*']
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: zeropsio/actions@v1.0.2
        with:
          access-token: ${{ secrets.ZEROPS_TOKEN }}
          service-id: <prod-runtime-service-id>
          setup: prod
```

**Atoms (atom-only PR per Phase 6a):**
- `launch-generate-prod-token.md` — deep-link to dashboard, scope guidance
- `launch-cicd-actions-handoff.md` — workflow YAML + stdin `gh secret set` commands
- `launch-delete-key.md` updated — prerequisite: prod token confirmed in GH Secrets via `gh secret list`
- `launch-post-checklist.md` updated — tag-push trigger flow

### 2.5 Existing-prod-project path (N3 — token first)

**ASKUSERQUESTION 1** (at launch start, BEFORE scope):

```
"Production project?"
  a) Vytvořit nový (Zerops launchKey, 5–15 min window)
  b) Použít existující (project-scoped token na ten projekt)
```

If (b):

```
1. ASKUSERQUESTION 2: "Project ID + access token"
2. ZCP constructs *Client with token, calls GetUserInfo
3. Validate: token sees EXACTLY 1 project; matches user-declared ExistingProjectID
   Refused: multi-project, no-project, scope mismatch
4. Validated → proceed to scope/classify (same as new-project)
5. Mutation: PostProjectServiceStackImport (NO CreateAndImportProject)
6. No DeleteProject recovery (user-owned)
```

Pin: `TestExistingProjectTokenValidation_RefusesMultiProject`, `..._RefusesScopeMismatch`, `TestExistingProjectImport_UsesServiceStackImportEndpoint`.

### 2.6 Env auto-classification (SDK-driven, Karel insight)

**Drop** `internal/tools/launch_platform_envs.go::platformEnvAutoClass` static table entirely. **Drop** F19 backlog (object-storage CDN keys).

**Replace** with SDK fields:

```go
type EnvVar struct {
    ID        string
    Key       string
    Content   string
    Type      EnvType  // NEW: USER | SYSTEM (server-side classification)
    Sensitive bool     // NEW: server-side flag
    Editable  bool     // NEW: read-only system envs vs editable
}
```

**Classifier rules** (`internal/envclass/classify.go`):

| Server says | Classification | LLM involvement |
|---|---|---|
| `Type=SYSTEM` (any) | `infrastructure` — target project regenerates own | none |
| `Editable=false` | drop entirely — target generates own (read-only platform-injected) | none |
| `Type=USER`, `Sensitive=true` | bias `AutoSecret` | LLM confirms or picks `ExternalSecret` |
| `Type=USER`, `Sensitive=false` | bias `PlainConfig` | LLM confirms or upgrades classification |

LLM receives only the `Type=USER` subset. Static table eliminated; pattern detection eliminated; F19 dissolved.

### 2.7 Shared cicd handoff composer (Layer 1b)

```go
// internal/ops/cicd/actions_handoff.go
type CICDTarget int
const (
    TargetStage CICDTarget = iota
    TargetProd
)

type ActionsHandoffInput struct {
    Target          CICDTarget
    OwnerRepo       string
    ServiceID       string
    SetupName       string         // "stage" | "prod"
    SecretName      string         // "ZEROPS_TOKEN"
    TokenSourceExpr string         // shell-expand expression
    Trigger         TriggerSpec    // branch | tag pattern
}

func ComposeActionsHandoff(in ActionsHandoffInput) ActionsHandoffOutput
```

**Two user-facing moments, identical pattern:**

| Moment | Trigger | Service-ID source | Token | Setup | GH workflow trigger |
|---|---|---|---|---|---|
| **Stage** (in `action=git-push-setup` confirm, ASKUSERQUESTION Y default) | post-stamp `RemoteURL` | source project, current `ZCP_API_KEY` lookup | `$ZCP_API_KEY` shell-expand | per-pair stage setup | `push: branches: [main]` |
| **Prod** (in launch terminal, single mandatory path post-launched) | post `Status=launched` | target project, from `ImportedServices` | NEW prod token (user paste, stdin form) | `prod` | `push: tags: ['v*.*.*']` |

**Provider auto-detect** (Q3): `parseGitHost(remoteURL)` →
- `github.com` → Actions handoff (this path)
- `gitlab.com` / `gitlab.*.*` → webhook fallback atom (manual dashboard OAuth — not push-mode)
- other → ASKUSERQUESTION: stage/prod cicd unsupported via Actions; manual `zcli push` only

### 2.8 Compile-time trust boundary (DTO split, unchanged from v3)

```go
package auth

type ZerropsToken string
type LaunchKey string
type ExistingProdToken string
type CredentialField interface{ isCredential() }

func (LaunchKey) isCredential() {}
func (ZerropsToken) isCredential() {}
func (ExistingProdToken) isCredential() {}
```

AST sentinel + serialization fixture: walk every serializable type reachable from response/state/audit roots; assert no `CredentialField`-typed field present; assert no `zcp:"secret"` struct tag; populate with sentinel value, run `fmt.Sprintf("%+v")` + `json.Marshal`, assert sentinel absent.

### 2.9 P-LP-3 active compare gate (Phase 0.5 — codex-corrected, unchanged)

Step 1: `launchReadyToLaunchResponse` persists baseline `SourceSnapshot` to launchState.
Step 2: `executeLaunchMutation` reads persisted baseline + recomputes current; refuses on drift.
Step 3: pins `TestPersistsSnapshotAtReadyToLaunch`, `TestRefusesOnSourceDriftBetweenReadyAndPublish`, `TestRefusesOnTamperedStateFile`.

---

## 3. User flows (v4)

### 3.1 Git-push-setup (chained from develop-close-mode-git-push-needs-setup)

```
1. ASKUSERQUESTION: "Máš git repo pro tento projekt?"
   a) Mám už hotové → "Provide URL"
   b) Nemám, vytvořím → hint atom (NEW, N1): https://github.com/new
                         + "vytvoř manuálně, pak vrať se s URL"
                         (žádná gh CLI automatika)

2. Walkthrough: token guidance (fine-grained PAT, single-repo scope,
   scopes: Contents R+W, Secrets R+W, Actions R+W — thinking ahead pro stage cicd)
   Container: `zerops_env action=set project=true variables=["GIT_TOKEN=..."]`
   Local: user's `gh auth login`

3. Confirm: validate URL → stamp meta.RemoteURL + GitPushState=Configured

4. ASKUSERQUESTION (NEW, Q2): "Auto-deploy stage on every push to main? [Y default]"
   Y → ComposeActionsHandoff(Stage, owner/repo, stageSvcID, $ZCP_API_KEY, "<setup>", main-branch)
       → atom-emit workflow YAML + stdin gh secret set + commit/push instructions
   N → meta.BuildIntegration=None, post-checklist note "later via action=build-integration"
```

### 3.2 Export

```
1. Server-direct: ops.FetchProjectExportYAML(projectID) → baseline
2. Variant pick (auto-default: if 1 RG runtime → that one; else ASKUSERQUESTION)
3. ZCP transforms on baseline:
   - Export-Dev variant: strip runtime envs, add buildFromGit-self-snapshot (current branch + commit)
   - Export-Stage variant: same + STAGE flag
4. Env auto-class via Layer 3 (SDK Type/Sensitive/Editable)
   - Type=USER subset → ASKUSERQUESTION or classification table to LLM
5. Server validation: ops.ValidateZeropsYAML
6. Output: bundle with yaml + warnings + nextSteps (commit + push)
```

### 3.3 Launch-production (REDESIGNED per N2)

```
START
  │
  ├─ ASKUSERQUESTION 1: "Production project?"
  │    a) Vytvořit nový → flow with launchKey
  │    b) Použít existující → flow with project-scoped token (entered FIRST)
  │
  ├─ (b only) Token validation: GetUserInfo single-project gate + ID match
  │
  ├─ Scope → classify (SDK-driven env auto-class, LLM picks USER subset)
  │
  ├─ Compose import yaml:
  │    • runtime entries: startWithoutCode:true, NO buildFromGit
  │    • managed deps: HA, priority:10, type-specific config
  │    • project tags: env:prod + source-project:<id> + managed-by:zcp-launch
  │
  ├─ ★ Phase 0.5: SourceSnapshot baseline written at ready-to-launch
  │
  ├─ Server validation: ops.ValidateZeropsYAML(yaml)
  │
  ├─ Mutation:
  │    (a) launchKey + PostClientProjectImport → CreateAndImportProject
  │    (b) existingProdToken + PostProjectServiceStackImport → ImportServicesIntoExistingProject
  │    ★ Phase 0.5: compare baseline vs current SourceSnapshot; refuse on drift
  │
  ├─ Status=launched
  │
  └─ Terminal (single mandatory cicd path, no choice):
       ZCP composes via ComposeActionsHandoff(Prod, owner/repo, prodRuntimeSvcID,
                                              "<paste-prod-token>", "prod", tag-trigger)
       Emit:
         - launch-generate-prod-token (dashboard deep-link, scope reminder)
         - launch-cicd-actions-handoff (workflow YAML + stdin gh secret set)
         - launch-delete-key (prereq: gh secret list confirms ZEROPS_TOKEN present)
         - launch-post-checklist (tag push v0.1.0 → first build)
```

### 3.4 CICD standalone

`action="build-integration"` retained for opt-in stage cicd post-fact (when user said N to inline ASKUSERQUESTION in git-push-setup). Same `ComposeActionsHandoff(Stage, ...)` composer used — no parallel implementation.

`webhook` integration kept as fallback for GitLab origins; **not** the prod cicd path.

`Path B pipeline integration` (`launch_pipeline.go`, dashboard-OAuth) **DEPRECATED** in v4 — removed in Phase 6.

### 3.5 Bootstrap/Develop — unchanged

---

## 4. Local + Container parity

| Concern | Container | Local | Parity ensured by |
|---|---|---|---|
| `ZCP_API_KEY` source | Zerops auto-inject env | `.mcp.json` env block | `auth.Resolve` |
| Token reuse expansion | `$ZCP_API_KEY` shell | `$(jq -r ... .mcp.json)` | `ghSecretValueExpr(rt)` |
| Git push | SSH-exec container | local exec | `rt.InContainer` branching |
| Bundle composer | env-agnostic | env-agnostic | input-driven |
| Inventory + ENVS (Type/Sensitive/Editable) | platform API + cache | same | env-agnostic |
| Envclass | env-agnostic | env-agnostic | pure functions |
| State files | `.zcp/state/...` SSHFS | local disk | `engine.StateDir()` |
| Workflow file write | SSHFS at `/var/www`; LLM via SSH | local `workingDir`; LLM via Bash | atom env-axis split |
| `gh` CLI availability | pre-installed in `zcp@1` image | user-installed | `setup-gh-required.md` precheck atom |
| `gh auth status` precheck | container shell | user shell | shared atom guidance |
| Source repo owner/repo derivation | `meta.RemoteURL` (cached) | `git remote get-url origin` | `ops.ParseGitRemoteOwnerRepo` |

Pin tests (Phase 7): `TestLaunchCICDActionsHandoff_{ContainerExpansion,LocalExpansion}`, `TestStageCICDHandoff_{Container,Local}`, `TestGhCLIAvailable_PreflightAtom`, `TestExistingProjectFlow_{Container,Local}`.

---

## 5. Bootstrap + Develop compatibility (hard guarantee)

Preserved surfaces — no behavior change through Phase 6 except where noted:

| Surface | Status |
|---|---|
| `action=start workflow=bootstrap` / `develop` | unchanged |
| `action=complete/skip/route/close/dispatch-brief-atom` | unchanged |
| `action=close-mode` | unchanged |
| `action=git-push-setup` | ASKUSERQUESTION (Q2 stage cicd) added in confirm; default Y; no breakage if N |
| `action=build-integration` | retained for opt-in; redirects to shared composer |
| `action=record-deploy` | unchanged |
| `action=adopt-local` | unchanged |
| `action=status` | envelope/plan/guidance unchanged |
| `ServiceMeta` JSON shape | additive; `BuildIntegrationWebhook` enum preserved for GitLab fallback |
| Atom corpus `bootstrap-*`/`develop-*` | unchanged; `setup-git-push-*.md` updated re: scope thinking-ahead |
| `launch-pipeline-*.md` atoms | **DEPRECATED** Phase 6 — kept readable for old launchState files |

Verification per phase: full eval suite + bootstrap/develop scenarios + adopt-local + recipe. Regression = phase rollback.

---

## 6. Phasing (REVISED per N2 — 8 phases, 5 gates)

```
                                                G1
Phase 0   ── Pin launch + export goldens   ─────┤
              + F19/F20/F21 reproducers          │
              + drift-injection golden           ▼
                                                G1
Phase 0.5 ── P-LP-3 active compare fix    ──────┤
              (baseline at ready + compare at    │
               mutation — codex correction)      ▼
                                                G2
Phase 1a  ── Layer 2 inventory + EnvVar    ─────┤
              (Type/Sensitive/Editable)          │
Phase 1b  ── Layer 1 bundle composer       ─────┤
              (export + launch variants;          │
               launch variant emits               │
               startWithoutCode + no              │
               buildFromGit)                      │
Phase 1c  ── Layer 1b cicd handoff composer ────┤
              (ComposeActionsHandoff               │
               stage + prod shared)                ▼
                                                G2
Phase 2   ── Server-direct integration     ─────┤
              (GetProjectExport,                  │
               PostServiceStackZeropsYamlValid,   │
               PostProjectServiceStackImport)     │
              + Layer 3 envclass (SDK-driven)     ▼
                                                G3
        ┌── Phase 4a — State schema versioning ┐
        │       + LaunchProjectMode field      │
        │       + ExistingProdToken transient  │
        │   ◀── PARALLEL ────────────────────▶ │
        ├── Phase 5  — DTO split + AST sentinel│
        │       + CredentialField interface    │
        └──────────────────────────────────────┘
                                                G3
Phase 4b  ── Atom-only PR FIRST            ─────┤
              + state promotion (launchState    │
                → WorkflowState.Launch)         │
              + SourceSnapshot to neutral pkg   │
                IN SAME PR                      │
              + migration: legacy wrap-archive  │
              + ZCP_SKIP_LAUNCH_MIGRATION       │
                                                ▼
                                                G4
Phase 6   ── Production redesign            ────┤
              + atom corpus: launch-cicd-       │
                actions-handoff, prod-token,    │
                delete-key updates              │
              + repo-create-hint atom (N1)      │
              + handler wiring: existing-       │
                project path + ASKUSERQUESTION  │
                1 (project mode)                │
              + Path B drop (launch_pipeline,   │
                PipelineConfigurations,         │
                pickPipelineAtomID)             │
              + orphan enum cleanup             │
                (GitPushBroken/Unknown          │
                 frontmatter)                   ▼
                                                G5
Phase 7   ── Eval scenarios validation     ──[ship]
              + launch-then-cicd-actions-handoff
              + launch-existing-project-path
              + stage-cicd-from-git-push-setup
              + container + local parity matrix
              + bootstrap/develop regression
```

### 5 decision gates

| Gate | Between | Evidence |
|---|---|---|
| **G1** | Phase 0.5 → 1a | Goldens captured; reproducers committed; P-LP-3 fix lands at TWO sites |
| **G2** | Phase 1c → 2 | Launch + export composer goldens green; cicd composer pin green |
| **G3** | Phase 5 → 4b | Migration tested on Karel's `.zcp/state/launch-production/`; AST sentinel + fixture green |
| **G4** | Phase 6 → 7 | Full prod-handoff chain green on eval-zcp (new + existing project, container + local); P-LP-10 ordering pin green |
| **G5** | Phase 7 → release | Full eval matrix green; bootstrap/develop regression-free |

### LOC budget

~2400 LOC churn across ~20 files (down from v3 estimate ~3000 due to Path B drop + cicd unification + env auto-class simplification).

### Schema migration sharp edges (unchanged from v3)

1. Deterministic SessionID for migrated launches: `hex(sha256("migrated::" + LaunchID))[:16]`
2. N concurrent launches preserved
3. SourceSnapshot moved to `internal/snapshot/` in same Phase 4b PR (no interstitial)
4. Retry: skip-with-log if already migrated
5. `ZCP_SKIP_LAUNCH_MIGRATION=1` env var — operator escape hatch
6. **One-way migration** — no inverse script; `.archive/` operator-recoverable

### Release sequence (5 releases)

| Release | Phases | What ships |
|---|---|---|
| v9.91 | 0 + 0.5 | Goldens + P-LP-3 latent-bug fix |
| v9.92 | 1a + 1b + 1c + 2 | Composer unification + server-direct + envclass |
| v9.93 | 4a + 5 | Schema versioning + DTO split + AST sentinel |
| v9.94 | 4b | State migration cliff (operator-aware) |
| v9.95 | 6 + 7 | Production redesign + cicd unification + full eval |

---

## 7. P-LP-1..N preservation matrix (v4 updated)

| Invariant | Current | After v4 redesign |
|---|---|---|
| **P-LP-1** launchKey never in state/log/response | Field-name discipline + sentinel | Compile-time `CredentialField` + AST scan + serialization fixture |
| **P-LP-2** ProjectAdminClient restricted import | `TestProjectAdminClientRestrictedImport` | Unchanged; existing-project path uses regular `Client` (no admin) |
| **P-LP-3** SourceSnapshot active compare | LATENT BUG — compared "against itself" | Phase 0.5: baseline write at ready + compare at mutation (TWO sites) |
| **P-LP-4** launched response mandatory delete-key | Pin | Unchanged through Phase 4b + 6 |
| **P-LP-5** EnvKey no Value field | Compile-time | Unchanged |
| **P-LP-6** audit append-only 0o600 | Runtime + test | Unchanged |
| **P-LP-7** ZCP never PUTs integration | `TestExecuteLaunchPipelineCheck_NoPutCallsByZCP` | Unchanged — push-mode preserves this; Path B drop doesn't violate |
| **P-LP-8** pipeline failures = WARN | Unchanged in flight | **DEPRECATED Phase 6** — Path B gone |
| **P-LP-9** noExternalRepositoryIntegration → NotConfigured | Pin | **DEPRECATED Phase 6** — no IntegrationStatus read path |
| **P-LP-10** (NEW) generate-prod-token atom BEFORE delete-key atom | not enforced today | Composer-level pin (`TestComposeLaunchTerminal_GenerateBeforeRevoke`) |
| **P-LP-11** (NEW v4) production runtime entries: `startWithoutCode:true` AND no `buildFromGit` | not enforced | Composer-level pin (`TestLaunchBundle_RuntimeNoBuildFromGit`, `..._StartWithoutCodeTrue`) |
| **P-LP-12** (NEW v4) existing-project token validates single-project scope | not enforced | Pre-mutation gate (`TestExistingProjectToken_RefusesMultiProject`, `..._RefusesScopeMismatch`) |

---

## 8. Closed decisions (v3 → v4 → GO)

All decisions closed by Karel:

| ID | Decision | Final |
|---|---|---|
| V3-D1 | GO/NO-GO entire plan | **GO** for v4 (this doc, with Karel's revisions) |
| V3-D2 | Option B (post-launched prod token) | **YES** (push-mode cicd) |
| V3-D3 | stdin `gh secret set` form | **YES** |
| V3-D4 | `CredentialField` Go interface | **YES** (Phase 5) |
| V3-D5 | 5-release sequence v9.91→v9.95 | **YES** |
| V3-D6 | `GitPushBroken/Unknown` orphan cleanup | **YES** Phase 6 |
| V3-D7 | F19 CDN keys defer to v2 | **DISSOLVED** — SDK Type/Sensitive/Editable removes the need |
| V3-D8 | Schema migration ONE-WAY + escape hatch | **YES** |
| N1 | Repo-create automation | **NO automation** — hint atom with `https://github.com/new` link |
| **N2** | **Production runtime entries** | **`startWithoutCode:true`** + **no `buildFromGit`** + cicd push-mode handoff |
| N3 | Existing-project token entry order | **FIRST** (before scope/classify), validated via single-project gate |
| R1 | P-LP-7 reevaluation (PUT integration) | **STAYS** (push-mode = ZCP doesn't need PUT) |
| R2 | Replace `composeImportYAML` with server `GetProjectExport` | **YES** (full replace, layered transforms on top) |
| R3 | Replace client-side schema with server validation | **YES** (full replace) |

---

## 9. References

**Code maps (substrates):**
- `/tmp/zcp-workflow-family-map.md`
- `/tmp/zcp-tokens-git-cicd-map.md`
- `/tmp/zcp-handlers-atoms-map.md`
- `/tmp/zcp-state-lifecycle-map.md`

**Codex reviews:**
- `/tmp/codex-out-1778686003-13029-12484.md` (round 1)
- `/tmp/codex-out-1778687519-20449-18788.md` (round 2)
- `/tmp/codex-out-1778699492-36999-32637.md` (round 3)

**Zerops SDK (zerops-go v1.0.18):**
- `types/enum/envTypeEnum.go` — USER\|SYSTEM env type enum (default USER)
- `dto/output/{serviceStackEnv,projectEnv}.go` — env DTOs with Type/Sensitive/Editable
- `sdk/{GetProjectExport,GetServiceStackExport}.go` — server-side yaml emit
- `sdk/PostServiceStackZeropsYamlValidation.go` — server-side validation
- `sdk/PutServiceStackTriggerPipeline.go` — direct deploy w/o git pull
- `dto/input/body/zeropsYamlValidation.go` — validation input shape
- `dto/input/body/putStandardServiceStackTriggerPipeline.go` — direct-deploy input

**Zerops docs:**
- `../zerops-docs/apps/docs/content/zcp/security/tokens-and-project-access.mdx` — single-project token gate
- `../zerops-docs/apps/docs/content/features/env-variables.mdx` — env taxonomy
- `../zerops-docs/apps/docs/content/zcp/security/{production-policy,trust-model}.mdx`
- `../zerops-docs/apps/docs/content/zcp/workflows/{promote-to-production,package-running-service}.mdx`
- `../zerops-docs/apps/docs/content/references/{import,github-integration,gitlab-integration}.mdx`

**Code anchors:**
- `internal/tools/workflow.go` (router)
- `internal/tools/workflow_launch_production.go` (launch handler; P-LP-3 sites L313+L332)
- `internal/tools/launch_state.go` (side-channel state, promoted Phase 4b)
- `internal/tools/launch_pipeline.go` (Path B verify — DROPPED Phase 6)
- `internal/tools/workflow_build_integration.go` (CI/CD action; refactor to shared composer Phase 6)
- `internal/tools/workflow_git_push_setup.go` (Q2 ASKUSERQUESTION addition Phase 6)
- `internal/tools/workflow_close_mode.go` (unchanged)
- `internal/tools/deploy_git_push.go` (unchanged)
- `internal/workflow/service_meta.go` (3 deploy axes; webhook integration enum kept for GL fallback)
- `internal/platform/project_admin.go` (launchKey-gated client)
- `internal/platform/zerops_env.go` (EnvVar struct extension Phase 1a — Type/Sensitive/Editable)
- `internal/platform/zerops_search.go` (PostProjectServiceStackImport already callable)

---

## 10. Decision summary

**Plan-doc v4 status:** comprehensive, code-validated, SDK-validated, dual-review-integrated (codex + Plan agent + Karel feedback rounds), awaits Karel GO for Phase 0.

**Net change v3 → v4:**

| Aspect | v3 | v4 |
|---|---|---|
| Prod cicd | 3-way choice (Actions/webhook/skip) | Single canonical Actions push-mode |
| Prod runtime yaml | `buildFromGit` set + pull-mode first build | `startWithoutCode:true`, no `buildFromGit`, push via cicd |
| Path B (`launch_pipeline.go`) | Phase 6 enhanced | **DROPPED Phase 6** |
| Env auto-class | Static table + pattern detection + F19 backlog | SDK Type/Sensitive/Editable; LLM classifies USER subset |
| Export bundle | Custom `composeImportYAML` | Server `GetProjectExport` baseline + transforms |
| Validation | Client-side JSON schema | Server `PostServiceStackZeropsYamlValidation` |
| Existing-project launch path | not addressed | First-class via `PostProjectServiceStackImport` + token gate |
| Project mode choice | implicit (always new) | ASKUSERQUESTION 1 (New vs Existing) — N3 first-entry token |
| Stage cicd entry | Separate `action=build-integration` | Inline ASKUSERQUESTION Y default in `action=git-push-setup` |
| Repo creation | not addressed | Hint atom with `https://github.com/new` link (N1, no automation) |
| Phase count | 12 (Phase 6 split 6a/6b/6c) | 8 (Phase 6 collapses to single PR) |
| LOC budget | ~3000 | ~2400 |
| Release sequence | v9.91→v9.95 unchanged | v9.91→v9.95 unchanged |
| New invariants | P-LP-10 | P-LP-10 + P-LP-11 (prod runtime shape) + P-LP-12 (existing-project token gate) |

**Karel-confirmed defaults:**
- Primary cicd = GitHub Actions push-mode (`zcli` based)
- Fine-grained GH PAT, single-repo scoped (atom recommends, doesn't enforce)
- ZCP never holds GH PAT or prod token value
- Production-policy compliance (prod project without zcp@1 service)
- Recipe scope = Aleš untouched
- Pre-prod cleanup allowed; no backward-compat shims
- Co-Authored-By NEVER in commits

**Phase 0 start awaits Karel GO.** Phase 0 = golden capture + F19/F20/F21 reproducers + drift-injection scenario; no production code changes. ~1 day work. v9.91 release after Phase 0.5 P-LP-3 fix.
