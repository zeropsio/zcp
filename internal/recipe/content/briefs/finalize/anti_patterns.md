## What NOT to do

- Do NOT re-run `emit-yaml shape=workspace` — that shape is provision-only.
- Do NOT pass your live workspace's secret as a `project_env_vars` value.
- Do NOT resolve `${zeropsSubdomainHost}` to a literal URL.
- Do NOT hand-edit stitched files; use `record-fragment` (default `append`)
  + `record-fragment mode=replace` for corrections.
- By finalize, codebase fragments should be green (scaffold + feature
  validated them at their own complete-phase). If finalize complete-
  phase still flags a residual codebase fragment violation, use
  `record-fragment mode=replace` to correct it. The §R API was added exactly for this case: default mode for codebase ids is append; pass `mode=replace` only when correcting an existing fragment.
