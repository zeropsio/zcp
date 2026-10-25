---
id: develop/dev-pool-dev-service
atomIds: [develop-intro, develop-strategy-review, develop-change-drives-deploy, develop-dev-server-triage, develop-self-deploy-reproducibility, develop-checklist-dev-mode, develop-dev-pool, develop-dynamic-runtime-start-container, develop-knowledge-pointers, develop-user-code-box, develop-auto-close-semantics, develop-dev-server-reason-codes, develop-verify-matrix, develop-mode-expansion]
description: "A deployed dev-mode service whose project also holds a Swarm pool the part's dev servers run in by lease. The dev-pool guidance fires only because a pool is present."
---
=== develop-intro ===
### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

---

=== develop-strategy-review ===
### DECISION — pick a close-mode now (auto-close stays BLOCKED until set)

Close-mode is `unset` on the listed services — auto-close stays blocked no matter how much you deploy + verify. Set it per in-scope service; it can precede the first deploy. This is the one call that unblocks auto-close:

```
zerops_workflow action="close-mode" closeMode={"partdev":"auto"}
```

Swap `auto` for the close-mode you want:

- `auto` — agent runs `zerops_deploy` directly via zcli; auto-close fires once scope-services are green. Fast for tight iteration.
- `manual` — **you** drive every deploy; ZCP records evidence, never deploys, auto-close stays open until you call `action="close"`.

Delivery is a SEPARATE dimension from close-mode: to deliver via git push, run `action="git-push-setup"` (then `zerops_deploy strategy="git-push"`); CI wiring is `action="build-integration"`. Both work under either close-mode — close-mode only owns the auto-close gate + iteration cadence, not how you deliver.

close-mode does NOT change what `action="close"` does (always session-teardown) — it selects the per-mode iteration guidance and drives the auto-close gate.

---

=== develop-change-drives-deploy ===
### Every code change must reach a durable state

Iteration cadence is mode-specific:

- Dev-mode dynamic runtime: edit code in place; reload via
  `zerops_dev_server` (no full redeploy for code-only changes).
- Simple / standard / local / first-deploy: every change →
  `zerops_deploy`.

Once close-mode is `auto` and every resolved deploy
target is deployed + verified, the work session auto-closes.

---

=== develop-dev-server-triage ===
### Dev-server state triage

Before deploying, verifying, or iterating on a runtime service, run
the triage rather than blind-starting a process.

**Step 1 — Determine the expectation** from `runtimeClass` + `mode`
in the envelope:

Only `runtimeClass: dynamic` + `mode: dev` needs a manual dev-server
action — its idle runtime container (`run.start: zsc noop --silent`) waits
for `zerops_dev_server action=start hostname="<dev>"`. Implicit-webserver, static, and
dynamic + simple/stage are platform-owned post-deploy; triage ends there.

**Step 2 — Check current state** for dev-mode dynamic:

```
# container env
zerops_dev_server action=status hostname="partdev" port={port} healthPath="{path}"

# local env — runs on your machine
Bash command="curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:{port}{path}"
```

Read the response:

- `running: true` with HTTP 2xx/3xx/4xx `healthStatus` → proceed to
  `zerops_verify`.
- `running: false` with `reason: health_probe_connection_refused` →
  start (step 3).
- `running: true` with `healthStatus: 5xx` → server runs but is
  broken; read logs and response body; do NOT restart (does not
  fix bugs). Edit code, then iterate per the mode-specific cadence
  (dev: edit + dev-server reload; simple/standard/local: redeploy).

For workers with no HTTP surface (`port=0`, `healthPath=""`), skip
HTTP status; call `zerops_logs` to confirm consumption.

**Step 3 — Act on the delta.**

```
# container env
zerops_dev_server action=start hostname="partdev" command="{start-command}" port={port} healthPath="{path}"

# local env
Bash run_in_background=true command="{start-command}"
```

A redeploy replaces the runtime container: its supervisor starts your dev
servers again right after it — read the deploy response's `devServer`
before `zerops_verify`; start one only when it did not come back.

---

=== develop-self-deploy-reproducibility ===
### The dev container is disposable; the repository isn't

A self-deploy replaces the dev container with a fresh one. Nothing on the
old dev container's disk survives that swap except what git carries with
it — the vault (values the `zerops.yaml` references, never a file) and
managed-service data (Postgres, object storage, …) are the other two
persistence mechanisms, and neither lives on the dev container's
filesystem either. A file that exists only on today's dev container — an
untracked script, a local SQLite DB, a `.env` — is gone the moment the new
container replaces it.

`zerops_deploy`'s response on a self-deploy carries this as data, not a
guess: `notCarried` names the git-ignored paths (count, bytes, a sample)
that will not exist in the new container, and `envFiles` lists any
`.env`/`.env.*` files found regardless of ignore state — values to
move into the vault with `zerops_env` (secrets `sensitive=true`)
and reference from `run.envVariables`, not carry forward as a workaround.
`repoState` reports whether the source is clean, dirty, mid-merge, mid-
rebase, or on a detached HEAD at deploy time.

### After bootstrap or adopt, commit before changing anything

Once a runtime's working tree is ready to iterate on — right after
bootstrap writes the scaffold, or right after adopt inherits an existing
one — write a `.gitignore` for the stack (build output, dependency
directories, local env files) and make a baseline commit before making any
other change. Every self-deploy after that point is then reproducible from
that commit forward; skipping this step means the first self-deploy is the
first moment anyone discovers what wasn't tracked.

---

=== develop-checklist-dev-mode ===
### Checklist (dev-mode dynamic-runtime services)

Applies to **dynamic runtimes only** (Node, Bun, Deno, Go, Rust, Python,
Ruby, Java, .NET — anything with a long-running app process under
manual control). For implicit-webserver runtimes (`php-apache`,
`php-nginx`) the implicit-webserver guidance fires instead; for static
runtimes the web server auto-starts and this checklist does not apply.

- Dev setup block in `zerops.yaml`: **`run.start: zsc noop --silent`**
  (a no-op keepalive), **no** `healthCheck`. You start the real dev
  process yourself via `zerops_dev_server action=start hostname="<dev>"`; its supervisor
  starts it again after later redeploys and restarts, until `action=stop`.
- Stage setup block (if a dev+stage pair exists): real `start:`
  command **plus** a `healthCheck`. Stage auto-starts on deploy and
  Zerops probes it on its configured interval.

---

=== develop-dev-pool ===
### A part's dev pool

A pool (`swarm@1`) is not an app runtime: it holds up to ten containers that
its orchestrator hands out by lease, one per branch you work on. The pool's
own hostname reaches a random container — address a leased one by its alias.

**Which pool.** All of this is for a part's dev pool: its zerops.yaml setup
starts `zcp-devsup run`, or the repository's `.zcp/dev-servers.json` names it
as a server's `service`. A pool the app calls through its orchestrator with
`${<pool>_API_TOKEN}` is infrastructure the app uses: leave it to the app —
never lease it, run a dev server on it or deploy to it; a lease takes one of
its containers away from the app.

**The loop**

1. `zerops_lease action=acquire pool="<pool>" repo="<https clone url>" branch="<branch>"`
   leases a clean container with no expiry. It writes the alias
   `<pool>-<number>` (SSH), mounts the leased container at `/var/www/<alias>` and
   checks the branch out there; a branch that does not exist yet is cut from
   `main`, or from the branch `from="<branch>"` names. `repo` is remembered
   per pool — pass it on the first acquire only.
   The clone uses the pool's own `GIT_TOKEN` service secret — for a
   repository on this Mate's HQ, the HQ credential zcp puts on the pool
   itself as `ZCP_HQ_GIT_TOKEN`; you never type a credential. Each lease it
   took carries `devServers`: each server's `status` — `running`, or
   `starting` while it still compiles — and the preview server's `url`.
   Check a `starting` one with
   `zerops_dev_server action=status hostname="<alias>" name="<server>"`.
2. Work in `/var/www/<alias>`. Run the part's dev servers with
   `zerops_dev_server action=start hostname="<alias>" ...`.
3. Commit and push from the leased container yourself, over SSH to the alias:
   `git push -u origin <branch>` (the first push of a new branch sets its
   upstream). The same pool secret authenticates it. A release never commits
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
servers that listen, mark the one the preview reaches with `"preview": true`
— one per pool.

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
   that pool, for server-side `http://${<VAR>}:<port>` calls. A browser
   cannot resolve it: for code that runs in the browser, write
   `"siblingPreviewEnv": {"<VAR>": "<pool>"}` instead, and `<VAR>` is that
   lease's preview URL (`https://`), resolved again whenever a lease of the
   branch joins or leaves — never copy it by hand. A name is in one of the
   two, never both. Without such a lease `<VAR>` keeps its own env value:
   set it in the calling part's pool env to the called part's stage
   hostname (a browser variable to the stage's public URL), and run the
   called part's dev server on its stage's port. Bind
   servers a sibling calls to `0.0.0.0`: a loopback-only one serves its
   preview, never a sibling. In a declaration several parts share, give each
   entry `"service": "<its pool>"`, or every part's lease runs it.
3. Each lease is a full checkout running its own part's servers. Commit in
   one lease, then `zerops_lease action=sync lease="<alias>"` pushes the
   branch and fast-forwards the other leases (uncommitted files refuse it).
   Sync before you commit in another: a lease with commits of its own or
   changes in the way is left as it was, named. Restart only what
   `unwatchedServers` lists (runtime entries); declared servers reload by
   themselves.
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
- A lease's dev servers and every command you run in it share its
  container's RAM. An SSH command that ends with exit 255 and no output was
  most likely killed for memory: run heavy checks one at a time and
  single-threaded where a check allows it, or size the pool's `minRam` for
  the dev server plus the heaviest check.
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

---

=== develop-dynamic-runtime-start-container ===
### Dynamic-runtime dev server

A dev server runs under the dev service's supervisor (`zcp-devsup`, the dev
setup's `run.start`); its first start declares it in `.zcp/dev-servers.json`.
If the dev setup still runs `zsc noop`, that start adds the supervisor's two
lines and redeploys once (uncommitted work stays) — commit both files.
Every deploy starts each `autostart` server (see `devServer`), a restart
each not `stop`ped; a start crash is retried once, a later one stays down.
A passing verify means "live now", not "durably shipped" — for an
always-on service use simple mode.

- `command` is the app's dev-server command (e.g. `npm run dev`), not the
  dev setup's `run.start`.
- `name` each server when one dev service runs several (`web`, `api`);
  stopping one leaves the others running.
- The health probe runs on localhost inside the dev container, so the app
  must bind `0.0.0.0`, not loopback, for the response's `url` to reach it. Read
  `running`, `healthStatus`, `reason` and `logTail` before another call.

Don't hand-roll `ssh partdev "cmd &"`: the SSH session ends with
the call and kills the process.

---

=== develop-knowledge-pointers ===
### Knowledge on demand — pull extra context

When the embedded guidance isn't enough, these are the canonical lookups:

- **`zerops.yaml` schema / fields** — `zerops_knowledge query="zerops.yaml schema"`
- **Runtime docs** (build tools, start commands, conventions) —
  `zerops_knowledge query="<runtime>"` (e.g. `nodejs`, `go`, `php-nginx`, `bun`);
  match the service's base stack.
- **Env var keys** (no values, safe) — `zerops_discover includeEnvs=true`
  (`includeEnvValues=true` only to troubleshoot).
- **Deeper platform topics** (infra changes, scaling, status codes,
  managed-service categories) — `zerops_knowledge query="<topic>"`.

---

=== develop-user-code-box ===
### A feature that runs code the app's users send

A feature that runs code the app does not write itself — users' snippets or submissions, notebook cells, an agent's tool calls, plugins — runs that code in a Zerops Box sandbox beside the app, never in the app's runtime container, where namespaces or a subprocess still share its kernel, files and project network. A project without a Box gets one through `zerops_import` (`type: ubuntu/zbox@1`; a Box exists only in a project in region prg1), and the app calls its workspace API with the key wired by reference: `zerops_knowledge uri="zerops://themes/services"`, section "Running code the app's users send".

---

=== develop-auto-close-semantics ===
### Work session auto-close

Auto-close fires only when EVERY in-scope service carries `closeDeployMode=auto` AND has a successful deploy + a passing verify that ran AFTER that deploy (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). On a pair with `gitPush=configured`, the deploy evidence is the delivered push build on the build target — the same gate, fed by the watched build instead of a direct deploy. Re-deploying re-opens verify: a deploy replaces the running app version, so a verify that passed before it no longer describes what is live — re-verify after the latest deploy. `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

Scope follows session topology — standard pairs include both halves. For dev-only work pass `outOfScope=["<stage>"]` on develop start; the stage half drops to a non-blocking reminder and the session closes on the dev half alone.

---

=== develop-dev-server-reason-codes ===
### `reason` values (DevServerResult)

When `zerops_dev_server` actions fail, the response's `reason` field
classifies the failure so you don't need a follow-up call to
diagnose. Dispatch table:

| `reason` | Meaning | Action |
|---|---|---|
| `health_probe_connection_refused` | The server started but nothing is listening on `port`. | Check that your app binds to `0.0.0.0` (not `localhost`), that `port` matches `run.ports[0].port`, and that your start command actually starts a server. Read `logTail` for crash output. |
| `health_probe_http_<code>` | Server runs but returned `<code>` (e.g. 500, 404). | Do NOT restart — it does not fix bugs. Read `logTail` + response body, edit code, deploy. |
| `crashed` | The server's process ended on its own; it stays down until started again. | Read `logTail` and the exit in `message`, fix the cause, `action=start`. |
| `port_in_use` | Something outside the dev server holds its port. | Find and stop that process (often a server started by hand outside `zerops_dev_server`), then `action=start`. |
| `stopped` | The server was stopped (or is declared to start only on request). | `action=start` when you need it. |

Observable always: `running` (bool), `healthStatus` (HTTP status
when `port` set, 0 otherwise), `startMillis` (time from start to
healthy), `logTail` (last log lines). Use these to confirm state
without a second tool call.

---

=== develop-verify-matrix ===
### Per-service verify matrix

Verify every service after deploy — deploy success ≠ working app. Shape from
`zerops_discover`: subdomain URL = web-facing; managed / no HTTP port = non-web.
Run `zerops_verify` first; a check with a `recovery` field → run it, re-verify,
before any browser probe.

| Shape | Check |
|---|---|
| non-web (managed / worker / no HTTP port) | `zerops_verify` → `status=healthy` is the whole check |
| web (dynamic / static / implicit-webserver) | `zerops_verify` → `http_internal` (project-network reachability) always runs; `http_public` (a custom domain instead runs as `public_domain`) reflects public-access intent: a public subdomain/domain → judge `httpStatus` + `bodyText` + `consoleErrors` — healthy + a real body (not a blank shell / error page, no fatal console error) proves it; internal-only by intent → `skip` (expected, not a failure — verify `http_internal` instead); mid-switch-on → `pending`, re-verify shortly |

When `bodyText`/`consoleErrors` are missing, truncated, or the page needs
interaction / SPA routes / non-root / auth, drive the browser **inline** with
`zerops_browser`; pass `screenshot: true` (`fullPage: true`: whole page) for
evidence. Only `wait` polls: wait for an element/text, never a fixed time. Use
`isolation: "fresh-context"` only for the first check after a deploy that
changed sign-in, storage, a service worker or caches. Never spawn a sub-agent
or call raw `agent-browser`.
To make an already-public service internal-only, call `zerops_subdomain action="disable"` — after that, `http_public` reports `skip` on every later verify, same as a `publicAccess: "none"` plan intent.

- **VERDICT: PASS** — healthy + real rendered content; proceed.
- **VERDICT: FAIL** — healthy infra but blank/broken/error page, or a failing check; iterate from the check's `detail` + render evidence.
- **VERDICT: UNCERTAIN** — no render data + URL unreachable; fall back to `zerops_verify`.

---

=== develop-mode-expansion ===
### Mode expansion — add a stage pair

This atom fires once per in-scope `mode: dev` or `mode: simple` (single-slot) service — for each, expanding to **standard** adds a stage sibling without touching the existing service. Expansion is an infrastructure change — it runs through the bootstrap workflow, not develop. Repeat the procedure below per service when multiple in-scope services need stage pairs.

```
zerops_workflow action="start" workflow="bootstrap"
  intent="expand partdev to standard — add stage"
```

Submit a plan that flags the existing runtime and names the new
stage hostname:

```json
{
  "runtime": {
    "devHostname": "partdev",
    "type": "<same type as current service>",
    "isExisting": true,
    "bootstrapMode": "standard",
    "stageHostname": "<new-stage-hostname>"
  },
  "dependencies": [
    { "hostname": "<existing dep>", "type": "<dep type>", "resolution": "EXISTS" }
  ]
}
```

Bootstrap leaves the existing service's code and runtime container untouched,
creates the new stage service via `zerops_import`, and at close the
envelope shows both snapshots:

- the original (now `mode: standard` with `stageHostname` set,
  `bootstrapped: true`, `deployed: true`, strategy intact);
- the new stage (`mode: stage`, `bootstrapped: true`,
  `deployed: false`).

After close, run a dev→stage cross-deploy to verify the pair
end-to-end.
