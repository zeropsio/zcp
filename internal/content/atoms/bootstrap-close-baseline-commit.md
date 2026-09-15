---
id: bootstrap-close-baseline-commit
priority: 8
phases: [bootstrap-active]
steps: [close]
routes: [recipe]
environments: [container]
title: "Close bootstrap — write .gitignore, make the baseline commit"
---

### Commit what bootstrap found before calling it done

Bootstrap brings runtimes online; it never stages or commits a single
file that already sits on a dev runtime's working tree. Before treating
the session as finished, check every dev runtime that has files on disk
— one per hostname if more than one was provisioned:

1. Write a `.gitignore` for the stack: dependency directories, build
   output, local caches, and `.env`/`.env.*` files all belong in it.
2. Commit everything else as the baseline, on the service itself over
   SSH — for example:

```
ssh {hostname} "cd /var/www && git add -A && git commit -m 'baseline commit'"
```

<!-- axis-k-keep: signal-#1 — same mount-vs-container-SSH guardrail as develop-first-deploy-write-app, load-bearing here too -->
Never run this from the ZCP-side mount — a mount-side `git init`/`git add` leaves root-owned objects that break every later commit on that service.

`.env` files stay on disk and out of every commit — real configuration
belongs in `zerops_env`, not in git. A service can already carry a single
empty marker commit from its own setup; that commit has no files in it
and is not the baseline. Skipping this step leaves the working tree
uncommitted, and the first self-deploy becomes the moment anyone
discovers what was never tracked.
