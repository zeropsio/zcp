---
surface: claude-section
verdict: fail
reason: wrong-surface
title: "CLAUDE.md section restates deploy instructions that belong in IG"
---

> ## Deploying this application
>
> When you are ready to ship, commit your changes and let the CI
> pipeline pick up the new tag. Zerops will:
>
> 1. Run `npm ci` during the build phase.
> 2. Run `npm run build` to produce the `dist/` artifact.
> 3. Deploy files matching `deployFiles: ./dist/~` into the container.
> 4. Restart the service with `start: node dist/main.js`.
>
> You can also add `deploy.readinessCheck` to the prod setup to gate
> traffic during rolling deploys at `minContainers: 2` on env 4+.

**Why this fails the CLAUDE.md test.**
This is deploy content — the mechanics of how a production deploy
happens. CLAUDE.md is about OPERATING this repo (dev loop, migrations,
container traps), not deploying it. The content is not wrong; it's on
the wrong surface.

**Correct routing**: spec §6 test *"Is this useful for operating THIS
repo specifically — not for deploying it to Zerops, not for porting
it to other code?"* — the listed bullets are deploy teaching. Route:
- build pipeline + deployFiles + start-command teaching → per-codebase
  `zerops.yaml` comments (the writer authors those)
- readinessCheck at `minContainers: 2` → IG item (porter-facing) or
  zerops.yaml comment (scaffold decision)
- A CLAUDE.md operator reading "deploying this application" gains
  nothing actionable for their immediate task (editing this repo).
