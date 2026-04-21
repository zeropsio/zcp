# Dispatch 1 — scaffold-appdev-svelte-spa

**Dispatched at**: `2026-04-21T14:20:57`
**Description**: Scaffold appdev (Svelte SPA)
**Subagent type**: `general-purpose`
**Prompt length**: 6188 chars
**Tool-use id**: `toolu_01XnMiExR4hsPmjAezaVfe3m`

---

## Transmitted prompt (verbatim)

```
You are a scaffolding sub-agent in the Zerops recipe workflow. Your codebase is **appdev** — a Svelte + Vite SPA dashboard that will talk to a NestJS API. Mount: `/var/www/appdev/` (SSHFS). Hostname for SSH: `appdev`.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

## Your job

Build a **bare health-dashboard-only SPA**:
- One page. One component. Polls `/api/status` every 5s. Renders one colored dot per managed service. That's it.
- NO feature sections (no items-crud, no cache-demo, no storage-upload, no search-items, no jobs-dispatch). Those are authored later by a feature sub-agent.
- NO zerops.yaml. NO README.md. The main agent writes those later.

## Where commands run

You run on zcp. `/var/www/appdev/` is an SSHFS mount into the container. File writes via Write/Edit work directly. Every **executable** command runs via `ssh appdev "cd /var/www && <command>"`. NEVER `cd /var/www/appdev && ...` on zcp.

## Steps

1. **Scaffold Vite + Svelte (TypeScript)** via `ssh appdev "cd /var/www && npm create vite@latest . -- --template svelte-ts --yes"`. Use `--yes` to bypass prompts; if it still prompts, use stdin heredoc or run with `echo "y" |`. If the scaffolder initializes a git repo, delete it: `ssh appdev "rm -rf /var/www/.git"`.

2. **Install deps**: `ssh appdev "cd /var/www && npm install"`.

3. **Read scaffolded files** (Read tool, batched before any Edit): `package.json`, `vite.config.ts`, `svelte.config.js`, `tsconfig.json`, `tsconfig.app.json`, `tsconfig.node.json`, `src/main.ts`, `src/App.svelte`, `src/app.css`, `index.html`, `.gitignore`.

4. **Edit vite.config.ts**: add `server.host: '0.0.0.0'`, `server.port: 5173`, `server.strictPort: true`, and `server.allowedHosts: ['.zerops.app']` (Vite dev-server host-check allow-list — without this, the public subdomain returns "Blocked request"). Also add `preview.allowedHosts: ['.zerops.app']` for completeness.

5. **Write `src/lib/api.ts`** — the single HTTP helper. Reads `import.meta.env.VITE_API_URL ?? ''`, enforces `res.ok` + `Content-Type: application/json`, throws descriptive error on failure. Copy the pattern EXACTLY from this reference (do not invent your own shape):

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

6. **Write `src/lib/StatusPanel.svelte`**: polls `/api/status` every 5s via the `api()` helper; renders rows for `db`, `cache`, `queue`, `storage`, `search`. Each row has a `data-service="{name}"` attribute and a colored dot (green=ok, red=error, yellow=loading/degraded). Outer wrapper: `<section data-feature="status">`. Include three explicit render states (loading, error with `[data-error]`, populated).

7. **Rewrite `src/App.svelte`**: imports + renders `<StatusPanel />` only. No routing, no layout slots, no nav. Include a minimal header ("Zerops × NestJS Showcase") and the status panel below.

8. **Edit `index.html`**: title = "Zerops × NestJS Showcase".

9. **Write `src/app.css`** (or keep scaffold's) with minimal modern dark-mode styles for the status panel: table-like rows, colored dots using CSS variables.

10. **Write `.env.example`**: document `VITE_API_URL=` (empty default → dev proxy handles it).

11. **Ensure `.gitignore`** covers `node_modules`, `dist`, `.env`, `.env.local` — the Vite scaffolder's default usually does this already.

12. **Pre-ship assertions** (run as inline `bash -c`, NOT saved to the codebase):

```
HOST=appdev
MOUNT=/var/www/$HOST
FAIL=0
# Assertion 8 — .gitignore covers node_modules
[ -f $MOUNT/.gitignore ] && grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore || { echo "FAIL: .gitignore missing node_modules"; FAIL=1; }
# Assertion 10 — no .git/ left
ssh $HOST "test -d /var/www/.git" 2>/dev/null && { echo "FAIL: /var/www/.git exists — delete it"; FAIL=1; } || true
# Assertion 11 — api.ts helper exists
[ -f $MOUNT/src/lib/api.ts ] || { echo "FAIL: src/lib/api.ts helper missing"; FAIL=1; }
# Assertion 12 — no direct fetch("/api/...") calls outside api.ts
grep -rnE 'fetch\("/api/' $MOUNT/src/ --include='*.svelte' --include='*.ts' 2>/dev/null | grep -v 'src/lib/api.ts' | head -1 && { echo "FAIL: direct fetch(/api/...) call outside api.ts helper"; FAIL=1; } || true
# Assertion 13 — Vite allowedHosts set
grep -q 'allowedHosts' $MOUNT/vite.config.ts || { echo "FAIL: vite.config.ts missing allowedHosts for .zerops.app"; FAIL=1; }
exit $FAIL
```

Fix every FAIL and rerun until exit 0.

13. **Do NOT write**: `zerops.yaml`, `README.md`, any feature component (items, search, storage, etc.), routing libraries.

## Return

Report the files you wrote, the env var name you wired (`VITE_API_URL`), and the exit code of the pre-ship script (must be 0). Under 150 words.
```
