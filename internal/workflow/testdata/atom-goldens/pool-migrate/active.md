---
id: pool-migrate/active
atomIds: [pool-migrate-plan, pool-migrate-env, pool-migrate-move]
description: "Pool-migrate workflow on a part's dev/stage pair — the read side names the provision step and renders the pool's import stanza."
---
=== pool-migrate-plan ===
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

**Env and after.** Once the pool exists and its import has finished, the
step is `env` until the pool holds the dev service's env, then `image`. Where
the answer names no `next`, `nextUnavailable` says why. Never copy the dev
service's env by hand: a secret value must not pass through you.

**Refusals.** `status: "not-a-candidate"` lists every reason in `refusals`,
each message naming what it needs:

| reason | meaning |
|---|---|
| `simple-mode`, `local-mode` | only a dev service moves |
| `pool-or-box` | the service already is a pool or a Box |
| `no-origin` | work moves by git through an https origin; outside a Mate set it up with `zerops_workflow action="git-push-setup" service="<dev>"`, in a Mate a develop task wires the pair to HQ |
| `crew-worktrees` | crew lanes stay on a plain dev service, so a dev service holding crew worktrees does not move |
| `variant-b`, `multi-part` | a dev service sharing a code volume, or serving several parts, does not move yet |
| `pool-hostname-taken`, `pool-hostname-invalid` | pass `pool="<free hostname>"` |
| `not-adopted`, `dev-not-live`, `dev-unreadable`, `not-in-container` | the dev service must be adopted and running, and its git readable from the zcp service in the project |

---

=== pool-migrate-env ===
### Give a part's pool the dev service's env

Once the pool exists, every answer carries `env`: the pool's own service env
against the dev service's. While they differ the step is `env`, and `next` is
`zerops_workflow workflow="pool-migrate" action="pool-env" service="<dev>"`.
That call copies the dev service's own service env to the pool itself, and
writes only the keys whose value differs:

- a plain value as the dev service holds it, `${...}` references included;
- a secret read in the dev service's container and written to the pool as a
  secret — its value never reaches you, so never copy one by hand;
- not the keys the dev service's `zerops.yaml` sets: they reach the pool with
  the pool setup in the image step; not project env, which every service
  already reads.

Nothing restarts. A process already running in a pool container keeps the env
it started with; a new shell session or a process started after the step
reads the new values. Each call reads both sides again, so calling the step
twice changes nothing the second time. The step also makes the dev service's
origin the repository a lease of the pool clones when it names none
(`env.defaultRepo`).

What the answer names:

| field | meaning |
|---|---|
| `env.toWrite` | keys the step would write (`add` or `update`) |
| `env.written` | keys the step wrote |
| `env.unchanged` | keys the pool already holds as the dev service does |
| `env.present` | secrets the pool holds, compared only when the step runs |
| `envRequests` | secrets the step could not read in the dev container: make each call as given — the person gives the value, never in the chat |
| `env.failed` | writes that did not take: call the step again |
| `env.neverCopied` | keys the step never writes, each with its reason |
| `env.stageDiff` | per stage, keys the stage has that the pool lacks (`stageOnly`) and the reverse (`poolOnly`) |

The values are the dev service's. A key set that differs from the stage's is
named, never aligned: tell the person, and change a key only when they ask.

`env.neverCopied` reasons:

| reason | meaning |
|---|---|
| `pool-credential` | `API_TOKEN` and `ADMIN_TOKEN` are the pool's own orchestrator tokens. An app value under one of those names cannot sit on the pool: give it another name there and reference that name in the pool setup |
| `pool-ssh-isolation` | the pool's own `sshIsolation`, set when it was imported |
| `zcp-owned` | a credential or setting zcp or the Mate app keeps for itself |
| `pool-zerops-yaml` | the pool's deployed `zerops.yaml` sets the key, so no service env sits under it |
| `platform` | a name Zerops keeps for itself |

**`GIT_TOKEN`** never holds the step; `env.gitToken.state` says where it is:

| state | what to do |
|---|---|
| `mate-credential` | nothing: in a Mate the pool's `GIT_TOKEN` is the Mate's own HQ credential, put there when a lease of an HQ repository takes a container |
| `ask` | ask the person whether the pool may get the dev service's `GIT_TOKEN` — every container a lease of the pool takes reads it. On yes, call the step again with `copyGitToken=true`. On no, the person sets the pool's own in the Zerops dashboard |
| `on-pool` | the pool holds one already, left as it is |
| `copied`, `unchanged` | the pool holds the dev service's |
| `dev-has-none`, `unreadable` | a lease of a private repository needs one: the person sets it on the pool in the Zerops dashboard |
| `failed` | call the step again with `copyGitToken=true` |

Never ask for a `GIT_TOKEN` value in the chat, and never make one up.

Once the pool holds the dev service's env the step is `image`, and `env`
rides on every later answer: a key added to the dev service later makes the
step `env` again.

---

=== pool-migrate-move ===
### Move a part's work through its origin

A pool's leased container starts empty and takes its branch from the origin,
so the part's work moves by git, and what the dev service holds that the
origin lacks stays behind with it. Every step's answer carries `census` — the
dev service's repository as it is now, its main checkout first and then every
worktree — and `workBranch`, the branch a lease of the pool checks out.
`census.clean: true` means nothing is left to move.

Each item the census names, and how to keep it (you commit and push; the
call commits nothing):

| census | what it is | to keep it |
|---|---|---|
| `trees[].staged`, `modified`, `untracked`, `conflicted` | uncommitted work in that checkout | commit it on a branch you keep, then push it |
| `trees[].inProgress` | a merge, rebase, cherry-pick, revert or bisect left unfinished | finish it (`--continue`) or abort it (`--abort`) |
| `trees[].detachedCommits` | commits only a detached HEAD holds | `git switch -c <branch>` in that checkout, then push it |
| `branches[]` | a local branch's commits on no remote branch | `git push -u origin <branch>` |
| `stashes[]` | stash entries | `git stash branch <branch> <ref>` with the entry's `ref`, then commit and push; drop the ones you do not keep |
| `reflogOnly` | commits only a reflog still reaches: reset, amended or rebased away | `git branch <branch> <commit>` and push the ones you keep |
| `trees[].envFiles` | `.env` files, which git does not carry | move each value into the pool's service env with `zerops_env`, then delete the file |

`trees[].ignored` and `t3Refs` are named for the record, not moved: ignored
files come back from the repository (dependencies, build output), and refs
under `refs/t3/` are the Mate app's own.

**The work's branch.** `workBranch.branch` is the branch a lease checks out:
in a Mate the open change's branch (`source: change`), or without an open
change the dev checkout's own branch (`current`); elsewhere the dev
checkout's branch's upstream on origin (`upstream`). Where
`workBranch.missing` names none yet, it says what names one. In a Mate, HQ
takes a push to any branch but `main`, and to a `mate/` branch only as an
open change's.

Call the flow again after moving: each moved item drops out of the census,
and what it still names is what the old dev service would take with it.
