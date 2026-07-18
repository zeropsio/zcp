# Slice brief: <ID> — <title>

Self-contained: no other file is required to execute this. Cite spec §s,
never the plan.

**Outcome** (observable): <what exists/works when this slice lands>

**Allowed scope**
- Files: <write-set, exact paths>
- Explicitly excluded: <paths/behavior this slice must NOT touch>

**Spec citations**: <docs/spec-<name>.md §<n> — the promoted contract this
slice implements>

**RED test list**
- `Test<Op>_<Scenario>_<Result>` — layer: <unit|tool|integration|e2e>
- `Test<Op>_<Scenario>_<Result>` — layer: <unit|tool|integration|e2e>

**Protocol**: RED → GREEN → REFACTOR.
1. Write the named test(s) first; confirm they fail for the right reason
   (assertion, or the exact missing-symbol error on a new seam):
   `go test ./<pkg> -run <Name> -short -count=1 -v`
2. Implement until green: same command, must pass.
3. Refactor with tests green; re-run the command; then `make lint-fast`.

**Report contract** (all required — never summarize away a failure)
- RED output + exit code (proves the test failed pre-implementation)
- GREEN output + exit code
- Files touched (exact list)
- Layer-matrix pass lines (one per CLAUDE.md change-impact layer this hits)
- Independent-oracle note: expected values came from spec/known-good
  literals, never recomputed the implementation's own way

**Stop conditions** (halt + handoff, don't push through): scope drift · a
material unknown · an acceptance-criteria change · a repeated unexplained
check failure.

**Definition of Done**
- [ ] RED replay: fails at slice base SHA, passes at slice head
- [ ] Named tests pass with `-count=1 -v`
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
