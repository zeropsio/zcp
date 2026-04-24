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

---

## 2026-04-23 later — v9.5.5: workflow-context gate + CLAUDE.md teach recipe flow

### Context

Run 5 dogfood (`runs/5/RAW_CHAT.md`) with v9.5.4. Progress was clean
through research + provision-yaml emit, then regressed on step 2 of
the provision atom. The agent called `zerops_import content=<yaml>`
verbatim as the atom instructs, but got:

```
{"code":"WORKFLOW_REQUIRED","error":"No active workflow. This tool
requires a workflow context.","suggestion":"Start a workflow:
workflow=\"bootstrap\" or workflow=\"develop\"."}
```

The agent then followed the error suggestion + the project CLAUDE.md's
"Bootstrap first when there are no services yet" guidance and started a
full bootstrap workflow, abandoning the recipe flow entirely. Two root
causes, both engine-side.

### Root causes

1. **Workflow-context gate didn't know about v3 recipe sessions.**
   `internal/tools/guard.go::requireWorkflowContext` guards
   `zerops_import` and `zerops_mount`; its comment promised it would
   accept "bootstrap/recipe session OR an open work session" but the
   implementation only checked v2's engine. A live v3 `recipe.Store`
   session wasn't recognized as valid context.

2. **CLAUDE.md template taught two entry points, not three.**
   `internal/content/templates/claude.md` instructed agents to start
   `zerops_workflow bootstrap` when there were no services yet — the
   exact reflex that derailed run 5. The template had zero mention of
   `zerops_recipe` so the agent had no frame for "this is a recipe run,
   not infra work."

### Fixes shipped

1. `recipe.Store.HasAnySession()` — new public predicate. Returns true
   if at least one recipe session is open in the store.

2. `requireWorkflowContext(engine, stateDir, recipeProbe
   RecipeSessionProbe)` — third argument is a nil-safe interface probe.
   `internal/tools/guard.go` declares `RecipeSessionProbe` (avoids a
   hard cross-package import of `internal/recipe`); `*recipe.Store`
   satisfies it. An active recipe session now satisfies the guard.

3. `RegisterImport` + `RegisterMount` in `internal/tools/` plumb the
   probe through; `server.go` passes the single `recipeStore` instance.

4. Error message updated to list `zerops_recipe action="start"` as the
   first option so an agent that hits the guard in a recipe context
   sees the recipe path explicitly.

5. CLAUDE.md template rewrite — "Starting a task" section becomes
   "Three entry points — pick the right one", with recipe authoring as
   option 1. Explicitly tells the agent **not** to start bootstrap or
   develop workflows during recipe authoring. Points at
   `zerops_recipe action="status"` for recovery.

### Adoption gate — next problem surfaced

`requireAdoption` (`internal/tools/guard.go:38`) gates deploy-related
tools (`zerops_deploy` variants) on ServiceMeta entries under
`stateDir/services/`. Recipe-provisioned services don't write
ServiceMeta — so once run 5's fix lets `zerops_import` pass, the next
call (`zerops_mount`, then `zerops_deploy` at scaffold phase) will fail
the adoption gate. Currently gated to activate only after
`stateDir/services/` exists (migration path), so fresh zcp installs
bypass it, but any install with prior bootstrap state will block.

Two options to fix when it bites:
- Have `zerops_recipe complete-phase provision` write ServiceMeta for
  every plan hostname (coupling v3 to v2's state shape).
- Extend `requireAdoption` to ALSO accept recipe-session hostnames as
  adopted (cleaner — mirror the guard split used above).

Deferred until a dogfood run hits it.

---

## 2026-04-23 even later — v9.5.6: zerops_knowledge scope + scaffold consults-before-writing

### Context

Run 6 dogfood (`runs/6/`) with v9.5.5. Research + provision + scaffold
dispatch + three sub-agent deployments + preship all green — the core
pipeline is now unblocked end-to-end. But the sub-agents surfaced four
runtime "gotchas" that all classify as self-inflicted / framework-quirk
per `docs/spec-content-surfaces.md` — none are platform traps, all are
agent discovery errors corrected at deploy time:

- nats.js v2 config takes structured `{servers, user, pass}` fields;
  sub-agent composed `nats://user:pass@host:port` URL and got rejected.
- Object-storage endpoint is `https://`; sub-agent wrote `http://` and
  hit 301-to-HTML-parse failure on S3 SDK v3.
- NestJS worker uses `createMicroservice`, not `create()`; sub-agent
  used the HTTP factory first.
- Vite preview log format doesn't match verify `startup_detected` regex
  (engine-side false negative, not gotcha material).

Per `spec-content-surfaces.md` classification taxonomy:
- "Framework quirk" → **DISCARD** (framework docs, not Zerops recipe)
- "Self-inflicted" → **DISCARD** (our code had a bug; reasonable porter
  won't hit it)

All four would be correctly refused at editorial-review even if
recorded. Fixing `zerops_record_fact` to accept v3 sessions (the
obvious next-like-v9.5.5 move) would record more discardable garbage,
not solve the content-quality problem.

### Root cause

None of the three scaffold sub-agents called `zerops_knowledge` during
their runs. They worked from framework training + trial-and-error at
deploy. The reason: v9.5.1's `zerops_knowledge` description rewrite
said **"NOT for authoring a new recipe via zerops_recipe"**. That
exclusion was meant to stop the MAIN agent during RESEARCH from
substituting zerops_knowledge for its framework knowledge (picking
services/versions). Over-broadened in v9.5.1: scaffold / feature /
writer sub-agents read "recipe authoring" as covering their phase too,
and skipped the one tool that would have told them "nats uses
structured fields" and "object-storage is https".

### Fixes shipped

1. **`zerops_knowledge` description narrowed** (`internal/tools/knowledge.go`)
   — exclusion scoped to `zerops_recipe` *research phase* only;
   sub-agents explicitly encouraged to consult for managed-service
   connection patterns before writing client code. Word count stays
   under the 60-word annotation cap.

2. **Scaffold `platform_principles.md` adds "Before writing client
   code" section** — every scaffold sub-agent's brief now tells it to
   call `zerops_knowledge runtime=<type>` or
   `zerops_knowledge query="<service> connection"` for each managed
   service BEFORE writing setup. Names the exact self-inflicted bugs
   that come from skipping (nats URL composition, object-storage
   scheme). Fits the 3 KB scaffold brief cap — earlier draft blew past
   and had to be tightened.

### Deeper lesson

The KB surface captures ONLY platform×framework intersections — not
agent self-inflicted bugs. The "four lost gotchas" framing from run-6
analysis was wrong: the right fix is upstream (stop generating
self-inflicted bugs by making sub-agents consult authoritative sources
first), not downstream (record more of them so editorial-review can
discard them).

### Still deferred

- `zerops_record_fact` + `zerops_workspace_manifest` still gate on v2's
  `engine.SessionID()`. Will bite at finalize (writer reads
  `workspace_manifest`). Same one-file fix pattern as v9.5.5's
  workflow-context probe — deferred until finalize actually hits it.
- `requireAdoption` gate on recipe-provisioned services (see v9.5.5
  section).

---

## 2026-04-24 — run-8-readiness: writer dispatch out, in-phase fragment authorship in

### Context

Run 7 closed 5 phases with trivial gates — structural only, prose
content never validated. The writer sub-agent reconstructed reasoning
from committed files that scaffold + feature already had in hand. That
reconstruction is both the efficiency hole and the quality hole:
stale, guessed causality on the reader-facing surfaces.

Plan: [plans/run-8-readiness.md](plans/run-8-readiness.md). Seven
commits in the order E → A1 → A2 → F → B → C → D, each green on local
tests + `make lint-local` before the next.

### Workstreams shipped

**E — deferred gate plumbing** (feat(recipe): route record_fact +
workspace_manifest under recipe session). `RecipeSessionProbe` gains
`CurrentSingleSession()`; the two v2-shaped tools resolve their target
paths from the single open recipe session's outputRoot instead of
erroring. v2 facts land in `legacy-facts.jsonl`; v3's `facts.jsonl`
stays reserved for `zerops_recipe action=record-fact`.

**A1 — templates + Plan.Fragments schema + assembler + record-fragment**
(refactor(recipe): replace writer dispatch with in-phase fragment
authorship). Engine owns structural templates (`content/templates/*.tmpl`,
string-replace tokens + fragment markers); fragments slot in via
`record-fragment` at the moment the agent holds the densest context.
Writer brief + examples + completion payload deleted. `stitchContent`
now walks surface templates, returns a missing-fragments list callers
gate on.

**A2 — two-root deliverable split + committed-yaml copy**. Per-codebase
`zerops.yaml` is copied verbatim from `Codebase.SourceRoot` (scaffold
sub-agent's workspace) into `outputRoot/codebases/<hostname>/zerops.yaml`,
so inline comments written at decision-moment survive byte-identical
into the published deliverable.

**F — content-authoring briefs + init-commands concept port**. New
`content/principles/init-commands-model.md` (ported from v2's
seed-execonce-keys.md), `briefs/scaffold/content_authoring.md`,
`briefs/feature/content_extension.md`. Engine-side `CitationMap`
(`citations.go`) replaces the deleted writer's citation_topics. Brief
caps raised from 3 KB/4 KB to 5 KB/5 KB — the original caps were set
before F's content was scoped.

**B — phase atom completeness** (feat(recipe): phase atom completeness).
Scaffold atom adds cross-deploy dev→stage + init-commands verification
(success-line attestation + post-deploy data check + burned-key
recovery). Feature atom adds seed step + browser-walk + cross-deploy
dev→stage. Finalize atom adds the single-question test per surface
from spec-content-surfaces.md. Wrapper-discipline refinement clarifies
what the main agent decides vs what the sub-agent discovers.

**C — classification pre-routing** (feat(recipe): engine-side fact
classification as safety net). `Classify` maps surface hints +
citation to the seven-class taxonomy from spec-content-surfaces.md.
`ClassifyLog` partitions publishable from DISCARD-class facts; the
safety net ensures framework-quirk / self-inflicted / library-
metadata records never reach a surface body even if mis-tagged.

**D — spec validators** (feat(recipe): spec validators per surface +
cross-surface uniqueness). Seven per-surface `ValidateFn`s wired via
`RegisterValidator` + `gateSurfaceValidators` on `FinalizeGates`:
root README factuality + deploy-button count + length; env README
meta-agent-voice + tier promotion verb; import-comments causal-word +
templated-opening; codebase IG numbered items + no-scaffold-filenames;
codebase KB bold symptom + citation-map guide references; CLAUDE.md
size floor + custom sections; zerops.yaml causal comments. Cross-
surface uniqueness on fact Topic ids.

### §7 open questions resolved

- **Q1 template format** — string-replace with `{TOKEN}` sigils + post-
  render unreplaced-token scan. Rationale: single substitution engine
  (markers + tokens share the same replace pass), no accidental
  parse failures on fragment bodies containing `{{`, templates diff
  cleanly against the reference `laravel-showcase/README.md`.
- **Q2 marker naming** — kept upstream's `#ZEROPS_EXTRACT_START:NAME#`
  (matches `zcp sync push recipes` extractor).
- **Q3 seed script location** — moot. Atom corpus stays framework-
  neutral per the no-framework-specific-atoms rule; seed shape is the
  sub-agent's framework-expertise call.
- **Q4 browser verification artifact** — FactRecord with
  `Type=browser_verification`, console + screenshot path in Evidence.
  Reuses the facts-log pipeline; no new schema.
- **Q7 committed-yaml-comment validator scope** — validate the WHOLE
  committed file (not just scaffold-authored stanzas). Rationale: that
  file IS the deliverable porters read; authorship origin is not the
  porter's concern.
- **Q9 validator failure → main-agent edit vs re-dispatch** — main-
  agent edits allowed. Iteration via `record-fragment` + re-stitch;
  no scaffold/feature re-dispatch. Preserves densest-context
  authorship the earlier phase already paid for.

### Non-goals / still deferred

- Chain-resolution delta yaml emission (§5.2 of plan) — defer until
  nestjs-minimal gets re-run via v3.
- Automated click-deploy verification — acceptance check 10 stays
  manual at run-8 start.
- `verify-subagent-dispatch` SHA check — real dispatch-integrity
  concern but separate from content-quality; ship after run 8
  confirms the content pipeline works.
- `requireAdoption` fix for recipe-provisioned services — inherited
  from v9.5.5.

### What run 8 proves

1. Every codebase has both dev and stage deploys green.
2. Browser verification recorded as a `browser_verification` fact per
   feature tab.
3. Seed ran once; GET /items returns ≥ 3 items before the agent
   manually creates anything.
4. Stitched output has canonical structure (root README, 6 tier
   READMEs + import.yamls, per-codebase README + CLAUDE.md +
   zerops.yaml).
5. Every finalize-phase validator passes — prose content, not just
   structure.
6. Fragments were authored in-phase; facts log shows `record-fragment`
   calls by scaffold + feature sub-agents, no writer dispatch.
