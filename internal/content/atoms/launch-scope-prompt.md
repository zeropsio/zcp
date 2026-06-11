---
id: launch-scope-prompt
priority: 2
phases: [launch-production-active]
title: "Launch scope — collect production target details"
references-fields: []
---

### Launch scope — collect production target details

**This workflow is stateless multi-call narrowing.** Every response's `inputs` block is the running accumulator: pass all previously-accepted parameters forward on every next `action="start"` call. `action="complete"` is reserved for bootstrap and returns `BOOTSTRAP_NOT_ACTIVE` here.

#### First — identify the launch path

launch-production has two mutation paths in one workflow. Pick which one matches the user's intent BEFORE collecting scope params; the choice surfaces in `inputs` and dispatches the right mutation at the `ready-to-launch` step.

| User intent signal | Path | Required token params |
|---|---|---|
| "Create new prod project", "launch to fresh project", or no existing project mentioned | **NEW-PROJECT** | `launchKey` (one-shot launch-window token with project-creation permission — surfaced at the `ready-to-launch` step via the launch-mutation-key-required atom) |
| "I have existing prod project", explicit project ID/token supplied, "deploy into project X" | **EXISTING-PROJECT** | `existingProjectId` + `existingProdToken` (project-scoped token from target project's dashboard) |

If the user explicitly hands you an existing project ID OR a project-scoped token, pass `existingProjectId` + `existingProdToken` on this first `action="start"` call alongside the scope params below — both will land in the `inputs` accumulator and the workflow will skip the `launchKey` prompt at `ready-to-launch`. Otherwise default to NEW-PROJECT and let the workflow ask for `launchKey` later.

#### Then — apply suggestions from `sourceContext`

- **`productionProjectName`** — `sourceContext.suggestedTargetName` (`<source>-dev` / `<source>-stage` → `<source>-prod`, else `<source>-prod` appended). Confirm name with user; don't silently rename.
- **`targetService`** — `sourceContext.promotionHeadline` when single. For standard-mode pairs the headline is the stage hostname (validated last-known-good); `devHostname` field discloses the iteration half. Either half is accepted as input — the handler normalizes internally. When the canonical post-normalization differs, `sourceContext.targetServiceCanonical` echoes the form the bundle composer will use.
- **`promotables`** — multi-runtime promotion. Pass an array of `{hostname, prodHostname?, prodSetupNameOverride?}` entries when more than one runtime is being promoted into the same prod project (monorepo with app + worker, or separate-repos with multiple services). Empty/absent → falls back to single-runtime from `targetService`. Production hostname derivation: `appdev`/`appstage` → `app`, `workerstage` → `worker`. Pass `prodHostname` to override.
- **`region`** — optional, default `eu-central`. Custom domains are attached by the operator in the Zerops dashboard after launch (Project → Public Access → HTTP Routing) — they are not a launch input.
- **`managedDeps`** — `sourceContext.managedDeps` lists the source's managed services (databases, KV stores); all are bundled by default. Per-dep decisions travel as `managedDeps={"<hostname>":"exclude"}` — the `ready-to-launch` preview marks each dep `referenced=true/false` (whether anything wires `${<host>_*}`) and recommends exclusions, so defer the decision there unless the user already named deps to drop.
- **`keepNonHA`** — optional `[]hostname` to keep at `NON_HA` (default: all managed deps go `HA`).
- **`runtimeScaling`** — optional per-runtime container counts `{"<hostname>":{"minContainers":N,"maxContainers":M}}`. Production default is a 2-container floor per runtime; an explicit `minContainers: 1` is accepted as a consented single-container run (surfaces as a warning, not a block). Ask the user about expected load before overriding either way.
- **`skipStageRecommendation`** — when the source is a dev-only / local shape, the scope response may carry a stage recommendation block (create a validated stage first). Pass `skipStageRecommendation=true` only after the user explicitly declines it.

When `sourceContext.availableRuntimes` has multiple entries, the user must pick. Use `AskUserQuestion` if your harness exposes it (structured choice UI); else surface the choice inline and wait for the user's next turn. For multi-runtime, ask the user whether to promote all or pick a subset — defensive default is "primary runtime + infra now, other runtimes as separate additive launches" unless promotables share the same source repo (monorepo).

After scope is complete, ZCP runs the source-control gate — every promoted runtime must carry `meta.GitPushState=configured` + a live remote that matches the recorded value + a clean working tree with pushed HEAD. Unresolved blockers surface as `source-control-required` with per-runtime Recovery hints chaining `git-push-setup` / `zerops_deploy strategy=git-push` / `build-integration`. Once green, ZCP advances to `classify-prompt` for env classification.
