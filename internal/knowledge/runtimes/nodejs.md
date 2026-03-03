# Node.js on Zerops

## Keywords
nodejs, node, npm, pnpm, yarn, express, nextjs, nuxt, nestjs, hono, fastify, zerops.yml, node_modules

## TL;DR
Node.js runtime with npm/pnpm/yarn pre-installed. MUST include `node_modules` in `deployFiles`. Bind `0.0.0.0`.

### Base Image

Includes Node.js, `npm`, `yarn`, `pnpm`, `git`, `npx`.

### Build Procedure

1. Set `build.base: nodejs@22` (or desired version)
2. `buildCommands` -- use ONE package manager:
   - `pnpm i` (preferred -- fastest, smallest disk, pre-installed)
   - `npm ci` (deterministic -- REQUIRES package-lock.json, fails without it)
   - `npm install` (flexible -- works without lockfile, creates/updates package-lock.json)
   - `yarn install` (requires yarn.lock)
3. `deployFiles`: MUST include `node_modules` (runtime doesn't run package install)
4. `run.start`: `node server.js` or framework start command

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `node index.js` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [pnpm i, pnpm build]`, `deployFiles` includes `node_modules` + build output

### Binding per Framework

- Express: `app.listen(port, "0.0.0.0")`
- Next.js: `next start -H 0.0.0.0`
- Fastify: `host: "0.0.0.0"`
- NestJS: `app.listen(port, "0.0.0.0")`
- Hono: `serve({hostname: "0.0.0.0"})`

### Framework Deploy Patterns

- Next.js SSR: deploy `.next`, `node_modules`, `package.json`, `next.config.js`, `public`
- Nuxt SSR: deploy `.output`, `node_modules`, `package.json`
- Cache: `node_modules`, `.next/cache`, `.pnpm-store`

### Common Mistakes

- Missing `node_modules` in `deployFiles` -> "Cannot find module" at runtime
- Not binding `0.0.0.0` -> 502 Bad Gateway
- Next.js missing `output: 'export'` for static -> produces SSR output instead
- Using `npm ci` without `package-lock.json` -> EUSAGE error ("can only install with existing lockfile")
