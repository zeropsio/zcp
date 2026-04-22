# Dispatch 1 — scaffold-nestjs-api-codebase

**Dispatched at**: `2026-04-22T09:07:29`
**Description**: Scaffold NestJS API codebase
**Subagent type**: `general-purpose`
**Prompt length**: 14646 chars
**Tool-use id**: `toolu_018P6fj1B79rBEuPM6zGaJoG`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for the Zerops recipe `nestjs-showcase`. Scaffold the **API codebase** only. Target: hostname `apidev`, mount `/var/www/apidev/`.

Target framework: **NestJS latest** on **nodejs@24**, HTTP port **3000**. Primary DB **postgresql@18** via TypeORM. The full service list (below) must be wired as clients at startup — but NO feature routes yet.

**Verify every import, decorator, and module-wiring call against the installed package, not against memory.** Before committing an `import` line, an adapter registration, or any language-level symbol binding, open the package's on-disk manifest (`node_modules/<pkg>/package.json`) and confirm the subpath / symbol you're about to reference is exported by the version actually installed. Training-data memory for library APIs is version-frozen and is the single biggest source of stale-path compile errors the code-review sub-agent has to reject at close time. When in doubt, run the tool's own scaffolder against a scratch directory and copy its import shapes verbatim.

You are scaffolding a health-dashboard-only skeleton. **You write infrastructure. You do NOT write features.** A feature sub-agent runs later with SSH access and authors every feature section end-to-end. Your job is to give it a healthy, deployable, empty canvas.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

**⚠ CRITICAL: where commands run**

You are running on the **zcp orchestrator container**, not on the target dev container. The path `/var/www/apidev/` on zcp is an **SSHFS network mount** — a bridge into the target container's `/var/www/`. It is a write surface, not an execution surface.

- **File writes** via Write/Edit/Read against `/var/www/apidev/` work correctly.
- **Executable commands** (npm install, npm run build, tsc, nest, npx, git init/add/commit) MUST run via `ssh apidev "cd /var/www && <command>"`. NOT via `cd /var/www/apidev && <command>`.

Running commands on zcp produces: node_modules owned by zcp-root (later EACCES), broken symlinks in node_modules/.bin/ that don't resolve inside the container, and native modules compiled for zcp's node ABI. If you see EACCES, "not found" for installed packages, or ownership surprises — you are on the wrong side of the boundary.

**⚠ Framework scaffolders that auto-init git.** `nest new` runs `git init` as a final step. Either pass `--skip-git` OR `ssh apidev "rm -rf /var/www/.git"` immediately after the scaffolder returns. The main agent's later `git init` collides with the residual `.git/`.

**WRITE — the API codebase:**

1. Run `nest new . --skip-git --package-manager npm` via SSH. Answer yes to overwrite prompts. Verify `nest new` actually created files — if it fails, fall back to a manual package.json with nestjs dependencies and write src files by hand.

2. After scaffold, install dependencies (all via `ssh apidev`):
   - `@nestjs/typeorm typeorm pg` (TypeORM + postgres driver)
   - `ioredis` (Valkey/Redis client — managed Valkey has NO password)
   - `nats` (NATS client for publishing from HTTP handlers; the worker codebase does the consuming)
   - `@aws-sdk/client-s3` (S3 for object-storage)
   - `meilisearch` (Meilisearch JS client — **verify the class name actually exported** by reading `node_modules/meilisearch/package.json` and its types; recent versions renamed from `MeiliSearch` to `Meilisearch`)
   - `@nestjs/config` (ConfigModule for env loading — but NEVER load a .env file)
   - Keep the `@nestjs/cli`, `typescript`, `ts-node`, `@types/node` that `nest new` installs in devDependencies.

3. **src/main.ts** — bootstrap with:
   - `app.enableCors({ origin: true, credentials: true })` so the dev Vite frontend can call it
   - `app.getHttpAdapter().getInstance().set('trust proxy', true)` (Principle 3 — proxy trust)
   - `app.enableShutdownHooks()` (Principle 1 — graceful shutdown)
   - `await app.listen(process.env.PORT ?? 3000, '0.0.0.0')` (Principle 2 — routable binding)

4. **src/app.module.ts** — wire:
   - `ConfigModule.forRoot({ isGlobal: true, ignoreEnvFile: true })` (never load .env)
   - `TypeOrmModule.forRoot({ type: 'postgres', host: process.env.DB_HOST, port: parseInt(process.env.DB_PORT, 10), username: process.env.DB_USER, password: process.env.DB_PASS, database: process.env.DB_NAME, entities: [Item], synchronize: false })`
   - Import HealthModule, StatusModule

5. **src/item.entity.ts** — Item entity with id (uuid, PK), title (varchar 200), description (text, nullable), createdAt (timestamptz, default now). The feature sub-agent will query against this.

6. **src/health/health.controller.ts** — `GET /api/health` returns `{ ok: true }` synchronously. `Content-Type: application/json` (default).

7. **src/status/status.controller.ts** + **src/status/status.service.ts** — `GET /api/status` returns a flat object `{ db: "ok"|"error", redis: "ok"|"error", nats: "ok"|"error", storage: "ok"|"error", search: "ok"|"error" }` by pinging each service. Each check wrapped in try/catch that sets the value to "error" on any failure. The controller uses `@Controller('api')` + `@Get('status')`.

8. **src/clients/** — factory/provider modules:
   - **redis.provider.ts** — `new Redis({ host: process.env.REDIS_HOST, port: parseInt(process.env.REDIS_PORT, 10) })` — no password, no url string with embedded creds (Principle 5). Ping on startup.
   - **nats.provider.ts** — `connect({ servers: 'nats://' + process.env.NATS_HOST + ':' + process.env.NATS_PORT, user: process.env.NATS_USER, pass: process.env.NATS_PASS })` — user/pass as structured options (Principle 5). Do NOT embed `user:pass@` in URL.
   - **s3.provider.ts** — `new S3Client({ endpoint: process.env.S3_ENDPOINT, region: 'us-east-1', credentials: { accessKeyId: process.env.S3_ACCESS_KEY, secretAccessKey: process.env.S3_SECRET_KEY }, forcePathStyle: true })`.
   - **meilisearch.provider.ts** — `new Meilisearch({ host: 'http://' + process.env.SEARCH_HOST + ':' + process.env.SEARCH_PORT, apiKey: process.env.SEARCH_MASTER_KEY })`.

   Wire each as a NestJS provider module with an OnApplicationShutdown hook that closes the client (Principle 1).

9. **src/migrate.ts** — standalone TypeORM migration runner. Uses the same DataSource config as app.module.ts but as a const export. Creates the `items` table if not exists. Exits with non-zero on any error. Run via `node dist/migrate.js` in prod / `npx ts-node src/migrate.ts` in dev. **Use loud failure** — no broad try/catch that swallows errors.

10. **src/seed.ts** — seed 3-5 Items plus sync them to Meilisearch. Uses the Meilisearch client to `.index('items').addDocuments([...])` AND awaits `client.waitForTask(task.taskUid)`. No row-count guard. The idempotency guard is the `execOnce` key in zerops.yaml (we key the seed by a static string so it runs exactly once across the project lifetime — the main agent will wire this). Exit non-zero on failure. Seed keyword 'alpha' in at least one item title for the feature sub-agent's later search smoke test.

11. **package.json scripts** — ensure `build`, `start:prod`, `start:dev`, `migrate`, `seed` exist. Scripts:
    ```
    "build": "nest build"
    "start:prod": "node dist/main.js"
    "start:dev": "nest start --watch"
    "migrate": "node dist/migrate.js"
    "seed": "node dist/seed.js"
    "migrate:dev": "npx ts-node src/migrate.ts"
    "seed:dev": "npx ts-node src/seed.ts"
    ```

12. **.gitignore** — ensure `node_modules`, `dist`, `.env` are ignored (`nest new` may already do this — verify).

13. **.env.example** — list every env var the app reads with placeholder values and a comment saying "DO NOT COPY TO .env ON ZEROPS — platform injects all vars as OS env".

**DO NOT WRITE (any of these fails the scaffold check):**
- `README.md` — delete if `nest new` emits one.
- `zerops.yaml` — the main agent writes it after smoke test.
- `.git/` — `nest new` runs `git init`; delete via `ssh apidev "rm -rf /var/www/.git"` immediately after scaffold.
- Any item CRUD route, cache-demo route, search route, jobs dispatch route, storage upload route, or corresponding frontend code.
- CORS config beyond `origin: true, credentials: true`.
- Any `.env` file with values.

**Env var names the app reads (MUST match these exactly — the main agent wires them in zerops.yaml `run.envVariables` from cross-service refs):**

| Purpose | Env var name | Source reference (for the main agent's zerops.yaml) |
|---------|--------------|------------------------------------------------------|
| Postgres host | `DB_HOST` | `${db_hostname}` |
| Postgres port | `DB_PORT` | `${db_port}` |
| Postgres user | `DB_USER` | `${db_user}` |
| Postgres password | `DB_PASS` | `${db_password}` |
| Postgres db | `DB_NAME` | `${db_dbName}` |
| Valkey host | `REDIS_HOST` | `${redis_hostname}` |
| Valkey port | `REDIS_PORT` | `${redis_port}` |
| NATS host | `NATS_HOST` | `${queue_hostname}` |
| NATS port | `NATS_PORT` | `${queue_port}` |
| NATS user | `NATS_USER` | `${queue_user}` |
| NATS pass | `NATS_PASS` | `${queue_password}` |
| S3 endpoint | `S3_ENDPOINT` | `${storage_apiUrl}` |
| S3 access key | `S3_ACCESS_KEY` | `${storage_accessKeyId}` |
| S3 secret | `S3_SECRET_KEY` | `${storage_secretAccessKey}` |
| S3 bucket | `S3_BUCKET` | `${storage_bucketName}` |
| Meilisearch host | `SEARCH_HOST` | `${search_hostname}` |
| Meilisearch port | `SEARCH_PORT` | `${search_port}` |
| Meilisearch master key | `SEARCH_MASTER_KEY` | `${search_masterKey}` |
| App secret (sessions/JWT) | `APP_SECRET` | project-level, already set |
| NODE_ENV / PORT | set by zerops.yaml | — |

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

The Zerops platform imposes invariants that every service must honor. Framework scaffolders do not automatically satisfy them. Before pre-ship assertions run, walk this list:

**Principle 1 — Graceful shutdown within thirty seconds**
Applies to: any long-running service. Rolling deploys send SIGTERM, wait up to 30s, then SIGKILL. Stop accepting new work, await in-flight, close long-lived connections, exit.

**Principle 2 — Routable network binding**
Applies to: HTTP/WebSocket/gRPC servers. Bind to all interfaces (0.0.0.0) — never loopback. L7 balancer routes to pod IP.

**Principle 3 — Client-origin awareness behind a proxy**
Applies to: HTTP servers. Configure framework proxy-trust for exactly one upstream hop.

**Principle 4 — Competing-consumer semantics at replica count two or more**
Applies to: subscription workers running with minContainers ≥ 2. Use the broker's competing-consumer mechanism (NATS queue group, Kafka consumer group, SQS visibility timeout).

**Principle 5 — Structured credential passing**
Applies to: any client with generated credentials. Pass user/pass as structured options, not embedded in scheme-user-pass-host URL.

**Principle 6 — Stripped build-output root for static deploys**
Applies to: static/SPA deploys whose build output lives in a subdirectory. Use tilde suffix in zerops.yaml deployFiles. (N/A for API codebase — informational only.)

<<<END MANDATORY>>>

Only principles 1, 2, 3, 5 apply to this API codebase. Verify each has a corresponding implementation in the scaffolded code. Record a fact (type=fix_applied, scope=both) for each principle noting the idiom used — e.g. "Principle 1 satisfied via app.enableShutdownHooks() in src/main.ts".

### Pre-ship self-verification script (run before returning)

```bash
HOST=apidev
MOUNT=/var/www/$HOST
FAIL=0

# Assertion 2 — app.listen binds 0.0.0.0
if grep -rq "app\.listen" $MOUNT/src/ 2>/dev/null; then
    if ! grep -rq "0\.0\.0\.0" $MOUNT/src/; then
        echo "FAIL: app.listen without 0.0.0.0 binding"
        FAIL=1
    fi
fi

# Assertion 3 — trust proxy
if ! grep -rq "trust proxy\|trustProxy" $MOUNT/src/; then
    echo "FAIL: missing trust proxy"
    FAIL=1
fi

# Assertion 4 — S3Client forcePathStyle
if grep -rq "S3Client(" $MOUNT/src/; then
    if ! grep -rq "forcePathStyle:\s*true" $MOUNT/src/; then
        echo "FAIL: S3Client without forcePathStyle: true"
        FAIL=1
    fi
fi

# Assertion 5 — NATS no URL-embedded creds
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: URL-embedded NATS creds"
    FAIL=1
fi

# Assertion 6 — Redis/Valkey no :password@
if grep -rnE "redis://[^@]*:[^@]*@|valkey://[^@]*:[^@]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: Valkey connection with :password@"
    FAIL=1
fi

# Assertion 8 — .gitignore covers node_modules
if [ ! -f $MOUNT/.gitignore ] || ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore; then
    echo "FAIL: .gitignore missing node_modules"
    FAIL=1
fi

# Assertion 9 — no populated .env
if [ -f $MOUNT/.env ] && [ ! -f $MOUNT/.env.example ]; then
    echo "FAIL: .env without .env.example"
    FAIL=1
fi

# Assertion 10 — no residual .git/
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
    echo "FAIL: residual .git on $HOST — rm -rf /var/www/.git"
    FAIL=1
fi

# No README at generate-complete
if [ -f $MOUNT/README.md ]; then
    echo "FAIL: README.md must not exist at scaffold-complete"
    FAIL=1
fi

# No zerops.yaml at generate-complete
if [ -f $MOUNT/zerops.yaml ]; then
    echo "FAIL: zerops.yaml must not exist at scaffold-complete"
    FAIL=1
fi

exit $FAIL
```

Run this inline via `bash -c '...'` from zcp. Do NOT persist it as a file inside the codebase. If any assertion fails, fix it in the code and re-run the full script. Return only when exit code is 0.

**Reporting back:** bulleted list of files you wrote + env var names you wired per managed service + the exit code of the pre-ship script (MUST be 0). Do not claim you implemented features. Short report under 400 words.
```
