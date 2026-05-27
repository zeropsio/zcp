# Setup-name as ServiceMeta-local canonical identity

**Date:** 2026-05-27
**Status:** PROPOSAL — supersedes archived `plans/launch-production-friction-fixes-2026-05-21.md` (kept in branch `archive/setup-name-impl-2026-05-27`)
**Trigger:** Re-design after prior implementation hit fatal architectural finding (platform UserData = container env vars → can't host ZCP-internal metadata)

---

## TL;DR

ServiceMeta on disk (`.zcp/state/services/<host>.json`) is the **only** canonical store for setup-name — same as every other `ServiceMeta` field (`CloseDeployMode`, `GitPushState`, `BuildIntegration`, `RemoteURL`, `BootstrappedAt`, `FirstDeployedAt`). **No platform write of any kind.** Each entry gate (recipe / classic / adopt) writes at a deterministic moment in the flow; lifecycle reads pull from meta. When meta lacks the value (orphan / pre-feature), one bounded local discovery runs once + writes back; if discovery can't determine, the user supplies via a new explicit MCP action.

Discarded from previous attempt:
- Platform UserData as canonical store (env-var pollution + auto-restart side effect on writes)
- Three-layer "platform canonical, local cache" mental model
- `zcp.canonicalSetup` UserData keys (write path entirely removed)

Kept from previous attempt's design thinking:
- 4 entry gates concept (R recipe, B classic, A adopt + M manual-import treated as orthogonal)
- `ResolveCanonicalSetup` central cascade for empty-meta discovery
- `ServiceSnapshot` denormalization so `DeployIntent.Resolve` stays pure
- Convention-fallback deletion across deploy/launch sites (no silent "prod" defaults)
- F7 / F2 / F8 / F1 atom edits (orthogonal frictions, batchable at end)

---

## Architectural decision: WHY local-file canonical

| Concern | Local meta-file | Platform UserData (rejected) |
|---|---|---|
| Consistency with rest of ServiceMeta | ✅ same as 7 other fields | ❌ only field with split storage |
| Env-var pollution in user containers | ✅ none | ❌ entry surfaces as container env var (verified live 2026-05-26: `printenv zcp` showed `zcpCanonicalSetup=test`) |
| Auto-restart side effect on writes | ✅ none | ❌ default behavior; skipRestart=true override possible but adds deploy-time surprise |
| Offline operation (no network) | ✅ works | ❌ bootstrap blocked on write failure |
| Single-device case (Karel's default) | ✅ optimal (no platform round-trip) | ⚠️ unnecessary platform call |
| Multi-device case | ⚠️ each device runs cascade independently first time (1-3 platform reads + 1 yaml parse, sub-second) | ✅ instant via UserData read |
| Implementation complexity | ✅ low (matches existing meta field pattern) | ❌ high (live verification proved public SDK can't write non-env-polluting UserData) |

**Multi-device penalty is acceptable.** Cascade reads (`GetServiceStackIntegrationStatus` + optional `GetAppVersionAppCode` archive fetch + workingDir yaml) take 1-3 platform calls + yaml parse, all under 1 second. Cross-device collisions handled by "second writer wins" (last `set-default-setup` call updates local; other device runs cascade on next mutation).

---

## The 4 entry gates (where canonical is written)

Each gate writes to **`ServiceMeta.PrimarySetupName` + `ServiceMeta.StageSetupName` only**. No platform writes anywhere. Empty fields are a legitimate "not yet known" state — `ResolveCanonicalSetup` discovers + writes back on first setup-sensitive read.

### Gate R — Recipe bootstrap

**Trigger:** `writeBootstrapOutputs` runs at bootstrap completion when `state.Bootstrap.Route == BootstrapRouteRecipe` and target is NOT existing (`!target.Runtime.IsExisting`).

**Value source:** `recipeSetupNamesForTarget(mode, metaHostname, stageHostname)` — pure function of mode shape (Standard → "dev"/"prod", Simple → "prod", LocalStage → "prod"/empty, LocalOnly → empty/empty).

**Belt-and-suspenders:** parse the emitted import yaml (already in hand at write time) + assert `recipeSetupName(target, isDev)` per service-stack equals what the emitter wrote. Catches future drift between `recipe_templates_import.go` and `recipeSetupNamesForTarget`.

**Items to verify (per Plan Fidelity rule, P1 acceptance):**
- [ ] `writeBootstrapOutputs` route check → only recipe + !IsExisting writes
- [ ] `writeProvisionMetas` same gating
- [ ] `mergeExistingMeta` preserves non-empty + migrate-forward empty
- [ ] Belt-and-suspenders verify implemented + tested
- [ ] All standard/simple/dev/local-stage/local-only mode shapes covered in test table
- [ ] Recipe-route flow-eval (`launch-production-from-standard-pair`, `launch-production-laravel-showcase`) confirms expected setup names in meta after bootstrap

### Gate B — Classic bootstrap

**Trigger:** `writeBootstrapOutputs` when route is `BootstrapRouteClassic`.

**Value source:** none at bootstrap time — user authors zerops.yaml later. Meta written with empty `PrimarySetupName`/`StageSetupName`.

**Discovery on first deploy:** `ResolveCanonicalSetup` runs cascade against live yaml (workingDir or SSH-readable container yaml), single-block auto-pick OR role/hostname disambiguation, writes back to meta.

**Items to verify:**
- [ ] Classic-route writeBootstrapOutputs leaves setup-name empty (no convention assumption)
- [ ] First deploy after classic bootstrap discovers via cascade step 6 (local yaml parse)
- [ ] Cascade write-back persists to meta so subsequent deploys hit cache
- [ ] When yaml has multiple non-matching setups: returns structured `requiresSetupInput` blocker (see §requiresSetupInput shape)
- [ ] Flow-eval covering classic bootstrap (TBD scenario or `classic-go-simple` adaptation)

### Gate A — Adoption

**Trigger:** `LocalAutoAdopt` (server-start single-runtime auto-link) or `handleAdoptLocal` (explicit `zerops_workflow action="adopt-local"`).

**Value source — 5-step cascade in order, first hit wins:**
1. ServiceStack.GithubIntegration.ZeropsYamlSetup (read-only platform call — service has GH integration configured)
2. ServiceStack.ActiveAppVersion.GithubIntegration.ZeropsYamlSetup (latest deploy via GH integration)
3. `GetAppVersionAppCode` → fetch archive → extract `zerops.yaml` → parse + disambiguate (orphan service with at least one prior deploy)
4. workingDir yaml (local env) OR SSH-readable `/var/www/<source>/zerops.yaml` (container env) → parse + disambiguate
5. structured `requiresSetupInput` blocker → user passes explicit setup via `set-default-setup` action

**On hit (steps 1-4):** write `ServiceMeta.StageSetupName` (pair shapes) or `PrimarySetupName` (singletons).

**Items to verify:**
- [ ] LocalAutoAdopt case 1 (single runtime auto-link) runs cascade + populates meta
- [ ] handleAdoptLocal runs cascade + populates meta
- [ ] On cascade miss: emits `requiresSetupInput` structured response (NOT silent empty meta)
- [ ] Container-side adoption (no agent at init time): writes meta with empty setup, first agent interaction surfaces blocker on first setup-sensitive call
- [ ] Cascade step 3 (archive fetch) implemented + tested with mocked URL
- [ ] flow-eval `adopt-existing-standard-pair` confirms setup name appears in meta post-adopt

### Gate M — Manual import (`zerops_import`)

**Trigger:** none — `zerops_import` is **orthogonal to setup-name**.

**Reasoning:** `zerops_import` creates services on the platform. ZCP doesn't track those services in `.zcp/state` (no `WriteServiceMeta` call) — they live in the import workflow that surrounds the call (recipe bootstrap, classic bootstrap, or manual zcli-side authoring). When the surrounding workflow does an adoption / classic-bootstrap / recipe-bootstrap, the appropriate gate (R/B/A) handles setup-name.

**Items to verify:**
- [ ] `internal/tools/import.go` makes no setup-name-related call (no `ExtractZeropsSetupMap`, no UserData write, no meta touch)
- [ ] No `Setup` / `StageSetup` field added to `ImportInput` schema
- [ ] Recipe-authoring use of `zerops_import` continues working unchanged

---

## ServiceMeta schema (P0)

```go
type ServiceMeta struct {
    // ... 9 existing fields ...

    // PrimarySetupName is the canonical zerops.yaml setup-block name for
    // self-deploy on Hostname's half. "" = not yet discovered; first
    // setup-sensitive call runs ResolveCanonicalSetup which writes back
    // on hit OR returns requiresSetupInput on total miss. Updated by
    // set-default-setup action OR cascade hits.
    PrimarySetupName string `json:"primarySetupName,omitempty"`

    // StageSetupName is the canonical setup-block name for cross-deploy
    // to StageHostname (pair shapes only). "" for non-pair modes OR
    // not-yet-discovered. Same lifecycle as PrimarySetupName.
    StageSetupName string `json:"stageSetupName,omitempty"`
}

// SetupNameFor returns the canonical setup-block name for a target
// hostname. Pair-keyed: targetHostname == StageHostname returns
// StageSetupName; targetHostname == Hostname returns PrimarySetupName;
// any other hostname returns "" (caller must load that hostname's meta).
//
// Empty result on in-scope hostname means "cache miss — run cascade."
func (m *ServiceMeta) SetupNameFor(targetHostname string) string
```

**ServiceSnapshot denormalization:**
```go
type ServiceSnapshot struct {
    // ... existing fields ...
    SetupName      string  // meta.PrimarySetupName projected for Hostname
    StageSetupName string  // meta.StageSetupName projected when StageHostname != ""
}
```

`ComputeEnvelope` populates both fields at snapshot-build time. `DeployIntent.Resolve` reads from snapshot — no IO, no convention fallback.

---

## ResolveCanonicalSetup cascade (P1)

`internal/workflow/setup_resolver.go` — single entry point for hot-path discovery when meta cache is empty.

```go
type ResolveCanonicalSetupInput struct {
    StateDir       string         // local meta dir; empty → skip cache + write-back
    ServiceID      string         // platform service stack ID; empty → skip steps 1-3
    TargetHostname string         // REQUIRED; selects meta half + drives disambiguation
    Mode           topology.Mode  // hint for candidate cascade
    LocalYAMLBody  string         // optional fallback body (workingDir or SSH cat)
}

// Cascade — first non-empty wins:
//   1. Local cache (ServiceMeta.SetupNameFor)
//   2. ServiceStack.GithubIntegration.ZeropsYamlSetup
//   3. ServiceStack.ActiveAppVersion.GithubIntegration.ZeropsYamlSetup
//   4. GetAppVersionAppCode → fetch archive → extract zerops.yaml → parse
//   5. LocalYAMLBody → PickSetupNameFromNames
//   6. ErrRequiresSetupInput{AvailableSetups, Reason} blocker
//
// On hit at steps 2-5: write back to local meta via WriteServiceMeta
// (no platform write). Cache hits at step 1 return without write.
func ResolveCanonicalSetup(ctx context.Context, client platform.Client, in ResolveCanonicalSetupInput) (string, error)
```

**Items to verify per cascade step:**
- [ ] Step 1: cache hit returns immediately, no platform calls
- [ ] Step 2: GH integration read tested via mock `GetServiceStackIntegrationStatus`
- [ ] Step 3: ActiveAppVersion GH integration tested (requires SDK surface — see §sdk-additions)
- [ ] Step 4: archive fetch + zip extract + yaml parse tested (requires HTTP harness)
- [ ] Step 5: workingDir yaml parse + single-block + role-match disambiguation
- [ ] Step 6: returns structured `ErrRequiresSetupInput` with AvailableSetups populated when yaml had multiple

---

## SDK surface needed (P0 — read-only additions)

| SDK endpoint | ZCP wrapper | Usage |
|---|---|---|
| `GetServiceStackExternalRepositoryIntegrationStatus` | `GetServiceStackIntegrationStatus(ctx, serviceID) IntegrationStatus` | Cascade step 2 |
| `GetServiceStack` (existing — exposes `ActiveAppVersion`) | Extend `Client.GetService` mapping to include `ActiveAppVersion.GithubIntegration.ZeropsYamlSetup` + `ActiveAppVersion.Id` | Cascade steps 3 + 4 |
| `GetAppVersionAppCode` | `GetAppVersionAppCode(ctx, appVersionID) string` returns archive download URL | Cascade step 4 |
| `PostServiceStackZeropsYamlValidation` (existing wrapper) | Already exists; pass non-empty `ZeropsYamlSetup` to verify post-set-default-setup | Stale-meta detection |

**NO write SDK wrappers added.** `PostServiceUserData` / `PutUserData` from archived impl are NOT in this plan — local meta-file is the only canonical.

---

## Lifecycle read sites (P6 — all conventions deleted)

Every setup-sensitive read site reads from snapshot (populated from meta cache) OR explicit `setup=` parameter. When both empty, the site invokes `ResolveCanonicalSetup` cascade. **All hardcoded `"prod"` / `"dev"` fallbacks are deleted.**

| Site (file:line) | Today's fallback | After |
|---|---|---|
| `deploy_preflight.go:84,136` (resolveSetupEntry chain) | explicit → role → "prod" → hostname | snapshot.SetupName OR cascade; empty → INVALID_ZEROPS_YML with availableSetups |
| `deploy_validate_api.go:60` (setupName = target.Name default) | hostname-as-setup default | drop default; require explicit setup OR resolved value |
| `deploy_intent.go:149,158,164` (hardcoded RecipeSetupDev/Prod) | hardcoded constants | snapshot.SetupName / StageSetupName; OMIT setup arg from DeployArgs when empty |
| `deploy_git_push.go:317` (hostname literal fallback) | `setupName = hostname` | cascade via ResolveCanonicalSetup before validation |
| `deploy_local_git.go:88` (same pattern) | same | same |
| `launch_pipeline.go:26` (`defaultPipelineZeropsYamlSetup = "prod"`) | constant fallback | DELETE constant; launch composer reads source meta or returns scope-prompt blocker |
| `launch_promotables.go:94` (per-promotable fallback to "prod") | constant fallback | cascade: per-promotable override → workflow override → source meta.StageSetupName → source meta.PrimarySetupName → scope-prompt blocker (no "prod" default) |
| `ops/bundle/launch.go:45` (bundle composition `"prod"` default) | constant fallback | composer fails if resolved setup empty; surface as blocker to handler |
| `workflow_export_probe.go:60` (pickSetupName heuristic) | candidate cascade with single-block auto-pick | reuse cascade logic via shared `PickSetupNameFromNames`; same disambiguation |
| `workflow_build_integration.go:443` + `actions_handoff.go:64` + `cicd/workflow_yaml.go:12` (CI YAML embed) | snapshot-based, often hostname | snapshot.SetupName at gen-time; if empty: anticipatedBuildTarget runs cascade |
| `env_plan.go:319` (generate-dotenv single-block auto-pick) | yaml-only single-block | service-scoped calls: cascade; YAML-utility-only calls: explicit setup required |

**Items to verify per site (Plan Fidelity — each is a separate checklist item):**
- [ ] Each site's fallback constant / hardcoded value DELETED (not commented out, not kept "as safety net")
- [ ] Each site reads from snapshot first, cascade on empty
- [ ] Test pinning each site: empty snapshot + no cascade source → structured blocker (no convention "prod" emerges)
- [ ] flow-eval regression coverage: every `launch-production-*` + `git-push-setup-*` + `delivery-*` scenario passes
- [ ] `defaultPipelineZeropsYamlSetup` constant grep across codebase returns zero matches post-P6

---

## requiresSetupInput structured response

When a gate or lifecycle read cannot determine setup-name + has no explicit override, the response is a structured blocker. Plan Fidelity requires this be a **named, schema-stable shape** — not an ad-hoc string error.

```json
{
  "status": "blocked",
  "reason": "requiresSetupInput",
  "service": "<hostname>",
  "targetHostname": "<hostname or stage-half>",
  "availableSetups": ["dev", "prod"],
  "ambiguityReason": "multiple setup blocks; no role-hostname match",
  "recovery": {
    "tool": "zerops_workflow",
    "action": "set-default-setup",
    "args": {
      "service": "<hostname>",
      "setup": "<choose-from-availableSetups>",
      "stageSetup": "<optional-for-pair>"
    }
  }
}
```

**Items to verify:**
- [ ] Response shape is a named Go struct (not inline `map[string]any`) so wire-format pins via test
- [ ] AvailableSetups always populated when yaml was readable (even if cascade failed)
- [ ] Recovery.action references existing tool/action (no typos that ship)
- [ ] Emitted by: adoption cascade miss, classic-bootstrap first-deploy cascade miss, set-default-setup with no live yaml + no explicit input
- [ ] Pinned by `TestRequiresSetupInputShape` covering at minimum 3 cases

---

## staleMetaSetup structured response

When meta says "dev" but live yaml has been renamed to `setup: development` (block name drift), the pre-deploy validator returns this:

```json
{
  "status": "blocked",
  "reason": "staleMetaSetup",
  "service": "<hostname>",
  "metaSetup": "dev",
  "liveYamlSetups": ["development"],
  "recovery": {
    "options": [
      {
        "label": "Restore yaml block name to match meta",
        "action": "edit zerops.yaml to use `setup: dev`"
      },
      {
        "label": "Update meta to match yaml (permanent)",
        "tool": "zerops_workflow",
        "action": "set-default-setup",
        "args": {"service": "<hostname>", "setup": "development"}
      },
      {
        "label": "One-shot deploy with override",
        "tool": "zerops_deploy",
        "args": {"targetService": "<hostname>", "setup": "development"}
      }
    ]
  }
}
```

**Items to verify:**
- [ ] Response shape is a named Go struct
- [ ] Emitted from `ValidatePreDeployContent` when platform validator returns `ZeropsYamlSetupNotFound`
- [ ] AvailableSetups list populated from validator's response OR parsed local yaml
- [ ] All 3 recovery options always present (deterministic, no conditional shapes)
- [ ] Pinned by `TestStaleMetaSetupShape`

---

## set-default-setup action (P8)

New `zerops_workflow action="set-default-setup"`. Pair-aware, validates against live yaml when available, **writes local meta only**.

```
zerops_workflow action="set-default-setup" service="<hostname>" setup="<name>" [stageSetup="<name>"]
```

**Sequence:**
1. Validate inputs (targetService required, setup OR stageSetup required)
2. Read existing meta; reject if not bootstrapped/adopted
3. Optional: validate setup names exist in live yaml via `PostServiceStackZeropsYamlValidation` (when ZCP can read live yaml). Mismatch → reject with availableSetups.
4. Update `meta.PrimarySetupName` / `meta.StageSetupName`
5. `WriteServiceMeta` (local file write)
6. Return structured response with previous + new values

**Items to verify:**
- [ ] Action dispatch added to `handleWorkflowAction`
- [ ] `Setup` + `StageSetup` input fields added to `WorkflowInput` schema
- [ ] `AcceptedWorkflowActions` list extended (atom lint pin)
- [ ] Validate-against-live-yaml branch tested (mock platform validator returning OK + ZeropsYamlSetupNotFound)
- [ ] No-yaml-available branch (skip validation, trust user input) tested
- [ ] Response shape pinned via `TestSetDefaultSetupResponseShape`
- [ ] Idempotent: same input twice = no-op + same response
- [ ] No platform UserData write anywhere (grep `PostServiceUserData` / `PutServiceUserData` in this handler returns zero)

---

## Atom + messaging edits (P9-P12)

These are orthogonal to the canonical-store design but were bundled in the archived plan. Keep as P9-P12.

| Phase | Files | Items to verify |
|---|---|---|
| **P9 F7 sourceContext reshape** | `launch_source_context.go` | Rename `SuggestedRuntime` → `PromotionHeadline`; add `TargetServiceCanonical` field; rename test references; golden file regen for any atom referencing the field |
| **P10 F2 walkthrough steps[]** | `workflow_git_push_setup.go` | Add `steps[]` field to walkthrough response with `{n, title, call}` entries per step; container vs local divergence (3 vs 2 steps); atom prose simplification |
| **P11 F1 idle-adopt-entry edit** | `internal/content/atoms/idle-adopt-entry.md` + golden | Add paragraph: "After adopt completes, runtime becomes launch-production source"; verify golden regen |
| **P12 F8 launch-intro lifecycle split** | `launch-intro.md` + golden | Two-window paragraph (launch-window key reuse OK, post-window revoke + use prod-scoped token); verify any other launch-pipeline-* atom that mentions key lifecycle for consistency |

**F4, F5, F6** — verified shipped in earlier plans per archived TL;DR + previous Plan Fidelity audit; this plan does not touch them.

---

## Phase plan (8 commits, ordered)

Each phase is a single commit. Plan Fidelity rule: at each phase ship, walk the per-phase checklist and report DONE/PARTIAL/SKIPPED per item. No "phase complete" claim without per-item verification.

| P | Title | Files touched (estimate) | Per-item count |
|---|---|---|---|
| **P0** | Foundation: schema + SDK read wrappers + ServiceSnapshot denorm | 8 files | 6 items |
| **P1** | `ResolveCanonicalSetup` cascade module | 2 new files (resolver + tests) | 6 items (one per cascade step) |
| **P2** | Gate R (recipe bootstrap writes meta cache + belt-and-suspenders) | 2 files | 6 items |
| **P3** | Gate A (adoption cascade + requiresSetupInput blocker shape) | 3 files | 6 items |
| **P4** | Gate B (classic bootstrap leaves empty; first-deploy cascade discovers) | 2 files | 5 items |
| **P5** | Lifecycle reads sweep — DELETE all conventions (each site enumerated) | ~11 files | 11 site items + grep verification |
| **P6** | `set-default-setup` action + staleMetaSetup structured blocker | 3 files | 8 items |
| **P7** | Atom edits: F7 sourceContext + F2 walkthrough steps[] + F1 + F8 | ~6 atom/golden pairs | 4 items |

**Total ~52 individually-verifiable items.** Each item gets explicit DONE/PARTIAL/SKIPPED report at phase ship.

---

## Acceptance per phase

Phase ships only when:

1. **Per-item checklist walked + reported.** Any partial / skipped item explicitly flagged + user-approved.
2. **`go test ./... -short -count=1` green** (mock platform.Client).
3. **`go test -race ./... -short` green.**
4. **`make lint-local` 0 issues** (strict golangci-lint).
5. **Phase-specific live verification** (per below):

| Phase | Live verification |
|---|---|
| P0-P1 | Tests only (cascade has no callers yet) |
| P2 | Live: `ssh zcp` after recipe bootstrap → cat `.zcp/state/services/<host>.json` → confirm `primarySetupName` populated |
| P3 | flow-eval `adopt-existing-standard-pair` → self-review confirms `stageSetupName` in meta |
| P4 | flow-eval classic-bootstrap scenario (TBD or `classic-go-simple` adaptation) |
| P5 | flow-eval `launch-production-from-standard-pair` + `launch-production-laravel-showcase` (non-canonical) + `cross-deploy-stage-promote-from-dev` |
| P6 | Live: `ssh zcp` rename setup in yaml → `zerops_deploy` returns `staleMetaSetup` blocker shape; `set-default-setup` action runs cleanly + updates meta |
| P7 | flow-eval `launch-production-from-standard-pair` confirms F7 fields in response; golden test regen passes |

After all 8 phases ship + final flow-eval batch (same 21-scenario list as archived run) shows ≥17/17 actually-executable scenarios pass: `make release-patch`.

---

## Out of scope (will NOT be touched)

- F4 (build-integration gh auth precondition) — already shipped per archived TL;DR
- F5 (skipRestart=true GIT_TOKEN walkthrough) — already shipped
- F6 (launch-post-checklist subsequent prod ops) — already shipped
- Platform UserData write of any kind — architecturally rejected (env-var pollution)
- Multi-device "instant sync" via platform write — accept cascade-on-first-read penalty
- Engine refactor to thread platform.Client — Engine stays pure; tool handlers carry platform context
- `BuildIntegration=actions` stamp-before-install (Codex F4 caveat) — backlog
- `set-default-setup` permanent rename via platform-side update — not happening; user re-edits yaml + meta updates next deploy
- ZeropsYamlSetup field on `ServiceStack` SDK type — not adding; rely on existing `GithubIntegration.ZeropsYamlSetup` already exposed

---

## Backward compatibility

`.zcp/state/services/<host>.json` is a user-facing surface (per `CLAUDE.local.md` Engineering Priority). Pre-P0 files have no `primarySetupName` / `stageSetupName` fields.

- `json.Unmarshal` with `omitempty` tolerates missing fields (zero-valued on read)
- First setup-sensitive operation against pre-P0 meta: `ResolveCanonicalSetup` cascade runs once + writes back
- No silent migration; no destructive rewrite; meta upgrades organically
- Pinned by `TestServiceMeta_PreP0Format_LoadsCleanly`

Existing flow-eval scenarios that adopted services pre-feature continue working — they hit cascade step 2+ on first deploy + populate meta naturally.

---

## What this plan explicitly does NOT do (and why)

**No `zerops_import` setup-name involvement** (Gate M dropped). Manual import is orthogonal to setup-name tracking. The surrounding workflow (recipe / classic / adopt) owns the meta write.

**No platform UserData write** (any kind). Public SDK `PostServiceUserData` surfaces entries as container env vars + triggers auto-restart. ZCP-internal metadata can't be stored there cleanly.

**No "platform canonical, local cache" mental model.** Each ServiceMeta field has exactly one canonical location: the local meta file. Consistency with `CloseDeployMode` / `GitPushState` / `BuildIntegration` etc.

**No silent fallback conventions.** When meta + cascade both fail, emit `requiresSetupInput` blocker — user supplies via `set-default-setup`. No "default to prod" anywhere.

**No structured `staleMetaSetup` blocker without 3 recovery options.** All-or-nothing schema; can't partially ship.
