# Audit develop atoms for declarative-state assertions; convert to procedural form

**Surfaced**: 2026-05-02 — live agent run on `nodejs-hello-world` recipe
revealed `develop-first-deploy-write-app.md` asserting *"`/var/www/<hostname>/`
on SSHFS is empty"* while recipe `buildFromGit` had populated the mount.
The acute fix touched five atoms: the three first-deploy core atoms
(`intro`, `scaffold-yaml`, `write-app`) plus the two asset-pipeline
atoms (`-container`, `-local`) that framed a configuration pattern as
recipe-specific.

**Why deferred**: The five-atom acute fix shipped in the same commit as
this entry. Auditing the remaining ~80 atoms for the same pattern is
hygiene, not urgent — no production-impact friction observed beyond the
five atoms already covered.

**Trigger to promote**:
- Next time an atom asserts external-world state and that state changes
  (new bootstrap route, new env shape, new recipe shape).
- Quarterly atom corpus hygiene pass.
- Any review surfacing an atom whose body is silently wrong for one of
  the existing routes (recipe / classic / adopt / mode-expansion).

## The pattern

Atoms describe the world from *"what ZCP did"* perspective instead of
*"what is now observable"* perspective:

- **Declarative-state assertions** that get propagated through a coherent
  set of atoms. Example fixed by the acute commit:
  `develop-first-deploy-write-app.md` claimed *"`/var/www/<hostname>/`
  on SSHFS is empty"* — true for fresh classic-route, lie for recipe
  buildFromGit / adoption / mode-expansion / continuation after prior
  deploy.
- **Provenance-framed configuration patterns** that are actually
  pattern-specific. Example fixed by the acute commit:
  `develop-first-deploy-asset-pipeline-{container,local}.md` opened with
  *"Recipes with..."* when the rule (php-nginx + Vite/Encore omits
  `npm run build` from dev) is reachable from any route — recipe is just
  the most common origin. Frontmatter `runtimes: [implicit-webserver]`
  doesn't restrict to recipe-bootstrapped services.

## The fix shape

Convert from declarative to procedural:

- **Before**: *"X is empty"* / *"Recipes with X..."* / *"Bootstrap does
  NOT do Y"*.
- **After**: *"Inspect X; if condition, do A, otherwise B"* / *"Services
  with configuration pattern X..."* / *"To establish Y, do Z (whether
  or not bootstrap pre-loaded it)"*.

The procedural form is robust to provenance changes by construction —
new routes (future buildFromGit-like mechanisms, custom adoption flows)
all flow through the same observable-state branches without atom edits.

## Sketch — audit procedure

1. Grep across `internal/content/atoms/develop-*.md` for declarative
   patterns:
   - `is empty`, `is not empty`, `does not contain`, `contains no`
   - `Bootstrap does NOT`, `bootstrap does not`, `Bootstrap leaves`
   - `Recipes with`, `Recipe ships`, `recipe-bootstrapped`
   - `from scratch`, `from nothing`, `placeholder`, `stub`
   - `until any code`, `until the deploy lands` (carefully — some are
     correct claims about runtime container, not mount; case-by-case)
2. For each hit, judge:
   - Is the claim **universal** (true regardless of route / env)? Keep.
   - Is the claim **gated** by axes (e.g. `routes: [classic]`)? Verify
     the gate matches the claim's actual scope. Keep if aligned.
   - Is the claim **declaring observable state**? Convert to procedural.
   - Is the claim **framing a pattern via provenance**? Reframe by
     pattern.
3. Out of scope for the audit: bootstrap-active atoms with correct
   `routes:` filter (their claims are scoped to that route by
   construction).

## Risks

- **Test fixture drift**: scenarios tests (`internal/workflow/scenarios_test.go`)
  pin atom rendering by atom ID and body fragment. Procedural-form
  rewrites will change body fragments — fixture updates land per atom,
  not as a sweep.
- **Atom axis lints**: prose-only edits should not trigger axis-K/L/M/N
  lints (those gate path strings + env-shaped tokens). Re-run
  `make lint-local` after each batch.
- **Cross-atom references**: `references-atoms` field must stay in sync
  with body cross-references (`TestAtomReferencesAtomsIntegrity`). The
  acute commit touched this in `develop-first-deploy-intro.md`; any
  audit batch needs the same care.

## Refs

- Acute fix commit: `atoms(first-deploy): convert declarative-state
  assertions to disk-inspection procedures` (2026-05-02).
- Friction surface: live recipe-route session 2026-05-02.
- Architectural anchor: `internal/workflow/synthesize.go:280` (route
  axis filter requires `env.Bootstrap` non-nil) +
  `internal/workflow/compute_envelope.go:16-19` (Bootstrap nilled at
  develop-active) — these mean develop atoms structurally cannot use
  the `routes:` axis. Procedural-form atoms sidestep the constraint
  entirely by not needing to know the route.
- Spec touchpoints (no edits planned): `docs/spec-workflows.md`
  Phase 1/2 boundary, `docs/spec-knowledge-distribution.md` atom
  authoring contract.
