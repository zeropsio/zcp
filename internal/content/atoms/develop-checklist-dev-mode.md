---
id: develop-checklist-dev-mode
priority: 3
phases: [develop-active]
modes: [dev]
environments: [container]
title: "Dev-mode checklist extras (container)"
references-fields: [workflow.ServiceSnapshot.Mode]
---

### Checklist (dev-mode services)

- Dev setup block in `zerops.yaml`: `start: zsc noop --silent`, **no**
  `healthCheck`. The platform keeps the container idle; you start
  the dev process yourself via `zerops_dev_server action=start` after
  each deploy.
- Stage setup block (if a dev+stage pair exists): real `start:`
  command **plus** a `healthCheck`. Stage auto-starts on deploy and
  the platform probes it on its configured interval.
