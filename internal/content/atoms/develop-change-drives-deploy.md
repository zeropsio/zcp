---
id: develop-change-drives-deploy
priority: 2
phases: [develop-active]
title: "Every code change must reach a durable state"
references-fields: [workflow.WorkSessionSummary.CloseReason]
references-atoms: [develop-auto-close-semantics, develop-platform-rules-common, develop-close-mode-auto-workflow-dev, develop-close-mode-auto-workflow-simple]
---

### Every code change must reach a durable state

Iteration cadence is mode-specific:

- Dev-mode dynamic runtime container: see
  `develop-close-mode-auto-workflow-dev`.
- Simple / standard / local / first-deploy: every change →
  `zerops_deploy`.

Auto-close: see `develop-auto-close-semantics`.
