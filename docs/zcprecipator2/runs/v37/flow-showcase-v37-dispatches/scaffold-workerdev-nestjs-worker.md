# Dispatch 3 — scaffold-workerdev-nestjs-worker

**Dispatched at**: `2026-04-21T19:36:58`
**Description**: Scaffold workerdev NestJS worker
**Subagent type**: `general-purpose`
**Prompt length**: 6999 chars
**Tool-use id**: `toolu_01PntJ6xVyE9aFeBB6hg5SVF`

---

## Transmitted prompt (verbatim)

```
You are a scaffold sub-agent for a Zerops recipe. Your codebase is the WORKER — a NestJS microservice consuming NATS, mounted at `/var/www/workerdev/`. Host to SSH into: `workerdev`. Mount is SSHFS — writes go via Write/Edit, executables run via `ssh workerdev "cd /var/www && <cmd>"`.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

Principles:
**1 — Graceful shutdown within 30s** — on SIGTERM, stop consuming, drain in-flight, close connections, exit.
**2 — Routable network binding** (N/A — the worker has NO HTTP server; it only connects OUT to NATS/db).
**3 — Client-origin awareness** (N/A — no inbound HTTP).
**4 — Competing-consumer semantics** — use NATS queue group on every subscription so only ONE replica processes each message.
**5 — Structured credential passing** — NATS creds as `{ servers, user, pass }` object, never URL-embedded.
**6 — Stripped build-output root for static** (N/A — runtime deploy).

Record a fact per applicable principle.

<<<END MANDATORY>>>

## Your deliverable — standalone NestJS NATS-worker skeleton

This is a SEPARATE codebase from apidev — its own package.json, its own tsconfig, its own dist. Do NOT symlink or share files with apidev; the worker clones column definitions it needs (re-declare the `JobEntity` with the same shape the API writes — they coordinate via DB table name and NATS subject name, not shared source).

**Framework setup**:
Use `ssh workerdev "cd /var/www && npx -y @nestjs/cli@latest new . --package-manager npm --skip-git --strict"` to scaffold. `--skip-git` prevents the scaffolder creating `.git/`. Verify + delete any residual `.git/` after.

The scaffolder emits a typical HTTP Nest app. You will rewrite `src/main.ts` into a microservice bootstrap (no HTTP listener). Keep the scaffolder's config files (tsconfig, nest-cli.json, eslint, package.json dev deps).

**Production dependencies to install** (via `ssh workerdev "cd /var/www && npm install <pkg>"`):
- `@nestjs/microservices nats` — NATS transport
- `@nestjs/typeorm typeorm pg` — write back JobEntity.processedAt
- `@nestjs/common @nestjs/core reflect-metadata rxjs` — core Nest (usually already in scaffolder's package.json)

**Remove dependencies NOT used by a worker**: `@nestjs/platform-express` if the scaffolder added it, because this is a pure microservice (no HTTP). Check the generated `package.json` and prune.

**Rewrite `src/main.ts`**:
```
async function bootstrap() {
  const app = await NestFactory.createMicroservice<MicroserviceOptions>(
    AppModule,
    {
      transport: Transport.NATS,
      options: {
        servers: [`nats://${process.env.QUEUE_HOST}:${process.env.QUEUE_PORT}`],
        user: process.env.QUEUE_USER,
        pass: process.env.QUEUE_PASS,
        queue: 'jobs-worker', // queue group — Principle 4 competing-consumer
      },
    },
  );
  app.enableShutdownHooks(); // Principle 1 — graceful shutdown on SIGTERM
  await app.listen();
}
bootstrap();
```

**src/app.module.ts**:
- `TypeOrmModule.forRootAsync` — same env-var names as API (DB_HOST, DB_PORT, DB_USER, DB_PASS, DB_NAME). `synchronize: false`. `entities: [JobEntity]`.
- Register `JobsController`.

**src/entities/job.entity.ts** — re-declare with same column shape as the API's JobEntity (id uuid primary, payload jsonb, status varchar, processedAt timestamp nullable, createdAt timestamp default now). Table name MUST match: `jobs`.

**src/jobs/jobs.controller.ts** — SKELETON ONLY. No real work.
- Use `@Controller()` + `@MessagePattern('jobs.run')` decorator on a method. For the scaffold it just logs "received job" and returns. The feature sub-agent later rewrites this to actually update `processedAt`.

**DO NOT WRITE**:
- `README.md` — delete scaffolder's.
- `zerops.yaml` — main agent handles.
- `.git/` — delete if present.
- `.env` files.
- Any real job processing logic. Skeleton only.
- An HTTP controller. This is a microservice, no HTTP.

**Env vars your code must read**:
- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME`
- `QUEUE_HOST`, `QUEUE_PORT`, `QUEUE_USER`, `QUEUE_PASS`
- `NODE_ENV`

**Installation + smoke validation**:
After writing, run:
- `ssh workerdev "cd /var/www && npm install"`
- `ssh workerdev "cd /var/www && npm run build"` — must compile.

### Pre-ship self-verification

Run on zcp (HOST=workerdev) and report exit code:

```bash
HOST=workerdev
MOUNT=/var/www/$HOST
FAIL=0

# NATS creds NOT URL-embedded
if grep -rnE "'nats://[^']*:[^']*@|\"nats://[^\"]*:[^\"]*@" $MOUNT/src/ 2>/dev/null | head -1; then
  echo "FAIL: URL-embedded NATS creds — Principle 5"; FAIL=1
fi

# createMicroservice used
if ! grep -rq "createMicroservice" $MOUNT/src/; then
  echo "FAIL: worker does not use createMicroservice"; FAIL=1
fi

# queue group present for competing-consumer
if ! grep -rq "queue:" $MOUNT/src/; then
  echo "FAIL: NATS queue group not set — Principle 4"; FAIL=1
fi

# enableShutdownHooks
if ! grep -rq "enableShutdownHooks" $MOUNT/src/; then
  echo "FAIL: enableShutdownHooks missing — Principle 1"; FAIL=1
fi

# No HTTP listen
if grep -rq "app\.listen.*3000\|app\.listen.*PORT" $MOUNT/src/; then
  echo "FAIL: worker has HTTP listen — pure microservice only"; FAIL=1
fi

# .gitignore
if [ ! -f $MOUNT/.gitignore ] || ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore; then
  echo "FAIL: .gitignore missing or does not ignore node_modules"; FAIL=1
fi

# No residual .git/
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
  echo "FAIL: /var/www/.git on workerdev — delete"; FAIL=1
fi

# No README
if [ -f $MOUNT/README.md ]; then
  echo "FAIL: README.md exists — delete"; FAIL=1
fi

# No zerops.yaml
if [ -f $MOUNT/zerops.yaml ]; then
  echo "FAIL: zerops.yaml exists — main agent's job"; FAIL=1
fi

exit $FAIL
```

Must exit 0. Don't leave the script in the codebase.

### Reporting back

Return: files written, scaffolder outputs kept/modified/deleted, pre-ship exit code, principle translations applied. No feature implementation claims.
```
