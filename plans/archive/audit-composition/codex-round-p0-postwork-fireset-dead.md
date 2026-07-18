# Codex round: Phase 0 POST-WORK fire-set DEAD walk (2026-04-26)

Round type: POST-WORK per §10.1 P0 row 3
Reviewer: Codex
Inputs read: CLAUDE.md:1-220; CLAUDE.local.md:1-130; internal/content/atoms/bootstrap-recipe-close.md:1-26; internal/workflow/synthesize.go:1-180,250-430; internal/workflow/compute_envelope.go:1-560; internal/workflow/bootstrap_guide_assembly.go:1-230; internal/workflow/engine.go:320-760; internal/workflow/bootstrap_steps.go:1-32; internal/workflow/bootstrap.go:1-270; internal/tools/workflow.go:1-850; internal/tools/workflow_bootstrap.go:1-190; internal/workflow/scenarios_test.go:1-180; internal/workflow/corpus_coverage_test.go:1-190,430-550; plans/audit-composition/fire-set-stderr-2026-04-26.txt:1-12

> **Artifact write protocol note (carries over from rounds 1+2).** Codex
> sandbox blocked the in-agent write; this artifact was reconstructed
> verbatim from Codex's text response. The grep-evidence and file:line
> citations were produced by Codex; Claude verified by spot-check before
> saving.

## bootstrap-recipe-close — DEAD or content-bug?

### ComputeEnvelope walk

`ComputeEnvelope` does not set `StateEnvelope.Bootstrap`: the file says
`StateEnvelope.Bootstrap` is populated by bootstrap guide assembly,
not `ComputeEnvelope`, and `ComputeEnvelope` leaves it nil
(`internal/workflow/compute_envelope.go:16`,
`internal/workflow/compute_envelope.go:115`).

The actual `BootstrapSessionSummary` site is `synthesisEnvelope`:
`summary := &BootstrapSessionSummary{Route: route, Step: step, RecipeMatch: b.RecipeMatch}`
(`internal/workflow/bootstrap_guide_assembly.go:136`), returned with
`Phase: PhaseBootstrapActive` and `Bootstrap: summary`
(`internal/workflow/bootstrap_guide_assembly.go:137`,
`internal/workflow/bootstrap_guide_assembly.go:141`).

Users CAN reach this envelope. `route=recipe` is accepted only with
`recipeSlug` (`internal/workflow/engine.go:333`,
`internal/workflow/engine.go:343`), resolves the recipe
(`internal/workflow/engine.go:355`,
`internal/workflow/engine.go:360`), and stores
`bs.Route = BootstrapRouteRecipe` (`internal/workflow/engine.go:371`,
`internal/workflow/engine.go:374`). Bootstrap steps are `discover`,
`provision`, `close` in order (`internal/workflow/bootstrap_steps.go:17`,
`internal/workflow/bootstrap_steps.go:31`). Completing a step advances
`CurrentStep` and marks the next step in progress
(`internal/workflow/bootstrap.go:164`,
`internal/workflow/bootstrap.go:170`). `BuildResponse` builds the
current step guide from the current detail name
(`internal/workflow/bootstrap.go:229`,
`internal/workflow/bootstrap.go:240`), so after `provision` advances
to `close`, `buildGuide(step="close")` calls
`synthesisEnvelope(step="close")`
(`internal/workflow/bootstrap_guide_assembly.go:23`,
`internal/workflow/bootstrap_guide_assembly.go:42`). The provision
fast-path only skips close for all-existing plans
(`internal/workflow/engine.go:493`,
`internal/workflow/engine.go:500`), so non-all-existing recipe
bootstraps reach route=recipe step=close.

### isPlaceholderToken behavior on `{hostname:value}`

`{hostname:value}` passes `isPlaceholderToken`: the function accepts
brace-wrapped tokens with non-empty inner text
(`internal/workflow/synthesize.go:383`,
`internal/workflow/synthesize.go:390`) and rejects only space,
newline, tab, nested braces, or quote characters inside
(`internal/workflow/synthesize.go:391`,
`internal/workflow/synthesize.go:396`); `:` is not rejected. It is
not in `allowedSurvivingPlaceholders`, whose entries run from
`{start-command}` through `{provider}` and do not include
`{hostname:value}` (`internal/workflow/synthesize.go:316`,
`internal/workflow/synthesize.go:343`). It is not in the replacer
substitutions, which only replace `{hostname}`, `{stage-hostname}`,
and `{project-name}` (`internal/workflow/synthesize.go:111`,
`internal/workflow/synthesize.go:115`). Unknown placeholders fail
synthesis (`internal/workflow/synthesize.go:117`,
`internal/workflow/synthesize.go:119`).

### Test coverage gap

No fixture in the two requested test files exercises route=recipe
step=close. The only `BootstrapRouteRecipe` fixture in
`scenarios_test.go` is route=recipe step=provision
(`internal/workflow/scenarios_test.go:98`,
`internal/workflow/scenarios_test.go:112`). The only
`BootstrapRouteRecipe` fixture in `corpus_coverage_test.go` is also
route=recipe step=provision
(`internal/workflow/corpus_coverage_test.go:130`,
`internal/workflow/corpus_coverage_test.go:139`).

### Verdict

ATOM STATUS: **CONTENT-BUG-BLOCKED**

- **Proposed fix:** change `strategies={hostname:value}` on
  `internal/content/atoms/bootstrap-recipe-close.md:25` to
  `strategies={"<hostname>":"<value>"}` so the brace token contains
  quotes and fails `isPlaceholderToken` by design
  (`internal/workflow/synthesize.go:391`,
  `internal/workflow/synthesize.go:396`).
- **Estimated impact:** 10 envelopes affected per fire-audit stderr;
  all 10 errors are
  `atom bootstrap-recipe-close: unknown placeholder "{hostname:value}" in atom body`
  (`plans/audit-composition/fire-set-stderr-2026-04-26.txt:1`,
  `plans/audit-composition/fire-set-stderr-2026-04-26.txt:12`).

## Side concerns

1. **`isPlaceholderToken` is broad:** it treats any `{...}` without
   whitespace, braces, or quotes as a placeholder, so example-map
   prose like `{hostname:value}` is fragile
   (`internal/workflow/synthesize.go:371`,
   `internal/workflow/synthesize.go:396`).

2. **Coverage gap for the reachable bootstrap route=recipe close
   envelope:** current recipe fixtures stop at provision
   (`internal/workflow/scenarios_test.go:110`,
   `internal/workflow/scenarios_test.go:112`;
   `internal/workflow/corpus_coverage_test.go:138`,
   `internal/workflow/corpus_coverage_test.go:139`).

## Disposition

- F0-DEAD-1 (`bootstrap-recipe-close`) is NOT a Phase 1 dead-atom
  candidate; it is a content bug needing sidecar fix.
- Sidecar commit BEFORE Phase 1 ENTRY: change the placeholder per
  proposed fix above.
- Sidecar commit MAY ALSO add a test fixture for route=recipe
  step=close (closes coverage gap; one-paragraph addition to
  `corpus_coverage_test.go::bootstrapCoverageFixtures`).
- Phase 1 dead-atom sweep proceeds with **0 confirmed DEAD atoms**
  (the only candidate was content-bug, not axis-dead).
