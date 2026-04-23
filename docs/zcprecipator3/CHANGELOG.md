# zcprecipator3 — changelog

Running log of changes on top of [plan.md](plan.md). Each entry captures what changed, why, and what run-analysis or session surfaced the gap.

---

## 2026-04-23 — v9.5.3 + follow-ups

### Context

Run 3 and run 4 dogfood (see `runs/3/RAW_CHAT.md`, `runs/4/RAW_CHAT.md`) surfaced three categorical engine defects — none was caught by fixture tests because they only materialize against a live agent/platform.

### Fixes shipped

1. **`RecipeInput.Plan/Fact/Payload` typed structs** (v9.5.1) — `json.RawMessage` fields generate MCP schemas with `type: ["null", "array"]`, rejecting JSON objects. Replaced with `*Plan`, `*FactRecord`, `map[string]any`. See `internal/recipe/handlers.go`.

2. **`zerops_knowledge` tool description owns the recipe-authoring exclusion** (v9.5.1) — schema-level "ALWAYS use this field" imperatives were out-competing markdown-level "Do NOT call" prohibitions. Rewrote the tool description to refuse recipe-authoring use at the schema layer; the research atom now cites the tool's own description for mutual reinforcement.

3. **`gateEnvImportsPresent` moved out of `DefaultGates()`** (v9.5.3) — was firing at every `complete-phase` including research, forcing the agent to emit all 6 `import.yaml` files before it knew what comments to write. Now only fires at `PhaseFinalize` close, after the writer sub-agent has populated comments. `emit-yaml` now also writes `<outputRoot>/<tier.Folder>/import.yaml` to disk so the gate can actually pass. See `internal/recipe/gates.go`, `internal/recipe/phase_entry.go`, `internal/recipe/workflow.go`.

### Gap identified but not yet fixed — provision/deliverable YAML shape + env-var lifecycle

**Background**: plan §3 stays-list says v3 reuses v2's YAML emitter and secret-forwarding rules. Plan §13 risk watch says *"v3's `yaml_emitter.go` wraps v2's yaml emitter, does not replace it."* v3 ignored both and wrote `internal/recipe/yaml_emitter.go` from scratch (296 LoC), losing v2's captured knowledge:

- **Two distinct YAML shapes**: v2 separates the *workspace import* (provision-time, agent-authored from atoms per `workflow/phases/provision/import-yaml/workspace-restrictions.md` — services-only, `startWithoutCode: true` on dev, no `project:`, no `buildFromGit`, no `zeropsSetup`, no preprocessor expressions) from the six *deliverable imports* (finalize, Go-generated via `recipe_templates_import.go::GenerateEnvImportYAML` — full `project:` + `envVariables` + `buildFromGit` + `zeropsSetup`). v2 enforces the distinction via a validator (`internal/tools/workflow_checks_finalize.go:208-215`) that refuses `startWithoutCode` in deliverables.

- **Three env-var timelines**:
  1. *Provision (live workspace)*: real secret values set via `zerops_env project=true action=set variables=["APP_KEY=<@generateRandomString(<32>)>"]` — preprocessor runs once, actual value lands on the project. Cross-service auto-inject keys cataloged via `zerops_discover includeEnvs=true`.
  2. *Scaffold (per-codebase `zerops.yaml`)*: `run.envVariables` references the discovered cross-service keys (`DB_HOST: ${db_hostname}`) — never raw values.
  3. *Finalize (6 deliverable yamls)*: `projectEnvVariables` is structured per-env input to `generate-finalize`. Envs 0-1 (dev-pair) carry `DEV_*` + `STAGE_*` URL constants; envs 2-5 (single-slot) carry `STAGE_*` only with hostnames `api`/`app` instead of `apistage`/`appstage`. Shared secrets re-emit as `<@generateRandomString>` templates so each end-user gets a fresh value. `${zeropsSubdomainHost}` stays literal — end-user's project substitutes at click-deploy.

**Why it matters**: the recipe is a template that produces a reproducible click-deploy. Conflating author-workspace state with deliverable yaml breaks security (every end-user inherits the author's APP_KEY), URL resolution (author's subdomain baked in instead of `${zeropsSubdomainHost}`), and provision itself (workspace yaml with `buildFromGit` tries to clone empty repos before scaffold has pushed them).

**What v3 has now**:
- `yaml_emitter.go` emits one shape — deliverable-shape — for all 6 tiers. No workspace shape exists.
- `Plan.ProjectEnvVars map[string]map[string]string` field exists but nothing populates it, no atom teaches it, emitter doesn't distinguish per-env shapes.
- Provision atom tells the agent to emit tier-0 yaml + `zerops_env` secrets simultaneously — conflicting state.
- Writer completion_payload has `env_import_comments` but no `project_env_vars` key.
- `stitch-content` is a stub that saves the writer blob as `.writer-payload.json` — doesn't regenerate deliverable yamls with writer-authored comments + env vars, doesn't write per-codebase READMEs or CLAUDE.md.
- No atom mentions `zerops_discover includeEnvs=true` for cross-service key discovery.
- No awareness of `${zeropsSubdomainHost}` as a literal template.

### Fix shipped in the same session — workspace/deliverable split + real stitch

1. **Split YAML emitter** (`internal/recipe/yaml_emitter.go`):
   - Added `Shape` type (`ShapeWorkspace` | `ShapeDeliverable`).
   - New `EmitWorkspaceYAML(plan)` — services-only, dev+stage pairs per
     codebase, dev runtimes `startWithoutCode: true`, stage runtimes omit
     it, no `project:` block, no `buildFromGit`, no `zeropsSetup`, no
     preprocessor expressions. Never written to disk; returned inline for
     `zerops_import content=<yaml>`.
   - Renamed `EmitImportYAML` → `EmitDeliverableYAML` (old name kept as
     a thin delegate for back-compat).
   - Enforcement by construction — the workspace path never emits the
     forbidden fields; no runtime validator needed.

2. **`emit-yaml` action takes `shape`** (`internal/recipe/handlers.go`):
   - `shape=workspace` returns yaml inline, does NOT write to disk
     (provision submits via `zerops_import content=<yaml>`).
   - `shape=deliverable` writes `<outputRoot>/<tier.Folder>/import.yaml`
     so the finalize gate can verify presence.
   - Default is `deliverable` when omitted.

3. **Real `stitch-content`** (`internal/recipe/handlers.go`):
   - Archives the writer payload at `.writer-payload.json` (gate reads).
   - Merges `env_import_comments` → `plan.EnvComments`.
   - Merges `project_env_vars` → `plan.ProjectEnvVars`.
   - Regenerates all 6 deliverable yamls to disk with writer-authored
     comments + project env vars.
   - Writes root `README.md`, env `<tier.Folder>/README.md`, per-codebase
     `codebases/<hostname>/README.md` (IG + KB fragments with markers),
     per-codebase `codebases/<hostname>/CLAUDE.md`.

4. **Atoms rewritten**:
   - `phase_entry/provision.md` — explains workspace vs deliverable
     distinction, tells the agent to `emit-yaml shape=workspace` + pass
     inline to `zerops_import content=`, then `zerops_env project=true`
     for secrets + `zerops_discover includeEnvs=true` for cross-service
     keys. No disk write.
   - `phase_entry/finalize.md` — explains the template model (shared
     secrets as `<@generateRandomString>`, URLs with
     `${zeropsSubdomainHost}` literal, per-env shape for `project_env_vars`).
   - `briefs/writer/completion_payload.md` — adds `project_env_vars` as a
     first-class key with per-env shape + leak rules.
   - New `principles/env-var-model.md` — single-source explanation of
     the three timelines (workspace / scaffold / deliverable) and the
     leak rule from timeline 1 into timeline 3.

5. **Tests pin the contract**:
   - `TestEmitWorkspaceYAML_ShapeContract` — workspace yaml forbids
     `project:`, `buildFromGit:`, `zeropsSetup:`, preprocessor, and
     requires `startWithoutCode: true`.
   - `TestDispatch_StitchContent_MergesEnvFieldsAndRegenerates` — the
     full stitch pipeline: payload merge → deliverable regeneration →
     content surface writes, with `${zeropsSubdomainHost}` preserved as
     literal (template-leak canary).

### Still not captured (conscious defer)

- `codebase_zerops_yaml_comments` splicing into per-codebase
  `zerops.yaml` files at their anchors — the `zerops.yaml` lives on the
  Zerops service mount, not in the output tree. Deferred until Commission
  B surfaces a concrete anchor-splice mechanism.
- `verify-subagent-dispatch` — still not implemented; scaffold atom
  acknowledges this and tells the main agent not to paraphrase briefs.
- Chain-resolution diff-aware yaml emission (plan §7 "engine renders
  import.yaml for showcase tiers by diffing against parent's env
  import.yaml"). Current emitter emits full yaml per tier; delta mode is
  Commission C.

