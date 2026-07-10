---
id: launch-intro
priority: 1
phases: [launch-production-active]
title: "Launch production — overview"
references-fields: []
---

### Launch production — overview

You are launching the source project to a separate Zerops production project. ZCP prepares the bundle, source-control changes, and verification steps; the token that creates the production project AND drives the production pipeline is acquired once for the whole lifecycle — either ZCP mints it itself from a one-time platform delegation on your explicit go-ahead, or you (the user) generate one manually as the fallback — and the launch window closes explicitly once production is verified working.

The launch creates infrastructure only: production runtimes come up ACTIVE with EMPTY containers, and the application arrives with the FIRST RELEASE TAG through the production pipeline — the same mechanism every later release uses. Nothing is platform-cloned at import time, so private repos work the same as public ones.

The launch narrows through these statuses (custom domains are post-launch dashboard work, not a launch input):

| Status | Means |
|---|---|
| `scope-prompt` | ZCP needs: production project name, region, scaling overrides. |
| `source-control-required` | Scope is complete, but a promoted runtime fails the git gate — wire git-push-setup / push HEAD / align the remote, then re-call. |
| `classify-prompt` | Project envs need bucketing (infrastructure / auto-secret / external-secret / plain-config / exclude). |
| `ready-to-launch` | Bundle composed, source-control changes pushed, schema clean, blockers cleared. Awaiting token acquisition — delegated mint on confirmation, or the manual launch token as fallback. |
| `launching` | Launch token in use; ZCP is creating the project + importing services. No build runs at import time. |
| `failed` | A mutation step failed; `blockers[]` describes recovery. |
| `launched` | Infrastructure live; the APPLICATION is not running yet. Follow the `firstRelease` block: wire the production delivery, push the first release tag, watch it land, verify the app works, THEN close the window via `action="confirm-production"`. External secrets + custom domain are set in Zerops UI. |

**Single-token lifecycle.** Token acquisition happens once, at the `action="start"` re-call that advances `ready-to-launch` into the mutation (there is no separate publish action). When a platform delegation is available, ZCP mints the token itself on your explicit `confirmLaunch=true` — no token value ever crosses the conversation. Otherwise the manual walkthrough applies: the value crosses the conversation exactly once as `launchKey`; it never lands in state, logs, or the audit trail. Either way the mutation immediately stages the resolved token as the `ZCP_LAUNCH_TOKEN` service secret on the source push service, and that staged secret becomes the single working copy: pipeline re-checks, prod-ops, reset and the close all read it server-side, and the GitHub repo-secret wiring copies it secret-to-secret (neither read passes through the conversation). Do NOT re-send a value on later calls — re-pass `launchKey` only if the staged secret is gone and no delegation is available.

**Launch-window lifecycle.** The window stays OPEN through delivery wiring, the first releases, and any recovery — CI/CD problems remain fixable, and a botched project can be deleted and relaunched (`action="reset"`), all through the staged secret without re-acquiring the token. Once the user confirms production is fully functional, close the window: `action="confirm-production" confirmFunctional=true` deletes the staged secret, leaving launch-window calls nothing to read. The token itself stays valid — regeneration, not expiry, is what invalidates it — and GitHub Actions keeps its repo-secret copy. Recommended hygiene at close: regenerate the token in the Zerops dashboard — regeneration keeps all settings and immediately invalidates the old value everywhere, including every copy this conversation ever saw — then the user updates the repo secret with the new value in their own terminal.
