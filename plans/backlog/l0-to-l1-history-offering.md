# L0→L1 transition: offer a clean-history option before the first push

**Surfaced**: 2026-07-12 — git-contract review
(`plans/git-sdlc-review-2026-07-12.md` §7a): the first
`zerops_deploy strategy=git-push` after git-push-setup pushes the FULL local
history to the user's GitHub — including, on pairs that lived under the old
contract, the accumulated robot-authored `deploy` commits. No clean-slate
offering exists; the only flatten code path is the shallow-clone-failure
recovery (F1b), never offered proactively.

**Why deferred**: P2 stopped minting `deploy` commits going forward, so the
polluted-history population is capped at pairs created before the fix.
Whether ZCP should proactively offer "flatten to a single snapshot commit
before your first push" is a product decision (it rewrites history the user
may want to keep), not a bug fix.

**Trigger to promote**: a real user hitting "my new GitHub repo is full of
`deploy` commits by Zerops Agent" after configuring git-push on a pre-fix
pair; or Karel deciding the setup flow should ask.

## Sketch

At git-push-setup confirm time (container), when the local history is
predominantly robot-authored `deploy` commits, surface a choice in the
response: (a) push history as-is (default, current behavior), (b) flatten —
the existing orphan-branch snapshot recipe (`git checkout --orphan …`,
identity via the seeded/derived value) producing one clean initial commit.
Never auto-flatten; the user owns history rewrites.

**Refs**: `internal/tools/workflow_git_push_setup.go` (flatten recovery
text), `plans/git-contract-2026-07-12.md` F3 (identity seeding the flatten
commit would inherit).
