# Provision — 2. Import services

Submit the workspace import.yaml to the platform:

```
zerops_import content="..."
```

Wait for every service to reach RUNNING before proceeding. Dev services reach RUNNING immediately because `startWithoutCode: true` brings up empty containers; managed services reach RUNNING once their platform image starts. Stage services stay in READY_TO_DEPLOY — they transition on first cross-deploy from dev, not at import time.

The next substep (mount dev filesystem) requires the dev services to be RUNNING — SSHFS mounts land on live containers. Any service stuck in a non-RUNNING state at this point is a provisioning defect that provision must resolve before continuing.
