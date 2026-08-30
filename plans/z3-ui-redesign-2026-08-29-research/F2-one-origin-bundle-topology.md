# Gap fill — one-origin bundle topology

## Summary
z3's web client is one Vite/React SPA (apps/web) that gets built up to 4 different ways from the same source: hosted-static (VITE_HOSTED_APP_CHANNEL/URL baked in, e.g. app.t3.codes), container-served under /z3/ (only VITE_BASE_PATH set), desktop/Electron (custom t3code:// protocol, no base path), and mobile (separate Expo app, Clerk auth, out of this web lineage). The critical finding: the ONLY bundle that shows the Zerops sign-in/sign-up/project-picker landing (ZeropsHostedLanding) is the 'hosted-static' build, gated by a BUILD-TIME env check (isHostedStaticApp() in hostedPairing.ts) — not by any server capability. The container-served /z3/ bundle (the one zcp actually deploys, per z3-dev-push.sh) never shows that landing: an unauthenticated visitor gets redirected to the generic, non-Zerops-branded /pair token-paste screen. The server ALREADY reports a usable capability signal for a runtime-keyed rewrite: ServerAuthDescriptor.bootstrapMethods includes 'zerops-identity' only when the server runs in Zerops mode (auth.ts:53-62), fetched pre-render via fetchSessionState/resolveInitialServerAuthGateState. Today nothing in __root.tsx reads it for UI branching — only isHostedStaticApp() does. Turnstile's site key is hostname-bound server-side by Cloudflare/Zerops, so wherever the Zerops sign-up form actually renders (today only the hosted-static build) must be an allowlisted origin — a Zerops-branded container origin (*.prg1.zerops.app) is very unlikely to already be allowlisted. Brand assets are NOT neutral today: apps/server/scripts/cli.ts's pack step unconditionally bakes T3's own production/nightly favicon+apple-touch-icon into dist/client (preparePublishIcons, keyed only on semver shape), and z3-dev-push.sh's version string never matches '-nightly.', so every z3-eval container currently ships T3's real black favicon and apple-touch-icon — confirmed by code path, not assumption. Three faces share the container's one 8080 origin (code-server welcome/agent-panel at '/', z3 at '/z3/', Data Console standalone via code-server's /proxy/<port>/): all three source their palette from CSS custom properties (--vscode-*, Tailwind zinc/indigo oklch, or VS Code theme vars) with zero shared tokens and zero use of Zerops' own zui/zef Roboto+Material+teal/blue system — a 'looks like Zerops' rule has to be invented fresh, not aligned to an existing shared layer.

## Report
## 1. Decision table — bundle variants

### 1a. How each variant's auth-gate state is produced

| Variant | Build-time env that shapes it | Runtime auth-gate producer | Verified? |
|---|---|---|---|
| **Hosted-static** (e.g. `app.t3.codes`, or a future Zerops CDN URL) | `VITE_HOSTED_APP_CHANNEL` and/or `VITE_HOSTED_APP_URL` set (`apps/web/vite.config.ts:50,56-65`); `VITE_HTTP_URL`/`VITE_WS_URL` **unset** (no co-located backend) | `isHostedStaticApp()` (`apps/web/src/hostedPairing.ts:32-42`) returns **true** → `__root.tsx:68-71` sets `authGateState:{status:"hosted-static"}` — a pure URL/env check, no network call | Verified in code |
| **Container-served, `/z3/`** (what zcp actually deploys) | Only `VITE_BASE_PATH` set (`eval/scripts/z3-dev-push.sh:78-85`, zcp repo) | `isHostedStaticApp()` returns **false** → `__root.tsx:73-77` calls `resolveInitialServerAuthGateState()` → `bootstrapServerAuth()` (`apps/web/src/environments/primary/auth.ts:320-343`): a bare browser visit yields `{status:"requires-auth", auth}` where `auth.bootstrapMethods` is the server's `ServerAuthDescriptor.bootstrapMethods` (`packages/contracts/src/auth.ts:53-62`, includes `"zerops-identity"` when `T3CODE_ZEROPS_PROJECT_ID` is set, `docs/spec-z3.md §3.1`, zcp repo) | Verified in code |
| **Desktop** (`t3code://app`) | No `VITE_BASE_PATH`, no hosted vars at the electron-protocol layer | `getDesktopBootstrapCredential()` supplies a trusted local handoff (`auth.ts:321,326-337`) → resolves to `{status:"authenticated"}` without showing a pairing UI in the common case | Verified in code (`apps/desktop/src/app/DesktopApp.ts:176-186`, `apps/desktop/src/electron/ElectronProtocol.ts:205-238`) |
| **Mobile** (Expo, `apps/mobile`) | Separate app; auth via Clerk (`relyingParty: "clerk.t3.codes"`, `apps/mobile/app.config.ts:70-88`) | N/A — not the web `ServerAuthGateState`/`__root.tsx` machinery; brief only asserts it "inherits D1 through the shared client runtime" | Inferred from brief; mobile auth code not inspected |

### 1b. First screen — new vs returning user (container-served, the actually-deployed target)

- **New user, direct visit to `https://<container>.prg1.zerops.app/z3/`:** `authGateState.status==="requires-auth"`. `__root.tsx:113-119` renders a bare `<Outlet/>`. Route-level guards (`_chat.tsx:192`, `projects.$projectKey.tsx:11`, `settings.tsx:103`, all `throw redirect({to:"/pair"})`) send them to `/pair` → **`PairingRouteSurface`** (`components/auth/PairingRouteSurface.tsx:42-166`): generic "Pair with this environment" / "Enter a pairing token to start a session with this environment." **No Zerops branding, sign-in, or sign-up appears.** Not screenshotted (READ-ONLY, no container probed).
- **New user via the discovery/hosted bundle's `connectZeropsIdentity` flow** (signed in on the hosted-static bundle, picked a project, that bundle POSTs the Zerops token to `/api/auth/zerops-identity`): lands on `/` already `authenticated` — no `/pair` screen shown. Only path today by which a container-bundle user arrives pre-authenticated with Zerops identity, and it requires the other bundle to exist and be visited first.
- **Returning user with an existing session:** `fetchSessionState()` returns `{status:"authenticated"}` (`auth.ts:322-325`) directly.
- **Hosted-static, new user, `environmentCount===0`:** `resolveChatIndexView` (`apps/web/src/routes/-chatIndexView.ts:8-17`) → `"zerops-onboarding"` → `_chat.index.tsx:28-36` renders **`ZeropsHostedLanding`** — sign-in/sign-up (Turnstile-gated) → `ZeropsProjectsPage` → provisioning wait → `connectZeropsIdentity`. Reachable **only** on this variant.

### 1c. Where the Zerops landing vs `/pair` appears

| Surface | Hosted-static? | Container `/z3/`? |
|---|---|---|
| `ZeropsHostedLanding` | Yes (`-chatIndexView.ts`) | **No** — gate never reaches `"hosted-static"` |
| `/pair` `PairingRouteSurface` (generic token box) | No (redirected away, `pair.tsx:14-16`) | **Yes** — actual first screen |
| `/pair` `HostedPairingRouteSurface` (host+token query pairing) | Fires whenever URL is exactly `/pair?host=&token=` (`__root.tsx:63-68`) | Same |

### 1d. Can Turnstile's hostname binding hold?

- The site key (`DEFAULT_ZEROPS_TURNSTILE_SITE_KEY="0x4AAAAAABkfI4SNvJav8428"`, `apps/web/src/zerops/turnstile.tsx:20`) is Zerops-owned and hostname-allowlisted server-side by Cloudflare; an unlisted origin gets error `110200` (`turnstile.tsx:12-14,38`), surfaced via `ZeropsRegistrationUnavailable` with hand-off to `app.zerops.io` (`ZeropsHostedLanding.tsx:112-140`).
- Today the only place this form renders is the hosted-static bundle, whose configured origin defaults to `https://app.t3.codes` (`packages/shared/src/connectAuth.ts:17`) — not a Zerops-controlled hostname (not verified whether Zerops has allowlisted it; no live probe made).
- If sign-up should instead be reachable on the container's own `*.prg1.zerops.app` origin, every per-project subdomain is a distinct hostname — Turnstile cannot bind per-container-subdomain at scale unless Zerops allowlists the whole wildcard, or registration stays confined to one fixed Zerops-owned domain while containers only ever do sign-in/identity-connect. **Open design question, not resolved in code.**

### 1e. What a single Zerops-aware bundle would change

| Change | File:line today | What it becomes |
|---|---|---|
| Root gate branch | `apps/web/src/routes/__root.tsx:64-70` — `isHostedStaticApp(url)` (build-time env) | Read `authGateState.status==="requires-auth" && authGateState.auth.bootstrapMethods.includes("zerops-identity")` (server already emits this, `packages/contracts/src/auth.ts:53-62`) |
| Chat-index landing gate | `apps/web/src/routes/-chatIndexView.ts:8-17` — keys on `authGateStatus==="hosted-static"` | Key on the same server-reported capability |
| Platform-discovery gating | `apps/web/src/connection/platform.ts:110-113,461-465` — `isHostedStaticApp()` turns off LAN discovery for hosted build | Keep as a separate, still env-driven axis — unrelated to Zerops identity |
| Turnstile/registration reachability | `apps/web/src/zerops/turnstile.tsx` hostname allowlist (Cloudflare-side) | Unaffected by a client capability change — still needs a Zerops-allowlisted hostname |
| Discovery-only concerns | `ZeropsProjectsPage.tsx`, `apps/web/src/zerops/candidates.ts:56` (`ZEROPS_CODE_BASE_PATH="/z3"` hardcoded), `containerHealth.ts` | Still meaningful only on whichever origin plays "the picker"; a self-showing container doesn't need to re-implement discovery of *other* containers |

**Net effect if implemented:** a container-served `/z3/` bundle, visited fresh, would show `ZeropsHostedLanding` (or a variant) instead of the bare-token `/pair` screen, without needing `VITE_HOSTED_APP_CHANNEL`/`URL` at all. Inferred design direction, not implemented anywhere today.

---

## 2. Second-device and non-Zerops-fallback story

### 2a. What retest step 8 exercises

`plans/z3-retest-2026-08-28.md:33` (zcp repo): "Second device: Settings → Connections → pairing code; pair a second browser profile; it stays connected past 15 minutes." Traced:

- `apps/web/src/routes/settings.connections.tsx` → `ConnectionsSettings` (`apps/web/src/components/settings/ConnectionsSettings.tsx`).
- `createServerPairingCredential` (~line 993, wrapping `auth.ts:359-378`) mints a grant on the current server.
- UI builds either (a) a same-origin `/pair?token=` link via `resolveDesktopPairingUrl` (`components/settings/pairingUrls.ts:6-11`, respects the container's `/z3` prefix via `withBasePath`), or (b) a **hosted-app** link via `resolveHostedPairingUrl`→`buildHostedPairingUrl` (`hostedPairing.ts:66-79`) that routes through `configuredHostedAppUrl()` — i.e. through `app.t3.codes`-shaped URL, not the target container's own origin.
- Second browser opening that URL → `hasHostedPairingRequest` true → `{status:"hosted-pairing"}` → **`HostedPairingRouteSurface`** (`PairingRouteSurface.tsx:172-278`) → `connectPairing`/`registerPairing` (`connection/onboarding.ts:12-25`) saves the environment locally.
- `docs/spec-z3.md §3.5` (zcp repo): a `one-time-token`-pairing session keeps upstream's lifetime (not capped at the 15-minute Zerops-membership window) — the mechanism behind "stays connected past 15 minutes."

### 2b. `poc-findings.md`'s `ManualConnectFallback` directive

`docs/internals/zerops/poc-findings.md:39`: replacing `HostedStaticOnboardingState` with `ZeropsHostedLanding` must keep upstream's manual-connect flow reachable behind it via `manualFallback`. Verified live: `ZeropsHostedLanding.tsx:33-44` renders `{manualFallback}` when `showManual` is set; only call site `_chat.index.tsx:36` passes `<HostedStaticOnboardingState/>`. Any future single-bundle UI must keep this one click away, never removed.

### 2c. Pairing copy — rewrite vs. stay

| Copy | Location | Verdict |
|---|---|---|
| "Pair with this environment" / "Enter a pairing token…" (`describeAuthGate`, `PairingRouteSurface.tsx:295-301`) | Needs a `zerops-identity` branch (e.g. "Sign in with your Zerops account") | **Rewrite** |
| `describeSupportedMethods` (`PairingRouteSurface.tsx:303-315`) | Same — needs a zerops-identity clause | **Rewrite** |
| "Pairing with this environment" / "Validating the pairing link…" (`PairingPendingSurface`, lines 17-30) | Generic transport-state copy | **Stays** |
| `HostedPairingRouteSurface` — "Pairing backend" / "Connecting to this backend." / "Verify the backend is reachable…supports CORS…" (lines 214-262) | Developer-register-a-server voice vs. Zerops' consumer voice elsewhere ("Pick a project and start talking to the agent inside it.") | **Optional rewrite** for voice; functionally generic/stays |
| "Pairing link" / "Show link" / "Copy code" / QR titles (`ConnectionsSettings.tsx:664-880`) | Pure transport-mechanics labels | **Stays** |
| `resolveHostedPairingUrl` routing through `configuredHostedAppUrl()` (`pairingUrls.ts:13-20`) | Hardcoded to a fixed hosted-app origin regardless of the target project's own picker bundle | Routing fix needed, not copy |

---

## 3. Inventory — three faces on one origin (container's 8080)

| | **code-server welcome / agent panel** | **Data Console** | **z3** |
|---|---|---|---|
| **Reach** | `location /` (code-server root), extension webview (`internal/content/templates/vscode-bootstrap-welcome.html`, zcp repo) | Embedded: native `WebviewPanel` (no HTTP path). Standalone: code-server's `/proxy/<port>/` forward (`docs/spec-dataconsole.md §4.1-4.2`, zcp repo) | `{BasePath}/` = `/z3/`, nginx-proxied to `127.0.0.1:3773/` (`docs/spec-z3.md §2.4`, zcp repo) |
| **Auth** | code-server cookie gate (`VSCODE_PASSWORD`) | Embedded: caller-bound write token inside the authenticated webview. Standalone: inherits code-server's cookie gate, read-only bearer only (`spec-dataconsole.md §4.2,§5.1`) | Own auth, outside the cookie gate (Z3D-4); `zerops-identity` door or generic pairing token |
| **Fonts** | `var(--vscode-font-family, -apple-system,…)` (`vscode-bootstrap-welcome.html:76`, zcp repo) | Same VS Code inheritance, independently re-declared (`internal/dataconsole/console/webui/dist/style.css:13`, zcp repo) | `--font-sans: -apple-system,…system-ui,sans-serif` (`apps/web/src/index.css:140`) — Tailwind default, not VS-Code-tied |
| **Palette source** | Bespoke `:root`: `--teal:#2bab9b`,`--teal-fg:#4fd8c6` over `--vscode-editor-background,#1e1e1e` (`vscode-bootstrap-welcome.html:32-58`, zcp repo) | `--bg:var(--vscode-editor-background,#1e1e1e)`, `--accent:var(--vscode-focusBorder,#4f9cff)`, `--btn:var(--vscode-button-background,#0e639c)` (`dist/style.css:3-13`, zcp repo) | Tailwind vars: `--background:var(--color-zinc-25)`, `--primary:oklch(0.488 0.217 264)` (indigo hue), full light/dark pairs (`apps/web/src/index.css:1391-1468`) |
| **Chrome pattern** | Single-column stack: header + agent rows + Data Studio box + skills/guided (`docs/spec-welcome-mode.md §6`, zcp repo) | `#topbar` + content area — minimal single-toolbar SPA | Full app shell: sidebar, command palette, toasts, thread view (`__root.tsx:118-142`) |
| **Embedded in / linked from z3?** | No relation — entirely separate surface | Brief says z3 should embed it (`plans/z3-brief-2026-08-28.md §5 D5`, `spec-welcome-mode.md §6` `zcpStudio.open`) — **no call site found in `apps/web/src`**; treat as planned, not built | Self |
| **Zerops brand alignment today** | Bespoke teal, not Zerops' `--color-zerops-dark:#02b1a3`/`--color-zerops-mid:#3cbdb2` (live `theme-tokens.json`) | VS Code blue accent, unrelated to Zerops' `--color-blue:#005cbb`/`--z-identityblue-green7:#0077cc` | Indigo/oklch; no Roboto, no Material Icons — furthest from Zerops' actual look (Roboto + Material + teal/blue, confirmed live: `bodyFont:"Roboto, sans-serif"`) |

**One "looks like Zerops" rule, stated from this inventory:** none of the three faces reference a shared token layer today — each hardcodes its own palette and inherits fonts from whatever host sets `--vscode-font-family` or defaults to system-ui. There is no existing bridge between Zerops GUI's `libs/zef`/`libs/zui` tokens and any of these three surfaces; a rule (e.g. one shared CSS custom-property set — Roboto stack, `#0077cc`/`#005cbb` blue + `#02b1a3` teal as the only accent hues) has to be invented, not discovered from an existing convention.

---

## 4. Brand-asset rename list

| Asset | File:line | Current value | Verified? |
|---|---|---|---|
| Favicon (dev build path — what z3-dev-push.sh produces) | `scripts/lib/brand-assets.ts:60-64` (`DEVELOPMENT_ICON_OVERRIDES`), applied by `apps/server/scripts/cli.ts:143-172` (`applyDevelopmentIconOverrides`, from `buildCmd`) | `assets/dev/blueprint-web-favicon*.png` | Verified |
| Favicon/apple-touch-icon (pack/publish path — what ships in the installed tarball) | `apps/server/scripts/cli.ts:85-111` (`preparePublishIcons`), `withReleaseAssets` (`:211-224`), `packCmd` (`:333-354`) | `resolveWebAssetBrandForPackageVersion` selects "production" unless version contains `-nightly.`; z3-dev-push.sh's `<pkg-version>-dev.<sha>` never matches → `assets/prod/t3-black-web-favicon.ico`/`-16x16.png`/`-32x32.png`/`t3-black-web-apple-touch-180.png` | **Verified**: every z3-eval container push bakes T3's real black favicon/apple-touch-icon |
| `apple-touch-icon.png` (also the boot-shell splash logo) | `apps/web/index.html:11` and `:463` (`<img id="boot-shell-logo" src="%BASE_URL%apple-touch-icon.png">`) | One file drives both the tab icon and splash | Verified |
| `manifest.webmanifest` | `apps/web/public/manifest.webmanifest:1-17` | `background_color`/`theme_color:"#161616"`; not brand-overridden by any script | Verified |
| `<title>` | `apps/web/index.html:459` | `T3 Code (Alpha)` (static; runtime title set separately via `head:()=>({meta:[{name:"title",content:APP_DISPLAY_NAME}]})`, `__root.tsx:87-89`) | Verified — two rename sites |
| Runtime display name | `apps/web/src/branding.ts:16-24`, format rule `branding.logic.ts:1-11` | `APP_BASE_NAME="T3 Code"`, reaches the tab title and desktop titlebar (confirmed live: `marketing-updated.png` shows "T3 Code · NIGHTLY") | Verified |
| `theme-color` meta | `apps/web/index.html:9` | `#0a0a0a` | Verified |
| og/meta/twitter tags | — | **None exist** in `apps/web/index.html` | Verified absence — nothing to rename, a gap if social cards are wanted |
| `apply-web-brand-assets.ts` channels | `scripts/apply-web-brand-assets.ts:17-21`, `scripts/lib/brand-assets.ts:35-38` | Only runs from `apps/web/vercel.ts:15` (Vercel hosted-web build) — the container path does NOT call this script | Verified — two independent brand-override code paths, neither has a "zerops" brand key |
| Desktop `appId` | `scripts/build-desktop-artifact.ts:56` | `DESKTOP_APP_ID="com.t3tools.t3code"` | Verified |
| Desktop `productName` | `scripts/build-desktop-artifact.ts:2123,2147` | `"T3 Code"`/`"T3 Code (Nightly)"` | Verified |
| Desktop `artifactName` | `scripts/build-desktop-artifact.ts:2148` | `"T3-Code-${version}-${arch}.${ext}"` | Verified |
| Desktop protocol/scheme names | `scripts/build-desktop-artifact.ts:2178-2183` (mac), `:2210-2216` (linux); allowlist `apps/server/src/zerops/origin.ts:31` | `schemes:["t3code","t3code-dev"]`, `StartupWMClass:"t3code"` | Verified; **brief marks scheme rename out of scope** (`plans/z3-brief-2026-08-28.md:222`) |
| Desktop DMG title/copy | `scripts/build-desktop-artifact.ts:2199` | Derives from `productName`; background PNGs are separate assets | Verified |
| Mobile identity | `apps/mobile/app.config.ts:66-88` | `appName`/`scheme`/`iosBundleIdentifier`/`androidPackage` all `t3tools`/`t3code` family; `relyingParty:"clerk.t3.codes"` (auth implication, not cosmetic) | Verified |

---

## Verified vs. inferred — summary

**Verified in code**: the entire auth-gate branch chain; the server-reported `zerops-identity` capability existing in the contract schema; the container build's env footprint; the origin/CORS allowlist; pairing-link generation and copy; every brand-asset file:line in §4 including the concrete fact that container packaging bakes T3's real production icon set; the three faces' font/palette sources via direct file reads.

**Inferred / not verified**: whether `app.t3.codes` is Turnstile-allowlisted (no live probe, READ-ONLY); whether mobile truly shares the zerops-identity door code path or only the phrase in the brief; Data Console's actual embed call site in z3 (not found by grep, treated as planned); no screenshot of the live container `/pair` screen (reasoned from route code only).

**Deliberately out of scope per the brief**: `t3code:` URL-scheme rename — desktop/mobile scheme and `origin.ts:31` allowlist entries stay as-is regardless of this concept's web-branding decisions.

## Key facts
- isHostedStaticApp() (apps/web/src/hostedPairing.ts:32-42) is a build-time env check, not a server capability read — it alone gates whether ZeropsHostedLanding can ever render (__root.tsx:64-70, routes/-chatIndexView.ts:8-17) — _apps/web/src/hostedPairing.ts:32-42; apps/web/src/routes/__root.tsx:64-70; apps/web/src/routes/-chatIndexView.ts:8-17_
- The container build (zcp's eval/scripts/z3-dev-push.sh:78-85) sets only VITE_BASE_PATH, never hosted-app vars, so isHostedStaticApp() is false there — a fresh visitor is redirected to the generic /pair token screen, never ZeropsHostedLanding — _eval/scripts/z3-dev-push.sh:78-85 (zcp repo); apps/web/src/routes/_chat.tsx:192, projects.$projectKey.tsx:11, settings.tsx:103; apps/web/src/components/auth/PairingRouteSurface.tsx:42-166_
- The server already reports a usable runtime capability: ServerAuthDescriptor.bootstrapMethods includes 'zerops-identity' only when T3CODE_ZEROPS_PROJECT_ID is set, fetched pre-render via fetchSessionState/resolveInitialServerAuthGateState — nothing client-side branches UI on it today — _packages/contracts/src/auth.ts:53-62,143-165; apps/web/src/environments/primary/auth.ts:320-343,510-528_
- apps/server/scripts/cli.ts's pack step (preparePublishIcons:85-111, via withReleaseAssets in packCmd) unconditionally selects the 'production' T3 icon brand unless the version string contains '-nightly.'; z3-dev-push.sh's dev version tag never matches, so every current z3-eval container ships T3's real black favicon/apple-touch-icon — _apps/server/scripts/cli.ts:85-111,211-224,333-354; scripts/lib/brand-assets.ts:38-41,60-64; eval/scripts/z3-dev-push.sh:105-114 (zcp repo)_
- Turnstile's Zerops site key is hostname-bound by Cloudflare server-side; an unlisted origin fails with error 110200 and hands the user to app.zerops.io — this form currently renders only on the hosted-static bundle, whose default origin (app.t3.codes) is not a Zerops-owned hostname — _apps/web/src/zerops/turnstile.tsx:12-14,20,38; apps/web/src/components/zerops/landing/ZeropsHostedLanding.tsx:112-140; packages/shared/src/connectAuth.ts:17_
- The second-device pairing link built in Settings→Connections routes through buildHostedPairingUrl/configuredHostedAppUrl() to a fixed hosted-app origin rather than to whatever Zerops-branded picker exists for that project — _apps/web/src/components/settings/pairingUrls.ts:13-20; apps/web/src/hostedPairing.ts:14,66-79_
- poc-findings.md's directive to keep ManualConnectFallback reachable is implemented: ZeropsHostedLanding renders manualFallback verbatim when showManual is set, wired from _chat.index.tsx's only call site — _docs/internals/zerops/poc-findings.md:39; apps/web/src/components/zerops/landing/ZeropsHostedLanding.tsx:33-44; apps/web/src/routes/_chat.index.tsx:36_
- All three same-origin faces (code-server welcome/agent panel, Data Console, z3) source palette/fonts independently and none references Zerops' zui/zef Roboto+Material+brand-teal/blue tokens — welcome panel uses a bespoke #2bab9b teal over VS Code vars, Data Console uses VS Code fallback blue #4f9cff/#0e639c, z3 uses Tailwind zinc + an indigo oklch primary — _internal/content/templates/vscode-bootstrap-welcome.html:32-58,76 (zcp repo); internal/dataconsole/console/webui/dist/style.css:1-13 (zcp repo); apps/web/src/index.css:140,1391-1468; theme-tokens.json_
- Data Console embedding inside z3 is asserted in the brief (D5, W-PANEL zcpStudio.open) but no call site for it was found in apps/web/src — treat as planned, not yet built — _plans/z3-brief-2026-08-28.md:214 (zcp repo); docs/spec-welcome-mode.md §6 (zcp repo); grep of apps/web/src found no reference_
- The t3code:// URL-scheme rename is explicitly out of scope per the brief, regardless of any web-branding decision — _plans/z3-brief-2026-08-28.md:222 (zcp repo); scripts/build-desktop-artifact.ts:2178-2183,2210-2216; apps/server/src/zerops/origin.ts:31_

## Gaps
- No live probe of an actual deployed container's /pair screen or / root was made (READ-ONLY constraint) — container first-screen conclusions come from tracing route/redirect code, not a screenshot.
- Whether app.t3.codes is Turnstile-allowlisted by Zerops today was not verified — no network call was made; this determines whether the hosted-static build's registration flow works as shipped.
- Mobile's actual auth/door code (apps/mobile) was not read beyond app.config.ts identity fields — the brief's claim that mobile inherits the zerops-identity door via 'the shared client runtime' is unverified against mobile source; Clerk (relyingParty clerk.t3.codes) suggests a possibly separate auth path.
- Data Console's embed call site inside z3 was not found by grep in apps/web/src — either it lives in a VS Code extension file not covered, or it's genuinely unbuilt; a targeted search in internal/dataconsole and the VS Code extension bootstrap files (zcp repo) would confirm.
- Test files (hostedPairing.test.ts, authBootstrap.test.ts) were not opened to cross-check behavioral claims against pinned test names, per CLAUDE.md's test-as-source-of-truth discipline — code line citations are the primary evidence here, test confirmation is a follow-up.
- apps/server/src/http.ts's static-serving guard was read only in the 330-450 range; the base-path 404 test names (Z3D-7) were not independently opened to confirm wording matches the zcp spec's paraphrase.
