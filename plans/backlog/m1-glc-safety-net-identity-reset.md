# Tighten the GLC `.git/` identity safety net (M1)

**Surfaced**: 2026-04-29 — `docs/audit-prerelease-internal-testing-2026-04-29.md`
finding M1. `InitServiceGit` failure is logged stderr-only at
`internal/tools/workflow_bootstrap.go:204-206` (no error propagation). Deploy-
side safety net at `internal/ops/deploy_ssh.go:199-202` only fires when `.git/`
is MISSING (`test -d .git || (...)`), so a wrong `.git/` left over from
another tool slips past identity reconfiguration and commits attribute to
whatever was set previously.

**Why deferred**: M1's failure mode is silent, not loud — wrong attribution
on commits is a UX paper-cut, not a blocker for first-day testing. The
audit's other M-tier items (M2 stage-timing, M4 placeholder lint) are in
the same "queue after testing reveals which one bites first" bucket.

**Trigger to promote**: a live-agent run where commits land with
unexpected author identity (e.g. an external pre-bootstrap state where
the user committed locally then ran adopt) AND the user notices.
Otherwise this stays parked.

## Sketch

Two complementary tightenings (audit suggestion):

1. **Tighten the safety net**: change the `deploy_ssh.go:199` guard from
   "init when `.git/` is missing" to "always re-set identity, init only
   when missing". Concretely:
   ```sh
   test -d .git || git init
   git config user.name "ZCP Bot"
   git config user.email "zcp-bot@zerops.io"
   ```
   Idempotent — re-setting identity on each deploy is cheap.

2. **Surface the InitServiceGit failure as MOUNT_FAILED** at the
   bootstrap site (`internal/tools/workflow_bootstrap.go:204`). A wrong
   `.git/` from a prior tool isn't a "config" issue, it's a mount-state
   inconsistency the bootstrap should refuse with a clear recovery path
   (rm -rf `.git/` on the dev container, then re-run bootstrap).

## Risks

- Re-setting identity on every deploy is a `.git/config` write — if the
  user explicitly customised `user.name`/`user.email` for another
  reason, ZCP would silently overwrite. Need to verify there's no
  legitimate reason a user would do this on a ZCP-managed `.git/`.
- The bootstrap-time hard-fail path could surprise users in environments
  where `.git/` is reused by tooling outside ZCP (e.g. devcontainer
  scripts that pre-clone). Provide an `--ignore-existing-git` escape
  hatch on `bootstrap` if testing surfaces this.

## Refs

- Audit M1 verified at HEAD `9669ebb5`:
  `internal/tools/workflow_bootstrap.go:204-206` log-only;
  `internal/ops/deploy_ssh.go:199-202` config-only-when-missing.
