# Single-token launch lifecycle — one credential from create-project to CI

**Date:** 2026-06-11 (rev 2 — Karel's protocol refinement)
**Origin:** Karel's direction: the launchKey is one-shot only by convention
(manual revoke; platform expiry is roadmap). Use ONE token for both creating
the project AND GitHub Actions. The token can create unlimited projects and
has access ONLY to the projects it created (Karel-confirmed platform
semantics). Integration tokens cannot mint or regenerate tokens
(live-verified). Regeneration is a RECOMMENDATION note, not an enforced step.

**The protocol (Karel, rev 2):** when ZCP receives the token, it immediately
sets it as a SECRET on the source dev service; for the whole integration
window ZCP works with it ONLY through that secret (never through repeated
MCP parameters); when the project is CONFIRMED fully functional, ZCP deletes
the env — which closes the window physically (nothing left to read), not by
policy. The window must stay open through CI/CD setup and the first builds:
problems can surface there and ZCP must still be able to fix them or delete
the whole project and start over (reset).

## Live-verified facts this design rests on (2026-06-11)

| Fact | Evidence |
|---|---|
| Integration token CANNOT create/regenerate/delete tokens — `403 notAllowedForIntegrationToken` before ID resolution | live POST create + PUT regenerate(fake id) |
| Integration token CAN READ token lists (ids+names, never values) — usable for the regenerate deep-link | live GET both list endpoints, 200 |
| Regenerate (GUI): "creates a new token value while preserving all settings (the old token immediately stops working)" | rbac.mdx |
| The launch token reaches the NEW prod project (creator access per Karel; GrantSelfRole(ADMIN) on the token's clientUserId is the belt-and-braces ZCP already does) — same token authenticates `zcli push` there | workflow_launch_production.go:742 + Karel-confirmed semantics; verify creator-access live in T1 e2e |
| Service-scope env writes are ALWAYS Type=SECRET (write-only read-back via API for low-privilege tokens); project-scope `sensitive` does NOT persist | ops/env.go GIT_TOKEN precedent + recon |
| Fresh SSH sessions see a platform env write in ~5-10s (no restart) — the in-container read path | git-push-setup session-auth verify, live-verified 2026-06-10 |
| GitHub repo secrets: write-only, sealed-box, no read audit; write access ≈ read access (workflow-edit exfiltration); environment+required-reviewers mitigation needs public repo or Pro/Team/Enterprise tiers | docs.github.com |
| zcli auth is `zcli login <token>` only | cli.mdx |

## The lifecycle

```
user (GUI, once): create integration token — canCreateProjects
  ↓ launchKey input — the ONLY time the value crosses the transcript
ZCP launch mutation:
  1. create project + import (startWithoutCode) + GrantSelfRole ADMIN
  2. IMMEDIATELY stage the token: EnvSetSecretService(dev service,
     ZEROPS_TOKEN_PROD, launchKey)   ← the protocol's single working copy
  ↓ from here on, EVERY launch-window operation reads the token from the
    staged secret (ZCP server-side via SSHDeployer 'printf %s "$ZEROPS_TOKEN_PROD"',
    local mode via VPN ssh) — pipeline re-check, prod-ops, reset, watch.
    The agent NEVER passes launchKey again; the transcript never re-sees it.
  ↓ delivery wiring (launched response):
  gh secret set ZEROPS_TOKEN_PROD -b "$(ssh dev 'printf %s "$ZEROPS_TOKEN_PROD"')" \
    -R owner/repo   (GH_TOKEN conveyance unchanged) — value goes to GitHub
    without re-entering the transcript
  ↓ first release: tag → pipeline → prod runs
  ↓ WINDOW STAYS OPEN: CI/CD problems, broken builds, reconfiguration, or
    full project delete + restart (reset reads the token from the staged env)
    are all still ZCP-solvable through the staged secret
  ↓ action="confirm-production" (explicit, user-acked; preconditions: first
    release live on the prod runtimes + smoke check done):
  ZCP DELETES the staged env → the window is closed PHYSICALLY — ZCP has
  nowhere to read the credential from; "never works with it again" enforced
  by absence, not policy. Final response carries the regenerate NOTE:
  "recommended: regenerate the token in the dashboard (deep-link) and re-run
  the prepared gh secret set with the new value — kills every copy the
  transcript ever saw; the GitHub secret stays the only live one."
```

## Why this shape

- **One token, one GUI visit**; transcript sees the value exactly once.
- **The staged secret is the single working copy** — repeated operations are
  transcript-safe by construction (the GIT_TOKEN credential-helper pattern,
  already proven for git pushes).
- **Window close = env delete** — elegant physical enforcement. prod-ops after
  close fails for lack of credential, with a message naming the lifecycle
  (fresh prod-scoped key / regenerate) rather than a policy refusal that the
  token would technically still satisfy.
- **Recovery stays first-class until confirmation**: CI/CD issues and the
  delete-and-restart path (reset) keep working through the staged copy — no
  re-prompting the user for the token mid-recovery.
- **Regeneration is a note**, not a gate (Karel): good to do, never nagging.

## Phases

### T1 — staging + conveyance (the protocol core)
- Launch mutation: immediately after project create succeeds, stage the token
  (`ops.EnvSetSecretService(devServiceID, "ZEROPS_TOKEN_PROD", launchKey)`).
  Staging failure = launch failure (the protocol depends on the staged copy).
- `topology.classifyInfrastructureKeys`: add exact key `ZEROPS_TOKEN_PROD`
  (bundle-leak guard — else export/launch bundles carry the value into
  agent-visible YAML).
- prodCD `secret.command`: replace the paste placeholder with the SSH read of
  the staged secret; `secret.source` rewritten to the single-token truth.
- GrantSelfRole stays; verify creator-access semantics live in T1 e2e (if the
  platform grants creator access automatically, the grant is belt-and-braces
  and stays non-fatal; if not, it becomes blocking).
- Tests: staging write in mutation; command shape; classify pin.

### T2 — secret-sourced operations (transcript-safe window)
- `launchKeyFromStage`: pipeline resume, prod-ops, reset, and the first-release
  watch resolve the token by reading the staged secret server-side
  (SSHDeployer exec; local mode over VPN ssh) when `launchKey` is absent.
  Explicit `launchKey` param still accepted (fallback when the env is gone).
- jsonschema + atoms: "pass launchKey ONCE; every later call reads the staged
  secret — do not re-send the value."
- P-LP-1 extension: the staged read is in-request only — never persisted,
  never echoed; pinned like the existing launchKey paths.

### T3 — confirm-production + close
- New `action="confirm-production"` (launch workflow): preconditions = first
  release observed live on prod runtimes (prod-ops listing via staged token) +
  explicit user ack of the smoke check. Effect: DELETE the staged env, stamp
  `launchState.WindowClosedAt` (for honest status messages), return the final
  lifecycle response with the regenerate NOTE + dashboard deep-link (token id
  via the list API) + prepared re-set command.
- prod-ops/resume after close: missing staged secret + no explicit launchKey →
  refuse with the lifecycle message.
- Atoms: launch-intro (single-token lifecycle), launch-delete-key →
  launch-token-lifecycle (staged-secret protocol + confirm step + regenerate
  note), post-checklist final step = confirm-production.

### T4 — GitHub hardening guidance (atom-only, optional)
- Recommendation: move ZEROPS_TOKEN_PROD to a `production` ENVIRONMENT secret
  + required reviewers where the plan allows (public any plan; private needs
  Pro/Team for environments, Enterprise for required reviewers), with the
  honest threat-model line (write access ≈ read access on plain repo secrets).

## Not doing
- No ZCP-side token minting or auto-regenerate (platform forbids both for
  integration tokens; regenerate is the user's GUI act).
- No forced regeneration — note only (Karel rev 2).
- No GitHub API direct writes (one-collect backlog unchanged); no OIDC.

## Resolved questions (Karel rev 2)
1. Window close: NOT at first release — only at explicit confirm-production
   after the project is verified fully functional; closing = deleting the
   staged env (physical, not policy).
2. Regenerate: checklist/closing NOTE only, never a blocker.
