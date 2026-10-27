---
id: develop/standup-dev-deployed-wired
atomIds: [develop-intro, develop-git-push-start-from-remote, develop-stage-out-of-scope, develop-change-drives-deploy, develop-self-deploy-reproducibility, develop-dynamic-runtime-start-container, develop-knowledge-pointers, develop-auto-close-semantics, develop-verify-matrix, develop-strategy-awareness]
description: "A Mate's stand-up after its dev half deployed, in a Mate wired to its HQ (push configured, close-mode auto) with the stage half still out of scope and never deployed — no delivery, no promotion, no first-deploy branch held open by the stage."
---
=== develop-intro ===
### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

---

=== develop-git-push-start-from-remote ===
The repo is the source of truth here, and this working copy is not it. A container's `/var/www` survives every session: it can still be sitting on a topic branch that was merged and deleted weeks ago, or on a `main` that is behind by everything anyone else has landed since. Writing code on top of that produces a diff against the wrong base, and the mistake only surfaces at the push — as a conflict, or worse, as a silent revert of someone else's work.

Sync before the first edit, not after — the command depends on whether this pair is wired to this Mate's HQ (a repository HQ gave it, whose `main` moves only by a person's merge).

Not wired to this Mate's HQ — sync by moving the working copy onto whatever the remote's default branch now is:

```
ssh <push-source-host> "cd /var/www && git fetch origin && git checkout <default-branch> && git reset --hard origin/<default-branch>"
```

Read the default branch from the remote rather than assuming `main` — `git remote show origin` names it, and a repo created before that convention may still be on `master`. Uncommitted changes in the working copy are the one thing this destroys, so look before resetting (`git status --short`). Local edits that survived a previous session are almost always leftovers from work that already landed; if they are not, commit them to a branch first and say so, rather than carrying them silently into new work.

Wired to this Mate's HQ — take `main` in and start the work on a branch of your own cut from it; `main` itself is never pushed. Run it in your own shell on the mounted checkout (`/var/www/<push-source-host>`, a code-volume pair's folder below it), never over ssh: whether the checkout's origin is HQ or GitHub, git asks `zcp hq git-credential`, which only your own shell has:

```
cd /var/www/<push-source-host> && git fetch origin && git checkout main && git merge --ff-only origin/main && git checkout -b <your-branch>
```

One branch per piece of work, one change per branch: carrying on an open change means its own branch (status lists each repository's open changes by number and branch), and new work starts on a new branch cut as above. Where an HQ keeps one open change per repository, develop's start names the open one and says how to take its branch.

A `main` that will not fast-forward carries local commits that never landed: put them on a branch of their own (`git branch <name>`), then `git reset --keep origin/main`, and say so rather than carrying them silently into new work. On this Mate's HQ, while `origin/main` holds only HQ's initial commit, a `main` sharing no history with it is the repository's first history instead: leave it as it is, cut your branch from it, push the branch and open its change — merged, HQ makes it `main`; never merge or rebase it onto that commit.

Where the remote later refuses the branch you push, the ref is protected — on this Mate's HQ that is `main`, a tag, or a branch another open change holds: push a branch of your own and open its change with `zerops_workflow action="open-change"`, never a fresh token. The transport classifier names that case directly when it happens.

---

=== develop-stage-out-of-scope ===
### The stage stays as it is this session

This develop session leaves the stage half of these pairs out of scope:

- `appdev` — its stage `appstage` stays as it is

The work runs on the dev half alone: deploy it, start its dev server and
verify it. The session closes once the dev half is deployed and verified.

Leave the stage untouched: no deploy, cross-deploy or promotion onto it, and
no commit or push for it — putting work on the stage happens only when the
person asks for it.

A Mate's stand-up is such a session: when a Mate first deploys services it
adopted, zcp leaves their stage out on its own, and the stand-up ends at a
running dev.

When the dev half is verified, end by telling the person what runs on the dev
half, and that putting it on their preview (the stage half) is the next step
they can ask for.
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

Don't hand-roll `ssh appdev "cmd &"`: the SSH session ends with
the call and kills the process.

---

=== develop-knowledge-pointers ===
### Knowledge on demand — pull extra context

- **`zerops.yaml` schema** — `zerops_knowledge query="zerops.yaml schema"`
- **Runtime docs** (build tools, start commands) — `zerops_knowledge
  query="<base stack>"`.
- **Env var keys** — `zerops_discover includeEnvs=true` (`includeEnvValues=true`
  only to troubleshoot).
- **Other platform topics** (scaling, status codes, managed services) —
  `zerops_knowledge query="<topic>"`.
- **Code the app's users send** (snippets, an agent's tool calls, plugins) runs
  in a Zerops Box sandbox, never the app's container:
  `zerops_knowledge uri="zerops://themes/pools-and-box"`, section "Running code
  the app's users send".

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

=== develop-strategy-awareness ===
**Deploy config — recorded dimensions + how delivery derives** — pull on demand: `zerops_knowledge uri="zerops://atoms/develop-strategy-awareness"`
