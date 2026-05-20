---
id: launch-pipeline-configure-dashboard
priority: 1
phases: [launch-production-active]
title: "Configure CD pipeline in Zerops dashboard"
references-fields: []
---

### Configure CD pipeline in Zerops dashboard

The production runtime has no CD pipeline yet — ongoing pushes will NOT auto-build. Configure it once via dashboard. (ZCP cannot do this through the launch-window key; see `plans/backlog/launch-pipeline-close-loop-oauth.md` for the Path A future.)

For each runtime listed in the `pipeline-not-configured-*` blockers:

1. Open the **deep-link** from the blocker (`https://app.zerops.io/service-stack/<svcID>/deploy`).
2. Click **Connect to GitHub** (or GitLab). Authorize Zerops if asked — uses your existing org-level grant, no extra setup.
3. Select the source repository listed in the blocker's `recommendation.repositoryFullName`.
4. Set the trigger:
   - **Event type:** `Tag`
   - **Tag regex:** the value from `recommendation.tagRegex` (default `^v\d+\.\d+\.\d+$` per Zerops production-checklist).
   - **Zerops YAML setup:** `prod` (matches the setup block written during launch).
5. Save.

Repeat for each runtime in the blockers list. When done, re-call `workflow="launch-production"` with the same `launchKey` — ZCP reads the live integration status and clears the blockers from the response.

To deploy after setup: `git tag v1.0.0 && git push --tags` (matching your tag regex).
