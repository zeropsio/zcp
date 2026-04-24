# Mount vs container — where commands run

Editor tools and framework CLIs run in DIFFERENT places.

## Editor tools on the mount

Local `Read`, `Edit`, `Write`, `Glob`, `Grep` against
`/var/www/<hostname>dev/` work transparently — SSHFS bridges every
read/write. No ssh indirection; it's a normal filesystem.

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
