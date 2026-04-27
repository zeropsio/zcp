---
id: develop-change-drives-deploy
priority: 2
phases: [develop-active]
title: "Every code change must reach a durable state"
references-fields: [workflow.WorkSessionSummary.CloseReason]
references-atoms: [develop-auto-close-semantics, develop-platform-rules-common, develop-push-dev-workflow-dev, develop-push-dev-workflow-simple]
---

### Every code change must reach a durable state

`deployFiles` is the persistence boundary (see
`develop-platform-rules-common`). Iteration cadence is mode-specific:

- Dev-mode dynamic runtime container: see
  `develop-push-dev-workflow-dev`.
- Simple / standard / local / first-deploy: every change →
  `zerops_deploy`.

Auto-close: see `develop-auto-close-semantics`.
