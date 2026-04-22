# Dispatch 3 — scaffold-worker-codebase

**Dispatched at**: `2026-04-22T18:26:18`
**Description**: Scaffold worker codebase
**Subagent type**: `general-purpose`
**Prompt length**: 9065 chars
**Tool-use id**: `toolu_01XsdRhqDzg9QppMZFSHmWpe`

---

## Transmitted prompt (verbatim)

```
You are scaffolding the WORKER codebase of the nestjs-showcase Zerops recipe (a multi-codebase showcase with separate frontend / NestJS API / worker repos).

## Codebase scope

- Hostname: **workerdev** (mount path `/var/www/workerdev/`)
- Stack: **NestJS 11 standalone application** on `nodejs@24`. Connects to NATS as a competing consumer.
- **No HTTP port. No public URL.** Workers are pure NATS subscribers.

The worker subscribes to NATS and processes background jobs. **For the scaffold phase you ship a no-op subscriber** — connects, subscribes, logs received messages, returns. The feature sub-agent later adds real job processing for the `jobs-dispatch` feature.

Managed services this worker uses:

| Service | Env var keys |
|---|---|
| db | `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME` |
| queue (nats) | `NATS_HOST`, `NATS_PORT`, `NATS_USER`, `NATS_PASS` |

## ⚠ CRITICAL: where commands run (read FIRST)

You are running on the **zcp orchestrator container**, not on the target dev container. The path `/var/www/workerdev/` on zcp is an **SSHFS network mount** — a write surface, not an execution surface.

- **File writes** via Write/Edit/Read against `/var/www/workerdev/` work correctly.
- **Executable commands** MUST run via SSH:
  ```
  ssh workerdev "cd /var/www && <command>"
  ```
  NEVER `cd /var/www/workerdev && <command>` from Bash.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

### Scaffold pre-flight — platform principles

- **Principle 1 — Graceful shutdown within 30s**: register SIGTERM/SIGINT handlers that drain in-flight work and close DB + NATS connections cleanly.
- **Principle 4 — Competing-consumer semantics at replica count ≥2**: every NATS subscription must use a queue group (the second arg to `connection.subscribe(subject, { queue: 'workers' }, ...)` or NATS JetStream durable consumer). Without it, every replica processes every message N times.
- **Principle 5 — Structured credential passing**: NATS connect via `connect({ servers, user, pass })` object. NEVER `nats://user:pass@host:port` URL — v2 client silently drops embedded credentials.

<<<END MANDATORY>>>

## What to write

1. **Scaffold NestJS at `/var/www/` on the workerdev container**:
   ```
   ssh workerdev "cd /var/www && npx -y @nestjs/cli new . --skip-git --skip-install --package-manager npm"
   ```
   Then `ssh workerdev "cd /var/www && npm install"` separately for visible logs.

2. **Trim the scaffolder's demo**: delete `src/app.controller.ts`, `src/app.service.ts`, `src/app.controller.spec.ts`. NestJS's HTTP scaffolding is unused — this is a standalone NATS consumer.

3. **Install runtime dependencies**:
   ```
   ssh workerdev "cd /var/www && npm install --save \
     @nestjs/typeorm typeorm pg \
     @nestjs/config \
     nats"
   ```

4. **`src/main.ts`** — standalone bootstrap:
   ```ts
   import { NestFactory } from '@nestjs/core';
   import { AppModule } from './app.module';
   import { WorkerService } from './worker/worker.service';

   async function bootstrap() {
     const app = await NestFactory.createApplicationContext(AppModule, {
       bufferLogs: false,
     });
     app.enableShutdownHooks();
     const worker = app.get(WorkerService);
     await worker.start();

     const shutdown = async (signal: string) => {
       console.log(`worker received ${signal}, draining…`);
       await worker.stop();
       await app.close();
       process.exit(0);
     };
     process.on('SIGTERM', () => shutdown('SIGTERM'));
     process.on('SIGINT', () => shutdown('SIGINT'));
   }
   bootstrap().catch((err) => { console.error(err); process.exit(1); });
   ```

5. **`src/app.module.ts`** — wire:
   - `ConfigModule.forRoot({ isGlobal: true })`
   - `TypeOrmModule.forRootAsync` reading DB_* env vars, `synchronize: false`, `autoLoadEntities: true`. (Worker shares the API's database. The feature sub-agent will define entities the worker writes to.)
   - `WorkerModule` (containing `WorkerService` + the NATS connection provider)

6. **`src/worker/nats.connection.ts`** — provider module:
   - Open `connect({ servers: [`${NATS_HOST}:${NATS_PORT}`], user: NATS_USER, pass: NATS_PASS, name: 'showcase-worker' })`
   - Export the `NatsConnection`
   - Implements `OnModuleDestroy` → `await drain()` (Principle 1)
   - **No URL-embedded credentials** (Principle 5)

7. **`src/worker/worker.service.ts`** — the no-op subscriber:
   - Injects the `NatsConnection`
   - `start()` subscribes to subject `jobs.>` with `{ queue: 'showcase-workers' }` (Principle 4 — competing consumer group)
   - For each message: log `subject=<sub> data=<JSON>` and continue. NO processing, NO DB writes, NO ack logic — the feature sub-agent fills this in for the jobs-dispatch feature.
   - `stop()` unsubscribes and waits for in-flight handlers to settle (with a small `Promise.allSettled` over an internal Set of pending promises so SIGTERM drains cleanly).

8. **`src/worker/worker.module.ts`** — declare the NATS provider + `WorkerService`. Re-export so `main.ts` can `app.get(WorkerService)`.

9. **`tsconfig.json`** — keep NestJS defaults. `outDir: "./dist"`, `target: "ES2022"`.

10. **`package.json` scripts** — keep `nest build`, `nest start --watch`. Add nothing else.

11. **`.env.example`** — list the env var names (DB_*, NATS_*).

12. **`.gitignore`** — Verify scaffolder one covers `node_modules`, `dist`.

13. **Delete `/var/www/.git/`** if scaffolder created one.

## What NOT to write

- **NO HTTP server code** — workers don't serve HTTP.
- **NO `zerops.yaml`** — main agent owns it.
- **NO `README.md`** — main agent writes after deploy.
- **NO entity definitions** — the worker uses entities defined in the API codebase. The feature sub-agent (which authors API + worker as a single unit) will add the entity here when it implements the jobs feature. Do NOT invent worker-only column sets.
- **NO real job processing logic** — only the no-op log-and-return subscriber.
- **NO `nats://user:pass@host:port` URLs**.

## Pre-ship self-verification (MANDATORY)

Save to `/tmp/preship-worker.sh` (NOT under `/var/www/workerdev/`) on zcp:

```bash
#!/bin/bash
HOST=workerdev
MOUNT=/var/www/$HOST
FAIL=0

# 1. .gitignore
[ -f $MOUNT/.gitignore ] || { echo "FAIL: .gitignore missing"; FAIL=1; }
grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore || { echo "FAIL: .gitignore missing node_modules"; FAIL=1; }

# 2. main.ts uses createApplicationContext (no HTTP)
grep -q "createApplicationContext" $MOUNT/src/main.ts || { echo "FAIL: main.ts not standalone (missing createApplicationContext)"; FAIL=1; }
grep -q "SIGTERM" $MOUNT/src/main.ts || { echo "FAIL: main.ts missing SIGTERM handler"; FAIL=1; }

# 3. No HTTP server
if grep -rn "NestFactory.create(\|app.listen" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: HTTP server code in worker - use createApplicationContext only"; FAIL=1
fi

# 4. NATS subscribe uses queue group
grep -rq "queue:" $MOUNT/src/worker/ || { echo "FAIL: NATS subscription missing queue group (Principle 4)"; FAIL=1; }

# 5. No URL-embedded NATS creds
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: URL-embedded NATS creds — Principle 5"; FAIL=1
fi

# 6. NO synchronize: true
if grep -rn "synchronize:\s*true" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: synchronize: true in worker"; FAIL=1
fi

# 7. No README, no zerops.yaml, no .git, no .env
[ ! -f $MOUNT/README.md ] || { echo "FAIL: README.md exists"; FAIL=1; }
[ ! -f $MOUNT/zerops.yaml ] || { echo "FAIL: zerops.yaml exists"; FAIL=1; }
[ ! -f $MOUNT/.env ] || { echo "FAIL: .env exists"; FAIL=1; }
ssh $HOST "test -d /var/www/.git" 2>/dev/null && { echo "FAIL: /var/www/.git exists - delete"; FAIL=1; }

# 8. Build compiles
ssh $HOST "cd /var/www && npm run build" >/tmp/preship-worker-build.log 2>&1 || {
  echo "FAIL: nest build failed - see /tmp/preship-worker-build.log"; FAIL=1
}

exit $FAIL
```

Run with `bash /tmp/preship-worker.sh`. Pre-ship script must NOT live in the codebase.

## Reporting back

Bulleted list of files, env vars wired, exit code (must be 0), any fixes. Do NOT claim feature implementation.
```
