# v2_ecc1866e01114a501106daaa868bd3c3095cc72bc2967a6e020d96185bd918d5

## Summary
1. Brand kit is fully recoverable from Zerops' own code: the mark is a 4-path SVG (viewBox 0 0 42.27 50.48) in `frontend-legacy/libs/zui/src/logo/`, two teals `#3cbdb2` (main) / `#00b1a3` (secondary); the wordmark is `libs/zui/src/logo-full/` (viewBox 0 0 175.99 36.25); docs ship a mono variant (`#323232`) and a downloadable pack (`zerops-docs/apps/docs/static/brand/zerops-assets.zip`). GUI primary `#00ccbb`, accent blue `#0077cc`, canvas `#eceff3` light / `#0c0f0e` dark with mint `#00e5c0`; fonts Roboto (GUI), Geologica + Roboto + JetBrains Mono (zerops.io), Inter + JetBrains Mono (docs). White on `#00b1a3` fails AA (2.61:1); use `#007e72`.

2. Assets: 87 tech/service SVG icons under `libs/zef/src/shared-assets/custom-icons/` (nodejs, postgresql, valkey, redis…) registered as `mat-icon svgIcon`; provider marks (claude/codex/cursor/grok/antigravity/ubuntu/vscode) as `currentColor` Angular components; favicons under `apps/zerops/src/assets/meta/`. `apps/zerops/src/assets/services/*.svg` is dead.

3. Naming: the GUI calls the container "Zerops Control Plane" / "ZCP" (chip "Control Plane"), surfaces "Web Terminal", "SSH (Terminal)", "Cloud IDE", "Desktop IDEs", agents "AUTHORIZED", "coding agent". T3 markets itself as "the open-source control plane for coding agents" — that phrase must not survive in z3 (spec-z3 §0: z3 is a reader, not a second control plane). Words to delete: T3 Code/Connect, Tailscale, pairing code/link, worktree, environment (user-facing), Local checkout.

4. Constraints: one z3 environment = one Zerops project; map groups runtimes/data/infrastructure; strip phrases are pinned in `z3/apps/web/src/zerops/strip.ts`; cards are total decoders; credential card bypasses chat; restart = upgrade (~19 s, 502 shown as "restarting"); identity sessions cap at 900 s and re-mint silently; GUI is desktop-only (`viewport width=1200`), so there is no Zerops mobile precedent.

5. Positioning: live service map + lifecycle strip beside a T3-style thread UI — no agent app has the map, no cloud console has the agent.

## Report
# R6 — Zerops brand kit, asset inventory, naming, product constraints

Legend: **[V]** verified in code / served HTML / screenshot (path cited); **[I]** inferred.

## 1. Brand kit

### 1.1 The mark
- **[V]** `frontend-legacy/libs/zui/src/logo/logo.component.html` — `<svg viewBox="0 0 42.27 50.48">`, four `<path>`s, all `transform="translate(-.46 -.44)"`. Paths 1–2 (left half: `M20.19.7L3 7.27…`, `M8.5 37.74…`) carry class `c-logo-theme-main`; paths 3–4 (right half: `M41.9 18.47…`, `M23 50.67…`) carry `c-logo-theme-secondary`. Shape: a hexagonal "Z"-cut cube — left half lighter teal, right half darker teal.
- **[V]** `logo.component.scss`: `.c-logo-theme-main { fill: #3cbdb2 }`, `.c-logo-theme-secondary { fill: #00b1a3 }`, `transition: fill 400ms`. Same two fills in `logo-full.component.scss`.
- **[V]** The identical mark (same viewBox + path prefix `M20.19.7L3 7.27`) is inlined on zerops.io (scratchpad `zerops-io.html`, Angular `_ngcontent-ng-c606948037` attrs) — marketing and GUI share one asset. **[I]** zerops.io is an Angular app built on the same zui kit.
- **[V]** Docs variant `zerops-docs/apps/docs/static/logo.svg`: `viewBox="0 0 237 284"`, two paths, fills `#3DB1A2` (left) / `#24A492` (right) — slightly different teals than the GUI. `static/black-logo.svg`: same geometry, `#323232` — the **mono** variant. Docusaurus navbar uses `img/logo-icon.png` / `logo-icon-dark.png`, mobile `logo-mobile{,-dark}.png` (`docusaurus.config.js:166-190`).
- **[V]** Mono silhouette for pinned tabs: `apps/zerops/src/assets/meta/safari-pinned-tab.svg` (potrace, 800×800, `fill="#000000"`).
- **[V]** Login screen (`shots/gui-nonmember/01-login.png`): mark + "ZEROPS" wordmark rendered in mid-grey (mono), subtitle "Developer-first cloud platform"; dashboard header pill (`03-after-login.png`, `10-dashboard-dark.png`): mark in mint on a pale-teal pill (light) / on dark-green pill (dark).

### 1.2 The wordmark
- **[V]** `libs/zui/src/logo-full/logo-full.component.html` — `viewBox="0 0 175.99 36.25"`, six letter paths (Z, E, R, O, S, P — geometric, mono-width strokes, all `c-logo-full-theme-main`) plus the mark on the left (`is-logo is-logo-left` main, `is-logo-right` secondary). Letterforms are custom paths, not a font.

### 1.3 Colour system
GUI theme, **[V]** `frontend-legacy/libs/zef/src/styles/_theme.scss`:

| Token | Hex | Role |
|---|---|---|
| `$zeropsAlphaColor` (Material primary 100/500/700) | `#00ccbb` | primary, contrast white |
| `$zeropsGammaColor` (Material accent) | `#0077cc` | accent / links / focus (`--zcp-wizard-focus-ring`, `--z-filter-color-active`) |
| `$zeropsBetaColor` | `#e3ce35` | yellow (unused as Material palette) |
| `identityRed / Green / Purple / Pink / Black` | `#cc0011` / `#00cc55` / `#cc0077` / `#bb00cc` / `#1a1a1a` | identity set |
| `background` / `backgroundDark` | `#eceff3` / `#00100f` | canvas |
| `textGreen / identityLightGreen / textLightGreen` | `#00aea2` / `#00ded0` / `#00eadb` | teal text ramps |
| `$fontMain / $fontMono` | Roboto / DejaVu Sans Mono | |

Live tokens, **[V]** `shots/gui-nonmember/theme-tokens.json` (416 vars, light root only — `htmlClass:""`): `--color-zerops-dark: #02b1a3`, `--color-zerops-mid: #3cbdb2`, `--color-zerops-light-green: #bcfffa`, `--z-app-background: #eceff3`, `--z-app-text: #1a1a1a`, `--mat-badge-background-color: #00ccbb`, `--bu: 24px` (base unit), `--max-width: 1400px`, `--font-main: "Roboto"`, `--font-alt: "Open Sans"`, `--z-github-bg: #24292e`, status greens `#66bb6a`, `--z-service-stack-pipeline-timeline-finished: #00cc55`. Status colours per platform state in `$serviceStackStatusesColors` (`_theme.scss`): ACTIVE = Material green-400, CREATING blue-200, STOPPED orange-400, FAILED/DELETED red-400, UPGRADING/SCALING blue-400, each with a `_DARK` darken(20%).

Dark theme, **[V]** `libs/zef/src/styles/base/_dark-theme.scss` header: canvas `#0c0f0e`, surface `#141918`, raised `#1b2220`, overlay `#1e2624`, text `#e9eeec`, secondary `#9faea9`, faint `#5e6e69`, **mint `#00e5c0` reserved as the single brand accent** (fills `rgba(0,229,192,.14)`, text-on-fill `#58efd4`); primary buttons in dark are mint-tinted, not solid. Pre-boot dark background `#0c0f0e` (`apps/zerops/src/index.html`). Toggle key `localStorage zef-preferred-theme` (`dark|system`), class `zef-dark-theme` on `<html>` and `<body>`.

Marketing (zerops.io), **[V]** curl 2026-08-28, `/assets/index-DR1ArQbs.css`: Material-3 tokens `--mat-sys-primary: light-dark(#00a49a, #8effee)`, `--mat-sys-primary-container: light-dark(#c6fff6, #02837c)`, `--mat-sys-on-primary: light-dark(#ffffff, #0c5552)`; `--background-light: #F0F0F0`, `--background-dark: #00100f`; `--alt-card-accent: #15D7C4`; `--section-indicator-active: light-dark(#0FB8A8, #15D7C4)`; identity vars identical to the GUI; `--code-block-bg: #171717`; agent marks shipped as data-URI vars `--agent-mark-claude` (fill `#D97757`), `--agent-mark-openai`, `--agent-mark-cursor`.

Accessibility, **[V]** `z3/docs/internals/zerops/verified.md:20`: white on `#00b1a3` = 2.61:1 (fails AA) → deepened `#007e72` (4.82:1) for text-on-teal.

### 1.4 Typography
- **[V]** GUI: Google Fonts `Roboto 300/400/500/700/900` + `Roboto Mono 400/500`, `Material Icons` + `Material Icons Outlined` (`apps/zerops/src/index.html`); local `assets/fonts/{Cascadia.ttf, DejaVuSansMono.ttf}` for terminal/mono. Rendered body font "Roboto, sans-serif" (`theme-tokens.json bodyFont`).
- **[V]** zerops.io: self-hosted `/fonts/geologica-latin.woff2` + `/fonts/jetbrains-mono-latin.woff2` (preloaded), Google Roboto 300–700 + Material Icons Outlined; CSS `--font-family-brand: Geologica, sans-serif` (headlines, `--mat-sys-display-*-font: Geologica`), `--font-family-plain: Roboto`, code `"JetBrains Mono", ui-monospace`.
- **[V]** docs.zerops.io: Inter (100–900) + Roboto Mono via `apps/docs/src/css/_variables.css`; served CSS also self-hosts JetBrains Mono 400; Docusaurus + Medusa theme, blue interactive `#3b82f6` (not brand teal).
- **[I]** For z3: Geologica for display/brand moments, Roboto (or system) for UI, JetBrains Mono for code/paths — matches marketing + docs and departs from T3's DM Sans.

### 1.5 Copy, taglines, tone
- **[V]** zerops.io H1: "Cloud platform for humans and their coding agents." Sub: "Develop and run production-grade apps from the first prompt." og:title identical. Meta description: "The platform for developers who want to be enhanced, not replaced. Coding agents build, prove, and ship production-grade software on real infrastructure." H2s: "Run your coding agent on Zerops, not your laptop…", "We started with the platform. Your coding agent becomes its power user.", "No token reselling here. Your agent runs on your own subscription.", "Bring your own tech stack…". Nav: Platform | Pricing | Blog | Docs | Status | Log-in. CTAs: "Try with $65 credit", "Get started", "Onboard with your coding agent", "Explore the platform", "See pricing". WebFetch also surfaced "The whole environment, provisioned as it's asked for."
- **[V]** GUI meta: "Zerops builds, deploys, runs and manages your apps on a developer-first cloud platform."; og:title "Zerops ~ zerops.io"; login subtitle "Developer-first cloud platform".
- **[V]** Docs: title "Zerops", tagline "Explore and learn how to use Zerops", meta "Get started with the Zerops cloud platform with our knowledge base."
- **[V]** Copy-voice rule already written for the container panel, `zcp/docs/spec-welcome-mode.md §6`: "every line is written from the point of view of a developer seeing the surface for the first time — internal state vocabulary, our architecture as explanation, and positioning statements are out."
- **[I]** Tone: short declarative sentences, second person, no exclamation marks, concrete nouns (databases, queues, balancers), anti-hype ("No token reselling here"), "developer-first" as the one self-descriptor. T3's voice ("Steal our code (legally)", "Tolerated by over 100,000 devs") is jokier; z3 copy should follow Zerops, not T3.

## 2. Asset inventory usable by z3

All `frontend-legacy` assets are Zerops' own code. Third-party marks carry their own terms (noted).

| Asset | Path | Notes |
|---|---|---|
| Mark (colour) | `libs/zui/src/logo/logo.component.html` + `.scss` | 2 fills; swap `fill` for mono |
| Wordmark + mark | `libs/zui/src/logo-full/…` | |
| Mark, docs colour/mono | `zerops-docs/apps/docs/static/{logo,black-logo}.svg` | 237×284 |
| Official pack | `zerops-docs/apps/docs/static/brand/zerops-assets.zip` (21 KB) | branding page `content/company/branding.mdx`: "Zerops Logo", "Zerops Icon", badges "Deploy in Zerops", "Powered by Zerops"; rules: don't modify, keep spacing, use light/dark versions, no implied endorsement, not as your own brand |
| Favicons | `apps/zerops/src/assets/meta/{android-chrome-192x192,android-chrome-512x512,apple-touch-icon,favicon-16x16,favicon-32x32,mstile-150x150}.png`, `safari-pinned-tab.svg`, `site.webmanifest` (theme `#ffffff`, name empty), `browserconfig.xml` (TileColor `#2b5797` = generator default, not brand) | `favicon.ico` at app root |
| Docs favicon/logos | `static/favicon.ico`, `static/img/logo-icon{,-dark}.png`, `logo-mobile{,-dark}.png` | |
| **Service/tech icons (87)** | `libs/zef/src/shared-assets/custom-icons/*.svg`, registry `custom-icons.constant.ts`, registered in `apps/zerops/src/modules/app/app.container.ts:37,195` as `/assets/shared-assets/custom-icons/<name>.svg` → `mat-icon [svgIcon]` | adonisjs, alpine, analog, angular, astro, bun, deno, directus, discourse, django, dotnet, echo, elasticsearch, elixir, express, filament, ghost, gleam, go, java, keydb, laravel, laravel-jetstream, linux, lxc, mariadb, medusa, meilisearch, meteor, mongodb, mysql, nats, nestjs, nette, nextjs, nginx, nodejs, nuxtjs, nx, opensearch, outline, payload, phoenix, php, postgresql, python, qdrant, qwik, rabbitmq, react, redis, remix, ruby, rust, solid, spring, sqlite, strapi, svelte, symfony, twill, typesense, ubuntu, umami, valkey, vue/vuejs; plus git `branch`, `commit`; social github, gitlab, google, discord, slack, telegram, x, twitter, youtube, linkedin, twitch, whatsapp, trello; payment visa, mastercard, american-express, discover, stripe |
| Dead assets | `apps/zerops/src/assets/services/*.svg` (docker, elasticsearch, go, mariadb, mongodb, nginx, nodejs, php, rabbitmq, redis) | referenced nowhere in `apps/zerops/src` or `libs` — ignore |
| Observability logos | `apps/zerops/src/assets/advanced-observability/*` (grafana, prometheus, betterstack, signoz…) | |
| **Provider marks** | `libs/zui/src/{claude-mark,codex-mark,cursor-mark,grok-mark,antigravity-mark,ubuntu-mark,vscode-mark}/*.component.ts` | inline SVG, `fill="currentColor"`, `1em`, standalone. Licences in doc-comments: codex = SimpleIcons "openai" CC0-1.0; cursor = official `cursor.com/favicon.svg` glyph; grok + antigravity = Boxicons MIT. claude-mark and ubuntu-mark carry no source comment |
| Agent display names | `apps/zerops/src/modules/core/zerops-services/zerops-services.model.ts:19-26` | 'Claude Code', 'opencode.ai' (not surfaced), 'Codex', 'Antigravity', 'Grok Build', 'Cursor CLI' |
| Agent strip states | `libs/zui/src/agent-strip/agent-strip.component.ts` | pending (grey), active (accent underline), done (green check), failed (red x), skipped (dash, struck) — 32 px logo, 14 px status row, label |
| Status icon | `libs/zui/src/status-icon-base/*.scss`, `service-stack-status-icon` | coloured dot, 8 px uppercase label, `pulse` for transitional, `ripple` |
| Control-plane chip | `libs/zui/src/control-plane-mini-card/…` | `mat-icon tune` + "Control Plane" + mode badge; bg `#e8f7ec` |
| T3 harness marks | `z3/apps/marketing/public/harnesses/{claude-ai-icon,cursor_light,grok-dark,openai_dark,opencode-dark}.svg` | replace with zui marks |
| T3 brand (to remove) | `z3/apps/mobile/src/components/{BrandMark,T3Wordmark}.tsx`, `apps/web/public/{favicon*,apple-touch-icon.png,manifest.webmanifest}` (theme `#161616`), `apps/web/index.html` (`theme-color #0a0a0a`, splash accent `#4f46e5/#818cf8`, default theme "t3-chat" oklch pinks), `apps/desktop/src/app/DesktopEnvironment.ts:88 APP_BASE_NAME = "T3 Code"` | |

**[V]** How the GUI actually shows types: service cards (`05-project-detail-full.png`) are text-only — "Node.js v22.22.3", "Zerops Control Plane v1", hostname `zcp:8080` with port in grey, chips "Domain public" / "Control Plane"; no per-type logo on cards. Tech icons appear only in `zui-service-stack-tile` (`techIcons` via `svgIcon`) and the marketing-style `zerops-project-visual`. **[I]** z3's service map may therefore use the 87-icon set freely — the GUI does not establish an icon-per-row convention, so either choice is consistent.

**[V]** The GUI is desktop-only: `<meta name="viewport" content="width=1200" />` (`apps/zerops/src/index.html`); the "mobile" screenshots (`12-mobile-dashboard.png`) are the desktop layout scaled. There is no Zerops mobile design precedent.

## 3. Naming and continuity

### 3.1 What the GUI calls things [V]
| Surface | Copy | Source |
|---|---|---|
| Service type | "Zerops Control Plane" (`zcp` category) | `service-stack-type-field.translations.ts:26` |
| Type-version | "Zerops Control Plane v1" | screenshots 03/05 |
| Card chip | "Control Plane" (icon `tune`) | `control-plane-mini-card.component.ts` |
| Add toggle | "Add Zerops Control Plane (ZCP) service"; icon row Ubuntu + VS Code + agents | `libs/zui/src/zcp-toggle/zcp-toggle.component.ts` |
| Project sidebar card | "ZCP — Zerops Control Plane": "Linux dev containers wired into the project private network with SSHFS mounts to your services and a Zerops MCP server + zcli inside. Add one or more — each becomes its own AI coding-agent or Cloud IDE workspace, accessible via SSH, web terminal, the Cloud IDE, or your own IDE over VPN." | `05-project-detail-full.png` |
| Service detail | "Linux dev container in the project private network — SSH, web terminal, Cloud IDE, optional coding agent, with Zerops MCP + zcli inside." | `service-stack-detail.page.html:178` |
| Buttons | "Web Terminal", "SSH (Terminal)", "Cloud IDE", "Desktop IDEs" | `zagent-service-card.feature.html:98-127` |
| Agent row | "Claude Code" + green dot + "AUTHORIZED"; header "Authorize agent"; "+" add | screenshot 05; template State A–C |
| Mounts | "Connected services" chips | `zagent-service-card.feature.html:70` |
| IDE page | "Cloud IDE (code-server) — Embedded code-server instance running on your control plane." / "Open code-server in New Window" | `06-service-detail-full.png` |
| Dialog | "Include Coding Agents", "Include Cloud IDE", "Deploy Control Plane Service"; modes `'ai-agent' \| 'human'` | `control-plane-dialog.feature.html:45-624`, `zerops-services.utils.ts:94` |
| Wizard | "Onboard your coding agent" | `zcp-onboard-wizard.component.ts:45` |
| Routes | `/service-stack/{id}/control-plane`, `/terminal` | `links-project.json` |
| Container panel (zcp) | activity-bar container **Zerops**, command "Zerops: Open Panel", "Data Studio", "Skill packs", "Guided", rows Authorize / Open terminal / Open extension / Try again / "+ Add another agent" | `spec-welcome-mode.md §1.4, §6` |
| z3 in zcp | init step "Zerops Code (z3)"; picker button "Enable Zerops Code"; client label `Zerops Code · <browser> on <os>`; grant label `Zerops <role>` | `spec-z3.md §2.1, §4.5, §4.8, §3.2` |

**[V]** Collision to resolve: T3 markets itself as "The open-source control plane for coding agents" (`z3/apps/marketing/src/pages/index.astro`, `Layout.astro` description). In Zerops, "Control Plane" is the container's product name, and `spec-z3.md` header states z3 "is not a second control plane: it is a reader". z3 must never call itself a control plane.

### 3.2 Glossary proposal — T3 term → Zerops-facing term [I, reasoning cites specs]
| T3 term | Zerops-facing | Why |
|---|---|---|
| environment | **project** (the Zerops project name) | D3: environment ⇔ Zerops project 1:1 (`brief §5`). Keep `environmentId` internal only |
| project (T3 workspace record) | invisible — one per Zerops project at `/var/www` | D3: "Services are not projects" |
| thread | **thread** (keep) | GUI has no equivalent; brief/spec use it ("thread Y also works on kanbandev") |
| turn / checkpoint | "checkpoint", grouped **by service** | `brief §7`, spec §6.4 multi-repo checkpoints |
| provider | **coding agent** / agent name (Claude Code, Codex…) | GUI: "coding agent", `AGENT_DISPLAY_NAMES`; marketing "your coding agent" |
| pairing / pairing code / pairing link | **Sign in with Zerops** (identity door); second device → "Connect another device" | §3.2 no pairing code; §3.5 second device keeps one-time-token path — keep the mechanism, drop the word |
| T3 Connect / Tailscale / relay / tunnel | removed | D5 out of scope: "VPN anywhere"; delivery is the public `/z3/` origin (§2.4) |
| worktree / "New worktree" / "Local checkout" | removed; isolation = **service** (or project) | D4: worktree mode off; "a parallel feature is a sibling dev service on a branch" |
| workspace root | "project files", tree grouped by **service hostname** (`/var/www/<host>`) | §6.1, brief §3 mount |
| "Add environment" / connect an environment | **Open project** / **New project** / candidate picker ("Enable Zerops Code" when old) | §4.2–4.7 |
| Settings → Connections | **Devices** (labels `Zerops Code · <browser> on <os>`) | §4.8 |
| "Commit & push" (T3 action) | delegated to zcp's git-push workflow or hidden | D4: "never two commit pipelines" |
| "Open" (editor) | **Cloud IDE** (code-server); map-row "Open" = subdomain URL | GUI button names; brief §7 |
| the `zcp` service | **Zerops Control Plane** / "control plane container", under **Infrastructure** in the map | GUI naming; §5.1 grouping |
| T3 home / base-dir | invisible | |
| stage pill "Alpha/Nightly/Dev" | keep the pill pattern; label per z3 channel | `BrandMark.tsx` |

**[V]** Footprint of the words to delete in the fork (`z3/apps/{web,mobile}/src`): "T3 Code" 49 files, "T3 Connect" 26, "Tailscale" 2, "pairing"/"Pairing" 40/28, "worktree"/"Worktree" 91/54, "environment"/"Environment" 480/413 (mostly identifiers — user-facing strings must be swept separately). Sample UI strings: "Enable T3 Connect", "Enter a pairing code.", "Pairing link — scan to open on another device", "New worktree", "Enable Tailscale HTTPS", "Open T3 Code in the desktop app…". Also delete: "app.t3.codes", "t3.gg", T3 tweets/endorsements, "Steal our code". **[V]** Provider list: GUI surfaces claude-code, codex, antigravity, grok, cursor (`ZCP_AGENTS` order); T3 also ships OpenCode — decide whether OpenCode appears in z3 (it has no zui mark).

## 4. Product-context constraints — what the UI must show

Sources: `zcp/docs/spec-z3.md` (§2–§5), `zcp/plans/z3-brief-2026-08-28.md` (§5 D3–D6, §7, §8), `z3/apps/web/src/zerops/strip.ts`, `z3/packages/contracts/src/zerops.ts`.

**Project model**
- One z3 environment = one Zerops project; `workspaceRoot = /var/www`; thread = one conversation = one agent PID = one zcp work session (brief D3). The UI header names the Zerops project, never "environment".
- Services are rows in a **service map**, not projects (D3). Two threads on one service are made visible ("thread Y also works on kanbandev"), never locked (D4). Bootstrap is exclusive per project: the strip says "infra session held by thread X" instead of a refusal (brief §7).

**Service map** (`spec-z3 §5.1`, contracts `ZeropsService`)
- Groups, in this order: **Runtimes / Data / Infrastructure** (brief S6; `ZeropsServiceGroup = ["runtimes","data","infrastructure"]`). Rule: `adoptionState === "zcp-self"` or type starts with `zcp` (OS prefix stripped) ⇒ infrastructure; `isInfrastructure` (managed data) ⇒ data; else runtimes (Z3F-2).
- Per row: hostname, type as the platform reports it (`nodejs@22`, `postgresql:single@18`), status (settled set ACTIVE/RUNNING/STOPPED/READY_TO_DEPLOY/FAILED/DELETED/ACTION_FAILED/CONTAINER_FAILED/REPAIR_FAILED; everything else `transient`), adoptionState (adopted/resumable/adoptable/managed-dep/zcp-self/bootstrapping — open vocabulary, default branch required), `mounted` + `mountPath`, `subdomainEnabled/Url` → "Open" chip; brief S6 adds live process, dev↔stage pair, prod project as a link.
- Feed availability is three things, not one boolean: `available:false` (no zcp binary → feed off, not an error), `degraded:true` (last-good rows kept, retrying), `doorbellConnected` tri-state (true/false/absent). A down doorbell renders **one quiet line**, not the degraded banner (§5.4). A service created from the web GUI appears without refresh (S6 acceptance).

**Lifecycle strip** (`§5.2, §5.4`; phrases pinned in `strip.ts` and asserted verbatim in `strip.test.ts`)
- Mounts **beside** `ChatHeader`, not inside (needs pending-question state). Tones: `idle | active | waiting | done`.
- Precedence: "waiting for you" (pending question) › "<tool> running" › phase. Phases (`zcp/internal/workflow/envelope.go:69-80`): `idle` → "no services yet" / "infrastructure ready · N services"; `bootstrap-active` → "setting up infrastructure · <step>"; `develop-active` → "developing <hosts>" or per host "<host> pending / deployed ✓ / deployed ✓ verified ✓ / deployed ✓ verify failed / deploy failed" joined by " · "; `develop-closed-auto` → "task complete"; `strategy-setup` → "choosing how to deploy"; `export-active` → "exporting the project"; `launch-production-active` → "launching production"; unknown phase → shown verbatim (never blank).
- The latest envelope per thread is persisted (migration 044): a reopened thread shows the strip after a restart; verified live through a mid-turn unit restart (`verified.md`).

**Cards** (brief D5/S6, `§5.3–5.4`)
- Set: plan → confirm; import with per-service progress; mount; deploy with build progress → URL chip; verify (`HTTP 200 ✓`); subdomain; structured errors (live: verify card `s3git1 · healthy` with two green checks; deploy-error card `SSH_DEPLOY_FAILED` with log lines, hint, `network` badge).
- Every card is a total decoder; undecodable / absent / over-cap result → the generic tool block (Z3F-7). Result text cap 48,000 bytes, dropped whole (Z3F-6). Cards currently render inside the collapsed "Used N tools" group — whether Zerops cards escape the fold is an open product call (`verified.md`).
- **Credential card rule**: a PAT / launch token goes card → z3 server → service secret through a zcp verb, never through the conversation (brief §4 rule 4, §7, §8.3; verb shape is owner-designed).

**Question cards** (brief §8, `§5.4`)
- Built on `user-input.requested/resolved` with `UserInputQuestion {question, header?, options[{label, description}], multiSelect?}`; single/multi select, option descriptions, a visible **"Other"** free-text answer, digit keys + arrow-key navigation, a visible "waiting for you" state in the thread and the strip. Under z3 zcp asks structurally (`ZCP_HARNESS=z3`): plan confirmation, route choice, close-mode, launch scope, blockers. Credentials are never questions.

**Quick actions** — prefill the composer only ("Deploy", "Show logs", "Add Redis"); the module graph imports no mutating RPC (Z3F-7). Hard rule: the client renders state, the agent mutates through MCP; read-only or caller-bound surfaces are allowed (open URL, files via mount, terminal, logs, Data Console).

**Local vs hosted client** (`§3 "Outside a Zerops project, nothing changes"`, `§4`, D6)
- Hosted web is the product (separately hosted static bundle); desktop = same bundle in Electron (one smoke build); mobile inherits via `packages/client-runtime`.
- Hosted flow: sign in (login/refresh shape difference, TOTP) → candidate picker (by service type `zcp@`, every org, each container a separate candidate, unavailable reason named) → registration (Turnstile mandatory; captcha failure → "sign up at app.zerops.io and come back to sign in") → provisioning waiter `awaiting-project (60 s) → awaiting-container (300 s) → awaiting-health (30 s) → ready`, plus `pool-exhausted` (a state, not a failure), `needs-enable`, `timed-out` (retryable) → readiness `ready | initializing | predates-z3 | unreachable` (5xx always `unreachable`; pre-z3 and unreachable look identical → both get "Enable Zerops Code") → identity connect → thread with the onboarding prompt **composed, not sent** (once per identity-door environment).
- Outside a Zerops project every seam is inert: pairing-code onboarding must stay reachable (poc-findings: keep `ManualConnectFallback`).

**Restart = upgrade** (`§2.5–2.6`)
- A service restart re-runs `install.sh` and replaces zcp; ~17 s to `/healthz`, ~19 s to z3 up, ~14 s of L7 502 in between → poll, render 502 as "restarting", cap 30 s. Restart keeps `~/.t3` (threads); **redeploy loses thread history** — "a client surfaces that first". `initAt` moving = container re-initialised.

**Identity window & sessions** (`§3.3, §3.5, §4.6`)
- Identity-door sessions cap at `T3CODE_ZEROPS_MEMBERSHIP_TTL_SECONDS` (default 900 s); the client **silently re-mints** with the Zerops token it holds — the user must never see a 15-minute logout; a removed member loses access within one window. One-time-token (second-device) sessions keep upstream lifetime. No browser cookie, no admin link; the Zerops token appears in exactly one request-body field.

**Devices** (`§3.5, §4.8`) — a signed-in member can connect a second device via the ordinary one-time-token path; client labels `Zerops Code · <browser> on <os>` tell two devices on one container apart. No numeric cap is stated in the spec.

**Delivery** (`§2.4, §2.7`) — z3 is served under `/z3/` on the container's 8080 origin only; code-server `/proxy/3773/` is closed; desktop origins `t3code://app`, `t3code-dev://app` are allowlisted (rename out of scope per D6 notes).

**Landing defects to design around** (`verified.md`) — auto-bootstrap names the project `www` and the first thread defaults to Codex (unauthenticated on these containers); the model picker reads "No models found" until typed. UI should default to the first authenticated agent.

## 5. Reference framing (from training knowledge — labelled, not browsed)
- Claude Desktop / Claude Code app: chat-first, per-folder sessions, no infrastructure view.
- Codex App: repo → thread list, worktree isolation, diff review, cloud sandboxes without a service topology.
- Cursor: editor-first; background agents as tabs; infrastructure absent.
- Conductor: parallel Claude Code workspaces, one git worktree each, kanban of agents — its isolation unit is exactly what D4 removes.
- T3 Code ([V] `shots/t3/marketing-updated.png`): projects → threads sidebar with status ("Working", "Completed"), "Add action / Open / Commit & push", changed-files tree with ±counts, composer with model/effort/mode/access pickers, footer "Local checkout · main".
- Vercel: deployment-centric project → deployments list; agents as review/generation add-ons.
- Railway: canvas service map — boxes with type icons + status, grouped per environment — the closest visual precedent for a service map.
- Render: service list with status pills, blueprints; no agent.
- Position for z3: a T3-shaped thread surface plus what none of the agent apps have — a **live project map and lifecycle strip** of real services — and what cloud consoles have without an agent. The isolation unit is a service, not a worktree.
- Naming: "Zerops Code" pairs with "Zerops Control Plane" the way marketing already pairs "humans and their coding agents" with the platform; avoid "control plane" for the client.

## Key facts
- Zerops mark = 4-path SVG, viewBox 0 0 42.27 50.48; left half #3cbdb2 (c-logo-theme-main), right half #00b1a3 (c-logo-theme-secondary); the same inline SVG is served on zerops.io — _/Users/macbook/Documents/Zerops-MCP/frontend-legacy/libs/zui/src/logo/logo.component.{html,scss}; scratchpad zerops-io.html_
- Wordmark ZEROPS + mark: custom letter paths, viewBox 0 0 175.99 36.25, same two teals — _/Users/macbook/Documents/Zerops-MCP/frontend-legacy/libs/zui/src/logo-full/logo-full.component.html_
- Docs mono/colour marks: logo.svg fills #3DB1A2/#24A492, black-logo.svg #323232 (237x284); official pack zerops-assets.zip; branding rules in content/company/branding.mdx — _/Users/macbook/Documents/Zerops-MCP/zerops-docs/apps/docs/static/{logo,black-logo}.svg, static/brand/zerops-assets.zip, content/company/branding.mdx_
- GUI Material palette: primary #00ccbb, accent #0077cc, background #eceff3, backgroundDark #00100f, identity red #cc0011 / green #00cc55 / purple #cc0077 / pink #bb00cc / black #1a1a1a; fonts Roboto + DejaVu Sans Mono — _/Users/macbook/Documents/Zerops-MCP/frontend-legacy/libs/zef/src/styles/_theme.scss_
- Dark theme ramp: canvas #0c0f0e, surface #141918, raised #1b2220, overlay #1e2624, text #e9eeec, secondary #9faea9, faint #5e6e69, mint accent #00e5c0 (fill rgba(0,229,192,.14), text #58efd4) — _/Users/macbook/Documents/Zerops-MCP/frontend-legacy/libs/zef/src/styles/base/_dark-theme.scss header comment; apps/zerops/src/index.html pre-boot bg_
- Live GUI tokens: --color-zerops-dark #02b1a3, --color-zerops-mid #3cbdb2, --color-zerops-light-green #bcfffa, --z-app-background #eceff3, --bu 24px, --max-width 1400px, body font Roboto — _/private/tmp/claude-501/-Users-macbook-Documents-Zerops-MCP-zcp/b6078889-5914-4f08-8f42-fddca6fdc03e/scratchpad/shots/gui-nonmember/theme-tokens.json_
- zerops.io fonts: Geologica (brand/display, self-hosted woff2), Roboto (plain), JetBrains Mono (code); M3 tokens --mat-sys-primary light-dark(#00a49a,#8effee), primary-container light-dark(#c6fff6,#02837c); --background-dark #00100f; --alt-card-accent #15D7C4 — _curl https://zerops.io + /assets/index-DR1ArQbs.css, 2026-08-28 (scratchpad zerops-io.css)_
- White on brand teal #00b1a3 = 2.61:1 (fails AA); deepened #007e72 = 4.82:1 — _/Users/macbook/Documents/Zerops-MCP/z3/docs/internals/zerops/verified.md:20_
- Marketing copy: H1 'Cloud platform for humans and their coding agents.' sub 'Develop and run production-grade apps from the first prompt.'; meta 'The platform for developers who want to be enhanced, not replaced…'; GUI meta 'developer-first cloud platform'; login subtitle 'Developer-first cloud platform' — _scratchpad zerops-io.html meta/h1; frontend-legacy/apps/zerops/src/index.html; shots/gui-nonmember/01-login.png_
- 87 tech/service SVG icons (nodejs, postgresql, valkey, redis, mariadb, mongodb, elasticsearch, rabbitmq, nats, qdrant, ubuntu, go, php, python…) registered as mat-icon svgIcon from /assets/shared-assets/custom-icons/<name>.svg — _/Users/macbook/Documents/Zerops-MCP/frontend-legacy/libs/zef/src/shared-assets/custom-icons/ + custom-icons.constant.ts; apps/zerops/src/modules/app/app.container.ts:37,195_
- apps/zerops/src/assets/services/*.svg (10 logos) is referenced nowhere — dead — _grep -rn 'assets/services' apps/zerops/src libs → no hits_
- Provider marks are currentColor inline-SVG Angular components: claude, codex (SimpleIcons openai CC0), cursor (official favicon glyph), grok + antigravity (Boxicons MIT), ubuntu, vscode; display names Claude Code / Codex / Antigravity / Grok Build / Cursor CLI (opencode.ai not surfaced) — _/Users/macbook/Documents/Zerops-MCP/frontend-legacy/libs/zui/src/*-mark/*.component.ts; apps/zerops/src/modules/core/zerops-services/zerops-services.model.ts:19-26_
- GUI names: service type 'Zerops Control Plane' (zcp), chip 'Control Plane', toggle 'Add Zerops Control Plane (ZCP) service', buttons 'Web Terminal' / 'SSH (Terminal)' / 'Cloud IDE' / 'Desktop IDEs', agent row 'AUTHORIZED', 'Connected services', modes ai-agent|human, wizard 'Onboard your coding agent' — _service-stack-type-field.translations.ts:26; libs/zui/src/zcp-toggle; feature/zagent-service-card/zagent-service-card.feature.html:98-127; zerops-services.utils.ts:94; zcp-onboard-wizard.component.ts:45; shots 05/06_
- T3 markets itself as 'The open-source control plane for coding agents' — collides with Zerops Control Plane; spec-z3 header says z3 'is not a second control plane: it is a reader' — _/Users/macbook/Documents/Zerops-MCP/z3/apps/marketing/src/pages/index.astro + layouts/Layout.astro; /Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md lines 1-10_
- T3 vocabulary footprint in fork UI code: 'T3 Code' 49 files, 'T3 Connect' 26, Tailscale 2, pairing 40+28, worktree 91+54; desktop APP_BASE_NAME = 'T3 Code' — _grep over /Users/macbook/Documents/Zerops-MCP/z3/apps/{web,mobile}/src; apps/desktop/src/app/DesktopEnvironment.ts:88_
- Service map taxonomy runtimes|data|infrastructure with zcp-self/zcp-type ⇒ infrastructure, isInfrastructure ⇒ data, else runtimes; per-row hostname/type/status/adoptionState/mounted/mountPath/subdomainUrl; availability = available/degraded/doorbellConnected tri-state — _/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md §5.1, Z3F-1/Z3F-2; /Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/zerops.ts:46-120_
- Strip phrases pinned: 'no services yet', 'infrastructure ready · N services', 'setting up infrastructure · <step>', 'developing <hosts>', '<host> deployed ✓ verified ✓ · <host> pending', 'task complete', 'choosing how to deploy', 'exporting the project', 'launching production', 'waiting for you', '<tool> running'; phases idle/bootstrap-active/develop-active/develop-closed-auto/strategy-setup/export-active/launch-production-active — _/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/strip.ts; /Users/macbook/Documents/Zerops-MCP/zcp/internal/workflow/envelope.go:69-80_
- Cards are total decoders falling back to the generic tool block; result text capped at 48,000 bytes dropped whole; quick actions only prefill the composer; credential card routes token card→server→service secret, never chat — _spec-z3.md §5.3-5.4, Z3F-6/7; plans/z3-brief-2026-08-28.md §5 D5, §7, §8.3, §4 rule 4_
- Restart = upgrade: ~17 s to /healthz, ~19 s to z3 up, ~14 s of 502 to render as 'restarting', cap 30 s; redeploy loses ~/.t3 thread history — _/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md §2.5-2.6_
- Identity-door sessions cap at 900 s and the client re-mints silently; one-time-token second-device sessions keep upstream lifetime; client label 'Zerops Code · <browser> on <os>'; no browser cookie — _/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md §3.3, §3.5, §4.8_
- Hosted client flow states: candidates by service type zcp@ (every org, each container separately); provisioning awaiting-project(60s)→awaiting-container(300s)→awaiting-health(30s)→ready plus pool-exhausted/needs-enable/timed-out; readiness ready|initializing|predates-z3|unreachable with 'Enable Zerops Code' for both latter; first prompt composed not sent — _/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md §4.2-4.8_
- Zerops GUI is desktop-only (viewport width=1200); mobile screenshots are scaled desktop — no Zerops mobile precedent — _/Users/macbook/Documents/Zerops-MCP/frontend-legacy/apps/zerops/src/index.html; shots/gui-nonmember/12-mobile-dashboard.png_
- Container panel copy rule: every line written for a developer seeing the surface the first time; no internal state vocabulary, architecture-as-explanation, or positioning — _/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-welcome-mode.md §6_

## Gaps
- zerops.io @font-face declarations (weights/axes of Geologica and JetBrains Mono) were not extracted — only the preload links and font-family vars; open scratchpad zerops-io.css and search '@font-face' or fetch /fonts/geologica-latin.woff2 headers to confirm variable-font axes.
- The brand mark's fills on zerops.io in dark mode were not found in the CSS bundle (no .c-logo rule; likely inline component styles inside the JS bundle) — inspect the live DOM computed fill on zerops.io in dark scheme.
- docs branding page references preview PNGs /img/brand/{zerops-logo,zerops-icon,deploy-badge,powered-badge}-preview.png that are absent from zerops-docs/apps/docs/static/img; zerops-assets.zip was not unpacked — unzip it in the scratchpad to see the official badge/logo files.
- claude-mark and ubuntu-mark components carry no source/licence comment — confirm provenance (path matches SimpleIcons 'claude'; Ubuntu 'circle of friends' is Canonical trademark) before shipping in a public client.
- theme-tokens.json captured only the light :root (htmlClass ""); dark values here come from the _dark-theme.scss header comment, not a live dump — re-run the token capture with html.zef-dark-theme set.
- No numeric 'two devices per container' cap exists in spec-z3; §3.5/§4.8 only guarantee a second device path and per-device labels; S0.9 in the ledger (questions.md Q-07) tracks the open question.
- No formal Zerops brand guideline (clear-space, minimum size, misuse examples) exists in any repo — only the five bullets in branding.mdx; if needed, ask the owner for the design-team source (Figma).
- Whether OpenCode should appear in z3's agent list is undecided: T3 ships it, the GUI defines 'opencode-ai' but does not surface it and has no zui mark.
- The GUI's service cards show no per-type logos, so no convention exists for icon-per-row in a service map; the 87-icon set is available but its use in the map is a design choice, not a continuity requirement.
- Docs-site Medusa/Tailwind palette (--docs-fg-base etc.) was only sampled from the served CSS; the tailwind.config.js in packages/docs-ui was not read.
