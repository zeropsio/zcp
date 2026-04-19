---
id: bootstrap-recipe-import
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [provision]
title: "Import recipe YAML"
---

A viable recipe was matched for this intent. Next step: import the recipe's
managed services + runtime skeleton via `zerops_import`. The recipe body
contains the canonical YAML; copy it verbatim unless the conductor already
ran the import.

After import completes, wait until every service reports `ACTIVE` status
(use `zerops_discover`). Only then proceed to deploy.
