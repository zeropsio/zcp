# zerops.yml Advanced Behavioral Reference

## Keywords
zerops.yml, health check, healthCheck, readiness check, readinessCheck, routing, cors, redirects, headers, crontab, cron, startCommands, initCommands, prepareCommands, envReplace, temporaryShutdown, zero downtime, rolling deploy, base image, extends, container lifecycle

## TL;DR
Behavioral semantics for advanced zerops.yml features: health/readiness checks, deploy strategies, cron, background processes, runtime init, envReplace, routing, and `extends`. Schema is in grammar.md -- this file covers what the schema cannot express.

---

## Health Check Behavior

Health checks run **continuously** on every container after startup. Two types (mutually exclusive):

- **`httpGet`**: GET to `localhost:{port}{path}`. Success = 2xx. Runs **inside** the container. Use `host` for custom Host header, `scheme: https` only if app requires TLS.
- **`exec`**: Shell command, success = exit 0. Has access to all env vars. Use YAML `|` for multi-command scripts.

| Parameter | Purpose |
|-----------|---------|
| `failureTimeout` | Seconds of consecutive failures before container restart |
| `disconnectTimeout` | Seconds before failing container is removed from load balancer |
| `recoveryTimeout` | Seconds of success before restarted container receives traffic again |
| `execPeriod` | Interval in seconds between check attempts |

**Failure sequence**: repeated failures -> `disconnectTimeout` removes from LB -> `failureTimeout` triggers restart -> `recoveryTimeout` gates traffic reconnection.

**DO NOT** configure both `httpGet` and `exec` in the same block.

---

## Readiness Check Behavior

Runs **only during deployments** to gate traffic switch to a new container.

```yaml
deploy:
  readinessCheck:
    httpGet: { port: 3000, path: /health }
    failureTimeout: 60
    retryPeriod: 10
```

**How it works**: Checks the **new** container at `localhost`. Until it passes, traffic stays on the old container. After `failureTimeout`, deploy fails and the old container remains active.

**DO NOT** confuse with healthCheck -- readiness gates a deploy; healthCheck monitors continuously after.

---

## temporaryShutdown

| Value | Behavior | Downtime |
|-------|----------|----------|
| `false` (default) | New containers start first, old removed after readiness | None (zero-downtime) |
| `true` | All old containers stop, then new ones start | Yes |

Use `true` when: exclusive DB migration access needed, or brief downtime acceptable. Use `false` for: production web services, APIs, user-facing apps.

---

## Crontab Execution

```yaml
run:
  crontab:
    - command: "php artisan schedule:run"
      timing: "* * * * *"
      workingDir: /var/www/html
      allContainers: false
```

Parameters: `command` (required), `timing` (required, 5-field cron: `min hour dom mon dow`), `workingDir` (default `/var/www`), `allContainers` (`false` = one container, `true` = all containers).

Cron runs inside the runtime container with full env var access. When `allContainers: false`, Zerops picks **one** container (good for DB jobs). Use `true` for cache clearing or log rotation everywhere. Minimum granularity is 1 minute.

---

## startCommands (Background Processes)

Runs **multiple named processes** in parallel. **Mutually exclusive** with `start`.

```yaml
run:
  startCommands:
    - command: npm run start:prod
      name: server
    - command: litestream replicate -config=litestream.yaml
      name: replication
      initCommands:
        - litestream restore -if-replica-exists -if-db-not-exists $DB_NAME
```

Each entry: `command` (required), `name` (required), `workingDir` (optional), `initCommands` (optional, per-process init). **DO NOT** use both `start` and `startCommands`.

---

## initCommands vs prepareCommands

| Feature | `run.initCommands` | `run.prepareCommands` |
|---------|-------------------|----------------------|
| **When** | Every container start/restart | Only when building runtime image |
| **Cached** | Never | Yes (base layer cache) |
| **Use for** | Migrations, cache warming, cleanup | OS packages, system deps |
| **Deploy files** | Present in `/var/www` | **Not available** -- DO NOT reference app files |
| **Reruns on** | Restart, scaling, deploy | Only when commands change |

---

## envReplace (Variable Substitution)

Replaces placeholders in deployed files with env var values at deploy time.

```yaml
run:
  envReplace:
    delimiter: "%%"
    target: [./config/, ./templates/settings.json]
```

File containing `%%DATABASE_URL%%` gets the placeholder replaced with the actual value. Multiple delimiters supported: `delimiter: ["%%", "##"]`. Use for: secrets in config files, PEM certificates, frontend configs.

**Directory targets are NOT recursive** -- `./config/` processes only files directly in that directory. Specify subdirectories explicitly.

---

## routing (Static Services Only)

```yaml
run:
  routing:
    cors: "'*' always"
    redirects:
      - { from: /old, to: /new, status: 301 }
      - { from: /blog/*, to: /articles/, preservePath: true, status: 302 }
    headers:
      - for: "/*"
        values: { X-Frame-Options: "'DENY'" }
```

- **`cors`**: Sets Access-Control-Allow-Origin. `"*"` auto-converted to `'*'`
- **`redirects[]`**: `from` (wildcards `*`), `to`, `status`, `preservePath`, `preserveQuery`
- **`headers[]`**: `for` (path pattern), `values` (header key-value pairs)
- **`root`**: Custom root directory

**DO NOT** use on non-static services -- silently ignored.

---

## extends (Configuration Inheritance)

```yaml
zerops:
  - setup: base
    build: { buildCommands: [npm run build], deployFiles: ./dist }
    run: { start: npm start }
  - setup: prod
    extends: base
    run: { envVariables: { NODE_ENV: production } }
```

Child sections **replace** parent sections entirely -- no deep merge. Must reference another `setup` name in the same file.

## Base Images

| Runtime | Versions | OS support |
|---------|----------|------------|
| Node.js | `nodejs@22`(latest), `@20`, `@18` | alpine, ubuntu |
| Python | `python@3.12`(latest), `@3.11` | alpine, ubuntu |
| Go | `go@1.22`(latest) | alpine, ubuntu |
| PHP | build: `php@8.4`/`@8.3`/`@8.1`; run: `php-apache@*`/`php-nginx@*` | alpine, ubuntu |
| .NET | `dotnet@9`(latest), `@8`, `@7`, `@6` | alpine, ubuntu |
| Java | `java@21`(latest), `@17` | alpine, ubuntu |
| Rust | `rust@1.80`(latest), `@1.78`, `@nightly` | alpine, ubuntu |
| Bun | `bun@1.2`(latest), `@nightly`, `@canary`, `@1.1`(Ubuntu) | alpine, ubuntu |
| Deno | `deno@2.0.0`(latest), `@1.45.5` | ubuntu only |
| Elixir/Gleam | `elixir@1.16`, `gleam@1.5` | elixir: both; gleam: ubuntu |
| nginx/static | `nginx@1.22`, `static@1.0` | alpine, ubuntu |
| OS images | `alpine@3.20`(latest)`-3.17`; `ubuntu@24.04`, `@22.04` | - |

`@latest` = newest stable. Shorthand aliases: `go@1` = `go@1.22`, `nodejs@latest` = `nodejs@22`.

---

## See Also
- zerops://foundation/grammar -- zerops.yml schema reference and platform rules
- zerops://foundation/runtimes -- runtime-specific configuration deltas
- zerops://guides/production-checklist -- production readiness including health check setup
