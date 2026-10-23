---
id: develop-dev-pool
priority: 3
phases: [develop-active]
runtimes: [pool]
environments: [container]
multiService: aggregate
title: "A part's dev pool — lease a container, work, push, release"
references-atoms: [develop-dynamic-runtime-start-container]
---

### A part's dev pool

A pool (`swarm@1`) is not an app runtime: it holds up to ten containers that
its orchestrator hands out by lease, one per branch you work on. The pool's
own hostname reaches a random container — address a leased one by its alias.

**The loop**

1. `zerops_lease action=acquire pool="<pool>" repo="<https clone url>" branch="<branch>"`
   leases a clean container with no expiry. It writes the alias
   `<pool>-<number>` (SSH), mounts the leased container at `/var/www/<alias>` and
   checks the branch out there; a branch that does not exist yet is cut from
   `main`. `repo` is remembered per pool — pass it on the first acquire only.
   The clone uses the pool's own `GIT_TOKEN` service secret; you never type
   a credential.
2. Work in `/var/www/<alias>`. Run the part's dev servers with
   `zerops_dev_server action=start hostname="<alias>" ...`.
3. Commit and push from the leased container yourself (over SSH to the alias). A
   release never commits for you.
4. `zerops_lease action=release lease="<alias>"` resets the leased container
   and returns it. It refuses while it holds commits on no remote
   branch or uncommitted files, naming them; `force=true` discards them.

`zerops_lease action=status` lists your leases: `held`, `unmounted` (the
record has no alias or mount), `lost` (its container is gone or no longer
reserved) or `unknown-holder` (reserved, but no record of yours).

**Preview.** With `previewPortBase` in `.zcp/dev-servers.json`, a lease
answers on `previewPortBase + ZEROPS_StableNumber`, and the dev-server
result's `previewUrl` is the pool's subdomain on that port. With several
servers that listen, mark the one the preview reaches with `"preview": true`.

**Stage.** `zerops_deploy sourceService="<alias>" targetService="<stage>"`
deploys the part's stage from the lease (add `sha` to deploy an exact commit).
Verify a container with `zerops_verify serviceHostname="<alias>"` — it
reports SSH reachability, the supervisor and the dev servers.

**Pitfalls**

- A deploy of the pool itself changes only containers created after it. A
  leased container keeps its version, and a released one keeps it too: the
  orchestrator replaces outdated free containers only when the pool is rolled
  out, so a new acquire can still hand you an older container.
- A reused container's `/var/www` comes back empty. Dependencies baked into
  the pool image survive only outside `/var/www` (installed in
  `run.prepareCommands`, e.g. under `/opt`).
- A container answers SSH only when the pool's `sshIsolation` is set as a
  service env variable admitting the zcp service. The top-level import field
  did not reach pool containers.
- A dev server needs the supervisor: the pool's setup must set
  `run.start: zcp-devsup run` and install `zcp-devsup` in
  `run.prepareCommands`. Without it the dev-server tool refuses the lease;
  fix the pool's `zerops.yaml`, deploy the pool, then acquire again.
- A per-port subdomain routes only to containers listening on that port: the
  preview ports (`previewPortBase` through `previewPortBase + maxContainers
  - 1`) must be declared in `run.ports` with `httpSupport: true`.
- The pool's service env reaches its containers. The orchestrator answers
  only inside the project.

The pool's import stanza and setup shape:
`zerops_knowledge uri="zerops://themes/services"`, section "A part's dev pool".
