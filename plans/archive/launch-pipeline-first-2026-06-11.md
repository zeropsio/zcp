# Launch pipeline-first — production's first deploy is its first release

**Date:** 2026-06-11
**Decision (Karel):** universal — for ALL repos, not just private. The launch-production
import composes WITHOUT `buildFromGit`; runtimes start via `startWithoutCode: true`;
the FIRST production build arrives through the production pipeline (tag-triggered),
exactly like every subsequent one. buildFromGit leaves the launch path entirely.
Export is untouched (public template repos are its purpose).

## Why

buildFromGit is a parallel delivery mechanism with a different auth model and the
worst observability in the system, live-verified 2026-06-10/11:

- **Public repos only.** The platform clone runs with no credential — a private repo
  reaches terminal FAILED in ~0.3 s with NO build container and NO logs.
- **Trailing `.git` fails identically** (handled by CanonicalRepoURL, but the failure
  class exists only because this mechanism exists).
- It builds whatever HEAD is at import time — not a release. Every later prod deploy
  goes through the pipeline; the first one was a special case.

Pipeline-first kills the public-repo constraint, the no-logs failure class, and the
FP-3 visibility gate wholesale, and gives production one invariant: **prod runs only
tagged releases, delivered by one mechanism.**

## Live-verified facts the design rests on (2026-06-11, eval-zcp/zcp-eval)

| Fact | Evidence |
|---|---|
| `startWithoutCode: true` import → service **ACTIVE**, container boots (systemd), `/var/www` empty, SSH works; import returns ~12 s (stack.create + stack.deploy, no build) | live import of `swcprobe` nodejs@22, deleted after |
| Schema: `startWithoutCode` = boolean, "Set true, if you want to start service without code." | GET import-project-yml-json-schema.json |
| Project-scoped token mint API exists: `POST /client/{clientId}/integration-token` (body: name, roleCode, capability bools, `projects:[{projectId, roleCode}]`); raw token returned ONLY by POST/regenerate | zerops-go SDK `PostClientIntegrationToken` (in vendored v1.0.20) |
| **Integration tokens cannot mint tokens**: `403 notAllowedForIntegrationToken` — "use a personal token or log in with your user account" | live POST with eval project key |
| Webhook repo integration is pure-API configurable: `PUT /service-stack/{id}/external-repository-integration` `{githubIntegration:{repositoryFullName, eventType: BRANCH\|TAG, tagRegex, isActive, zeropsYamlSetup, triggerBuild}}` + status GET + manual trigger PUT | SDK `PutServiceStackExternalRepositoryIntegration` (v1.0.20) |
| Without the user's browser OAuth grant the PUT fails `400 githubAuthorizationRequired`; grant is a browser-only OAuth code flow (`GET /github/auth-url` → `POST /github/user-repository-access`), no PAT accepted | live PUT on eval `app` service |
| zcli push works with an org-role NO_ACCESS + per-project ADMIN integration token (the eval key's exact shape) | daily flow-eval usage |
| prodCD Actions block already emits tag-triggered (`v*.*.*`) `.github/workflows/zerops-prod.yml` with concrete imported service IDs + `zcli login "$ZEROPS_TOKEN_PROD"` + `zcli push --service-id … --setup …` + SSH-conveyed `gh secret set` one-liner | `workflow_launch_production.go:1728-1809` |
| Composer already anticipated this variant: "launch-prod (no buildFromGit, startWithoutCode) deferred to Phase 6b" | `ops/bundle/rules.go:23-25` |

Consequences of the two 4xx findings: **token mint and webhook API-config are
best-effort** (they succeed only when the launch identity is a personal token, +
OAuth grant for webhook). v1 ships the manual instructions (which already exist);
opportunistic automation is v2 (NOT a masking fallback — the manual step IS the
documented platform flow; automation is an enhancement on top, with the platform
error codes mapped to the manual instruction).

## Shape

```
launched = infra ACTIVE (managed deps + empty runtime containers), app not yet running
  ↓ wire prod delivery (family mirrors the pair's BuildIntegration)
     actions: push zerops-prod.yml (via the configured pair push) + ZEROPS_TOKEN_PROD repo secret
     webhook: dashboard TAG integration (GUI; API attempt in v2)
  ↓ release act: git tag v1.0.0 + push tag        ← the first prod deploy
  ↓ ZCP watches the first release during the launch window (admin client)
  ↓ HTTP exposure choice → smoke test → revoke launchKey
```

`RepoURL` stays REQUIRED on launch inputs — production still builds from the repo;
it feeds pipeline wiring (`owner/repo` for the secret command, `repositoryFullName`
for webhook) instead of the YAML. The source-control gate (push-proof, drift,
clean-tree — checks 1-7) survives unchanged: pushed HEAD remains the only truth.
Only repo-VISIBILITY dies (check 5.5 + the mutation hard-refusal), because nothing
clones without a credential anymore.

When the pair's BuildIntegration is `none`/unset: surface the family choice on the
ready-to-launch response (webhook = fewer secrets, GUI steps; actions = fully
agent-drivable) — no silent default.

## Phases

### P0 — composer: pipeline-first bundle (DONE when: TestBuildLaunch_* pin new emission)
- `ops/bundle/launch.go`: runtime entries emit `startWithoutCode: true`, never
  `buildFromGit` (both VariantLaunchNew + VariantLaunchExisting). Keep `RepoURL
  required` validation (input contract — pipeline wiring needs it).
- `ops/bundle/rules.go:23-25` note → live behavior description.
- Tests: re-pin `TestBuildLaunch_BuildFromGitStripsDotGit` → repurpose to
  `TestBuildLaunch_NoBuildFromGit_StartsWithoutCode`; CanonicalRepoURL stays used
  by export + the identity-compare sites (drift gate) — its launch-emit call site
  moves to the pipeline-wiring strings.
- Export composer untouched (`ops/bundle/export.go:134` keeps buildFromGit).

### P1 — kill FP-3 (repo visibility) (DONE when: grep for the gate is empty + tests green)
- Delete `gateCheckRepoNotPublic` (check 5.5), `readLaunchRepoVisibility`,
  `runRepoVisibilityCheck`, the check-6 suppression coupling, the mutation
  hard-refusal block (`executeLaunchMutation` re-probe), and
  `ops.BuildGitUnauthenticatedLsRemoteCommand` + its tests.
- Gate order list + blocker copy updated; spec FP-3 invariant retired.

### P2 — launched semantics + first-release watch
- Readiness rubric: drop first-build expectations at import time (import is fast,
  no build); add "runtimes ACTIVE-empty by design" check.
- prodCD block: attach for family=actions ALWAYS (today gated on
  meta.BuildIntegration==actions — same condition, now it IS the delivery, plus the
  family-choice prompt when unset).
- Webhook family: launched response carries the dashboard TAG-integration
  instruction (existing launch-pipeline-configure-dashboard atom, re-scoped from
  optional post-step to THE delivery wiring) + integration-status read
  (`launch_pipeline.go`) as the verification.
- First-release watch — DECISION (P2 ship): v1 watches via the EXISTING prod-ops
  surface (the `firstRelease.watch` field points at `action="prod-ops"` service
  listing during the launch window); a dedicated appVersion/process poll
  ("await-first-release") is deferred to P5 alongside the other admin-client
  extensions — prod-ops already shows the deploying runtime, so the dedicated
  poll is an enhancement, not a gap.
- Post-checklist atom reorder: 1. wire delivery, 2. tag release, 3. watch to
  ACTIVE, 4. HTTP exposure choice, 5. smoke test, 6. revoke launchKey.

### P3 — atoms + spec + CLAUDE.md
- Atoms: launch-mutation-key-required (consent preview unchanged), launch flow
  atoms re-keyed off "ZCP imports with buildFromGit" everywhere; HA-assessment +
  classify untouched.
- `docs/spec-workflows.md §10` + `plans/spec-git-delivery-target-2026-06-10.md`
  cross-ref: first prod deploy = first release.
- CLAUDE.md invariant bullet replace (launch-production orchestrator bullet: the
  buildFromGit sentence → pipeline-first truth).
- zerops-docs: startWithoutCode row already in local uncommitted edit; import.mdx
  buildFromGit private-repo note stays (true for export/import surface).

### P4 — existing-project path parity
- Same composition (services-only YAML + startWithoutCode), same wiring fields,
  same checklist. The OAuth-alternative motivation for this path shrinks; the path
  itself stays (merge-into-existing-infra remains its use).

### P5 — v2 automation (opportunistic, after v1 verified)
- `platform.MintIntegrationToken` (PostClientIntegrationToken; shape:
  org roleCode=NO_ACCESS, capabilities false, projects=[{prodProjectId, ADMIN}]) —
  attempted right after project creation; on `notAllowedForIntegrationToken` →
  manual instruction (today's secret.source). Raw value returned ONCE in the
  launched response (P-LP-5 analog: never persisted).
- Webhook: attempt `PutServiceStackExternalRepositoryIntegration` (TAG + regex);
  map `githubAuthorizationRequired`/`gitlabAuthorizationRequired` → GUI atom.
- e2e: positive mint test needs a personal token (operator-supplied); negative
  path pinned with the eval integration key.

### P6 — e2e + flow-eval round-trip
- Launch e2e: pipeline-first composition assertions; flow-eval launch scenario
  walkthrough; full-suite + race + lint.

## Ship log (2026-06-11)

| Phase | Status | Commit |
|---|---|---|
| P0 composer | SHIPPED | 1c562260 |
| P1 FP-3 retirement | SHIPPED (one residual: `ops.BuildGitUnauthenticatedLsRemoteCommand` orphan — file read-blocked by the session permission glob `Read(**/*credential*)`; delete once the glob is adjusted) | d14ae879 |
| P2 launched semantics | SHIPPED (firstRelease block, family-aware blockers/atom, rubric pin; first-release watch v1 = prod-ops pointer, dedicated poll → P5) | bfcad917 |
| P3 atoms+spec+CLAUDE.md | SHIPPED | 99b3e3be |
| P4 existing parity | FREE (shared composer; zero buildFromGit refs in the existing path; endpoint live-verified via service-import probe) | — |
| P5 v2 automation | DEFERRED by design (after v1 verified + Karel's ack on ZCP minting a standing prod CI credential) | — |
| P6 verification | DONE: unit/tool/integration/content green; full `-race` clean; lint-local 0; e2e failure set IDENTICAL pre-cut vs HEAD (23=23, zero symmetric diff — all pre-existing main drift: e2e fixtures vs required `bootstrapMode`, NOT this cut); flow-eval `launch-production-from-standard-pair` on the HEAD build — no cut-related friction (run 20260611-111354); eval container deployed v9.112.1-42-g99b3e3be | — |

Residual live verification that needs an operator-held credential: a REAL
new-project launch mutation (startWithoutCode import via launchKey +
first release through a real pipeline) — needs a launchKey-grade token
only Karel can mint.

## Not doing
- No buildFromGit fallback flag on launch ("just this once") — one mechanism.
- No PAT-based webhook configuration — the platform accepts only browser OAuth.
- No auto-tagging — the release act stays the user/agent's explicit act.
- Export keeps buildFromGit by design.

## Open risks
- Personal-token mint positive case unverified (no personal token at hand) —
  blocks nothing (v1 manual; v2 attempt-and-fallback).
- Stage-pair-less launches (BuildIntegration unset) get one extra prompt (family
  choice) — acceptable; silent default would mint a delivery mechanism the user
  never chose.
