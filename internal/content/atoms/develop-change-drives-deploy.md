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

- Dev-mode dynamic runtime: edit code in place; reload via
  `zerops_dev_server` (no full redeploy for code-only changes).
- Simple / standard / local / first-deploy: every change →
  `zerops_deploy`.

Once close-mode is `auto` or `git-push` and every in-scope service has
both a successful deploy and passing verify, the work session
auto-closes (`closeReason=auto-complete`).
