# Dispatch 2 — scaffold-svelte-vite-frontend

**Dispatched at**: `2026-04-22T09:08:23`
**Description**: Scaffold Svelte+Vite frontend
**Subagent type**: `general-purpose`
**Prompt length**: 9345 chars
**Tool-use id**: `toolu_016wTARzYFbBVjP4VPwdXaEb`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for the Zerops recipe `nestjs-showcase`. Scaffold the **frontend codebase** only. Target: hostname `appdev`, mount `/var/www/appdev/`.

Target framework: **Svelte 5 + Vite + TypeScript**. Dev server port **5173**. Prod is a static build served by Nginx on Zerops. This is the lightweight SPA dashboard that consumes the NestJS API (apidev/apistage) — API-first showcase pattern.

**Verify every import and symbol against the installed package**, not memory. Before committing an `import`, open `node_modules/<pkg>/package.json`. When in doubt, run `npm create vite@latest` in a scratch directory and copy its shapes.

You are scaffolding a health-dashboard-only skeleton. **You write infrastructure. You do NOT write features.** The dashboard shows five colored dots, one per managed service (db/redis/nats/storage/search). That is the entire UI.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

**⚠ CRITICAL: where commands run.** `/var/www/appdev/` on zcp is an SSHFS mount — write surface only. Every `npm install`, `npm run build`, `vite`, `tsc`, `svelte-check`, `git` command MUST run via `ssh appdev "cd /var/www && <cmd>"`. Files via Write/Edit directly on the mount.

**⚠ Framework scaffolders auto-init git.** `npm create vite` may or may not. After the scaffolder returns, run `ssh appdev "rm -rf /var/www/.git"` unconditionally. The main agent's later `git init` needs a clean slate.

**WRITE — the frontend codebase:**

1. Scaffold via SSH: `ssh appdev "cd /var/www && npm create vite@latest . -- --template svelte-ts"`. If it prompts about non-empty directory, pass `--yes` or answer the prompt via heredoc. Verify src/ and package.json exist after.

2. `ssh appdev "cd /var/www && rm -rf /var/www/.git"` immediately after scaffold.

3. Install dependencies (via SSH): only what the scaffold template already adds. No extra libs.

4. **vite.config.ts** — Vite config MUST include an `allowedHosts` entry that accepts the Zerops dev subdomain wildcard `.zerops.app`. Write:
   ```ts
   import { defineConfig } from 'vite';
   import { svelte } from '@sveltejs/vite-plugin-svelte';

   export default defineConfig({
     plugins: [svelte()],
     server: {
       host: '0.0.0.0',
       port: 5173,
       allowedHosts: ['.zerops.app'],
     },
     preview: {
       host: '0.0.0.0',
       port: 5173,
       allowedHosts: ['.zerops.app'],
     },
   });
   ```
   Without `allowedHosts: ['.zerops.app']` the Vite dev server returns "Blocked request. This host is not allowed" for the public dev subdomain.

5. **src/lib/api.ts** — the mandatory single API helper that every component uses:
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
   ```
   Components MUST call `api('/api/status')` — never `fetch('/api/status')` directly. The content-type check catches nginx SPA fallback returning HTML.

6. **src/App.svelte** — renders `<StatusPanel />` and nothing else. Wraps it in `<main data-feature="status">`. No routing, no layout with empty slots, no tabs, no nav.

7. **src/lib/StatusPanel.svelte** — polls `/api/status` every 5000ms via the api helper. Renders:
   - Title "Service Health"
   - Three explicit render states:
     - Loading: `<p data-loading>Checking…</p>` while the initial request is in-flight.
     - Error: `<p data-error>Error: {err.message}</p>` visible in red if fetch throws.
     - Populated: a list of service rows. Each row `<li data-service="{name}"><span data-dot class="dot {state}"></span>{name}: {state}</li>`. The dot class gets `ok`/`error` based on the value, CSS shows green/red.
   - Services to render (in order): `db`, `redis`, `nats`, `storage`, `search`.

8. **src/app.css** (or inline `<style>` in StatusPanel.svelte) — minimal styling: dot as 12px circle, green background for `.ok`, red for `.error`, `data-error` text in red. No elaborate design system.

9. **.env.example** — list `VITE_API_URL=` with comment "DO NOT COPY TO .env ON ZEROPS".

10. **.gitignore** — ensure `node_modules`, `dist`, `.env` are ignored (Vite template usually does this).

11. **index.html** — Vite template default is fine. Page title "NestJS Showcase — Status".

**DO NOT WRITE:**
- `README.md` — delete if the template emits one.
- `zerops.yaml` — main agent writes it after smoke test.
- `.git/` — delete before returning.
- CORS config, Vite proxy rules, types.ts shared across codebases.
- Item list component, cache demo component, upload form, search box, jobs panel — ALL feature UI belongs to the feature sub-agent at deploy step 4b. The scaffold dashboard is five green dots, nothing else.
- Routing (no svelte-kit, no page router). This is a single-page SPA with one component.
- A populated `.env` file with any values.

**Env var consumed by this codebase:** only `VITE_API_URL` (from the project-level `STAGE_API_URL` / `DEV_API_URL`, wired in zerops.yaml by the main agent). The api.ts helper reads `import.meta.env.VITE_API_URL`.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

**Principle 1** (graceful shutdown) — N/A for a static-served SPA. The Vite dev server handles SIGTERM itself; prod is Nginx.
**Principle 2** (0.0.0.0 binding) — APPLIES to the Vite dev server. `server.host: '0.0.0.0'` in vite.config.ts.
**Principle 3** (proxy trust) — N/A (no custom HTTP server in prod; Nginx handles it).
**Principle 4** (competing consumer) — N/A.
**Principle 5** (structured creds) — N/A.
**Principle 6** — Stripped build-output root — APPLIES at zerops.yaml writing (main agent handles). The build output is `./dist/`.

<<<END MANDATORY>>>

Record a fact (type=fix_applied, scope=both) for Principles 2 and 6 noting the idiom used.

### Pre-ship self-verification

```bash
HOST=appdev
MOUNT=/var/www/$HOST
FAIL=0

# Vite dev server binds 0.0.0.0
if ! grep -q "host:\s*'0\.0\.0\.0'" $MOUNT/vite.config.ts; then
    echo "FAIL: vite.config.ts server.host not '0.0.0.0'"
    FAIL=1
fi

# allowedHosts includes .zerops.app
if ! grep -q "\.zerops\.app" $MOUNT/vite.config.ts; then
    echo "FAIL: vite.config.ts missing .zerops.app in allowedHosts"
    FAIL=1
fi

# api.ts helper exists and reads VITE_API_URL
if [ ! -f $MOUNT/src/lib/api.ts ]; then
    echo "FAIL: src/lib/api.ts missing"
    FAIL=1
elif ! grep -q "VITE_API_URL" $MOUNT/src/lib/api.ts; then
    echo "FAIL: src/lib/api.ts not reading import.meta.env.VITE_API_URL"
    FAIL=1
fi

# StatusPanel uses api helper, not direct fetch
if grep -rq "fetch('/api" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: component calling fetch('/api/...') directly — must use api() helper"
    FAIL=1
fi

# data-feature="status" attribute present
if ! grep -rq 'data-feature="status"' $MOUNT/src/; then
    echo "FAIL: data-feature=\"status\" wrapper missing"
    FAIL=1
fi

# .gitignore covers node_modules
if [ ! -f $MOUNT/.gitignore ] || ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore; then
    echo "FAIL: .gitignore missing node_modules"
    FAIL=1
fi

# no populated .env
if [ -f $MOUNT/.env ] && [ ! -f $MOUNT/.env.example ]; then
    echo "FAIL: .env without .env.example"
    FAIL=1
fi

# no residual .git/
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
    echo "FAIL: residual .git on $HOST"
    FAIL=1
fi

# no README
if [ -f $MOUNT/README.md ]; then
    echo "FAIL: README.md must not exist at scaffold-complete"
    FAIL=1
fi

# no zerops.yaml
if [ -f $MOUNT/zerops.yaml ]; then
    echo "FAIL: zerops.yaml must not exist at scaffold-complete"
    FAIL=1
fi

exit $FAIL
```

Run via `bash -c '...'`. Do NOT persist in the codebase. Fix all failures in code, re-run, return only when exit 0.

**Reporting back:** list files written, env var wired (`VITE_API_URL`), exit code of pre-ship script (must be 0). Under 300 words.
```
