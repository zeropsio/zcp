---
id: idle-tool-preload
priority: 1
phases: [idle]
environments: [container]
title: "Pre-load deferred tool schemas before the first workflow call"
---

### Pre-load tool schemas in one batch

`zerops_*` tools are deferred — schemas load via `ToolSearch`. Loading
them one at a time across turns burns N-1 round-trips before the first
real action. On the very first turn — BEFORE calling `zerops_workflow` —
batch-load every tool you'll need across bootstrap and develop:

```
ToolSearch query="select:zerops_workflow,zerops_discover,zerops_knowledge,zerops_import,zerops_env,zerops_mount,zerops_deploy,zerops_verify,zerops_logs,zerops_events,zerops_subdomain,zerops_manage,zerops_process"
```

`select:` accepts a comma-separated list and returns all matching schemas
in one round-trip. Phase-specific batch reminders appear later (in
bootstrap and develop) as fallbacks for late-arriving sessions
(compaction recovery, develop without prior bootstrap); when you start
fresh at idle, this single batch covers the full session.
