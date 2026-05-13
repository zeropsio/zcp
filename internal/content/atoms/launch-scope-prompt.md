---
id: launch-scope-prompt
priority: 2
phases: [launch-production-active]
title: "Launch scope — collect production target details"
references-fields: []
---

### Launch scope — collect production target details

The scope-prompt response carries a `sourceContext` block when source-project discovery succeeded. Apply the suggestions on the next workflow call:

- **`productionProjectName`** — use `sourceContext.suggestedTargetName` (derived: `<source>-dev` / `<source>-stage` → `<source>-prod`, otherwise `<source>-prod` appended). Confirm the name with the user briefly; do not silently rename if it differs from what the user already mentioned in conversation.
- **`targetService`** — runtime hostname to promote. Use `sourceContext.suggestedRuntime` (always the dev-half hostname) when populated — source has exactly ONE runtime in that case. When `sourceContext.availableRuntimes` lists multiple entries, ask the user which to promote and disclose `stageHostname` on standard-mode pairs ("promoting `appdev` ships the dev-stage pair's published source — `appstage` is the staging half"). Managed services are excluded from the list — they get bundled implicitly. If the user names the stage-half (e.g. `appstage`), the response fires the `scope-stage-half-not-promotable` blocker pointing at the correct dev-half; re-call with that hostname.
- **`region`** — optional; defaults to `eu-central`. Other values via Zerops dashboard.
- **`customDomain`** — optional; if set, ZCP emits DNS records + verification probes; user attaches in Zerops UI.
- **`keepNonHA`** — optional; array of managed-service hostnames to keep at `NON_HA` in prod (default: all promoted to `HA`).
- **`envOverrides`** — optional plain-config env overrides for the prod bundle. **No secret values here** — ZCP never receives them.

After scope is complete, ZCP advances to `classify-prompt` for the project-env classification pass.
