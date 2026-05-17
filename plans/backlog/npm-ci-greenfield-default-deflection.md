# `npm ci` default on greenfield Node trips first build

- **Surfaced**: 2026-05-17, two flow-eval runs in a row (`pm-app-czech-byty-run1-replay` 2026-05-16 suite `20260516-225809`, retro item 1; `cadence-multiservice-build-run2-replay` 2026-05-17 suite `20260517-045943`, retro item 1). Both agents defaulted to `npm ci` as "the production-correct thing" on a greenfield project where no `package-lock.json` is committed yet. First build fails after ~24s with the npm `EUSAGE` error.
- **Why deferred**: low cost — the existing `build:npm-ci-missing-lockfile` signal classifier names the exact remedy, agents recover with one yaml edit. Two retros say the recovery felt clean. Not blocking; pure UX-tightening.
- **Trigger to promote**: any retro where the agent doesn't get the npm-ci-missing-lockfile signal and falls to the build baseline ("no recognized log pattern matched"), OR a third retro complaining about the default. Two retros already on record, third is the tipping point.

## What both agents wrote

Run 1 replay (`pm-app-czech-byty-run1-replay`):

> "The build guidance scaffolded a fresh project from nothing, but `npm ci` is reproducible-install only — it refuses without a committed lockfile. The failure classification was actually excellent here (told me exactly what to do), but I should have written `npm install` from the start for a greenfield scaffold. Default to `npm install` in `buildCommands` unless you know a lockfile already exists."

Run 2-build replay (`cadence-multiservice-build-run2-replay`):

> "If you copy the standard `buildCommands: [npm ci, ...]` pattern from anywhere, the first build fails after ~24s with a long npm error block. Just use `npm install` from the start unless you've already committed a lockfile. The guidance never told me to use `npm ci`; I just defaulted to it as 'the production-correct thing.' Don't."

Two independent agents on different prompts, same default mistake, same eventual recovery. The atoms don't currently tell the agent which install command to pick for which state.

## Sketch — candidate fixes

1. **Atom guidance — name the choice in `develop-first-deploy-scaffold-yaml.md` or a sibling.** One line: *"On greenfield scaffolds (no committed `package-lock.json`), use `npm install` in `buildCommands`. Switch to `npm ci` only after the lockfile is committed — pnpm/yarn equivalents share the same rule."* Cheapest fix; touches one atom.
2. **Atom example yaml — show both commands and when to pick which.** Anywhere develop-active guidance carries a `buildCommands` example, include the npm-install-vs-ci split as a comment.
3. **Scaffold-time generator — when ZCP scaffolds a fresh `zerops.yaml` (workflow or recipe path), pick `npm install` for greenfield by default and `npm ci` only when a lockfile is already in the mount.** Bigger scope; touches generator paths.

Option 1 is enough for the friction observed; option 3 would close the gap structurally.

## Side note — same trap for pnpm + yarn

The retro doesn't say it but `pnpm install --frozen-lockfile` and `yarn install --frozen-lockfile` have identical semantics. If the guidance ever expands beyond npm, it should cover all three.

## Risks

- The agent that recovers from the npm-ci-failure is sometimes the most valuable signal for learning the platform — pre-deflecting might rob them of seeing the structured failure classification work. Tradeoff favors deflecting since the empirical retro evidence is "this is annoying, not informative."

## Refs

- `internal/ops/deploy_failure_signals.go::build:npm-ci-missing-lockfile` — the current classifier signal. Works correctly; this isn't a bug in failure detection, it's a missing pre-deflection in scaffold guidance.
- Retros:
  - `eval/behavioral/runs/20260516-225809/pm-app-czech-byty-run1-replay/self-review.md` (item 1)
  - `eval/behavioral/runs/20260517-044505/pm-app-czech-byty-run1-replay/self-review.md` (item — same run re-replay confirmed)
  - `eval/behavioral/runs/20260517-045943/cadence-multiservice-build-run2-replay/self-review.md` (item 1)
- Candidate atom for the edit: `develop-first-deploy-scaffold-yaml.md` or a new dedicated `develop-greenfield-package-install.md`.
