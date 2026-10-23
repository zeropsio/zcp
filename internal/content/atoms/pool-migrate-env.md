---
id: pool-migrate-env
priority: 3
phases: [pool-migrate-active]
title: "Give a part's pool the dev service's env"
references-fields: []
---

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
