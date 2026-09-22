---
id: develop/git-push-configured-webhook
atomIds: [develop-intro, develop-git-push-start-from-remote, develop-change-drives-deploy, develop-git-push-delivery, develop-self-deploy-reproducibility, develop-dynamic-runtime-start-container, develop-knowledge-pointers, develop-auto-close-semantics, develop-verify-matrix, develop-build-observe, develop-strategy-awareness]
description: "Standard pair, GitPushState configured (push is the delivery), BuildIntegration webhook."
---
=== develop-intro ===
### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

---

=== develop-git-push-start-from-remote ===
The repo is the source of truth here, and this working copy is not it. A container's `/var/www` survives every session: it can still be sitting on a topic branch that was merged and deleted weeks ago, or on a `main` that is behind by everything anyone else has landed since. Writing code on top of that produces a diff against the wrong base, and the mistake only surfaces at the push — as a conflict, or worse, as a silent revert of someone else's work.

Sync before the first edit, not after — the command depends on whether this pair is wired to the account's own Gitea (a repository the broker gave it; its branch is never `main`, which is protected there).

Not wired to the account's own Gitea — sync by moving the working copy onto whatever the remote's default branch now is:

```
ssh <push-source-host> "cd /var/www && git fetch origin && git checkout <default-branch> && git reset --hard origin/<default-branch>"
```

Read the default branch from the remote rather than assuming `main` — `git remote show origin` names it, and a repo created before that convention may still be on `master`. Uncommitted changes in the working copy are the one thing this destroys, so look before resetting (`git status --short`). Local edits that survived a previous session are almost always leftovers from work that already landed; if they are not, commit them to a branch first and say so, rather than carrying them silently into new work.

Wired to the account's own Gitea — there is no command to run here, on purpose: the working copy stays on this Mate's own branch always, and neither checking it out to the base, resetting it, nor merging the base in by hand is the sync. A plain `git merge origin/<base>` run by hand can fail on a false conflict — the base can carry a squash of THIS Mate's own earlier work, which shares no history with the branch it came from, so an ordinary merge reads it as two histories that both add the same files even though nothing really collides. zcp takes the base in for you: the moment a pass learns this Mate's own pull request merged, and again before every push that follows — a delivery's own commit+push, or an ordinary `strategy="git-push"` — both fold a landed squash in first, before the base merge runs, so that false conflict never reaches you or the pull request it opens. The only conflict worth resolving is one zcp itself reports, from either of those; that one is real, and resolving it (never resetting past it) is what the next push or deploy is for.

Where the remote later refuses the branch you push, the ref is protected: the answer is a pull request a person merges, never a fresh token. The transport classifier names that case directly when it happens.

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

=== develop-git-push-delivery ===
Git push is configured for this service (`gitPush=configured`), so the repo is the source of truth and delivery happens by pushing to it. Development work ends with a push, not a redeploy — code that reached the remote persists across container replacement, which is what made redeploy-for-persistence necessary in the first place. Where an integration consumes the pushes (`buildIntegration=webhook|actions`), direct self/cross `zerops_deploy` calls on this pair answer `push-delivery-required` with the recommended push call instead of deploying; on `buildIntegration=none` nothing rebuilds from the repo, so those calls deploy and flag that the service now runs code the remote does not carry.

**Push source vs build target.** For a standard pair, the push originates from the DEV half (push source) and the build lands on the STAGE half (build target). For simple / single-runtime modes, push source equals build target.
<!-- axis-l-keep -->

## Ship the work

```
zerops_deploy targetService="appdev" setup="<source-setup>" strategy="git-push"
```

`targetService` is the PUSH SOURCE hostname (dev half of a standard pair, or the service itself for simple modes). Leave `branch` unset: it defaults to `main`, except on the account's own Gitea, where it defaults to this Mate's own branch because every repository's `main` is protected and the Mate lands through a pull request. The call refuses an empty tree or uncommitted changes — commit first (`ssh <host> "cd /var/www && git add -A && git commit -m '<msg>'"` for the runtime container, `git -C <workingDir> add -A && git commit -m '<msg>'` on a dev machine). It pushes HEAD to the configured remote and then follows the integration build on the build target until it settles:

| Response `status` | Meaning |
|---|---|
| `"DELIVERED"` | Push landed AND the integration build reached an active app version. The response carries `buildTarget` + `buildStatus`, and `autoRecorded: true` means the session already counts this as the build target's deploy evidence — no follow-up call needed. |
| `"PUSHED"` + failed `buildStatus` | The push landed but the build failed. The same response carries `failureClassification` (category + cause + suggested action) and `buildLogs` — read the classification first, fix, commit, push again. |
| `"PUSHED"` + build not observed | No integration build appeared inside the watch window. Either `buildIntegration=none` (see below) or the CI is slow — check `zerops_events serviceHostname="<build-target>"` later. |

**Setup parameter:** `setup=<name>` MUST match a `setup:` block name in the project's `zerops.yaml` (the push pre-flight validates the SOURCE setup; the remote build consumes its own setup name — for standard pairs typically `prod`). Read the project's `zerops.yaml` first and pass the matching name explicitly; an `INVALID_ZEROPS_YML` error response carries `attemptedSetup` + `availableSetups` for a one-round-trip recovery.

## What runs the build depends on `buildIntegration`

| `buildIntegration` | What happens after the push |
|---|---|
| `webhook` | Zerops pulls the repo and runs the build pipeline on the build target. |
| `actions` | A workflow in the repository runs the build from CI. On GitHub that is `.github/workflows/`, running `zcli push` with a CI token. On the account's own Gitea (a remote on `$GITEA_URL`) it is `.gitea/workflows/`, which asks the account's broker to deploy with the job's own token and carries no Zerops token at all. Either way the build lands on the build target. |
| `none` | The push is archived at the remote; no watched build fires. The push response offers the choice: wire an integration via `zerops_workflow action="build-integration"`, keep your independent CI, or stay archive-only. Until one is wired, a direct `zerops_deploy` is what puts code on the service — including the dev→stage promotion of a standard pair — and the push still matters as the durable copy. |

## After "DELIVERED"

Verify the build target: `zerops_verify serviceHostname="<build-target>"`. Deploy evidence for the session is already in place when the response said `autoRecorded: true`; `zerops_workflow action="record-deploy" targetService="<build-target>"` is only the recovery call for a build that landed OUTSIDE the watch window (confirm the app version via `zerops_events` first, then record).

If the push fails with a credential cause, the token was rotated or revoked upstream — ask the user for a fresh token and re-run `zerops_workflow action="git-push-setup" service="appdev" remoteUrl="..." gitToken="<fresh PAT>"`. Never invent or reuse a token the user didn't supply. On a remote whose host is `$GITEA_URL` there is no user to ask: the credential is the Mate's own bot token in `$GITEA_TOKEN`, and a rejection there means the broker rotated it — re-read the variable rather than asking for one.

A push rejected because the remote carries commits yours doesn't returns `GIT_PUSH_NON_FAST_FORWARD` with a `next` block naming exactly three options (rebase / merge / replace-remote) and their exact commands — that decision belongs to the user; never run `git push --force` or merge on their behalf without asking first.

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
one. The dev server is unsupervised, so
the URL 502s after any container cycle until restarted: a passing verify
means "live now", not "durably shipped". For an always-on service use
simple mode. Action family on `zerops_dev_server`:

| Action | Use | Args |
|---|---|---|
| `status` | check before `start` (idempotent) — avoids duplicate listener | `hostname port healthPath` |
| `start` | spawn the dev process | `hostname command port healthPath` |
| `restart` | survives-the-deploy config/code change | `hostname command port healthPath` |
| `logs` | tail recent for diagnosis | `hostname logLines=40` |
| `stop` | end of session, free the port | `hostname port` |

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

=== develop-build-observe ===
The git-push delivery pattern (push command, watched-build statuses)
lives in the git-push delivery guidance fired alongside this atom.
A failed build inside the watch window already carries its diagnosis
in the push response (`failureClassification` + `buildLogs`). Use this
section when a build that ran OUTSIDE a push call — a teammate's push,
a delayed CI run — lands on a failure status in `zerops_events`.

## Failure statuses

When `zerops_events serviceHostname="<hostname>"` reports
`BUILD_FAILED`, `DEPLOY_FAILED`, or `PREPARING_RUNTIME_FAILED`, read
the failed event's `failureClass` (build / start / verify / network /
config / credential / other) + `failureCause` for the structured
diagnosis — same vocabulary the synchronous deploy path produces in
`DeployResult.FailureClassification`. That IS the diagnosis: a failed
build never started a runtime process, so `zerops_logs` is empty for
the service — read the event, not the log stream. Recovery is whatever
fixed the build (yaml change, missing env var, code issue) plus a
fresh push.

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
