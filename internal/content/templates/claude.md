# Zerops

Zerops is a PaaS with its own schema — not Kubernetes, Docker Compose, or
Helm. The ZCP MCP server exposes `zerops_*` tools that handle every project
operation. This file lists the invariants that apply at all times; phase-
specific rules (diagnostics, deploy flow, strategy, per-runtime behaviour)
are delivered by `zerops_workflow` — always enter through it.

## Where you are

You run on **ZCP** — a small Ubuntu control-plane container with
`Read`/`Edit`/`Write`, `zcli`, `psql`, `mysql`, `redis-cli`, `jq`, and
network access to every service. App code runs in the runtime containers,
not here.

```
┌─ ZCP (you are here) — Ubuntu + tools ─────────────────────────┐
│  SSHFS mounts at /var/www/{hostname}/  →  Read/Edit/Write     │
├─ Runtime containers (nodejs/go/php/bun/…) — reach via ssh ────┤
│  Code runs here. No jq/psql inside; pipe back to ZCP.         │
│    ssh {hostname} "curl ..." | jq .                           │
├─ Managed services (postgresql/valkey/rabbitmq/…) — no SSH ────┤
│  Query from ZCP:  psql "$db_connectionString"                 │
└───────────────────────────────────────────────────────────────┘
```

**Edit on the mount, run over SSH.** Dev containers are git + a dev
server — not where you edit code.

## Workflow is the entry point

**Every task starts with `zerops_workflow action="status"`.** It returns
the current phase (idle / bootstrap / develop / recipe), the concrete next
action, and phase-specific guidance. Skipping it = operating blind.

- **No services yet** → `zerops_workflow action="start" workflow="bootstrap"`.
  The first call is **route discovery**: the engine inspects the project
  and returns a ranked `routeOptions[]` list (resume, adopt, recipe
  candidates, classic) plus a human-readable message. Pick one and call
  start again with `route=<chosen>` (and `recipeSlug=<slug>` when
  route=recipe) to commit the session. Bootstrap itself is
  **infrastructure only** — it provisions services, mounts runtimes, and
  discovers managed-service env var names. It does NOT write code or
  deploy. On retry it hard-stops and escalates to the user.
- **Write code, scaffold `zerops.yaml`, first deploy, iterate** →
  `zerops_workflow action="start" workflow="develop" intent="<one-liner>"`.
  Develop owns everything past bootstrap. A runtime that was just
  provisioned enters the **first-deploy branch**: scaffold `zerops.yaml`
  from the discovered env var catalog, write the application, deploy,
  verify. A passing verify stamps `FirstDeployedAt` on the ServiceMeta
  and future develop sessions enter the normal edit loop.
- **Per-service rules** (dev-mode reload vs. dynamic-runtime restart vs.
  stage-redeploy, asset compile commands) live in
  `/var/www/{hostname}/CLAUDE.md`. Read it before editing.

## Invariants

- **Container user is `zerops`, not root.** Package installs need `sudo`
  (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
- **Build ≠ run container.** Runtime-only packages (`ffmpeg`, `imagemagick`)
  go in `run.prepareCommands`; build-only packages in `build.prepareCommands`.
- **Deploy replaces the run container.** Only `deployFiles` entries survive;
  `/tmp`, installed packages, and runtime edits are wiped.
- **`zerops.yaml`** lives at the repo root. Each `setup:` block (e.g.
  `prod`, `stage`, `dev`) is deployed independently.

## Runtime classes

- **Dynamic** (nodejs, go, python, bun, ruby, …) — start with `zsc noop`;
  the real server starts over SSH after each deploy.
- **Static** (php-apache, nginx) — auto-start after deploy, no manual step.
- **Managed** (postgresql, mariadb, redis/valkey, keydb, rabbitmq, nats,
  object storage) — no deploy; scale and connect only.

## Cross-service references

Use `${hostname_varName}` in `zerops.yaml` and import YAML — `hostname` is
the target service's hostname from its import, `varName` is an env var the
target exposes (verify with `zerops_discover includeEnvs=true`). Typos
render as literal strings — no error raised. `envSecrets` in import YAML
are auto-injected at runtime — never re-reference them in `run.envVariables`.
