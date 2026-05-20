# Zerops yaml OS-prefix shape drift — ZCP wide

- **Surfaced**: 2026-05-18, during deep-research investigation of broken behavior in eval review 20260518 subset (see `plans/eval-review-20260518-subset/deep-research/01-atoms-templates.md` Finding E + Karel's clarification in chat).
- **Update 2026-05-18 (Sunday release confirmation)**: Karel confirms upstream change. `mode:` and `os:` sibling fields are deprecated (kept for BC); the canonical shape is the composite `type` field carrying the full identifier:
  ```
  before: type: nodejs@22         after: type: alpine/nodejs@22
          os:   alpine
  before: type: postgresql@18     after: type: postgresql:single@18
          mode: NON_HA
  ```
  ZCP atoms / prompts that hardcode `os` or `mode` need update. Recipes need a full pass to rewrite import.yml templates to the composite form. Plan validator already requires composite (visible eval friction).
- **Why deferred**: Originally framed as a 5-line strip-OS-prefix fix in `topology/runtime_class.go`. Karel's clarification: Zerops upstream changed yaml shape — service type strings used to be bare (`nginx@1.22`, `php-apache@8.4`, `nodejs@22`), now arrive composite (`alpine/nginx@1.22`, `ubuntu/nodejs@22`, `postgresql:single@18`). The classifier bug is one visible leaf; the actual drift surface spans the full ZCP stack (knowledge atoms, recipe templates, validators, render goldens, plan-vs-yaml conversion). A single-site strip is symptom-axis — bringing ZCP to parity with the new upstream shape is a design pass and an audit, not a point fix.
- **Trigger to promote**: NOW. Karel confirms Sunday release shipped this; ZCP recipes + atoms are out of sync with platform reality. Visible eval evidence: `classic-static-nginx-simple`, `landing-page-static-simple`, `classic-php-mariadb-standard`, plus any PHP-apache/PHP-nginx/static-nginx scenario; plus the `adopt-existing-standard-pair` eval (20260518-194541) where the agent self-review explicitly named bare `nodejs@22` not-in-catalog as biggest friction.
- **Scope inventory (2026-05-18)**: 48 files carry `os:` or `mode:` siblings that need rewrite to composite `type:`:
  - 47 recipe files under `internal/knowledge/recipes/` (`.import.yml` + `.md` recipe pairs)
  - 1 workflow example: `internal/content/workflows/recipe/phases/provision/import-yaml/standard-mode.md`
  - 0 atom files (after `idle-adopt-entry.md` rewrite which mentions `os:` + `mode:` only in BC-deprecation prose, not as yaml example)
  - Plus the `topology/runtime_class.go` classifier strip + test extension surfaced earlier in this backlog entry.

## Visible symptom (the classifier leaf) — FIXED 2026-05-20

`internal/topology/runtime_class.go::RuntimeClassFor` now strips the known OS
prefix (`alpine/`, `ubuntu/`) before the HasPrefix runtime-class check, so
composite forms classify identically to their bare equivalents.

Pinned by:
- `TestRuntimeClassFor` — extended table now carries 13 composite-shape cases
  (alpine/*, ubuntu/*, mixed-case ALPINE/, mode-encoded `postgresql:single@18`).
- `TestRuntimeClassFor_BareCompositeSymmetry` — 11 bare↔composite pairs assert
  classification symmetry; gate against silent regression on a future managed-
  prefix or stripKnownOSPrefix refactor.

Downstream `RuntimeClassFor` consumers — `compute_envelope.go:223`,
`deploy_poll.go:233`, `deploy_subdomain.go:118`, `subdomain.go:146` — all
inherit the fix transparently (`go test ./internal/{topology,workflow,ops,tools}`
green after the change).

The broader audit work below stays open (atom content alignment, recipe
yaml regeneration, render goldens); the classifier leaf is no longer the
gating defect.

## Sketch — why a strip is not the whole fix

A 5-line `lower = strings.TrimPrefix(...)` in `RuntimeClassFor` resolves the classifier symptom. But the OS prefix is now load-bearing semantic data — it appears throughout:

1. **`liveTypes[].Versions[].Name` registry** (plan validator at `internal/workflow/validate.go:387-397::typeExists`) — already uses composite/OS-prefixed forms. Plan input must match.
2. **`zerops.yaml` runtime declaration** — accepts BOTH bare and OS-prefixed (per `internal/platform/project_admin_api_test.go:35,76,153`). Recipe yamls (`internal/knowledge/recipes/*.import.yml`) currently use bare.
3. **Atom prose + recipe knowledge** — many references to runtime types in atoms/recipes assume bare form. Search reveals dozens of `nginx@` / `nodejs@22` patterns; some are docs-only, some are templating values.
4. **Composite-mode encoding** for managed dependencies (`postgresql:single@18` vs `postgresql@18 + mode: NON_HA`) — separate but related asymmetry, covered by **finding G** in the eval review (`plans/eval-review-20260518-subset/deep-research/03-workflow-recovery.md`).

Pre-production rule from CLAUDE.local.md: pick one canonical encoding per concept and normalize at every boundary. Question is: **is the canonical form OS-prefixed (`alpine/nginx@1.22`) or bare (`nginx@1.22`)?** The current Zerops yaml shape says OS-prefixed for runtime declaration but accepts both at the import API. Plan validator wants the OS-prefixed form. Recipe yamls ship bare.

## Risks

- The strip-and-move-on fix masks the wider drift. Six months from now another shape change at Zerops will surface another leaf and the same audit will be needed.
- Some OS-prefixed names are NOT runtime classifiers — e.g. `postgresql:single@18` has a colon (mode encoding), not a slash. Classifier strip must distinguish OS prefix from mode encoding from version separator.
- Atom gating axes (`runtimes:[static]`, `runtimes:[implicit-webserver]`) — once the classifier is fixed, all those atoms WILL start firing. Verify their content is still accurate for the OS-prefixed-yaml world; specifically, any inline yaml examples that show bare-form `nginx@1.22` should match what the agent will actually see.
- Recipe yamls + recipe knowledge files (`internal/knowledge/recipes/*.md` and `*.import.yml`) may need re-templating if they currently ship bare-form runtime types that no longer match what Zerops returns.
- Render goldens (`*_golden_test.go`) may pin bare-form output that needs regeneration.

## Sketch — phased fix when promoted

1. **Audit pass**: ~~enumerate every site that consumes service type strings~~.
   **DONE 2026-05-18** — `plans/eval-review-20260518-subset/os-prefix-bc-audit.md`
   inspected 38 comparison sites; 3 broken sites fixed in commit `0696f646`
   (verify_checks.go, deploy_validate.go, validate.go); 6 SUSPECT sites flagged
   for Karel verify; 29 SAFE sites confirmed shape-tolerant.
2. **Canonical-form decision**: OS-prefixed `<os>/<runtime>@<version>` everywhere ZCP touches the platform side. Bare form acceptable in human-facing prose only, normalized at the boundary.
   - Cross-boundary equivalence handled via `topology.TypesAreEquivalent`
     (commit `a3314929`) + asymmetric semantic (commit `4932e4b4`).
3. **Classifier fix** (the leaf): ~~`runtime_class.go` strip-after-slash before HasPrefix check. Extend test table with OS-prefixed forms.~~
   **DONE 2026-05-20** — see "Visible symptom" section above.
4. **Plan validator alignment**: ~~confirm `typeExists` at `validate.go:387-397` reads from the canonical registry; reject bare forms with an actionable error pointing at the OS-prefixed name.~~
   **DONE** — `typeAcceptedByCatalog` already delegates to
   `topology.TypesAreEquivalent` (commit `a3314929`); bare-form plans accepted
   against composite live catalog.
5. **Recipe yaml audit** (Aleš scope): regenerate import.yml templates with OS-prefixed forms IF live import API has moved past bare-form acceptance (verify empirically). Live `services[].type` enum still carries both shapes, so this is a Aleš-side authoring preference, not a forcing requirement.
6. **Atom content audit**: every atom that shows inline yaml with `nginx@1.22` / similar should match what `zerops_discover` actually returns. Otherwise agent reads atom, copies bare, gets `INVALID_ZEROPS_YML: unknown base`. **PARTIAL** — `atoms_lint_axes.go` doesn't enforce composite-shape today; manual sweep needed for atoms emitting yaml examples (`develop-first-deploy-scaffold-yaml.md`, `scaffold-zerops-yaml.md`, `launch-write-prod-setup.md`, framework-tagged atoms).
7. **Render golden regeneration**: after atoms/recipes regenerate, run `go test ./internal/content/... -update` (or equivalent).
8. **Eval re-run**: `classic-static-nginx-simple`, `landing-page-static-simple`, `classic-php-mariadb-standard`, and any other scenario whose runtime gets OS-prefixed by the Zerops side.

Estimated remaining effort: 1 day for the atom-content audit + recipe alignment + golden regeneration. The classifier + downstream-classifier + plan-validator work is done.

## Refs

- `internal/topology/runtime_class.go:28-32` — classifier site
- `internal/topology/runtime_class_test.go:5-35` — gap-hiding test
- `internal/workflow/validate.go:387-397::typeExists` — plan validator (separate but related)
- `internal/platform/project_admin_api_test.go:35,76,153` — empirical evidence import-yaml API accepts both forms
- `internal/knowledge/recipes/*.import.yml` — recipe yamls (current bare-form usage)
- Researcher report: `plans/eval-review-20260518-subset/deep-research/01-atoms-templates.md` (Finding E section)
- Related backlog: composite-type plan-vs-yaml asymmetry (finding G in `03-workflow-recovery.md`) — covers `postgresql:single@18` vs `postgresql@18 + mode` axis
- Karel's clarification (chat 2026-05-18): yaml shape changed upstream, drive was bare prefix, needs separate audit pass; classifier is one leaf of a broader drift surface.
