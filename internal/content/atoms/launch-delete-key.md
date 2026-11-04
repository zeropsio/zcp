---
id: launch-delete-key
priority: 1
phases: [launch-production-active]
title: "Close the launch window (confirm-production)"
references-fields: []
---

### Close the launch window (confirm-production)

The production project is live, but the launch window STAYS OPEN until production is verified fully functional — keep it open while wiring delivery, shipping the first release, and fixing anything that surfaces. The staged `ZCP_LAUNCH_TOKEN` secret on the source push service — the production project's deploy key — is the launch window's single working copy: prod-ops, pipeline re-checks and reset read it server-side, so you never re-send the value.

When everything works end-to-end:

1. Ask the user to confirm production is fully functional (first release live on the production runtimes + smoke check passed).
2. Call `zerops_workflow action="confirm-production" productionProjectName="<name>" confirmFunctional=true`. This **deletes the staged secret** — the window closes physically: launch-window calls have nothing left to read.
3. Surface the response's `tokenLifecycle` note to the user: GitHub Actions keeps delivering with the deploy key in its repo secret. If the launch key was made for this launch, the user deletes it in [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management) — nothing in the delivery chain uses it.

**Tearing the project down instead (test launch, wrong name, abandoned)?** Do NOT reach for zcli or the raw token value — `zerops_workflow action="reset" workflow="launch-production" productionProjectName="<name>"` deletes the production project (the staged deploy key is read server-side), the staged secret, and the launch state, behind a diagnose-then-ack gate. This works only while the launch window is open: after `confirm-production` the staged deploy key is gone and the project can be deleted only from the dashboard.
