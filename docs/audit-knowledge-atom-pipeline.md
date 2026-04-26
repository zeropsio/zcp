# Audit: Knowledge Atom Pipeline

Date: 2026-04-25

Scope: runtime knowledge atoms, the path that turns them into Claude-facing
tool responses, and the adjacent install-time `CLAUDE.md` generation path.

This document is self-contained handoff material for a follow-up session. It
does not implement fixes.

## Executive Summary

The atom architecture is fundamentally sound: runtime guidance is authored as
small axis-tagged markdown atoms, filtered against a typed `StateEnvelope`, and
rendered into MCP tool responses. This is the right shape for LLM workflow
guidance because it is deterministic, testable, compaction-safe, and avoids
duplicating the same operational fact across multiple prose files.

The current weakness is not the model. The weakness is enforcement. A few
production paths still bypass or soften the atom pipeline:

- some atom synthesis errors are swallowed and converted to empty guidance;
- non-push-git strategy guidance reads raw `atom.Body` instead of calling
  `Synthesize`;
- optional frontmatter parsing is tolerant in a way that can turn malformed
  axes into wildcards;
- at least one atom asks for context (`{slug}`) that the synthesizer does not
  know how to substitute or allow.

The root-cause fix should make the atom pipeline authoritative, not patch each
symptom independently.

## Intended Architecture

Main runtime flow:

```text
internal/content/atoms/*.md
  -> content.ReadAllAtoms()
  -> workflow.LoadAtomCorpus()
  -> workflow.Synthesize(StateEnvelope, corpus)
  -> workflow.RenderStatus(...)
  -> zerops_workflow MCP response
  -> Claude / agent context
```

Key contract from `docs/spec-knowledge-distribution.md`:

- Every workflow-aware tool response should be based on
  `ComputeEnvelope -> BuildPlan -> Synthesize`.
- Atom filtering is driven by typed axes such as phase, environment, mode,
  strategy, runtime, route, step, deploy state, service status, trigger, and
  idle scenario.
- Service-scoped axes must match on the same service.
- Placeholder substitution and unknown-placeholder checking happen inside
  `Synthesize`.
- Recipe authoring is intentionally separate from the runtime atom pipeline.

Install-time `CLAUDE.md` generation is adjacent, not the same path:

```text
internal/content/templates/claude_shared.md
internal/content/templates/claude_container.md
internal/content/templates/claude_local.md
  -> content.BuildClaudeMD(runtime.Info)
  -> init.generateCLAUDEMD(...)
  -> disk CLAUDE.md managed block
```

That template path gives Claude static project-entry guidance. Runtime atoms
give per-state tool-response guidance.

## Corpus Inventory Reviewed

Reviewed:

- `internal/content/atoms/*.md`: 76 atom files, 3130 total lines.
- Atom parser and synthesizer:
  - `internal/workflow/atom.go`
  - `internal/workflow/synthesize.go`
  - `internal/content/content.go`
- Tool response entry points:
  - `internal/tools/workflow.go`
  - `internal/tools/workflow_develop.go`
  - `internal/tools/workflow_strategy.go`
  - `internal/tools/workflow_immediate.go`
- Bootstrap guide assembly:
  - `internal/workflow/bootstrap_guide_assembly.go`
- `CLAUDE.md` generation:
  - `internal/content/build_claude.go`
  - `internal/content/templates/claude_*.md`
  - `internal/init/init.go`
- Relevant tests and specs:
  - `docs/spec-knowledge-distribution.md`
  - `internal/workflow/synthesize_test.go`
  - `internal/workflow/corpus_coverage_test.go`
  - `internal/workflow/atom_reference_field_integrity_test.go`
  - `internal/workflow/atom_references_atoms_integrity_test.go`
  - `internal/content/atoms_lint.go`
  - `internal/content/atoms_lint_test.go`
  - `internal/content/content_test.go`
  - `internal/content/atoms_test.go`
  - `internal/tools/workflow_strategy_test.go`

Targeted tests run during audit:

```sh
go test ./internal/workflow -run 'TestSynthesize|TestCorpus|TestLoadAtomCorpus|TestWorkflowScenarios' -count=1 -short
go test ./internal/content -run 'TestAtom|TestReadAllAtoms|TestBuildClaude' -count=1 -short
go test ./internal/tools -run 'TestBuildStrategyGuidance|TestWorkflow.*Strategy|TestGuidance|TestKnowledge' -count=1 -short
```

All passed. The findings below are therefore coverage gaps or architectural
contract gaps, not failures currently caught by the suite.

## Findings

### 1. High: recipe bootstrap close can lose all guidance silently

Where:

- `internal/content/atoms/bootstrap-recipe-close.md`
- `internal/workflow/synthesize.go`
- `internal/workflow/bootstrap_guide_assembly.go`

Evidence:

`bootstrap-recipe-close.md` contains:

```md
zerops_workflow action="complete" step="close" attestation="Recipe {slug} bootstrapped — services active and verified"
```

`Synthesize` treats `{...}` tokens as placeholders. It only substitutes:

- `{hostname}`
- `{stage-hostname}`
- `{project-name}`

and allows a fixed set of surviving agent-filled placeholders. `{slug}` is not
in that allowlist.

Result: a recipe close synthesis that matches this atom returns:

```text
atom bootstrap-recipe-close: unknown placeholder "{slug}" in atom body
```

`BootstrapState.buildGuide` currently handles synthesis errors by returning an
empty string:

```go
bodies, err := Synthesize(envelope, corpus)
if err != nil {
    return ""
}
```

What breaks:

When the recipe bootstrap close atom is selected, the agent can receive no
close guidance at all instead of a visible error. The tool response looks like
a valid state with missing guidance, which is worse than failing loudly.

Additional content drift in the same atom:

```md
points at the primary follow-ups: `develop` (...) and `cicd` (...)
```

`cicd` is retired in favor of `action="strategy"` / push-git setup atoms.

Fundamental assessment:

This is not just a bad placeholder. It shows that recipe-specific dynamic
context leaked into the runtime atom corpus without being modeled in
`StateEnvelope` or explicitly allowed as an agent-filled placeholder. It also
shows that a knowledge synthesis failure can disappear below the tool boundary.

Recommended fix:

- Decide whether recipe slug belongs in the envelope for bootstrap recipe
  close. If yes, add a typed field and substitute it.
- If not, remove `{slug}` from this runtime atom and phrase the attestation
  without a placeholder.
- Replace retired `cicd` wording with the current strategy/push-git entry.
- Make atom load/synthesis failures fatal at the response boundary. Do not
  return empty guidance for corpus or synthesis errors.

### 2. High: non-push-git strategy guidance bypasses the atom pipeline

Where:

- `internal/workflow/strategy_guidance.go`
- `internal/tools/workflow_strategy.go`

Evidence:

`BuildStrategyGuidance` directly appends raw atom bodies:

```go
parts := make([]string, 0, len(matched))
for _, atom := range matched {
    parts = append(parts, atom.Body)
}
```

This bypasses:

- placeholder substitution;
- unknown-placeholder checking;
- service-scoped conjunction via `atomMatches`;
- environment filtering via `StateEnvelope`;
- future behavior added to `Synthesize`.

The tool wrapper also drops errors:

```go
func buildStrategyGuidance(strategies map[string]string) string {
    g, _ := workflow.BuildStrategyGuidance(strategies)
    return g
}
```

What breaks:

For a call such as:

```text
zerops_workflow action="strategy" strategies={"appdev":"push-dev"}
```

the returned guidance may contain literal commands with `{hostname}` or
`{stage-hostname}` instead of the actual service names. If corpus loading
fails, guidance can silently disappear.

Fundamental assessment:

This is the clearest architectural breach. An atom should not mean "markdown
that any caller can render however it wants". An atom should mean "input to the
synthesizer". Once production code reads `atom.Body` directly, the central
invariants of the atom model no longer dominate the system.

Recommended fix:

- Remove raw atom rendering from production.
- Build a minimal `StateEnvelope` from the target service metas for push-dev
  and manual strategy updates, then call `Synthesize`.
- Propagate errors to the MCP response as platform errors, mirroring immediate
  workflow handling.
- Add an architecture test that rejects production uses of `atom.Body` outside
  parser/synthesizer/test code.

### 3. Medium: malformed optional atom axes become wildcards

Where:

- `internal/workflow/atom.go`

Evidence:

`parseYAMLList` returns `nil` for any non-empty value that is not in inline
list form:

```go
if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
    return nil
}
```

For optional axes, `nil` means wildcard. Examples:

```yaml
environments: local
strategies: push-dev
serviceStatuses: [READY_TO_DEPLOY]
```

These would parse without an error. The first two become unfiltered wildcard
axes. The third is an unknown key and therefore ignored entirely because the
parser only reads `serviceStatus`.

What breaks:

A safety rule intended for one environment, strategy, route, or service status
can accidentally broadcast more widely than intended. This kind of bug is
hard to catch by reading rendered output because the atom still parses and
may still appear in many fixtures.

Fundamental assessment:

Atom corpus is author-time configuration, not user input. Tolerant parsing is
the wrong tradeoff here. Invalid metadata should fail at test/build time.
Defaults are acceptable only when the default is semantically harmless. Here,
the default is often "match everything".

Recommended fix:

- Make non-empty malformed list values an error.
- Validate all frontmatter keys against a known set.
- Validate enum values for phases, environments, modes, strategies, triggers,
  runtimes, routes, deploy states, service status conventions, and idle
  scenarios.

## Architectural Evaluation

### What is good

The foundation is good. The typed envelope and atom synthesizer are exactly
the right primitive for per-turn LLM guidance:

- deterministic output for the same envelope;
- no runtime filesystem dependency for atom prose;
- strong separation between observable surface and hidden implementation
  state;
- service-scoped axis conjunction prevents cross-service false positives;
- atom reference tests and prose linting reduce drift.

The explicit separation of recipe authoring from runtime atoms is also
reasonable. Recipe authoring is a long-form content production workflow, not
short state-dependent runtime guidance.

### What is weak

The atom pipeline is not yet authoritative. There are still bypasses and soft
failure modes. This weakens the primary value of the design: one path, one
renderer, one set of invariants.

The biggest smell is silent failure. Knowledge guidance is safety-critical
for an agent. If it fails to render, the system should stop with a precise
error. Empty guidance should mean "there is intentionally nothing to say",
not "the knowledge engine failed".

The second smell is parser tolerance. Because optional axes are filters, a
parse failure becoming `nil` changes behavior from "narrow" to "broad". That
is a high-risk default.

The third smell is dynamic context ambiguity. `{slug}` in a runtime atom shows
that an atom wanted context not present in the envelope. Every placeholder
should be one of:

- substituted from typed envelope data;
- explicitly agent-filled and whitelisted;
- removed from atom prose.

No fourth category should exist.

## Recommended Structural Fix

Do not treat the findings as three isolated patches. The root-cause phase
should be:

```text
Make the atom pipeline authoritative.
```

Four phases, each a separately verifiable commit. Sequencing matters: until
errors propagate (Phase 1), RED tests in later phases pass-but-fail because
synthesis failures stay invisible.

### Phase 1 — error semantics (must come first)

- Change `BootstrapState.buildGuide` from `string` to `(string, error)` and
  propagate the error through the workflow tool handlers as a platform error.
- Stop discarding `BuildStrategyGuidance`'s error in
  `tools/workflow_strategy.go::buildStrategyGuidance` (`g, _ := ...`).
- RED tests that force `LoadAtomCorpus` failure and unknown-placeholder
  failure, then assert the tool response surfaces the error rather than
  returning empty guidance.

### Phase 2 — production rendering through Synthesize only

- Replace raw `atom.Body` rendering in `BuildStrategyGuidance` with a
  `StateEnvelope` plus `Synthesize`/`SynthesizeImmediateWorkflow`.
  Design question to settle in this phase: the minimal envelope shape
  (services from target metas, phase=develop-active, strategy axis) that
  push-dev and manual strategy updates need.
- Add an architecture test that rejects production uses of
  `KnowledgeAtom.Body` outside the parser/synthesizer/test code.

### Phase 3 — placeholder contract and recipe boundary

- Decide `{slug}`: add `Slug` to the bootstrap envelope (the recipe match is
  already on `BootstrapState.RecipeMatch`) and substitute, or remove the
  placeholder from `bootstrap-recipe-close.md`. Adding is the cleaner call —
  the attestation reads better with the slug and the data already lives in
  state.
- Drop the retired `cicd` follow-up wording from `bootstrap-recipe-close.md`
  and replace with the current `action="strategy"` / push-git entry.
- Audit every `{...}` token in `internal/content/atoms/*.md`. Each must be
  (a) substituted from typed envelope data, (b) explicitly whitelisted in
  `allowedSurvivingPlaceholders`, or (c) removed. No fourth category.
- Coverage test: every embedded atom synthesizes against at least one
  representative envelope without unknown-placeholder errors.

### Phase 4 — strict atom metadata parser

- `parseYAMLList` returns an error for non-empty values that are not bracketed
  inline-list syntax (no more silent wildcards).
- Validate all frontmatter keys against a known set; unknown key is a load
  error.
- Validate enum values per axis (phases, environments, modes, strategies,
  triggers, runtimes, routes, deploy states, service-status conventions,
  idle scenarios).

This phase is likely to surface existing atom typos in the corpus. Fix
forward — pre-production, no compatibility shims.

## Suggested Acceptance Tests

Add tests with these properties:

- Every embedded atom ID can be matched by at least one fixture envelope and
  synthesized without error.
- No production package outside `workflow` parser/synthesizer code reads
  `KnowledgeAtom.Body` directly.
- `BootstrapState.buildGuide` fails or exposes an error when atom synthesis
  fails; it must not return empty guidance for synthesis errors.
- `handleStrategy` for push-dev/manual returns guidance with real hostnames,
  not `{hostname}` placeholders.
- Invalid atom frontmatter such as `environments: local` fails
  `LoadAtomCorpus`.
- Unknown frontmatter keys fail `LoadAtomCorpus`.

## Residual Risk

The current test suite is broad and mostly aligned with the design, but it
does not prove pipeline completeness. It proves many specific scenarios. The
next step should add architectural tests that protect the invariants directly,
because the most serious issues found here are bypasses and silent failure
paths rather than missing scenario assertions.

