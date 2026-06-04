---
id: develop/standard-auto-pair
atomIds: [develop-intro, develop-change-drives-deploy, develop-close-mode-auto, develop-dynamic-runtime-start-container, develop-env-var-shell-usage, develop-knowledge-pointers, develop-auto-close-semantics, develop-verify-matrix, develop-strategy-awareness, develop-close-mode-auto-standard]
description: "Standard dev+stage pair, close-mode auto on both halves, both deployed."
---
### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

---

### Every code change must reach a durable state

Iteration cadence is mode-specific:

- Dev-mode dynamic runtime: edit code in place; reload via
  `zerops_dev_server` (no full redeploy for code-only changes).
- Simple / standard / local / first-deploy: every change →
  `zerops_deploy`.

Once close-mode is `auto` or `git-push` and every resolved deploy
target is deployed + verified, the work session auto-closes.

---

This service is on `closeDeployMode=auto`. Your delivery pattern is direct `zerops_deploy` calls via zcli — fast, synchronous, the canonical default for tight iteration cycles. `action="close"` itself is a session-teardown call regardless of close-mode; auto-close fires when the deploys you ran during iterations satisfy the green-scope gate.

## How auto-close fires

When auto-close conditions land (every service in scope has a successful deploy + passed verify), ZCP closes the develop session automatically. The deploys that landed during develop iterations ARE the close deploys — there's no separate close-time push, and no special call from the close handler.

The env-specific mechanics (SSH push from `/var/www` for container, `zcli push` from CWD for local) live in the env-scoped deploy guidance fired alongside this atom.

## When you might switch

`auto` is great for "make a change, see it live, repeat." If the workflow grows — multiple contributors landing changes, CI pipelines that should run before deploy, release branches — switch:

- `git-push` if pushing to a git remote should trigger the build (Zerops webhook or GitHub Actions). After the close-mode flip, `action=git-push-setup` provisions the capability.
- `manual` if external orchestration owns close decisions. ZCP still records every deploy/verify; auto-close just doesn't fire.

Switch close-mode per service:

```
zerops_workflow action="close-mode" closeMode={"appdev":"git-push"}
zerops_workflow action="close-mode" closeMode={"appstage":"git-push"}
```

(Replace `git-push` with `manual` to yield to user orchestration.) The default stays auto until you explicitly switch.

---

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

Response carries `running`, `healthStatus`, `reason`, and `logTail`
— read these before making another call.

Don't hand-roll `ssh appdev "cmd &"`: the SSH session ends with
the call and kills the process. Always go through `zerops_dev_server`.

---

### Reference by name in container-side commands

When SSHing into a container to run a command that needs a secret
(psql, prisma, redis-cli, curl auth header), refer to the env var by
name in a **single-quoted** command body. Bash inside the runtime container
expands it at exec time from its already-injected OS env — the value
never enters your context.

Reference the name THIS service has in its OWN env: a var its
`run.envVariables` defined (e.g. `DATABASE_URL`), an inherited project
var, or its own secret. A sibling's bare `${db_*}` is NOT in your
container under the default service isolation — only the name your
service imported it under resolves.

```bash
# WRONG — value pasted from earlier discover output into the command
ssh apidev 'npx prisma migrate --url postgresql://postgres:U_UjIq5TC...@db:5432/db'

# RIGHT — single-quoted; $DATABASE_URL is apidev's OWN run.envVariables
# var (e.g. DATABASE_URL: ${db_connectionString}), expanded at exec time
ssh apidev 'npx prisma migrate --url "$DATABASE_URL"'
```

Same for `curl` auth headers (`Authorization: Bearer $API_TOKEN`),
`redis-cli`, `aws s3` — reference the name your service defined.

**Read vs use.** Inspecting values for diagnosis is fine — mask in
output so secrets don't enter your context:

```bash
ssh apidev 'env | grep -E "^(DB_|APP_)" | sed "s/=.*/=<set>/"'
```

If you DO pull values into context (export classification, debugging
an unresolved ref), the next command should still reference by
`${name}`, not the value you just saw.

---

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

### Work session auto-close

Auto-close fires only when EVERY in-scope service carries `closeDeployMode ∈ {auto, git-push}` AND has a successful deploy + passing verify (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

Scope follows session topology — standard pairs include both halves. For dev-only work pass `outOfScope=["<stage>"]` on develop start; the stage half drops to a non-blocking reminder and the session closes on the dev half alone.

---

### Per-service verify matrix

Verify every service after deploy — deploy success ≠ working app. Shape from
`zerops_discover`: subdomain URL = web-facing; managed / no HTTP port = non-web.
Run `zerops_verify` first; a check with a `recovery` field → run it, re-verify,
before any browser probe.

| Shape | Check |
|---|---|
| non-web (managed / worker / no HTTP port) | `zerops_verify` → `status=healthy` is the whole check |
| web (dynamic / static / implicit-webserver) | `zerops_verify` → judge `http_root`: `httpStatus` + `bodyText` + `consoleErrors`; healthy + a real body (not a blank shell / error page, no fatal console error) proves it |

When `bodyText`/`consoleErrors` are missing, truncated, or the page needs
interaction / SPA routes / non-root / auth, drive the browser **inline** with
`zerops_browser`. Never spawn a sub-agent, call raw `agent-browser`, or use `eval`.
Internal-only service (no public subdomain) → `zerops_subdomain action="disable"` after deploy.

- **VERDICT: PASS** — healthy + real rendered content; proceed.
- **VERDICT: FAIL** — healthy infra but blank/broken/error page, or a failing check; iterate from the check's `detail` + render evidence.
- **VERDICT: UNCERTAIN** — no render data + URL unreachable; fall back to `zerops_verify`.

---

### Deploy config — current axes + how to change

Each runtime service has three orthogonal deploy-config axes — the
rendered Services block shows them as
`closeMode=auto|git-push|manual gitPush=unconfigured|configured|broken|unknown buildIntegration=none|webhook|actions`:

- `closeMode` — what the develop close action does. `auto` runs
  `zerops_deploy` directly (zcli push); `git-push` commits + pushes
  to a configured remote so Zerops/CI builds; `manual` yields to
  you for orchestration. `unset` is the bootstrap-written
  placeholder that develop converts on first use.
- `gitPush` — capability state for the git-push path. `configured`
  means the last `git-push-setup` probe **proved end-to-end auth**: the
  supplied token authenticates against the remote URL, project env carries
  `GIT_TOKEN` (sensitive), and the working tree's git config has its
  `origin` synced. `unconfigured` / `broken` indicate setup is
  needed before `closeMode=git-push` can fire (`broken` means a previously-
  configured token stopped working, e.g. PAT rotation).
- `buildIntegration` — the ZCP-managed CI shape that was picked. `actions`
  (GitHub Actions workflow + secrets), `webhook` (Zerops dashboard OAuth),
  or `none`. Requires `gitPush=configured`. The flag records the choice
  and the handoff shape (workflow YAML body / dashboard URL); workflow
  commit, secrets landing, and OAuth completion happen outside ZCP's
  reach and are not verified by this flag. Treat as "this is the
  integration shape we wired", not "the build trigger is confirmed live".

Switch any axis without closing the session — three actions, each
operating at a different scope:

- `close-mode` is **per-pair** under the hood (one ServiceMeta per dev/stage pair) but accepts a multi-entry map: one call sets close-mode for any subset of services. Passing both halves of a pair with the SAME value is accepted (canonical write once). Passing both halves with DIFFERENT values is rejected with an explicit conflict diagnostic — pick one value for the pair.
- `git-push-setup` and `build-integration` are **per-pair**: capability is stamped on the dev half's meta and shared by both halves. `git-push-setup` rejects stage-half input with `INVALID_PARAMETER` (it mutates push-side state — would write to the wrong target). `build-integration` is permissive: pair-keyed lookup resolves either half to the dev meta and the response carries `pushSource`/`buildTarget`/`topologyNote` so the redirect is visible. Either way: prefer passing the dev half directly.

```
zerops_workflow action="close-mode" closeMode={"appdev":"auto"}
zerops_workflow action="git-push-setup" service="appdev" remoteUrl="..."
zerops_workflow action="build-integration" service="appdev" integration="actions"
```

Substitute `appdev` with the dev-half hostname (or single-runtime hostname). For a multi-service project, repeat each call once per dev-half service — never per stage-half.

Mixed config across services in one project is fine — each service's three axes are independent in the envelope.

---

### Closing the task

Deploy dev first, start the dev server, verify, then promote to stage. Run per dev/stage pair in scope:

```
zerops_deploy targetService="appdev" setup="dev"
zerops_dev_server action=start hostname="appdev" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="appdev"

zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"
zerops_verify serviceHostname="appstage"
```

Cross-deploy builds the dev source on stage (dev side unchanged); stage has a real `run.start` + `healthCheck`, so it auto-starts (no `zerops_dev_server` on the stage side). The work session closes once both halves have a successful deploy + passing verify (`closeReason=auto-complete`). If the dev server is already running after a code-only change, run `action=status` first; if `running: true`, skip `action=start`.
