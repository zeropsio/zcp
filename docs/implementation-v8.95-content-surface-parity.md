# v8.95 — Content-Surface Parity (standalone implementation guide)

**Intended reader**: A fresh Opus 4.7 instance (or equivalent) tasked with implementing this change from scratch. This doc is self-contained — you don't need prior conversation context.

**Prerequisite reading (in order)**:
1. [docs/recipe-version-log.md](recipe-version-log.md) §v29 entry — the run that triggered this plan. Read the "Three confirmed defect classes shipped" paragraph in full.
2. [docs/implementation-v8.94-content-authoring.md](implementation-v8.94-content-authoring.md) §§1–5 — the previous release. v8.95 extends v8.94; it does not replace it. You need the mental model of the fresh-context writer subagent + facts log + classification taxonomy.
3. [internal/workflow/recipe_templates.go](../internal/workflow/recipe_templates.go) — **read this BEFORE anything else**. The "env-README fabrications" I initially attributed to the writer subagent are actually hardcoded in this Go file. Two dry-run simulations revealed that env READMEs are NOT writer output; they come from `GenerateEnvREADME` at runtime, always overwriting whatever the writer writes at finalize time via `BuildFinalizeOutput`.
4. [CLAUDE.md](../CLAUDE.md) — project conventions, especially TDD ("RED before GREEN"), "max 350 lines per .go file", "fix at the source, not downstream", "no fallbacks."

**Target ship window**: single release (v8.95). Three narrow fixes; do not expand scope.

---

## 0. Dry-run simulation status

**This guide was written, then stress-tested via THREE successive simulations. The first two caught implementation traps + attribution errors. The third verified proposed replacement text against Zerops docs + internal knowledge and found two more fabrications + API errors that would have shipped had they gone unchecked.** The guide text below reflects all three rounds of corrections.

### First simulation (surface-level gaps)

Six implementation traps identified in the initial guide draft. All corrected in-place:

1. §5.3 originally assumed a `/tmp/zcp-brief-discards-*.json` file that doesn't exist. Fixed: writer emits `ZCP_CONTENT_MANIFEST.json` structured output; check reads from there.
2. §5.5 originally assumed `validateImportYAML` could cross-reference env yamls. Reality: it can't. Fixed: first-pass loop in `checkRecipeFinalize` to load all env yamls into `map[int]importYAMLDoc`, then iterate for README checks.
3. `workflow_checks_finalize.go` (691 lines) and `workflow_checks_recipe.go` (1,095 lines) are already past the 350-line limit. Fixed: 3 new files for §§5.4/5.5/5.6/5.7; thin wire-up in existing files.
4. §5.4 should fire at generate-step (scaffold-write time), not deploy-step.
5. §5.4 reference-detection must enumerate ALL command-bearing YAML fields.
6. §5.6 needs causation-link detection, not token co-occurrence.

### Third simulation (Zerops-docs factual verification — more fabrications avoided)

After the second simulation's root-cause correction, proposed Go-template replacement text was verified against Zerops docs at `/Users/fxck/www/zerops-docs/apps/docs/content/` and internal knowledge guides at `/Users/fxck/www/zcp/internal/knowledge/guides/`. Three concrete fabrications caught:

1. **`override-import` does not exist.** An earlier version of this guide proposed replacement text citing `override-import` as the in-place promotion mechanism. Exhaustive `grep -rln 'override.import'` across Zerops docs + internal knowledge returns zero matches. The actual zcli commands per [references/cli/commands.mdx](/Users/fxck/www/zerops-docs/apps/docs/content/references/cli/commands.mdx) are `zcli project project-import` (creates a new project) and `zcli project service-import` (adds services to an existing project — additive, does not modify existing services). There is no "override" variant. Fixing the v29 fabrication by citing `override-import` would have shipped a second fabrication. §5.2 now removes the false data-persistence claim WITHOUT replacing it with a different mechanism claim — honesty over completeness.

2. **Zero-downtime ≠ readinessCheck dependency.** Earlier replacement text for §5.3 said *"Rolling deploys at `minContainers: 2` are zero-downtime when `readinessCheck` is configured"*. Per `deployment-lifecycle.md`:
   - Default (`temporaryShutdown: false`) IS zero-downtime — "new containers start BEFORE old ones stop"
   - readinessCheck GATES traffic routing; it does NOT enable zero-downtime
   - `minContainers: 2+` provides redundancy during rollover, not zero-downtime per se

   Proposed text rewritten to acknowledge these semantics. The factual truth is more nuanced than my first-pass template edit captured.

3. **Mode immutability forces new project.** Per `scaling.md`, managed-service `mode` (NON_HA/HA) is immutable after creation. Tier 4→5 promotion (which includes the DB/cache mode flip) literally cannot apply in-place — it requires a new project. Template text for `envPromotionPath(4)` updated to say this explicitly.

### Second simulation (root-cause misattribution — THE LOAD-BEARING FINDING)

The INITIAL attribution of v29's env-README defects to writer behavior was **factually wrong**. The second simulation traced each defect to the actual Go source line that produces it:

| v29 defect | Actual source |
|---|---|
| env 0 README L26: *"Data persists across tier promotions because service hostnames stay stable..."* | `recipe_templates.go:172` (hardcoded byte-for-byte in `GenerateEnvREADME`) |
| env 0 README L33: *"Existing data in DB / cache / storage persists across the tier bump..."* | `recipe_templates.go:305` (hardcoded in `envPromotionPath(0)`) |
| env 3 README L32: *"Runtime container stays at `minContainers: 1`"* | `recipe_templates.go:324` (`envPromotionPath(3)`) |
| env 4 README L24: *"Runtime services stay at `minContainers: 1`..."* | `recipe_templates.go:279` (`envDiffFromPrevious(4)`) |
| env 4 README L33: *"Runtime `minContainers` flips to `2`..."* | `recipe_templates.go:330` (`envPromotionPath(4)`) |
| env 4 README L41: *"Deploys incur downtime because `minContainers: 1`..."* | `recipe_templates.go:370` (`envOperationalConcerns(4)`) |
| env 5 README L20: *"Every runtime `minContainers` flips to `2`..."* | `recipe_templates.go:285` (`envDiffFromPrevious(5)`) |

**Evidence**: `grep -n "Data persists across tier promotions" /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v29/environments/*/README.md recipe_templates.go` returns both the published README file AND the Go source — byte-identical lines.

**Mechanism**: The writer subagent DOES create env READMEs under `/var/www/environments/{N — brief-name}/README.md` at deploy.readmes time. But at finalize time, `handleRecipeGenerateFinalize` at [internal/tools/workflow_recipe.go:246](../internal/tools/workflow_recipe.go#L246) calls `BuildFinalizeOutput(plan)` which:

1. Calls `GenerateEnvREADME(plan, envIndex)` to produce the env README content (this is the hardcoded-claims source).
2. Writes files keyed by `envTiers[i].Folder` — `"0 — AI Agent"`, `"1 — Remote (CDE)"`, etc. — the `EnvFolder()` names.
3. `OverlayRealREADMEs` at [internal/workflow/recipe_overlay.go:27](../internal/workflow/recipe_overlay.go#L27) overlays per-CODEBASE READMEs from SSHFS mounts, but NOT env READMEs.
4. Writes to disk, overwriting any previous content at the target path.

Net effect: writer's env-README output lands at `/var/www/environments/0 — AI Agent Workspace/README.md` (per its brief); finalize writes Go-generated content to `/var/www/environments/0 — AI Agent/README.md` (per `EnvFolder(0)`); both folders exist on disk; the publish pipeline (`zcp sync recipe export`) copies only the finalize-generated paths. Writer's env-README work is orphaned.

**Implication**: Every v8.95 fix targeting env READMEs must change the Go templates, not the writer brief. The second simulation's corrections replace §§5.5 + §5.6 (finalize-time content checks scanning writer output) with §5.2 + §5.3 (direct Go-template edits + regression tests).

The writer-DISCARD-enforcement fix (renamed §5.4 below) remains valid — it targets per-codebase gotcha surfaces, which DO flow through `OverlayRealREADMEs` and reach the published tree.

---

## 1. Context — why this exists

v29 shipped v8.94's fresh-context content-authoring subagent. The architecture worked for gotchas — **gotcha-origin ratio jumped from v28's 33% genuine to v29's 79% genuine**, with zero folk-doctrine fabrications in the gotcha surface.

But v29's honest audit found three defect classes:

1. **`apidev/scripts/preship.sh` (2,840 bytes, 12 pre-ship assertions) shipped in the published deliverable** — the apidev scaffold subagent authored this file to run its own pre-flight self-test, committed it via `git add -A`, never cleaned up. Assertions like `fail "README.md must not be written by scaffolder"` are meaningful only during recipe authoring. Asymmetric: appdev + workerdev subagents used inline `ssh ... "set -e; ..."` chains. No rule forbade committed self-test scripts.

2. **env 0 README ships a cross-tier data-persistence fabrication** — lines 26 + 33 claim data persists across tier promotions because hostnames stay stable. Factually wrong: the six env `import.yaml` files declare six distinct `project.name` values, so `zerops_import` creates a NEW project per tier. **Second simulation revealed**: these lines are hardcoded in `recipe_templates.go:172` and `recipe_templates.go:305`, NOT produced by the writer subagent. The `v8.94` release that introduced the 40-80-line env READMEs encoded the fabrication into Go source.

3. **env 3/4/5 README minContainers factual drift** — four wrong claims across three env READMEs about minContainers at env 4. **Second simulation revealed**: all originate from hardcoded `switch envIndex` arms in `envDiffFromPrevious`, `envPromotionPath`, and `envOperationalConcerns` at `recipe_templates.go:254-337+343-380`. No runtime input; static text that drifted from the YAML it claims to describe.

4. **Writer DISCARD override** — writer kept 2 of 14 facts (14% override rate) the brief had explicitly classified as DISCARD. healthCheck-bare-GET (recipe-authoring concern) shipped as apidev gotcha; Multer-FormData (framework-quirk documenting own scaffold) shipped as appdev gotcha.

The first three are independent. The fourth IS a writer-brief concern that v8.94's architecture can tighten. All four get fixed in v8.95.

## 2. Goals

**Goal 1 — Scaffold-phase artifact blocklist**. Scaffold subagents must not leave committed files behind that serve only the recipe-authoring process. Adds a scaffold-brief prohibition + a generate-step check that walks the codebase tree.

**Goal 2 — Fix the Go-template env-README defects directly**. Edit `recipe_templates.go` to:
- Remove the fabricated "data persists across tier promotions because hostnames stay stable" claim.
- Correct the minContainers claims so each env's README prose matches what `GenerateEnvImportYAML(envIndex)` produces for the same envIndex.
- Add regression tests that pin each claim against the YAML's declared values.

**Goal 3 — DISCARD enforcement as hard gate for codebase README gotcha surfaces**. Writer subagent emits a structured `ZCP_CONTENT_MANIFEST.json` before returning. Post-writer check reads the manifest and enforces: facts classified framework-quirk/library-meta/self-inflicted must either route to `discarded` OR carry a non-empty `override_reason`.

### Explicit non-goals

- **No move of env-README authorship to the writer subagent.** That would require cutting the Go templates, wiring `OverlayRealREADMEs` to pick up writer-produced env READMEs, and resolving the folder-name mismatch (writer's `0 — AI Agent Workspace` vs `EnvFolder` `0 — AI Agent`). Architecturally cleaner but out of scope for v8.95 — too many moving parts. Fix the text in-place first; re-architect authorship in v8.96+ if the static-template approach proves fragile against plan-variance.
- **No env-README finalize-time content checks.** The initial guide proposed `env_readme_factual_claims` + `env_readme_promotion_semantic` checks. Under the current architecture (Go-generated env READMEs are deterministic per envIndex, modulo `envDescription(plan, envIndex)` variance), such checks would either always pass (after template fixes) or always fail (before template fixes). They guard against nothing useful once the templates are correct. Regression tests in `recipe_templates_test.go` are the right layer.
- **No new content-quality checks at the gotcha-origin layer.** v8.94's gotcha-origin architecture works — sustain, don't extend.
- **No rework of the v8.94 classification taxonomy** (framework-quirk-with-recurrence-risk → scaffold-preamble route is a v8.96 concern).
- **No rollback of any v8.94 mechanism.** `zerops_record_fact` stays mandatory. Writer subagent stays fresh-context.

---

## 3. Scope — what's in and what's not

### In scope for v8.95

1. **Scaffold-brief rule** (in `recipe.md` scaffold-subagent-brief block): "pre-ship verification uses inline `ssh ... "set -e; ..."` chains, not committed files; any `scripts/`, `test/`, `verify/`, `assert/` directory present at generate-complete must be referenced by `zerops.yaml` or it fails the check."
2. **New check** `{hostname}_scaffold_artifact_leak` at generate-step complete. Walks each codebase tree; fails on unreferenced scaffold-phase files.
3. **Edit `recipe_templates.go`** to fix v29's hardcoded env-README defects (ten in-place edits — the original seven plus three added by the post-draft verification pass that caught cross-section contradictions the numerical-claim edits would have left behind):
   - L172 (`GenerateEnvREADME`, envIndex 0, "First-tier context" section) — remove "Data persists across tier promotions..." fabrication, replace with correct mechanism teaching.
   - **L236-239 (`envAudience(4)` sibling bullets, G1)** — "Runtime runs one container" / "Expect brief downtime on every deploy" / "no HA replica" contradict the new minContainers:2 claims in Edits 1/4/5 below; edit together.
   - L279 (`envDiffFromPrevious(4)`) — change "Runtime services stay at `minContainers: 1`" to "Runtime services run at `minContainers: 2`" (matching what env 4 `GenerateEnvImportYAML` produces).
   - L285 (`envDiffFromPrevious(5)`) — the "flips to 2" framing is wrong once env 4 already has 2; change to "`minContainers: 2` already set at Small Production; the HA-distinct change is `mode: HA` on DB and cache..."
   - **L302 (`envPromotionPath(0)` first bullet, G2)** — "Deploy ... into your Zerops project" implies in-place promotion; replace with "this provisions a new project for the Remote tier; it does NOT modify your AI-Agent tier project".
   - L305 (`envPromotionPath(0)`) — remove "Existing data in DB / cache / storage persists..." fabrication; replacement is plan-independent (uses stable `-agent`/`-remote` suffix convention, NOT recipe slug).
   - L324 (`envPromotionPath(3)`) — change "Runtime container stays at `minContainers: 1`" to "Runtime services run at `minContainers: 2` — one replica keeps serving while the other rolls..."
   - **L326 last bullet (`envPromotionPath(3)` sibling, G1)** — "Plan for brief deploy-time downtime windows" contradicts the L324 edit; replace with readinessCheck guidance.
   - L330 (`envPromotionPath(4)`) — remove "Runtime `minContainers` flips to `2`" line (no flip happens at env 4→5 because both are 2); replace with "Runtime services continue at `minContainers: 2`..." with mode-immutability note.
   - L370 (`envOperationalConcerns(4)`) — change "Deploys incur downtime because `minContainers: 1`" to "Rolling deploys at `minContainers: 2` keep one replica serving..."
4. **Regression tests in `recipe_templates_test.go`**:
   - `TestGenerateEnvREADME_NoDataPersistenceFabrication` — for each envIndex 0..5, assert README never contains phrases like "data persists across tier promotions", "hostnames are identical", "persists because hostnames".
   - `TestEnvPromotionPath_NoFabricatedMechanism` — assert no promotion-path string contains `override-import` / `override_import` / `project-override`, since those are not real Zerops mechanisms (the third dry-run established this).
   - `TestGenerateEnvREADME_MinContainersMatchesYAML` — for each envIndex, extract numeric `minContainers: N` claims from the README and assert each matches a value declared on a runtime service in env N's YAML **OR env N+1's YAML** (union semantics — promotion-path prose legitimately describes the TARGET tier). Uses the `declaredRuntimeMinContainers` helper.
   - `TestEnvPromotionPath_CrossEnvClaimsMatchTargetYAML` — tighter per-section pin: claims appearing specifically in `envPromotionPath(N)` must match env N+1's YAML only.
5. **Writer-brief extension** (in `recipe.md` content-authoring-brief block): require writer to emit `ZCP_CONTENT_MANIFEST.json` at `/var/www/ZCP_CONTENT_MANIFEST.json` before returning. Specify exact JSON shape. Extend permitted-tools to allow Write to that specific path.
6. **New check** `writer_content_manifest` at `complete step=deploy` (as part of `checkRecipeDeployReadmes`). Two sub-checks (a) classification-consistency: each fact classified `framework-quirk|library-meta|self-inflicted` with `routed_to != "discarded"` requires non-empty `override_reason`; (b) manifest-honesty: for facts marked `routed_to: discarded`, grep published gotcha stems for Jaccard-similar titles (≥ 0.3 threshold, stop-word-stripped) — fail if found.

### NOT in scope for v8.95

- Move of env-README authorship to writer subagent (see non-goals above)
- Env-README finalize-time content checks (templates are static; regression tests are the right layer)
- Taxonomy extension for "framework-quirk with recurrence risk"
- Editorial-pass subagent / second-writer review
- Folder-name reconciliation between writer brief ("0 — AI Agent Workspace") and `EnvFolder()` ("0 — AI Agent"). The writer's output is orphaned either way; fixing the folder-name mismatch without changing authorship is cosmetic. Revisit together with env-README authorship rework in v8.96.

---

## 4. Architecture — the before/after shape

### Before (v29 state)

```
generate.scaffold substep:
  scaffold subagent writes files + runs inline ssh verification
  apidev subagent ALSO wrote scripts/preship.sh (2.8 KB) and committed it — no rule against this

generate.zerops-yaml → generate-complete:
  existing checks fire; no scaffold-artifact check

deploy.readmes substep:
  fresh-context writer subagent (v8.94)
  writer writes:
    - 3 per-codebase READMEs + 3 CLAUDE.md (will be overlaid by OverlayRealREADMEs into finalize output)
    - 6 env READMEs at /var/www/environments/{N — writer's label}/README.md (ORPHANED)
    - classification tallies in return prose (no structured output)
  writer returns to main agent

deploy.readmes complete → deploy complete:
  checkRecipeDeployReadmes runs per-codebase README content checks
  no check of writer classification vs published gotchas

finalize step:
  handleRecipeGenerateFinalize calls BuildFinalizeOutput(plan)
  GenerateEnvREADME writes hardcoded env README text → /var/www/environments/{N — EnvFolder}/README.md
    ^^ This is where v29's fabrication + factual drift come from, NOT from the writer
  OverlayRealREADMEs overlays /var/www/{hostname}dev/README.md → files["{hostname}dev/README.md"]
  files flushed to disk at outputDir
```

### After (v8.95 state)

```
generate.scaffold substep:
  scaffold subagent brief FORBIDS committed self-test scripts (new rule)

generate-complete:
  NEW: checkScaffoldArtifactLeak §5.1 — walks each codebase tree, fails on unreferenced scripts/

deploy.readmes substep:
  fresh-context writer subagent (v8.94 shape, extended brief)
  NEW: writer brief requires structured ZCP_CONTENT_MANIFEST.json before return
  NEW: permitted-tools allows Write to /var/www/ZCP_CONTENT_MANIFEST.json

deploy complete:
  existing checkRecipeDeployReadmes runs
  NEW: checkWriterContentManifest §5.4 — reads manifest, enforces DISCARD classification

finalize step:
  handleRecipeGenerateFinalize unchanged — but GenerateEnvREADME text now correct
  FIXED: recipe_templates.go lines 172, 279, 285, 305, 324, 330, 370 edited
  NEW: regression tests in recipe_templates_test.go pin the claims against YAML truth
```

**Key property**: env-README fix is at the source (Go template edit); writer-DISCARD fix is at the destination the writer actually controls (per-codebase READMEs). No misdirected work at surfaces the writer doesn't reach.

---

## 5. Fixes (detailed)

### 5.1 Scaffold artifact leak check

**Problem**: apidev scaffold subagent in v29 wrote `scripts/preship.sh` as a pre-ship verification script, committed it via `git add -A`, never cleaned up. File shipped in published deliverable. Asymmetric: appdev + workerdev used inline `ssh ... "set -e; ..."` chains. No rule forbade the file-based approach.

**File (new)**: `internal/tools/workflow_checks_scaffold_artifact.go` (~120 lines — raised from 100 after API reconciliation below).

**Firing point**: `checkRecipeGenerate` at [internal/tools/workflow_checks_recipe.go:74](../internal/tools/workflow_checks_recipe.go#L74). Specifically, extend `checkRecipeGenerateCodebase` which already iterates per-codebase with mount-path access. Generate-step is the right firing point because:
- Scaffold subagents write at generate, not deploy — firing here catches the leak when it first lands.
- Remediation is cheap: `ssh {codebase} "cd /var/www && rm scripts/preship.sh && git add -A && git -c user.email=scaffold@zcp.local -c user.name=scaffold commit --amend --no-edit"` + retry `complete step=generate`. The `-c user.email=... -c user.name=...` flags are inline overrides so the amend succeeds even if the container's `git config` was never populated by the initial scaffold — they apply only to this one commit, never modify the container's git config.
- Firing at deploy (30+ min later) would catch the same leak but multiple redeploys have already shipped the file.

**API reality (verified against codebase)**:
- Type is `*ops.ZeropsYmlDoc`, NOT `*ops.ZeropsYml` — see [internal/ops/deploy_validate.go:125](../internal/ops/deploy_validate.go#L125).
- Top-level field is `doc.Zerops []ZeropsYmlEntry`, NOT `doc.Setups`.
- `ZeropsYmlEntry.Run` is `zeropsYmlRun` (lowercase type — package-private, but field access is public through the struct).
- `zeropsYmlRun` at [deploy_validate.go:252](../internal/ops/deploy_validate.go#L252) models: `Base`, `Start`, `Ports`, `HealthCheck`, `DeployFiles`, `PrepareCommands` (type `any`), `EnvVariables`. **`InitCommands` is NOT modeled** in this struct (but exists in the YAML schema — see [references/importyml/type-list](references/importyml/type-list) for the full schema; `initCommands` lives under `deploy.initCommands` in some versions, under `run.initCommands` in others).
- `ZeropsYmlEntry.Build` is `zeropsYmlBuild` with `BuildCommands any` + `PrepareCommands any`.
- Since `PrepareCommands` / `BuildCommands` are typed `any`, they can be string OR []string — handle both cases via reflection or YAML-native any.
- `ParseZeropsYml(workingDir)` at [deploy_validate.go:161](../internal/ops/deploy_validate.go#L161) returns `(*ZeropsYmlDoc, error)`.

**Behavior**:

```go
package tools

import (
    "fmt"
    "path/filepath"
    "strings"

    "github.com/zeropsio/zcp/internal/ops"
    "github.com/zeropsio/zcp/internal/workflow"
)

func checkScaffoldArtifactLeak(ymlDir string, doc *ops.ZeropsYmlDoc, rawYAML, hostname string) []workflow.StepCheck {
    patterns := []string{
        "scripts/*.sh", "scripts/*.py",
        "verify/*", "assert/*", "preflight/*",
        "_scaffold*", "scaffold-*",
    }
    var leaks []string
    for _, pattern := range patterns {
        matches, _ := filepath.Glob(filepath.Join(ymlDir, pattern))
        for _, m := range matches {
            rel, _ := filepath.Rel(ymlDir, m)
            if isReferencedByZeropsYml(rel, doc, rawYAML) {
                continue
            }
            leaks = append(leaks, rel)
        }
    }
    if len(leaks) == 0 {
        return []workflow.StepCheck{{
            Name: hostname + "_scaffold_artifact_leak", Status: statusPass,
        }}
    }
    return []workflow.StepCheck{{
        Name:   hostname + "_scaffold_artifact_leak",
        Status: statusFail,
        Detail: fmt.Sprintf(
            "scaffold-phase artifacts present in %s but not referenced by zerops.yaml: %s. Scaffold subagents must run pre-ship verification via inline 'bash -c' or 'ssh <host> \"set -e; ...\"' chains, not committed files under the codebase tree. Remove these and amend the scaffold commit (inline git identity so amend succeeds even on an unconfigured container): ssh %s \"cd /var/www && rm %s && git add -A && git -c user.email=scaffold@zcp.local -c user.name=scaffold commit --amend --no-edit\".",
            hostname, strings.Join(leaks, ", "),
            hostname, strings.Join(leaks, " "),
        ),
    }}
}

// isReferencedByZeropsYml returns true if any command-bearing field in any
// setup entry mentions the script path. Because initCommands is NOT modeled
// in the local zeropsYmlRun struct (only string commands via PrepareCommands
// any-type), we fall back to raw-YAML substring search for safety. Two
// passes:
//   1. Structured search on modeled fields (run.start, run.prepareCommands,
//      build.prepareCommands, build.buildCommands) — catches the common case
//      with type safety.
//   2. Raw-YAML substring match — catches initCommands and any other
//      command-bearing field not modeled structurally.
// A false-negative (legitimate script not detected) is worse than a
// false-positive-free match — err on the "reference exists, skip" side.
func isReferencedByZeropsYml(relPath string, doc *ops.ZeropsYmlDoc, rawYAML string) bool {
    needles := []string{relPath, "./" + relPath}

    // Pass 1: structured fields.
    if doc != nil {
        for _, entry := range doc.Zerops {
            candidates := []string{entry.Run.Start}
            candidates = append(candidates, anyCommandsToStrings(entry.Run.PrepareCommands)...)
            candidates = append(candidates, anyCommandsToStrings(entry.Build.PrepareCommands)...)
            candidates = append(candidates, anyCommandsToStrings(entry.Build.BuildCommands)...)
            for _, cmd := range candidates {
                for _, n := range needles {
                    if strings.Contains(cmd, n) {
                        return true
                    }
                }
            }
        }
    }

    // Pass 2: raw-YAML substring — catches initCommands + any unmodeled field.
    for _, n := range needles {
        if strings.Contains(rawYAML, n) {
            return true
        }
    }
    return false
}

// anyCommandsToStrings normalizes a YAML `any` field that may be string
// OR []string into a flat []string. Both shapes are legal per the zerops
// schema: `buildCommands: - cmd1` (list) and `buildCommands: "cmd1"` (scalar).
func anyCommandsToStrings(v any) []string {
    switch x := v.(type) {
    case string:
        return []string{x}
    case []any:
        out := make([]string, 0, len(x))
        for _, item := range x {
            if s, ok := item.(string); ok {
                out = append(out, s)
            }
        }
        return out
    case []string:
        return x
    }
    return nil
}
```

**Integration point**: in `checkRecipeGenerateCodebase`, the caller has already done `ops.ParseZeropsYml(ymlDir)` (returning `doc, parseErr`). Pass BOTH the parsed doc AND the raw YAML contents to the check:

```go
// workflow_checks_recipe.go around line 180, after existing checks:
rawYAMLData, _ := os.ReadFile(filepath.Join(ymlDir, "zerops.yaml"))
if rawYAMLData == nil {
    rawYAMLData, _ = os.ReadFile(filepath.Join(ymlDir, "zerops.yml"))
}
checks = append(checks, checkScaffoldArtifactLeak(ymlDir, doc, string(rawYAMLData), hostname)...)
```

**Scaffold brief edit** — TWO coordinated changes to `internal/content/workflows/recipe.md` scaffold-subagent-brief block (NOT content-authoring-brief). Both MUST land together; the second without the first leaves the ambiguous "save to a temp script" instruction intact and v29's failure-path remains reachable.

**Edit 1** — Fix the ambiguous invocation instruction at recipe.md L987. The existing text reads:

```
> Run via `bash -c '...'` (or save to a temp script and invoke it) on the zcp side against the mount.
```

v29's apidev subagent interpreted *"save to a temp script"* as `apidev/scripts/preship.sh` — inside the codebase tree. Replace that line with:

```
> Run via `bash -c '...'` directly, OR save to a scratch file OUTSIDE the codebase tree (e.g. `/tmp/zcp-preship-$$.sh`) and invoke it from there. NEVER save it under `/var/www/{hostname}/` — files committed to the codebase ship to the porter's clone, and assertions about "no README.md at generate-complete" are meaningless outside recipe authoring.
```

**Edit 2** — Append a new terminal subsection to the pre-ship block after the bash script's closing "As new recurrent traps surface..." paragraph, BEFORE the "**Reporting back:**" line (which sits at recipe.md L995):

```markdown
### Committed-artifact prohibition (v8.95)

The pre-ship script above runs ephemerally. No artifact from pre-ship verification — script file, results log, assertion helper — may remain in the codebase at generate-complete. A `{hostname}_scaffold_artifact_leak` check walks each codebase at generate-complete and fails on `scripts/*.sh`, `scripts/*.py`, `verify/*`, `assert/*`, `preflight/*`, `_scaffold*`, or `scaffold-*` entries that are NOT referenced by the codebase's `zerops.yaml` (via `run.start`, `run.prepareCommands`, `build.buildCommands`, `build.prepareCommands`, or raw-YAML substring match on `initCommands` or any unmodeled field).

If you need more than ~20 assertions, split into 2-3 separate `bash -c` or `ssh` invocations. Do NOT persist them as a script file inside the codebase.

Legitimate `scripts/*.sh` files used at runtime (e.g. a `healthcheck.sh` referenced from `run.start`) are fine — the check confirms the reference before failing. A script referenced by `zerops.yaml` is platform code, not scaffold-phase temp.
```

**Tests** (`internal/tools/workflow_checks_scaffold_artifact_test.go`, ~150 lines):

```go
func TestScaffoldArtifactLeak_v29_PreshipLeak(t *testing.T) {
    // Fixture: copy v29's apidev/scripts/preship.sh into tempdir
    // Assert: check fires with leaks=[scripts/preship.sh]
}

func TestScaffoldArtifactLeak_ReferencedScript_Passes(t *testing.T) {
    // Fixture: scripts/healthcheck.sh + zerops.yaml with run.start: "./scripts/healthcheck.sh"
    // Assert: pass
}

func TestScaffoldArtifactLeak_EmptyTree_Passes(t *testing.T) { ... }
func TestScaffoldArtifactLeak_InitCommandsReference_Passes(t *testing.T) { ... }
func TestScaffoldArtifactLeak_BuildCommandsReference_Passes(t *testing.T) { ... }
func TestScaffoldArtifactLeak_Multiple_Leaks(t *testing.T) { ... }
```

---

### 5.2 Fix env 0 README data-persistence fabrication

**Problem**: `recipe_templates.go:172` and `recipe_templates.go:305` each hardcode a factually wrong claim about cross-tier data persistence.

**File**: `internal/workflow/recipe_templates.go`.

**CRITICAL — verified against Zerops docs before writing replacement text**:

- `override-import` does **NOT** exist. Exhaustive grep across `/Users/fxck/www/zerops-docs/apps/docs/content/` + `/Users/fxck/www/zcp/internal/knowledge/` returns zero matches. The previous draft of this guide proposed replacement text citing `override-import` — that would have shipped a second fabrication in place of the first. Corrected below.
- Actual Zerops commands per [references/cli/commands.mdx](/Users/fxck/www/zerops-docs/apps/docs/content/references/cli/commands.mdx):
  - `zcli project project-import <path>` — creates a **new** project
  - `zcli project service-import <path>` — creates services **in an existing** project (additive only; does not modify existing services)
- Managed-service `mode` (NON_HA/HA) is **immutable** after creation per [internal/knowledge/guides/scaling.md](../internal/knowledge/guides/scaling.md). So tier 4→5 promotion (which includes DB/cache NON_HA→HA flip) **cannot** apply in-place on existing services — it requires a new project.
- Runtime sizing + `minContainers` can be modified in-place on existing services via `zerops_scale` MCP tool OR GUI, but the "tier promotion" framing bundles multiple changes (mode + sizing + env var set) into one — so in practice each tier is a separate Zerops project.

**Correct approach: don't claim a mechanism that doesn't exist. Remove the false data-persistence claim entirely; don't replace it with a different specific claim. State the factual truth: separate projects have separate state.**

**Edit 1** — `GenerateEnvREADME`, envIndex==0 "First-tier context" section. Replace line 172:

```go
// Before (line 172):
b.WriteString("- Data persists across tier promotions because service hostnames stay stable — you can iterate here and move up without rebuilding the database from scratch.\n\n")

// After:
b.WriteString("- Each tier's `import.yaml` declares a distinct `project.name`, so deploying a later-tier template creates a NEW Zerops project. Service state (DB rows, cache entries, stored files) does NOT carry across tiers by default — promote by first exporting data from this tier's project and importing into the next, or re-seed in the new project.\n\n")
```

**Edit 2** — `envPromotionPath(0)`. Replace line 305:

```go
// Before (line 305):
"- Existing data in DB / cache / storage persists across the tier bump because hostnames are identical.\n" +

// After (plan-independent — envPromotionPath has no *RecipePlan param, so
// the prose references the stable suffix convention from envSlugSuffix,
// not a specific framework slug. `-agent` / `-remote` suffixes are recipe-
// independent per envSlugSuffix at recipe_templates.go:414; the full
// project.name is `{plan.Slug}-{suffix}` — see TestGenerateEnvImportYAML_
// ProjectNameSuffixes for the invariant):
"- Deploying the `1 — Remote (CDE)/import.yaml` creates a NEW Zerops project — each tier's YAML declares a distinct `project.name` suffix (this tier's ends `-agent`, Remote's ends `-remote`). Service hostnames match by convention, but the containers are separate. If you need data carry-over, export from this tier's project before provisioning Remote; otherwise plan to re-seed.\n" +
```

**Also edit line 302 first bullet** — the preceding line has the same mental-model defect (*"into your Zerops project"* implies in-place) and must be fixed together or the replacement above reads as a contradiction:

```go
// Before (line 302):
"- Deploy the `1 — Remote (CDE)/import.yaml` into your Zerops project (or click the deploy button for that tier).\n" +

// After:
"- Deploy the `1 — Remote (CDE)/import.yaml` via the Zerops dashboard or the deploy button (this provisions a new project for the Remote tier; it does NOT modify your AI-Agent tier project).\n" +
```

**Audit and fix any sibling fabrications** — run:

```bash
grep -nE 'persists|data.*(across|survives|carries)|hostnames.*(identical|stable|match)' internal/workflow/recipe_templates.go
```

Any remaining "persists because hostnames identical" / "carries over" / similar claims in `envPromotionPath(1)`, `envPromotionPath(2)`, `envPromotionPath(3)`, `envPromotionPath(4)`, `envDiffFromPrevious(N)`, or `envOperationalConcerns(N)` get the same treatment: remove the false mechanism claim; if the tier-transition prose needs to discuss state, say plainly that tiers are separate projects with separate state.

**Regression test** (`internal/workflow/recipe_templates_test.go` extension):

```go
func TestGenerateEnvREADME_NoDataPersistenceFabrication(t *testing.T) {
    plan := testShowcasePlan() // actual helper name in recipe_templates_test.go (L37). Earlier drafts referenced a non-existent buildMinimalShowcasePlan.
    forbidden := []string{
        "data persists across tier promotions",
        "persists across the tier bump",
        "hostnames are identical",
        "hostnames stay stable",
        "persists because hostnames",
    }
    for i := 0; i < EnvTierCount(); i++ {
        readme := GenerateEnvREADME(plan, i)
        low := strings.ToLower(readme)
        for _, phrase := range forbidden {
            if strings.Contains(low, phrase) {
                t.Errorf("env %d README contains fabricated phrase %q — state persistence across different project.name values is false; separate projects have separate state", i, phrase)
            }
        }
    }
}

// TestEnvPromotionPath_NoFabricatedMechanism ensures no promotion-path
// prose invents a Zerops mechanism that does not exist. `override-import`
// is specifically called out because an earlier draft of v8.95 proposed
// citing it as the in-place-promotion mechanism — it does NOT exist in
// zcli. Only `zcli project project-import` (new project) and
// `zcli project service-import` (services into existing project) are real.
func TestEnvPromotionPath_NoFabricatedMechanism(t *testing.T) {
    for i := 0; i < EnvTierCount()-1; i++ {
        path := strings.ToLower(envPromotionPath(i))
        forbidden := []string{
            "override-import",
            "override_import",
            "project-override",
            "overrideimport",
        }
        for _, phrase := range forbidden {
            if strings.Contains(path, phrase) {
                t.Errorf("envPromotionPath(%d) cites %q, which is NOT a real Zerops mechanism. Actual zcli commands: project-import (new) / service-import (additive). See references/cli/commands.mdx.", i, phrase)
            }
        }
    }
}
```

---

### 5.3 Fix env 3/4/5 README minContainers factual drift

**Problem**: four hardcoded lines in `recipe_templates.go` make claims about minContainers that don't match the values `GenerateEnvImportYAML` produces for the same envIndex.

**Verification step (do this FIRST)** — the regression test below parses `GenerateEnvImportYAML(envIndex)` output against README claims. The `importYAMLDoc` struct is **package-private inside `internal/tools/`** ([workflow_checks_finalize.go:114](../internal/tools/workflow_checks_finalize.go#L114)) and cannot be imported from `internal/workflow/` (circular dependency: tools → workflow). Define an anonymous YAML-unmarshal shape INLINE in the test, or put the test under a new `internal/workflow/recipe_templates_cross_pkg_test.go` with an integration build tag if tests need to run in-tree:

```go
package workflow

import (
    "strings"
    "testing"
    "gopkg.in/yaml.v3"
)

// TestGenerateEnvImportYAML_MinContainers_ActualValues is exploratory —
// run it first to see what minContainers values each env actually declares.
// The assertions in TestGenerateEnvREADME_MinContainersMatchesYAML below
// depend on these ground-truth values.
func TestGenerateEnvImportYAML_MinContainers_ActualValues(t *testing.T) {
    plan := testShowcasePlan() // actual helper in recipe_templates_test.go:37 (also testMinimalPlan / testDualRuntimePlan for other shapes)
    for i := 0; i < EnvTierCount(); i++ {
        yamlContent := GenerateEnvImportYAML(plan, i)
        var shape struct {
            Services []struct {
                Hostname      string `yaml:"hostname"`
                Type          string `yaml:"type"`
                MinContainers *int   `yaml:"minContainers"`
                Mode          string `yaml:"mode"`
            } `yaml:"services"`
        }
        if err := yaml.Unmarshal([]byte(yamlContent), &shape); err != nil {
            t.Fatalf("env %d yaml: %v", i, err)
        }
        for _, svc := range shape.Services {
            mc := "nil(default=1)"
            if svc.MinContainers != nil {
                mc = fmt.Sprintf("%d", *svc.MinContainers)
            }
            t.Logf("env=%d service=%s type=%s minContainers=%s mode=%q",
                i, svc.Hostname, svc.Type, mc, svc.Mode)
        }
    }
}
```

This gives you ground truth: what DOES `GenerateEnvImportYAML` declare at each envIndex? Confirm the v29 observation that env 4 declares `minContainers: 2` on api/app/worker. Then edit the templates to match.

**Edits** (match to ground-truth findings; the values below assume v29's observed state: env 3 declares no explicit minContainers → default 1, env 4 declares 2, env 5 declares 2):

**Important note on zero-downtime semantics** (verified against [internal/knowledge/guides/deployment-lifecycle.md](../internal/knowledge/guides/deployment-lifecycle.md)):

- Rolling deploy is **default** (`temporaryShutdown: false`) — "new containers start BEFORE old ones stop" — applies at ANY `minContainers ≥ 1`.
- readinessCheck **gates traffic routing** to new containers; it does NOT "enable" zero-downtime. Without it, traffic may route to a new container before it's accepting requests → transient 502s.
- `minContainers: 2+` provides **redundancy** (another replica serving while one rolls), not zero-downtime per se. The v7-gold-standard recipe teaching "minContainers:2 for rolling deploys" is a best-practice recipe choice to reduce load-spike risk during rollover, not a Zerops-documented requirement.

The edits below keep the original "minContainers:2" framing (which matches the recipe's design choice) but remove the factually-wrong "flips to 2" transition claims and avoid over-claiming zero-downtime dependencies. **Verify each replacement string against the deployment-lifecycle guide before committing.**

**Edit 1** — `envDiffFromPrevious(4)` at line 279:

```go
// Before (line 279):
"- Runtime services stay at `minContainers: 1` but are sized for real traffic.\n" +

// After:
"- Runtime services run at `minContainers: 2` — the second replica provides redundancy during rolling deploys so a slow-starting new container doesn't starve traffic.\n" +
```

**Edit 2** — `envDiffFromPrevious(5)` at line 285:

```go
// Before (line 285):
"- Every runtime `minContainers` flips to `2` so rolling deploys survive without visible downtime.\n" +

// After:
"- `minContainers: 2` already set at Small Production; the HA-distinct changes are `mode: HA` on DB and cache (automatic failover on node failure) and tighter readiness-probe windows.\n" +
```

**Edit 3** — `envPromotionPath(3)` at line 324:

```go
// Before (line 324):
"- Runtime container stays at `minContainers: 1`.\n" +

// After:
"- Runtime services run at `minContainers: 2` — one replica keeps serving while the other rolls during a deploy; configure `readinessCheck` to gate traffic handoff if start-up is slow.\n" +
```

**Edit 4** — `envPromotionPath(4)` at line 330:

```go
// Before (line 330):
"- Runtime `minContainers` flips to `2` so rolling deploys survive zero-downtime.\n" +

// After:
"- Runtime services continue at `minContainers: 2`; the HA-distinct change is `mode: HA` on DB and cache. Note: `mode` is immutable after creation per Zerops' scaling rules, so the HA flip happens via a new project, not by modifying the existing Small-Production services in place.\n" +
```

**Edit 5** — `envOperationalConcerns(4)` at line 370:

```go
// Before (line 370):
"- Deploys incur downtime because `minContainers: 1` — the single runtime container is replaced during deploy.\n" +

// After:
"- Rolling deploys at `minContainers: 2` keep one replica serving while the other rolls, but traffic can still land on a not-yet-ready replica unless `readinessCheck` is configured on runtime services; add `deploy.readinessCheck` with an `httpGet` probe to gate traffic handoff.\n" +
```

**Edit 6** — `envAudience(4)` at lines 236-239. Three sibling bullets in the same case block make claims that contradict Edits 1, 4, and 5 above once applied. A porter reading env 4's README sees "minContainers: 2" in the Diff/Promotion/Ops sections and "Runtime runs one container" / "Expect brief downtime on every deploy" in the Audience section. Fix all three together or the content ships internally contradictory:

```go
// Before (lines 236-239, inside case 4 of envAudience):
"- Runtime runs one container (scaled vertically for cost); DB and cache are single-replica.\n" +
"- Daily backups are retained per the recipe's backup policy.\n" +
"- Expect brief downtime on every deploy — there is no HA replica to absorb traffic while the new container starts.\n" +
"- Best fit: budget-constrained production where occasional 30-60s downtime windows during deploy are acceptable."

// After:
"- Runtime services run at `minContainers: 2` (scaled vertically for cost); DB and cache are single-replica and run in `mode: NON_HA`.\n" +
"- Daily backups are retained per the recipe's backup policy.\n" +
"- Rolling deploys are graceful when `readinessCheck` is configured — one replica keeps serving while the other rolls. DB/cache remain NON_HA, so node-level failures incur downtime until the platform restarts the affected instance.\n" +
"- Best fit: budget-constrained production where occasional node-failure downtime is tolerable but routine deploys must not drop traffic."
```

**Edit 7** — `envPromotionPath(3)` final bullet (directly after the L324 edit above, at the bottom of the case 3 return block). The unedited last bullet *"Plan for brief deploy-time downtime windows."* contradicts Edit 3's new *"Runtime services run at `minContainers: 2` — one replica keeps serving"*:

```go
// Before (last bullet in case 3 of envPromotionPath):
"- Plan for brief deploy-time downtime windows."

// After:
"- Configure `deploy.readinessCheck` on runtime services before this promotion — the rolling-deploy behavior at `minContainers: 2` assumes it; without readiness gating, traffic can reach a not-yet-ready new replica during rollover."
```

**Regression test**:

```go
// extractMinContainersClaimsFromProse finds all `minContainers: N` patterns
// in prose, returning N values. Skips fenced code blocks. Regex anchors
// on the literal token + colon + optional backticks/whitespace + digit(s).
var minContainersClaimRe = regexp.MustCompile(`minContainers:\s*(\d+)`)

func extractMinContainersClaimsFromProse(s string) []int {
    // Strip fenced code blocks first (non-greedy between ``` pairs).
    fenced := regexp.MustCompile("(?s)```[^`]*```")
    stripped := fenced.ReplaceAllString(s, "")
    var claims []int
    for _, m := range minContainersClaimRe.FindAllStringSubmatch(stripped, -1) {
        n, err := strconv.Atoi(m[1])
        if err == nil {
            claims = append(claims, n)
        }
    }
    return claims
}

// TestGenerateEnvREADME_MinContainersMatchesYAML pins every minContainers:N
// claim in an env README to either env N's YAML OR env N+1's YAML (because
// GenerateEnvREADME(plan, N) concatenates envAudience(N) + envDiffFromPrevious(N)
// + envPromotionPath(N) + envOperationalConcerns(N), and envPromotionPath(N)
// legitimately describes the TARGET tier — env N+1). A claim not found in
// either declared set is a fabrication that drifted from YAML truth.
//
// An earlier single-env-scan draft of this test would FAIL on env 3 after
// §5.3 Edit 3 applied envPromotionPath(3)'s claim of "minContainers: 2"
// (describing env 4), because env 3 YAML declares 1 (default). The union
// approach catches the original defect class (minContainers claim drift
// from actual YAML value) without raising false positives on legitimate
// cross-tier promotion-path prose.
//
// IsRuntimeType is the actual helper name — see recipe_service_types.go:46.
// An earlier draft of this guide used isRuntimeServiceType, which doesn't exist.
func TestGenerateEnvREADME_MinContainersMatchesYAML(t *testing.T) {
    plan := testShowcasePlan()
    for i := 0; i < EnvTierCount(); i++ {
        readme := GenerateEnvREADME(plan, i)
        readmeClaims := extractMinContainersClaimsFromProse(readme)
        if len(readmeClaims) == 0 {
            continue
        }
        // Union env i and (if not last) env i+1 declared values — a non-last
        // env's README includes envPromotionPath(i) which describes the next
        // tier, so claims about either tier are legitimate.
        declared := declaredRuntimeMinContainers(t, plan, i)
        if i < EnvTierCount()-1 {
            for k := range declaredRuntimeMinContainers(t, plan, i+1) {
                declared[k] = true
            }
        }
        for _, claim := range readmeClaims {
            if !declared[claim] {
                var vals []int
                for k := range declared {
                    vals = append(vals, k)
                }
                t.Errorf("env %d README claims minContainers: %d but neither env %d nor env %d declares that value. Declared (union): %v",
                    i, claim, i, i+1, vals)
            }
        }
    }
}

// declaredRuntimeMinContainers returns the set of minContainers values
// declared on runtime services in env envIndex's generated import.yaml.
// Returns {1} when no runtime service declares an explicit minContainers
// (default per import.mdx). Shared helper for both the README test above
// and TestEnvPromotionPath_CrossEnvClaimsMatchTargetYAML below.
func declaredRuntimeMinContainers(t *testing.T, plan *RecipePlan, envIndex int) map[int]bool {
    t.Helper()
    yamlContent := GenerateEnvImportYAML(plan, envIndex)
    var shape struct {
        Services []struct {
            Type          string `yaml:"type"`
            MinContainers *int   `yaml:"minContainers"`
        } `yaml:"services"`
    }
    if err := yaml.Unmarshal([]byte(yamlContent), &shape); err != nil {
        t.Fatalf("env %d: yaml parse: %v", envIndex, err)
    }
    declared := map[int]bool{}
    for _, svc := range shape.Services {
        if IsRuntimeType(svc.Type) && svc.MinContainers != nil {
            declared[*svc.MinContainers] = true
        }
    }
    if len(declared) == 0 {
        declared[1] = true
    }
    return declared
}
```

**Tighter cross-env test** — the README-level union test above is permissive by design (claims can match env N or env N+1). The test below further pins claims that appear specifically within `envPromotionPath(N)` against env N+1's YAML, catching the case where a promotion-path claim drifts from its target tier:

```go
// TestEnvPromotionPath_CrossEnvClaimsMatchTargetYAML pins every
// minContainers:N claim appearing in envPromotionPath(i) against env i+1's
// declared runtime minContainers. Promotion-path headings explicitly name
// the target tier ("From Stage to Small Production"), so claims there must
// reflect the target's YAML. The README-level test above uses a union
// (env i OR env i+1) to avoid false failures when the two are mixed in
// GenerateEnvREADME output; this test is the tighter per-section pin.
func TestEnvPromotionPath_CrossEnvClaimsMatchTargetYAML(t *testing.T) {
    plan := testShowcasePlan()
    for i := 0; i < EnvTierCount()-1; i++ {
        path := envPromotionPath(i)
        claims := extractMinContainersClaimsFromProse(path)
        if len(claims) == 0 {
            continue
        }
        declared := declaredRuntimeMinContainers(t, plan, i+1)
        for _, claim := range claims {
            if !declared[claim] {
                var vals []int
                for k := range declared {
                    vals = append(vals, k)
                }
                t.Errorf("envPromotionPath(%d) claims minContainers: %d for target env %d, but env %d's yaml declares %v",
                    i, claim, i+1, i+1, vals)
            }
        }
    }
}
```

---

### 5.4 Writer content manifest + DISCARD enforcement

**Problem**: v29 writer subagent classified 2 of 14 facts as DISCARD-class (framework-quirk / library-meta) but shipped them as gotchas. No post-writer gate caught this.

**Architecture constraint identified by first simulation**: The per-run DISCARD list lives only in the main agent's `Agent()` dispatch prompt text (server never sees it) and the writer's free-form return message (only main agent receives). There's no existing structured artifact the server can read at check time.

**Solution**: writer emits `/var/www/ZCP_CONTENT_MANIFEST.json` before returning. Check reads it from disk. The manifest path is fixed, the JSON shape is fixed, the writer brief mandates both.

**Writer brief extension** — in `internal/content/workflows/recipe.md`, `content-authoring-brief` block. Insert this new subsection IMMEDIATELY AFTER the "Inputs you do NOT have" paragraph (at approximately recipe.md L2218) and BEFORE the "### The six content surfaces" heading. This placement keeps all "Inputs" and "Return" contract metadata together before the taxonomy + workflow sections. The section name referenced in an earlier draft ("Four key deliverables") does not exist — the actual section is "### Deliverables" further down at approximately L2345.

```markdown
### Return contract: content manifest (MANDATORY)

Before returning from this dispatch, Write a file at `/var/www/ZCP_CONTENT_MANIFEST.json` with this exact shape:

```json
{
  "version": 1,
  "facts": [
    {
      "fact_title": "<exact title from facts-log FactRecord.Title>",
      "classification": "invariant|intersection|framework-quirk|library-meta|scaffold-decision|operational|self-inflicted",
      "routed_to": "apidev-gotcha|apidev-ig|apidev-claude-md|apidev-zerops-yaml-comment|appdev-gotcha|appdev-ig|appdev-claude-md|appdev-zerops-yaml-comment|workerdev-gotcha|workerdev-ig|workerdev-claude-md|workerdev-zerops-yaml-comment|env-yaml-comment|discarded",
      "override_reason": ""
    }
  ]
}
```

- Every fact in `$ZCP_FACTS_LOG` gets exactly one entry. Missing entries fail the deploy-step check.
- Facts classified `framework-quirk`, `library-meta`, or `self-inflicted` should route to `discarded` by default. Routing them to any other surface requires a non-empty `override_reason` explaining why (e.g. "reframed from scaffold-internal bug to porter-facing symptom with concrete failure mode").
- Missing or malformed manifest fails the deploy-step check with `writer_did_not_emit_content_manifest`.
```

**Permitted-tools extension** in the content-authoring-brief block:

```markdown
**Permitted tools:**
- File ops: `Read`, `Edit`, `Write`, `Grep`, `Glob` against the SSHFS-mounted content paths named in this brief AND the single manifest path `/var/www/ZCP_CONTENT_MANIFEST.json`
- ... (existing list)
```

**File (new)**: `internal/tools/workflow_checks_content_manifest.go` (~180 lines).

**Firing point**: `checkRecipeDeployReadmes` at [internal/tools/workflow_checks_recipe.go:252](../internal/tools/workflow_checks_recipe.go#L252). Add one `checks = append(checks, checkWriterContentManifest(...)...)` call after the existing cross-README dedup check.

**Behavior**:

```go
// Sub-check A — manifest presence + parse
func checkWriterContentManifest(projectRoot string, readmesByHost map[string]string) []workflow.StepCheck {
    path := filepath.Join(projectRoot, "ZCP_CONTENT_MANIFEST.json")
    data, err := os.ReadFile(path)
    if err != nil {
        return []workflow.StepCheck{{
            Name: "writer_content_manifest_exists", Status: statusFail,
            Detail: fmt.Sprintf("content manifest missing at %s — the content-authoring subagent must Write ZCP_CONTENT_MANIFEST.json at the recipe root before returning (see recipe.md content-authoring-brief §'Return contract').", path),
        }}
    }
    var manifest contentManifest
    if err := json.Unmarshal(data, &manifest); err != nil {
        return []workflow.StepCheck{{
            Name: "writer_content_manifest_valid", Status: statusFail,
            Detail: fmt.Sprintf("content manifest invalid JSON at %s: %v", path, err),
        }}
    }

    var checks []workflow.StepCheck
    checks = append(checks, workflow.StepCheck{Name: "writer_content_manifest_exists", Status: statusPass})
    checks = append(checks, workflow.StepCheck{Name: "writer_content_manifest_valid", Status: statusPass})

    // Sub-check B — classification consistency
    checks = append(checks, checkManifestClassificationConsistency(manifest)...)
    // Sub-check C — manifest honesty (discarded facts shouldn't appear as gotchas)
    checks = append(checks, checkManifestHonesty(manifest, readmesByHost)...)
    // Sub-check D — manifest completeness: every distinct FactRecord.Title
    // in the facts log must appear as a manifest entry. Guards against the
    // deceptive-empty-manifest attack (writer emits {"facts":[]} to bypass
    // sub-checks B and C trivially).
    checks = append(checks, checkManifestCompleteness(manifest, factsLogPath)...)

    return checks
}

// Sub-check D — manifest completeness. `factsLogPath` resolves to
// ops.FactLogPath(sessionID); the caller plumbs it via the step-checker
// factory (see plumbing note below). If the facts log is unreadable
// (file missing, permission error), this sub-check PASSES — a real run
// always produces a facts log, and a missing file likely means the
// check is running in a synthetic test context.
func checkManifestCompleteness(m contentManifest, factsLogPath string) []workflow.StepCheck {
    if factsLogPath == "" {
        return []workflow.StepCheck{{Name: "writer_manifest_completeness", Status: statusPass,
            Detail: "facts-log path not plumbed; completeness check skipped (test context)"}}
    }
    facts, err := ops.ReadFacts(factsLogPath)
    if err != nil {
        return []workflow.StepCheck{{Name: "writer_manifest_completeness", Status: statusPass,
            Detail: fmt.Sprintf("facts log unreadable at %s (%v); completeness check skipped", factsLogPath, err)}}
    }
    if len(facts) == 0 {
        // No facts recorded. An empty manifest is consistent here.
        return []workflow.StepCheck{{Name: "writer_manifest_completeness", Status: statusPass}}
    }
    // Titles are deduplicated — the same fact can be recorded multiple times
    // across substeps during iteration; each distinct title must have ONE
    // manifest entry (the writer collapses duplicates by title).
    logTitles := make(map[string]bool, len(facts))
    for _, f := range facts {
        if t := strings.TrimSpace(f.Title); t != "" {
            logTitles[t] = true
        }
    }
    manifestTitles := make(map[string]bool, len(m.Facts))
    for _, entry := range m.Facts {
        if t := strings.TrimSpace(entry.FactTitle); t != "" {
            manifestTitles[t] = true
        }
    }
    var missing []string
    for title := range logTitles {
        if !manifestTitles[title] {
            missing = append(missing, title)
        }
    }
    if len(missing) == 0 {
        return []workflow.StepCheck{{Name: "writer_manifest_completeness", Status: statusPass}}
    }
    return []workflow.StepCheck{{
        Name:   "writer_manifest_completeness",
        Status: statusFail,
        Detail: fmt.Sprintf("manifest missing entries for %d distinct FactRecord.Title values that appear in the facts log: %s. Every recorded fact must have exactly one manifest entry with classification + routed_to. An under-populated manifest bypasses sub-checks B and C.", len(missing), strings.Join(missing, "; ")),
    }}
}
```

**Facts-log path plumbing** — the step-checker closure at [internal/tools/workflow_checks_recipe.go:252](../internal/tools/workflow_checks_recipe.go#L252) currently receives `(_ context.Context, plan *workflow.RecipePlan, _ *workflow.RecipeState)`. `RecipeState` has no `SessionID` field (verified at [internal/workflow/recipe.go:23-34](../internal/workflow/recipe.go#L23)); only the outer `WorkflowState` does (`state.go:8`). Extend the step-checker factory signature to accept a resolver:

```go
// Before:
func checkRecipeDeployReadmes(stateDir string, kp knowledge.Provider) workflow.RecipeStepChecker

// After (v8.95):
func checkRecipeDeployReadmes(stateDir string, kp knowledge.Provider, factsLogPathFn func() string) workflow.RecipeStepChecker
```

The engine wire-up (in `buildRecipeStepChecker` or equivalent) passes `func() string { return ops.FactLogPath(session.SessionID) }`. Nil resolver → `checkWriterContentManifest` treats `factsLogPath == ""` as test context and passes sub-check D with a skip note (see implementation above).

func checkManifestClassificationConsistency(m contentManifest) []workflow.StepCheck {
    discardClasses := map[string]bool{
        "framework-quirk": true, "library-meta": true, "self-inflicted": true,
    }
    var failures []string
    for _, entry := range m.Facts {
        if !discardClasses[entry.Classification] {
            continue
        }
        if entry.RoutedTo == "discarded" {
            continue
        }
        if strings.TrimSpace(entry.OverrideReason) != "" {
            continue
        }
        failures = append(failures, fmt.Sprintf(
            "fact %q classified %s but routed to %s without override_reason",
            entry.FactTitle, entry.Classification, entry.RoutedTo,
        ))
    }
    if len(failures) == 0 {
        return []workflow.StepCheck{{Name: "writer_discard_classification_consistency", Status: statusPass}}
    }
    return []workflow.StepCheck{{
        Name:   "writer_discard_classification_consistency",
        Status: statusFail,
        Detail: "manifest inconsistencies: " + strings.Join(failures, "; ") + ". Either route these facts to 'discarded' OR supply a non-empty override_reason explaining why the default classification doesn't apply (e.g. 'reframed to porter-facing symptom').",
    }}
}

func checkManifestHonesty(m contentManifest, readmesByHost map[string]string) []workflow.StepCheck {
    // For each fact where routed_to == "discarded", verify no published gotcha has a title
    // that Jaccard-matches the fact_title at >= 0.3 (stop-word-stripped).
    var failures []string
    for _, entry := range m.Facts {
        if entry.RoutedTo != "discarded" {
            continue
        }
        for host, readme := range readmesByHost {
            stems := extractGotchaStems(readme)
            for _, stem := range stems {
                sim := jaccardSimilarityNoStopwords(entry.FactTitle, stem)
                if sim >= 0.3 {
                    failures = append(failures, fmt.Sprintf(
                        "fact %q marked discarded but %s/README.md ships gotcha %q (Jaccard=%.2f)",
                        entry.FactTitle, host, stem, sim,
                    ))
                }
            }
        }
    }
    if len(failures) == 0 {
        return []workflow.StepCheck{{Name: "writer_manifest_honesty", Status: statusPass}}
    }
    return []workflow.StepCheck{{
        Name:   "writer_manifest_honesty",
        Status: statusFail,
        Detail: "manifest says discarded but matching gotcha shipped: " + strings.Join(failures, "; ") + ". Either remove the gotcha or update manifest entry with the correct routed_to + override_reason.",
    }}
}
```

**Helper functions** (also in the new file):

```go
type contentManifest struct {
    Version int                   `json:"version"`
    Facts   []contentManifestFact `json:"facts"`
}
type contentManifestFact struct {
    FactTitle      string `json:"fact_title"`
    Classification string `json:"classification"`
    RoutedTo       string `json:"routed_to"`
    OverrideReason string `json:"override_reason"`
}

var stopWords = map[string]bool{
    "a": true, "an": true, "the": true, "is": true, "are": true, "was": true,
    "must": true, "may": true, "can": true, "should": true, "for": true, "of": true,
    "in": true, "on": true, "at": true, "to": true, "from": true,
    "have": true, "has": true, "had": true, "be": true, "been": true, "if": true, "when": true,
    "not": true, "no": true, "with": true, "by": true, "as": true,
}

func jaccardSimilarityNoStopwords(a, b string) float64 {
    ta := tokenize(a)
    tb := tokenize(b)
    if len(ta) == 0 || len(tb) == 0 {
        return 0
    }
    setA := make(map[string]bool, len(ta))
    for _, t := range ta { setA[t] = true }
    setB := make(map[string]bool, len(tb))
    for _, t := range tb { setB[t] = true }
    intersect := 0
    for t := range setA {
        if setB[t] { intersect++ }
    }
    union := len(setA) + len(setB) - intersect
    if union == 0 { return 0 }
    return float64(intersect) / float64(union)
}

func tokenize(s string) []string {
    // lowercase, split on any non-alphanumeric, drop stop-words and empty tokens
    var out []string
    var b strings.Builder
    flush := func() {
        if b.Len() == 0 { return }
        t := strings.ToLower(b.String())
        b.Reset()
        if stopWords[t] { return }
        out = append(out, t)
    }
    for _, r := range s {
        if unicode.IsLetter(r) || unicode.IsDigit(r) {
            b.WriteRune(r)
        } else {
            flush()
        }
    }
    flush()
    return out
}

func extractGotchaStems(readme string) []string {
    // Gotcha stems live inside the knowledge-base fragment as "- **<stem>** — ..." bullets.
    // Extract bold text (content between first ** pair) from each bullet.
    var stems []string
    inKB := false
    for _, line := range strings.Split(readme, "\n") {
        if strings.Contains(line, "ZEROPS_EXTRACT_START:knowledge-base") {
            inKB = true; continue
        }
        if strings.Contains(line, "ZEROPS_EXTRACT_END:knowledge-base") {
            inKB = false; continue
        }
        if !inKB || !strings.HasPrefix(strings.TrimSpace(line), "- **") {
            continue
        }
        // Extract between first ** and second **
        idx1 := strings.Index(line, "**")
        if idx1 < 0 { continue }
        idx2 := strings.Index(line[idx1+2:], "**")
        if idx2 < 0 { continue }
        stem := line[idx1+2 : idx1+2+idx2]
        if stem != "" { stems = append(stems, stem) }
    }
    return stems
}
```

**Jaccard threshold justification** (from second simulation):

- v29 healthCheck-bare-GET case: fact title "plan.healthCheck path must have a GET handler — feature sweep rejects 4xx" vs gotcha stem "Feature-sweep rejects the recipe if the plan.healthCheck path lacks a bare GET". Stop-word-stripped tokens shared: {plan, healthcheck, path, get, feature, sweep, rejects} = 7. Union: ~12. Jaccard = 0.58. ✓ Catches at 0.3 threshold.
- v29 Multer FormData case: fact title "api.ts Content-Type must NOT be injected for FormData bodies" vs gotcha stem "Multer rejects uploads with `400 Unexpected end of form` when the browser sent a valid multipart body". Stop-word-stripped tokens shared: {} = 0. Jaccard = 0. ✗ MISSED at any threshold.

The Multer case is a SEMANTIC reframing where writer explicitly turned the scaffold-internal bug into a porter-facing symptom with a different vocabulary. Jaccard doesn't catch this class; requires mechanism-level comparison.

For v8.95: **accept the Multer-class miss**. The primary enforcement path is §5.4 Sub-check B (classification-consistency) — if writer correctly marks Multer as framework-quirk and ships to appdev-gotcha, override_reason must be present. That catches 2/2 v29 cases. Sub-check C (manifest-honesty via Jaccard) catches deceptive-manifest cases where writer lies about routing — useful but not the primary gate.

**Tests** — `internal/tools/workflow_checks_content_manifest_test.go` (~220 lines). Each sub-check gets explicit coverage for pass + fail paths, plus v29 regression fixtures:

```go
// Sub-check A — presence + parse
func TestContentManifest_MissingFile_Fails(t *testing.T) { ... }       // empty projectRoot → fail with "content manifest missing"
func TestContentManifest_MalformedJSON_Fails(t *testing.T) { ... }     // "{not valid json" → fail with parse error detail
func TestContentManifest_ValidMinimal_Passes(t *testing.T) { ... }     // {"version":1,"facts":[]} parses; downstream sub-checks run

// Sub-check B — classification consistency
func TestContentManifest_DiscardClassRoutedToGotcha_FailsWithoutReason(t *testing.T) { ... }  // framework-quirk → apidev-gotcha, empty reason → fail
func TestContentManifest_DiscardClassRoutedToGotcha_PassesWithReason(t *testing.T) { ... }   // same + non-empty override_reason → pass
func TestContentManifest_DiscardClassRoutedToDiscarded_Passes(t *testing.T) { ... }          // framework-quirk → discarded → pass
func TestContentManifest_NonDiscardClass_NoEnforcement(t *testing.T) { ... }                 // invariant → apidev-gotcha → pass (sub-check B doesn't fire)

// Sub-check C — manifest honesty (Jaccard gotcha-stem match)
func TestContentManifest_Honesty_DiscardedButGotchaShipped_Fails(t *testing.T) { ... }       // fact marked discarded, gotcha with similar stem in readmesByHost → fail
func TestContentManifest_Honesty_DiscardedAndNoMatch_Passes(t *testing.T) { ... }            // fact marked discarded, no matching gotcha → pass
func TestContentManifest_Honesty_JaccardThreshold_v29HealthCheck(t *testing.T) { ... }       // v29 healthCheck-bare-GET fixture: Jaccard=0.58, should fail at threshold 0.3

// Sub-check D — manifest completeness (facts log cross-check)
func TestContentManifest_Completeness_AllFactsPresent_Passes(t *testing.T) { ... }           // 14 facts in log, 14 manifest entries → pass
func TestContentManifest_Completeness_EmptyManifestNonEmptyLog_Fails(t *testing.T) { ... }   // 14 facts in log, 0 manifest entries → fail (the deceptive-empty attack)
func TestContentManifest_Completeness_PartialManifest_Fails(t *testing.T) { ... }            // 14 facts in log, 10 manifest entries → fail with missing titles in detail
func TestContentManifest_Completeness_FactsLogMissing_SkipsGracefully(t *testing.T) { ... }  // factsLogPath points to nonexistent file → pass with skip note
func TestContentManifest_Completeness_EmptyLog_Passes(t *testing.T) { ... }                  // facts log exists but is empty → pass
```

---

## 6. Test plan

See §5.1 tests, §5.2 tests, §5.3 tests, §5.4 tests above. All RED-first.

**Integration test** (`integration/recipe_flow_test.go` extension): a full-workflow test that:
1. Runs through research → provision → generate
2. At generate-complete, asserts `{hostname}_scaffold_artifact_leak` passes for a clean scaffold
3. Plants `apidev/scripts/preship.sh`, retries, asserts check fails with the exact v29-class detail message
4. Removes the file + recommits, retries, asserts pass

**Shadow test against v29's published tree**:

```bash
# §5.1 scaffold_artifact_leak should fire on apidev/scripts/preship.sh
go test ./internal/tools/ -run TestScaffoldArtifactLeak_v29_PreshipLeak -v

# §5.2 template edits — regression tests pin the corrected phrasing
go test ./internal/workflow/ -run 'TestGenerateEnvREADME_NoDataPersistenceFabrication|TestEnvPromotionPath_NoFabricatedMechanism' -v

# §5.3 template edits — minContainers claims match YAML
go test ./internal/workflow/ -run 'TestGenerateEnvREADME_MinContainersMatchesYAML|TestEnvPromotionPath_CrossEnvClaimsMatchTargetYAML' -v

# §5.4 content manifest enforcement
go test ./internal/tools/ -run 'TestContentManifest' -v
```

---

## 7. File budget

Per CLAUDE.md's "max 350 lines per .go file". `workflow_checks_finalize.go` (691 lines) and `workflow_checks_recipe.go` (1,095 lines) are already past the limit — do NOT extend either body-wise; glue additions only.

| File | New / Modified | Purpose | Est. lines |
|---|---|---|---:|
| `internal/tools/workflow_checks_scaffold_artifact.go` | NEW | §5.1 | ~100 |
| `internal/tools/workflow_checks_scaffold_artifact_test.go` | NEW | §5.1 tests | ~150 |
| `internal/tools/workflow_checks_content_manifest.go` | NEW | §5.4 | ~180 |
| `internal/tools/workflow_checks_content_manifest_test.go` | NEW | §5.4 tests | ~200 |
| `internal/workflow/recipe_templates.go` | MODIFIED | §5.2 + §5.3 in-place edits (lines 172, 236-239, 279, 285, 302, 305, 324, 326-last, 330, 370 — 10 edits total after G1+G2 additions) | +~6 net (prose replacements roughly in-place; envAudience(4) + envPromotionPath(3) last-bullet rewrites slightly longer than originals) |
| `internal/workflow/recipe_templates_test.go` | MODIFIED | §5.2 + §5.3 regression tests + `declaredRuntimeMinContainers` helper | +~150 |
| `internal/tools/workflow_checks_recipe.go` | MODIFIED | Wire §5.1 into `checkRecipeGenerateCodebase`; wire §5.4 into `checkRecipeDeployReadmes` **with new `factsLogPathFn func() string` parameter**; engine wire-up passes `func() string { return ops.FactLogPath(session.SessionID) }` | +~15 |
| `internal/content/workflows/recipe.md` | MODIFIED | §5.4 writer-brief Return-contract subsection (placed after "Inputs you do NOT have"); §5.1 scaffold-brief edits (replace L987 "save to a temp script" line + append new "Committed-artifact prohibition (v8.95)" subsection) | +~100 |

All NEW files stay under 350 lines. Existing `_finalize.go` is NOT modified (first simulation's initial §5.5/§5.6 plan — which WOULD have extended it — has been removed, the env-README fix moved to Go-template edits in §5.2 + §5.3).

---

## 8. Rollout

### Git branch

Create branch `v8.95-content-surface-parity` off current `main`.

### Commit sequence (TDD, one commit per fix)

1. `test(workflow): recipe_templates_test.go RED` — adds §5.2 + §5.3 regression tests (`TestGenerateEnvREADME_NoDataPersistenceFabrication`, `TestEnvPromotionPath_NoFabricatedMechanism`, `TestGenerateEnvREADME_MinContainersMatchesYAML` with union semantics, `TestEnvPromotionPath_CrossEnvClaimsMatchTargetYAML`, plus helper `declaredRuntimeMinContainers`); all fail against current template text.
2. `fix(workflow): recipe_templates.go — correct env README fabrications + minContainers drift` — TEN in-place edits: L172 (data-persistence fabrication), L236-239 (envAudience(4) sibling-prose contradictions, Edit 6), L279 (envDiffFromPrevious(4)), L285 (envDiffFromPrevious(5)), L302 (envPromotionPath(0) first bullet, G2), L305 (envPromotionPath(0) data-persistence), L324 (envPromotionPath(3)), L326-last (envPromotionPath(3) sibling downtime bullet, Edit 7), L330 (envPromotionPath(4)), L370 (envOperationalConcerns(4)); tests from commit 1 go GREEN.
3. `test(tools): workflow_checks_scaffold_artifact_test.go RED` — six named test cases per §5.1.
4. `feat(tools): workflow_checks_scaffold_artifact.go GREEN` + wire into `checkRecipeGenerateCodebase` (pass `doc` + `rawYAML`; remediation detail includes inline git identity override per U3).
5. `test(tools): workflow_checks_content_manifest_test.go RED` — 15 named test cases covering sub-checks A/B/C/D per §5.4 test plan.
6. `feat(tools): workflow_checks_content_manifest.go GREEN` + wire into `checkRecipeDeployReadmes`. **Signature change**: `checkRecipeDeployReadmes(stateDir, kp, factsLogPathFn func() string)` — engine wire-up passes `func() string { return ops.FactLogPath(session.SessionID) }`. Sub-check D (completeness) requires this plumbing; sub-checks A/B/C do not.
7. `feat(recipe.md): v8.95 brief extensions` — TWO scaffold-brief edits (edit existing L987 "save to a temp script" instruction per B3; append new `### Committed-artifact prohibition (v8.95)` subsection before L995 "Reporting back"); writer-brief `### Return contract: content manifest (MANDATORY)` subsection inserted per U1 placement; permitted-tools extension for manifest path.
8. `test(integration): scaffold-artifact + content-manifest integration cases` — full-flow fixtures for plant/retry/remove cycle plus facts-log-present content-manifest flow.

### Pre-release verification

- `make lint-local` clean
- `go test ./... -count=1 -race` passes
- Shadow test against v29's published tree: every new check fires on the correct defect, passes on v25/v28

### v30 run calibration

Execute a fresh nestjs-showcase recipe run against v8.95. Bars (per v29 log entry's v30 calibration section):

- 0 scaffold-phase artifacts in published tree (`find published/ -name 'preship.sh' | wc -l == 0`)
- env 0 README does NOT contain "data persists across tier" + "hostnames stable" causation (shadow-grep confirms the Go templates were actually edited and in-binary)
- env 3/4/5 README minContainers claims match their target env's YAML values (shadow-check published tree)
- `ZCP_CONTENT_MANIFEST.json` present at recipe root; every fact in facts-log has exactly one entry
- 0 writer DISCARD overrides without `override_reason`
- Gotcha-origin ratio ≥ 75% genuine (sustain v29's 79%)
- All v8.90+v8.93+v8.94 calibration items held

---

## 9. What v8.95 explicitly does NOT solve

1. **Env-README authorship architecture**. Current design: Go templates deterministic per envIndex, writer output orphaned. Alternative (writer owns env READMEs via `OverlayRealREADMEs` extension): recipe-specific content, plan-parametric claims, but requires resolving folder-name mismatch + new overlay code. v8.96 decision. v8.95 accepts the static-template approach with templates fixed in-place.
2. **Multer-FormData-class DISCARD-override semantic reframing detection**. Jaccard fails on this case; would require mechanism/failureMode-level comparison. Sub-check B (classification-consistency with override_reason) catches it when writer is honest; no catch when writer is deceptive. v8.96 concern only if v30 evidence shows the deceptive-writer case recurring.
3. **Framework-quirk-with-recurrence-risk taxonomy route**. v29's `cache.tokens.ts` DI-symbol-extraction fix recurs every run, gets discarded every run, re-derived every run. Needs cross-run facts comparison or a new fact-type. v8.96+.
4. **env 4 + env 5 YAML app-static comment internal contradiction** (*"minContainers:2 on a static service is not needed"* + declared `minContainers: 2`). `factual_claims` regex sees 2 on both sides and passes. Semantic contradiction detection. Editorial fix; add detection only if v30 reproduces this phrasing.
5. **Conceptual-contradiction detection in env import.yaml comments generally**. v25/v29 have documented instances; calibration explicitly says "editorial fix, not new check." v8.95 preserves that rule.
6. **Writer override rate floor > 0**. v8.95's DISCARD enforcement allows override with reason. If v30 shows writers abusing the reason field (documenting nothing substantive), tighten further.

---

## 10. Exit criteria

v8.95 ships when:

- [ ] All new .go test files pass with `-race`
- [ ] Regression tests in `recipe_templates_test.go` pass (assertions on §5.2 + §5.3 fixes)
- [ ] Integration tests pass
- [ ] `make lint-local` clean
- [ ] Recipe.md brief extensions reviewed (read by fresh eye)
- [ ] Shadow test against v29's published tree: each new check identifies its target defect
- [ ] Documentation commit updates `docs/recipe-version-log.md` §v30 calibration bar with the specific `find` / `grep` commands verifying each bar
- [ ] Branch merged to main
- [ ] v30 recipe run executed; post-run audit confirms calibration bars

---

## 11. Reading order for implementation

1. This doc §§0-5 — understand the target AND the architectural misattribution avoided in §0
2. `internal/workflow/recipe_templates.go` — read the `switch envIndex` arms in `envDiffFromPrevious`, `envPromotionPath`, `envOperationalConcerns` top-to-bottom; these ARE the env READMEs
3. `docs/recipe-version-log.md` §v29 — the concrete defects
4. `internal/tools/workflow_checks_factual_claims.go` — existing `checkFactualClaims` is the mental-model template for the new checks' shape (pass/fail + detail strings)
5. `internal/tools/workflow_checks_recipe.go:74-151` (checkRecipeGenerate) and `:252-310` (checkRecipeDeployReadmes) — the two wire-up points for §5.1 and §5.4
6. Start coding in TDD order per §8 commit sequence

---

*v8.95 is the narrow follow-up to v8.94. The second simulation's load-bearing finding: env-README defects originate in Go templates, not writer behavior. The plan was fully rewritten around that reality. Three fixes: scaffold-artifact leak at generate, env-README Go-template corrections with regression tests, writer content-manifest for DISCARD enforcement on gotcha surfaces. No new infrastructure, no new subagents; one new data contract (`ZCP_CONTENT_MANIFEST.json`) and one new hygiene check.*

*A fourth dry-run pass (post-guide-draft verification) caught nine additional defects the first three simulations missed: (1) the regression test's single-env scan would fail on env 3 after the promotion-path edit because `envPromotionPath(3)` legitimately describes env 4 — fixed via union-of-N-and-N+1 semantics + tighter per-section cross-env test; (2) proposed replacement text for `envPromotionPath(0)` hardcoded the `nestjs-showcase` slug in a plan-independent function — fixed to use the stable `-agent`/`-remote` suffix convention; (3) the existing scaffold brief's "save to a temp script and invoke it" instruction was the exact ambiguity v29's agent exploited — now edited directly rather than merely appended around; (4) post-edit env 4 README would mix "Runtime runs one container" (envAudience) with "minContainers: 2" (envDiffFromPrevious) — fixed by editing envAudience(4) in the same commit; (5) same class: `envPromotionPath(3)` last bullet "Plan for brief deploy-time downtime windows" contradicts the L324 edit — fixed; (6) deceptive-empty-manifest attack (writer emits `{"facts":[]}`) bypassed sub-checks B/C trivially — added sub-check D (manifest completeness vs facts-log) with explicit factsLogPathFn plumbing through the step-checker factory; (7) plan's test code referenced `buildMinimalShowcasePlan()` which doesn't exist — renamed to the actual `testShowcasePlan()` helper; (8) writer-brief insertion point named a non-existent section ("Four key deliverables") — clarified to point after "Inputs you do NOT have" before "### The six content surfaces"; (9) remediation message assumed git identity configured on the container — added inline `-c user.email=... -c user.name=...` overrides so the amend succeeds even on a bare `git init` state. Expected v30 outcome: B+ overall with gotcha-origin ≥75% sustained and 0 factual drift in env READMEs.*
