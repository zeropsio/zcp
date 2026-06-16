---
title: Close-loop pipeline config via OAuth bridge for launch-window token
status: backlog
opened: 2026-05-12
trigger_to_promote: |
  - User feedback that dashboard-driven config (v1 Path B) is friction enough
    to justify automation, OR
  - Zerops platform team adds a token-side OAuth attachment API (e.g.
    `PostClientUserGithubLink(launchKey, code, state)`) that bypasses the
    SPA-mediated browser flow, OR
  - GitHub adds device-flow OAuth for OAuth Apps (currently only GitHub Apps)
    so ZCP can complete handshake without a browser interaction at all.
---

## Context

Phase A spike (2026-05-12) verified empirically that the Programmatic-SDK
close-loop approach for Part 2 (originally user's preferred Path A) is
not practically viable:

1. **`PutServiceStackExternalRepositoryIntegration` requires the calling
   clientUser to have a per-user GitHub OAuth grant** (error code
   `githubAuthorizationRequired`). The launch-window machine token's
   clientUser does NOT inherit the human user's grant — it's its own
   identity from Zerops's perspective.
2. **OAuth handshake for a machine clientUser via the browser is fragile:**
   - SPA at `/github-auth` route consumes the OAuth `code` parameter
     atomically on page-load (before user can react), making it impossible
     to inject the code into ZCP's launch token via paste-back.
   - Server-side attribution of which clientUser gets the grant appears
     to use the calling Authorization header, not the `state` token's
     embedded `key` — so even if the code did stay valid, the SPA's call
     would attach the grant to whichever user-session is active in the
     browser (Karel-human), not to the machine token's clientUser.
3. **No device-flow / programmatic-handshake alternative exists** — Zerops
   uses GitHub OAuth App (not GitHub App), which restricts OAuth completion
   to browser-mediated flows.

v1 ships with Path B (dashboard-driven). ZCP returns a deep-link to the
prod project's source-code page in Zerops dashboard; user configures
the integration there (where their own clientUser's existing OAuth grant
applies); ZCP verifies via `GetServiceStackExternalRepositoryIntegrationStatus`.

## Why this is in backlog (not rejected)

Path A's promise — fully programmatic, regex set by ZCP not user — remains
attractive. The blocker is Zerops-platform-side (no token-OAuth bridge API),
not ZCP-side. If the platform exposes one, Path A becomes feasible without
the SPA race.

Specifically, a platform API like:

```
POST /api/rest/public/client-user/{id}/link-github-via-installation
body: { installationId }
```

…where ZCP just passes a GitHub App installation ID (no OAuth code dance)
would enable Path A. Or any other server-to-server mechanism that doesn't
require browser-redirect.

## v1 friction this could remove

- User has to leave ZCP, click into dashboard, find the right service,
  fill in `repositoryFullName` + `tagRegex` + select `zeropsYamlSetup`.
  ~3 minutes of careful UI work per runtime service.
- Multi-runtime prod (frontend + api) → 2× repeat.
- Easy to typo regex (path A would default-set it).
- Easy to miss `zeropsYamlSetup: prod` (user might leave default).

## Sketch when promoted

1. Coordinate with Zerops platform team — confirm they expose a
   non-browser OAuth attachment API (sketch above).
2. Extend `ProjectAdminClient` with `PutServiceStackIntegration` +
   `LinkGithubAccountByInstallation` methods.
3. Extend handler `configuring-pipeline` branch: attempt PUT; on
   `githubAuthorizationRequired`, return blocker with installation-id-prompt
   instead of dashboard deep-link.
4. Cleanup of attribution: at workflow `launched`, unlink the GitHub
   association from the machine clientUser (it's a one-shot token; grant
   should be released).
5. Atom updates: replace `launch-pipeline-configure-dashboard.md` with
   `launch-pipeline-link-github-installation.md`.

## Cost when implemented

~200-400 LOC + atoms + tests, on top of v1's Path B implementation.

## References

- `plans/archive/production-lifecycle-part2-2026-05-12.md` (v1 plan)
- `/tmp/zcp-part2-spike-findings.md` (Phase A spike notes — promoted to
  `docs/spec-launch-production-platform-spike.md §B` at end of Phase A)
- SDK files: `PostServiceStackGithubWebhook.go`,
  `PostGithubUserRepositoryAccess.go`, `PutServiceStackExternalRepositoryIntegration.go`
