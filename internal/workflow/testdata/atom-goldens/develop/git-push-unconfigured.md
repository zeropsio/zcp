---
id: develop/git-push-unconfigured
atomIds: [develop-intro, develop-change-drives-deploy, develop-close-mode-git-push-needs-setup, develop-env-var-shell-usage, develop-knowledge-pointers, develop-auto-close-semantics, develop-verify-matrix, develop-strategy-awareness]
description: "Standard pair, close-mode git-push, GitPushState unconfigured — agent must run git-push-setup before close."
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

This service is on `closeDeployMode=git-push`, but the runtime's `gitPushState` is not `configured` — pushing now will be rejected by `zerops_deploy strategy="git-push"` pre-flight (`PREREQUISITE_MISSING`).

Run the capability setup first; the env-aware setup atom will be returned synchronously with the walkthrough:

```
zerops_workflow action="git-push-setup" service="appdev"
```

Follow the returned setup walkthrough (sets `GIT_TOKEN`, configures the remote, stamps `GitPushState=configured`). `git-push-setup` is per-pair (one call from the dev half writes capability for both halves of a standard pair); once `gitPushState=configured`, the next develop response delivers the actual push command.

If the previous state was `broken` (a setup attempt left the artifact damaged), the setup walkthrough re-runs the token + remote stamp from scratch — no manual cleanup needed.

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

Auto-close fires only when EVERY in-scope service carries `closeDeployMode ∈ {auto, git-push}` AND has a successful deploy + a passing verify that ran AFTER that deploy (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). Re-deploying re-opens verify: a deploy replaces the running app version, so a verify that passed before it no longer describes what is live — re-verify after the latest deploy. `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

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
  supplied token authenticates against the remote URL, the push source carries
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
