# Dispatch 3 — scaffold-workerdev-nestjs-standalone

**Dispatched at**: `2026-04-21T07:32:34`
**Description**: Scaffold workerdev (NestJS standalone)
**Subagent type**: `general-purpose`
**Prompt length**: 7548 chars
**Tool-use id**: `toolu_0115a1Yq64RMuYvWywWBpL6H`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for a Zerops recipe session. You scaffold the **NestJS worker codebase** on the dev container mounted at `/var/www/workerdev/` (SSH alias `workerdev`).

This is a SEPARATE codebase from the API — its own repo, its own `package.json`, its own dependencies. It shares NOTHING at runtime with apidev except entity shapes it will re-declare locally.

## Recipe plan (nestjs-showcase)

- Framework: NestJS standalone application context (no HTTP server) consuming NATS subject `jobs.dispatch` with queue group `nestjs-showcase-workers` (competing-consumer semantics — Principle 4)
- Runtime: `nodejs@22`
- Package manager: npm
- NO HTTP port (worker consumes messages, no HTTP surface)
- Database: postgresql@18 via TypeORM (writes job results back to `jobs` table)

## Managed services wired

- db (postgresql@18) — env vars `db_hostname`, `db_port`, `db_user`, `db_password`, `db_dbName`
- queue (nats@2.12) — env vars `queue_hostname`, `queue_port`, `queue_user`, `queue_password` (pass creds as structured options, NEVER URL-embedded)

## Dependencies (package.json)

- `@nestjs/common`, `@nestjs/core`, `@nestjs/microservices`, `@nestjs/typeorm`, `@nestjs/config`
- `typeorm`, `pg`, `reflect-metadata`, `rxjs`, `nats`
- Dev: `typescript`, `ts-node`, `@types/node`

## Files to write

**src/main.ts**:
```ts
import { NestFactory } from '@nestjs/core';
import { Transport, MicroserviceOptions } from '@nestjs/microservices';
import { WorkerModule } from './worker.module';

async function bootstrap() {
  const app = await NestFactory.createMicroservice<MicroserviceOptions>(WorkerModule, {
    transport: Transport.NATS,
    options: {
      servers: [`nats://${process.env.queue_hostname}:${process.env.queue_port}`],
      user: process.env.queue_user,
      pass: process.env.queue_password,
      queue: 'nestjs-showcase-workers', // Principle 4: competing-consumer
    },
  });
  app.enableShutdownHooks(); // Principle 1: graceful shutdown
  await app.listen();
  console.log('[worker] listening on NATS subject jobs.dispatch (queue group nestjs-showcase-workers)');
}
bootstrap().catch((e) => { console.error('[worker] bootstrap failed:', e); process.exit(1); });
```

**src/worker.module.ts**: `@Module({ imports: [ConfigModule.forRoot({isGlobal:true}), TypeOrmModule.forRoot({...postgres config...}), TypeOrmModule.forFeature([Job])], controllers: [JobsController] })`.

**src/entities/job.entity.ts**: same shape as API's — `{ id: uuid PK, payload: jsonb, status: string default 'queued', processedAt: timestamptz nullable, createdAt: timestamptz }`. Re-declared here independently (worker owns its own schema binding).

**src/jobs.controller.ts**:
```ts
import { Controller } from '@nestjs/common';
import { MessagePattern, Payload } from '@nestjs/microservices';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository } from 'typeorm';
import { Job } from './entities/job.entity';

@Controller()
export class JobsController {
  constructor(@InjectRepository(Job) private readonly repo: Repository<Job>) {}

  @MessagePattern('jobs.dispatch')
  async handle(@Payload() data: { id: string }) {
    const job = await this.repo.findOne({ where: { id: data.id } });
    if (!job) return { ok: false, reason: 'not found' };
    // Simulated work — feature sub-agent refines.
    await new Promise((r) => setTimeout(r, 500));
    job.status = 'done';
    job.processedAt = new Date();
    await this.repo.save(job);
    return { ok: true };
  }
}
```

**tsconfig.json**, **tsconfig.build.json**, **nest-cli.json** — typical NestJS monorepo-free config.

**.gitignore**: `dist/`, `node_modules/`, `.env`, `*.log`.

**.env.example**: document every env var the worker reads.

## What NOT to write

- No HTTP server, no controllers serving HTTP routes
- No cache/search/storage clients — worker doesn't use them at this stage
- No README.md — delete if nest-cli creates one
- No zerops.yaml — main agent writes it
- No .git/ — delete after scaffolder runs
- No .env file

## Workflow

1. `ssh workerdev "cd /var/www && npx @nestjs/cli new . --package-manager npm --skip-git --skip-install"` — force nest into /var/www. If it balks, write files manually.
2. `ssh workerdev "cd /var/www && npm install"`
3. Write source files via Write/Edit on the mount.
4. Verify @nestjs/microservices NATS transport API against `node_modules/@nestjs/microservices/package.json` + its d.ts — confirm `createMicroservice`, `Transport.NATS`, `MessagePattern`, `Payload` symbols exist in the installed version.
5. Delete `.git/` + README.md: `ssh workerdev "rm -rf /var/www/.git /var/www/README.md"`.

## Pre-ship self-verification

```bash
HOST=workerdev
MOUNT=/var/www/$HOST
FAIL=0

# No URL-embedded NATS creds
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: URL-embedded NATS creds"; FAIL=1
fi

# Queue group set (Principle 4)
if ! grep -rq "queue:" $MOUNT/src/main.ts; then
    echo "FAIL: no queue group for competing-consumer"; FAIL=1
fi

# enableShutdownHooks
if ! grep -rq "enableShutdownHooks\|SIGTERM\|onApplicationShutdown" $MOUNT/src/; then
    echo "FAIL: no graceful shutdown"; FAIL=1
fi

# No README
if [ -f $MOUNT/README.md ]; then echo "FAIL: README exists"; FAIL=1; fi

# No zerops.yaml
if [ -f $MOUNT/zerops.yaml ]; then echo "FAIL: zerops.yaml exists"; FAIL=1; fi

# .gitignore covers node_modules
if ! grep -qE "node_modules" $MOUNT/.gitignore; then echo "FAIL: .gitignore missing node_modules"; FAIL=1; fi

# No .git/
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then echo "FAIL: .git exists"; FAIL=1; fi

exit $FAIL
```

Fix any failure and re-run until exit 0.

## Return report

- Files written
- Pre-ship exit code (must be 0)
- NestJS + @nestjs/microservices versions installed
- Confirmation that NATS queue group is wired

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

**Principle 1 — Graceful shutdown within thirty seconds** — worker has in-flight messages; must finish or NACK them before exiting. Use `app.enableShutdownHooks()`.

**Principle 4 — Competing-consumer semantics at replica count two or more** — set the NATS queue group on the microservice options so multiple worker replicas share load instead of each processing every message. This is MANDATORY.

**Principle 5 — Structured credential passing** — pass NATS user/pass as structured options, never URL-embedded.

Principles 2, 3, 6 don't apply (no HTTP server, no static build).

<<<END MANDATORY>>>

```
