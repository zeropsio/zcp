# Workflow Family Unified Architecture

**Status:** specification + implementation plan
**Scope:** export, git-push, stage CI/CD, production launch, production CI/CD, token model
**Owner:** Karel (ZCP, Zerops)
**Constraints:** pre-production (no backward-compat shims), TDD-driven, layer-rule pinned (`internal/topology/architecture_test.go` + `.golangci.yaml::depguard`), English code, parity across local + container modes, recipe scope (`internal/recipe/`, `workflow_recipe.go`, `internal/workflow/recipe_templates_import.go`, `internal/recipe/yaml_emitter.go`) hard-untouched.

This document is standalone. It specifies (Part I) the target operational state and (Part II) how to implement it from scratch. Anyone reading this without prior context can execute it.

---

# PART I — SPECIFICATION

## 0. Intent

ZCP (Zerops Control Plane) is a Go binary serving MCP + CLI for a coding agent acting on a Zerops project. The agent's typical journey:

1. **Bootstrap** — discover/create a Zerops project, scaffold yaml, first deploy.
2. **Develop** — iterate on code, deploy to dev/stage, close work sessions to git.
3. **Promote** — export configuration as reproducible bundle, OR launch a new (or existing) production project.

The workflow family covers everything **between first successful deploy and production running**: persisting code in git, automating builds via CI/CD, packaging environments, and provisioning production with a clean trust boundary.

**The objectives are:**

- **Maximum LLM off-load.** Determinism wherever the platform/SDK can answer instead of the agent guessing. User input is reduced to irreducible decisions (do you have a repo, do you want auto-deploy stage, new vs existing prod project).
- **Robust by design.** Single canonical path per concern (no choose-your-own-cicd menus). State machines have explicit phases and pin tests. Compile-time invariants for credential handling.
- **Local + container parity.** Every flow works identically on the user's Mac and in the remote `zcp@1` container.
- **Reuse existing knowledge, not parallel implementations.** Stage CI/CD and production CI/CD share one composer. Export and launch share one bundle composer. Env classification uses the same engine for both.

**The intent is foundational, not patch-level.** This redesign reshapes the underlying primitives (token taxonomy, env classification, bundle composer, state model) so the user-facing flows become thin, declarative, and uniform. Pre-production constraint allows no compatibility shims.

---

## 1. Token model

ZCP recognizes **two persistent token shapes** and **two transient credential contexts**. No other credential lives in the codebase.

### 1.1 Persistent tokens

| Token | Scope | Who creates | Where it lives | ZCP holds value? |
|---|---|---|---|---|
| **Zerops API token** | Single project (Custom access per project, Full access) | User in Zerops dashboard | Container env (`ZCP_API_KEY` auto-injected by Zerops) OR local `.mcp.json` env block | env-read at boot only |
| **GitHub fine-grained PAT** | Single repository | User on github.com/settings/tokens | Local: user's `gh` CLI auth. Container: project env `GIT_TOKEN` (set by user via `zerops_env action=set project=true variables=["GIT_TOKEN=..."]`). | **never** |

**Token reuse rule** (one project ⇄ one Zerops token):

The same project's `ZCP_API_KEY` value flows through three identical surfaces:
- ZCP at runtime (env var)
- `zcli` via `zcli login "$ZEROPS_TOKEN"` in CI
- GitHub Actions repository/environment secret `ZEROPS_TOKEN`

Shell-expand at user side never crosses the MCP protocol wire.

**GitHub PAT recommended scopes** (atom guidance, not enforced):
- Contents: Read + Write (git push)
- Secrets: Read + Write (`gh secret set` for CI/CD)
- Actions: Read + Write (workflow runs)

Single-repository blast radius. Account-wide or org-wide PATs are not refused but flagged in atom guidance.

### 1.2 Transient credentials

| Credential | Purpose | Lifetime | Lives | ZCP holds value? |
|---|---|---|---|---|
| **`LaunchKey`** | Account-wide one-shot token to create a NEW Zerops project (`PostClientProjectImport`) | 5–15 min dashboard-generated window | Handler in-memory only (`defer admin.Close()`) | only in `*ZeropsClient` SDK handler transport; never in state/audit/response |
| **`ExistingProdToken`** | Project-scoped token for an EXISTING prod project (`CreateProjectEnv` + `PostProjectServiceStackImport`) | Single MCP call (input-only, same shape as `LaunchKey`) | Handler in-memory only | only in `*Client` transport |

Both transient credentials are **input-only per MCP call**. They are never written to disk (state, audit log, response). The user re-supplies them on retry, identical to how `LaunchKey` is supplied today (see `internal/tools/workflow.go::WorkflowInput.LaunchKey`).

### 1.3 Credential type system

```go
// internal/auth/credentials.go

type ZerropsToken string         // ZCP_API_KEY shape (project-scoped)
type LaunchKey string            // account-wide transient
type ExistingProdToken string    // project-scoped transient

type CredentialField interface{ isCredential() }
func (LaunchKey) isCredential()         {}
func (ZerropsToken) isCredential()      {}
func (ExistingProdToken) isCredential() {}
```

Three guards prevent leakage:
1. **AST sentinel** walks every serializable type reachable from response/state/audit roots and rejects any `CredentialField`-typed field.
2. **Field-tag sentinel** rejects `zcp:"secret"` tags reachable from the same roots.
3. **Serialization fixture** populates input with sentinel value `ZCP-LAUNCH-KEY-SENTINEL-DO-NOT-LEAK`, runs `fmt.Sprintf("%+v")` + `json.Marshal`, asserts sentinel absent.

### 1.4 Validation gates

| Stage | What's validated | How |
|---|---|---|
| Boot | `ZCP_API_KEY` scopes to exactly 1 project | `auth.Resolve` → `client.ListProjects`; refuse multi-project/empty |
| Pre-launch (NEW project path) | `LaunchKey` valid | `*ZeropsClient.GetUserInfo` succeeds inside `ProjectAdminClient.NewProjectAdminClient` |
| Pre-launch (EXISTING project path) | `ExistingProdToken` scopes to exactly 1 project AND matches user-declared `ExistingProjectID` | `client.GetUserInfo` → `client.ListProjects(clientID)`; refuse `len != 1` OR `projects[0].ID != input.ExistingProjectID` |

### 1.5 Dual-token architecture

A single launch session may run with TWO Zerops tokens active concurrently — `ZCP_API_KEY` (boot-resolved, source project) and a transient credential (`LaunchKey` for new project, `ExistingProdToken` for existing project). They scope to **different projects**:

```
                    Source project              Target/Prod project
                    ──────────────              ──────────────────
ZCP_API_KEY        ✓ scoped                    ✗ no access
LaunchKey          ✗ no access (account-       ✓ creates this project
                       wide creator)              + grants self role
ExistingProdToken  ✗ no access                 ✓ scoped (single-project)
```

**Construction isolation:** Source-project operations use `client *platform.Client` constructed at boot from `ZCP_API_KEY`. Target-project operations use either:

| Project mode | Constructor | Trust class |
|---|---|---|
| NEW | `*platform.ProjectAdminClient` from `auth.LaunchKey` (restricted import — P-LP-2; only `workflow_launch_production.go`) | account-wide transient |
| EXISTING | `*platform.Client` from `auth.ExistingProdToken` (new factory `platform.NewClientWithToken`; same shape as boot client, different token source) | project-scoped transient |

**Lifecycle isolation:** Source client lives for the entire ZCP process; target clients are per-MCP-call (`defer Close()` at function scope inside the launch mutation handler). No cross-pollination — source client never sees prod project, target clients never see source.

**Why this matters:** The boot gate (`auth.Resolve`) only validates the source-side credential. The target-side credential goes through a **second gate at mutation time** (§1.4 row 3) — without it, ZCP would silently operate on whatever project the user-supplied token has access to, which violates the single-project blast-radius principle. P-LP-12 pin enforces the second gate.

**Storage isolation:** Source state files (`.zcp/state/...`) belong to the source project (per-project `.zcp` directory). Target-side credentials never touch disk — purely input-bound to the handler call.

---

## 2. Git-push capability

**Purpose:** Push project code to a git remote so it persists outside the Zerops container / local-only volume. Recommended after first successful deploy; user-initiated.

### 2.1 Triggers (three paths, all user-initiated)

ZCP does not chain into git-push-setup automatically. Three entry points exist:

| Entry | Trigger | Initiator |
|---|---|---|
| **Soft recommendation atom** | After first successful deploy (work-session deploy success OR `record-deploy` stamp on a service without `RemoteURL`) | Atom prose recommends git-push for persistence; LLM relays to user; user replies "set up git push" → LLM picks intent → `action=git-push-setup` |
| **Explicit close-mode chain** | User picks `closeDeployMode=git-push` for a pair that has `GitPushState != Configured` | Existing `develop-close-mode-git-push-needs-setup` chain emits guidance pointer; user invokes `action=git-push-setup` |
| **Direct invocation** | User asks LLM for git-push anytime ("set up git push") | LLM calls `action=git-push-setup` directly |

In every entry, the actual strategy flow (§2.2+) runs only when `action=git-push-setup` is invoked. The post-first-deploy atom is **soft recommendation only** — no `ASKUSERQUESTION`, no automatic state transition. ZCP guides via prose; user decides timing.

### 2.2 Repository provisioning

Inside `action=git-push-setup` walkthrough, ZCP emits guidance covering three cases:

| User's repo state | ZCP emits |
|---|---|
| "Existující repo, mám URL" | Confirm step accepts URL; flow continues |
| "Existující repo, ale URL neznám" | Atom guidance: `git remote get-url origin` locally, or fetch from provider web UI |
| "Nemám repo" | Atom hint: `https://github.com/new` (manual creation; no `gh repo create` automation — fine-grained PATs cannot create repos and scope themselves) |

These three options surface as plain-text guidance in the walkthrough response. The user/LLM picks one and supplies `RemoteURL` in the next call. No `ASKUSERQUESTION` primitive here (text-driven, client-agnostic — see §11 ASKUSERQUESTION contract).

**Provider support:**

Auto-detect from origin URL (`parseGitHost` in `internal/ops/deploy_git_push.go`):

| Detected | Recommended cicd method | Alternative |
|---|---|---|
| `github.com` | GitHub Actions push-mode (§3, §4) | Zerops native webhook (§3.6, §4.6) |
| `gitlab.com`, `gitlab.*` | Zerops native webhook (§3.6, §4.6) | Manual `zcli push` |
| other | Manual `zcli push` only | n/a |

Push itself is provider-agnostic — works for any git URL (HTTPS/SSH/scp-form). Cicd methods are listed as **first-class alternatives**, not fallbacks. User picks (§3, §4 detail).

### 2.3 Token guidance

Atom `setup-git-push-{container,local}.md` instructs user to create a fine-grained PAT, single-repository, with the recommended scopes (Contents/Secrets/Actions R+W). Atom mentions the scopes are **also used later for CI/CD setup**, so user creates the token once.

### 2.4 State stamping

On confirm with valid URL:

```
ServiceMeta {
  RemoteURL:    <validated URL>
  GitPushState: Configured
}
```

URL validation per `internal/tools/workflow_git_push_setup.go::validateRemoteURL` (URI parse OR scp-form regex).

### 2.5 Stage CI/CD inline question

After confirm-stamp succeeds, ZCP emits a co-response containing:
- Git-push-setup success acknowledgment
- Structured prompt (client-agnostic): *"Recommended: auto-deploy stage on every push to `main`. Choose: (a) Actions push-mode [recommended], (b) Zerops native webhook, (c) None — manual `zcli push` only."*

The prompt is emitted as **plain-text body + machine-readable options array** in the same MCP response (see §11 ASKUSERQUESTION contract). LLM/client renders the chooser; user picks; next call carries `AutoDeployStage` field with the choice. Default recommendation (a) is highlighted in prose but not auto-applied — user must explicitly confirm.

Per choice:
- **(a) Actions** — triggers `ComposeActionsHandoff(Stage)` (§3)
- **(b) Webhook** — emits webhook setup atom (§3.6); user configures via Zerops dashboard
- **(c) None** — stamps `BuildIntegration=None`; post-checklist points to opt-in via `action=build-integration` later

The two MCP responses (confirm + cicd prompt) are co-emitted in a single tool result so the develop-close-mode chain sees the flow advance atomically.

### 2.6 Container vs local

| Concern | Container | Local |
|---|---|---|
| Git credentials | `GIT_TOKEN` project env → `.netrc` (trap-clean, umask 077) | User's SSH keys / Keychain / credential helper |
| Push execution | SSH-exec inside `/var/www` | Local `git push` in `workingDir` |
| Identity | Bootstrap-time `agent@zerops.io` in `/var/www/.git/config` | User's `git config user.*` |

ZCP never reads `$GIT_TOKEN` value. Local mode delegates entirely to user's git config.

---

## 3. Stage CI/CD

**Purpose:** Auto-deploy stage on every push to main branch.

### 3.1 Trigger + method choice

Triggered in three ways (all user-initiated):
- Inline structured prompt during `action=git-push-setup` confirm (§2.5), default recommendation **Actions push-mode**
- Explicit `action=build-integration integration=actions` post-fact opt-in (Actions)
- Explicit `action=build-integration integration=webhook` post-fact opt-in (Webhook)

Two cicd methods are first-class alternatives, **not fallbacks**:

| Method | Composer | When user picks |
|---|---|---|
| **Actions push-mode** | `ComposeActionsHandoff(target=Stage)` (§3.2) | GitHub repo; prefers push-mode (recommended for GitHub) |
| **Zerops native webhook** | Webhook setup atom (§3.6) | GitLab repo; or GitHub user who prefers pull-mode; or simpler setup |

User decides per method-prompt response (§2.5).

### 3.2 Composition

ZCP emits in the MCP response:

**A) Workflow YAML** to be written at `.github/workflows/zerops-stage.yml`:

```yaml
name: zerops-stage-deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install zcli
        run: curl -L https://zerops.io/zcli/install.sh | sh
      - name: zcli login
        run: zcli login "${{ secrets.ZEROPS_TOKEN_STAGE }}"
      - name: zcli push
        run: zcli push --service-id <stage-svc-id> --setup <stage-setup-name>
```

**B) Secret-set command** (stdin form — no shell-transcript leak):

Container variant:
```bash
echo "$ZCP_API_KEY" | gh secret set ZEROPS_TOKEN_STAGE -R <owner>/<repo>
```

Local variant:
```bash
jq -r '.mcpServers.zcp.env.ZCP_API_KEY' .mcp.json | gh secret set ZEROPS_TOKEN_STAGE -R <owner>/<repo>
```

**C) GitHub PAT recommendation** (re-affirm scopes from §2.3 if user is creating token now).

### 3.3 Distinct secret name

Stage uses secret name **`ZEROPS_TOKEN_STAGE`**, not bare `ZEROPS_TOKEN`. Prevents collision with prod secret in the same repository.

### 3.4 State stamping

```
ServiceMeta {
  BuildIntegration: Actions
}
```

### 3.5 Trigger semantics

Push to `main` → GitHub Actions runs → `zcli push` deploys to stage service-stack with setup name `<stage-setup-name>` (pair-keyed setup, typically `stage` or recipe-tier-specific).

### 3.6 Webhook alternative (first-class)

When user picks Webhook in §2.5 method-prompt:

ZCP emits atom `setup-build-integration-webhook.md` updated to include:
- Deep-link to source service-stack's source-code page (`/dashboard/project/<pid>/service-stack/<sid>/service-stack-source-code`)
- Steps: Connect to GitHub/GitLab → authorize Zerops org-level OAuth (one-time) → pick event type Branch + branch `main` + Zerops yaml setup `<stage-setup-name>`
- After user completes dashboard wiring, Zerops pulls from git on every push to `main`

State stamping after user confirms wiring in dashboard:
```
ServiceMeta {
  BuildIntegration: Webhook
}
```

ZCP read-only verifies webhook state via `client.GetServiceStackIntegrationStatus` (existing wired call); never PUTs. P-LP-7 stays.

The webhook path uses the same `BuildIntegration` axis as Actions but a different value. Both first-class.

---

## 4. Production CI/CD

**Purpose:** Auto-deploy production on every git tag matching `v*.*.*` (Actions) or every push to `main` (Webhook).

### 4.1 Trigger + method choice

Emitted in launch terminal phase (post `Status=launched`) as a structured method-prompt (client-agnostic, §11):

*"Production CI/CD method? (a) Actions push-mode [recommended], (b) Zerops native webhook, (c) None — manual `zcli push` only."*

Two cicd methods are first-class alternatives (NOT fallbacks):

| Method | Composer | Trigger | When user picks |
|---|---|---|---|
| **Actions push-mode** | `ComposeActionsHandoff(target=Prod)` (§4.2) | tag push `v*.*.*` | GitHub repo; preferred |
| **Zerops native webhook** | Path B webhook atom (§4.6) | push to `main` (configurable) | GitLab repo; preferred for non-GitHub; simpler setup |
| **None** | atom hint only | manual `zcli push` from CI of user's choice | user owns CI/CD |

Provider auto-detect (§2.2) influences the **recommended default** (GitHub → Actions, GitLab → Webhook), but the user/LLM picks explicitly. No silent fallback.

### 4.2 Composition (Actions path)

Identical pattern to §3.2 (shared `ComposeActionsHandoff` composer), but:

**A) Workflow YAML** at `.github/workflows/zerops-prod.yml`:

```yaml
name: zerops-prod-deploy
on:
  push:
    tags: ['v*.*.*']
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install zcli
        run: curl -L https://zerops.io/zcli/install.sh | sh
      - name: zcli login
        run: zcli login "${{ secrets.ZEROPS_TOKEN_PROD }}"
      - name: zcli push
        run: zcli push --service-id <prod-svc-id> --setup stage
```

**Setup-name default = `stage`.** Prod cicd reuses the existing `setup: stage` block in source `zerops.yaml`. The "stage" name is a build-recipe identifier in the yaml, NOT a destination semantic — the same build process (production-shaped: optimized assets, env=production, etc.) deploys to both stage service-stacks and prod service-stacks. Destination is the `--service-id`, not the setup name. Composer atom guidance reminds the user this default works; if prod build needs to diverge, user duplicates as `setup: prod` and overrides via `WorkflowInput.ProdSetupNameOverride`.

**B) Secret-set command** (stdin form, user pastes value from dashboard):

```bash
echo "<paste-prod-token>" | gh secret set ZEROPS_TOKEN_PROD -R <owner>/<repo>
```

The literal `<paste-prod-token>` placeholder is substituted by the user in their shell. The value is a fresh project-scoped token generated by user on the new prod project's dashboard. ZCP never holds it.

**C) Token generation atom** — deep-link to prod project's dashboard "Custom access per project, Full access" creation flow, with scope reminder.

### 4.3 Why raw `zcli`, not `zeropsio/actions@v1.0.2`

The `zeropsio/actions` GitHub Action does not accept a `setup` parameter (see `internal/tools/workflow_build_integration.go::actionsWorkflowYAML`). Setup-aware deploys require raw `zcli push --setup <name>`. Both stage and prod use the same raw form.

### 4.4 Distinct secret name + atom ordering

Prod uses **`ZEROPS_TOKEN_PROD`**, distinct from `ZEROPS_TOKEN_STAGE`. Both can coexist in the same repository without overwriting each other.

**Atom ordering** (P-LP-10): `launch-generate-prod-token` and `launch-cicd-actions-handoff` MUST appear before `launch-delete-key` in the terminal response. `launch-delete-key` (LaunchKey revocation) carries a prerequisite check: `gh secret list -R <owner>/<repo>` must show `ZEROPS_TOKEN_PROD` present.

### 4.5 First deploy after launch

User pushes a tag (`git tag v0.1.0 && git push --tags`) to trigger the first prod build. Prod runtime service exists as infra shell (`startWithoutCode:true`, no `buildFromGit` — see §6.4), so no pull-mode pre-build happens.

### 4.6 Webhook alternative (first-class)

When user picks Webhook in §4.1 method-prompt:

ZCP emits atom `launch-pipeline-configure-dashboard.md` (kept alive — not deprecated) updated with prod-project context:
- Deep-link to **target prod project's** service-stack source-code page
- Steps: Connect to GitHub/GitLab → authorize Zerops org-level OAuth → pick event type **Tag** + tag regex `^v\d+\.\d+\.\d+$` + Zerops yaml setup **stage** (the build-recipe name — see §4.2)
- User completes dashboard wiring, then re-invokes launch with same `launchKey` (within window); ZCP refreshes `pipelineConfigurations` via existing `executeLaunchPipelineCheck` (Path B verify, read-only)

State recorded in `WorkflowState.Launch.PipelineConfigurations[hostname]` (preserved — see §9.6 and §11).

The webhook path uses Path B pull-mode integration: Zerops server pulls from git on tag push, builds with the named setup, deploys to the configured service-stack. ZCP read-only verifies via `client.GetServiceStackIntegrationStatus`; never PUTs (P-LP-7 stays).

Both Actions push-mode and Webhook pull-mode produce identical functional behavior (prod auto-deploys on tag push). Choice is workflow/UX preference.

---

## 5. Export

**Purpose:** Package a running configuration (dev + managed deps OR stage + managed deps) as a self-referential `zerops-project-import.yaml` bundle the user can commit to their repo and re-import elsewhere.

### 5.1 Multi-call narrowing

`workflow=export` is stateless. Each MCP call carries the narrowing inputs (`TargetService`, `Variant`, `EnvClassifications`); ZCP responds with the next status.

```
scope-prompt → variant-prompt → [scaffold-required | git-push-setup-required] →
              classify-prompt → publish | validation-failed
```

### 5.2 Server-direct baseline

ZCP invokes `client.GetProjectExport(projectID)` (already wired at `internal/ops/export.go:46`) for the canonical yaml as known to the platform. This baseline is **authoritative for** all service/project shape that the platform set itself (auto-generated tags, server-injected fields).

### 5.3 ZCP layered transforms

On top of the server baseline, ZCP applies typed Variant transforms:

| Variant | Transform |
|---|---|
| `Dev` | Strip runtime envs (target imports fresh secrets); add `buildFromGit: <origin URL>@<current commit>` self-snapshot; preserve managed-dep config; preserve `enableSubdomainAccess` |
| `Stage` | Same as Dev but for stage half of pair; runtime envs from stage service; `buildFromGit` from stage half |

Each transform is a closed-set rule (no function-field map). Pin: `TestBundleCompose_Variant_*`.

### 5.4 Env classification (server-driven, simplified to 3 rules)

Server is authoritative for env taxonomy. Two distinct enums on the server (verified against eval-zcp live API — see `plans/research/env-types-investigation-2026-05-14.md`):

**Project envs** carry `Type EnvTypeEnum` (`USER` | `SYSTEM`) + `Sensitive bool` + `Editable bool`.

**Service envs** carry `Type UserDataTypeEnum` (`READ_ONLY` | `EDITABLE` | `SECRET` | `INTERNAL` | `ENV`) + `Sensitive bool`. No `Editable` field.

Layer 3 classifier splits envs into 4 SecretClassifications:
- `infrastructure` (target regenerates own — never composed into target yaml)
- `auto-secret` (sensitive, generate at re-import via `<@generateRandomString(<32>)>` preprocessor)
- `external-secret` (sensitive, user supplies at re-import)
- `plain-config` (non-sensitive, copy literal)

**Classifier rules — 3 total:**

| Scope | Rule | Outcome |
|---|---|---|
| **Service envs** (any service stack) | Always drop | Target's own managed services regenerate equivalent keys (`accessKeyId`/`apiUrl`/`secretAccessKey`/etc.). Source service envs are never carried over. |
| **Project envs, `Type=SYSTEM`** | Always drop | Platform-injected (`zeropsSubdomainHost`, `staticCdnUrl`, `envIsolation`, etc.). Target project regenerates own. The `Editable=true` subset (`envIsolation`/`sshIsolation`) is dropped same as `Editable=false` — both are platform defaults. |
| **Project envs, `Type=USER`** | LLM classifies | Bias: if `Key` matches pattern `(?i)(_KEY\|_SECRET\|_TOKEN\|_PASS\|APP_KEY)$` → suggest `auto-secret`; otherwise → suggest `plain-config`. LLM has final say (may upgrade to `external-secret` for known third-party creds). |

**`Sensitive` flag** is supplementary signal, not authoritative — server marks `ZCP_API_KEY` (a literal bearer token) as `Sensitive=false` on eval-zcp, proving the flag is guidance not truth. LLM uses the flag as input but does not auto-elevate based on it.

**F19 (CDN/object-storage keys) resolved**: CDN URLs are `Type=SYSTEM` + `Editable=false` (verified live). Object-storage service envs are `Type=READ_ONLY` (all of them, including credentials). Both classes hit "always drop" via the table above. No pattern detection, no static table, no fallback logic.

### 5.5 Validation

Client-side JSON schema validation via `schema.ValidateImportYAML` (kept). Server has no project-import-yaml validation endpoint; `PostServiceStackZeropsYamlValidation` validates per-runtime `zerops.yaml` (different document).

For each runtime service in the bundle, additional server-side validation MAY be invoked per its zerops.yaml content (already wired at `internal/platform/zerops_validate.go::ValidateZeropsYaml`).

### 5.6 Publish

The bundle (yaml + warnings + nextSteps) is returned to the user. ZCP does not commit/push for the user — emitted instructions cover `git add <file>`, `git commit`, `git push`.

### 5.7 Repository preconditions

If `git remote get-url origin` is empty (no remote configured), export status is `git-push-setup-required`. Response chains to `action=git-push-setup` (§2) before resuming export.

Once `meta.RemoteURL` is stamped, export re-validates against `git remote get-url origin` at publish time (drift surfaces as a warning).

---

## 6. Production launch

**Purpose:** Provision a Zerops project that runs production code, with clean trust boundary, optional cross-project handoff via push-mode CI/CD.

### 6.1 State machine

8 states (`topology.LaunchProductionStatus`):

| State | Triggered when |
|---|---|
| `unset` | First call, no inputs |
| `awaiting-project-mode-choice` | After first call; user has not yet chosen New vs Existing project mode |
| `scope-prompt` | Project mode chosen; scope inputs (target service, variant, region, etc.) missing |
| `classify-prompt` | Scope complete; envs need classification |
| `ready-to-launch` | All inputs complete; `SourceSnapshot` baseline written here |
| `launching` | Mutation in flight (`LaunchKey` or `ExistingProdToken` supplied) |
| `launched` | Terminal success; terminal phase emits cicd handoff |
| `failed` | Terminal failure |

State persists in `WorkflowState.Launch` (promoted from side-channel — see §9.4).

### 6.2 Project mode choice (Q1)

First user-facing decision, emitted as a structured method-prompt (client-agnostic, §11):

*"Production project? (a) Vytvořit nový — uses Zerops launchKey, 5–15 min window. (b) Použít existující — uses project-scoped token on the target project."*

ZCP **emits and waits** — never silently assumes. The MCP response carries `status: awaiting-project-mode-choice` with the prompt text and a typed options array. LLM/client relays to user; user replies; LLM re-calls with `LaunchProjectMode` field populated (`New` or `Existing`). No default value assumed — empty `LaunchProjectMode` keeps the workflow in `awaiting-project-mode-choice` state.

State transitions:
- `LaunchProjectMode=New` → `scope-prompt`
- `LaunchProjectMode=Existing` → if `ExistingProjectID` empty → re-emit prompting for it; else → token validation (§1.4) → `scope-prompt` on success, `failed` on token-scope mismatch

### 6.3 Scope + classify

| Field | NEW project path | EXISTING project path |
|---|---|---|
| `TargetService` (source runtime) | required | required |
| `Variant` | n/a (launch always packages source-as-prod) | n/a |
| `ProductionProjectName` | required | **must equal** existing project's name |
| `Region` | required | n/a (existing has region) |
| `CustomDomain` | optional | optional |
| `KeepNonHA` (managed deps that stay NON_HA) | optional | optional |
| `EnvClassifications` (map per env key) | required for `Type=USER` envs in source | same |
| `ExistingProjectID` | n/a | required |

### 6.4 Import yaml composition

ZCP composes the import yaml using Layer 1 `Bundle.Compose(Variant=Launch, ...)`:

**Runtime service entries (production-specific shape):**

```yaml
- hostname: <runtime>
  type: <stack-type>
  mode: HA                            # opt-out via KeepNonHA
  priority: 10
  minContainers: 2
  verticalAutoscaling:
    cpuMode: DEDICATED
  zeropsSetup: stage                  # default: reuse stage build recipe (§4.2)
                                       # override via ProdSetupNameOverride
  startWithoutCode: true              # NO first-build wait
  # NO buildFromGit                   # push-mode CI/CD only (§4)
```

`zeropsSetup: stage` means the new prod service-stack will build using the `setup: stage` block from the source repo's `zerops.yaml` (production-shaped build recipe — optimized assets, env=production). Same recipe, different destination. User overrides via `WorkflowInput.ProdSetupNameOverride` only if prod build needs to diverge (rare).

**Managed service entries:** preserved from source with HA promotion (unless in `KeepNonHA` opt-out).

**Project block:**

```yaml
project:
  name: <ProductionProjectName>
  tags:
    - env:prod
    - source-project:<source-project-id>
    - managed-by:zcp-launch
  envVariables:
    # classified envs from §5.4 rules (infrastructure → re-emit references;
    # plain-config → copy literal; auto-secret/external-secret → preprocessor sentinels)
```

### 6.5 Source snapshot (P-LP-3)

At state transition `classify-prompt → ready-to-launch`, ZCP writes `SourceSnapshot` to `WorkflowState.Launch.SourceSnapshot`:

```
SourceSnapshot {
  CommitSHA        // current source repo HEAD commit
  YAMLHash         // SHA256 of zerops.yaml
  EnvDigest        // hash of classified env keys + buckets
  ServiceListDigest // hash of source service inventory
}
```

At state transition `ready-to-launch → launching`, ZCP recomputes and compares against persisted baseline. Drift refuses with `source-drift` blocker. Pin: `TestPersistsSnapshotAtReadyToLaunch`, `TestRefusesOnSourceDriftBetweenReadyAndPublish`, `TestRefusesOnTamperedStateFile`.

### 6.6 Mutation

Two paths, differing only in API call:

**NEW project path:**

```
1. Construct ProjectAdminClient(LaunchKey, ...) [defer Close]
2. Validate SourceSnapshot (P-LP-3 compare)
3. PostClientProjectImport(yaml) → returns new projectID + ImportedServices
4. GrantSelfRole on new project (A.10 role assignment)
5. Audit log entry (no LaunchKey)
```

**EXISTING project path:**

```
1. Construct *Client(ExistingProdToken, ...) [defer Close]
2. Validate token scope: GetUserInfo → ListProjects → exactly 1, matches ExistingProjectID
3. Validate SourceSnapshot (P-LP-3 compare)
4. Preflight: ListServices on existing project → check hostname conflicts with about-to-import services
   - Conflict → refuse with hostname-conflict blocker (user resolves manually)
5. For each project-level env: CreateProjectEnv(projectID, key, content, sensitive)
6. PostProjectServiceStackImport(projectID, services-only-yaml)
   - "services-only-yaml" strips the project: block (project envs/tags already set in step 5)
7. Audit log entry (no ExistingProdToken)
```

`PostProjectServiceStackImport` rejects yaml with a `project:` block (per `internal/ops/import.go:90`). The split (separate project-env mutation + services-only import) is mandatory.

### 6.7 Post-launched terminal

On `Status=launched`, ZCP emits terminal response composed by `launchTerminalCompose(corpus, state)`:

```
1. launch-generate-prod-token   (deep-link to new prod project dashboard;
                                  user generates fresh project-scoped token)
2. launch-cicd-actions-handoff  (workflow YAML + stdin gh secret set for
                                  ZEROPS_TOKEN_PROD; via ComposeActionsHandoff(Prod))
3. launch-delete-key            (LaunchKey revoke; prereq: gh secret list confirms
                                  ZEROPS_TOKEN_PROD present in repo;
                                  ONLY for NEW project path — EXISTING path
                                  has no LaunchKey to revoke)
4. launch-post-checklist        (push tag v0.1.0 → first deploy triggers)
```

Atom ordering invariant (P-LP-10): atoms 1 and 2 always appear before atom 3.

For EXISTING project path, atoms 1+2 are emitted; atom 3 is omitted (no LaunchKey).

### 6.8 Production policy compliance

The new prod project does NOT contain a `zcp@1` service. ZCP does not run inside the prod project. This is enforced in the composer (Layer 1) — Launch variant emits no `zcp@1` entry.

---

## 7. Cross-area interactions

How the five areas combine.

### 7.1 Token reuse across surfaces

Single Zerops project token (`ZCP_API_KEY`) flows through:

```
ZCP runtime                     → reads env var
       ↓
zcli (in GitHub Actions)        → secrets.ZEROPS_TOKEN_{STAGE,PROD} == ZCP_API_KEY value
       ↓
ZCP composes handoff atoms      → emits "echo $ZCP_API_KEY | gh secret set ZEROPS_TOKEN_STAGE -R ..."
                                  (container: shell-expand;
                                   local: jq from .mcp.json)
```

For prod, the token is a NEW project-scoped token on the new prod project (Option B). For stage, the token is the source project's existing `ZCP_API_KEY`.

### 7.2 Git-push ↔ Stage CI/CD

Single user flow:
1. `action=git-push-setup` confirm → stamps `RemoteURL`
2. Co-emitted ASKUSERQUESTION Y default: "Auto-deploy stage?"
3. Y → `ComposeActionsHandoff(Stage)` runs in same response

No separate `action=build-integration` hop required for the common case.

### 7.3 Git-push ↔ Export

`meta.RemoteURL` cache is shared. Export refuses publish if no remote configured; chains to git-push-setup before resuming.

### 7.4 Git-push ↔ Launch

Production launch does **not** require git-push-setup beforehand. Prod runtime entries use `startWithoutCode:true` + no `buildFromGit` — the deploy comes from GitHub Actions push-mode, not server pull. However, the prod cicd handoff (§4) emits a workflow file that must be committed to the source repo, so the source project's git-push capability is a precondition for code reaching production.

For users who run launch without ever configuring git-push on the source, the terminal phase still emits the workflow file content; the user must commit it manually or skip prod CI/CD entirely (manual `zcli push` from local).

### 7.5 Stage CI/CD ↔ Production CI/CD

Both share `ComposeActionsHandoff` composer (Layer 1b). Differences:

| Aspect | Stage | Production |
|---|---|---|
| Workflow file | `.github/workflows/zerops-stage.yml` | `.github/workflows/zerops-prod.yml` |
| Trigger | `push: branches: [main]` | `push: tags: ['v*.*.*']` |
| Secret name | `ZEROPS_TOKEN_STAGE` | `ZEROPS_TOKEN_PROD` |
| Setup name | pair-keyed (`stage`/recipe-tier) | `prod` |
| Service ID | source stage service-stack | target prod service-stack |
| Token source | reuse `$ZCP_API_KEY` (same project as ZCP) | NEW project-scoped token (Option B) |
| Where emitted | inline in git-push-setup | launch terminal phase |

Both coexist in the same repository without secret collision.

### 7.6 Export ↔ Launch

Both share Layer 1 bundle composer. Variant axis dispatches:

| Variant | Composer behavior |
|---|---|
| `Export-Dev` | self-snapshot dev half, env-strip, NON_HA preserved, `buildFromGit` self-referential |
| `Export-Stage` | self-snapshot stage half, env-strip, NON_HA preserved, `buildFromGit` self-referential |
| `Launch` | source pair → prod, HA promotion, DEDICATED cpuMode, `minContainers:2`, **no `buildFromGit`**, **`startWithoutCode:true`** |

Both share Layer 3 env classifier (SDK-driven).

### 7.7 New project ↔ Existing project (launch sub-modes)

Q1 ASKUSERQUESTION dispatches to one of two mutation paths:

| Aspect | NEW | EXISTING |
|---|---|---|
| Trust credential | `LaunchKey` (account-wide transient) | `ExistingProdToken` (project-scoped transient) |
| Validation | Constructor `GetUserInfo` | `GetUserInfo` + `ListProjects` (single-project + ID match) |
| API call | `PostClientProjectImport(full yaml)` | `CreateProjectEnv` (per env) + `PostProjectServiceStackImport(services-only yaml)` |
| Role grant | Yes (`GrantSelfRole`) | No (user already owns project) |
| Recovery on failure | `DeleteProject` available (within launchKey window) | None (user owns project; manual cleanup) |
| `launch-delete-key` atom | Yes (revoke LaunchKey) | No (no LaunchKey exists) |
| Hostname conflict preflight | n/a (new project always empty) | Required (refuse before mutation) |

### 7.8 ASKUSERQUESTION sites summary

Three new sites:

| Site | Question | Default |
|---|---|---|
| `action=git-push-setup` (start) | "Máš git repo?" | — (3 options) |
| `action=git-push-setup` (confirm) | "Auto-deploy stage on every push to main?" | Y |
| `workflow=launch-production` (start) | "Production project? New / Existing" | — (2 options) |

All use the existing MCP elicitation pattern (server returns prompt, client/LLM relays, next call carries answer in typed input field).

---

## 8. Invariants

Twelve behavior-level invariants. Each enforced by code structure, pin test, OR both.

| ID | Invariant | Enforcement |
|---|---|---|
| **P-LP-1** | `LaunchKey` and `ExistingProdToken` never appear in state files, audit log, MCP response, or formatted log strings | Compile-time `CredentialField` interface + AST sentinel + serialization fixture |
| **P-LP-2** | `ProjectAdminClient` (the launchKey-gated client) is imported only by `internal/tools/workflow_launch_production.go` and `internal/tools/launch_pipeline.go` (both kept alive — webhook flow uses `launch_pipeline.go` Path B verify) | `TestProjectAdminClientRestrictedImport` AST scan |
| **P-LP-3** | `SourceSnapshot` baseline written at `ready-to-launch` transition; compared against current at `launching` transition; refuse on drift | Two-site fix: `launchReadyToLaunchResponse` writes baseline; `executeLaunchMutation` reads + compares. Pin: `TestPersistsSnapshotAtReadyToLaunch`, `TestRefusesOnSourceDriftBetweenReadyAndPublish`, `TestRefusesOnTamperedStateFile` |
| **P-LP-4** | `launched` response includes mandatory `launch-delete-key` atom (NEW project + Actions cicd path; absent for EXISTING project; for Webhook/None path, prerequisite differs — wiring confirmed in dashboard / immediate) | Composer-level pin (`TestComposeLaunchTerminal_NewProjectActions_IncludesDeleteKey`) |
| **P-LP-5** | `EnvKey` carries `(Key, Sensitive)` but never `Value` | Struct definition + `TestEnvKey_NoValueField` |
| **P-LP-6** | Audit log appended via `O_APPEND` `0o600`, one JSON object per line | Runtime path + `TestAuditLog_AppendOnlyMode` |
| **P-LP-7** | ZCP never calls `PutStandardServiceStackTriggerExternalRepositoryIntegration` (no PUT-side integration writes); CI/CD wiring is user-driven (Actions: user runs `gh secret set` + commits workflow file; Webhook: user clicks OAuth in Zerops dashboard) | `TestExecuteLaunch_NoPutCallsByZCP` (no consumer of this SDK call anywhere in production code) |
| **P-LP-8** | Pipeline-integration failures (webhook path) surface as `Severity=Warn` blockers; never block `Status=launched` | `TestPipelineCheck_FailuresAreWarn` |
| **P-LP-9** | `noExternalRepositoryIntegration` HTTP-400 error code maps to `IntegrationStatus.State=NotConfigured` (state read, not error propagation) | `mapIntegrationOutput` test in `platform/project_admin.go` |
| **P-LP-10** (new) | Atoms `launch-generate-prod-token` and `launch-cicd-actions-handoff` appear before `launch-delete-key` in terminal response (Actions path only) | Composer-level pin (`TestComposeLaunchTerminal_ActionsPath_GenerateBeforeRevoke`) |
| **P-LP-11** (new) | Production runtime entries have `startWithoutCode:true` AND no `buildFromGit` field | Composer-level pin (`TestBundleCompose_LaunchVariant_NoBuildFromGit`, `TestBundleCompose_LaunchVariant_StartWithoutCodeTrue`) |
| **P-LP-12** (new) | `ExistingProdToken` validates against exactly 1 project AND matches user-declared `ExistingProjectID` before any mutation | Pre-mutation gate (`TestExistingProjectToken_RefusesMultiProject`, `TestExistingProjectToken_RefusesScopeMismatch`) |
| **P-LP-13** (new) | Existing-project path uses `CreateProjectEnv` (per env) + `PostProjectServiceStackImport` (services-only yaml). Composer refuses to emit `project:` block when Variant indicates existing-project import | Composer-level pin (`TestBundleCompose_ExistingProject_NoProjectBlock`) |
| **P-LP-14** (new) | Existing-project mutation refuses on hostname conflict with any service already in target project | Preflight gate (`TestExistingProjectImport_RefusesOnHostnameConflict`) |

Invariants P-LP-8 (pipeline failures = WARN, never block `launched`) and P-LP-9 (`noExternalRepositoryIntegration` → `IntegrationNotConfigured` mapping) **stay alive** — webhook flow is a first-class alternative (§3.6, §4.6); Path B verify uses both invariants.

---

## 9. Layered architecture

ZCP code is organized into 4 layers above a thin Layer 0 of server-direct calls. Imports must respect the layer rule (enforced by `internal/topology/architecture_test.go` + `.golangci.yaml::depguard`).

### 9.1 Layer 0 — Server-direct (existing wrappers)

| SDK call | ZCP wrapper | Purpose |
|---|---|---|
| `GetProjectExport(projectID)` | `client.GetProjectExport` (already in `internal/platform/client.go`) | Canonical project yaml baseline for export composer |
| `GetServiceStackEnv` / `GetProjectEnv` | `client.GetServiceEnv` / `client.GetProjectEnv` | Env reads with Type/Sensitive/Editable shape |
| `PostServiceStackZeropsYamlValidation` | `client.ValidateZeropsYaml` (already at `internal/platform/zerops_validate.go`) | Per-runtime zerops.yaml validation (NOT project import yaml) |
| `PostClientProjectImport` | `ProjectAdminClient.CreateAndImportProject` | NEW project mutation (launchKey-gated) |
| `PostProjectServiceStackImport` | `client.ImportServices` (already at `internal/platform/zerops_search.go`) | Services-only import into existing project |
| `CreateProjectEnv` | `client.CreateProjectEnv` | Per-env mutation (existing project path) |
| `ListProjects` | `client.ListProjects` | Token-scope validation |

### 9.2 Layer 1 — `internal/ops/bundle/` (Bundle composer)

```go
type Variant int
const (
    VariantExportDev   Variant = iota  // self-snapshot dev, env-strip
    VariantExportStage                 // self-snapshot stage, env-strip
    VariantLaunchNew                   // HA promotion, startWithoutCode, no buildFromGit, full project block
    VariantLaunchExisting              // HA promotion, startWithoutCode, no buildFromGit, NO project block
)

type Compose struct {
    ServerBaseline string         // from GetProjectExport
    Variant        Variant
    Inputs         BundleInputs   // ServiceTypeRules table closed set
    Classifications map[string]SecretClassification
}

func (c Compose) Build() (BundleResult, error)
```

`ServiceTypeRules` is a closed table (AcceptsMode, RequiresObjectStorageSize, AllowsBuildFromGit, etc.) — no function-field maps.

### 9.3 Layer 1b — `internal/ops/cicd/` (CICD handoff composer)

```go
type CICDTarget int
const (
    TargetStage CICDTarget = iota
    TargetProd
)

type ActionsHandoffInput struct {
    Target          CICDTarget
    OwnerRepo       string         // "owner/repo" derived from ParseGitRemoteOwnerRepo
    ServiceID       string         // stage svcID OR prod svcID
    SetupName       string         // "stage" pair-keyed OR "prod"
    SecretName      string         // ZEROPS_TOKEN_STAGE or ZEROPS_TOKEN_PROD
    TokenSourceExpr string         // "$ZCP_API_KEY" container | jq local | placeholder for prod paste
    Trigger         TriggerSpec    // {branch=main} OR {tags=v*.*.*}
    Env             EnvMode        // Container | Local
}

type ActionsHandoffOutput struct {
    WorkflowYAML      string
    SecretSetCommand  string         // stdin form
    GhPatRecommendation string
    Instructions      []string
}

func ComposeActionsHandoff(in ActionsHandoffInput) ActionsHandoffOutput
```

Used by both `action=git-push-setup` confirm (Stage) and launch terminal (Prod).

Provider detection (`internal/ops/cicd/provider.go`):

```go
type Provider int
const (
    ProviderGitHub Provider = iota
    ProviderGitLab
    ProviderUnknown
)

func DetectProvider(remoteURL string) Provider  // github.com / gitlab.com / gitlab.* / other
```

For `ProviderGitLab` and `ProviderUnknown`, ZCP falls back to webhook atom guidance (manual dashboard OAuth) for stage cicd; production cicd refuses Actions and emits manual `zcli push` instructions only.

### 9.4 Layer 2 — `internal/ops/inventory/` (Inventory + envs)

Service inventory:

```go
func ListProjectServices(ctx, client, projectID) ([]Service, error)  // existing
func LookupService(ctx, client, projectID, hostname) (*Service, error)
```

Env reads with scope-specific shape (two distinct SDK enums per `plans/research/env-types-investigation-2026-05-14.md`):

```go
// internal/topology/types.go

type ProjectEnvType string
const (
    ProjectEnvUser   ProjectEnvType = "USER"
    ProjectEnvSystem ProjectEnvType = "SYSTEM"
)

type ServiceEnvType string
const (
    ServiceEnvReadOnly ServiceEnvType = "READ_ONLY"
    ServiceEnvEditable ServiceEnvType = "EDITABLE"
    ServiceEnvSecret   ServiceEnvType = "SECRET"
    ServiceEnvInternal ServiceEnvType = "INTERNAL"
    ServiceEnvEnv      ServiceEnvType = "ENV"
)

// internal/ops/inventory/envs.go

type ProjectEnvVar struct {
    Key       string
    Content   string
    Type      ProjectEnvType
    Sensitive bool
    Editable  bool
}

type ServiceEnvVar struct {
    Key       string
    Content   string
    Type      ServiceEnvType
    Sensitive bool
    // no Editable — SDK service env DTO doesn't carry it
}

func FetchProjectEnvs(ctx, client, projectID) ([]ProjectEnvVar, error)
func FetchServiceEnvs(ctx, client, serviceID) ([]ServiceEnvVar, error)
```

Existing `EnvVar { ID, Key, Content }` is removed (Layer 1a redesign — see Part II). The two enums are NOT aliased — they are different platform concepts (project-level user authorship vs service-stack-level platform classification). Mixing them is a type error at compile time.

### 9.5 Layer 3 — `internal/envclass/` (SDK-driven classifier)

```go
package envclass

type Decision int
const (
    Drop Decision = iota          // never composed into target yaml
    PromptUser                    // bias guidance, LLM finalizes
)

type Result struct {
    Decision Decision
    Bias     SecretClassification // when PromptUser; otherwise zero
}

// Always returns {Drop, _} — service envs are universally dropped.
func ClassifyServiceEnv(env inventory.ServiceEnvVar) Result

// Returns {Drop, _} for Type=SYSTEM; {PromptUser, bias} for Type=USER.
// Bias = AutoSecret when Key matches credentialPattern;
// PlainConfig otherwise.
func ClassifyProjectEnv(env inventory.ProjectEnvVar) Result
```

Three classifier rules total (§5.4):
1. All service envs → `Drop`
2. Project env `Type=SYSTEM` → `Drop`
3. Project env `Type=USER` → `PromptUser` with name-pattern bias

Implementation is ~30 LOC pure function. No static tables, no fallback patterns. F19 (CDN/object-storage backlog) closes — server `Type=SYSTEM` covers all platform-managed project envs (verified live).

Package `envclass` has zero non-stdlib imports beyond `topology` and `inventory`. It is a peer of `ops/` (not a child) so `ops/bundle` imports it.

### 9.6 Layer 4 — `internal/workflow/` (State + composers)

```go
type WorkflowState struct {
    Version    string          // "2" — bumped from "1" with Launch promotion
    SessionID  string
    PID        int
    ProjectID  string
    Workflow   string
    Iteration  int
    Intent     string
    Bootstrap  *BootstrapState
    Recipe     *RecipeState
    Launch     *LaunchState    // promoted from side-channel .zcp/state/launch-production/
}

type LaunchState struct {
    LaunchID                string                     // deterministic hash, preserved
    ProjectMode             LaunchProjectMode          // New | Existing
    SourceProjectID         string
    SourceRepoURL           string
    TargetProjectID         string
    TargetProjectName       string
    ExistingProjectID       string                     // only when ProjectMode=Existing
    TargetServiceHostname   string
    ImportedServices        []ImportedServiceEntry
    SourceSnapshot          snapshot.Source            // moved to internal/snapshot/
    Classifications         map[string]SecretClassification
    Status                  LaunchProductionStatus
    CreatedAt               time.Time
    LastUpdate              time.Time
    LastError               string                     // P-LP-1: never carries credential
    PipelineConfigurations  map[string]PipelineConfigEntry   // webhook flow state (§4.6)
    PipelineCheckedAt       time.Time                        // last Path B verify
    CICDMethod              CICDMethod                       // Actions | Webhook | None (terminal phase)
}
```

`SourceSnapshot` lives in `internal/snapshot/` (neutral package — neither `ops/` nor `workflow/` owns it, satisfying layer rule).

`LaunchProjectMode` is `topology.LaunchProjectMode` (`New` | `Existing`).

### 9.7 Layer rule (depguard + architecture_test)

```
topology/             imports stdlib only
platform/             imports topology, stdlib, zerops-go SDK
snapshot/             imports topology, stdlib
envclass/             imports topology, stdlib, inventory
inventory/            imports topology, platform
ops/bundle/           imports topology, platform, inventory, envclass, snapshot
ops/cicd/             imports topology, platform, inventory
workflow/             imports topology, platform, inventory, envclass, snapshot, ops/* (limited)
tools/                imports all of above
cmd/                  imports tools, server
```

`ops/` does NOT import `workflow/` or `tools/`. `workflow/` does NOT import `tools/`. Recipe package (`internal/recipe/`) is a separate v3 scope and is not part of this graph.

---

# PART II — IMPLEMENTATION PLAN

## 10. Methodology

ZCP follows TDD and foundation-first refactor discipline.

### 10.1 TDD discipline

```
RED → GREEN → REFACTOR
```

Every phase ships pin tests for the new behavior. Pure refactors (no behavior change) skip RED; verify all layers stay green.

Test layers and their gates:

| Layer | Path | Triggered on |
|---|---|---|
| Unit | `./internal/...` | Edit (file save) |
| Tool | `./internal/tools/...` | Turn (assistant message) |
| Integration | `./integration/` (mock platform) | Commit (pre-commit hook) |
| E2E | `./e2e/ -tags e2e` (real Zerops via eval-zcp) | CI on release tag |

Pre-commit hook runs `make lint-local` (~15s) + `go test ./... -short` (~1 min). Release CI runs `go test -race ./... -count=1` + full `golangci-lint`.

### 10.2 Layer order (foundation-first)

Refactors proceed bottom-up:

```
topology  →  platform  →  snapshot  →  envclass  →  inventory  →  ops/{bundle,cicd}  →  workflow  →  tools  →  cmd
```

A change in layer N requires no edits in layer N-k (k>0). A change in layer N-k requires verification in every layer N-k+1, ..., N.

### 10.3 No backward-compat shims

Pre-production allowance: rename types freely, reshape tool JSON, restructure atoms, move packages. Compatibility shims are perpetual tax. State migration is one-way; legacy state files are wrapped under `.archive/` with operator-recoverable hatch.

### 10.4 Phased verification

Each phase has a verifiable boundary (`make lint-local && go test ./... -short && go test -tags e2e ./e2e/`). Phase rollback = git revert before next phase starts. Gates G1-G5 (§13) prevent advancement without evidence.

### 10.5 E2E + behavioral eval as primary truth (verification philosophy)

Unit tests verify pure code (compile-time invariants, classifier rules, composer logic). They are necessary but NOT sufficient — mock-based unit tests can pass while the feature is broken against the real platform.

**Each phase ships ONE explicit verification scenario** that proves the deliverable works against real Zerops. Scenarios run via `make test -tags e2e ./e2e/` (real platform — eval-zcp project, programmatic) or `flow-eval` / `flow-eval-local` (behavioral, agent-driven). The phase is "done" when its scenario is green.

Tier mapping (which test type per concern):

| Concern type | Test tier | Why |
|---|---|---|
| Compile-time invariants (DTO split, AST sentinel, depguard) | Unit | Static analysis; no runtime needed |
| Pure functions (classifier, composer, provider detect) | Unit (table-driven) | Determinism per input; fast |
| State transitions (P-LP-3 drift, migration) | Unit + e2e (one scenario per phase) | Logic + filesystem |
| Cross-project mutation (launch, existing-project import) | E2E only | Real API + real services; mocks insufficient |
| User flow (launch → cicd handoff → first deploy → tag push runs) | Behavioral eval | Agent-driven; observes actual UX |

Result: ~70% unit tests / 30% e2e+eval coverage by file count, but e2e+eval is the **gate evidence** (G1-G5). Unit failures stop a commit; e2e/eval failures stop a release.

No unit tests targeting "every code path" of glue code. If a 10-line glue function has no observable behavior outside an e2e flow that covers it, the e2e is the test.

---

## 11. Phase sequence

12 phases (Phase 0, 0.5, 1a, 1b, 1c, 2, 4a, 5, 4b, 6a, 6b, 7), 5 gates G1-G5, 4 releases v9.91 / v9.92 / v9.93 (now bundles 4a + 5 + 4b) / v9.95.

### ASKUSERQUESTION contract (client-agnostic)

ZCP uses structured method-prompts in multiple places (§2.5 stage cicd, §4.1 prod cicd, §6.2 project mode). The MCP response carries TWO surfaces in the same payload:

```json
{
  "status": "awaiting-cicd-method",
  "prompt": "Production CI/CD method? Choose: (a) Actions push-mode [recommended], (b) Zerops native webhook, (c) None — manual zcli push.",
  "options": [
    {"value": "Actions",  "label": "Actions push-mode", "recommended": true},
    {"value": "Webhook",  "label": "Zerops native webhook"},
    {"value": "None",     "label": "None — manual zcli push"}
  ],
  "inputField": "CICDMethod"
}
```

The `prompt` field is the plain-text question, used by clients that don't parse `options` (worst-case: LLM agent reads the prompt + relays to user verbatim). The `options` array is structured for clients that render rich choosers (Claude Code, future). `inputField` names the typed `WorkflowInput` field the next MCP call must populate with the chosen `value`.

**No per-client detection.** Same response shape for every MCP client. The prompt always works (LLM can read text); rich rendering is bonus. Karel's anchor clients are Claude Code (today) + Codex (incoming); both can consume this shape.

Pin: `TestStructuredPrompts_PromptFieldAlwaysPresent`, `TestStructuredPrompts_OptionsParseable`, `TestStructuredPrompts_InputFieldExistsInWorkflowInput`.

### Phase 0 — Goldens + reproducers + verification baseline

**Purpose:** Capture current behavior before any change. Anchor regression evidence + define what "works" looks like at each gate.

**Goldens directory split:**

```
testdata/goldens/regression/    Immutable. Snapshots of CURRENT launch + export
                                output for known fixture inputs. Captured once
                                in Phase 0; never updated unless intentional
                                behavior change (review-gated).
testdata/goldens/target/        Per-phase. Updated when a phase intentionally
                                changes observable output. Each phase that
                                modifies composer/handler output stamps its
                                phase ID in the diff PR.
```

**Deliverables:**

- `testdata/goldens/regression/launch-*.yaml` — current launch composer output for 3-4 fixtures (standard pair, dev-only, recipe-tier nodejs, recipe-tier laravel)
- `testdata/goldens/regression/export-*.yaml` — current export composer output for 2-3 fixtures (dev variant, stage variant)
- F19/F20/F21 reproducers committed as `internal/ops/bundle/regression_test.go::TestF19_*` (initially fail — proves bug reproducible)
- Drift-injection scenario `internal/tools/launch_drift_injection_test.go` — state file mutated between ready-to-launch and launching; mutation must refuse (initially passes incorrectly without P-LP-3 fix; serves as negative pin)
- **E2E scenario (gate G1 evidence):** `e2e/launch_existing_baseline_test.go` — provisions an object-storage + nodejs service in eval-zcp, runs `client.GetServiceEnv` and `client.GetProjectEnv`, asserts SDK fields (Type/Sensitive/Editable) decode correctly. Run via `go test -tags e2e ./e2e/launch_existing_baseline_test.go`. Replaces speculation about env types with live verification.
- New behavioral eval scenarios committed (run during Phase 7, declared in Phase 0):
  - `eval/behavioral/scenarios/launch-production-existing-project-token.md` (Q1 path b)
  - `eval/behavioral/scenarios/launch-production-new-project-push-mode.md` (Q1 path a)
  - `eval/behavioral/scenarios/git-push-setup-with-cicd-method-prompt.md` (§2.5 method prompt)
  - `eval/behavioral/scenarios/launch-production-existing-with-webhook.md` (§4.6 path)

**Gate G1 evidence (e2e + unit):**
- E2E baseline test green against eval-zcp (verifies SDK field shape live)
- Regression goldens captured (`testdata/goldens/regression/*` committed)
- F19/F20/F21 reproducers fail as expected (negative goldens)
- Drift-injection scenario refuses after Phase 0.5 lands
- All 4 new behavioral scenarios appear in `flow-eval.sh list`

**LOC:** ~500 (mostly test fixtures + 1 e2e file)
**Release:** v9.91 (in same release as Phase 0.5)

### Phase 0.5 — P-LP-3 active compare gate

**Purpose:** Fix the latent bug where `SourceSnapshot` is written only at mutation site and compared against itself.

**Files:**
- `internal/tools/workflow_launch_production.go`:
  - `launchReadyToLaunchResponse` (currently around L850–L890): persist `SourceSnapshot` baseline to `launchState` at state transition `classify-prompt → ready-to-launch`
  - `executeLaunchMutation` (currently L240–L350): read baseline, recompute current, compare; refuse on drift
- `internal/tools/launch_state.go`: ensure `SourceSnapshot` field persists in JSON state file

**Pin tests:**
- `TestPersistsSnapshotAtReadyToLaunch`
- `TestRefusesOnSourceDriftBetweenReadyAndPublish`
- `TestRefusesOnTamperedStateFile`

**Gate G1 evidence:** P-LP-3 negative pin passes (drift-injection scenario refuses).

**LOC:** ~150
**Release:** v9.91

### Phase 1a — EnvVar scope-specific extension

**Purpose:** Replace single `EnvVar { ID, Key, Content }` with scope-specific `ProjectEnvVar` and `ServiceEnvVar` carrying SDK-provided metadata.

**Files:**
- `internal/platform/types.go`: define new types
- `internal/platform/zerops_env.go`: propagate `Type` (USER|SYSTEM), `Sensitive`, `Editable` (project only) from SDK output to wrapper types. Update `GetServiceEnv` and `GetProjectEnv` signatures.
- `internal/platform/mock_methods.go`: update mock factory builders (`WithServiceEnv`, `WithProjectEnv`) to accept full shape. Existing test fixtures continue to work with Go zero-values (Type=USER, Sensitive=false, Editable=true).
- 8 callers in `internal/ops/` that read only `.Key`/`.Content` need no source changes (additive struct extension).
- 2 callers (`internal/ops/env_generate.go:204`, `internal/ops/subdomain.go:258`) read `.Content` value and need no awareness either.
- `internal/platform/project_admin.go::GetServiceEnvKeys`/`GetProjectEnvKeys` (lines 326-348): preserve P-LP-5 strip behavior; strip `Content` but propagate `Type`/`Sensitive`/`Editable` if useful.

**Pin tests:**
- `TestProjectEnvVar_CarriesEditable`
- `TestServiceEnvVar_NoEditableField`
- `TestEnvVar_AdditiveExtensionPreservesExistingCallers`

**Gate:** internal — must pass full unit + tool tests before Phase 1b.

**LOC:** ~120
**Release:** v9.92 (bundled with 1b/1c/2)

### Phase 1b — Layer 1 bundle composer

**Purpose:** Introduce `internal/ops/bundle/` package with typed Variant-based composer; reshape existing `composeImportYAML` to delegate.

**Files:**
- `internal/ops/bundle/compose.go` (NEW): `Compose` struct + `Variant` enum + `Build()` method
- `internal/ops/bundle/rules.go` (NEW): `ServiceTypeRules` closed table
- `internal/ops/bundle/launch_variant.go` (NEW): Launch variant rules — HA promotion, `startWithoutCode:true`, no `buildFromGit`, project block per New/Existing
- `internal/ops/bundle/export_variant.go` (NEW): Export-Dev / Export-Stage variants — env-strip, buildFromGit-self-snapshot
- `internal/ops/export_bundle.go`: rewrite `composeImportYAML` (currently L231–L287) to delegate to `bundle.Compose` with appropriate Variant
- `internal/ops/launch_bundle.go`: rewrite `BuildLaunchBundle` (currently L138–L264) to delegate

**Pin tests:**
- `TestBundleCompose_ExportDev_StripsEnvs`
- `TestBundleCompose_ExportDev_AddsBuildFromGitSelfSnapshot`
- `TestBundleCompose_ExportStage_StagePair`
- `TestBundleCompose_LaunchNew_NoBuildFromGit`
- `TestBundleCompose_LaunchNew_StartWithoutCodeTrue`
- `TestBundleCompose_LaunchNew_HAPromotion`
- `TestBundleCompose_LaunchExisting_NoProjectBlock`

**Gate:** golden tests from Phase 0 must still pass (Variant transforms preserve observable output for current functionality).

**LOC:** ~600 (significant rewrite, mostly moving logic to typed Variant rules)
**Release:** v9.92

### Phase 1c — Layer 1b CICD handoff composer

**Purpose:** Introduce `internal/ops/cicd/` package with shared composer for stage + prod CI/CD.

**Files:**
- `internal/ops/cicd/actions_handoff.go` (NEW): `ComposeActionsHandoff` function + types
- `internal/ops/cicd/provider.go` (NEW): `DetectProvider` from origin URL
- `internal/ops/cicd/workflow_yaml.go` (NEW): YAML template emission (raw `zcli install/login/push --setup <name>` pattern)
- `internal/tools/workflow_build_integration.go`: rewrite `actionsConfirmResponse` (currently L177–L240) to delegate to `cicd.ComposeActionsHandoff(Stage, ...)`

**Pin tests:**
- `TestComposeActionsHandoff_Stage_RawZcli` (no `zeropsio/actions@v1.0.2`)
- `TestComposeActionsHandoff_Stage_BranchTrigger`
- `TestComposeActionsHandoff_Prod_TagTrigger`
- `TestComposeActionsHandoff_Stage_DistinctSecretZeroOpToken_Stage`
- `TestComposeActionsHandoff_Prod_DistinctSecretZeroOpToken_Prod`
- `TestComposeActionsHandoff_TokenSourceExpr_ContainerVsLocal`
- `TestDetectProvider_GitHubDotCom`
- `TestDetectProvider_GitLabFallback`

**Gate:** existing eval `delivery-git-push-actions-setup.md` passes with new composer output.

**LOC:** ~400
**Release:** v9.92

### Phase 2 — Server-direct baseline + Layer 3 envclass + existing-project path

**Purpose:** Shift export composer to use `GetProjectExport` as baseline; replace static env table with 3-rule SDK-driven classifier; wire existing-project import path.

**Files:**
- `internal/envclass/classify.go` (NEW, ~30 LOC pure function): three rules per §5.4
- `internal/ops/inventory/envs.go` (NEW): `FetchProjectEnvs`, `FetchServiceEnvs` returning scope-specific types (project: 3 fields, service: 2 fields)
- `internal/ops/bundle/launch_variant.go`: use server baseline from `client.GetProjectExport` as starting point; apply Variant transforms on top
- `internal/ops/bundle/export_variant.go`: same
- `internal/tools/launch_platform_envs.go`: **DELETE** entire file — `platformEnvAutoClass` table + `mergePlatformAutoClassifications` + `needsClassifyPromptForLaunch` + `filterUserClassificationEnvs` superseded by `envclass.ClassifyProjectEnv`/`ClassifyServiceEnv`
- `internal/tools/workflow_launch_production.go`: existing-project mutation path using `client.CreateProjectEnv` (per project env) + `client.ImportServices` (services-only yaml after stripping `project:` block)

**Unit tests (pure functions):**
- `TestClassifyProjectEnv_SystemDrops` (covers both `Editable=true` and `Editable=false`)
- `TestClassifyProjectEnv_UserPattern_BiasAutoSecret` (`*_KEY|*_SECRET|*_TOKEN|*_PASS|APP_KEY` patterns)
- `TestClassifyProjectEnv_UserOther_BiasPlainConfig`
- `TestClassifyServiceEnv_AnyType_Drops` (all 5 UserDataTypeEnum values map to Drop)
- `TestExportComposer_UsesServerBaseline_LayersVariantTransforms`
- `TestLaunchComposer_ExistingProject_StripsProjectBlock`

**E2E scenario (gate G2 evidence):**
- `e2e/launch_existing_project_e2e_test.go`: provisions a temp eval-zcp project + project-scoped token for it; runs full existing-project launch flow via the handler against eval-zcp (token validation + CreateProjectEnv + ImportServices); asserts target project ends with imported services + classified envs. Includes negative case: token scoped to different project → refuse with structured `ErrTokenScopeMismatch`.

**Gate G2 evidence (e2e + unit):**
- Goldens (regression set) stay green
- F19/F20/F21 reproducers now PASS (composer handles object-storage modes correctly via server baseline)
- E2E existing-project scenario green against eval-zcp
- `e2e/launch_baseline_test.go` (from Phase 0) confirms SDK field decoding still consistent

**LOC:** ~600 (delete `launch_platform_envs.go` -150; new envclass +30 pure; inventory +120; existing-project handler path +250; bundle baseline integration +350)
**Release:** v9.92

### Phase 4a — State schema versioning

**Purpose:** Bump `WorkflowState.Version` from "1" to "2"; add `Launch *LaunchState` field; add `LaunchProjectMode` enum.

**Files:**
- `internal/workflow/state.go`: add `Launch *LaunchState`; bump Version
- `internal/topology/types.go`: add `LaunchProjectMode` (New | Existing); add `awaiting-project-mode-choice` to `LaunchProductionStatus` enum
- `internal/workflow/launch_state.go` (NEW): move `LaunchState` struct from `internal/tools/launch_state.go`; `SourceSnapshot` field references `internal/snapshot/`
- `internal/snapshot/source.go` (NEW): neutral package for `SourceSnapshot` type

**Pin tests:**
- `TestWorkflowState_Version2_DecodesLaunchField`
- `TestLaunchProjectMode_New_Existing_Aliases`
- `TestLaunchProductionStatus_IncludesAwaitingProjectModeChoice`

**Gate:** Internal — parallel-safe with Phase 5.

**LOC:** ~200
**Release:** v9.93 (parallel with Phase 5)

### Phase 5 — DTO split + AST sentinel

**Purpose:** Compile-time credential type system with three guards.

**Files:**
- `internal/auth/credentials.go` (NEW): `ZerropsToken`, `LaunchKey`, `ExistingProdToken` types + `CredentialField` interface
- `internal/tools/workflow.go`: `WorkflowInput.LaunchKey` typed as `auth.LaunchKey`; add `WorkflowInput.ExistingProdToken auth.ExistingProdToken`; add `WorkflowInput.ExistingProjectID string`
- `internal/tools/sentinel_test.go` (NEW): AST scan + serialization fixture
- `internal/tools/launch_state.go` (PRE-PROMOTION): no `LaunchKey` or `ExistingProdToken` fields (already enforced)

**Pin tests:**
- `TestCredentialField_NoLeakageInResponse`
- `TestCredentialField_NoLeakageInState`
- `TestCredentialField_NoLeakageInAudit`
- `TestSerializationFixture_SentinelAbsent`

**Gate G3:**
- `make lint-local` passes (depguard updated)
- AST sentinel green
- Serialization fixture green

**LOC:** ~300 (parallel with Phase 4a)
**Release:** v9.93

### Phase 4b — State promotion (no migration scope)

**Purpose:** Move `launchState` from side-channel (`.zcp/state/launch-production/<id>.json`) into `WorkflowState.Launch`. **No migration of legacy state files** — current launch-production flow has never functioned end-to-end, so any on-disk legacy state is stale debug residue, not load-bearing.

**Files:**
- `internal/tools/launch_state.go`: **DELETE** side-channel persistence (`readLaunchState`, `writeLaunchState`, `findActiveLaunchState`); reads/writes go through `WorkflowState.Launch`
- `internal/workflow/session.go`: load + save `WorkflowState.Launch` as part of session lifecycle
- `internal/workflow/launch_state.go` (NEW): `LaunchState` struct moved from `internal/tools/`; carries `ProjectMode`, `ExistingProjectID`, `PipelineConfigurations`, `PipelineCheckedAt`, `CICDMethod`, and all existing fields
- `internal/tools/workflow.go`: update `findActiveLaunchState` callers to use `WorkflowState.Launch`
- `internal/snapshot/source.go`: ensure `SourceSnapshot` is here (moved in same PR — no interstitial)
- Boot behavior: if `.zcp/state/launch-production/` directory exists at boot, **silently rename it to `.zcp/state/launch-production.legacy-<timestamp>/`** (one-shot, no migration logic; operator can `rm -rf` later). No env-var hatch, no dry-run, no inverse script.

**Unit tests:**
- `TestLaunchStateRoundTrip_InWorkflowState`
- `TestFindActiveLaunch_UsesWorkflowState`
- `TestLegacyLaunchDir_RenamedAtBoot`

**Gate G3 evidence:** state-promotion tests green; running ZCP fresh against eval-zcp produces a valid `WorkflowState.Launch` end-to-end (verified by Phase 2 e2e existing-project test which exercises the full path).

**LOC:** ~250 (down from ~500 — migration scope removed)
**Release:** v9.93 (merged with Phase 4a + 5; no longer needs operator-aware standalone release)

### Phase 6a — Atom corpus PR

**Purpose:** Atom-only changes for production redesign. PR ships first, before any handler wiring referencing new atoms.

**Files (new atoms):**
- `internal/content/atoms/launch-generate-prod-token.md` — dashboard deep-link, scope reminder
- `internal/content/atoms/launch-cicd-actions-handoff.md` — workflow YAML emit + stdin gh secret set
- `internal/content/atoms/launch-cicd-method-prompt.md` — structured method prompt (Actions/Webhook/None) for terminal phase (§4.1)
- `internal/content/atoms/setup-gh-required.md` — precheck atom for `gh` CLI availability (both container and local)
- `internal/content/atoms/setup-git-push-create-repo.md` — hint atom with `https://github.com/new` link (N1)
- `internal/content/atoms/develop-first-deploy-git-push-recommendation.md` — soft recommendation atom emitted post-first-deploy (§2.1), no chain, LLM relays to user

**Files (updated atoms — kept alive, NO deprecation):**
- `internal/content/atoms/launch-pipeline-configure-dashboard.md` — context update: now used for webhook-alternative cicd path (§4.6), still active
- `internal/content/atoms/launch-pipeline-configured.md` — kept (webhook success state)
- `internal/content/atoms/launch-pipeline-skipped.md` — kept (None-choice state)
- `internal/content/atoms/launch-pipeline-configuring.md` — kept (in-flight state)
- `internal/content/atoms/launch-delete-key.md` — prerequisite `gh secret list -R <owner>/<repo>` confirms `ZEROPS_TOKEN_PROD` present (NEW project + Actions path); for Webhook path, prerequisite is dashboard wiring confirmed; for None, immediate
- `internal/content/atoms/launch-post-checklist.md` — tag push flow + webhook-alternative branch
- `internal/content/atoms/launch-intro.md` — project mode choice context
- `internal/content/atoms/setup-git-push-container.md` — scope thinking-ahead reminder for stage cicd (recommended scopes)
- `internal/content/atoms/setup-git-push-local.md` — same
- `internal/content/atoms/setup-build-integration-actions.md` — scoped secret names (`ZEROPS_TOKEN_STAGE`, `ZEROPS_TOKEN_PROD`)
- `internal/content/atoms/setup-build-integration-webhook.md` — context update: now used for stage webhook alternative (§3.6)

No atoms deprecated. The webhook flow stays alive as a first-class alternative (§3.6, §4.6).

**Pin tests:**
- `TestAtomLintAcceptedActionsMatchDispatcher`
- `TestAtomReferenceFieldIntegrity` (new atoms must reference valid fields)
- `TestSetupAtomsLintGitPushStateAxes_OnlyReachableValues`

**Gate:** Atoms parse + lint pass.

**LOC:** ~600 (markdown only)
**Release:** v9.95

### Phase 6b — Handler wiring (production redesign + stage inline)

**Purpose:** Handler-level changes for production redesign. References atoms shipped in Phase 6a. **No Path B drop** — webhook stays alive as first-class alternative (§3.6, §4.6).

**Files:**
- `internal/tools/workflow_launch_production.go`:
  - Add Q1 method-prompt (`awaiting-project-mode-choice` status) before scope-prompt; emit-and-wait pattern (§6.2)
  - Add existing-project mutation path (token validation via `client.ListProjects` + `CreateProjectEnv` per env + `client.ImportServices` services-only yaml)
  - Replace `launchLaunchedResponse` (L854–L889) with `launchTerminalCompose`:
    - Emits cicd method prompt (Actions/Webhook/None) for user choice
    - On `CICDMethod=Actions` → `ComposeActionsHandoff(Prod, ..., SetupName="stage")`
    - On `CICDMethod=Webhook` → `launch-pipeline-configure-dashboard` atom + Path B verify via existing `executeLaunchPipelineCheck` (kept alive)
    - On `CICDMethod=None` → `launch-pipeline-skipped` atom (no cicd; user owns deploys)
  - `executeLaunchPipelineCheck`, `pickPipelineAtomID`, `pipelineBlockers`, `pendingPipelineConfigurations`, `pipelineSkipRecorded` — **KEPT** (webhook path uses them)
- `internal/tools/launch_pipeline.go`: **KEPT** — webhook flow alive
- `internal/tools/workflow_git_push_setup.go`:
  - After confirm-stamp, co-emit method prompt (Actions/Webhook/None) for stage cicd choice (§2.5, §3)
  - On user choice, dispatch to `ComposeActionsHandoff(Stage)` OR webhook atom OR none-stamp
- `internal/tools/workflow.go`: add `LaunchProjectMode`, `ExistingProjectID`, `ExistingProdToken`, `CICDMethod`, `StageCICDMethod`, `ProdSetupNameOverride` input fields to `WorkflowInput`
- `internal/topology/types.go`: add `CICDMethod` enum (`Actions` | `Webhook` | `None`); add `LaunchProjectMode` enum; remove orphan enum values `GitPushBroken`, `GitPushUnknown`
- `internal/workflow/service_meta.go`: update axis-validation pin to reflect orphan enum removal
- `internal/content/atoms/setup-git-push-*.md`: update frontmatter `gitPushStates:` to drop unreachable values
- `internal/platform/project_admin_imports_test.go`: keep `launch_pipeline.go` in allowed-files map (still imports `ProjectAdminClient`)

**Unit tests:**
- `TestLaunchProjectModePrompt_BeforeScope_EmitAndWait`
- `TestLaunchExistingProjectPath_TokenScopeValidates`
- `TestLaunchExistingProjectPath_HostnameConflictRefuse`
- `TestLaunchTerminal_CICDMethodPrompt_OptionsActionsWebhookNone`
- `TestLaunchTerminal_ActionsPath_IncludesGenerateProdTokenBeforeDeleteKey` (P-LP-10)
- `TestLaunchTerminal_WebhookPath_KeepsPathBCheck`
- `TestLaunchTerminal_NonePath_LaunchPipelineSkipped`
- `TestLaunchBundle_NoBuildFromGitField` (P-LP-11)
- `TestLaunchBundle_StartWithoutCodeTrue` (P-LP-11)
- `TestExistingProjectImport_ServicesOnlyNoProjectBlock` (P-LP-13)
- `TestGitPushSetupConfirm_EmitsCICDMethodPrompt`
- `TestGitPushSetupConfirm_StageCICDActions_TriggersComposer`
- `TestGitPushSetupConfirm_StageCICDWebhook_EmitsWebhookAtom`

**E2E scenarios (gate G4 evidence — 4 e2e tests, all real-platform):**
- `e2e/launch_new_actions_e2e_test.go` — NEW project, Actions cicd path; provisions new prod project in eval-zcp parent org; runs full launch flow; asserts service-stack created with `startWithoutCode:true` + no buildFromGit; asserts workflow YAML content matches composer expectation
- `e2e/launch_new_webhook_e2e_test.go` — NEW project, Webhook cicd path; runs launch flow; asserts terminal phase emits `launch-pipeline-configure-dashboard` deep-link + Path B verify works
- `e2e/launch_existing_actions_e2e_test.go` — EXISTING project, Actions cicd path; reuses Phase 2 e2e test infra; asserts split mutation (CreateProjectEnv + ImportServices services-only)
- `e2e/git_push_setup_stage_actions_e2e_test.go` — full stage cicd inline flow; asserts workflow file written; asserts secret command emitted with `ZEROPS_TOKEN_STAGE` distinct name

**Gate G4 evidence:** All 4 e2e scenarios green on eval-zcp; container + local parity matrix green; P-LP-10/11/12/13/14 unit pins green.

**LOC:** ~900 (Q1 prompt + existing-project path + cicd method dispatch + stage inline + atom integration)
**Release:** v9.95

### Phase 7 — Behavioral eval matrix + bootstrap/develop regression

**Purpose:** Final end-to-end UX validation through agent-driven behavioral eval flows. Different from Phase 6 e2e (which is programmatic) — Phase 7 runs the actual agent loop end-to-end to observe friction.

**Deliverables:**
- Run behavioral scenarios committed in Phase 0:
  - `launch-production-new-project-push-mode.md` (Q1=New, CICDMethod=Actions)
  - `launch-production-existing-project-token.md` (Q1=Existing, single-project token gate)
  - `launch-production-existing-with-webhook.md` (Q1=Existing, CICDMethod=Webhook)
  - `git-push-setup-with-cicd-method-prompt.md` (post-first-deploy soft recommendation → user opts in → method prompt → Actions)
- Run container + local parity matrix via `flow-eval-local`
- Run all existing eval scenarios (8 today) to confirm no regression:
  - `delivery-git-push-actions-setup.md`
  - `export-buildfromgit-self-snapshot.md`
  - `launch-production-from-standard-pair.md` (legacy — explicit `LaunchProjectMode=New` annotation added in Phase 7 prep)
  - `launch-production-pipeline-configured.md`
  - `launch-production-pipeline-not-configured.md`
  - `launch-production-pipeline-skip.md`
  - `launch-production-dev-only.md`
  - `launch-production-laravel-showcase.md`
- Karel inspects retrospectives + manual smoke if anything looks off

**Gate G5 evidence:**
- All 4 NEW behavioral scenarios green (retrospectives free of "lost" / "wrong-route" / "couldn't proceed" patterns)
- All 8 EXISTING scenarios green (no regression)
- Container + local parity matrix green
- Karel explicit GO for v9.95 release

**LOC:** ~50 (scenario annotation updates only — fixtures + content already committed in Phase 0)
**Release:** v9.95 (final phase)

---

## 12. Release sequence

| Release | Phases | What ships | Operator concern |
|---|---|---|---|
| **v9.91** | 0 + 0.5 | Goldens (regression + target dirs) + P-LP-3 latent-bug fix + Phase 0 e2e baseline (live SDK field verification) | None (additive) |
| **v9.92** | 1a + 1b + 1c + 2 | Composer unification + envclass (3-rule SDK-driven) + existing-project import path + Phase 2 e2e (existing-project flow) | EnvVar shape split (`ProjectEnvVar` + `ServiceEnvVar`); Layer 1/1b/3 are new packages |
| **v9.93** | 4a + 5 + 4b | Schema versioning + `LaunchProjectMode` + DTO split + AST sentinel + `CredentialField` interface + launchState promoted into `WorkflowState.Launch` | `WorkflowState.Version` bump; legacy `.zcp/state/launch-production/` dir auto-renamed to `.legacy-<timestamp>/` at boot (no migration — current flow never functioned end-to-end so no live state to preserve) |
| **v9.95** | 6a + 6b + 7 | Production redesign atoms + handler wiring (Q1 + existing-project + cicd method dispatch + stage inline) + Path B kept alive as webhook alternative + behavioral eval matrix | User-visible: Q1 project mode prompt at launch start; cicd method prompt (Actions/Webhook/None) at both stage-setup and prod-terminal moments; post-first-deploy soft recommendation |

All releases are independently revertable (legacy dir rename is reversible: `mv .zcp/state/launch-production.legacy-<ts> .zcp/state/launch-production`).

---

## 13. Verification gates

Five gates between phase groups. Advancement requires evidence committed in the repo.

| Gate | Between | Evidence (e2e + unit + behavioral) |
|---|---|---|
| **G1** | Phase 0.5 → 1a | **E2E**: `e2e/launch_baseline_test.go` green against eval-zcp (live SDK field decoding verified). **Unit**: regression goldens captured (`testdata/goldens/regression/*`); F19/F20/F21 reproducers committed (initially fail per design); drift-injection scenario refuses after P-LP-3 fix at TWO sites; `make test-pin` green. **Behavioral**: 4 new scenarios appear in `flow-eval.sh list` |
| **G2** | Phase 2 → 4a | **E2E**: `e2e/launch_existing_project_e2e_test.go` green (existing-project mutation + token scope gate + hostname conflict refuse). **Unit**: regression goldens stay green; F19/F20/F21 reproducers now PASS (composer fixed); classifier table-tests green; `make lint-local` green |
| **G3** | Phase 4b → 6a | **E2E**: state promotion round-trip green; Phase 2 existing-project e2e still green (uses promoted state). **Unit**: AST sentinel + serialization fixture green; legacy-dir-rename test green; depguard rule for `internal/snapshot/` lands |
| **G4** | Phase 6b → 7 | **E2E**: all 4 Phase 6 e2e scenarios green (`launch_new_actions`, `launch_new_webhook`, `launch_existing_actions`, `git_push_setup_stage_actions`) against eval-zcp (container + local). **Unit**: P-LP-10/11/12/13/14 pins green; `setup-build-integration-*.md` atom-lint green |
| **G5** | Phase 7 → release v9.95 | **Behavioral**: all 4 new scenarios green; all 8 existing scenarios green (no regression); container + local parity matrix green. **Unit**: full `go test -race ./... -count=1` + full `golangci-lint`. **Operator**: Karel reads retrospectives + explicit GO |

---

## 14. Legacy state handling (Phase 4b)

The current launch-production flow has not functioned end-to-end (no successful launches in production use), so on-disk `.zcp/state/launch-production/<launchID>.json` files are stale debug residue rather than load-bearing state.

### 14.1 Algorithm

```
On engine boot:
  if .zcp/state/launch-production/ directory exists:
    rename to .zcp/state/launch-production.legacy-<RFC3339-timestamp>/
  (no parsing, no field-by-field migration, no .archive subdir)

Operator action (any time):
  rm -rf .zcp/state/launch-production.legacy-*   # cleanup when ready
```

### 14.2 Sharp edges

| Edge | Handling |
|---|---|
| Directory rename retry | Idempotent — second boot finds renamed dir gone, no-op |
| Permissions denied on rename | Log + boot continues; operator handles manually |
| Concurrent processes racing rename | Filesystem-level atomicity; loser sees ENOENT, no-op |

### 14.3 Reversibility

Trivial: `mv .zcp/state/launch-production.legacy-<ts> .zcp/state/launch-production` restores prior state if operator wants to forensically inspect with older ZCP build.

---

## 15. Pin test inventory

Every layer carries pins for the invariants it owns.

| Layer | Pins (sample) |
|---|---|
| `topology` | `TestPushSourceCheck_*`, `TestLaunchProjectMode_New_Existing_Aliases`, `TestLaunchProductionStatus_IncludesAwaitingProjectModeChoice` |
| `platform` | `TestProjectAdminClientRestrictedImport`, `TestExistingProjectToken_RefusesMultiProject`, `TestExistingProjectToken_RefusesScopeMismatch` |
| `snapshot` | `TestSourceSnapshot_DigestStability`, `TestSourceSnapshot_DriftDetected` |
| `envclass` | `TestClassifyProjectEnv_SystemDrops`, `TestClassifyServiceEnv_NoEditableField`, `TestClassifyProjectEnv_UserSensitiveBiasAutoSecret` |
| `inventory` | `TestProjectEnvVar_CarriesEditable`, `TestServiceEnvVar_NoEditableField` |
| `ops/bundle` | `TestBundleCompose_LaunchNew_NoBuildFromGit`, `TestBundleCompose_LaunchNew_StartWithoutCodeTrue`, `TestBundleCompose_LaunchExisting_NoProjectBlock`, `TestBundleCompose_ExportDev_StripsEnvs`, `TestBundleCompose_ExportStage_StagePair` |
| `ops/cicd` | `TestComposeActionsHandoff_Stage_RawZcli`, `TestComposeActionsHandoff_Stage_DistinctSecret`, `TestComposeActionsHandoff_Prod_TagTrigger`, `TestDetectProvider_GitHubDotCom`, `TestDetectProvider_GitLabFallback` |
| `workflow` | `TestLaunchStateRoundTrip_InWorkflowState`, `TestFindActiveLaunch_UsesWorkflowState`, `TestLegacyLaunchDir_RenamedAtBoot` |
| `tools` | `TestLaunchProjectModePrompt_BeforeScope_EmitAndWait`, `TestLaunchExistingProjectPath_HostnameConflictRefuse`, `TestComposeLaunchTerminal_ActionsPath_GenerateBeforeRevoke`, `TestLaunchTerminal_WebhookPath_KeepsPathBCheck`, `TestGitPushSetupConfirm_EmitsCICDMethodPrompt`, `TestCredentialField_NoLeakageInResponse`, `TestStructuredPrompts_PromptFieldAlwaysPresent` |
| `integration` | `TestLaunchEndToEnd_NewProject_ProdPushMode`, `TestLaunchEndToEnd_ExistingProject_ServicesOnlyImport` |
| `e2e` | `TestPodLaunchPushModeAgainstEvalZcp` (real platform, gated by `-tags e2e` + `ZCP_API_KEY`) |

---

## 16. Local + Container parity

Every flow must work identically on local Mac and remote container. Parity is enforced by env-axis-aware atoms + per-environment composer expressions.

| Concern | Container | Local |
|---|---|---|
| `ZCP_API_KEY` source | Zerops auto-inject env | `.mcp.json` env block |
| Token reuse for stage cicd | `$ZCP_API_KEY` shell-expand | `jq -r '.mcpServers.zcp.env.ZCP_API_KEY' .mcp.json` |
| Prod token paste | stdin form same as local | stdin form same as container |
| Git push | SSH-exec inside `/var/www` | local exec in `workingDir` |
| Inventory + envs | platform API + cache | same |
| Envclass | env-agnostic pure functions | same |
| State files | `.zcp/state/...` on SSHFS | `.zcp/state/...` on local disk |
| Workflow file write | SSHFS at `/var/www/.github/workflows/` | local `workingDir/.github/workflows/` |
| `gh` CLI | pre-installed in `zcp@1` image | user-installed; `setup-gh-required.md` atom guides |
| `gh auth` | container shell `gh auth login` | local shell `gh auth login` |

Pin tests (Phase 7):
- `TestLaunchCICDActionsHandoff_ContainerExpansion`
- `TestLaunchCICDActionsHandoff_LocalExpansion`
- `TestStageCICDHandoff_ContainerExpansion`
- `TestStageCICDHandoff_LocalExpansion`
- `TestGhCLIAvailable_PreflightAtom`
- `TestExistingProjectFlow_BothEnvironments`

---

## 17. Bootstrap + Develop compatibility (hard guarantee)

These surfaces are preserved with no behavior change:

| Surface | Status after redesign |
|---|---|
| `action=start workflow=bootstrap` | unchanged |
| `action=start workflow=develop` | unchanged |
| `action=complete/skip/route/close/dispatch-brief-atom` | unchanged |
| `action=close-mode` | unchanged |
| `action=git-push-setup` | walkthrough + confirm unchanged; **Phase 6b adds** co-emitted structured cicd method prompt in confirm response (§2.5) |
| `action=build-integration` | retained for opt-in cicd (Actions OR Webhook); delegates to shared `ComposeActionsHandoff` composer (Actions) or webhook atom (Webhook) |
| `action=record-deploy` | unchanged; **Phase 6a adds** post-first-deploy soft recommendation atom emit when service stamps `FirstDeployedAt` and `RemoteURL` is empty |
| `action=adopt-local` | unchanged |
| `action=status` | envelope/plan/guidance pipeline unchanged |
| `ServiceMeta` JSON shape | additive: `BuildIntegrationWebhook` enum preserved (first-class alternative, GitLab + non-Actions users); `GitPushBroken`/`GitPushUnknown` orphan enums removed |
| Atom corpus `bootstrap-*`/`develop-*` | unchanged (`setup-git-push-*.md` frontmatter axis cleanup in Phase 6b); `develop-first-deploy-git-push-recommendation.md` new in 6a |

Regression in any preserved surface = phase rollback.

---

## 18. Rollback shape per release

| Release | Reversibility | Recovery |
|---|---|---|
| v9.91 | Trivial (pure tests + P-LP-3 fix) | `git revert` |
| v9.92 | Structural (composer + envclass + existing-project path) | `git revert`; envclass is additive (drops fallback to static table); composer is pure function (no state) |
| v9.93 | Additive (schema bump + interface + sentinel) | `git revert`; legacy state still readable (Version "1" still parses if Version "2" reverted) |
| v9.95 | Behavior shift: Q1 project mode prompt + cicd method prompt at stage-setup and prod-terminal + post-first-deploy soft recommendation; webhook stays alive; launchState promoted into WorkflowState.Launch | `git revert` restores prior emit shape; legacy launch-production dir auto-renamed (reversible by rename-back); eval matrix at G5 catches regressions before release |

Pre-release verification protocol (G5):
1. Full `make test` green
2. Full `flow-eval all` + `flow-eval-local all` retrospectives reviewed by Karel
3. Manual smoke against eval-zcp (eval-zcp credentials in `.mcp.json`)
4. Grep no Path B production references (`IntegrationStatus`, `PipelineConfigurations`, `pickPipelineAtomID`, `executeLaunchPipelineCheck`) outside test files
5. Karel explicit GO

---

# PART III — REFERENCE

## 19. Code anchors

Primary files this redesign touches, with current line counts and trajectory:

| File | Current LOC | Trajectory |
|---|---|---|
| `internal/tools/workflow.go` | 1015 | Router unchanged; add `ExistingProdToken` + `ExistingProjectID` + `AutoDeployStage` input fields |
| `internal/tools/workflow_launch_production.go` | 930 | Rewrite terminal phase (composer); add existing-project path; remove Path B references |
| `internal/tools/launch_state.go` | 329 | Move to `internal/workflow/launch_state.go`; delete side-channel persistence |
| `internal/tools/launch_pipeline.go` | 245 | **KEPT** — Path B verify for webhook cicd path (§4.6) |
| `internal/tools/launch_pipeline_test.go` | (test) | **DELETE** |
| `internal/tools/workflow_build_integration.go` | 386 | Delegate to shared `cicd.ComposeActionsHandoff` |
| `internal/tools/workflow_git_push_setup.go` | 169 | Add Q2 co-emit (Phase 6c) |
| `internal/tools/workflow_close_mode.go` | 204 | Unchanged |
| `internal/tools/launch_platform_envs.go` | 149 | **DELETE** static table; delegate to `envclass.Classify*` |
| `internal/tools/deploy_git_push.go` | 398 | Unchanged |
| `internal/tools/deploy_local_git.go` | (existing) | Unchanged |
| `internal/workflow/service_meta.go` | 526 | Update axis-validation pin for orphan enum removal |
| `internal/workflow/state.go` | (existing) | Bump Version + add Launch field |
| `internal/platform/zerops_env.go` | 150 | Propagate Type/Sensitive/Editable |
| `internal/platform/types.go` | (existing) | Replace `EnvVar` with `ProjectEnvVar` + `ServiceEnvVar` |
| `internal/platform/project_admin.go` | (existing) | Unchanged (launchKey-gated client) |
| `internal/platform/integration.go` | 66 | Kept; consumers removed (`IntegrationStatus` orphaned for future cleanup) |
| `internal/topology/types.go` | (existing) | Add `LaunchProjectMode`, add `awaiting-project-mode-choice` state; remove `GitPushBroken`/`Unknown` orphan enum values |
| `internal/ops/bundle/` | (NEW) | ~600 LOC across compose.go + rules.go + variant files |
| `internal/ops/cicd/` | (NEW) | ~400 LOC across actions_handoff.go + provider.go + workflow_yaml.go |
| `internal/ops/inventory/envs.go` | (NEW) | ~150 LOC scope-specific env fetch |
| `internal/envclass/` | (NEW) | ~200 LOC classifier + decision |
| `internal/snapshot/source.go` | (NEW) | ~80 LOC SourceSnapshot type |
| `internal/auth/credentials.go` | (NEW) | ~50 LOC credential types + CredentialField interface |

**Total LOC delta (estimate):** ~2400 across 25 files.

## 20. SDK references (zerops-go v1.0.18)

Path: `~/go/pkg/mod/github.com/zeropsio/zerops-go@v1.0.18/`

| File | Purpose |
|---|---|
| `types/enum/envTypeEnum.go` | USER\|SYSTEM env type enum (default USER) |
| `types/enum/userDataTypeEnum.go` | Service env Type values |
| `dto/output/projectEnv.go` | ProjectEnv DTO: `Type` + `Sensitive` + `Editable` |
| `dto/output/serviceStackEnv.go` | ServiceStackEnv DTO: `Type` + `Sensitive` (no `Editable`) |
| `sdk/GetProjectExport.go` | Returns `output.ProjectExport{Yaml types.Text}` |
| `sdk/GetServiceStackEnv.go` | Service env list endpoint |
| `sdk/PostServiceStackZeropsYamlValidation.go` | Per-runtime `zerops.yaml` validation |
| `sdk/PostClientProjectImport.go` | NEW project mutation (launchKey-gated) |
| `sdk/PostProjectServiceStackImport.go` | Services-only import into existing project |
| `sdk/PostProjectEnv.go` | Project env mutation |
| `sdk/PutServiceStackTriggerPipeline.go` | Direct deploy without git pull (NOT in this redesign's scope) |
| `dto/input/body/putStandardServiceStackTriggerExternalRepositoryIntegration.go` | NOT used (P-LP-7 stays) |

## 21. Zerops docs (source of truth for platform semantics)

Path: `../zerops-docs/apps/docs/content/`

| Doc | Topic |
|---|---|
| `zcp/security/tokens-and-project-access.mdx` | Single-project token gate |
| `zcp/security/trust-model.mdx` | Trust boundary semantics |
| `zcp/security/production-policy.mdx` | Prod project = no zcp@1 service |
| `zcp/workflows/promote-to-production.mdx` | Launch flow overview |
| `zcp/workflows/package-running-service.mdx` | Export flow overview |
| `features/env-variables.mdx` | USER\|SYSTEM env taxonomy |
| `references/import.mdx` | Import yaml shape |
| `references/github-integration.mdx` | GitHub OAuth + Actions patterns |
| `references/gitlab-integration.mdx` | GitLab OAuth patterns |
| `references/cli.mdx` | `zcli` reference |

Live schemas (authoritative for yaml validation):
- `https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json`
- `https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json`

## 22. ZCP internal specs

| Spec | Path | Coverage |
|---|---|---|
| Workflows | `docs/spec-workflows.md` | Workflow steps, invariants, envelope/plan/atom pipeline |
| Work Session | `docs/spec-work-session.md` | Per-PID Work Session, compaction survival, auto-close |
| Knowledge Distribution | `docs/spec-knowledge-distribution.md` | Atom corpus authoring contract |
| Scenarios | `docs/spec-scenarios.md` | Per-phase walkthroughs, pinned by `internal/workflow/scenarios_test.go` |
| Local Dev | `docs/spec-local-dev.md` | Local-machine vs container differences |
| Content Surfaces | `docs/spec-content-surfaces.md` | Recipe content-quality contract |
| Architecture | `docs/spec-architecture.md` | Per-package mapping + examples |

Error codes catalog: `internal/platform/errors.go`.

---

## End

This document is self-contained. Phase 0 begins with golden capture against current behavior. After Karel's GO, implementation proceeds bottom-up per §10.2; advancement is gated by G1–G5 per §13; releases ship per §12; rollback follows §18.
