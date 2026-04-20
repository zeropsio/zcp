---
id: bootstrap-recipe-match
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [discover]
title: "Recipe matched — plan from the import YAML"
---

### A recipe matched this intent

The recipe's import YAML is the source of truth — do not edit it.

In this `discover` step:

1. **Read the recipe's import YAML and mode** — both are rendered below in
   this same guide. Extract each runtime service's hostname, type, and
   managed-service dependencies from the YAML. Note the **mode** in the
   header; every plan target must use that exact mode.
2. **Submit the plan** via `zerops_workflow action="complete" step="discover"`
   with one `BootstrapTarget` per runtime service. Use the runtime hostnames
   from the YAML verbatim — never rename. Set `bootstrapMode` on every
   target to match the recipe's mode (standard / simple / dev). Managed
   services become dependencies on their runtime owner (derive from
   `priority`, `type`, and the app's needs as documented in the recipe body).
3. **Set `isExisting: false`** on every target — the services don't exist yet;
   `zerops_import` at the provision step creates them. (Adoption kicks in at
   bootstrap close when the conductor writes ServiceMeta evidence files;
   you do NOT need to mark the targets as existing to get adopt semantics.)

Do not write code in this step — `buildFromGit` pulls the recipe's
codebase at import time.
