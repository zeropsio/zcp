---
id: launch-pipeline-configured
priority: 4
phases: [launch-production-active]
title: "Pipeline integration confirmed"
references-fields: []
---

### Pipeline integration confirmed

ZCP read each runtime's `external-repository-integration-status` and saw all configured. Ongoing CD is wired:

- Tag pushes matching the configured regex trigger Zerops builds automatically.
- Push to deploy: `git tag v1.0.0 && git push --tags` (substituting your version).

State file (`.zcp/state/launch-production/<launchID>.json`) records the live config under `pipelineConfigurations` for audit.
