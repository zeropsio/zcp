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
- **`targetService`** — runtime hostname to promote. If `sourceContext.suggestedRuntime` is populated (source has exactly ONE user runtime), use it without asking. If `sourceContext.availableRuntimes` lists multiple, ask the user which to promote (e.g., "frontend vs api vs worker"). Managed services are excluded from the list — they get bundled implicitly.
- **`region`** — optional; defaults to `eu-central`. Other values via Zerops dashboard.
- **`customDomain`** — optional; if set, ZCP emits DNS records + verification probes; user attaches in Zerops UI.
- **`keepNonHA`** — optional; array of managed-service hostnames to keep at `NON_HA` in prod (default: all promoted to `HA`).
- **`envOverrides`** — optional plain-config env overrides for the prod bundle. **No secret values here** — ZCP never receives them.

After scope is complete, ZCP advances to `classify-prompt` for the project-env classification pass.
