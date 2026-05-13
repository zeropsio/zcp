---
id: launch-mutation-key-required
priority: 3
phases: [launch-production-active]
title: "Launch — one-shot API key required for publish"
references-fields: []
---

### One-shot API key required for publish

ZCP cannot create the production project with its standing token (project-scoped). Walk the user through generating a temporary **account-wide** Zerops API key for the launch window — and wait for them to paste the value back before calling the workflow again:

1. Open [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management).
2. Click **Create token**. Name it `zcp-launch-<production-project-name>`.
3. Leave **Custom access per project** UNCHECKED — needs account-wide scope to create projects.
4. Copy the token value (shown once).
5. Paste the value back into the conversation.

When the value lands, re-call the launch workflow with the publish action and the token value passed as `launchKey`. Do NOT invent or guess a value, and do NOT proceed without it — the key is the gate.

The key flows through the workflow handler only — never persisted to state, logs, or transcripts. Once the launch reaches `launched` status, ZCP returns a mandatory checklist that includes **deleting the key** at the same dashboard URL.
