# Substep: subagent (showcase)

This substep completes when the feature sub-agent returns and every feature declared in `plan.Features` has been implemented on the mount. It runs only for showcase-tier recipes — minimal recipes skip this substep entirely.

## What this substep accomplishes

The feature sub-agent lands on a working dashboard skeleton (written at generate) already deployed at dev and verified healthy. Its task is to implement the feature set end-to-end: wiring the frontend components that exercise each feature, the API endpoints the frontend calls, the worker consumer for any queued work, and any managed-service interactions (DB rows, cache puts, search-index pushes, storage uploads). The sub-agent iterates against the running containers — scaffolded files on the mount, processes live, managed services reachable — until every feature's UI interaction produces the declared observable change.

## The action at this substep

Compose and transmit the feature sub-agent dispatch prompt, then await its return. The dispatch brief is assembled from the `briefs/feature/*` atoms by the Go stitching layer; you do not author brief content here. You do:

1. Confirm every dev target is healthy (verify-dev and init-commands have completed) — the sub-agent needs a working baseline to iterate against.
2. Dispatch the sub-agent via the Agent tool with the composed brief.
3. Wait for the sub-agent to return a completion-shape report. Do not interleave other work while it runs.
4. On return, read its completion report, confirm every feature from `plan.Features` is addressed, and move to the next substep (`snapshot-dev`) which re-deploys to bake the sub-agent's edits onto the dev containers.

While the sub-agent is running, do not issue browser calls, do not issue `zerops_deploy` calls against the targets it is working on, and do not edit files inside its codebase trees — the sub-agent owns the mount during its dispatch window.
