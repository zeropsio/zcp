---
id: develop/git-push-broken
atomIds: [develop-intro, develop-change-drives-deploy, develop-git-push-broken, develop-close-mode-auto, develop-dynamic-runtime-start-container, develop-knowledge-pointers, develop-auto-close-semantics, develop-verify-matrix, develop-strategy-awareness, develop-close-mode-auto-standard]
description: "Standard pair, GitPushState broken — previously-configured credential degraded; agent must repair via git-push-setup before pushing."
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

Once close-mode is `auto` and every resolved deploy
target is deployed + verified, the work session auto-closes.

---

Git-push delivery was configured for this service, but the capability is now `gitPush=broken` — a previously-working credential stopped authenticating (typical cause: the token was rotated or revoked upstream). Pushing now will be rejected by `zerops_deploy strategy="git-push"` pre-flight (`PREREQUISITE_MISSING`).

Repair runs the same setup action — ask the user for a fresh token (never invent one) and re-run:

```
zerops_workflow action="git-push-setup" service="appdev" gitToken="<fresh PAT>"
```

The setup probe re-verifies end-to-end auth against the recorded remote and rewrites the credential in place — no manual cleanup of the previous state is needed. `git-push-setup` is per-pair (one call from the dev half repairs capability for both halves of a standard pair); once `gitPush=configured` again, the next develop response delivers the push command.

---

This service is on `closeDeployMode=auto` with no configured git remote. Your delivery pattern is direct `zerops_deploy` calls via zcli — fast, synchronous, the canonical default for tight iteration cycles. `action="close"` itself is a session-teardown call regardless of close-mode; auto-close fires when the deploys you ran during iterations satisfy the green-scope gate.

## How auto-close fires

When auto-close conditions land (every service in scope has a successful deploy + passed verify), ZCP closes the develop session automatically. The deploys that landed during develop iterations ARE the close deploys — there's no separate close-time push, and no special call from the close handler.

The env-specific mechanics (SSH push from `/var/www` for container, `zcli push` from CWD for local) live in the env-scoped deploy guidance fired alongside this atom.

## When you might switch

`auto` is great for "make a change, see it live, repeat." If the workflow grows — multiple contributors landing changes, CI pipelines that should run before deploy, release branches — change the config:

- Configure git-push delivery (`zerops_workflow action="git-push-setup" service="appdev"`) if the repo should become the source of truth: once `gitPush=configured`, delivery is commit + git push (Zerops webhook or GitHub Actions runs the build) and direct deploys answer with the recommended push call instead. Close-mode stays `auto` — it only owns when the session counts as done.
- Switch close-mode to `manual` if external orchestration owns close decisions. ZCP still records every deploy/verify; auto-close just doesn't fire:

```
zerops_workflow action="close-mode" closeMode={"appdev":"manual"}
zerops_workflow action="close-mode" closeMode={"appstage":"manual"}
```

The default stays auto until you explicitly switch.

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

Response carries `running`, `healthStatus`, `url` (the hostname-vantage
address to reach the server — the probe runs localhost inside the
container, so the app must bind `0.0.0.0`, not loopback), `reason`, and
`logTail` — read these before making another call.

Don't hand-roll `ssh appdev "cmd &"`: the SSH session ends with
the call and kills the process. Always go through `zerops_dev_server`.

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

Auto-close fires only when EVERY in-scope service carries `closeDeployMode=auto` AND has a successful deploy + a passing verify that ran AFTER that deploy (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). On a pair with `gitPush=configured`, the deploy evidence is the delivered push build on the build target — the same gate, fed by the watched build instead of a direct deploy. Re-deploying re-opens verify: a deploy replaces the running app version, so a verify that passed before it no longer describes what is live — re-verify after the latest deploy. `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

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
