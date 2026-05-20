# Backlog — Production L7 exposure policy: zerops.app subdomain on launch?

**Surfaced:** 2026-05-19 (eval root-cause review)
**Status:** Open — product policy decision needed
**Triggers re-evaluation:** Karel decides whether production launch should auto-enable `*.zerops.app` subdomain for HTTP-eligible runtimes, or keep the current "explicit custom-domain" stance.

---

## Question

Should `zerops_workflow workflow="launch-production"` auto-enable the `zerops.app` subdomain on the imported production runtime services, matching the behavior of `zerops_deploy` for dev/stage/simple/standard/local-stage?

## Current behavior (eval evidence + code audit)

- `zerops_deploy` calls `maybeAutoEnableSubdomain` from 4 sites (`deploy_local.go:176`, `deploy_ssh.go:248`, `deploy_batch.go:142`, `workflow_record_deploy.go:186`) — first-deploy auto-enable for eligible modes.
- `launch-production` does NOT call it from `executeLaunchMutation` or `executeExistingProjectMutation` — composer at `internal/ops/bundle/launch.go:90-126` deliberately strips `enableSubdomainAccess` per P-PROD-2 invariant.
- Result: post-launch `curl https://{hostname}-{subdomainHost}.zerops.app` returns 502 even though `{hostname}_zeropsSubdomain` env var is populated.
- 5 eval runs (`launch-to-existing-prod-project` retries, `launch-production-pipeline-not-configured`, `launch-production-laravel-showcase`) reported this as a friction point. Agents cargo-culted dev-side auto-enable expectation.

## Why current policy is intentional

- `docs/spec-launch-production-platform-spike.md:445-457` documents the strip behavior.
- `launch_readiness.go:127-135` reports `prod-subdomain-disabled` as a PASSING readiness check (production-best-practice).
- Production prefers custom-domain attached via Zerops dashboard (HTTPS-with-LetsEncrypt, no `zerops.app` branding leaking to end users).

## What would change if policy flips

If Karel decides prod SHOULD auto-enable zerops.app subdomain:

1. **`internal/platform/project_admin.go:33-110`** — `ProjectAdminClient` lacks `GetProject`, `GetService`, `EnableSubdomainAccess`, `DisableSubdomainAccess`. New-project launch cannot reach `ops.Subdomain` today; expand the interface (P-LP-2 narrowing implications — `TestProjectAdminClientRestrictedImport` would fail without an explicit allowlist update).
2. **`executeLaunchMutation`** (new-project path) and **`executeExistingProjectMutation`** (existing-project path) — call `maybeAutoEnableSubdomain` (or equivalent against target-project client) after import processes settle. Already polls service ACTIVE; new step waits for L7 too.
3. **`internal/ops/bundle/launch.go`** — restore `enableSubdomainAccess: true` emission for eligible runtimes (or keep stripping + rely on post-import explicit enable).
4. **`docs/spec-launch-production-platform-spike.md`** + **CLAUDE.md** — invariant flip.
5. **Tests** — `TestBuildLaunchBundle_StripsSubdomainAccess` inverts; `readinessCheckSubdomainDisabled` becomes a non-event; new `TestLaunchAutoEnableSubdomain_*` cases.
6. **Atom** — `launch-subdomain-policy.md` rewrites to reflect auto-enable as default with opt-out hint.
7. **Cross-project recovery surface** — agent post-launch cannot call `zerops_subdomain action=enable` from source-project MCP session. If auto-enable replaces the need, this is fine. If we want both auto + manual override, see separate backlog `launch-prod-subdomain-recovery-from-source-mcp.md` (which doesn't exist yet — write it before shipping option 2 below).

## Options to consider

1. **Keep current policy (recommended baseline)** — production stays custom-domain-first. Ship-now mitigation: explicit `launch-subdomain-policy` atom + CLAUDE.md scope update (done, FIX 2 of 2026-05-19 review). No code change to launch handlers.
2. **Auto-enable on launch (policy flip)** — match dev-side behavior. Affects ~12-18 files per Codex estimate. Worth doing only if eval evidence keeps surfacing the friction even after FIX 2's guidance lands.
3. **Hybrid: opt-in field at launch time** — agent passes `enableProductionSubdomain=true` in `WorkflowInput` to trigger auto-enable; default still off. Smallest blast radius if Karel wants both stances available.

## Triggers to promote out of backlog

- Repeated eval friction on `launch-production-*` scenarios reporting "expected subdomain to work post-launch" even after FIX 2 atom lands.
- Product decision: Karel wants prod-side L7 to be exposed by default (low-friction onramp for first-time users).
- Bundle composer / launch state shape needs cleanup that's coupled to subdomain emission anyway.

Don't promote without an explicit "ship the auto-enable" or "ship the hybrid" call from Karel. Default action when surfaced again: re-route to the guidance atom.

## Related

- Code: `internal/ops/bundle/launch.go`, `internal/tools/workflow_launch_production.go`, `internal/tools/launch_existing.go`, `internal/tools/deploy_subdomain.go`
- Tests: `internal/ops/bundle/launch_bundle_test.go::TestBuildLaunchBundle_StripsSubdomainAccess`, `internal/tools/launch_readiness.go`
- Specs: `docs/spec-launch-production-platform-spike.md:445-457`, CLAUDE.md "Subdomain L7 activation" invariant
- Eval evidence: `eval/behavioral/runs/20260517-185653/launch-to-existing-prod-project/self-review.md` (Issue 6), `20260517-174900/launch-to-existing-prod-project/self-review.md` (Issues 3-4), `20260518-150106/launch-production-dev-only/self-review.md`
