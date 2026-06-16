---
title: Multi-runtime promotion in launch-production
status: backlog
opened: 2026-05-13
trigger_to_promote: |
  - First real-world launch-production request on a source project that
    has multiple USER-category runtimes (e.g. frontend + api + worker)
    where the user expects all of them promoted, OR
  - Eval scenario surfaces silent single-runtime promotion as confusing
    (agent picks one runtime without disclosing the choice), OR
  - Security/product review flags "silent single-runtime promotion" as
    a footgun against full-stack promotions.
---

## Context

v1 launch-production takes `targetService` as a singular string —
exactly one runtime gets promoted per launch call. The original plan
(`production-lifecycle-part2-2026-05-12.md` §10) explicitly cut
multi-runtime promotion out of scope:

- Bundle composition is single-runtime + managed-deps.
- Import yaml schema lists one `buildFromGit` runtime.
- Pipeline-config Path B walks the user to one repository integration
  at a time.

Original feedback (`launch-production-feedback-fixes-2026-05-13.md` #8)
classified the singular-target shape as NOT-A-BUG (it was a deliberate
scope cut). Codex review 2026-05-13 flagged this re-classification was
too lenient:

> If the user says "deploy project to prod" on a multi-runtime project,
> silently promoting only one runtime is a product-safety issue.

The agent today receives `availableRuntimes` (potentially multi-entry)
in the scope-prompt response and is expected to ask the user which to
promote. Two failure shapes exist:

1. **Silent single-runtime pick on multi-entry list:** if the agent
   doesn't surface the choice to the user (atom guidance might be
   sufficient or might not — eval depends on which agent runs the call),
   the user could end up with only `api` promoted when they expected
   `frontend` + `api`, only discovering this after the fact.

2. **True intent is "promote all":** even with full agent disclosure,
   the user may want all USER-category runtimes promoted in one workflow.
   v1 forces N separate launch calls — friction + N tokens + N approval
   loops.

## Sketch when promoted

### Detection (cheap addition; can ship before full multi-target)

In `gatherLaunchSourceContext` post-collapse: when
`len(availableRuntimes) > 1`, return an additional field
`multiRuntimeWarning: true` (or surface explicitly in scope-prompt
blockers as `scope-multi-runtime-explicit-choice-required`). Agent atom
guidance directs: "Source has multiple runtimes. You MUST disclose all
of them to the user and confirm which to promote in this launch.
Promoting one does not promote the others."

This is a small surface change — does not enable multi-target, but
ensures the silent single-pick failure mode (#1 above) cannot fire.

### Full multi-target (larger surface)

`targetService` becomes `targetServices []string` (or stays singular with
a new `additionalTargetServices` field for clarity). Bundle composition:

- Each target gets its own `buildFromGit` entry.
- Managed deps are shared (one postgres, one valkey, etc. — same as
  source project's topology).
- Source-immutability hashing covers all targets' source trees.
- Pipeline-config Path B emits N dashboard deep-links, one per target.

Atom corpus:

- `launch-scope-prompt.md` — describe multi-target syntax.
- New atom `launch-multi-target-active.md` — describe per-target
  progress reporting in `launching` status.

State file:

- `launchState.Targets []targetEntry` instead of singular
  `TargetService` + `TargetProjectName`. Idempotent resume must handle
  partial completion (target A imported, target B blocked).

Failure semantics:

- One target's failure does not abort the others — `failed` status
  becomes per-target.
- `launched` status requires ALL targets succeeded.
- Partial state: `partial-launched` or surface in blockers on `launched`.

## Why this is in backlog (not rejected)

The detection-only addition (warn-on-multi) is small enough to land in
F2-adjacent work IF the eval scenario surfaces the silent-pick concern.
Full multi-target is a substantive design pass touching bundle, state,
atom, and failure model. Defer the latter; the former can be promoted
on first concrete signal.

## Cost estimates

- **Detection-only:** ~50 LOC + 1 atom edit + 2-3 tests. ~1 hour.
- **Full multi-target v1:** ~600-900 LOC + 4-6 atom edits + 15-20 tests
  + state-migration considerations. ~2-3 days.

## References

- `plans/archive/production-lifecycle-part2-2026-05-12.md` §10 (original scope
  cut rationale — single-runtime simplification for v1 import yaml
  composition + Path B pipeline-config user-walk).
- `plans/archive/launch-production-feedback-fixes-2026-05-13.md` §1 (this
  finding's re-classification from NOT-A-BUG to BACKLOG).
- `internal/tools/launch_source_context.go::gatherLaunchSourceContext`
  (current `availableRuntimes` shape — F2 makes it pair-collapsed but
  still surfaces multiple entries when truly multi-runtime).
