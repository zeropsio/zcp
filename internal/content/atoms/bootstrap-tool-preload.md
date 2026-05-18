---
id: bootstrap-tool-preload
priority: 1
phases: [bootstrap-active]
environments: [container]
title: "Pre-load deferred tool schemas in one ToolSearch call"
---

### Pre-load tool schemas in one batch

`zerops_*` tools are deferred — schemas load via `ToolSearch`. Loading
them sequentially burns 2-3 round-trips before the first real action.
On the first turn, batch-load:

```
ToolSearch query="select:mcp__zerops__zerops_workflow,mcp__zerops__zerops_discover,mcp__zerops__zerops_import,mcp__zerops__zerops_deploy,mcp__zerops__zerops_verify,mcp__zerops__zerops_logs,mcp__zerops__zerops_events,mcp__zerops__zerops_dev_server"
```

`select:` accepts a comma-separated list and returns all matching
schemas in one round-trip. Loading sequentially defeats the point.
