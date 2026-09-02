# Spec: Zerops Mate (`mate`)

Zerops Mate — `mate` in code, paths and unit names — is a fork of the open-source T3 Code that rides inside the `zcp` container. Its server runs
next to nginx and code-server and spawns the coding agent (Claude Code, Codex) with ZCP's MCP
tools attached; its client — web, desktop, mobile — is the product surface a Zerops user signs
into. Because the agent operates the project through the same `zerops_*` tools an agent in a
terminal uses, mate's UI is not a second control plane: it is a **reader** of what those tools
report. That reading contract is what this spec owns.

- Delivery — how the mate bundle gets into the container, the supervised process, nginx, the
  `/mate/` base path, readiness — §2.
- The door — the Zerops-identity bootstrap a project member uses to reach a hosted mate server,
  with no pairing code and no shared container secret — §3.
- Client flow — how a browser reaches a hosted mate: session/candidates, registration, the
  provisioning waiter, the readiness probe, identity connect, new project, first prompt — §4.
- Zerops-aware client — the server's topology and lifecycle feeds over `zcp studio`, and the
  web surfaces that read them: service map, lifecycle strip, result cards, quick actions — §5.
- Git — each mounted dev service is its own repository, reached over a multiplexed SSH
  connection rather than the sshfs mount; multi-repo checkpoints, diff, restore, pruning — §6.
- The fork — Zerops Mate is a hard fork of T3 Code: frozen upstream base, four zones (import / port /
  owned / owned product), the adapter SPI between ported drivers and owned code, the upstream
  intake ritual — §7 (rules and measurements live in the fork: `../z3/docs/internals/zerops/`).
- Agent authorization — the agent CLI signs in from inside mate: a self-verified agent-auth feed,
  the platform flag written by `zcp agent mark-oauth`, server-driven login sessions whose parsed
  prompts (URL, device code) reach the client as actions — §8.
- Clients on Zerops only — desktop as a pure hosted client, the activity relay re-shelled for
  Zerops with project-bound environment links, T3 Connect reach gone — §9.
- Related: `docs/spec-workflows.md` (the envelope/plan/atom pipeline that produces the
  state), `docs/spec-work-session.md` (per-PID session, compaction survival).

---

## 1. Envelope on the wire

The mate client rebuilds a thread's lifecycle state by reducing over the provider's tool-result
stream. It never reads `.zcp/state` and never calls the Zerops API for lifecycle: the
`workflow.StateEnvelope` a workflow-aware tool already computes **is** the state. For that to
work the envelope has to survive the trip from the MCP handler, through the provider CLI, to
the mate server's reducer.

### 1.0 Two carriers, one reducer

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
  `workflow.ExtractEnvelope`. A mate client implements the same rule in TypeScript.

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

mate rides inside the `zcp` container rather than the platform's `zcp@1` recipe: installed and
supervised by `zcp init` (a `run.init` command), reached through the container's existing nginx on
port 8080. A plain restart re-runs the recipe's `install.sh`, picks up the latest zcp release, and
turns mate on for a project whose container predates it — no platform-side change. Everything zcp
knows about mate lives in `internal/mate`, kept to stdlib plus `runtime`/`schema`.

### 2.0 The gate — nothing mate-shaped happens unasked

**`ZCP_MATE_ENABLED` is the single input mate is keyed off**, read once into `runtime.Info.MateEnabled`
beside the `ZCP_AUTHORING` gate. It accepts `1` or `true`, case-insensitive, surrounding space
tolerated — deliberately more forgiving than `ZCP_AUTHORING`'s exact `1`, because this one is typed
into a service's env in the Zerops GUI, where a silently ignored value is indistinguishable from a
broken feature.

The governing rule is stated as a test, not as prose: **with the flag unset, a container running a
zcp release that carries this delivery path behaves exactly as one that predates it.** Concretely,
flag off means

| | with the flag off |
|---|---|
| bundle | nothing downloaded, nothing installed, no network request made |
| unit | no `zerops@mate` registered — and a leftover one is stopped and removed (§2.1b) |
| nginx | no `/mate/` location, no `/mate/healthz`, and **`/proxy/3773/` left open**, so the port is an ordinary user port again |
| root `/healthz` | code-server's own, unshadowed |
| `zcp init` output | not one extra line — the step is not even registered (§2.1) |
| readiness marker | not written |
| git | untouched; zcp writes no `.gitignore` in any configuration (§6) |

`zcp init` is the reconciler for all of it, and it converges **both** directions: turning the flag
off and restarting is a supported operation, not a state zcp only knows how to enter.

### 2.1 The init step — a reconcile, and an update lifecycle

`zcp init` gains a container-only step, **Zerops Mate (mate)**, after the SSH-config step. It is
**registered only when there is something to reconcile** — `MateEnabled`, or a leftover unit file a
now-off flag has to remove — so a container that never had mate prints not one extra line (§2.0).

**Enabled** (`reconcileMate` → `enableMate`):

1. **Refuse without a project.** `runtime.Info.ProjectID` empty ⇒ degrade — a non-empty project id
   is the sole signal the server binds to a Zerops project.
2. **Bundle** — `mate.EnsureInstalled` (below) in full.
3. **Capability note.** `mate.SupportsBasePath` reads `serve --help` once; unadvertised ⇒ logged to
   stderr (§2.2) — such a bundle answers under `BasePath` but its root-absolute assets hit the
   cookie gate instead.
4. **Environment.** `~/.zcp/mate.env` rewritten (mode 0600) every boot — §2.3.
5. **Unit.** `mate.UnitFilePath` absent ⇒ `sudo -E zsc unit create mate "zcp service start mate"`.

The step is **best-effort** (`step.degraded`): a release 404, an unset/mismatched digest, or an npm
dependency failure names the cause but `zcp init` still exits successfully — it is a `run.init`
command, and mate must never take a container start down with it. When the bundle cannot be had **no
unit is registered** — an unresolvable ExecStart crash-loops at every boot. The unit file's
presence, not a `zsc unit` upsert (there is none), is the idempotency check.

### 2.1a The update lifecycle — `mate.EnsureInstalled`

Each version is its own complete npm prefix, and a symlink names the live one:

```
~/.zcp/mate/versions/0.1.0/node_modules/.bin/mate
~/.zcp/mate/versions/0.2.0/node_modules/.bin/mate
~/.zcp/mate/current -> versions/0.2.0        # relative target
BinPath() = ~/.zcp/mate/current/node_modules/.bin/mate
```

`InstalledVersion()` reads `current/node_modules/zerops-mate/package.json` — **npm's own record**,
never a side file zcp would have to keep honest. `DesiredRelease()` answers `{Version, URL,
SHA256}`; today that is exactly the compiled-in pin, and §2.8 says why it stays one.

One pass, in order:

1. **Compare.** Installed == desired ⇒ done, **no network request made at all**. This is what keeps
   a warm restart off the network, and it is what the old "the binary exists" rule was reaching for
   without being able to say so.
2. **Keep a dev build.** An installed *semver prerelease* (`0.1.0-dev.<sha>`, what
   `eval/scripts/mate-dev-push.sh` tags) is never replaced by the pinned release unless `Force`. The
   protection is now a stated rule rather than an accident of which files happen to exist.
3. **Stage** into a fresh `versions/<desired>`: one `GET` to `ReleaseURL`, streamed to a temporary
   file while its SHA-256 is computed and compared with `PinnedSHA256`. A mismatch reports both
   digests and **never invokes npm**; an empty pin refuses before the first request. Only a matching
   tarball reaches `npm install --prefix versions/<desired> …`. npm still resolves the package's
   dependencies from its registry, so this is not an offline install. Download and npm share one
   3-minute deadline.
4. **Smoke** the staged binary (`mate --version`, 15 s).
5. **Activate atomically** — build the symlink under a temporary name and `os.Rename` it onto
   `current`, so an interrupted activation leaves either the old link or the new one, never a
   half-written one.
6. **Prune** to the two newest version directories; the live one is never removed.

**Any failure in 3–5 returns before the rename**, removing the half-built version directory, so
`current` still names the version that was working — the guarantee the whole layout exists for.

7. **Restart the unit** when it already existed and step 3 replaced the bundle (or the env contract
   changed, §2.3). The unit does not wait for this command — it starts at boot from
   `WantedBy=multi-user.target`, so it is already serving what was on disk then; without this a
   moved pin would land on disk and serve only from the *next* restart, which is not what
   "a restart is also an upgrade" (§2.6) promises. A unit created by this same run is left alone —
   `zsc unit create` starts it.

**`zcp mate update [--force]`** runs the identical pass from the CLI and then restarts `zerops@mate`
when the unit is registered, so an update needs no container restart.

### 2.1b Disabling — the reverse direction

`MateEnabled` false with a unit file present: stop the unit (best-effort — it may already be stopped),
`sudo -E zsc unit remove mate` (a real failure here is the one error this branch returns), and delete
`~/.zcp/mate.env`. **`mate.Prefix()` is deliberately left alone**: the downloaded bundle stays on disk,
so re-enabling costs a `zcp init` and no network. With no unit file there is nothing to do and the
step does not even register.

### 2.1c The pin, and moving it

The pin currently rides `v0.2.1` / `zerops-mate-0.2.1.tgz` (19,919,871 B), with locally computed
SHA-256 `749071c18705ff1e5fa9a45339a6f22e1b7916aad87222a1f228b98284eb03d9` — the release that renames the
product to Zerops Mate (package `zerops-mate`, executable `mate`) and keeps the sign-in hand-over on its registered mode, which is the half of that change the fork
owns. The release owner fills the digest only after publishing the tag: download the release asset, compute its SHA-256 locally, compare it
with the release's `SHA256SUMS` as a human cross-check, and paste the locally computed lowercase
64-hex digest into `PinnedSHA256`. `SHA256SUMS` never becomes the authority because it travels with
the artifact. `PackageName` (`zerops-mate`) and `PinnedVersion` are the only asset-identity inputs;
the asset name and URL derive from them. The fork's workspace package deliberately remains named
`t3`; it does not identify the release artifact zcp downloads.

For every later release, update `PinnedVersion` and the locally computed `PinnedSHA256` **in the
same commit**. The release must exist and its pin must be committed and verified **before any zcp
release containing the delivery path**. The empty-pin guard remains defense in depth.

### 2.2 The supervised process

`zcp service start mate` runs `mate.ServeArgv(bin, withBasePath)` — never `npx` (resolving the package
at every start cost 58 s cold, measured, see the mate ledger; the argv always runs the local bundle):

```
~/.zcp/mate/node_modules/.bin/mate serve --mode web --host 127.0.0.1 --port 3773 [--base-path /mate] \
  --base-dir ~/.t3 --no-browser --auto-bootstrap-project-from-cwd /var/www
```

- `--auto-bootstrap-project-from-cwd` is **boolean**; the workspace (`/var/www`) is a trailing
  **positional** — writing it as the flag's value bootstraps the unit's launch directory instead.
  `--base-dir` (`~/.t3`) keeps thread history across a restart; a redeploy starts it empty.
- **`--base-path` is a capability, not a preference**: passed only when `mate.SupportsBasePath(bin)`
  is true — the CLI treats an unknown flag as a fatal parse error, and the fork reports the same
  version string with and without it, so it cannot be gated by version. Omitting it degrades safely
  (only assets miss) and is logged at both `zcp init` and the unit's journal.
- **An explicit `--auto-bootstrap-project-from-cwd` wins over the `serve` command's own opt-out.**
  Upstream's `serve` command sets a headless startup presentation that, left to itself, forces
  auto-bootstrap off; the fork's config resolution checks the explicit flag *first*, so zcp's
  boolean flag (above) still lands. The first boot that finds none creates one project titled from
  `path.basename(cwd)` and one thread; every later boot that finds them reuses both rather than
  re-creating. `config.test.ts` — "honors an explicit auto-bootstrap flag over the serve command
  opt-out"; `serverRuntimeStartup.test.ts` — "resolveAutoBootstrapWelcomeTargets returns existing
  project and thread ids".

### 2.3 The environment contract

`zcp init` writes the following while the container's full environment is present;
`service.Start("mate")` merges it over the unit's own, not guaranteed to inherit it:

| Key | Source | Written when |
|---|---|---|
| `T3CODE_ZEROPS_PROJECT_ID` | `runtime.Info.ProjectID` | always — THE Zerops-environment signal; nothing else votes |
| `T3CODE_ZEROPS_API_HOST` | `mate.ResolveAPIHost(ZCP_API_HOST)`, else `schema.CanonicalAPIHost` | always — bare host, scheme/trailing slash stripped, port kept |
| `T3CODE_ZEROPS_ALLOWED_ORIGINS` | service env `ZCP_MATE_ALLOWED_ORIGINS` | only when set — unwritten ⇒ server's own default |

Only non-secret identifiers are written; a token never enters `~/.zcp/mate.env` (mode 0600),
rewritten every boot so a unit's frozen ExecStart never has to change. A missing/unreadable env
file is reported and mate starts anyway — diagnosable, unlike a unit that refuses to launch.

### 2.4 nginx — three locations, all outside the cookie gate, all behind the gate

Rendered identically whether or not `VSCODE_PASSWORD` is set, and **only when `MateEnabled`** — all
three live inside one `{{- if .MateEnabled}}` region.

| Location | Behaviour |
|---|---|
| `{BasePath}/` (`/mate/`) | Proxies to `http://127.0.0.1:3773/` — **trailing slash strips the prefix**, so mate's routes stay at the loopback root and only URLs it *emits* (`--base-path`) carry it. Websocket upgrade headers, `proxy_read_timeout 86400s`. Outside the cookie gate: mate owns its own auth (§3). |
| `~ ^/(abs)?proxy/3773(/|$)` | `return 404`. code-server's `/proxy/<port>/`/`/absproxy/<port>/` reach any loopback port for whoever holds the container cookie — a second door, closed; evaluated before `location /`. Closed **only while mate is enabled**: with the flag off nothing of ours listens on 3773 and the port is an ordinary user port. |
| `= {BasePath}/healthz` | Serves `mate.InitMarkerPath` verbatim, `application/json`, `no-store`; falls back to `{"initComplete":false,"initAt":null}` with no marker yet. No proxy, no process — answers even when nginx is all that's up. |

The readiness route lives **inside mate's namespace, not at the container root**. An nginx exact match
beats a prefix location regardless of source order, so `= /mate/healthz` is served from the static
marker and never reaches the `/mate/` proxy; it sits next to that block for readability only. Two
reasons it is not `= /healthz`: that path is code-server's own, and shadowing it took something away
from every container; and under an opt-in gate the route's *existence* is information — answering
means mate is enabled here, `404` means it is not, a distinction a route that answered on every zcp
container could not make.

Both readiness branches (marker-present and the uninitialized fallback) also send
`Access-Control-Allow-Origin: *` — a hosted mate web client (§4) reads its own container's origin
before it holds any credential, so the response has to survive a cross-origin `fetch` even though
its body is only two non-secret fields. `/mate/` and the cookie-gated `location /` stay CORS-less:
`/mate/` inherits mate's own allowlist (§3.4), and code-server's location is never fetched
cross-origin. `TestRunNginx_HealthzHasCORSForCrossOriginProbe`.

With the flag off the render is **byte-for-byte identical** to the pre-mate one, with and without a
password — verified by rendering both templates side by side, and pinned by
`TestRunNginx_MateDisabled_RendersNoMateSurface`, which asserts the absence of `/mate`, `3773`, `healthz`
and the marker path *and* the presence of every non-mate structure.

Live edits do not survive — every boot re-renders `internal/content/templates/nginx.conf.tmpl`.

### 2.5 Readiness — two probes, no process

**`GET {BasePath}/healthz`** → always `200 application/json` *while mate is enabled*: the marker
`zcp init` writes at
`/var/www/.zcp/state/init-complete` when its step list ends —
`{"initComplete":true,"initAt":"<RFC3339>"}`. Records the list **finished**, not that every step
succeeded (a degraded mate step still leaves the marker); `initAt` moving is how a client sees a
restart re-initialized the container.

**`GET {BasePath}/.well-known/t3/environment`** → mate's own liveness: `200` **and**
`content-type: application/json` **and** a body carrying `"basePath":"/mate"` — never the status
code alone, since a stripped or mis-proxied prefix answers `200 text/html` from the SPA catch-all.
`mateUp` is the **client-side conjunction of both**: stock nginx cannot branch a response body on a
subrequest, so folding both fields into the readiness route would need the sidecar process this
design removes. A client must also read a `404` on `{BasePath}/healthz` as its own third state —
*mate is not enabled on this container* — distinct from "still starting". Budget (measured, see the ledger): a restart is ~17 s to `/healthz`, ~19 s to `mateUp`,
~14 s of L7 `502` in between — poll `mateUp`, render `502` as "restarting", cap at 30 s.

### 2.6 What survives what

| | restart | redeploy | disable (§2.1b) |
|---|---|---|---|
| `~/.zcp/mate/versions/*` (the bundles) | kept | lost | **kept — deliberately** |
| `~/.zcp/mate/current` (the live link) | kept | lost | kept (points at a version nothing runs) |
| `~/.t3` (threads, sessions, auth) | kept | **lost — recreated empty** | kept |
| `~/.zcp/mate.env` | kept (rewritten anyway) | rewritten by the new container's init | removed |
| `zerops@mate` unit | kept | lost, re-created by init | **stopped and removed** |
| `/var/www/.zcp/state/init-complete` | rewritten each boot | rewritten each boot | not rewritten |
| live nginx edits | erased (template re-rendered) | erased | erased |
| container id | unchanged | changes | unchanged |

A restart is also an upgrade — `install.sh` re-runs and replaces `/usr/local/bin/zcp` (measured,
see the ledger), and the new binary's pin is what the next `zcp init` reconciles toward (§2.1a).
Thread history is one redeploy away from gone; a client surfaces that first. A **redeploy** losing
the bundle is not a regression under the versioned layout — it loses the whole container — and a
**disable** keeping it is what makes re-enabling free.

### 2.6a What a broken mate costs — measured

The unit `zsc unit create` writes is `Restart=always`, `RestartSec=3`, under systemd's default
`StartLimitBurst=5` / `StartLimitIntervalSec=10`. One restart every 3 s is ~3.3 per 10 s, **below**
the burst threshold — so a unit whose `ExecStart` fails immediately **never trips the start limit
and never gives up**. Measured on `z3-eval` with the bundle path broken: the restart counter climbs
linearly (4–5 per 15 s) and the unit stays `activating` indefinitely; it never reaches `failed`.

That is survivable only because the blast radius is one route. Measured in the same state:

| | |
|---|---|
| Zerops service status | `ACTIVE` |
| `zerops@nginx`, `zerops@vscode` | `active` |
| code-server `/`, `/healthz` | unaffected |
| `{BasePath}/healthz` | **`200`** — a static file nginx serves with no process behind it, which is exactly why readiness is not a proxied route (§2.5) |
| `{BasePath}/` | `502` |

So a mate that cannot start costs `/mate/` and nothing else: the container starts, stays healthy, and
keeps serving the editor. Recovery is `zcp init` — and it works even for a container whose flag is
now off, because the step registers on `MateEnabled || <unit file exists>` (§2.1), so a stray unit is
reconciled away rather than left looping. A zcp release that predates mate has no such step and would
leave the loop running (`unknown service "mate"`), which is one more reason a container should not sit
between the two for long.

### 2.7 Base path on the mate side

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
code-server's `/proxy/3773/` door and direct-to-3773 browsing do not work with a `/mate/`-built
bundle; `/mate/` is the only supported origin.

### 2.8 The zcp↔mate contract

zcp and mate ship from two repositories on two schedules, and the coupling between them is the handful
of facts below — none of which either side can change alone. **Today nothing in the code enforces
them: the hard pin does.** `PinnedVersion` moves only by a zcp commit, so a human reads this list
when they move it, and that is the whole enforcement mechanism.

That is also precisely why `DesiredRelease()` (§2.1a) still answers with a compiled-in version and
digest rather than resolving "latest". Automatic tracking needs a release to *declare* the contract
it satisfies, so zcp can refuse one it does not understand; no release does. When that changes, the
declared number and a `ContractVersion` in `internal/mate` become the check, and `DesiredRelease()` is
the one function that has to learn about it.

| # | The fact | Owned by |
|---|---|---|
| C-1 | The artifact is `zerops-mate-<version>.tgz`, a GitHub release asset on `zeropsio/mate`, whose npm `bin` entry is `mate` at `node_modules/.bin/mate` (`mate.BinName`, pinned together with the package name) | fork's `cli.ts pack` + release workflow |
| C-2 | `serve` accepts `--mode web --host --port --base-dir --no-browser --auto-bootstrap-project-from-cwd` with the working directory as a trailing **positional**. **An unknown flag is fatal**, so every flag added later reaches production only behind a capability probe — `--base-path` is the precedent and stays one (§2.2) | fork's `cli/config.ts` |
| C-3 | `T3CODE_ZEROPS_{PROJECT_ID,API_HOST,ALLOWED_ORIGINS}` keep their meaning, and a non-empty `PROJECT_ID` remains the sole Zerops-environment signal (§2.3, §3.1) | fork's `ZeropsEnvironment` |
| C-4 | Liveness is `GET {basePath}/.well-known/t3/environment` → `200 application/json` carrying `basePath` (§2.5) | fork's environment descriptor |
| C-5 | The server binds loopback only and never claims a declared platform port (§2.4) | zcp's `ServeArgv`, fork's `--host` |
| C-6 | **`/mate` is baked into the released artifact, not chosen by zcp.** The release workflow builds the bundled web client with `VITE_BASE_PATH=/mate`, and `pack` refuses a tarball without `dist/client/index.html`. `mate.BasePath` must equal it; moving the prefix is a coordinated two-repo change | fork's release workflow + `mate.BasePath` |

A later "latest compatible" resolver is well-formed on the release side already: the fork's release
workflow triggers only on stable `v<major>.<minor>.<patch>` tags, so the published release list *is*
the candidate set — nightlies never produce one.

**The web client is not zcp's.** zcp does not build it, configure it, serve it or update it; the
centralized client reaches the server over `{BasePath}/`. That the release tarball still carries a
client is incidental — zcp simply does not use it. A server-only artifact is not merely absent but
actively refused today (C-1's `pack` assertion), so it would be a fork-side change if ever wanted.

### Invariants

| ID | Invariant |
|---|---|
| MD-1 | The init step never fails the container start, degrading instead — for any install, download, integrity or unit-removal failure. `TestRun_Mate_InstallFailures_Degrade`, `TestRun_Mate_NoProjectID_Degrades`, `TestRun_MateDisabled_UnitRemoveFails_Degrades`. |
| MD-2 | `--base-path` is passed only when the installed bundle's `serve --help` advertises it. `TestServeArgv`, `TestSupportsBasePath`, `TestStart_Mate_Argv`. |
| MD-3 | The env contract carries only non-secret identifiers; an absent `ZCP_MATE_ALLOWED_ORIGINS` leaves that key unwritten. `TestEnvLines`, `TestRun_Mate_WritesEnvContract`, `TestRun_Mate_WritesAllowedOrigins_WhenConfigured`. |
| MD-4 | With the flag ON, `/mate/` and `/mate/healthz` render outside the cookie gate and code-server's `/proxy/3773/`/`/absproxy/3773/` are closed. `TestRunNginx_MateOutsideCookieGate`, `TestRunNginx_ClosesCodeServerProxyDoorToMate`. |
| MD-5 | `{BasePath}/healthz` answers before AND after the first `zcp init` completes, as parseable JSON, whether or not a step degraded. `TestRunNginx_HealthzServesTheInitMarker`, `TestRunNginx_HealthzFallbackIsValidJSON`, `TestRun_WritesInitCompleteMarker`, `TestRun_Mate_DegradedStepStillMarksInitComplete`. |
| MD-6 | A local (non-container) `zcp init` installs no bundle, writes no env file, leaves no marker. `TestRun_NoMate_OutsideContainer`. |
| MD-7 | A request still carrying the base path past the proxy gets a named `404`, never the SPA shell; client-side helpers preserve a URL's prefix. `server.test.ts` — "names a forwarded base path instead of answering with the shell"; `packages/shared/src/basePath.test.ts`. |
| MD-8 | mate's process environment merges `~/.zcp/mate.env` over the container's live env store, read once at unit start — so `zcp init` restarts the unit itself whenever it rewrote that file or replaced the bundle (MD-15). `TestLoadLiveEnv`, `TestMergeMateEnv_OrderAndPrecedence`. |
| MD-9 | `{BasePath}/healthz` carries `Access-Control-Allow-Origin: *` on both branches; `/mate/` and the cookie-gated `location /` carry none. `TestRunNginx_HealthzHasCORSForCrossOriginProbe`. |
| MD-10 | A fresh install refuses an unset digest before making an HTTP request; otherwise it downloads the pinned fork release asset, verifies it against the SHA-256 compiled into zcp before npm runs, and has no registry-package fallback. Download, integrity, and npm failures register the same degraded init outcome and no unit. `TestInstallArgs_UsesPinnedReleaseAsset`, `TestInstallRelease_ChecksumMismatch_RefusesInstall`, `TestInstallRelease_DownloadFailure_RefusesInstall`, `TestInstallRelease_UnsetPinnedDigest_RefusesInstall`, `TestRun_Mate_InstallFailures_Degrade`. |
| MD-11 | **With `ZCP_MATE_ENABLED` unset, a container behaves exactly as one predating mate**: the rendered nginx.conf carries no `/mate`, no `3773`, no `healthz` and no marker path while keeping every non-mate structure; nothing is downloaded or installed; no unit is registered; no readiness marker is written; and `zcp init` prints no extra step line. `TestRunNginx_MateDisabled_RendersNoMateSurface`, `TestRun_MateDisabled_NoUnitFile_NoOp`, `TestDetect_MateDisabled_ByDefault`. |
| MD-12 | Disabling is a real reverse direction, not an absence of the forward one: a leftover unit is stopped and removed and `~/.zcp/mate.env` deleted, while `mate.Prefix()` is left on disk so re-enabling costs no network. `zcp service start mate` refuses under the off flag, so a unit surviving a failed removal cannot resurrect the server — reading the flag from the **live env store**, never from its own environment, because a systemd unit inherits neither (live-verified: a guard reading `os.Environ` crash-looped the unit on an enabled container). An unreadable store fails OPEN, since the unit exists only because an enabling `zcp init` created it. `TestRun_MateDisabled_UnitFilePresent_StopsAndRemoves`, `TestStart_Mate_GuardReadsLiveEnvStore_NotOnlyProcessEnv`, `TestStart_Mate_GuardRefusesWhenStoreSaysDisabled`, `TestStart_Mate_GuardFailsOpenOnUnreadableStore`, `TestStart_OtherServices_UnaffectedByMateGuard`. |
| MD-13 | An update is staged into its own version directory, smoke-tested, and only then activated by an atomic symlink rename; **any failure leaves `current` naming the version that was working**. Equal versions reach no network at all, and an installed semver prerelease (a hand-pushed dev build) is never replaced without `Force`. `TestEnsureInstalled_SameVersion_NoNetwork_ResultNone`, `TestEnsureInstalled_DifferentVersion_InstallsAndRepointsCurrent`, `TestEnsureInstalled_NpmFailure_LeavesCurrentUnchanged`, `TestEnsureInstalled_SmokeFailure_LeavesCurrentUnchanged`, `TestEnsureInstalled_DevVersionInstalled_KeptWithoutForce`, `TestEnsureInstalled_DevVersionInstalled_ReplacedWithForce`, `TestEnsureInstalled_Pruning_KeepsTwoAndTheLiveVersion`. |
| MD-15 | The unit starts at boot on its own (`WantedBy=multi-user.target`), independently of `zcp init` — measured on `z3-eval`: `active` at 16:45:12, the zcp binary replaced by `install.sh` at 16:45:14, `zcp init` later still. So it serves whatever was on disk at boot, and `zcp init` restarts an ALREADY-EXISTING unit whenever it replaced the bundle or rewrote the env contract; a unit it created in the same run is left alone, since `zsc unit create` starts it. `TestRun_Mate_UpdatedBundle_RestartsExistingUnit`, `TestRun_Mate_ChangedEnvContract_RestartsExistingUnit`, `TestRun_Mate_UnchangedBundle_DoesNotRestart`, `TestRun_Mate_FirstBoot_DoesNotRestartFreshUnit`. |

---

## 3. The door (S1)

A mate server running inside a Zerops project lets a member in on their own Zerops identity — no
pairing code, no shared container secret, no second session model. The mechanism lives in
`apps/server/src/zerops/`: one detection rule, one endpoint exchanging a Zerops access token for
the ordinary pairing grant every other bootstrap method produces, and upstream seams (CORS, WS
upgrade, session descriptor, admin link) narrowed for a server on the public internet.

### 3.1 The environment rule

One explicit signal: `T3CODE_ZEROPS_PROJECT_ID` set and non-empty ⇒ Zerops mode
(`resolveZeropsEnvironment`); nothing else votes. Every Zerops-specific behaviour keys off
`config.zerops !== undefined` (`isZeropsEnvironment`), never a re-derivation of the rule. `zcp init`
sets it from `runtime.Info.ProjectID` (§2.3); a laptop, a desktop build or a plain `mate serve` never
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
cannot be re-checked server-side. Instead the window IS the session's lifetime — but only for the
sessions that can renew themselves. **The window follows the door, not the environment**: a grant
records which door minted it (`BootstrapGrant.method`, persisted on the pairing link), and
`exchangeBootstrapCredentialForAccessToken` caps a session at
`T3CODE_ZEROPS_MEMBERSHIP_TTL_SECONDS` (default 900s) iff that method is `zerops-identity` —
including a DPoP session, whose upstream default would otherwise be one hour. When the window lapses
the next connect fails and the client silently re-mints with the Zerops token it still holds;
*that* re-mint is the real membership check — removing a member ends access within one window, with
no stored credential and no second state field; an already-open socket is not torn down mid-window.

A session from `one-time-token` pairing (the authenticated second-device path, §3.5) keeps
upstream's lifetime, and a DPoP one keeps its hour. That device holds no Zerops token and so has
nothing to re-mint with: capping it at the window would end its session every 15 minutes with no
way back in, which is what an environment-wide cap did until it was found live on `z3-eval`.
`revokeBySubject(userId)` revokes every live session for one user immediately (an ops-path
primitive) — a no-op on an unknown subject, counted once per session however often it is called.

### 3.4 Origin and CORS allowlist

Upstream leaves CORS at a wildcard and puts no `Origin` check on the WS upgrade — survivable on
loopback, not for a server nginx publishes on the public internet. In Zerops mode both close over
one allowlist (`makeZeropsOriginAllowlist`): the container's own origin (matched per request
against `Host`/`X-Forwarded-Host`, never configured — same-origin under `/mate/` never reaches CORS
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
`exec.run` is unreachable — a plain `mate serve` run outside `zcp` is unchanged from upstream.

### Invariants

| ID | Invariant |
|---|---|
| MS1-1 | `T3CODE_ZEROPS_PROJECT_ID` set and non-empty is the ONLY signal for Zerops mode; nothing else votes, and the token is never stored, logged, or carried in an error payload. `ZeropsEnvironment.test.ts`, `ZeropsIdentity.test.ts`. |
| MS1-2 | A non-member gets `403` and no grant; an invalid token gets `401` and no grant. `ZeropsIdentityGate.test.ts` — "refuses a non-member and leaves no grant behind", "refuses an invalid token and leaves no grant behind". |
| MS1-3 | The membership window caps a session iff its grant came from the `zerops-identity` door, including a DPoP one that would otherwise default to one hour; a `one-time-token` pairing keeps upstream's lifetime, because it holds no Zerops token to re-mint with. `EnvironmentAuth.test.ts` — "caps a session from the identity door at the membership window", "leaves a one-time-token pairing on the ordinary session lifetime", "keeps DPoP's own lifetime for a one-time-token pairing", "caps a DPoP session from the identity door at the window, not the hour". |
| MS1-4 | `revokeBySubject` revokes exactly one user's sessions, is a no-op on an unknown subject, and counts each session once. `EnvironmentAuth.test.ts` — "revokes every session belonging to one subject and leaves the rest", "counts each session once, however often the subject is revoked". |
| MS1-5 | A foreign `Origin` is refused on the WS upgrade before the ticket is read; no `Origin` falls through to fail on the credential. `origin.test.ts`; `server.test.ts` — "refuses a websocket upgrade from a foreign origin, before authenticating". |
| MS1-6 | In Zerops mode no browser-session cookie is ever issued, and the credential is not consumed trying. `server.test.ts` — "refuses to open a cookie session inside a Zerops project". |
| MS1-7 | `exec:operate` is granted only by the identity door, never the standard client scope set, and a failing command is a successful RPC. `ExecService.test.ts`; `RpcAuthorization.ts` wiring. |
| MS1-8 | Outside a Zerops project every seam above is inert. `server.test.ts` — "leaves the websocket upgrade alone outside a Zerops project"; `ExecService.test.ts` — "is not offered outside a Zerops project". |

---

## 4. Client flow (S4)

The mate web client — hosted separately, not built into the container — is how a browser reaches a
project's mate server without a pairing code: it signs the user into their own Zerops account, finds
or creates the `zcp` container that runs mate, and drives the door (§3) to land in a thread. Every
call targets the public Zerops API directly except the identity exchange and mate's own endpoints
(§3.2); every *mutating* call is user-initiated — the client never calls one on its own.

### 4.1 Session client

`packages/client-runtime/src/zerops/api.ts` wraps Zerops REST auth. `/auth/login` and
`/auth/refresh` differ in shape: login nests session fields under `auth`, refresh returns them at
the **top level** — a client that assumes one shape for both breaks silently on refresh. A `403`
from refresh **leaves the session** (the caller may still be a member, just rate-limited); only an
**unrefreshable `401`** clears it and signs out; N callers racing a `401` collapse into one shared
refresh. TOTP is signalled by `twoFAMethods.length > 0 && twoFAVerified !== true`, completed with
`POST /2fa/totp/login {token}`.
`api.test.ts` — "maps 403 to a forbidden error carrying the platform code, keeping the session",
"maps an unrefreshable 401 to an expired-session error and signs out", "coalesces three parallel
401s into exactly one refresh", "signals TOTP from twoFAMethods and posts the code to
/2fa/totp/login".

The Zerops account session is also the outer product mount boundary for the hosted web client and
the desktop shell that embeds it. `loading`, `signed-out`, and `totp-required` render only the
Zerops account sign-in surface on every ordinary route: routed product content, the sidebar,
command palette, environment bootstrap/repair and renderer background hosts do not mount. Only
`signed-in` mounts the product. The `/zerops_/authorized` identity handover remains a bare route so
its credential fragment can be consumed, and the account gate never redirects it through login.
The exclusive signed-out surface does not offer manual backend pairing; `/pair` remains governed by
the separate door contract (§3).

### 4.2 Reaching a container: candidates and the picker

A zcp container is identified by **service type** (`serviceStackTypeVersionName` starting with
`zcp@`), never by hostname — a service named `zcp` running something else is not a candidate, and
a container named anything else still is. Candidates are derived across **every** org the account
belongs to; a project holding several containers offers each as a **separate** candidate. An
unavailable candidate names its own reason: project status, container status, public access off,
port 8080 not exposed, or no public subdomain.
`candidates.test.ts` — "finds a zcp container by service type, whatever its hostname is", "does not
mistake a service merely named zcp for a container", "offers every zcp container in a project, not
one per project", "reports a project that is not active as unavailable, naming its status", "names
the specific reason a container has no reachable origin".

### 4.3 Registration

`POST /registration` (`{email, password, name, accountName, languageId:"en", claimZcpPool:true,
token}`) claims a pool project by default. **Turnstile is unconditionally required** — there is no
captcha-less path; the client refuses to send the request at all with an empty token. Two failure
shapes both render the same fallback ("sign up at app.zerops.io and come back to sign in"): the
platform's own `cloudflareCaptchaVerificationFailed` code, and Cloudflare's own widget error
`110200` (a site key not bound to this hostname).
`api.test.ts` — "refuses to send a registration with no captcha token"; `turnstile.test.ts` —
"names the domain refusal the way a person can act on"; `registration.test.ts` — "recognises the
platform's captcha refusal".

### 4.4 The provisioning waiter

After registration or "New project" the client waits for the claimed project's `zcp` service, using
**direct reads only** (`GET /client/{id}/project`, `GET /project/{id}`, `GET
/project/{id}/service-stack`) — never `/project/search`, which lags a fresh write (the same ES-lag
rule `spec-workflows.md §3.5` states for zcp itself). States: `awaiting-project` (cap 60s) →
`awaiting-container` (cap 300s) → `awaiting-health` (cap 30s) → `ready`; plus `pool-exhausted`
(`zcpClaimed:false` — a fact, not a failure; a missing field reads as claimed), `needs-enable`
(§4.5), and `timed-out` (a cap expiry is retryable, never an error). An empty read is never a
verdict: absence keeps the waiter waiting rather than concluding "does not exist."
`provisioning.test.ts` — "reads projects through the direct read, never the search index", "never
concludes 'no container' from one read of a fresh project", "turns a cap expiry into a retryable
state, never an error", "an exhausted pool is a state of its own, not a failure".

### 4.5 The readiness probe

The mate **descriptor** (`GET {origin}/mate/.well-known/t3/environment`) is the authority, probed
first; `/healthz` (MD-9) is a fallback signal only, never the primary check — there is no `mateUp`
field in the shipped protocol. Four states: `ready`, `initializing`, `predates-mate`, `unreachable`.
A **5xx on either probe always resolves to `unreachable`**, never `predates-mate` — a transient 502
must not be misread as a container that lacks mate. **A pre-mate container and an unreachable one look
identical to a browser** (neither sends a CORS header, so every `fetch` throws the same way) — the
picker offers "Enable Zerops Mate" for **both**.

**Enable writes the flag, then restarts** (`enableZeropsMate`, the user's own token throughout).
A restart alone cannot turn mate on: `zcp init` registers no mate step without `ZCP_MATE_ENABLED` (§2.0),
so the container returns to the state it was restarted out of. Neither half is enough on its own —
a service env change reaches a container's process environment only at the boot `zcp init` reads it
on. The write is an upsert done as `GET /service-stack/{id}/env` → `DELETE /user-data/{id}` →
`POST /service-stack/{id}/user-data` (`sensitive` **required**; the bulk env-file PUT is never used
— it replaces the whole file and drops every other var the user set), and a flag that already reads
as on is left alone rather than rewritten: that is what makes Enable safe to offer for a container
that is merely away, and it never attempts a delete a yaml-baked key would refuse.

This is the ONLY path that turns mate on for a container this client did not create — pool containers
included. The platform's `zcp@1` recipe deliberately does **not** carry the flag: a zcp container
Zerops hands out is not a Zerops Mate container by default.
`containerHealth.test.ts` — "treats the mate descriptor as the authority, and asks nothing else once
it answers", "never reads a 5xx as a container that predates Zerops Mate", "reads the cookie
gate's redirect as a container that predates Zerops Mate"; `api.test.ts` — "writes the Zerops Mate
flag before restarting a container that lacks it", "replaces a Zerops Mate flag that is present but
switched off", "writes nothing when the flag already reads as on, and still restarts".

### 4.6 Identity connect

`connectZeropsIdentity` presents the Zerops access token in exactly **one** place across the whole
flow: the `token` field of the identity request's **body** — never a header. A non-member gets the
same generic `ConnectionBlockedError{reason:"permission"}` every other environment-auth failure
maps to — there is no Zerops-specific error type, and the mapping depends on the platform's `403`
body being contract-shaped.
`onboarding.zerops.test.ts` — "puts the Zerops token in the identity request and nowhere else",
"fails without registering anything when the account is not a member".

### 4.7 New project

"New project" is a wizard at `/zerops/new`, not a form: org scope (skipped when the account has one
membership — a one-option chooser is noise) → name and location → which coding agents the container
offers → the provisioning wait (§4.4), which lands in a thread through the same identity connect
(§4.6) every other path uses. The sidebar's `+` and its empty-state CTA are its entry points; the
`/zerops` picker reaches it where the create form used to sit, and so does the `pool-exhausted` state,
which otherwise has nowhere to go. The command palette's source list (folder, git URL, a provider's
repo) is NOT this: it adds a workspace to a container that already exists, and it lives as a
per-project row action.

The create itself is still two calls traced from the GUI: `POST /client/{id}/project` (`mode:"LIGHT"`),
then `PUT /project/{id}/first-class-recipe/development-container` with the platform's own import YAML.
`VSCODE_PASSWORD` is generated client-side (`crypto.getRandomValues`, rejection-sampled — no modulo
bias) and sent once; mate never reads it back. A second container in the same project is named `zcp1`.

The document is the GUI's byte for byte plus the keys this client adds, because the GUI has no reason
to write them:

- `ZCP_MATE_ENABLED` — without it `zcp init` installs no bundle, registers no unit and publishes no
  `/mate/` location, so the container could not serve the product that created it.
- `ZCP_AGENTS` — the agent selection, comma-separated in canonical order. This is the key the
  container acts on: its bootstrap's `resolveAvailableAgentIds` reads it live as **presentation
  policy** — which agents the container offers, in which order — and it is neither authorization nor a
  security boundary. An **absent** key offers every agent; an **empty** one offers none, failing
  closed by design. So an empty selection omits the key entirely rather than emitting `""`, and those
  two are opposite meanings that one test must keep apart.
- `ZCP_AGENT_AUTH_TYPE_<SUFFIX>: "oauth"` per selected agent — GUI parity only. Nothing inside the
  container reads it; the Zerops GUI's metadata parser does, and because `envSecrets` entries are
  `sensitive`, it reads back `REDACTED` and can tell presence but not mode.

The selection installs nothing. `zcp init`'s adapters only configure binaries the `zcp@1` image
already carries (`Detect` gates on `LookPath`). Authorization is a separate act that cannot happen
here at all: it needs an RPC on the mate server inside the container, so it is only reachable after
the connect — and only for the agents in `ZeropsAgentId`, while the rest sign in from the terminal.

`newProject.test.ts` — "generates the container password, sends it, and forgets it", "draws from the
injected randomness without modulo bias", "never emits a container with a public subdomain and no
password", "matches the platform's own numbering", "emits the platform's own import document, byte for
byte, plus the mate flag", "never emits ZCP_AGENTS at all when the list is absent or empty", "emits
ZCP_AGENTS as a comma-separated list in canonical order"; `ZeropsNewProjectWizard.test.tsx` — the
scope step's visibility and that the create carries the selected agents and the chosen location;
`ZeropsNewProjectAgents.test.tsx` — "marks the different sign-in phrase only for agents outside
ZeropsAgentId".

### 4.8 Landing in the thread

The mate server's own auto-bootstrap (§2.2) creates the first project and thread; the client composes
one fixed onboarding prompt into the composer after `connectZeropsIdentity` returns an environment
— filling it, never sending it, so the user reads it before it costs them a turn — guarded by a
marker keyed on `environmentId`, only for an identity-door environment, never a manually paired
one; a reconnect stays silent. The client label reads `Zerops Mate · <browser> on <os>` so two
devices on one container are told apart.
`firstPrompt.test.ts` — "composes once for a freshly connected Zerops environment", "stays quiet on
every reconnect to the same environment", "never writes into an environment somebody paired by
hand"; `clientMetadata.test.ts` — "names the device, so two clients on one container are told
apart".

### Invariants

| ID | Invariant |
|---|---|
| MC-1 | An unrefreshable `401` signs out; a `403` from refresh keeps the session. `api.test.ts` — "maps 403 to a forbidden error carrying the platform code, keeping the session", "maps an unrefreshable 401 to an expired-session error and signs out". |
| MC-2 | A candidate is identified by service type, never hostname; every zcp container in a project is offered separately. `candidates.test.ts` — "finds a zcp container by service type, whatever its hostname is", "offers every zcp container in a project, not one per project". |
| MC-3 | Registration never sends without a Turnstile token; the platform's captcha refusal and Cloudflare's own domain-binding error both render the same "sign up at app.zerops.io" fallback. `api.test.ts` — "refuses to send a registration with no captcha token"; `turnstile.test.ts` — "names the domain refusal the way a person can act on". |
| MC-4 | The provisioning waiter reads only direct endpoints, never `/project/search`; an empty read is never a "does not exist" verdict. `provisioning.test.ts` — "reads projects through the direct read, never the search index", "never concludes 'no container' from one read of a fresh project". |
| MC-5 | The mate descriptor is the readiness authority; a 5xx on either probe is always `unreachable`, never `predates-mate`; a pre-mate and an unreachable container get the same "Enable Zerops Mate" offer. `containerHealth.test.ts` — "treats the mate descriptor as the authority, and asks nothing else once it answers", "never reads a 5xx as a container that predates Zerops Mate". |
| MC-6 | The Zerops access token appears in exactly one request body field during identity connect, never a header. `onboarding.zerops.test.ts` — "puts the Zerops token in the identity request and nowhere else". |
| MC-7 | `VSCODE_PASSWORD` is generated client-side, sent once, and never read back; a container with a public subdomain always carries one. `newProject.test.ts` — "generates the container password, sends it, and forgets it", "never emits a container with a public subdomain and no password". |
| MC-8 | The first onboarding prompt is composed into the composer, sent once per newly connected identity-door environment, never on reconnect or for a manually paired one. `firstPrompt.test.ts` — "composes once for a freshly connected Zerops environment", "stays quiet on every reconnect to the same environment", "never writes into an environment somebody paired by hand". |
| MC-9 | "Enable Zerops Mate" WRITES `ZCP_MATE_ENABLED` and then restarts — a restart alone returns the container to the identical state, because `zcp init` registers no mate step without the flag (§2.0). The write is an upsert (delete-then-create, `sensitive` required, never the bulk env-file PUT), and a flag already reading as on is left untouched rather than rewritten. `api.test.ts` — "writes the Zerops Mate flag before restarting a container that lacks it", "replaces a Zerops Mate flag that is present but switched off", "writes nothing when the flag already reads as on, and still restarts". |
| MC-10 | The Zerops account session fails closed at the outer product mount: only `signed-in` mounts routed/background product surfaces; `loading`, `signed-out`, and `totp-required` mount only account login, while `/zerops_/authorized` stays bare. `-accountGate.test.ts`, `AppRoot.test.tsx`, `ZeropsHostedLanding.test.tsx`. |
| MC-11 | The agent selection reaches the container as `ZCP_AGENTS` (presentation policy, canonical order), and an EMPTY selection omits the key rather than emitting `""` — absent offers every agent, empty offers none. `ZCP_AGENT_AUTH_TYPE_*` is GUI parity with no in-container reader, and no agent token is ever written to the import document. `newProject.test.ts` — "never emits ZCP_AGENTS at all when the list is absent or empty", "emits ZCP_AGENTS as a comma-separated list in canonical order". |
| MC-12 | The account gate covers the Zerops entry AND everything under it, so `/zerops/new` — which creates a real project on the user's own account — is never reachable signed out, including on the branch a local server takes; the identity callback is matched first and keeps its own surface. `-accountGate.test.ts` — "keeps a Zerops entry's sub-route a bare login too, so the project wizard is not reachable signed out", "still hands the identity callback over rather than gating it as a Zerops sub-route". |

---

## 5. Zerops-aware client (S6: server feeds + web)

Once a member is inside a thread, the mate server itself becomes a **reader** of the same Zerops
project: two independent, read-only feeds — **topology** (what exists) and **lifecycle** (where
the agent is) — surface as a service map, a lifecycle strip, and cards under the tool calls that
carry them. Neither feed imports the other; neither ever mutates the platform.

### 5.1 The topology feed

`ZeropsTopology` sources one snapshot per server (one server ⇔ one Zerops project) from `zcp studio
topology` (a direct, short-lived read), kept current by a doorbell — `zcp studio watch`, a
long-lived child whose stdin is held open (never closed): closing it is what the child treats as
its own cancel signal. The doorbell stream is decoded **per line**, not through a whole-stream
NDJSON decoder — a decoder that fails the whole channel on one unreadable line would silence the
doorbell over a single stray write. While no `zerops_*` tool has completed recently the feed is
idle; any completion opens a **90s nudge window** polled every **3s**, then reverts — the feed
carries no live process state of its own, so this is how it catches a status settling after an
agent action. A snapshot publishes only when its content actually changed.
`ZeropsCli.test.ts` — "keeps the child's stdin open so the watcher is not cancelled at spawn",
"ignores a line that is not a doorbell event"; `ZeropsTopology.test.ts` — "does not republish an
unchanged topology".

**Availability is not one boolean.** The zcp binary missing (`ENOENT`) is `available:false` — a
permanent fact about the machine, the feed never retries. The binary present but failing (auth,
network, non-zero exit) is `available:true, degraded:true`, keeping the **last-good** `services`
rather than going blank, and the poll keeps retrying. `doorbellConnected` is a **tri-state**, not a
boolean: `true` (live push), `false` (doorbell down, feed still polling — "still correct, just a
few seconds behind," not degraded), and **absent** (no doorbell to report on at all — a plain
`false` would claim a doorbell exists and is merely down, a different, false claim).
`ZeropsTopology.test.ts` — "switches the feed off when zcp is not installed", "stays available but
degraded when zcp is present and failing", "reports the doorbell down until it connects, then up",
"says nothing about a doorbell that does not exist".

Grouping is ordered: `adoptionState === "zcp-self"` or the type (its `<os>/` prefix stripped) starts
with `zcp` ⇒ **infrastructure**; `isInfrastructure` (a managed data service) ⇒ **data**; otherwise
⇒ **runtimes** — the OS-prefix strip matters only for a runtime type (`ubuntu/nodejs@22`).
`zeropsTopologyParse.test.ts` — "puts the zcp container in infrastructure even though zcp says it
is not managed", "is not confused by the OS prefix on a runtime type".

### 5.2 The lifecycle feed

`ZeropsLifecycle` reduces over the provider's runtime event stream, gated on the **tool name**
(normalised `mcp__<server>__<tool>` → `<tool>`, accepting `zerops_*`), never on the provider's own
`itemType` — Claude's own classifier tests `…delete…` before `…mcp…`, so
`mcp__zerops__zerops_delete` arrives typed `file_change`; gating on `itemType` would silently drop
it. The envelope has two carriers on the reducer side too, matching §1.3, **stricter on the JSON
branch**: a result text that parses as a JSON object has its top-level `envelope` key as the
**only** answer — the fence rule is never tried on it, even when a string field inside that
document happens to quote a fenced block. A malformed body — either carrier — leaves the previous
envelope untouched rather than falling back to an earlier block. The latest envelope per thread is
**persisted** (migration `044`), so a returning client sees it after a container restart, not just
a reconnect. The `recentTools` ring holds the last 8 tool calls per thread, keyed by `itemId` so a
started-then-completed call updates one entry rather than appending two.
`zeropsActivityResult.test.ts` — "accepts zerops_delete, whose itemType Claude misclassifies";
`zeropsEnvelope.test.ts` — "does not read a fenced block quoted inside a JSON document", "still
prefers the envelope key when the document also quotes a block", "does NOT fall back to an earlier
block when the last one is malformed"; `ZeropsLifecycle.test.ts` — "reads a thread's state back
after a restart".

### 5.3 Result text reaches the client

The platform's activity projection normally **drops** an MCP tool's raw result (kept fields:
`type, id, tool, server, status, arguments, appContext, error, durationMs`) for an 84-character
teaser — Claude gets not even that. A `zerops_*` tool is the one exception: its **raw result text**
rides on all **three** projection routes (live WS, reconnect/history snapshot, thread-detail
snapshot) so a card can decode it wherever it appears. The gate sits ahead of the `itemType ===
"mcp_tool_call"` branch that would otherwise misfile `zerops_delete` (§5.2). Capped at **48,000
bytes**; over the cap the text is **dropped whole**, not sliced — a truncated JSON document parses
as nothing, and a card built on half a document would render a lie.
`ActivityPayloadProjection.test.ts` — "carries it on the live event path", "carries it on the
thread-detail snapshot a reopened thread renders from"; `zeropsActivityResult.test.ts` — "drops the
text whole when it exceeds the cap, and says so".

### 5.4 Web surfaces

The service map and lifecycle strip read the two feeds as atoms; the strip mounts **beside**
`ChatHeader`, not inside it, because it needs pending-question state that lives one level up. Every
card is a **total decoder** (`decode(resultText) → Payload | undefined`); on `undefined` — not
JSON, the wrong JSON document, `resultText` absent (over the cap, or a pre-S6 server) — the row
renders as the ordinary generic tool block, the same behaviour an unrecognised `zerops_*` tool gets.
Quick actions only **prefill** the composer; the component's whole module graph is asserted to
import no mutating RPC. The question card gained a visible **"Other"** free-text option and
arrow-key navigation beside the digit keys. A down doorbell renders one quiet line, deliberately
not the degraded-feed banner.
`ZeropsQuickActions.test.tsx` — "cannot reach Zerops or the RPC layer at all";
`ComposerPendingUserInputPanel.test.tsx` — "offers Other as a visible way to answer in the user's
own words".

The web presentation keeps project state in chrome rather than letting it scroll away. The
lifecycle phrase is one compact, full-width band directly below the thread header: it uses the
canonical phrase producer, always pairs a status dot with a word, and opens the service map. Agent
authorization is rendered exactly once, in the service-map tray — never as a heightless overlay on
the timeline. When authorization needs attention while the panel is closed, an in-flow attention
affordance remains visible and opens that tray; it disappears when attention clears. The tray keeps
the resolver-owned snapshot and the existing sign-in/cancel wiring unchanged.

The service map starts with liveness, then renders compact **Runtimes**, **Data**, and
**Infrastructure** groups in the topology view model's order. Every service row exposes its status
as dot + word; the Zerops Control Plane row uses the shared mint treatment. Degraded topology keeps
last-good rows visible and a down doorbell remains one quiet line. Recognized Zerops results use one
shared process-card anatomy — semantic kicker/status, operation title, steps/outcome, and separate
URL or information chips — for `plan`, `import`, `mount`, `deploy`, `verify`, `subdomain`, and
`error`. The total-decoder fallback above remains authoritative for undecodable, absent, oversized,
or future result kinds.

The web sidebar presents the hierarchy the client can prove: a logical project contains its
connected environment/workspace members, and each member contains its own threads. A Zerops
topology project name may replace the generic workspace basename, but only when the feed supplies
it. Environment rows keep their descriptor label as the fallback and never infer a Zerops tag group
or production role from a hostname. Search remains a flat result mode so keyboard navigation does
not acquire hidden tree state. True Zerops tag-group and production-lane placement requires those
identities in a future contract; the web must not manufacture them.

Within that truthful tree, one untouched thread per workspace — defined by both `latestTurn` and
`latestUserMessageAt` being absent, never by its title — is presented as the small workspace-level
new-thread shortcut rather than as an empty full-size card. The shortcut opens that exact existing
thread and remains available when the workspace is collapsed; additional untouched threads remain
visible so the presentation cannot silently discard data. Live thread cards use content-driven
height and at most two title lines, while settled and snoozed shelves remain compact single-line
rows. The workspace toggle and thread shortcut are separate controls, never nested buttons.

Narrow service-map rows preserve the full hostname, type, and mount path with bounded wrapping;
status and links do not disappear to make room. The terminal drawer's pointer resize seam is also a
focusable horizontal ARIA separator exposing its current/minimum/maximum height. Arrow Up/Down
resize it in small steps and Home/End select the bounds through the same clamp path used by pointer
dragging.

Every workspace row exposes one compact **New thread in …** action. When an untouched shell already
exists, the action opens that exact shell and the first such shell stays out of the card list; when
none exists, it creates a draft for that workspace's exact environment/project ref. It never creates
a second shell while one is available, and additional untouched shells remain visible rather than
being discarded.

When the active topology supplies a non-empty Zerops project name, that name is the presentation
identity across the thread header, draft hero, file-panel project label, and active composer
environment indicator. The local workspace/environment labels remain the exact fallback when the
feed is absent, unavailable, or unnamed; the client never infers identity from a hostname. A
connected Zerops thread uses a plain-language composer prompt while advanced `@`, `$`, and `/`
syntax remains functional and all higher-priority approval, question, plan, disconnected, and
unavailable phrases remain authoritative.

Agent authorization lays its identity/status and action cluster out responsively: actions wrap,
the browser step remains primary, a device code is visibly code with an adjacent copy action, and
cancel remains secondary. A provider mark in a thread row is identity, not status, so it is visually
quiet at rest and yields urgency to the canonical semantic status. The Zerops panel remains
full-width on narrow surfaces but centers its sections in a readable maximum-width column on a wide
surface.

On wide web layouts, the Zerops project panel opens once when topology first proves that the active
thread belongs to a Zerops environment and that thread has no prior panel choice. This default may
not replace an already active Files, Diff, Preview, Terminal, or Agents surface. Closing the panel
records a thread-scoped choice, including when its tab is removed, so rerender and reload do not
make it spring back. Narrow layouts never auto-present the panel as a sheet.

Pending questions and approvals share one visible **Waiting for you** anatomy: attention state,
human request kind, complete detail, progress when queued, and a legible action hierarchy. Provider
options and all keyboard/selection/response semantics remain authoritative. One-shot approval is
the primary action when advertised; broader session permission is secondary; refusal actions stay
visually quieter without being hidden.

### 5.5 Subscriptions are flow-controlled — a raw probe must `Ack`

effect-rpc applies per-request flow control to streamed responses: after each Chunk the server
closes a latch and waits for the client's `[{"_tag":"Ack","requestId":"<id>"}]` before sending the
next. A client that never acks receives exactly one Chunk and no Exit on a healthy socket —
indistinguishable from a feed that stopped publishing. `RpcClient` acks automatically, so every real
client sees pushes; a hand-rolled WebSocket probe of any `subscribe*` method must ack every Chunk.
Live-verified with an acking probe: `subscribeZeropsTopology` delivered an imported service 0.5 s
after `zcli` began and a deletion ~2 s after it landed. `ZeropsTopology.test.ts` — "publishes a
change made from inside an RPC handler's fiber to an open subscription", "a subscriber that arrives
after the first one left still receives changes".

### Invariants

| ID | Invariant |
|---|---|
| MF-1 | The zcp binary missing is `available:false` (permanent, no retry); present-but-failing is `available:true, degraded:true` with the last-good rows kept; `doorbellConnected` is a tri-state (`true`/`false`/absent), never collapsed to a boolean. `ZeropsTopology.test.ts` — "switches the feed off when zcp is not installed", "stays available but degraded when zcp is present and failing", "says nothing about a doorbell that does not exist". |
| MF-2 | Taxonomy order is zcp-self/type-prefix ⇒ infrastructure, then `isInfrastructure` ⇒ data, else runtimes; the OS prefix is stripped before the type check. `zeropsTopologyParse.test.ts` — "puts the zcp container in infrastructure even though zcp says it is not managed", "is not confused by the OS prefix on a runtime type". |
| MF-3 | The lifecycle reducer gates on the tool NAME, never `itemType`. `zeropsActivityResult.test.ts` — "accepts zerops_delete, whose itemType Claude misclassifies". |
| MF-4 | A JSON-document result's top-level `envelope` key is the unconditional carrier; the fence rule never runs on it, even when the document's own text quotes a fence. `zeropsEnvelope.test.ts` — "does not read a fenced block quoted inside a JSON document", "still prefers the envelope key when the document also quotes a block". |
| MF-5 | The latest envelope per thread survives a container restart. `ZeropsLifecycle.test.ts` — "reads a thread's state back after a restart". |
| MF-6 | A `zerops_*` result's raw text reaches the client on all three projection routes, capped at 48,000 bytes; over the cap the text is dropped whole, never sliced. `ActivityPayloadProjection.test.ts` — "carries it on the live event path", "carries it on the thread-detail snapshot a reopened thread renders from"; `zeropsActivityResult.test.ts` — "drops the text whole when it exceeds the cap, and says so". |
| MF-7 | A card that cannot decode its result text renders the generic tool block; quick actions never call a mutating RPC. `ZeropsQuickActions.test.tsx` — "cannot reach Zerops or the RPC layer at all". |
| MF-8 | Live subscription delivery of a topology change is unproven — a live WS test saw zero frames across an import and a delete despite a fresh `get` succeeding between them; the offline reducer behaviour is pinned, live push is not. `ZeropsTopology.test.ts` — "re-reads when the doorbell rings, and publishes the change". |

---

## 6. Git (S3)

On Zerops the thread's cwd (`/var/www`) is not itself a repository: each repository is a mounted
**dev service**, with its own `.git` living on that service's own disk, reachable only over SSH. A
turn that edits `kanbandev` and `apidev` produces two checkpoints, one in each service's own `.git`,
and a diff that lists files as `kanbandev/src/x.ts` / `apidev/main.go` — grouped by service. The
absolute invariant is that **no git process ever runs against the sshfs mount** (measured, see the
mate ledger: a full turn costs 12.7 s over the mount against 1.37 s over a multiplexed SSH connection,
and a first checkpoint on a repository without a `.gitignore` costs 245 s). The mechanism lives in
`apps/server/src/zerops/{ZeropsRepositorySource,ZeropsGitSpawner,ZeropsPolicy,
ZeropsCheckpointTargets}.ts`.

### 6.1 The repository set

`ZeropsRepositorySource` answers "which repositories exist" from `zcp studio topology` — a direct
platform read, never a scan of `/var/www` for `.git` — through the `ZeropsCli.readTopology` seam,
which owns the one shell-out and the one parser. A service qualifies when zcp reports it `mounted`
with a non-empty `mountPath` (zcp only emits that after `stat`ing `/var/www/<hostname>`, so it means
"really mounted right now") and is not a managed service.

Three outcomes, deliberately distinct — a caller must never read one as another:

| Outcome | Means | A caller does |
|---|---|---|
| `disabled` | not a Zerops environment | nothing — the topology command never runs |
| `unavailable` | Zerops, but the topology read failed (no creds, `zcp` missing, a timeout) | degrades, names the reason, warns once (cleared by the next successful read) |
| `available` | the answer, possibly `[]` | `[]` renders "no repositories yet" — a fact, not an error |

The set is cached for 30 s (`REPOSITORY_CACHE_TTL`) and unconditionally re-read by `refresh`, which
a turn start calls explicitly. The live source builds its own `ZeropsCli` off the **raw**
`ChildProcessSpawner` rather than the layer-provided `ProcessRunner`: the Zerops git spawner (§6.2)
decorates that same tag, and the source decides where a git command belongs — a source consuming the
decorated spawner would close a dependency cycle, and running `zcp` itself on the raw spawner keeps
the one command that discovers the repositories out of the path map entirely.

### 6.2 The SSH executor

Upstream has three git process paths, not one — `GitVcsDriverCore.executeRaw` (cwd form),
`GitVcsDriver.gitCommand` (`-C` form), and `RepositoryIdentityResolver` talking to `ProcessRunner`
directly — and all three bottom out in `ChildProcessSpawner`. `ZeropsGitSpawner` decorates that one
seam, so all three are covered with no edit to a single vcs file. Everything that is not `git`, and
every `git` call outside a mounted repository, is handed to the platform spawner byte-identically —
`claude`, `codex`, `gh`, shells, node-pty and the `zcp` call from §6.1 keep upstream behaviour.

For a git call inside a mount: the host resolves from `-C <path>` (which wins — `GitVcsDriver` spawns
from the server's own cwd and carries the repository in `-C`) or else from `options.cwd`. The
rewrite emits `ssh -l zerops <pinned options…> <host> env <K=V…> git -C /var/www <args…>`, every
remote token POSIX single-quoted (`shellQuote` — ssh has no argv past the host, so a commit message
with a space or a `$(...)` is an injection unless quoted here). *Pinned, not inherited*: zcp's own
managed `~/.ssh/config` block already sets the same options for `Host *`, but the spawner pins them
itself so it does not depend on a file another program owns — the `ControlPath` deliberately matches
zcp's template so mate reuses the master zcp already holds (8 ms vs 59 ms per round trip).

*The path map, one rule both directions*: everything T3 hands the spawner is mount-side, everything
it hands git is host-side, everything git hands back becomes mount-side again — `/var/www/<host>` ⇄
`/var/www`. Mapped inbound: `options.cwd`, the value after `-C`/`--git-dir`/`--work-tree`, and
path-valued env keys (`GIT_INDEX_FILE`, `GIT_DIR`, `GIT_WORK_TREE`, and the object-directory keys).
Mapped outbound: stdout of exactly the two argv shapes that return an absolute path —
`rev-parse --show-toplevel` and `worktree list --porcelain` — replaced at a path position only (a
commit message that happens to contain `/var/www` is data, not a location). `--git-common-dir` is
deliberately left alone: git answers it *relatively* (`.git`), which both consumers already resolve
against the mount-side cwd correctly.

*Environment* crossing the wire is an allowlist, not the whole `process.env` T3 spreads upstream:
`GIT_*` and `LC_ALL` only; `GIT_TRACE2*` is dropped (its trace file is written locally and watched
with `fs.watch`, which never fires for host-side changes anyway). *Concurrency* is a semaphore per
host, 4 in flight (comfortably under sshd's default `MaxSessions` — 16 concurrent sessions measured
ok, see the mate ledger), unbounded across hosts, so three repositories in one turn cost close to what
one does. *Transport failures*: ssh's own exit code 255 is reported as a distinct warning naming the
host and the remote command — never conflated with a git failure (exit 1).

### 6.3 Policy — enforced, not defaulted

Three of T3's own behaviours are wrong on Zerops, decided in one place (`ZeropsPolicy`) and enforced
at the single chokepoint each rides through, because a `.t3/project` file in a repository or a
hand-written RPC can set anything a mere default would allow:

| Rule | Why | Enforced at |
|---|---|---|
| No worktrees (`worktreesAllowed:false`) | the isolation unit on Zerops is a service, not a directory — a dev service has one `/var/www`, one process, one subdomain; a second checkout is a checkout nobody serves | the decider (`thread.create`, `thread.meta.update` persist `worktreePath:null`; `project.meta.update` forces `defaultThreadEnvMode:"local"`) — the sole write chokepoint every command funnels through |
| No second commit pipeline (`stackedVcsActionsAllowed:false`) | zcp owns init, identity, the PAT, commit and push; mate owns turn-level history only | `GitManager.runStackedAction` refuses server-side; the `pullRequests`/`vcsStackedActions` capabilities hide the client control too — hiding is presentation, the refusal is the enforcement |
| No background fetch (`upstreamRefreshAllowed:false`) | a status poll's `fetch` against a PAT-backed origin mate does not own is unwanted network from every mounted service at once | `GitManager.remoteStatus` forces `refreshUpstream:false` |
| Restore keeps untracked files (`restoreRemovesUntrackedFiles:false`) | on Zerops the tree is a *running application's disk* — uploads, sqlite files, logs the live app wrote after the checkpoint are not the agent's to delete | `GitVcsDriver.checkpoints.restoreCheckpoint` skips `git clean -fd` |

`zeropsPolicy` reads `ServerConfig` **optionally**: no config in context (most existing tests) reads
as `UPSTREAM_POLICY`, the same set every one of upstream's own behaviours already has — the policy
fails toward doing nothing rather than toward silently changing behaviour a test never asked for.

### 6.4 Checkpoints across repositories

`resolveCheckpointTargets(cwd, repositories)` decides which repositories one checkpoint covers:

- Zerops with the topology unreadable → the single **upstream** target at `cwd` — an unreadable
  topology must never silently shrink a turn's history.
- `cwd` containing mounted repositories → one target per repository, prefixed `<host>/` unless the
  repository *is* the cwd — this is what makes the merged diff read grouped by service with no
  contract change (the projection keeps its one `checkpoint_ref` column; the repository set is
  recovered at read time).
- `cwd` inside a single repository, or an ordinary repository elsewhere → that one target,
  unprefixed — the upstream single-target case.
- a Zerops project with zero mounted repositories → no targets — "no repositories yet".

`captureAcrossTargets` runs the capture concurrently across targets and merges every repository's
diff into the one flat, sorted `files[]` the turn contract already carries — the **same ref string**
(`refs/t3/checkpoints/<thread>/turn/<n>`) names the turn in every repository, since each has its own
ref store. Failure is per repository and never propagates: a repository refused by the guard, or one
whose capture or diff failed, is reported (`skipped` / `diffUnavailable` / `missingBaseline`) while
the others keep their history — capture is best-effort, and half a turn's history beats none.

**The untracked-file guard.** `git add -A` swallows every untracked file, and `git status` never
reveals it (it collapses untracked directories). Before every capture, `captureBaselineAcrossTargets`
and `captureAcrossTargets` probe `ls-files --others --exclude-standard -z` capped at 256 KB and read
the executor's own `stdoutTruncated` flag as the overflow signal — no new threshold, and the probe
itself is cheap. On trip: refuse **that repository's** checkpoint, name the collapsed offenders in
the turn's activity, and let the rest of the turn proceed. The probe is deliberately **not
memoized**: an earlier version cached "already probed", which read like a free optimisation and was
in fact the whole guard — the reactor drives the pre-turn baseline twice per turn
(`turn-start-requested`, then `message-sent`), so the second call skipped the probe and committed the
very tree the first had just refused. Live-verified failing this way on `z3-eval` before the fix
(19,308 untracked paths went into a checkpoint despite the probe running over SSH), and re-verified
correct after it — the fix is proved against the real executor (`ZeropsUntrackedProbe.test.ts`) and
against the full SSH stack, faked only at the network boundary (`ZeropsGuardOverSsh.test.ts`).

**Restore** fans out sequentially, not concurrently — a restore rewrites a running application's
disk, and a half-applied fan-out is easier to reason about in a known order — `git restore --source
--worktree --staged` + `git reset --quiet`, never `git clean -fd` (§6.3). Live-verified: restore
visibility through the mount for a rewritten tracked file lands in the same sub-10-second poll cycle
as the fan-out itself dispatching — the S0.4 20 s reverse lag applies to directory listings and
stale entries, not to a file a restore actually rewrites.

**Pruning.** Checkpoint refs are hidden and nothing else ever removed them, so they accumulate per
turn × repository × thread and outlive the thread — on Zerops, on another service's disk.
`pruneThreadRefsAcrossTargets` sweeps every repository's `refs/t3/checkpoints/<thread>/*` by prefix
(never by turn count — a deleted thread's turn count is already gone from the projection) on
`thread.deleted`, tolerant per repository: one that has been unmounted or deleted keeps its refs and
is logged, rather than stranding the ones still reachable. `resolvePruneTargets` sweeps the
**absolute** mounted repository set on Zerops regardless of the deleted thread's own cwd (a real gap
found live before this landed — refs survived a thread delete on both hosts); off Zerops, where the
repository set is not absolute, it falls back to the thread's own cwd.

### 6.5 What stays on the mount, and why

The sshfs mount is still where the agent, the editor and every non-git tool read and write files —
writes through it are write-through (a create/edit/delete made on zcp is visible to a host-side
`git status`/`git diff` immediately, measured, see the mate ledger) and a host-side change reaches the
mount's file **content** and **path lookups** just as fast. What lags is **directory listings and
stale entries** specifically — a fixed 20 s `dcache_timeout` sshfs default the product mount does not
override, because weakening it costs 3–27× on `git status`. No watcher works over the mount either:
`inotify` never fires for a host-side change (only for a write made *through* the mount itself), so
anything that needs to know about host-side change must poll over SSH rather than watch — the
topology feed's own doorbell (§5.1) already does this for service state; nothing in S3 tries to
watch the mount for git state.

### What S3 does not do

- **No workspace-index reimplementation.** A planned slice (S3.5) would have moved file search off
  the mount, on the premise that T3's native `FileFinder` (a closed-source Rust index) could not skip
  `node_modules` and would time out scanning every mount at once. Live-verified the opposite: rooted
  at `/var/www` with a 19,308-file `node_modules` present, the index answers in 2–4 s, results are
  mount-relative, and no `node_modules` path is ever returned — the premise was wrong, so S3.5 is
  dropped rather than built.
- **Identity, remotes and push stay zcp's.** mate never runs `git init`, never touches a remote, never
  commits or pushes outside a checkpoint ref — those are zcp's workflow, reached from mate only by the
  agent going through MCP, the same as any other zcp mutation.
- **Known cost, accepted rather than fixed**: two independent processes read the same topology on
  their own schedules — the repository source here (30 s TTL, refreshed at turn start) and the S6
  topology feed (§5.1, its own cache plus a doorbell). Unifying them was out of scope for this
  stream; each read is cheap and direct, so the duplication costs a little latency, not load.

### Invariants

| ID | Invariant |
|---|---|
| MG-1 | The repository set comes only from `zcp studio topology` (mounted, non-managed services), carries three distinct outcomes, and is never a `/var/www` scan for `.git`. `ZeropsRepositorySource.test.ts` — "keeps mounted runtimes and drops everything else", "a project with no mounted runtime is available and empty, never unavailable", "a failing topology read degrades to unavailable and names the reason". |
| MG-2 | No git process ever runs against the sshfs mount: every `git` spawn located under a mount is rewritten to `ssh … git -C /var/www …`; every non-git command and every `git` call outside a mount passes through byte-identical. `ZeropsGitSpawner.test.ts` — "hands a non-git command to the inner spawner untouched", "leaves git alone outside every mount", "resolves the host from the -C form and rewrites -C to the remote path", "no git argv ever carries a mount path". Live: verified.md S3 live audit — zero bare git processes, zero argv carrying a `/var/www/<host>` path. |
| MG-3 | Only `GIT_*` and `LC_ALL` cross the wire, `GIT_TRACE2*` is stripped, path-valued flags/env are mapped mount→host, and the two absolute-path-returning argv shapes are mapped host→mount (`--git-common-dir` left alone). `ZeropsGitSpawner.test.ts` — "forwards only GIT_* and LC_ALL, and never the server's own environment", "strips the trace2 event stream, whose file is local and whose watcher never fires", "maps the two argv shapes that return an absolute path", "leaves --git-common-dir alone, because git answers it relatively". |
| MG-4 | ssh's own exit 255 is reported as a distinct transport failure, never as a git verdict; concurrency is capped per host (4) and unbounded across hosts. `ZeropsGitSpawner.test.ts` — "names an ssh transport failure rather than letting it read as a git verdict", "caps concurrent sessions per host without capping across hosts". |
| MG-5 | Worktrees, the stacked commit→push→PR action, and background fetch are off on Zerops, enforced at the decider / `GitManager` — never left to a default a client or a `.t3/project` file could override. `ZeropsPolicy.test.ts` — "thread.create persists a null worktree path on Zerops", "project.meta.update forces the default thread env mode to local", "refuses the stacked commit/push/PR action server-side", "never lets a status read fetch from a remote". |
| MG-6 | Restoring a checkpoint on Zerops never runs `git clean -fd`; every other environment keeps it. `ZeropsPolicy.test.ts` — "leaves what the running application wrote on Zerops", "still cleans untracked files everywhere else". |
| MG-7 | A turn's checkpoint fans out per repository under the identical ref name and merges into one sorted, `<host>/`-prefixed diff; one repository's capture or diff failure never costs another's history. `ZeropsCheckpointTargets.test.ts` — "captures once per repository and merges the diffs into one grouped list", "names the turn with one ref in every repository, which is what keeps the projection flat", "keeps one repository's checkpoint when another's fails". |
| MG-8 | The untracked-file guard probes fresh before every capture (never memoized) and refuses only the overflowing repository. `ZeropsCheckpointTargets.test.ts` — "refuses only the repository whose untracked set overflows the probe", "keeps refusing while the repository still overflows, however often it is asked"; `ZeropsUntrackedProbe.test.ts` — "reports truncation once the untracked path list passes the cap"; `ZeropsGuardOverSsh.test.ts` — "refuses a repository whose untracked set overflows, exactly as it does locally". |
| MG-9 | A deleted thread's checkpoint refs are pruned from every repository it touched; the swept set is the absolute mounted repository set on Zerops, not the thread's own cwd. `ZeropsCheckpointTargets.test.ts` — "deletes every ref the thread left in every repository it covered", "tolerates a repository that is gone and still prunes the rest", "sweeps every mounted repository, without needing the deleted thread's cwd". |

## 7. The fork

Zerops Mate is a **hard fork** of T3 Code (MIT), frozen at `upstream/main` `f94a0d646` on 2026-08-28 (fork tag
`upstream-base-2026-08-28`). Upstream is never merged or rebased again; what is still taken from it is
taken in two ways, and everything else is owned. The rules, the measurements behind them and the
freeze checklist live in the fork — `../z3/docs/internals/zerops/fork.md` (rules), `spi.md` (the
adapter contract), `intake.md` (last-reviewed upstream SHA + decisions), `compat.md` (ported SHA ×
CLI versions) — and the fork's `CLAUDE.md` is the map. This section records the decision and the
invariants zcp relies on.

### 7.1 Zones

| Zone | What | How upstream reaches it |
|---|---|---|
| Imported | the standalone wire-protocol packages (`packages/effect-codex-app-server`, `packages/effect-acp`) | byte-identical re-import from an upstream SHA, pinned by `imported.lock` |
| Ported | the provider drivers (`apps/server/src/provider/**`, provider contracts) | upstream commits are ported behind the adapter SPI; our own edits there stay minimal |
| Owned core | the rest of the server, shared packages, desktop, mobile | optional cherry-picks chosen by triage |
| Owned product | `apps/server/src/zerops/**`, `apps/server/src/spi/**`, `apps/web/src/zerops/**`, the new UI | ours only |

Why not a merge: 44 % of upstream's provider commits also change orchestration, contracts or UI
(85 commits in the 60 days before the freeze), and the drivers import owned server modules — a
provider-directory checkout would be a bespoke merge every time; the UI is rewritten anyway.

Names follow the zones: the product identity is Zerops Mate (`mate` executable, `zerops-mate` release package, `/mate` base path, `zerops@mate` unit, `ZCP_MATE_*` envs), while upstream's names — `t3`, `t3code`, `T3CODE_*`, `@t3tools/*`, `/.well-known/t3/environment` — are inherited plumbing that runs through the ported and imported zones, is never user-visible, and is never renamed (fork rules `fork.md` §4.1).

### 7.2 The adapter SPI

The contract between ported drivers and owned code is the normalized `ProviderRuntimeEvent` stream
declared with a real version in `packages/contracts/src/providerRuntimeSpi.ts`, carried by one owned
lossless bus (`apps/server/src/spi/ProviderRuntimeEventBus.ts`), enriched with a typed tool-call view
so owned code never reads a driver's raw `payload.data`, and proven by recorded fixtures replayed
through the real drivers (goldens per driver). Owned code reaches driver internals only through
typed capabilities in `spi/`. Delivery guarantee, fixture format and the porting checklist: `spi.md`.

### Invariants

| ID | Invariant |
|---|---|
| MF-1 | The imported zone equals the tree recorded in `imported.lock` for the recorded upstream commit; CI fails on any drift. `scripts/imported-lock.test.ts`; `node scripts/imported-lock.ts --check`. |
| MF-2 | The ported zone imports nothing named `zerops`; `apps/server/src/zerops/**` imports no provider internals; `textGeneration/**` and `usage/**` reach providers only through `spi/**` and the sanctioned service tags. `scripts/mate-zone-architecture.test.ts`. |
| MF-3 | The Zerops lifecycle and topology feeds consume the SPI bus, not `ProviderService`; the bus is lossless while subscribed (unbounded fan-out, fresh subscription per subscriber, no replay before subscription). `apps/server/src/spi/ProviderRuntimeEventBus.test.ts`; `ZeropsLifecycle.test.ts` layer test. |
| MF-4 | Every driver has a golden: a recorded (Claude, Codex) or scripted (Cursor, Grok, OpenCode) stream replayed through the real adapter must normalize to the checked-in expected events; the Claude envelope golden carries both StateEnvelope wire carriers. `apps/server/src/spi/replay/goldens.test.ts`. |
| MF-5 | The fork's version line is its own (`0.1.x`), the model manifest is refreshed from the fork's `main`, and CI is the fork's `ci.yml` alone. `apps/server/package.json`; `ModelManifest.test.ts`; `.github/workflows/`. |
| MF-6 | The manifest carries the **complete Claude model catalog** (models, aliases, status, badge, capability profiles, per-model CLI version bounds), not just a current/legacy overlay. Since its URL is fork-controlled, a new Claude model on an existing profile is a JSON commit to the fork's `main` — **no mate release and no `PinnedVersion`/`PinnedSHA256` bump in zcp**. Codex still discovers its models from its app server. `ModelManifest.ts`; `ClaudeModelCatalog.test.ts`; the fork's `docs/internals/model-manifest.md`. |

## 8. Agent authorization (S7)

A Zerops user with a Claude or ChatGPT subscription signs the agent CLI in **from inside mate**; nothing
credential-shaped enters a thread, a feed or the ledger. Two halves: the **agent-auth feed** (what the
container knows about each agent's login) and the **login session** (how the user gets there).

### 8.1 The agent-auth feed

`subscribeZeropsAgentAuth` (stream, snapshot-typed) publishes, per agent (`claude-code`, `codex`):
`credPresent` (the credential artifact exists — `~/.claude/.credentials.json`, `~/.codex/auth.json`;
presence only, never contents), `flagOAuth` / `flagToken` (the platform flags `ZCP_AGENT_OAUTH_<S>`,
`ZCP_AGENT_TOKEN_<S>` read from the zembed env store), `state` (the welcome panel's five-value
matrix, `spec-welcome-mode.md §3`, verbatim), `providerAuth` (`authenticated | unauthenticated |
unknown`) and, while a login runs, `login` (§8.2). The server watches the credential FILE (parent
directory filtered by basename; a missing `~/.claude`/`~/.codex` is watched for via `$HOME`) and the
env store; events coalesce (~1 s, single-flight per agent).

**Presence is not authentication.** On a credential event the server verifies with the CLI itself —
`claude auth status` (JSON `loggedIn`) / `codex login status` — and only a fresh `authenticated`
result spawns `zcp agent mark-oauth <agent>` (argv, no shell; idempotent; the OAuth flag is written
non-sensitive because the GUI's flag read path redacts sensitive entries — `spec-welcome-mode.md §4.2`).
Upstream's provider probe is NOT the gate: it reports Claude as authenticated from `~/.claude.json`'s
account even after logout. The registry refresh stays only to warm the picker's cache (which may lag
≤5 min after a logout — upstream's `CAPABILITIES_PROBE_TTL`). The mark latch resets when the flag
disappears from the env store. Verification results and spawns are logged.

### 8.2 The login session

The client never types into a terminal. `zerops.agentLogin.start {agentId, threadId}` opens a
terminal named for the agent, writes the login command (`claude /login`; `codex login --device-auth`
— plain `codex login` opens a `localhost:1455` callback the user's browser cannot reach), attaches to
the PTY stream and runs the pure output parser ported from the Zerops GUI walker: chunk-boundary-safe
URL anchors, OSC 8, DEC graphics, paste/success/failure patterns, Y/N confirm, and a stall timer that
presses Enter through any unrecognized screen (Claude's login-method menu). The parsed prompt rides
the feed as `login.phase` (`starting | menu | awaiting-browser | awaiting-code | succeeded | failed |
cancelled`) with `url`, `code` (Codex's device code), `message`, `terminalId`. The card renders the
URL as an "Open sign-in link" action (+ copy link / copy code); the paste-code step stays in the
terminal — the code is never a form field the server sees. Success re-runs the verification of §8.1.
One session per agent; `zerops.agentLogin.cancel` sends Ctrl-C and closes the terminal.

### 8.3 Threat model
- The agent process and every project member share the container home: a credential file is
  project-wide. S7 does not change that; it is the platform's one-zcp-per-project model.
- The authorization code / device URL cross the terminal RPC exactly as in any terminal; they never
  enter a thread, a feed (the feed carries the URL the user must open, never the code they type
  back), or the ledger.
- A planted or stale credential file cannot flip the platform flag: the flag is written only after
  the CLI's own status says logged in.

### Invariants

| ID | Invariant |
|---|---|
| MA-1 | The feed's `state` equals the welcome panel's matrix for every combination of flag/credential; credential files are probed for presence only. `ZeropsAgentAuth.test.ts` (matrix table). |
| MA-2 | `mark-oauth` is spawned only after a fresh `authenticated` verification, once per credential appearance; `unauthenticated`/`unknown` never spawn; a burst of file events coalesces into one verification. `ZeropsAgentAuthIo.test.ts`, `ZeropsAgentAuthVerify.test.ts`. |
| MA-3 | The OAuth flag is written non-sensitive and a legacy sensitive row is migrated (`migrated:true`); token variables stay sensitive. zcp `TestMarkAgentOAuth_*`, `TestRunAgentMarkOAuth_SensitiveRow_MigratedTrue`. |
| MA-4 | Owned product reaches the provider registry only through `spi/providerInstances.ts`. `scripts/mate-zone-architecture.test.ts` rule 2. |
| MA-5 | The login walker turns the CLI's output into `login` phases with `url`/`code` from the recorded lines (Codex device URL + code; Claude menu → oauth URL), and cancel ends the session. `zeropsAgentLoginWalker.test.ts`, `zeropsAgentLoginOutputParser.test.ts`, `ZeropsAgentLogin.test.ts`. |
| MA-6 | Live: moving the credential aside flips the feed within ~0.5 s and `providerAuth` to `unauthenticated`; restoring it returns `authorized`/`authenticated`; the public `/mate/` renders the hosted-static landing. `verified.md` S7-3 + follow-up rows. |

Open (kept in the S7 plan until they land): the mobile card + parsed prompts on the phone (S7-4) and
the `setup-token` path (S7-5, a second zcp verb).

## 9. Clients on Zerops only (S5)

### 9.1 Desktop — a hosted client in an Electron shell
The desktop app ships the web bundle built in hosted-static mode (`VITE_HOSTED_APP_CHANNEL` ∈
{`latest`, `nightly`} — the only values the client accepts; `VITE_HTTP_URL`/`VITE_WS_URL` empty) and
serves it from `resources/web` through the `t3code://` protocol handler (SPA fallback, traversal
guard); the window opens unconditionally. The local backend, WSL, SSH launch, Clerk, the server
sidecar, path-returning dialogs and the network-exposure/QR endpoint picker are gone; keychain,
dialogs, updater (GitHub target `krls2020/z3` by default), preview webview and window/menu/theme stay.
The same rule governs the container-served client: `zcp`'s push loop builds it hosted-static, or
`/mate/` falls to `/pair`.

### 9.2 The activity relay on Zerops
`infra/relay` is a Node service over direct Postgres (Drizzle; migrations applied by `scripts/migrate.ts`)
that keeps only the activity/push API: `mobile` (device + Live Activity registration), `link`
(challenge + link), `token` (DPoP exchange), `server` (activity publish), health/metadata. Auth is the
Zerops identity: a bearer is verified at `/user/info`, the principal is the Zerops user id, and an
environment link proof must carry `zeropsProjectId` + `endpointOrigin` — the relay verifies the caller's
membership of that project and that the origin belongs to one of its subdomain-enabled services. The
mate server stamps both fields (project id from `T3CODE_ZEROPS_PROJECT_ID`; origin from
`T3CODE_ZEROPS_PUBLIC_ORIGIN` or the linking request, `https://` and never loopback) and refuses
outside Zerops mode. APNs delivery is a durable job table (unique `job_id`, `SKIP LOCKED` lease,
backoff, dead-letter at 5 attempts, lease recovery, 24 h expiry). Deployment files
(`infra/relay/zerops.yml`, `zerops-import.yml`) validate against the platform schema; the `z3-relay`
project is created only on the owner's go.

### 9.3 What left with T3 Connect
The relay-discovered environment list, managed endpoints/tunnels, the connect/status client groups,
the web "cloud connect" vertical, the mobile cloud-environment list and Connect onboarding sheet, and
Tailscale. Reach is the identity door (§3–§4) everywhere; mobile keeps the pairing-code screen as the
fallback until its Zerops session lands (S5-3).

### Invariants

| ID | Invariant |
|---|---|
| MC-1 | The desktop spawns no backend; the bundle is served from disk with the hosted-static gate short-circuiting the primary-environment resolution. `apps/desktop` tests (417), the Electron CDP proof in `verified.md` S5-1. |
| MC-2 | The relay verifies a Zerops token and binds a link to a project + origin; a proof without them, a non-member, or a foreign origin is refused. `infra/relay` `ZeropsAuth`/`ZeropsProjectBinding`/`EnvironmentLinker` tests (152). |
| MC-3 | The mate server's link proof carries `zeropsProjectId` + `endpointOrigin` in Zerops mode and refuses outside it; origin precedence env → request → refuse. `apps/server/src/cloud/http.test.ts`. |
| MC-4 | The APNs queue dedupes on `job_id`, leases exclusively, retries with backoff, dead-letters at five, recovers expired leases. `ApnsDeliveryJobStore.test.ts`, `ApnsDeliveryWorker.test.ts`. |
| MC-5 | No client calls a deleted relay group; `client-runtime`, web and mobile typecheck against `RelayLinkGroup` only. package typechecks; `linkEnvironment.test.ts` (mobile, web). |

Open (in the S5 plan): shared client logic into `client-runtime` (S5-2), the mobile Zerops session +
picker (S5-3), the relay deployment + client link trigger (S5-4b UI), server-side T3 Connect reach
deletion (S5-5).
