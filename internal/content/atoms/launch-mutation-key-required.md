---
id: launch-mutation-key-required
priority: 3
phases: [launch-production-active]
title: "Launch — one-shot API key required for publish"
references-fields: []
---

### One-shot API key required for publish

**Note**: this guidance applies to the **NEW-PROJECT** launch path only. If you're deploying into an existing prod project (the user supplied `existingProjectId` + `existingProdToken` at the scope-prompt step), you'll have advanced past this point — the workflow uses the project-scoped token instead and goes straight to `launching`. See the scope-prompt's path-selection table for which params trigger which path.

ZCP cannot create a NEW production project with its standing token (project-scoped, no project-creation permission). Walk the user through generating a temporary launch-window token — and wait for them to paste the value back before calling the workflow again:

1. Open [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management).
2. Click **Create token**. Name it `zcp-launch-<production-project-name>`.
3. Under **Primary Access**, select **Custom access per project**.
4. Turn ON the **Allow creating projects** toggle that appears below — this is the gate that lets the token create the new prod project. Without it, the launch call will fail at create-project.
5. Leave **Per Project Access Customization** empty — the launch-window token only needs project-creation; it does not need read/write access to any existing project.
6. Copy the token value (shown once).
7. Paste the value back into the conversation.

When the value lands, re-call the launch workflow with the publish action and the token value passed as `launchKey`. Do NOT invent or guess a value, and do NOT proceed without it — the key is the gate.

The key flows through the workflow handler only — never persisted to state, logs, or transcripts. Once the launch reaches `launched` status, ZCP returns a mandatory checklist that includes **deleting the key** at the same dashboard URL.
