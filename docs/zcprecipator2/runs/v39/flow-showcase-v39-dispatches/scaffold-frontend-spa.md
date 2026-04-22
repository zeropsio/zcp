# Dispatch 1 — scaffold-frontend-spa

**Dispatched at**: `2026-04-22T18:24:07`
**Description**: Scaffold frontend SPA
**Subagent type**: `general-purpose`
**Prompt length**: 8552 chars
**Tool-use id**: `toolu_01337XmQHfLTF88VFV9C4S2u`

---

## Transmitted prompt (verbatim)

```
You are scaffolding the FRONTEND codebase of the nestjs-showcase Zerops recipe (a multi-codebase showcase with separate frontend / NestJS API / worker repos).

## Codebase scope

- Hostname: **appdev** (mount path `/var/www/appdev/`)
- Stack: **Svelte 5 + Vite + TypeScript** built into a static bundle. Production runtime is `static` (Nginx); dev runtime is `nodejs@24` (the host that runs `vite`).
- Dev server port: **5173** (Vite default)
- Public dev URL: `https://appdev-${zeropsSubdomainHost}-5173.prg1.zerops.app`
- Public stage URL: `https://appstage-${zeropsSubdomainHost}.prg1.zerops.app`

The recipe declares 5 features (items-crud, cache-demo, storage-upload, search-items, jobs-dispatch). **You implement NONE of them.** Your scaffold is the bare health dashboard. A separate feature sub-agent will fill the dashboard later.

## ⚠ CRITICAL: where commands run (read FIRST)

You are running on the **zcp orchestrator container**, not on the target dev container. The path `/var/www/appdev/` on zcp is an **SSHFS network mount** — a write surface, not an execution surface.

- **File writes** via Write/Edit/Read against `/var/www/appdev/` work correctly.
- **Executable commands** MUST run via SSH:
  ```
  ssh appdev "cd /var/www && <command>"
  ```
  NEVER `cd /var/www/appdev && <command>` from Bash.

Every `npm install`, `npx`, `npm create vite`, `vite build`, `tsc`, `git init/add/commit` runs through the SSH form. Wrong-side execution produces EACCES, broken `.bin/` symlinks, and native-module ABI mismatches.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

## What to write

1. **Scaffold a Vite + Svelte + TypeScript project at `/var/www/` on the appdev container.** Use the official template: `ssh appdev "cd /var/www && npm create vite@latest . -- --template svelte-ts"`. The scaffolder is interactive about overwriting an empty dir — you may need to `--yes` or pre-clear conflicts. After scaffold returns, **delete `/var/www/.git/`** with `ssh appdev "rm -rf /var/www/.git"` (the main agent re-inits git).

2. **`vite.config.ts`** — configure dev server for the platform:
   - `server.host: '0.0.0.0'`, `server.port: 5173`
   - `server.allowedHosts: ['.zerops.app']` (Vite 5+ requires this for the public dev subdomain or it returns "Blocked request / Invalid Host header")
   - Same for `preview` if you ship one
   - Read `import.meta.env.VITE_API_URL` at runtime — do NOT proxy to localhost; the bundle calls the API directly via the helper

3. **`src/lib/api.ts`** — the SINGLE HTTP helper every component must use. Verbatim shape (do not invent your own):
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
       throw new Error(`API ${path} returned non-JSON content-type ${ct} — likely SPA fallback or wrong VITE_API_URL`);
     }
     return res;
   }
   ```

4. **`src/App.svelte`** — renders ONLY `<StatusPanel />`. No nav, no layout, no routing, no other sections. The outer wrapper carries `data-feature="status"`.

5. **`src/lib/StatusPanel.svelte`** — calls `api("/api/status")` every 5 seconds via the helper. Renders:
   - Three explicit visual states: loading (spinner / "Checking…"), error (red banner with `data-error` attribute holding the message), populated.
   - One `<div data-service="{name}">` row per service from the response (db, redis, nats, storage, search). Each row has a colored dot (green = `"ok"`, red = anything else) and the service name.
   - Outer `<section data-feature="status">` wrapper.
   - Tasteful inline styling — readable but minimal. No buttons, no forms, no tables.

6. **`src/main.ts`** — standard Svelte mount, scaffolder default is fine.

7. **Strip the scaffolder's demo content** — delete `src/lib/Counter.svelte`, the scaffolder's default `<App>` content, demo CSS, the public Vite logo. Keep `index.html` (you may need to update title to "NestJS Showcase").

8. **`package.json` cleanup** — keep only what's needed: `svelte`, `@sveltejs/vite-plugin-svelte`, `vite`, `typescript`, `svelte-check`, `tslib`, `@tsconfig/svelte`. No router, no UI library.

9. **`.gitignore`** — must include `node_modules`. The Vite scaffold ships one; verify.

10. **`.env.example`** — document the one client-side var: `VITE_API_URL=`. (Real value injected by Zerops via project env vars.)

## What NOT to write

- **NO `zerops.yaml`** — main agent writes it.
- **NO `README.md`** — main agent writes it after deploy.
- **NO feature code**: no items list, no cache demo, no upload form, no search UI, no jobs UI. Touching any of these corrupts the v14 author-coherence guarantee.
- **NO `.git/`** — delete after scaffolder runs.
- **NO routing, tabs, layouts, multiple sections.**

## Pre-ship self-verification (MANDATORY)

Save the following to `/tmp/preship-app.sh` (NOT under `/var/www/appdev/`) on zcp and run it. Fix any FAIL and re-run until exit 0:

```bash
#!/bin/bash
HOST=appdev
MOUNT=/var/www/$HOST
FAIL=0

# 1. .gitignore covers node_modules
[ -f $MOUNT/.gitignore ] || { echo "FAIL: .gitignore missing"; FAIL=1; }
grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore || { echo "FAIL: .gitignore missing node_modules"; FAIL=1; }

# 2. api.ts exists with the canonical shape
grep -q "VITE_API_URL" $MOUNT/src/lib/api.ts || { echo "FAIL: src/lib/api.ts missing VITE_API_URL reader"; FAIL=1; }
grep -q "application/json" $MOUNT/src/lib/api.ts || { echo "FAIL: src/lib/api.ts missing content-type guard"; FAIL=1; }

# 3. StatusPanel renders data-feature="status"
grep -q 'data-feature="status"' $MOUNT/src/lib/StatusPanel.svelte || { echo "FAIL: StatusPanel missing data-feature wrapper"; FAIL=1; }

# 4. App.svelte ONLY mounts StatusPanel
grep -q "StatusPanel" $MOUNT/src/App.svelte || { echo "FAIL: App.svelte does not import StatusPanel"; FAIL=1; }

# 5. vite.config.ts has allowedHosts and 0.0.0.0
grep -qE "allowedHosts" $MOUNT/vite.config.ts || { echo "FAIL: vite.config.ts missing allowedHosts"; FAIL=1; }
grep -qE "'0\.0\.0\.0'|\"0\.0\.0\.0\"" $MOUNT/vite.config.ts || { echo "FAIL: vite.config.ts not binding 0.0.0.0"; FAIL=1; }

# 6. No README, no zerops.yaml, no .git
[ ! -f $MOUNT/README.md ] || { echo "FAIL: README.md exists - main agent owns it"; FAIL=1; }
[ ! -f $MOUNT/zerops.yaml ] || { echo "FAIL: zerops.yaml exists - main agent owns it"; FAIL=1; }
ssh $HOST "test -d /var/www/.git" 2>/dev/null && { echo "FAIL: /var/www/.git exists - delete it"; FAIL=1; }

# 7. No feature code
for f in items cache search upload jobs storage; do
  if grep -ril "data-feature=\"$f" $MOUNT/src/ 2>/dev/null | head -1; then
    echo "FAIL: feature code for $f found - scaffold ships health dashboard only"; FAIL=1
  fi
done

# 8. No fetch() direct calls outside api.ts
if grep -rn "fetch(" $MOUNT/src/ 2>/dev/null | grep -v "src/lib/api.ts" | head -1; then
  echo "FAIL: direct fetch() outside api.ts - components must use api() helper"; FAIL=1
fi

exit $FAIL
```

Run with `bash /tmp/preship-app.sh`. Pre-ship script must NOT be saved inside the codebase.

## Reporting back

Return a bulleted list of files written, the exit code of the pre-ship script (must be 0), and any fixes you applied. Do NOT claim feature implementation — you didn't.
```
