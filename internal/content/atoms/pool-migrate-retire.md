---
id: pool-migrate-retire
priority: 3
phases: [pool-migrate-active]
title: "Lease the pool, then retire the old dev service"
references-fields: []
---

### Lease the pool, then retire the old dev service

Once the pool runs the part's pool setup, three steps remain.

**Move.** While `census` names work the origin lacks, the step is `move`:
push each branch you keep. Commits only a reflog reaches (`reflogOnly`)
never hold the retire — its bundle keeps every one of them.

**Lease.** With nothing left to move, `next` is
`zerops_lease action="acquire" pool="<pool>" branch="<work's branch>"`:
a container of the pool checks the work's branch out from the dev
service's origin. Start its dev servers on the lease's alias and prove it
runs with `zerops_verify serviceHostname="<alias>"`. Whatever you did not
push is visibly absent from it.

**Ready to retire.** Once this zcp holds a lease of the pool on the work's
branch — of every part's pool, for several parts — `next` is
`zerops_workflow workflow="pool-migrate" action="retire" service="<dev>"`.
Deleting a service is permanent, so the retire takes two calls:

1. The first call reads everything again and changes nothing. It answers
   `DIAGNOSIS_REQUIRED` with `wouldDestroy`: the dev service and the id it
   is deleted by, `unsavedWork` — what the dev holds that its origin lacks
   — and `notKept`, what no copy keeps.
2. Ask the person whether to delete the dev service, by its name. Only once
   they agreed, re-send the call with
   `confirmDestructive={"operation":"pool-retire","acknowledgedTargets":["<dev>"]}`
   (`wouldDestroy.retryCall` is that call). While `unsavedWork` names
   anything, the call also needs `force=true` — only when the person agreed
   to leaving that work; otherwise push it and call again.

The retire refuses (`status: "retire-refused"`, nothing changed), naming
each reason:

| reason | what to do |
|---|---|
| `not-ready` | make the step's `next` call first |
| `no-lease`, `lease-unverified`, `lease-unreadable` | lease the pool on the work's branch and verify the lease |
| `dev-busy` | wait for the dev service's process to finish |
| `dev-deleting` | a delete outside the flow runs; call again once it finished |
| `launch-active` | finish or reset the production launch first |
| `env-reference` | point every `${<dev>_…}` reference elsewhere |
| `domain` | the person moves the domain in the dashboard |
| `unsaved-work` | push the work, or `force=true` after the person agreed |
| `pair-busy` | another step holds the pair; call again |
| `volume-mounted` | a stage or pool still mounts the code volume: remove `run.volume` from its setup in `zerops.yaml`, commit, deploy it, call again |
| `volume-not-live` | the code volume the pair names is gone or unreadable; call again |

**What the acknowledged call does.** It takes the dev service's mount on
this container down, then writes a verified bundle of its repository —
every branch, every stash entry, every commit a reflog reaches, each
worktree's HEAD, its staged and unstaged edits and its untracked files —
to this container, never a `.env` file or an ignored file. It keys the
pair on the pool — for several parts, one pair per part, each keyed by its
pool — checks right before the delete that the repository did not move
since the bundle, and deletes the dev service by its id. The
answer's `retire` names the bundle and `retire.restore`, the commands that
put everything back in an empty directory.

**A repository on a code volume.** When the pair's dev services share their
repository on a code volume — a local-storage service each mounts through
`run.volume` — `wouldDestroy` names every dev service and the volume, and the
acknowledgment names them all. The census and the bundle read the volume's
checkout. The retire deletes every dev service first: a dev service's delete
leaves the volume and its files. It deletes the volume last, and only that
step loses the checkout for good — a volume created later under its hostname
is new and empty. Zerops refuses that delete while any service still
mounts the volume, without saying which: the answer reads `retiring` with
`volume-mounted`. Remove `run.volume` from that service's setup and deploy it
— a deploy replaces its container without the mount — or delete the service,
then re-send the acknowledged call. If the checkout changed while the dev
services were going, nothing on the volume is deleted until the person agrees
and the call carries `force=true`; a new bundle is taken from the volume
first — the newer copy, which the retired answer names with
`retire.codeVolume.restore`.

If the repository moved meanwhile, or a step fails before the delete, the
retire stops: `status: "aborted"`, the pair keyed by the dev service again,
its mount back, nothing deleted. Call the retire again once nothing writes
to it.

**Retiring.** A retire cut short — a delete still running, a lost call —
reads `retiring`; re-send the acknowledged call and it resumes from where
it stopped, never deleting anything twice. **Retired**: the dev service is
gone, its URL with it; the stage deploys from a lease of the pool
(`zerops_deploy sourceService="<alias>" targetService="<stage>"`).
`retire.gaps` names what a migrated part does not have yet.
