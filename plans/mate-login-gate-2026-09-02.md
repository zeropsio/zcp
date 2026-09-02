# Plan: mate-login-gate

## Run State
- `phase:` shape
- `base:` z3 `092f44cd7` (mate 0.2.1) · zcp `2993b038`
- `integration:` —
- `approved:` —
- `review:` judge pass 2026-09-02 — 15 findings, all incorporated (Rev-2); Codex not requested
- `next:` owner decides D1/D2 (D3 settled by P5) and approves the register; then PROVE runs P1b (client spike) + P3 (needs a GUI handover token), then BUILD wave 1
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame

**Outcome**: Zerops Mate has exactly one identity — the Zerops account — and one derived
credential — a per-environment mate session that proves "this user is a member of this project"
for one membership window. The app is unreachable without a signed-in account, sign-out ends
every session it holds (server-side, in every tab, on every device the user asks for), and no
pairing code, pairing link, pairing URL, cookie session or boot-time admin credential exists
anywhere in the product.

| obs | evidence |
|---|---|
| Two independent sessions per browser: the Zerops account (`localStorage` `zerops-mate.zerops-session.v1`) and a per-environment mate bearer/DPoP token in IndexedDB (`t3code:connection-runtime`, `document.credentials`) | `apps/web/src/zerops/ZeropsSessionProvider.tsx:9`, `apps/web/src/connection/storage.ts:36,413-437` |
| The account gate fails closed only for a `hosted-static` client; the mate session gate (`-door.ts`) is a second, independent gate with `manual-link`/`hosted-pairing` surfaces still live | `apps/web/src/routes/__root.tsx:104-108,129`, `apps/web/src/routes/-door.ts:144-172` |
| Account sign-out calls the platform `/auth/logout` and clears `localStorage`; it does not touch mate sessions (server rows stay valid ≤15 min), IndexedDB credentials, open sockets, or other tabs | `packages/client-runtime/src/zerops/api.ts:473-484`, `ZeropsSessionProvider.tsx:292-295`; no `storage` listener on the session key (`:197-215` listens on the selection key only) |
| The mate server cannot end the caller's own session: `revokeClient` refuses it, and there is no logout route | `apps/server/src/auth/EnvironmentAuth.ts:946-953`, `apps/server/src/auth/http.ts:410-429`, `packages/contracts/src/environmentHttp.ts:425-500` |
| Revocation is checked only when a ticket/token is verified; a live socket survives `revoke`/`revokeBySubject` until it reconnects | `apps/server/src/auth/SessionStore.ts:750-755,855-860`; no `revokedAt` reader in `ws.ts` |
| Zerops mode still advertises `one-time-token` and serves the pairing-credential/link routes to any `access:write` session | `apps/server/src/auth/EnvironmentAuthPolicy.ts:35-38`, `auth/http.ts:345-396` |
| The identity door mints a persisted 2-minute pairing grant that the client immediately exchanges — the legacy pairing carrier on the product path | `apps/server/src/zerops/ZeropsIdentityGate.ts:56-84`, `PairingGrantStore.ts` |
| Legacy pairing UI is reachable from the signed-in product: zero-environment landing → "Add a reachable backend manually" → `/settings/connections` (pairing code/URL form, QR, pairing-link admin) | `apps/web/src/routes/_chat.index.tsx:33-36,159-184`, `components/settings/ConnectionsSettings.tsx:263-375`, `components/auth/PairingRouteSurface.tsx:51-56` |
| `mate pair` and `mate auth pairing create` mint credentials regardless of Zerops mode | `apps/server/src/cli/pair.ts`, `cli/auth.ts` (ledger `verified.md:66`) |
| Mobile still ships the manual/QR pairing screen and `connectPairingUrl` beside the Zerops door | `apps/mobile/src/Stack.tsx:169,612`, `apps/mobile/src/connection/onboarding.ts:13,28` |
| Upstream's cookie session code sets `httpOnly`+`sameSite:lax` and no `secure`; inert in Zerops mode (refused first) | `apps/server/src/auth/http.ts:231-247` |
| A saved identity-door environment stores an absolute `httpBaseUrl`; after the `/z3` → `/mate` base-path move the entry loops "Reconnecting…" forever (`/z3/...` → `302 /zcp-login`, no CORS ⇒ transport error), and `ZeropsIdentityRepair` only repairs `authentication`, never a settled `unreachable` | live 2026-09-02 on `zcp-25ea-8080` (`/z3/.well-known` `302`, `/mate/.well-known` `200`, server 0.1.8); `ZeropsIdentityRepair.ts:22-24` |
| The handover reads the token from the fragment and scrubs history; a query-string fallback for the pairing token is still accepted by `takePairingTokenFromUrl` | `apps/web/src/routes/zerops_.authorized.tsx:39-46`, `environments/primary/auth.ts:143-163` |

- AC1: With no signed-in Zerops account, every route of the web client, the desktop shell and the
  mobile app renders only the account sign-in surface; `/zerops_/authorized` stays bare. — planned
  evidence: `-accountGate.test.ts` matrix (every path × every status), mobile `Stack` gate test,
  puppeteer on `/mate/` deep links.
- AC2: Sign-out in one tab ends the mate session server-side (`auth_sessions.revokedAt` set for
  every session this browser held), closes its open sockets within 1 s, deletes IndexedDB
  credentials + cached snapshots for those environments, clears the account session, and every
  other tab of the same origin lands on the sign-in surface without a reload. — planned
  evidence: server test "logout revokes the caller and closes its socket"; client-runtime test
  "sign-out revokes, forgets and closes every identity-door environment"; puppeteer two-tab run
  on `z3-eval`.
- AC3: "Sign out everywhere" revokes every mate session of the caller's Zerops user id on that
  environment and closes their sockets within 1 s; a session of another user is untouched; a
  device that still holds a valid Zerops account session re-enters only on a user gesture, never
  by silent re-mint (the close reason discriminates). Whether the platform can end the account
  everywhere is P5 (D3). — planned evidence: `EnvironmentAuth.test.ts` extension of MS1-4 + WS
  close assertion; client-runtime "a `session_revoked` close is never repaired silently".
- AC4: An unrefreshable account `401` runs the same local teardown as sign-out (best-effort
  server revoke, IndexedDB purge, socket close, cross-tab). — planned evidence: client-runtime
  test "an expired account tears down the environments it signed in".
- AC5: No pairing surface exists: the descriptor advertises `bootstrapMethods: ["zerops-identity"]`
  in Zerops mode and `[]` outside; `/api/auth/pairing-token`, `/pairing-links`,
  `/browser-session`, `/pair`, `?host=&token=`, `mate pair`, `mate auth pairing`, the mobile
  pairing screen and the settings pairing UI are deleted, not disabled. — planned evidence: grep
  guard test (no `pairing`/`/pair` identifiers outside the door's own names), descriptor test,
  `mate-zone-architecture` rule, `curl` of the deleted routes → `404` on `z3-eval`.
- AC6: The identity door returns the access token directly (one round trip, one DPoP proof); no
  `auth_pairing_links` row is written on the product path. — planned evidence:
  `ZeropsIdentityGate.test.ts` "mints a session, not a grant"; live: table row count unchanged
  after a sign-in on `z3-eval`.
- AC7: Outside a Zerops project the server starts, serves the descriptor and health, and nothing
  can authenticate (replaces MS1-8's "upstream behaviour unchanged"). — planned evidence:
  `server.test.ts` "refuses every bootstrap outside a Zerops project".
- AC8: Version skew degrades to a named state, never a decode error: a new client against an old
  server (grant-shaped door response, `one-time-token` still advertised) and an old client
  against a new server (token-shaped response) both connect or report `ConnectionBlockedError`
  with a reason; enum literals decode leniently for one release. — planned evidence:
  `onboarding.zerops.test.ts` both shapes; contracts decode test with an unknown literal.

**Non-goals**: membership re-check inside a window (the window stays the re-check, §3.3) ·
tearing down a socket at window *lapse* (silent re-mint stays) · the relay/mobile push auth
(§9.2, already Zerops-identity) · agent CLI login (§8) · renaming upstream `T3CODE_*`/`t3`
plumbing (fork.md §4.1).
**Constraints**: fork zones — everything touched is Owned core or Owned product (no Imported /
Ported path) · client design system for `apps/web/src/zerops/**` · `packages/contracts` wire
changes are versioned by the SPI note, not silently.

**Risk class**: high — trigger: security/credential surface + public wire contract change.

**Decisions the owner takes (D1, D2) — the register below assumes the recommended option**

- **D1 — how far "no legacy pairing" goes.**
  *A (narrow)*: delete pairing from the Zerops-mode paths only; keep upstream's headless/browser
  boot, cookie session, `mate pair`, `desktop-bootstrap` inert outside Zerops mode (MS1-8 as
  written). *B (recommended)*: the server is Zerops-only — `ServerAuthBootstrapMethod` collapses
  to `zerops-identity`, `ServerAuthSessionMethod` to bearer/DPoP, `PairingGrantStore`,
  `startupAccess` minting, `cli/pair.ts`, `cli/auth.ts` pairing verbs, the cookie route and the
  desktop-bootstrap method are deleted. A laptop `mate serve` gets in by pointing
  `T3CODE_ZEROPS_PROJECT_ID` at any project the developer is a member of (the door already works
  from anywhere the public API is reachable). B is what the outcome statement says; A leaves a
  second door and ~3 k lines of pairing code the product cannot reach. B also deletes
  `desktop-bootstrap`, the auth path of the desktop SSH launch — that launch is already dead end
  to end (web `SshEnvironmentGateway` is a hard stub, no SSH IPC method exists; S5-1 removed it),
  so `fork.md` §4's Desktop row is rewritten to match (S11).
- **D3 — what "Sign out everywhere" promises.** At the mate level it revokes every mate
  session of this user on that container and closes their sockets. Ending the *account* on other
  devices is the platform's: P5 probes whether `/auth/logout` (or another route) revokes every
  refresh token / personal token of the user. If yes, "everywhere" calls it too; if no, the
  button reads "Sign out of this project everywhere" and §3.7 records that another device signed
  into Zerops re-enters on its next gesture. The recommended default is the honest wording until
  P5 answers.
- **D2 — where "Sign out" lives in the UI.** Today: Settings → Zerops. Proposed: keep it there
  and add the account row (email · Sign out · Sign out everywhere) to the sidebar's account
  menu; the landing shows nothing (it is the signed-out surface).

**Assumptions**:
- [VERIFIED] The account gate already fails closed for hosted-static clients (MC-10) — `__root.tsx:129`.
- [VERIFIED] `revokeBySubject` exists and is per-user (MS1-4) — `EnvironmentAuth.ts:912-916`.
- [VERIFIED] `SessionStore` publishes changes on a PubSub and `ws.ts` already consumes it for the access stream — `SessionStore.ts:508-514`, `ws.ts:355-390`.
- [VERIFIED] `registry.remove` exists and removes credentials for the target — `registry.ts:544-560,491`.
- [LOGICAL] P1a — a live WS closes when the fiber running `rpcWebSocketHttpEffect` is interrupted: `RpcServer.toHttpEffectWebsocket` blocks in `socket.runRaw` inside `Effect.scope` and its finalizer fires on interrupt (`effect/unstable/rpc/RpcServer.js:925-949`); `revoke`/`revokeBySubject`/`revokeAllExcept` all publish `{type:"clientRemoved", sessionId}` on `sessions.streamChanges` (`SessionStore.ts:514-518,897-967`), which today has one informational subscriber (`ws.ts:2451-2475`). The hook: race the RPC effect against that stream filtered on `session.sessionId` inside the existing `acquireUseRelease` (`ws.ts:2636-2640`).
- [PROBE] P1b — the client supervisor reads that close (code `4401`) as a settled `blocked/authentication`, never as a transient it retries forever (`supervisor.ts:396-404` lease monitor).
- [VERIFIED 2026-09-02, live] P2 — the platform's `POST /auth/logout` invalidates the access token at once (`/user/info` → `401 notAuthorized`); the refresh token answers `400 refreshTokenInvalid`. Was: `POST /auth/logout` invalidates the refresh token AND the access token (a stolen `localStorage` copy can neither refresh nor call `/user/info` after sign-out). If the access token survives to natural expiry, §3.7 records the residual window as an accepted risk. Live on `api.app-prg1.zerops.io` with a throwaway session.
- [REFUTED 2026-09-02] P5 — the platform has no all-devices sign-out: `/auth/logout` ends only the presented session; D3 resolves to the honest wording.
- [PROBE] P3 — the handover personal token (`adoptPersonalToken`, no refresh token) is revoked by `/auth/logout` too, and `/user/info` with it answers `401` afterwards.
- [PROBE] P4 — `EnvironmentAuth` can issue a session for (`method`, `subject`, `scopes`, `label`, `proofKeyThumbprint`, client metadata) without a grant row: the internals of `exchangeBootstrapCredentialForAccessToken` (`EnvironmentAuth.ts:719-787`) split into consume + issue with no behaviour change for the membership-window cap (MS1-3).
- [VERIFIED] Desktop persists the whole connection catalog (credentials included) encrypted through `safeStorage` behind the same IPC bridge the web store uses, so `registry.remove` purges it too (`DesktopConnectionCatalogStore.ts:382-401`, `ipc/methods/connectionCatalog.ts:9-38`, `apps/web/src/connection/storage.ts:262-271`). `DesktopSavedEnvironments` survives only as a one-shot legacy migration input (`DesktopConnectionCatalogStore.ts:350,435-490`); the SSH mode card in Connections is dead UI — the web `SshEnvironmentGateway` is a hard stub (`apps/web/src/connection/platform.ts:108-134`, card at `ConnectionsSettings.tsx:1748-1756`).
- [VERIFIED] Mobile's Zerops path is complete (sign-in → picker → identity connect → Home, `ZeropsConnectRouteScreen.tsx:392-446`); the pairing screen is reached only from "Connect with a one-time link" on the signed-out surface (`:158-163`, `Stack.tsx:168-174,612-620`). `connection/migration.ts` is a catalog-format migration with a live caller (`catalog-store.ts:67`) — not pairing, stays.
- [VERIFIED] `registry.remove(environmentId)` deletes target+profile+credential+DPoP token from the catalog, closes the service scope (socket) and clears the IndexedDB caches (`registry.ts:544-600`, `storage.ts:645-663`); no "remove all"/"disconnect all" exists, and nothing runs on the account-gate flip — sockets close only by atom GC (`runtime.ts:47-50`, `AtomRegistry.js:291-321`), which any lingering subscriber defeats.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| P1a socket close on revoke | AC2, AC3 | repo | scout read of `RpcServer.js:925-949` + `SessionStore.ts:514-518`; proven by the S2 test | fiber interrupt closes the socket; `clientRemoved` carries `sessionId` | CONFIRMED (code) | `server.test.ts` "closes the socket of a revoked session" |
| P1b client reads 4401 as settled auth failure | AC2 | spike | client-runtime supervisor test: server closes with `4401` → state `blocked`, `lastFailure.reason: "authentication"`, no retry loop | — | INCONCLUSIVE | `supervisor.test.ts` |
| P2 access token dies at logout | AC4 | verifier | throwaway account, 2026-09-02: `POST /auth/login` → `GET /user/info` `200` → `POST /auth/logout` `200 {success}` → `GET /user/info` with the old token | `401 notAuthorized` immediately; no residual window | CONFIRMED | `verified.md` row; §3.7 |
| P2b refresh token dies at logout | AC4 | verifier | re-probe: login `auth.refreshToken` → logout → `POST /auth/refresh {refreshTokenId: <it>}` | `400 refreshTokenInvalid` — a **400**, not a 401: the client's unrefreshable-401 rule (MC-1) must also treat this code as terminal | CONFIRMED | `api.test.ts` "a refreshTokenInvalid 400 signs out"; `verified.md` |
| P5 all-devices account sign-out exists | AC3, D3 | verifier | two logins of one throwaway user (distinct access tokens); `/auth/logout` on session 1; session 2 `GET /user/info`; candidates `POST /auth/logout-all`, `/auth/logout/all`, `GET /auth/sessions` | session 2 stays `200`; every candidate `404` (`/user/*` variants `405` = path shape, not a route) | REFUTED — per-session only | D3 = honest wording: "Sign out of this project everywhere"; §3.7 records that another signed-in device re-enters on its next gesture |
| P3 personal token dies at logout | AC4 | verifier | handover token → `/auth/logout` → `GET /user/info` → expect `401` | — | INCONCLUSIVE | `verified.md` |
| P4 issue-without-grant seam | AC6 | repo | `vp test run apps/server/src/auth/EnvironmentAuth.test.ts` after the split, MS1-3 cases untouched | — | INCONCLUSIVE | `EnvironmentAuth.test.ts` |

## Target shape (the contract the slices implement)

**Server (Zerops mode)**
- `POST /api/auth/zerops-identity {token}` (+ optional DPoP proof) → `AuthAccessTokenResult`
  directly: session `method: "zerops-identity"`, `subject = userId`, `scopes = zeropsGrantScopes`,
  `expires_in = membershipTtl`. No grant row. The 401/403/404/500 table of §3.2 is unchanged.
  Skew (AC8): the client accepts both shapes for one release (a grant-shaped answer is exchanged
  as today), and `ServerAuthBootstrapMethod`/`ServerAuthSessionMethod` decode unknown literals
  leniently (dropped, never a decode error). The desktop shell and the phone ship on their own
  cadence against containers on different pins; the container-served web bundle is always the
  server's own version.
- `POST /api/auth/logout {scope: "this" | "all-mine"}` (authenticated, no scope requirement;
  `no-store`): `this` revokes the caller's session; `all-mine` calls `revokeBySubject(subject)`
  — refused (`403 operation_forbidden` `subject_logout_unsupported`) for a session whose subject
  is not a Zerops user id (there are none after D1-B). Returns `{revokedCount}`. A revoked or
  unknown bearer answers `401` as today; the client treats it as done.
- Revocation closes live sockets: the upgrade handler subscribes to `sessions.changes` **before**
  verifying the ticket, re-reads `revokedAt` once after, and a `clientRemoved` for its own
  `sessionId` interrupts the connection scope — WS close `4401` reason `session_revoked`.
  In-flight RPC handlers are interrupted (a revoke must be immediate; a network drop already cuts
  them the same way, so the commit pipeline (§6.3) must already tolerate it — S1 pins that with
  "an RPC in flight when the session is revoked"). Window lapse still does not close a socket
  (§3.3), and a ticket refused for an *expired* session keeps its `expired` reason: the client
  re-mints silently on `expired` and never on `session_revoked` (user gesture required).
- Descriptor: `bootstrapMethods: ["zerops-identity"]`, `sessionMethods: ["bearer-access-token",
  "dpop-access-token"]`; outside Zerops mode `bootstrapMethods: []`.
- Deleted (D1-B): `PairingGrantStore`, `auth_pairing_links` reads/writes (table left in place, a
  migration drops it in a later release), `/api/auth/browser-session`, `/api/auth/pairing-token`,
  `/pairing-links`, `/pairing-links/revoke`, `issueStartupPairingCredential`/`issueStartupPairingUrl`,
  `formatHeadlessServeOutput`/`buildPairingUrl`, `cli/pair.ts`, the `auth pairing` CLI verbs,
  `desktop-bootstrap`, `one-time-token`, `browser-session-cookie`, the `AuthAccessRead/Write`
  pairing-link admin (client-session listing/revoke of *others* stays, it is the "devices" list).

**Client-runtime (shared by web, desktop shell, mobile)**
- `signOutOfZerops({scope})` in `packages/client-runtime/src/zerops/signOut.ts`: for every
  registered identity-door environment, in parallel with a 3 s cap: `POST /api/auth/logout`
  with that environment's credential (best-effort), then `registry.remove(environmentId)` — the
  one existing primitive that drops credential + DPoP token + IndexedDB caches and closes the
  socket, on web and (through the IPC bridge) desktop alike; then platform `logout()` in a
  `try/finally` whose `finally` is `signOutLocally()`. Order matters for two reasons: the mate
  revoke uses the mate bearer, so it must run before the supervisor can re-mint with a still-valid
  account token, and the same routine must work with no account token at all (AC4). A **teardown
  epoch** on the catalog discards — and best-effort revokes — any identity-connect result that
  lands after the sign-out started (the re-mint race). A logout POST that fails or times out is
  accepted: the residual is one server row that dies at the window, never retried.
- Mobile: sign-out also unregisters the device from the activity relay (best-effort) so a
  signed-out phone stops receiving pushes.
- `onSessionChange(null)` (unrefreshable 401) runs the same routine minus the platform call.
- Cross-tab: a `storage` event on `ZEROPS_SESSION_STORAGE_KEY` → `newValue === null` ⇒ post
  `logout {scope:"this"}` with this tab's own in-memory credential (it may be a session another
  tab's re-mint replaced in the catalog), then the local half, then `signed-out` — short-circuits
  when already signed out (idempotent; `removeItem` on a missing key fires no event, so no loop);
  a rewrite with a value (a refresh in another tab) ⇒ adopt the tokens in memory only, no network;
  `null → value` (sign-in elsewhere) ⇒ `restoreSession` + `fetchUser`.
- The identity connect consumes the direct token (`prepareZeropsIdentityRegistration` no longer
  exchanges a grant).

**Web**
- Root gate = account gate only: `handover` → bare outlet; not `signed-in` → landing (exclusive,
  no manual fallback); `signed-in` → app shell. `-door.ts`, `AuthGateState`,
  `resolveInitialServerAuthGateState`, `isHostedStaticApp`, `hostedPairing.ts`, `pairingUrl.ts`,
  `routes/pair.tsx`, `PairingRouteSurface` (manual half), `HostedStaticOnboardingState`, the
  pairing parts of `ConnectionsSettings` (code/URL form, QR, pairing-link admin, scope picker)
  are deleted; the zero-environment state is the project picker.
- Settings → Zerops: "Sign out" + "Sign out everywhere"; the sidebar account menu gets the same
  two actions (D2).

**Mobile**
- `ConnectionsNewRouteScreen`, `features/connection/pairing.ts`, `connection/migration.ts`,
  `connectPairingUrl` deleted; `signOut` calls `signOutOfZerops`.

**Spec**: `spec-mate.md` §3.2 (direct mint), §3.5 (bearer/DPoP only; logout; socket close on
revoke), new §3.7 "Sign-out", §4.1 (account gate is the only gate), §4.6 (direct token), §9.1
(desktop: same gate), §9.3 (mobile pairing gone); MS1-3/MS1-8/MC-10 rewritten, MS1-9..11 + MC-11
added; `fork.md` §4 row "T3 cloud — reach/pairing" gains the client-side deletion note.

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S0 | Test door: fake Zerops platform layer (`GET /project/{id}`, `GET /user/info`) + "session via the door" helper; inventory of every test/CI script that gets in through `one-time-token` or the printed startup credential | — | `apps/server/src/test/zeropsPlatformDouble.ts` (new), `apps/server/src/test/doorSession.ts` (new), `.github/workflows/ci.yml` (read; edit only if headless serve is used) | server test infra | review | pending |
| S1 | Door mints the session directly (P4 seam) + `POST /api/auth/logout` (`this`/`all-mine`) + revoke closes the socket (subscribe-before-verify; in-flight RPC interrupted; `session_revoked` vs `expired` reasons) | — | `packages/contracts/src/environmentHttp.ts`, `packages/contracts/src/auth.ts` (logout schemas + lenient method decode), `apps/server/src/auth/EnvironmentAuth.ts`, `auth/http.ts`, `auth/SessionStore.ts`, `ws.ts` (upgrade handler), `zerops/ZeropsIdentityGate.ts`, `zerops/http.ts`, tests | server unit + contract + `server.test.ts` | review | pending |
| S2 | Client-runtime: sign-out routine (teardown epoch, 3 s cap, relay unregister hook), direct-token identity connect with grant fallback (AC8), `session_revoked` never repaired, cross-tab contract; identity-door entries keyed by container origin with the base path re-derived from the descriptor at connect (a stale `/z3` entry re-probes `/mate` instead of looping) | S1 | `packages/client-runtime/src/zerops/signOut.ts` (new)+test, `zerops/api.ts`, `connection/onboarding.ts` (`prepareZeropsIdentityRegistration`), `connection/registry.ts` (epoch, bulk remove), `connection/supervisor.ts` (close-reason handling)+test | client-runtime unit | review | pending |
| S3 | Web: the account gate is the only gate; delete `-door.ts` and the hosted/pairing gate states | — | `apps/web/src/routes/__root.tsx`, `routes/-door.ts`+test (delete), `routes/-accountGate.ts`+test, `routes/pair.tsx` (delete), `routes/_chat.index.tsx`, `environments/primary/auth.ts`, `environments/primary/index.ts`, `hostedPairing.ts`+test, `pairingUrl.ts`, `components/auth/PairingRouteSurface.tsx`+test, `components/zerops/landing/ZeropsHostedLanding.tsx`+tests | web unit | review | pending |
| S4 | Web sign-out wiring: provider (routine + `storage` listener), Settings › Zerops, sidebar account menu (D2), first-prompt markers cleared | S2, S3 | `apps/web/src/zerops/ZeropsSessionProvider.tsx`+test, `components/settings/ZeropsSettings.tsx`, the sidebar account menu component (named after a scout pass in the brief), `zerops/firstPromptStorage.ts` | web unit + puppeteer | review | pending |
| S5 | Settings › Connections = devices list only (pairing form/QR/link admin, scope picker and the dead SSH card go) | S3 | `components/settings/ConnectionsSettings.tsx`, `components/settings/pairingUrls.ts`+test (delete), `apps/web/src/connection/onboarding.ts` (`connectSshEnvironment`), `apps/web/src/connection/platform.ts` (SSH stub) | web unit | autonomous | pending |
| S6 | Server: delete the pairing world (D1-B) — grant store, startup minting, cookie route, CLI verbs, `one-time-token`/`desktop-bootstrap`/`browser-session-cookie`; every test moved onto S0's door helper | S0, S1 | `apps/server/src/auth/PairingGrantStore.ts`+test (delete), `auth/EnvironmentAuth.ts` (grant paths), `auth/EnvironmentAuthPolicy.ts`+test, `auth/http.ts` (routes), `startupAccess.ts`+test, `serverRuntimeStartup.ts`, `cli/pair.ts`+test (delete), `cli/auth.ts`, `cliAuthFormat.ts`+test, `bin.ts`+test, `packages/contracts/src/auth.ts` (method literals), `environmentHttp.ts` (delete routes), `persistence/AuthPairingLinks.ts`, every `*.test.ts` that pairs | server unit + `server.test.ts` + `bin.test.ts` | review | pending |
| S7 | Client-runtime: delete pairing registration + hosted pairing request + remote pairing target | S2 | `packages/client-runtime/src/connection/onboarding.ts`, `connection/resolver.ts`, `packages/shared/src/remote.ts`, `packages/shared/src/connectAuth.ts`, tests | client-runtime unit | autonomous | pending |
| S8 | Desktop: delete the `saved-environments.json` migration + SSH target/profile re-creation; scout the deep-link handler for `?host=&token=` first and delete that parse too | S3 | `apps/desktop/src/settings/DesktopSavedEnvironments.ts`+test (delete), `apps/desktop/src/app/DesktopConnectionCatalogStore.ts`+test, `apps/desktop/src/app/DesktopEnvironment.ts`, deep-link handler (named after the scout pass) | desktop unit | autonomous | pending |
| S9 | Mobile: delete the pairing screen, its "Connect with a one-time link" entry, `pairing.ts`, `connectPairingUrl`, `onConnectPress`; sign-out via S2 incl. relay unregister; gate test (AC1) | S2, S7 | `apps/mobile/src/Stack.tsx`, `features/connection/ConnectionsNewRouteScreen.tsx` (delete), `features/connection/pairing.ts`+test (delete), `features/connection/useConnectionController.ts`, `state/use-remote-environment-registry.ts`, `connection/onboarding.ts`, `features/zerops/ZeropsConnectRouteScreen.tsx`, `features/zerops/ZeropsSessionProvider.tsx`+test, `features/cloud/managedRelay.ts` (unregister) | mobile unit | autonomous | pending |
| S10 | Guards: `no-pairing.test.ts` grep guard + `mate-zone-architecture` rule; runs on the fully integrated tree | S3, S5, S6, S7, S8, S9 | `scripts/no-pairing.test.ts` (new), `scripts/mate-zone-architecture.test.ts` | scripts | review | pending |
| S11 | Spec + fork rules + ledger (orchestrator-written: `fork.md` §5 "one writer edits the ledger") | — | `zcp/docs/spec-mate.md`, `z3/docs/internals/zerops/fork.md` (§4 Desktop row, reach/pairing row), `verified.md`/`hacks.md`/`map.md` pairing sections | docs | owner | pending |

Gate ∈ autonomous\|review\|owner · State ∈ pending\|building\|landed\|blocked. Overlapping `Files` never share a wave.

Waves: **W1** S0 · S1 · S3 (disjoint: server test infra / server auth + contracts / web gate) ·
**W2** S2 · S5 · S6 (S6 edits `packages/contracts/src/auth.ts` after S1 landed it) · **W3** S4 ·
S7 · S8 · **W4** S9 · **W5** S10. S11 runs at GATE 1 (contract rows) and at LAND (reconciliation
+ ledger). LAND also carries the release step: fork tag `v0.3.0` → `PinnedVersion`/`PinnedSHA256`
in `zcp/internal/mate/mate.go` → zcp release (the order `CLAUDE.local.md` fixes: tag first).

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `-accountGate.test.ts` matrix; puppeteer `/mate/settings`, `/mate/threads/x`, `/mate/` with cleared storage → sign-in surface only | not-run | — |
| AC2 | server: logout revokes + closes socket ≤1 s; client-runtime: sign-out revokes/forgets/closes every environment; puppeteer two tabs on `z3-eval` | not-run | — |
| AC3 | `EnvironmentAuth.test.ts` all-mine revokes one subject + closes its sockets; other subject untouched | not-run | — |
| AC4 | client-runtime: 401-expired account runs local teardown; P2/P3 rows | not-run | — |
| AC5 | `no-pairing.test.ts` guard; descriptor test; `curl` deleted routes on `z3-eval` → 404 | not-run | — |
| AC6 | `ZeropsIdentityGate.test.ts` no grant row; live `auth_pairing_links` count unchanged after sign-in | not-run | — |
| AC7 | `server.test.ts` outside Zerops mode: descriptor `bootstrapMethods: []`, identity route 404, no boot output credential | not-run | — |
| — | negative: a revoked bearer on `/api/auth/logout` → 401, client still completes local sign-out | not-run | — |
| — | negative: sign-out while an environment is unreachable → local teardown completes, no hang past 3 s | not-run | — |
| — | regression: membership window (MS1-3) unchanged; window lapse does not close a socket | not-run | — |
| AC8 | new client × old server (grant shape, `one-time-token` advertised) and old client × new server both reach a named state | not-run | — |
| — | race: identity re-mint completing after sign-out started is discarded and revoked (epoch) | not-run | — |
| — | race: revoke between ticket verify and subscribe still closes the socket | not-run | — |
| — | race: RPC in flight when the session is revoked is interrupted; the commit pipeline leaves the repo consistent | not-run | — |
| — | idempotency: two tabs signing out; `registry.remove` on an absent environment is a no-op | not-run | — |

## Promotion
- Contracts → `docs/spec-mate.md` §3.2 (direct mint), §3.5 (session methods, logout, revoke closes sockets), §3.7 (sign-out routine, cross-tab), §4.1 (single gate), §4.6, §9.1, §9.3
- Invariants → `MS1-3` (rewritten: window keyed on `method`, no grant), `MS1-8` (rewritten: outside Zerops nothing authenticates), `MS1-9` logout revokes+closes (subscribe-before-verify), `MS1-10` all-mine is per subject, `MS1-11` no pairing surface, `MS1-12` `session_revoked` is never repaired silently; `MC-10` (rewritten: the only gate, web+desktop+mobile), `MC-11` sign-out cascade + teardown epoch + cross-tab, `MC-12` version skew degrades to a named state; tests named per slice
- CLAUDE.md trap line (≤1): none — the guard test `no-pairing.test.ts` is the trap
- This plan → `plans/archive/` on LAND close
