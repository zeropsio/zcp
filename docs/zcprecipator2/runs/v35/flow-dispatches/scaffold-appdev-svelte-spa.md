# Dispatch 2 — scaffold-appdev-svelte-spa

**Dispatched at**: `2026-04-21T07:31:59`
**Description**: Scaffold appdev (Svelte SPA)
**Subagent type**: `general-purpose`
**Prompt length**: 8340 chars
**Tool-use id**: `toolu_015gxhJXnSUTWo9TpVVzVTqP`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent for a Zerops recipe session. You scaffold the **Svelte SPA frontend codebase** on the dev container mounted at `/var/www/appdev/` (SSH alias `appdev`).

## Recipe plan (nestjs-showcase)

- Framework: Svelte 5 + Vite 7 (Svelte SPA, not SvelteKit — pure client-side)
- Runtime for dev: `nodejs@22`
- Runtime for prod: `static` (served by built-in Nginx)
- Package manager: npm
- Dev server port: 5173 (Vite default)
- Prod port: 80 (static, omitted from subdomain URL)

## What to do

1. SSH into appdev: `ssh appdev "cd /var/www && npm create vite@latest . -- --template svelte-ts"` (non-interactive with all defaults). If it balks, do it manually.
2. `ssh appdev "cd /var/www && npm install"`
3. Add dependencies: nothing extra needed — Svelte 5 + TypeScript is sufficient.
4. Write source files via Write/Edit on the mount.

## Files to write

**vite.config.ts** — must include host-check allow-list for Zerops dev subdomain. Vite's config key is `server.allowedHosts`. Use wildcard `['.zerops.app']` so both appdev and appstage URLs work. Also set `server.host: '0.0.0.0'` for routable binding.

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

**src/lib/api.ts** — single HTTP helper, every component uses this. Reads `import.meta.env.VITE_API_URL`, defaults to empty string for dev proxy / same-origin. Enforces `res.ok` + content-type JSON. Copy this shape verbatim:

```ts
const BASE = (import.meta.env.VITE_API_URL ?? "").replace(/\/$/, "");

export async function api(path: string, init?: RequestInit): Promise<Response> {
  const url = `${BASE}${path}`;
  const res = await fetch(url, init);
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`API ${res.status} ${res.statusText} ${path}: ${body.slice(0, 200)}`);
  }
  const ct = res.headers.get("content-type") ?? "";
  if (!ct.toLowerCase().includes("application/json")) {
    throw new Error(`API ${path} returned non-JSON content-type ${ct} — likely SPA fallback, check VITE_API_URL baking`);
  }
  return res;
}
```

**src/lib/StatusPanel.svelte** — polls `/api/status` every 5s, renders one row per service (db, redis, nats, storage, search) with a colored dot. Three render states: loading / error (with `data-error` element) / populated. Outer wrapper carries `data-feature="status"`. Each row carries `data-service="{name}"`. Use Svelte 5 runes (`$state`, `$effect`).

```svelte
<script lang="ts">
  import { api } from './api';
  import { onMount, onDestroy } from 'svelte';

  let status = $state<Record<string, string> | null>(null);
  let error = $state<string | null>(null);
  let timer: ReturnType<typeof setInterval>;

  async function poll() {
    try {
      const res = await api('/api/status');
      status = await res.json();
      error = null;
    } catch (e) {
      error = (e as Error).message;
    }
  }

  onMount(() => { poll(); timer = setInterval(poll, 5000); });
  onDestroy(() => clearInterval(timer));
</script>

<section data-feature="status">
  <h2>Service Status</h2>
  {#if error}
    <p data-error>Error: {error}</p>
  {:else if status === null}
    <p>Loading…</p>
  {:else}
    <ul>
      {#each Object.entries(status) as [name, state]}
        <li data-service={name}>
          <span class="dot" class:ok={state === 'ok'} class:err={state !== 'ok'}></span>
          {name}: {state}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .dot { display:inline-block; width:10px; height:10px; border-radius:50%; margin-right:8px; }
  .dot.ok { background:#22c55e; }
  .dot.err { background:#ef4444; }
  ul { list-style:none; padding:0; }
  li { padding:6px 0; }
</style>
```

**src/App.svelte** — renders `<StatusPanel />` and literally nothing else. No tabs, no routing, no empty sections.

```svelte
<script lang="ts">
  import StatusPanel from './lib/StatusPanel.svelte';
</script>

<main>
  <h1>Nest.js Showcase Dashboard</h1>
  <StatusPanel />
</main>
```

**src/main.ts** — standard Vite entry mounting App to `#app`.

**.env.example** — `VITE_API_URL=` (empty by default).

**.gitignore** — Vite default + `node_modules/`, `dist/`.

**tsconfig.json / tsconfig.app.json / tsconfig.node.json** — what Vite create generated.

**svelte.config.js** — whatever vite-svelte template emits.

## What NOT to write

- No README.md — delete any emitted one.
- No zerops.yaml — main agent writes it.
- No `.git/` — delete `.git/` after scaffolder runs: `ssh appdev "rm -rf /var/www/.git"`.
- No feature sections (ItemsList, JobsDemo, UploadForm, etc.) — feature sub-agent owns those.
- No .env file (only .env.example).
- No dev proxy to /api (let VITE_API_URL do the work in both dev and prod).

## Workflow

1. Scaffold via npm create vite (SSH into appdev).
2. npm install (SSH).
3. Write source files listed above (mount).
4. Verify imports — open `node_modules/@sveltejs/vite-plugin-svelte/package.json` and check exports. Use Svelte 5 idioms from the installed version's d.ts.
5. Delete .git/ and README.md.

## Pre-ship self-verification

```bash
HOST=appdev
MOUNT=/var/www/$HOST
FAIL=0

# allowedHosts for .zerops.app present
if ! grep -q "\.zerops\.app" $MOUNT/vite.config.ts 2>/dev/null; then
    echo "FAIL: vite.config.ts missing .zerops.app allowedHosts"; FAIL=1
fi

# src/lib/api.ts has content-type guard
if ! grep -q "application/json" $MOUNT/src/lib/api.ts 2>/dev/null; then
    echo "FAIL: api.ts missing content-type guard"; FAIL=1
fi

# VITE_API_URL reader present
if ! grep -q "VITE_API_URL" $MOUNT/src/lib/api.ts 2>/dev/null; then
    echo "FAIL: api.ts doesn't read VITE_API_URL"; FAIL=1
fi

# StatusPanel has data-feature="status"
if ! grep -q 'data-feature="status"' $MOUNT/src/lib/StatusPanel.svelte 2>/dev/null; then
    echo "FAIL: StatusPanel missing data-feature=status"; FAIL=1
fi

# No README
if [ -f $MOUNT/README.md ]; then echo "FAIL: README exists"; FAIL=1; fi

# No zerops.yaml
if [ -f $MOUNT/zerops.yaml ]; then echo "FAIL: zerops.yaml exists"; FAIL=1; fi

# .gitignore has node_modules + dist
if ! grep -qE "node_modules" $MOUNT/.gitignore; then echo "FAIL: .gitignore missing node_modules"; FAIL=1; fi
if ! grep -qE "dist" $MOUNT/.gitignore; then echo "FAIL: .gitignore missing dist"; FAIL=1; fi

# No .git/
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then echo "FAIL: .git exists"; FAIL=1; fi

exit $FAIL
```

Fix any failure and re-run until exit 0.

## Return report

- Files written
- Pre-ship exit code (must be 0)
- Svelte/Vite versions installed

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

**Principle 2 — Routable network binding** — bind to all network interfaces (0.0.0.0), not loopback. Applies to the Vite dev server.

**Principle 6 — Stripped build-output root for static deploys** — zerops.yaml will use `./dist/~` tilde suffix (main agent writes this, not you). Ensure Vite's `build.outDir` stays `dist` (default) so the main agent's zerops.yaml path aligns.

The other principles (1, 3, 4, 5) don't apply to a pure SPA with no backend.

<<<END MANDATORY>>>

```
