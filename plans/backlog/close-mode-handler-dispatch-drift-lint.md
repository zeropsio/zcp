# Close-mode handler-dispatch drift lint

**Surfaced**: 2026-04-30 — close-mode framing sweep (commit `57c91749`).
Codex review of `docs/intro-zerops-and-zcp.md` flagged 2 BLOCKER findings,
one of which (`action="close"` does not deploy/verify) was a textual drift
that had propagated across 3 atoms (`develop-close-mode-auto`,
`develop-close-mode-git-push`, `develop-strategy-review`) and 4 spec
sections (`spec-workflows §1.2 + §4.3`, `spec-knowledge-distribution §3.4`,
`spec-work-session §6`). The drift class is "claims that the close handler
dispatches based on close-mode" — handler at `internal/tools/workflow.go:
873-902` is deliberately pure session-teardown.

**Why deferred**: the sweep just shipped; corpus is currently clean. A lint
gate is class-prevention for FUTURE atoms, not a current-bug fix. Adding
the gate now would block a clean commit on a hypothetical risk; better to
let the pattern recur once before encoding it.

**Trigger to promote**: a future atom or spec edit reintroduces any of the
banned phrases AND the matrix simulator's `containsLegacyStrategyVocab`
(or its successor) doesn't catch it. Periodic recurrence → bake into lint.

## Sketch

Extend `internal/content/atoms_lint.go` with a new banned-phrase rule
analogous to `axisMDriftPatterns`:

```go
// closeHandlerDispatchDrift catches phrases that frame action="close" as
// dispatching a deploy based on close-mode. The handler is always pure
// session-teardown (internal/tools/workflow.go::handleWorkSessionClose);
// close-mode shapes the agent's pre-close ritual via per-mode atoms, not
// the close handler.
var closeHandlerDispatchDrift = []*regexp.Regexp{
    regexp.MustCompile(`(?i)\bwhat\s+\x60?action="close"\x60?\s+does\b`),
    regexp.MustCompile(`(?i)\bwhat\s+\x60?zerops_workflow action="close"\x60?\s+does\b`),
    regexp.MustCompile(`(?i)\b(the\s+)?(develop\s+)?close\s+(action\s+)?(runs|commits|pushes|yields|orchestrates)\b`),
    regexp.MustCompile(`(?i)\bclose\s+runs\s+\x60?zerops_deploy\x60?\b`),
    regexp.MustCompile(`(?i)\bzcp\s+runs\s+\x60?zerops_deploy\x60?\s+directly\s+at\s+close\b`),
}
```

Apply to atom bodies AND, ideally, to `docs/spec-*.md` via a separate
lint pass (spec drift is the upstream cause; atom drift is the symptom).

Suppress with `<!-- close-handler-dispatch-keep: explicit-negation -->`
for the one legitimate use: explicitly negating the misconception
(`develop-strategy-review.md:13` — "Close-mode does not change what
`action="close"` does (close is always a session-teardown call)").

Add 3 fires-on-known-violations fixtures:
- atom body says "Close runs zerops_deploy" → fires
- atom body says "what action=\"close\" does" → fires
- atom body explicitly negates the misconception with the keep marker → no fire

## Risks

- The regex set must NOT fire on legitimate uses where atoms describe
  what the AGENT does (vs. what the close handler does). The negation
  framing in `develop-strategy-review.md:13` and the per-atom "Your
  delivery pattern is..." openings are correct and must pass. Solution:
  the explicit-negation marker handles strategy-review; the per-atom
  openings naturally don't match the regex (no "close runs/commits/
  pushes" verb pattern).

- `docs/spec-workflows.md §4.3` deliberately retains the phrase
  "What `zerops_workflow action="close"` does" inside the negation
  paragraph: "It does NOT make the close handler dispatch anything..."
  Same marker handles this case if we extend the lint to specs.

- Spec drift detection is harder than atom drift because spec markdown
  has no frontmatter and no canonical body extraction — the lint would
  need a directory walker. Worth it only if drift recurs in specs after
  the atom-level lint stabilizes.

## Refs

- Sweep commit: `57c91749` "docs(close-mode): reframe as delivery
  pattern, not close-handler dispatcher"
- Handler ground truth: `internal/tools/workflow.go:873-902`
  (`handleWorkSessionClose`)
- Audit referencing this drift class:
  `docs/audit-prerelease-internal-testing-2026-04-29.md` (mental-model
  section + future audit rounds)
- Sister lint rule pattern:
  `internal/workflow/lifecycle_matrix_test.go::containsLegacyStrategyVocab`
  catches the analogous `action="strategy"` legacy-vocab drift class.
