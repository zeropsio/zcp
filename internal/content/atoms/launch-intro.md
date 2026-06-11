---
id: launch-intro
priority: 1
phases: [launch-production-active]
title: "Launch production — overview"
references-fields: []
---

### Launch production — overview

You are launching the source project to a separate Zerops production project. ZCP prepares the bundle, source-control changes, and verification steps; you (the user) generate ONE integration token for the whole lifecycle — it creates the production project AND drives the production pipeline — and the launch window closes explicitly once production is verified working.

The launch creates infrastructure only: production runtimes come up ACTIVE with EMPTY containers, and the application arrives with the FIRST RELEASE TAG through the production pipeline — the same mechanism every later release uses. Nothing is platform-cloned at import time, so private repos work the same as public ones.

Six top-level statuses gate progress:

| Status | Means |
|---|---|
| `scope-prompt` | ZCP needs: production project name, region, optional custom domain, scaling overrides. |
| `classify-prompt` | Project envs need bucketing (infrastructure / auto-secret / external-secret / plain-config / exclude). |
| `ready-to-launch` | Bundle composed, source-control changes pushed, schema clean, blockers cleared. Awaiting the launch token. |
| `launching` | Launch token in use; ZCP is creating the project + importing services. No build runs at import time. |
| `failed` | A mutation step failed; `blockers[]` describes recovery. |
| `launched` | Infrastructure live; the APPLICATION is not running yet. Follow the `firstRelease` block: wire the production delivery, push the first release tag, watch it land, verify the app works, THEN close the window via `action="confirm-production"`. External secrets + custom domain are set in Zerops UI. |

**Single-token lifecycle.** The token value crosses the conversation exactly ONCE — the `action="start"` re-call that carries `launchKey` (there is no separate publish action); it never lands in state, logs, or the audit trail. The mutation immediately stages it as the `ZEROPS_TOKEN_PROD` service secret on the source push service, and that staged secret becomes the single working copy: pipeline re-checks, prod-ops, reset and the close all read it server-side, and the GitHub repo-secret wiring copies it secret-to-secret (neither read passes through the conversation). Do NOT re-send the value on later calls — re-pass `launchKey` only if the staged secret is gone.

**Launch-window lifecycle.** The window stays OPEN through delivery wiring, the first releases, and any recovery — CI/CD problems remain fixable, and a botched project can be deleted and relaunched (`action="reset"`), all through the staged secret without re-asking for the token. Once the user confirms production is fully functional, close the window: `action="confirm-production" confirmFunctional=true` deletes the staged secret, leaving launch-window calls nothing to read. The token itself stays valid (Zerops has no one-shot token type) and GitHub Actions keeps its repo-secret copy. Recommended hygiene at close: regenerate the token in the Zerops dashboard — regeneration keeps all settings and immediately invalidates the old value everywhere, including every copy this conversation ever saw — then the user updates the repo secret with the new value in their own terminal.
