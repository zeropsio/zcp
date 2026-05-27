---
id: launch-intro
priority: 1
phases: [launch-production-active]
title: "Launch production — overview"
references-fields: []
---

### Launch production — overview

You are launching the source project to a separate Zerops production project. ZCP prepares the bundle, source-control changes, and verification steps; you (the user) generate a one-shot Zerops API key for the mutation window and **delete that key** after launch completes.

Six top-level statuses gate progress:

| Status | Means |
|---|---|
| `scope-prompt` | ZCP needs: production project name, region, optional custom domain, scaling overrides. |
| `classify-prompt` | Project envs need bucketing (infrastructure / auto-secret / external-secret / plain-config). |
| `ready-to-launch` | Bundle composed, source-control changes pushed, schema clean, blockers cleared. Awaiting one-shot launch key. |
| `launching` | One-shot key in use; ZCP is creating + importing + polling first deploy. |
| `failed` | A mutation step failed; `blockers[]` describes recovery. |
| `launched` | Done. Delete the launch key. Set external secrets in Zerops UI. Attach custom domain in Zerops UI per emitted DNS records. |

ZCP has **zero standing access** to the production project. The one-shot key flows in via the `launchKey` parameter only during `publish` action; ZCP never writes it to state, logs, or audit trail. The MCP tool-call transcript itself records the parameter (that surface is your client's, not ZCP's) — generate the key right before `publish`, then revoke it in the Zerops dashboard the moment `launched` status returns.

**Two-window key lifecycle.** During the launch window itself (between `publish` and `launched`), the same one-shot key MAY be reused for the small number of poll calls ZCP makes to drive the import + first-deploy sequence — re-prompting the user mid-launch would interleave with Zerops's async import + build pipeline. Once `launched` returns, the launch window is closed: **revoke the launch key in the Zerops dashboard** and switch to a fresh prod-scoped key for any subsequent production operations (verify, env reads, custom-domain DNS lookups). The launch key has full project mutation rights; leaving it active turns "zero standing access" into permanent admin access.
