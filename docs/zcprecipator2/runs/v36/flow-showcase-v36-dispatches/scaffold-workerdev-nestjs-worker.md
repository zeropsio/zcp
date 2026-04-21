# Dispatch 3 — scaffold-workerdev-nestjs-worker

**Dispatched at**: `2026-04-21T14:22:29`
**Description**: Scaffold workerdev (NestJS worker)
**Subagent type**: `general-purpose`
**Prompt length**: 7694 chars
**Tool-use id**: `toolu_01KnYkwSrodH5SAswJx5stFG`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent in the Zerops recipe workflow. Your codebase is **workerdev** — a standalone NestJS background worker consuming NATS messages. Mount: `/var/www/workerdev/`. SSH hostname: `workerdev`.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Platform principles for workers

**Principle 1 — Graceful shutdown within 30 seconds.** On SIGTERM, stop pulling new messages, let in-flight handlers complete, close NATS + DB connections, exit cleanly. NestJS standalone app: `app.enableShutdownHooks()` + `onModuleDestroy` on each service.

**Principle 4 — Competing-consumer semantics at replica count ≥ 2.** If the worker runs with `minContainers ≥ 2`, each replica subscribes independently — without a queue group, every message is processed N times. NATS idiom: subscribe with a `queue` parameter (queue group) so the broker load-balances one message to exactly one subscriber across replicas. The worker here MUST use a queue group even at replica count 1, so scaling up later does not silently duplicate work.

**Principle 5 — Structured credential passing.** Pass NATS user/password via `{ servers, user, pass }` options. NEVER `nats://user:pass@host` — the v2 client strips credentials silently.

<<<END MANDATORY>>>

## Your job

Build a **minimal NATS subscriber scaffold**:
- Connects to NATS via structured credentials (user/pass options, NOT URL-embedded)
- Subscribes to ONE subject (`jobs.process`) with a queue group `workers`
- Logs the received message and returns. **No real processing, no DB writes, no result tables.**
- The feature sub-agent at deploy step 4b will replace this no-op with a real job handler when it authors the `jobs-dispatch` feature.

## Env var contract

- `queue_hostname`, `queue_port`, `queue_user`, `queue_password` — NATS broker
- `db_hostname`, `db_port`, `db_user`, `db_password`, `db_dbName` — optional; worker can connect to DB later, not required for the scaffold's no-op

## Steps

1. **Scaffold NestJS (standalone)** via `ssh workerdev "cd /var/www && npx --yes @nestjs/cli@latest new . --package-manager npm --skip-git"`. Delete leftover `.git` if created: `ssh workerdev "rm -rf /var/www/.git"`.

2. **Install deps**: `ssh workerdev "cd /var/www && npm install nats && npm install -D ts-node @types/node"`. Remove the HTTP-only packages that the scaffold adds if you want to keep the footprint lean — but leaving `@nestjs/platform-express` installed is harmless (workers just don't use it).

3. **Read** (batch before Edit): `package.json`, `tsconfig.json`, `tsconfig.build.json`, `nest-cli.json`, `.gitignore`, `src/main.ts`, `src/app.module.ts`, `src/app.controller.ts`, `src/app.service.ts`.

4. **Rewrite `src/main.ts`** as a standalone NestJS application (no HTTP server):

```ts
import { NestFactory } from '@nestjs/core';
import { AppModule } from './app.module';
import { WorkerService } from './worker.service';

async function bootstrap() {
  const app = await NestFactory.createApplicationContext(AppModule);
  app.enableShutdownHooks();
  const worker = app.get(WorkerService);
  await worker.start();
  // keep process alive — shutdown hooks handle SIGTERM
}
bootstrap();
```

5. **Write `src/worker.service.ts`** — NATS connection + subscription with queue group:

```ts
import { Injectable, Logger, OnModuleDestroy } from '@nestjs/common';
import { connect, NatsConnection, StringCodec, Subscription } from 'nats';

@Injectable()
export class WorkerService implements OnModuleDestroy {
  private readonly log = new Logger(WorkerService.name);
  private nc?: NatsConnection;
  private sub?: Subscription;

  async start(): Promise<void> {
    this.nc = await connect({
      servers: [`nats://${process.env.queue_hostname}:${process.env.queue_port}`],
      user: process.env.queue_user,
      pass: process.env.queue_password,
      name: 'worker',
    });
    this.log.log(`connected to NATS ${this.nc.getServer()}`);
    const sc = StringCodec();
    this.sub = this.nc.subscribe('jobs.process', { queue: 'workers' });
    (async () => {
      for await (const m of this.sub!) {
        try {
          const payload = sc.decode(m.data);
          this.log.log(`received jobs.process ${payload}`);
          // scaffold: no real processing — feature sub-agent replaces this
        } catch (err) {
          this.log.error(`handler error: ${(err as Error).message}`);
        }
      }
    })();
  }

  async onModuleDestroy(): Promise<void> {
    this.log.log('shutting down worker');
    try { await this.sub?.drain(); } catch { /* ignore */ }
    try { await this.nc?.drain(); } catch { /* ignore */ }
  }
}
```

6. **Rewrite `src/app.module.ts`** — registers `WorkerService`:
```ts
import { Module } from '@nestjs/common';
import { WorkerService } from './worker.service';
@Module({ providers: [WorkerService] })
export class AppModule {}
```

7. **Delete `src/app.controller.ts` and `src/app.service.ts`** if the scaffolder created them — workers have no HTTP controllers. `ssh workerdev "rm -f /var/www/src/app.controller.ts /var/www/src/app.service.ts /var/www/src/app.controller.spec.ts"`.

8. **Write `.env.example`** documenting expected env vars.

9. **Ensure `.gitignore`** covers `node_modules`, `dist`, `.env`.

10. **Pre-ship assertions** (inline `bash -c`):

```
HOST=workerdev
MOUNT=/var/www/$HOST
FAIL=0
# No URL-embedded NATS creds
grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1 && { echo "FAIL: URL-embedded NATS creds"; FAIL=1; } || true
# Structured creds present
grep -rq "user:\s*process\.env\.queue_user" $MOUNT/src/ || { echo "FAIL: structured user credential missing"; FAIL=1; }
# Queue group
grep -rq "queue:\s*'workers'\|queue:\s*\"workers\"" $MOUNT/src/ || { echo "FAIL: queue group missing (Principle 4)"; FAIL=1; }
# Shutdown hooks
grep -q "enableShutdownHooks" $MOUNT/src/main.ts || { echo "FAIL: missing enableShutdownHooks"; FAIL=1; }
# .gitignore
[ -f $MOUNT/.gitignore ] && grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore || { echo "FAIL: .gitignore missing node_modules"; FAIL=1; }
# No README.md, no zerops.yaml
[ -f $MOUNT/README.md ] && { echo "FAIL: README.md must not exist"; FAIL=1; } || true
[ -f $MOUNT/zerops.yaml ] && { echo "FAIL: zerops.yaml must not exist"; FAIL=1; } || true
# No .git/
ssh $HOST "test -d /var/www/.git" 2>/dev/null && { echo "FAIL: /var/www/.git exists"; FAIL=1; } || true
exit $FAIL
```

Then compile-check: `ssh workerdev "cd /var/www && npx tsc --noEmit"` must exit 0.

11. **Do NOT write**: `zerops.yaml`, `README.md`, real job processing logic, DB writes, result tables.

## Return

Report files written, env vars wired, assertions exit 0, tsc exit 0. Under 120 words.
```
