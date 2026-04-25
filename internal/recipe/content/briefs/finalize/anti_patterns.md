## What NOT to do

- Do NOT re-run `emit-yaml shape=workspace` — that shape is provision-only.
- Do NOT pass your live workspace's secret as a `project_env_vars` value.
- Do NOT resolve `${zeropsSubdomainHost}` to a literal URL.
- Do NOT hand-edit stitched files; use `record-fragment` (default `append`)
  + `record-fragment mode=replace` for corrections.
- Do NOT touch `codebase/<h>/{intro,integration-guide,knowledge-base,
  claude-md/*}` ids — scaffold + feature have already validated their
  content at their own complete-phase. By finalize, those surfaces are
  green.
