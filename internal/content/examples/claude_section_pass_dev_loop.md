---
surface: claude-section
verdict: pass
reason: decision-why-ok
title: "CLAUDE.md dev-loop — operational commands for THIS repo"
---

> ## Dev loop
>
> The dev container idles with a no-op start. To work on the API:
>
> ```bash
> # from your local machine
> zcli ssh apidev
>
> # inside the container
> cd /var/www
> npm install           # idempotent; rerun after package.json edits
> npm run start:dev     # nest start --watch on port 3000
> ```
>
> The subdomain `https://<project>-apidev.zerops.app` maps to port 3000
> inside the container — open it in a browser after `npm run start:dev`
> is up.
>
> ## Migrations
>
> Migrations do NOT run on the dev slot (`setup: dev` has no
> `initCommands`). Run them by hand:
>
> ```bash
> # inside the dev container
> npm run migration:run
> # or to roll back one migration
> npm run migration:revert
> ```
>
> ## Container traps
>
> - **SSHFS uid drift**: if you edit a file locally and see `EPERM` when
>   writing from inside the container, the SSHFS mount's uid got out of
>   sync. Exit and re-ssh; the session's uid resets to the container's.
> - **Dev-deps pruning**: `npm prune --omit=dev` is a PROD step, not dev.
>   If you see `Cannot find module '@nestjs/cli'` after boot, something
>   ran the prune step on dev — fix the zerops.yaml `setup: dev` block.

**Why this passes the CLAUDE.md test.**
- Operational, not deploy — SSH commands for THIS repo's dev loop.
- Migration commands the operator runs by hand.
- Repo-specific traps (SSHFS uid, pruning).
- No deploy teaching, no gotchas rerouted, no framework basics assumed
  as new.

Spec §6 test: *"Is this useful for operating THIS repo specifically —
not for deploying it to Zerops, not for porting it to other code?"* —
yes; a human or AI agent working this repo uses every command shown.
