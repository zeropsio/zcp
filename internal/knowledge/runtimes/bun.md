# Bun on Zerops

## Keywords
bun, bunx, hono, elysia, javascript, typescript, zerops.yml, bundled deploy

## TL;DR
Bun runtime with npm/yarn/git pre-installed. Recommended: bundle to `dist/` and deploy without node_modules. Bind `0.0.0.0` via `Bun.serve`.

### Base Image

Includes Bun, `npm`, `yarn`, `git`, `npx`.

### Build Procedure

1. Set `build.base: bun@latest`
2. `buildCommands`: `bun i`, then `bun run build` or `bun build --outdir dist --target bun`
3. **Bundled deploy** (recommended): `deployFiles: [dist, package.json]` (NO node_modules) + `start: bun dist/index.js`
4. **Source deploy**: `deployFiles: [src, package.json, node_modules]` + `start: bun run src/index.ts`
5. **CRITICAL**: do NOT use `deployFiles: dist/~` with `start: bun dist/index.js` -- tilde strips the `dist/` prefix, so the file lands at `/var/www/index.js`, not `/var/www/dist/index.js`

### Binding

`Bun.serve({hostname: "0.0.0.0"})` -- default localhost = 502
- Elysia: `hostname: "0.0.0.0"` in constructor
- Hono: `Bun.serve({fetch: app.fetch, hostname: "0.0.0.0"})`

### Key Settings

Use `bunx` instead of `npx`. Cache: `node_modules`.

### Resource Requirements

**Dev** (install on container): `minRam: 0.5` — `bun install` fast, lower peak than npm.
**Stage/Prod**: `minRam: 0.25` — Bun runtime lightweight.

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `bun run index.ts` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [bun i, bun build --outdir dist --target bun src/index.ts]`, `deployFiles: [dist, package.json]`, `start: bun dist/index.js`
