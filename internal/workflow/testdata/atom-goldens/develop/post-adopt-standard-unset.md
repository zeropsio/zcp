---
id: develop/post-adopt-standard-unset
atomIds: [develop-intro, develop-strategy-review, develop-change-drives-deploy, develop-dynamic-runtime-start-container, develop-env-var-shell-usage, develop-knowledge-pointers, develop-standard-unset-iterate, develop-standard-unset-promote-stage, develop-auto-close-semantics, develop-verify-matrix]
description: "Adopted standard pair, both halves running, close-mode never picked."
---
### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

---

### Pick an ongoing close-mode

The first deploy is on record (`deployed: true`). Before iterating, declare the develop session's delivery pattern. Close-mode does not change what `action="close"` does (close is always a session-teardown call) — it picks the per-mode atoms that guide every subsequent deploy and gates auto-close:

- `auto` — agent runs `zerops_deploy` directly via zcli. Auto-close fires when scope-services are green. Fast for tight iteration cycles.
- `git-push` — agent runs `zerops_deploy strategy="git-push"` to commit + push to a configured remote. Zerops or your CI picks the push up and builds. Requires `git-push-setup` first.
- `manual` — **you** drive every deploy. ZCP records evidence, never initiates a deploy, and the auto-close gate stays open until you call `action="close"` explicitly.

Pick a close-mode per service:

```
zerops_workflow action="close-mode" closeMode={"appdev":"auto"}
```

Replace `auto` with `git-push` or `manual` to match your workflow. Switching to `git-push` returns chained guidance pointing at `action="git-push-setup"` to provision GIT_TOKEN / .netrc / remote URL. The build integration (webhook / actions) is independent — wire it via `action="build-integration"` whenever git-push capability lands.

Pick explicitly before iterating; the default keeps working but committed close-modes drive the post-first-deploy auto-close gate.

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

### Dynamic-runtime dev server

Dev-mode dynamic runtimes deploy with `run.start` omitted — the
runtime container idles and no dev process is live until you start
one. The dev server is unsupervised, so the URL 502s after any
container cycle until restarted: a passing verify means "live now",
not "durably shipped". For an always-on service use simple mode.
Action family on `zerops_dev_server`:

| Action | Use | Args |
|---|---|---|
| `status` | check before `start` (idempotent) — avoids duplicate listener | `hostname port healthPath` |
| `start` | spawn the dev process | `hostname command port healthPath` |
| `restart` | survives-the-deploy config/code change | `hostname command port healthPath` |
| `logs` | tail recent for diagnosis | `hostname logLines=40` |
| `stop` | end of session, free the port | `hostname port` |

Args:
- `command` — exact `run.start` from `zerops.yaml`.
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

### Knowledge on demand — where to pull extra context

When the embedded guidance is not enough, these are the canonical lookups:

- **`zerops.yaml` schema / field reference**:
  `zerops_knowledge query="zerops.yaml schema"`
- **Runtime-specific docs** (build tools, start commands, conventions):
  `zerops_knowledge query="<your runtime>"` — e.g. `nodejs`, `go`,
  `php-apache`, `bun`. Match the base stack name of the service you are
  working with.
- **Env var keys** (no values — safe by default):
  `zerops_discover includeEnvs=true`. Add `includeEnvValues=true` only
  for troubleshooting.
- **Infrastructure changes** (shared storage, scaling rules, nginx
  fragments): platform-rules guidance in the develop response covers
  base mechanics; deeper detail comes from `zerops_knowledge
  query="<topic>"`. For dev → standard mode expansion, start a new
  bootstrap session with `isExisting=true` on the existing runtime
  plus a `stageHostname` for the new stage pair.
- **Platform constants** (status codes, managed service categories,
  runtime classes): `zerops_knowledge query="<topic>"` — examples:
  `"service status"`, `"managed services"`, `"subdomain"`.

---

### Dev iteration loop (close-mode unset)

The strategy-review section of this response advises picking a close-mode before iterating, but the dev iteration steps are the SAME regardless of which mode you eventually pick — close-mode only changes what the *close* call does, not what the iteration looks like. While close-mode is `unset`, run the same per-iteration sequence on the dev half:

```
zerops_deploy targetService="appdev" setup="dev"
zerops_dev_server action=start hostname="appdev" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="appdev"
```

After each iteration lands cleanly on the dev half, the stage half stays at adopt-time content until you cross-deploy — the promote-stage section of this response carries the dev → stage cross-deploy template. Auto-close stays blocked while close-mode is `unset` (auto-close requires every in-scope service to carry `closeDeployMode ∈ {auto, git-push}`); pick a close-mode (`auto`, `git-push`, or `manual`) once you've confirmed the iteration loop works for this task.

---

### Promote dev to stage

After each successful `zerops_deploy` + `zerops_verify` on the dev half, cross-deploy the dev tree into the paired stage so the stage's public artifact reflects current code:

```
zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"
zerops_verify serviceHostname="appstage"
```

Cross-deploy builds the dev source on stage (dev side unchanged); stage runs its own `run.start`. Independent of close-mode — close-mode picks the per-mode iteration cadence on the dev side, not whether the stage half stays current. Standard-pair auto-close requires both halves to carry a successful deploy + passing verify and `closeDeployMode ∈ {auto, git-push}`; while `unset`, the session stays open until you commit a delivery pattern.

---

### Work session auto-close

Auto-close is gated on every in-scope service carrying `closeDeployMode ∈ {auto, git-push}`. Services with `closeDeployMode=unset` or `closeDeployMode=manual` BLOCK the auto-close trigger — the session stays open until you either pick a close-mode for those services or call `action="close"` explicitly.

When the gate is open, the session closes automatically when either:

- **`auto-complete`** — every service in scope has both a successful
  deploy and a passing verify; `closeReason: auto-complete`.
- **`iteration-cap`** — the workflow's retry ceiling was hit; same
  close-state shape, `closeReason: iteration-cap`.

Explicit `zerops_workflow action="close" workflow="develop"` emits
the same closed state manually and is rarely needed — starting a new
task with a different `intent` replaces the session.

Close scope follows the session topology: standard-mode pairs include
BOTH halves by default. For dev-only work ("leave staging as it is"),
pass `outOfScope=["<stage>"]` on develop start — the stage half drops to
a non-blocking reminder and the session closes on the dev half alone.
Dev-only or simple services close after one successful deploy + verify.

Close is cleanup, not commitment — work is durable in git + on Zerops.

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
