# Workspace import.yaml — dual-runtime URL constants (API-first recipes)

Dual-runtime recipes (one runtime serves an API, another runtime serves a frontend) reference each other by URL. The generate step writes those URLs into zerops.yaml's `run.envVariables` using project-level env var references, so the project-level constants must exist on the workspace before generate begins. Set them after services reach RUNNING, in a single `zerops_env` call.

Single-runtime recipes skip this entirely.

## URL shape — port suffix rule

Each service has a deterministic public URL derived from its `${hostname}`, the platform-provided project env `${zeropsSubdomainHost}`, and its HTTP port. The port segment appears for dynamic-runtime services; serve-only services omit it:

```
https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app   # dynamic runtime on port N
https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app          # static (Nginx, no port segment)
```

The port comes from the target's `run.ports[0].port` in zerops.yaml. You are writing zerops.yaml at the next step but you already know the port from `plan.Research.httpPort` (for example `3000` for NestJS, `5173` for Vite dev, `80` for static). Set the URL constants now with the correct port suffixes — setting them later forces a cascading restart.

## What to set at provision

The workspace pair is dev + stage for the roles the plan declares (typical roles: `API`, `FRONTEND`; add `WORKER` only when the worker exposes a public surface, which is rare). For each role, set both `DEV_{ROLE}_URL` and `STAGE_{ROLE}_URL` with the correct port suffix.

```
zerops_env project=true action=set variables=[
  "DEV_API_URL=https://apidev-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app",
  "DEV_FRONTEND_URL=https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
  "STAGE_API_URL=https://apistage-${zeropsSubdomainHost}-{apiPort}.prg1.zerops.app",
  "STAGE_FRONTEND_URL=https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
]
```

The generate step will reference these through `run.envVariables` so the application code reads one env var per URL. Static-frontend services in dev (Vite on port 5173) use the dev server port, not port 80 — the dev setup overrides the serve-only base with a toolchain runtime, and the URL must reflect the toolchain port.

## Batch all project-level env vars into one call

Each `zerops_env set` invocation restarts every container that reads project-level vars. Multiple calls in sequence trigger multiple cascading restarts, each killing any SSH-launched processes. Set `JWT_SECRET` and every framework secret alongside the `DEV_*` and `STAGE_*` URL constants in a single invocation — one call, one restart wave, one stable state.

## Handoff to finalize

The URL constants set here are the dev+stage pair only — envs 0 and 1 of the six-env recipe deliverable. The full six-env breakdown (STAGE-only values in envs 2–5) is produced at finalize and baked into the deliverable imports there. At provision the job is the workspace pair, computed with the correct port suffixes, set in one batched call.
