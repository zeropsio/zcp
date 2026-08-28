# S2 (z3 half) — `--base-path` end to end

Date: 2026-08-28. Owner of this stream: the S2-z3 agent. Worktree: `/Users/macbook/Documents/Zerops-MCP/z3-wt/s2`, branch `z3-s2` (cut from `z3` @ `40b124779`).
Brief: `plans/z3-brief-2026-08-28.md` §5 D2, §6 S2, §4a. Measured facts: `../z3/docs/internals/zerops/verified.md` § S0.5.
All file:line references are against `z3-s2` at cut time.

---

## 1. The contract with nginx — decision

**(a) nginx strips the prefix.** The recommended block is S0.5's measured one, unchanged:

```nginx
location /z3/ {
    proxy_pass http://127.0.0.1:3773/;      # trailing slash = strip /z3
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_read_timeout 86400s;
}
```

Nothing beyond the S0.5 block is required. No `X-Forwarded-Prefix` — the server is told its
prefix by `--base-path`, not by a per-request header, so the value cannot be spoofed by a client
and cannot drift per request.

**Why (a), not (b).** Mounting every route under a prefix would mean rewriting the route literals
in `packages/contracts/src/environmentHttp.ts` (a contracts change shared by web, mobile, desktop
and the relay), `ws.ts`'s `/ws`, the three raw routes in `http.ts` and `McpHttpServer`. That is a
large non-additive diff straight through the seams upstream moves in daily — fork rule 6 forbids
it. The client-side work is identical under (a) and (b), so (b) buys nothing.

**A tolerant ingress strip was tried and dropped.** Rewriting the request URL before routing
needs `HttpRouter.serve`'s `middleware` option (the only hook that runs before `router.find`,
`effect/unstable/http/HttpRouter.ts:191-196`). Passing *any* middleware there — identity
included — leaves the `R` type parameter unsolvable, because `R` has inference sites in both
`appLayer` and the middleware's argument type; it collapses to `unknown` and the served layer's
requirements go with it. 499 type errors, independent of how the middleware itself is typed
(three shapes tried, including `Effect.updateContext`, which touches no requirement at all).
Not worth fighting for a robustness nicety the brief did not ask for.

**What replaced it** (S2z-4): the catch-all *names* the mistake instead of absorbing it. A
request that still carries a configured prefix gets `404 application/json` saying the proxy is
expected to strip it and that an nginx `proxy_pass` needs its trailing slash. So S0.5's worst
finding — a missing slash turning every API call into `200 text/html` — is loud, though the
arrangement is still unsupported. Consequence to accept: code-server's `/proxy/3773/…` door and
direct-to-3773 browsing do **not** work with a `/z3/`-built bundle; use the `/z3/` origin.

---

## 2. Seam map

### 2.1 The URL helpers that overwrite `pathname` (the cost D2 names)

| Site | What it does today | Needed |
|---|---|---|
| `packages/client-runtime/src/environment/endpoint.ts:3-9` `environmentEndpointUrl` | `url.pathname = pathname` | join |
| `packages/client-runtime/src/authorization/remote.ts:239-243` `resolveRemoteWebSocketConnectionUrl` | `/ws` only when pathname is `""`/`"/"` — a `/z3/` base gets **no** `/ws` at all | join `/ws` onto the prefix |
| `packages/client-runtime/src/authorization/remote.ts:265-269` `resolveRemoteDpopWebSocketConnectionUrl` | same | same |
| `packages/client-runtime/src/connection/resolver.ts:51-58` `primarySocketUrl` | same | same |
| `packages/shared/src/advertisedEndpoint.ts:36` `normalizeHttpBaseUrl` | `url.pathname = "/"` | preserve the prefix |
| `packages/shared/src/advertisedEndpoint.ts:42-46` `deriveWsBaseUrl` | inherits the above | preserve |
| `packages/shared/src/remote.ts:100` `normalizeRemoteBaseUrl` | `url.pathname = "/"` | preserve |
| `packages/shared/src/remote.ts:113` `toHttpBaseUrl` / `:126` `toWsBaseUrl` | `next.pathname = "/"` | preserve |
| `apps/web/src/environments/primary/target.ts:216-240` `resolveWindowOriginPrimaryTarget` | `window.location.origin` — drops the prefix the page is served under | origin + base path |
| `apps/web/src/environments/primary/target.ts:274-290` `resolvePrimaryEnvironmentHttpUrl` | `url.pathname = pathname` | join |
| `apps/server/src/cli/pair.ts:206-210` `probeEnvironmentDescriptor` | `new URL("/.well-known/…", baseUrl)` — absolute path wins, prefix lost | join |
| `apps/server/src/cliAuthFormat.ts:34-41` (pairUrl) | `new URL("/pair", baseUrl)` | join |
| `apps/server/src/startupAccess.ts:92-98` `buildPairingUrl` | `url.pathname = "/pair"` | join |
| `apps/server/src/auth/EnvironmentAuth.ts:911-921` `issueStartupPairingUrl` | `url.pathname = "/pair"` | join + base path |

`packages/client-runtime/src/environment/endpoint.ts:1` re-exports all of `advertisedEndpoint`,
so `connection/onboarding.ts:12` picks the path-dropping normalizers up transitively.

`BearerConnectionTarget` / `BearerConnectionProfile` (`connection/catalog.ts`, `model.ts:18-24`)
carry `httpBaseUrl`/`wsBaseUrl` as plain `Schema.String` — a path needs **no schema change**, only
the helpers above. Same for `KnownEnvironmentConnectionTarget`
(`packages/client-runtime/src/environment/knownEnvironment.ts:3-6`).

### 2.2 Static / bundle

- `apps/web/vite.config.ts` — no `base` today (`defineConfig` at :154, `build` at :260).
- `apps/web/index.html:10-12,462,466` — root-absolute `/favicon.ico`, `/apple-touch-icon.png`,
  `/manifest.webmanifest`, the boot-splash `<img src="/apple-touch-icon.png">`.
- `apps/web/public/manifest.webmanifest` — `id`/`start_url`/`scope` = `"/"`, icon `src` root-absolute.
  Files in `public/` are copied verbatim; Vite never rewrites them.
- `apps/web/src/router.ts:5-11` — `createRouter({ routeTree, history, context })`, no `basepath`.
  Without it every client-side route under `/z3/` misses.
- `apps/web/src/main.tsx:22` — `createBrowserHistory()`; TanStack reads the router `basepath`, so
  the history needs no change.
- No production service worker (`public/mockServiceWorker.js` is MSW, test-only).
- `apps/server/scripts/cli.ts:165-171` — `build` copies `apps/web/dist` → `apps/server/dist/client`,
  which `ServerConfig.resolveStaticDir` (`apps/server/src/config.ts:217-236`) finds. So the base is
  baked by the **web** build that runs before `cli.ts build`.

### 2.3 The silent-failure surface

`apps/server/src/http.ts:329-419` `staticAndDevRouteLayer` is `GET *`. It 404s
`isDevProxiedPath(pathname)` (`/api`, `/oauth`, `/.well-known`, `/ws` —
`packages/shared/src/devProxy.ts:11`) **only when `config.devUrl` is set** (`http.ts:341-343`).
In production every unmatched `/api/**` returns `200 text/html` — S0.5's "a base-path bug looks
like the app loads but nothing works".

`packages/client-runtime/src/environment/descriptor.ts:13` probes the well-known path and decodes
the body against a schema; an HTML body fails as a decode error, not as "wrong prefix".

### 2.4 Server config / flags

- `apps/server/src/cli/config.ts:20-75` flag definitions, `:77-140` env (`T3CODE_*`),
  `:143-157` `CliServerFlags`, `:164-190` `sharedServerCommandFlags`, `:211+` `resolveServerConfig`.
- `apps/server/src/config.ts:58-96` the `ServerConfig` service shape, `:168-212` `makeTest`.
- `apps/server/src/server.ts:673` the single `HttpRouter.serve` call.
- `packages/contracts/src/environment.ts:96-102` `ExecutionEnvironmentDescriptor`;
  built at `apps/server/src/environment/ServerEnvironment.ts:142`.
- The server sets **no cookies** (grep: no `Set-Cookie` anywhere in `apps/server/src`), so the
  cookie-`Path` half of the brief's list is a no-op. Recorded, not skipped.

---

## 3. Slices (RED → GREEN, one commit each)

**S2z-1 — path-preserving URL algebra** LANDED `6312a44a2`.  (`packages/shared`, `packages/client-runtime`).**
New `packages/shared/src/basePath.ts`: `normalizeBasePath` (→ `""` | `/z3`), `joinBasePath`,
`withBasePath(baseUrl, path)`, `socketUrlFrom(wsBaseUrl)`. Rewrite the ten helpers in §2.1 rows
1-8 on top of it. `/ws` rule: append `/ws` to the base path unless the path already ends in `/ws`
(keeps upstream's explicit-socket-path escape hatch).
RED: `packages/shared/src/basePath.test.ts`, `remote.test.ts`, `advertisedEndpoint` cases,
`packages/client-runtime/src/environment/endpoint.test.ts`,
`packages/client-runtime/src/connection/onboarding.test.ts`.
Proves: `https://h/z3` + `/api/auth/session` → `https://h/z3/api/auth/session`;
`wss://h/z3` → `wss://h/z3/ws`, `wss://h/` → `wss://h/ws`, `wss://h/z3/ws` unchanged.

**S2z-2 — `apps/web` under a prefix.** LANDED `36aba1e00`.
`vite.config.ts`: `base` from `VITE_BASE_PATH` (default `/`). `index.html`: `%BASE_URL%`-prefixed
asset refs. A small plugin emitting `manifest.webmanifest` with the base applied. New
`apps/web/src/basePath.ts` reading `import.meta.env.BASE_URL`. `router.ts`: `basepath`.
`environments/primary/target.ts`: window-origin target = origin + base path;
`resolvePrimaryEnvironmentHttpUrl` joins.
RED: `apps/web/src/basePath.test.ts`, new cases in `environments/primary/bootstrap.test.ts`.

**S2z-3 — `t3 serve --base-path` (server).** LANDED `b72194310`.
`--base-path` flag + `T3CODE_BASE_PATH` env + `ServerConfig.basePath` (normalized, default `""`).
`basePath` added to `ExecutionEnvironmentDescriptor` as an optional key, filled in
`ServerEnvironment.ts`, and checked by the web bootstrap — a client built for one prefix and
pointed at a server published under another otherwise reaches a server that answers. The four
server-side pair/probe URL builders (§2.1 rows 11-14) join.

**S2z-4 — non-silent failure.** LANDED `b72194310`.
`staticAndDevRouteLayer`: the `isDevProxiedPath` 404 is unconditional (it was dev-only) and
returns JSON, plus a second guard naming a forwarded prefix. Client side is the descriptor
`basePath` comparison above rather than a content-type assertion — it catches the case the
server cannot see (both ends healthy, prefixes disagree), and needed no surgery on the shared
contract-client path.

**S2z-5 — build + hand-off.** Build `apps/web` with `VITE_BASE_PATH=/z3/`, `cli.ts build`,
`cli.ts pack`; hand main the nginx block, the `t3 serve` argv, and the live curl/WS checks.

Verification per fork rule 6: `vp test run <changed files>` + targeted typecheck only.

---

## 4. What main gets

- The nginx `location` block above — unchanged from S0.5, no extra headers.
- The serve argv: `t3 serve --base-path /z3 --base-dir /home/zerops/.t3 --port 3773 ... /var/www`.
- Live checks (run against the container after the push):
  - `curl -sS -D- -o/dev/null https://<origin>/z3/.well-known/t3/environment` → `200`,
    `content-type: application/json`, body carries `"basePath":"/z3"`.
  - `curl -sS -D- -o/dev/null https://<origin>/z3/api/nope` → `404`, `application/json`
    (today: `200 text/html`).
  - `curl -s https://<origin>/z3/ | grep -o 'src="[^"]*assets[^"]*"'` → every ref begins `/z3/assets/`.
  - each of those asset URLs → `200` with a JS/CSS content-type.
  - a Node `WebSocket` to `wss://<origin>/z3/ws?wsTicket=…` → `101`, held 60 s.
