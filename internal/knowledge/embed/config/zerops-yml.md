# zerops.yaml Specification

## Keywords
zerops.yaml, build, deploy, run, configuration, pipeline, ports, health check, readiness check, cron, env variables, prepare commands, addToRunPrepare, multi-base

## TL;DR
`zerops.yaml` defines the build, deploy, and run configuration for each service. Root key is `zerops:` with a list of service configs identified by `setup:`.

## Structure
```yaml
zerops:
  - setup: <service-hostname>
    build:
      base: <runtime>@<version>          # or array for multi-base
      os: <alpine|ubuntu>                # optional, default alpine
      prepareCommands:                    # cached in base layer
        - <command>
      buildCommands:                      # main build
        - <command>
      deployFiles:                        # what to deploy
        - <path>
      cache:                              # what to cache
        - <path>
      addToRunPrepare:                    # FILES copied from build to run container
        - <path>
    deploy:
      readinessCheck:
        httpGet:
          port: <number>
          path: <path>
        # OR
        exec:
          command: <command>
    run:
      base: <runtime>@<version>          # optional, defaults to build base
      os: <alpine|ubuntu>                # optional
      prepareCommands:                    # runtime image customization
        - <command>
      initCommands:                       # run on every container start
        - <command>
      start: <command>                    # single start command
      ports:
        - port: <number>
          httpSupport: true              # for HTTP ports
        # OR
        - port: <number>
          protocol: <TCP|UDP>            # for non-HTTP ports
      envVariables:
        KEY: value
      healthCheck:
        httpGet:
          port: <number>
          path: <path>
        # OR
        exec:
          command: <command>
      crontab:
        - command: <command>
          timing: <cron-expression>
      documentRoot: <path>               # PHP/Nginx/Static only
```

## Key Sections

### build
- `base` — Runtime and version (e.g., `nodejs@22`, `go@1`). Can be an array for multi-base builds (e.g., `[php@8.4, nodejs@18]`)
- `prepareCommands` — Install system deps; cached in base layer; change invalidates both cache layers
- `buildCommands` — Compile/bundle; runs every build
- `deployFiles` — Files/dirs to deploy (mandatory). Use `path/~` tilde syntax to deploy directory contents
- `cache` — Paths to cache between builds
- `addToRunPrepare` — Files/directories to copy from build container into the run container's base image. Listed under `build:`, not `run:`

### deploy
- `readinessCheck` — Check run during deploy phase before switching traffic (httpGet or exec)

### run
- `start` — Entry point (required for runtimes)
- `ports` — Internal ports (range 10-65435). Use `httpSupport: true` for HTTP, `protocol: TCP|UDP` for non-HTTP
- `healthCheck` — Runtime readiness check (httpGet or exec, 5s timeout, 5min retry window)
- `initCommands` — Runs on every container start (not for package installation)
- `prepareCommands` — System packages/setup for runtime container
- `crontab` — Scheduled tasks (standard cron syntax)
- `envVariables` — Key-value pairs
- `documentRoot` — Subdirectory to serve (PHP/Nginx/Static)

### Special Features
- Multi-base build: `base: [php@8.4, nodejs@18]` — install multiple runtimes in build
- `extends` — DRY: inherit from another service config
- `envReplace` — Replace env var references in static files at deploy time
- `routing` — Static/Nginx only: redirects, CORS, custom headers

## Multi-Service Example
```yaml
zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i && pnpm build
      deployFiles: ./dist
    run:
      start: node dist/index.js
      ports:
        - port: 3000
          httpSupport: true

  - setup: web
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i && pnpm build
      deployFiles:
        - dist/~
    run:
      base: static
```

## Gotchas
1. **Root format is `zerops:` list**: Each service is `- setup: <hostname>`, not a top-level hostname key
2. **`deployFiles` is mandatory**: Build output not auto-deployed — must list explicitly
3. **`initCommands` runs every restart**: Don't install packages here — use `prepareCommands`
4. **`prepareCommands` change = full rebuild**: Both cache layers invalidated
5. **Ports 80/443 reserved**: Cannot use them (except PHP services which use port 80) — Zerops handles SSL termination
6. **`httpSupport: true` for HTTP ports**: Don't use `protocol: HTTP` — `protocol` only accepts TCP or UDP
7. **`addToRunPrepare` belongs under `build:`**: It copies files from build to run container — it's not a run-time command

## See Also
- zerops://config/import-yml
- zerops://platform/build-cache
- zerops://services/_common-runtime
- zerops://examples/zerops-yml-runtimes
