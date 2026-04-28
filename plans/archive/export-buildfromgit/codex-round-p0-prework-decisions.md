# Codex PRE-WORK round — decisions + integrity (agent A)

Date: 2026-04-28
Plan SHA at round time: `b743cda0`

## Verdict: NEEDS-REVISION

The plan is directionally sound but must be revised on three blockers before implementation:

1. It omits the standalone `zerops_export` MCP tool surface
2. It understates schema-validation work in Phase 5
3. Phase 4 atom replacement conflicts with existing tests that pin exactly one `export` atom

The deploy-decomp tri-axis model (CloseDeployMode/GitPushState/BuildIntegration) does not invalidate §4 decisions, but it changes how export atoms with `gitPushStates` front-matter are rendered — `SynthesizeImmediatePhase` passes only phase+environment, no services, so service-scoped axes would silently never fire.

## Key findings by section

### Rendering pipeline

`internal/workflow/synthesize.go:51,76–80` confirms multiple atoms compose cleanly. But `SynthesizeImmediatePhase` at `:524–525` passes no service objects, so any export atom using `gitPushStates` axis would never be selected. Phase 4 front-matter shape (`closeDeployModes`/`gitPushStates`/`buildIntegrations`) is already parsed at `internal/workflow/atom.go:201,207,213`.

**Implication**: Phase 4's `export-publish-needs-setup.md` proposed front matter `gitPushStates: [unconfigured, broken, unknown]` would NEVER fire under `SynthesizeImmediatePhase`. The handler must inject the GitPushState dimension into the synthesize call (e.g., a richer envelope path) OR rely on handler-side composition (return the chain pointer in the response payload, not via atom selection).

### Corpus fixture gotcha

`internal/workflow/corpus_coverage_test.go:778` pins `MustContain: ["import.yaml"]`; Phase 4 renames the output file to `zerops-project-import.yaml`. The plan's §6 Phase 4 test list says "replace export fixtures" but does not call out this specific token. This is a hidden gotcha.

### Critical gaps

**zerops_export coexistence.** `zerops_export` tool is still registered at `internal/server/server.go:197` and `internal/tools/export.go:19–35`. The plan must explicitly retain (raw platform export), repurpose, or deprecate it.

**Schema validation complexity.** For Phase 5 validation, no JSON Schema library is vendored; current schema work (`internal/schema/validate.go:54–57`) only does unknown-field detection against extracted enums, not full JSON Schema validation. Phase 5 effort is materially understated.

## Citation amendments

- Plan cites `workflow.go:142` for tool route; actual is `internal/tools/workflow.go:144` (IsImmediateWorkflow) / `:150` (synthesis call)
- Plan cites `corpus_coverage_test.go:768`; actual fixture is `:766` with stale `import.yaml` token at `:778`

## Effective verdict

**NEEDS-REVISION** — amendments addressable in-place; once addressed converges to APPROVE.

## Required amendments before Phase 1

1. **Add explicit handling for `zerops_export` coexistence** in plan §1 (gap row) and §6 Phase 3 (decision: retain as raw export, do NOT route through workflow handler).

2. **Reshape Phase 5 work-scope** to acknowledge no JSON Schema lib is vendored. Either:
   - Add a Phase 4.5 step to vendor `github.com/santhosh-tekuri/jsonschema/v5` (or chosen lib), OR
   - Hand-roll per-rule validation against a curated set of enums + required fields.

3. **Resolve service-scoped axis rendering**. Either:
   - Drop `gitPushStates` axis from `export-publish-needs-setup.md` and rely 100% on handler-side chain composition (recommended — simpler, matches close-mode pattern), OR
   - Extend `SynthesizeImmediatePhase` to accept a service context so service-scoped axes fire.

4. **Update `corpus_coverage_test.go:778` `MustContain`** in Phase 4: replace `"import.yaml"` with `"zerops-project-import.yaml"`. Already implicit but call out explicitly.

5. **Citation hygiene**: update §5.2 entry-points list with corrected line numbers per "Citation amendments" above.
