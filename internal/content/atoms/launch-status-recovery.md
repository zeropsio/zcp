---
id: launch-status-recovery
priority: 4
phases: [launch-production-active]
title: "Launch status — mid-flight recovery"
references-fields: []
---

### Launch status — mid-flight recovery

When `action="status"` returns `kind: "launch-active"`, a launch-production workflow is mid-flight for this source project. Conversation context was likely lost (compaction, restart). The envelope carries enough state to resume:

| Field | Use |
|---|---|
| `targetProjectName` | Pass back as `productionProjectName` on the resume call. |
| `status` | Tells you which phase to expect on the next response (e.g. `ready-to-launch` means you still need `launchKey`; `launching` / `configuring-pipeline` means polling). |
| `lastUpdate` | Sanity-check that this is the launch you remember — if minutes old, it's the active one; if days old, the user may have abandoned it (ask before resuming). |
| `ambiguousChoices` | When present, multiple non-terminal launches exist for this source. Pick a `productionProjectName` from the list before the resume call. |

Resume call shape:

```
zerops_workflow workflow="launch-production" productionProjectName="<from envelope>"
```

The `launchKey` is NOT required at the status step — only generate and pass it when the workflow re-enters `ready-to-launch` and you intend to advance to `launching`. Status is read-only; ZCP never constructs a project-admin client on this path.
