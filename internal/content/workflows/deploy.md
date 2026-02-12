# Deploy: Deploying Code to Zerops Services

## Overview

Deploy application code to Zerops services using zcli push or SSH-based deployment.

## Steps

### 1. Verify Service State

Confirm the target service exists and is running:

```
zerops_discover service="api"
```

### 2. Ensure zerops.yml Exists

Your project needs a `zerops.yml` that defines the build and deploy pipeline:

```yaml
zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist
        - node_modules
        - package.json
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node dist/index.js
```

### 3. Deploy

Deploy using the deploy tool:

```
zerops_deploy workingDir="/path/to/project" serviceId="<service-id>"
```

### 4. Monitor Deployment

Check deployment status via events:

```
zerops_events serviceHostname="api" limit=5
```

Check logs for runtime errors after deployment:

```
zerops_logs serviceHostname="api" since="5m" severity="error"
```

## Build Pipeline

The Zerops build pipeline runs in order:

1. **prepareCommands** — cached, run once (install system deps, global tools).
2. **buildCommands** — run every deploy (compile, bundle, test).
3. **deployFiles** — files/dirs copied to runtime container.

## Runtime Startup

1. **initCommands** — run on every container start (migrations, cache warm-up).
2. **start** — the main process command.

## Tips

- `prepareCommands` are cached. Change them only when system dependencies change.
- `initCommands` run on every start -- keep them fast and idempotent.
- Use `deployFiles` to specify exactly what goes to production (no source code, no dev deps).
- Check `zerops_logs` immediately after deploy to catch startup errors.
