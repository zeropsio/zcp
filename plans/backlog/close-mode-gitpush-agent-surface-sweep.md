# Agent-facing surface still presents `git-push` as a close-mode / delivery selector

**Status:** open / deferred
**Surfaced by:** CLAUDE.md de-bloat reconciliation + Codex review (2026-06-16).
**Severity:** low — stale agent-facing text, no behavior bug (the handler still
accepts `git-push` and folds it to `auto`; delivery derives from `GitPushState`).

## Background
The 2026-06-10 git-delivery supersession retired the `git-push` CloseDeployMode
VALUE: it folds one-way to `auto` at parse (`foldLegacyCloseMode`), and delivery
is now DERIVED from `GitPushState` (`resolveDelivery`), not chosen by close-mode.
The CLAUDE.md de-bloat reconciled the SPEC prose to this model (spec-workflows
§4.3/§4.5/S1/S5, spec-work-session §7.5/§9.7, spec-local-dev, spec-knowledge-
distribution). But the AGENT-FACING surface — tool schema descriptions, the
close-mode handler's hints/validation, the router offering, and the
spec-scenarios golden that mirrors them — still presents `git-push` as a live
close-mode value the agent should pick. These are deferred because each is
agent-facing output, several are pinned by golden / drift tests, and they should
move together (and ideally alongside the broader git-delivery draft promotion).

## Stale sites (all still say `git-push` close-mode / `.netrc`)
- `internal/tools/workflow.go:54,61,328` — the `zerops_workflow` action jsonschema
  description: "close-mode (set per-pair CloseDeployMode auto/git-push/manual)".
  Likely pinned by `description_drift_test.go` — check before editing.
- `internal/tools/workflow_close_mode.go:80,113,131,184,192` — validation +
  hints: "closeMode={…:<auto|git-push|manual>}", "Valid values: auto, git-push,
  manual", "pick git-push / manual, which work without a stage".
- `internal/workflow/router.go:184` — `strategyOfferings` close-mode offering
  hint: "set per-pair close-mode (auto/git-push/manual). For git-push close-mode,
  follow up with action=git-push-setup to provision GIT_TOKEN/.netrc/remote URL".
  TWO defects: `git-push` close-mode framing AND `.netrc` (the shipped mechanism
  is the credential helper, `internal/ops/git_credential.go` — `.netrc` is dead;
  spec-workflows.md §4.4 / git-push-setup action already says so).
- `docs/spec-scenarios.md:382` — scenario hint "switching to closeMode=git-push /
  closeMode=manual" — faithfully mirrors the shipped handler hint above, so it
  must be updated IN LOCKSTEP with `workflow_close_mode.go` (it is pinned by
  `scenarios_test.go`).

## Fix when promoted
Rewrite all sites to the shipped model: close-mode is `auto`/`manual` (done-ness
ownership); to deliver via git push, run `action="git-push-setup"` (provisions
`GIT_TOKEN` as a service secret via the credential helper — NO `.netrc`); legacy
`git-push` close-mode input is accepted but folds to `auto`. Update the handler +
jsonschema + router together, re-bless the scenarios golden, and check
`description_drift_test.go` / `annotations_test.go`. Bundle with the
git-delivery draft promotion if that lands first.

## Why deferred (not fixed in the de-bloat pass)
The de-bloat scope was instruction-file + spec truth-telling. These are
agent-facing CODE outputs with golden/drift-test coupling — a deliberate,
separately-verified change, not a doc edit.
