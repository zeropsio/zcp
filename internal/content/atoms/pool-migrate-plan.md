---
id: pool-migrate-plan
priority: 2
phases: [pool-migrate-active]
title: "Move a part's dev service onto a dev pool"
references-fields: []
---

### Move a part's dev service onto a dev pool

`zerops_workflow workflow="pool-migrate" service="<dev>"` moves one part whose
dev environment is a plain dev service — alone, or the dev half of a
dev/stage pair — onto a dev pool: one leased container per branch instead of
one shared container. Each call reads the project as it is now and answers
with the part's current `step` and the exact `next` call. The call itself
changes nothing: make the `next` call, then call the flow again. After a
context compaction the same call lands on the same step.

The steps, in order (`steps` marks each `done`, `current` or `pending`):

1. **provision** — import the part's pool.
2. **env** — the pool gets the dev service's own env.
3. **image** — deploy the part's pool setup to the pool.
4. **move** — push every branch you keep to the origin.
5. **lease** — lease a container on the work's branch and prove it runs.
6. **ready-to-retire**, **retiring**, **retired** — the old dev service is
   deleted, and only after the person agrees by name.

Until the last step the dev service keeps serving, its dev servers and URL
included.

**Provision.** `next` is `zerops_import` with the pool's stanza in
`next.args.content`; pass it as it is, inside a develop task (an import needs
one). The stanza names the pool `<stem>pool` — the dev hostname without its
`dev` suffix — unless you pass `pool="<hostname>"`; pass the same `pool` on
every later call. It holds `swarm@1`, `minContainers: 0`, `maxContainers: 3`,
the dev service's own `verticalAutoscaling`, and `sshIsolation` in the pool's
`vault` admitting this zcp service: a pool container answers SSH only with it.
It holds no `GIT_TOKEN` — a credential is never written into an import. While
the pool's import still runs, `next` waits on it.

**Several parts.** A dev service serving several parts — one stage per
part — moves each part onto a pool of its own, named after its stage
(`webstage` → `webpool`). `parts` lists every part with its pool and step;
each answer walks one part (`part`, its stage): the one you name with
`service="<its stage>"` or `pool="<its pool>"`, else the first part not yet
ready to retire. Every part walks the steps below on its own pool; the
retire waits for all of them and deletes the dev service once.

**Env and after.** Once the pool exists and its import has finished, the
step is `env` until the pool holds the dev service's env, then `image` until
the pool runs the part's pool setup from a commit, then `move`, `lease` and
`ready-to-retire`. Where the answer names no `next`, `nextUnavailable` says
why. Never copy the dev
service's env by hand: a secret value must not pass through you.

**Refusals.** `status: "not-a-candidate"` lists every reason in `refusals`,
each message naming what it needs:

| reason | meaning |
|---|---|
| `simple-mode`, `local-mode` | only a dev service moves |
| `pool-or-box` | the service already is a pool or a Box |
| `no-origin` | work moves by git through an https origin; outside a Mate set it up with `zerops_workflow action="git-push-setup" service="<dev>"`, in a Mate a develop task wires the pair to HQ |
| `crew-worktrees` | crew lanes stay on a plain dev service, so a dev service holding crew worktrees does not move |
| `variant-b` | a dev service sharing a code volume does not move yet |
| `pool-hostname-taken`, `pool-hostname-invalid` | pass `pool="<free hostname>"`; for several parts each pool is named after its stage |
| `pool-not-a-part` | pass the pool of one of the parts `parts` lists |
| `not-adopted`, `dev-not-live`, `dev-unreadable`, `not-in-container` | the dev service must be adopted and running, and its git readable from the zcp service in the project |
