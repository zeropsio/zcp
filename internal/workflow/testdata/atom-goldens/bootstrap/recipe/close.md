---
id: bootstrap/recipe/close
atomIds: [bootstrap-recipe-close, bootstrap-verify, bootstrap-close, bootstrap-close-baseline-commit]
description: "Recipe route, close step — bootstrap finishing, agent prompted for handoff to develop."
---
=== bootstrap-recipe-close ===
### Close the recipe bootstrap

Complete the close step:

```
zerops_workflow action="complete" step="close" attestation="Recipe bootstrapped — services active and verified"
```

After close, every service the recipe provisioned appears in the envelope with `bootstrapped: true` and `closeMode: unset`. Close-mode and git-push capability are configured in develop after the first deploy lands; the develop response surfaces the menu when actionable. Start develop next:

```
zerops_workflow action="start" workflow="develop"
```

---

=== bootstrap-verify ===
### Verify infrastructure before closing bootstrap

Bootstrap is infra-only: no code, no deploy, no HTTP probe. Close must
confirm the **platform layer** is healthy before develop starts.

```
zerops_discover
```

Required state for every planned service:

- Platform `status` = `RUNNING` for managed services (databases, caches,
  object storage). A managed service that never reached `RUNNING` means
  the import failed silently — investigate `zerops_process` logs, do
  not close.
- Runtime services: dev / simple runtimes sit at `RUNNING` / `ACTIVE`
  (`startWithoutCode` empty filesystem); stage runtimes wait at
  `READY_TO_DEPLOY` for the first dev → stage cross-deploy — both are
  expected. Code and the first deploy happen in the develop workflow.
- Env vars discovered during provisioning must be recorded in the
  session so develop can wire them without re-discovering.

Do **not** run `zerops_verify` here — that tool probes the app layer
(HTTP reachability, `/status` endpoints) which only makes sense **after**
develop writes code and runs the first deploy. Running it during
bootstrap will report every runtime as failing and is noise.

If a managed service is stuck in a non-`RUNNING` state, bootstrap
hard-stops: surface the failure to the user rather than retrying —
infrastructure issues require the user's judgment.

---

=== bootstrap-close ===
### Closing bootstrap

Bootstrap is **infrastructure-only**. After
`action="complete" step="close"`, planned runtimes show
`bootstrapped: true`: managed services are `RUNNING`, runtimes are
registered, dev containers are SSH-mount-ready, and managed env vars
are discoverable. Classic and recipe-with-first-deploy-later services
show `deployed: false` and enter develop's first-deploy branch. Adopted
services and recipes that deployed during bootstrap show `deployed: true`.

No application code is written, no `zerops.yaml` generated, and no
deploy runs as part of bootstrap close itself.

**Next step — `zerops_workflow action="start" workflow="develop"`.** Develop owns code, the first deploy, verify, iteration, and close-mode setup. Services with `deployed: false` enter the first-deploy branch on develop entry.

Direct tools (`zerops_scale`, `zerops_env`, `zerops_subdomain`, `zerops_discover`) stay callable without a workflow wrapper for one-shot infra changes.

Complete this step before starting develop.

---

=== bootstrap-close-baseline-commit ===
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
ssh appdev "cd /var/www && git add -A && git commit -m 'baseline commit'"
```

Never run this from the ZCP-side mount — a mount-side `git init`/`git add` leaves root-owned objects that break every later commit on that service.

`.env` files stay on disk and out of every commit — real configuration
belongs in `zerops_env`, not in git. A service can already carry a single
empty marker commit from its own setup; that commit has no files in it
and is not the baseline. Skipping this step leaves the working tree
uncommitted, and the first self-deploy becomes the moment anyone
discovers what was never tracked.
