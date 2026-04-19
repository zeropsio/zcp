---
id: bootstrap-intro
priority: 0
phases: [bootstrap-active]
title: "Bootstrap — overview"
---

### Bootstrap in progress

Bootstrap builds the infrastructure substrate for a Zerops project: service
creation, initial verification, and a persisted evidence file
(`ServiceMeta`) that downstream workflows rely on. Three routes exist:

- **Recipe** — user intent matched a viable recipe; services come from the
  recipe's import YAML.
- **Classic** — no recipe match; deploy a minimal verification server per
  runtime so infrastructure is provably reachable before app code lands.
- **Adopt** — project already has non-managed services; attach
  `ServiceMeta` without touching code.

The route is chosen once at bootstrap start and persists for the session.
Progress through the step list emitted by `zerops_workflow action="status"`.
