# Completion payload

Return one JSON object with:

- `root_readme` — recipe-root README body (surface 1)
- `env_readmes` — map keyed `"0".."5"` → README body (surface 2)
- `env_import_comments` — map keyed `"0".."5"` → `{project, service:
  {hostname → comment}}` (surface 3). Comments wrap the corresponding
  blocks in the generated import.yaml when the engine regenerates the
  6 tier files at `stitch-content`.
- `project_env_vars` — map keyed `"0".."5"` → `{envVarName → value}`.
  Populates each tier's `project.envVariables` in the deliverable
  yamls. Per-env shape:
  - Envs `"0"`, `"1"` (dev-pair slots exist): `DEV_*` + `STAGE_*` URL
    constants with hostnames `apidev`/`apistage`/`appdev`/`appstage`
  - Envs `"2"`..`"5"` (single-slot): `STAGE_*` only with hostnames
    `api`/`app`
  Values:
  - Shared secrets: `<@generateRandomString(<32>)>` — evaluated once
    per end-user at their click-deploy
  - URLs: use `${zeropsSubdomainHost}` literal — the end-user's
    platform substitutes it at their import
  - Anything else emits verbatim
- `codebase_readmes` — map keyed hostname → `{integration_guide,
  gotchas}` (surfaces 4 + 5)
- `codebase_claude` — map keyed hostname → CLAUDE.md body (surface 6)
- `codebase_zerops_yaml_comments` — map keyed hostname →
  `[{anchor, comment}]`. Engine splices above the named field (surface 7).
- `citations` — map `topic → zerops_knowledge guide id`. Required for
  every `platform-trap` fact.
- `manifest` — `{surface_counts: {<surface>: int}, cross_references:
  [{from, to}]}`. Gate checks read this.

Return once — the engine's `stitch-content` handler merges
`env_import_comments` + `project_env_vars` into the plan, regenerates
all 6 deliverable import.yaml files, and writes the surface bodies to
their canonical paths. Writer-owned paths are locked at the engine
boundary.
