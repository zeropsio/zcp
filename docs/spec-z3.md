# Spec: z3 — Zerops Code

z3 is a fork of the open-source T3 Code that rides inside the `zcp` container. Its server runs
next to nginx and code-server and spawns the coding agent (Claude Code, Codex) with ZCP's MCP
tools attached; its client — web, desktop, mobile — is the product surface a Zerops user signs
into. Because the agent operates the project through the same `zerops_*` tools an agent in a
terminal uses, z3's UI is not a second control plane: it is a **reader** of what those tools
report. That reading contract is what this spec owns.

- Delivery — how the z3 bundle gets into the container, the supervised process, nginx, the
  `/z3/` base path, readiness — §2.
- The door — the Zerops-identity bootstrap a project member uses to reach a hosted z3 server,
  with no pairing code and no shared container secret — §3.
- Related: `docs/spec-workflows.md` (the envelope/plan/atom pipeline that produces the
  state), `docs/spec-work-session.md` (per-PID session, compaction survival).

---

## 1. Envelope on the wire

The z3 client rebuilds a thread's lifecycle state by reducing over the provider's tool-result
stream. It never reads `.zcp/state` and never calls the Zerops API for lifecycle: the
`workflow.StateEnvelope` a workflow-aware tool already computes **is** the state. For that to
work the envelope has to survive the trip from the MCP handler, through the provider CLI, to
the z3 server's reducer.

### 1.1 The block

The envelope always travels **inside the result text**, as verbatim JSON. Which of two
carriers a tool uses follows from the shape of its answer:

| The result text is | Carrier |
|---|---|
| prose (rendered markdown) | a trailing fenced ```` ```json zcp-envelope ```` block (§1.1) |
| one JSON document | a top-level `envelope` key beside the tool's own fields (§1.2) |

A JSON document cannot take the fence — appending one would stop it parsing as JSON — and prose
has no top-level key to hang the envelope from. Hence two carriers, one reducer (§1.3).

### 1.1 The fenced block (prose results)

A prose tool result's text ends with exactly one fenced code block whose info string is
`json zcp-envelope`:

````
## Status

Phase: develop-active
… rendered markdown guidance …

```json zcp-envelope
{"phase":"develop-active","environment":"container",…}
```
````

- The body is the `workflow.StateEnvelope` as **compact single-line JSON** (`json.Marshal`).
  Serialization is deterministic — the type sorts its slices at construction and
  `encoding/json` sorts map keys — so identical state produces identical bytes and a reducer
  can dedupe by content.
- Nothing but whitespace follows the closing fence.
- The producer is `workflow.AppendEnvelope`; the reference reducer is
  `workflow.ExtractEnvelope`. A z3 client implements the same rule in TypeScript.

Appending over a text that already **ends** with an envelope block replaces that block rather
than adding a second, so a producer chain cannot emit two trailing envelopes. A block embedded
earlier in the text is content, not structure, and is left alone.

### 1.2 The `envelope` key (JSON-document results)

A tool whose result text is one JSON document carries the envelope as a top-level `envelope`
key, a sibling of the fields it already returned:

```json
{"status":"ACTIVE","targetService":"apidev","workSessionState":{"status":"open"},"envelope":{"phase":"develop-active",…}}
```

Every pre-existing field keeps its name, shape and position — the envelope is an **added key,
never a reshape**. A result whose envelope computation failed omits the key entirely
(`omitempty`), leaving the document byte-identical to what the tool produced before it carried
an envelope at all. Producers are the `*Response` wrapper types in `internal/tools`, each
embedding its underlying `ops.*` result so the existing fields stay flat;
`TestJSONCarriers_WireContract` pins both halves for every one of them.

### 1.3 The reducer rule

One reducer reads both carriers, in this order:

1. **JSON carrier first.** If the whole text parses as a JSON object, take its top-level
   `envelope`. Trying this first means a fence that appears inside one of the document's string
   values — a captured log tail, say — cannot outrank the real envelope.
2. **Otherwise the fence.** Scan for lines that consist solely of the opening fence. The match
   is **line-anchored**: a fence mentioned mid-line (prose describing this format) is text.
   The **last** complete block wins — a transcript may concatenate several tool results, and
   the newest state is the last envelope in it.
3. A malformed envelope is **ignored** — the reducer keeps its previous state rather than
   adopting it. That covers a JSON document with no `envelope` key, an unterminated block, and
   a block whose body does not parse.

### 1.4 Which tools carry it

| Tool | Result | Carrier |
|---|---|---|
| `zerops_workflow action="status"` | prose | fence — the canonical lifecycle carrier (P4 recovery primitive) |
| `zerops_workflow workflow="develop" action="start"` | prose | fence — seeds a new thread's strip without a second call |
| `zerops_workflow action="close"` | prose (terse) | fence — so the strip sees the transition |
| `zerops_deploy` (local, ssh, and both git-push routes) | JSON | `envelope` key |
| `zerops_verify` (single and all-services) | JSON | `envelope` key |
| `zerops_import` | JSON | `envelope` key |
| `zerops_mount` (mount, unmount, status) | JSON | `envelope` key |
| `zerops_workflow` bootstrap `start`/`complete`/`skip`/`status` | JSON | `envelope` key |

The three prose carriers route through `tools.statusResult` / `tools.withFreshEnvelope`; the
first two render and append from the *same* envelope, so the markdown and the machine-readable
state can never describe different moments. The JSON carriers each call `tools.freshEnvelope`
**after** their mutation has succeeded, so the envelope describes the state that mutation
produced.

Error results carry **no** envelope, under either carrier. An error is a leaf payload
(`spec-workflows.md` P4); attaching state to one would let a reducer read a failed call as
fresh truth.

Envelope computation is an addendum and is **total**: a failure attaches nothing, leaves the
tool's own result untouched, and reports to stderr (JSON-only stdout). The lifecycle strip
degrading to slightly stale state is always preferable to a tool call failing over its
telemetry.

### 1.5 Size

The envelope is small next to what it rides on: **140 B** fenced for an idle envelope, **~1.7 KB**
for four services plus a work session with deploy/verify attempts. On a JSON result the same
state costs ~1.2 KB (no fence, no markdown) — a deploy response measured 119 B before and
1287 B after.

The synthesized guidance a prose result carries is held under `workflow.ComposeBodyBudget`
(24 KB), which sits below the 28 KB soft cap and the **32 KB MCP tool-response cap** precisely
to leave room for the scaffold `RenderStatus` adds — so the envelope fits inside existing
headroom and needs no budget of its own. `TestAppendEnvelope_BlockSizeBudget` pins it. JSON
results are far smaller than the prose ones and are nowhere near the cap.

### 1.6 Why not `structuredContent`

The Go MCP SDK marshals a non-nil typed handler output (a handler's second return value) into
the JSON-RPC result's `structuredContent` field, *alongside* the text content. **Claude Code
replaces the model-facing tool result with `structuredContent` when it is present** — the text
block never reaches the model. Routing the envelope that way would silently strip every atom of
guidance a workflow result renders. Measured live; recorded in
`../z3/docs/internals/zerops/verified.md`, section "S6 PROVE".

So the typed-output slot stays empty at every handler, guarded by
`TestNoStructuredContentOnToolResults` (which checks named handlers *and* the closures handed
to `mcp.AddTool`).

A second `mcp.Content` block was the other way to reach a JSON result, and was rejected: it
would make the envelope's delivery depend on every provider forwarding multi-block tool results
intact, which is unproven, where a sibling key inside the one block they already forward is
not.

---

## 2. Delivery

z3 rides inside the `zcp` container rather than the platform's `zcp@1` recipe: installed and
supervised by `zcp init` (a `run.init` command), reached through the container's existing nginx on
port 8080. A plain restart re-runs the recipe's `install.sh`, picks up the latest zcp release, and
turns z3 on for a project whose container predates it — no platform-side change. Everything zcp
knows about z3 lives in `internal/z3`, kept to stdlib plus `runtime`/`schema`.

### 2.1 The init step — local bundle first

`zcp init` gains a container-only step, **Zerops Code (z3)**, after the SSH-config step:

1. **Refuse without a project.** `runtime.Info.ProjectID` empty ⇒ degrade — a non-empty project id
   is the sole signal the server binds to a Zerops project.
2. **Bundle.** `z3.BinPath()` present ⇒ used as-is, no version check, no network. Absent ⇒ one
   `npm install --prefix ~/.zcp/z3 --no-audit --no-fund --loglevel=error t3@<PinnedVersion>`,
   capped at 3 minutes.
3. **Capability note.** `z3.SupportsBasePath` reads `serve --help` once; unadvertised ⇒ logged to
   stderr (§2.2) — such a bundle answers under `BasePath` but its root-absolute assets hit the
   cookie gate instead.
4. **Environment.** `~/.zcp/z3.env` rewritten (mode 0600) every boot — §2.3.
5. **Unit.** `z3.UnitFilePath` absent ⇒ `sudo -E zsc unit create z3 "zcp service start z3"`.

A hand-placed dev bundle and a registry fetch share step 2 onward — the local-bundle-first rule
that keeps a warm restart off the network (`~/.zcp/z3` survives a restart; a redeploy loses it).
The step is **best-effort** (`step.degraded`): a container with no bundle and no reachable registry
must still boot, and when the bundle cannot be had **no unit is registered** — an unresolvable
ExecStart crash-loops at every boot. The unit file's presence, not a `zsc unit` upsert (there is
none), is the idempotency check: a unit survives a restart, and `zcp init` runs on every boot.

### 2.2 The supervised process

`zcp service start z3` runs `z3.ServeArgv(bin, withBasePath)` — never `npx` (resolving the package
at every start cost 58 s cold, measured, see the z3 ledger; the argv always runs the local bundle):

```
<bin> serve --mode web --host 127.0.0.1 --port 3773 [--base-path /z3] \
  --base-dir ~/.t3 --no-browser --auto-bootstrap-project-from-cwd /var/www
```

- `--auto-bootstrap-project-from-cwd` is **boolean**; the workspace (`/var/www`) is a trailing
  **positional** — writing it as the flag's value bootstraps the unit's launch directory instead.
  `--base-dir` (`~/.t3`) keeps thread history across a restart; a redeploy starts it empty.
- **`--base-path` is a capability, not a preference**: passed only when `z3.SupportsBasePath(bin)`
  is true — the CLI treats an unknown flag as a fatal parse error, and the fork reports the same
  version string with and without it, so it cannot be gated by version. Omitting it degrades safely
  (only assets miss) and is logged at both `zcp init` and the unit's journal.

### 2.3 The environment contract

`zcp init` writes the following while the container's full environment is present;
`service.Start("z3")` merges it over the unit's own, not guaranteed to inherit it:

| Key | Source | Written when |
|---|---|---|
| `T3CODE_ZEROPS_PROJECT_ID` | `runtime.Info.ProjectID` | always — THE Zerops-environment signal; nothing else votes |
| `T3CODE_ZEROPS_API_HOST` | `z3.ResolveAPIHost(ZCP_API_HOST)`, else `schema.CanonicalAPIHost` | always — bare host, scheme/trailing slash stripped, port kept |
| `T3CODE_ZEROPS_ALLOWED_ORIGINS` | service env `ZCP_Z3_ALLOWED_ORIGINS` | only when set — unwritten ⇒ server's own default |

Only non-secret identifiers are written; a token never enters `~/.zcp/z3.env` (mode 0600),
rewritten every boot so a unit's frozen ExecStart never has to change. A missing/unreadable env
file is reported and z3 starts anyway — diagnosable, unlike a unit that refuses to launch.

### 2.4 nginx — three locations, all outside the cookie gate

Rendered identically whether or not `VSCODE_PASSWORD` is set.

| Location | Behaviour |
|---|---|
| `{BasePath}/` (`/z3/`) | Proxies to `http://127.0.0.1:3773/` — **trailing slash strips the prefix**, so z3's routes stay at the loopback root and only URLs it *emits* (`--base-path`) carry it. Websocket upgrade headers, `proxy_read_timeout 86400s`. Outside the cookie gate: z3 owns its own auth (§3). |
| `~ ^/(abs)?proxy/3773(/|$)` | `return 404`. code-server's `/proxy/<port>/`/`/absproxy/<port>/` reach any loopback port for whoever holds the container cookie — a second door, closed; evaluated before `location /`. |
| `= /healthz` | Serves `z3.InitMarkerPath` verbatim, `application/json`, `no-store`; falls back to `{"initComplete":false,"initAt":null}` with no marker yet. No proxy, no process — answers even when nginx is all that's up. Shadows code-server's own `/healthz`. |

Live edits do not survive — every boot re-renders `internal/content/templates/nginx.conf.tmpl`.

### 2.5 Readiness — two probes, no process

**`GET /healthz`** → always `200 application/json`: the marker `zcp init` writes at
`/var/www/.zcp/state/init-complete` when its step list ends —
`{"initComplete":true,"initAt":"<RFC3339>"}`. Records the list **finished**, not that every step
succeeded (a degraded z3 step still leaves the marker); `initAt` moving is how a client sees a
restart re-initialized the container.

**`GET {BasePath}/.well-known/t3/environment`** → z3's own liveness: `200` **and**
`content-type: application/json` **and** a body carrying `"basePath":"/z3"` — never the status
code alone, since a stripped or mis-proxied prefix answers `200 text/html` from the SPA catch-all.
`z3Up` is the **client-side conjunction of both**: stock nginx cannot branch a response body on a
subrequest, so folding both fields into `/healthz` would need the sidecar process this design
removes. Budget (measured, see the ledger): a restart is ~17 s to `/healthz`, ~19 s to `z3Up`,
~14 s of L7 `502` in between — poll `z3Up`, render `502` as "restarting", cap at 30 s.

### 2.6 What survives what

| | restart | redeploy |
|---|---|---|
| `~/.zcp/z3` (the bundle) | kept | lost |
| `~/.t3` (threads, sessions, auth) | kept | **lost — recreated empty** |
| `~/.zcp/z3.env` | kept (rewritten anyway) | rewritten by the new container's init |
| `zerops@z3` unit | kept | lost, re-created by init |
| `/var/www/.zcp/state/init-complete` | rewritten each boot | rewritten each boot |
| live nginx edits | erased (template re-rendered) | erased |
| container id | unchanged | changes |

A restart is also an upgrade — `install.sh` re-runs and replaces `/usr/local/bin/zcp` (measured,
see the ledger). Thread history is one redeploy away from gone; a client surfaces that first.

### 2.7 Base path on the z3 side

nginx strips the prefix (§2.4); the server learns its **public** prefix from `--base-path` /
`T3CODE_BASE_PATH` and joins it onto every absolute URL it emits (assets, `/ws`, well-known,
`pairUrl`). The web bundle bakes the same prefix at build time (`VITE_BASE_PATH`) into
`index.html`, the manifest, and the router's `basepath` — a default build is byte-shape identical
to upstream. `ExecutionEnvironmentDescriptor.basePath` is **optional**: an older server stating
none is still accepted, so a client built for one prefix pointed at a server published under
another reaches a descriptor that disagrees — a mismatch a status code alone cannot see.

A request still carrying the configured prefix past nginx (a `proxy_pass` missing its trailing
slash) gets a **loud `404 application/json`** naming the mistake, not the SPA catch-all's silent
`200 text/html`. A tolerant ingress-side strip was dropped — any middleware passed to Effect's
`HttpRouter.serve` leaves its `R` type parameter unsolvable, a structural dead end. Consequence:
code-server's `/proxy/3773/` door and direct-to-3773 browsing do not work with a `/z3/`-built
bundle; `/z3/` is the only supported origin.

### Invariants

| ID | Invariant |
|---|---|
| Z3D-1 | A bundle at `z3.BinPath()` is used as-is (no version check, no network); the init step never fails the container start, degrading instead. `TestRun_Z3_UsesExistingBundle_NoInstall`, `TestRun_Z3_InstallFailure_Degrades`, `TestRun_Z3_NoProjectID_Degrades`. |
| Z3D-2 | `--base-path` is passed only when the installed bundle's `serve --help` advertises it. `TestServeArgv`, `TestSupportsBasePath`, `TestStart_Z3_Argv`. |
| Z3D-3 | The env contract carries only non-secret identifiers; an absent `ZCP_Z3_ALLOWED_ORIGINS` leaves that key unwritten. `TestEnvLines`, `TestRun_Z3_WritesEnvContract`, `TestRun_Z3_WritesAllowedOrigins_WhenConfigured`. |
| Z3D-4 | `/z3/`, `/healthz` render outside the cookie gate; code-server's `/proxy/3773/`/`/absproxy/3773/` are closed. `TestRunNginx_Z3OutsideCookieGate`, `TestRunNginx_ClosesCodeServerProxyDoorToZ3`. |
| Z3D-5 | `/healthz` answers before AND after the first `zcp init` completes, as parseable JSON, whether or not a step degraded. `TestRunNginx_HealthzServesTheInitMarker`, `TestRunNginx_HealthzFallbackIsValidJSON`, `TestRun_WritesInitCompleteMarker`, `TestRun_Z3_DegradedStepStillMarksInitComplete`. |
| Z3D-6 | A local (non-container) `zcp init` installs no bundle, writes no env file, leaves no marker. `TestRun_NoZ3_OutsideContainer`. |
| Z3D-7 | A request still carrying the base path past the proxy gets a named `404`, never the SPA shell; client-side helpers preserve a URL's prefix. `server.test.ts` — "names a forwarded base path instead of answering with the shell"; `packages/shared/src/basePath.test.ts`. |

---

## 3. The door (S1)

A z3 server running inside a Zerops project lets a member in on their own Zerops identity — no
pairing code, no shared container secret, no second session model. The mechanism lives in
`apps/server/src/zerops/`: one detection rule, one endpoint exchanging a Zerops access token for
the ordinary pairing grant every other bootstrap method produces, and upstream seams (CORS, WS
upgrade, session descriptor, admin link) narrowed for a server on the public internet.

### 3.1 The environment rule

One explicit signal: `T3CODE_ZEROPS_PROJECT_ID` set and non-empty ⇒ Zerops mode
(`resolveZeropsEnvironment`); nothing else votes. Every Zerops-specific behaviour keys off
`config.zerops !== undefined` (`isZeropsEnvironment`), never a re-derivation of the rule. `zcp init`
sets it from `runtime.Info.ProjectID` (§2.3); a laptop, a desktop build or a plain `t3 serve` never
has it and keeps every upstream behaviour untouched.

### 3.2 Identity bootstrap: `POST /api/auth/zerops-identity`

The client presents its own Zerops access token; the server proves membership with two reads
against the public Zerops REST API, **using the caller's token, never the container's**:

1. `GET {apiBase}/project/{projectId}` — membership: `200` member, `403 insufficientPermissions`
   not a member, `401 notAuthorized` invalid token, `400`/`404` unknown project.
2. `GET {apiBase}/user/info` — the caller's user id, and the role from the `clientUserList` entry
   whose `clientId` matches the *project's* (never "has any org" — a user can sit in several).

On success the server mints the ordinary pairing grant (`createPairingLink`), scoped to
`zeropsGrantScopes` (§3.6), `subject = <Zerops userId>`, a 2-minute redeem window, label
`"Zerops <role>"`. The token is a parameter and a request header only — never stored, logged, or
carried in a failure payload. A DPoP-bound caller proves its key in the same request and the grant
carries that thumbprint, redeemable only with the matching proof.

| Upstream membership result | Client response |
|---|---|
| Member (`200`) | `200` — ordinary `{id, credential, expiresAt}` |
| Not a member (`403`) | `403 operation_forbidden` `zerops_project_membership_required` — no grant |
| Invalid token (`401`) | `401 auth_invalid` `invalid_credential` |
| Unknown project (`400`/`404`) | `404 not_found` `zerops_project_not_found` |
| Zerops mode off / unresolved / platform unreachable | `404 not_found` `zerops_identity_unavailable` — or `500 internal` `zerops_membership_check_failed` on a transport failure |

### 3.3 The membership window

The server holds no caller token and the platform has no member-list endpoint, so membership
cannot be re-checked server-side. Instead the window IS the session's lifetime: whenever
`config.zerops` is set, **every** session `exchangeBootstrapCredentialForAccessToken` issues is
capped at `T3CODE_ZEROPS_MEMBERSHIP_TTL_SECONDS` (default 900s) — including a DPoP session, whose
upstream default would otherwise be one hour. When the window lapses the next connect fails and the
client silently re-mints with the Zerops token it still holds; *that* re-mint is the real
membership check — removing a member ends access within one window, with no stored credential and
no second state field; an already-open socket is not torn down mid-window. `revokeBySubject(userId)`
revokes every live session for one user immediately (an ops-path primitive) — a no-op on an unknown
subject, counted once per session however often it is called.

### 3.4 Origin and CORS allowlist

Upstream leaves CORS at a wildcard and puts no `Origin` check on the WS upgrade — survivable on
loopback, not for a server nginx publishes on the public internet. In Zerops mode both close over
one allowlist (`makeZeropsOriginAllowlist`): the container's own origin (matched per request
against `Host`/`X-Forwarded-Host`, never configured — same-origin under `/z3/` never reaches CORS
at all, only the WS upgrade); `localhost` on any port/scheme, matched on **hostname** never a
suffix, not extended to `127.0.0.1`; the two desktop shell origins (`t3code://app`,
`t3code-dev://app`); anything in `T3CODE_ZEROPS_ALLOWED_ORIGINS`, matched exactly.

A request with **no** `Origin` is allowed to upgrade — a non-browser caller cannot be forged. A
foreign `Origin` is refused `403 operation_forbidden` `origin_not_allowed` **before** the ticket is
read; no `Origin` falls through to fail on the credential instead (`401`), as outside Zerops mode.

### 3.5 Sessions: bearer/DPoP only, no admin link

In Zerops mode the descriptor drops `browser-session-cookie` from `sessionMethods`
(`["bearer-access-token","dpop-access-token"]`) and puts `zerops-identity` ahead of
`one-time-token` in `bootstrapMethods` — a signed-in member can still pair a second device the
ordinary way. `POST /api/auth/browser-session` refuses outright (`403 operation_forbidden`
`browser_session_unsupported`, credential never consumed): a cookie is the one credential a browser
attaches on its own, and a server on the public internet issues none, closing the CSRF surface the
wildcard CORS would otherwise open.

Upstream mints an `administrative-bootstrap` credential — wider-scoped than an ordinary grant — on
every boot, printed to stdout or turned into a `/pair#token=` link (measured, see the ledger). In
Zerops mode `resolveStartupAccessMode` resolves to `"zerops"` instead of `"headless"`/`"browser"`:
nothing is minted at startup, and the boot output names the identity door as the way in instead.

### 3.6 Exec RPC and scopes

`exec.run` — one command, one result, no session, no pty (the terminal RPCs already cover
interactive work) — runs over the same authenticated WS as the rest of the RPC surface, returning
the result directly rather than streaming output frames. `args` is a list handed straight to the
process spawner: no command-line composition, no shell to interpret metacharacters. A non-zero
exit is a **successful RPC** — `{exitCode, stdout, stderr, timedOut, stdoutTruncated,
stderrTruncated}` — never an RPC-level failure; only "could not run at all" (bad timeout, spawn
failure) fails the call. Default timeout 60s, capped at 600s, each stream truncated past 1 MiB.

`exec:operate` gates the RPC (`RPC_REQUIRED_SCOPES`) and is granted by `zeropsGrantScopes` — the
identity door's own set, never the plain client scopes an ordinary pairing carries. A project
member can already open a shell here through code-server and the coding agent, so the scope adds no
reach beyond what the door already proved.

### Outside a Zerops project, nothing changes

Every seam above is gated on `config.zerops !== undefined`. With the variable unset, the descriptor
reports `bootstrapMethods: ["one-time-token"]` (or the desktop set), CORS stays wildcard, the WS
upgrade takes no `Origin` at all, the admin-bootstrap link mints every boot as before, and
`exec.run` is unreachable — a plain `t3 serve` run outside `zcp` is unchanged from upstream.

### Invariants

| ID | Invariant |
|---|---|
| Z3S1-1 | `T3CODE_ZEROPS_PROJECT_ID` set and non-empty is the ONLY signal for Zerops mode; nothing else votes, and the token is never stored, logged, or carried in an error payload. `ZeropsEnvironment.test.ts`, `ZeropsIdentity.test.ts`. |
| Z3S1-2 | A non-member gets `403` and no grant; an invalid token gets `401` and no grant. `ZeropsIdentityGate.test.ts` — "refuses a non-member and leaves no grant behind", "refuses an invalid token and leaves no grant behind". |
| Z3S1-3 | Every session in Zerops mode is capped at the membership TTL, including a DPoP session that would otherwise default to one hour. `ZeropsIdentityGate.test.ts` — "exchanges into a session whose subject is the user and whose life is the window". |
| Z3S1-4 | `revokeBySubject` revokes exactly one user's sessions, is a no-op on an unknown subject, and counts each session once. `EnvironmentAuth.test.ts` — "revokes every session belonging to one subject and leaves the rest", "counts each session once, however often the subject is revoked". |
| Z3S1-5 | A foreign `Origin` is refused on the WS upgrade before the ticket is read; no `Origin` falls through to fail on the credential. `origin.test.ts`; `server.test.ts` — "refuses a websocket upgrade from a foreign origin, before authenticating". |
| Z3S1-6 | In Zerops mode no browser-session cookie is ever issued, and the credential is not consumed trying. `server.test.ts` — "refuses to open a cookie session inside a Zerops project". |
| Z3S1-7 | `exec:operate` is granted only by the identity door, never the standard client scope set, and a failing command is a successful RPC. `ExecService.test.ts`; `RpcAuthorization.ts` wiring. |
| Z3S1-8 | Outside a Zerops project every seam above is inert. `server.test.ts` — "leaves the websocket upgrade alone outside a Zerops project"; `ExecService.test.ts` — "is not offered outside a Zerops project". |
