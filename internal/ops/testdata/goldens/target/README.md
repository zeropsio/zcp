# Target goldens — per-phase

These yaml files capture the **expected post-phase observable output**
of `BuildLaunchBundle` and `BuildBundle` after intentional behavior
changes shipped in a specific phase.

## Update policy

- **Per-phase.** Each phase that intentionally changes observable
  composer output stamps its phase ID in the diff PR.
- Distinct from `regression/`: `target/` describes the GOAL state;
  `regression/` describes the CURRENT state at Phase 0 capture time.
- Until the corresponding phase ships, the file lists the **expected
  delta** as a comment header so the diff is reviewable in isolation.

## Files

No target goldens exist yet. They will land per-phase:

- Phase 2 fix of F20 → `target/launch-recipe-laravel-f20-fixed.yaml`
  (storage entry has no `mode:` field)
- Phase 2 fix of F21 → `target/launch-recipe-laravel-f21-fixed.yaml`
  (storage entry has `objectStorageSize: <quotaGBytes>`)
- Phase 6b composer rewrite for production push-mode → 
  `target/launch-recipe-*-push-mode.yaml` (runtime entry has
  `startWithoutCode: true`, no `buildFromGit`)

See `plans/workflow-family-architecture-2026-05-14.md` §11 for the
full phase sequence.
