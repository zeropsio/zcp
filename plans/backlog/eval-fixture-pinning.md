# Eval fixture pinning — buildFromGit honours only `@<branch>`

**Surfaced**: 2026-09-13 — live probe (project `eval-x`) while building the eval
core matrix. `buildFromGit: <url>@<sha>` is silently ignored (platform builds the
default branch, records `publicGitSource.branchName: main`); a sibling `ref:` key
is ignored too.

**Parked because**: the way this project works with git is about to change
completely; do not build a pin scheme on the current model.

**State**: FM-62 + `TestEvalScenarioFixtures_BuildFromGitPinned` accept only
`@pin/<label>`; 9 fixtures sit in the shrinking `legacyUnpinned` allowlist.

**When revisited**: decide the fixture source of truth under the new git model
(own repo, immutable branches, or a pushed source snapshot), then empty
`legacyUnpinned`.
