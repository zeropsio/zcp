---
id: bootstrap-intro
priority: 0
phases: [bootstrap-active]
title: "Bootstrap — overview"
---

Bootstrap has three routes:

- **Recipe** — services come from a matched recipe's import YAML.
- **Classic** — deploy a minimal verification server per runtime so
  infrastructure is provably reachable before app code lands.
- **Adopt** — attach `ServiceMeta` to existing non-managed services; no
  code touched.

Route is chosen at bootstrap start and persists for the session. Follow
the step list from `zerops_workflow action="status"`.
