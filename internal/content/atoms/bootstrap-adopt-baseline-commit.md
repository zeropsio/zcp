---
id: bootstrap-adopt-baseline-commit
priority: 5
phases: [bootstrap-active]
routes: [adopt]
steps: [provision]
environments: [container]
title: "Adopt — write .gitignore, make the baseline commit"
references-fields: [workflow.RepoStatus.Provenance]
---

### Commit an adopted runtime's uncommitted tree before calling adopt done

`complete step="provision"` is what actually mounts and initializes an
adopted service's repository. Check each adopted dev runtime once that
call returns, before reporting adopt as done — a runtime whose tree came
back uncommitted (`repo.provenance: initialized` in this response's
envelope — `services[].repo`, the same block `zerops_workflow
action="status"` carries — or `git status` over SSH shows
untracked/staged content) still needs:

1. A `.gitignore` for the stack: dependency directories, build output,
   local caches, and `.env`/`.env.*` files all belong in it.
2. The baseline commit, on the service itself over SSH — for example:

```
ssh {hostname} "cd /var/www && git add -A && git commit -m 'baseline commit'"
```

<!-- axis-k-keep: signal-#1 — same mount-vs-container-SSH guardrail as develop-first-deploy-write-app, load-bearing here too -->
Never run this from the ZCP-side mount — a mount-side `git init`/`git add` leaves root-owned objects that break every later commit on that service.

A runtime that already carried history (`repo.provenance: existing`) is
left exactly as adopt found it — no `.gitignore`, no commit; that
history is the user's, not a starting point to overwrite. `.env` files
stay on disk and out of every commit — real configuration belongs in
`zerops_env`, not in git.
