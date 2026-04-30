# Aggregate-atom bare-placeholder lint (M4)

**Surfaced**: 2026-04-29 — `docs/audit-prerelease-internal-testing-2026-04-29.md`
finding M4. `internal/workflow/synthesize.go::Synthesize` aggregate-mode
rendering uses `globalHost`/`globalStage` for placeholder substitution
OUTSIDE `{services-list:...}` directives. Future aggregate atoms with
stray `{hostname}` / `{stage-hostname}` placeholders OUTSIDE the directive
re-introduce the wrong-host bug fixed in C2 (deploy-decomp Phase 6
mixed-runtime renders).

**Why deferred**: P5 Lever A added 14 atoms in aggregate mode under
direct authoring care + Codex POST-WORK round caught the one slip-up
(develop-close-mode-auto.md:27 — moved into a services-list during
P5). The class isn't currently broken on disk. M4 is class-prevention
for FUTURE aggregate atoms, not a current-bug fix.

**Trigger to promote**: a future commit adds another aggregate atom AND
a stray `{hostname}` outside a directive AND the matrix simulator
doesn't catch it (e.g. only one service in the matrix-sim fixture, so
globalHost == in-scope-host and the bug is invisible). Periodic recurrence
of this slip → time to bake it into the lint engine.

## Sketch

New lint rule in `internal/content/atoms_lint.go` (extend the existing
axis-rule machinery):

1. For each atom with `multiService: aggregate` in frontmatter:
2. Strip every `{services-list:TEMPLATE}` directive from the body using
   the same brace-matched parser as
   `synthesize.go::expandServicesListDirectives`.
3. Scan the residual body for `{hostname}` or `{stage-hostname}` tokens.
4. Each match is a violation: "aggregate atom has bare {hostname}
   outside services-list — substitution will use globalHost, may
   surface wrong service in mixed-runtime scope."

Add 2 fires-on-known-violations fixtures:
- aggregate atom with `{hostname}` outside any directive → fires
- aggregate atom with `{hostname}` ONLY inside `{services-list:...}` → no fire

## Risks

- The brace-matched parser used by Synthesize handles nesting; the lint
  needs to match its rules exactly or false-positive on legitimate
  nesting. Solution: extract `expandServicesListDirectives`'s parser
  into a shared helper that both the synthesizer and the lint use, so
  drift is impossible.
- Some atoms may legitimately want `{hostname}` outside a directive —
  e.g. the agent-readable example "Use closeMode={"{hostname}":..."}"
  where globalHost as the canonical example IS the intent. Need a
  marker (`<!-- aggregate-host-keep: example -->`) for those cases.

## Refs

- Audit M4 verified at HEAD `9669ebb5`:
  `internal/workflow/synthesize.go:118-122` aggregate-mode global
  fallback.
- P5 Lever A converted 14 atoms — see commits `7dcb1b46` and the
  Codex POST-WORK round 1 finding on `develop-close-mode-auto.md:27`.
