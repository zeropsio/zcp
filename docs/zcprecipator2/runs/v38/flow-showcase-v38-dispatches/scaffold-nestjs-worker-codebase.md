# Dispatch 3 — scaffold-nestjs-worker-codebase

**Dispatched at**: `2026-04-22T09:09:03`
**Description**: Scaffold NestJS worker codebase
**Subagent type**: `general-purpose`
**Prompt length**: 8200 chars
**Tool-use id**: `toolu_01EK87XUjv1tZ3TXfae1PS3z`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for the Zerops recipe `nestjs-showcase`. Scaffold the **worker codebase** only. Target: hostname `workerdev`, mount `/var/www/workerdev/`.

Target framework: **NestJS standalone** on **nodejs@24**. Role: background NATS subscriber — no HTTP port. Separate codebase from the API (the 3-repo case). Consumes the `jobs.process` subject with queue group `job-workers` for competing-consumer semantics.

The worker logs received messages and returns ack. **No job processing, no DB writes, no result storage.** The feature sub-agent at deploy step 4b will extend this to process actual jobs.

**Verify every import against the installed package**, not memory. Before committing `import { ... } from '@nestjs/microservices'`, open `node_modules/@nestjs/microservices/package.json` and inspect its exports.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

**⚠ CRITICAL: where commands run.** `/var/www/workerdev/` is an SSHFS mount. All `npm install`, `nest build`, `tsc`, `git` commands via `ssh workerdev "cd /var/www && <cmd>"`.

**⚠ Framework scaffolders that auto-init git.** Run `ssh workerdev "rm -rf /var/www/.git"` after any scaffolder that might create it.

**WRITE — the worker codebase:**

1. Scaffold via SSH: `ssh workerdev "cd /var/www && nest new . --skip-git --package-manager npm"`. If `nest new` isn't available, first `npm i -g @nestjs/cli` on the container, then run nest new.

2. Install runtime deps:
   - `@nestjs/microservices` (NATS transport)
   - `nats` (the NATS client library — @nestjs/microservices pulls this as peer dep)
   - `@nestjs/typeorm typeorm pg` (shares DB schema with API — the Item entity is imported here but NO writes yet)
   - `@nestjs/config`
   - Keep scaffolder-installed devDeps (typescript, ts-node, @types/node, @nestjs/cli).

3. **src/main.ts** — bootstrap as a NestJS microservice hybrid:
   ```ts
   import { NestFactory } from '@nestjs/core';
   import { Transport, MicroserviceOptions } from '@nestjs/microservices';
   import { AppModule } from './app.module';

   async function bootstrap() {
     const app = await NestFactory.createMicroservice<MicroserviceOptions>(
       AppModule,
       {
         transport: Transport.NATS,
         options: {
           servers: [`nats://${process.env.NATS_HOST}:${process.env.NATS_PORT}`],
           user: process.env.NATS_USER,
           pass: process.env.NATS_PASS,
           queue: 'job-workers',
         },
       },
     );
     app.enableShutdownHooks();  // Principle 1
     await app.listen();
     console.log('[worker] listening on NATS queue group job-workers');
   }
   bootstrap().catch((e) => {
     console.error('[worker] bootstrap failed', e);
     process.exit(1);
   });
   ```
   Note: user/pass passed as STRUCTURED options, not embedded in URL (Principle 5). `queue: 'job-workers'` gives competing-consumer semantics (Principle 4).

4. **src/app.module.ts** — imports TypeOrmModule (same config as API, readonly usage), ConfigModule, JobsModule.

5. **src/item.entity.ts** — copy the same Item entity shape from the API codebase (id uuid, title varchar(200), description text, createdAt timestamptz). The feature sub-agent will make the worker write to this table in deploy step 4b.

6. **src/jobs/jobs.controller.ts** — one `@MessagePattern('jobs.process')` handler. Takes a payload, logs it with a timestamp, returns `{ ok: true, processedAt: new Date().toISOString() }`. NO DB writes, NO processing. Pattern:
   ```ts
   @Controller()
   export class JobsController {
     @MessagePattern('jobs.process')
     handle(@Payload() data: unknown) {
       console.log('[worker] received', JSON.stringify(data));
       return { ok: true, processedAt: new Date().toISOString() };
     }
   }
   ```

7. **package.json scripts:**
   ```
   "build": "nest build"
   "start:prod": "node dist/main.js"
   "start:dev": "nest start --watch"
   ```

8. **.gitignore** — ensure `node_modules`, `dist`, `.env` ignored.

9. **.env.example** — list env vars, comment "DO NOT COPY TO .env ON ZEROPS".

**DO NOT WRITE:**
- `README.md` — delete if nest new emits one.
- `zerops.yaml` — main agent writes it.
- `.git/` — delete before returning.
- Any actual job processing logic (result tables, DB upserts, side effects). The handler just logs + returns.
- HTTP routes. This is a pure NATS microservice — no app.listen(), no ports.
- A populated `.env`.

**Env vars this codebase reads (main agent wires in zerops.yaml):**

| Purpose | Env var | Source ref |
|---------|---------|-----------|
| Postgres host | `DB_HOST` | `${db_hostname}` |
| Postgres port | `DB_PORT` | `${db_port}` |
| Postgres user | `DB_USER` | `${db_user}` |
| Postgres password | `DB_PASS` | `${db_password}` |
| Postgres db | `DB_NAME` | `${db_dbName}` |
| NATS host | `NATS_HOST` | `${queue_hostname}` |
| NATS port | `NATS_PORT` | `${queue_port}` |
| NATS user | `NATS_USER` | `${queue_user}` |
| NATS pass | `NATS_PASS` | `${queue_password}` |
| App secret | `APP_SECRET` | project-level (optional — not used yet) |

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

**Principle 1** (graceful shutdown) — APPLIES. `app.enableShutdownHooks()` in src/main.ts.
**Principle 2** (0.0.0.0 binding) — N/A (no HTTP).
**Principle 3** (proxy trust) — N/A (no HTTP).
**Principle 4** (competing consumer) — APPLIES. `queue: 'job-workers'` in NATS transport options is the NATS equivalent of a queue group.
**Principle 5** (structured creds) — APPLIES. NATS user/pass passed as structured options, not URL-embedded.
**Principle 6** — N/A.

<<<END MANDATORY>>>

Record a fact (type=fix_applied, scope=both) for each applicable principle.

### Pre-ship self-verification

```bash
HOST=workerdev
MOUNT=/var/www/$HOST
FAIL=0

# Principle 1 — enableShutdownHooks
if ! grep -rq "enableShutdownHooks" $MOUNT/src/; then
    echo "FAIL: missing enableShutdownHooks"
    FAIL=1
fi

# Principle 4 — NATS queue group
if ! grep -rq "queue:\s*['\"]job-workers['\"]" $MOUNT/src/; then
    echo "FAIL: NATS queue group 'job-workers' missing — Principle 4"
    FAIL=1
fi

# Principle 5 — NATS structured creds, not URL-embedded
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: URL-embedded NATS creds"
    FAIL=1
fi

# No HTTP server in worker
if grep -rq "app\.listen(3" $MOUNT/src/; then
    echo "FAIL: worker should not start HTTP listener"
    FAIL=1
fi

# .gitignore node_modules
if [ ! -f $MOUNT/.gitignore ] || ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore; then
    echo "FAIL: .gitignore missing node_modules"
    FAIL=1
fi

# no populated .env
if [ -f $MOUNT/.env ] && [ ! -f $MOUNT/.env.example ]; then
    echo "FAIL: .env without .env.example"
    FAIL=1
fi

# no .git/
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
    echo "FAIL: residual .git"
    FAIL=1
fi

# no README / zerops.yaml
[ -f $MOUNT/README.md ] && { echo "FAIL: README.md exists"; FAIL=1; }
[ -f $MOUNT/zerops.yaml ] && { echo "FAIL: zerops.yaml exists"; FAIL=1; }

exit $FAIL
```

Run via `bash -c '...'`. Fix + re-run until exit 0.

**Reporting back:** list files written, env vars wired, exit code of pre-ship (must 0). Under 300 words.
```
