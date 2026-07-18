# Spec-vs-reality audit — full findings (2026-06-16)

20-unit parallel audit + adversarial verify + Codex opponent pass. 129 findings; 1 spec clean (spec-authoring-boundary.md).

**Split after Codex:** ~123 mechanical truth-telling · 4 genuine decisions (2 structural whole-spec + env-local pair).

## OUTCOME — SHIPPED (uncommitted working tree)

Karel's decisions: rubric→ARCHIVE · spike→RECONCILE-whole-to-code · env-local→DEMOTE-to-design-intent · mechanical→apply-all-spec-by-spec.

- **Archived** `docs/spec-content-quality-rubric.md` → `docs/archive/` (no live pointer; verified).
- **Reconciled** the spike's "Locked contracts" block to shipped `project_admin.go` + composer (interface, defaults, e2e names, A.7 corePackage). Kept §A/§B Phase-A observations (code still cites them). CLAUDE.md does NOT reference the spike — no pointer change needed (the decision-item claim it did was wrong; grep-verified).
- **Demoted** `.env.local` creation/seeding + git-tracking warning to DESIGN-INTENT in spec-env-handling; fixed the dangling `EnsureEnvLocal`/3-test refs. Also demoted the two atoms that still claimed the live behavior (`develop-local-env-channels`, `develop-local-env-troubleshoot`) so the agent-facing tell matches.
- **13-spec fan-out** applied 123 mechanical edits (101 applied + 15 adjusted-more-accurate + 0 skipped), each agent re-verifying current code shape, section-level rewrites where a section self-contradicted.
- **Follow-up code/atom/residual fixes** (agents are doc-only): `build_plan.go` recipe-active comment, `plan.go` `mode:`→type-variant comments, `session.go` dead `BuildIterationDelta` comment, work-session §6.3 (close = delete+terse), local-dev §12/§4, workflows D2c (`MarkServiceDeployed`→`RecordDeployAttempt`), scenarios `BuildIterationDelta`×2, four `30 scenarios` test comments → count-free.

**Verification:** `go build` OK · `go test ./... -short` green · `make lint-local` 0 issues · final dangling sweep clean (no live claim on a non-existent identifier remains).

**Still backlogged (not this pass):** `plans/backlog/close-mode-gitpush-agent-surface-sweep.md` — the agent-facing handler surface (`workflow_close_mode.go` hints) still advertises `git-push` close-mode for wire-compat; scenarios §6.11/§6 walkthroughs faithfully MIRROR that shipped handler, so they move together with the handler change, not here.

## Codex verdict (opponent)
- **A. spec-content-quality-rubric.md → ARCHIVE** (dead; derived_rules.md is sole substrate; tests pin embedded_rubric.md gone).
- **B. spec-launch-production-platform-spike.md → SPLIT** (keep Phase-A observations as history; delete/reconcile the stale 'Locked contracts' block). CLAUDE.md key-home pointer must update.
- **env-local-creation + git-tracking-detection → REAL product decision** (build vs demote-to-design; a 'mechanical' edit would silently erase intended work).
- Everything else → mechanical. Systemic: fix same-file internal contradictions by SECTION-level rewrite, not line patches.


## docs/spec-workflows.md (13 findings)

### dependency-mode-ha-nonha-model-stale [medium/confirmed/DECISION]
- **@** §2.3 Step 1: Discover — Plan structure, lines 444-452 (behavior-model)
- **spec:** Dependencies: [{
      Hostname    string
      Type        string
      Mode        string  // "HA" | "NON_HA" (defaults to NON_HA for managed)
      Resolution  string  // "CREATE" | "EXISTS" | "SHARED"
    }]
- **real:** HA is now specified as a TYPE VARIANT, not a `mode:` field. When the dependency Type already encodes a deployment variant (e.g. `postgresql:ha@16` / `postgresql:single@16`), resolveManagedDepMode DROPS the redundant sibling mode entirely ('variant authoritative'). The bare `mode:` form survives only
- **fix:** Update §2.3 to note that managed-service HA is expressed as a type variant (e.g. `postgresql:ha@16`); the `mode:` field is legacy backward-compat only and is dropped when the type carries a variant. Cross-reference the variant mechanism rat

### closemode-axis-still-lists-git-push [medium/confirmed/mech]
- **@** §1.5 Atom Corpus axes table, line 238 (removed-feature)
- **spec:** | `closeDeployModes` | `unset`, `auto`, `git-push`, `manual` | Empty = any close-mode. |
- **real:** The atom `closeDeployModes` axis enum has only three values: unset, auto, manual. `git-push` was removed. internal/workflow/atom.go validAtomEnumValues["closeDeployModes"] = {unset, auto, manual}. No atom frontmatter `closeDeployModes:` line contains git-push; it survives only as a legacy CloseDeplo
- **fix:** Change the closeDeployModes axis value list to `unset`, `auto`, `manual` and drop `git-push` (it is no longer a valid axis value; it folds to auto).

### plan-dispatch-recipe-active-branch-nonexistent [medium/confirmed/mech]
- **@** §1.4 Plan dispatch list, line 197 (and build_plan.go branch comment claim) (dangling-ref)
- **spec:** 6. `PhaseRecipeActive` → Primary=continue-recipe.
- **real:** There is no `PhaseRecipeActive` Phase constant and BuildPlan has no recipe-active case. The Phase enum is {idle, bootstrap-active, develop-active, develop-closed-auto, strategy-setup, export-active, launch-production-active}. `recipe-active` exists ONLY as an atom phases-axis value (atom.go), not as
- **fix:** Remove dispatch branch 6 (`PhaseRecipeActive → continue-recipe`) from §1.4 and renumber. Bootstrap recipe-route progression runs through PhaseBootstrapActive (branch 5), not a separate recipe phase. Also fix the build_plan.go doc-comment th

### phase-enum-not-exhaustive-missing-launch-production [medium/confirmed/mech]
- **@** §1.2 Phase Enum table, lines 44-55 (removed-feature)
- **spec:** The enum is exhaustive — every tool response resolves to exactly one phase: ... (six rows: idle, bootstrap-active, develop-active, develop-closed-auto, strategy-setup, export-active)
- **real:** The Phase enum has SEVEN values; PhaseLaunchProductionActive ("launch-production-active") is missing from the table. BuildPlan switches on it (returns empty Plan like export). The atom phases-axis enum carries both `launch-production-active` and `recipe-active` beyond the six listed. Calling the six
- **fix:** Add a `launch-production-active` row to the §1.2 table (set by zerops_workflow workflow=launch-production; stateless-ish, handler emits its own per-status guidance, BuildPlan returns empty Plan). Either drop the word 'exhaustive' or note th

### modes-axis-table-missing-values [medium/confirmed/mech]
- **@** §1.5 Atom Corpus axes table, line 237 (removed-feature)
- **spec:** | `modes` | `dev`, `stage`, `simple` | Empty = any mode. |
- **real:** The valid atom `modes` enum has six values: dev, stage, simple, standard, local-stage, local-only. The spec lists only three (missing standard, local-stage, local-only). The example atom on the same page (line 217) even uses `modes: [dev, standard]`, so `standard` is demonstrably a valid value the t
- **fix:** Set the modes axis value list to `dev`, `stage`, `simple`, `standard`, `local-stage`, `local-only`.

### servicemeta-field-comment-lists-git-push [low/confirmed/mech]
- **@** §1.1 ServiceMeta struct block, line 88 (removed-feature)
- **spec:** CloseDeployMode          CloseDeployMode  // unset | auto | git-push | manual (how develop close runs)
- **real:** CloseDeployMode's live value space is {unset, auto, manual}. `git-push` is a legacy-only value folded to auto at every read/write (foldLegacyCloseMode). The same §1.1 prose (line 104) and §1.2/§4.3 establish close-mode owns done-ness ownership {auto, manual, unset} with delivery DERIVED from GitPush
- **fix:** Change the ServiceMeta field comment to `// unset | auto | manual (legacy git-push folds to auto)`.

### mermaid-note-lists-git-push-closemode [low/confirmed/mech]
- **@** §1.1 ServiceMeta state-diagram `note right of CloseModeSet`, line 129 (removed-feature)
- **spec:** Close-mode = auto | git-push | manual.
- **real:** Close-mode value space is {auto, manual} (plus unset before decision). git-push is not a settable/persisted close-mode — the handler folds it to auto.
- **fix:** Change the mermaid note to `Close-mode = auto | manual.` (the legacy git-push input folds to auto).

### phases-axis-table-missing-values [low/confirmed/mech]
- **@** §1.5 Atom Corpus axes table, line 235 (removed-feature)
- **spec:** | `phases` | `idle`, `bootstrap-active`, `develop-active`, `develop-closed-auto`, `strategy-setup`, `export-active` | MUST be non-empty. |
- **real:** The valid atom `phases` enum has eight values: the six listed PLUS `recipe-active` and `launch-production-active`.
- **fix:** Add `recipe-active` and `launch-production-active` to the phases axis value list.

### routes-axis-table-missing-resume [low/confirmed/mech]
- **@** §1.5 Atom Corpus axes table, line 242 (removed-feature)
- **spec:** | `routes` | `recipe`, `classic`, `adopt` | Bootstrap-only. Empty = any route. |
- **real:** The valid atom `routes` enum has four values: recipe, classic, adopt, resume. `resume` is missing from the spec list.
- **fix:** Add `resume` to the routes axis value list.

### atom-count-stale [low/confirmed/mech]
- **@** §1.5 Atom Corpus, line 207 (stale-example)
- **spec:** Runtime-dependent guidance lives as ~74 atoms under `internal/content/atoms/*.md`, embedded via `//go:embed`.
- **real:** There are 113 atom .md files under internal/content/atoms/, not ~74.
- **fix:** Update the count to ~113 (or phrase as 'over 110 atoms').

### plan-struct-missing-perservice-field [low/confirmed/mech]
- **@** §1.4 Plan — Typed Trichotomy, lines 182-188 (stale-example)
- **spec:** type Plan struct {
    Primary      NextAction   // never zero — if we don't know, we error out upstream
    Secondary    *NextAction  // set only when a second action is commonly done in tandem
    Alternatives []NextAc
- **real:** The real Plan struct has FOUR fields, not three: Primary, PerService (map[string]NextAction), Secondary, Alternatives. PerService carries one action per pending scope service in develop-active and is rendered when len>1. The spec snippet omits it entirely (the section is even titled 'Typed Trichotom
- **fix:** Add the `PerService map[string]NextAction` field to the struct snippet with its doc (per-service pending actions in develop-active; rendered only when >1 service).

### envelope-table-missing-discriminator-fields [low/confirmed/mech]
- **@** §1.6 StateEnvelope field table, lines 258-267 (stale-example)
- **spec:** | `Phase` | ... | | `Environment` | ... | | `SelfService` | ... | | `Project` | ... | | `Services[]` | ... | | `WorkSession` | ... | | `Bootstrap` | ... | | `Generated` | ... |
- **real:** StateEnvelope carries two additional fields that drive atom filtering and are absent from the table: IdleScenario (discriminates PhaseIdle sub-cases empty/bootstrapped/adopt/incomplete — atoms filter on idleScenarios axis) and ExportStatus (discriminates PhaseExportActive sub-states — atoms filter o
- **fix:** Add IdleScenario and ExportStatus rows to the §1.6 field table (both discriminate phase sub-states and drive atom filtering).

### adoption-writes-environment-to-meta [low/confirmed/mech]
- **@** §3.2 What Happens, lines 569-572 (behavior-model)
- **spec:** 3. **Write evidence**: Create ServiceMeta with:
   - Hostname, Mode, StageHostname (if standard)
   - Environment (container/local)
- **real:** ServiceMeta has NO Environment field — the spec's own §1.1 (line 104-105) states 'Environment is not persisted: environment is a property of the currently running ZCP process (runtime-detected), not of a service.' Adoption cannot and does not write an Environment field to the meta.
- **fix:** Remove the `Environment (container/local)` bullet from the §3.2 ServiceMeta write list — it contradicts §1.1 and the actual struct.

## docs/spec-workflows.md (8 findings)

### WF-6-manual-no-deploy [high/confirmed/mech]
- **@** §6 Workflow Routing, line 846 (behavior-model)
- **spec:** Manual close-mode → no deploy offering (user manages directly).
- **real:** The router ALWAYS offers `develop` at Priority 1 whenever any bootstrapped meta exists; it never reads CloseDeployMode to suppress the deploy offering. Deploy is informational, not a gate.
- **fix:** Delete the line "Manual close-mode → no deploy offering (user manages directly)." Replace with: "Deploy (`develop`) is always offered regardless of close-mode — close-mode is informational, resolved within the flow, never a routing gate."

### WF-6-recipe-utility [medium/confirmed/mech]
- **@** §6 Workflow Routing, line 844 (removed-feature)
- **spec:** 6. (P4-P5) Utilities → recipe, scale
- **real:** The router's utility appender adds only `scale` (priority 5). The `recipe` workflow offering was removed; recipe authoring is the ZCP_AUTHORING-gated `zerops_recipe` tool and recipe consumption is bootstrap's recipe route — neither is a routing offering.
- **fix:** Change line 844 to "6. (P5) Utility → scale". Drop `recipe` from the utilities list.

### WF-4.7-iteration-delta-file [medium/confirmed/mech]
- **@** §4.7 Iteration on Failure, line 743 (dangling-ref)
- **spec:** Escalating guidance tiers live in `internal/workflow/iteration_delta.go` and are shared by bootstrap and develop deploys
- **real:** No file `internal/workflow/iteration_delta.go` exists. The 3-tier escalation guidance (DIAGNOSE / SYSTEMATIC / STOP) is no longer a Go data table; the cited tier text ("PREVIOUS FIXES FAILED", the env-vars/bind-address/deployFiles/ports checklist) lives only in this spec — delivered to the agent via
- **fix:** Drop the file citation. State the cap lives in `internal/workflow/session.go` (`defaultMaxIterations = 5`) and the tier guidance is delivered via atoms (e.g. develop-standard-unset-iterate and the deploy-iteration atoms), not a Go table. Al
- **Codex: mechanical; drop the dead iteration_delta.go ref. Also remove the stale BuildIterationDelta comment in session.go:14 (code cleanup).**

### WF-4.3-gitpush-unknown [medium/confirmed/mech]
- **@** §4.3 Close-Mode Options table, line 666 (removed-feature)
- **spec:** | Git-push capability | `GitPushState` + `RemoteURL` | unconfigured / configured / broken / unknown | Whether `strategy="git-push"` works |
- **real:** GitPushState has only three values: unconfigured, configured, broken. The `unknown` value was dropped in Phase 6 of the systemic fix and is dead.
- **fix:** Remove `/ unknown` from the GitPushState values in the §4.3 table (line 666). Same stale `unknown` value also appears in §1.1 ServiceMeta comment (line 89) and the §1.5 axes table (line 239) — outside this area but the same fix.

### WF-4.5-checkDeployPrepare [low/partial/mech]
- **@** §4.5 Pre-Deploy Phase, line 711 (dangling-ref)
- **spec:** **Deploy checker** (`checkDeployPrepare`):
- zerops.yaml exists and parses.
- Setup entries match targets ...
- DM-2 enforcement ...
- Env var references ... validated.
- **real:** No function `checkDeployPrepare` exists. The develop flow is stateless (no step checkers — §7.4). The described validation (zerops.yaml parse, setup matching, DM-2 deployFiles enforcement, env-ref validation) lives in `deployPreFlight` (internal/tools/deploy_preflight.go) plus internal/ops/deploy_va
- **fix:** Replace `(checkDeployPrepare)` with `(deployPreFlight in internal/tools/deploy_preflight.go)`. The listed check semantics remain accurate.

### WF-4.6-checkDeployResult [low/confirmed/mech]
- **@** §4.6 Mode-Specific Deploy Behavior, line 735 (dangling-ref)
- **spec:** **Deploy result checker** (`checkDeployResult`):
- `RUNNING`/`ACTIVE` → pass
- `READY_TO_DEPLOY` → fail ...
- **real:** No function `checkDeployResult` exists. Deploy-status classification now lives in internal/tools/deploy_ssh.go (`classifyDeployStatus`), internal/ops/deploy_failure.go (`ClassifyDeployFailure`), and internal/tools/next_actions.go (`deploySuggestionForStatus`/`deployNextActionForStatus`). `checkDeplo
- **fix:** Replace `(checkDeployResult)` with the actual owners (deploy status classification in internal/tools/deploy_ssh.go::classifyDeployStatus + internal/ops/deploy_failure.go). The status→outcome semantics remain accurate.

### WF-7.2-worksession-struct [low/confirmed/mech]
- **@** §7.2 Work Sessions, lines 884-897 (stale-example)
- **spec:** WorkSession {
  Version ...
  PID ...
  ...
  ClosedAt        RFC3339 (empty = open)
  CloseReason     "explicit" | "auto-complete"
}
- **real:** The struct omits two load-bearing fields and understates the CloseReason enum: (1) `StartTime` (the (pid,startTime) recycled-PID identity guard); (2) `Roles map[hostname]string` (RC-B per-service roles that drive the auto-close denominator). CloseReason has four constants, not two: explicit, auto-co
- **fix:** Add `StartTime RFC3339` and `Roles map[hostname]string  // RC-B session roles; absent → required` to the struct sketch. Change CloseReason to `"explicit" | "auto-complete" | "abandoned" | "iteration-cap"`.

## docs/spec-workflows.md (4 findings)

### rco-1-rewrite-probe-model-gone [high/unverif/mech]
- **@** §8 Recipe Collision Override (F6), Execution flow / RCO-1 (lines 1055-1061, 1075) (behavior-model)
- **spec:** Plan-submit pre-flight (RCO-1) — BootstrapCompletePlan runs RewriteRecipeImportYAML(recipeYAML, plan) as a probe after ValidateBootstrapTargets and ValidateBootstrapRecipeMode pass. Probe failure rejects the plan...
- **real:** BootstrapCompletePlan now REJECTS recipe-route plans outright (engine.go:516-518: 'recipe route derives its plan from the recipe'). The recipe plan is DERIVED, not probe-validated: BootstrapCompleteRecipePlan calls reconcileRecipeOverrides + DeriveRecipePlan from the recipe shape (engine.go:533-608)
- **fix:** Rewrite the RCO Execution-flow + RCO-1 to the derive model: recipe-route plans are DERIVED by BootstrapCompleteRecipePlan (the agent authors nothing in the happy path); a submitted plan is reconciled into RecipeShapeOverrides (hostname rena

### rco-6-runtimeslot-matcher-deleted [high/unverif/mech]
- **@** §8 RCO-6 (line 1080) (removed-feature)
- **spec:** RCO-6 | runtimeSlot matching consumes plan targets in first-unused order by (type, role) where role is dev (zeropsSetup=dev) or stage (anything else). Each runtime target's dev + stage halves MUST map to exactly one reci
- **real:** The slot-matcher was explicitly DELETED (commit ee9bf666 'refactor(workflow): delete the recipe slot-matcher (R3-P4.6)'). grep for runtimeSlot/RuntimeSlot/first-unused across internal/ returns zero hits. The recipe plan is now DERIVED from the recipe shape (DeriveRecipePlan over the verbatim recipe 
- **fix:** Delete RCO-6. The slot-matcher concept (consuming plan targets by (type, role) first-unused) no longer exists; the recipe shape is the single owner and the derived plan keeps every runtime verbatim (workers, second-repo pairs, cross-type st

### rco-2-rco-5-buildguide-rewrite-fn-name [medium/unverif/mech]
- **@** §8 RCO Execution flow RCO-2 (lines 1063-1067) + RCO-5 (line 1079) (identifier)
- **spec:** Provision-step guidance (RCO-2) — buildGuide(StepProvision) calls RewriteRecipeImportYAML once more and injects the rewritten YAML into the atom surface. ... Enforced by bootstrap_guide_assembly.go::buildGuide step-branc
- **real:** buildGuide is a method on BootstrapState: func (b *BootstrapState) buildGuide(step string, iteration int, env Environment, _ knowledge.Provider) string — not a free function buildGuide(StepProvision). It calls RewriteRecipeImportYAMLFromShape(b.RecipeMatch.ImportYAML, overrides) (bootstrap_guide_ass
- **fix:** Update RCO-2/RCO-5 to name RewriteRecipeImportYAMLFromShape(importYAML, overrides) (overrides sourced from b.RecipeOverrides), and describe buildGuide as the BootstrapState method whose discover|provision step-branch (line 74) chooses verba

### d2b-stale-line-citation [low/unverif/mech]
- **@** §8 Develop Flow / D2b (line 979) (dangling-ref)
- **spec:** D2b | handleGitPush refuses with PREREQUISITE_MISSING when there is no committed code at the working directory (rationale comment at internal/tools/deploy_git_push.go:233-240).
- **real:** Lines 230-241 of deploy_git_push.go are now the gitPushSetupPointerInstructions const, NOT the committed-code rationale. The actual rationale comment ('this replaces the old meta.IsDeployed() gate which false-positived on adopted services') lives at lines 311-318, and the PREREQUISITE_MISSING refusa
- **fix:** Change the citation in D2b from 'deploy_git_push.go:233-240' to 'deploy_git_push.go:311-318' (rationale) / ':328-334' (the refusal). Or drop the line numbers and cite the function + the two pinning tests, which are stable.

## docs/spec-workflows.md (6 findings)

### plp2-stale-two-file-allowlist [medium/confirmed/mech]
- **@** §10.3 invariant P-LP-2, line 1202 (identifier)
- **spec:** `platform.NewProjectAdminClient` / `platform.ProjectAdminClient` symbols are reachable only from `internal/tools/workflow_launch_production.go` and `internal/tools/launch_pipeline.go` (Part 2 sibling). Pinned by `TestPro
- **real:** The pinning test's symbol-seam allowlist (allowedSymbolFiles) permits FOUR files, not two: workflow_launch_production.go, launch_pipeline.go, launch_prod_ops.go (F7 bring-up window), and launch_confirm.go (confirm-production close). Both launch_prod_ops.go and launch_confirm.go actually reference pl
- **fix:** Update P-LP-2 to list all four allowed files for the symbol seam: workflow_launch_production.go, launch_pipeline.go, launch_prod_ops.go, launch_confirm.go (and note the factory-var seam additionally allows launch_reset.go + launch_existing.

### e3-dangling-test-name [low/partial/mech]
- **@** §9.4 invariant E3, line 1147 (dangling-ref)
- **spec:** Pinned by `TestHandleExport_GitPushUnconfigured_ChainsAfterClassify` + `TestHandleExport_MissingGitRemote_ChainsToGitPushSetup`.
- **real:** No test function named TestHandleExport_GitPushUnconfigured_ChainsAfterClassify exists — that string survives only as a stale comment (workflow_export_test.go:455). The real test for the git-push-unconfigured-after-classify path is TestHandleExport_GitPushUnconfigured_DeliversComposeReady (the flow 
- **fix:** Replace the cited test name with TestHandleExport_GitPushUnconfigured_DeliversComposeReady in E3.

### plp12-dangling-applymerge-test [low/confirmed/mech]
- **@** §10.3 invariant P-LP-12, line 1213 (dangling-ref)
- **spec:** Pinned by `TestDetectExistingProjectConflicts_*` + `TestApplyMergeSkipsToBundle_*` + `TestMissingDestructiveAckForReplaces_*`.
- **real:** No test matching TestApplyMergeSkipsToBundle_* exists. The actual merge-resolution test is TestApplyMergeResolutionsToBundle_DropsSkipsAndOverridesReplaces.
- **fix:** Replace TestApplyMergeSkipsToBundle_* with TestApplyMergeResolutionsToBundle_DropsSkipsAndOverridesReplaces in P-LP-12.

### section11-misnumbered-subsection [low/partial/mech]
- **@** §11 Planned Features subsection heading, line 1220 (naming)
- **spec:** ### 9.1 Mode Expansion (simple/dev → standard)
- **real:** This subsection sits under '## 11. Planned Features' (line 1218) but is numbered '### 9.1', colliding with the genuine '### 9.1 Multi-call narrowing' under §9 (line 1104). Any cross-reference to §9.1 is now ambiguous. Should be §11.1.
- **fix:** Renumber the §11 subsection heading from '### 9.1 Mode Expansion' to '### 11.1 Mode Expansion'.

### classify-infra-allowlist-incomplete [low/confirmed/mech]
- **@** §9.3 Four-category secret classification, line 1139 (identifier)
- **spec:** the exact-key `topology.IsClassifyInfrastructure` allowlist for `ZCP_API_KEY` / `ZCP_AGENT_TYPE` / `GIT_TOKEN`
- **real:** The actual classifyInfrastructureKeys allowlist contains FIVE keys: ZCP_API_KEY, ZCP_AGENT_TYPE, ZCP_AGENT_TYPES, GIT_TOKEN, and ZCP_LAUNCH_TOKEN. The spec enumerates only three and omits ZCP_AGENT_TYPES and the load-bearing ZCP_LAUNCH_TOKEN (the staged single-token launch secret that MUST drop from
- **fix:** Extend the §9.3 allowlist enumeration to include ZCP_AGENT_TYPES and ZCP_LAUNCH_TOKEN (the latter is the staged launch-token secret whose bundle-drop is part of the single-token launch invariant P-LP-14).

### mode-expansion-preserved-fields-incomplete [low/confirmed/mech]
- **@** §11 (mis-numbered §9.1) Mode Expansion, point 1 (Meta merge), line 1242 (behavior-model)
- **spec:** user-authored fields (`BootstrappedAt`, `CloseDeployMode`, `CloseDeployModeConfirmed`, `GitPushState`, `RemoteURL`, `BuildIntegration`, `FirstDeployedAt`) are preserved.
- **real:** mergeExistingMeta preserves those seven fields PLUS PrimarySetupName and StageSetupName (migrate-forward-empty: non-empty existing value wins) and ProvisionedFromGit (sticky-once-set OR). The spec's seven-field list is a subset; the setup-name and provenance fields are also user-authored/sticky and 
- **fix:** Add PrimarySetupName / StageSetupName (preserved when existing non-empty) and ProvisionedFromGit (sticky OR) to the preserved-fields enumeration, or rephrase to note the list is illustrative and the authoritative set lives in mergeExistingM

## docs/spec-knowledge-distribution.md (20 findings)

### missing-axes-in-spec [high/unverif/mech]
- **@** §3 Axes (lines 85-221), §4.2 frontmatter table (lines 237-259) (removed-feature)
- **spec:** Six axes decompose the guidance space. Each axis is declared in an atom's frontmatter as a list
- **real:** Five real, validated axes/attributes are entirely undocumented in the spec: `runtimeBases` (service-scoped concrete-stack filter), `envelopeDeployStates` (envelope-scoped twin of deployStates, mutually exclusive with it at parse), `managedTypes` (envelope-scoped managed-dep-type gate, the F7 fix), `
- **fix:** Add §3.x subsections + §4.2 frontmatter rows for runtimeBases, envelopeDeployStates, managedTypes, multiService, coverageExempt. Each is a shipped, parse-validated axis the authoring contract must document.

### strategies-axis-retired [high/unverif/mech]
- **@** §4.2 Frontmatter fields, line 247 (removed-feature)
- **spec:** | `strategies` | no | Service-scoped (§3.4). |
- **real:** There is no `strategies` axis. It was replaced by the three orthogonal axes closeDeployModes/gitPushStates/buildIntegrations. `strategies` is not in validAtomFrontmatterKeys (so a `strategies:` frontmatter key fails ParseAtom as 'unknown atom frontmatter key'), no AxisVector field exists, and §3.4 —
- **fix:** Delete the `strategies` row from §4.2. In §5.1 step 1, KD-15, KD-16 replace `strategies` with the closeDeployModes/gitPushStates/buildIntegrations triad (or note 'the close-mode/git-push/build-integration service-scoped axes').

### closedeploymode-axis-value-git-push [high/unverif/mech]
- **@** §3.4 closeDeployModes table, line 134 (removed-feature)
- **spec:** | `git-push` | Commit + push to configured remote delivery. Build trigger is `BuildIntegration`'s concern. Auto-close fires on green-scope. |
- **real:** `git-push` is NOT a valid closeDeployModes axis value. validAtomEnumValues['closeDeployModes'] = {unset, auto, manual} — a `closeDeployModes: [git-push]` frontmatter fails ParseAtom. The git-push CloseDeployMode VALUE was retired and folds to auto at parse; delivery is derived from GitPushState. The
- **fix:** Remove the `git-push` row from the §3.4 closeDeployModes axis table (the axis value set is {unset, auto, manual}). If retaining a note about the retired value, mark it explicitly as legacy/folded-to-auto, not a live axis value.

### phases-missing-launch-production [high/unverif/mech]
- **@** §3.1 phases table, lines 91-100 (removed-feature)
- **spec:** | `export-active` | Stateless export immediate workflow. |  (end of §3.1 phases table)
- **real:** The phases enum has a `launch-production-active` value (validAtomEnumValues['phases'] + PhaseLaunchProductionActive), with 16 launch-* atoms scoped to it. The §3.1 table ends at export-active and never lists launch-production-active.
- **fix:** Add a `launch-production-active` row to the §3.1 phases table and document the launch-production phase + its 16 launch-* atoms (also touch §5.2 which only lists strategy-setup + export-active as stateless phases, and §5.4 inventory which om

### exportstatus-enum-drift [high/unverif/mech]
- **@** §3.11 exportStatus, line 212 (also §3.11 line 218 mentions 'six other status atoms'; §4.2 line 253 'Closed enum of seven export sub-statuses') (identifier)
- **spec:** which is `topology.ExportStatus`. Closed enum of seven values: `scope-prompt`, `variant-prompt`, `scaffold-required`, `git-push-setup-required`, `classify-prompt`, `validation-failed`, `publish-ready`.
- **real:** The closed enum no longer contains `variant-prompt` (removed — pinned by TestExportStatusValues with a comment 'The dev/stage variant-prompt was removed'). It instead contains `compose-ready`. The validAtomEnumValues['exportStatus'] set is {scope-prompt, scaffold-required, git-push-setup-required, c
- **fix:** Replace `variant-prompt` with `compose-ready` in the §3.11 enum list. Verify §4.2's 'seven' count still holds (it does: 7 non-empty values). Audit §3.11's surrounding prose (variant-prompt example references) for the same stale value.

### kd08-recipe-guidance-dangling [high/unverif/mech]
- **@** §10 Invariants, KD-08, line 485 (dangling-ref)
- **spec:** | KD-08 | Recipe authoring runs through `recipe_*.go` section parsers, NOT the atom synthesizer. | `internal/workflow/recipe_guidance.go` does not call `Synthesize` |
- **real:** internal/workflow/recipe_guidance.go does NOT exist. The in-core v2 recipe pipeline (workflow=recipe, recipe_*.go guidance, internal/content/workflows/recipe.md) was deleted 2026-06-12 — §7 line 393 itself states this. Recipe authoring now lives entirely in internal/authoring/recipe/ (the v3 engine,
- **fix:** Rewrite KD-08 to cite the v3 authoring boundary: recipe authoring runs through internal/authoring/recipe/ (ZCP_AUTHORING-gated), not the atom synthesizer. Point the code reference at the authoring package / spec-authoring-boundary.md. Recon
- **Codex: fix the invariant TABLE row only, not the whole §; correct boundary already stated at lines 34/389.**

### synthesize-return-type [medium/unverif/mech]
- **@** §5 Synthesizer, line 308 (identifier)
- **spec:** func Synthesize(envelope StateEnvelope, corpus []KnowledgeAtom) ([]string, error)
- **real:** Synthesize returns []MatchedRender, not []string. Body strings are extracted via BodiesOf(matches). §5.1 step 4 ('Return: the ordered list of rendered bodies') and KD-02 also assume the []string return.
- **fix:** Change the §5 signature to `([]MatchedRender, error)`. Note that SynthesizeBodies (synthesize.go:296) is the []string-returning variant; reference it if the spec wants to describe a body-string surface.

### gitpushstates-unknown-value [medium/unverif/mech]
- **@** §3.4 gitPushStates table, line 145 (removed-feature)
- **spec:** | `unknown` | Adopted/migrated meta — needs probe. |
- **real:** `unknown` is not a valid gitPushStates axis value. validAtomEnumValues['gitPushStates'] = {unconfigured, configured, broken} and topology.GitPushState has only GitPushUnconfigured/GitPushConfigured/GitPushBroken. A `gitPushStates: [unknown]` would fail ParseAtom.
- **fix:** Remove the `unknown` row from the §3.4 gitPushStates table (valid values are unconfigured, configured, broken).

### modes-enum-undercount [medium/unverif/mech]
- **@** §3.2 modes table, lines 105-110 (removed-feature)
- **spec:** | `standard` | Dev half of a standard pair when viewed as a runtime ... |  (§3.2 modes table: dev / stage / simple / standard)
- **real:** The modes axis accepts 6 values: dev, stage, simple, standard, local-stage, local-only. The spec lists only the first four; local-stage and local-only (the local-machine topologies) are missing.
- **fix:** Add `local-stage` and `local-only` rows to the §3.2 modes table (local-machine topologies).

### service-scoped-axes-count-seven [medium/unverif/mech]
- **@** §3.10 Service-scoped axis conjunction, line 206 (also §3.10 line 208 lists envelope-wide axes; KD-16 line 493) (behavior-model)
- **spec:** The seven service-scoped axes (`modes`, `closeDeployModes`, `gitPushStates`, `buildIntegrations`, `runtimes`, `deployStates`, `serviceStatus`) evaluate **together per service**
- **real:** serviceSatisfiesAxes evaluates EIGHT service-scoped axes under conjunction: Modes, CloseDeployModes, GitPushStates, BuildIntegrations, Runtimes, RuntimeBases, DeployStates, ServiceStatuses. The spec omits `runtimeBases` from the conjunction group (it is service-scoped). Separately, §3.10's envelope-
- **fix:** Add `runtimeBases` to the §3.10 service-scoped conjunction list and update 'seven' → 'eight'. KD-16's parenthetical (modes, strategies, runtimes, deployStates) needs the same correction (strategies→the triad, + runtimeBases/serviceStatus).

### develop-strategy-review-deploystates-example [medium/unverif/mech]
- **@** §3.10 conjunction example, line 206 (stale-example)
- **spec:** e.g. `develop-strategy-review (deployStates=[deployed], closeDeployModes=[unset])` would surface when service A is deployed+`auto` and service B is never-deployed+`unset`
- **real:** The develop-strategy-review atom does NOT declare a deployStates axis. Its real axes are `phases: [develop-active]`, `closeDeployModes: [unset]`, `multiService: aggregate`. The §3.10 example fabricates a deployStates=[deployed] axis the atom never had. §3.4 line 136 makes the related claim that this
- **fix:** Rewrite the §3.10 example to use a real two-service-scoped-axis atom, or drop the deployStates clause. Fix §3.4 line 136 to say develop-strategy-review fires whenever close-mode is unset (it can precede first deploy), not 'post-first-deploy

### servicesnapshot-strategy-field [medium/unverif/mech]
- **@** §2.1 Envelope Fields, Services[] row, line 52 (identifier)
- **spec:** Per-service: `{Hostname, TypeVersion, RuntimeClass, Status, Bootstrapped, Mode, Strategy, StageHostname}`.
- **real:** ServiceSnapshot has no `Strategy` field. The strategy concept was decomposed into CloseDeployMode, GitPushState, BuildIntegration — all three are real ServiceSnapshot fields (plus RemoteURL, Deployed, etc.). §8.4 correctly lists the new fields, so §2.1 is internally inconsistent with §8.4.
- **fix:** Replace `Strategy` in the §2.1 Services[] field list with `CloseDeployMode, GitPushState, BuildIntegration` (matching §8.4).

### kd09-iteration-delta-dangling [medium/unverif/mech]
- **@** §10 Invariants, KD-09, line 486 (dangling-ref)
- **spec:** | KD-09 | Iteration-tier text (`BuildIterationDelta`) is emitted as an addendum to synthesized atoms, not as an atom. | `internal/workflow/iteration_delta.go` is called independently by deploy handlers |
- **real:** internal/workflow/iteration_delta.go does NOT exist. BuildIterationDelta is defined in internal/workflow/session.go. The invariant itself (iteration text is an addendum, not an atom) may still hold, but the cited file is wrong.
- **fix:** Change the KD-09 code reference from `internal/workflow/iteration_delta.go` to `internal/workflow/session.go::BuildIterationDelta`.
- **Codex: rewrite or DROP KD-09; do NOT preserve the dead file/function ref. Note: 'BuildIterationDelta defined in session.go' is WRONG — it's only a comment (session.go:14).**

### kd17-markservicedeployed-dangling [medium/unverif/mech]
- **@** §10 Invariants, KD-17, line 494 (dangling-ref)
- **spec:** | KD-17 | `MarkServiceDeployed` resolves hostname via `findMetaForHostname`, so verifying the stage half of a standard pair stamps the dev-keyed meta. ... | `service_meta.go:findMetaForHostname`; `TestMarkServiceDeployed
- **real:** None of the three cited identifiers exist. There is no MarkServiceDeployed function, no findMetaForHostname function, and no TestMarkServiceDeployed_StampsViaStageHostname test. The real mechanism: RecordDeployAttempt (work_session.go:286) calls stampFirstDeployedAt, which resolves hostname through 
- **fix:** Rewrite KD-17 to cite RecordDeployAttempt → stampFirstDeployedAt resolving via FindServiceMeta; pin reference TestRecordDeployAttempt_StampsViaStageHostname.

### corpus-count-and-inventory-stale [medium/unverif/mech]
- **@** §1.2 line 23 and §5.4 line 340 (+ inventory table lines 342-348) (stale-example)
- **spec:** internal/content/atoms/*.md        # ~74 atoms, embedded via //go:embed   ...   Inventory as of 2026-04-19 (74 atoms total):
- **real:** The corpus has 113 atoms, not 74. Per-prefix actual counts: idle-* = 4 (spec 3), bootstrap-* = 20 (spec 27), develop-* = 61 (spec 25), export-* = 7 (spec 6), launch-* = 16 (spec: not listed at all), plus the 4 setup-git-push/setup-build-integration atoms. Every row of the §5.4 inventory table and th
- **fix:** Update §1.2 '~74' to ~113 and regenerate the §5.4 inventory table (drop the dated 'as of 2026-04-19' snapshot or refresh it; add the launch-* row). Consider replacing the hard count with a 'generated by corpus_coverage_test' note so it does

### synthesize-immediate-workflow-signature [low/unverif/mech]
- **@** §5.2 Rendering into the tool response, line 325 (identifier)
- **spec:** Stateless synthesis (`strategy-setup`, `export-active`) uses `SynthesizeImmediateWorkflow(phase, env)` which joins bodies with `\n\n---\n\n` and returns a single string.
- **real:** SynthesizeImmediateWorkflow takes a single StateEnvelope argument, not (phase, env). The phase is carried inside the envelope.
- **fix:** Change `SynthesizeImmediateWorkflow(phase, env)` to `SynthesizeImmediateWorkflow(env)`.

### six-axes-undercount [low/unverif/mech]
- **@** §3 Axes, line 87 (behavior-model)
- **spec:** Six axes decompose the guidance space.
- **real:** AxisVector declares 17 axis/attribute fields: Phases, Modes, Environments, CloseDeployModes, GitPushStates, BuildIntegrations, Runtimes, RuntimeBases, Routes, Steps, IdleScenarios, DeployStates, EnvelopeDeployStates, ServiceStatuses, ExportStatuses, ManagedTypes, MultiService. The spec body itself d
- **fix:** Drop the fixed count ('Six axes') — say 'A set of axes decompose the guidance space' or update to the real count; the §3.x subsections already enumerate them.

### routes-enum-missing-resume [low/unverif/mech]
- **@** §3.6 routes table, lines 172-175 (removed-feature)
- **spec:** Bootstrap step names ... §3.6 routes: recipe / classic / adopt
- **real:** The routes axis enum accepts a fourth value, `resume`, in addition to recipe/classic/adopt.
- **fix:** Add a `resume` row to the §3.6 routes table.

### agent-filled-placeholders-incomplete [low/unverif/mech]
- **@** §4.4 Placeholders (Agent-filled), line 280 (stale-example)
- **spec:** `{start-command}`, `{task-description}`, `{your-description}`, `{next-task}`, `{port}`, `{name}`, `{token}`, `{url}`, `{runtimeVersion}`, `{runtimeBase}`, `{setup}`, `{serviceId}`, `{targetHostname}`, `{devHostname}`, `{
- **real:** allowedSurvivingPlaceholders contains four agent-filled placeholders the spec omits: `{path}` (dev-server health path), `{task-id}` (harness background-task id), `{provider}`, and `{workingDir}` (local checkout dir for git-push setup). The spec's list is otherwise a subset of the real whitelist.
- **fix:** Add {path}, {task-id}, {provider}, {workingDir} to the §4.4 agent-filled placeholder list (or point to allowedSurvivingPlaceholders as the authoritative source rather than copying it).

### close-mode-action-fold-not-documented [low/unverif/mech]
- **@** §8.3 Close-Mode + Capability Updates, line 432 (behavior-model)
- **spec:** `zerops_workflow action="close-mode" closeMode={hostname:value}` writes `CloseDeployMode` (validates `auto` / `git-push` / `manual`) and stamps `CloseDeployModeConfirmed=true`.
- **real:** The handler accepts auto/git-push/manual on the wire (validCloseModes), but a passed `git-push` is FOLDED to auto at persist — the meta is written with the user's intent, and the legacy git-push value never lands as CloseDeployMode (it folds: 'git-push folded — push delivery derives from git-push-se
- **fix:** Note in §8.3 that git-push is accepted for wire-compat but folds to auto at persist (delivery derives from GitPushState); the persisted CloseDeployMode value set is {auto, manual} (+ unset sentinel).

## docs/spec-knowledge-distribution.md (8 findings)

### kd17-markServiceDeployed-dangling [high/confirmed/DECISION]
- **@** §10 Invariants table, KD-17 (line 494) (dangling-ref)
- **spec:** | KD-17 | `MarkServiceDeployed` resolves hostname via `findMetaForHostname` … | `service_meta.go:findMetaForHostname`; `TestMarkServiceDeployed_StampsViaStageHostname` |
- **real:** All three identifiers are absent from the codebase. There is no `MarkServiceDeployed` function (the deploy-stamping API is `RecordExternalDeploy` / `RecordDeployAttempt` / `stampFirstDeployedAt` in service_meta.go), no `findMetaForHostname` helper (the lookup helpers are `FindServiceMeta` / `ReadSer
- **fix:** Rewrite KD-17 against the real API: the stage-half-stamps-dev-keyed-meta invariant is carried by RecordExternalDeploy/RecordDeployAttempt resolving through FindServiceMeta (pair-keyed meta, §8.2 E8). Replace the function names and cite an a

### kd09-iteration-delta-dangling [medium/confirmed/DECISION]
- **@** §10 Invariants table, KD-09 (line 486) (dangling-ref)
- **spec:** | KD-09 | Iteration-tier text (`BuildIterationDelta`) is emitted as an addendum to synthesized atoms, not as an atom. | `internal/workflow/iteration_delta.go` is called independently by deploy handlers |
- **real:** No file `internal/workflow/iteration_delta.go` exists, and `BuildIterationDelta` is NOT a defined function anywhere in the main tree — the only occurrence is a stale prose comment in session.go ('defaultMaxIterations aligns with the 3-tier escalation ladder in BuildIterationDelta'). The function the
- **fix:** Re-anchor KD-09 to the real iteration-escalation implementation (session.go: maxIterations / defaultMaxIterations 3-tier ladder, or the actual addendum emitter), or remove the invariant if the BuildIterationDelta concept no longer ships. Up
- **Codex: rewrite or DROP KD-09; do NOT preserve the dead file/function ref. Note: 'BuildIterationDelta defined in session.go' is WRONG — it's only a comment (session.go:14).**

### kd08-recipe-guidance-dangling [medium/confirmed/mech]
- **@** §10 Invariants table, KD-08 (line 485) (dangling-ref)
- **spec:** | KD-08 | Recipe authoring runs through `recipe_*.go` section parsers, NOT the atom synthesizer. | `internal/workflow/recipe_guidance.go` does not call `Synthesize` |
- **real:** No file `internal/workflow/recipe_guidance.go` exists. The in-core recipe v2 pipeline (workflow=recipe, recipe_*.go section parsers, recipe.md) was DELETED 2026-06-12 — the SAME spec says so in §7 (line 393). The recipe_*.go files that remain (recipe_corpus_store.go, recipe_import_local.go, recipe_o
- **fix:** Rewrite KD-08 to reflect the retired v2 / gated-authoring reality: recipe authoring is the ZCP_AUTHORING-gated engine in internal/authoring/recipe/ and does not pass through Synthesize. Drop the dead `recipe_guidance.go` code reference (poi
- **Codex: fix the invariant TABLE row only, not the whole §; correct boundary already stated at lines 34/389.**

### kd15-strategies-axis-retired [medium/confirmed/mech]
- **@** §10 Invariants table, KD-15 (line 492) (removed-feature)
- **spec:** | KD-15 | Mixed strategies across a single Work Session scope are permitted — each service's strategy drives its own atom filtering. | `build_plan.go` + atom `strategies` axis |
- **real:** The atom `strategies:` axis is retired. atom.go states the three orthogonal axes (closeDeployModes/gitPushStates/buildIntegrations) 'replace the legacy `strategies:`'; AxisVector has no Strategies field and ParseAtom has no parseStrategies. There is no 'strategies axis' for a service's strategy to d
- **fix:** Reword KD-15 to reference the close-mode / git-push-state / build-integration axes (the §3.4 trio) instead of the retired single `strategies` axis, consistent with §3.10's 'seven service-scoped axes' enumeration which already omits strategi

### kd16-strategies-axis-and-anyServiceMatchesAll [medium/confirmed/mech]
- **@** §10 Invariants table, KD-16 (line 493) (identifier)
- **spec:** | KD-16 | Service-scoped axes (`modes`, `strategies`, `runtimes`, `deployStates`) … | `synthesize.go:anyServiceMatchesAll`; `TestSynthesize_ServiceScopedAxesRequireSameService` |
- **real:** Two stale references: (1) the axis list includes the retired `strategies` axis (replaced by closeDeployModes/gitPushStates/buildIntegrations); §3.10 of the same spec correctly lists seven service-scoped axes without 'strategies'. (2) The code reference `synthesize.go:anyServiceMatchesAll` is a funct
- **fix:** Update KD-16's axis list to the current service-scoped set (modes, closeDeployModes, gitPushStates, buildIntegrations, runtimes, deployStates, serviceStatus per §3.10) and change the code reference from the dead `anyServiceMatchesAll` to `s

### sec114-hotshell-allowlist-omitted [low/confirmed/mech]
- **@** §11.4 Allowlist policy (lines 579-583); same closed list repeated in §11.7 line 881-882 (naming)
- **spec:** Per-axis allowlists (`axisLAllowlist`, `axisKAllowlist`, `axisMAllowlist`, `axisNAllowlist`) live in `atoms_lint_seed_allowlist.go` and follow the same key/rationale shape; an entry there suppresses ONE rule for ONE atom
- **real:** atoms_lint_seed_allowlist.go declares FIVE per-axis allowlists, not four: axisLAllowlist, axisKAllowlist, axisMAllowlist, axisHotShellAllowlist, axisNAllowlist. The spec's closed enumeration omits axisHotShellAllowlist (the axis-hot-shell lint family that flags nohup/disown/trailing-& backgrounding 
- **fix:** Add axisHotShellAllowlist to the §11.4 (and §11.7) per-axis allowlist enumeration; optionally add a one-line §11.6.x or §11.7 mention of the axis-hot-shell lint so the spec's coverage of enforced axes matches lintAtomCorpus (which also runs

### sec12-scenario-count-30-vs-32 [low/confirmed/mech]
- **@** §12 Goldens intro (line 1005); reinforced by canonicalGoldenScenarios()'s own stale doc-comment (behavior-model)
- **spec:** The 30 canonical scenarios at `internal/workflow/testdata/atom-goldens/` are the regression boundary for atom-rendered guidance.
- **real:** There are 32 canonical scenarios, not 30. Measured at runtime: len(canonicalGoldenScenarios()) == 32 (idle 4 + bootstrap 5 + develop 13 + strategy-setup 2 + export 7 + launch-production 1), and there are 32 golden files on disk. The number 30 is stale; the launch-production group and export growth (
- **fix:** Replace '30 canonical scenarios' with '32' in §12 intro, §12.2 ('When the suite crosses 40' threshold unaffected), and the scenarios_fixtures_test.go doc-comments. Better: phrase as 'the canonical scenarios' without a hardcoded count to avo

### sec11-plan-doc-paths-archived [low/partial/mech]
- **@** §11.6.5 (line 810), §11.7.5 (line 893), §11.11 (line 990), §12 (implicit); §11.5 glob plans/audit-composition/axis-{k,l,m}-*.md (line 588) (dangling-ref)
- **spec:** the atom-corpus-verification plan (`plans/atom-corpus-verification-2026-05-02.md`) Phase 2 ran a 6-agent review pass …
- **real:** The cited plan was archived: it now lives at plans/archive/atom-corpus-verification-2026-05-02.md, not plans/atom-corpus-verification-2026-05-02.md. CLAUDE.md states plans are transient and 'a line pointing at an archived plan → promote-or-delete'. Separately, the §11.5 glob plans/audit-composition/
- **fix:** Either drop the transient plan-doc citations (per CLAUDE.md, plans must not be cited as sources — promote the durable content into the spec body) or, if kept for provenance, fix the paths to plans/archive/... and correct the axis-{k,l,m} gl

## docs/spec-work-session.md (7 findings)

### verify-stamps-firstdeployedat-wrong-fn [high/unverif/mech]
- **@** §7.2 zerops_verify, line 392 (dangling-ref)
- **spec:** On success, `RecordVerifyAttempt` calls `MarkServiceDeployed(stateDir, hostname)` to stamp `FirstDeployedAt` on the ServiceMeta — the signal that exits the first-deploy branch on the next session. The hostname lookup res
- **real:** `RecordVerifyAttempt` EXPLICITLY no longer mutates ServiceMeta (its doc-comment: "Under plan phase A.3 this no longer mutates ServiceMeta"). FirstDeployedAt is stamped by `RecordDeployAttempt` (on deploy success, not verify) via `stampFirstDeployedAt`, which resolves the hostname through `FindServic
- **fix:** Rewrite §7.2 to: on deploy success, `RecordDeployAttempt` calls `stampFirstDeployedAt` which writes `FirstDeployedAt` via `UpdateServiceMeta` (idempotent first-stamp only). The hostname resolves through `FindServiceMeta` (direct file match 

### status-json-shape-fabricated [high/unverif/mech]
- **@** §6.2 action=status, lines 311-327 (stale-example)
- **spec:** **Work Session status response:**
```json
{
  "workflow": "develop",
  "intent": "add login form",
  "services": [...],
  "createdAt": "...", "lastActivityAt": "...",
  "suggestedNext": "Fix build timeout on api, then re
- **real:** `handleLifecycleStatus` returns a rendered MARKDOWN status block via `workflow.RenderStatus(...)` (textResult), assembled from ComputeEnvelope + Synthesize + BuildPlan. There is no JSON object with `workflow`/`intent`/`services[]`/`suggestedNext`/`canClose` keys. The identifiers `suggestedNext` and 
- **fix:** Replace the JSON snippet with the actual rendered-markdown status surface (RenderStatus output: envelope status block + synthesized atom guidance + plan), or describe the status response as a markdown Lifecycle Status block rather than a st

### suggestednext-pure-function-nonexistent [medium/unverif/mech]
- **@** §6.2 action=status, lines 332-338 (removed-feature)
- **spec:** `suggestedNext` is computed by a pure function over deploy/verify state + live strategy:
- Some service has unsucceeded deploy attempts → "Fix and redeploy [hostname]"
- All deployed, some not verified → "Verify [hostnam
- **real:** There is no `suggestedNext` field and no pure function producing it. Next-step guidance is produced by the atom synthesis pipeline (Synthesize over the atom corpus + BuildPlan), not a switch over deploy/verify state with the literal strings quoted. The "Idle > 4h" rule and these exact phrasings do n
- **fix:** Delete the `suggestedNext` pure-function description, or rewrite to: next-step guidance is synthesized by the atom pipeline (Synthesize over the corpus + BuildPlan) keyed off the envelope's deploy/verify/close-mode state; there is no single

### data-model-missing-starttime-roles [medium/unverif/mech]
- **@** §3 Work Session Data Model, lines 117-130 (stale-example)
- **spec:** type WorkSession struct {
    Version        string ...
    PID            int ...
    ProjectID      string ...
    Environment    string ...
    Intent         string ...
    Services       []string ...
    CreatedAt  
- **real:** The actual struct has two additional load-bearing fields the spec omits: `StartTime string` (json:startTime — the (pid,startTime) identity guarding against a recycled PID inheriting a dead predecessor's session) and `Roles map[string]string` (json:roles — per-service session role: required/deferred/
- **fix:** Add `StartTime string json:"startTime,omitempty"` (recycled-PID identity guard) and `Roles map[string]string json:"roles,omitempty"` (per-service required/deferred/out-of-scope role) to the §3 struct, and add a paragraph documenting the rec

### autoclose-pseudocode-ignores-roles-and-resolved-targets [medium/unverif/mech]
- **@** §7.5 Auto-close — DERIVED, lines 412-418 (behavior-model)
- **spec:** autoClose = len(Services) > 0 &&                      # DECLARED scope is the denominator
    every in-scope service has CloseDeployMode == auto &&         # manual/unset block; legacy git-push folds to auto at parse
   
- **real:** The real `EvaluateAutoClose` does NOT iterate "all s in Services" and does NOT check close-mode over every in-scope service. (1) The close-mode gate (`autoCloseGateOpen`) iterates `RequiredServices(ws)` only — deferred / out-of-scope role services are excluded from the denominator. (2) The deploy+ve
- **fix:** Rewrite the §7.5 pseudocode so the denominator is `RequiredServices(ws)` (role-filtered: deferred/out-of-scope excluded), the deploy+verify iteration is over `ResolvedDeployTargets` (pair-seam resolved), and add the staleVerify clause (pass

### sessionentry-struct-stale-fields [low/unverif/mech]
- **@** §4 Registry as Single Source of Session Ownership, lines 164-170 (stale-example)
- **spec:** type SessionEntry struct {
    SessionID string `json:"sessionId"`
    PID       int    `json:"pid"`
    Workflow  string `json:"workflow"`     // bootstrap | recipe | work
    ProjectID string `json:"projectId"`
    Cre
- **real:** The actual `SessionEntry` has 8 fields, not 5: it adds `StartTime` (recycled-PID identity), `Intent`, and `UpdatedAt` between/after the spec's fields.
- **fix:** Update the §4 SessionEntry snippet to include `StartTime string json:"startTime,omitempty"`, `Intent string json:"intent"`, and `UpdatedAt string json:"updatedAt"`.

### closereason-comment-missing-iteration-cap [low/unverif/mech]
- **@** §3 Work Session Data Model, line 129 (identifier)
- **spec:** CloseReason    string                             `json:"closeReason,omitempty"` // explicit | auto-complete | abandoned
- **real:** There are FOUR CloseReason values, not three: the code adds `CloseReasonIterationCap = "iteration-cap"` (an infrastructure workflow hitting maxIterations auto-closes the work session). The §7.5 derived model also notes auto-complete is NEVER persisted, so in practice the persisted-on-disk CloseReaso
- **fix:** Update the §3 comment to `// explicit | auto-complete | abandoned | iteration-cap` and note that auto-complete is derived (never persisted), so only explicit / iteration-cap / abandoned are written to disk.

## docs/spec-launch-production-platform-spike.md (5 findings)

### projectadminclient-interface-drift [low/confirmed/DECISION]
- **@** Locked contracts — ProjectAdminClient interface, lines 407-444 (behavior-model)
- **spec:** type ProjectAdminClient interface {
    CreateAndImportProject(ctx context.Context, yaml string, opts CreateOpts) (CreateAndImportResult, error)
    ...
    GetServiceEnv(ctx context.Context, serviceID string) ([]EnvKey,
- **real:** The shipped interface diverges on nearly every member: CreateAndImportProject takes (ctx, yaml string) and returns (*ImportResult, error) — no CreateOpts param, no CreateAndImportResult type. Env readers are GetServiceEnvKeys/GetProjectEnvKeys (not GetServiceEnv/GetProjectEnv). DeleteProject returns
- **fix:** This is a Phase-A spike's predicted/locked contract that Phase B+ then evolved. Either (a) add a one-line banner at the interface block noting 'spike-era proposed contract; the shipped interface lives in internal/platform/project_admin.go a

### runtime-mode-non-ha-not-emitted [medium/confirmed/mech]
- **@** LaunchBundleBuilder defaults table, line 454 (removed-feature)
- **spec:** | Runtime `mode` | `NON_HA` (platform constraint) | Plan §7.2 |
- **real:** The launch composer deliberately does NOT emit `mode: NON_HA` on runtime entries. The runtime services[] entry is built with only hostname/type/startWithoutCode + scaling/cpuMode transforms; a code comment explicitly states emitting a legacy `mode: NON_HA` is 'vestigial noise' because runtimes are a
- **fix:** Remove the `Runtime mode | NON_HA` row from the defaults table (lines 454), or annotate it as 'NOT emitted — runtimes are always HA on the platform; replica count is the minContainers axis'. Aligns with the recent managed-service-mode-retir

### project-mode-field-is-corepackage [medium/partial/mech]
- **@** LaunchBundleBuilder defaults table, line 451 (also CreateOpts struct line 419: `Mode ProjectModeEnum`) (identifier)
- **spec:** | `project.mode` | `SERIOUS` | A.7 |
- **real:** The composer emits the project tier under the YAML key `corePackage` (value `SERIOUS`), NOT `mode`. The default constant is `productionDefaultCorePackage = "SERIOUS"`. The launch-readiness rubric also greps for the literal string `corePackage: SERIOUS`. There is no `project.mode` field in the emitte
- **fix:** Rename the table row `project.mode` → `project.corePackage`. Update the CreateOpts struct field `Mode ProjectModeEnum` to `CorePackage string` to match. Note this also intersects the retired managed-service-`mode:` cluster — `mode:` is no l

### publicipv4shared-not-emitted [low/confirmed/mech]
- **@** LaunchBundleBuilder defaults table, line 452 (removed-feature)
- **spec:** | `project.publicIpV4Shared` | `false` | A.5 — production should opt for dedicated IPv4 when configured; default false until user picks |
- **real:** The composer never emits a `publicIpV4Shared` field in the project block. The project map is composed with only name, tags, corePackage, location (+ envVariables when present). No reference to publicIpV4Shared exists anywhere in the bundle package.
- **fix:** Remove the `project.publicIpV4Shared` row from the defaults table — it is not part of the shipped launch bundle. If dedicated-IP guidance is still intended, route it to the relevant atom rather than the bundle-defaults contract.

### future-test-names-drifted [low/confirmed/mech]
- **@** What still needs admin-token verification (Phase B e2e), lines 391-394 (stale-example)
- **spec:** 3. **TestProjectAdminClient_DeleteProject_Live** ... 4. **TestProjectAdminClient_MaxCreditLimit_Halts** ... 5. **TestProjectAdminClient_GetServiceEnv_OmitsValues** ... 6. **TestProjectAdminClient_LaunchKeyValidatesAtCons
- **real:** These are proposed future e2e test names. The shipped tests landed under different names: DeleteProject_LiveCycle (not _Live), GetServiceEnvKeys_OmitsValues (not GetServiceEnv_OmitsValues), LaunchKeyRejectedAtConstruction (not LaunchKeyValidatesAtConstruction). No TestProjectAdminClient_MaxCreditLim
- **fix:** Update the four proposed test names to the actually-shipped names (DeleteProject_LiveCycle, GetServiceEnvKeys_OmitsValues, LaunchKeyRejectedAtConstruction) and drop the MaxCreditLimit_Halts entry (MaxCreditLimit is not in the shipped launch

## docs/spec-authoring-boundary.md — CLEAN (0 findings)

## docs/spec-oss-port-flow.md (2 findings)

### emitted-per-service-mode-field-retired [low/partial/mech]
- **@** §7 Verification rubric → FitCeiling, line 188-189 (removed-feature)
- **spec:** `haDeps` (the managed deps the agent MEASURED running HA) is the single source of truth for both the C6 grade and the emitted per-service `mode:` — an unmeasured dep is never force-promoted to HA.
- **real:** HA is NOT emitted as a per-service `mode:` field. The recipe YAML emitter encodes HA in the type VARIANT (e.g. `postgresql:ha@18` / `postgresql:single@18`) via topology.WithDeploymentVariant. ManagedServiceModeForTier returns an INTERNAL mode string (modeHA/modeNonHA) which is immediately converted 
- **fix:** Replace 'the emitted per-service `mode:`' with 'the emitted per-service HA type-variant (`:ha`/`:single`)'. The single-source-of-truth claim (haDeps drives both C6 and the emitted topology) is correct and should stay; only the `mode:`-field

### modemeasured-mode-emission-terminology [low/confirmed/mech]
- **@** §9 Stage B — capture & publish, line 230-231 (removed-feature)
- **spec:** `Service.ModeMeasured` (measured per-service `mode:` emission — `ManagedServiceModeForTier` is the single mode-resolution owner).
- **real:** `Service.ModeMeasured` and `ManagedServiceModeForTier` both still exist and the description of ManagedServiceModeForTier as the single mode-RESOLUTION owner is accurate (it resolves modeHA/modeNonHA for the two emit sites yaml_emitter + tier_service_deltas). BUT the phrase 'per-service `mode:` emiss
- **fix:** Reword to 'measured per-service HA-variant emission' (the emitted artifact is a `:ha`/`:single` type variant). Keep '`ManagedServiceModeForTier` is the single mode-resolution owner' — that internal resolver is accurate; only the claim that 

## docs/spec-content-surfaces.md (4 findings)

### scaffold-decision-missing-env-import-surface [medium/unverif/DECISION]
- **@** §Classification × surface compatibility (table, line 386); also §Fact classification taxonomy 'Route to' column (line 373) (behavior-model)
- **spec:** | scaffold-decision | zerops.yaml comments (config flavor), IG with the diff (code flavor) | KB, CLAUDE.md (recipe-internal flavor: all surfaces — discard or move the principle to IG) |
- **real:** compatibleSurfaces(ClassScaffoldDecision) returns THREE surfaces: SurfaceCodebaseIG, SurfaceCodebaseZeropsComments, AND SurfaceEnvImportComments (Surface 3 = env import.yaml comments). The spec's compatibility table lists only two (Surface 7 zerops.yaml comments + Surface 4 IG) and omits Surface 3. 
- **fix:** Add the env import.yaml comments surface (Surface 3) to the scaffold-decision row of the compatibility table and to the 'Route to' column of the fact-classification taxonomy, e.g. 'Config flavor → per-codebase zerops.yaml comment (Surface 7

### dangling-implementation-doc-ref [low/unverif/mech]
- **@** Intro (line 7) (dangling-ref)
- **spec:** The spec is the ground truth that the content-authoring sub-agent (see [implementation-v8.94-content-authoring.md](implementation-v8.94-content-authoring.md)) reads as part of its brief
- **real:** No file named implementation-v8.94-content-authoring.md exists anywhere in the repo (docs/, plans/, plans/archive/ — only unrelated copies inside .claude/worktrees/). The markdown link target resolves to nothing.
- **fix:** Remove the parenthetical link (the named implementation doc was never committed / has been removed), or repoint it to the actual brief that the content-authoring sub-agent reads (internal/authoring/recipe/content/briefs/...).

### claudemd-validator-overstated-checks [low/unverif/mech]
- **@** §Surface 6 — Per-codebase CLAUDE.md, 'Validator' paragraph (line 299) (behavior-model)
- **spec:** **Validator**: `validateCodebaseCLAUDE` confirms the sub-agent's output shape held — title + framing line + 2–4 H2 sections, ≥200 bytes, ≤80 lines.
- **real:** validateCodebaseCLAUDE (the finalize validator) only checks: body ≥200 bytes (claude-md-too-short), lines ≤80 (claude-md-too-long), and forbidden cross-codebase subsections (Notice). It does NOT check 'title + framing line' and does NOT check '2–4 H2 sections'. The 2-4 H2-section structural check li
- **fix:** Split the attribution: 'validateCodebaseCLAUDE (finalize) confirms ≥200 bytes, ≤80 lines, and flags legacy cross-codebase subsections as Notice; the 2–4 H2-section structural shape is enforced at record time by checkClaudeMDAll (slot_shape.

### citation-map-nonexistent-guide-ids [low/unverif/mech]
- **@** §Citation map — which topics require zerops_knowledge citation (table, lines 477,479,480) (identifier)
- **spec:** | Rolling deploys, SIGTERM, multi-replica services | `rolling-deploys` / `minContainers-semantics` | ... | | Deploy files, tilde suffix, static base | `deploy-files` / `static-runtime` | ... | | L7 balancer, `httpSupport
- **real:** The 'Guide ID' column lists minContainers-semantics, static-runtime, and l7-balancer as alternative guide IDs, but none of these are guide IDs the citation validator accepts as proof-of-citation. CitationMap resolves the topic keys 'minContainers' and 'l7-balancer' to the guide ID 'rolling-deploys' 
- **fix:** Drop the non-existent slash-alternates from the Guide ID column (keep only the actual accepted guide IDs: rolling-deploys, http-support, deploy-files). If the intent was to name topic families, move minContainers/static/l7-balancer into the
- **Codex: don't just delete — aliases ARE recognized as topic/friendly handles in refinement prose (briefs_refinement2.go:304); separate canonical guide ID from aliases.**

## docs/spec-content-quality-rubric.md (6 findings)

### embedded-rubric-retired [high/confirmed/DECISION]
- **@** Intro, lines 15-18 (removed-feature)
- **spec:** The runtime brief carries `internal/authoring/recipe/content/briefs/refinement/embedded_rubric.md` which is byte-identical to this file (synced via `go:generate`; drift caught by `TestEmbeddedRubric_MatchesSpec`).
- **real:** embedded_rubric.md was RETIRED in run-33 fix #2 and the atom file DELETED in run-34 Fix A. The file does not exist at that path (only stale copies under .claude/worktrees/ on the old internal/recipe/ path). The refinement brief now loads derived_rules.md as the sole scoring substrate. There is no go
- **fix:** Remove the byte-identical/go:generate/embedded_rubric.md/TestEmbeddedRubric_MatchesSpec paragraph entirely. If the spec is retained at all, replace with a note that the runtime substrate is now derived_rules.md + refinement2/audit_checklist

### rubric-not-load-bearing-authority [high/confirmed/DECISION]
- **@** Intro, lines 9-15 (behavior-model)
- **spec:** The rubric is the load-bearing authority for run-17 quality. The refinement sub-agent at phase 8 reads it, scores every surface, and acts on criteria below 8.5 only when the fix is unambiguous from the reference distilla
- **real:** The refinement sub-agent does NOT read this rubric. The refinement brief assembles derived_rules.md (pass 1) and refinement2/audit_checklist.md (refinement2 pass). The scoring model is rule-walk by surface, not criterion-scoring 'every surface on six criteria'. Tests explicitly assert the brief must
- **fix:** Strike 'load-bearing authority for run-17 quality', 'refinement sub-agent at phase 8 reads it, scores every surface', and 'single-source'. The current authority is derived_rules.md (rule-walk by surface) + refinement2/audit_checklist.md; re

### threshold-cite-rubric-criterion [medium/confirmed/mech]
- **@** Refinement edit threshold, lines 725-728 (stale-example)
- **spec:** Per-fragment edit threshold for the refinement sub-agent: **I can cite the violated rubric criterion, the exact fragment, and the preserving edit.** When you can name all three, ACT.
- **real:** The runtime threshold language was flipped in run-34 Fix A from 'cite the violated rubric criterion' to 'cite the violated rule' / 'cite the rule id + the exact phrase + the preserving edit'. A test FAILS if the phase-entry atom still teaches rubric-criterion scoring. The rubric-criterion framing is
- **fix:** Change 'I can cite the violated rubric criterion' to 'I can cite the violated rule' to match the shipped threshold language in synthesis_workflow.md / phase_entry/refinement.md.

### update-protocol-stale-mechanism [medium/confirmed/mech]
- **@** Updates, lines 754-756 (dangling-ref)
- **spec:** Update protocol: edit this file → `go generate` regenerates `embedded_rubric.md` → `TestEmbeddedRubric_MatchesSpec` confirms sync → ship in the next minor.
- **real:** Same retired mechanism as the intro: no go:generate directive, no embedded_rubric.md target, no TestEmbeddedRubric_MatchesSpec test. The whole sync chain described here does not exist.
- **fix:** Delete the update-protocol sentence (the sync pipeline it describes is gone). If the doc is retained as historical, replace with a note that it is no longer auto-synced into the runtime brief.

### cross-service-refs-phantom-slug [low/partial/mech]
- **@** Criterion 3, lines 244-246 (identifier)
- **spec:** the internal `zerops_knowledge` corpus slug ID (`init-commands`, `env-var-model`, `managed-services-nats`, `rolling-deploys`, `cross-service-refs`, `object-storage`, `http-support`)
- **real:** Six of the seven listed slugs are valid CitationMap guide ids, but `cross-service-refs` is NOT a CitationMap key or value. The real guide id for cross-service env topics is `env-var-model` (topic key cross-service-env). `cross-service-refs` appears nowhere in citations.go; it survives only as a phan
- **fix:** Replace `cross-service-refs` with `env-var-model` (the real guide id) in the slug list. (Same fix applies in the two atom files that repeat the phantom slug, but those are out of this spec's scope.)

## docs/spec-knowledge-architecture.md (2 findings)

### zsc-noop-mislabeled-as-spec-drift [medium/confirmed/mech]
- **@** §3.0 SCOPE/AUDIENCE table, Developer/spec row — line 62 (stale-example)
- **spec:** authoritative for DESIGN; but they restate platform facts too → governed against drift (specs have already drifted: several still say `zsc noop`)
- **real:** `zsc noop --silent` IS the platform-authoritative value for dev-mode dynamic-runtime `run.start`, NOT a drift. The spec's own §2 (line 28) documents the 2026-06-03 resolution: the prior 'omit run.start' convention was the drift, and every ZCP surface was restated TO `zsc noop --silent` (reconciled t
- **fix:** Delete the parenthetical '(specs have already drifted: several still say `zsc noop`)' or replace it with a correct example of spec-restated-platform-fact drift. `zsc noop` is the platform-authoritative value per §2's resolution — a spec sta

### boot-shim-path-notation-slash-collapsed [low/confirmed/mech]
- **@** §3.0 SCOPE/AUDIENCE table, Persistent workspace context row — line 61 (naming)
- **spec:** `agents_shared/container/local.md` (boot-shims)
- **real:** The boot-shim templates are FLAT files, not a nested path: internal/content/templates/agents_shared.md, agents_container.md, agents_local.md, agents_authoring.md. The slash form `agents_shared/container/local.md` reads as a single nested path that does not exist (no `agents_shared/` directory). The 
- **fix:** Change `agents_shared/container/local.md` to `agents_{shared,container,local}.md` (or `agents_*.md`) to match the actual flat filenames and the form already used in §3.1/§6.

## docs/spec-env-handling.md (10 findings)

### env-local-creation-unimplemented [high/unverif/DECISION]
- **@** §2 Core mental model (lines 76-80); also §7.1 lines 310-332 (removed-feature)
- **spec:** **The overlay (`.env.local`) is the user's no-touch zone.** ZCP creates it once during explicit bootstrap or adoption (seeded with detected/declared local-mode flags), then never writes again.
- **real:** No production code path ever creates or seeds `.env.local`. ZCP only READS `.env.local` (via ops.readEnvLocal in env_plan.go) as an overlay during BuildEnvPlan. The only file ZCP writes in this subsystem is the `.env` sink (atomicWriteFile to envPath). grep over internal/ + cmd/ (excluding tests) fi
- **fix:** Either mark §2/§7.1 `.env.local` creation/seeding as design-intent-not-yet-shipped (Theme 1 reservation) the way §11 is explicitly labeled 'design-only', or implement EnsureEnvLocal seeding. The spec currently asserts shipped behavior ('ZCP

### git-tracking-detection-unimplemented [medium/unverif/DECISION]
- **@** §7.5 lines 357-362 (also §9.1 'Hard warning case' line 399) (removed-feature)
- **spec:** ZCP detects if `.env.local` is git-tracked (via `git ls-files .env.local`) and surfaces a high-severity warning.
- **real:** No production code runs `git ls-files .env.local` or otherwise detects whether `.env.local` is git-tracked. No code emits the high-severity warning the spec (and the atom develop-local-env-troubleshoot.md) describe.
- **fix:** Mark §7.5 / §9.1 git-tracking detection as unimplemented/intended, or implement it. The spec asserts a shipped warning that no code produces (the atom corpus describes the warning text but nothing emits it).

### ensure-env-local-dangling-ref [high/unverif/mech]
- **@** §7.1 lines 328-332 (also §13 line 663) (dangling-ref)
- **spec:** After creation, ZCP **never writes to `.env.local`**. This is enforced by lint (only `internal/ops/env_local_overlay.go::EnsureEnvLocal` is permitted to write the file; other call sites would fail `atoms_lint`-extension 
- **real:** Neither the file `internal/ops/env_local_overlay.go` nor the function `EnsureEnvLocal` exists anywhere in the tree. No `atoms_lint`-extension (or any lint) gates `os.WriteFile` to `.env.local`. The cited single-writer enforcement does not exist.
- **fix:** Remove the `env_local_overlay.go::EnsureEnvLocal` citation and the 'enforced by lint / atoms_lint-extension' claim from §7.1 and §13 (the `.env.local contract` block), or implement the function + lint. As written these are dead references t

### ensure-env-local-tests-dangling [high/unverif/mech]
- **@** §13 `.env.local contract` (lines 659-663) (dangling-ref)
- **spec:** EnsureEnvLocal creates when absent: `TestEnsureEnvLocal_CreatesWhenAbsent`. EnsureEnvLocal refuses when present (idempotency): `TestEnsureEnvLocal_RefusesWhenPresent`. Header content stable: `TestEnsureEnvLocal_HeaderSta
- **real:** None of these three tests exist. `grep -rn 'TestEnsureEnvLocal'` over the whole tree returns nothing. The `.env.local contract` invariant block cites four tests/lints, none of which are real.
- **fix:** Delete the three `TestEnsureEnvLocal_*` lines and the single-writer-lint line from §13's `.env.local contract` block (they pin nonexistent code), or add the tests if the creation feature is implemented.

### plan-construction-test-names-wrong [medium/unverif/mech]
- **@** §13 Plan construction (lines 643-646) (dangling-ref)
- **spec:** Source precedence (project < yaml-setup < local-overlay): `TestBuildEnvPlan_SourcePrecedence`. Overlay always wins on conflict: `TestBuildEnvPlan_OverlayWinsOnConflict`. ... Setup parameter required when multiple blocks:
- **real:** Three of the five cited test names do not exist. The real tests are: `TestBuildEnvPlan_PrecedenceYAMLOverProject` (not `_SourcePrecedence`); `TestBuildEnvPlan_OverlayWinsOverYAML` + `TestBuildEnvPlan_OverlayWinsOverProject` (not `_OverlayWinsOnConflict`); `TestBuildEnvPlan_MultipleSetups_RequiresSel
- **fix:** Update §13 citations: `_SourcePrecedence`→`_PrecedenceYAMLOverProject`; `_OverlayWinsOnConflict`→`_OverlayWinsOverYAML` (+ `_OverlayWinsOverProject`); `_MultipleSetups_RequireSelection`→`_MultipleSetups_RequiresSelection`.

### preview-json-shape-fictional [medium/unverif/mech]
- **@** §6.1 Dry-run mode (lines 255-266) (stale-example)
- **spec:** {
  "plan": EnvPlan,
  "diff": {
    "added":    ["KEY1", "KEY2"],
    "modified": [{"key": "DATABASE_URL", "from": "...", "to": "..."}],
    "removed":  ["OLD_KEY"],
    "unowned":  ["MANUAL_EDIT"]
  },
  "wouldWrite": 
- **real:** The actual preview response is an `EnvDotenvResult` struct: fields path, setup, services, variables, vpnHint, omittedPlatformKeys, warnings, diff, preview, refused. There is no top-level `plan` or `wouldWrite` field. The `EnvDiff` struct has only `added`, `modified`, `unowned` — there is NO `removed
- **fix:** Replace the §6.1 example JSON with the real `EnvDotenvResult` shape: drop top-level `plan` and `wouldWrite`; drop `diff.removed` (use `unowned`); add `preview: true`. Or footnote it as schematic and reference the EnvDotenvResult/EnvDiff typ

### buildenvplan-signature-wrong [low/unverif/mech]
- **@** §3 EnvPlan primitive (line 139); EnvPlan struct lines 127-132 (identifier)
- **spec:** func BuildEnvPlan(ctx context.Context, project ProjectClient, setup string, cwd string) (*EnvPlan, error)
- **real:** Actual signature is `func BuildEnvPlan(ctx context.Context, client platform.Client, projectID string, setup string, cwd string) (*EnvPlan, error)`. The type `ProjectClient` does not exist anywhere in the tree; the real param is `platform.Client` plus a separate `projectID string`. The `EnvPlan` stru
- **fix:** Update the §3 signature to `BuildEnvPlan(ctx, client platform.Client, projectID, setup, cwd string)` and add the OmittedPlatformKeys / TouchedServiceHostnames fields to the EnvPlan struct illustration (or note the struct elides provenance f

### error-sentinel-vs-type-names [low/unverif/mech]
- **@** §3 doc-comment lines 136-138 (also §5 line 233, §6.3 line 288) (identifier)
- **spec:** Returns ErrSetupRequired when zerops.yaml has multiple setup blocks and `setup` is empty. Returns ErrRefResolveTransient when a ${svc_var} ref cannot resolve
- **real:** There are no sentinel error vars named `ErrSetupRequired` / `ErrRefResolveTransient`. The implementation returns error TYPES `*SetupRequiredError` (with an Available []string) and `*RefResolveTransientError` (with Service + Cause), detected via errors.As. The `Err*` names appear only inside a test c
- **fix:** Reword §3/§5/§6.3 to name the error TYPES `*SetupRequiredError` and `*RefResolveTransientError` (detected via errors.As), not sentinel `Err*` vars.

### output-param-not-supported [low/unverif/mech]
- **@** §12.4 Multi-target local (lines 626-632) (stale-example)
- **spec:** `generate-dotenv setup=<name> output=.env.<name>` allows simultaneous multi-target rendering ... Already supported via §5 (`output=` optional parameter).
- **real:** There is no `output` parameter on the zerops_env tool. The generate-dotenv schema params are action, serviceHostname, setup, preview, force, project, variables, skipRestart. `EnvGenerateDotenv` always writes to `<workingDir>/.env` (envPath := filepath.Join(workingDir, '.env')) — no output-path overr
- **fix:** Remove the 'Already supported via §5 (output= optional parameter)' sentence from §12.4 — §12 is a future-reservations section, so multi-target should read as unshipped, not 'already supported'.

### render-sink-dryrundiff-doc-mismatch [low/unverif/mech]
- **@** §3 lines 144-151 (behavior-model)
- **spec:** type EnvSink int
const (
    SinkDotenv      EnvSink = iota // .env file content
    SinkShellExport                 // `export KEY=VALUE` lines
    SinkDryRunDiff                  // human-readable diff vs existing .env
- **real:** The enum + Render exist, but `Render(SinkDryRunDiff)` does NOT produce a human-readable diff — it returns an error ('SinkDryRunDiff requires diff input; call DiffAgainstExisting + RenderDiff'). The diff path is a separate two-call API (DiffAgainstExisting + a RenderDiff helper), not Render. The §3 c
- **fix:** Note in §3 that SinkDryRunDiff is not renderable via Render (it needs the existing-file side-input); the diff is produced by DiffAgainstExisting + RenderDiff. Keep the enum value but correct the implied behavior.

## docs/spec-zerops-env-lifecycle.md (2 findings)

### stale-recipe-corpus-count [low/unverif/mech]
- **@** §13 (line 204) and identical claim in §10 (line 179: "23/36 recipe corpus files already do this (${db_hostname} etc.), so the corpus is production-ready") (stale-example)
- **spec:** 23/36 recipe corpus files already do this.
- **real:** The recipe corpus now has 38 .md files (40 tracked files total in git), and 25 of them contain ${...} cross-service refs — not 36 total / 23 with refs.
- **fix:** Update both §10 and §13 to '25/38 recipe corpus files' — or, since this count drifts every time a recipe is added, replace the hard ratio with a qualitative claim ('most recipe corpus files already use explicit ${host_var} refs') so it cann

### stale-reconciliation-note-env-handling-s4 [low/unverif/mech]
- **@** §11 first bullet (line 184) (dangling-ref)
- **spec:** Its §4 "API does not cleanly distinguish user from system service envs" is **too narrow** — the real blocker is API *incompleteness* (slim `/env` misses yaml-baked); should cite §6 here.
- **real:** spec-env-handling.md §4 no longer carries that standing claim. It has already been reconciled (lines 201-212): it explicitly retracts the old attribution ('Earlier this was attributed to the API being unable to distinguish ... that was too narrow. The slim service-stack/{id}/env returns typed record
- **fix:** Update §11 bullet 1 to reflect that env-handling §4 is now reconciled (retracted the 'too narrow' attribution and cites §6), or delete the bullet since the recommended fix has shipped. Same applies to §11 bullet 3 (§12.1): env-handling §12.

## docs/spec-architecture.md (6 findings)

### parsemode-example-fabricated [medium/partial/DECISION]
- **@** OK / not-OK examples — "tool boundary parses string → topology type once" — lines 188-193 (stale-example)
- **spec:** func handle(input Input) error {
    mode, err := topology.ParseMode(input.Mode)  // boundary parse
    if err != nil { return err }
    // mode is topology.Mode from here on; no more string casts.
    return engine.Upda
- **real:** topology.ParseMode does not exist anywhere in the repo, and engine.UpdateCloseMode does not exist either. The example presents a canonical boundary-parse pattern using two symbols that have no implementation; an LLM following it verbatim would fail to compile. (The cited file internal/tools/workflow
- **fix:** Replace ParseMode/UpdateCloseMode with real boundary-parse symbols (e.g. show the actual string→typed conversion used in internal/tools/workflow_close_mode.go), or mark the snippet as illustrative-pseudocode. As written it references nonexi

### wrong-arch-test-path [medium/confirmed/mech]
- **@** Intro / "The dependency rule is pinned by" — line 8 (dangling-ref)
- **spec:** `internal/architecture_test.go` — fails `go test ./...` on any
  forbidden import.
- **real:** The architecture-pinning test lives at internal/topology/architecture_test.go (package topology_test). There is no file internal/architecture_test.go.
- **fix:** Change the path to `internal/topology/architecture_test.go` (matches CLAUDE.md, which already cites the correct path).

### gitpush-unknown-nonexistent [medium/confirmed/mech]
- **@** Layer 2 — Constants list — line 59 (removed-feature)
- **spec:** `GitPushUnconfigured/Configured/Broken/Unknown`,
- **real:** GitPushState has exactly three constants: GitPushUnconfigured, GitPushConfigured, GitPushBroken. There is no GitPushUnknown; the test suite explicitly refers to it as 'the dead GitPushUnknown value' — `broken` covers the previously-configured-now-failing case.
- **fix:** Drop `Unknown` from the GitPushState constant list: `GitPushUnconfigured/Configured/Broken`.

### planmode-stage-nonexistent [medium/confirmed/mech]
- **@** Layer 2 — Aliases list — line 63 (identifier)
- **spec:** Aliases: `PlanModeStandard/Dev/Stage/Simple/LocalStage/LocalOnly`,
- **real:** aliases.go defines PlanModeStandard, PlanModeDev, PlanModeSimple, PlanModeLocalStage, PlanModeLocalOnly. There is no PlanModeStage (the plan-time vocabulary has no stage alias; ModeStage is only a per-service envelope projection, exposed via DeployRoleStage, not PlanModeStage).
- **fix:** Remove `Stage` from the PlanMode alias list: `PlanModeStandard/Dev/Simple/LocalStage/LocalOnly`. (DeployRole* listing further down — DeployRoleDev/Stage/Simple — is correct and DOES include Stage.)

### wrong-golangci-filename [low/confirmed/mech]
- **@** Intro / "The dependency rule is pinned by" — line 10 (dangling-ref)
- **spec:** `.golangci.yml::depguard` — surfaces violations during `make lint-local`.
- **real:** The lint config file is named .golangci.yaml (the .yml variant does not exist).
- **fix:** Change `.golangci.yml` to `.golangci.yaml` (two occurrences in this spec: lines 10 and 180-ish prose 'make lint-local fails on depguard' is fine; the file-name cite at line 10 is the one to fix).

### envclass-missing-from-table [low/confirmed/mech]
- **@** Cross-cutting packages table — line 147 (other)
- **spec:** | `internal/preprocess/`, `internal/schema/`, `internal/catalog/`, `internal/sync/`, `internal/init/`, `internal/update/` | Utility / cross-cutting. Each obeys "import only what you actually need from below." |
- **real:** internal/envclass/ is a real, non-trivial cross-cutting/Layer-3 package (the SDK-driven env classifier; imports internal/ops/inventory) that is absent from the cross-cutting table entirely. The catch-all utility row enumerates preprocess/schema/catalog/sync/init/update but omits envclass.
- **fix:** Add `internal/envclass/` to the cross-cutting package table (or the utility catch-all row), noting it is a Layer-3 env classifier that imports ops/.

## docs/spec-local-dev.md (9 findings)

### LD-2 [high/confirmed/mech]
- **@** §9 Guidance System, path B (Develop workflow guidance) (dangling-ref)
- **spec:** **B. Develop workflow guidance** (`buildPrepareGuide`, `buildDeployGuide`): - `buildPrepareGuide` has env with container/local branches. - `buildDeployGuide` has env parameter — uses `writeLocalWorkflow` for single-targe
- **real:** None of `buildPrepareGuide`, `buildDeployGuide`, or `writeLocalWorkflow` exist anywhere in the Go codebase. They were removed in commit c53e86b1 ("refactor(develop): replace session state machine with stateless briefing + pre-flight harness"). Develop-workflow local guidance is now atom-driven (e.g.
- **fix:** Rewrite §9 path B to reference the develop-mode local atoms (develop-local-workflow, develop-first-deploy-* , develop-close-mode-auto-*-local) instead of the deleted functions, or describe the stateless-briefing/pre-flight model that replac

### LD-1 [medium/confirmed/mech]
- **@** §9 Guidance System, path A (Bootstrap guidance) (behavior-model)
- **spec:** Active local atoms: `bootstrap-discover-local` (discover step) and `bootstrap-provision-local` (provision step). Both are route-agnostic — the `routes: [classic]` filter was removed so recipe and adopt paths also pick th
- **real:** Only `bootstrap-discover-local` is route-agnostic. `bootstrap-provision-local.md` STILL carries `routes: [classic]` in its front matter (line 5), and its title is literally "Local provision addendum (classic route)". So the claim that BOTH had the routes filter removed is false for one of the two na
- **fix:** Correct §9 to: `bootstrap-discover-local` is route-agnostic (no routes filter); `bootstrap-provision-local` remains `routes: [classic]` (its provision addendum is classic-route only). Don't say "Both are route-agnostic".

### LD-3 [medium/confirmed/mech]
- **@** §13 Decision Log, row D6 (behavior-model)
- **spec:** | D6 | zcli push positional arg (hostname) | Simpler than --service-id flag |
- **real:** The local deploy uses explicit `--service-id <id>` and `--project-id <pid>` flags, NOT a positional hostname arg. The decision rationale ("Simpler than --service-id flag") is the exact opposite of what the code does.
- **fix:** Replace D6 with the current decision: zcli push uses --service-id + --project-id flags for non-interactive (no-TTY) mode; rationale = avoids interactive scope prompt. Drop the "positional arg / simpler than --service-id" framing.

### LD-4 [medium/confirmed/mech]
- **@** §13 Decision Log, row D4 (stale-example)
- **spec:** | D4 | hostname=appstage in local ServiceMeta | Must exist on Zerops for filterStaleMetas |
- **real:** The local ServiceMeta hostname is the Zerops PROJECT NAME (project.Name), not a service hostname like "appstage". The whole point (stated in §4/§6 of this same spec and enforced in code) is that the local hostname is NEVER a live Zerops service hostname, so filterStaleMetas keeps local metas UNCONDI
- **fix:** Replace D4 with: hostname=project name in local ServiceMeta; rationale = project-keyed (never a live service hostname) so filterStaleMetas keeps it unconditionally. This matches §4/§6.

### LD-6 [medium/confirmed/mech]
- **@** §7 Env Var Bridge (identifier)
- **spec:** The MCP tool is `zerops_env action="generate-dotenv" serviceHostname="app"` (NOT `zerops_discover`)
- **real:** `serviceHostname` is now DEPRECATED for generate-dotenv; the preferred parameter is `setup`. The schema description for serviceHostname says "For generate-dotenv: deprecated — prefer the setup parameter ... Still accepted as a fallback when setup is empty; emits a deprecation warning in the result."
- **fix:** Change §7 (and §8 diagnostics) example to `zerops_env action="generate-dotenv" setup="app"`; note serviceHostname is a deprecated fallback.

### LD-5 [low/confirmed/mech]
- **@** §7 Env Var Bridge, .env Generation example (stale-example)
- **spec:** ```
# Generated by ZCP via zerops_env generate-dotenv
# VPN required: zcli vpn up <projectId>
# WARNING: Contains secrets. Do not commit.

# db (postgresql@16)
db_host=db
...
```
- **real:** The cited generation path (`ops.EnvGenerateDotenv` → `BuildEnvPlan` → `EnvPlan.Render`) produces a DIFFERENT header and has no per-service section comments. Actual renderDotenv() emits: `# Generated by ZCP from project envVariables, zerops.yaml setup <setup>, and .env.local overlay.` / `# Do not edi
- **fix:** Update the §7 example to the actual renderDotenv header (Generated by ZCP from project envVariables / zerops.yaml setup / .env.local overlay; Do not edit directly; edit .env.local for overrides) and drop the fabricated per-service `# db (po

### LD-7 [low/confirmed/mech]
- **@** §5 Deploy Architecture, DeployLocalInput (stale-example)
- **spec:** ```go
type DeployLocalInput struct {
    TargetService string  // Zerops service hostname
    WorkingDir    string  // Local path (default: ".")
    Strategy      string  // "" (default zcli push) | "git-push"
}
```
- **real:** The actual struct has six fields: TargetService, Setup, WorkingDir, Strategy, RemoteURL, Branch, BreakGlass (FlexBool). The spec shows only three and omits Setup, RemoteURL, Branch, BreakGlass.
- **fix:** Add Setup, RemoteURL, Branch, BreakGlass to the §5 struct illustration (or note it shows core fields only). RemoteURL/Branch support strategy=git-push; Setup selects the zerops.yaml setup block; BreakGlass overrides the L1 push-delivery red

### LD-8 [low/confirmed/mech]
- **@** §5 Deploy Architecture, ops.DeployLocal() step 4 (identifier)
- **spec:** 4. Run `ValidateZeropsYml(workingDir, targetService)` with local path
- **real:** ValidateZeropsYml's actual signature is `ValidateZeropsYml(workingDir, targetHostname, serviceType string, class DeployClass, roles ...topology.DeployRole)` — four-plus arguments, called with the resolved setupName, the resolved serviceType, and DeployClassCross, not a two-arg `(workingDir, targetSe
- **fix:** Update step 4 to the real signature/arguments (workingDir, setupName, serviceType, DeployClassCross) and add the .deployignore lint + pre-deploy API validation steps before login.

### LD-9 [low/confirmed/mech]
- **@** §6 ServiceMeta struct comment (removed-feature)
- **spec:** CloseDeployMode          CloseDeployMode  `json:"closeDeployMode,omitempty"`           // unset | auto | git-push | manual
- **real:** `git-push` is a LEGACY close-mode value that folds one-way to `auto` at every meta read AND write (foldLegacyCloseMode). Nothing reads it post-fold; the close-mode list handler offers only {auto, manual}. Presenting `git-push` as a live valid close-mode value alongside auto/manual (without the legac
- **fix:** Change the §6 inline comment to `// unset | auto | manual (legacy git-push folds to auto at parse)` to match the shipped fold semantics; the §11 prose already describes the fold correctly.

## docs/spec-scenarios.md (8 findings)

### SC-4 [medium/confirmed/DECISION]
- **@** §6.4 (line 330), §6.6 (line 352), §6.7 (line 357), §6.11 (line 388) — error-code citations (identifier)
- **spec:** - **Response**: error `STATE_CORRUPT` with path. ... - **Response**: error `CONFIG_MISSING`. ... - **Response**: error `ZCLI_MISSING` with install instructions. ... - **Response**: error `NO_DEPLOY_FOR_MANAGED`.
- **real:** None of these four error codes exist in the catalog. Actual codes: state-corruption is `WORK_SESSION_CORRUPT`; the zcli-missing path returns `PREREQUISITE_MISSING`; there is no `CONFIG_MISSING`, `ZCLI_MISSING`, or `NO_DEPLOY_FOR_MANAGED` code at all.
- **fix:** Replace with real catalog codes: §6.4 STATE_CORRUPT→WORK_SESSION_CORRUPT; §6.6 CONFIG_MISSING→the actual zerops.yaml-missing code (FILE_NOT_FOUND / INVALID_ZEROPS_YML — verify the local-deploy path); §6.7 ZCLI_MISSING→PREREQUISITE_MISSING; 

### SC-6 [medium/confirmed/DECISION]
- **@** §6.2 (line 320) and §6.4 (line 331) — zerops_manage actions (removed-feature)
- **spec:** **Plan.Primary** = `{zerops_manage action=bind-project args={id|name}, rationale: ...}` ... **Plan.Primary** = `{zerops_manage action=reset-state args={confirm: true}, rationale: ...}`
- **real:** `zerops_manage` exposes only `start, stop, restart, reload, connect-storage, disconnect-storage`. There is no `bind-project` and no `reset-state` action. State reset is `zerops_workflow action="reset"`.
- **fix:** §6.2: replace with the real project-binding path (token scope / zerops_discover) or drop the manage-action form. §6.4: change to `zerops_workflow action="reset"`.

### SC-2 [high/confirmed/mech]
- **@** §8.2 (line 484) — 'Existing container env, mixed services' rendered status walkthrough (behavior-model)
- **spec:** - laravelstage (php-nginx@8.3): mode=stage, closeDeployMode=git-push, gitPushState=configured, stage-of=laraveldev
- **real:** The rendered status line uses fields `closeMode=` and `gitPush=` (not `closeDeployMode=`/`gitPushState=`), and the value `git-push` can never appear: `foldLegacyCloseMode` rewrites CloseModeGitPush→CloseModeAuto at every meta read. A configured stage half renders `closeMode=auto gitPush=configured b
- **fix:** Change the laravelstage line to `closeMode=auto, gitPush=configured, buildIntegration=<...>, stage=laraveldev` (folded value + real field names), matching renderBootstrappedFields output.

### SC-1 [medium/confirmed/mech]
- **@** §2.1 (line 117), §2.2 (line 129), §2.3 (line 149) — bootstrap close-step atom columns (dangling-ref)
- **spec:** | `close` | `route=recipe, step=close` | `zerops_workflow action=close workflow=bootstrap` | `bootstrap-recipe-close`, `bootstrap-write-metas` | ... | `bootstrap-close`, `bootstrap-write-metas` | ... | `bootstrap-close`,
- **real:** Atom `bootstrap-write-metas` was DELETED; its one paragraph was folded into `bootstrap-close`. The atom no longer exists in the corpus.
- **fix:** Remove `bootstrap-write-metas` from the close-step atom columns in §2.1, §2.2, §2.3 (its content now lives inside `bootstrap-close`, already listed alongside it in §2.2/§2.3).

### SC-3 [medium/confirmed/mech]
- **@** §1.3 (line 73) — 'All services bootstrapped' envelope (behavior-model)
- **spec:** **Envelope**: `Services=[{laraveldev, mode=dev, closeDeployMode=auto}, {laravelstage, mode=stage, closeDeployMode=git-push, gitPushState=configured}, {db, managed}]`.
- **real:** A service's CloseDeployMode can never be `git-push` in any computed envelope — it is folded to `auto` at meta read. The configured stage half is `closeDeployMode=auto` with `gitPushState=configured`; delivery is derived from GitPushState, not stored as a close-mode.
- **fix:** Change the laravelstage envelope entry to `{laravelstage, mode=stage, closeDeployMode=auto, gitPushState=configured}` so the example reflects the post-fold model.

### SC-5 [medium/confirmed/mech]
- **@** §6.10 (line 382) and §3.2 prose (line 213) — local-only closeMode=auto rejection (identifier)
- **spec:** **Response**: error `PREREQUISITE_MISSING` from `workflow_close_mode.go` with hint pointing at `action="adopt-local"` ... `auto` for `local-only` ... `workflow_close_mode.go` rejects with `PREREQUISITE_MISSING`
- **real:** The local-only `closeMode=auto` rejection returns `INVALID_PARAMETER` (ErrInvalidParameter), not `PREREQUISITE_MISSING`.
- **fix:** Change the cited code from `PREREQUISITE_MISSING` to `INVALID_PARAMETER` in both §6.10 and §3.2 prose (the adopt-local hint text is correct).

### SC-7 [low/confirmed/mech]
- **@** §3.6 (line 265) — explicit close (user-initiated mid-work) (identifier)
- **spec:** **Envelope**: `Phase=idle, WorkSession.CloseReason=user-close`.
- **real:** There is no `user-close` CloseReason value. An LLM-initiated `action=close` records `CloseReasonExplicit = "explicit"`.
- **fix:** Change `CloseReason=user-close` to `CloseReason=explicit` in §3.6.

### SC-8 [low/confirmed/mech]
- **@** §3.1 (line 190) and §3.3 table (lines 237, 242) — deploy plan arg name (identifier)
- **spec:** **Plan.Primary** = `{zerops_deploy, args: {hostname: first-service}, ...}` ... | pre-deploy | `zerops_deploy args={hostname}` | ... | failed deploy, any iter | `zerops_deploy args={hostname}` |
- **real:** The deploy Plan.Primary arg key is `targetService`, not `hostname`. (The verify action uses `serviceHostname`.)
- **fix:** Change the deploy-plan arg notation from `{hostname}` to `{targetService}` in §3.1 and the §3.3 table rows; keep `serviceHostname` for the verify rows (the spec already uses `serviceHostname` correctly in §3.3's verify-pending row and S6).

## docs/schema-integration.md (5 findings)

### SI-5 [medium/confirmed/DECISION]
- **@** docs/schema-integration.md §"1. Recipe plan validation" (lines 20-30) (behavior-model)
- **spec:** When the LLM submits a `RecipePlan` after the research step, we validate against schema enums: | `runtimeType` | ... | `buildBases[]` | zerops.yaml `build.base` enum | ... | `targets[].type` | ...
- **real:** There is no `RecipePlan` type with `runtimeType`/`buildBases[]` fields and no 'research step' in internal/workflow — these reference the retired recipe-v2 flow. The current bootstrap model uses BootstrapTarget/RuntimeTarget with Type/StageType, validated by HasServiceType (TYPE only). buildBases[] v
- **fix:** Rewrite §1 to the current model: (a) the bootstrap/recipe route validates submitted target service TYPES (BootstrapTarget.Type/StageType) against the import.yaml service-type enum via schemas.HasServiceType in internal/workflow/validate.go;

### SI-1 [high/confirmed/mech]
- **@** docs/schema-integration.md §"2. Import pre-flight validation" (line 41) (removed-feature)
- **spec:** | `services[].mode` | Must be `HA` or `NON_HA` | Only validated when present (required for managed services only) |
- **real:** zerops_import does NO client-side mode validation. The import handler is documented as taking no client-side validator at all — the Zerops API is the sole validator. The schema's Modes enum field is extracted+cloned but never read for validation anywhere in the codebase. `services[].mode` itself is 
- **fix:** Remove the `services[].mode` row from the Import pre-flight validation table. There is no client-side mode validation, and `mode:` itself is retired (HA is now a type variant `postgresql:ha@16`, not a `mode:` field). State that mode/HA vali

### SI-2 [high/confirmed/mech]
- **@** docs/schema-integration.md §"2. Import pre-flight validation" (line 42) (removed-feature)
- **spec:** | `services[].objectStoragePolicy` | Must be one of 5 valid policies | Only validated when present (only relevant for `object-storage`) |
- **real:** No client-side objectStoragePolicy validation exists. The schema's StoragePolicies enum is extracted (schema.go:141) and cloned in the active filter (active_filter.go:142) but is never read by any validator. The import handler explicitly does no client-side field validation (import.go:74-78).
- **fix:** Remove the `services[].objectStoragePolicy` row from the Import pre-flight validation table (and likely the whole '2. Import pre-flight validation' subsection, since neither claimed check exists). objectStoragePolicy is validated server-sid
- **Codex: SCOPE to 'not in zerops_import' — objectStoragePolicy IS still client-validated in export/launch structure validation (validate_structure_test.go:38). Don't write 'no client-side validation exists'.**

### SI-4 [high/confirmed/mech]
- **@** docs/schema-integration.md §"Code locations" (line 65) (dangling-ref)
- **spec:** | `internal/workflow` | `recipe_validate.go` | Validate recipe plans against build/run base enums |
- **real:** internal/workflow/recipe_validate.go does not exist. Build/run-base enum validation (CheckZeropsBasesLive, using HasBuildBase/HasRunBase) lives in internal/schema/validate_bases.go and is invoked ONLY from the authoring domain (internal/authoring/recipe/validators_zerops_yaml_schema.go), not from an
- **fix:** Replace the dangling `internal/workflow`/`recipe_validate.go` row with the real locations: `internal/schema`/`validate_bases.go` (`CheckZeropsBasesLive` — build/run base enum membership), invoked from `internal/authoring/recipe/validators_z

### SI-3 [medium/confirmed/mech]
- **@** docs/schema-integration.md §"2. Import pre-flight validation" (line 43) (behavior-model)
- **spec:** Service type validation uses the schema-derived catalog (`schemas.HasServiceType`, equivalence-aware) — the stack-types API it formerly fell back to is deleted.
- **real:** zerops_import does NOT do client-side service-type validation via HasServiceType. The import handler takes no client-side type catalog at all. HasServiceType-based catalog validation exists, but in the BOOTSTRAP workflow path (internal/workflow/validate.go catalogTypeErrors), not in the zerops_impor
- **fix:** Either move this sentence under the bootstrap/recipe-plan validation context (where HasServiceType is actually used) or rewrite the Import section to state that zerops_import performs NO client-side enum validation — all field/type/mode/pol

## docs/spec-headless.md (4 findings)

### claudemd-only-model-stale [high/confirmed/mech]
- **@** §Required setup: `zcp init`, lines 12-16 (behavior-model)
- **spec:** `zcp serve` requires `CLAUDE.md` to be present in the working directory the agent will operate from. Without it, the agent has only tool descriptions — workflow doctrine, the canonical `status` recovery primitive, and SS
- **real:** The agent-context model is now AGENTS.md (canonical body holding the doctrine) + CLAUDE.md (a thin `@AGENTS.md` include wrapper, because Claude Code reads only CLAUDE.md). The startup warning fires only when BOTH AGENTS.md AND CLAUDE.md are missing; either present → silent. The spec describes a CLAU
- **fix:** Rewrite §Required setup and §Verifying to state that `zcp init` writes AGENTS.md (canonical doctrine body) + CLAUDE.md (thin @AGENTS.md wrapper), and that doctrine is missing only when BOTH files are absent. Doctrine lives in AGENTS.md; CLA

### warning-string-mismatch [high/confirmed/mech]
- **@** §Verifying, lines 36-39 (quoted warning block) (stale-example)
- **spec:** WARNING: no CLAUDE.md in working directory; MCP-only mode delivers no
workflow doctrine. Run `zcp init` here first for full agent guidance.
- **real:** The actual emitted warning string reads "WARNING: no AGENTS.md or CLAUDE.md in working directory; MCP-only mode delivers no workflow doctrine. Run `zcp init` here first for full agent guidance." — it names both files, and only fires when both are absent.
- **fix:** Update the quoted warning block to the verbatim current string: "WARNING: no AGENTS.md or CLAUDE.md in working directory; MCP-only mode delivers no workflow doctrine. Run `zcp init` here first for full agent guidance."

### dangling-refresh-claude-ref [high/confirmed/mech]
- **@** §Why not auto-inject doctrine via MCP init, lines 53-56 (dangling-ref)
- **spec:** A long-lived container's CLAUDE.md is also auto-refreshed when the embedded template changes between releases (see `internal/content/refresh_claude.go`); operators only need to run `zcp init` once per working directory.
- **real:** There is no `internal/content/refresh_claude.go` file (not present on disk and not git-tracked). The auto-refresh-on-serve-start logic lives in `internal/content/refresh_agents.go` (function `RefreshAgentContext`), which refreshes BOTH AGENTS.md and CLAUDE.md — not just CLAUDE.md — and runs for cont
- **fix:** Replace the citation with `internal/content/refresh_agents.go` (`RefreshAgentContext`), and note that it refreshes both AGENTS.md and CLAUDE.md and runs in both local and container envs (not container-only).

### container-init-git-identity-false [medium/confirmed/mech]
- **@** §Required setup: `zcp init`, lines 27-29 (removed-feature)
- **spec:** Container env additionally writes SSH config, git identity, and a global Claude Code MCP entry.
- **real:** Container `zcp init` writes SSH config and a global Claude Code MCP entry (~/.claude.json via the claude adapter ContainerInit), but it does NOT write git identity. The container init path explicitly performs no git setup; git initialization is ops.InitServiceGit's job at bootstrap time, not zcp ini
- **fix:** Remove "git identity" from the container-env write list. Leave "SSH config" and "a global Claude Code MCP entry" (~/.claude.json). Optionally note git setup happens at bootstrap (ops.InitServiceGit), not at zcp init.