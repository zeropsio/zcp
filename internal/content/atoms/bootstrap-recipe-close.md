---
id: bootstrap-recipe-close
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [close]
title: "Recipe — auto-adopt on close"
---

### Close the recipe bootstrap

Complete the close step — `ServiceMeta` records (mode from the plan,
strategy left unset) and the CLAUDE.md log entry are written
automatically. Strategy is picked by develop on first use.

```
zerops_workflow action="complete" step="close" attestation="Recipe {slug} bootstrapped — services active and verified"
```

After close, `zerops_workflow action="status"` will report the transition
message with the two primary follow-up workflows: `develop` (iterate on
the code the recipe provided) and `cicd` (wire git-based deploys).
