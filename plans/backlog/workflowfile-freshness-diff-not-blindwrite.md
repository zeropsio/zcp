# build-integration workflow-file freshness — diff, don't blind-write

**Surfaced**: 2026-06-17, flow-eval `git-push-setup-then-actions` retrospective
(suite 20260617-171651). The build-integration response handed the agent a complete
`.github/workflows/zerops.yml` to write+commit+push; the remote already carried that
file from a prior run → non-fast-forward → pull → merge conflict on the exact file →
NOTHING_TO_PUSH. The whole write/commit/conflict/resolve cycle was wasted.

**Why deferred**: the obvious fix — "if the file is present on the remote, skip the
write" — is a content-blind masking fallback. The launch earn-probe checks PRESENCE
only (`test -f`), never content, but `actionsWorkflowYAML` is topology-parameterized
(--setup, -g). A present-but-DRIFTED file (wrong --setup, missing -g, a teammate's
hand-rolled file) would be wrongly suppressed. The friction was also eval-env
amplified (the "recipe ships the workflow" trigger is fictional — zero recipes ship
`.github/workflows/`), and `deploy_git_push` already gives correct non-fast-forward
recovery, so the cost is one wasted cycle, not a deadlock.

**Trigger to promote**: a second independent eval where a clean (non-stale-repo) run
hits the write→conflict cycle, OR a recipe that legitimately ships a workflow file.

**Sketch (additive, NOT suppress)**: keep emitting the content always; add a
freshness HINT to the build-integration response — "before writing, `ssh <host> 'git
fetch origin && git log HEAD..origin/main --oneline'`; if the workflow file already
exists on the remote, DIFF it against the emitted content and only commit if it
differs (don't blind-write)." Probe-on-content, not probe-on-presence.

**Refs**: plans/minor-findings-rootcause-2026-06-17.md (F3, dropped-from-fix-now).
