# brief-scaffold-workerdev-showcase-composed.md

**Role**: scaffold sub-agent (worker codebase)
**Tier**: showcase
**Source atoms**:

```
briefs/scaffold/mandatory-core.md
briefs/scaffold/symbol-contract-consumption.md  (same SymbolContract JSON)
briefs/scaffold/framework-task.md
briefs/scaffold/worker-codebase-addendum.md
briefs/scaffold/pre-ship-assertions.md
briefs/scaffold/completion-shape.md
+ pointer-include principles/where-commands-run.md
+ pointer-include principles/file-op-sequencing.md
+ pointer-include principles/tool-use-policy.md
+ pointer-include principles/platform-principles/01..06.md (principles 1 + 4 + 5 are load-bearing for workers)
+ pointer-include principles/comment-style.md
+ pointer-include principles/visual-style.md
+ PriorDiscoveriesBlock(sessionID, substep=generate.scaffold)
```

Interpolations: `{{.Hostname}} = workerdev`, `{{.SymbolContract | toJSON}}` (same blob), `{{.Framework}} = NestJS standalone (no HTTP)`.

---

## Composed brief

```
You are a scaffolding sub-agent. Scaffold the worker codebase for hostname `workerdev` on the SSHFS mount `/var/www/workerdev/`.

--- [briefs/scaffold/mandatory-core.md] ---

(Same as apidev / appdev. Permitted/forbidden tools + file-op sequencing.)

--- [principles/where-commands-run.md] ---

(Same positive-form atom.)

--- [briefs/scaffold/symbol-contract-consumption.md, interpolated] ---

(Same SymbolContract JSON.)

Rules applicable to `worker` role (filter `appliesTo` ⊇ `worker` or `any`):

- `nats-separate-creds` — pass user/pass as separate options on `connect()`.
- `graceful-shutdown` — register SIGTERM handler; drain subscription; drain connection; exit.
- `queue-group` — every subscription declares `queue: '<contract.NATSQueues.workers>'` (= `"workers"`).
- `gitignore-baseline`, `env-example-preserved`, `no-scaffold-test-artifacts`, `skip-git`, `env-self-shadow` (main-agent substep, filtered at your role).

Relevant contract sections:

- `EnvVarsByKind.db` + `EnvVarsByKind.queue` — your worker consumes Postgres + NATS. No S3, no Meilisearch, no Redis, no SMTP.
- `NATSSubjects` — this scaffold subscribes `jobs.scaffold` only (a ping echo) at generate-time. Feature sub-agent adds `jobs.process`.
- `NATSQueues.workers` = `"workers"` — use this exactly as the queue group.
- `Hostnames[worker]` = `{role: "worker", dev: "workerdev", stage: "workerstage"}`.
- `DTOs` — `JobDTO` is shared with api; the worker reads/writes `Job` entity rows but does NOT own schema (api runs the migration).

--- [briefs/scaffold/framework-task.md] ---

(Same shape as api: scaffold via ssh, install deps, read files before Edit, then apply the addendum.)

Install command:

    ssh workerdev "cd /var/www && npx -y @nestjs/cli new . --skip-git --skip-install --package-manager npm"
    ssh workerdev "cd /var/www && npm install --save @nestjs/typeorm typeorm pg @nestjs/config nats nestjs-pino pino-http pino-pretty"

--- [briefs/scaffold/worker-codebase-addendum.md] ---

You scaffold a minimal NATS-subscribing NestJS standalone application. No HTTP listener. You write infrastructure only; real job handlers are feature-sub-agent territory.

Files you write:

- `src/main.ts` — standalone NestJS application (no HTTP listener):

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
        app.get(Logger).log('worker: subscribed and running');
      }

      bootstrap().catch((e) => { console.error('worker bootstrap failed', e); process.exit(1); });

- `src/app.module.ts` — ConfigModule.forRoot({ isGlobal: true }); LoggerModule (nestjs-pino, pino-pretty in non-prod); TypeOrmModule.forRoot reading DB_* from the contract, synchronize: false, entities: [Job]; TypeOrmModule.forFeature([Job]); providers: WorkerService + NATS async provider.
- `src/entities/job.entity.ts` — same shape as the api codebase's Job entity (id uuid pk, payload jsonb, status varchar(20), result text nullable, createdAt, processedAt). Schema alignment is contract-based (api owns migration).
- `src/worker.service.ts` — the NATS subscription:

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
          this.sub = this.nats.subscribe('jobs.scaffold', { queue: 'workers' });
          this.processLoop();
        }

        private async processLoop() {
          if (!this.sub) return;
          for await (const msg of this.sub) {
            try {
              const payload = this.codec.decode(msg.data) as { id?: string };
              this.logger.log({ payload }, 'worker: received scaffold ping');
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

  Platform principle 4 (competing consumer) satisfied by queue group. Principle 1 (graceful shutdown) satisfied by onModuleDestroy drain sequence. Principle 5 (structured credentials) satisfied by NATS async provider using `connect({ servers: [`${process.env.NATS_HOST}:${process.env.NATS_PORT}`], user: process.env.NATS_USER, pass: process.env.NATS_PASS })`.

  Record a fact (scope=both) per principle after implementation.

- `.gitignore` + `.env.example` — same baseline as other scaffold dispatches.

Files you do NOT write:
- README.md — main agent at deploy.readmes.
- zerops.yaml — main agent after return.
- .git/ — deleted post-scaffold (FixRule `skip-git`).
- Real job handlers — `jobs.process` subscription, items-sync, mail-dispatch, search-reindex — feature sub-agent.

--- [principles/platform-principles/01..06.md, pointer-included] ---

Load-bearing for worker role: principles 1 (graceful shutdown), 4 (competing consumer), 5 (structured credentials).

- Principle 1: SIGTERM → drain subscription → drain connection → exit. Implemented via `onModuleDestroy`. Record a fact.
- Principle 4: `this.nats.subscribe('jobs.scaffold', { queue: 'workers' })`. Without the queue group, `minContainers > 1` runs every message N times. Record a fact.
- Principle 5: NATS `connect({ servers, user, pass })` — NEVER `nats://user:pass@host`. Record a fact.

Principles 2 (routable bind), 3 (proxy trust), 6 (stripped build root) do NOT apply (no HTTP; no deploy-files authoring).

--- [principles/comment-style.md + principles/visual-style.md] ---

(Same atoms — ASCII-only.)

--- [briefs/scaffold/pre-ship-assertions.md] ---

FixRecurrenceRules with `appliesTo` ⊇ `worker` or `any`, aggregate exit 0.

Reminder snapshot:

    HOST=workerdev
    MOUNT=/var/www/$HOST
    ! grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/       || { echo FAIL: URL-embedded NATS creds; exit 1; }
    grep -q "queue:" $MOUNT/src/worker.service.ts                                 || { echo FAIL: NATS subscribe missing queue group; exit 1; }
    grep -q "drain" $MOUNT/src/worker.service.ts                                  || { echo FAIL: no drain() call; exit 1; }
    grep -q 'enableShutdownHooks' $MOUNT/src/main.ts                              || { echo FAIL: main.ts missing enableShutdownHooks; exit 1; }
    test ! -f $MOUNT/README.md                                                    || { echo FAIL: README.md present; exit 1; }
    test ! -f $MOUNT/zerops.yaml                                                  || { echo FAIL: zerops.yaml present; exit 1; }
    ! ssh $HOST 'test -d /var/www/.git'                                           || { echo FAIL: /var/www/.git present; exit 1; }
    test ! -f $MOUNT/.env                                                         || { echo FAIL: .env present; exit 1; }
    test -f $MOUNT/.env.example                                                   || { echo FAIL: .env.example missing; exit 1; }
    grep -qE '^(node_modules|/node_modules)' $MOUNT/.gitignore                    || { echo FAIL: .gitignore missing node_modules; exit 1; }
    exit 0

After assertions:

    ssh workerdev "cd /var/www && npm run build 2>&1 | tail -40"

Fix TypeScript errors. Do not start the worker — main owns smoke testing.

--- [briefs/scaffold/completion-shape.md] ---

(Same as api/frontend: bulleted files + byte counts, exit code, build tail, record_fact calls, env-var names consumed by this codebase.)

--- [PriorDiscoveriesBlock(sessionID, substep=generate.scaffold)] ---

(Empty at first dispatch. If the recipe re-dispatches due to a provisional failure, downstream-scope facts from prior dispatches land here — including facts emitted by api or appdev that scope=both or scope=downstream and are flagged as relevant to worker.)
```

**Composed byte-budget**: ~8 KB (v34 workerdev was 8668 chars; near parity because worker scaffold is small).
