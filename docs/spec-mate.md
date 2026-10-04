# Spec: Zerops Mate (`mate`)

Zerops Mate — `mate` in code, paths and unit names — is a fork of the open-source T3 Code that rides inside the `zcp` container. Its server runs
next to nginx and code-server and spawns the coding agent (Claude Code, Codex) with ZCP's MCP
tools attached; its client — web, desktop, mobile — is the product surface a Zerops user signs
into. Because the agent operates the project through the same `zerops_*` tools an agent in a
terminal uses, mate's UI is not a second control plane: it is a **reader** of what those tools
report. That reading contract is what this spec owns.

- Boundaries — the three rules every later section obeys, who owns which fact, and the closed
  list of mate↔zcp touchpoints — §0.
- Delivery — how the mate bundle gets into the container, the supervised process, nginx, the
  `/mate/` base path, readiness — §2.
- The door — the Zerops-identity bootstrap a project member uses to reach a hosted mate server,
  with no pairing code and no shared container secret — §3.
- Client flow — how a browser reaches a hosted mate: session/candidates, registration, the
  birth, the readiness probe, identity connect, new project, first prompt — §4.
- Zerops-aware client — the service map as a client projection of the Zerops API, maintained by
  native platform streams under §5.1, the server's lifecycle feed from the envelope, and the web
  surfaces that read them: service map, lifecycle strip, result cards, quick actions — §5.
- Git — each mounted dev service is its own repository, reached over a multiplexed SSH
  connection rather than the sshfs mount; multi-repo checkpoints, diff, restore, pruning — §6.
- The fork — Zerops Mate is a hard fork of T3 Code: frozen upstream base, four zones (import / port /
  owned / owned product), the adapter SPI between ported drivers and owned code, the upstream
  intake ritual — §7 (rules and measurements live in the fork: `../z3/docs/internals/zerops/`).
- Agent authorization — the agent CLI signs in from inside mate: a self-verified agent-auth feed,
  the platform flag written by the mate server through the Zerops API, server-driven login sessions
  whose parsed prompts (URL, device code) reach the client as actions, and sign-out — §8.
- Clients on Zerops only — desktop as a pure hosted client, the activity relay re-shelled for
  Zerops with project-bound environment links, T3 Connect reach gone — §9.
- Related: `docs/spec-workflows.md` (the envelope/plan/atom pipeline that produces the
  state), `docs/spec-work-session.md` (per-PID session, compaction survival).

---

## 0. Boundaries

Three rules bound every later section. A section that describes another mechanism is superseded
by the ownership table below.

1. **Identity is the client's.** The user's Zerops token exists only in the client. The client
   reads and writes the platform with it: organizations, projects, services, processes, subdomains,
   user-data, and the platform's push channel. The mate server sees the token once, at the door
   (§3.2), for two reads, and discards it. The container's project token belongs to zcp: mate never
   reads it, never forwards it, and never calls the platform with it.
2. **Mate's clients reach the container only through the mate server.** Threads, the agent, git,
   the browser viewport. code-server and SSH are the platform's own doors and stay outside mate.
3. **zcp does not distinguish its caller.** Two entry points, stdio MCP and CLI. No layer exists for
   mate; mate adds no dependency to zcp. zcp knows mate as a unit it installs and supervises (§2),
   as a reader of the envelope it already emits (§1), and as two CLI subcommands mate spawns. `zcp
   studio watch` is the Zerops Studio extension's transport; mate does not consume it.

Every fact has one owner and one path to the client:

| Fact | Owner | Path to the client |
|---|---|---|
| identity, membership | Zerops API, user token | client; once through the door (§3) |
| what exists in the project, its status, its subdomains | Zerops API, user token | client projection (§5.1) |
| live platform observations | native platform websocket plus direct bootstrap/repair, user token | shared client read model (§5.1) |
| what the platform is doing now | Zerops API, user token | client overlay (§5.4) |
| where the agent is in the workflow | the envelope in a tool result | mate server reducer → lifecycle feed (§1, §5.2) |
| which services are mounted | the container's mount table | mate server (§6.1) |
| agent sign-in | the agent CLI's own status + the platform flag | mate server feed (§8) |
| thread state | mate server, `~/.t3` | mate server |
| the agent's browser | zcp, in the container | the agent via `zerops_browser`; the user via the viewport stream relayed by the mate server (§5, Browser surface) |

Touchpoints between mate and zcp, closed list:

- the envelope carried in tool results (§1);
- `zcp` argv, a closed list: `zcp mate status` / `zcp mate update` (§2.9 MU-2) and `zcp studio
  console serve`, the loopback data console the mate server hosts and brokers (§5.7). The mate server
  runs no other `zcp` argv; the agent sign-in flag it writes through the Zerops API (§8.1);
- `zcp init` installing and supervising the mate unit, and the contract that goes with it (§2.8);
- the agent-browser daemon's published stream port, `~/.agent-browser/default.stream`, on
  localhost — read by the mate server, never written by it, unknown to zcp as a mate concern;
- two conventions mate reads: the `ZCP_AGENT_OAUTH_*` / `ZCP_AGENT_TOKEN_*` flags in the platform
  env store (§8.1; those keys only, never another value in that file) and the `/var/www/<host>`
  sshfs layout (§6.1).

Anything else is a violation and needs this section changed first.

### Invariants

| ID | Invariant |
|---|---|
| MA-6 | `apps/server/src/zerops/**` spawns `zcp` with no argv outside the closed list `mate status`, `mate update` (§2.9 MU-2) and `studio console serve`. `scripts/mate-boundaries.test.ts`. |
| MA-7 | The env-store reader keeps only the `ZCP_AGENT_OAUTH_*` and `ZCP_AGENT_TOKEN_*` keys; a store carrying `ZCP_API_KEY` and `VSCODE_PASSWORD` yields neither. `ZeropsAgentAuth.test.ts` — "keeps only the agent flag keys". |

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
| `zerops_standup` | JSON | `envelope` key |

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
SHA256, Contract}` — read from the release manifest (§2.1c), never compiled in.

One pass, in order:

1. **Compare.** Installed == desired ⇒ done, **no network request made at all**. This is what keeps
   a warm restart off the network, and it is what the old "the binary exists" rule was reaching for
   without being able to say so.
2. **Keep a dev build.** An installed *semver prerelease* (`0.1.0-dev.<sha>`, what
   `eval/scripts/mate-dev-push.sh` tags) is never replaced by the pinned release unless `Force`. The
   protection is now a stated rule rather than an accident of which files happen to exist.
3. **Stage** into a fresh `versions/<desired>`: one `GET` to the manifest's `url`, streamed to a
   temporary file while its SHA-256 is computed and compared with the manifest's `sha256`. A
   mismatch reports both digests and **never invokes npm** — this is an integrity check against a
   damaged download, not a trust boundary (§2.1c). Only a matching tarball reaches
   `npm install --prefix versions/<desired> …`. npm still resolves the package's
   dependencies from its registry, so this is not an offline install. Download and npm share one
   3-minute deadline.
4. **Smoke** the staged install: `mate --version`, then the native-addon probe run **from the
   installed package's directory** (`node_modules/zerops-mate`), where the server resolves its own
   runtime imports — a bundled dependency (node-pty since 0.8.1) lives in the package's own
   `node_modules` and is invisible from the prefix root.
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

**The unit installs before it starts.** `zcp service start mate` runs the same pass (steps 1–6,
the manifest read afresh) before it launches the server, and it, `zcp init` and `zcp mate update`
each hold one install lock — an flock on `~/.zcp/mate/install.lock` — across the pass and, for the
two that restart, the restart. Whichever runs first installs the release; the other finds nothing
to do. So an update starts the server once, on the new release: the unit at boot installs it and
`zcp init` restarts nothing, or `zcp init` installs it while the unit's start waits on the lock,
and its restart stops only that waiting start (before the lock, an update started the server on
the old release and restarted it onto the new one — two starts, ~10 s down each). Nothing keeps
the server down: a lock is waited for at most four minutes, longer than an install's own bounds,
and then the start goes on with what is installed; an install that fails starts what is
installed; the kernel drops the lock with its holder, so a file left on disk never blocks. The one
case left with two starts is a boot that also updates zcp itself while the release requires the
new zcp: the unit's start, still on the old zcp, keeps the installed release.

**`zcp mate update [--force] [--json]`** runs the identical pass from the CLI and then restarts
`zerops@mate` when the unit is registered, so an update needs no container restart. It is what the
web client's "Update" verb ends in (§2.9). **`zcp mate status --json`** answers
`{installed, latest, contract, updateAvailable}` from the same resolver, using the manifest cache
(§2.1c) — the one reader of "is there a newer mate" in the whole product.

### 2.1b Disabling — the reverse direction

`MateEnabled` false with a unit file present: stop the unit (best-effort — it may already be stopped),
`sudo -E zsc unit remove mate` (a real failure here is the one error this branch returns), and delete
`~/.zcp/mate.env`. **`mate.Prefix()` is deliberately left alone**: the downloaded bundle stays on disk,
so re-enabling costs a `zcp init` and no network. With no unit file there is nothing to do and the
step does not even register.

### 2.1c The release manifest — no pin

zcp does not pin a mate version. It tracks the fork's **stable release** the same way the container
tracks zcp itself (`install.sh` → `releases/latest`), and refuses only what it cannot drive.

**The manifest.** The fork's release workflow publishes, next to the tarball, a release asset
`stable.json`:

```json
{ "version": "0.8.1", "asset": "zerops-mate-0.8.1.tgz",
  "url": "https://github.com/zeropsio/mate/releases/download/v0.8.1/zerops-mate-0.8.1.tgz",
  "sha256": "<64 hex>", "size": 21690443, "contract": 1, "publishedAt": "2026-09-09T07:23:00Z" }
```

`DesiredRelease()` reads `https://github.com/zeropsio/mate/releases/latest/download/stable.json`
(GitHub's own "latest release" redirect; a release marked *pre-release* is never "latest", which is
the rollback lever: mark the bad release as pre-release, re-mark the previous one as latest). The
answer is cached on disk with a 1-hour TTL (`~/.zcp/mate/manifest.json`, the same shape as zcp's
own `internal/update` cache) so a warm restart and `zcp mate status` reach the network at most
hourly; `zcp mate update` always refreshes. An unreachable manifest with a mate already installed is
**not** a failure: the installed version keeps serving and the step logs the miss. With nothing
installed it degrades like any other install failure (MD-1).

**What zcp checks, and what it does not.**

| Check | Rule |
|---|---|
| `contract` | must equal `mate.SupportedContract` (today `1` = C-1…C-7 in §2.8). A manifest declaring a contract zcp does not know is refused with a message naming both numbers — the one case where an old zcp deliberately stays on the mate it has. |
| `version` | must be ≥ `mate.MinimumMateVersion`, the oldest release zcp still drives (moves only when a contract fact changes). A manifest below it is refused. |
| `sha256` | the downloaded tarball must match it — damage detection, nothing more. |

**There is no trust chain, by decision (2026-09-09).** The container installs zcp from a GitHub
release over TLS with no signature; a manifest digest compiled into zcp guarded mate alone against
an attacker who could replace assets in `zeropsio/mate` but not in `zeropsio/zcp`, which is not a
boundary anyone defends. The boundary is the GitHub organisation: 2FA, protected `v*` tags, who may
push them. If the product ever needs build provenance it is done for both repositories at once
(Sigstore keyless with GitHub OIDC), never with a key held in a repository secret — whoever can edit
the workflow can sign with that key.

**What moved out of zcp.** `PinnedVersion`, `PinnedSHA256`, `ReleaseAssetName`, `ReleaseURL`,
`scripts/mate-pin.sh` and the `serve --help` golden are gone. The flag contract (C-2) is now tested
where it changes: the fork's CI asserts that `mate serve --help` advertises every flag of contract 1
(the list in §2.8), and a flag added on either side goes through a contract bump, not a pin move.

**Release order** is no longer a rule. A mate release is complete when its tag's workflow has
published the tarball and `stable.json`; every container picks it up at its next `zcp init` or
`zcp mate update`. A zcp release never waits for mate.

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
| `T3CODE_ZEROPS_ALLOWED_ORIGINS` | service env `ZCP_MATE_ALLOWED_ORIGINS` | only when set and well-formed (a comma-separated list of `https://<host>[:port]`, `mate.ValidateAllowedOrigins`) — unwritten ⇒ server's own default; a malformed value is named in one diagnostic line and left unwritten, init continues |

Only non-secret identifiers are written; a token never enters `~/.zcp/mate.env` (mode 0600),
rewritten every boot so a unit's frozen ExecStart never has to change. A missing/unreadable env
file is reported and mate starts anyway — diagnosable, unlike a unit that refuses to launch.

Every launch adds three more on top (`mate.LaunchEnvLines`), whatever init wrote:

| Key | Value |
|---|---|
| `T3CODE_BASE_PATH` | `mate.BasePath` (§2.7) |
| `ZCP_STATUS_FILE` | the status file a new Mate's setup is read from (`mate.DefaultStatusFilePath`) |
| `T3CODE_ZEROPS_HQ_ENROLLMENT` | the file zcp keeps the Mate's HQ enrollment in (`hq.EnrollmentPath`, `~/.zcp/hq/enrollment.json`, mode 0600): the HQ address and the credential the server opens its link to HQ with. It names a path, never the credential. zcp enrolls by itself in a Mate container and keeps enrolling, retrying whatever is not possible yet (`hq.Keep`); until the file exists the server's link stays quiet and its HQ state is unknown |

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
of facts below — none of which either side can change alone. The facts are numbered as **contract
1**: a release declares the contract it satisfies in `stable.json` (§2.1c), zcp carries
`SupportedContract`, and a release declaring a number zcp does not know is refused rather than
installed. Changing any fact below is a contract bump on both sides; adding a flag that an older
mate would reject is either a bump or a capability probe (`--base-path` is the precedent).

| # | The fact | Owned by |
|---|---|---|
| C-1 | The artifact is `zerops-mate-<version>.tgz`, a GitHub release asset on `zeropsio/mate`, whose npm `bin` entry is `mate` at `node_modules/.bin/mate` (`mate.BinName`), and whose release publishes `stable.json` beside it (§2.1c) | fork's `cli.ts pack` + release workflow |
| C-2 | `serve` accepts `--mode web --host --port --base-dir --no-browser --auto-bootstrap-project-from-cwd` with the working directory as a trailing **positional**. **An unknown flag is fatal**, so every flag added later reaches production only behind a capability probe — `--base-path` is the precedent and stays one (§2.2) | fork's `cli/config.ts` |
| C-3 | `T3CODE_ZEROPS_{PROJECT_ID,API_HOST,ALLOWED_ORIGINS}` keep their meaning, and a non-empty `PROJECT_ID` remains the sole Zerops-environment signal (§2.3, §3.1). `T3CODE_ZEROPS_HQ_ENROLLMENT` names the HQ enrollment file and nothing else; it is additive — an older mate ignores it, and a launch without it leaves the server's link to HQ quiet | fork's `ZeropsEnvironment` |
| C-4 | Liveness is `GET {basePath}/.well-known/t3/environment` → `200 application/json` carrying `basePath` (§2.5) | fork's environment descriptor |
| C-5 | The server binds loopback only and never claims a declared platform port (§2.4) | zcp's `ServeArgv`, fork's `--host` |
| C-6 | **`/mate` is baked into the released artifact, not chosen by zcp.** The release workflow builds the bundled web client with `VITE_BASE_PATH=/mate`, and `pack` refuses a tarball without `dist/client/index.html`. `mate.BasePath` must equal it; moving the prefix is a coordinated two-repo change | fork's release workflow + `mate.BasePath` |
| C-7 | `~/.zcp/hq/outcome.json`, beside the HQ enrollment, is zcp's last word on enrolling: `{state: enrolled\|no_hq\|refused, code?, at}`, written by its keep loop in a single rename after every attempt that says something about this Mate (HQ or Zerops not answering leaves it as it was). The server reads it from the directory `T3CODE_ZEROPS_HQ_ENROLLMENT` names, only to say why a new Mate's stand-up waits (`/setup.json`: `no_hq`, `not_enrolled` with HQ's `code`); a missing, unreadable or unknown document is plain `not_enrolled`. Additive: an older zcp writes none | zcp's `hq.SaveOutcome`, fork's `ZeropsHqLink` |

The fork's release workflow triggers only on stable `v<major>.<minor>.<patch>` tags, so GitHub's
"latest release" *is* the stable channel — nightlies never produce one.

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
| MD-10 | The desired release comes from `stable.json` and nowhere else: a manifest whose `contract` is not `SupportedContract` or whose `version` is below `MinimumMateVersion` is refused before any download; the tarball must match the manifest's `sha256` before npm runs; there is no registry-package fallback. An unreachable manifest keeps an installed mate serving and degrades only a first install. Download, integrity, and npm failures register the same degraded init outcome and no unit. `TestDesiredRelease_*`, `TestInstallRelease_ChecksumMismatch_RefusesInstall`, `TestInstallRelease_DownloadFailure_RefusesInstall`, `TestRun_Mate_InstallFailures_Degrade`, `TestRun_Mate_ManifestUnreachable_KeepsInstalled`. |
| MD-11 | **With `ZCP_MATE_ENABLED` unset, a container behaves exactly as one predating mate**: the rendered nginx.conf carries no `/mate`, no `3773`, no `healthz` and no marker path while keeping every non-mate structure; nothing is downloaded or installed; no unit is registered; no readiness marker is written; and `zcp init` prints no extra step line. `TestRunNginx_MateDisabled_RendersNoMateSurface`, `TestRun_MateDisabled_NoUnitFile_NoOp`, `TestDetect_MateDisabled_ByDefault`. |
| MD-12 | Disabling is a real reverse direction, not an absence of the forward one: a leftover unit is stopped and removed and `~/.zcp/mate.env` deleted, while `mate.Prefix()` is left on disk so re-enabling costs no network. `zcp service start mate` refuses under the off flag, so a unit surviving a failed removal cannot resurrect the server — reading the flag from the **live env store**, never from its own environment, because a systemd unit inherits neither (live-verified: a guard reading `os.Environ` crash-looped the unit on an enabled container). An unreadable store fails OPEN, since the unit exists only because an enabling `zcp init` created it. `TestRun_MateDisabled_UnitFilePresent_StopsAndRemoves`, `TestStart_Mate_GuardReadsLiveEnvStore_NotOnlyProcessEnv`, `TestStart_Mate_GuardRefusesWhenStoreSaysDisabled`, `TestStart_Mate_GuardFailsOpenOnUnreadableStore`, `TestStart_OtherServices_UnaffectedByMateGuard`. |
| MD-13 | An update is staged into its own version directory, smoke-tested, and only then activated by an atomic symlink rename; **any failure leaves `current` naming the version that was working**. Equal versions reach no network at all, and an installed semver prerelease (a hand-pushed dev build) is never replaced without `Force`. `TestEnsureInstalled_SameVersion_NoNetwork_ResultNone`, `TestEnsureInstalled_DifferentVersion_InstallsAndRepointsCurrent`, `TestEnsureInstalled_NpmFailure_LeavesCurrentUnchanged`, `TestEnsureInstalled_SmokeFailure_LeavesCurrentUnchanged`, `TestEnsureInstalled_DevVersionInstalled_KeptWithoutForce`, `TestEnsureInstalled_DevVersionInstalled_ReplacedWithForce`, `TestEnsureInstalled_Pruning_KeepsTwoAndTheLiveVersion`. |
| MD-15 | The unit starts at boot on its own (`WantedBy=multi-user.target`), independently of `zcp init` — measured on `z3-eval`: `active` at 16:45:12, the zcp binary replaced by `install.sh` at 16:45:14, `zcp init` later still. So it serves whatever was on disk at boot, and `zcp init` restarts an ALREADY-EXISTING unit whenever it replaced the bundle or rewrote the env contract; a unit it created in the same run is left alone, since `zsc unit create` starts it. The unit's own start installs the release first, under the install lock `zcp init` and `zcp mate update` hold across their install and restart, so an update starts the server once. `TestRun_Mate_UpdatedBundle_RestartsExistingUnit`, `TestRun_Mate_ChangedEnvContract_RestartsExistingUnit`, `TestRun_Mate_UnchangedBundle_DoesNotRestart`, `TestRun_Mate_FirstBoot_DoesNotRestartFreshUnit`, `TestRun_Mate_InstallsUnderTheSharedLock`, `TestStart_Mate_InstallsBeforeItStarts`, `TestRunMateUpdate_InstallsAndRestartsWhenUnitPresent`, `TestLockInstall`. |
| MD-16 | Every flag in `ServeArgv` belongs to contract 1's flag list (C-2), asserted on the zcp side against a literal list and on the fork side by CI against `mate serve --help`; the env contract's key set is exactly `T3CODE_ZEROPS_{PROJECT_ID,API_HOST,ALLOWED_ORIGINS}`; a malformed `ZCP_MATE_ALLOWED_ORIGINS` is never written and never fails init. `TestServeArgv_FlagsAreContract1`, `TestEnvContract_KeysAreTheSpecList`, `TestValidateAllowedOrigins`, `TestRun_Mate_InvalidAllowedOrigins_NotWritten_InitContinues`; fork: `serve-contract.test.ts`. |
| MD-17 | `zcp mate status --json` and `zcp mate update` share `DesiredRelease()` with `zcp init`; `status` never installs, `update` restarts the unit only after a successful activation, and both answer JSON a caller can act on without parsing prose; `status --refresh` bypasses the manifest cache (§2.9 step 5). `TestMateStatus_*`, `TestMateUpdate_*`, `TestRunMateStatus_Refresh_*`. |

---

### 2.9 Updates in the product — one reader, one verb

The web client knows **nothing about versions** except the floor it needs to sign in
(`MINIMUM_MATE_SERVER_VERSION`, the identity-exchange "Update required" door, unchanged). Everything
else about "is there a newer Mate" is answered by zcp and merely displayed:

1. **zcp reads.** `zcp mate status --json` (§2.1a) is the only place that compares an installed
   mate with the stable manifest.
2. **The server relays.** The mate server runs it through `ZeropsCli` (a narrow spawner with its own
   timeout and error types) once at start, then hourly, and on demand after
   an update; the answer becomes the descriptor field `update: {installed, latest, available}` on
   `/.well-known/t3/environment` and rides the existing `serverConfig` the client already holds. A
   missing `zcp` (a standalone server) leaves the field absent, and the client shows nothing.
3. **The client shows, quietly.** `available: true` renders as one muted line on the Mate card in
   the projects overview ("0.8.0 · 0.8.1 available") and the same line in the thread header's
   environment menu — a status, never a banner, never dismissable, never stored. The upstream
   "Server versions differ" banner, `versionSkew.ts` and its localStorage dismissals are deleted:
   they encode "client and server ship in one box", which this product does not.
4. **One verb: Update.** Next to that line. It calls `zerops.mate.update` — a Zerops-zone RPC
   gated by `exec:operate`, shaped like upstream's server self-update RPC — whose handler runs
   `zcp mate update --json` through `ZeropsCli`, returns its JSON, and then the client waits for the
   socket to come back exactly as "Restart to install" does today (`restartAndVerifyMate`), with
   the descriptor's `serverVersion` as the proof. Running threads stop; the client says so before
   the click. There is no container restart in this path.
5. **One check: Check for updates.** The status behind the line is cache-served twice over —
   zcp keeps the manifest an hour, the server re-reads status hourly — so a release just published
   is invisible for up to two hours. The Mate menu therefore always offers "Check for updates"
   wherever `capabilities.mateUpdate` is true; it calls `zerops.mate.checkUpdate` (read scope,
   never operate), whose handler runs `zcp mate status --json --refresh` — the same reader with
   the manifest cache bypassed — stores the answer as the descriptor's `update` and returns it, so
   the line and the verb repaint at once. The client still compares nothing (MU-1).
6. **Capabilities gate every Zerops RPC.** A surface that needs a server-side feature reads it
   from the descriptor's `capabilities` and never sends the RPC when the flag is absent — the
   version-skew rule the thread commands already follow. The data console is `capabilities.dataConsole`;
   without it the Data surface shows one line ("This Mate doesn't include the data console yet")
   with the update line and verb from step 3–4 beside it, instead of a defect from an older server.

### Invariants

| ID | Invariant |
|---|---|
| MU-1 | The client compares versions in exactly one place, the sign-in floor; the update line and verb render only from the descriptor's `update` field. `ZeropsMateCard.test.tsx`, `versionSkew.test.ts` is gone. |
| MU-2 | `zerops.mate.update` is offered only inside a Zerops project with `zcp` on PATH and requires `exec:operate`; a failing `zcp mate update` is a successful RPC carrying its JSON, never a transport error. `ZeropsMateUpdate.test.ts`. |
| MU-3 | The descriptor's `update` field is absent, never fabricated, when `zcp` cannot be run; the card then shows the installed version alone. `ServerEnvironment.test.ts`. |

## Current account contract — Mate 0.11.0

Mate 0.11.0 supersedes the historical S1/S4 pairing and account behavior below. Every product client
requires verified Zerops identity; manual pairing, startup credentials and cookie sessions are
removed, including standalone entry. Effective project roles (including overrides that lower
organization permissions) determine operation access. Sessions come from the throwaway door (§10.4),
use `zerops-user:<id>` subjects, and revoke themselves via `POST /api/auth/logout` without
administrative scopes. A session holds no credential of the person's and has no membership window:
the server asks who the project lets in — HQ's relay while it holds, else the member list and the
project's `userRoles` read with its own key (§10.4) — every `T3CODE_ZEROPS_ROLE_RECHECK_SECONDS`
(default 300 s; from the fork's server slice S.0 a configured value above 300 s is clamped to 300 s)
and when HQ relays a different answer, ends every session whose answer is no longer `open` (§3.3),
ends every Zerops session once it has not known for two intervals, and ends any session older than
24 hours. Ending a session closes its sockets; the client opens a new one with a
fresh throwaway, on every rejection and with backoff (from the fork's slice 0.9b). From mate 0.11.81 the client keeps a Mate's session per account across loads and presents it again only where a fresh one would go, once the Mate confirms it still holds it with every scope the client asks for (D33). The client's
credential renewer (`credentialRenewal.ts`) is reserved for a door that re-presents a credential;
the throwaway door does not, so nothing renews a Zerops session. The GUI closes connections and
clears account memory immediately on logout, retaining only account-scoped personal context for
revalidated restoration. A removed project cannot return from an old local catalog.

The GUI's minimum supported Mate server version (`MINIMUM_MATE_SERVER_VERSION`, 0.11.0: the first
server whose only door is the throwaway one) is explicit and independent of its own package version.
**Release rule:** the hosted client's floor never exceeds the Mate version the fork's published
release manifest serves (`stable.json`, §2.1c), the version a pre-connection restart installs; a
floor is raised only after the release carrying that version is published as the latest one. Every
floor verdict — below the floor, and whether a restart helps because the descriptor's
`update.latest` reaches the floor — comes from `serverCompatibility.ts`, the client's one version
comparison (MU-1); below the floor with no restart that reaches it, the Mate is shown as needing a
newer zcp and no verb is offered (the restart verdict from slice 0.9a). Version mismatches show both
the actual server version and the required minimum. Restart uses the platform service restart API
with the user's current account, sends one confirmed request, and then checks the server version; it
never repeatedly restarts on an uncertain response. No manually paired fallback exists.

A rule the fork's client makes true in a later slice names that slice; the fork's
`docs/internals/zerops/client-state-model.md` lists every slice and what it lands.

The complete contract and recovery procedure are maintained in
[Mate account lifecycle](https://github.com/zeropsio/mate/blob/main/docs/internals/zerops/account-lifecycle.md)
and [Mate release recovery](https://github.com/zeropsio/mate/blob/main/docs/operations/account-lifecycle-release.md).
zcp's launch arguments, proxy, supervision, environment variables and persistent container state
are unchanged. Only the selected Mate version, its verified digest and contract golden change.

## 3. The door (S1, historical baseline)

**Superseded 2026-09-16 (mate 0.11.0):** the raw-token door of §3.2 is gone — a person's Zerops
token no longer reaches a container at all — and with it the membership window and the client's
re-mint (MS1-3, MS1-4b). In Zerops mode the server offers `zerops-throwaway` only, reads the
caller's role with its own key and ends sessions itself (§10.4). §3.3 states the session rule that
replaced the window. §3.1 and §3.3–3.6 stand.


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

### 3.3 Session lifetime — no membership window

A session from the throwaway door (§10.4) holds no credential of the person's, so nothing about it
expires with a Zerops token and there is nothing to re-present. Its validity is the server's own
re-check, `ZeropsMembershipWatch`: every `T3CODE_ZEROPS_ROLE_RECHECK_SECONDS` (default 300 s; from
the fork's slice S.0 a configured value above 300 s is clamped to 300 s, so the bound below holds
whatever `mate.env` sets), and at once when HQ relays a different answer, it asks who the project
lets in — HQ's relay while it holds (§10.4), else the org's member list and the project's
`userRoles` read with the Mate's key — by the role function (§10.3), and ends every Zerops session
whose answer is no longer `open`. One pass asks once however many people are connected. A pass that
cannot say changes nothing while the last answer is under two intervals old; past that it ends every
Zerops session, because by then the server has not known who belongs for two intervals — at once
after a lapsed relay, which held one interval already. A session older than
`T3CODE_ZEROPS_SESSION_MAX_AGE_SECONDS` (default 24 hours) ends at the next pass whatever the read
said. A removed member or a lowered role
loses access within two re-check intervals plus one pass.

The end reaches open sockets. `/ws` verifies the session once at the upgrade; for the socket's
life the upgrade races its handler against the session's own end — its deadline, and a
`clientRemoved` change naming it — and ends the connection when either arrives (MS1-4a). A session
with no stored deadline is left alone rather than closed on a guess.

The client does not renew. When its socket is rejected it mints a fresh throwaway and exchanges
again, and the door answers with the current role — on every rejection, with backoff, from the
fork's slice 0.9b. `credentialRenewal.ts` keeps its contract for a door that re-presents a
credential; the throwaway door does not, so nothing renews a Zerops session (MB-3).
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
| MS1-3 | *(historical — the door of §10.4 has no window and nothing renews; MB-3)* The membership window caps a session iff its grant came from the `zerops-identity` door, including a DPoP one that would otherwise default to one hour; a `one-time-token` pairing keeps upstream's lifetime, because it holds no Zerops token to re-mint with. `EnvironmentAuth.test.ts` — "caps a session from the identity door at the membership window", "leaves a one-time-token pairing on the ordinary session lifetime", "keeps DPoP's own lifetime for a one-time-token pairing", "caps a DPoP session from the identity door at the window, not the hour". |
| MS1-4 | `revokeBySubject` revokes exactly one user's sessions, is a no-op on an unknown subject, and counts each session once. `EnvironmentAuth.test.ts` — "revokes every session belonging to one subject and leaves the rest", "counts each session once, however often the subject is revoked". |
| MS1-4a | A live websocket ends when its session expires or is revoked; a session carrying no deadline is left open, and a changes stream that merely ENDS is not a revocation. `sessionLifetime.test.ts` — "reports the deadline once the session's own lifetime elapses", "reports a revocation that lands while the session is still young", "ignores a revocation aimed at a different session", "waits indefinitely for a session that carries no deadline". |
| MS1-4b | *(historical — the door of §10.4 has no window and nothing renews; MB-3)* The client rotates a `zerops-identity` bearer at 80% of its window through the credential store, never by re-registering, and never rotates one that carries no deadline. `credentialRenewal.test.ts`; `registry.test.ts` — "renews a Zerops bearer before its membership window lapses", "never renews a credential that carries no deadline". |
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

### 4.4 The birth

Superseded 2026-10-02: main's birth — a record persisted under the account's key and the account's
birth worker that drove it from any page (`birth/birthStore.ts`, `birth/birthWorker.ts`, Web Lock
`mate:birth:<projectId>`), with its `tags`, `registry`, `harden` and `health` steps — is not in the
rebuild; the press below took its place (pass 28). `provisioning.ts`'s wait (`awaiting-container` →
`awaiting-settled` → `hardening` → `awaiting-health`) and its tests still stand, and nothing drives
it at z3 `7cd89a21a6`.

A Mate is born by **the press**: every step that needs the person's rights, run in the foreground
before _Add_ returns, so nothing after it needs a browser (`matePress.ts:1-17`). Its steps are the
plan's (`planEnvironmentCreation`, `createEnvironment.ts:250-287`), in order:

- `create-project` (`POST /client/{id}/project`), or `import-project` where the tier carries a
  `project:` block; then `import-managed` — a Mate's managed services alone — or `import-recipe` — a
  stage's or a production's tier, whole. A Mate's project is created with the `mate` tag, a stage's
  or a production's with none: where it stands is HQ's to say (`createEnvironment.ts:347-355`).
- `import-container` — the zcp container, holding the key the person mints for it, and the tier's
  runtimes for zcp to import on boot (`MATE_SETUP_RUNTIMES`).
- `close-off` — `envIsolation` written `service` wherever a read says anything else, then the
  project marked closed off at HQ once two reads two seconds apart say `service`
  (`runEnvironmentCreation.ts:195-202`, `:396-406`). Where HQ is not open in the tab the mark is
  not written, and _Finish setup_ writes it (`matePress.ts:634-638`, `:673-676`).
- `register` — the registration in the organization's HQ (MB-24): a Mate's record attached to its
  application with its face, or a record in no application (`POST /api/mates`), with who
  asks for its stand-up in the same write — an attach that closes the Mate's birth intent takes the
  ask the intent was recorded with (`POST /api/births`, `standUp`), any other carries its own
  (`standUp`), so the record and its ask never part; a stage or a production attached as its tier,
  with its deploy key (§10.11) (`matePress.ts` `pressRegistration`, HQ `structure.ts`).
- `share-reach` — the group's other Mates' keys extended to `READ_ONLY` on the new project where the
  person may edit them; best-effort, said on its step, the group-reach reconcile covering the rest
  (`runEnvironmentCreation.ts:407-419`, `matePress.ts:534-623`).
- `await-ready` — for a Mate, the hand-off: the press returns; for a stage or a production, the wait
  for every service `ACTIVE`, at most 600 s, read every 5 s (`runEnvironmentCreation.ts:222-224`,
  `:441-462`).

The close-off comes before the registration, so a registration HQ refuses leaves a Mate closed off
and running, the refusal said on its step, for an owner's _Finish setup_; a stage or a production
has no close-off to keep, and its press stops there (`runEnvironmentCreation.ts:420-435`).
_Finish setup_ — on a half-made Mate's ⋯ menu, by an owner or an admin, in any browser — runs the
same steps from the container on: `import-container` where the project has none, `close-off`,
`register` where there is a registry to write, `share-reach`, `await-ready`; a Mate made before the
press first has its key lowered from `ADMIN` and moved off the project's variables (`hardenMate`)
(`matePress.ts:828-858`, `:912-959`). A pool project a registration claimed goes the same way once
the inventory lists it — hardened, closed off, nothing registered; a registration that answers
`zcpClaimed:false` has no project to set up and opens _New project_, and a missing field reads as
claimed (`ZeropsProjectsPage.tsx:2034-2083`).

**Nothing is stored.** The tab keeps its presses in memory for as long as its screens need them; a
reload forgets them, and the listing draws the rest (`matePress.ts:14-17`). One press or _Finish
setup_ runs per project across the browser's tabs, under the Web Lock `mate:press:<projectId>`
(`mateLocks.ts:23`); one that finds it held says "Its setup is already running in another tab."
(`matePress.ts:731-744`). The close-off's mark and the registration are each tried up to four times,
two seconds apart. A press that stopped at `import-container`, `close-off` or `register` resumes
there with _Try again_, on the same project (`share-reach` never stops one); one that stopped at its
project or at a services import is not resumed — a second import is refused for the hostnames the
first made — and offers _Remove_ instead (`runEnvironmentCreation.ts:204-220`,
`matePress.ts:812-824`, `mateComing.ts:18-20`).

**After the press the container does the rest**, with a browser open or none: zcp enrolls the Mate
with its HQ, which is its Git access, from its boot on (zcp `internal/service/service.go:256-259`,
`internal/hq/enroll.go:62-67`), imports the tier's runtimes only once Zerops reads the project
closed off — HQ's mark is a receipt (`internal/ops/closed_off.go`, `internal/matesetup/runtimes.go`
`awaitClosedOff`), and the Mate's server starts the stand-up once HQ's record names who asked, or
settles it as none at once where it names nobody (`ZeropsSetup.ts`). Any browser reads where that
stands off `GET /mate/setup.json` — `container`, `git`, `runtimes`, `signin`, `standup` — and an
older Mate's off `/mate/healthz` (`mateSetup.ts:1-26`). **Nothing connects to a Mate before its
project is closed off**: auto-connect wants a Mate only once its health reads `ready`, and holds
one whose container carries the press's marker while HQ does not hold its project closed off
(`autoConnect.ts:44-49`, `:71-80`, `closeOffGate` `:103-121`); _Finish setup_ is what clears it
(`interruptedPresses`, `matePress.ts:991-1009`).
`runEnvironmentCreation.test.ts` — "closes off before the registration", "keeps a Mate closed off
and running when its registration is refused", "stops a stage's press at a refused registration: it
has no close-off to keep", "reads no process, and closes off in two reads two seconds apart",
"resumes at the step that stopped, on the project the first press made"; `matePress.test.ts` —
"settles a press that stopped with Try again, which resumes it at the step that stopped", "runs none
where another tab is pressing the project, and says so", "hardens, then marks it closed off without
reading the isolation again"; `hqMateBirth.test.ts` — "leaves a close-off to the registration where
HQ holds no record yet", "keeps a record HQ holds already"; `createEnvironment.test.ts` — "creates a
production with no tag of ours: where it stands is HQ's to say"; `autoConnect.test.ts` — "the Mate
on screen still waits for its health and its close-off"; zcp
`TestRun_ImportsOnlyIntoAProjectClosedOff`, `TestRun_WaitingToBeClosedOff_SaysSo`.

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

_New project_ is one form at `/zerops/new` — the name, and the location when the account offers
more than one — with one button, _Create project_ (mate 0.11.2). The org chooser, the agent-selection
step and the brief the wizard once carried are gone: a one-option chooser is noise, the container
offers every agent and the signer decides who runs one (§10.5), and the person's first words belong
in the conversation. The sidebar's `+` and the first-run page's one button are its entry points.
What _Create_ does, in order (`ZeropsNewProjectHost.tsx:240-321`, `newProjectBirth.ts:8-20`), over
the organization's official HQ, which stands before any project does — an organization with none
sees HQ's gate in place of the product (`ZeropsHqGate.tsx:1-5`):

- **The registry.** The project's application in HQ (`POST /api/apps`), which HQ names: its id is
  the project's group from then on. Nothing platform-side is made for the project itself
  (`ZeropsNewProjectHost.tsx:6-16`, `:271-273`). Superseded 2026-10-02: main made the org's Gitea
  project here when it had none (`ensureGitea` → `createToolProject`) and wrote the group on it
  (§10.6).
- **The first Mate's project and its container in one call** (`createProjectWithZeropsMate`,
  `api.ts:1894-1960`): the two calls traced from the GUI, `POST /client/{id}/project`
  (`mode:"LIGHT"`, the `mate` tag on it before the container goes on) then
  `PUT /project/{id}/first-class-recipe/development-container` with the platform's own import YAML
  byte for byte plus `ZCP_MATE_ENABLED` — without it `zcp init` installs no bundle, registers no unit
  and publishes no `/mate/` location. `ZCP_AGENTS` is emitted only for a selection, and the dialog
  makes none, so the key is absent and the container offers every agent (MC-11 keeps the document's
  rule). `VSCODE_PASSWORD` is generated client-side (`crypto.getRandomValues`, rejection-sampled)
  and sent once; a second container in the same project is named `zcp1`.
- **Then the press** (§4.4), from the moment the platform takes the project: `close-off`,
  `register` — the Mate attached to the application with the face the dialog asked, its
  birth recorded closed off and no stand-up asked, since a new project has no code to stand up —
  `share-reach`, `await-ready` (`ZeropsNewProjectHost.tsx:20-26`, `:276-313`). Superseded
  2026-10-02: main's birth carried the Mate's membership and the broker's grant as its `tags` and
  `registry` steps, and its `harden` closed the project off.
- **The dialog stays on the press** until the project is marked closed off and its registration
  has gone through or been refused, then lands on the first Mate's own view — `/mate/new/$birthId`,
  which hands the route to `/mate/$projectId` (`pressThrough`,
  `ZeropsEnvironmentCreationDialog.logic.ts:525-534`; `newProjectBirth.ts:12-14`). A step that
  stops says why there, with _Try again_, which resumes from that step — except a creation the
  platform may have taken anyway, which a second try could make twice (`newProjectBirth.ts:16-20`).
  A tab closed before the close-off leaves a Mate the ⋯ menu's _Finish setup_ completes, in any
  browser (`ZeropsNewProjectHost.tsx:31-39`). The platform's verdict on the creation is
  `project.create`'s, read from `POST /process/search`; one it failed after answering `200` reads
  "Could not be created." with the projects page's _Remove_ (`candidates.ts:160-188`,
  `creationFailedLine`; ledger 2026-09-16, _A project creation that the platform failed after
  answering 200_).

`newProject.test.ts` — "generates the container password, sends it, and forgets it", "draws from the
injected randomness without modulo bias", "never emits a container with a public subdomain and no
password", "matches the platform's own numbering", "emits the platform's own import document, byte
for byte, plus the mate flag", "emits an empty agent list as exactly the no-agent document";
`newProjectBirth.test.ts` — "registers the project in the organization's HQ, then creates its
Mate", "resumes from the step that stopped it, with the same project, on Try again", "never tries
again a creation the platform may have taken", "is through once its Mate is marked closed off and
registered, and not before"; `projectCreation.test.ts` — "takes the newest project.create of that
project, whatever the order", "recognises the platform's empty internal error and nothing else";
`ZeropsProjectsPage.test.ts`.

A creation's container reaches the projects page through the platform's pushes, and a push can be
missed: the card waited at "Almost there." on a Mate that had answered minutes earlier, while a
reload found it at once (the owner's run, 2026-09-17). The press asks for its organization's
inventory once the platform takes the project, so the listing catches up with it
(`ZeropsNewProjectHost.tsx:288`, `useEnvironmentCreation.ts:215`); superseded 2026-10-02, main's
birth read its own project directly and asked for the inventory once its harden found the container.
A re-read keeps what the page holds (0.11.11): the runtime re-establishes the organization's
interests on a fresh receiver and releases nothing, so the reads keep their members until the new
baseline replaces them, and the page paints from the list already read — a re-read spins the
header's reload glyph and nothing else. Before that the clock re-took the leases, which drop what
they read, and the page painted "Reading your projects…" and an empty menu at every re-read (the
owner's run, 2026-09-17: "it does this full refresh, that's crazy bad").

**A Mate's name is its Zerops project's name** (D3, 2026-10-03, the owner: Zerops is the source of
truth, HQ stores only what Zerops lacks, and a name's source is Zerops). The _Add Mate_ dialog asks
for a name, a colour and a shape and nothing else, and the Mate's project is created under that
name, whole — "Fen", never `<project> - <name>`; the first Mate at _New project_ likewise. HQ holds
no name of a Mate or of a Mate on its way (HQ migration `0030`), only its face, its place and its
birth; every surface names a Mate by its project, and the names taken on the account are the Mates'
projects'. _Rename Mate_ renames the project (`PUT /project/{id}` through the client's one writer
of a project's record, its tags put back as a fresh read holds them), offered where the platform
takes it — effective `OWNER` or `ADMIN` there; a rename in the Zerops GUI reaches the client by the
organization's project updates. A stage and a production are offered `<project> - stage` and
`<project> - production`, numbered once taken — a suggestion the person may change, here or in
Zerops, that nothing reads back; they have that one name, an agent in one going by it, and every
surface names them whole, never cutting the project's name off as a prefix. An application's title
stays HQ's (`proposedEnvironmentName`; `ZeropsEnvironmentCreationDialog.logic.test.ts`
"proposedEnvironmentName", `newProjectBirth.test.ts` "named as its Mate is"; HQ `structure.test.ts`
"a Mate goes by its project's name in Zerops, renamed there as it is"). Supersedes the 2026-09-17
rule "A Mate is named after its bot": "Todo - Fen" and its `<project> - <name>`.

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

One opening message is sent rather than composed. An environment this client created — the
new-project wizard, or a new environment in a group — carries a **creation hand-off**
(`creationHandoff.ts`): what the environment is, where its application came from, and the one job
left. Its birth keeps it until the connect names the environment, and the environment keeps it
until the job is said. It takes the fixed prompt's place in the composer, and `useZeropsCreationJob` sends it once a
coding agent is signed in and the thread can take it, retrying for up to 90 s. The hand-off is spent
only when the send lands, so a reconnect, a second tab or a later visit says nothing, and a send that
never lands leaves the job in the composer for the person to send.
`creationHandoff.test.ts` — "says the job once everything it needs is there", "says nothing while no
agent is signed in", "waits rather than gives up: the same input answers again once it is ready",
"refuses an empty composer rather than reporting an empty send as sent".

Closed 2026-09-16 (D17, mate 0.11.2): the form asks no question, so there are no words of the
person's to send; the generated hand-off is composed into the composer and left for them
(`resolveZeropsCreationJob` answers `compose` for a hand-off with no brief). Nothing is sent by
itself.

### Invariants

| ID | Invariant |
|---|---|
| MC-1 | An unrefreshable `401` signs out; a `403` from refresh keeps the session. `api.test.ts` — "maps 403 to a forbidden error carrying the platform code, keeping the session", "maps an unrefreshable 401 to an expired-session error and signs out". |
| MC-2 | A candidate is identified by service type, never hostname; every zcp container in a project is offered separately. `candidates.test.ts` — "finds a zcp container by service type, whatever its hostname is", "offers every zcp container in a project, not one per project". |
| MC-3 | Registration never sends without a Turnstile token; the platform's captcha refusal and Cloudflare's own domain-binding error both render the same "sign up at app.zerops.io" fallback. `api.test.ts` — "refuses to send a registration with no captcha token"; `turnstile.test.ts` — "names the domain refusal the way a person can act on". |
| MC-4 | A birth's wait reads only direct endpoints, never `/project/search`; an empty read is never a "does not exist" verdict. `provisioning.test.ts` — "never concludes 'no container' from one read of a fresh project"; `birthWorker.test.ts` — "reads its own project until the container's boot is over, then hardens it once". |
| MC-5 | The mate descriptor is the readiness authority; a 5xx on either probe is always `unreachable`, never `predates-mate`; a pre-mate and an unreachable container get the same "Enable Zerops Mate" offer. `containerHealth.test.ts` — "treats the mate descriptor as the authority, and asks nothing else once it answers", "never reads a 5xx as a container that predates Zerops Mate". |
| MC-6 | The Zerops access token appears in exactly one request body field during identity connect, never a header. `onboarding.zerops.test.ts` — "puts the Zerops token in the identity request and nowhere else". |
| MC-7 | `VSCODE_PASSWORD` is generated client-side, sent once, and never read back; a container with a public subdomain always carries one. `newProject.test.ts` — "generates the container password, sends it, and forgets it", "never emits a container with a public subdomain and no password". |
| MC-8 | The first onboarding prompt is composed into the composer once per newly connected identity-door environment, never on reconnect or for a manually paired one; a creation's hand-off takes its place, composed and never sent by itself, and only once a coding agent is signed in. `firstPrompt.test.ts` — "composes once for a freshly connected Zerops environment", "stays quiet on every reconnect to the same environment", "never writes into an environment somebody paired by hand"; `creationHandoff.test.ts` — "names what the environment is, where it came from, and the job"; `birthStore.test.ts` — "keeps a birth from create-accepted until its connect, then the job under its environment". |
| MC-9 | "Enable Zerops Mate" WRITES `ZCP_MATE_ENABLED` and then restarts — a restart alone returns the container to the identical state, because `zcp init` registers no mate step without the flag (§2.0). The write is an upsert (delete-then-create, `sensitive` required, never the bulk env-file PUT), and a flag already reading as on is left untouched rather than rewritten. `api.test.ts` — "writes the Zerops Mate flag before restarting a container that lacks it", "replaces a Zerops Mate flag that is present but switched off", "writes nothing when the flag already reads as on, and still restarts". |
| MC-10 | The Zerops account session fails closed at the outer product mount: only `signed-in` mounts routed/background product surfaces; `loading`, `signed-out`, and `totp-required` mount only account login, while `/zerops_/authorized` stays bare. `-accountGate.test.ts`, `AppRoot.test.tsx`, `ZeropsHostedLanding.test.tsx`. |
| MC-11 | A selection reaches the container as `ZCP_AGENTS` (presentation policy, canonical order), and an EMPTY selection omits the key rather than emitting `""` — absent offers every agent, empty offers none. `ZCP_AGENT_AUTH_TYPE_*` is GUI parity with no in-container reader, and no agent token is ever written to the import document. _New project_ makes no selection since 0.11.2, so the key is absent and every agent is offered. `newProject.test.ts` — "emits an empty agent list as exactly the no-agent document", "emits ZCP_AGENTS as a comma-separated list in canonical order". |
| MC-12 | The account gate covers the Zerops entry AND everything under it, so `/zerops/new` — which creates a real project on the user's own account — is never reachable signed out, including on the branch a local server takes; the identity callback is matched first and keeps its own surface. `-accountGate.test.ts` — "keeps a Zerops entry's sub-route a bare login too, so the project wizard is not reachable signed out", "still hands the identity callback over rather than gating it as a Zerops sub-route". |
| MC-13 | A birth hardens exactly once, in the wait's `hardening` level, before anyone is admitted: it restarts the project's other services, never the `zcp` container, and no page reconcile restarts a project. A level — a birth's or a container's — is left on a read fact, never a timer; a cap past its budget sets `overdue` on the level it is on and is never a state. `birthWorker.test.ts` — "reads its own project until the container's boot is over, then hardens it once", "a reload resumes the health wait on the container hardening found, never hardening again", "a cap past its budget is overdue on the same step, and Keep waiting clears it (MC-13)"; `containerMachine.test.ts` — "a cap past its budget sets overdue on the level it is on, and nothing else"; `projectIsolation.test.ts` — "restarts every other service, never the container"; `useZeropsGroupReach.test.tsx` — "a reach reconcile never restarts a project"; `provisioning.test.ts` — "turns a cap expiry into overdue words, never a stop (B-2)". |
| MC-14 | A registered environment leaves the catalog only when its organization's complete listing no longer holds its project, when its `zcp` service is missing from a complete observing query of the project's services and then from a direct read of those services finished after that omission was seen, or when the person removed it — never because its container reads `RESTARTING`, its project's services are not read yet, or one listing dropped the service. `targets.test.ts` — "a Mate restarting keeps its target, transitioning", "services not read yet: every Mate of the project is unknown, never gone", "a complete listing that lacks the project: gone", "a direct read no newer than the omission holds it", "a deleted service loses its Mate only after a confirming read"; `accountRuntime.test.ts` — "a deleted service loses its Mate only after a confirming read (§9 C19, MC-14)", "a service one listing drops keeps its Mate when the direct read finds it (MC-14)"; `environmentMachine.test.ts` — "an exchange that answers after the user removed the Mate is logged stale and never held". |

---

## 5. Zerops-aware client

Once a member is inside a thread, the client and mate server supply two independent feeds:
**topology** (what exists) comes from the client's platform model, while **lifecycle** (where the
agent is) comes from the server's envelope reducer. They surface as a service map, lifecycle strip
and cards under tool calls. Neither feed imports the other.

### 5.1 The service map is a client projection of the Zerops API

**Implemented in the client, 2026-09-08.** The client owns one account-scoped
`ZeropsDataRuntime`: an event-driven, normalized reactive domain read model with ports/adapters,
pure reducers and command/query separation without client event sourcing. Project, ServiceStack
and Process have shared typed identities; query membership, configuration facets, telemetry
windows, logs and command attempts have distinct representations. Native list/update/metric
streams deliver ordinary changes, including operations initiated by agents, CLI or other users.
REST establishes baselines, hydrates details and repairs observations. There is no general query
cache, SWR, TTL freshness layer or per-event refetch path.

The detailed contracts live in the fork's
[architecture decision](../../z3/docs/internals/zerops/platform-data-architecture.md) and
[consistency contract](../../z3/docs/internals/zerops/platform-data-consistency.md). They fix
source-aware admission, terminating recovery, separate entity/membership/access semantics and
explicit `observing` health without claiming source order, replay or lossless reconnect. Effect
scopes own one receiver per demanded organization, multiplexed interests, bounded ingress/repair,
access-aware resources and build-log sessions. Atom projections publish narrow views. Features read those
projections and invoke typed commands. The existing account verification/admission contract is
unchanged; observations never renew authorization and the server holds no platform token or model.

Inventory, candidates, topology, activity, configuration, current usage, build logs and existing
product mutation callers use this runtime. The former signal/refetch topology watcher, direct
activity poller, inventory reconciliation and feature-owned build-log session are removed. A
successful organization-authorized project creation/import can admit its returned project only
until the unchanged access deadline and next authoritative verification.

What exists in a project — its services, status, subdomains and running processes — is the
platform's fact, read by the client with the user's token (§0, rule 1). There is no server topology
feed: the mate server spawns no `zcp studio watch`, holds no snapshot and answers no topology RPC.
`packages/client-runtime/src/zerops/topology.ts` remains the mapping owner. It composes shared
canonical Project, ServiceStack and Process observations produced by native streams plus direct
bootstrap/repair into the map, quick actions and chat chrome. Grouping is ordered: the `zcp` type
prefix ⇒ **infrastructure**; categories `STANDARD` and `OBJECT_STORAGE` ⇒ **data**; category
`USER` ⇒ **runtimes**; unknown categories ⇒ **infrastructure**. The OS prefix is stripped before
the prefix check. A service is **transient** while its status is unsettled or an
in-flight process names it. `data/runtime.test.ts`; `topology.test.ts`.

**Which project an environment is** is remembered by the client, not asked of the server: the
candidate connected at the door carries its project and organization ids, stored per environment
id (`environmentProjectRef.ts`, source `connect`); an environment connected before this design gets
one origin match through the candidate loader (source `match`, never repeated on a miss for the
same instance); the descriptor's optional `zerops.projectId` (§6.1, S4) is a third source. Sign-out
clears the store. A miss leaves the map empty, never an error.

The map no longer shows which services are **mounted** or the container's own adoption state —
those are container facts (§0 ownership table) and belong to the repository set (§6.1); the
protected surfaces (`ZeropsServiceMap`, `ZeropsLifecycleStrip`, `ZeropsQuickActions`,
`ZeropsOperationCard`) read the view through a read-only atom and import no platform client
(`scripts/mate-zone-architecture.test.ts` — "protected roots render only").

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

### 5.3a Lifecycle order

A projected `thread.activity-appended` row persists the enclosing orchestration event's
`sequence`, never the provider activity's optional nested sequence. That event sequence is the
authoritative order for live delivery, reconnect, and freshly projected history. A legacy row whose
stored sequence is `NULL` sorts by `createdAt`, then lifecycle rank
`tool.started < tool.updated < tool.completed`, then activity id; legacy rows precede sequenced
rows when both populations coexist. Newest-window selection uses the exact inverse comparator
before restoring ascending presentation order, and superseded-update compaction consumes that same
order, so an equal-time completion cannot fall before or outside the update it supersedes.

### 5.4 Web surfaces

The service map and lifecycle strip read the two feeds as atoms; the strip mounts **beside**
`ChatHeader`, not inside it, because it needs pending-question state that lives one level up. A
normalized recognized `zerops_*` tool name is sufficient to create its stable process-card shell;
arguments may enrich the title or detail later but never gate the shell. Every result body is a
**total decoder** (`decode(resultText) → Payload | undefined`). A decoded payload fills the shell's
result region. If a recognized call's result is absent or undecodable — not JSON, the wrong JSON
document, over the cap, or from a pre-S6 server — the shell and its timeline anchor remain mounted:
absence is its pending presentation, while an undecodable terminal result renders the ordinary
generic tool block *inside* its result region. Only an unrecognized tool call uses the ordinary
generic row from end to end. Quick actions only **prefill** the composer; the component's whole
module graph is asserted to import no mutating RPC. The question card gained a visible **"Other"** free-text option and
arrow-key navigation beside the digit keys. A map whose required interests are not all `observing`
reports `polling` and says so in one quiet line, deliberately not a degraded banner.
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
as dot + word; the Zerops Control Plane row (the infrastructure group) uses the shared mint
treatment. A failed read keeps the last-good rows visible and a `polling` map remains one quiet line. Recognized Zerops results use one
shared process-card anatomy — semantic kicker/status, operation title, steps/outcome, and separate
URL or information chips — for `plan`, `import`, `mount`, `deploy`, `verify`, `subdomain`, and
`error`. The total-decoder fallback above remains authoritative for undecodable, absent, oversized,
or future result kinds, but for a call whose tool name is recognized it occupies only the stable
shell's result region and never changes the outer row kind or key.

`zerops_workflow` is an overloaded transport, not synonymous with PLAN: only calls with explicit
bootstrap evidence (or a decoded bootstrap response) use the PLAN shell. Develop-session and
configuration actions remain compact generic lifecycle rows. Likewise, an error returned by a
Zerops tool outside the card registry remains compact and may fold with ordinary work; it is not
promoted into a full error milestone. Import responses aggregate platform processes by service
hostname for their visible step/count, while each process id owns any action/failure detail chip.

**One stable shell per call; one card per lifecycle object.** Every recognized call has a call key
`tool:<turnId>:<toolCallId>`. Its first row fixes the timeline id, position, start timestamp, row
kind, and mounted shell; updates and completion replace only mutable lifecycle/result regions. A
decoded payload may additionally carry an orthogonal server-side *card identity*
(`plan:<sessionId>` for bootstrap responses). Repeated calls sharing that identity fold into ONE
card anchored at the first matching call, rendering the latest matching result. All successfully
decoded kinds — `plan`, `import`, `mount`, `deploy`, `verify`, `subdomain`, and `error` — are
milestones that remain visible through active grouping, settled folds, summaries, overflow, and
pagination; folds hide generic chatter only.

A bootstrap continuation has no `sessionId` in its invocation, so arguments never create a PLAN
identity. While exactly one decoded PLAN anchor is open (`completed < total`), a pending
continuation other than `start` or `reset` may provisionally update only that card's running
presentation. A matching decoded result folds the call into the anchor; a different session,
ambiguity, reset/start, or no open anchor keeps a separate call shell. The association is therefore
reversible presentation, never result truth. Live stream, reconnect replay, cold history, a
windowed snapshot, and current-version warm cache reduce the same lifecycle vectors to the same
call anchors, card set, and order.

**Platform activity is an overlay, never a verdict.** The agent's tool result is the only authority
a card has. While a `zerops_deploy` call is pending and the browser holds the user's own Zerops
session, the client reads the shared native Process projection with direct bootstrap/repair and
renders the attributed process's pipeline steps in the shell's optional region, every string labelled
"Platform".
Attribution requires all of: the topology snapshot's `serviceId` for the call's `targetService`,
the snapshot's project id, `created` no earlier than the server-stamped tool start minus 5 s, and
`created` no later than the tool start plus 30 min, and
`actionName ∈ {stack.deploy, stack.build}`; all matches are shown, other actions in the window are
chips. The overlay is ownership-neutral, per viewer, and in memory only: it never persists, never
enters the transcript or the envelope, and is discarded the moment a normal `resultText` lands — a
disagreement is resolved by the result, silently. Its reducer distinguishes no completed read,
successful-empty searching, observed, settled-on-platform, stale, unavailable, and resolved; each
shared activity projection publishes observation or failure state so recovery cannot freeze.
Absence of activity is never rendered as done or idle; a process that settles on the platform
before the result reads "waiting for the agent's result", not ✓/✗. When observation cannot run (no
Zerops session, 401/403/404, project mismatch, stale beyond 60 s, or 30 min past the tool's start),
only the `Platform` region is absent — the recognized call shell remains. The one allowlisted
exception is a resolved `deploy` whose status is `BUILD_TRIGGERED` (git-push delivery returns at
push time): it may keep the overlay below its own verdict line until the platform settles or the
ceiling passes. A reopened resolved thread therefore shows exactly what the agent reported; a
reopened recognized call that never resolved shows a quiet pending shell without a Platform region
or verdict.

The shell root and semantic step ids stay stable while mutable regions change. Card and step
enter/update/exit motion is one-shot and bounded to 150–200 ms. Settled cards do not pulse, and
`prefers-reduced-motion` removes nonessential transitions.

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

The Zerops project panel opens only when the person opens it — its button, the lifecycle strip, an
agent's Authorize, a link in the conversation. It never presents itself on a Mate's first visit, on
any layout (the owner, 2026-09-27: "this right panel keeps being opened on zerops by default (like
when I add a new mate etc..) I don't think its necessary").

Pending questions and approvals share one visible **Waiting for you** anatomy: attention state,
human request kind, complete detail, progress when queued, and a legible action hierarchy. Provider
options and all keyboard/selection/response semantics remain authoritative. One-shot approval is
the primary action when advertised; broader session permission is secondary; refusal actions stay
visually quieter without being hidden.

**A project's flow is one derivation (D29, 2026-09-23).** `groupFlow` (client-runtime, pure) takes
what the surfaces already hold — the group tree's members, the account's project flow (§10.11) and
the platform's pushed deployments — and returns the project's Mates, its open changes (since
2026-10-02 a Mate's changes in HQ, from HQ's structure stream; recipe changes apart, never a step),
`main`, its stages, its production and its one next step. The
projects page and the left menu feed it through the one input (`groupFlowInputOf`) and the one
gate on _Add production_ (`productionAddable`), lay out what it returns and decide nothing of their
own.
What a stop runs is the platform's pushed deployment; the deploy half's version name stands for it
only while that answer is on its way, so a production whose `appVersionName` names a merge waiting
for its first release reads "Nothing live yet" (measured: `fsadfdasfsa`, `055a7e8`, nothing live).
A Mate's preview is the public route of its pair's stage half (`pairPreviewRoute`: `appstage` beside
`appdev`, `todoappstage` beside `todoapp`), never a service that only ends in `stage`. The next step
is one, worst first: a Mate waiting on an answer, a Mate stopped on an error, a failed deploy —
production's, then a stage's (2026-09-25) — a pull request the person can merge, one that cannot
land, a release, _Add production_, a first task for a Mate nobody has spoken to. A failed stage
never hides a release (D28), and a failed production keeps the release that might clear it, drawn
beside the failure. _Add production_ is the next step once `main` has code, the recipe's production
tier is on `main`, and the person may create one; production reads "After the first merge", with no
button, where `main` is known empty.
**Every merge, landing, release and roll back goes through one Review (2026-09-29).** Every door
to one says _Review_ — a change's row in the left menu, the composer's top, a run's result, a crew's
line, the production chip's menu, the projects page, a change's, a project's and a stop's pages
(_Review release_ for a release), the Git tab, the release rows' _Roll back to this_, the crew board
— and every door opens the same review, a dialog over the conversation (`ZeropsReviewProvider`,
`openReview`): the verdict first, what the change does in the Mate's words, its files and their
diffs, where to try it, and one button that says what it will
do — _Merge_, _Land_, _Release v0.1.57_, _Roll back to v0.1.55_ — beside what that does. Since
2026-10-02 a change's files with their diffs, its commits and how it merges are HQ's one detail of
the change, read when the review opens and kept per head and per `main`, and a change in HQ runs no
checks: the checks are gone from the review, the rows and the menu. Nothing
merges, lands, releases or rolls back from a row, a banner or a page: the one-click merges of the
banner, the projects page and the Git tab, and the roll back that asked nothing, are gone. The
button is off while the verdict says why (a conflict with `main`, a head whose files are not shown
yet); the focus and ⌘↵ reach it only while it is safe, never for _Release_ or _Roll back_; and a
merge takes only the head the review showed. A change merges and closes in HQ, as the person, only
where HQ's rule offers it (`merge_change`: Basic user or above on one of the application's projects;
`close_change`: the same, or the organization's owner or admin): _Merge_ says "Merging into main" ·
"Squashing N commits into one" while it runs and "Merged into main" once done, and a refusal comes
back in HQ's words, "Not merged", tried again only by a deliberate press; a quiet _Close without
merging_ in the review's foot asks "Close #N without merging?" before it closes. Nothing is polled:
HQ's stream brings the change's new state. A change's conversation (people's comments) and the
pictures of its description are HQ's too.
A Mate's conversation offers only what is that Mate's, at the composer's top: its own mergeable
change (MB-30), read from the flow's open changes, so the strip and the page cannot
disagree about one — offered as _Review_, never as a merge, for the merge is the review's own
button, where the change is read first. A release carries every Mate's merges and a production is
the project's, so both stay with the project — its production chip in the left menu, and the
projects page — and never offer themselves in one Mate's conversation (the owner, 2026-09-26:
"merges could be coming from different mates, let's keep it on the left").
`groupFlow.test.ts` — "takes the worst step first: $case" (its cases include "a failed stage is
the deploy to fix, ahead of a release"), "names production's failure before a stage's, and keeps
the release a failed stage does not block", "still ranks a failed production above the release",
"reads production as $case", "offers Add production only where $case → $addable",
`pairPreviewRoute` "is $case"; `mateNextStep.test.ts` — "$case" (its own change, waiting for the
person's review; nothing for another Mate's change, its own that does not merge, a recipe change):
it reads only the open changes, so it cannot offer a release or a production;
`ZeropsNextStepBanner.test.tsx` — "reads as the composer's top: the Mate's face asking, the words,
Review", "opens the change's review from the button that was pressed";
`ZeropsReviewDoors.test.tsx` — "every door opens the review and never acts itself (R1)", "the
review's one button (R5)"; `reviewVerdict.test.ts` — "changeReview: the verdict comes first (R2)",
"changeReview: Merge takes only a head whose change was shown";
`SidebarZeropsTree.test.tsx` — "offers Review on every change, and never Merge, Ask or a check dot
from the row", "never dots a project for the production it does not have".

**Open: `main` is not read.** No caller supplies `mainHasCode` or `mainHead`: `groupFlowInputOf`
leaves both unread for every group, and the default-branch read (`planMainHeadReads`) runs only for
a group that already has a production and feeds the release offer, not `groupFlow`. So the flow
knows `main` has code only from a merged code pull request still in the recent list. Until it is
wired, "After the first merge" is never drawn, the flow's own _Add production_ misses code a recipe
planted at birth or a merge that has scrolled off that list, and the project's menu offers _Add
production_ as the stop-gap wherever the role is still creatable and the person may create
projects. The conversation's gate is the page's, with "some Mate in the project is up" taken as met,
since the conversation runs no health probes.

### 5.5 Subscriptions are flow-controlled — a raw probe must `Ack`

effect-rpc applies per-request flow control to streamed responses: after each Chunk the server
closes a latch and waits for the client's `[{"_tag":"Ack","requestId":"<id>"}]` before sending the
next. A client that never acks receives exactly one Chunk and no Exit on a healthy socket —
indistinguishable from a feed that stopped publishing. `RpcClient` acks automatically, so every real
client sees pushes; a hand-rolled WebSocket probe of any `subscribe*` method must ack every Chunk.
`server.test.ts` — "subscribeZeropsBrowserStream applies flow control (Ack after every Chunk)".

### 5.6 Browser surface

One browser, in the container, owned by zcp. The agent drives it through `zerops_browser` (a
bounded batch; §0 rule 3); the user sees the same browser two ways, both through the mate server
(§0 rule 2), and zcp knows nothing about either viewer.

- **The card.** A `zerops_browser` call with `screenshot: true` returns the PNG as a second MCP
  content block; Claude Code forwards it (measured: ~300 KB base64 at the default viewport). The
  SPI carries it as an optional `images` list on the tool-call result (SPI 2.2; one image ≤ 1 MiB
  base64, else dropped with `imagesDropped`; the 48 KB text cap is unchanged), the activity
  projection keeps it on the completed row, and the browser card renders it as its thumbnail.
- **The live view.** `ZeropsBrowserStream` connects, on the first subscriber, to the agent-browser
  daemon's own stream (`ws://127.0.0.1:<port>/?pacing=ack&maxFps=10`, the port read from
  `~/.agent-browser/default.stream` on every attempt, never written) and relays `frame`, `status`,
  `tabs` and `url` to `subscribeZeropsBrowserStream`; `zeropsBrowserInput` forwards CDP-shaped
  `input_mouse` / `input_keyboard` messages. Acks are forwarded, never generated: the daemon's
  `{"type":"ack","seq"}` goes out only when the mate client has acked the frame (§5.5), one frame
  in flight end to end; a stalled client stops receiving frames while state keeps flowing; with
  several clients one ack per seq and the payload held once. The daemon socket closes on the last
  unsubscribe (the feed's idle TTL is seconds, not minutes) and reconnects with backoff while
  subscribed; no browser open is `no-browser`, never an error; a server without the method (0.3.0
  and older) makes the panel say unavailable, never a toast.
- **The panel.** Input is disabled while a `zerops_browser` call is in progress unless the user
  takes over (reset on the agent's next call); a line says who is driving and which page; clicks
  map canvas → viewport CSS px through the frame's device size and `pageScaleFactor` (no scroll
  term: CDP input is viewport-relative); drags carry `button: left`, hovers are not forwarded;
  the last frame stays on screen when the page is quiet.

### 5.7 Data surface

One data console, in the container, owned by zcp. The user opens a **Data** panel in the client and
browses the project's data services: a service list, a tree per service, a table grid, a read-only
SQL statement, a blob preview. Discovery is the console's own — mate does not project the service
map (§5.1) onto it, because the console classifies by engine family and support tier
(`spec-dataconsole.md` §6) and the platform's service list does not carry that. Refresh is manual:
a service added while the panel is open appears after a refresh, never by push. A blob is a
preview, cut at 256 KiB in the mate server and flagged truncated; there is no download in this
slice.

- **The process.** The mate server spawns `zcp studio console serve` on the first request that
  needs it, one child per server, with stdin and stdout as pipes; the process contract — ready
  line, loopback bind, bearer, stdin-EOF shutdown — is `spec-dataconsole.md` §4, not restated here.
  The child is reused afterwards, killed after ten minutes with no request and at server shutdown,
  and EOFs on its own if the server dies without killing it. The ready line is read once and never
  logged, traced, or sent to a client.
- **The broker.** The client never talks to the console. Every request crosses mate's WebSocket RPC
  to a server module that maps one typed request onto one allowlisted method and `/api` path
  (the same closed list the VS Code broker mirrors, `spec-dataconsole.md` §4.1), dials only the
  ready line's URL — which the server refuses unless its host is loopback — and adds the bearer
  there. Same trust-boundary shape as that broker and as the browser stream (§5.6): the broker
  executes what the client asks, so the fixed destination and the closed path list are what keep an XSS in a rendered cell from becoming a localhost SSRF.
- **Degrade, never crash.** The session carries a status: `idle`, `starting`, `ready`,
  `unsupported`, `unavailable`. A child that exits before printing a ready line is `unsupported`
  when zcp reports an unknown subcommand; a child that prints nothing within a bounded wait is
  killed and reported `unavailable`, which is also what a zcp without `studio` at all yields.
  `unavailable` carries a sanitized one-line reason — the raw stderr never reaches a client, it
  carries container paths. `unsupported` holds for the server's lifetime; `unavailable` is retried
  by the next call, never by a timer. Both states are visible in the panel, never a toast and
  never a respawn loop.
- **Read-only in this slice.** `--allow-writes` is not passed, no write token is minted, and every
  mutating route is refused by the console itself. Writes arrive later the way the console already
  expects them (`spec-dataconsole.md` §5): a confirm in the Mate UI, and a write token that lives in
  the mate server beside the bearer and never reaches a client.
- **Two hosts, two processes.** The Zerops Studio extension keeps spawning its own console for the
  code-server surface. Nothing is shared between them — not the process, not the port, not the
  tokens.

This stays inside §0 rule 3. zcp learns nothing about mate here: the console is a CLI subcommand
mate spawns, not a layer zcp grows for it, and it is not configured through `zcp init` or the unit
contract (§2.8) — an old zcp reports `unsupported`.

### Invariants

| ID | Invariant |
|---|---|
| MF-1 | The service map is a shared client projection of platform observations read with the user's token through native streams plus direct bootstrap/repair; the mate server exposes no topology RPC and spawns no `zcp studio watch`. `data/runtime.test.ts`; `topology.test.ts`; `scripts/mate-boundaries.test.ts` (MA-6). |
| MF-2 | Taxonomy order is type-prefix `zcp` ⇒ infrastructure, then category `STANDARD` or `OBJECT_STORAGE` ⇒ data, category `USER` ⇒ runtimes, else infrastructure; the OS prefix is stripped before the prefix check. `topology.test.ts` — "groups a captured project into runtime, data and infrastructure". |
| MF-3 | The lifecycle reducer gates on the tool NAME, never `itemType`. `zeropsActivityResult.test.ts` — "accepts zerops_delete, whose itemType Claude misclassifies". |
| MF-4 | A JSON-document result's top-level `envelope` key is the unconditional carrier; the fence rule never runs on it, even when the document's own text quotes a fence. `zeropsEnvelope.test.ts` — "does not read a fenced block quoted inside a JSON document", "still prefers the envelope key when the document also quotes a block". |
| MF-5 | The latest envelope per thread survives a container restart. `ZeropsLifecycle.test.ts` — "reads a thread's state back after a restart". |
| MF-6 | A `zerops_*` result's raw text reaches the client on all three projection routes, capped at 48,000 bytes; over the cap the text is dropped whole, never sliced. `ActivityPayloadProjection.test.ts` — "carries it on the live event path", "carries it on the thread-detail snapshot a reopened thread renders from"; `zeropsActivityResult.test.ts` — "drops the text whole when it exceeds the cap, and says so". |
| MF-7 | A recognized call whose result cannot decode keeps its call shell and renders the generic tool block inside the result region; an unrecognized call stays a generic row. Quick actions never call a mutating RPC. `ZeropsCallCard.test.tsx` — "keeps the shell for an undecodable terminal result"; `ZeropsQuickActions.test.tsx` — "cannot reach Zerops or the RPC layer at all". |
| MF-8 | Native typed payloads update the shared model without per-event rereads; membership and entity state are separate; reconnect, overflow or failed registration degrades affected interests and triggers bounded recovery. `data/runtime.test.ts`; `data/platformProtocol.test.ts`; `data/restAdapter.test.ts`. Live observations are recorded in the fork's `verified.md`; no lossless or monotonic guarantee is claimed. |
| MF-9 | A recognized call keeps its first call key/id/time/position/row kind and mounted shell through completion. Result-derived `plan:<sessionId>` identity may fold matching calls into the first PLAN anchor; pending association never invents identity and safely detaches on start/reset/ambiguity/mismatch. Every decoded result kind is a visible milestone. `callLifecycle.test.ts`; `MessagesTimeline.logic.test.ts`; `identity.test.ts`; `milestone.test.ts`. |
| MF-10 | Platform activity is an in-memory, ownership-neutral, "Platform"-labelled optional region read from the §5.1 shared Process projection with direct bootstrap/repair. It publishes observation and failure state, never persists or renders a verdict, and disappears without removing the recognized shell. The only post-result continuation is a `BUILD_TRIGGERED` deploy. `data/activity.test.ts`; `useProjectActivity.test.ts`; `ZeropsDeployActivityCard.test.tsx`. |
| MF-11 | Projected activity order uses orchestration event sequence. Legacy NULL rows use `createdAt`, lifecycle rank `started < updated < completed`, then activity id; newest-window selection and compaction preserve the same order and terminal row. `ProjectionPipeline.test.ts`; `ProjectionSnapshotQuery.test.ts`; `ActivityPayloadProjection.test.ts`. |
| MF-12 | One browser vocabulary: mate carries no MCP server and no browser of its own; the agent's browser is `zerops_browser` and nothing else. `scripts/mate-boundaries.test.ts` (MA-6); `serve accepts --no-browser`; fork.md delete rows for the preview directories. |
| MF-13 | The container browser reaches the client only through the mate server: frames over `subscribeZeropsBrowserStream` with forwarded acks (one daemon ack per seq, sent after the client's Ack), input over `zeropsBrowserInput` (operate scope), the daemon port read from `~/.agent-browser/default.stream` and never written, the socket closed on the last unsubscribe. `ZeropsBrowserStream.test.ts` — "connects on first subscriber and disconnects on last", "two subscribers, one stalled: one ack per seq", "an ack or input during reconnect never ends the subscription"; `server.test.ts` — "subscribeZeropsBrowserStream applies flow control (Ack after every Chunk)". |

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

`ZeropsRepositorySource` answers "which repositories exist" from the container's own **mount
table** (`/proc/mounts` by default, injected for tests) — never a platform call, never a scan of
`/var/www` for `.git`, and never `zcp` (§0 rule 3: the mate server spawns `zcp` only for the closed
list of §0). A repository is a `fuse.sshfs` mount whose mountpoint is a direct child of
`/var/www` (`<host>:/var/www /var/www/<host> fuse.sshfs …`, one line per mounted dev service,
measured 2026-09-04) and whose bounded probe succeeds: `stat` with a 2 s timeout, the same check
zcp runs before it reports a service mounted. A mount whose probe times out is dropped from the set
and the others are kept; the `/var/www/<host>` layout is one of the two conventions mate reads
from the container (§0 touchpoints).

Three outcomes, deliberately distinct — a caller must never read one as another:

| Outcome | Means | A caller does |
|---|---|---|
| `disabled` | not a Zerops environment | nothing — the mount table is never read |
| `unavailable` | Zerops, but the mount table could not be read at all | degrades, names the reason, warns once (cleared by the next successful read); never empties the set on this path |
| `available` | the answer, possibly `[]` | `[]` renders "no repositories yet" — a fact, not an error |

The set is cached for 30 s (`REPOSITORY_CACHE_TTL`) and unconditionally re-read by `refresh`, which
a turn start calls explicitly. `ZeropsRepositories` consumers — the git spawner (§6.2), the
checkpoint reactor, the diff query — see the same shape they always did.

The environment descriptor (`/.well-known/t3/environment`, contract C-5) states the container's
Zerops project as an optional `zerops.projectId` (from `T3CODE_ZEROPS_PROJECT_ID`, Zerops mode
only): a fact the env contract already owns, stated non-secretly, and the third source of the
client's environment→project ref (§5.1). Additive only; a server too old to have the field is a
server without it, never an error.

zcp guarantees every mounted dev service's `/var/www/<host>` is already a git repository by the
time mate would look — bootstrap runs the HEAD guarantee (GLC-1), adopt preserves existing history or creates a snapshot (GLC-7)
(`docs/spec-workflows.md`'s Git Lifecycle section) — so mate never scans for one or falls back to
initializing it itself; `ZeropsRepositorySource` still reads the mount table, not `.git`, to
answer "which repositories exist" (§6.1 above).

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
anything that needs to know about host-side change must poll over SSH rather than watch; nothing in
S3 tries to watch the mount for git state.

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

### Invariants

| ID | Invariant |
|---|---|
| MG-1 | The repository set is the `fuse.sshfs` mount table under `/var/www`, probed with a bounded `stat`; three distinct outcomes; never a `/var/www` scan for `.git`, never a platform call, never `zcp`. `ZeropsRepositorySource.test.ts` — `parseMountTable` (a literal `/proc/mounts` fixture; non-sshfs and outside-`/var/www` lines ignored), "drops a mount whose probe times out and keeps the others", "reports unavailable when the mount table cannot be read and never empties the set on that path", "keeps the 30 s cache and refreshes at turn start". |
| MG-2 | No git process ever runs against the sshfs mount: every `git` spawn located under a mount is rewritten to `ssh … git -C /var/www …`; every non-git command and every `git` call outside a mount passes through byte-identical. `ZeropsGitSpawner.test.ts` — "hands a non-git command to the inner spawner untouched", "leaves git alone outside every mount", "resolves the host from the -C form and rewrites -C to the remote path", "no git argv ever carries a mount path". Live: verified.md S3 live audit — zero bare git processes, zero argv carrying a `/var/www/<host>` path. |
| MG-3 | Only `GIT_*` and `LC_ALL` cross the wire, `GIT_TRACE2*` is stripped, path-valued flags/env are mapped mount→host, and the two absolute-path-returning argv shapes are mapped host→mount (`--git-common-dir` left alone). `ZeropsGitSpawner.test.ts` — "forwards only GIT_* and LC_ALL, and never the server's own environment", "strips the trace2 event stream, whose file is local and whose watcher never fires", "maps the two argv shapes that return an absolute path", "leaves --git-common-dir alone, because git answers it relatively". |
| MG-4 | ssh's own exit 255 is reported as a distinct transport failure, never as a git verdict; concurrency is capped per host (4) and unbounded across hosts. `ZeropsGitSpawner.test.ts` — "names an ssh transport failure rather than letting it read as a git verdict", "caps concurrent sessions per host without capping across hosts". |
| MG-5 | Worktrees, the stacked commit→push→PR action, and background fetch are off on Zerops, enforced at the decider / `GitManager` — never left to a default a client or a `.t3/project` file could override. `ZeropsPolicy.test.ts` — "thread.create persists a null worktree path on Zerops", "project.meta.update forces the default thread env mode to local", "refuses the stacked commit/push/PR action server-side", "never lets a status read fetch from a remote". |
| MG-6 | Restoring a checkpoint on Zerops never runs `git clean -fd`; every other environment keeps it. `ZeropsPolicy.test.ts` — "leaves what the running application wrote on Zerops", "still cleans untracked files everywhere else". |
| MG-7 | A turn's checkpoint fans out per repository under the identical ref name and merges into one sorted, `<host>/`-prefixed diff; one repository's capture or diff failure never costs another's history. `ZeropsCheckpointTargets.test.ts` — "captures once per repository and merges the diffs into one grouped list", "names the turn with one ref in every repository, which is what keeps the projection flat", "keeps one repository's checkpoint when another's fails". |
| MG-8 | The untracked-file guard probes fresh before every capture (never memoized) and refuses only the overflowing repository. `ZeropsCheckpointTargets.test.ts` — "refuses only the repository whose untracked set overflows the probe", "keeps refusing while the repository still overflows, however often it is asked"; `ZeropsUntrackedProbe.test.ts` — "reports truncation once the untracked path list passes the cap"; `ZeropsGuardOverSsh.test.ts` — "refuses a repository whose untracked set overflows, exactly as it does locally". |
| MG-9 | A deleted thread's checkpoint refs are pruned from every repository it touched; the swept set is the absolute mounted repository set on Zerops, not the thread's own cwd. `ZeropsCheckpointTargets.test.ts` — "deletes every ref the thread left in every repository it covered", "tolerates a repository that is gone and still prunes the rest", "sweeps every mounted repository, without needing the deleted thread's cwd". |

### 6.6 A Mate's HQ access is its enrollment (2026-10-02; D20 superseded)

A Mate reaches its HQ with the credential its enrollment holds, and nothing else: `zcp service
mate` keeps it enrolled (`hq.Keep`, C-7), and zcp opens the enrollment at each use, so a
re-enrollment's credential is the one it uses. Kept enrolled is the kept HQ asked alone every 10 min
whether it still knows the credential (`Enroller.Recheck`); the org's member list is read again for
the official HQ only with no enrollment kept, when HQ refuses the credential (`401
mate_credential_required`, or names another project for it), or when the kept HQ has not answered
for 10 min — an HQ that lost the anchor answers `503 not_active` for good, a deploy's handover for
seconds (R6). An enrollment the official HQ cannot serve goes back to rechecking the kept one with
that clock reset, so an HQ outage reads the member list at most once per 10 min; a new Mate's, with
nothing kept, is tried again on the usual backoff capped at 60 s, so a Mate born in an outage
enrolls within a minute of HQ answering. Everything zcp does with HQ goes through it — a
pair's work delivered as a change (§10.10, *Delivery to HQ*), the recipe proposed and its AI Agent
tier read for the stand-up, a repository the tier names checked before HQ is asked for it — over
HQ's Mate API (`Authorization: Mate <credential>`) and git over HTTPS as the user `mate`. The dev
container now holds the Mate's HQ credential as its `GIT_TOKEN`, as it held the Gitea bot token
before.

**One Mate per project (audit D2, 2026-10-03).** A Mate is a `zcp@1` service, and HQ keys a Mate
by its project — its record, its credential, its overview — so a project holds one. The Mate's
record at HQ names its zcp service: the one the client that set it up found there already (*Set up
Mate*, *Finish setup*), else the first that enrolls naming itself. zcp names its own service with
each enrollment (`hq.Enroller.ServiceID`, the container's `serviceId`). HQ refuses any other zcp
service of the project `409 not_this_projects_mate` and never revokes the Mate's credential for it;
a named service Zerops no longer has gives its place to the next. zcp records the refusal as its
outcome (C-7), says it once, and stops enrolling until it starts anew (`hq.Keep`). An older zcp
that names no service is enrolled as before, unless it would revoke the credential of the Mate the
record names. The client refuses *Set up Mate* and *Finish setup* on a project holding several zcp
services, naming them, and a Mate's tier that declares one; inside a Mate's container
`zerops_import` refuses a document that declares a `zcp@` service, naming it, and imports nothing.

Until 2026-10-02 a Mate's Gitea bot token reached its container through the org's broker, as
`GITEA_URL`, `MATE_BROKER_URL` and `GITEA_TOKEN` on its `zcp` service (D20). zcp reads none of them
now; `GITEA_TOKEN` stays masked on every value dump and out of every recipe tier while a container
still carries one.

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
| MZ-1 | The imported zone equals the tree recorded in `imported.lock` for the recorded upstream commit; CI fails on any drift. `scripts/imported-lock.test.ts`; `node scripts/imported-lock.ts --check`. |
| MZ-2 | The ported zone imports nothing named `zerops`; `apps/server/src/zerops/**` imports no provider internals; `textGeneration/**` and `usage/**` reach providers only through `spi/**` and the sanctioned service tags. `scripts/mate-zone-architecture.test.ts`. |
| MZ-3 | The Zerops lifecycle feed consumes the SPI bus, not `ProviderService`; the bus is lossless while subscribed (unbounded fan-out, fresh subscription per subscriber, no replay before subscription). `apps/server/src/spi/ProviderRuntimeEventBus.test.ts`; `ZeropsLifecycle.test.ts` layer test. |
| MZ-4 | Every driver has a golden: a recorded (Claude, Codex) or scripted (Cursor, Grok, Antigravity, OpenCode) stream replayed through the real adapter must normalize to the checked-in expected events; the Claude envelope golden carries both StateEnvelope wire carriers. `apps/server/src/spi/replay/goldens.test.ts`. |
| MZ-5 | The fork's version line is its own (`0.1.x`), the model manifest is refreshed from the fork's `main`, and CI is the fork's `ci.yml` alone. `apps/server/package.json`; `ModelManifest.test.ts`; `.github/workflows/`. |
| MZ-6 | The manifest carries the **complete Claude model catalog** (models, aliases, status, badge, capability profiles, per-model CLI version bounds), not just a current/legacy overlay. Since its URL is fork-controlled, a new Claude model on an existing profile is a JSON commit to the fork's `main` — **no mate release and no `PinnedVersion`/`PinnedSHA256` bump in zcp**. Codex still discovers its models from its app server. `ModelManifest.ts`; `ClaudeModelCatalog.test.ts`; the fork's `docs/internals/model-manifest.md`. |

## 8. Agent authorization (S7)

A Zerops user with a Claude or ChatGPT subscription signs the agent CLI in **from inside mate**; nothing
credential-shaped enters a thread, a feed or the ledger. Two halves: the **agent-auth feed** (what the
container knows about each agent's login) and the **login session** (how the user gets there).

**Agents Mate signs nobody in to** (2026-10-03, the owner: "all of them should still work with everything
we've been doing"). Cursor, OpenCode, Grok and Antigravity sign in through their own CLI or the Zerops GUI,
never through mate, and stay outside the feed. A Mate whose only ready agent is one of them works like any
other: one shared readiness test — installed, enabled, available, ready, signed in (`authenticated`), at least
one model — lifts the sign-in screen in its conversation and Crew tab, its stand-up starts on that instance
(the asker's own Claude Code or Codex sign-in first, the conversation's own instance next), and its first
browser to find the agent ready records which driver runs. In the upstream Gitea backbone that was
the project tag `mate:runs:<driver>`, with its maker (`mate:by:`, else `mate:standup:`)
as owner. In the rebuild a project tag proves no owner: HQ holds the maker and stand-up
records (§10.6, §10.10), while the Mate holds Claude Code and Codex signers (§10.5).
The shared readiness test still applies to every agent. A crew runs on an agent whose adapter carries
the thread profile (Claude Code, Codex, Grok, OpenCode); Cursor and Antigravity are refused until their gate
is seen live, since zcp pre-approves the Zerops tools in Cursor's project config. zcp gives OpenCode the
`zerops` MCP server in `~/.config/opencode/opencode.json`, adding `zerops_*` as allowed
only where it does not overrule the user's permission rules (`TestOpenCode_*`).

### 8.1 The agent-auth feed

`subscribeZeropsAgentAuth` (stream, snapshot-typed) publishes, per agent (`claude-code`, `codex`;
Google Antigravity, offered since the 2026-09-05 intake, is **not** in the feed — it signs in through
upstream's own flow, the Google URL in the settings provider setup and a pasted callback forwarded
from inside the container; adding it here and to the flag writer is a separate slice, its MCP attachment
is `z3` `questions.md` Q-14):
`credPresent` (the credential artifact exists — `~/.claude/.credentials.json`, `~/.codex/auth.json`;
presence only, never contents), `flagOAuth` / `flagToken` (the platform flags `ZCP_AGENT_OAUTH_<S>`,
`ZCP_AGENT_TOKEN_<S>` read from the zembed env store), `state` (the welcome panel's five-value
matrix, `spec-welcome-mode.md §3`, verbatim), `providerAuth` (`authenticated | unauthenticated |
unknown`) and, while a login runs, `login` (§8.2). The server watches the credential FILE (parent
directory filtered by basename; a missing `~/.claude`/`~/.codex` is watched for via `$HOME`) and the
env store; events coalesce (~1 s, single-flight per agent).

**Presence is not authentication.** On a credential event the server verifies with the CLI itself —
`claude auth status` (JSON `loggedIn`) / `codex login status` — and only a fresh `authenticated`
result writes the flag. The mate server writes it itself, on its own zcp service, through the Zerops
API with its own key (`GET /service-stack/{serviceId}/env`, `POST /service-stack/{serviceId}/user-data`,
`DELETE /user-data/{id}`; the hardened key may, measured 2026-09-22): absent → create, a
non-sensitive `true` → nothing, any other value or a sensitive row → replaced. The flag is written
non-sensitive because the GUI's flag read path redacts sensitive entries (`spec-welcome-mode.md §4.2`).
zcp's `agent mark-oauth` keeps the same rule for the VS Code panel; mate no longer spawns it. A probe
that started before a sign-out never writes the flag (a per-agent epoch), and a failed write
re-queues the check with a backoff instead of waiting for the next credential event.
Upstream's provider probe is NOT the gate: it reports Claude as authenticated from `~/.claude.json`'s
account even after logout.

**The flag decides what is signed in, for every surface** — as in the Zerops GUI and the VS Code panel,
which read nothing else. One classification (`@t3tools/shared/zeropsAgentAuth`) answers for the rows,
the band, the empty conversation and the model picker: `authorized` / `authorized-token` → signed in
the moment the flag lands, whatever the CLI check has or has not answered; a definite
`unauthenticated` from the CLI under a set flag → "sign in again"; `local-only` → registering (the
flag not written yet); `reconnect` → sign in again (a rebuild left no credential); `not-authorized`.
The server overlays it onto every provider list it sends (`server.getConfig`, the config stream —
driven by the agent-auth feed too —, `server.refreshProviders`, `server.updateProvider`): on a set flag
the agent's default instance is `ready`; otherwise its message says why. The client then decides, per
viewer, whether they can run each agent (`@t3tools/client-runtime/zerops/agentAvailability`, case for
case the server's `turnRefusal`): the composer selects only an agent the viewer can run — with none
there is no selection and no send, a draft stays; the picker shows an agent the viewer cannot run
with its sign-in (or "Use my account" when someone else's login it is) in place of its models, from
a session locked to another agent too. The server refuses a turn on an agent that is not signed in,
before D6 asks whose login it is (§10.5). Models, version and usage stay the driver's. The driver's probe re-runs on its own
interval only (5 min, skipped while no client is in the foreground — a Codex sign-in stayed "not
authenticated" in the picker for 7 min, 2026-09-22), and Codex lists models only once signed in, so
every CHANGE of a verified status is also handed to the registry (`spi/providerInstances.ts`,
`reconcileAgentAuth`): a definite, contradicting `auth.status` on the instance is re-probed at once.
A snapshot still `unknown` (its own startup probe pending) is left alone. An explicit Claude refresh
drops the driver's cached capabilities probe (`CAPABILITIES_PROBE_TTL`) first, since the snapshot's
auth comes from it. The registry's answer never flows back into this feed. The mark latch resets
when the flag disappears from the env store or a sign-out invalidates it. Verification results and spawns are logged.

### 8.2 The login session

The client never types into a terminal. `zerops.agentLogin.start {agentId, threadId}` opens a
terminal named for the agent, writes the login command (`claude /login`; `codex login --device-auth`
— plain `codex login` opens a `localhost:1455` callback the user's browser cannot reach), attaches to
the PTY stream and runs the pure output parser ported from the Zerops GUI walker: chunk-boundary-safe
URL anchors, OSC 8, DEC graphics, paste/success/failure patterns, Y/N confirm, and a stall timer that
presses Enter through any unrecognized screen (Claude's login-method menu). The parsed prompt rides
the feed as `login.phase` (`starting | menu | awaiting-browser | awaiting-code | succeeded | failed |
verifying-code | cancelled`) with `url`, `code` (Codex's device code), `message`, `terminalId` and
`startedBy` (the Zerops user id of the session that started it, from the grant — never from input;
optional on the wire: a Mate keeps its installed version while the hosted client moves on, and a
required field an older server does not send fails the whole snapshot).
The card renders the URL as an "Open sign-in link" action (+ copy link / copy code).

Claude's code comes back through a field, as in the Zerops GUI dialog: `zerops.agentLogin.submitCode
{agentId, code}` (scope `terminal:operate`, like start/cancel; offered where the descriptor's
`capabilities.agentLoginCode` is true — without it the dialog asks for the code in the terminal, as
before) types the code into the login terminal
and, 100 ms later, Enter — the GUI measured that an Enter in the same chunk can be dropped, and Claude's
prompt takes one chunk as a paste. It is accepted only while a paste-code login sits at its prompt
(`awaiting-browser` or `awaiting-code`: Claude 2.1.278 prints "Paste code here if prompted >" right
under the URL, measured live 2026-09-22), and only as one run of printable ASCII — a control character
or a line break could run something else in that terminal. The phase becomes `verifying-code` until the
CLI answers: "Login successful" → `succeeded`; a wrong code prints "OAuth error: Request failed with
status code 400 / Press Enter to retry" → `failed`. Pasting into the terminal pane still works.

Every `start` opens a fresh PTY (the previous login terminal is closed with its history): a CLI left
at "Press Enter to retry" would otherwise take the next login command as its code. Success re-runs the
verification of §8.1. One session per agent; `zerops.agentLogin.cancel` sends Ctrl-C and closes the
terminal. A dialog opened after a login ended shows no trace of it: only an attempt started from that
opening counts.

`start` over a live login is how an account is switched or taken over: the CLI shows a first login's
screens, and a cancelled one leaves the old login untouched (measured 2026-09-22). A running turn
keeps the token it holds after the credential file is replaced and completes (measured), so a
takeover stops nothing.

`zerops.agentLogin.signOut {agentId}` (scope `terminal:operate`; offered where
`capabilities.agentSignOut` is true) ends a login for any client: a token-authorized agent is refused
(the token belongs to the project); then the login session is cancelled, that agent's live sessions
are stopped (`thread.session.stop` for every thread whose session runs on it, waited for up to 10 s —
a running turn would otherwise go on with the token it holds), the CLI logs out (`claude auth
logout` / `codex logout`, removing the credential file if it remains), the flag is deleted (with
`ZCP_AGENT_AUTH_TYPE_<S>` when it says `oauth`) and the feed re-checks. Every step after the token
check is best-effort. Threads stay. Who signed it in is not touched: the server lets that record go
once the credential's absence lasts (§10.5), a record without a credential speaks for nobody, and
the next sign-in replaces it.

The Zerops panel's agents card stays whenever the feed is available: per agent who signed it in and
the actions — Switch account and Sign out for one's own login, Use my account and Sign out for
someone else's, none for a project token.

The signer record (D6) is the server's own: the login walker keeps the person in `startedBy` the
moment a sign-in it walked succeeds, before anything else hears of the success (§10.5). No client
writes it, and the feed's `authorizedBy` and the logins' `signedInBy` are read from it. A finished
login never overrides the
verified status in a row: the login's recheck resets the agent's `providerAuth` to `unknown`, a
`succeeded` login shows "Confirming" until the check answers and the verified status after, and
`failed` steps aside once that says signed in. A credential whose check answered `unknown` is checked
again after 15 s, then 1 min, then every 5 min, instead of sitting at "Checking…".

### 8.3 Threat model
- The agent process and every project member share the container home: a credential file is
  project-wide. S7 does not change that; it is the platform's one-zcp-per-project model.
- The authorization code crosses the wire once, in `zerops.agentLogin.submitCode` — the same
  exposure as the terminal-write RPC it replaces, under the same scope — and goes only into the PTY:
  never a thread, the feed (which carries the URL the user must open, never the code they type
  back), a span attribute, a log, or the ledger.
- A planted or stale credential file cannot flip the platform flag: the flag is written only after
  the CLI's own status says logged in.

### Invariants

| ID | Invariant |
|---|---|
| MA-1 | The feed's `state` equals the welcome panel's matrix for every combination of flag/credential; credential files are probed for presence only. `ZeropsAgentAuth.test.ts` (matrix table). |
| MA-2 | The flag is written only after a fresh `authenticated` verification, once per credential appearance; `unauthenticated`/`unknown` never write; a burst of file events coalesces into one verification; a probe older than a sign-out never writes; a failed write is retried. `ZeropsAgentAuthIo.test.ts`, `ZeropsAgentAuthVerify.test.ts`. |
| MA-3 | The OAuth flag is written non-sensitive and a legacy sensitive row is replaced (`migrated:true`); sign-out deletes it and an `oauth` auth-type row, never a token's. `ZeropsAgentFlag.test.ts` (`planMarkSignedIn`, `planClearSignedIn`). |
| MA-4 | The agent-auth feed verifies through the agent CLI's own status command and the platform flag, never through the registry's probe; it reaches the registry only through `spi/providerInstances.ts`, to re-probe a picker snapshot that contradicts a changed verified status. `ZeropsAgentAuth.test.ts` — "verification spawns only the CLI status command"; `ZeropsAgentAuthIo.test.ts` — "hands every CHANGE of the verified status to the model picker's reconcile"; `spi/providerInstances.test.ts`; `scripts/mate-zone-architecture.test.ts` (no `provider/**` import from `apps/server/src/zerops/**`). |
| MA-5 | The login walker turns the CLI's output into `login` phases with `url`/`code` from the recorded lines (Codex device URL + code; Claude menu → oauth URL), and cancel ends the session. `zeropsAgentLoginWalker.test.ts`, `zeropsAgentLoginOutputParser.test.ts`, `ZeropsAgentLogin.test.ts`. |
| MA-13 | Live: moving the credential aside flips the feed within ~0.5 s and `providerAuth` to `unauthenticated`; restoring it returns `authorized`/`authenticated`; the public `/mate/` renders the hosted-static landing. `verified.md` S7-3 + follow-up rows. |
| MA-8 | `submitCode` types the code then a separate Enter, only into a paste-code login at its prompt, and the code never reaches the published state. `ZeropsAgentLogin.test.ts` — "submitCode types the code, then Enter…", "…is refused when no login waits for a code", "the code never reaches the published login state". |
| MA-9 | The signer is the person whose session started the sign-in this server walked to success, kept before anything else hears of it and across a restart; a credential's absence lets it go only once it lasts; a finished login never overrides the verified status in a row. `ZeropsAgentLogin.test.ts` — "keeps who signed in last, and lets it go with the credential", "a credential absent for one reading, then there again: its sign-in is kept", "a sign-in made while its credential reads absent outlives that absence"; `zeropsSignIns.test.ts`; `agentLogin.test.ts` (`classifyAgentRowLogin`). |
| MA-11 | Sign-out refuses a token agent, then cancels the login, stops that agent's live sessions, logs the CLI out, deletes the flag and re-checks, in that order, best-effort. `ZeropsAgentSignOut.test.ts` (`threadsToStopForAgent`, call order). |
| MA-12 | A turn starts only on an agent that is signed in, then only for its signer (D6); the client offers exactly the turns the server accepts. `ZeropsProjectSigners.test.ts` (`turnRefusal`); `agentAvailability.test.ts` (mirrors its rows). |
| MA-10 | The platform flag decides signed-in for every surface; the CLI check only refines a set flag to "sign in again"; every provider list the server sends carries that answer. `packages/shared/src/zeropsAgentAuth.test.ts`; `zeropsAgentProviderOverlay.test.ts`; `agentLogin.test.ts` (`agentAuthLabel / agentAuthAction`). |

Open (kept in the S7 plan until they land): the mobile card + parsed prompts on the phone (S7-4) and
the `setup-token` path (S7-5, a second zcp verb).

## 9. Clients on Zerops only (S5)

### 9.1 Desktop — a hosted client in an Electron shell
The desktop app ships the web bundle built in hosted-static mode (`VITE_HOSTED_APP_CHANNEL` ∈
{`latest`, `nightly`} — the only values the client accepts; `VITE_HTTP_URL`/`VITE_WS_URL` empty) and
serves it from `resources/web` through the `t3code://` protocol handler (SPA fallback, traversal
guard); the window opens unconditionally. The local backend, WSL, SSH launch, Clerk, the server
sidecar, path-returning dialogs, the network-exposure/QR endpoint picker, the preview webview and its
`preview_*` MCP toolkit are gone (§0 rule 3: the only browser is zcp's; §5, Browser surface); keychain,
dialogs, updater (GitHub target `krls2020/z3` by default) and window/menu/theme stay.
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
| MK-1 | The desktop spawns no backend; the bundle is served from disk with the hosted-static gate short-circuiting the primary-environment resolution. `apps/desktop` tests (417), the Electron CDP proof in `verified.md` S5-1. |
| MK-2 | The relay verifies a Zerops token and binds a link to a project + origin; a proof without them, a non-member, or a foreign origin is refused. `infra/relay` `ZeropsAuth`/`ZeropsProjectBinding`/`EnvironmentLinker` tests (152). |
| MK-3 | The mate server's link proof carries `zeropsProjectId` + `endpointOrigin` in Zerops mode and refuses outside it; origin precedence env → request → refuse. `apps/server/src/cloud/http.test.ts`. |
| MK-4 | The APNs queue dedupes on `job_id`, leases exclusively, retries with backoff, dead-letters at five, recovers expired leases. `ApnsDeliveryJobStore.test.ts`, `ApnsDeliveryWorker.test.ts`. |
| MK-5 | No client calls a deleted relay group; `client-runtime`, web and mobile typecheck against `RelayLinkGroup` only. package typechecks; `linkEnvironment.test.ts` (mobile, web). |

Open (in the S5 plan): shared client logic into `client-runtime` (S5-2), the mobile Zerops session +
picker (S5-3), the relay deployment + client link trigger (S5-4b UI), server-side T3 Connect reach
deletion (S5-5).

## 10. The auth backbone — Zerops roles as the one source, HQ per org

**Since 2026-10-02 the org's HQ carries what an org's Gitea and its broker carried** (§10.1). Zerops
roles stay the one source (§10.3), a Mate's own door and its signer stay as they are (§10.4, §10.5),
and zcp reaches HQ with its enrollment (§6.6, §10.10). What HQ already holds: the structure
(applications and the projects attached to them), the repositories, a Mate's changes, their review,
merge and close, the recipe and its landing, an application's environments, their deploy tokens and
the stages' deploys (§10.8). What moves to it next: production and the release, and the client
surfaces that still read Gitea (§10.11). The rest of this section's history follows.

Landed 2026-09-16 to 2026-09-17 as mate 0.11.0–0.11.5, zcp v9.176.0 and gitea-mate v1–v2.1. This
section records the decisions as they landed and the invariants that hold them; where each slice
stands is the fork's `docs/internals/zerops/primer.md`, the measurements are the fork's ledger
(2026-09-15 to 2026-09-17), and the names, tags, variables and HTTP contracts the three codebases
share are `gitea-mate/docs/` (`broker-api.md`, `vocabulary.md`, `roles.md`, `group-repo.md`) —
nothing here renames what those decide.

### 10.1 The shape

**Since 2026-10-02:** an org that uses Mate has one **HQ project** — Core, its Postgres and a volume
for the bare repositories — set up by an admin or owner of the org (_Set up HQ_ on the projects
page). HQ's address is the project's own domain; it is known as the org's official HQ by an anchor,
an org `ADMIN` integration token named `mate-hq:<project id>:<address>` whose value is dropped, which
every member, a Mate's key included, finds in the org's member list (`internal/hq/official.go`). An
app is an **application** in HQ's structure, and a project is attached to it as a Mate, a stage or a
production. A Mate's zcp enrolls with the official HQ without handing over its key — HQ's challenge
written into the Mate project's env and read back by HQ — and gets a Mate credential for HQ's API and
git (§6.6); a person comes in through HQ's door with a throwaway (§10.4). An application's
repositories, its recipe repository `group` among them, a Mate's changes and their merge are HQ's
(§10.10).

**Until 2026-10-02:** an org that uses Mate gets one **Gitea project** — `web` (Gitea), `db`, `volume` and the
**broker**, plus one runner service per group — made by the app with the org's first _New project_
(§4.7). An app is a **group**: an entry in the registry (§10.6), never a Zerops project of its own.
A person works in a **Mate**: a project with a `zcp` container running this server and a coding
agent, one dev/stage pair per codebase (D12). Gitea holds a **group repo** per group — the recipe
as the published tier layout, `environments.yaml`, release tags — and one repository per codebase.
A group's stages and its production are projects of their own, created from the recipe's tiers,
that only the broker deploys to.

### 10.2 Decisions, as landed

| Id  | Decision                                                                                                                                                                                                                                                                                                                      |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | A Mate learns who you are from a **throwaway token per session** (§10.4; a session outlives the load, D33); Gitea from the broker's OIDC provider fed the same way (§10.9). A person's Zerops token reaches the Zerops API and nothing else. Holds in HQ: HQ learns who a person is the same way, through its own door (§10.4); the Gitea sign-in is retired with the Gitea. |
| D3  | Group membership is authoritative in **tags on the org's Gitea project**, which only `OWNER`/`ADMIN` can write — the platform enforces it. Superseded 2026-10-02: HQ's structure holds which projects an application's Mates, stages and productions are (§10.1, §10.6). |
| D4  | The broker is **its own repository and Go module**, `zeropsio/gitea-mate`, with its own Zerops client; the plan's `zcp gitea broker` was not built. The recipe composer lives in zcp and runs inside a Mate (§10.10). Superseded 2026-10-02: the broker is retired; HQ's Core holds what it did, and the recipe composer proposes to HQ (§10.10). |
| D5  | A `READ_ONLY` person **sees every Mate listed and opens none**: the door refuses with `zerops_read_only`. Opening needs `BASIC_USER` or above on the Mate's project.                                                                                                                                                             |
| D6  | **Only the person who signed an agent in runs it** (§10.5), recorded by the Mate's server as the person whose session started the sign-in it saw succeed; no backward compatibility for unrecorded logins.                                                                                                                       |
| D8  | A Mate releases only while its group's **release switch** is on — a registry flag, owner-only, off by default; the broker checks it for a tag a bot pushed. Superseded 2026-10-02: the broker that checked it is retired; release moves to HQ next. |
| D10 | Gitea site admin = org `OWNER` only; `ADMIN`s get teams by role. Retired with the Gitea on 2026-10-02. |
| D11 | The creator of a Mate is its project's `OWNER`; org owners and admins keep their reach; _Assign_ (a project-role override to `OWNER`) hands a Mate over.                                                                                                                                                                       |
| D12 | Every Mate keeps its own dev/stage pair; group stages are projects of the group's.                                                                                                                                                                                                                                              |
| D13 | The recipe lives in the group repo (in HQ since 2026-10-02: the application's recipe repository, `group`, §10.10) as the published layout (`0 — AI Agent`, `3 — Stage`, `4 — Small Production`, each a whole-project `import.yaml`), never in a code repository; `main` is protected to the group's releasers.                                                                                                |
| D15 | **The broker holds the only deploy key** and deploys only what protected state allows; workflows orchestrate by calling it. No deploy secret in Gitea or a repository. **Changed by D27:** the broker still decides and still holds every key, but it no longer deploys — a job does, with the environment's key in its memory for one `zcli push`. Superseded 2026-10-02: the broker is retired; HQ deploys an application's stages itself, from its own archive of a commit, with the deploy token it keeps (§10.8); production moves to HQ next. |
| D16 | Any number of stages per group, each fed by a branch or a mix of branches merged into `env/{name}` by the broker; the default follows `main`; production's source is `release`. Superseded 2026-10-02: the broker's `env/{name}` merges are retired; an environment is HQ's record, a stage following `main` (§10.8). |
| D17 | **No brief.** _New project_ asks for the name and nothing else (the owner, 2026-09-16); a creation's generated hand-off is composed into the composer and never sent by itself.                                                                                                                                                 |
| D18 | Gitea is made **with the org's first _New project_**, in the same run as the first Mate; nothing happens at sign-up and there is no pool. Superseded 2026-10-02: the org's HQ takes the Gitea project's place, set up by an admin or owner (§10.1). |
| D19 | The account's Gitea is the only forge Mate drives. Superseded 2026-10-02: the org's HQ is where a Mate delivers (§10.10); a remote of the user's own stays theirs. |
| D20 | **A Mate's Gitea access is delivered by the broker's rights loop** into its `zcp` service's variables; the app grants the broker the Mate's project at registration and asks for nothing. Superseded 2026-10-02: a Mate reaches HQ with its enrollment, and zcp reads none of the variables (§6.6).                                                                                                                                 |
| D21 | **A person signed in to Mate is signed in to Gitea** (2026-09-17, the owner: "it should use the same login I have"). The app proves the person to the broker with a throwaway, as at the door, and the broker — Gitea's site admin — makes their account exist, bound to the OIDC source, and mints a token that acts as them (§10.9). No Gitea screen, no button; the OAuth2 client, the PKCE flow and the callback route are gone. Retired with the Gitea on 2026-10-02: a person reaches HQ through its door (§10.4). |
| D22 | **A Gitea serves every app origin** (2026-09-17, the owner: "didn't you just make it use Mate's logged-in user's token?"). Under D21 every browser call carries a bearer in a header and no cookie, so an origin allowlist proves nothing and only pinned a Gitea to the origin that made it. Gitea's `[cors]` and the broker's `POST /person/token` answer `*` (credentials off); the import sends no origin list; one Gitea serves mate.zerops.io, a developer's localhost and the shells. Gitea's own-page sign-in is untouched, its consent page on the origin that made the Gitea (`MATE_APP_URL`, a redirect target). Retired with the Gitea on 2026-10-02. |
| D23 | **The group repo takes merges from anyone with write, and a Mate's recipe proposal lands by itself** (2026-09-17, the owner, on a first recipe that sat as PR #1 waiting for a releaser: "they all should be able to merge on the import yaml repo"). `main` on the group repo keeps no merge whitelist — the `write` and `release` teams merge — and the broker merges a pull request a registered Mate's bot opened against it on the next pass, nudged by the hook that announces it. What sets the releasers apart is the `v*` tag protection; a person's pull request stays theirs to merge. **Narrowed by D30:** the broker lands a Mate's proposal by itself only when it only adds files, and a Mate proposes only the tiers `main` lacks. **Narrowed by D31:** every registered Mate writes the group repo too, so a person's pull request, like a Mate's change to a file `main` carries, is merged by a person with write or by a Mate its person asks. Superseded 2026-10-02: in HQ a person merges a change, and Core lands a Mate's recipe change by itself when it only adds files and closes one that adds nothing (§10.10). |
| D24 | **A group's Mates share its service repositories** (2026-09-17, the owner asking for a run that ends with two Mates, a stage and a production, all wired). The AI Agent tier's `buildFromGit` names the same repository for every Mate the recipe creates, and the broker's `POST /mate/repository` answered `409 taken` to every bot but the one that made it, so no second Mate could push. A registered Mate of the group asking for a service repository that exists is made a collaborator with write — its own branch, its own pull requests, `main` behind them; the group repository stays refused by name, made yet or not. An owner's _Add Mate_ registers the Mate at birth, as _New project_ does. **Narrowed by D31:** the refusal stands but no longer keeps a Mate off the group repository — every registered Mate writes it as a collaborator the rights loop makes, and `POST /mate/repository` only refuses to make or join it as a service repository. Holds in HQ: every Mate of an application reaches all of its repositories, each pushing only its own change's branch (§10.10). |
| D26 | **The Git tab is the Mate's; the project's flow is the left menu's and the projects screen's; Gitea's overview is the footer's** (2026-09-17, the owner: "this seems like git for the whole project, shouldn't it be git for this Mate and have project git somewhere else … the left menu … a list of open PRs of each Mate between mates and the stage/prod", and earlier "at the bar down I imagine a 'gitea' button, where I'll see overview of all repos I have access to and their open PR; in the menu I imagine each group as a timeline: mates, their open PRs, stage, production"). One provider reads every project's flow for the account; a Mate's tab shows its own branch and pull request and nothing of the project's. Since 2026-10-02 the Git tab reads the Mate's change from HQ, and the footer's Gitea overview is the account menu's Git page (`/git`), every application's repositories and open changes as HQ holds them (§10.11). |
| D27 | **A job deploys, with `zcli push`; the broker decides and hands over the key** (2026-09-18, the owner reading a tier's `buildFromGit` and then the broker's own upload: "the gitea runner should literally just do zcli push, the whole process must be as standard as possible"). Every deploy of a group environment is a job of the service repository's workflow on the group's runner: it checks the commit out and runs `zcli push` with the tier's setup, so the build's log is the job's log and nothing re-implements zcli. The broker keeps deciding: a push to `main` starts the job by itself; a release, a new environment and the catch-up pass dispatch it (`workflow_dispatch`); the job asks `POST /deploy/grant`, and the broker hands it the environment's **deploy token** only when the job is proved, runs the default branch's workflow, holds exactly the commit protected state wants there, and sits on a runner that has run nothing but such jobs since it was made (§10.8). The token is one per environment — `BASIC_USER` on that project and nothing else — minted by the app as the person who adds the environment and kept as a secret variable on the broker's service: a token cannot mint a token (ledger 2026-09-15), so it cannot be one per job. Production is built from the release's commits; promoting the stage's artifact is gone. Supersedes the second half of D15 and rewrites MB-14. Superseded 2026-10-02: the broker and its jobs are retired; HQ deploys a stage itself, with the environment's deploy token, which no job ever holds (§10.8); production moves to HQ next. |
| D28 | **A release lists what is merged, and a stage is never what it waits on** (2026-09-18, the owner asking for a second project — "this time we can have just one mate and one prod" — and then, watching Release do nothing while a stage deployed: "I hope that even with stage prod release is not tied to stage in any way"). The candidate is each production service's repository at its default branch, whether or not the group has a stage: a group may be Mates and a production with nothing in between, and one that has a stage has it as a place that runs `main` too, not a gate the tag waits behind. The person's merge is the review, the tag is still the approval, and the broker still deploys only what the tag lists. Holding production until a stage has the commit is said once and explicitly, as `requireOnStage`. The offer shows what pressing it would carry — the commits `main` has that the production is not running, which with squash merges is one line per task. Release moves to HQ next; the decision stands. |
| D29 | **The UI is built around the flow** (2026-09-23, the owner, after a walk of the projects page as KRLS — 9 of 10 groups said "Nothing needs you here.", the only two things that needed the person, a merge and a release, sat ~4000 px down, the menu and the page disagreed about what one production ran, and "put it on production" sent the Mate into launch-production: "these are extremely important findings the whole UI should be built around"). Every surface draws a project in the one order its code travels — **Mates (each with its preview) → pull requests → `main` → production** — and a group stage as an optional side branch of `main`, drawn only where one exists and never as an empty slot. **Preview** is the stage half of a Mate's own dev/stage pair (§10.10), the one place a change runs before its pull request; **stage** is a group stage project and nothing else. A project has **one next step**, derived by the client and drawn by every surface, never decided by one. The environments and the releases are the person's, on the projects page and — since 2026-09-29 — in the left menu's production chip, whose _Review_ opens the release's review; the Mate's part ends at the pull request and the recipe, and the copy says so where the verb is ("Production is added here, not by the Mate."). A Mate's conversation offers the next step from the project's flow, not from the agent's text, so "dej to na produkci" gets the right button even while the agent's answer is wrong; zcp's launch-production refuses in a wired Mate, so the agent's answer is right too (§10.10). §5.4, §10.11. |
| D30 | **A Mate's recipe proposal only adds** (2026-09-26: the medusa group's second Mate, which had joined the group's existing repositories, proposed its own composition of every tier over the hand-written ones — project env, secrets and the storage policy dropped, a `mailpit` added, production naming the dev setups — the broker merged it, and the next release built production's storefront with the dev setup, a 502; the first Mate had done the same the day before). Every Mate composes the whole app from its own project, so a proposal over a tier `main` carries is one Mate's view replacing the group's. zcp proposes only the tiers `main` lacks, from a branch cut at `main`'s tip, so its pull request only ever adds files, and closes its bot's proposals once `main` has every tier; the broker merges a Mate's proposal by itself only when every file it changes is added, and one that modifies, removes or renames a file `main` carries waits for a person with write. A tier on `main` changes only through a person's pull request. §10.10. **Narrowed by D31:** a tier on `main` still changes only through a pull request, and a registered Mate may open one and merge it when its person asks; the automation stays additive and withdraws only its own proposals. Holds in HQ: zcp proposes only the tiers `main` lacks as the Mate's change, which only adds, and Core lands it (§10.10). |
| D31 | **Every registered Mate writes its group repo** (2026-09-30, the owner, after a person asked their Mate to add a service to two tiers and merge the group's open pull requests, and the Mate could open the pull request and not merge it: "mate should have write right to groups" · "the first mate ever has to stand up the group, every new one should be able to change it as well"). The broker's rights loop makes every registered Mate's bot a collaborator with `write` on `{slug}/group`, a planned action of every pass: a Mate registered before the rule gains it on its next pass with no token minted, a new one on the pass that makes its bot; a bot whose Mate leaves the group's registry loses it, as it loses its `read` team, and a deleted Mate's bot is retired and keeps it, as it keeps its team. `main` keeps its protection exactly — no direct push from anyone, merges from anyone with write, no merge whitelist; `env/*` stays the broker's and `v*` tags the release team's. So a Mate still changes the recipe only through a pull request, now one it may merge itself when its person asks, and its merge does what a person's does: the services a changed tier adds are imported into every environment built from that tier, with no production guard. The automation stays as D30 has it: zcp proposes only the tiers `main` lacks, from the bot's fork, and withdraws only the proposals it opened, never a pull request the Mate opened itself; the broker lands a Mate's pull request by itself only when it only adds files. `POST /mate/repository` still refuses `group`: the write is the rights loop's, never an answer there. §10.8, §10.10. Superseded 2026-10-02: in HQ a Mate writes only its own change; it proposes the recipe through it, and a change to a tier `main` carries is a person's to merge. |
| D32 | **A new Mate stands up from its recipe in one zcp call** (2026-09-30, the owner confirming how a Mate is added, after the Beviro trial: without it the model improvised an adopt and four deploys one after another — mailpit 1:55, medusadev 5:49, nextstoredev 4:39 — could not place the stage half, and took sixteen minutes). The person's browser builds the project from the group's AI Agent tier, the Mate reaches HQ with its enrollment (§6.6), and the first turn's one call, `zerops_standup`, adopts every pair, checks `main` out onto the Mate's branch, deploys every dev half at once and answers once they stand, then, on the model's second call, each stage after its dev half and the stages above it by the tier's priority, and reports each service with the model's next step. It delivers nothing — no commit, push or pull request — and touches the Mate's own project only. The model is the backup: whatever fails, it carries on in the same turn with zcp's own tools. §10.10. |
| D33 | **A Mate's session outlives the load** (2026-10-02, the owner, on the per-load throwaway: it "feels like it's making the system brittle"). The session a throwaway opened is kept per account in the browser and presented again on a later load only where the door would present a fresh one — the descriptor names the expected project and the environment it was kept for — and only once the Mate answers `GET /api/auth/session` with `authenticated` and every scope the client asks for; a kept session spends no mint and waits on no mint pace. Every way out of a login ends the kept sessions at their Mates: the account's close, a stored login the platform refuses, and a session displaced from the store. With reconnects free, every ready Mate in the menu connects on its own (a bound of 48). Why: memory-only sessions (2026-09-07) were free while the door took the person's own token; once it took a throwaway every load minted and deleted one per Mate, waited on the mint pace and the Zerops API, and left each dropped session live on its Mate for a day — while the same storage keeps the Zerops token that can open every Mate. |
| D34 | **A group recipe gives a search engine room to reindex, and keeps what a Mate learns** (2026-10-02, the owner: "while creating the recipe it should think about minimal viable resources (but not go overboard either)"). zcp writes Meilisearch, Elasticsearch and Typesense at no less than 2 GB `minRam` with 0.5 GB `minFreeRamGB` on every tier, each the max of the live value and the floor; every other service keeps the numbers it runs with. The recipe carries the free-memory buffer and a managed service's `profileOverrides` (Valkey's `maxmemory-policy`). A scale change or import override that leaves the recipe behind answers with each tier's old and new values, and `group-recipe scaling=<host>` proposes only that host's `verticalAutoscaling` into the tiers on `main`, for Review; a block it cannot rewrite safely is refused, never duplicated. Why: a new Mate's catalog import ran Meilisearch out of memory at the recipe's 1 GB, and the fix its agent made stopped at that one Mate. In HQ (2026-10-02): the proposal is the Mate's change in the recipe repository, titled `Mate: <host>'s scale in the group recipe`, for a person to merge; HQ keeps one open change per Mate per repository, so a scaling proposal and the additive one (D30) never write over each other — whichever finds the other open leaves it as it is and says so (§10.10). |
| —   | D2 (a relay), D7 (stage deploys), D9 (integration tokens at the door) and D14's build (adoption) are closed, superseded or not built; D14 stands as the design: adoption is the new-app flow with existing source, nothing adopted in place.                                                                                    |

### 10.3 The role function

One pure rule in two languages — `packages/shared/src/zeropsRoles.ts` in the fork,
`internal/roles` in gitea-mate — kept equal by `zeropsRoles.fixtures.json`, a byte-identical copy of
gitea-mate's `internal/roles/fixtures.json` that both suites replay whole. Inputs: the member list
(`clientUserList`), each project's `userRoles` overrides (either direction), the registry. Output:
`active` (only `ACTIVE` carries rights), `siteAdmin` (org `OWNER`), `canCreate`, per group
`read`/`write`/`release` (`write` = `BASIC_USER` or above on **any** of the group's projects, since
a Mate's creator owns only their Mate; `release` = `BASIC_USER` on production, or org `ADMIN`/`OWNER`
until production exists, because the recipe is merged before it), per Mate `open`/`listed`/`hidden`,
and the OIDC `groups` claim (`org:owner`, `g:{slug}:read|write|release`). Consumers: the app's list
and verbs, the door (§10.4), the broker's teams, admin flag and claims. A change to the rule is a
change to the fixture first, in both repositories. Since 2026-10-02 HQ's Core is a consumer too (who
may do what in HQ, `zeropsPermissions.ts`); gitea-mate's copy, the broker's teams and the OIDC
claims retire with the Gitea.

### 10.4 The door: `POST /api/auth/zerops-throwaway`

Replaces §3.2–3.3. In Zerops mode `bootstrapMethods` is `["zerops-throwaway"]` and nothing else
(`EnvironmentAuthPolicy.ts`). The client (`authorization/zerops.ts`, `doorThrowaway.ts`) mints an
integration token as the person — `roleCode: NO_ACCESS`, no project grants, no flags, named
`mate-door:{projectId}:{nonce}` — presents its value once, and deletes it whether the door admitted
or refused; on start it sweeps its stale `mate-door:*` and `gitea-signin:*` tokens. The server
(`ZeropsThrowawayIdentity.ts`) reads `/user/info` as the token for its id, reads the token's own
record from **this** org, and refuses anything but `NO_ACCESS`, empty `projects`, every flag false,
the name for this project, and a `created` within five minutes of the Zerops API's own `Date`
header — never the container's clock. The caller is `createdByUser`; with the Mate's own key the
server reads the member list and the project's `userRoles`, and the role function answers `open`
(the standard client scopes plus `exec:operate`), `listed` (`403 zerops_read_only`) or `hidden`
(`403 zerops_project_membership_required`). The flag check is load-bearing: a token minted through a
delegation names the delegating person, so a flagged token is never a throwaway, whoever made it.

**Who the project lets in, relayed (R6).** HQ relays Zerops's access, aged; a Mate trusts it at most
5 min from HQ's Zerops read, then reads Zerops itself. HQ reads the org's member list and its
projects' `userRoles` for its own roles anyway, and sends down each Mate's link `access`: whom the
Mate's project opens for and whom it lists, by the same role function over every row of the member
list (`@t3tools/shared/mateAccess`), and how long before it was sent Zerops answered that view —
whole after every view it reads, nothing for a project its view lacks, nothing past the link's
64 KiB frame bound (logged once per Mate) (`apps/hq/src/mateAccess.ts`). While the relay holds, the
door lets in a creator it opens for without reading the member list; whomever it lists or leaves
out, the Mate's own read decides, with the refusals above. With no relay that holds — before HQ
links, under an HQ older than the relay, in an org with no official HQ — the Mate reads the member
list itself, as it always did (`ZeropsProjectAccess.ts`).

**HQ's door** (since 2026-10-02) takes the same throwaway, named for HQ — `mate-door:{HQ project id}:{nonce}`
— at `POST /api/door` and answers a session for HQ's API; the client deletes the throwaway after. The
Gitea sign-in this section budgets (§10.9) retires with the Gitea.

**No membership window.** `ZeropsMembershipWatch` asks who the project lets in — HQ's relay while it
holds, else the member list and `userRoles` read with the Mate's key — on a timer and at once when
HQ relays a different answer, and ends the sessions whose answer changed; a pass that cannot say
keeps them while the last answer is under two intervals old, and ends them at once once a relay
lapsed, which held one interval already. Nothing renews (`credentialRenewal.ts` keeps the contract,
no client supplies a
Zerops credential); the client opens a new session with a fresh throwaway when it holds no kept session its Mate still confirms (D33). The
minimum server a client connects to is 0.11.0 (`serverCompatibility.ts`).

**Minting and deleting a throwaway** (from the fork's slice 0.7; its admission from slice 2.4).
A mint of `roleCode: NO_ACCESS` with `projects: []` and every flag false, here and for Gitea
(§10.9), is an `account-write`: the client mints one only once the sign-in's first access grant is
admitted, because nothing that exchanges or signs in to Gitea runs before it, and a verification
window that has closed since does not hold it up. Any mint that grants a project, or an organization
role above `NO_ACCESS`, stays a `project-write` and is refused while the window is closed. The
client checks no organization or project role for the throwaway — the door and the broker decide —
so a `BASIC_USER`, or a `READ_ONLY` member with a project override, mints like anyone else.

Why a closed window does not hold up the throwaway: the window guards against a person whose access
lapsed acting on the platform past it. A token with no organization role, no project grant and no
flag carries no rights, so minting one while project writes are closed cannot grant anything. What
it opens is decided when it is presented, by the door and the broker, which re-derive the person's
role from the member list and the project's `userRoles` with their own keys — a fresher check than
the client's window — and refuse a token that carries a grant or a flag. A mint that grants a
project would add authority, so it keeps the window.

The delete is `deleteThrowaway({clientId, tokenId, name}, {token})`, an
`account-write` call that refuses any name failing `isThrowawayName`, sends the minting token
explicitly, never clears or refreshes the session, and carries its own 15 s timeout; it runs in a
finalizer that neither the exchange's abort nor the account's close cancels, and retries once after
5 s. A `401` or `403` there leaves the token to the sweep and never reaches the session, so a
delete can neither run under another account nor sign anyone out. The sweep, under the same
principal, uses the same call; `deleteIntegrationToken` stays `project-write` for every other
caller (deploy tokens, grants). Deleting a throwaway only removes authority, and the name check
keeps real tokens off this path. Mints are budgeted per tab: 10 door exchanges a minute and a
separate 4 Gitea sign-ins a minute.

### 10.5 Who runs an agent (D6)

A Zerops project carries only the exact `mate` marker; every Mate metadata fact lives in Mate HQ,
keyed by project id. ZCP filters `project.tags` in create-and-import YAML at the API boundary,
retaining only that marker and reporting that dropped facts must be stored in HQ. Platform tag
transport decoding remains native.

When a sign-in the Mate's server walked succeeds, the server keeps who started it — the Zerops user
behind that door session — before anything else hears of the success, in `~/.mate/signed-in.json`
beside the logins' homes (`zeropsSignIns.ts`). The document is read once, at start: a restart keeps
the record, and a rewrite under a running server changes nothing. A Mate migrated from main, whose
signer facts have been ported to HQ and which has no document, gets it written from the enrolled
HQ's `GET /api/mate/self` signer map by `zcp service mate` before its server first starts — once,
never again, each sign-in dated by the seed (`mate.SeedSignIns`). Both the enrollment and HQ's
answer must name the container's project id. An existing document is preserved without asking HQ.
An absent HQ answer leaves the store untouched and logs the reason; the attempt marker keeps the
failure visible on later launches without retrying. To ask again explicitly, remove
`~/.zcp/state/mate-sign-ins-seeded` and start the Mate service again. HQ omits the signer map when
its ported record is empty. An API key login has no sign-in
to walk: the session that stores its key signs it in, and its stored key is its credential. That
record is the gate's (`ZeropsProjectSigners.ts`): the server refuses
`orchestration.dispatchCommand`'s turn-starting commands from any session but the signer's, holds a
turn while a code being checked would change the answer (up to 30 s), and signs the agent out when
its signer is no longer an `ACTIVE` member — a member list it cannot read signs nobody out. A credential that goes takes its record with it once
its absence lasts (`CREDENTIAL_GONE_AFTER`, 10 s), never on a moment's: a CLI that removes its
credential before it writes the new one reads absent for an instant. A login with no record —
signed in before the record existed, from a terminal, or copied in — runs for nobody until somebody
signs it in through Mate. Forging a record takes access to the container, and that access already
holds the login's credential. The Mate reports its signers to its HQ in its summary
(`MateSummary.signers`), and HQ hands them on to whoever may observe the Mate for the menu's
"signed in by". HQ also keeps the ported signer metadata in its project record for this one-time
seed; the server's sign-in store owns subsequent sign-ins. The card names the
signer in place of the composer for everyone else. Said out loud: the gate stops turns; everyone
who can open a Mate can copy its login file from the terminal. D6 keeps a colleague from running
someone else's agent by habit, and records who signed it in; it does not claim to stop theft.

### 10.6 The registry and groups

**Since 2026-10-02** HQ's structure is the registry: an application, the projects attached to it as
Mates, stages and productions, read and changed through HQ's API (`/api/structure`, `/api/apps`,
`/api/apps/{id}/projects`) by the people the role function lets. What follows is the Gitea-era
registry it replaces.

Tags on the org's Gitea project: `mate:gn:{groupId}:{slug}` names a group,
`mate:gm:{groupId}:{projectId}:{role}` a membership (`mate`, `stage`, `production`), plus the
group's release switch and `mate:leaving:{userId}` marks (`groupRegistry.ts`, gitea-mate
`internal/registry`; the exact spellings are `vocabulary.md`'s). Written with `PUT /project/{id}` —
`name`, `description`, `tagList`, `publicIpV4Shared`, `maxCreditLimit`, never `userRoles` — by an
`OWNER`/`ADMIN`, which is the whole access control. _New project_ writes the group and the first
Mate's membership at birth; registering a Mate then widens the broker's token in place with
`BASIC_USER` on the Mate's project (`brokerGrant.ts`; a failed grant after the registry write is
reported and has no retry yet). Group reach on a Mate's own token narrows by itself and widens only
on a person's action (`planGroupReach`, `useZeropsGroupReach`). Per-project tags (`mate`,
`mate:g:`, `mate:role:`, `mate:name:`, `mate:bot:`) stay display hints. Who signed an agent in,
who asked for a Mate's stand-up and whether its project is closed off are no tags — any member can
write a tag by API: the first is the Mate's own record (§10.5), the other two the Mate's record at
its HQ (§10.10). The budget is measured:
65 534 bytes of tag JSON per project, about 2 500 entries.

### 10.7 A Mate's project

The creation steps (`createEnvironment.ts`): `create-project` → `import-container` →
`secure-container-token` (the Mate's key lowered to `NO_ACCESS` at the org and `BASIC_USER` on its
own project) → `drop-container-delegation` (the one-use _can create projects_ delegation every
platform-made key carries, deleted) → `isolate-project-env` (`envIsolation: service`; `ZCP_API_KEY`
moved from the project onto the `zcp` service as a sensitive variable and deleted from the project;
`sshIsolation` untouched; every service restarted, `zcp` last — a running process keeps what it
captured at start) → `import-recipe` where a tier applies. The group-reach reconcile (`useZeropsGroupReach`) lowers
the key of any Mate from any page; the delegation and the isolation run only as creation steps,
which the wizard's one-call path (§4.7) skips — a Mate made by _New project_ keeps its delegation
and runs `envIsolation: none` with its key at project level (measured 2026-09-17; open in the
primer). `planProjectIsolation` also deletes a key outright from a stage or production project
that has no container to move it to. The Mate's server reads its
key from zcp's own env store, captured at first boot (ledger 2026-09-16).

### 10.8 HQ

**The project** (§10.1): Core, its Postgres and a volume for the bare repositories, at the
project's own domain. Only the leading Core answers; a standby, or a Zerops HQ cannot read, answers
`503` with `Retry-After`, which every caller takes for "try again". Core's API is JSON over HTTP;
its contracts are the fork's `@t3tools/shared` (`hqChanges`, `hqRecipe`, `mateLink`), and who may do
what is `zeropsPermissions.ts`'s `can` over the role function (§10.3).

**A Mate's side** (`Authorization: Mate <credential>`, §6.6): its enrollment (`POST
/api/mate/challenge`, `POST /api/mate/credential`), its own state (`GET /api/mate/self`: its record,
the application HQ holds it in and that application's name, its changes there); a repository of
its application, made or joined (`POST /api/mate/repos`); its change in a repository, one open at a
time (`POST /api/mate/changes`, `PATCH …/:repo/:n`, `POST …/attachments`); a tier of the recipe
(`GET /api/mate/recipe/:tier`); git over HTTPS at `/git/<appId>/<repo>.git` as the user `mate`,
which takes a Mate's push only on the branch of its own open change, only forward. §10.10 is what
zcp does with it.

**A Mate's environment observation** (ADR 0003; parity 84, 260, 275). `zerops_observe` is
registered only in a Mate's container, with no SSH requirement. It uses the enrollment's Mate
credential: `action="environments"` reads `GET /api/mate/environments`; `action="status"` reads
`GET /api/mate/environments/:projectId`; `action="logs"` reads
`GET /api/mate/environments/:projectId/services/:serviceId/logs?limit=`, 1–100 entries (default
100), each message at most 4,096 characters. The first call supplies environment project IDs,
the second service IDs. No application ID or Zerops credential is accepted from the model.
HQ resolves the Mate's current placement by ID on each call and permits only environments of
that application that every active person able to operate the Mate may read in Zerops. These
permission facts are read for the call, never served from the outage fallback. HQ opens and checks
the environment's deploy key against its one-project Basic user scope before reading services,
active-version IDs and names, or the platform's signed log backend. No env, raw service DTO,
deploy key or signed log address is returned. A missing or widened key is a visible refusal;
an unavailable read is a failure, not an empty answer. Each observation attempts once, including
a `503`; it ends visibly and asks the agent to call again manually. No sibling grants are minted
or synchronized; legacy grants still require separate manual removal. Tests: the fork's
`observation.test.ts`, `zerops/observationHttp.test.ts`; zcp `TestObserve_*`,
`TestServer_ObserveToolGating`, `TestAnnotations_ObserveTool`.

**A person's side** (`Authorization: Bearer <session>`, from the door, §10.4): the structure
(`GET /api/structure`, and its stream over a WebSocket ticket) — applications, the projects attached
to them as Mates, stages and productions, each application's changes and environments, as the
reader sees them in Zerops; an application made (`POST /api/apps`) and a project attached (`POST
/api/apps/:id/projects`); a change read, commented on, merged (`{expectedHead}`, a squash onto `main`
named `{title} (#{n})`) and closed; a tier of the recipe (`GET /api/apps/:appId/recipe/:tier`).
Core lands a Mate's recipe change by itself when it only adds files, and closes one that adds
nothing (§10.10).

**Environments** (since 2026-10-02). A stage or a production attached to an application is its
environment, recorded with the attach — named as the attach asks or after its project, following
`main` (a stage) or `release` (a production), in the order declared — with no pull request. Its
**deploy token** — `NO_ACCESS` in the org, `BASIC_USER` on the environment's project alone — is
minted by the client of whoever attaches it (a token cannot mint a token) and handed over with `PUT
/api/apps/:appId/environments/:name/deploy-token`; HQ keeps it only once Zerops says it reaches
exactly that project, never answers it back, and the structure says only `keyHeld`, and `keyInvalid`
once a deploy finds it no longer answers or reaches more. **A stage's deploys** are Core's: when
`main` moves, every stage following it gets the services its stage tier builds from the repository
whose `main` moved — the exact commit's archive, its zerops.yaml, the tier's setup, a version named
`main <7 hex>`, deployed with the environment's own token; a deploy is live only once the service
runs that commit, and an HTTP service then gets its subdomain. **Deploys are jobs** (the deploy-jobs
design, 2026-10-03): nothing deploys on a timer, and nothing is tried twice. An event — `main`
moved, a release, a developer's _Run again_ (`POST /api/apps/:appId/environments/:name/redeploy`)
or _Add {service}_ (`…/services`), an environment added, a deploy key kept, an import's hold —
writes its rollout in its own write, and the request that made it runs it before it answers:
planned into jobs once, then each environment's next job submitted where none builds, and the
answer says where each stands (`deploys`: building with its process, queued behind the build under
way, or refused, skipped or live, with HQ's words why); a deploy that did not go through never
undoes the merge or the release. A job is made per environment, service and commit — none while a
job of that commit is under way or HQ last put it live there, and `skipped`, saying so, where the
commit carries no `zerops.yaml` with the tier's setup or its build failed there and no release or
person asks again. One build at a time per environment; a newer job supersedes only one still
queued, and what waits is submitted once the build ends. A job is `queued → submitting → building
→ live`, or ends `failed` (the build's own, final until a person asks again), `refused` (what did
not go through at its one try — Zerops not answering, git failing, a key missing, dead or reaching
more than its project — with HQ's words why), `skipped` or `superseded`. A lost answer is never
submitted again: HQ reads the version it made, and follows a build by its process (else its
version) 75 min at most. A Core taking the lead follows what builds, refuses a submission whose
version it never heard ("HQ restarted before Zerops answered"), submits what waits and runs the
rollouts left unplanned. A service running a version HQ did not put there, while no job of HQ's is
under way, is said to the reader — with _Deploy {sha} again_ for whoever may _Run again_ while its
newest job is of HQ's live commit, and the service in Zerops — and never overwritten. A recipe
merge's delta imports only the services its tier change added (audit D2), follows its own import's
processes to their end and only then asks for their deploys; a service a person deleted comes back
only by _Add {service}_, and a changed declaration of a service the project has is reported, never
applied. Every request to Zerops is asked once, a read too: what does not answer is its caller's
answer, and a handle HQ follows is read again on its own cadence. An application's environments and their newest jobs (20, and each
service's newest live one past them) go to whoever reads its changes. Live on 2026-10-02 (`mate-rig-a`): the stage deploying 3 s after the merge and live at
63–81 s, its version `main 48b289b`, its own subdomain answering. Production and the release move
to HQ next.

### 10.9 A person in HQ

A person comes into HQ through its door with a throwaway, as into a Mate (§10.4): `mate-door:{HQ
project id}:{nonce}` at `POST /api/door`, answered with a session for HQ's API, the throwaway deleted
after. There is no Gitea account and no Gitea sign-in: what the client reads of a Mate's changes —
the rows, the review's detail, the comments, the pictures — is HQ's, as the person (§5.4, §10.11).

### 10.10 zcp inside a Mate

As soon as a dev pair exists, zcp gives it a repository in the application HQ holds the Mate in and
puts the pair on a branch that descends from that repository's `main` (a fresh history cannot merge
into the seeded one); a pair's work reaches `main` only as a change a person merges (*Delivery to
HQ*, below). It proposes the application's recipe — the whole app as the three tiers, composed by
one policy table (`bundle`), every runtime keeping its `buildFromGit` and `zeropsSetup` pair, its
`buildFromGit` the pair's repository in HQ (`https://<HQ>/git/<appId>/<repo>.git`; the stand-up
reads the repository's name from its last segment), no secret's value in any file — to the
application's recipe repository in HQ, `group`, and only what its `main` lacks, a tier directory at
a time (D30): a group's first recipe whole, a tier `main` lacks on any later pass, and nothing once
`main` has every tier — no change opened. A proposal is the Mate's own change in `group`, titled
exactly "Mate: the group's import files" with no description (the client knows a proposal by that
title), pushed from a scratch repository of zcp's own on the `zcp` container (`hq.Scratch`: the
credential reaches git through its environment only, as an Authorization header scoped to HQ). Its
branch holds `main`'s tree with the missing files written over it, so it only ever adds, and only
moves forward: a changed composition is a commit on its head, with `main` as a second parent once
`main` has moved, so the same change follows the project; a change `main` has overtaken — the tiers
it proposed landed from another Mate's — is brought to `main`'s tree, adds nothing, and Core closes
it. Nothing about the proposal is stored: each pass composes it anew and finds the change in the
Mate's state (`GET /api/mate/self`); two passes racing on the branch end in a refused push and the
next pass's work. It runs on every bootstrap close and adopt pass, right after the repository
reconcile, after a stage deploy's delivery (once the delivery let go of the pair's checkout), and
when the agent asks (`zerops_workflow action="group-recipe"`), which answers with the change or that
`main` already carries every tier; Core lands a proposal that only adds by itself, and a person
merges one that changes a file (SPEC §3.2c). The recipe, and the stage and production projects its
tiers create, are named after the application: the slug of its name as HQ gives it
(`MateState.appName`, by the group-slug rule main's client named groups with — lowercase letters,
digits and dashes, starting with a letter, 2 to 30 long), its id while HQ names it nothing. The
recipe's names — the repository `group`, the proposal's title, each tier's path — are
`@t3tools/shared/hqRecipe`'s, mirrored in `internal/hq/recipe.go`. `group` is reserved: HQ answers a
repository of that name with the recipe repository whoever asks, so zcp wires no pair to it — a dev
half named `group` is told to rename the service, a recipe naming it to fix the recipe — and the
recipe leaves such a pair out rather than wait on it. zcp's own key is never handed out: no `ZCP_API_KEY` in a
build-integration secret, `GITEA_TOKEN` masked on every value dump, the one `zcli push` of a
self-deploy given the key through its environment and never `zcli login`. `hq_recipe.go`,
`hq_recipe_inputs.go`, `internal/hq/scratch.go`, `internal/hq/recipe.go`;
`TestGroupRecipe_ProposedAsTheMatesChange`, `TestGroupRecipe_ProposesOnlyWhatMainLacks`,
`TestGroupRecipe_FollowsTheProjectOnTheSameChange`, `TestGroupRecipe_MainMovedUnderAnOpenProposal`,
`TestGroupRecipe_AProposalMainOvertookAddsNothing`, `TestGroupRecipe_OpensNothingMainAlreadyHas`,
`TestGroupRecipe_WaitsForAnUnwiredPair`, `TestGroupRecipe_ProposesTheLiveProject`,
`TestHandleGroupRecipe_Table`, `TestGroupRecipe_NamedAfterTheApplication`, `TestRecipeSlug`,
`TestWireHQPair_TheRecipeRepositoryIsNoServicesRepository`, `TestGitEnv_CarriesTheCredentialScopedToHQ`
— against a fake HQ that serves real git with HQ's ref rules. A Mate joining from the recipe has its parts and no live proof: _Add Mate_ imports
the AI Agent tier with the first Mate's hostnames, every Mate of an application reaches all of its
repositories in HQ (a hostname names one repository there), and the repository reconcile cuts the
Mate's branch from `main` — a checkout with no commit of its own takes `main`'s tree. The adopt-time
reconcile runs where the metas are complete.

**The retired Gitea workflow decision (2026-10-03).** The upstream backbone's
`zeropsio/gitea-mate/actions/deploy@v4` workflow set up tests as follows. This applies
only to that backbone; the rebuild keeps Core's deploy jobs (§10.8), and zcp emits
no Gitea workflow or broker credential in an HQ repository.

The group's runner has no
language runtime, so the file sets up the pair's own — the dev half's Zerops type, read from the direct
service list, at that version — before the Test step, which stays a no-op until the project fills it
in; only where a setup action works on the runner as it stands: Node.js, Go and Java (a `.tar.gz`
build). Bun and Deno (their actions extract a `.zip`, and the runner has no `unzip`), Python (its
builds expect the hosted runners' `/opt/hostedtoolcache`) and PHP (installed through apt on the
releases it lists) get a Test step whose comment says no setup action works and shows what does — the
distribution's packages, at the distribution's version, or the language's own installer — and a type
zcp cannot read or has no word on gets the general rule: a setup action that downloads a `.tar.gz`
build works, anything else comes from the distribution's packages or the language's own installer. Wiring and every delivery write the file only where it is missing, is exactly the untouched file an
earlier zcp wrote (which gains the setup), or names an earlier version of the broker's deploy action
(its Test step kept); a file naming this zcp's action, whatever was made of it, and a workflow of the
project's own that does not deploy through the broker are the project's, never rewritten
(`TestGiteaWorkflowYAML_SetsUpTheServicesRuntime`,
`TestReconcileGiteaRepositories_WiringWritesTheWorkflowOnlyWhereItIsNotCurrent`,
`TestADeliveryBringsTheWorkflowToThisZcps`).

**What a tier carries (2026-09-30).** A tier is the Mate's project as it runs, written for the
person who reads and edits it in the recipe repository: two-space YAML, each service opening on its
`hostname` and `type`, a header naming the tier, the Mate it was written from and that it is the
group's to edit. The project block carries the recipe's name (the AI Agent tier keeps the Mate's
own), the live `corePackage`, and the project's user-set variables —
config under `envVariables`, secrets under `envSecrets` — and each runtime its own variables under
`envSecrets`; the platform's own and the control plane's (zcp's key and agents, the git token — on
a dev half, the Mate's HQ credential — the launch token and a Gitea token) are never written. The composer decides each variable
unattended and fails closed: a value of `${name}` references alone is wiring, kept as written, and
any other value is kept only when no signal marks it secret — the platform's flag, a masked read, a
credential's name (a PIN, a seed, a passphrase and a webhook's address included, since no shape
tells those from a word), a secret's shape — and it has a narrow config shape (a number, size or
version, a phrase of words, an email or a mailbox, a URL or path with no credential or fragment and
a query of plain settings, a glob, a cron line, an image, a list of these, flags built from flag
words that name no credential), or when its name is public by design (`NEXT_PUBLIC_*`,
`*_PUBLISHABLE_KEY`) with no SECRET, PASSWORD or PRIVATE beside it, the value is no private or
vendor key, and the platform neither flags nor masks it. Everything else is written as
`<@generateRandomString(<N>)>`, N its live length (16 to 1024; 32 when masked), empty when empty, a
`user:password` keeping only a plain-word user, with a line asking for it again unless the app makes
it for itself (`…SECRET`, `…PASSWORD`, `…SALT`, `APP_KEY`). The flag alone is not the rule: older
secrets read back not sensitive, and a project variable's flag never persisted. A recipe's order
is designed when it is written, from what each runtime's build reads: priority is dependency order,
since the platform creates each priority group only after the one before it is created and deployed,
and the broker deploys an environment's runtimes — and a Mate's stand-up its halves — in the same
order: the managed services at 10, a runtime no build reads at 1, and a runtime another's build reads
one above the longest chain of runtimes whose builds read it — a read being a `${host_key}` in any
setup's build variables of its zerops.yaml, directly, through a runtime variable the build lifts
(`${RUNTIME_X}`, from the setup's run variables or its own service variables) or through project
variables (`bundle.BuildReadValues`, `bundle.ReferencedHosts`). What a runtime reads only once it runs
orders nothing, so services with no build-time read between them share a priority; runtimes whose
builds read each other share a rank. The import format has no field for the reads themselves, so
the tier carries only the priorities they give, and its header says what they mean; the stand-up reads them
again from the same zerops.yaml. A managed service keeps its live scale (Small Production's
production profile replaces a profile-bearing one's), an object storage its size and its policy when
the platform names it (a custom policy's document can hold a secret, so it stays out, said), a type
with no HA variant stays single-node there; a standalone runtime built from a public repository — no
pair records it, no dev/stage sibling runs beside it (mailpit) — is written as it runs on every
tier, its build URL — a runtime's too — bare of any credential and absent when the URL carried one;
a finished pair the repository pass will still wire, or a dev/stage pair zcp has not adopted, holds
the recipe back, while a pair no pass will wire (its own remote, an unfinished bootstrap) is left
out, said; the group environments' values name their own runtimes (`http://medusastage:9000` →
`http://medusa:9000`). A string a YAML 1.1 reader would reinterpret (`yes`, `on`, `1:30`) is quoted.
A read that decides what a tier carries fails the pass rather than propose a tier with a gap `main`
would keep (`TestGroupPriorities_*`, `TestBuildGroupRecipe_PriorityFollowsBuildReads`, `TestBuildReads_OnlyWhatTheBuildReads`, `TestRecipeSecret_Rule`, `TestBuildGroupRecipe_FailsClosed`,
`TestBuildGroupRecipe_MedusaGolden`, `TestComposeGroupRecipeInputs_ReadsTheLiveProject`,
`TestComposeGroupRecipeInputs_WaitsOnlyForWhatALaterPassBrings`).

**Delivery to HQ (2026-10-02, SPEC §3.2a).** A Mate enrolled with its org's official HQ delivers
through changes, and only a dev/stage pair can: the dev half is the checkout that pushes, the stage
half the verified basis a production is promoted from. So zcp's classic route refuses a plan that
gives such a Mate a runtime with no stage half (`bootstrapMode` simple or dev), naming the standard
pair to re-submit.

- **The repository** is `<appId>/<name>` in the application HQ holds the Mate in, the name the dev
  hostname (or the one a recipe names). zcp asks for it — `POST /api/mate/repos {name}`, idempotent,
  `main` born with one commit of the empty tree — and wires the pair: `git-push-setup` to
  `https://<HQ>/git/<appId>/<repo>.git` with the Mate credential, the Mate's local branch
  `mate/<project id>` cut from `main` (`ops.BuildMateBranchCommand`: a pair fresh from `zcp init`
  takes `main`'s tree, a pair with history of its own is joined onto HQ's empty seed, anything else
  is refused with its remedy), and the pair's record `ServiceMeta.HQ {appId, repo, branch}`. A remote
  that is not this HQ's is the user's own and left alone. One repository per pair is enforced by that
  record; a pair the stand-up adopted is wired to the recipe's name, and wired again to it.
- **A deploy onto a wired pair's stage half delivers it** with nothing asked of the agent or the
  person — "build a todo app" is the whole prompt (the owner, of a prompt that had to name a git-push
  deploy: "no person is ever going to say this"). In the dev half's checkout zcp commits the tree as
  deployed, in the work session's words; an unignored `node_modules`, `vendor` or `.venv` stops it
  before anything is staged. It fetches, absorbs a squash of the Mate's own landed change (below),
  takes `main` in by merge — never a rebase, so history only moves forward and no force can lose a
  commit — and a collision only a person can settle leaves the checkout whole and is named with the
  exact commands. Then it sends the candidate tree's git hash to HQ — `POST
  /api/mate/changes {repo, title, tree}`. HQ compares that hash with its own `main`'s tree before
  allocating a number or returning an open change. Equal trees answer
  `{change:null, created:false, reason:"nothing_to_deliver"}`, recorded as `delivery_empty`;
  zcp reports "nothing to deliver: main already has this". Otherwise HQ answers the Mate's open
  change in that repository or opens the next number (one open change per Mate per repository) — and pushes HEAD to its branch
  `mate/<project id>/<n>`, the one ref HQ takes from the Mate: only its own open change, only
  forward; nobody deletes a branch and `main` moves only by HQ's merge. A change still titled
  `Mate: <hostname>` takes the task's words (`PATCH`). Nothing differing from `main` opens nothing.
  A delivery never fails the deploy: every outcome is a line in its next actions.
- **The result** keeps the shape the chat card decodes: `pullRequest {repo, branch, base, number,
  created, url, described?, descriptionNote?}`, its `url` the change's address at HQ,
  `https://<HQ>/changes/<appId>/<repo>/<n>`, which HQ leads into the client; the line tells the
  agent to give the person that link.
- **What became of the change** (merged with `mergedSha` and `landedHead`, closed) is read fresh
  from the Mate's own state, `GET /api/mate/self`, by every delivery, push, `describe-change` and
  reconcile pass — no merge passes through this process, so nothing waits to be told. A merged
  change records its landing (`ServiceMeta.HQ.Landed`) and the next delivery folds the squash in as
  a real merge, proven lossless first (`ops.BuildAbsorbLandedChangeCommand`). After taking `main`
  in, that explicit delivery keeps the previous history under `refs/zcp/landed/<mergedSha>` and
  starts the Mate's branch at current `main`: equal trees leave HEAD at `main`, newer work is
  carried as one commit with the delivery's title and `main` as its parent. The working tree and
  index stay intact, and the tool reports the new base and the kept ref. A pass only records the
  landing; it never straightens checkout history. With no change open and no local landing left,
  the explicit delivery uses HQ's latest recorded landing as its base, so a legacy checkout
  follows the same transition. The record's number is
  cleared once the change is no longer open, so the next delivery opens the next one.
- **HQ not answering.** Every 503 is "try again": the client waits out `Retry-After` within 20 s (a
  deploy runs two HQs side by side for about that long), and a push HQ's standby answers 503 —
  which git prints without the header — is tried again within the same bound. A delivery HQ did not
  refuse but could not be reached for is recorded as pending (`ServiceMeta.HQ.Pending`, its title
  kept), its work committed in the checkout. `zcp service mate` finishes it once HQ answers, with
  nobody asked: beside the enrollment's keep it runs rounds over the pairs the Mate's agents keep
  (`/var/www/.zcp/state`) every 30 s, and while a delivery stays owed every 5 s doubling to 5 min.
  A round only finishes on a clean checkout of the Mate's branch and says nothing while HQ is away
  — the enrollment's keep is what logs that. The next delivery, git-push or reconcile pass finishes
  it too, whichever comes first.
- **One process at a time on a checkout.** Every step that runs git on a pair's checkout — a
  delivery, a git-push, a stand-up's wiring, a pass, a round of `zcp service mate` — holds the pair's
  lock (`workflow.LockPair`, a file lock in the state directory keyed by the pair's dev hostname) for
  its whole sequence, and reads the pair's record afresh once it holds it. A pass and a round leave a
  pair held elsewhere to their next turn, recording no attempt; an agent's step waits up to 2 min and
  then runs no git and says why.
- **A Mate HQ now holds in another application** is wired there on its next delivery or pass: the
  repository of the same name in the new application, made if new, `origin` pointed at it and the
  branch joined onto its `main`; its change in the old application stays where it is.
- **The credential.** The dev service holds the Mate credential as its `GIT_TOKEN` service secret,
  which the per-command credential helper presents as the user `mate`: the dev container now holds
  the Mate's HQ credential, as it held the Gitea bot token before. The enrollment is the
  credential's one home, and a re-enrollment replaces it, so before every delivery, push and pending
  finish zcp compares the copy with the enrollment's (constant time), rewrites it and proves a fresh
  session with it; a credential HQ refuses marks the pair `broken`, healed once a session
  authenticates. The helper persisted in `.git/config` for HQ's host answers the dev service from
  `GIT_TOKEN` and the Mate's own shell from `zcp hq git-credential`, which reads the enrollment at
  each request and answers for the enrolled HQ only.
- A push to HQ is watched for no build and offers no integration, and a wired pair's direct deploys
  are never redirected to a push. `zerops_deploy strategy="git-push"` to HQ delivers committed work
  the same way — `main` taken in, the change opened, HEAD pushed to its branch — and a branch of its
  own choosing, `main` included, is refused before git runs. The develop session's auto-close note
  tells the agent to hand the person the change's link and the next step — once the session
  delivered, that is deployed a stage half; a stand-up's closes without it.

`hq_delivery.go`, `hq_change.go`, `hq_wiring.go`, `hq_pending.go`, `hq_pair_lock.go`,
`hq_push_credential.go`, `hq_git_push.go`, `workflow/pair_lock.go`, `ops/delivery_git.go`, `ops/delivery_absorb.go`, `internal/hq/changes.go`;
`TestAStageDeployOfAWiredPairDeliversItself`, `TestAStageDeployAbsorbsTheMatesOwnMergedChange`,
`TestAStageDeployWithNothingBeyondMainOpensNoChange`, `TestADeliveryHQCouldNotReachIsFinishedByAPass`,
`TestFinishPendingDeliveries_OnceHQAnswers`, `TestKeepFinishingDeliveries_BacksOffWhileOneIsOwed`,
`TestTwoFinishesOfOnePairRunGitOnce`, `TestAPassSkipsAPairHeldElsewhere`,
`TestADeliveryWaitsForItsPairsCheckout`, `TestLockPair`,
`TestADeliveryWaitsOutAStandby`, `TestADeliveryFollowsTheMateToAnotherApplication`,
`TestADeliveryBringsTheCredentialToTheCurrentOne`, `TestADeliveryRefusedByItsGitSaysWhatToDo`,
`TestReconcileHQRepositories_*`, `TestGitPushToHQ_*`, `TestAMateDeliveringThroughHQPlansOnlyStandardPairs`,
`TestBuildDeliveryCommand_*`, `TestBuildMateBranchCommand_*`, `TestClient_*`, `TestGitCredential_*`,
`TestPersistedCredentialHelper_*` — against a fake HQ that serves real git with HQ's ref rules.

**A new Mate stands up from its recipe in one call (D32, 2026-09-30).** Adding Wren to Beviro —
whose recipe is `0 — AI Agent/import.yaml` on `main` of the application's recipe repository in HQ,
`group` — runs like this.

Who holds what:

- **The person's browser, as the person**, reads the recipe and builds the project: it imports the
  `project:` block and the managed services, adds the `zcp` container, lowers the container's
  token (`BASIC_USER` on its project, `READ_ONLY` on the project's other environments), drops
  the one-time delegation, grants the broker, registers the Mate, closes the project off once
  `zcp` answers (per-service env isolation, `ZCP_API_KEY` a sensitive variable on `zcp`), and
  imports the runtimes.
- **HQ** keeps the recipe on `main` of the recipe repository and gives each pair its repository;
  the Mate reaches both with its enrollment (§6.6).
- **zcp**, with its lowered token and its enrollment, stands the Mate's development up in its own
  project and nowhere else — the group's stage and production are not its, and it reads no tier but
  `0 — AI Agent`.
- **The model** acts only on its signer's message, and is the backup.

The order:

1. The browser reads the recipe as the person.
2. It imports the project with the recipe's `project:` block and the managed services only.
3. It adds the `zcp` container through the platform's development-container recipe.
4. It lowers the container's token, drops the delegation, grants the broker and registers the Mate.
5. When `zcp` answers, it closes the project off and records that at the Mate's HQ
   (`POST /api/mates/{projectId}/closed-off`, once the Mate's record exists there), and the sign-in
   is offered. HQ's mark is a receipt: zcp's runtime import, `zerops_import` and `zerops_standup`
   read closed-off from Zerops with the Mate's own key — `envIsolation`'s first word `service` and
   no `ZCP_API_KEY` left project-wide (`ops.ProjectClosedOff`); a read that fails, or one without
   `envIsolation` yet, holds the import, never opens it.
6. It imports the runtimes in one wave: dev halves `startWithoutCode: true` (running and empty, the
   checkout target), stage halves with no build (`READY_TO_DEPLOY`), public-build utilities with
   their build, which the platform runs.
7. `zcp service mate` enrolls the Mate with its HQ (`hq.Keep`): the enrollment is what the stand-up
   reads the recipe and the recipe's repositories with.
8. The person signs the agent in meanwhile; the Mate's server keeps who did (§10.5).
9. The Mate's server sends "Stand up development of the project." into the main conversation as
   the person its HQ names as the stand-up's asker (`standupRequestedBy`, written in the same write
   as the Mate's record), once that person signed an agent in here (`ZeropsSetup.ts`). Until the
   link to HQ brings the Mate nobody is known to have asked, and nothing is settled on that; HQ's
   record naming nobody settles it as none at once. No client sends it. While it waits,
   `/setup.json` says why where the server knows: `no_hq`, `not_enrolled` (with HQ's refusal
   `code`), `not_linked` (C-7). The Mate's AGENTS.md sends that message to `zerops_standup` first.
   The tool:
   - waits, bounded, for the Mate's enrollment with HQ (3 min) and for the runtimes (5 min: every dev half
     running and answering SSH, every stage half created), and for whatever the import still has in
     flight on them and on the managed services;
   - reads the tier from `main` of the recipe repository of the application HQ holds the Mate in
     (`GET /api/mate/self` for the application, `GET /api/mate/recipe/mate` for the tier);
   - pairs the runtimes that build from the application's repositories in HQ
     (`<HQ>/git/<appId>/<repo>`), per repository, by zcp's naming —
     the stage half ends in `stage`, its dev half is the repository's other runtime or the one named
     like it — with each half's `zeropsSetup` (else its hostname) and the pair's priority, the
     higher of its halves'; managed services and public builds are skipped, and so, by name, is a
     runtime with no partner or another application's repository (`workflow.ParseMateTier`);
   - adopts each pair exactly as the adopt route records it, plus the tier's setups
     (`workflow.AdoptPair`), mounts the dev half and wires it, through the reconcile's own wiring, to
     the repository of the recipe's name in HQ — refused first when HQ does not have that
     repository (git's own `info/refs` answering not found), since HQ makes what it is asked for;
   - deploys each half as soon as what it needs stands, at most five at once, over two calls. What
     orders the halves is what each build reads of the others, read from each half's own setup in
     its pair's zerops.yaml as the recipe writer reads it: a storefront's stage build pre-rendering
     from its API's stage waits for that stage, and nothing else waits. Every dev half starts at
     once — a dev build installs dependencies — unless its build reads another dev half; the first
     call answers once they stand, its stages queued; on the model's second call, a stage is
     cross-deployed from its dev half — never alongside it, since both read the dev half and one
     source's deploys serialise on its git lock in goroutine order, a self-deploy first dropping its
     SSH session under the cross-deploy — once what its build reads stands. It ships the dev half's
     HEAD commit exactly (deploy-from-commit, named `{branch} {short sha}`), never its working
     tree, which the dev servers started between the calls may have touched. A read counts by every
     route the writer counts: directly, through a setup it extends, through a runtime variable it
     lifts from the setup or from the half's own service variables in the tier, or through the
     tier's project variables. A pair whose zerops.yaml cannot be read keeps its priority's edges;
     a recipe whose builds read nothing of each other, or read each other in a circle, falls back to
     its priority: a stage waits for every stage above it. A half the platform already runs code in
     (an earlier call, or a deploy by hand) is left as it is, whatever it waits for. Through `zerops_deploy_batch`'s gate, pre-flight and
     post-deploy steps (subdomains included), with no delivery: the stage runs `main` as the Mate's
     preview;
   - starts no dev server — no dev command is recorded anywhere zcp reads — and names, per dev half,
     the `zerops_dev_server` call with the port its dev setup declares;
   - answers one entry per service: adopted, wired, deployed with its URL, queued, or failed with
     zcp's failure classification, the dev server's state, and the model's next step.
10. The model starts the dev servers, verifies, and tells the person development is up, with each dev
    half's address.
11. It calls `zerops_standup` again in the same turn, which builds the stages and waits for them. A
    stage never called for stays `READY_TO_DEPLOY`, as the import left it: nothing breaks, and any
    later call, or a deploy by hand, builds it.

The stand-up's section of the status file (`ZCP_STATUS_FILE`, `standup`) is one stand-up over its
two calls: `running` from the first call's start until the second has returned, never `done` between
them. A first call that leaves the stages queued ends with the section still `running`, in the
`stage` phase, its stages `build`/`pending`, and keeps beating it; the second call goes on with that
section — the same `startedAt`, the same halves — and ends it `done` or `failed`. A second call that
does not come within 15 minutes ends it `done` in the `development` phase, the stages still pending.
`TestStandup_WritesItsProgressForTheRunCard`, `TestStandup_TheRecordNeverSaysDoneBetweenItsCalls`,
`TestStandup_ACarriedStandUpEndsWhenNoStageCallComes`.

The model is the backup:

- A refusal before anything is touched — no enrollment with HQ, no application (HQ's state saying
  none, or HQ refusing the tier `mate_not_in_app`), no tier on `main`, a tier past HQ's read bound
  (`413 too_large`), a tier that does not read or holds no pair — is an error result naming the
  adopt route.
- A pair that cannot be stood up — a runtime still missing after the wait (its next step is the
  `zerops_import` of the tier's entry, shaped as the browser imports it), a dev half not running, a
  repository not in HQ, a checkout refused — stops alone; a failed build skips its stage; a
  half that does not stand holds only the halves whose builds read it (with no reads, the stages
  below it by priority), which say what they waited for.
- The model carries on in the same turn with zcp's own tools (adopt, `zerops_deploy_batch`,
  `zerops_dev_server`), and `zerops_standup` may be called again: it skips what is adopted, wired or
  deployed and waits for a build still running.

`standup.go`, `standup_deploy.go`, `standup_report.go`; `TestStandup_StandsUpEveryPairFromTheRecipe`, `TestStandupAfter_EveryDevHalfStartsAtOnce`,
`TestStandupReads_FromTheRecipe`, `TestStandupReads_EveryRouteTheWriterCounts`, `TestStandup_TheModelIsTheBackup`, `TestStandup_ReturnsOnceDevelopmentIsUp`,
`TestStandup_WaitsForTheRuntimesTheBrowserIsImporting`, `TestParseMateTier`, `TestParseMateTier_PairsOnlyRepositoriesOfThisApplication`,
`TestClient_RecipeTier_ReadsTheTierFromHQ`, `TestClient_RepoExists_AsksGitWithoutMakingIt`, `TestAdoptPair_RecordsWhatTheAdoptRouteRecords`,
`TestBuildAgentsMD_Container_StandUpRoutesToTheTool`, `TestServer_StandupToolGating`. Not yet run on a
live Mate.

**Open: a stage from a commit, beside its dev half.** On Beviro's measured builds the graph brings
development up at ~6 min (it was 19) and everything at ~20.5 min (it was 26); ~15 min is reachable
if a stage starts beside its dev half instead of after it. zcli always waits for the pipeline, and
its archiver ends with `git prune`, which deletes another push's in-flight objects from the same
checkout, so one source's pushes cannot overlap from the working tree; a stage pushed from a commit
(extracted to `/tmp`, `--no-git`) only reads reachable objects and could drop the source lock after
the extraction. The blocker is the Mate project's shared database: the dev and stage setups both run
their start-up migrations and seed at deploy, and would run them against it at the same time.

**A stand-up stops at dev (2026-09-30).** A new Mate's stand-up done by the model — the fallback
of D32, when `zerops_standup` stops short — deploys the dev halves it adopted (their build installs
the packages), starts their dev servers, verifies them, and names the stage as the next step the
person can ask for: no promotion, so no commit, change or push unasked. zcp tells a
recipe-born Mate's empty services from deployed ones since the Beviro trial of 2026-09-29, so their
first deploy opens develop's first-deploy branch — which promotes each standard pair to its stage,
and in a Mate delivering through HQ a stage deploy is the delivery above. So in a Mate, develop start leaves the
stage half of an adopted pair whose dev half never deployed out of the session's scope
(spec-workflows D2f): nothing is asked of the agent, the session closes on the dev halves, and the
next task's session, its dev half deployed, delivers through the stage as before. A pair a Mate
created for a task keeps delivering through its stage on its first deploy — "build a todo app" is
still the whole prompt (`TestHandleDevelopBriefing_MateStandUp_LeavesTheStageOut`,
`TestScenario_S14_StageLeftOutOfScope`).

**A change's description (2026-09-29; in HQ since 2026-10-02).** A change opens with the task as
its title and nothing more — the task is all zcp knows — while the person reviews it by what it says
(§5.4: the review shows what the change does in the Mate's words, right after the verdict). So the
Mate writes it: `zerops_workflow action="describe-change" service=… description=…` — markdown, at
most 20000 characters and no NUL, which no text in HQ keeps — sets the body of the pair's open change
(`PATCH /api/mate/changes/:repo/:n {body}`), the one on record or the open one the Mate's state names,
and replaces what was there; the title stays the task's. The words are kept on the pair first
(`ServiceMeta.HQ.ChangeDescription`), so none are lost: with no change open, or HQ not answering,
they wait for the change the pair opens or reaches next, and the delivery or push that does puts
them on. Words never reach a change they were not written for: a change on record that merged or
closed before they reached it is named to the Mate and nothing is kept, and words kept for a change
that is gone are dropped rather than inherited by the next one. Describing never opens a change. A
delivery or a push that leaves a change open asks for the description — what it does and why, how it
was checked, and again whenever it grows — or says the words it kept are on it now.
`hq_change_description.go`; `TestDescribeChange`, `TestDescribeChange_KeptWordsGoOntoTheNextChange`,
`TestDescribeChange_Refusals`, `TestWorkflowTool_DescribeChangeReachesItsHandler`.

**A change's pictures (2026-09-29; in HQ since 2026-10-02).** The pictures that prove a change
are the screenshots `zerops_browser` takes, and each is kept as one of the Mate's pictures
(`workflow.KeepPicture`, under the state dir) and named in the tool's result — `screenshot.picture`,
`shot-3`, an id never given twice; the newest twenty are kept, and every one a kept description
names. A description shows one as `![what it shows](shot-3)`: `describe-change` sends each picture
it names to HQ as the change's attachment (`POST /api/mate/changes/:repo/:n/attachments`, the PNG as
the body, at most 20 MiB, once per change, the address remembered with the picture) and publishes it
as `<img alt width height src>` at `https://<HQ>/api/apps/<appId>/changes/<repo>/<n>/attachments/<id>`,
which the client reads as the person — the width and height the picture's shape at 720 pixels wide
at most, so a reader reserves its box before the bytes arrive. Never a body with a broken picture: a
picture the Mate does not keep is refused before anything is written, and one HQ will not keep
writes nothing and keeps the words. `TestDescribeChange_Pictures`, `TestKeepBrowserPicture`,
`TestKeepPicture_*`, `TestPictureRefs`.

**A merge may come at any moment (2026-10-03; delivered through HQ).** A change appears in
its Mate's conversation as soon as it opens, and the person may merge it while the agent still
works. Every stage delivery and git-push that leaves one open asks for its description, then tells
the agent that the person may merge it at any moment; the next stage deploy folds that merge in
and opens a change only for what `main` still lacks. News of the previous change precedes the new
delivery's result, including a failed result, and a result that already opened the next change
no longer promises one. A closing session hands over a change only where its successfully
deployed stage half still records an open one. Nothing beyond `main` is a normal outcome,
never a failed proposal or change `#0`: HQ compares the candidate tree with `main` before it
allocates a number (§10.10, *Delivery to HQ*), and the additive and scaling recipe paths keep
Core's same empty-change rule. A pending landing remains recorded until its push succeeds.
Production guidance reads the Mate's changes from HQ even after that push cleared the local
landing; an unreadable HQ is a visible unknown, with a manual ask again.
This preserves upstream's empty-diff, hand-over and remembered-merge decisions. Its
Gitea-specific compare (`?output=diff`, with a JSON commit-count fallback before Gitea 1.27)
and `GiteaRepoRef.LastLanded` are replaced by HQ's tree comparison and authoritative change
history, rather than restored as a second forge or a local copy of HQ's history.
`TestAStageDeployAfterASquashWithNoNewWorkReportsNothingToDeliver`,
`TestAStageDeployAbsorbsTheMatesOwnMergedChange`, `TestAStageDeployOfAWiredPairDeliversItself`,
`TestGitPushToHQ_DeliversCommittedWorkAsTheChange`, `TestSessionAnnotations_HandoffOnlyAfterADelivery`,
`TestProductionKnowsMergedWorkAfterTheMergeIsAbsorbed`, `TestProductionGuidance_HQUnavailableIsUnknown`,
`TestGroupRecipe_OpensNothingMainAlreadyHas`, `TestGroupRecipeScaling_ProposesFromMainsHeadEveryTierFresh`.

**A Mate's production is its application's (2026-09-23; through HQ since 2026-10-02).** Asked
"muzes to dat na produkci?", a wired Mate started launch-production and answered from inside its launch gate, while
its group already had a production with `055a7e8 Mate: weatherdev (#4)` merged and waiting for
_Release_. launch-production creates its own production project on a user-owned remote and stages
its own token; it has no case for a group's production, which the person adds from the projects page
and which runs what a release tag on the group repo lists (D16, D27, D28). So in a Mate
enrolled with its HQ (`hqWired()`), `handleLaunchProduction` refuses before scope, state or any
mutation, with the blocker `wired_mate_production_is_the_groups` and a next step that says what is
true of this Mate's own pairs — never "nothing open" read as "merged": every recorded change is first
read fresh from the Mate's own state in HQ, then one still open is named (even if its local
number was lost) — "merge `<host>`'s change #N on `<repo>` first" — a merge HQ holds,
even after the local landing was absorbed, points at the projects page, and no change at all
or one closed without merging says to deliver through the stage half first. The classic route
is untouched. The `idle-launch-entry` atom branches on "delivers through its HQ" the same way and
tells the agent not to start the workflow. `hq_delivery.go`;
`TestHandleLaunchProduction_HQWiring_RefusesBeforeAnyStep`,
`TestLaunchProduction_AMateDeliveringThroughHQRefusesAndSaysWhatIsTrue`; golden
`idle/bootstrapped-with-managed`. No zcp tool tags an HQ release; Core does so for the person.
Outside HQ, `action="release"` keeps tagging the pair's own checkout remote.

**An agent's release request in HQ.** `zerops_workflow action="release"` returns
`status="release-person-required"` when this Mate is enrolled with HQ, or the named pair has a
recorded HQ repository. This happens before service/bootstrap requirements, git-push prerequisites,
freshness checks, tag reads or tag writes; `releaseVersion` does not authorize an agent to tag HQ.
The result tells the agent to ask the person to open this Mate's application on Mate's projects
page, choose _Review release_, review what it carries, and press _Release_ with the version shown.
The pipeline note says Core deploys production from approved releases, and explicitly says this
tool has not checked production setup or release readiness. `ProdLaunches` is not evidence of either
in HQ. The handoff conditionally points to _Add production_ when missing and _Merge_ for any
still-open changes the person wants included; a blocked review names its unmet requirements in Mate.
It never recommends launch-production or tag-consuming CI for HQ.
`TestHandleRelease_HQExplainsProductionWithoutLegacyPipelineAdvice` covers both missing and stale
legacy launch records. Nothing has been released by this tool.
Outside HQ the source-side release route verifies a clean tree and pushed HEAD. Pass `service`
without `releaseVersion` to receive `status="release-prompt"`, the next `vX.Y.Z` suggestion derived
from remote tags, and the exact confirmation call. After the user confirms, re-call with
`releaseVersion` to tag and push that verified HEAD; the tag fires the production pipeline.
The input schema keeps the HQ/outside-HQ distinction and call prerequisites concise; the release
results carry the full walkthrough when requested, rather than paying for it on every `tools/list`.
`TestHandleRelease_HQHandsOffBeforeLegacyPreflight`, `TestHandleRelease_AWiredPairHandsOff`, and
`TestHandleRelease_PromptSuggestsNextVersion` cover these routes.

### 10.11 Environments, the Git tab, release

_Add stage_ and _Add production_ (mate 0.11.0) create the project from the recipe's tier converted
to import-ready form — every `buildFromGit` + `zeropsSetup` becomes `startWithoutCode` with the
source map kept, because the platform cannot clone a private repository and refuses a setup without
a source. _Add Mate_ reads the AI Agent tier the same way. Since 2026-10-02 the three read their
tier through the organization's official HQ, as the person (`GET /api/apps/:appId/recipe/:tier`),
keyed by the application: HQ answers present or absent, a tier this build finds no service in is
unreadable, and the recipe is loading — never absent — until HQ is open; a landing of the recipe's
proposal reads it again. A stage's or a production's project carries no tag of ours
(`createEnvironment.ts:347-355`); its press's `register` step (§4.4) makes it an environment of its
application in two writes, in an order that never leaves a half-made one
(`addGroupEnvironment.ts:1-18`, `useEnvironmentCreation.ts:143-178`): the project attached in HQ as
its stage or production, as the person (`POST /api/apps/{id}/projects`) — HQ records the environment
with it, its name, its sources and its place in the order, and keeps one production per application
— then its deploy key, `BASIC_USER` on that project alone, minted by the person's client and handed
to HQ only where HQ holds none that works (§10.8). There is no pull request and no broker. A stage
or a production whose attach or key a reload lost, or whose key HQ holds broken, is finished by the
projects page on its next read with the same two writes (`ZeropsProjectsPage.tsx:1958-1977`,
`useZeropsGroupEnvironmentReconcile.ts:1-20`). Superseded 2026-10-02: `environments.yaml` in the
group repo declared each environment — name, tier, project, sources — and the add tagged the project
into the group, widened the broker's token with it and wrote the declaration (a commit for a
releaser, a pull request otherwise).
`addGroupEnvironment.test.ts` — "attaches the project as its tier, then hands HQ the key minted for
that one project", "mints nothing for an environment whose key HQ holds and finds working", "stops
at the registry with HQ's refusal, minting nothing"; `runEnvironmentCreation.test.ts` — "registers a
stage or a production before waiting on its services"; `groupEnvironments.test.ts` — "names a
production HQ neither places nor holds as an environment, and nothing else";
`useZeropsGroupEnvironmentReconcile.test.ts` — "a cold load with a half-made environment repairs it
once, and settles".

The **Git tab** — a right-panel kind `git` beside `diff`, `browser` and `data` — is the Mate's
own leg of the project's flow and nothing else (D26): per codebase — the dev half of each pair, or
the single service a pair grew from (zcp's expansion keeps the dev hostname: `todoapp` with
`todoappstage`, mate 0.11.12); the stage half is deployed to and never a checkout (0.11.10) — one
block with the checkout's branch and counts from the Mate server's `subscribeVcsStatus`, and from
HQ (since 2026-10-02) the Mate's newest change in that repository — its open one, or the one that
landed last — with its commits from HQ's detail of it; the remote's health from a live `git
ls-remote` through the server (`ZeropsGitRemoteProbe`); one verb — _Push_, _Update from main_ or
_Review_, which opens the change's review (§5.4), whose _Merge_ merges; the tab merges nothing
itself, reads nothing from Gitea, and offers no _Open pull request_: a Mate's push opens its change.
Checkout actions run as the agent's user for the Mate's owner only. It infers nothing from
another source.

**Open, for decision: §6 and this tab disagree.** §6.3 keeps a second commit pipeline off on Zerops,
and §6's "What S3 does not do" says mate never touches a remote and never commits or pushes outside
a checkpoint ref. This tab's _Update from main_ pulls into the checkout through the Mate server
(`useVcsPullAction`), and the _Merge_ of the review its _Review_ opens runs in HQ as the person;
_Push_ is listed above, but the tab renders no Push verb, because the
push is the agent's. Which rule governs a Mate's own checkout, §6's or this tab's, is undecided;
neither section changes until it is.

The **project's flow** (`projectFlow.ts`, mate 0.11.16) is read once for the whole account
(`ZeropsProjectFlowProvider`, every sixty seconds and at once after a verb; since 2026-10-02 its
changes come down HQ's structure stream, never polled, and the Gitea group forge reads only
releases) and, since D29, drawn
through `groupFlow` (§5.4) in three places, in the one order: Mates → pull requests → `main` →
production, a group stage a side branch of `main`. The **left menu** shows each project as its
heading and its Mates (2026-09-29). On the heading, production is one chip — the release it serves,
whether it is healthy, what waits to go out — whose menu holds the stages, the public links, what
waits with _Review_ (the release's review) and, while production is in trouble, the fix; a stage is
the chip only where the group has no production, and a group with neither has no chip. Under the
heading the Mates, under each its open changes (HQ records which Mate opened each, and only Mates
open changes; more than three fold behind a count; _Review_ on each). A recipe change — a change in
the application's recipe repository — is never a Mate's row there. No heading
carries a dot: a folded project's heading shows the faces of its Mates that need somebody, stopped
on an error, finished unseen or work.
The **projects screen** has two views of the same flows, the projects that wait on somebody
first (_Next step first_, the default; newest and name remain). **Overview**: a _Next steps_ strip
with one item per project whose step waits on somebody — its words, which jump to the project's
row, then the row's own verb at the item's end (_Review_ for a change, _Review release_, _+ Add
production_), the same verb the row acts with — a _Review_ opens the review (§5.4), and nothing
merges or releases from the strip; a step with no verb to press is not listed. Then one row per
project with work on it, the four steps as columns (`Mates`, `Pull requests`, `main`,
`Production`, no arrows), at most two lines to a cell and one verb to a row, at the end of the cell
it acts on — the one second verb is _Review release_ beside a failed production that has a release
to offer (D28); a group stage is one line under `main`'s, `↳ ● Deployed e014b0e`. A row opens to the
Mate cards, the pull requests, the environments and the project's rows; a project with only a Mate
nobody has spoken to is a tile; the containers no project holds fold into one line of their states
with _Try again (N)_; the tools are one quiet line at the end. **Projects** (`?view=projects`,
`&group=` scrolls to its card): every project with work on it as a card, the next step named in its
header without its verb, its steps side by side with the verb in its step, a "+" beside the `Mates`
label that adds a Mate, and under the steps only what a release would carry and the project's other
environments. In both views an empty step says its word in the muted hand (`None open`, `Nothing
merged`, `Not set up`), never a dashed place, and a Mate opens its conversation from wherever it is
drawn — its chip in a row, the whole tile, its card. A project's rows are why _Review release_ is
not offered when it is not, what a release would carry, the releases with _Roll back to this_ —
which opens the roll back's review — and the recipe changes last.
A project's menu adds a Mate and a stage (_Add stage — optional_) while its
adds are offered, and a production while the role is creatable and the person may create projects;
nothing else on the page adds a stage, and _Add production_ is otherwise only the next step's verb,
with "Production is added here, not by the Mate." as its tooltip, never a row asking for a missing
tier. The **Git page** (`/git`, _Git_ in the account menu) lists every application whose changes
the person may read, its repositories and the changes open on each, newest first, across the
account; a change's title opens the change, a repository's name opens nothing (`routes/git.tsx:7`,
`SidebarZeropsAccount.logic.ts:85-90`, `ZeropsGitPage.tsx:1-18`). Everything on it is HQ's: each
application's repositories read as the person (`GET /api/apps/:appId/repos`) together and again
every minute while the page is open, the changes down HQ's stream; an application whose read fails
keeps what it read and names why, and never reads as "no repositories"
(`useZeropsAppRepos.ts:1-17`).
A repository's row still counts its changes as "open pull requests" (`gitRepositoryLine`,
`gitOverview.ts:35-39`). Superseded 2026-10-02: a **Gitea overview** (`/gitea`, the footer's Gitea
button) listed every Gitea repository the person could reach and the pull requests open on it.
`gitOverview.test.ts` — "lists each application by name, its repositories by name, their changes
newest first", "names each change by its number and its Mate, under its repository's row";
`ZeropsGitPage.logic.test.ts` — "leaves out an application whose changes the person may not read",
"names why a read did not answer, beside what was read". What an environment runs is the sha in the
app version's name, read from Zerops; what is open is HQ's (since 2026-10-02), and what was released
is still Gitea's.

**Release** (`release.ts`): per service, what the stage runs against what production runs, read in
the release's review before anything is tagged; its button (_Release v0.1.57_) creates a tag
`v{semver}` on the group repo as the person, its message listing each service's full sha and
nothing that is not one; the broker judges it on the pusher's production rights (§10.8). A
**rollback** is a release: _Roll back to this_ on an earlier approved release opens the roll back's
review, which names the version it goes back to and the tag it makes, and its button (_Roll back to
v0.1.55_) creates a new tag carrying that release's message verbatim; a tag name is never reused and
`/deploy` takes no ref. Neither button takes the focus or ⌘↵, and the review follows its tag until
production runs it — "Released", "Rolled back", or the failure and its fix. Built and unit-tested;
the first live release is still to run. The release moves to HQ next.

### Invariants

| ID    | Invariant                                                                                                                                                                                                                                                                                                                                                                                         |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MB-1  | The door takes a throwaway and nothing else: any grant, any flag, another name, another org, a stale `created` or an inactive creator is refused, and the role comes from the role function with the Mate's own key. `ZeropsThrowawayIdentity.test.ts`, `ZeropsIdentityGate.test.ts`; `EnvironmentAuthPolicy.ts` offers `zerops-throwaway` only.                                                   |
| MB-2  | The client mints, connects and deletes in that order, and deletes even when the connect threw; the mint carries no grants and no flags. From the fork's slice 0.7 the mint checks no role, and the delete is `deleteThrowaway` with the minting token: not cancelled by the exchange's abort, never clearing, refreshing or borrowing the current session, refusing any name that is not a throwaway's. From slice 2.4 that mint is an `account-write` that a closed verification window does not hold up, and a mint that grants a project stays a `project-write`. `doorThrowaway.test.ts` — "mints, connects and deletes, in that order", "deletes even when the connect threw, and re-throws what threw", "sweeps every stale throwaway in one pass"; `zeropsThrowaway.test.ts` — "mints with no grants and no flags, under the name it was given"; slice 0.7 adds a mint during a renewal proceeding, an abort after the mint still deleting, a delete's `401` after sign-out and a new sign-in leaving the new session alone, and a `BASIC_USER` and an overridden `READ_ONLY` member minting; slice 2.4 adds "a door throwaway mints while project writes are closed; a project-granting mint is refused" and a rights-less mint whose answer is lost reading as uncertain. |
| MB-3  | A session ends when the Mate's own re-check says so, and nothing renews a Zerops session. `ZeropsMembershipWatch.test.ts` — "leaves sessions that did not come from the Zerops door alone"; `credentialRenewal.ts`.                                                                                                                                                                                 |
| MB-4  | The two role functions answer every fixture identically, and the fixture covers every outcome. `zeropsRoles.test.ts` — "carries every case the Go twin replays"; gitea-mate `TestComputeAgainstFixtures`, `TestFixturesCoverEveryOutcome`, `TestReleaseWithoutProduction`.                                                                                                                         |
| MB-5  | Only the recorded signer's session starts a turn on an OAuth-signed agent; a signer the org no longer knows is signed out; an unreadable member list signs nobody out. `ZeropsProjectSigners.test.ts` — "goes by who this server saw sign the login in, and nobody else", "signs out the agent whose signer the org no longer knows", "signs nobody out when the member list could not be read"; `agentOwnership.test.ts` — "says only the signer runs somebody else's agent". |
| MB-6  | The registry round-trips and is stable, and registering a Mate writes the registry entry first and then the broker's grant — a failed registry write gives the broker nothing. `groupRegistry.test.ts` — "round-trips a registry it read", "is stable, so a registry that has not moved writes the same document"; `brokerGrant.test.ts` — "writes the registry entry, then gives the broker the Mate's project", "stops at a registry write that failed and gives the broker nothing". |
| MB-7  | Isolation moves the key onto the container and never deletes an entry without re-reading its id; a project with no container loses the key outright. `projectIsolation.test.ts` — "closes a Mate's project and moves its key onto the container", "never deletes without re-reading the entry's id first", "drops the key outright where there is no container to move it to".                     |
| MB-8  | A registered Mate's access is delivered by a pass, once, with no restart; older token generations wait for the grace and one generation is never revoked; there is no credential route. gitea-mate `TestPassDeliversAMatesAccessOnceAndNeverRestarts`, `TestOlderTokenGenerationsWaitForTheGrace`, `TestOneGenerationIsNeverRevoked`, `TestThereIsNoCredentialRoute`.                                 |
| MB-9  | A read that fails or comes back partial writes nothing, and a plan over the cap is reported, not applied. gitea-mate `TestAReadThatFailsWritesNothing`, `TestSearchProjectsRefusesAPartialPage`, `TestAPlanOverTheCapIsReportedNotApplied`.                                                                                                                                                         |
| MB-10 | Production deploys the newest approved tag read from the statuses, a refused tag stays refused on redelivery and after a restart, a release is judged on its pusher, a deploy proves its caller, and the loop catches up. gitea-mate `TestNewestApprovedReadsTheStatusesNotTheTagList`, `TestARefusedTagStaysRefusedAcrossBothPaths`, `TestAReleaseIsJudgedOnItsPusher`, `TestDeployStatusProvesItsCaller`, `TestTheLoopCatchesUp`. Retiring with the broker (2026-10-02): production deploys move to HQ next. |
| MB-11 | A tier is imported only after conversion, and a release tag carries only full shas. `recipeTierImport.test.ts`; `recipeTier.test.ts`; `release.test.ts` — "drops anything that is not a full sha rather than writing a tag the broker refuses", "round-trips: what it writes is what it reads".                                                                                                    |
| MB-12 | The Git tab offers one verb per block from the checkout's facts and never answers with the production; an open pull request's verb is _Review_, whatever Gitea says of it — the review, never the tab, merges (§5.4). `gitTab.test.ts` — "a dev pair with no repository yet says so, and offers nothing", "an unpushed branch offers Push, and only Push", "never answers with the production, whose source is a release", "offers the review of an open pull request whatever Gitea says of it (%s)". Since 2026-10-02 the block reads the Mate's newest change in the repository from HQ, open or landed last, and offers no _Open pull request_: a Mate's push opens its change. |
| MB-13 | The Gitea import document the app sends is gitea-mate's, byte for byte. `giteaRecipe.test.ts`. Retiring with the Gitea (2026-10-02). |
| MB-14 | zcp hands its key to no forge and no app container, and writes no workflow into a Mate's repositories in HQ (2026-10-02: the `.gitea` workflow and its refresh are gone; deploys from HQ are HQ's). zcp `workflow_build_integration_citoken_test.go`, `deploy_ssh_test.go`.                                                                                                                                             |
| MB-16 | The app's Gitea session is acquired from the broker by a throwaway named for that Gitea, once per Gitea however many surfaces ask, kept in memory for one account lifetime and forgotten when the account closes (the fork's slice 0.1). From slice 0.13: a `401` re-acquires, and a third `401` in 10 minutes refuses; a refusal is asked again every 5 minutes while the tab is visible and a surface wants it; "Gitea still setting up" and "broker unreachable" are separate waits, each re-mint preceded by a credential-less request to the broker, and nothing is minted while the broker does not answer; `expiresIn` is honoured and the token renewed only while wanted; dependent facts keep their values through a `401` and show the cause after two failed acquisitions. Refusal retries stay at or under 12 an hour and Gitea mints at or under 4 a minute per tab. `giteaSession.test.ts` — "acquires a token from the broker by throwaway, once, and keeps it for the tab", "sign-out, another person signs in on the same tab: no Gitea request carries the first person's token" (slice 0.1), and until slice 0.13 replaces them, "says what Gitea refused, in Gitea's words, and does not retry it", "names a refusal in the person's terms and does not retry it by itself" and "forgets the session on the first 401 Gitea answers, so the surface acquires again", which pin today's no-retry and forget-on-first-`401` behaviour; `forge/giteaSession.test.ts` (slice 0.13) — the session machine's transition table in the fork's `docs/internals/zerops/client-state-model.md`; `giteaBroker.test.ts` — "asks the broker with the throwaway as the bearer, and keeps what it answers"; gitea-mate `TestAPersonGetsATokenThatActsAsThemAndAnAccountBoundToTheSource`, `TestAPersonWhoIsNotAnActiveMemberGetsNothing`, `TestStaleAppTokensAreRetiredAndNeverCounted`, `TestAGiteaRefusalIsAnsweredInItsWordsNotAsStillSettingUp`. Retiring with the Gitea (2026-10-02): a person reaches HQ through its door (§10.4). |
| MB-18 | Gitea serves only with its `zerops` login source; a boot that cannot add it is re-run, never served. gitea-mate `TestStartRefusesToServeWithoutTheZeropsSource`. Retiring with the Gitea (2026-10-02). |
| MB-19 | A recipe pull request a registered Mate's bot opened on the group repo is merged by the rights loop when every file it changes is added — one that modifies, removes or renames a file `main` carries waits for a person with write, who merges it or asks a Mate to (D30, D31) — and nobody else's is; `main` on the group repo keeps no merge whitelist and takes no direct push. Every registered Mate's bot is a collaborator with write on the group repo from the pass that makes it, a Mate registered before D31 from its next pass with no token minted; a bot whose Mate leaves the group loses it, and a deleted Mate's bot keeps it, retired (D31). gitea-mate `TestAMatesRecipePullRequestIsMergedAndNobodyElses`, `TestAMatesRecipePullRequestIsMergedByThePass`, `TestAMatesRecipePullRequestNudgesTheLoop`, `TestAMatesRecipeMergesOnlyWhenItAddsFiles`, `TestAMatesRecipeThatRewritesATierWaitsForAPerson`, `TestGroupRepoProtections`, `TestEveryRegisteredMateWritesItsGroupRepo`, `TestPlanBuildsAGroupFromNothing`, `TestPassBuildsAndThenChangesNothing`, `TestAMateServedBeforeD31WritesItsGroupRepoOnTheNextPass`, `TestABotWhoseMateLeftTheGroupLosesItsWriteOnTheGroupRepo`, `TestAProjectMissingFromSearchAndNotFoundTwiceIsExcludedAndItsBotRetired`, `TestCollaborators`. Superseded 2026-10-02: in HQ Core lands a Mate's recipe change by itself when it only adds files, a person merges one that changes a file, and Core closes one that adds nothing; live 2026-10-02, `group` #1 landed by Core right after the adopt pass proposed it. |
| MB-20 | A re-read of the inventory keeps what the reads hold: no published state loses a member or goes back to unread, and the page paints from the list already read. `runtime.test.ts` "re-reads an organization's inventory on a fresh receiver and keeps what it holds"; `ZeropsProjectsPage.test.ts` "keeps an empty organization's invitation up while its list is re-read". |
| MB-21 | A tier reads whatever its indentation, and a `buildFromGit` that opens an item converts to `startWithoutCode` with its dash kept; the import's project block is rewritten at the recipe's own indentation, one mapping. `recipeTier.test.ts` "reads four-space items and converts a build that opens its item", "replaces the name at the block's own indentation and keeps the rest of the block". |
| MB-22 | The Git tab's _Open pull request_ runs in Gitea as the person, onto the repository's default branch the block carries, and its _Review_ opens the change's review, whose _Merge_ runs in Gitea as the person; the forge is read again once the verb settles. `gitTab.test.ts` "carries the repository's default branch, main until Gitea says"; `ZeropsReviewDoors.test.tsx` "every door opens the review and never acts itself (R1)". Superseded 2026-10-02: there is no _Open pull request_, and the review's _Merge_ and _Close without merging_ run in HQ as the person, offered by HQ's rule; HQ's stream brings the change back, nothing is read again (§5.4). |
| MB-23 | A stage and a production run no agent unless the person says so; only a dev environment is a Mate by default. `createEnvironment.test.ts` "gives $role an agent". |
| MB-24 | A new birth carries its own group writes — the registry, the broker's grant, the deploy token, the declaration — as its `tags` and `registry` steps (§4.4), so an organization switch, leaving the page or a reload after create-accepted never strands them. A stage or a production half-made by a birth on another device or by an older build is finished by an account worker acting only on complete known inputs (the fork's slice 4.7; until then the projects page finishes it on its next read). Either way the declaration write declares nothing twice and reuses a branch or request an earlier attempt left. `zeropsBirths.host.test.tsx` "org switch after create-accepted still finishes tags and registry"; `groupEnvironments.test.ts` "halfMadeGroupEnvironments"; `addGroupEnvironment.test.ts` "declares nothing twice…", "reuses the branch it left…", "reuses the request it left…". Superseded 2026-10-02: the group writes are the press's `register` step (§4.4), run before the press returns — a Mate's record attached to its application in HQ, then its birth; a stage or a production attached as its tier, then its deploy key — each safe to ask again: an attach HQ holds already stands, and a key is minted only where HQ holds none that works; a press that stopped resumes there with _Try again_, and a half-made Mate's _Finish setup_ runs it in any browser. A stage or a production whose attach or key a reload lost is finished by the projects page on its next read, not by an account worker (§10.11). `hqMateBirth.test.ts` "keeps a record HQ holds already"; `addGroupEnvironment.test.ts` "attaches the project as its tier, then hands HQ the key minted for that one project", "mints nothing for an environment whose key HQ holds and finds working"; `runEnvironmentCreation.test.ts` "resumes at the step that stopped, on the project the first press made"; `groupEnvironments.test.ts` "names a stage placed but held as no environment"; `useZeropsGroupEnvironmentReconcile.test.ts` "tries a failed repair again only after its backoff". |
| MB-25 | A second registered Mate asking for a service repository of its group joins it with write, and `POST /mate/repository` refuses the group repository whether it exists or not — it makes and joins service repositories, and a registered Mate's write on the group repository is the rights loop's (D31), which the refusal says without sending the Mate to a fork; an owner's _Add Mate_ registers the Mate with the two writes the card's _Register in {group}_ makes, as its birth's first steps, and a Mate made from the recipe is sent to the group's code on Gitea. gitea-mate `TestASecondMateJoinsAServiceRepositoryOfItsGroup`, `TestRepositoryRefusals`; `brokerGrant.test.ts` "registerMateInGroup"; `creationHandoff.test.ts` "sends a Mate made from the recipe to the group's code on Gitea". Superseded 2026-10-02: in HQ a Mate's `POST /api/mate/repos` makes or joins the application's repository of that name, and zcp never asks for `group` on a pair's behalf (§10.10). |
| MB-26 | A deploy onto a wired pair's stage half commits, opens the change and pushes it with nothing asked of the agent; a dependency directory nobody ignored stops the commit; a change is opened only when HQ says the candidate tree differs from `main` once `main` is taken in, and HEAD is pushed to its branch `mate/<project id>/<n>`, never to `main`; a push to HQ watches for no build and offers no integration; a wired pair's direct deploys are never redirected; a group's stage and production build the stage half's setup — the one a deploy of the stage half recorded, else the one setup the pair's zerops.yaml declares beside the dev one, else its only setup — and a tier that would build a stage setup nothing names is withheld until one does, never given the dev setup (a joining Mate records no stage setup, and production built the dev loop's `zsc noop`, 2026-09-26); the delivery brings the repository's workflow file to the one this zcp writes, a file naming that deploy action left as it is, and gives a change still called `Mate: {hostname}` the task's words; it takes `main` in before it pushes, by merge and never by rebase, so a second Mate stays mergeable after the first lands, and a collision only a person can settle leaves the checkout whole and is named. HQ merges a change by **squash**: its title is the task, so `main` is one commit per task delivered. A squash shares no history with the branch that became it, so before the take-`main`-in merge runs, a delivery first absorbs its OWN change's landing — the squash (`mergedSha`) and the head it squashed (`landedHead`), read fresh from the Mate's state in HQ — as a real merge, never a rebase, never a force, proven lossless by `git merge-tree --write-tree` first, or — whenever that fast path fails for any reason — a portable plumbing fallback (a real 3-way merge into a TEMPORARY index), accepted only on an exact tree match; the conflict handler is a brace group (`|| { …; exit 4; }`), never a nested subshell (live-reproduced and fixed 2026-09-23); unprovable falls through to the ordinary merge unchanged but marked, and a genuine conflict on either merge aborts and is named — the absorb's own S^1 conflict under its own marker, because the recovery differs: proven lossless, `merge S^1`, resolve and commit, then `merge -s ours S`, then take `main` in; unprovable, the same first step but a PLAIN `merge S`. Uncommitted changes touching what the S^1 merge would touch are checked first and marked with their own marker, so silence never reads as "no conflict"; the fix is to commit first. After the recorded landing, the explicit delivery starts its next change from current `main`, keeping the earlier history under `refs/zcp/landed/<mergedSha>` and reporting it; a pass leaves the checkout unchanged, and the write that records or clears a change's outcome is guarded against a concurrent pass having moved the pair onto a newer change. A wired pair's `strategy="git-push"` to HQ absorbs and takes `main` in the same way before it pushes to its change, and a genuine conflict there blocks the push and is reported. zcp `TestAStageDeployOfAWiredPairDeliversItself`, `TestAStageDeployAbsorbsTheMatesOwnMergedChange`, `TestAStageDeployWithNothingBeyondMainOpensNoChange`, `TestADeliveryRefusedByItsGitSaysWhatToDo`, `TestGitPushToHQ_DeliversCommittedWorkAsTheChange`, `TestGitPushToHQ_ABranchOfItsOwnIsRefused`, `TestBuildDeliveryCommand_CommitsTheDeployedTreeAndSaysHowFarAhead`, `TestBuildDeliveryCommand_TakesMainInBeforeAnythingIsPushed`, `TestBuildDeliveryCommand_AbsorbsASquashLanding`, `TestBuildDeliveryCommand_AbsorbsASquashLanding_AColleaguesWorkSurvives`, `TestBuildDeliveryCommand_UnprovableLandingFallsThroughToTheOrdinaryMerge`, `TestBuildDeliveryCommand_ARealConflictAfterTheAbsorbedLandingStillAborts`, `TestBuildDeliveryCommand_ARealS1ConflictAbortsTheWholeChain`, `TestBuildDeliverySyncCommand_ARealS1ConflictAbortsCleanly`, `TestBuildAbsorbLandedChangeCommand_FallsBackToPortablePlumbingWhenMergeTreeFails`, `TestBuildAbsorbLandedChangeCommand_UncommittedChangesBlockTheMerge`, `TestBuildGroupRecipe_GroupEnvironmentsBuildTheStageHalfsSetup`, `TestBuildGroupRecipe_StageSetup_ResolvedOrWithheld`, `TestComposeGroupRecipeInputs_JoinerWithoutStageSetup_BuildsTheYAMLsOtherSetup`. |
| MB-27 | A second Mate joins its group's service repository and works from `main` (live, 2026-09-17); a recipe pull request is opened only for a branch ahead of `main`, and one Gitea calls empty is closed by the broker, never retried; a job's deploy takes a tier's name for the group's only environment of that tier; _Add Mate_ begins the Mate's birth — its registration and its hand-off — as soon as the project exists, a failed later step included. zcp `TestGroupRecipe_OpensNothingMainAlreadyHas` (in HQ: a landed proposal leaves nothing to open); gitea-mate `TestAnEmptyRecipePullRequestIsClosedNotRetried`, `TestDeployTakesATiersNameForItsOnlyEnvironment`; `brokerGrant.test.ts` "registerMateInGroup"; ledger _The whole chain through the UI, from a wiped org_. |
| MB-30 | A release lists what each production repository's `main` holds, stage or no stage, and shows the commits it would carry; the offer carries the entries the tag will list, and no refusal names a stage at all (D28). A Mate's open pull request is offered in its own conversation, as _Review_ at the composer's top, and merging from its review is Gitea's, as the person, onto the head the review showed; the Mate's branch absorbs the merged `main` losslessly, never by rebase, by its own next delivery at the latest (MB-26) — a pass records the landing without changing the checkout, and the explicit delivery starts the next change from current `main`, keeping the earlier history. `release.test.ts` — "a release lists what is merged", "carries the entries the tag would list, so the verb tags what the offer showed"; `groupDeploys.test.ts` — "what a release has to read"; `mateNextStep.test.ts` — "$case" (its cases include "its own change, waiting for the person's review", "the newest where it has two, so the strip is stable"); `ZeropsNextStepBanner.test.tsx` — "opens the change's review from the button that was pressed"; `giteaClient.test.ts` — "merge sends the shown head and squash, so `main` is one commit per task", "throws Gitea's own status and message on anything else". |
| MB-31 | In a Mate enrolled with its HQ launch-production refuses before it reads a scope or writes anything, and its next step names only a change the Mate's fresh state says is still open; a merged one points at the projects page, and none, or one closed without merging, says to deliver through the stage half first; a Mate not delivering through HQ keeps its route. zcp `TestHandleLaunchProduction_HQWiring_RefusesBeforeAnyStep` (the handler, enrolled and not: no SSH read, admin client, staged token or state file before the refusal), `TestLaunchProduction_AMateDeliveringThroughHQRefusesAndSaysWhatIsTrue` (the next step). |
| MB-32 | A project's flow and its one next step are one derivation the projects page, the left menu and a Mate's conversation all draw (D29): worst first, a failed deploy the next step — production's before a stage's — a failed stage never hiding a release, and a failed production keeping the release that might clear it; production "After the first merge" with nothing to press while `main` is empty; a preview only the stage half beside its own dev half; a group stage drawn only where one exists; the conversation's step from the flow — its own mergeable change, as _Review_ at the composer's top, and nothing of the project's. The page lays it out one way: a _Next steps_ item carries its row's own verb (_Review_, _Review release_, _+ Add production_), a _Review_ opening the review and nothing merging or releasing from the page, and a step with no verb to press is not listed; a cell holds at most two lines, a row one verb at the end of the cell it acts on (and _Review release_ beside a failed production); an empty step is a muted word, never a dashed place; _Add stage_ and _Add production_ are the project menu's, never a footer link; a Mate opens from its chip, its tile or its card. The left menu draws production as one chip on the project's heading, a stage only where there is no production, and dots no heading — a folded one shows its busy Mates' faces. `groupFlow.test.ts` — "takes the worst step first: $case", "reads production as $case", "offers Add production only where $case → $addable", "still ranks a failed production above the release", "names production's failure before a stage's, and keeps the release a failed stage does not block", `pairPreviewRoute` "is $case"; `mateNextStep.test.ts` — "$case" (its cases include "its own change, waiting for the person's review", "another Mate's change", "its own change that does not merge"); `OverviewView.test.tsx` — "carries the step's verb %s on each strip item, the row's own verb", "leaves a step with no verb to press out of the strip", "never holds more than two lines in a cell: %s", "puts the verb in the cell it belongs to", "still offers the release beside a broken production, not just the build (D28)", "draws a group stage under main only where one exists — never an empty slot", "reads production as coming after the first merge, with nothing to press", "opens any Mate a row names into its conversation, by a real button in reading order", "opens a tile's Mate from the whole tile, with no Open button beside it"; `ProjectsView.test.tsx` — "names the next step in its header, without the verb", "keeps Add stage and Add production in the group menu: no footer links", "draws each Mate as a row that carries its Preview on its first line"; `flowSteps.test.tsx` — "says its word in the muted hand, never dashed: %s", "holds its verbs at the cell's end, in order"; `projectsView.logic.test.ts` — "says %s awaits somebody: %s"; `SidebarZeropsTree.test.tsx` — "never dots a project for the production it does not have"; `SidebarProductionChip.logic.test.ts` — "$state" (its cases include "no production, only stage"), "draws no chip where there is neither a production nor a stage"; `SidebarProjects.logic.test.ts` — "headingFaces — who a folded project's heading shows (M15)"; `ZeropsReviewDoors.test.tsx` — "every door opens the review and never acts itself (R1)". |
| MB-33 | A Mate proposes only the recipe tiers `main` of the application's recipe repository in HQ lacks (D30): a tier directory `main` has any file in is left whole, the group's first recipe lands whole, a top-level file `main` has is never proposed. The proposal is the Mate's own change in `group`, titled exactly "Mate: the group's import files" with no description, its branch `main`'s tree with the missing files written over it, so it only adds; it moves forward only — a changed composition on its head, with `main` as a second parent once `main` moved — and one `main` has overtaken is brought to `main`'s tree and adds nothing. A group whose `main` has every tier gets no change opened, and the agent's `group-recipe` answers that `main` already carries every tier. Every `buildFromGit` is the pair's repository in HQ. zcp `TestMissing_ATierTheRepositoryHas_IsLeftWhole`, `TestGroupRecipe_ProposedAsTheMatesChange`, `TestGroupRecipe_ProposesOnlyWhatMainLacks`, `TestGroupRecipe_FollowsTheProjectOnTheSameChange`, `TestGroupRecipe_MainMovedUnderAnOpenProposal`, `TestGroupRecipe_AProposalMainOvertookAddsNothing`, `TestGroupRecipe_OpensNothingMainAlreadyHas`, `TestHandleGroupRecipe_Table`. |
| MB-34 | A Mate's description of its change is its change's body in HQ: set on the pair's open change — on record or named by the Mate's state, never opened by describing — kept on the pair while none is open or HQ does not answer and put on the one the pair reaches next, and never put on a change it was not written for (one that merged or closed first is named to the Mate and nothing is kept; words kept for a change that is gone are dropped); every delivery or push that leaves a change open asks for it. The pictures it names are the Mate's own kept screenshots, attached to the change once each and published as `<img alt width height src>` at HQ's address; a picture not kept is refused before anything is written, and one HQ will not keep writes nothing and keeps the words. A wired pair's push credential is brought to the enrollment's before a delivery, a git-push or a pending finish, and a credential refusal marks the pair, which heals once a fresh session authenticates. zcp `TestDescribeChange`, `TestDescribeChange_KeptWordsGoOntoTheNextChange`, `TestDescribeChange_Refusals`, `TestWorkflowTool_DescribeChangeReachesItsHandler`, `TestDescribeChange_Pictures`, `TestKeepBrowserPicture`, `TestADeliveryBringsTheCredentialToTheCurrentOne`. |
| MB-35 | A new Mate stands up from its application's AI Agent tier, read from HQ, in one call (D32): every pair built from the application's repositories in HQ is read by zcp's naming, not its setups' names, adopted as the adopt route records it with the tier's setups, checked out onto the Mate's branch from the repository the tier names (never one HQ would have to make), and deployed every dev half at once, the first call answering once they stand with the stages queued, and on the second call each stage from its dev half once what its build reads stands (with no reads, every stage above it by priority); a stage never called for stays `READY_TO_DEPLOY` — with nothing committed, pushed or proposed; the runtimes the browser is still importing are waited for, bounded; a refusal before anything is touched names the adopt route, a pair that fails stops alone with the model's next step and holds only the halves whose builds read it, which say what they waited for, and a second call skips what is done. Registered in a Mate only, and a Mate's AGENTS.md sends "Stand up development of the project." to it first. zcp `TestStandup_StandsUpEveryPairFromTheRecipe`, `TestStandupAfter_EveryDevHalfStartsAtOnce`, `TestStandupReads_FromTheRecipe`, `TestStandupReads_EveryRouteTheWriterCounts`, `TestStandup_ReturnsOnceDevelopmentIsUp`, `TestStandup_ASecondCallContinuesAndSkipsWhatIsDone`, `TestStandup_TheModelIsTheBackup`, `TestStandup_WaitsForTheRuntimesTheBrowserIsImporting`, `TestParseMateTier`, `TestParseMateTier_PairsOnlyRepositoriesOfThisApplication`, `TestParseRecipeImportShape_RolesFollowTheHostnameConvention`, `TestAdoptPair_RecordsWhatTheAdoptRouteRecords`, `TestServer_StandupToolGating`, `TestBuildAgentsMD_Container_StandUpRoutesToTheTool`; integration `TestStandup_OverMCP_AMateInNoApplicationIsRefusedAndNothingIsTouched`. |
| MB-36 | A kept Mate session is presented only after the descriptor names the expected project and its environment and the Mate confirms it with every scope the client asks for; one the Mate ended or one short of a scope is forgotten and a throwaway opens a new one; no word keeps it and mints; it never waits on the mint pace; the account's close, a refused stored login and a displaced session each end it at its Mate (D33). `keptSessions.test.ts` (client runtime and web), `identityExchange.test.ts`, `exchangeDriver.test.ts`, `signIn.guards.test.tsx`. |
| MB-37 | A group recipe writes each search engine at no less than 2 GB `minRam` and 0.5 GB `minFreeRamGB` on every tier and never lowers a value; an unread scale writes no block; `profileOverrides` and the free-memory buffer are carried; a scaling proposal is the Mate's change in the recipe repository in HQ, written over `main`, rewriting only that host's block in every tier naming it; it refuses a block it cannot splice safely, moves forward on the same change for the same host, brings a stale one to `main`'s tree when nothing differs, and is refused while the Mate's additive proposal is open there, which in turn leaves an open scaling proposal alone; the steer speaks only after a change that finished (D34). `group_floors_test.go`, `host_scaling_test.go`, `group_profile_overrides_test.go`, `scale_test.go`; zcp `TestGroupRecipeScaling_SteersAndProposesOneHostsBlock`, `TestGroupRecipeScaling_ProposesFromMainsHeadEveryTierFresh`, `TestGroupRecipeScaling_OneOpenChangeInTheRecipeRepository`, `TestScratch_File`. |
| MB-38 | A Mate reads from its first turn, even on an empty project, that its code and changes live in its application's HQ, bootstrap comes first and ZCP wires the pair's repository once the service stands, Core deploys the application's environments, and the person is never asked for a GitHub or other repository, URL or token. The block is Mate-only; a plain container or local install reads none of it. This keeps upstream's repository-first guidance while replacing its Gitea workflow with HQ's jobs (§10.8). zcp `TestBuildAgentsMD_Container_GitHostSaysWhereTheCodeLives`. |
| MB-39 | A person may merge an open HQ change while the agent works; stage deliveries and git-pushes tell the agent that the next stage deploy absorbs it and proposes only what main lacks. A delivery reports the previous change before the next one, without promising a change it already opened; a closing session hands over only an open change. Production guidance reads merged history from HQ after the local landing was absorbed, and a failed read stays unknown with a manual ask again. This retains upstream's merge-any-moment and empty-proposal decisions on HQ's tree comparison (§10.10). zcp `TestAStageDeployAbsorbsTheMatesOwnMergedChange`, `TestAStageDeployOfAWiredPairDeliversItself`, `TestGitPushToHQ_DeliversCommittedWorkAsTheChange`, `TestSessionAnnotations_HandoffOnlyAfterADelivery`, `TestProductionKnowsMergedWorkAfterTheMergeIsAbsorbed`, `TestProductionGuidance_HQUnavailableIsUnknown`. |
| MB-28 | A pull request belongs to the Mate whose branch it is (zcp's `mate/{login}`) or whose bot opened it, a person's own is listed after the Mates and never dropped, a group repo's is a recipe change whoever opened it, and a roll-back is offered only to an earlier approved release, its _Roll back to this_ opening the roll back's review. `projectFlow.test.ts` — "whose pull request it is", "puts each Mate's under it, newest first, and the rest after the Mates", "is a recipe change on the group repo, whoever opened it"; `release.test.ts` — "a release's row"; `ZeropsReviewDoors.test.tsx` — "Roll back to this opens the roll back's review, from the row pressed"; `SidebarZeropsTree.test.tsx` "the project's flow under it"; `ZeropsGitPanel.test.tsx` "is this Mate's repositories and nothing of the project's". Since 2026-10-02 a change in HQ is the Mate's that opened it (only Mates open changes), and a change in the application's recipe repository is a recipe change (`RECIPE_REPO`), the proposal known by zcp's exact title. |
| MB-29 | A deploy token reaches a job only when the job is proved, runs the default branch's workflow from the repository itself, holds the commit protected state wants on that environment, and its runner has run nothing but such jobs since it was made; a superseded or already-live commit gets no token and no failure; the job pushes the commit's tree (`--workspace-state clean`), never the working directory. gitea-mate `internal/server/deploy_test.go`, `internal/pipeline/grant_test.go`, `internal/pipeline/runner_test.go`, `actions/deploy` script test; zcp `workflow_build_integration_test.go`. Superseded 2026-10-02: HQ keeps an environment's deploy token and deploys with it itself; no job holds one (§10.8). |
| MB-17 | A Gitea and its broker answer every browser origin, since every call carries a bearer and no cookie: the import sends no origin list and `POST /person/token` answers `*`. `giteaRecipe.test.ts` — "sends no origin list: a Gitea answers every origin, since every call carries a bearer"; gitea-mate `TestGiteaProjectImportCarriesNoOriginList`, `TestPersonTokenAnswersEveryOrigin`. Retiring with the Gitea (2026-10-02). |
| MB-15 | Live: from an emptied org, one _New project_ yields Gitea, the registry, a Mate on its lowered key, and the three variables delivered by the loop with nothing restarted; a merge deploys a stage through the webhook. Ledger 2026-09-16 _The backbone's first live run_, _A real Mate through the backbone_; 2026-09-17 _D20 driven end to end_. History: the Gitea backbone's live runs. |

Open (kept in the primer until they land): a live rollback, the app's leaver flow, the runner's
cross-org proof and the Gitea restore, adoption, _Set up Mate_ and zcp's delegated launch removed,
the demo account migrated. Joining from the recipe and the release ran live on 2026-09-17.
