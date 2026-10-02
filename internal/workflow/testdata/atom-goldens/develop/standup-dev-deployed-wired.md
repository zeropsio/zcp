---
id: develop/standup-dev-deployed-wired
atomIds: [develop-intro, develop-git-push-start-from-remote, develop-stage-out-of-scope, develop-change-drives-deploy, develop-self-deploy-reproducibility, develop-dynamic-runtime-start-container, develop-knowledge-pointers, develop-auto-close-semantics, develop-verify-matrix, develop-strategy-awareness]
description: "A Mate's stand-up after its dev half deployed, in a Mate wired to its group's Gitea (push configured, close-mode auto) with the stage half still out of scope and never deployed — no delivery, no promotion, no first-deploy branch held open by the stage."
---
=== develop-intro ===
### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

---

=== develop-git-push-start-from-remote ===
The repo is the source of truth here, and this working copy is not it. A container's `/var/www` survives every session: it can still be sitting on a topic branch that was merged and deleted weeks ago, or on a `main` that is behind by everything anyone else has landed since. Writing code on top of that produces a diff against the wrong base, and the mistake only surfaces at the push — as a conflict, or worse, as a silent revert of someone else's work.

Sync before the first edit, not after — the command depends on whether this pair is wired to this Mate's HQ (a repository HQ gave it; its branch is never `main`, which moves there only by a person's merge).

Not wired to this Mate's HQ — sync by moving the working copy onto whatever the remote's default branch now is:

```
ssh <push-source-host> "cd /var/www && git fetch origin && git checkout <default-branch> && git reset --hard origin/<default-branch>"
```

Read the default branch from the remote rather than assuming `main` — `git remote show origin` names it, and a repo created before that convention may still be on `master`. Uncommitted changes in the working copy are the one thing this destroys, so look before resetting (`git status --short`). Local edits that survived a previous session are almost always leftovers from work that already landed; if they are not, commit them to a branch first and say so, rather than carrying them silently into new work.

Wired to this Mate's HQ — there is no command to run here, on purpose: the working copy stays on this Mate's own branch always, and neither checking it out to the base, resetting it, nor merging the base in by hand is the sync. A plain `git merge origin/<base>` run by hand can fail on a false conflict — the base can carry a squash of THIS Mate's own earlier work, which shares no history with the branch it came from, so an ordinary merge reads it as two histories that both add the same files even though nothing really collides. When a change of this Mate's own is recorded as merged, zcp folds that landing in for you: the moment a pass learns of the merge, and again before whatever push follows it needs a landing absorbed — a delivery's own commit+push, or an ordinary `strategy="git-push"`. Usually that keeps the false conflict from ever reaching you or the change it pushes to; an old container git, or a landing zcp cannot prove lossless, can still leave it unabsorbed, and the ordinary step can then hit that same shape for real.

Either way, every conflict zcp reports here — from the absorb step itself, or the ordinary step behind an unabsorbed landing — names the exact commands in its own message: never a plain reset, and never the plain `git fetch origin && git merge origin/<base>` on its own, which would only repeat the same conflict. Run what the message says, in the checkout it names, then push or deploy again — that is what resolves it.

Where the remote later refuses the branch you push, the ref is protected: the answer is a change a person merges, never a fresh token. The transport classifier names that case directly when it happens.

---

=== develop-stage-out-of-scope ===
### The stage stays as it is this session

This develop session leaves the stage half of these pairs out of scope:

- `appdev` — its stage `appstage` stays as it is

The work runs on the dev half alone: deploy it, start its dev server and
verify it. The session closes once the dev half is deployed and verified.

Leave the stage untouched: no deploy, cross-deploy or promotion onto it, and
no commit or push for it. In a Mate delivering through its HQ, a deploy onto
a stage half is a delivery — a commit, a change and a push — so it happens
only when the person asks for it.

A Mate's stand-up is such a session: when a Mate first deploys services it
adopted, zcp leaves their stage out on its own, and the stand-up ends at a
running dev.

When the dev half is verified, end by telling the person what runs on the dev
half, and that promoting it to the stage is the next step they can ask for.
If they already asked for it, start a new develop session for it once this
one closes — with the dev half deployed, its stage is part of the work again.

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

=== develop-self-deploy-reproducibility ===
### The dev container is disposable; the repository isn't

A self-deploy replaces the dev container with a fresh one. Nothing on the
old dev container's disk survives that swap except what git carries with
it — project envs (auto-injected as OS env vars, never a file) and
managed-service data (Postgres, object storage, …) are the other two
persistence mechanisms, and neither lives on the dev container's
filesystem either. A file that exists only on today's dev container — an
untracked script, a local SQLite DB, a `.env` — is gone the moment the new
container replaces it.

`zerops_deploy`'s response on a self-deploy carries this as data, not a
guess: `notCarried` names the git-ignored paths (count, bytes, a sample)
that will not exist in the new container, and `envFiles` lists any
`.env`/`.env.*` files found regardless of ignore state — a config file the
agent should move into `zerops_env`, not carry forward as a workaround.
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

=== develop-dynamic-runtime-start-container ===
### Dynamic-runtime dev server

Dev-mode dynamic runtime containers start running `zsc noop --silent`
after deploy — a no-op keepalive; no dev process is live until you start
one. Once started, zcp keeps it — one per dev container, the last you
started: when the dev container restarts or is redeployed, zcp starts
it again with the same command, working directory and port — a
deploy's response reports it under `devServer` — until you `stop` it.
A server that crashes in its container stays down for you to read and
fix. It is still a dev process: a passing
verify means "live now", not "durably shipped" — for an always-on
service use simple mode. Action family on `zerops_dev_server`:

| Action | Use | Args |
|---|---|---|
| `status` | check before `start` (idempotent) — avoids duplicate listener | `hostname port healthPath` |
| `start` | spawn the dev process | `hostname command port healthPath` |
| `restart` | survives-the-deploy config/code change | `hostname command port healthPath` |
| `logs` | tail recent for diagnosis | `hostname logLines=40` |
| `stop` | free the port; zcp stops keeping it | `hostname port` |

Args:
- `command` — the app's dev-server start command (the real long-running
  process, e.g. `npm run dev`). NOT the `zsc noop --silent` keepalive that
  sits in the dev block's `run.start`.
- `port` — `run.ports[0].port`.
- `healthPath` — app-owned (`/api/health`, `/status`) or `/`.

Response carries `running`, `healthStatus`, `url` (the hostname-vantage
address to reach the server — the probe runs localhost inside the
container, so the app must bind `0.0.0.0`, not loopback), `reason`, and
`logTail` — read these before making another call.

Don't hand-roll `ssh appdev "cmd &"`: the SSH session ends with
the call and kills the process. Always go through `zerops_dev_server`.

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

=== develop-auto-close-semantics ===
### Work session auto-close

Auto-close fires only when EVERY in-scope service carries `closeDeployMode=auto` AND has a successful deploy + a passing verify that ran AFTER that deploy (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). On a pair with `gitPush=configured`, the deploy evidence is the delivered push build on the build target — the same gate, fed by the watched build instead of a direct deploy. Re-deploying re-opens verify: a deploy replaces the running app version, so a verify that passed before it no longer describes what is live — re-verify after the latest deploy. `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

Scope follows session topology — standard pairs include both halves. For dev-only work pass `outOfScope=["<stage>"]` on develop start; the stage half drops to a non-blocking reminder and the session closes on the dev half alone.

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
`zerops_browser` — inner commands cover click/fill/find/get/is/wait plus
`set viewport`/`set device`/`set media` for responsive and dark-mode checks;
pass `screenshot: true` for visual evidence; failed/4xx/5xx network requests
are always reported alongside errors/console, no flag needed. Never spawn a
sub-agent, call raw `agent-browser`, or use `eval`.
To make an already-public service internal-only, call `zerops_subdomain action="disable"` — after that, `http_public` reports `skip` on every later verify, same as a `publicAccess: "none"` plan intent.

- **VERDICT: PASS** — healthy + real rendered content; proceed.
- **VERDICT: FAIL** — healthy infra but blank/broken/error page, or a failing check; iterate from the check's `detail` + render evidence.
- **VERDICT: UNCERTAIN** — no render data + URL unreachable; fall back to `zerops_verify`.

---

=== develop-strategy-awareness ===
### Deploy config — recorded dimensions + how delivery derives

Each runtime service records three deploy-config dimensions — the
rendered Services block shows them as
`closeMode=auto|manual|unset gitPush=unconfigured|configured|broken buildIntegration=none|webhook|actions`:

- `closeMode` — who owns "done". `auto` lets the work session close
  itself once every in-scope service has a successful deploy + passing
  verify; `manual` yields close decisions to you / external
  orchestration. `unset` is the bootstrap-written placeholder that
  develop converts on first use.
- `gitPush` — capability state for push delivery. `configured`
  means the last `git-push-setup` probe **proved end-to-end auth**: the
  supplied token authenticates against the remote URL, the push source carries
  `GIT_TOKEN` (sensitive), and the working tree's git config has its
  `origin` synced. `broken` means a previously-configured token stopped
  working (e.g. PAT rotation) — re-run setup before pushing.
- `buildIntegration` — the ZCP-managed CI shape that was picked. `actions`
  (GitHub Actions workflow + secrets), `webhook` (Zerops dashboard OAuth),
  or `none`. Requires `gitPush=configured`. The flag records the choice
  and the handoff shape (workflow YAML body / dashboard URL); workflow
  commit, secrets landing, and OAuth completion happen outside ZCP's
  reach and are not verified by this flag. Treat as "this is the
  integration shape we wired", not "the build trigger is confirmed live".

**The delivery mechanism is DERIVED, not chosen separately:** when
`gitPush=configured` (and closeMode is not `manual`), delivery is
commit + git push — direct `zerops_deploy` self/cross calls on that pair
answer `push-delivery-required` with the recommended push call. Otherwise
delivery is the direct `zerops_deploy` path. Want push delivery? Configure
the capability; there is no separate mode switch.

Change any dimension without closing the session:

- `close-mode` is **per-pair** under the hood (one record per dev/stage pair) but accepts a multi-entry map: one call sets close-mode for any subset of services. Passing both halves of a pair with the SAME value is accepted (canonical write once). Passing both halves with DIFFERENT values is rejected with an explicit conflict diagnostic — pick one value for the pair.
- `git-push-setup` and `build-integration` are **per-pair**: capability is stamped on the dev half's record and shared by both halves. `git-push-setup` rejects stage-half input with `INVALID_PARAMETER` (it mutates push-side state — would write to the wrong target). `build-integration` is permissive: pair-keyed lookup resolves either half to the dev record and the response carries `pushSource`/`buildTarget`/`topologyNote` so the redirect is visible. Either way: prefer passing the dev half directly.

```
zerops_workflow action="close-mode" closeMode={"appdev":"auto"}
zerops_workflow action="git-push-setup" service="appdev" remoteUrl="..."
zerops_workflow action="build-integration" service="appdev" integration="actions"
```

Substitute `appdev` with the dev-half hostname (or single-runtime hostname). For a multi-service project, repeat each call once per dev-half service — never per stage-half.

Mixed config across services in one project is fine — each service's dimensions are independent in the envelope.
