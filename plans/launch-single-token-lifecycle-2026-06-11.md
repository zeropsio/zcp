# Single-token launch lifecycle — one credential from create-project to CI

**Date:** 2026-06-11
**Origin:** Karel's direction (this session): the launchKey is one-shot only by
convention (manual revoke; platform expiry is roadmap, not shipped). If the
pipeline needs a standing token anyway, use ONE token for both: create the
project with it AND run GitHub Actions with it. ZCP must structurally maximize
"ZCP/LLM never touches it again", and the max-security recommendation to the
user is REGENERATE + re-set in GitHub. Integration tokens cannot mint other
tokens (live-verified) — that constraint stands.

## Live-verified facts this design rests on (2026-06-11)

| Fact | Evidence |
|---|---|
| Integration token CANNOT create/regenerate/delete tokens — `403 notAllowedForIntegrationToken` fires before ID resolution | live POST create + PUT regenerate(fake id) |
| Integration token CAN READ token lists (org integration tokens AND the account's personal tokens — ids+names, never values) | live GET both list endpoints, HTTP 200 |
| Regenerate (GUI): "creates a new token value while preserving all settings (the old token immediately stops working)" | rbac.mdx Managing Tokens |
| GrantSelfRole(ADMIN) grants the launch token's OWN clientUserId admin on the NEW prod project — the same token then authenticates `zcli push` there | project_admin.go + workflow_launch_production.go:742; eval key pushes daily |
| Service-scope env writes are ALWAYS Type=SECRET (write-only read-back, masked); project-scope `sensitive` does NOT persist | ops/env.go GIT_TOKEN precedent + recon |
| GitHub repo secrets: write-only (no API/CLI read-back), sealed-box, NO read audit; BUT write access ≈ read access (workflow-edit exfiltration) | docs.github.com security-hardening |
| GitHub mitigation (environment `production` + required reviewers) needs: public repo (any plan) or Pro/Team for private environments, ENTERPRISE for private required-reviewers | docs.github.com deployments/environments |
| zcli auth is `zcli login <token>` only (no token env var) | zerops-docs cli.mdx |
| Token expiration: not shipped, platform roadmap only | docs absence confirmed |

## The lifecycle

```
user (GUI, once): create integration token — canCreateProjects, custom-per-project
  ↓ launchKey input (transcript sees it ONCE — residual; see backlog A/B)
ZCP launch: create project + import (startWithoutCode) + GrantSelfRole ADMIN  ← grant becomes BLOCKING
  ↓ post-import, inside the same mutation:
ZCP writes token → SECRET service env ZEROPS_TOKEN_PROD on the source dev service
  (GIT_TOKEN pattern: EnvSetSecretService; fresh SSH sessions see it in ~5-10s)
  ↓ launched response emits prodCD secret command WITHOUT paste-placeholder:
  GH_TOKEN=$(ssh dev 'printf %s "$GIT_TOKEN"') gh secret set ZEROPS_TOKEN_PROD \
    -b "$(ssh dev 'printf %s "$ZEROPS_TOKEN_PROD"')" -R owner/repo
  → the value reaches GitHub WITHOUT re-entering the transcript
  ↓ immediately after gh secret set succeeds:
agent deletes the dev-service env copy (zerops_env delete) — the staging copy lives minutes
  ↓ first release: tag → pipeline → prod runs
ZCP stamps launchState.WindowClosedAt (first-release observed or explicit close)
  → prod-ops REFUSES from then on (structural "ZCP never works with it again")
  ↓ recommendation (atom + checklist, with the token's dashboard deep-link — we CAN
    read the token id via the list API):
user REGENERATES the token in dashboard + re-runs the prepared gh secret set with
the new value → every copy the transcript/LLM/env ever saw is DEAD; the only live
copy is the GitHub secret the user set himself
```

## Why this shape

- **One token, one GUI visit** (vs today's two: launchKey + manual ZEROPS_TOKEN_PROD).
- **Transcript residual is bounded and KILLABLE**: the value enters the transcript
  once (launchKey input — unchanged from today); regeneration invalidates that
  copy entirely. Post-regenerate the system converges to the two-token model's
  security with single-token UX.
- **"ZCP never touches it again" is structural where possible**: no persistence
  (P-LP-1 stays), staging env auto-deleted, prod-ops hard-refuses after
  WindowClosedAt. It is NOT enforceable against the transcript copy — that is
  exactly what the regenerate recommendation (and someday platform expiry /
  backlog transcript-safe handoff A/B) addresses. Honest, not pretended.
- **GitHub side stays gh-CLI** (one-collect backlog unchanged); secret value
  write-only once set.

## Phases

### T1 — conveyance (the mechanical core)
- GrantSelfRole failure becomes BLOCKING in the launch mutation (it is now
  load-bearing for CD; today warning-only).
- Post-import: `ops.EnvSetSecretService(devServiceID, "ZEROPS_TOKEN_PROD", launchKey)`.
- prodCD `secret.command`: replace the `<paste>` placeholder with the SSH read
  of the staged env; `secret.source` rewritten (single-token truth).
- `topology.classifyInfrastructureKeys`: add exact key `ZEROPS_TOKEN_PROD`
  (bundle-leak guard — without it export/launch bundles carry the token into
  agent-visible YAML as auto-secret).
- post-checklist step: after `gh secret set` succeeds, DELETE the staged env
  (`zerops_env action=delete`) — the agent-drivable cleanup.
- Tests: mutation writes env; command shape; classify pin; env-delete guidance pin.

### T2 — window close + lifecycle texts
- `launchState.WindowClosedAt` — stamped when prod-ops/status observes the first
  release live on the prod runtimes (or explicit `action="close-launch-window"`).
- `handleLaunchProdOps`: refuse when WindowClosedAt set ("window closed — the
  token now belongs to CI; for prod operations create a fresh prod-scoped key
  or regenerate").
- Atoms: launch-intro (two-window lifecycle → single-token lifecycle),
  launch-delete-key → launch-token-lifecycle (regenerate recommendation with
  dashboard deep-link composed from the token id read via the list API +
  prepared re-set command), post-checklist reorder.
- jsonschema launchKey text: drop "revoked immediately"; describe the lifecycle.

### T3 — GitHub hardening guidance (atom-only)
- Optional recommendation in the prodCD block / atom: move ZEROPS_TOKEN_PROD to
  a `production` ENVIRONMENT secret + required reviewers where the plan allows
  (public repo any plan; private needs Pro/Team for environments, Enterprise
  for required reviewers) — with the honest threat-model line: with a plain
  repo secret, anyone with write access can exfiltrate via workflow edit.

## Not doing
- No ZCP-side token minting (platform forbids it for integration tokens) and no
  auto-regenerate (same 403; regenerate is the USER's GUI act by design).
- No GitHub API direct writes (one-collect backlog unchanged; gh CLI boundary).
- No OIDC (Zerops has no federation endpoint).
- Reset path keeps requiring the launchKey input (the token outlives the window
  by design now — reset semantics unchanged).

## Open questions for Karel
1. T2 refusal hardness: hard-refuse prod-ops after WindowClosedAt, or
   warn+require an explicit override flag? (Hard refuse = strongest "never
   again" story; override flag = escape hatch for emergencies.)
2. Should the launched response carry the regenerate recommendation as a WARN
   blocker (nagging until acked) or checklist-step only?
