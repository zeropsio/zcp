# Plan: Aggregate-mode axis decoupling (Option B follow-up to E1)

> **STATUS — DEFERRED 2026-04-27.** Codex adversarial review found two
> defects in this plan and one design question worth reconsidering:
>
> 1. **§3 / §4 "0 LoC change in synthesize.go" claim is false.** The
>    aggregate branch alone doesn't handle the dual-axis form because
>    `synthesize.go:65-74` (the `if len(idxs) == 0 { continue }` guard)
>    skips an atom whose service-scoped axes match no service — even
>    when the envelope-scoped axes are satisfied. Implementation
>    requires ~5-10 LoC change to make that guard aggregate-aware
>    (fire with empty directive instead of skip).
> 2. **§3 worked example case (c) — stage-only orphan envelope.** The
>    plan implies the dual-axis atom would fire with empty directive.
>    Under current code it would not fire. Closer inspection of the
>    semantics: an atom whose entire purpose is to teach a per-host
>    `zerops_deploy targetService=...` cmd has no actionable content
>    when no service matches the cmd filter. **Current single-filter
>    behavior is semantically correct** — silence is the right output.
> 3. **Concrete motivating use case is already addressed by Option A.**
>    The local-stage regression that motivated Option B is closed by
>    widening the `modes` filter (shipped in commit `ed5e1382`).
>    Searching for other use cases where Option B's general "broad
>    fire + narrow filter" decoupling is needed: prose-only atoms
>    already work via envelope-axis-only; per-runtime cmd splits
>    work via separate atoms; promote-stage's prose+cmd share the
>    same correct filter. **No second concrete case demands Option B.**
>
> Decision: do not ship Option B. Single-filter design holds. If a
> future content authoring need surfaces a real "broad prose + narrow
> cmd" pair that can't be split into two atoms or solved by widening
> the filter, reopen this plan with that motivation as Section 1.
>
> Codex review touchpoints flagged for posterity (apply if Option B
> is reopened): file:line discrepancy
> (`TestParseAtom_DeployStatesAndEnvelopeDeployStatesMutuallyExclusive`
> lives at `synthesize_test.go:387-405`, not `atom_test.go`); doc
> comment at `atom.go:61-69` also pins the invariant; `aggregate_render
> _probe_test.go` and `corpus_coverage_test.go:450-463` are missing
> from the test-update list.

> **Reader contract.** Engine ticket E1.1 — root-cause fix for the
> axis-coupling defect uncovered while shipping E1. Tickets are PROBLEM
> STATEMENTS + APPROACH PROPOSALS, not phased exec plans (per parent
> plan §3 reader contract).
>
> **Sister plans:**
> - `plans/engine-atom-rendering-improvements-2026-04-27.md` (E1
>   shipped commit `ed5e1382`).
> - `plans/audit-composition-v3/post-e1-rescore-2026-04-27.md` (E1
>   rescore — two-pair G3 CLEAN-SHIP).

## 1. Problem

E1 introduced `multiService: aggregate` + `{services-list:TEMPLATE}`
directive, collapsing per-service render duplication into a single
render with inline service enumeration. The atom-firing condition AND
the directive-enumeration set are determined by the SAME axis vector
on the atom.

This couples two distinct concerns:

- **Firing condition**: when does the atom appear in the briefing?
  Typically broad (e.g. "any envelope with a never-deployed
  service" — `envelopeDeployStates: [never-deployed]`).
- **Directive enumeration set**: which services appear in the
  per-service command list? Typically narrow (e.g. "dev-mode dynamic
  runtimes that need a first deploy" —
  `deployStates: [never-deployed] + modes: [dev, simple, standard]`).

Pre-E1 the workaround was atom split: parent (envelope-scoped prose) +
`-cmds` child (service-scoped, per-service render). E1 collapsed these
via aggregate but inherited the cmds' tight filter for atom-firing.
Result: prose visibility regresses for envelopes that satisfy the
loose envelope-axis but not the tight service-axis.

Concrete regression caught in E1 review: `local-stage` mode envelope.
Pre-E1 `develop-first-deploy-execute.md` fired prose via
`envelopeDeployStates: [never-deployed]` (loose), the cmds atom did
not fire (tight filter `modes: [dev, simple, standard]` excluded
`local-stage`), so agents saw prose without a templated deploy
command. Post-E1 the merged atom inherits the tight filter and
loses BOTH prose and cmd for `local-stage` envelopes.

The Option A fix shipped in `ed5e1382` widens the modes filter to
`[dev, simple, standard, local-stage]`. This treats the symptom
(specific mode missing) but not the root cause: **atom-firing and
directive-enumeration should be independent axes, not the same axis**.
A future mode addition (or an analogous prose/cmd split) will hit the
same friction.

The parser invariant in `internal/workflow/atom.go:369-371` actively
prevents the natural fix:

```go
if len(atom.Axes.DeployStates) > 0 && len(atom.Axes.EnvelopeDeployStates) > 0 {
    return atom, fmt.Errorf("atom %q declares both deployStates (service-scoped) and envelopeDeployStates (envelope-scoped) — pick one; an atom is either per-service or once-per-envelope", atom.ID)
}
```

This invariant was correct PRE-aggregate: an atom was either per-
service (deployStates iterates) or once-per-envelope
(envelopeDeployStates fires once). Aggregate mode introduces a third
shape — once-per-envelope WITH inline per-service enumeration — that
the invariant doesn't accommodate.

## 2. Goal

Lift the parser invariant for aggregate atoms and update `Synthesize`
so an aggregate atom can declare BOTH:

- Envelope-scoped axes (`envelopeDeployStates`, plus the existing
  envelope-wide axes — phases, environments, routes, steps,
  idleScenarios) for **firing**.
- Service-scoped axes (`deployStates`, `modes`, `runtimes`,
  `strategies`, `triggers`, `serviceStatus`) for **directive
  enumeration**.

Per-service atoms (legacy, no `multiService: aggregate`) keep current
behavior — service-scoped axes determine BOTH firing and per-service
iteration. The dual-axes form is **only valid when**
`multiService: aggregate`.

## 3. Approach

### Parser change (`internal/workflow/atom.go:369-371`)

Replace the unconditional reject with an aggregate-aware check:

```go
if len(atom.Axes.DeployStates) > 0 && len(atom.Axes.EnvelopeDeployStates) > 0 {
    if atom.Axes.MultiService != MultiServiceAggregate {
        return atom, fmt.Errorf("atom %q declares both deployStates (service-scoped) and envelopeDeployStates (envelope-scoped) — pick one; non-aggregate atoms are either per-service or once-per-envelope (or set `multiService: aggregate` to use both)", atom.ID)
    }
}
```

For aggregate atoms with both: `envelopeDeployStates` becomes the
firing-gate, `deployStates` becomes one component of the directive-
enumeration filter.

### Synthesize semantic update (`internal/workflow/synthesize.go:51-167`)

Two-phase axis matching for aggregate atoms:

1. **Firing phase** (envelope-scoped):
   `atomEnvelopeAxesMatch(atom, envelope)` runs unchanged — it already
   checks `envelopeDeployStates`.
2. **Filter phase** (service-scoped): if the atom declares any
   service-scoped axis, `serviceSatisfiesAxes` runs per service — same
   path as today. The set of satisfying indices populates
   `pending.matches`.

The aggregate branch at `synthesize.go:103-137` already iterates
`p.matches` indices ≥ 0 and feeds them into the directive expansion.
**No code change is needed in the aggregate branch** — the existing
loop handles the dual-axes form naturally. The only touchpoint is
the parser invariant.

### Edge case: aggregate atom with envelope-only axes (no service-
scoped axis)

Post-E1 today's behavior:
- `hasServiceScopedAxes` returns `false` →
  `pending.matches = [-1]`.
- Aggregate branch: `matched := []` (the `-1` index is filtered out
  by the `idx >= 0` check at `synthesize.go:108-113`).
- Directive expands to empty.

This is the right behavior — an aggregate atom that wants directive
enumeration MUST declare a service-scoped filter explicitly. No
"magic" enumeration over all envelope services. Documented in §11 of
`spec-knowledge-distribution.md`.

If an aggregate atom has envelope-firing axes ONLY (no directive at
all), it renders the body with no per-service expansion — same as a
service-agnostic atom with `multiService: aggregate` is a no-op. The
parser allows it but a lint warning flags the pointless aggregate.

### Atom migration (E1 atoms revisit)

After parser change, revisit the three migrated atoms:

**`develop-first-deploy-execute.md`** — currently:
```yaml
modes: [dev, simple, standard, local-stage]
deployStates: [never-deployed]
multiService: aggregate
```

After Option B:
```yaml
envelopeDeployStates: [never-deployed]   # fires per envelope (broad)
deployStates: [never-deployed]           # directive filter (narrow)
modes: [dev, simple, standard, local-stage]
multiService: aggregate
```

The behavior delta: the atom now also fires for envelopes whose
never-deployed services are NONE-OF the listed modes (e.g. a stage-
only never-deployed envelope from an adoption mid-state). Prose still
applies — it speaks generally about `zerops_deploy` execution. The
directive correctly expands to empty in that edge case (no services
satisfy modes-filter), so the agent sees prose with no cmd list.

Whether that's a regression or improvement is content-cycle work; the
engine change is independent.

**`develop-first-deploy-verify.md`** — currently:
```yaml
deployStates: [never-deployed]
multiService: aggregate
```

After Option B:
```yaml
envelopeDeployStates: [never-deployed]   # fires per envelope
deployStates: [never-deployed]           # directive filter — same axis
multiService: aggregate
```

Both axes carry the same value here because the atom's firing AND
directive-enumeration are both gated on never-deployed. The dual form
is explicit; future filter narrowing (e.g. add a mode filter) would
narrow only the directive without changing firing.

**`develop-first-deploy-promote-stage.md`** — currently:
```yaml
deployStates: [never-deployed]
modes: [standard]
environments: [container]
multiService: aggregate
```

No change needed. Promote-stage prose is conceptually scoped to
"standard pairs with a never-deployed dev side" — both firing and
directive enumeration share that filter. The single-axis form is
correct here.

### Test changes

**`atom_test.go::TestParseAtom_DeployStatesAndEnvelopeDeployStatesMutuallyExclusive`**
(currently at `internal/workflow/synthesize_test.go:393-405`) —
expand to a table:
- non-aggregate atom with both axes → error (existing assertion).
- aggregate atom with both axes → parses (new assertion).
- aggregate atom with only `envelopeDeployStates` → parses + lint
  warning (no directive, pointless aggregate).

**`synthesize_test.go`** — new test:
- aggregate atom with `envelopeDeployStates: [never-deployed] +
  modes: [dev]` fires for an envelope where the only never-deployed
  service is `mode=dev`; directive lists that one host.
- Same atom on an envelope where never-deployed services exist but
  none have `mode=dev` (e.g. `mode=stage` only): atom STILL fires
  (envelope axis satisfied), directive expands to empty.

**`aggregate_render_probe_test.go::TestAggregateRender_LocalStageFiresExecuteAtom`**
(shipped in `ed5e1382`) — keep, but extend assertions to cover the
dual-axis form post-Option-B migration.

### Lint addition (optional, deferrable)

`internal/content/atoms_lint.go` — add a soft warning:

> Aggregate atom `<id>` declares only service-scoped axes. Prefer
> dual form (envelopeDeployStates for firing + service axes for
> directive filter) when the prose applies broader than the cmd
> list.

This is an authoring nudge, not an enforcement gate. Skip if the
ratchet is tight enough already.

### Documentation

`docs/spec-knowledge-distribution.md` §11 (atom authoring contract):

- Add a new sub-section §11.8 "Aggregate atom dual-axis form".
- Document: envelope-scoped axes drive firing; service-scoped axes
  drive directive enumeration; the form is only valid for
  `multiService: aggregate`.
- Worked example: `develop-first-deploy-execute.md` post-Option-B.
- Migration recipe for existing aggregate atoms.

## 4. Blast radius

Changed files (estimate):
- `internal/workflow/atom.go` — 1 conditional, ~5 LoC.
- `internal/workflow/synthesize.go` — 0 LoC (existing aggregate branch
  handles it; verify with test).
- `internal/workflow/atom_test.go` — expand mutual-exclusivity test.
- `internal/workflow/synthesize_test.go` — add dual-axis aggregate
  test.
- `internal/workflow/aggregate_render_probe_test.go` — extend local-
  stage test.
- `internal/content/atoms/develop-first-deploy-execute.md` — adopt
  dual form.
- `internal/content/atoms/develop-first-deploy-verify.md` — adopt
  dual form.
- `internal/content/atoms_lint.go` — optional soft warning.
- `docs/spec-knowledge-distribution.md` — new §11.8.

Pinned tests that must update:
- `TestParseAtom_DeployStatesAndEnvelopeDeployStatesMutuallyExclusive`
  — expand to the table form above.

No corpus-coverage MustContain changes expected (the directive output
is identical; only the firing condition broadens slightly for the
execute atom).

## 5. Risks

- **Cognitive load**: dual-axis aggregate atoms have two distinct
  axis roles (firing vs. directive filter). Authors must understand
  the split. *Mitigation*: clear docs in `spec-knowledge-
  distribution.md` §11.8 + soft lint warning.
- **Unintended firing widening**: switching `develop-first-deploy-
  execute.md` to use envelopeDeployStates broadens its firing to
  envelopes the modes-filter would have excluded (e.g. orphan stage-
  only never-deployed services). The directive correctly expands to
  empty there, so the agent sees prose without a cmd list. Whether
  the prose IS appropriate for that edge case is a content-cycle
  judgment, independent of engine work. *Mitigation*: phased
  migration, content-cycle review of the edge cases.
- **Parser invariant asymmetry**: lifting the invariant for aggregate
  but keeping for non-aggregate is conceptually asymmetric. *Mitig-
  ation*: explicit error message + test case naming both shapes.
- **Backward-compat with E1 corpus**: the three migrated atoms in
  E1 work correctly under both the current single-axis form and the
  new dual-axis form. Migration is a content-only change, no engine
  break.

## 6. Suggested execution order

Single ticket, three commits:

1. **Engine + tests** — parser invariant lift, test expansion, no
   atom changes yet. The corpus still uses single-axis aggregate; the
   parser permits dual-axis but no atom uses it. This is the
   foundation commit; tests prove the engine handles both shapes.
2. **Atom migration** — switch `develop-first-deploy-execute.md` and
   `develop-first-deploy-verify.md` to dual form. Re-run corpus
   coverage + aggregate render probe tests; rescore composition (no
   regressions expected).
3. **Docs + lint** — `spec-knowledge-distribution.md` §11.8, optional
   soft lint warning.

Total estimated effort: 1-2 days. Smaller than E1 because the
aggregate infrastructure already exists.

## 7. Acceptance

- Parser tests green: aggregate atoms accept dual-axis form,
  non-aggregate atoms reject it.
- Synthesize tests green: dual-axis aggregate atom fires per envelope
  and directive iterates filtered services.
- Existing E1 probe tests green: post-migration body bytes within
  ±200 B of pre-migration (no structural regression).
- Composition rescore: G3 strict-improvement holds across all five
  fixtures (no fixture regresses).
- Live verification: `develop-first-deploy-execute` fires for a
  `local-stage`-only envelope (prose visible, directive lists the
  local-stage host).

## 8. Out of scope

- Adding new axis types (e.g. a "list-all-services" filter for
  directive enumeration without service-axis narrowing).
- Combining other mutually-exclusive axes (no other pairs exist
  today).
- Per-service iteration semantics for non-aggregate atoms.
- E2 (`zerops_deploy` error-response enrichment) — separate ticket.

## 9. Provenance

Drafted 2026-04-27 alongside E1 ship. The local-stage regression
caught during E1 review surfaced the underlying axis-coupling defect.
Option A (widen modes filter, shipped in `ed5e1382`) addresses the
specific symptom; Option B addresses the design defect that produced
it.

The hygiene-cycle work + E1 reached a design ceiling: the prose-cmd
split is fundamental to atom authoring (broad guidance + narrow
imperatives in the same atom), and the engine should accommodate it
explicitly rather than forcing authors to pick one filter for both.
