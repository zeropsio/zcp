# Completion payload

Return one JSON object with:

- `root_readme` — recipe-root README body (surface 1)
- `env_readmes` — map keyed `"0".."5"` → README body (surface 2)
- `env_import_comments` — map keyed `"0".."5"` → `{project, service:
  {hostname → comment}}` (surface 3)
- `codebase_readmes` — map keyed hostname → `{integration_guide,
  gotchas}` (surfaces 4 + 5)
- `codebase_claude` — map keyed hostname → CLAUDE.md body (surface 6)
- `codebase_zerops_yaml_comments` — map keyed hostname →
  `[{anchor, comment}]`. Engine splices above the named field (surface 7).
- `citations` — map `topic → zerops_knowledge guide id`. Required for
  every `platform-trap` fact.
- `manifest` — `{surface_counts: {<surface>: int}, cross_references:
  [{from, to}]}`. Gate checks read this.

Return once — the engine's `stitch-content` handler writes into the
file tree. Writer-owned paths are locked at the engine boundary.
