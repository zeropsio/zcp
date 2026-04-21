# Dispatch 1 — scaffold-appdev-svelte-spa

**Dispatched at**: `2026-04-21T19:35:27`
**Description**: Scaffold appdev Svelte SPA
**Subagent type**: `general-purpose`
**Prompt length**: 7886 chars
**Tool-use id**: `toolu_01GUG4P7MVHUNQvnozh5wAYV`

---

## Transmitted prompt (verbatim)

```
You are a scaffold sub-agent for a Zerops recipe. Your codebase is the FRONTEND — a Svelte 5 + Vite + TypeScript SPA mounted at `/var/www/appdev/`. Host to SSH into: `appdev`. Mount is SSHFS — writes go via Write/Edit, executables run via `ssh appdev "cd /var/www && <cmd>"`.

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every Edit must be preceded by a Read of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, cargo new, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>

<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

The Zerops platform imposes invariants that every service must honor. Framework scaffolders do not automatically satisfy them. Before pre-ship assertions run, walk this list. For each principle that applies to your codebase type:

1. Identify the framework's specific idiom that satisfies the principle (you just scaffolded the framework — you know its APIs).
2. Verify the idiom is present in the scaffolded code. If absent, implement it.
3. Record a fact with scope=both naming both the principle number AND the idiom used.
4. If the framework offers no idiom for a principle that applies, implement the behavior yourself AND record a fact explaining the implementation.

Principles are absolute. Idioms are framework-specific and not listed here; the subagent translates.

**Principle 1 — Graceful shutdown within thirty seconds** — on termination signal, stop accepting new work, drain in-flight, close connections, exit within 30s.
**Principle 2 — Routable network binding** — bind to all interfaces (the wildcard address) or the container's advertised IP, never loopback.
**Principle 3 — Client-origin awareness behind a proxy** — configure framework proxy-trust / forwarded-header handling for exactly one hop.
**Principle 4 — Competing-consumer semantics at replica count two or more** — broker queue-group / consumer-group on every subscription.
**Principle 5 — Structured credential passing** — pass user/pass as structured options, not URL embedded (passwords may contain URL-reserved chars).
**Principle 6 — Stripped build-output root for static deploys** — in zerops.yaml static deploy, strip the build-output dir via the tilde suffix.

<<<END MANDATORY>>>

## Your deliverable — Svelte 5 SPA "health dashboard only" skeleton

**Framework choice**: Use `npm create vite@latest . -- --template svelte-ts` (Svelte 5 + TS + Vite). Run via `ssh appdev "cd /var/www && npm create vite@latest . -- --template svelte-ts"`. Add `--yes` / non-interactive flags as needed. Scaffolder may create `.git/` — delete it before returning with `ssh appdev "rm -rf /var/www/.git"`.

**Dependencies to install** (after scaffold): whatever Vite's svelte-ts template wants — nothing extra.

**Vite config requirements**:
- `vite.config.ts` — set `server.host: true` (bind 0.0.0.0), `server.port: 5173`, `server.allowedHosts: ['.zerops.app']` (wildcard so both dev and stage subdomains pass), and `preview.allowedHosts: ['.zerops.app']`. Also set `preview.host: true` and `preview.port: 5173`. **DO NOT add API proxy** — the frontend reads `VITE_API_URL` from env and calls the API origin directly (CORS handled on API side by the main agent later). Emit absolute base for assets if needed. Strict port false.

**Write these files**:

1. `src/lib/api.ts` — the single HTTP helper. Reads `import.meta.env.VITE_API_URL` (defaults to `''`). Exports an async `api(path, init?)` function that prefixes the base URL, calls fetch, asserts `res.ok` and `Content-Type` starts with `application/json`, parses and returns JSON. Throws a descriptive error on any failure. Every component MUST use this helper, never raw fetch.

2. `src/lib/StatusPanel.svelte` — top-level Svelte 5 component that:
   - Calls `api('/api/status')` every 5s (setInterval, cleanup on unmount).
   - Renders loading / error / populated states (three explicit branches).
   - Populated state: one row per service in the plan — `db`, `redis`, `queue`, `storage`, `search`. For each row: a colored dot (green = `"ok"`, red = anything else or missing), the service name, and the value text.
   - Outer wrapper: `<section data-feature="status">`.
   - Each row: `<div data-service="{name}">` with the dot inside.
   - Error branch: an element with `data-error` containing the thrown message.
   - Use Svelte 5 runes syntax (`$state`, `$effect`).

3. `src/App.svelte` — renders `<StatusPanel />` and nothing else. Title "Zerops NestJS Showcase" in an h1 is acceptable.

4. `src/main.ts` — standard Vite Svelte bootstrap (the scaffolder already writes this; leave it unless it needs tweaking).

5. `.env.example` — document `VITE_API_URL=` (blank default, set per env at runtime).

6. `.gitignore` — ensure `node_modules`, `dist`, `.env` present (Vite scaffolder usually writes this; extend if needed).

**DO NOT WRITE**:
- `README.md` — delete any that Vite emits.
- `zerops.yaml` — main agent writes this later.
- Any route or component beyond StatusPanel.
- Feature components (items, cache, storage, search, jobs). These come later from a feature sub-agent.
- `.git/` — delete if scaffolder creates one.

**Installation**:
After scaffolding + file writes, run `ssh appdev "cd /var/www && npm install"` to populate `node_modules`. Then verify: `ssh appdev "cd /var/www && npx vite build"` should compile without errors. If the build fails, fix — do not return broken.

### Pre-ship self-verification

Run this on zcp after all files are written (substitute HOST=appdev) and report exit code:

```bash
HOST=appdev
MOUNT=/var/www/$HOST
FAIL=0

# .gitignore covers node_modules
if [ ! -f $MOUNT/.gitignore ] || ! grep -qE "^(\s*node_modules|\s*/node_modules)" $MOUNT/.gitignore; then
  echo "FAIL: .gitignore missing or does not ignore node_modules"; FAIL=1
fi

# No residual .git/
if ssh $HOST "test -d /var/www/.git" 2>/dev/null; then
  echo "FAIL: /var/www/.git on appdev — delete before returning"; FAIL=1
fi

# api.ts helper exists and rejects non-JSON
if [ ! -f $MOUNT/src/lib/api.ts ]; then
  echo "FAIL: src/lib/api.ts missing"; FAIL=1
fi

# StatusPanel exists with data-feature="status"
if ! grep -q 'data-feature="status"' $MOUNT/src/lib/StatusPanel.svelte 2>/dev/null; then
  echo "FAIL: StatusPanel.svelte missing or no data-feature=status wrapper"; FAIL=1
fi

# vite.config.ts has allowedHosts and 0.0.0.0 bind
if ! grep -qE "allowedHosts.*zerops.app" $MOUNT/vite.config.ts 2>/dev/null; then
  echo "FAIL: vite.config.ts does not allow .zerops.app hosts"; FAIL=1
fi

# No README
if [ -f $MOUNT/README.md ]; then
  echo "FAIL: README.md exists — delete before returning"; FAIL=1
fi

exit $FAIL
```

Run via `bash -c '...'` on zcp. Must exit 0. Fix any issues and re-run until clean. **Do NOT leave the script in the codebase.**

### Reporting back

Return a concise list of: files you wrote, files the scaffolder emitted that you kept/modified/deleted, pre-ship script exit code (must be 0), and any platform-principle translations you made (e.g., "Principle 2 satisfied via server.host: true in vite.config.ts"). Do NOT claim any feature implementation.
```
