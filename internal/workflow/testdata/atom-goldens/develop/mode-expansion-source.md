---
id: develop/mode-expansion-source
atomIds: [develop-intro, develop-change-drives-deploy, develop-close-mode-auto-deploy-container, develop-checklist-simple-mode, develop-close-mode-auto, develop-close-mode-auto-workflow-simple, develop-knowledge-pointers, develop-auto-close-semantics, develop-verify-matrix, develop-strategy-awareness, develop-mode-expansion, develop-close-mode-auto-simple]
description: "Deployed simple-mode service running close-mode auto — single-slot non-mutating runtime, common starter shape for a worker / API before considering pair expansion. S8 differentiation: ModeSimple (vs steady-dev's ModeDev) covers the simple arm of develop-mode-expansion's modes:[dev,simple] axis."
---
=== develop-intro ===
### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

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

=== develop-close-mode-auto-deploy-container ===
### close-mode=auto Deploy

The dev container uses SSH push — `zerops_deploy` uploads the working tree from `/var/www/<hostname>/` straight into the service without a git remote. Authentication is handled by `zerops_deploy` itself; no credentials on your side. The response's `mode` is `ssh`; `sourceService` and `targetService` identify the deploy class.

- Self-deploy (single service): `sourceService == targetService`, class is self.
- Cross-deploy (dev → stage): class is cross — emit `sourceService` and `targetService` separately.

```
zerops_deploy targetService="appdev"
```

`deployFiles` discipline differs per class: self-deploy needs `[.]` (narrower patterns destroy the target's source); cross-deploy cherry-picks build output.

---

=== develop-checklist-simple-mode ===
### Checklist (simple-mode services)

- A dynamic runtime needs a real `start:` command in its `zerops.yaml`
  entry — simple-mode services auto-start on deploy (no manual dev-server
  step like dev mode). `healthCheck` is **recommended** (a deterministic
  readiness probe) but **not required** — `run.start` keeps the service up
  and verify probes `GET /` regardless. (Implicit-webserver runtimes auto-run
  their server; no explicit `start:` needed.)
- There is no dev+stage pair; `appdev` is the single runtime container.

---

=== develop-close-mode-auto ===
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
```

The default stays auto until you explicitly switch.

---

=== develop-close-mode-auto-workflow-simple ===
### Development workflow

Edit code at `/var/www/<hostname>/` for each in-scope simple-mode runtime. Every code change → redeploy; the runtime container auto-starts on deploy (via `run.start`):

```
zerops_deploy targetService="appdev" setup="prod"
zerops_verify serviceHostname="appdev"
```

Config-only changes still deploy; env-var live timing rules carry the same axis.

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
| web (dynamic / static / implicit-webserver) | `zerops_verify` → judge `http_root`: `httpStatus` + `bodyText` + `consoleErrors`; healthy + a real body (not a blank shell / error page, no fatal console error) proves it |

When `bodyText`/`consoleErrors` are missing, truncated, or the page needs
interaction / SPA routes / non-root / auth, drive the browser **inline** with
`zerops_browser`. Never spawn a sub-agent, call raw `agent-browser`, or use `eval`.
Internal-only service (no public subdomain) → `zerops_subdomain action="disable"` after deploy.

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

---

=== develop-mode-expansion ===
### Mode expansion — add a stage pair

This atom fires once per in-scope `mode: dev` or `mode: simple` (single-slot) service — for each, expanding to **standard** adds a stage sibling without touching the existing service. Expansion is an infrastructure change — it runs through the bootstrap workflow, not develop. Repeat the procedure below per service when multiple in-scope services need stage pairs.

```
zerops_workflow action="start" workflow="bootstrap"
  intent="expand appdev to standard — add stage"
```

Submit a plan that flags the existing runtime and names the new
stage hostname:

```json
{
  "runtime": {
    "devHostname": "appdev",
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

---

=== develop-close-mode-auto-simple ===
### Closing the task

Simple-mode services auto-start on deploy; every code change → `zerops_deploy`:

```
zerops_deploy targetService="appdev" setup="prod"
zerops_verify serviceHostname="appdev"
```
