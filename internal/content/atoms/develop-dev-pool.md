---
id: develop-dev-pool
priority: 3
phases: [develop-active]
runtimes: [pool]
environments: [container]
multiService: aggregate
title: "A part's dev pool — lease a container, work, push, release"
references-atoms: [develop-dynamic-runtime-start-container]
references-fields: [ops.DeployResult.Warnings, ops.LeaseView.Reused, ops.LeaseSyncMember.UnwatchedServers, ops.LeaseView.Idle, ops.LeaseIdle.Next, tools.leaseToolResult.Warm]
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
3. Commit and push from the leased container yourself, over SSH to the alias:
   `git push -u origin <branch>` (the first push of a new branch sets its
   upstream). The pool's `GIT_TOKEN` authenticates it. A release never commits
   for you.
4. `zerops_lease action=release lease="<alias>"` hands the leased container
   back: reset, as the pool's warm container, or removed when the pool
   already has one or it runs an outdated version. It refuses while it holds
   commits on no remote branch or uncommitted files, naming them;
   `force=true` discards them. Files git ignores and stashes go either way.

`zerops_lease action=status` lists your leases: `held`, `parked` (stopped
and kept), `unmounted` (the record has no alias or mount), `lost` (its
container is gone, free, or another holder's now; a plain release drops the
record and touches nothing else) or `unknown-holder` (reserved, but no
record of yours). A lease marked `unverified` is one its pool's orchestrator
did not answer about: it may be another holder's, so nothing reads it and
every call that would change it is refused; a later status asks again. It
reads each other lease's activity and unsaved work, changing
nothing: a lease untouched for a day is `idle`, its `idle.next` naming the
release or what a release would lose. `warm` shows each pool's warm
container.

**Preview.** With `previewPortBase` in `.zcp/dev-servers.json`, a lease
answers on `previewPortBase + ZEROPS_StableNumber`, and the dev-server
result's `previewUrl` is the pool's subdomain on that port. With several
servers that listen, mark the one the preview reaches with `"preview": true`.

**Stage.** The part's stage tracks `main`: review a branch on its lease's
preview URL, not on the stage. Once the branch is merged, put main on the
stage from a lease of the part:
`zerops_deploy sourceService="<alias>" targetService="<stage>" sha="origin/main"`
— the deploy fetches `main` first, so this ships main as the remote has it now.
A deploy of a commit that is not on `origin/main`, or of uncommitted changes,
still runs; its `warnings` say the stage no longer tracks main and name the
call that puts main back. Verify a container with
`zerops_verify serviceHostname="<alias>"` — it reports SSH reachability, the
supervisor and the dev servers.

**Data.** Every lease of a part uses the part's dev database. The pool's
service env reaches every leased container, so the database it names — wired
into the pool's env the way a dev service's is — is the one each branch's app
connects to, with no change to the app. A migration one branch runs is
seen by every other branch at once. Keep migrations additive (new tables and
columns; nothing renamed or dropped), and agree a destructive one with
whoever works on the other branches before running it — branches whose
migrations conflict break each other. No lease gets a database or schema of
its own.

**A branch across several parts.** Give the branch one lease per part whose
code the change edits and runs, each part its own pool; a part it only calls
stays unleased.

1. `zerops_lease action=acquire pools=["<pool>","<pool>"] repo="<https clone url>" branch="<branch>"`
   leases one container per pool, or none. A part added later joins with
   `pool="<pool>"` and the same branch; one already held answers
   `reused: true`.
2. `"siblingEnv": {"<VAR>": "<pool>"}` in `.zcp/dev-servers.json` gives each
   server started in a lease `<VAR>` = the hostname of the branch's lease on
   that pool, for server-side `http://${<VAR>}:<port>` calls (a browser
   cannot resolve it). Without such a lease `<VAR>` keeps its own env value:
   set it in the calling part's pool env to the called part's stage
   hostname, and run the called part's dev server on its stage's port. Bind
   servers a sibling calls to `0.0.0.0`: a loopback-only one serves its
   preview, never a sibling. In a declaration several parts share, give each
   entry `"service": "<its pool>"`, or every part's lease runs it.
3. Each lease is a full checkout running its own part's servers. Commit in
   one lease, then `zerops_lease action=sync lease="<alias>"` pushes the
   branch and fast-forwards the other leases (uncommitted files refuse it).
   Sync before you commit in another: a lease with commits of its own or
   changes in the way is left as it was, named. Restart what
   `unwatchedServers` lists.
4. `zerops_lease action=release branch="<branch>"` releases every lease of
   the branch, or none: unsaved work in one refuses them all.

**Pitfalls**

- A deploy of the pool itself changes only containers created after it. A
  leased container keeps its version, and a released one keeps it too, so a
  new acquire can still hand you an older container.
  `zerops_lease action=rollout pool="<pool>"` replaces the free outdated
  containers; a held lease keeps its version until you release it and roll
  the pool out again.
- A release resets a container to its image: what `run.prepareCommands`
  wrote survives, `/var/www` included; everything written or started since
  is gone. Bake the part's dependencies into `/var/www` there, listing every
  file the install reads in `build.addToRunPrepare` — the lockfile, and in a
  workspace monorepo every member's manifest and the workspace definition —
  so the image rebuilds when one of them changes.
  A branch whose lockfile differs declares its incremental install as
  `prepare` in `.zcp/dev-servers.json` (e.g. `npm install --prefer-offline`):
  acquire runs it after the checkout, before the dev servers start, and
  reports it; a failed one leaves the lease held.
- Import the pool with `minContainers: 0`. Containers Zerops creates for the
  minimum cannot be reset, so releasing one deletes it (the release says
  so). The pool's warm container takes their place: after each acquire or
  release one is kept or created through the orchestrator, ready for the
  next acquire at once.
- A container answers SSH only when the pool's `sshIsolation` is set as a
  service env variable admitting the zcp service. The top-level import field
  did not reach pool containers.
- A dev server needs the supervisor, `zcp-devsup`, in the pool's image:
  install it in `run.prepareCommands` and set `run.start: zcp-devsup run`. A
  reused container starts clean — nothing survives its reset but the image,
  not a single process — and `run.start` never runs in it again, so
  `zerops_lease` and `zerops_dev_server` start the supervisor in a lease
  themselves. A pool whose image lacks it is no dev pool: acquire says so and
  `zerops_dev_server` refuses its leases; fix the pool's `zerops.yaml`,
  deploy the pool, release the lease, roll the pool out, then acquire again.
- A per-port subdomain routes only to containers listening on that port. A
  container's `ZEROPS_StableNumber` starts at 1, so the preview ports
  `previewPortBase + 1` through `previewPortBase + maxContainers` must be
  declared in `run.ports` with `httpSupport: true`.
- The pool's service env reaches its containers. The orchestrator answers
  only inside the project.
- A full pool refuses an acquire at once and queues nothing: an acquire
  succeeds only after a lease is released. A pool holds at most ten
  containers — a higher `maxContainers` is capped at ten.
- A lease you will come back to can be parked instead of released:
  `zerops_lease action=park lease="<alias>"` stops its container and keeps
  `/var/www` and `$HOME` (an Ubuntu base empties `/tmp` at every start);
  nothing reaches it, and it still counts
  toward the pool's ten, until `action=unpark` starts it again.
- Nothing releases a lease by itself, however long it sits idle. Status
  and a full pool's refusal list every lease this zcp service holds, other
  conversations' too: ask the person before releasing one this
  conversation did not take.
- Lowering the pool's `maxContainers` below the containers it holds deletes
  the highest-numbered ones, leased ones included, each with its `/var/www`.
- A stopped pool's orchestrator does not answer. A restart, or a stop and
  start, keeps each container's number, hostname, lease, `/var/www` and
  `/home/zerops`; its processes and `/tmp` are gone.
- Never set `envIsolation: none` on the pool: every service of the project
  would get its `ADMIN_TOKEN` and `API_TOKEN`. A service that needs the
  pool's token references it under another name — this zcp service as
  `<pool>_LEASE_TOKEN: ${<pool>_API_TOKEN}`. A variable named
  `<pool>_API_TOKEN` holding `${<pool>_API_TOKEN}` stays the literal text.
- A container answers from anywhere in the project on every port it listens
  on, at `node-id-<n>.runtime.<pool>.zerops`; `run.ports` governs only the
  public subdomain.
- Code in a leased container reads the project's Shared vault values and the
  pool's own; in a project with `envIsolation: none` it also reads every
  other service's values as `<host>_KEY`, the zcp service's `ZCP_API_KEY`
  included. Run there only code you would trust with them —
  `zerops_knowledge uri="zerops://themes/services"`, section "Untrusted code".

The pool's import stanza, setup shape — a workspace monorepo's dependencies
included — and a declaration several parts share:
`zerops_knowledge uri="zerops://themes/services"`, section "A part's dev pool".
