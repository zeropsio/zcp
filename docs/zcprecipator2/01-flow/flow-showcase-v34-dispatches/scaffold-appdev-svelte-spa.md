# Dispatch 1 — scaffold-appdev-svelte-spa

**Dispatched at**: `2026-04-20T10:23:14`
**Description**: Scaffold appdev Svelte SPA
**Subagent type**: `general-purpose`
**Prompt length**: 10459 chars
**Tool-use id**: `toolu_01GcTMseTyNNi9xvy6n7idzm`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for the `nestjs-showcase` Zerops recipe. Scaffold the **frontend codebase** (a Svelte SPA) into the SSHFS mount `/var/www/appdev/`. Your working directory is a zcp orchestrator container; `/var/www/appdev/` is a network mount to the dev container `appdev`.

**⚠ TOOL-USE POLICY — read before your first tool call.**

You are a sub-agent. Workflow state is main-agent-only.

**Permitted tools:** Read, Edit, Write, Grep, Glob against `/var/www/appdev/`; Bash ONLY via `ssh appdev "..."`; `mcp__zerops__zerops_dev_server`, `mcp__zerops__zerops_knowledge`, `mcp__zerops__zerops_logs`, `mcp__zerops__zerops_discover`.

**Forbidden:** `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`, `zerops_subdomain`, `zerops_mount`, `zerops_verify`. Calling any is a sub-agent-misuse bug.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

**⚠ CRITICAL: where commands run (read this FIRST, before writing any files)**

You are running on the **zcp orchestrator container**, not on the target dev container. The path `/var/www/appdev/` on zcp is an **SSHFS network mount** — a bridge into the target container's `/var/www/`. It is a write surface, not an execution surface.

**File writes** via Write/Edit/Read against `/var/www/appdev/` work correctly — you are editing the target container's filesystem through the mount. Do this for every source file, config, `package.json`, etc.

**Executable commands** MUST run via SSH into the target container:
```
ssh appdev "cd /var/www && <command>"
```
NOT `cd /var/www/appdev && <command>`.

**⚠ Framework scaffolders that auto-init git**: pass `--skip-git` if available, or `ssh appdev "rm -rf /var/www/.git"` after the scaffolder returns. The main agent's later `git init` requires a clean slate.

## What to scaffold

A **health-dashboard-only** Svelte + Vite SPA scaffold. You write infrastructure. You do NOT write feature code. A feature sub-agent runs later and implements every showcase feature (items-crud, cache-demo, storage-upload, search-items, jobs-dispatch, mail-send).

**Write these files on the mount (`/var/www/appdev/`):**

- `package.json` — `svelte`, `vite`, `@sveltejs/vite-plugin-svelte`, `typescript`, `svelte-check`, `tsconfig-to-swcconfig` NOT needed. Use Vite 7 + Svelte 5.
- `vite.config.ts` — import svelte plugin; `server.host: '0.0.0.0'`; `server.port: 5173`; `server.allowedHosts: ['.zerops.app']`; `server.strictPort: true`; preview config also sets `host: '0.0.0.0'`, `allowedHosts: ['.zerops.app']`. Include a dev proxy: `server.proxy: { '/api': { target: 'http://127.0.0.1:3000', changeOrigin: true } }` so local dev against a local NestJS works — but production relies on `VITE_API_URL` directly.
- `tsconfig.json` — svelte/vite defaults, target ES2022, moduleResolution bundler, allowImportingTsExtensions false, verbatimModuleSyntax true.
- `svelte.config.js` — `import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';` export default `{ preprocess: vitePreprocess() }`.
- `index.html` — standard Vite Svelte index pointing to `/src/main.ts`.
- `src/main.ts` — `mount(App, { target: document.getElementById('app')! })` (Svelte 5 API).
- `src/app.css` — a single modest stylesheet: CSS custom properties for theme, simple card/panel/button/input styles, `[data-feature]` container style, `[data-row]`, `[data-hit]`, `[data-file]`, `[data-result]`, `[data-status]`, `[data-processed-at]`, `[data-error]` selectors with baseline styling. The feature sub-agent will consume these classes. Include a dashboard grid layout (single column on mobile, 2 columns on wide screens).
- `src/lib/api.ts` — **exactly the shape below. Do not invent variants. Components NEVER call `fetch()` directly — always `api()`.**

```ts
const BASE = (import.meta.env.VITE_API_URL ?? '').replace(/\/$/, '');

export async function api(path: string, init?: RequestInit): Promise<Response> {
  const url = `${BASE}${path}`;
  const res = await fetch(url, init);
  if (!res.ok) {
    const body = await res.text().catch(() => '');
    throw new Error(`API ${res.status} ${res.statusText} ${path}: ${body.slice(0, 200)}`);
  }
  const ct = res.headers.get('content-type') ?? '';
  if (!ct.toLowerCase().includes('application/json')) {
    throw new Error(`API ${path} returned non-JSON content-type ${ct} — likely SPA fallback, check VITE_API_URL baking`);
  }
  return res;
}

export async function apiJson<T = unknown>(path: string, init?: RequestInit): Promise<T> {
  const res = await api(path, init);
  return (await res.json()) as T;
}
```

- `src/App.svelte` — **mounts `<StatusPanel />` and nothing else.** Outer `<main class="dashboard">` with a `<header>` containing the recipe title "NestJS Showcase" and a subtitle "Zerops full-stack recipe". Below that, `<StatusPanel />` mounted inside a section. No routing, no tabs, no nav, no other feature components. Include a comment explaining that the feature sub-agent mounts additional feature sections here later.
- `src/lib/StatusPanel.svelte` — reads `/api/status` every 5s with the `api()` helper, renders one row per managed service: `db`, `redis`, `nats`, `storage`, `search`. Three explicit render states: loading, error (visible red banner with `data-error`), populated. Outer wrapper: `<section class="panel" data-feature="status">`. Each row: `<div class="status-row" data-service="{name}"><span class="dot dot-{state}"></span><span>{name}</span></div>`. Use Svelte 5 runes (`$state`, `$effect`). Dot colors via CSS: `dot-ok` green, `dot-degraded` yellow, `dot-error` red, `dot-unknown` gray.
- `.env.example` — `VITE_API_URL=` with a comment explaining it's auto-baked from `${STAGE_API_URL}` by zerops.yaml in prod and `${DEV_API_URL}` in dev.
- `.gitignore` — ignore `node_modules`, `dist`, `.DS_Store`, `*.log`, `.env` (but not `.env.example`), `.svelte-kit`, `.vite`.

**DO NOT WRITE:**
- `README.md` — the main agent writes it at deploy-readmes.
- `zerops.yaml` — main agent writes it after your scaffold returns.
- `.git/` — delete any the scaffolder creates (`ssh appdev "rm -rf /var/www/.git"`).
- Any feature code beyond the StatusPanel.
- CORS config — the API handles CORS.
- A service client or adapter module.
- `src/lib/types.ts` shared cross-codebase — main agent reconciles after scaffolds return.

## Scaffold pre-flight — platform principles

The Zerops platform imposes invariants that every service must honor. For a Vite dev server the relevant principle is **Principle 2 — Routable network binding** (bind 0.0.0.0, not loopback). You satisfy this by setting `server.host: '0.0.0.0'` in `vite.config.ts`. Record that fact with `mcp__zerops__zerops_record_fact` after scaffold completes.

Principle 6 — stripped build-output root — is main-agent territory (zerops.yaml deployFiles with `~` suffix).

## Pre-ship self-verification (MANDATORY — do not return to the main agent until all assertions pass)

Before returning, run the script below and ensure it exits 0.

```bash
HOST=appdev
MOUNT=/var/www/$HOST
FAIL=0

# Assertion 1 — .gitignore exists and covers node_modules
if [ ! -f $MOUNT/.gitignore ]; then
    echo "FAIL: .gitignore missing"
    FAIL=1
elif ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore; then
    echo "FAIL: .gitignore does not ignore node_modules"
    FAIL=1
fi

# Assertion 2 — vite.config binds 0.0.0.0
if ! grep -q "0.0.0.0" $MOUNT/vite.config.ts; then
    echo "FAIL: vite.config.ts missing 0.0.0.0 bind"
    FAIL=1
fi

# Assertion 3 — vite.config allows .zerops.app
if ! grep -q "zerops.app" $MOUNT/vite.config.ts; then
    echo "FAIL: vite.config.ts missing allowedHosts for .zerops.app"
    FAIL=1
fi

# Assertion 4 — api.ts reads VITE_API_URL and enforces content-type
if ! grep -q "VITE_API_URL" $MOUNT/src/lib/api.ts; then
    echo "FAIL: src/lib/api.ts missing VITE_API_URL reader"
    FAIL=1
fi
if ! grep -q "application/json" $MOUNT/src/lib/api.ts; then
    echo "FAIL: src/lib/api.ts missing content-type assertion"
    FAIL=1
fi

# Assertion 5 — StatusPanel uses api() helper, not fetch()
if grep -q "fetch(" $MOUNT/src/lib/StatusPanel.svelte; then
    echo "FAIL: StatusPanel uses fetch() directly — must use api() helper"
    FAIL=1
fi

# Assertion 6 — no README.md
if [ -f $MOUNT/README.md ]; then
    echo "FAIL: README.md must not be written during generate"
    FAIL=1
fi

# Assertion 7 — no zerops.yaml
if [ -f $MOUNT/zerops.yaml ]; then
    echo "FAIL: zerops.yaml must not be written during generate"
    FAIL=1
fi

# Assertion 8 — no .git on container
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
    echo "FAIL: /var/www/.git exists on appdev — delete before returning"
    FAIL=1
fi

# Assertion 9 — .env.example exists and no .env file
if [ -f $MOUNT/.env ]; then
    echo "FAIL: .env exists — delete to avoid shadowing OS env vars"
    FAIL=1
fi
if [ ! -f $MOUNT/.env.example ]; then
    echo "FAIL: .env.example missing"
    FAIL=1
fi

exit $FAIL
```

Run it via `bash -c` from a scratch file in `/tmp` (NEVER inside `/var/www/appdev/`).

## Installation

After writing files, run `ssh appdev "cd /var/www && npm install"` to install dependencies. Then verify TypeScript compilation by running `ssh appdev "cd /var/www && npx vite build 2>&1 | tail -30"`. If the build succeeds, stop — you do NOT need to start a dev server.

## Reporting back

Return a bulleted list of files you wrote, the pre-ship script exit code (must be 0), the result of `npm install` and `vite build`, and any record_fact calls you made. Do not claim to have implemented any features.
```
