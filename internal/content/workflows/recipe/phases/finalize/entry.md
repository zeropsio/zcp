# Finalize — phase entry

This phase authors the last pieces of prose + project-level env configuration that bake into every deliverable import.yaml. It completes when `envComments` and (where needed) `projectEnvVariables` have been passed to `generate-finalize`, the six env import.yaml files regenerate with comments inline, and the post-generate READMEs have been reviewed. `zerops_workflow action=status` shows the authoritative substep list; read it first, then work each substep in the order the server returns.

## What finalize does

The six deliverable environments already exist on disk from the generate step's template render. Finalize feeds two agent-authored inputs through the same renderer so the deliverable carries its editorial layer:

1. **`envComments`** — per-env service + project comments that explain WHY each field is shaped the way it is for THAT env.
2. **`projectEnvVariables`** — per-env `project.envVariables` maps (dual-runtime URL constants, any cross-service env vars shared across all containers of an environment).

Both inputs are keyed by env index as string (`"0"` through `"5"`) and round-trip through the renderer, so a second call to `generate-finalize` is byte-identical to the first. Hand-editing the generated import.yaml to add comments or project vars is wasted work — the next render wipes the edit. Pass it through `generate-finalize` and the edit survives.

## envComments shape (schema)

```
envComments = {
  "<envIndex>": {
    "service": { "<serviceKey>": "<comment text>", ... },
    "project": "<comment text>"
  },
  ...
}
```

- `envIndex` is `"0"` through `"5"`.
- `serviceKey` matches the hostnames present in THAT env's import.yaml: envs 0-1 carry the dev+stage pair (`"appdev"`, `"appstage"`, showcase adds `"apidev"`/`"apistage"` + worker), envs 2-5 collapse to a single runtime slot (`"app"`, `"api"`, `"worker"`). Managed services keep their base hostname everywhere (`"db"`, `"cache"`, `"storage"`, ...).
- `project` is the comment emitted above the `project:` block. Each env can carry different project text — envs 4-5 explain production scaling rationale, envs 0-1 explain dev workspace rationale.
- Refining one env: call `generate-finalize` again with only that env's entry under `envComments` — other envs remain untouched. Within an env, passing a service key with an empty string deletes its comment; passing an empty project string leaves the existing project comment.

## projectEnvVariables shape (schema)

```
projectEnvVariables = {
  "<envIndex>": { "<ENV_VAR_NAME>": "<value>", ... },
  ...
}
```

- Dual-runtime recipes declare cross-service URL constants here (`DEV_API_URL`, `STAGE_API_URL`, `DEV_FRONTEND_URL`, `STAGE_FRONTEND_URL`). Envs 0-1 carry both `DEV_*` + `STAGE_*`; envs 2-5 carry `STAGE_*` only with single-slot hostnames.
- Values emit verbatim — `${zeropsSubdomainHost}` and other interpolation markers are preserved for the platform to resolve at end-user project-import time.
- Single-runtime recipes without cross-service URL constants omit `projectEnvVariables` entirely — the template renders the shared secret on its own.

## Finalize substeps (authoritative order)

1. **Author per-env comments** — write one tailored comment set per env (`envComments`).
2. **Author project env variables** — pass `projectEnvVariables` when the recipe has dual-runtime URL constants or any cross-service env vars that live at project level.
3. **Run `generate-finalize`** with both inputs so the six import.yaml files regenerate with comments + project vars inline.
4. **Review READMEs** — root README intro + env READMEs for factual accuracy against the live plan.
5. **Attest complete** — attestation states that all six import.yaml files regenerated with comments baked in.
