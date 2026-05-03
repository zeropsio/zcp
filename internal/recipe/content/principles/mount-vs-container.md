# Mount vs container — where commands run

Editor tools and framework CLIs run in DIFFERENT places.

## Editor tools on the mount

Local `Read`, `Edit`, `Write`, `Glob`, `Grep` against
`/var/www/<hostname>dev/` work transparently — SSHFS bridges every
read/write. No ssh indirection; it's a normal filesystem.

Only `<hostname>dev` is the source mount. `<hostname>stage` is a
deployed runtime — verify via `zerops_verify` / `zerops_logs` / URL.
Never search `/var/www/<hostname>stage` for source.

## Framework CLIs via ssh to the container

`npm install`, `npx build`, `tsc`, `nest build`, `artisan`, `composer`,
`pip`, `bun`, `curl localhost` — all run in-container:

```
ssh <hostname>dev "cd /var/www && npm install"
ssh <hostname>dev "cd /var/www && php artisan migrate"
ssh <hostname>dev "cd /var/www && curl -s http://localhost:3000/health"
```

Two reasons:

1. **Correct environment.** The container has the right runtime
   version, package-manager cache, and platform-injected env vars
   (like `${apidev_zeropsSubdomain}` for build-time URL aliases). A
   local build misses all of this.
2. **FUSE cost + semantics.** Running the process locally tunnels
   every file IO through SSHFS (10-100× slower than native), and the
   artifacts don't match what the deploy's in-container build
   produces.

## Don't run local builds to "pre-verify"

`zerops_deploy` runs `npm ci` + `npm run build` on a fresh in-
container filesystem. A local build on the mount gates nothing the
deploy won't also check, and its artifacts don't match (different
env, different cache, different runtime).

## One-shot vs long-running

Framework CLIs are one-shot — they exit in seconds, no channel-
lifetime concern. Dev servers are long-running — they need
`zerops_dev_server` (see `principles/dev-loop.md`), not ssh.

## During feature phase: edit in place, do not redeploy dev slots

The dev container's source tree is SSHFS-mounted at
`/var/www/<hostname>dev/`. Edit/Write tool changes flow through the
mount immediately — they're live in the container the moment your
tool call returns.

The dev server (started via `zerops_dev_server`) picks up source
changes via the framework's watch mode (`nest --watch`, Vite HMR,
`vite dev`, Next.js dev, etc.). **No redeploy is needed for code
changes.**

When you change `zerops.yaml` env vars during feature phase,
restart the dev server via
`zerops_dev_server action=restart hostname=<host>dev` — NOT a
redeploy. The runtime container's env is re-read on dev-server
respawn.

Cross-deploy to stage (e.g. `zerops_deploy targetService=apistage`)
is the only legitimate `zerops_deploy` during feature phase. That's
the "snapshot to production-shape" step. Verify-in-place via SSH or
`zerops_browser` against the dev URL until the feature works; deploy
to stage once at the end of the feature.

Forbidden in feature phase:

- `zerops_deploy targetService=<host>dev` (apidev, appdev,
  workerdev — any dev slot). The mount IS the source of truth; a
  deploy doesn't make the source any more "live" than it already
  is.
- `zerops_deploy` triggered "to make new code live" — code is
  already live via SSHFS the moment your Edit/Write returns.
- `zerops_deploy` triggered "to apply env-var changes" — restart
  the dev server instead.

The codebase yaml comments encode this as the recipe's own
documented pattern (e.g. *"Idle the container so the porter owns
the long-running dev process"*). Honor it during feature phase too
— don't redeploy dev slots to thrash the mount-state you just
edited.

## zcli scope

`zcli` is a host-side tool. Inside the dev container (over `ssh
<host>`) the binary is not available — DO NOT use `zcli env get`,
`zcli vpn`, or other zcli verbs in container-side commands. The
container has the platform-injected env vars (`$DB_PASSWORD`,
`$NATS_USER`, etc.) by construction; read those instead. If a
project-level secret needs fetching from outside the container, run
`zcli` on the host shell, never tunneled through ssh.
