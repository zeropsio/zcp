# brief-scaffold-appdev-showcase-composed.md

**Role**: scaffold sub-agent (frontend codebase)
**Tier**: showcase
**Source atoms** (per atomic-layout.md §6):

```
briefs/scaffold/mandatory-core.md
briefs/scaffold/symbol-contract-consumption.md  (same contract JSON as apidev)
briefs/scaffold/framework-task.md
briefs/scaffold/frontend-codebase-addendum.md
briefs/scaffold/pre-ship-assertions.md
briefs/scaffold/completion-shape.md
+ pointer-include principles/where-commands-run.md
+ pointer-include principles/file-op-sequencing.md
+ pointer-include principles/tool-use-policy.md
+ pointer-include principles/platform-principles/01..06.md
+ pointer-include principles/comment-style.md
+ pointer-include principles/visual-style.md
+ PriorDiscoveriesBlock(sessionID, substep=generate.scaffold)
```

Interpolations: `{{.Hostname}} = appdev`, `{{.SymbolContract | toJSON}}` (same blob), `{{.Framework}} = Svelte 5 + Vite 7`.

---

## Composed brief (sub-agent receives this)

```
You are a scaffolding sub-agent. Scaffold the frontend codebase for hostname `appdev` on the SSHFS mount `/var/www/appdev/`.

--- [briefs/scaffold/mandatory-core.md] ---

(Same as scaffold-apidev: permitted tools, forbidden tools, file-op sequencing — verbatim.)

--- [principles/where-commands-run.md, pointer-included] ---

(Positive-form: executables run via `ssh appdev "cd /var/www && <command>"` — same atom, same text, pointer re-used.)

--- [briefs/scaffold/symbol-contract-consumption.md, interpolated] ---

(Same SymbolContract JSON as the api dispatch — byte-identical. Only the interpretation differs: `appliesTo` filters which rules the frontend must enforce before return.)

The rules that apply to `frontend`: `routable-bind` (Vite dev server binds 0.0.0.0), plus the role-universal rules (`gitignore-baseline`, `env-example-preserved`, `no-scaffold-test-artifacts`, `skip-git`).

The rules whose `appliesTo` is `api` or `worker` do NOT apply to this codebase — you may skip their preAttestCmds, but you should still read the shared sections (HTTPRoutes, NATSSubjects, DTOs) because the frontend CONSUMES those endpoints and DTOs.

Relevant contract sections for your role:

- `HTTPRoutes` — `/api/health`, `/api/status` exist at this scaffold substep. Feature routes (`/api/items`, `/api/cache`, `/api/files`, `/api/search`, `/api/jobs`, `/api/mail`) land later via the feature sub-agent. Your scaffold consumes `/api/status` in the StatusPanel; everything else is out-of-scope for now.
- `Hostnames` — your code has no direct need for hostnames at build time. Runtime calls the API via `VITE_API_URL` which is baked at build time from `${STAGE_API_URL}` or `${DEV_API_URL}` by the main agent's zerops.yaml.
- `DTOs` — `ItemDTO`, `JobDTO`, `FileDTO`, `SearchHitDTO`, `MailDTO` are produced by the api codebase and consumed by panel components the feature sub-agent will add later. Your scaffold only touches a StatusPanel type.

--- [briefs/scaffold/framework-task.md] ---

You scaffold a health-dashboard-only Svelte + Vite SPA on the mount. Feature panels are owned by a later sub-agent. You own: Vite + Svelte 5 skeleton, `src/lib/api.ts` helper, a single StatusPanel component, dashboard CSS tokens, `.env.example`, `.gitignore`.

Workflow:

1. Run the framework scaffolder via SSH with --skip-git or remove `.git/` after:

       ssh appdev "cd /var/www && npm create vite@latest . -- --template svelte-ts --skip-git"

   (Vite's create script does not emit `--skip-git` on every version. If the flag is unknown, drop it and run `ssh appdev "rm -rf /var/www/.git"` after return. The FixRule `skip-git` accepts either branch.)

2. Read the scaffolder output before the first Edit.

3. Write the files in the addendum below using Write/Edit on the mount.

--- [briefs/scaffold/frontend-codebase-addendum.md] ---

Files you write:

- `package.json` — Vite 7 + Svelte 5. Deps: `svelte`, `vite`, `@sveltejs/vite-plugin-svelte`, `typescript`, `svelte-check`. Script `dev = vite --host 0.0.0.0`, `build = vite build`, `preview = vite preview`.
- `vite.config.ts` — import `@sveltejs/vite-plugin-svelte`. `server.host = '0.0.0.0'`, `server.port = 5173`, `server.allowedHosts = ['.zerops.app']`, `server.strictPort = true`, `server.proxy = { '/api': { target: 'http://127.0.0.1:3000', changeOrigin: true } }`. `preview.host = '0.0.0.0'`, `preview.allowedHosts = ['.zerops.app']`.
- `tsconfig.json` — Vite/Svelte defaults, target ES2022, moduleResolution bundler, verbatimModuleSyntax true.
- `svelte.config.js` — `import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';` default export `{ preprocess: vitePreprocess() }`.
- `index.html` — standard Vite entry pointing to `/src/main.ts`.
- `src/main.ts` — `mount(App, { target: document.getElementById('app')! })` (Svelte 5 API).
- `src/app.css` — single modest stylesheet: CSS custom properties for theme, `.panel` / `.dashboard` / form-input styles, selectors `[data-feature]`, `[data-row]`, `[data-hit]`, `[data-file]`, `[data-result]`, `[data-status]`, `[data-processed-at]`, `[data-error]`. Single-column on mobile, 2-column on wide screens. These selectors are consumed by feature panels later — declare the styles once here.
- `src/lib/api.ts` — fetch helper with content-type enforcement. Signature:

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

  Components always call `api()`/`apiJson()`; they do not call `fetch()` directly.
- `src/App.svelte` — mounts `<StatusPanel />` and nothing else. Outer `<main class="dashboard">` with a `<header>` showing the recipe title and subtitle, then one `<section>` wrapping `StatusPanel`. Include a Svelte comment noting that feature panels get appended here by the feature sub-agent.
- `src/lib/StatusPanel.svelte` — reads `/api/status` every 5s via `apiJson`, renders one row per managed service (`db`, `redis`, `nats`, `storage`, `search`). Three render states: loading, error (visible banner with `data-error`), populated. Wrapper `<section class="panel" data-feature="status">`. Each row is `<div class="status-row" data-service="{name}"><span class="dot dot-{state}"></span><span>{name}</span></div>`. Svelte 5 runes (`$state`, `$effect`). Dot colors from CSS: `dot-ok` green, `dot-degraded` yellow, `dot-error` red, `dot-unknown` gray.
- `.env.example` — `VITE_API_URL=` with a one-line comment explaining the value is baked at build time from the api hostname.
- `.gitignore` — ignore `node_modules`, `dist`, `.DS_Store`, `*.log`, `.env` (keep `.env.example`), `.svelte-kit`, `.vite`.

Files you do NOT write at this substep:
- README.md — main agent writes at deploy.readmes. Delete if Vite created one.
- zerops.yaml — main agent writes after this scaffold returns.
- .git/ — delete after the scaffolder runs (FixRule `skip-git`).
- Any feature panel beyond StatusPanel (`ItemsPanel`, `CachePanel`, `StoragePanel`, `SearchPanel`, `JobsPanel`, `MailPanel`).
- CORS config — the API handles CORS.
- Any cross-codebase type file — the feature sub-agent reconciles shared types after scaffolds return.

--- [principles/platform-principles/01..06.md, pointer-included] ---

01 Graceful shutdown — N/A to frontend (browser client is stateless).
02 Routable bind — vite dev server `server.host = '0.0.0.0'` AND `preview.host = '0.0.0.0'`.
03 Proxy trust — N/A to frontend.
04 Competing consumer — N/A.
05 Structured credentials — N/A (frontend holds no credentials).
06 Stripped build root — main-agent `deployFiles: ./dist/~` concern; frontend emits `dist/` output only.

Record a fact via mcp__zerops__zerops_record_fact after setting up the routable-bind rule so the writer has an audit trail.

--- [principles/comment-style.md + principles/visual-style.md, pointer-included] ---

(Same atoms as apidev — ASCII-only, no box-drawing, no Unicode separators.)

--- [briefs/scaffold/pre-ship-assertions.md] ---

Source of truth: `SymbolContract.FixRecurrenceRules` where `appliesTo` includes `frontend` or `any`. Aggregate exit must be 0.

Reminder snapshot (codebase-level):

    HOST=appdev
    MOUNT=/var/www/$HOST
    test -f $MOUNT/.gitignore                                                     || { echo FAIL: .gitignore missing; exit 1; }
    grep -qE '^(node_modules|/node_modules)' $MOUNT/.gitignore                    || { echo FAIL: .gitignore missing node_modules; exit 1; }
    grep -q '0.0.0.0' $MOUNT/vite.config.ts                                       || { echo FAIL: vite.config.ts missing 0.0.0.0 bind; exit 1; }
    grep -q 'zerops.app' $MOUNT/vite.config.ts                                    || { echo FAIL: vite.config.ts missing allowedHosts; exit 1; }
    grep -q 'VITE_API_URL' $MOUNT/src/lib/api.ts                                  || { echo FAIL: api.ts missing VITE_API_URL; exit 1; }
    grep -q 'application/json' $MOUNT/src/lib/api.ts                              || { echo FAIL: api.ts missing content-type assertion; exit 1; }
    ! grep -q 'fetch(' $MOUNT/src/lib/StatusPanel.svelte                          || { echo FAIL: StatusPanel uses fetch() directly; exit 1; }
    test ! -f $MOUNT/README.md                                                    || { echo FAIL: README.md present; exit 1; }
    test ! -f $MOUNT/zerops.yaml                                                  || { echo FAIL: zerops.yaml present; exit 1; }
    ! ssh $HOST 'test -d /var/www/.git'                                           || { echo FAIL: /var/www/.git present; exit 1; }
    test ! -f $MOUNT/.env                                                         || { echo FAIL: .env present; exit 1; }
    test -f $MOUNT/.env.example                                                   || { echo FAIL: .env.example missing; exit 1; }
    exit 0

After assertions:

    ssh appdev "cd /var/www && npm install"
    ssh appdev "cd /var/www && npx vite build 2>&1 | tail -30"

Fix any TypeScript errors before returning. Do not start the dev server — main owns smoke testing.

--- [briefs/scaffold/completion-shape.md] ---

(Same as apidev: bulleted files written + byte counts, pre-attest exit code, build tail output, record_fact calls, env-var names consumed by the frontend.)

--- [PriorDiscoveriesBlock(sessionID, substep=generate.scaffold)] ---

(Empty at first dispatch.)
```

**Composed byte-budget**: ~8.5 KB (v34 appdev dispatch was 10459 chars; reduction similar shape to apidev).
