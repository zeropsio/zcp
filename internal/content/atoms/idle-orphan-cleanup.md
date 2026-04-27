---
id: idle-orphan-cleanup
priority: 1
phases: [idle]
idleScenarios: [orphan]
title: "Clean up orphan service metas"
references-fields: [workflow.OrphanMeta.Hostname, workflow.OrphanMeta.Reason]
---

The project has metas on disk for services that no longer exist on
the platform. Either the services were deleted externally (Zerops
dashboard, zcli) or a bootstrap session died before completing.
Listed in `orphanMetas[]` on the envelope.

Clear them before bootstrapping fresh — a new bootstrap with the
same hostname would clash with the stale meta:

```
zerops_workflow action="reset" workflow="bootstrap"
```

Reset clears orphan metas (those whose live counterpart is gone) and
unregisters any dead bootstrap session. Complete metas tied to live
services are preserved. After reset the hostname is free to reuse
for a fresh bootstrap.

After reset, start a fresh bootstrap to recreate the runtimes:

```
zerops_workflow action="start" workflow="bootstrap" intent="<your-description>"
```
