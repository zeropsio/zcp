---
id: launch-mutation-key-required
priority: 3
phases: [launch-production-active]
title: "Launch — launch token to create the production project"
references-fields: []
---

### Launch token to create the production project

**Note**: this guidance applies to the **NEW-PROJECT** launch path only. If you're deploying into an existing prod project (the user supplied `existingProjectId` + `existingProdToken` at the scope-prompt step), you'll have advanced past this point — the workflow uses the project-scoped token instead and goes straight to `launching`. See the scope-prompt's path-selection table for which params trigger which path.

**Before asking for the key, walk the `bundlePreview` with the user** — this is the consent moment. Three fields demand an explicit answer when present: `setupProvenanceHint` (production's build recipe resolved from the dev setup or a legacy default — confirm which `zerops.yaml` setup production builds with, or pass `prodSetupNameOverride`), `managedDepHint` (a managed dep nothing references — exclude or wire it), and per-runtime `containers` (the production scale being paid for). Silence on any of these means the user learns about it from the invoice or the first prod build.

ZCP cannot create a NEW production project with its standing token (project-scoped, no project-creation permission). Walk the user through generating the launch token manually — it creates the project and mints the project's deploy key, and goes with that one call; the deploy key covers the bring-up window and drives the GitHub Actions pipeline. Wait for them to paste the value back before calling the workflow again:

1. Open [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management).
2. Click **Create token**. Name it `zcp-launch-<production-project-name>`.
3. Under **Primary Access**, select **Custom access per project**.
4. Turn ON the **Allow creating projects** toggle that appears below — this is the gate that lets the token create the new prod project. Without it, the launch call will fail at create-project.
5. Leave **Per Project Access Customization** empty — the token only needs project-creation; it gains access to the projects it creates and needs no access to existing ones.
6. Copy the token value (shown once).
7. Paste the value back into the conversation — this is the ONLY time the value crosses it.

When the value lands, re-call the launch workflow with the SAME `action="start"` call shape and the same accumulated inputs, adding the token value as `launchKey` (there is no separate publish action — the launchKey-bearing call IS the mutation call). Do NOT invent or guess a value, and do NOT proceed without it — the key is the gate.

The mutation stages the production project's deploy key — never the launch token — as the `ZCP_LAUNCH_TOKEN` service secret on the source push service; every later launch-window call (prod-ops, pipeline re-check, reset, confirm-production) reads it, so do NOT re-send the value. The launch token never lands in state, logs, the audit trail or a secret. Only a person's own token mints the deploy key: when the launch says the deploy key was not minted, nothing is staged — follow the steps in its warning. Once production is verified fully functional, the window closes via `action="confirm-production"`.
