# Dispatch 3 — scaffold-workerdev-nestjs-worker

**Dispatched at**: `2026-04-20T10:25:27`
**Description**: Scaffold workerdev NestJS worker
**Subagent type**: `general-purpose`
**Prompt length**: 8668 chars
**Tool-use id**: `toolu_014Xpk1tzsjRf9gfwRNDQpWx`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for the `nestjs-showcase` Zerops recipe. Scaffold the **worker codebase** (a separate NestJS process that subscribes to NATS) into the SSHFS mount `/var/www/workerdev/`. Your working directory is the zcp orchestrator container; `/var/www/workerdev/` is a network mount to the dev container `workerdev`.

**⚠ TOOL-USE POLICY — read before your first tool call.**

You are a sub-agent. Workflow state is main-agent-only.

**Permitted tools:** Read, Edit, Write, Grep, Glob against `/var/www/workerdev/`; Bash ONLY via `ssh workerdev "..."`; `mcp__zerops__zerops_dev_server`, `mcp__zerops__zerops_knowledge`, `mcp__zerops__zerops_logs`, `mcp__zerops__zerops_discover`.

**Forbidden:** `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify`. Calling any is a sub-agent-misuse bug.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

**⚠ CRITICAL: where commands run**

`/var/www/workerdev/` is an SSHFS mount. Every install/build MUST run via `ssh workerdev "cd /var/www && <command>"`. Do NOT `cd /var/www/workerdev && npm install` on zcp.

## Service plan context

This worker consumes from NATS (`queue` service) and writes to PostgreSQL (`db` service).
- `queue` — NATS 2.12, auth required. Env vars: `${queue_hostname}`, `${queue_port}`, `${queue_user}`, `${queue_password}`. Password may contain URL-reserved chars → pass `user`/`pass` as separate options, never URL-embed.
- `db` — PostgreSQL 18. Env vars: `${db_hostname}`, `${db_port}`, `${db_user}`, `${db_password}`, `${db_dbName}`.

The worker SHARES the `job` database table with the API (managed by the API's migrate script — the worker reads/writes rows but does NOT own schema).

Project-level env vars: `APP_SECRET`, and others — most irrelevant here.

## What to scaffold

A **minimal NATS-subscribing NestJS standalone application**. You write infrastructure. You do NOT write real job handlers — the feature sub-agent adds them at deploy. Your worker subscribes to one subject, logs the message, and returns. That's the entire scaffold.

### Step 1 — Scaffold Nest

```
ssh workerdev "cd /var/www && npx -y @nestjs/cli new . --skip-git --skip-install --package-manager npm"
```

Read the files it emitted.

### Step 2 — Install deps

```
ssh workerdev "cd /var/www && npm install --save @nestjs/typeorm typeorm pg @nestjs/config nats nestjs-pino pino-http pino-pretty"
```

### Step 3 — Write files

**`src/main.ts`** — standalone application (NO HTTP listener). Use:
```ts
import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { Logger } from 'nestjs-pino';
import { AppModule } from './app.module';
import { WorkerService } from './worker.service';

async function bootstrap() {
  const app = await NestFactory.createApplicationContext(AppModule, { bufferLogs: true });
  app.useLogger(app.get(Logger));
  app.enableShutdownHooks();
  const worker = app.get(WorkerService);
  await worker.start();
  const logger = app.get(Logger);
  logger.log('worker: subscribed and running');
}

bootstrap().catch((e) => { console.error('worker bootstrap failed', e); process.exit(1); });
```

**`src/app.module.ts`** — compose:
- `ConfigModule.forRoot({ isGlobal: true })`.
- `LoggerModule.forRoot({ pinoHttp: { transport: process.env.NODE_ENV !== 'production' ? { target: 'pino-pretty' } : undefined } })`.
- `TypeOrmModule.forRoot` reading `DB_*` env vars, `synchronize: false`, `entities: [Job]`.
- `TypeOrmModule.forFeature([Job])`.
- Providers: `WorkerService`, `NATS` (async provider using `connect({ servers: [`${host}:${port}`], user, pass })`).

**`src/entities/job.entity.ts`** — copy the shape from the API codebase (same table). Id (uuid), payload (jsonb), status (varchar 20), result (text nullable), createdAt, processedAt. Since TypeORM won't `synchronize`, schema alignment is contract-based, not enforced.

**`src/worker.service.ts`** — the subscription. Structure:
```ts
import { Injectable, Inject, OnModuleDestroy } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Logger } from 'nestjs-pino';
import { Repository } from 'typeorm';
import type { NatsConnection, Subscription } from 'nats';
import { JSONCodec } from 'nats';
import { Job } from './entities/job.entity';

@Injectable()
export class WorkerService implements OnModuleDestroy {
  private sub?: Subscription;
  private codec = JSONCodec();
  constructor(
    @Inject('NATS') private readonly nats: NatsConnection,
    @InjectRepository(Job) private readonly jobs: Repository<Job>,
    private readonly logger: Logger,
  ) {}

  async start() {
    // queue group "workers" = competing-consumer semantics (Principle 4).
    // Multiple worker replicas → each message handled by exactly one replica.
    this.sub = this.nats.subscribe('jobs.scaffold', { queue: 'workers' });
    this.processLoop();
  }

  private async processLoop() {
    if (!this.sub) return;
    for await (const msg of this.sub) {
      try {
        const payload = this.codec.decode(msg.data) as { id?: string };
        this.logger.log({ payload }, 'worker: received scaffold ping');
        // The feature sub-agent replaces this with real job processing.
      } catch (err) {
        this.logger.error({ err }, 'worker: message handler error');
      }
    }
  }

  async onModuleDestroy() {
    await this.sub?.drain();
    await this.nats.drain();
  }
}
```

Principle 4 — use NATS queue group. Record a fact.
Principle 1 — `onModuleDestroy` drains subscription + connection. Record a fact.
Principle 5 — NATS connect uses separate user/pass options. Record a fact.

**`.gitignore`**, **`.env.example`** — as in standard Node projects.

**DO NOT WRITE:**
- `README.md` — main agent at deploy-readmes.
- `zerops.yaml` — main agent after scaffold returns.
- `.git/` — delete after scaffolder.
- Real job handlers (items-sync, mail-dispatch, search-reindex, etc.) — feature sub-agent.

## Pre-ship self-verification (MANDATORY)

```bash
HOST=workerdev
MOUNT=/var/www/$HOST
FAIL=0

# Assertion 1 — no URL-embedded NATS creds
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: URL-embedded NATS creds"
    FAIL=1
fi

# Assertion 2 — subscribe uses queue group
if ! grep -rq "queue:" $MOUNT/src/worker.service.ts 2>/dev/null; then
    echo "FAIL: NATS subscribe missing queue group (Principle 4)"
    FAIL=1
fi

# Assertion 3 — shutdown drains
if ! grep -q "drain" $MOUNT/src/worker.service.ts; then
    echo "FAIL: no drain() in worker.service — violates graceful shutdown"
    FAIL=1
fi

# Assertion 4 — no README.md
if [ -f $MOUNT/README.md ]; then
    echo "FAIL: README.md present"
    FAIL=1
fi

# Assertion 5 — no zerops.yaml
if [ -f $MOUNT/zerops.yaml ]; then
    echo "FAIL: zerops.yaml present"
    FAIL=1
fi

# Assertion 6 — no .git on container
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
    echo "FAIL: /var/www/.git exists on workerdev"
    FAIL=1
fi

# Assertion 7 — .gitignore and .env.example
if ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore 2>/dev/null; then
    echo "FAIL: .gitignore missing node_modules"
    FAIL=1
fi
if [ ! -f $MOUNT/.env.example ]; then
    echo "FAIL: .env.example missing"
    FAIL=1
fi
if [ -f $MOUNT/.env ]; then
    echo "FAIL: .env file present"
    FAIL=1
fi

exit $FAIL
```

## Build verification

```
ssh workerdev "cd /var/www && npm run build 2>&1 | tail -40"
```

Fix TS errors if any. Do NOT start the worker — main agent handles smoke testing.

## Reporting back

Bulleted files written, pre-ship exit code, build result, record_fact calls. Do NOT claim any features implemented — your worker only logs scaffold pings.
```
