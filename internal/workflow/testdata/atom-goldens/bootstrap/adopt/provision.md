---
id: bootstrap/adopt/provision
atomIds: [bootstrap-env-var-discovery, bootstrap-adopt-baseline-commit]
description: "Adopt route, provision step — complete step=provision just returned; agent checks the adopted runtime's repo state before reporting adopt done."
---
=== bootstrap-env-var-discovery ===
### Discover env vars during provision

Once newly-provisioned (classic) or newly-attached (adopt) services have reached RUNNING / ACTIVE, run discovery so the session records env-var KEYS for every managed service. This is authoritative — do not guess alternative spellings; unknown cross-service references become literal strings at runtime and fail silently.

```
zerops_discover includeEnvs=true
```

Record one row per service in the provision attestation. Keys are enough — values stay redacted; discovery is for cataloguing, not consumption. The develop response covers per-service canonical key names plus cross-service reference syntax (`${hostname_varName}`) when wiring `run.envVariables` at first deploy.

**Adopt route — skip when no new wiring:** adopted services already carry their env wiring in the running app, so this discovery is only needed if THIS task adds NEW cross-service references. For a code-only change to an already-wired app (edit / redesign / bugfix), skip it and fetch keys lazily at wiring time — running it now is a no-op round-trip.

**Pre-first-deploy caveat (classic route)**: classic creates runtime services with `startWithoutCode: true` so they reach RUNNING before any code lands; env vars in such containers live in the project catalogue, not `process.env`, until develop runs the first deploy and references fire. Adopted services are usually ACTIVE.

When `zerops_discover` shows a runtime stuck at `status=READY_TO_DEPLOY`, branch on whether it ever tried to build (check `zerops_events`):

- **Never built** (created without `startWithoutCode: true`, no failed build in the timeline): re-import with `startWithoutCode: true` + `override: true` to reach ACTIVE. Safe — there is no deployed code to lose.
- **Build FAILED** (the timeline shows a failed build / prior deploy attempt): the service still holds the buildFromGit code that failed to build. DIAGNOSE first — `zerops_events` then `zerops_logs` — fix the cause (e.g. add the missing managed dependency the build needed), then re-deploy. Do **NOT** `override`: it REPLACES the service stack and wipes the very source you need to fix. (`override=true` on a service with deploy history returns `DIAGNOSIS_REQUIRED`; acknowledging `confirmDestructive` still wipes — only do it if the code lives elsewhere, e.g. git.)

---

=== bootstrap-adopt-baseline-commit ===
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
ssh appdev "cd /var/www && git add -A && git commit -m 'baseline commit'"
```

Never run this from the ZCP-side mount — a mount-side `git init`/`git add` leaves root-owned objects that break every later commit on that service.

A runtime that already carried history (`repo.provenance: existing`) is
left exactly as adopt found it — no `.gitignore`, no commit; that
history is the user's, not a starting point to overwrite. `.env` files
stay on disk and out of every commit — real configuration belongs in
`zerops_env`, not in git.
