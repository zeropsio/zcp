---
id: launch-intro
priority: 1
phases: [launch-production-active]
title: "Launch production — overview"
references-fields: []
---

### Launch production — overview

You are launching the source project to a separate Zerops production project. ZCP prepares the bundle, source-control changes, and verification steps; you (the user) generate a one-shot Zerops API key for the mutation window and **delete that key** after the first release is live.

The launch creates infrastructure only: production runtimes come up ACTIVE with EMPTY containers, and the application arrives with the FIRST RELEASE TAG through the production pipeline — the same mechanism every later release uses. Nothing is platform-cloned at import time, so private repos work the same as public ones.

Six top-level statuses gate progress:

| Status | Means |
|---|---|
| `scope-prompt` | ZCP needs: production project name, region, optional custom domain, scaling overrides. |
| `classify-prompt` | Project envs need bucketing (infrastructure / auto-secret / external-secret / plain-config / exclude). |
| `ready-to-launch` | Bundle composed, source-control changes pushed, schema clean, blockers cleared. Awaiting one-shot launch key. |
| `launching` | One-shot key in use; ZCP is creating the project + importing services. No build runs at import time. |
| `failed` | A mutation step failed; `blockers[]` describes recovery. |
| `launched` | Infrastructure live; the APPLICATION is not running yet. Follow the `firstRelease` block: wire the production delivery, push the first release tag, watch it land, THEN delete the launch key. External secrets + custom domain are set in Zerops UI. |

ZCP has **zero standing access** to the production project. The one-shot key flows in via the `launchKey` parameter only on the mutation call (the `action="start"` re-call that carries `launchKey` — there is no separate publish action); ZCP never writes it to state, logs, or audit trail. The MCP tool-call transcript itself records the parameter (that surface is your client's, not ZCP's) — generate the key right before the launchKey-bearing call, then revoke it in the Zerops dashboard once the first release is live.

**Two-window key lifecycle.** During the launch window itself (between the launchKey-bearing call and the first release reaching the production runtimes), the same one-shot key MAY be reused for the small number of follow-up calls ZCP makes — the pipeline-status re-check, prod-ops reads while the first release deploys. Re-prompting the user mid-launch would interleave with Zerops's async pipeline. Once the first release is live (or the user explicitly defers it), the launch window is closed: **revoke the launch key in the Zerops dashboard** and switch to a fresh prod-scoped key for any subsequent production operations (verify, env reads, custom-domain DNS lookups). The launch key has full project mutation rights; leaving it active turns "zero standing access" into permanent admin access.
