---
id: launch-delete-key
priority: 1
phases: [launch-production-active]
title: "Close the launch window (confirm-production)"
references-fields: []
---

### Close the launch window (confirm-production)

The production project is live, but the launch window STAYS OPEN until production is verified fully functional — keep it open while wiring delivery, shipping the first release, and fixing anything that surfaces. The staged `ZEROPS_TOKEN_PROD` secret on the source push service is the single working copy of the launch token: prod-ops, pipeline re-checks and reset read it server-side, so you never re-send the value.

When everything works end-to-end:

1. Ask the user to confirm production is fully functional (first release live on the production runtimes + smoke check passed).
2. Call `zerops_workflow action="confirm-production" productionProjectName="<name>" confirmFunctional=true`. This **deletes the staged secret** — the window closes physically: launch-window calls have nothing left to read.
3. Surface the response's `tokenLifecycle` note to the user: the integration token itself stays valid (GitHub Actions keeps its repo-secret copy). Recommended hygiene — regenerate the token in [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management); regeneration keeps all settings and immediately invalidates the old value everywhere (including every copy this conversation ever saw). The user then updates the GitHub repo secret with the new value in their own terminal — the fresh value never enters the conversation.
