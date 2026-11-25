# Spec: Zerops Mate (`mate`)

Zerops Mate — `mate` in code, paths and unit names — is a fork of the open-source T3 Code that rides inside the `zcp` container. Its server runs
next to nginx and code-server and spawns the coding agent (Claude Code, Codex) with ZCP's MCP
tools attached; its client — web, desktop, mobile — is the product surface a Zerops user signs
into. Because the agent operates the project through the same `zerops_*` tools an agent in a
terminal uses, mate's UI is not a second control plane: it is a **reader** of what those tools
report. That reading contract is what this spec owns.

The zcp-facing seam is described here without requiring private documents. The companion HQ
architecture lives at `docs/architecture/README.md` in the private `zeropsio/mate-hq` repository
when that document is available.

- Boundaries — the three rules every later section obeys, who owns which fact, and the closed
  list of mate↔zcp touchpoints — §0.
- Delivery — how the mate bundle gets into the container, the supervised process, nginx, the
  `/mate/` base path, readiness — §2.
- Trust and credentials — the credentials the `zcp` container holds, what zcp and the Mate's server
  present and never forward; the model is HQ's — §3.
- What zcp gives a Mate's surfaces — the browser and the call that holds it, the data console,
  vault asks and the Mate's own vault, pages, the origin and mirror asks — §5.
- Git — each mounted dev service is its own repository, reached over a multiplexed SSH
  connection rather than the sshfs mount — §6.
- Agent sign-in — the platform flag zcp and the Mate server share, and the agent configs zcp
  writes — §8.
- Related: `docs/spec-workflows.md` (the envelope/plan/atom pipeline that produces the
  state), `docs/spec-work-session.md` (per-PID session, compaction survival).

---

## 0. Boundaries

Four rules bound every later section. A section that describes another mechanism is superseded
by the ownership table below.

1. **Identity is the client's.** The user's Zerops token exists only in the client. The client
   reads and writes the platform with it: organizations, projects, services, processes, subdomains,
   user-data, and the platform's push channel. HQ sees the access token at sign-in and renewal, for one
   identity read, and in the body of a verb that decides on a role, for that verb's one fresh read,
   and keeps nothing (§3); a Mate's server never sees it. The container's project
   key is the project's: zcp operates the project with it, and the mate server uses it for its own
   flag write (§8.1) and presents it only to HQ, in its link's first frame (§3.5).
2. **Mate's clients reach the container only through the mate server.** Threads, the agent, git,
   the browser viewport. code-server and SSH are the platform's own doors and stay outside mate.
3. **zcp does not distinguish its caller.** Two entry points, stdio MCP and CLI. No layer exists for
   mate; mate adds no dependency to zcp. zcp knows mate as a unit it installs and supervises (§2),
   as a reader of the envelope it already emits (§1), and as two CLI subcommands mate spawns. `zcp
   studio watch` is the Zerops Studio extension's transport; mate does not consume it.
4. **Recover automatically; never let a clock or a hidden fix stand in for an owner's answer**
   (the owner, 2026-10-05: "problém mám s automatickými opakováními jako třeba to, že se na FE
   nastavilo a čekalo, že něco proběhne do 30 min, nebo že se magicky dělo něco v Gitee — ne to, že
   se něco automaticky opakuje a vyrovnává se stavem, který vzniká, a je to jeho legitimní řešení,
   jako že se automaticky obnoví session"). Wanted, in the client, the server and zcp alike:
   recovery with clear logic — renewing a session, reconnecting, re-subscribing, re-reading, running
   an idempotent step again after a transient failure (the network, a timeout, a 5xx, the network
   back, a tab woken) — bounded in rate, not in count (a backoff up to a steady cap), paused while
   nothing needs it, visible while it runs ("Reconnecting…", with a way to try now), and ended
   only by a definitive refusal, visibly, with a manual _Try again_. Forbidden: a clock
   standing in for an owner's answer ("after 30 min assume it ended"); a side effect nobody asked
   for — a timer that silently compares and fixes, the Gitea era's magic; retrying a definitive
   refusal; and repeating a non-idempotent side effect without first reading its own handle.

Every fact has one owner and one path to the client:

| Fact                                                   | Owner                                                                                      | Path to the client                                                                                                  |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------- |
| identity                                               | Zerops API, user token                                                                     | client; HQ at sign-in and renewal (§3)                                                                            |
| membership, roles                                      | Zerops API: the person's token for the client and HQ's verbs, the project key for the Mate | client over the Zerops websocket (§3); HQ per verb, one fresh read (§3), and per ticket (§3) |
| what exists in the project, its status, its subdomains | Zerops API, user token                                                                     | client projection                                                                                                   |
| live platform observations                             | native platform websocket plus direct bootstrap/repair, user token                         | shared client read model                                                                                            |
| what the platform is doing now                         | Zerops API, user token                                                                     | client overlay                                                                                                      |
| where the agent is in the workflow                     | the envelope in a tool result                                                              | mate server reducer → lifecycle feed (§1)                                                                           |
| which services are mounted                             | the container's mount table                                                                | mate server (§6.1)                                                                                                  |
| agent sign-in                                          | the agent CLI's own status + the platform flag                                             | mate server (§8)                                                                                                    |
| thread state                                           | mate server, `~/.t3`                                                                       | mate server                                                                                                         |
| the agent's browser                                    | zcp, in the container                                                                      | the agent via `zerops_browser`; the user via the viewport stream relayed by the mate server (§5.6)                  |

Touchpoints between mate and zcp, closed list:

- the envelope carried in tool results (§1);
- the Mate's lifecycle (§2.1): zcp starts the server with its configuration (§2.3, §2.8) and stops
  it; asks it "may I restart you now?" on its loopback updater (§2.1d: yes stops intake and, without a
  restart, reopens after a bound; not now); and observes its process, its boot file
  (`ZCP_MATE_BOOT_FILE`) and its version on the local port (§2.1a). The server refuses to start on
  state a newer version wrote; zcp never reads or touches its state;
- `zcp` argv the server runs, a closed list (MA-6): `zcp mate status` (what is installed and
  available, for the screen) / `zcp mate update` (a person's "update now", §2.9 MU-2), `zcp studio
  console serve`, the loopback data console the mate server hosts and brokers (§5.7), `zcp version`,
  `zcp lease acquire` (a crew writer's copy, `docs/spec-workflows.md` §8 DP-39),
  `zcp lease release --lease-id <id> --json` (a crew writer's copy at removal, DP-40, gated on the first zcp release with it),
  `zcp lease release --branch` (a conversation's archive, `docs/spec-workflows.md` §8 DP-35) and
  `zcp self restart` (§2.9 step 7, MU-4), and `zcp git <host> ls-remote` for a checkout's remote
  health, run in the Mate's container, never by git in the dev service (§6.8); the agent sign-in flag it writes through the Zerops API
  (§8.1);
- the agent-browser daemon's published stream port, `~/.agent-browser/default.stream`, on
  localhost — read by the mate server, never written by it, unknown to zcp as a mate concern;
- two conventions mate reads: the `ZCP_AGENT_OAUTH_*` / `ZCP_AGENT_TOKEN_*` flags and `ZCP_API_KEY`
  values, and the presence of `MATE_SETUP`, in the platform env store (§8.1, §3.5, C-7; the marker
  checks variable names only), and the
  `/var/www/<host>` sshfs layout (§6.1);
- files the Mate writes and zcp reads:
  `~/.zcp/hq/observe-credential` for Observe (§2.8; zcp
  `internal/hq/observe_credential.go`); `~/.zcp/hq/state.json`, its state as its link last heard it
  and whether the link is up (§2.8; zcp `internal/hq/mate_state.go`);
  and `~/.zcp/hq/state.lock`, held by `zcp service start mate` while it supervises the server.

Anything else is a violation and needs this section changed first.

### Invariants

| ID | Invariant |
|---|---|
| MA-6 | The Mate server spawns `zcp` with no argv outside the closed list `mate status`, `mate update` (§2.9 MU-2), `studio console serve`, `version`, `lease release --branch <branch> [--repo <repo URL>] --json` (`docs/spec-workflows.md` §8 DP-35: on a conversation's archive, never `--force`, gated on `zcp version` ≥ 9.223.0), `lease acquire --pool <pool> --branch <branch> [--repo <repo URL>] --json` (DP-39: a crew writer's copy, at Apply, gated on `zcp version` ≥ the first release with it), `lease release --lease-id <id> --json` (DP-40: a crew writer's copy at its removal, gated on the first zcp release with it), and `self restart --json` (§2.9 step 7, MU-4: once the Mate has drained, gated on `zcp version` ≥ 9.226.0). mate lint rule `t3code/zcp-verb-list`. |
| MA-7 | The Mate server sends `ZCP_API_KEY` only to HQ, in the first frame of its link, and to Zerops for project and organisation reads and flag writes (§3.13, §8.1); it never forwards it anywhere else. |

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

A Mate's envelope carries `"mate": true` (`runtime.Info.MateEnabled`); only zcp's atom selection
reads it, and the Mate server's decoder ignores it, so the key is additive and needs no Mate change.

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
| `zerops_lease` (acquire, status, release) | JSON | `envelope` key — built after the operation, so the reducer sees the new lease set at once |
| `zerops_workflow` bootstrap `start`/`complete`/`skip`/`status` | JSON | `envelope` key |
| `zerops_standup` | JSON | `envelope` key |

The three prose carriers route through `tools.statusResult` / `tools.briefingResult` /
`tools.withFreshEnvelope`; the first two render and append from the *same* envelope, so the
markdown and the machine-readable state can never describe different moments. The JSON carriers
each call `tools.freshEnvelope` **after** their mutation has succeeded, so the envelope describes
the state that mutation produced.

Only `action="status"` carries the work session's whole deploy and verify history. Every other
carrier keeps each host's last deploy and last verify attempt, and every host that has one
(`tools.replyEnvelope`): Mate reads only the last attempt and which hosts have any, and the
recovery primitive still has the rest. `TestEnvelope_AttemptHistory_OnlyStatusCarriesItWhole`.

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
headroom and needs no budget of its own — for a small project. What a project's services cost the
rest of the answer counts against that budget (`workflow.GuidanceBudget`), on `action="status"` and
develop's start alike: the per-service and lease lines rendered above the guidance, and the envelope
block's bytes beyond a small project's 2 KB; the base-prefix rule those lines refer to is said once
beneath them. On 2026-10-27 develop's start, which applied no budget, answered 38–55 KB in three
Mate sessions, one read back from a file past the client's display limit; a six-part pool project's
start now renders 28.7 KB (a 30.7 KB frame) with its dev-pool loop whole, where it rendered 51.6 KB
(`TestDevelopBriefing_MultiPartPoolProject_UnderTheCap`,
`TestHandleDevelopBriefing_ManyPairs_StaysUnderTheMCPCap`). `TestAppendEnvelope_BlockSizeBudget` pins it, with eight
leases (§2.8a.3) on top of the four-service fixture (~3.1 KB fenced). JSON
results are far smaller than the prose ones and are nowhere near the cap.

### 1.6 Why not `structuredContent`

The Go MCP SDK marshals a non-nil typed handler output (a handler's second return value) into
the JSON-RPC result's `structuredContent` field, *alongside* the text content. zcp keeps the
envelope and all model-facing guidance together in the text content, so a provider's treatment
of `structuredContent` cannot separate the envelope from that guidance.

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
knows about mate lives in `internal/mate`: stdlib, the Go-maintained
`golang.org/x/mod/semver` comparator, and the existing `runtime`, `schema` and `hq` seams.

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
| code-server settings | the template, byte for byte — no `"git.enabled": false` (§6.8); a container whose flag is turned off converges back at its next `zcp init` (`TestConfigureVSCode_MateTurnedOff_GitSettingRemoved`) |

`zcp init` is the reconciler for all of it, and it converges **both** directions: turning the flag
off and restarting is a supported operation, not a state zcp only knows how to enter.

### 2.1 Three jobs — prepare, start, keep

zcp has three small jobs for the Mate, and one owner per decision.
This section covers zcp's part in a Mate's birth, while
`zeropsio/mate-hq:docs/architecture/lifecycle-and-runtime.md` traces the birth across the web,
HQ, the platform and zcp and names its three ends (deployment active, HQ admission and first
usable screen). The deployment is active once `zcp init` returns, before the start job has
installed or launched anything.

| Job | Runs | Does |
|---|---|---|
| prepare | `zcp init`, once per container boot | writes the server's configuration, creates the unit and the keep job's timer, starts the unit; installs and repairs nothing |
| start | `zcp service start mate`, the unit's command, at every start of the unit | the only installer and the only writer of `current`, `last-good` and the failed version: starts the version to run and judges whether it came up (§2.1a) |
| keep | `zcp mate fuse`, the timer every 5 minutes, and `zcp mate update` at once | decides when the Mate restarts, nothing else (§2.1d) |

**Prepare** is init's last step, **Zerops Mate (mate)**, after Agent adapters. It is registered only
when there is something to reconcile — `MateEnabled`, or a leftover unit file a now-off flag has to
remove — so a container that never had mate prints not one extra line (§2.0). Enabled
(`reconcileMate` → `enableMate`):

1. **Refuse without a project.** `runtime.Info.ProjectID` empty ⇒ degrade — a non-empty project id
   is the sole signal the server binds to a Zerops project.
2. **Environment.** `~/.zcp/mate.env` rewritten (mode 0600) every boot — §2.3. The server reads it
   at its next start.
3. **Unit.** `mate.UnitFilePath` absent ⇒ `sudo -E zsc unit create --enable=false --start=false mate
   "zcp service start mate"`; every enabled init then runs `sudo systemctl disable
   zerops@mate.service` (a failed disable is a named line, not a stop) and writes the keep job's units
   (§2.1d). When systemd says the unit's file changed on disk since it loaded it (`systemctl show
   --property=NeedDaemonReload --value zerops@mate.service` answers `yes`), `sudo systemctl
   daemon-reload` before the start; an unchanged unit is never reloaded
   (`TestRun_Mate_UnitFileChangedOnDisk_ReloadsSystemd`). `eval/scripts/mate-dev-push.sh` reloads by
   the same rule before its own restart.
4. **Start.** `sudo systemctl start --no-block zerops@mate.service`, requested even when the step
   failed, provided a unit is registered: starting an active unit is a no-op, so a running Mate is
   never restarted by init.

The step is **best-effort** (`step.degraded`): `zcp init` is a `run.init` command, and mate must
never take a container start down with it. The unit file's presence, not a `zsc unit` upsert (there
is none), is the idempotency check.

### 2.1a The start job — the version to run

Each version is its own complete npm prefix, and links name the live one and the last one that came up:

```
~/.zcp/mate/versions/0.2.0/node_modules/.bin/mate
~/.zcp/mate/versions/0.3.0/node_modules/.bin/mate
~/.zcp/mate/current   -> versions/0.3.0        # relative target, what the unit starts
~/.zcp/mate/last-good -> versions/0.2.0        # the newest version that came up under this zcp
~/.zcp/mate/failed-version                     # text: the newest version that did not come up
~/.zcp/mate/manifest.json                      # the release list as last read, for status --local (§2.1c)
BinPath() = ~/.zcp/mate/current/node_modules/.bin/mate
```

`InstalledVersion()` reads `current/node_modules/zerops-mate/package.json` — npm's own record. These
are the only durable facts: two links, one failed version, the downloaded versions, the release
list. There are no phases and no "update in progress" record: whatever a crash or a container
restart interrupts, the next start does what every start does.

**The version to run** (`mate.VersionToRun`) is one function, read by the start job and by the keep
job: the release the release list names when it is newer than `last-good`, newer than the failed
version and not older than `current`; otherwise `current`, or `last-good` when `current` is missing or
older. It never moves `current` back: a release list that is missing or goes backwards changes
nothing, and a `current` newer than `last-good` (a start that was cut short, or an update the previous
zcp left) runs and is judged; only that verdict returns it to `last-good`, never below `MinimumMateVersion` (step 4). A dev build in `current` (`<base>-dev.<sha>`, what
`eval/scripts/mate-dev-push.sh` installs) at or above `MinimumMateVersion` is kept while a dev pin is
active, and without one only until the release list names a release newer than its base; from then
the rule above decides, as for any `current`.

**The dev pin** (`~/.zcp/mate/dev-pin.json`, `{since, until, reason}` in RFC 3339 UTC) is the one
permission for a hand-placed build to stay. There is one per container, covering every dev build in
it: a dev Mate in `current` and a zcp binary no release names (a dev or git-describe build).
`zcp mate pin --for <duration> --reason <text>` places it (`--for` defaults to 24 h and is at most
7 days, longer is refused; the reason is required; placing it again replaces the record, `since`
now), `zcp mate pin --end` removes it and `zcp mate pin` shows it; nothing else writes it. It is
active while the file reads and `until` is in the future; an ended, missing or unreadable pin is no
pin. Only its time or `--end` ends it: an update press, `zcp mate update` and a restart of the Mate
do not, and ending it does not by itself return to a release. A dev build's **base** is its version
up to the first `-` (`v9.256.0-16-g14dcbe7d5` → `v9.256.0`, `0.16.211-dev.959f81` → `0.16.211`); a
base that is not a release (bare `dev`, a commit) is older than every release. Under an active pin
every dev build stays; without one a dev build stays only until a release newer than its base is
known, and then has no standing of its own: the version to run decides for the Mate, and a dev zcp
reads `behind: true`, so the keep job asks for the container restart that installs the release
(§2.1d), under the organization's policy like any release. The floor outranks the pin. A container
restart replaces a dev zcp with the latest release whatever the pin says (`install.sh` in the
service's `initCommands`, §2.6); the pin then still covers a dev Mate. Its two readers are
`mate.VersionToRun` and zcp's reading of its own release (the status's `zcp` key).

**The start job** (`mate.BeginStart`, `Start.Judge`), at every start of the unit, takes the install
lock without waiting and:

1. **Picks.** One bounded read of the release list from its source (§2.1c), and a download of the
   version to run unless the keep job already has; on any error it starts `current` as it is. The
   read, the download and its smoke (step 2) share one deadline
   (`mate.StartStageTimeout`, one minute, against about 8 s for a normal download and npm install on a
   Mate container) when `current` exists; what does not finish in it is left to the keep
   job (`mate.InstallTimeout`, three minutes, the same three for its download and smoke). A first install has the three minutes. The network never stops a start; only a first install without network
   fails, and systemd retries. A lock the keep job holds starts `current` as it is.
2. **Installs** (`mate.StageRelease`) into `versions/<v>`: one `GET` to the manifest's `url`, streamed
   to a temporary file while its SHA-256 is compared with the manifest's `sha256` — a mismatch reports
   both digests and never invokes npm; this is an integrity check against a damaged download, not a
   trust boundary (§2.1c). Then `npm install --prefix versions/<v> …` (npm still resolves the
   package's dependencies from its registry), and the smoke: `mate --version`, then the native-addon
   probe run from the installed package's directory, where the server resolves its runtime imports.
   The smoke has no clock of its own, only the caller's deadline: a container over its cgroup's
   `memory.high` (the granted RAM less the minimum free RAM, which the autoscaler raises in steps)
   stalls every process, so its duration says nothing about the install. An existing directory of that version that passes the smoke is kept; one that fails it is
   downloaded afresh by the keep job and by a first install, while the start job with a `current`
   leaves it as it is and starts `current`.
   Only a smoke that ran to its end and failed counts: one the deadline cut downloads nothing and
   its error names the deadline. An install just downloaded stays, because its smoke had only what
   the download left of the deadline. An existing directory, whose smoke had the whole deadline,
   stays under the start job with a `current`, for the keep job to smoke again; the keep job and a
   first install, which have `InstallTimeout`, remove it unless `current` or `last-good` names it,
   so the next run downloads it afresh: an install whose smoke hangs on every run is replaced on
   the second run, and a slow download never becomes a download on every run (a first install
   whose smoke is cut still fails, and systemd retries). A failure
   removes the half-built directory; `current` and `last-good` directories are never replaced.
3. **Switches** `current` atomically: the symlink is built under a temporary name and renamed onto
   `current`. A version below `MinimumMateVersion` is never started (`mate.BelowFloorError`; `zcp
   mate status --json` reports it as `belowFloor` beside `minimum`).
4. **Launches and judges.** "Up" is the process this start launched answering the loopback updater
   readiness (§2.1d) with its version; whether it is idle plays no part. The boot file
   (`ZCP_MATE_BOOT_FILE`) contains `{protocol, pid, progress, error}` and the server's own record
   of its boot steps (`startedAt`, `steps`, `readyAt`), which zcp does not read. Only an advance
   of `progress` restarts the boot window (`mate.BootWindow`, 2 minutes), so a boot that keeps
   advancing is never judged by elapsed time.
   - Up → `last-good` names the version (never a dev build, nor one below the floor), a failed
     version it reached is forgotten, and old version directories are pruned to `current` and
     `last-good` (the newest other release is kept when these name one version). A dev build's
     directory (`versions/dev`) is never kept: the version to run only ever names a release
     directory, so once a release is up nothing runs the dev build again.
   - A version newer than `last-good` that is not up → it is ended, remembered as the failed
     version, `current` returns to `last-good`, and the job exits non-zero; systemd starts the unit
     again and the same job starts `last-good`. The switch back never targets a `last-good` below
     `MinimumMateVersion`, which zcp would refuse to start; every release from 0.16.178 refuses to
     start on state written by a newer version, so one at or above the floor is safe to return to.
     Below it the newer version stays, and systemd retries it.
   - `last-good` itself, a dev build or a first install not up → nothing is written or switched; the
     job exits non-zero and systemd keeps retrying. A newer release in the list is picked by the next
     start.
   - A stop the launcher was asked for is no verdict.

The server's rule that makes this safe: it refuses to start on state written by a newer version
(mate 0f1db9f6cf). A change of stored shape is the server's own migration. zcp never reads, copies,
moves or restores the server's state.

**`zcp mate status --json`** answers `{installed, minimum, belowFloor, latest, contract,
updateAvailable, updater, zcp, restartNeeded, why}` — the one reader of "is there a newer mate" in the
whole product. `installed` is `current`; `updateAvailable` is `current` older than the version to
run; `updater` is `{protocol: 1, phase: "idle", runningVersion: current, failedVersion}` while a
Mate is installed, with `failedVersion` only when it is newer than `current`. While the keep job's
record holds a deferral whose `from` is `current` (the running Mate answered its last question not
now, §2.1d), the phase is `postponed` with `postponedSince` (the first not now of the unbroken row),
`postponedFor` (what was asked for: the Mate version to run, or `zcp <version>`) and, when the Mate
gave one, `postponedWhy` (its own words, opaque to zcp, at most 300 bytes), all plain strings. A
Mate's automatic updates switched off by its organization reads the same while a newer release is
out. The text status adds one line for it. Every status reads both releases from their sources;
`--local` answers without a network call, from the releases as zcp last read them (the release list
in `~/.zcp/mate/manifest.json`, §2.1c; zcp's latest in `~/.cache/zcp/update.json`, which reads as
unknown when it is older than the installed zcp), for the Mate server's own read at its boot, which
must not wait on the network.

The same status says whether a container restart would change anything. **`zcp: {installed, latest,
behind}`**, always present, is this binary against the zcp release `install.sh` installs (GitHub's
`releases/latest` for `zeropsio/zcp`), read from that redirect on every status: one `HEAD` of
`https://github.com/zeropsio/zcp/releases/latest`, bounded by 5 s, not the REST API and its
per-address quota. The self-update (`zcp update`, and `zcp serve` at its start outside a Mate
container) reads it the same way; in a Mate container the container restart is zcp's only update
(MU-4).
`latest` is `""` while that release is unknown, never guessed; a build no release names (a dev or
git-describe build) reads it the same way. `behind: true` is a release older than a known `latest`,
or a dev build with no active dev pin whose base is older than a known `latest` (§2.1a).
**`devPin: {since, until, reason}`** is present only while a dev pin is active. **`restartNeeded`** is always present, and **`why`**, one line for a person,
only when it is true: zcp is behind, or the running Mate answered its loopback updater readiness with
exactly 404 — older than protocol 1 — while a newer Mate is out. The Mate is asked only in an enabled
Mate container (elsewhere 3773 is an ordinary user port), only when `updateAvailable`, and never under
`--local`. The text status adds one line when zcp is behind, or names the restart when only the Mate
needs it; it names a dev zcp that is not behind with its latest release, and with any dev build
installed it ends with the active pin, or with the rule that replaces the build when there is none.

### 2.1b Disabling — the reverse direction

`MateEnabled` false with a unit file present: stop the unit (best-effort — it may already be stopped),
`sudo -E zsc unit remove mate` (a real failure here is the one error this branch returns), and delete
`~/.zcp/mate.env`. **`mate.Prefix()` is deliberately left alone**: the downloaded bundle stays on disk,
so re-enabling costs a `zcp init` and no network. With no unit file there is nothing to do and the
step does not even register.

### 2.1c The release manifest — no pin

zcp does not pin a mate version. It tracks the fork's **stable release** the same way the container
tracks zcp itself (`install.sh` → `releases/latest`), and refuses only what it cannot drive. Releases
are never pinned; the dev pin (§2.1a) concerns hand-placed builds only.

**The manifest.** The fork's release workflow publishes, next to the tarball, a release asset
`stable.json`:

```json
{ "version": "0.16.0", "asset": "zerops-mate-0.16.0.tgz",
  "url": "https://github.com/zeropsio/mate/releases/download/v0.16.0/zerops-mate-0.16.0.tgz",
  "sha256": "<64 hex>", "size": 21690443, "contract": 2, "publishedAt": "2026-10-26T18:08:45Z" }
```

`DesiredRelease()` reads `https://github.com/zeropsio/mate/releases/latest/download/stable.json`
(GitHub's own "latest release" redirect; a release marked *pre-release* is never "latest", which is
the rollback lever: mark the bad release as pre-release, re-mark the previous one as latest). Every
decision reads it when it decides: each start job, each keep run (every 5–7 minutes) and each
`zcp mate status` makes one plain download of that file, bounded by 5 s; no copy answers in its
place. A read that fails says so and decides on no release list: the start job starts `current`
and logs the miss (with nothing installed the start fails and systemd retries), the keep job logs
the error and knows no newer release, and `zcp mate status` reports the error. So a restart the keep
job made for a release returns on the running version when the start job's own read fails; the
keep job asks again on its next run. Each successful read is
recorded in `~/.zcp/mate/manifest.json`, which only `zcp mate status --local` reads (§2.1a).

- A release list never moves the Mate back: the version to run is never older than `last-good` (§2.1a); only a dev push goes back.
- Private manifests selected by `ZCP_MATE_MANIFEST_URL` may name a full, unprefixed semantic
  prerelease version. Version checks preserve SemVer precedence: a prerelease of the minimum
  release is below that release, numeric prerelease identifiers compare numerically, and build
  metadata does not change precedence. The contract, version floor and tarball digest gates apply
  before installation exactly as for stable releases; the default latest-stable source is unchanged.

**What zcp checks, and what it does not.**

| Check | Rule |
|---|---|
| `contract` | must equal `mate.SupportedContract` (today `2` = C-1…C-12 in §2.8 and §2.8a). A manifest declaring a contract zcp does not know is refused with a message naming both numbers — the one case where an old zcp deliberately stays on the mate it has. |
| `version` | must be ≥ `mate.MinimumMateVersion`, the oldest release zcp still drives (moves only when a contract fact changes; 0.16.195, the first release that keeps no member list (§3.13); 0.16.54 was the first rev 7 release, which answers HQ's tokened tickets). A manifest below it is refused, and so is a recorded one under `status --local`: the record holds what an earlier zcp checked against its own floor. |
| `sha256` | the downloaded tarball must match it — damage detection, nothing more. |

**There is no trust chain, by decision.** The container installs zcp from a GitHub
release over TLS with no signature; a manifest digest compiled into zcp guarded mate alone against
an attacker who could replace assets in `zeropsio/mate` but not in `zeropsio/zcp`, which is not a
boundary anyone defends. The boundary is the GitHub organisation: 2FA, protected `v*` tags, who may
push them. If the product ever needs build provenance it is done for both repositories at once
(Sigstore keyless with GitHub OIDC), never with a key held in a repository secret — whoever can edit
the workflow can sign with that key.

**What moved out of zcp.** `PinnedVersion`, `PinnedSHA256`, `ReleaseAssetName`, `ReleaseURL`,
`scripts/mate-pin.sh` and the `serve --help` golden are gone. The flag contract (C-2) is now tested
where it changes: the fork's CI asserts that `mate serve --help` advertises every flag of contract 2
(the list in §2.8), and a flag added on either side goes through a contract bump, not a pin move.

**Release order is Mate first.** Publish the stable Mate artifact and `stable.json` before
releasing a zcp that raises `MinimumMateVersion` or changes `SupportedContract`. Both the local
release target and the release workflow fetch the published stable manifest and refuse to
publish unless its version reaches the floor and its contract equals `SupportedContract`.
The release check uses the canonical stable channel, never a container's manifest override
or recorded read. An older zcp ignores additive manifest metadata; no new `serve` flag is passed.
The floor is 0.16.195, the first Mate that keeps no member list (§3.13); rev 7 HQ (0.16.54) refuses every ticket an older Mate
asks for. A 0.16.195 prerelease sorts below the stable floor and cannot authorize publication. The supporting stable manifest must
be published before zcp releases.
Auto-update protocol 1 is probed before it is used.

### 2.1d The keep job — `zcp mate fuse`

The keep job decides when the Mate restarts, nothing else. It runs outside the Mate server as the
systemd template service `zcp-mate-fuse@<way>.service` (`cmd/zcp/mate_fuse.go`): its timer
`zcp-mate-fuse.timer` runs `zcp-mate-fuse@automatic` 10 minutes after boot, then 5 minutes after each
run, each with up to 2 minutes of jitter; `zcp mate update` starts `zcp-mate-fuse@attended` (or
`@automatic` with `--automatic`, `@forced` with `--force`) at once. Prepare writes both under `/etc/systemd/system`, reloads
systemd when either changed, and enables and starts the timer, so it runs while the Mate's unit is
down or crash-looping and starts at boot even when `zcp init` fails before its Mate step. Disabling
Mate (§2.1b) removes them. The names stay "fuse": a renamed timer would leave the old one enabled on
every running container.

Each run takes the install lock without waiting: while a start job installs or judges a boot, or
another keep run asks, it does nothing. Then the first that applies:

1. **Server not there** → the unit is restarted; its start job reads the release list and starts
   the version to run. "Not there" = the unit is not running (`systemctl is-active` says `inactive`
   or `failed`), or the server neither answers the loopback readiness nor advances its boot file
   within the boot window. A booting, installing or busy server is never "not there".
2. **The version to run differs from the running one** → it is downloaded (idempotent), and the
   server is asked "may I restart you now?". Yes → the unit restarts; its start job starts the
   version to run. Not now → recorded (below), and the next run asks again. A failed release is never the version to run, so it causes
   no restarts. Every run reads the release list from its source for this; a run whose read fails
   knows no newer release. A version that cannot be staged
   fails the run without skipping step 3: a newer zcp is what repairs a staging defect, and in a
   container nobody can reach the keep job is the only automatic way it arrives.
3. **A newer zcp** (`zcp.behind`; for a dev zcp only without an active dev pin, §2.1a) the keep job
   has not restarted for from the installed zcp build → the same question. Yes → it records the
   release and the installed build, its version and build time (`zcpRestartedFor`,
   `zcpRestartedFrom`, `zcpRestartedFromBuilt` in `~/.zcp/mate/fuse.json`) and `zcp self
   restart` asks the platform for the container restart that is a boot: install.sh installs the
   newest zcp, the start job the newest Mate (§2.9 step 7). Every run reads zcp's latest release from
   its source (bounded as a status read). A restart refused for any other reason, or unconfirmed,
   forgets the record, and the next run asks again; an accepted one that does not end the process
   within 5 minutes (the Mate server's own grace) cancels the question and keeps the record: one
   attempt per installed build and release. A restart refused because Zerops marked the service
   FAILED (`serviceStackIsFailed`) keeps the record the same way, without the grace: the service
   comes back only by a stop and then a start from outside the container (the Mate's Restart in the
   web, or Zerops), since the stop ends the container that would send the start, so zcp never stops
   its own service and its answer says so. A run that finds another build
   installed (another version or build time) forgets the record: the attempt took effect, and a
   build pushed again, even under the same version, is a new placement, so a dev zcp placed after an
   attempt is still replaced once its pin ends. A record without the build is forgotten the same
   way. With any dev build installed, the run's last line names the pin, or says for each unpinned
   dev build why this run left it: zcp's latest release is unknown, the restart for it was already
   asked from this build, the Mate release list was not read, the Mate release is not staged, or no
   release is newer than its base.
4. Otherwise nothing: no question, no restart.

**The deferral.** Every not now the Mate answers (a drain answered `drained: false`, at once or at its
deadline, whatever the reason: not idle, not quiet, or the organization's automatic updates off), by
either way of asking, is recorded as `deferred: {since, from, for, why}` in `~/.zcp/mate/fuse.json`:
`since` is the first not now of an unbroken row, kept by every later one while `from`, the running
Mate version, stays; `for` is what was asked for (the Mate version to run, or `zcp <version>` in step
3); `why` is the drain answer's `why`, else the `blockers` of one `GET readiness` right after it
(bounded to 5 s) joined with `; `, else empty, cut to 300 bytes. A drain without an answer
(nothing listened, the connection broke, the request's own deadline) or with an answer that is not
protocol 1 records nothing. The deferral is cleared when the Mate says yes, on a run that finds
another version running than `from`, and on a run that asked nothing because it knew there was
nothing to ask: the release list was read, no Mate version was left unstaged and, for a `zcp
<version>` deferral, zcp's latest release was read. A run that failed to look knows nothing and
leaves the deferral and its `since` as they are, and posts nothing. A yes whose restart never
comes records nothing. `zcp mate status` reads it (§2.1a). The record is written before the
`POST cancel` that follows the not now, and a run that clears a deferral without a restart posts
`cancel` as well: the cancel is the Mate's cue to re-read its status. A record that cannot be
written never holds up a question or a restart: the run says so and goes on, and only a not now it
could not record ends the run with 1. No timer, retry or forced
restart rides on it: a Mate that never says yes keeps its version until it, its person or an
operator says yes, an operator's yes being an operator's forced update (below).

**An operator's forced update** (`zcp mate update --force`, which runs `zcp mate fuse --forced`)
asks in place of the Mate's own drain wherever a run asks (steps 2 and 3), so a Mate whose outgoing
version's drain refuses on a blocker that is no live work can still be replaced without
interrupting live work. It runs, in order:

1. One `GET readiness` (bounded to 5 s). No answer → not now: nothing is closed and nothing
   recorded.
2. `ready`, or no `blockers` → the attended question (without the policy), unchanged.
3. Any blocker that is not overridable → not now, naming those blockers. Nothing is closed and the
   deferral on record stays as it is: the Mate answered no drain.
4. Only overridable blockers → zcp closes intake by posting the attended drain and, once that
   request is written, reads readiness once more. A blocker that is not overridable, or no answer →
   `POST cancel`, not now, nothing restarts. Only overridable blockers, or ready → yes, and a
   deferral on record is cleared as on any yes; the run's journal line names the blockers it forced
   past. For a Mate restart the run keeps the drain request open after the unit restart until the
   stopping Mate drops it (at most the request's own 10 minutes and 1 s): the Mate reopens intake
   when its drain is interrupted, so a run that ended first would reopen it before systemd stops
   it. For a zcp release the container restart's existing grace holds the request open.

Its cost: a conversation whose native session cannot resume starts a fresh provider session on its
next turn. Its known residue: the Mate gives no receipt that intake closed, so a turn accepted
between the second readiness read and the drain's close is interrupted, as `systemctl restart`
interrupts it. The forced update is an operator's verb only: the Mate server never passes `--force`
(MA-6). Steps 1 and "not there" of the keep job are unchanged.

**The question** is the server's loopback updater (`http://127.0.0.1:3773/api/mate/update/`,
protocol 1): `GET readiness` answers `{protocol: 1, version, bootId, ready, blockers}` once the
server reached command readiness, `blockers` being the Mate's own words on what keeps it from idle;
`POST drain` with `{version, deadlineMs: 600000, automatic}` answers `{protocol: 1, drained, bootId,
why?}`, `drained` once it stopped taking work; `why`, sent with `drained: false`, is the Mate's one
line on what it waits on or why it refuses, optional (zcp never requires it), at most 300 bytes on
the wire: zcp decodes at most 4096 bytes of either answer; a drain answer it cannot decode is no
answer and records nothing, a readiness answer it cannot decode leaves `why` empty. zcp reads one
word of them: a blocker whose text, after an optional leading `<id>: `, starts with `native resume `
is overridable, the Mate's words for "a restart would not resume this conversation's native
session", and the Mate keeps that prefix for such blockers only. Every other blocker, an unknown
one included, is live work. Only an operator's forced update reads the word; every other question
leaves both answers opaque.
An automatic question leaves the server open and waits up to 10 minutes for it to be idle and quiet,
under the organization's policy; a person's question stops intake at once. Not now (a refusal, the
deadline, no answer) is followed by `POST cancel`, which reopens what the question closed and makes
the server re-read `zcp mate status --local`; a yes whose restart never comes is reopened the same
way, and the server reopens by itself after its own bound. A keep run is not started again while one
is waiting, so a busy Mate is never closed by being asked.

**Without HQ.** The organization's policy holds whether or not the Mate has a link to HQ; a start of
the Mate starts the version to run (§2.1a).

**`zcp mate update [--automatic | --force] [--json]`** runs the keep job now and answers at once
`{action: "none", from: <installed>, restarted: false, started: true}`, outside the Mate server that
may be calling it: the question can take the drain's 10 minutes. Without `--automatic` it asks
without the policy (a person's or an operator's update); with it, with the policy; with `--force`
it is an operator's forced update (above). `--force` with `--automatic` is a usage error (exit 1)
before any other check. A held install lock is reported (`error`, exit 1), never waited for.

### 2.2 The supervised process

`zcp service start mate` runs `mate.ServeArgv(bin)` — never `npx` (resolving the package
at every start cost 58 s cold, measured, see the mate ledger; the argv always runs the local bundle):

```
~/.zcp/mate/current/node_modules/.bin/mate serve --mode web --host 127.0.0.1 --port 3773 \
  --base-dir ~/.t3 --no-browser --auto-bootstrap-project-from-cwd /var/www
```

- `--auto-bootstrap-project-from-cwd` is **boolean**; the workspace (`/var/www`) is a trailing
  **positional** — writing it as the flag's value bootstraps the unit's launch directory instead.
  `--base-dir` (`~/.t3`) keeps thread history across a restart; a redeploy starts it empty.
- **No flag depends on the bundle.** The CLI treats an unknown flag as a fatal parse error, and a
  launch may start a bundle nothing vetted — a dev build under its pin, or the installed one after an install
  failed. So the public prefix is not passed as `--base-path`: it rides `T3CODE_BASE_PATH`
  (`mate.LaunchEnvLines`, §2.3), the same setting, which every release since v0.1.0 reads. Nothing
  runs `serve --help` before a launch.
- **An explicit `--auto-bootstrap-project-from-cwd` wins over the `serve` command's own opt-out.**
  Upstream's `serve` command sets a headless startup presentation that, left to itself, forces
  auto-bootstrap off; the fork's config resolution checks the explicit flag *first*, so zcp's
  boolean flag (above) still lands. The first boot that finds none creates one project titled from
  `path.basename(cwd)` and one thread; every later boot that finds them reuses both rather than
  re-creating.
- **A stop ends the Mate on its own terms.** `zcp service start` (`service.runCommand`, the launcher
  of every `zerops@<name>` unit) relays each SIGINT/SIGTERM it receives to its child and waits for the
  child to exit; it never sends SIGKILL to a server it was asked to stop (it kills only a server
  whose launch did not come up, §2.1a, which admitted no work). The Mate's runtime runs its shutdown (sockets and stores
  closed). Mate's interruption-only shutdown exits 130; zcp treats that exit as successful only
  after it has successfully relayed a stop to the Mate, whatever sent the child's signal: the
  operator requested a stop, so inactive is the right result. Exit 130 before a relay, any other
  nonzero exit and termination by a signal remain errors. The deadline is the unit's: systemd's
  `TimeoutStopSec` (90 s on a Zerops container),
  then SIGKILL to the cgroup. An update's `systemctl restart` (§2.1d) thus stops a drained Mate cleanly.

### 2.3 The environment contract

`zcp init` writes the following while the container's full environment is present;
`service.Start("mate")` merges it over the unit's own, not guaranteed to inherit it:

| Key | Source | Written when |
|---|---|---|
| `T3CODE_ZEROPS_PROJECT_ID` | `runtime.Info.ProjectID` | always — THE Zerops-environment signal; nothing else votes |
| `T3CODE_ZEROPS_API_HOST` | `mate.ResolveAPIHost(ZCP_API_HOST)`, else `schema.CanonicalAPIHost` | always — bare host, scheme/trailing slash stripped, port kept |

Only non-secret identifiers are written; a token never enters `~/.zcp/mate.env` (mode 0600),
rewritten every boot so a unit's frozen ExecStart never has to change. A missing/unreadable env
file is reported and mate starts anyway — diagnosable, unlike a unit that refuses to launch.

Every launch adds six more on top (`mate.LaunchEnvLines`), whatever init wrote:

| Key | Value |
|---|---|
| `T3CODE_BASE_PATH` | `mate.BasePath` (§2.7) |
| `ZCP_MATE_UPDATE_STATE_FILE` | the automatic-update coordinator state (`mate.UpdateStatePath`) |
| `ZCP_MATE_BOOT_FILE` | the file a booting server reports its progress in until it accepts commands (`mate.BootFilePath`, §2.1d) |
| `ZCP_STATUS_FILE` | the status file a new Mate's setup is read from (`mate.DefaultStatusFilePath`) |
| `ZCP_MATE_STATE_FILE` | the file the server keeps the Mate's state in (`hq.MateStatePath`) |

### 2.4 nginx — locations outside the cookie gate, behind the Mate gate

Rendered identically whether or not `VSCODE_PASSWORD` is set, and **only when `MateEnabled`** — all
locations and the upstream render only inside `{{- if .MateEnabled}}` regions.

| Location | Behaviour |
|---|---|
| `{BasePath}/` (`/mate/`) | Proxies through `mate_backend` to `127.0.0.1:3773` — **trailing slash strips the prefix**, so mate's routes stay at the loopback root and only URLs it *emits* (`T3CODE_BASE_PATH`) carry it. The upstream keeps 16 idle connections per worker; HTTP/1.1 and an empty Connection header allow reuse. Only the server's HTTP API and WebSocket answer here; the container serves no client bundle and no sign-in page (§3.13). Outside the cookie gate: mate owns its own auth (§3). |
| `= {BasePath}/ws` | Proxies through the same upstream to `/ws`, retaining query parameters, websocket upgrade headers and `proxy_read_timeout 86400s`. |
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
`Access-Control-Allow-Origin: *` — a hosted mate web client reads its own container's origin
before it holds any credential, so the response has to survive a cross-origin `fetch` even though
its body is only two non-secret fields. `/mate/` and the cookie-gated `location /` stay CORS-less:
`/mate/` inherits mate's own origin rule (§3.13), and code-server's location is never fetched
cross-origin. `TestRunNginx_HealthzHasCORSForCrossOriginProbe`.

With the flag off the render is **byte-for-byte identical** to the pre-mate one, with and without a
password — verified by rendering both templates side by side, and pinned by
`TestRunNginx_MateDisabled_RendersNoMateSurface`, which asserts the absence of `/mate`, `3773`, `healthz`
and the marker path *and* the presence of every non-mate structure.

Nginx sends both access and error logs to the container's system journal through the
`/dev/log` Unix syslog socket (`local1`, tag `nginx`, access severity `info` and error severity
`error`). The supervised launch also passes `-e stderr`: nginx opens its compiled-in
error-log destination before parsing this configuration, so its bootstrap diagnostics must
use the inherited journal stream too. `TestStart_KnownService_ArgsCorrect` pins that launch.
Journal retention is bounded by the platform's journald configuration. Nginx does
not open `/var/log/nginx` files, so rotating a legacy log into a different owner cannot stop
it from starting, reopening logs or serving Mate. This logging contract also applies with
Mate disabled.

After successfully rendering the configuration, `zcp init nginx` removes the obsolete
`/etc/logrotate.d/nginx` policy. A missing policy is already converged; any other removal
failure is reported. A failed configuration render leaves the policy in place. Init owns
nginx's cache and temporary directories, and leaves legacy log contents and permissions
and unrelated rotation policies untouched. `TestRunNginx_LogsUseSystemJournal`,
`TestRunNginx_FileRotationRetired`, `TestRunNginx_LegacyLogFilesPreserved`.

Live edits do not survive — every boot re-renders `internal/content/templates/nginx.conf.tmpl`.

### 2.5 Readiness — two probes, no process

**`GET {BasePath}/healthz`** → always `200 application/json` *while mate is enabled*: the marker
`zcp init` writes at
`/var/www/.zcp/state/init-complete` when its step list ends —
`{"initComplete":true,"initAt":"<RFC3339>"}`. Records the list **finished**, not that every step
succeeded (a degraded mate step still leaves the marker); `initAt` moving is how a client sees a
restart re-initialized the container.

**`GET {BasePath}/.well-known/t3/environment`** is the Mate descriptor and the client's primary
readiness evidence. A JSON response must contain an `environmentId`; if it names a `basePath`, that
path must be `/mate`. Status alone cannot prove the right service answered. The client returns
`ready` when the descriptor is valid and reads `/healthz` only as a fallback when it is not. The
fallback distinguishes an initializing Mate, a container that predates or does not serve Mate, and
an unreachable container; a 5xx is not evidence that Mate is absent. There is no `mateUp` field in
the shipped protocol. See the [readiness probe](#45-the-readiness-probe).

### 2.6 What survives what

| | restart | redeploy | disable (§2.1b) |
|---|---|---|---|
| `~/.zcp/mate/versions/*` (the bundles) | kept | lost | **kept — deliberately** |
| `~/.zcp/mate/current`, `last-good`, `failed-version` | kept | lost | kept (`current` points at a version nothing runs) |
| `~/.zcp/mate/dev-pin.json` (§2.1a) | kept | lost | kept |
| `~/.t3` (threads, sessions, auth) | kept | **lost — recreated empty** | kept |
| `~/.zcp/mate.env` | kept (rewritten anyway) | rewritten by the new container's init | removed |
| `~/.agent-browser/sessions/zcp-default.json` (the browser's kept sign-in, §2.6c) | kept | lost | kept |
| `zerops@mate` unit | kept | lost, re-created by init | **stopped and removed** |
| `/var/www/.zcp/state/init-complete` | rewritten each boot | rewritten each boot | not rewritten |
| live nginx edits | erased (template re-rendered) | erased | erased |
| container id | unchanged | changes | unchanged |

A restart is also an upgrade — `install.sh` re-runs and replaces `/usr/local/bin/zcp` (measured,
see the ledger), and every start of the Mate's unit starts the version to run (§2.1a): a
release that does not come up is replaced by `last-good`, and a newer one is picked by the next start.
Thread history is one redeploy away from gone; a client surfaces that first. A **redeploy** losing
the bundle is not a regression under the versioned layout — it loses the whole container — and a
**disable** keeping it is what makes re-enabling free.

### 2.6a What a broken mate costs — measured

The unit `zsc unit create` writes is `Restart=always`, `RestartSec=3`, under systemd's default
`StartLimitBurst=5` / `StartLimitIntervalSec=10`. One restart every 3 s is ~3.3 per 10 s, **below**
the burst threshold — so a unit whose `ExecStart` fails immediately **never trips the start limit
and never gives up**. With the bundle path broken: the restart counter climbs
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

### 2.6b Managed browser batches

`zerops_browser` and `zerops_verify`'s render check share one warm browser per
container: the transient systemd service `zcp-browser.service` holds agent-browser's
daemon and its Chrome between calls. It is `Type=forking`, started through
`agent-browser open about:blank`, with the daemon's pid file as `PIDFile`. Its
`RuntimeDirectory` holds the daemon socket, so once the service is gone no client
can respawn a daemon outside it. systemd bounds it: `TasksMax=1000`, `CPUQuota` at
75% of the container's CPUs, `RuntimeMaxSec=3600`, and `KillMode=control-group`
with `SIGKILL`; `UMask=0077` makes every file it writes private to its user.
Chrome's profile lives in `/tmp/zcp-browser`, which `ExecStopPost`
removes on every stop; the sign-in outlives it apart (§2.6c). The daemon exits after
20 idle minutes, and the service ends with it. The service and every step get one
environment built by one function, the same for every isolation, because agent-browser
restarts a daemon whose settings differ from its client's; it names no agent-browser
session, so agent-browser itself never restores or saves a sign-in
(`TestBrowserEnv_IdenticalForTheBrowserAndEveryStep`).

A call waits on one container-wide `flock` until its own deadline, then fails as
busy. It reuses the warm browser only if the browser is running, the last call in
it ended cleanly (a marker in the runtime directory says `idle`; a caller that died
mid-call leaves it `busy`), neither the idle exit nor the max age can cut the
call, and the service used at most a tenth of a CPU since that call ended (systemd's
`CPUUsageNSec`, recorded in the marker): agent-browser's tab list can miss popups of a
storm and Chrome can keep a hung renderer, so a browser that stays busy while idle is
recycled. One that used more is first waited out, up to 4 s in windows of 250 ms: closing
a heavy page keeps Chrome busy for seconds after a call (4.7 s of CPU in the 6 s after a
three.js example on a Mate, 2026-11-22, then nothing), and without the wait every call
that followed within that time relaunched Chrome and restored the sign-in; a browser
whose use in one window falls to a tenth of a CPU is kept, one still above it at 4 s is
recycled. Otherwise it starts a fresh one, and names why — on the call's stderr and in
the container's system log as `zcp-browser` — so a relaunch is readable from outside the
agent's session. Each step is its own `agent-browser --json`
process, so a deadline or cancel keeps the finished steps. A call clears the
daemon-wide logs, opens its own tab, and closes every tab but the base one when it
ends; a caller's `window`, `tab new`, `tab close` and their aliases are stripped, so the
base tab is always the first, and a caller's tab list or switch reaches only the call's
tabs (**A call's popups** below). A deadline, cancel, fork exhaustion, unanswered step, CDP wedge or failed
cleanup stops the service instead; the stop has its own bound, so a cleanup that hung
on a spinning tab cannot spend it. A waiting call gives up while a sixth of its time
is left and answers busy without touching the browser, so a late waiter is never cut
mid-walk. The unit caps memory at 75% of the container's ceiling at its start and
sets `OOMScoreAdjust=1000`, so Chrome, not the agent, is the first to die of memory.

Each step answers agent-browser's own `error` text verbatim with an `errorKind` beside it:
`wedge` (a CDP wedge signal, which also recycles the service), `invalid-command` (a command,
subcommand or `find` action agent-browser does not know — the call was wrong, not the page),
`stale-ref` (an `@eN` ref no snapshot in the call's tab made; refs die with the tab),
`script-timeout` (an `eval` that ran into the CDP timeout while the page still answered a
trivial evaluate probed under a 5 s bound: the script's promise never settled, the page is
alive — the walk goes on and the service is kept; a probe that fails leaves the step `wedge`),
`slow-page` (an `open` agent-browser's client gave up on — "no response came back" — while one
plain request to the page, sent after it and bounded at 30 s, got an answer: the step notes that
request's time to first byte and status, the walk goes on, a later unanswered step on that page
is `slow-page` too, and the service is kept; a request that gets no answer leaves the open
`wedge`), `server-error`
(the page answered 5xx — the document's status read once after the first-paint wait, never a
step, or that plain request's — so each caller command after it comes back as a `server-error`
step that did not run while the screenshot and the canned tail run, and the service is kept),
`selector-not-found`, `not-editable`, `timeout` or `other`. A server error anywhere in the walk
puts its message on the result: the status and where to read the server's logs. Otherwise an
open that is `slow-page`, or `timeout` (agent-browser answered that its own wait for the page's
load ran out), says only what the walk measured: the open got no answer (or that answer), the
plain request's time to first byte and status when there was one, that the page had not finished
loading when agent-browser stopped waiting for it (a dev server's bundle still compiling, a
request that never ends), that the steps after the open ran on the page as it was, and that a
long first byte itself means a slow server a later call will find warmed up. `timeoutSeconds`
does not reach agent-browser's load wait, so the message never advises raising it, and it names
none of agent-browser's own bounds, which change with it. Measured: a
Vite dev server's first request took 30 s and its open recycled Chrome as wedged twice, and a
wait for text on a 502 page waited out its 60 s (`TestRunBrowserSteps_OpenWithNoAnswer_ServerAnswers_IsASlowPage`,
`TestRunBrowserSteps_ServerErrorPage_SkipsTheCallersCommands`,
`TestBrowserBatch_SlowPageAndServerError_SaySoAndKeepTheBrowser`). The first-paint wait after the
open is zcp's own step: a page that did not paint within its 5 s comes back as a step that
succeeded, its result saying so, and the walk goes on; any other failure of it stays as it is.
`failedSteps` counts the steps that answered `success:false`, the refused ones included, and a
result nothing else put a message on says "N of M steps failed; the first: COMMAND (ERRORKIND)",
the command cut to 80 characters; a failed step never makes the call an error. `zerops_verify`'s
render check reads no rendered text from a walk that recycled the browser, and reads it whatever
the message says otherwise: the failed steps themselves are skipped. `errorsOutput` and
`consoleOutput` are read from the canned tail's `errors` and `console` steps, whose own results
are then dropped, so each appears once; the `network requests` step stays whole, since
`networkOutput` keeps only each failed request's URL, method, resource type and status.
`consoleOutput` keeps agent-browser's `{"messages":[...]}` with only its `warning`, `error` and
`assert` messages, each as its `type` and `text`, and `belowWarning` counting the others it left
out; a caller's own `["console"]` step keeps every level. Only `wait` polls (agent-browser's
25 s default, at most 120 s when asked); `click`, `fill`, `check`, `find`, `get` and `is` look
once. A caller's `wait` whose own `--timeout` asks more than 120000 runs with 120000 and its step
names that; one that times out there adds to its error that zcp ends a wait at 120 s and, on a dev
server, to read `zerops_dev_server action=logs` and call again to wait on. In November 6 of 161
timed-out agent waits asked more and lost at least 570 s, no clean walk with a longer wait ran
past 98 s, and waits that succeeded after 60 s were measured, so the bound is not lower.

`isolation` is one parameter: `tab` (default) is a tab in the warm default context,
so cookies, storage, HTTP cache and service workers carry over between calls, and the
sign-in also across browser restarts (§2.6c); `fresh-context` is `window new`, a new
browser context, which starts signed out; agent-browser cannot dispose one, so the
service stops when that call ends; `new-browser` recycles the service before the call
(`TestWarmBrowser_ReusableOnlyInsideItsBounds`). The Mate's stream endpoint
`~/.agent-browser/default.stream` is a symlink
into the runtime directory, and dangles (no stream) while no browser runs; beside the
stream zcp names the tool call that holds the browser (§5.6, C-16). No
process-name sweep or PID has kill authority. Raw browser tools and provider
lifetimes are outside this service.

**A call's popups.** A call's tabs are its own tab, which the setup opened, and every
tab its pages open during the call — a sign-in provider's popup, a `target=_blank` link;
the walk follows them as a person's eyes do. agent-browser 0.39.1 launches Chrome with
`--disable-popup-blocking`, so a page may open one at any time, its load included. While
the call's own page loads — the viewport, the `open` and the first-paint wait the walk
puts first — a tab it opens is recorded and the call's own tab is made active again, so
the paint wait and the document's status are the page's own; that tab is followed once
they ran, before the first caller step. After each caller step it reads agent-browser's
tab list: a tab opened since becomes the active one (the last, when several opened); when
the active tab closed, the tab that was active when it was seen is active again while it
is open, and the call's own tab otherwise (the list names no opener); else the walk's
active tab stays active, whatever agent-browser made active (it activates a discovered
page only when its next command drains the event, so a `wait` that started before the
popup existed polls the opener throughout, and after a close it falls back to a
neighbour, the base tab among them). A list that fails while a tab the call opened is
active is asked once more; one that fails again runs no caller step more — each comes
back "not run", the screenshot too, while the canned tail runs — since the next step
could run in the base tab. Each tab the walk or the caller makes active is brought to
front and holds focus — `window.focus()`, then a wait until `document.hasFocus()` has
held for 900 ms, at most 3 s — and a newly opened one first gets the first-paint wait; a
hold cut by the page moving on by itself (its script redirected it) is held once more
after the next page's first paint. A provider that arms its Approve only after its page
has had focus for about 600 ms is so armed before the next step, and agent-browser's
`click` does not wait for an enabled button (measured: it answered success on a
just-opened popup's disabled Approve, which did nothing). This time is the call's own:
every step the walk takes for a tab is bounded by the call's deadline, and with under
10 s of it left the walk only switches, without the paint wait or the hold, and says so
in the `note` of the step after which it did. A popup that closes itself once done can
close under the step after the one that finished it ("Session with given id not found",
measured), and the step that finished it can fail the same way when its own effect
closed it. A failed step in a tab the call opened that is gone after it runs once more in
the tab the call is back in only when it cannot act — agent-browser 0.39.1's `wait` for a
time, a selector, text, a URL or a load state (not `--fn`, which runs the page's script,
nor `--download`), `get` text, html, value, attr, url, title, count, box or styles, `is`
visible, enabled or checked, and `snapshot` — and the result holds that run; any other
step keeps its one answer, which adds that the popup closed under it and the step was not
run again: a key, click, fill or script run twice would act on the opener's site. Such a
step's "Target closed" is no CDP wedge, since the tab list after it answered. The
caller may also list and switch: `["tab"]` or `["tab","list"]` answers the call's tabs
only, and `["tab","t<N>"]` with an id from that list switches; a switch to a tab open
before the call — the base tab, another call's leftover — does not run and comes back as
an `invalid-command` step, as do a label or target id, `tab new` and `tab close`. Every
name agent-browser 0.39.1 takes for a refused command is refused (it matches names
exactly, case included): `open`, `goto`, `navigate`, `close`, `quit`, `exit`, `window`,
and `batch`, whose commands of its own could be any of them; a command's name is read
past agent-browser's global flags, so a flag before it lets none through. The walk's own
steps are none of the result's, so each step's echo stays the command as written, and
`{{env:KEY}}` substitution and redaction (§5.8a) apply to a popup's commands and answers
as to any other step. The save of the kept sign-in (§2.6c) reads the call's own tab —
the one the setup opened — from the tab list, whatever is active, so a popup that opened
after the walk's last look never puts its site in the kept file; the cleanup closes the
popups with every other tab, and a `fresh-context` call's popups open in its own context
and end with it. Each caller step costs one tab list more
(about 10 ms) and each switch 0.9 s. Measured with agent-browser 0.39.1 and Chrome against
an opener whose "Sign in" opens a provider popup that arms "Approve" after 600 ms of focus
and posts back and closes on approve: one call clicked Sign in, filled `{{env:KEY}}` into
the popup (the result carried the placeholder only), clicked Approve, and the opener said
signed in; a page that opened the provider while it loaded kept its own paint wait and
status and its first caller step ran in the popup; a popup a timer opened as the call
ended was not saved from; a switch to the base tab or to a tab opened before
the call was refused; the cleanup left the base tab only; the next `tab` and
`new-browser` calls were signed in to the provider and a `fresh-context` call was not
(`TestRunCallSteps_APopupThePageOpensIsWhereTheNextStepRuns`,
`TestRunCallSteps_APopupOpenedDuringLoadLeavesThePagesOwnLoad`,
`TestRunCallSteps_AClosedPopupReturnsToTheTabItWasOpenedFrom`,
`TestRunCallSteps_APopupWhoseOpenerClosedReturnsToTheCallsOwnTab`,
`TestRunCallSteps_ATabListLostAfterAPopupRunsNoStepBlind`,
`TestRunCallSteps_AFocusHoldCutByARedirectIsHeldOnTheNextPage`,
`TestRunCallSteps_AFollowWithLittleTimeLeftOnlySwitchesAndSaysSo`,
`TestRunCallSteps_AStepAClosingPopupRanInRunsAgainInTheTabBeforeIt`,
`TestRunCallSteps_AStepThatMayActRunsOnceWhenItsPopupClosesUnderIt`,
`TestRunCallSteps_APopupClosingUnderAStepIsNoWedge`,
`TestBrowserReadOnly_OnlyCommandsThatCannotAct`,
`TestRunCallSteps_ATabOpenBeforeTheCallIsNeverReached`,
`TestRunCallSteps_TheTabListNamesOnlyTheCallsTabs`,
`TestBuildCanonicalBatch_ACommandIsReadPastItsGlobalFlags`,
`TestBuildCanonicalBatch_EveryNameOfARefusedCommandIsRefused`,
`TestSaveBrowserSignIn_ReadsTheCallsOwnPageNotAPopups`).

### 2.6c The kept sign-in

The warm browser is replaced often — after 20 idle minutes, after any call that did not
end cleanly, at its max age, after a `fresh-context` or `new-browser` call, on a
container restart — and its profile goes with it. What a sign-in leaves, the cookies and
local storage of the default context, is kept apart in one file zcp owns, so the next
browser starts signed in. zcp restores and saves it through Chrome's DevTools protocol
itself, on the browser target of the Chrome agent-browser drives — whose websocket URL
agent-browser's `get cdp-url` names, since only its daemon knows the Chrome it runs now
(a relaunched Chrome has a new port and profile folder) — and never loads a site for
either: cookies are read and set browser-wide (`Storage.getCookies`,
`Storage.setCookies`), and local storage, which Chrome hands out only to a page of its
origin, on the base tab, borrowed through a DevTools session of zcp's own: while zcp holds
it, it answers every request of the tab itself with an empty page (`Fetch`), with service
workers bypassed, so the tab never reaches the network. Why: with
agent-browser's own `state save` and `state load` (0.39.1), both of which open each origin
for real, a real Mate's end-of-call save added more than 5 s to 23 of 73 measured calls
(at most 20 s), and on a Mac 7–9 of 32 saves stalled about 35 s inside agent-browser's
read of an origin through a temporary tab (measured); the restore opened each
saved site under a 4 s bound, a cold Chrome's first navigation missed it, and the site's
storage was dropped and Chrome relaunched. The browser's start restores nothing. The file
keeps agent-browser 0.39.1's state format (`cookies`, `origins` with `localStorage`, and
zcp's `zcpLastUsed`), so every kept sign-in carries over.

- **Which calls keep it.** A `tab` or `new-browser` call restores it into a browser
  started for that call, and every call of either isolation that hands the browser back
  warm saves it. `new-browser` recovers from a browser that misbehaves — Chrome's process
  state, not who is signed in — so it keeps the sign-in; a clean identity is
  `fresh-context`, which neither restores nor saves: its new context starts and stays
  signed out, and the browser it ran in is stopped, never handed back, so nothing of it
  is saved (`TestBrowserKeepsSignIn_TabAndNewBrowserKeepFreshContextNever`).
- **The restore.** Before the call's own tab opens, under one bound of 5 s for the whole
  restore. The kept origins are pruned first (see **Use and expiry**). A kept file zcp
  cannot read, or that is no kept state, is set aside — renamed to
  `zcp-default.json.aside`, one copy at most, never restored again — and the call runs
  signed out; the next clean save writes a new kept file. Otherwise, into the Chrome
  whose base tab agent-browser lists — the connection must find that tab — every kept
  cookie is set, all in one command; when Chrome refuses that, one by one, so a cookie
  Chrome refuses costs only itself: it is named, and the next save no longer keeps it.
  Then each kept origin's local storage, most recently used first, is written on the
  borrowed base tab, given back after the last one. The write checks the page's origin
  before it writes: a stub that landed elsewhere — Chrome's HTTPS upgrade of a kept
  `http://` site — writes nothing, and the site stays kept, named as not restored. Only
  a site whose page refuses the write (its script throws: a quota) is dropped from the
  kept file and named. Any other failure — the bound out, the connection closed, the tab
  or Chrome dead — stops the restore there: that site and every one after it stay kept
  and are named as not restored, and an empty read of them in this browser cannot erase
  them. Cookies that do not get in — Chrome not reached, or the bound out before they
  are set — and a base tab that is another one after the restore — Chrome died and
  agent-browser relaunched it empty — leave the browser without the kept cookies: its
  saves are refused (see **The save**), so they stay kept, and no later call reuses it.
  A restore never restarts the browser.
  Measured on a Mac with agent-browser 0.39.1 and Chrome 157: a fresh browser got back an
  httpOnly cookie and the local storage of four sites — among them a dead `.invalid`
  origin, a site whose service worker hangs every navigation and a page that never
  finishes loading — in 85 ms, with no request to any of their servers; 20 origins take
  about 0.2 s (`TestRestoreBrowserSignIn_AFileThatIsNoKeptStateIsSetAside`,
  `TestRestoreBrowserSignIn_EveryKeptCookieAndSiteComesBackWithNoSiteLoaded`,
  `TestRestoreBrowserSignIn_ASiteChromeRefusesLosesOnlyItsOwnStorage`,
  `TestRestoreBrowserSignIn_ARestoreThatRunsOutDropsNothing`,
  `TestRestoreBrowserSignIn_AChromeThatDiesMidRestoreKeepsEverything`,
  `TestRestoreBrowserSignIn_ASiteChromeUpgradesToHTTPSIsKeptNotWrittenElsewhere`,
  `TestRestoreBrowserSignIn_CookiesThatDoNotGetInStopTheSaves`,
  `TestRestoreBrowserSignIn_ACookieChromeRefusesCostsOnlyItself`).
- **The borrowed base tab.** zcp opens no tab of its own: agent-browser adopts every tab
  it discovers (its daemon drains Chrome's target events every 100 ms), and a stub tab
  opened and closed behind its back left it a stale entry — on a Mate with agent-browser
  0.39.4 every hand-back then failed closing a tab agent-browser no longer had, which
  stopped the warm browser after every call, and a new browser's next command waited
  out agent-browser's 30 s read limit. So zcp borrows the base tab, which agent-browser
  keeps on `about:blank` and never runs a call in, for one restore or save, and gives it
  back whatever happened, its bound run out included (2 s of its own): back on
  `about:blank` and its DevTools session detached, which takes the request answering and
  the service-worker bypass with it. The warm browser runs Chrome with its back/forward
  cache off (`AGENT_BROWSER_ARGS=--disable-back-forward-cache`, the same on every command
  so agent-browser never relaunches for it), so no stub page sits there, a renderer apiece,
  while the browser idles: with the cache on, a Mate's idle browser held 15 renderers
  against 11, and Chrome 157 refused clearing the base tab's history there ("Not attached
  to an active page"). A call's tabs close when it ends, so the cache held nothing else. agent-browser's tab list is the same before and after
  (`TestSaveBrowserSignIn_OpensNoTabAndGivesTheBaseTabBack`,
  `TestBrowserEnv_TheWarmBrowserKeepsNoPageInItsBackForwardCache`). Measured on a Mac with
  agent-browser 0.39.4 and Chrome 157 through zcp's own steps: a fresh browser's restore
  10 ms, each hand-back (save, tabs closed) 72–79 ms with no error, the next `tab` call
  reused the warm browser, and a `new-browser` call took the browser's start (1.1 s) +
  restore (42 ms) + setup and walk (0.1 s).
- **What the call says.** A call whose new browser could not restore all of the kept
  sign-in says so in its result's `keptSignIn` field, never in `message`, and it never
  marks the call failed; a later call on the same browser does not repeat it.
  `zerops_verify` passes it on in its HTTP check's `keptSignIn` and keeps the rendered
  body text. The words, joined as they apply: a file set aside — "The kept sign-in could
  not be restored: its file could not be read, so it was set aside. This call ran signed
  out; signing in again keeps the new sign-in."; dropped sites — "The saved local storage
  of <sites> was dropped: the page refused to take it back while this call restored the
  kept sign-in. Sign in there again if it needs it."; refused cookies — "Chrome refused
  the kept cookies <name (domain), …>, so they are no longer kept. Sign in there again if
  it needs it."; sites not restored — "The saved local storage of <sites> was not
  restored in this call; it is still kept."; a browser whose base tab cannot be read after
  the restore — "The kept sign-in was restored, but this browser could not be confirmed as
  the one it went into, so a sign-in made in this browser is not kept; the kept sign-in is
  still kept for the next new browser."; cookies not restored — "The kept sign-in's cookies were not
  restored in this call. They are still kept for the next new browser; a sign-in made in
  this browser is not kept."; and "The rest of the kept sign-in was restored." only after
  dropped sites or refused cookies when it is true. Up to three sites or cookies are
  named; no value is
  (`TestRestoreBrowserSignIn_ABrowserItCannotConfirmSaysSoAndHoldsNothing`,
  `TestBrowserBatch_ASignInThatCouldNotBeRestored_IsItsOwnField`,
  `TestRenderHTTPRoot_AKeptSignInNote_KeepsTheRenderAndIsPassedOn`).
- **What the browser holds.** zcp keeps, in the browser's runtime folder — so it dies with
  the browser — a presence file (`zcp-signin.json`, written inside the call's lock, 0600,
  never through a symlink): whether the browser holds the kept cookies (their restore
  succeeded, or none were kept), and the origins an `open` step's page ended on in this
  browser, read from each step's answer: those, and the call's own page, are the origins
  a save reads. It also keeps the kept origins with storage this browser did not get
  restored, and the base tab's CDP target when the kept sign-in was restored:
  agent-browser relaunches a Chrome whose process exited or whose connection died — an
  OOM kill, which `OOMScoreAdjust=1000` makes likely — on the next command, inside the
  same service, with a new, empty profile and a new base tab (`cdp/chrome.rs`,
  `actions.rs`), so another base tab means the kept sign-in is no longer in the browser.
  A call that keeps the sign-in reuses the warm browser only while it holds the kept
  cookies in the Chrome they were restored into; any other — Chrome relaunched, or
  cookies not held — is stopped and the call restores into a fresh one
  (`TestRecordBrowserOpened_TheOriginAnOpenEndedOnIsRead`,
  `TestKeptSignInLetsReuse_OnlyTheChromeTheSignInWasRestoredInto`).
- **The save.** At the end of every call that hands the browser back warm, inside the
  call's lock, before its tabs close and the marker says `idle`, under one bound of 5 s.
  A browser that does not hold the kept cookies saves nothing — logged — so a call that
  restored them only in part never writes its own over them; nor does one whose base tab
  is no longer the one they were restored into — Chrome relaunched empty during the
  call: that save is refused and logged, the browser is left holding nothing so no later
  call reuses it, and the call's `keptSignIn` says "Chrome restarted empty during this
  call, so the kept sign-in was not in it: it is still kept for the next call, and a
  sign-in made in this call was not kept." Otherwise zcp reads every cookie of the
  default context and, on the borrowed base tab, the local storage of each origin it reads: every
  origin an `open` step ended on in this browser, and the origin of the call's own tab's
  page — the one the setup opened, found in the tab list whatever is active, so a popup
  still open on an identity provider never puts its site in the kept file. An origin this
  browser did not get restored is not read, so opening it cannot erase storage it never
  got; one whose read fails — the stub landed elsewhere, an HTTPS upgrade among them — is
  logged and keeps its kept entry. The connection must find the base tab agent-browser
  listed: a Chrome relaunched between the tab list and the read saves nothing, is left
  holding nothing, and the call's `keptSignIn` says Chrome restarted empty, as above. Session storage is never
  read. zcp merges the result with the kept file: the saved cookies replace the kept
  ones, since the browser holds every kept cookie; each origin read is what the save
  holds for it, nothing when its storage is empty, so a clear sticks even after the page
  left the site; a kept origin the save did not read keeps its kept entry. The result is
  written into the directory's `.tmp` and renamed over `zcp-default.json` only once it is
  a kept state, so a save that fails or runs out leaves the kept file as it was. It is
  logged and neither fails the call nor recycles the browser. So a sign-in survives a
  recycle that follows within seconds. Measured on a Mac with agent-browser 0.39.1 and
  Chrome 157: a save reading three sites — one whose service worker hangs every
  navigation, and the call's own page still loading an image that never answers — took
  53–70 ms, with no request to any of their servers. agent-browser saves nothing itself:
  no periodic save and no save as its daemon exits idle. Nothing is lost by that: a page
  runs only inside a call, and every clean call ends with a save. A browser zcp stops — a
  recycle after an unclean call or a wedge, `new-browser`, the max age, a `fresh-context`
  call's end — saves nothing: the last good save stands
  (`TestHandBackBrowser_SavesTheSignInBeforeTheBrowserGoesIdle`,
  `TestHandBackBrowser_AFailedSaveStillHandsTheBrowserBack`,
  `TestSaveBrowserSignIn_ABrowserWithoutTheKeptCookiesSavesNothing`,
  `TestSaveBrowserSignIn_KeepsTheSitesTheBrowserDidNotRead`,
  `TestSaveBrowserSignIn_TheWayOutEmptiesWhatItClears`,
  `TestSaveBrowserSignIn_AClearSticksWhereverThePageWent`,
  `TestSaveBrowserSignIn_ASiteNotRestoredIsNotErasedByOpeningIt`,
  `TestSaveBrowserSignIn_ASiteWhoseReadFailsKeepsItsStorage`,
  `TestSaveBrowserSignIn_ReadsTheCallsOwnPageNotAPopups`,
  `TestSaveBrowserSignIn_ASaveThatRunsOutKeepsTheLastSave`,
  `TestSaveBrowserSignIn_AnotherChromeThanTheListedOneSavesNothing`,
  `TestHandBackBrowser_AnotherChromeThanTheListedOneSaysTheSignInWasNotKept`,
  `TestHandBackBrowser_AChromeRelaunchedDuringTheCallSavesNothing`,
  `TestBrowserBatch_WhatTheHandBackFoundReachesKeptSignIn`,
  `TestReleaseBrowser_ARecycledBrowserIsNeverHandedBack`).
- **Use and expiry.** Each kept origin carries its last use, `zcpLastUsed`, set when a
  save reads it — an `open` step ended on it, or it was the call's own page; a restore
  alone is not use. agent-browser 0.39.1, which once read the file, ignores the field:
  its `OriginStorage` (`cli/src/native/state.rs`) derives `Deserialize` without
  `deny_unknown_fields`. An origin no call used for 7 days — a site gone or renamed — is
  dropped at the next restore or save, and only the 20 most recently used are kept, the
  latest first, so a restore stays bounded; an entry without a last use counts as used at
  that moment. The whole file's own 7-day expiry stays
  (`TestRestoreBrowserSignIn_ASiteUnusedForAWeekIsDropped`,
  `TestRestoreBrowserSignIn_KeepsTheTwentyMostRecentlyUsedSites`,
  `TestSaveBrowserSignIn_OnlyUseRenewsAKeptSite`).
- **Logged.** Every restore or save that fails, and every save refused, is logged on
  zcp's stderr with the kept file's path or the origin and Chrome's own message, never a
  value.
- **What is kept.** Every cookie of the default context (with its partition, for a
  partitioned cookie), and the local storage of every origin a save read in the last 7
  days, the 20 most recently used, until a restore drops it or a page of its site clears
  it. Session storage, the HTTP cache and service workers are not kept.
- **Where, and how private.** `$HOME/.agent-browser/sessions/zcp-default.json`, `$HOME`
  being `/home/zerops` on a Zerops container: outside the workspace `/var/www`, which
  holds git checkouts and is mounted into dev services, and no checkout itself. zcp
  resolves `$HOME`'s symlinks first; a `$HOME` that is relative, `/`, or `/var/www` or
  inside it keeps nothing, and every call runs signed out after a browser replacement.
  Before a keeping browser starts, zcp creates `~/.agent-browser`, `sessions` and its
  `.tmp` one by one with mode 0700 and refuses any of them that is a symlink, which
  fails the call naming the path; it tightens a looser one of them to 0700 and its own
  files (the kept file and the set-aside copy) to 0600; a symlink where one of its files
  belongs is removed, never followed; and it reads and writes its files without following
  a symlink. The unit's `UMask=0077` makes every file agent-browser writes 0600. The file
  survives a browser stop and a container restart (`$HOME` survives it); a redeploy may
  lose it (`TestReadyBrowserState_LeavesTheSignInPrivate`,
  `TestReadyBrowserState_RefusesASymlinkedDirectory`,
  `TestReadyBrowserState_ASymlinkedKeptFileIsRemovedNotFollowed`,
  `TestBrowserUnitArgs_FilesTheBrowserWritesArePrivate`).
- **Expiry.** As a keeping browser starts, zcp removes its kept file and its set-aside
  copy when they are older than 7 days — long enough to span a weekend and a paused task,
  short enough that a sign-in nobody uses does not sit in the container for weeks — and
  touches no other file in the folder, which other programs' saved sessions share
  (`TestReadyBrowserState_ExpiresOnlyItsOwnFiles`).
- **No encryption.** The file is not encrypted: a key would live in the same container,
  readable by the same user as the file, so it would protect against nothing the modes
  do not, and a lost or changed key would leave the file unloadable.
- **The way out.** A Mate forgets a sign-in by passing the signed-in page as `url` in a
  `tab` call and running `["cookies","clear"]` and `["storage","local","clear"]` before the
  call leaves it: that page's origin is one an `open` step ended on, so the save reads
  it even after the page then leaves the site (an app's sign-out redirecting to an
  identity provider, a popup), keeps no cookie and nothing for that site, and the next
  browser, even after a `SIGKILL`, starts signed out there. A site reached only by a click
  is read only while the call's own tab is still on it as the call ends; another site's
  saved storage is that site's to clear. The tool's `isolation` words say so: "To forget
  a sign-in, pass the signed-in page as url in a tab call and run ["cookies","clear"] and
  ["storage","local","clear"] before the call leaves it; the next browser starts signed
  out there."

### 2.7 Base path on the mate side

nginx strips the prefix (§2.4); the server learns its **public** prefix from `T3CODE_BASE_PATH`
(zcp passes no `--base-path`, §2.2) and joins it onto every absolute URL it emits (`/ws`,
well-known). The container serves no web client: only the hosted client is released (§3.13), and
it addresses a Mate at `<container origin>/mate/`. The descriptor's `basePath` is
**optional**: an older server stating none is still accepted, so a client pointed at a server
published under another prefix reaches a descriptor that disagrees — a mismatch a status code alone
cannot see.

A request still carrying the configured prefix past nginx (a `proxy_pass` missing its trailing
slash) gets a **loud `404 application/json`** naming the mistake. A tolerant ingress-side strip was
dropped — any middleware passed to Effect's `HttpRouter.serve` leaves its `R` type parameter
unsolvable, a structural dead end. Consequence: code-server's `/proxy/3773/` door and
direct-to-3773 access do not work; `/mate/` is the only supported origin.

### 2.8 The zcp↔mate contract

zcp and mate ship from two repositories on two schedules, and the coupling between them is the handful
of facts below — none of which either side can change alone. The facts are numbered as **contract 2**:
a release declares the contract it satisfies in `stable.json` (§2.1c), zcp carries
`SupportedContract`, and a release declaring a number zcp does not know is refused rather than
installed. Changing any fact below is a contract bump on both sides, except an environment key every
release at or above `MinimumMateVersion` ignores when added or reads as its own default when absent;
so is a flag zcp starts passing that a release at or above `MinimumMateVersion` would reject — a
setting older releases may lack rides the environment instead, never a probed flag (§2.2).

| # | The fact | Owned by |
|---|---|---|
| C-1 | The artifact is `zerops-mate-<version>.tgz`, a GitHub release asset on `zeropsio/mate`, whose npm `bin` entry is `mate` at `node_modules/.bin/mate` (`mate.BinName`), and whose release publishes `stable.json` beside it (§2.1c) | the fork's `pack` + release workflow |
| C-2 | `serve` accepts `--mode web --host --port --base-path --base-dir --no-browser --auto-bootstrap-project-from-cwd` with the working directory as a trailing **positional**. **An unknown flag is fatal**, so zcp passes only flags every release it drives accepts; it sets the prefix through `T3CODE_BASE_PATH`, not `--base-path` (§2.2) | the fork's `serve`; mate ZC1 |
| C-3 | `T3CODE_ZEROPS_{PROJECT_ID,API_HOST}` keep their meaning, and a non-empty `PROJECT_ID` remains the sole Zerops-environment signal (§2.3, §3.1). The server links to HQ with the project key it reads from the env store (§3.5). The installed package declares the shared HQ origin as `mate.hqOrigin` in `package.json`; zcp reads it when `T3CODE_ZEROPS_HQ_ORIGIN` is unset | the fork; mate EV3 |
| C-4 | Liveness is `GET {basePath}/.well-known/t3/environment` → `200 application/json` carrying `basePath` (§2.5) | fork's environment descriptor |
| C-5 | The server binds loopback only and never claims a declared platform port (§2.4) | zcp's `ServeArgv`, fork's `--host` |
| C-6 | **`/mate` is the server's public prefix, and the hosted client addresses it.** zcp sets it through `T3CODE_BASE_PATH` from `mate.BasePath` (§2.2) and nginx serves only the server's API and WebSocket there (§2.4). The released artifact carries no web client and `pack` requires none; moving the prefix is a coordinated two-repo change (zcp, the hosted client) | zcp's `mate.BasePath` + the hosted client |
| C-7 | The Mate's server keeps the Git credential HQ sends it in its memory only, for the `gitFingerprint` its hello reports (§3.3); it writes no file, and after a restart it holds none until HQ sends one on the next link. zcp neither reads nor names it: what Git presents to HQ is the project key (§2.8). The new-Mate import writes `MATE_SETUP` beside the project key; the server checks for that name once in its environment or live env store, and without it settles server-initiated stand-up as not due. Why a new Mate's stand-up waits is the server's own word from its link (`/setup.json`: `not_linked` with the link's reason, §3.5) | the fork, the hosted client (the import); mate CR12 |
| C-13 | **A Mate's project admits its container over SSH.** From the moment the `zcp` container is imported, the project's `sshIsolation` is `vpn service@<container>`: people on the VPN and the container may SSH into every service of the project, app code into none. The hosted client's container import (a birth and Finish setup) updates the platform's own entry just before it imports the container; zcp never writes the project's value and relies on it for SSH into every service without a value of its own (a dev half, a stage, a mount, a deploy's readiness check). A service's own value holds for that service alone | the hosted client |
| C-14 | **A Mate's project shares its services' variables with its container.** Before the import the press sets the project's `envIsolation` to `service service@<container>` (the platform's own entry, updated, never added; equal already → nothing written). Every service that states no isolation of its own inherits it, also one added later, and the platform writes its variables into the container's live env store as `<hostname>_<key>` (`docs/spec-zerops-env-lifecycle.md` §5); a service that states its own keeps it. The data console reads a value the API withholds from the Mate's key there (§5.7). A project the press did not set (one born before it, or changed since) is brought over by the container itself: at every start, a start after a zcp update included, `zcp init` in a Mate's container reads the project's `envIsolation` once and, when it does not admit the container (`service@<container>` or `none`), updates the entry with the Mate's own key, adding `service@<container>` and keeping every other rule, and prints the one change; a `-service@<container>` rule is someone's choice and stays, as does every service's own value. A refusal is printed once by that start, the start completes and nothing retries; the data console then answers `no_credentials`. A container that is no Mate never asks. A Mate's container reads every service's variables of its project: a `service@<container>` removed by hand returns at the next start, and only `-service@<container>` keeps the container out | the hosted client (the press); zcp `ops.AdmitContainerToProjectEnv` (the start); `TestAdmitContainerToProjectEnv_ProjectNotAdmittingTheContainer_GainsItKeepingEveryOtherRule`, `TestRun_MateContainer_AdmissionRefused_InitCompletes`, `TestRun_MateDisabled_NoAdmission` |
| C-15 | **Every variable Mate or zcp writes on a Mate's container for itself is named under a hidden rule.** The Mate's own vault is the container service's user variables minus two rules of names (§5.8a): in every vault `ZCP_*`, `MATE_*`, `GITEA_*` and `GIT_TOKEN`; in the Mate's own vault also `T3CODE_*`, `VSCODE_PASSWORD` and `*_LEASE_TOKEN`. zcp and the client each carry both rules and they agree, name for name, case-sensitive. A variable either side starts writing on the container for itself is named under one of the rules, or both lists gain its name in the same change: otherwise the person sees it in the Mate's vault as a value to change or remove, and the agent may list it and hand it to a command. Adding this fact changes no release's contract number: every release at or above `MinimumMateVersion` already writes only such names | zcp `ops.IsVaultHidden`, `ops.IsOwnVaultHidden`; the client; `TestEnvTool_OwnVault_HidesTheContainersOwnMachinery`, `TestEnvTool_TheContainersMachineryNames_StayOrdinaryInSharedAndAnAppsVault` |
| C-16 | **zcp names the tool call that holds the browser.** `/run/zcp-browser/default.call` holds `{"callId","client","since","targetId"}` while a call holds the browser: the calling client's own tool-call id from the `tools/call` request's `_meta`, verbatim (Claude Code's `claudecode/toolUseId`, its `toolu_…` id), the client's `initialize` name, when the call took the browser (ms since the epoch) and the CDP target of its own tab. zcp writes it in one rename once the call holds the browser lock and has its tab, removes it when the call lets go however it ended, and the next call to take the lock removes a file a dead zcp left. No file is nobody Mate can name driving; zcp never writes a guessed or generated id (§5.6). Adding this fact changes no release's contract number: a release without a reader ignores the file, and a zcp without a writer leaves none, which reads as the card's placeholder | zcp `ops.publishBrowserCall` (`internal/ops/browser_call.go`), `tools.browserCall`; the fork, mate AC7; `TestPublishBrowserCall_ACallWithAnID_NamesItsIDClientAndTab`, `TestPublishBrowserCall_ACallWithoutAnID_PublishesNoneAndClearsAStaleOne`, `TestBrowserCall_ClientsOwnToolCallID_IsTheCallVerbatim` |

- **Registration and access.** HQ resolves a presented project key and checks whether that same key
  can read its own value in clear among the project's and non-system services' variables, refusing
  it if visible; no organisation key is needed for registration. The Mate keeps no member list and
  reads no member of its organisation: HQ's ticket names the role a person enters at (§3). A stale
  known key is checked on a later non-Git admission,
  not by an hourly idle-link sweep.
- **Link.** The Mate dials HQ over native IPv6 only, then reconnects with backoff after the link
  ends. HQ sends JSON `{"type":"ping"}` and the Mate answers JSON `{"type":"pong"}`; HQ closes a
  silent link `4408`, counting any received application frame as liveness. When a state frame first
  advertises `pingIntervalMs`, the Mate also closes the link after no inbound frame for three of
  those intervals; an older HQ that never advertises it does not arm that deadline (mate HL5).
- **Stand-up.** Once the server sends stand-up, zcp reads the Mate's state file and, if applicable, the
  recipe once; a refused key is returned at once, without a link-wait loop. The shared HQ client may
  retry a `503` within its bounded budget (zcp `internal/tools/standup.go`, `internal/hq/client.go`).
- **Git.** Every git credential zcp answers for HQ's own host — `zcp hq git-credential` in the
  `zcp` container and a relayed `zcp git` call (§6.8) — is Basic `mate:<ZCP_API_KEY>`: the project
  key the container already has, read from the live env store at each call, which HQ asks Zerops
  about on the push. No new stored secret and no `serve` flag; a container without the key declines,
  so git never sends an empty password. The key reaches a dev service only over a relayed call's
  ssh session and is never written there: zcp's own git in a dev service — the wiring's fetch,
  the `main` check — runs as a relayed script too (`ops.RelayedSSH`), so a dev service stores no
  Git credential, and neither does a pool: zcp's git in a lease runs relayed too and the
  agent's through `zcp git <alias>` (`spec-workflows.md` §8 DP-12, DP-16) (zcp
  `internal/hq/gitcredential.go`, `internal/ops/git_relayed_script.go`;
  `TestGitCredential_AnswersOnlyTheLinkedHQ`, `TestRunHQGitCredential`,
  `TestBrokeredGit_Relay_HQFrame_AnsweredWithTheProjectKeyAnyPath`,
  `TestRelayedSSH_ZcpsScriptReachesHQWithTheProjectKey`,
  `TestRelayOnly_ADevServicesPushToHQIsAnsweredThroughTheRelayWithTheProjectKey`).

The Observe child credential belongs to the normal ZCP authorization context: Mate writes
`~/.zcp/hq/observe-credential` atomically with mode `0600`, or removes it on revocation;
zcp reads that fixed file for each observation and never writes it (mate CR6, CR9; zcp `internal/hq/observation.go`,
`internal/hq/observe_credential.go`). Its value is
`zobs.<orgId>.<32 random bytes as base64url>`, used only as `Authorization: Observe …`
on `POST /api/mate/observe`. It is not the Git credential, a project key, a provider
setting, or a model argument. No new `serve` flag or provider environment is involved.
HQ binds ordinary ZCP credential establishment to the verified person and exact native parent;
child issue, renewal and acknowledged delivery belong to HQ/Mate. No separate Observe consent
is required. The owner opening their own unauthorized Mate's conversation counts as the explicit
reconnect: on a current, non-stale unauthorized read the client submits `authorize-observe` once
per organisation and project in the session. A colleague's Mate or a stale read does not trigger it;
starting a turn and provider login do not establish the context (§3.3, §3.6).

The Mate's state reaches zcp the same way, through a file, never a call. The Mate's server keeps
`~/.zcp/hq/state.json` at the path its launch environment names (`ZCP_MATE_STATE_FILE`, §2.3), replaced
whole in one rename: `{ "link": "linked"|"not-linked", "reason"?: string, "heardAt": string|null,
"mate": object|null }`. A server whose launch names no file writes none, and the server hands the
variable to nothing it spawns. `mate` is the `mate` of the link's last `state` frame exactly as HQ
sent it, taken from the raw frame, never a credential. At its start the server keeps the `mate` the
file already holds for its own project and writes `not-linked`, `starting`; on every `state` it writes
`linked`; when the link ends or is refused it writes `not-linked` with why and keeps `mate`; when it
stops it writes `stopped`. A failed write leaves the old file. zcp only reads it, at each use, and its
service watches it replaced, never on a timer. zcp reads `linked` only while `zcp service start mate`
holds `~/.zcp/hq/state.lock`; with the lock free the server is not running. No file is no Mate server
here; a file zcp cannot decode is unreadable, never empty; keys zcp does not know are ignored. The state carries a required non-negative `rev`, the HQ revision at which it was read. Chief,
the wired repositories' origins, changes, the stand-up's application and link status come from
this file, and whether the application limits who merges: `mergeLimited: true`, beside
`openPerBranch`, present only while it does and absent from an older HQ. It names nobody and
changes only what zcp says (§10.10 *An application that limits who merges*). Beside them HQ says
what the application has: `production: "zerops" | "git" | "outside"` (how its production is
run; absent while it has none), `hasStage: true` (absent when false) and `mates`, how many Mates
the application holds, this one included — always present from an HQ that sends these facts,
absent from an older one. An absent `production` alone does not tell an application without one
from an HQ that sends none of them, so zcp reads "no production" only beside a `mates`; a kind
zcp does not know says nothing about production. They too change only what zcp says (§10.10
*What the application has*). Each successful change write returns `Mate-State-Rev`; zcp records that revision with
the change it wrote and does not decide its standing from a state with an older revision. No authorization is decided from the file. What zcp does with
it: §6.3,
_Release on archive and on completion_ (zcp `internal/hq/mate_state.go`,
`internal/tools/hq_dev_git.go`, `internal/hq/file_watch.go`).

The monorepo facts C-8…C-12 continue this table in §2.8a.

A project without a Mate uses the plain-project door (§6.7); it does not change this Mate
contract. Once the project has a Mate, zcp uses the
existing Mate routes and credential flow at the same repository addresses.

The Mate's hello carries `{type: "hello", key, serviceId, origin, gitFingerprint?}`.
`gitFingerprint`, when present, is `sha256(sha256(credential))`, where each `sha256` hashes the
UTF-8 string and returns lowercase hexadecimal: the outer hash takes the first digest's hex string,
not its raw bytes. It lets HQ retain that credential across a relink without sending its secret (mate
HL2).
`origin` is its public
origin: HTTPS, a bare origin, no path; HQ requires no particular host suffix. HQ validates it,
keeps it with the link and returns it in the ticket answer; an invalid origin closes the link as
bad hello. No Zerops read discovers it: the server takes it from the platform's `zeropsSubdomain`,
which reaches its environment through the live env store (§2.3, MD-8), so nothing is set by hand
(mate ZC2).
Without a public origin the Mate does not link and reports `not-linked` / `no_public_origin` (mate HL3).

The fork's release workflow triggers only on stable `v<major>.<minor>.<patch>` tags, so GitHub's
"latest release" *is* the stable channel — nightlies never produce one.

**The web client is not zcp's.** zcp does not build it, configure it, serve it or update it.
The hosted client at `https://mate.zerops.io` reaches the server over `{BasePath}/`; the
Mate release artifact contains the server and no web client (C-6).

### Invariants

| ID | Invariant |
|---|---|
| MD-1 | The init step never fails the container start, degrading instead — for a missing project or a unit-removal failure; it installs nothing. `TestRun_Mate_NoProjectID_Degrades`, `TestRun_MateDisabled_UnitRemoveFails_Degrades`. |
| MD-2 | `ServeArgv` carries no flag whose support depends on the installed bundle, and no launch runs `serve --help`; the public prefix rides `T3CODE_BASE_PATH` on every launch. `TestServeArgv`, `TestStart_Mate_Argv`, `TestStart_Mate_BasePathRidesTheEnv`. |
| MD-4 | With the flag ON, `/mate/` and `/mate/healthz` render outside the cookie gate and code-server's `/proxy/3773/`/`/absproxy/3773/` are closed. `TestRunNginx_MateOutsideCookieGate`, `TestRunNginx_ClosesCodeServerProxyDoorToMate`. |
| MD-5 | `{BasePath}/healthz` answers before AND after the first `zcp init` completes, as parseable JSON, whether or not a step degraded. `TestRunNginx_HealthzServesTheInitMarker`, `TestRunNginx_HealthzFallbackIsValidJSON`, `TestRun_WritesInitCompleteMarker`, `TestRun_Mate_DegradedStepStillMarksInitComplete`. |
| MD-6 | A local (non-container) `zcp init` writes no env file and leaves no marker. `TestRun_NoMate_OutsideContainer`. |
| MD-7 | A request still carrying the base path past the proxy gets a named `404`, never the SPA shell; client-side helpers preserve a URL's prefix. |
| MD-8 | mate's process environment merges `~/.zcp/mate.env` over the container's live env store, read once at unit start; a changed environment reaches a running Mate at its next start. `TestLoadLiveEnv`, `TestMergeMateEnv_OrderAndPrecedence`, `TestMergeMateEnv_StoreNamesTheSubdomain_ServerGetsItsPublicOrigin`. |
| MD-9 | `{BasePath}/healthz` carries `Access-Control-Allow-Origin: *` on both branches; `/mate/` and the cookie-gated `location /` carry none. `TestRunNginx_HealthzHasCORSForCrossOriginProbe`. |
| MD-10 | The release list comes from `stable.json` and nowhere else: a manifest whose `contract` is not `SupportedContract` or whose `version` is below `MinimumMateVersion` is refused before any download; the tarball must match the manifest's `sha256` before npm runs; there is no registry-package fallback. An unreachable manifest starts `current`; a version below the floor is never started. `TestDesiredRelease_*`, `TestInstallRelease_ChecksumMismatch_RefusesInstall`, `TestInstallRelease_DownloadFailure_RefusesInstall`, `TestRunMateStatus_JSON_ReportsTheFloor`. |
| MD-11 | **With `ZCP_MATE_ENABLED` unset, a container behaves exactly as one predating mate**: the rendered nginx.conf carries no `/mate`, no `3773`, no `healthz` and no marker path while keeping every non-mate structure; nothing is downloaded or installed; no unit is registered; no readiness marker is written; and `zcp init` prints no extra step line. `TestRunNginx_MateDisabled_RendersNoMateSurface`, `TestRun_MateDisabled_NoUnitFile_NoOp`, `TestDetect_MateDisabled_ByDefault`. |
| MD-12 | Disabling is a real reverse direction, not an absence of the forward one: a leftover unit is stopped and removed and `~/.zcp/mate.env` deleted, while `mate.Prefix()` is left on disk so re-enabling costs no network. `zcp service start mate` refuses under the off flag, so a unit surviving a failed removal cannot resurrect the server — reading the flag from the **live env store**, never from its own environment, because a systemd unit inherits neither (live-verified: a guard reading `os.Environ` crash-looped the unit on an enabled container). An unreadable store fails OPEN, since the unit exists only because an enabling `zcp init` created it. `TestRun_MateDisabled_UnitFilePresent_StopsAndRemoves`, `TestStart_Mate_GuardReadsLiveEnvStore_NotOnlyProcessEnv`, `TestStart_Mate_GuardRefusesWhenStoreSaysDisabled`, `TestStart_Mate_GuardFailsOpenOnUnreadableStore`, `TestStart_OtherServices_UnaffectedByMateGuard`. |
| MD-13 | A version is installed into its own directory, smoke-tested, and only then made `current` by an atomic symlink rename; any failure leaves `current` as it was. The start job is the only installer and the only writer of `current`, `last-good` and the failed version; the start job and the keep job read one version to run (`mate.VersionToRun`), never older than `last-good`; an installed dev build at or above `MinimumMateVersion` is kept under an active dev pin, and without one until a release newer than its base is out (§2.1a). |
| MD-15 | Prepare installs nothing and never restarts a running Mate; the start job holds the install lock from its download until its verdict, and a keep run that finds the lock held does nothing (§2.1, §2.1a, §2.1d). `TestLockInstall`. |
| MD-16 | Every flag in `ServeArgv` belongs to contract 2's flag list (C-2), asserted on the zcp side against a literal list and on the fork side by CI against `mate serve --help`; the env contract's key set is exactly `T3CODE_ZEROPS_{PROJECT_ID,API_HOST}`, both always written and nothing else. `TestServeArgv_FlagsAreContract1`, `TestEnvContract_KeysAreTheSpecList`, `TestEnvLines`, `TestRun_Mate_WritesEnvContract`; fork: `serve-contract.test.ts`. |
| MD-17 | `zcp mate status --json` reads `DesiredRelease()` and the versions the container holds and never installs; `zcp mate update` starts the keep job and answers at once; both answer JSON a caller can act on without parsing prose; every status but `--local` reads the release source (§2.1a). `TestMateStatus_*`, `TestMateUpdate_*`. |

### 2.8a Monorepo: dev servers, stages, the receipt (C-8…C-12)

An application is made of **parts**; a part is the `zeropsSetup` its stage/production
service names. This section is the cross-seam shape every monorepo lane builds against. Each fact
has one source; a reader never recomputes it. **A single-part app means exactly what it means
today**: every new key below is additive, and an app with one part shows no part name, no part
count and no receipt anywhere.

| # | The fact | Owned by |
|---|---|---|
| C-8 | `.zcp/dev-servers.json` in the repo root declares the dev servers (§2.8a.1) | zcp (writes on a set change), the supervisor (reads) |
| C-9 | `zcp-devsup`, run by the dev setup's `run.start`, runs the declared servers; its runtime state lives in `$HOME/.zcp/devsup/` and zcp drives it with `zcp-devsup ctl` over SSH (§2.8a.2) | zcp repo `cmd/zcp-devsup` |
| C-10 | The envelope's dev-service snapshot carries `stages[]` and `devServers[]` (§2.8a.3) | zcp `workflow.ServiceSnapshot`, the fork; the shared fixtures (§2.8a.7) |
| C-11 | A tier names a part by `zeropsSetup` beside its hostname (§2.8a.5) | zcp composer, HQ `tierRuntimes` |
| C-12 | An HQ change of an app with more than one part carries a `receipt` (§2.8a.6); zcp writes none | HQ change record |

All five are additive to contract 2: an older release ignores an unknown key, so none bumps
`SupportedContract`. New envelope keys follow the field order in §2.8a.3.

#### 2.8a.1 The declaration — `.zcp/dev-servers.json`

The file sits beside `zerops.yml` in the repo root (variant A: `/var/www`; variant B: the shared
volume, e.g. `/var/www/ws`). It changes only when the **set** of servers changes; start and stop
never write it.

```json
{
  "version": 1,
  "servers": [
    { "name": "web", "port": 3000, "command": "bun --watch index.ts", "cwd": "apps/web" },
    { "name": "api", "port": 8080, "command": "go build -o app . && exec ./app", "cwd": "apps/api",
      "watch": { "paths": ["apps/api"], "ignore": ["apps/api/app*"] } },
    { "name": "worker", "command": "bun --watch src/worker.ts", "cwd": "apps/worker" },
    { "name": "admin", "port": 4200, "command": "npx ng serve --host 0.0.0.0 --port 4200", "cwd": "apps/admin",
      "autostart": false }
  ]
}
```

| Key | Rule |
|---|---|
| `version` | required integer, format version; today `1`. A supervisor that does not know it runs nothing new and reports it (§2.8a.2 *Versions*) |
| `servers[].name` | required, `^[a-z][a-z0-9-]{0,39}$`, unique in the file. It is the key everywhere: state, logs, `ctl`, envelope |
| `servers[].port` | optional integer 1–65535, unique in the file; absent for a server without a listener (a worker) |
| `servers[].command` | required, non-empty; run as `/bin/sh -c <command>` in `cwd`, as the leader of its own process group, with the environment `run.start` gave the supervisor (the service's env vars) plus the overlay set with `ctl env` (§2.8a.2), an overlay entry replacing the service's value of the same name |
| `servers[].cwd` | optional, default `"."`; relative to the repo root; after cleaning and resolving symlinks it stays inside the repo root, otherwise the file is invalid |
| `servers[].watch` | optional; the supervisor restarts the server's group on a change under `paths` (relative to the repo root), never for paths under `.git`, `node_modules` or matching `ignore`. An `ignore` entry is matched against a repo-relative path one segment at a time with `path.Match`, a `**` segment matching any number of directories, and ignores the path it names and everything under it; a build output in a watched directory needs its glob — `go build -o app` also writes `app-go-tmp-umask` beside it, hence `apps/api/app*`, and `uv run` rewrites `src/<pkg>.egg-info` at any depth, hence `**/*.egg-info` (`TestWatch_AnIgnoreGlobMatchesAtAnyDepthWithDoubleStar`). Absent: the command's own watcher (`--watch`, vite) does it — so zcp never names a declared server as keeping the old code after a sync's fast-forward (docs/spec-workflows.md §8 DP-24) |
| `servers[].healthPath` | optional, default `/`; must start with `/`. The HTTP path a health probe of the server's `port` reads — an API part that answers 404 on `/` names the path it answers on (`TestParseDeclaration_Shapes`). The supervisor counts the server `running` only once a `GET` of it first answered (§2.8a.2 `status`) |
| `servers[].service` | optional; the hostname of the dev service whose supervisor runs it — in variant B every entry carries it; in a repository whose parts are each a Swarm dev pool it is the part's pool, whose containers' `hostname` is the pool's, and an entry a lease's start adds carries it (docs/spec-workflows.md §8 DP-22), and moving a dev service serving several parts onto one pool per part requires every part's servers to name its pool (docs/spec-workflows.md §13 PM-11, PM-12) — from then on the dev service's own supervisor runs none of them. Absent: run by every supervisor reading the file (variant A: the one) (`TestDevServer_LeaseDeclaresItsPoolAsService`, `TestDevServer_LeaseReadsItsPoolsEntries`) |
| `previewPortBase` | optional integer, top level; read in a Swarm pool container only (docs/spec-workflows.md §8 DP). The supervisor forwards `previewPortBase + ZEROPS_StableNumber` to the preview server's port, so each lease answers on its own per-port subdomain; while the server is not `running` the port answers the holding page instead (§2.8a.2 *Preview port*). `ZEROPS_StableNumber` starts at 1, so the pool's `run.ports` declares `previewPortBase + 1` through `previewPortBase + maxContainers` with `httpSupport: true`. zcp reads it, never writes it. To a running server the forward is a byte copy that passes a half-close on: a client that shuts its write side after its request still reads the whole reply, and a connection ends once both directions have, 30 s after the first did, or with the forward. A forward that ends stops listening and cuts its connections — and a dial to the server still under way — at once, and the next forward never waits for it (`TestSupervisor_PreviewForward_SlotPort`, `TestDeclaration_PreviewPortBase_*`, `TestForward_ClientHalfCloses_GetsTheWholeReply`, `TestSyncPreview_ClosingAForward_NeverHoldsTheNextSync`) |
| `siblingEnv` | optional object, top level: an environment variable a part's dev server reads → the pool (`^[a-z0-9]{1,40}$`) whose lease of the same branch it reaches, e.g. `{"HQ_HOST": "hqpool"}`; the app keeps reading its own variable. A name matches `^[A-Za-z_][A-Za-z0-9_]*$` and is never `ZEROPS_*`, `hostname`, `serviceId`, `projectId`, `PATH` or `HOME`; a broken rule makes the file invalid like any other. The supervisor never puts it into any environment: what reaches a server is only the overlay set with `ctl env` (§2.8a.2). zcp reads it, keeps it when it rewrites the file and never writes it: before it starts or reloads a lease's servers it sends the full overlay — each name set to the container hostname of the branch's lease on the named pool, a pool the branch holds no lease on left to the container's own value — to a supervisor whose `version` lists `env` (docs/spec-workflows.md §8 DP-22), and hands every other lease of the branch the recomputed overlay when a lease joins or leaves it (DP-26) (`TestParseDeclaration_SiblingEnv_Shapes`, `TestSupervisor_EnvOverlay_DeclaredSiblingEnvNeverReachesAServer`, `TestDeclareDevServer_KeepsTheSiblingEnv`, `TestDevServer_LeaseSiblingEnv_SendsFullOverlayBeforeStart`) |
| `siblingPreviewEnv` | optional object, top level: an environment variable a part's dev server reads from a browser → the pool (`^[a-z0-9]{1,40}$`) whose lease of the same branch it reaches, e.g. `{"VITE_HQ_URL": "hqpool"}`; the name rule is `siblingEnv`'s, and a name is never in both, or the file is invalid. Where `siblingEnv` hands a container hostname, which only server-side code resolves, zcp sets each name to that lease's preview URL — `https://`, the pool's subdomain on `previewPortBase` + that lease's container's stable number, as `zerops_dev_server action=status` reports it — resolved at every point it resolves `siblingEnv` and never hand-copied, since the preview port follows the container (docs/spec-workflows.md §8 DP-31). A top-level key of its own, so a supervisor that predates it ignores it; the supervisor never puts it into any environment: zcp sends the value through `ctl env` (§2.8a.2). zcp reads it, keeps it when it rewrites the file and never writes it (`TestParseDeclaration_SiblingPreviewEnv_Shapes`, `TestDeclareDevServer_KeepsTheSiblingPreviewEnv`, `TestDevServer_LeaseSiblingPreviewEnv_HandsTheSiblingsPreviewURL`) |
| `prepare` | optional string, top level; read in a Swarm pool container only (docs/spec-workflows.md §8 DP-14). After a lease's checkout zcp runs it with `/bin/sh -c` in the repo root, before it starts or reloads the supervisor: the incremental install a branch's own lockfile needs on top of the dependencies the pool's image baked into `/var/www`, e.g. `npm install --prefer-offline`. Its failure leaves the lease held and is reported in the acquire result. The supervisor never runs it; zcp keeps it when it rewrites the file and never writes one (`TestDeclaration_Prepare_ParsesAndRoundTrips`, `TestDeclareDevServer_KeepsTheRepositorysPrepare`) |
| `servers[].preview` | optional bool; the server the preview forward reaches when more than one server listens (with one listening server, that one) — of the entries a supervisor runs (`service`, above). At most one per supervisor: two entries marked preview make the file invalid when one supervisor would run both — the same `service`, or either naming none — so in a repository whose parts are each a pool, each pool marks its own (`TestDeclaration_PreviewPortBase_ParsesAndOldFilesUnchanged`). zcp reads it, never writes it |
| `servers[].autostart` | optional bool, default `true`; the server's **first** desired state: `autostart ? running : stopped` (§2.8a.2 *Events*). `false` keeps a declared server down until `ctl start` — a repo with many heavy dev servers (five Angular apps) starts only the ones it marks |

**Unknown keys.** The supervisor ignores a key it does not know, at the top level and in an entry, so
an optional key never needs a new `version`. zcp writes only the keys above.

HTTP support is **not** declared here: it is the dev setup's `run.ports[].httpSupport` in
`zerops.yml`, read by zcp (verify, URL). A port not in `run.ports` is internal only.

**A missing file** is an empty declaration: the supervisor starts nothing and stays up.
`zerops_dev_server action=start` without `name` behaves as today (one server per dev service) and
writes the file once with one entry named after the **dev service hostname**, so a single-part app
reports one dev server under its hostname. **An invalid file** (parse error or a broken rule) is
never overwritten by zcp; the supervisor keeps what it runs, starts nothing new and records the
error in its state.

#### 2.8a.2 The supervisor — `zcp-devsup`

A `zerops_dev_server` start that moved the host onto the supervisor and redeployed it carries `migratedToSupervisor: true` in its result (omitted otherwise); the Mate acts on that field, never on the message sentence.

**Binary and delivery.** `zcp-devsup` is built from the zcp repository (`cmd/zcp-devsup`) and
published on every zcp release as the asset `zcp-devsup-linux-amd64`, same version as zcp. zcp
writes the dev setup's two lines, pinned to the **newest published zcp
release whose supervisor `formats` include the declaration's `version`** — never to the release of
the zcp that writes them (a dev build has no asset). A release never drops a format, so this is the
newest release zcp's update check sees (`internal/update`, `releases/latest`); when no published
release reads the declaration's `version` yet, zcp writes no pin and says so.

**A pin is needed, and a pin stays.** Zerops reuses the runtime image `run.prepareCommands` built
while their text, `run.base` and `build.addToRunPrepare` stay the same (zerops-docs, pipeline
"Custom runtime image caching"), so a line naming whichever release is newest
(`releases/latest/download/…`) would install the release of the service's first build and keep it
until something else edits the line — a version no one can read off the file, different per
service. So zcp names a release, and a named release stays: a dev setup that already installs
zcp-devsup keeps the release it names, whichever it is, on the dev service's first deploy and on
every later one — a newer release out since is no reason to change a person's file in the middle
of their change (`TestAdoptSupervisorYAML`,
`TestDeploy_ADevSetupThatAlreadyRunsTheSupervisor_KeepsTheReleaseItPins`). Only a supervisor that
cannot run the declaration format zcp writes, or the one the repository's declaration carries
(missing from its `ctl version` formats), moves the pin: before a self-deploy of a dev service
deployed before, zcp points the zcp-devsup line its dev setup carries at the newest release,
changing nothing else, and the deploy reply names both releases and why — that deploy installs it,
nothing redeploys on its own, and a dev setup without the line stays as written
(`TestDeploy_ADevSetupWhoseSupervisorCannotRunWhatZcpWrites_IsRePinnedAndSaysWhy`). A deploy that
fails after the re-pin names it too
(`TestDeploy_ADeployThatFailsAfterTheSupervisorRePinStillSaysTheDevSetupChanged`) and says the
next successful deploy installs it
(`TestDeploy_ABuildThatFailsAfterTheSupervisorRePinSaysTheNextSuccessfulDeployInstallsIt`).

**zcp's own edit lands as a commit of its own.** What zcp edits in the repository unasked — the dev
setup's lines on a first deploy or a re-pin, and with a first start's move onto the supervisor (or
a kept server's carry) also `.zcp/dev-servers.json` — the reply names once, file by file, with why,
and zcp commits it before the push or redeploy, so that deploy ships it from git: a commit of its
own on the checked-out branch holding only those files, whatever else is staged, signed `Zerops
Agent <agent@zerops.io>`. The repository's commit checks run: a person's hooks are their policy, and
zcp never bypasses one (no `--no-verify`); since a pin no longer moves on its own, the commit is
rare — a first deploy, a supervisor that cannot run what zcp writes — so their cost is acceptable.
The container bounds the commit at the build-poll horizon (`ops.OwnEditCommitBudget`, 15 minutes),
and a client that asked for progress hears that zcp is committing and that the checks run, then
every 30 seconds that they still run. A check that refuses, or runs past the bound, commits nothing:
the files stay staged, and the reply gives the check's reason (the tail of its output) beside the
exact command to commit them once it is fixed. Where each branch is one change the person merges
(HQ, §10.10), the commit rides with the branch's change apart from the person's own commits, and
once merged `main` carries it, so no branch cut from `main` needs the edit again. zcp commits
nothing, and says what to commit with the command, when the checkout is on `main` or `master` —
only a merged change moves it, and a commit there could not be pushed — when one of the files held
uncommitted lines before zcp's edit, on a detached HEAD, or when git or a commit check refuses
(`TestDeploy_ZcpsOwnEditToADevSetup_LandsAsACommitOfItsOwn`,
`TestDevServer_TheMoveOntoTheSupervisor_CommitsBothFilesOnTheWorkBranch`,
`TestCommitOwnEdit_CommitsOnlyZcpsFilesOnTheWorkBranchAsZcp`,
`TestCommitOwnEdit_CommitChecksPastTheBudgetCommitNothing`). Left uncommitted, such a file blocks a
checkout of every branch that changes it and ships with every deploy from outside git.

**Crew save.** Before every push from a service, zcp runs `<git-common-dir>/t3-crew-save` when it is executable; absent → the push runs as before; non-zero → the deploy is refused with the save's error.

```yaml
run:
  prepareCommands:
    - sudo sh -c 'u=https://github.com/zeropsio/zcp/releases/download/vX.Y.Z/zcp-devsup-linux-amd64; f=/usr/local/bin/zcp-devsup; if command -v curl >/dev/null; then curl -fsSL "$u" -o "$f"; else wget -qO "$f" "$u"; fi && chmod 0755 "$f"'
  start: zcp-devsup run
```

The binary lands in the image, never in `$HOME` or `~/bin` (both are lost on redeploy).
`run --root <dir>` overrides the repo root (variant B); in variant B the supervisor runs only the
entries whose `service` is its own `hostname` (`--service` overrides it). For a variant-B dev
service zcp writes `start: zcp-devsup run --root <mountPath>` — the code volume's mount
(`ServiceMeta.WorkingDirFor`), where the shared declaration lives — and edits that dev setup in the
folder its deploy runs from; variant A keeps `zcp-devsup run`
(`TestDevServer_FirstStartOnAVariantBDevService_RunsTheSupervisorOnTheCodeVolume`,
`TestDeploy_NewVariantBDevSetup_RunsTheSupervisorOnTheCodeVolume`). The download line falls
back to `wget` because Alpine bases lack `curl`; it runs on both kinds of base (bun on Alpine,
go on Ubuntu).

**Dev override.** `ZCP_DEVSUP_URL`, an env var on the zcp service, replaces the release download
URL in the line zcp writes, so a dev build of the supervisor can be tried before any release. Dev
loop only; production never sets it.

**Variant B stand-up.** zcp sets up no new code volume; a stand-up called again on a recorded
variant-B pair (§2.8a.4a) still fills the volume when it is empty. The dev services start before
the repo is cloned onto the shared volume, so each supervisor first reads a missing file (an empty
declaration). After the clone, stand-up runs `zcp-devsup ctl reload` on every dev service's
supervisor; each starts only the entries of its own service
(`TestSupervisor_EachDevServicesSupervisorStartsOnlyTheServersAssignedToIt`).

**Runtime state.** `$HOME/.zcp/devsup/state.json`, written only by the supervisor (atomic rename),
beside `logs/<name>.log` and the control socket `sock`. `$HOME` survives a container restart and is
lost on a redeploy — which is exactly the start-up rule below.

```json
{
  "format": 1,
  "supervisor": { "version": "vX.Y.Z", "pid": 412, "startedAt": "2026-10-14T21:00:00Z" },
  "declaration": { "version": 1, "error": "" },
  "env": { "HQ_HOST": "node-id-7.runtime.hqpool.zerops" },
  "servers": {
    "web":    { "source": "declared", "desired": "running", "status": "running", "pid": 731, "pgid": 731, "leaderStart": 88213,
                "startedAt": "2026-10-14T21:00:01Z", "restarts": 0, "exit": null, "logPath": "/home/zerops/.zcp/devsup/logs/web.log" },
    "worker": { "source": "declared", "desired": "running", "status": "crashed", "pid": 0, "pgid": 0, "leaderStart": 0,
                "startedAt": "2026-10-14T21:00:01Z", "restarts": 0, "exit": { "code": 1, "signal": "", "at": "2026-10-14T21:03:10Z" },
                "logPath": "/home/zerops/.zcp/devsup/logs/worker.log" },
    "crew-ada": { "source": "runtime", "desired": "running", "status": "running", "command": "bun run dev", "cwd": ".crew/ada", "port": 4100,
                "pid": 902, "pgid": 902, "leaderStart": 90544, "startedAt": "2026-10-14T21:05:00Z", "restarts": 0, "exit": null,
                "logPath": "/home/zerops/.zcp/devsup/logs/crew-ada.log" }
  }
}
```

- `desired` (`running|stopped`) is the agent's last word; `restarts` counts starts after the first
  in this state file's life; `exit` is the last end the agent did not ask for `{code, signal, at}`
  or `null`: the server ending on its own (`code` -1 and `signal` such as `SIGKILL` when a signal
  ended it) or a failed start (`port_busy`; `start_failed` with the reason in the server's log). A
  stop or restart never overwrites it, so a crash stays visible after the server runs again;
  `leaderStart` is the leader's start time from `/proc/<pid>/stat`, the identity check below;
  `rssKb` is the resident memory of the server's whole process group in KiB at read time
  (omitted when there is no live group). zcp's `start` of a server that is not running reads it
  with the container's cgroup memory (used = `memory.current` less the page cache, `active_file` and
  `inactive_file`; ceiling = the `maxRam` the service's autoscaling can grow to, from the service's own
  `currentAutoscaling`, else `customAutoscaling`, else `memory.max`) and refuses the start only when
  a server runs and the headroom is below the largest running server's `rssKb` (floor 1 GiB, at most
  a quarter of the ceiling), naming the numbers (`TestDevServer_StartThatWouldLeaveLessHeadroom…`).
  With no server running nothing is refused, the migration's final start included
  (`TestDevServer_AFirstDevServerStartWithNoServerRunning…`, `TestDevServer_ADefaultAutoscaledService…`,
  `TestDevServer_PageCache…`).
- `status` is derived **once, by the supervisor**: `stopped` = desired stopped; `crashed` = desired
  running, no live group, `exit` set; `starting` = live group with a `port` whose `healthPath` has
  not yet answered, or whose port does not accept TCP now, or no live group and no `exit` (a start
  about to begin, a stop under way); `running` = everything else, so only a live group runs; a start
  still waiting for its port is `starting`. The `healthPath` answered when the server reacted to a
  `GET` of it on its port — a first byte of a reply, or the end of the connection — within 30 s of
  the request; a dev server that accepts at once and holds its first page while it compiles (Vite
  for over a minute) has not, and is asked again. Every server with a `port` is asked so: one that
  reads a `GET` and neither answers nor closes the connection stays `starting` — and holds back its
  siblings' one more starts — until, past the listen wait (10 min after the start) without an
  answer, the status follows the port alone (`TestPreview_PVAnswered_AcceptingIsNotRunningUntilTheHealthPathAnswered`).
  zcp and Mate read it, never recompute it.
  Beside `starting` only, `startPhase` (capability `start-phase`) says where the server is, from the
  supervisor's own start and clock: `launching` (a start under way — a restart's stop grace, the
  port wait, a boot's stagger — or no live group and no `exit`, even while the previous run had
  answered), `awaiting_port` (its process lives and its `healthPath` has not answered since this
  start — its port accepts nothing, or holds the `GET` — within the listen wait), `stalled` (that
  wait ran out without an answer and its port does not accept now), `listened` (its `healthPath`
  answered since this start and its port refuses now) (`TestStartPhase_STPhase_TheSupervisorsAlone`).
- `retryPending: true` (omitted otherwise) marks a `crashed` server whose one more start after a
  start crash, or after the preview server's exit (*Events* below), is still to come. A deploy or `zerops_dev_server` reply names such a
  server as coming back, and one still `starting` when its wait ends as still starting, each with
  when to read its status again — never as one to fix
  (`TestSupervisor_AServerThatCrashedWhileStartingSaysItsOneMoreStartIsPendingUntilItStarts`,
  `TestDeployNextActions_AServerThatCrashedWhileStartingWithItsOneMoreStartPendingIsNamedAsComingBack`,
  `TestDeployNextActions_AServerStillStartingWhenTheWaitEndsIsNamedWithWhenToCheckItAgain`).
- `taskLimit: {max, current}` (omitted otherwise) marks a `crashed` server whose start was refused
  with `EAGAIN` or during whose run the kernel refused a fork (the `max` count of the supervisor's
  cgroup `pids.events` grew): `max` is `pids.max`, `current` the `pids.current` when the state is
  read. A deploy or `zerops_dev_server` reply names the limit and the tasks running now — as coming
  back while `retryPending`, else with what frees tasks
  (`TestSupervisor_AServerThatCrashedWhenTheServicesTaskLimitWasReachedIsStartedAgainWithBackoffWithoutSpendingItsOneMoreStart`,
  `TestDevServer_AServerThatCrashedOnTheServicesTaskLimitNamesTheLimitAndTheTasksRunningNow`,
  `TestDeployNextActions_AServerThatCrashedOnTheServicesTaskLimitIsNamedWithTheLimit`).
- `outOfMemory: true` (omitted otherwise) marks a server whose last start is the boot start after
  systemd restarted the service because it ran out of memory (*Events* below); its
  `lastRestartReason` says so. A `zerops_dev_server` status names that reason and points to the
  service's max RAM (`zerops_scale`) or stopping a server; a deploy reply says it once for all
  servers (`TestDevServer_StatusOfAServerSystemdRestartedWhenTheServiceRanOutOfMemoryPointsToItsRAM`,
  `TestDeployNextActions_DevServersSystemdRestartedWhenTheServiceRanOutOfMemorySayWhyOnce`).
- `env` is the overlay `ctl env` last set, `{}` when none is: container hostnames and preview URLs, never a secret. It
  lives in `$HOME` with the rest of the state, so a container restart starts every server under it
  again, while a redeploy or a pool container's reset (a fresh `$HOME`) starts without it until it is
  set again (`TestSupervisor_EnvOverlay_SurvivesRestart`).
- `source: runtime` entries are not in the declaration: crew apps. They carry their
  own `command`, `cwd`, `port`, are added and removed only through `ctl`, and are not restored after
  a redeploy — their owner, Mate's engine, adds them again.
- `previewLastConnAt` and `previewOpenConns` ride on the preview server only, beside its
  `previewPort` (§2.8a.1), and only in `ctl status`: when its preview forward last relayed a
  connection to the server in this supervisor's life (absent until one did) and how many relayed
  connections it holds open now (`0` included). They are counted at the forward without delaying or
  changing a byte: the clients, never the forward's own dial to the server, nor the holding page and
  its held request (*Preview port* below), which no one's work is behind — an abandoned tab on it
  would keep the lease watched. A stop or restart of the server cuts the connections relayed to it; the forward stays, and
  so does the time. Only its shut ends a forward: an `Accept` that fails
  while it is open (every file descriptor in use, a connection aborted before it was taken) waits
  5 ms, doubling up to 1 s, and takes the next — a forward that ended there held its port bound,
  serving nothing, while the count read as observed (`TestForward_AnAcceptErrorNeverEndsTheForward`).
  The two fields live in the supervisor's memory: `state.json`
  never holds them, and a new supervisor starts without them. `ctl version` lists `activity` for
  them (`TestForward_RecordsLastConnectionAndOpenCount`,
  `TestSupervisor_PreviewActivity_KeptAcrossServerRestartsNeverInStateFile`,
  `TestCtlStatus_PreviewActivity`).

**Preview port.** The preview forward lives with the supervisor: it listens from boot and from each
`reload` whenever the declaration names a preview server with a port and the container has a
`ZEROPS_StableNumber`, whatever that server's status, and only the supervisor's shutdown or a
declaration without it ends it — a dev server that has not started, starts, crashed or is stopped
never leaves the port to the platform's error page (`TestPreview_PVForward_EveryStatusOfThePreviewServerIsForwarded`).
Each connection is decided when the port takes it, without a probe: to a `running` server it is the
byte copy of §2.8a.1; otherwise, and when the dial to a running server fails, the port reads the
request itself (and drops up to 1 MiB of its body, so the answer never ends in a reset) and
answers, with what holds once the request came, `503` and the holding page, `Cache-Control:
no-store` and `Connection: close`, so each later request is decided again. The page names the
server and says why: starting; starting again, with `lastRestartReason`; stopped with its `exit`
and one more start to come; or, with nothing to start it, stopped with its `exit`, or stopped by a
`stop` (`TestPreview_PVPage_SaysWhetherSomethingWillStartTheServer`). It renders in Mate's
sandboxed frame, which allows scripts and, for a page on another origin, its own origin: inline
style and script only, nothing fetched but its one held request, a `GET` of the reserved path
`/.zcp-devsup/hold?page=<id>`, the id naming the page. The port answers that path itself while
the server is not `running`, with `Access-Control-Allow-Origin: *` so the page's opaque origin
reads it: `204` as soon as it would answer anything but that page — the server runs and its port
takes the dial, or the page now says something else, a page that says the server stopped
included, so it opens the server once someone starts it — and it holds while the page would be the
same, so no page reloads into itself (`TestPreview_PVHold_TheHeldRequestReturnsOnceThePortWouldAnswerAnotherPage`).
After 25 s with nothing changed it answers `204` with `X-Zcp-Devsup: again`, on which the page asks
again; it reloads on every other answer, and 2 s after the request failed. A running server gets
the path like any other, and the page reloads on its answer too. The held request is the page's
only channel to the supervisor: it starts when the page is served and ends when what the port
would answer changes, the bound passes or the tab closes. A `stop` and an exit reach it at once,
before the group's grace; a shutdown lets the port go before it stops the servers. `ctl version` lists `hold` for the port, the page, the reserved
path and the `healthPath` rule of `status`: a client that reads `hold` shows the frame and leaves
starting, stopped and restarting to the page.

**Control.** zcp (and Mate's engine) run `zcp-devsup ctl <verb> --json` over SSH; `ctl` talks to the
supervisor over the unix socket and prints one JSON answer. Every process is a child of the
supervisor, never of the SSH session, so an SSH session killed by OOM loses only the answer; every
verb is idempotent, so the caller asks `status` again.

| Verb | Effect |
|---|---|
| `status [name]` | the `servers` map above (all or one) plus `supervisor` and `declaration` |
| `wait <name> --timeout <seconds>` | the same answer as `status <name>`, when the supervisor is no longer bringing the server up or the required timeout (1–600 s) passes, whichever comes first. A start in progress, an awaited port or a crashed server with a retry pending is awaited; a running, stopped, stalled, listened or finally crashed server answers at once. It changes nothing (capability `wait`; `TestStartPhase_STAwaited_OnlyWhileTheSupervisorBringsItUp`) |
| `start <name>` / `stop <name>` / `restart <name>` | sets `desired` and acts; never writes the declaration. `start` and `restart` first re-read it as `reload` does, so an edited entry runs as edited; a server that re-read already restarted is not restarted again (`TestSupervisor_StartAndRestartRunTheDeclarationAsEditedWithoutAReload`) |
| `reload` | same as `SIGHUP` |
| `logs <name> [--lines N] [--since <cursor>]` | the tail of `logs/<name>.log`. With `--since` (capability `logs-since`): `{name, lines, cursor, dropped}`, the complete lines the log gained after an earlier `--since` answer's `cursor`, the newest `N` of them, `dropped` counting the others among its last 256 KiB; the next read starts at `cursor`, past the last complete line, so a line still being written comes whole later. A cursor of another file — empty, a log rotated away, a new `$HOME` behind the same alias — reads the log from its start (`TestLogSince_LGSince_OnlyCompleteLinesAfterTheCursor`, `TestLogSince_LGOtherFile_ReadFromItsStart`) |
| `run --name <n> --cwd <d> [--port <p>] -- <command>` / `rm <name>` | add / remove a `runtime` entry |
| `env (--set NAME=VALUE)…` / `env --clear` | replaces the env overlay with exactly the `--set` pairs (`--clear` alone: none) — it never merges, so a full set also drops whatever a previous holder left; neither flag is refused (`status` shows the overlay). A name breaking the `siblingEnv` name rule (§2.8a.1) is refused and changes nothing. The answer — the state, `env` included — comes as soon as the overlay is stored in `state.json`; every server started from then on (boot, `start`, `restart`, `reload`, `run`, a watched change, a retry) gets the service's environment plus the overlay. The live servers whose group started under another overlay are then restarted by the supervisor itself, one at a time a stagger apart, each with `lastRestartReason: "env changed"` — a stop's grace of up to 10 s per server could outlast `ctl`'s 60 s wait. A stopped or crashed server is not started by it, and an identical overlay restarts nothing (`TestCtl_Env_AnswersBeforeTheRestarts`, `TestCtl_Env_ReplacesTheOverlay`, `TestCtl_Env_RejectsReservedAndMalformedKeys`, `TestSupervisor_EnvOverlay_AppliedAtEveryStart`, `TestSupervisor_EnvOverlay_ChangeRestartsRunningServersOnce`, `TestSupervisor_EnvOverlay_SameOverlay_NoRestart`) |
| `version` | `{"version":"vX.Y.Z","formats":[1],"capabilities":["env","activity","hold","logs-since","start-phase","wait"]}`; `capabilities` names the verbs, flags and answer fields an older supervisor lacks, and the behaviours a client reads off it: `env` the verb, `activity` the preview server's `previewLastConnAt`/`previewOpenConns` in `status`, `hold` the preview port of *Preview port* above, `logs-since` the `logs --since` flag, `start-phase` a starting server's `startPhase` in `status`, `wait` the read-only wait verb. An older `ctl` fails on a verb or flag it does not know, so a caller sends one only when `version` lists it, and reads such a field only then (`TestCtl_VersionNamesCapabilities`, `TestCapabilities_IncludeActivity`) |

Exit codes: `0` done, `2` no supervisor (socket absent or refused), `3` unknown name, `4` invalid
declaration, `5` supervisor not answering (the dial timed out or found the backlog full, or no answer
came in time). On `5` every `zerops_dev_server` action changes nothing and answers reason
`supervisor_unresponsive` — the container may be out of memory or CPU, check its load and memory and
retry — and a start never takes the move onto the supervisor below; a deploy's reply says the same
(`TestDevServer_ASupervisorThatDoesNotAnswerInTimeIsReportedUnresponsiveNotMissing`). A container
that does not answer the SSH call itself in time answers the same reason, naming the host, ssh's own
error and its class, with the container's load (`zerops_logs`, `zerops_scale`) as what to check and
never a restart: for a lease that would restart its pool
(`TestDevServer_AContainerThatDoesNotAnswerSSHInTimeIsReportedNotAnsweringWithItsRecovery`).
A start that fails still answers `0`; the failure is the server's `exit` in the answer.
When the supervisor still runs a declared server whose entry the file no longer holds (removed by
hand), every `zerops_dev_server` action first sends `reload`, which stops and drops it, and its reply
says that server was stopped (`TestDevServer_AServerRemovedFromTheDeclarationIsStoppedAndTheReplySaysSo`).
A file missing from the checkout removed nothing by hand: no `reload` is sent and status and logs leave
every server running (`TestDevServer_StatusAndLogsWithoutADeclarationFileLeaveTheRunningServersAlone`).
`zerops_dev_server` adds an undeclared server to the file once (then `reload`, then `start`). On a
dev service where `zcp-devsup` is absent or exits `2`, stop/status/logs change nothing and say so,
and the first `start` moves the dev setup onto the supervisor without asking: it writes the two
lines above into the dev setup (the meta's setup, else the only one, else the one named after the
hostname, else `dev`), declares the server being started, redeploys the dev service once from
inside (uncommitted work stays), starts the server under the supervisor and prefixes its answer
with one sentence saying what changed. A failed redeploy answers `supervisor_redeploy_failed`; the
files stay written, so the next deploy finishes the move. Nothing stays on the old SSH start.
The two lines are spliced into the bytes the user wrote — blank lines, comments, list indentation
and flow maps stay as written, and a file zcp cannot splice without changing anything else is
refused with the lines to add by hand. While any crew copy
(`.crew/<name>`, outside every deploy) has uncommitted or untracked changes, the move redeploys
nothing: it answers `crew_work_unsaved` with one sentence naming the copies and changes nothing
(decision C). A call with `workDir` never rewrites a declared server's `cwd`: a copy of the repo
runs as a `runtime` entry beside it (decision D).
`TestDevServer_FirstStartWithoutSupervisor_MigratesIt`, `TestAdoptSupervisorYAML`,
`TestAdoptSupervisorYAML_ChangesNothingButTheSupervisorLines`,
`TestDevServer_MigrationWaitsForDirtyCrewCopies`,
`TestDevServer_WorkDirOfACrewCopy_NeverRewritesTheDeclaredCwd`.

**A new dev setup** gets the two lines before it ever runs: the first deploy of a dev-mode
dynamic dev service (a self-deploy, the pair never deployed) — through `zerops_deploy`,
`zerops_deploy_batch` or `zerops_standup`'s dev-half deploy, one path for all three — writes them
into the dev setup it deploys when the pin rule above finds a release or `ZCP_DEVSUP_URL` is set,
and the deploy's `message` says so and asks for the edit to be committed (the stand-up report
carries each deploy's `message` and `warnings`); an edit that fails is one of its `warnings`. The
first `start` then finds the supervisor and redeploys nothing. Otherwise the dev setup keeps
today's shape and the first `start` moves it as above.
`TestDeploy_NewDevSetup_GetsTheSupervisorLines`, `TestStandup_FirstDeployPutsTheDevSetupUnderTheSupervisor`,
`TestStandup_ReportSaysWhatTheSupervisorStepDid`.

**A move's waits** are never silent: a client that asked for progress hears from zcp at least
every 15 s of every `zerops_dev_server` call (the server's keep-alive), and a move onto the
supervisor says when its redeploy begins and when the dev server's start begins after the build.
The keep-alive owns each call's progress stream: it numbers every notification on the call's token
(its own, a build poll's, a tool's) so the values rise, and the answer trails the last one by at
least 1 s, so a step that fails at once never shares a stdin chunk with a notification.
Its answer carries `migrationTimes` — the push, the build's wait with the checks after it
(`buildDuration` is the platform's own), and the dev server's start — in milliseconds.
`TestDevServer_MigrationSaysEachPhaseAndItsTime`,
`TestKeepAlive_DevServerStartAfterTheBuild_SendsProgressEachInterval`,
`TestKeepAlive_DevServerMoveFailingAtOnce_AnswersAfterTheGap`, `TestKeepAlive_ProgressRisesAcrossEverySource`.

**Stop** signals the **group**: `SIGTERM` to `-pgid`, up to 10 s, then `SIGKILL` to `-pgid` — only
after confirming the group is still the one it started (the leader's `/proc` start time equals
`leaderStart`; a reused pid is left alone, and so is a group whose leader is gone, since nothing
identifies it). A leader that ends on its own takes its group with it: the supervisor kills what is
left of the group at once, while the pgid still cannot be reused. **Start** waits up to 10 s for its `port` to be free;
still busy → the start fails with `exit: {code: -1, signal: "port_busy"}`; a deploy or `zerops_dev_server` reply names it as a server whose port stayed busy (`TestDeployNextActions_AServerWhosePortStayedBusyIsNamedAsZeropsDevServerNamesIt`). No `pkill`, `pgrep` or
command-pattern match anywhere.

**First desired state.** A declared name the state file does not hold yet — every name on an empty
`$HOME`, a name `reload` adds — gets `desired: running` when its `autostart` is `true` (the
default), else `desired: stopped`. From then on only `ctl start`/`stop`/`restart` change `desired`.

| Event | Behaviour |
|---|---|
| A server exits on its own | left down and visible: `status: crashed`, `exit` recorded, **no restart** in this container life except by a watched change (below), seen at the agent's next call. Two exceptions, one more start each: a server that exits within its **start window** (10 s after a start) or before its `healthPath` first answers, and the preview server (the one the preview port reaches, §2.8a.1) exiting after it answered, whom a person watches through that port, are started once more, one window later and only once no sibling is still starting (bounded by the listen wait), unless something started, stopped or removed it meanwhile — siblings started beside it (four Maven wrappers unpacking one distribution) come back one at a time; a crash of that second start, or of any other server after it answered, stays down (`TestPreview_PVOnceMore_ThePreviewServerIsStartedOnceMoreAfterAnExit`, `TestSupervisor_AServerThatCrashesInItsStartWindowIsStartedOnceMoreThenStaysDown`, `TestSupervisor_AServerThatCrashesBeforeItFirstListensIsStartedOnceMoreOneAtATime`, `TestSupervisor_AServerThatExitsStaysDownAndVisible`) |
| A server meets the task limit | every server shares the cgroup of the platform's `zerops@zerops.service` unit, which sets no `TasksMax`: systemd's default, 15 % of the container's `pids.max` 2000, is 300 tasks (processes and threads) for the supervisor and all its servers. The supervisor never raises it. A start refused with `EAGAIN`, or an exit during whose run a fork was refused (`pids.events` `max` grew since the start), is started again after a backoff of 2 s doubling up to 1 min, one server at a time as a retry is, at most 6 times in a row; such a start is not the one more start above, and a start for any other reason resets the count. The server's `lastRestartReason` names the limit; after the sixth it stays down with `taskLimit` set (`TestSupervisor_AServerThatMeetsTheTaskLimitOnEveryStartStopsBeingStartedAndNamesTheLimit`, `TestMetTaskLimit_AnEAGAINStartOrARefusedForkDuringTheRunMetTheLimit`). Servers started together (boot, reload) start 1 s apart (`TestSupervisor_ServersStartedTogetherStartAGapApart`) |
| A watched path changes | a change is a write, create, delete or move; reading a file or moving only its access time is not (`TestSupervisor_ReadingAWatchedFileNeverRestartsItsServerAndWritingOneDoes`). For a server with `desired: running`: a live group restarts, and no live group (`crashed` — the agent fixed the code) starts; `restarts` + 1 either way. A `stopped` server stays stopped. After 10 starts in a row that each saw a watched change within their own start window, a change within the window is the server's own output and restarts nothing (the supervisor log names the path to add to `ignore`); a change after the window restarts, and one there after a start that saw none resets the count — except, once that guard tripped, a burst after the window holding only paths the server's own output changed within its windows: a server writing them without a pause keeps each burst open until its 5 s bound, so every other burst ends past the window, and restarting on those restarted it every window, forever; a path its output never changed still restarts it (`TestSupervisor_AServerWritingItsWatchedFilesWithoutPauseStopsBeingRestartedOnceTheGuardTrips`). Within or after is decided by when the burst's last change was made, never by when its settle ended: a write 9.7 s into a 10 s window settles after it, and decided by the restart's time it reset the count and restarted the server every start, forever (`TestSupervisor_AServerThatChangesItsWatchedFilesOnEveryStartIsNotRestartedByThemForever`, `TestSupervisor_AChangeMadeInTheStartWindowCountsThoughItsBurstSettlesAfterIt`). A burst of changes restarts the server once: the supervisor restarts it when no change has come for 500 ms (at most 5 s after the burst's first change), with the reason of the path that opened the burst; a burst whose last change came before the server's latest start began restarts nothing, since that start already runs it. A reload, or a start, stop or restart of any server, keeps each watcher whose `watch` reads the same, with the burst still settling in it — a sync's fast-forward that changes the declaration reloads a few hundred ms after it changed the watched files, and a replaced watcher dropped that burst, leaving the server on the old code (`TestSupervisor_AReloadOrAStartOfAnotherServerKeepsAWatchedChangeStillSettling`). Measured: one write over sshfs (`cat > file`: truncate, bytes, close) restarted a watched server 2–3 times in the same second, ~10 s after the write — the old server's stop grace (up to 10 s for one that does not end on `SIGTERM`), during which each later change queued another restart of the new one; the watch itself is inotify, no poll (`TestSupervisor_ABurstOfWatchedChangesRestartsTheServerOnce`, `TestSupervisor_AChangeMadeWhileARestartStopsTheServerRestartsNothingMore`) |
| The kernel's OOM killer kills a process of the service | the platform's `zerops@zerops.service` has `OOMPolicy=stop` and `Restart=always` (`RestartSec=3`): once systemd notices the kill — at once, or only when it next reaps a process of the unit, minutes later — it stops the whole unit, every server with it, logs `Failed with result 'oom-kill'` and starts it again. The supervisor never changes the unit. On boot it reads systemd's result for the previous run of its unit from the journal (`journalctl UNIT=<its cgroup's unit> -o json`, through `sudo -n` for the `zerops` user, as only root and `systemd-journal` read it; the end line before the current start, with no start between); after `oom-kill` every server it starts gets `lastRestartReason` "the service ran out of memory: the kernel killed one of its processes and systemd restarted every dev server" and `outOfMemory` until it starts again for another reason. The container's journal names neither the killed process (the kernel log is not readable there) nor a usable amount: the unit's logged memory peak was measured far below what its processes used (`TestSupervisor_AfterSystemdRestartedTheServiceBecauseItRanOutOfMemoryEachServerSaysSo`, `TestSupervisor_AfterAnyOtherEndOfThePreviousRunTheSupervisorStartedAgain`, `TestPreviousRunResult_ReadsHowSystemdEndedTheRunBeforeTheCurrentOne`) |
| The supervisor stops (`SIGTERM`: its unit stops, or the lease supervisor zcp started under `setsid` ends, DP-13) | every server is stopped and keeps its `desired`; from then on nothing starts and no watch is set up — a `ctl` call or a watched burst still waiting for its turn starts nothing, so no process group outlives the supervisor, which a supervisor started under `setsid` has no unit to clean up (`TestSupervisor_NothingStartsAfterShutdown`) |
| The supervisor is killed (zenv starts `run.start` again) | the new supervisor stops the groups the old one left (identity-checked), then starts every entry with `desired: running` |
| Container restart (`$HOME` kept) | every entry with `desired: running` starts, `crashed` ones included; `stopped` stays stopped |
| Redeploy (new container, `$HOME` empty) | every declared server for this service gets its first desired state: `autostart` ones start, the rest stay `stopped` |
| `SIGHUP` / `ctl reload` | re-reads the declaration: an added name gets its first desired state (starts only with `autostart`), a removed one stops and is dropped, a changed `command`/`cwd`/`port`/`watch` restarts that one only if it is `desired: running`, the rest is untouched; an invalid file changes nothing and sets `declaration.error` |

**Versions.** A declaration whose `version` is not in the supervisor's `formats` is not run (the
supervisor keeps what runs and sets `declaration.error`); zcp compares `ctl version` with the file
and answers that the supervisor is older than the declaration, naming both.

#### 2.8a.3 The envelope — `stages[]` and `devServers[]`

Both ride the dev service's `ServiceSnapshot` (the snapshot whose `hostname` keys the pair).

A pair whose dev half is a dev pool (`spec-workflows.md` §13 PM-10) is keyed by the pool, so `stages`
rides the pool's snapshot — `runtimeClass: "pool"`, no `mode` — and `devServers` never does: a pool's
dev servers run in its leases and ride `leases[]`. A migrated part's tier on `main` still names its
retired dev service, so this snapshot is where the part's pool shows beside its stages (fixture
`envelope.pool-pair.json`, §2.8a.7). Additive like `leases[]`: no key is new, no version gate.

```json
"stages": [ { "hostname": "webstage", "setup": "web" }, { "hostname": "apistage", "setup": "api" } ]
```

- `stages` projects `ServiceMeta.Stages` (§2.8a.4) in its order; `setup` is omitted while zcp has
  not resolved it, exactly as `stageSetupName` is today. Absent when the pair has no stage.
- Each stage names its `part`, the same name a lease of its part carries (`leases[].part`): for a
  pair keyed by its dev pool, the dev server's name a lease of the pool runs, else the stage's
  setup; for a dev service, the stage's setup (the part's dev server is declared under it), else the
  stage hostname without its `stage` suffix (`TestWithParts_LeasesAndStagesNameTheirPart`).
  Additive like the rest: `{"hostname": "webstage", "setup": "web", "part": "web"}`.
- It **replaces** `stageHostname` and `stageSetupName`: one stage ≡
  `[{hostname: stageHostname, setup: stageSetupName}]`. Each snapshot's own `setupName` stays as it
  is — it names that service's setup, not a pair.
- zcp's synthesis and render read it (the envelope is their input, §1). Mate pairs from the tier on
  `main`, then an open recipe change, then the hostname stem — **with one
  exception: a pool-keyed pair** (a part migrated to a dev pool, `docs/spec-workflows.md` §13). A
  `runtimeClass: "pool"` snapshot carrying `stages[]` (or, before field-order step 3, a single
  `stageHostname`) pairs those stages with the pool: a tier or recipe naming a dev that still stands
  wins, then the envelope, then the stem — so a tier naming the retired dev yields to the envelope.
  Such a stage is never a checkout row, and the pool (no checkout of its own) is not one either; the
  pairing comes from the thread's own last envelope. A preview's address is its Zerops subdomain and the commit a preview runs is
  the receipt's (§2.8a.6), so neither rides here.

```json
"devServers": [
  { "name": "web", "port": 3000, "url": "https://appdev-1df2-3000.prg1.zerops.app", "status": "running",
    "readAt": "2026-10-14T21:04:00Z", "verify": { "ok": true, "httpStatus": 200, "at": "2026-10-14T21:04:00Z" } },
  { "name": "worker", "status": "crashed", "readAt": "2026-10-14T21:04:00Z",
    "lastExit": { "code": 1, "at": "2026-10-14T21:03:10Z" } }
]
```

| Key | Meaning |
|---|---|
| `name`, `port` | from the declaration (§2.8a.1); `port` absent for a portless server |
| `url` | the port's public subdomain URL as Zerops reports it — only when the dev setup's port has `httpSupport` and the subdomain is enabled |
| `status` | the supervisor's `status`, verbatim: `running\|stopped\|crashed\|starting` |
| `readAt` | when zcp read the supervisor for this envelope |
| `verify` | `{ok, httpStatus?, at}` — zcp's HTTP probe of this port, only when this call probed it |
| `lastExit` | `{code?, signal?, at}` — the supervisor's `exit`, when set |

zcp reads the supervisor on `zerops_workflow action="status"`, `zerops_verify`, `zerops_deploy` and
`zerops_dev_server`, which gains the `envelope` key (§1.2 — an added key, never a reshape; its own
fields stay, `name` added only when it differs from `hostname`). **Absent `devServers` means "not
read in this call"**, never "none running": Mate keeps the last read with its `readAt`.
A read that found none is `devServers: []`, which replaces it.
A dev service with one dev server shows today's card, without a server name.

**`standIn` — the dev service pool-migrate holds as the repository's stand-in.** Inside a Mate the dev
service's snapshot carries, after `stages` and `devServers`, `"standIn": {"for": "pool-migrate"}` while
pool-migrate holds it as its repository's stand-in (`docs/spec-workflows.md` §13 PM-14): the dev half
of a pair serving several parts — a dev service with a stage per part made before a Mate's plan created pools
(§10.10 "One repository per monorepo") — from the plan that made the pair until the retire re-keys it
onto its pools, and every dev service of a variant-B pair. Absent otherwise: a pair of one stage, a pair
keyed by its dev pool, every stage, and every snapshot outside a Mate. `zerops_dev_server` names the
same object at its result's top level for that host. Additive to contract 2 like `leases[]`: no key
changes, no `SupportedContract` bump (fixtures `envelope.three-parts.json`,
`dev-server.three-servers-one-crashed.json`, `dev-server.running-http-404.json`).

| Key | Meaning |
|---|---|
| `standIn.for` | the flow that holds the dev service as its repository's stand-in: `pool-migrate` |

**A part with no dev server.** `zerops_verify` of a container dev service whose pair has more than one
stage names, in its `note`, each stage's part that runs a process (its setup, or one it extends, has a
`run.start`) and has no server declared under its name, with the `zerops_dev_server action=start
hostname=… name=<part>` call that starts it. The pair's first part is also served by the server named
after the dev hostname; while servers named after no part are as many as the unnamed parts, none is
named. It never fails the verify; a pair with one stage and local mode read nothing more
(`TestVerifyTool_APartWithoutADevServer_NamesTheStartCall`, `TestPartsWithoutDevServer`).

**One source in the `zerops_dev_server` result.** Its top-level `running` is exactly
`status == "running"` of the `devServers[]` entry it reports (the entry named `name`, else the one
named `hostname`) — never a second probe. The HTTP probe is a health fact beside it — `healthStatus`,
`reason` and that entry's `verify` (2xx/3xx passes) — and never decides `running`: an api with no
root route runs and answers 404 (`TestDevServer_Running_IsTheSupervisorsStatus`). From field-order step (3) Mate's card reads that entry's
`status` and `url`; at step (4) zcp stops emitting the top-level `running` and `url`.

**Starting is no failure.** zcp answers a server the supervisor reports `starting` by its
`startPhase` (§2.8a.2) alone, never by its own clock. `launching` or `awaiting_port` is answered
`reason: "starting"` with when to read it again. A start or restart waits 15 s by default; a status waits only when asked. A supervisor listing `wait` owns that wait (at most 300 s); on an older supervisor zcp probes for at most 45 s on a start or restart and a status never waits. A still-awaited server is not probed: a dev server that compiles its first page
for 90 s is never told to restart. `stalled` is `reason: "start_stalled"`, with the run's log, to
fix and restart. While its port is awaited or stalled and its process group listens on other ports
only (`ss -ltnp`), the answer is `reason: "port_elsewhere"`, naming those ports. `listened`, or a
supervisor without `start-phase`, is probed as a running server and answered by the probe
(`TestDevServer_DSStarting_AnsweredStartingWithARecheck`, `TestDevServer_DSStalled_ToFixNotToWaitFor`,
`TestDevServer_DSPortElsewhere_NamesThePortsItListensOn`, `TestDevServer_DSListened_ProbedNotWaitedFor`).
Mate shows `starting` as a start in progress, never as a failed step.

**What the log gained.** A status that reports one server, of a supervisor listing `logs-since`,
carries `newLog: {errors?, tail, omitted?}`: the lines the server's current run (from its last
start marker) wrote since this zcp process's previous status of it, whoever called that status
(`ctl logs --since`; the first status: the current run), plain text, at most 40 — the lines in an
error's shape first (at most 30, the newest), then the newest of the rest, each in the log's order —
and `omitted`, how many it leaves out, an earlier run's lines included. The cursor moves when the
supervisor answers, so a status whose result never reaches its caller still moves it. A zcp that
restarts reads from the current run again. Absent when the supervisor lacks `logs-since` or its
answer could not be read (`TestDevServer_DSNewLog_ErrorLinesFirstThenTheNewest`,
`TestDevServer_DSErrorShape_OnlyAnErrorsShape`, `TestDevServer_DSNewLogRun_OnlyTheCurrentRun`).

**Field order**: (1) a Mate release accepts `stages` and `devServers` beside the old
keys; (2) zcp raises `MinimumMateVersion` to it; (3) zcp emits `stages`/`devServers` and stops
emitting `stageHostname`/`stageSetupName`; (4) Mate drops the old keys. Until (2) zcp emits today's
keys only. No Mate on `kh-next` reads `stageHostname`/`setupName`/`stageSetupName` beyond its schema.

Single-part, today and after (3) — the same pair, the same meaning:

```json
{"hostname":"appdev",…,"stageHostname":"appstage","setupName":"dev","stageSetupName":"prod"}
{"hostname":"appdev",…,"setupName":"dev","stages":[{"hostname":"appstage","setup":"prod"}],"devServers":[{"name":"appdev","port":3000,…}]}
```

The full 1- and 3-part envelopes are the fixtures (§2.8a.7).

**`leases[]` — the leases this container's zcp holds, not a service's.** It is a top-level key of the
envelope, beside `services`, not a field of a `ServiceSnapshot`. It lists every lease zcp holds for the
container, whichever conversation acquired it; which conversation a lease is bound to is Mate's own
record (§6.3), never a field here:

```json
"leases": [ { "alias": "webpool-3", "pool": "webpool", "branch": "feat-x",
              "containerHostname": "node-id-3.runtime.webpool.zerops", "state": "held", "unverified": false,
              "acquiredAt": "2026-10-23T12:00:01Z", "repo": "https://hq.example.zerops.app/acme/web" } ]
```

| Key | Meaning |
|---|---|
| `alias` | `<pool>-<number>`, the name the lease is addressed by (`zerops_lease`, `zerops_verify`, `zerops_dev_server`, the SSH alias, the mount `/var/www/<pool>-<number>`) |
| `pool`, `branch` | the pool service's hostname; the branch checked out in the leased container |
| `containerHostname` | the container's own hostname (`node-id-<n>.runtime.<pool>.zerops`) |
| `devServers`, `devServersReadAt` | the lease's dev servers — the same `DevServerState` entries a dev service's `devServers[]` carries, the preview server's `url` being the pool's per-port subdomain — **only on a call that read the lease's supervisor, with `devServersReadAt`** (RFC 3339, UTC, the time of that read): an acquire (it starts each lease's supervisor and reads it, `spec-workflows.md` §8 DP-30) and `zerops_dev_server` on the alias. zcp keeps no last-known read — its lease record holds nothing that changes after the acquire — so every other envelope leaves both out, and absent means "not read in this call", never "none": the client keeps the last read it was told while the lease is held (Mate keeps a lease's last read when the field is absent, and replaces it when present). `devServers: []` is a read that found none, stamped with the envelope's time since it carries no read time of its own, so it replaces a stamped last read (`TestWithParts_LeaseDevServers`) |
| `part`, `stage` | the part the lease's pool serves — its dev server's name (the one named after the pair's stage setup when it runs several), else the pool-keyed pair's stage setup — and the stage hostname that pool's pair pairs with (`spec-workflows.md` §13 PM-10); each absent where zcp knows neither, e.g. a pool no pair keys and none of whose dev servers it has read. A client names a workspace by its part ("Web → preview / stage") rather than its pool, alias or branch (`TestWithParts_LeasesAndStagesNameTheirPart`, fixture `envelope.pool-pair.json`) |
| `state` | the reconciled state, verbatim (`held`, `lost`, `unknown-holder`, `unmounted`, `parked`, …) — the one `zerops_lease action=status` reports (`spec-workflows.md` §8 DP-2), never enumerated by a reader. `parked` is a lease that is still held whose container is stopped and kept (`spec-workflows.md` §8 DP-28): a client keeps it bound, as it keeps a `held` one (`TestLeaseTool_ParkThenUnpark_StateRidesResultAndEnvelope`). Whose a container is comes from the orchestrator's renew of the record's own lease (`spec-workflows.md` §8 DP-2): a lease whose container is no longer its reservation — gone, being deleted, free, or reserved by another holder since the lease ended outside zcp — is `lost`; one another holder took read `held` before zcp asked the orchestrator and reads `lost` now, and a lease stopped or started outside zcp stays `parked`/`held`. No state was added (`TestLeaseTool_Ownership_LeaseAnotherHolderTook_LostThenDropped`, `TestLeaseTool_Ownership_StoppedOutsideZcp_StaysParked`) |
| `unverified` | `true` when the state is zcp's own last-known record because the pool's orchestrator did not answer — per pool: a stopped pool marks only its own leases, the others stay confirmed (`TestLeaseTool_Status_OneStoppedPool_OthersStillListed`) — or when the pool's listing answered but the renew of that lease did not (`spec-workflows.md` §8 DP-2): the state is then the listing's (`held`, `parked`, `unmounted`), never `lost`, so a client keeps the lease bound as that state says, and nothing is read from it (`TestLeaseTool_Ownership_RenewFails_UnverifiedNeverRead`) |
| `acquiredAt` | when zcp acquired the lease (RFC 3339, UTC), from its record — also when the record stands in, unverified; absent for a lease zcp has no record of (`unknown-holder`). The leases of one branch are one group (`spec-workflows.md` §8 DP-17): it is the key a client can rebind by when the bound lease ends while others of its branch stay held (the earliest acquired). Additive (`TestEnvelopeJSON_LeaseAcquiredAt`, `TestComputeEnvelope_OrchestratorUnreachable_LeasesUnverified`) |
| `repo` | the repository the lease's group is keyed by beside `branch` (`spec-workflows.md` §8 DP-17): the record's identity, canonical (no `.git`, no trailing slash) and credential-free — no userinfo, query or fragment ever rides it (`topology.RepoIdentity`) — after `acquiredAt`; absent for a lease zcp has no record of (`unknown-holder`) and for a record older than keeping it. Two apps of one Mate both on `main` hold leases of the same branch: a client binds and rebinds by `repo` and `branch` together, never by branch alone (§6.3). Additive (`TestLeaseViews_CarryTheRepository`, `TestComputeEnvelope_LeaseRecords_CarryTheRepository`, `TestMateContractFixtures_LeaseStatus_RenderedByTheHandler`) |
| `activity` | present only on a call that read the lease's checkout — `zerops_lease action=status`, for each lease zcp holds (`spec-workflows.md` §8 DP-21), and `zerops_dev_server action=status` on the lease's alias, for that lease, a read beside its supervisor's — exactly as that read found it: `lastChangeAt` (the checkout's last change), `previewLastConnAt` and `previewOpenConns` (the preview forward's last connection and open connections, when the lease's supervisor reports them; one that reports them and runs no dev server on a preview port reads `previewOpenConns: 0`, since no preview connection can be open), `acquiredAt`, `readAt` (when zcp read it), all times RFC 3339 UTC, and `note` (what could not be read, and why). Inside it too a fact not read is absent, never zero: a lease status did not read (`unverified`, its ssh alias missing) carries `activity` with only `acquiredAt` and `note`; a read that failed (the container unreachable, the read cut at its budget) carries its `readAt`, keeps what it did read and says what failed in `note`. zcp's own `idle` label (`spec-workflows.md` §8 DP-25) rides the result only, never the envelope: a client draws idleness from these facts (`TestEnvelopeJSON_LeaseActivity`, `TestLeaseTool_EnvelopeCarriesActivityOnStatus`, `TestLeaseTool_Status_IdleLease_NamesWhatFreesIt`) |
| `unsaved` | on the same calls, what releasing the lease would lose, read with the release guard's own reads (`spec-workflows.md` §8 DP-4): `files` (the first 20 `git status --porcelain` lines), `fileCount` (all of them), `unpushedCommits`. A clean checkout is `{"fileCount":0,"unpushedCommits":0}`; absent when either read did not answer. Files git ignores and stashes are never in it, and a release loses them too. The per-ask `zerops_dev_server` status carries it, so a client can say that the Preview holds work no change has; that result names the same object at its top level (`TestEnvelopeJSON_LeaseActivity`, `TestLeaseTool_EnvelopeCarriesActivityOnStatus`, `TestDevServer_DSUnsaved_AStatusOfALeaseCarriesItsUnsavedWork`) |
| `notAnswering` | why a `zerops_lease` status call's read of the lease's container found it not answering, on that call's envelope only: one of zcp's own fixed reasons — the container at its process limit (fork answered `resource temporarily unavailable`), or ssh not reaching it — never ssh's or the container's output, so it is credential-free (`spec-workflows.md` §8 DP-21). zcp records none of it; every other call that runs in the lease says it in its own result or refusal. Absent when the read reached the container, or none ran. Additive |

- **Absent means "not read"; `[]` means "read, none held".** The key is emitted whenever zcp read
  the leases for this envelope — `zerops_workflow action="status"`, the develop briefing
  (`action="start" workflow="develop"`), every JSON carrier (§1.4, through `tools.freshEnvelope`) and
  `zerops_lease`, all from `tools.leaseEnvelopeOptions` — and left out when it did not (no reader, or
  a project where zcp holds no lease record and keys no pair by a dev pool: a pool an app calls as its
  own infrastructure costs no orchestrator call, and its reserved containers are `zerops_lease
  action=status`'s to list). `zerops_lease`'s own envelope reads always, so a release of the last
  lease carries the emptied list. Never `null`. A client keeps the last list it read until a
  newer envelope carries the key (`TestComputeEnvelope_LeasesFromReader`, `TestEnvelopeJSON_LeasesNotRead_NoKey`,
  `TestEnvelopeJSON_LeasesReadNone_EmptyArray`, `TestDevelopBriefing_PoolPairStage_LeaseNoOrchestratorVouchesFor_NeverTheSource`).
- **Additive: no version gate.** Mate decodes the envelope forward-compatibly, so the key needs no
  `SupportedContract` bump and no `MinimumMateVersion` raise; a Mate that does not know it ignores it.
  The same holds for `activity` and `unsaved`.
- **Absent `activity` / `unsaved` means "not read in this call"**, never "no activity" or "nothing
  unsaved", as with `devServers`; a client keeps the last one it read, with its `readAt`. Only a
  status call that read a lease's checkout — `zerops_lease`'s, or `zerops_dev_server`'s on the alias —
  lays them on (`workflow.WithLeaseActivity`, keyed by alias); the lease reader every envelope is
  filled by asks the pool orchestrators only and runs no ssh, so every other call's envelope —
  `zerops_dev_server`'s other actions on the alias included — carries neither and pays no ssh read,
  and no envelope reader derives them (`TestLeaseReader_NeverReadsActivity`,
  `TestComputeEnvelope_LeaseActivity_NeverFromTheReader`, `TestEnvelope_OtherToolsCarryNoLeaseActivity`,
  `TestJSONCarriers_LeaseActivity_ReadBackVerbatim`; fixture `envelope.lease-status.json`, §2.8a.7).
- **The lease string never appears** (`spec-workflows.md` §8 DP-3): a lease has no field that could
  carry it (`TestEnvelopeJSON_NoLeaseString`).

#### 2.8a.4 `ServiceMeta.Stages[]` (zcp-internal)

```go
type StageRef struct {
	Hostname  string `json:"hostname"`
	SetupName string `json:"setupName,omitempty"`
}
// on ServiceMeta, replacing StageHostname / StageSetupName:
Stages []StageRef `json:"stages,omitempty"`
```

- A pair is one dev service (`Hostname`, `PrimarySetupName`) and N previews. `Stages[0]` is the
  pair's first stage — today's `StageHostname`. Local-stage mode holds exactly one.
- Readers: `SetupNameFor(host)` and the pair-keyed lookups (`FindServiceMeta`,
  `ManagedRuntimeIndex`) match any `Stages[i].Hostname`; deploy takes the setup from here, never from
  a hostname suffix (`SetupCandidatesFor` goes); develop scope takes every stage under today's rule
  (a recipe-born pair's stages wait until asked); the recipe plan adds a second stage of the same repo
  to the pair; the composer emits one stage/production runtime per entry; develop's promote-to-stage
  commands cross-deploy every stage with its own setup, `prod` only while none is recorded
  (`TestSynthesize_PromoteStage_NamesTheRepoOwnSetup`).
- Validation, before import: stage hostnames are unique in the pair, differ from `Hostname`, and no
  other meta names them.
- Plan, before it is accepted: a classic or adopt plan that pairs an existing dev service with
  stages has that service's repo read first (with the dev's setup when the plan names it). When its
  root `zerops.yml` declares more than one part, the plan is refused only for a stage that matches no
  part — one naming a setup the file declares that is no part of the repo (the dev setup, a base
  only extended), or one naming no setup when the repo is a monorepo (a workspace marker or two or
  more packages) — with the plan made from the repo: a stage the plan already gave a part keeps its
  hostname, the others are proposed as `<part>stage` for the agent to submit. Such a pair attaches
  existing services only, so the refusal names the import of every proposed stage the project does
  not hold yet. A stage whose setup the file does not declare yet is a part the agent adds (develop
  writes its setup); it is accepted and kept in the proposed plan, as is one naming a part's setup
  for another environment. A part without a stage is
  accepted, and without monorepo evidence a stage naming no setup is too, so an app whose further
  setups are environments (`dev`/`stage`/`prod`) or a recipe pair keeps its one stage
  (`TestBootstrapPlan_MountedRepoParts_ReadBeforeThePlanIsAccepted`,
  `TestBootstrapPlan_RepoSetups_RefusesOnlyAStageMatchingNoPart`,
  `TestBootstrapPlan_AddingAPartToAMultiPartRepo_KeepsItsStage`,
  `TestBootstrapPlan_RefusedPlan_ImportsEveryProposedStageNotLive`). The meta of a pair planned
  with several stages records each stage's setup from the plan; a one-stage pair's setup is resolved
  at its first deploy (`TestBootstrapComplete_MultiPartPair_RecordsEachStagesSetup`).
- Migration on load: no `stages` and a `stageHostname` → `Stages = [{stageHostname, stageSetupName}]`;
  when `stages` is present it wins and the old keys are not read. On write zcp writes `stages` and
  mirrors `Stages[0]` into `stageHostname`/`stageSetupName`, so a zcp a container restart pulls back
  still sees today's pair.
- Rebuild: when zcp loads a ServiceMeta without `stages` and the repo's root `zerops.yml` declares
  more than one part, it rebuilds `Stages[]` from the repo — the parts in `zerops.yml` are the truth.
  A part is a setup other than the dev setup (`PrimarySetupName`), other than a setup
  only `extends` names (a `base`) and other than a setup that builds and starts what an earlier part
  does — the same build commands, deploy files, start and document root —, which is that part in
  another environment: an app's `stage` and `prod` are one part, and only distinct code makes a
  monorepo. A setup naming none of these stays a part (`TestReadPartsSignal_EnvironmentsOfAPart`). The mirror's stage stays first; every other part's preview
  hostname is the app's tier entry whose `zeropsSetup` is that part (§2.8a.5); a part no tier names
  stays out until a plan adds its stage — zcp never derives a hostname. zcp then writes the meta. A
  single-part app loses nothing: the mirror is its whole pair.
- While an old zcp runs, the mirror holds the pair only and dev servers are degraded: the old
  zcp's keeper and its SSH start do not know the supervisor and may collide with it on a declared
  port. This ends when the new zcp is back: `zcp init` at every container start, and each `zcp
  serve`, removes the old keeper's `zerops@devservers-*` units (`TestRetireDevServerKeeper`,
  `TestRetireDevServerKeeperAtBoot_RemovesKeeperUnits_NeverFailsInit`). Its `dev-servers.json` record stays
  until each host moves: a `zerops_deploy` (or batch) self-deploy of a dev service with a record
  and no supervisor carries the move — the two lines, the kept server declared from the record —
  so the supervisor brings the server back with no extra redeploy; the host's entry goes once the
  supervisor answers after that deploy, or after a `start` moved the host (decision B,
  `TestDeploy_KeptDevServer_ComesBackUnderTheSupervisor`).

#### 2.8a.4a Variant B: `ServiceMeta.CodeVolume` (zcp-internal)

```go
type CodeVolume struct {
	Hostname    string          `json:"hostname"`    // the local-storage service
	MountPath   string          `json:"mountPath"`   // e.g. /var/www/ws
	DevServices []DevServiceRef `json:"devServices"` // {hostname, setupName}
}
// on ServiceMeta, and derive-only on the plan's runtime target (never published):
CodeVolume *CodeVolume `json:"codeVolume,omitempty"`
```

**zcp sets up no new code volume.** Dev pools replace variant B: every part gets a Swarm dev pool
of its own (docs/spec-workflows.md §8 DP; §10.10 "One repository per monorepo"). A variant-B pair
recorded before keeps working exactly as below, and pool migration moves it onto one dev pool per
part (docs/spec-workflows.md §13 PM-13). The record stays, and so does every reader of it: each
meta write writes the whole struct, pool migration reads it, and zcp cannot know when every
installation has migrated. A pair without `codeVolume` is variant A and reads, deploys and stands
up exactly as before.

- No path sets one up:
  - Plan: the plan runtime publishes no `codeVolume`, so a call still carrying one is refused by
    the input schema before any handler runs — the agent reads `validating "arguments": … validating
    /properties/plan/items/properties/runtime: unexpected additional properties ["codeVolume"]` —
    and a plan reaching the engine with one is refused naming the field, a dev pool per part and
    pool-migrate, with nothing planned (`TestWorkflowToolSchema_PlanRuntimeCarriesNoCodeVolume`,
    `TestBootstrapCompletePlan_PlanCarryingCodeVolume_RefusedNamingPools`).
  - Adopt records a code volume only on a volume already holding the repository (below) — a
    variant-B project whose record was lost, recorded again so pool migration can move it. Dev
    services whose volume holds nothing yet or an empty folder adopt as separate services, as a
    data volume's do: adopting them as a pair would make the volume a code volume, its first deploy
    filling it. Only a scope records such a pair, never a plan: its setups are read through the
    runtimes the adopt scope names, each from its own `zerops.yaml` (`/var/www` over SSH, else its
    mount) — never from whichever runtime of the project sorts first — and the mount is probed only
    on the runtimes whose own file declares the setup, so the project's other runtimes are neither
    read nor reached, wherever they sort
    (`TestAdoptVolumeSetups_ReadsTheScopedRuntimesNotTheProjectsFirstRuntime`). A setup mounting
    the volume runs on an adoptable runtime of its `run.base` that finds the checkout at the mount:
    the one named after the setup (a service's default setup is its hostname), else the only one no
    other setup takes — stages and other runtimes of the same base hold no checkout there, so they
    never make the choice ambiguous. The pair's stages are the scoped stage hostnames (ending in
    `stage`) named like one of its dev services (`apistage` → `apidev` or `api`, as the stand-up
    pairs them) or whose setup the repository's `zerops.yaml` declares, each recorded with the
    setup named after it when declared, the one named like the dev half first; with stages the
    pair is standard. The adopt answer names the code volume, further dev services and stages
    (`TestBootstrapCompleteAdoptPlan_VariantBBesideItsStages_IsOneStandardPair`,
    `TestBootstrapCompleteAdoptPlan_VariantBPair_TheAnswerNamesWhatItRecorded`,
    `TestHandleBootstrapComplete_AdoptVariantBBesideAnotherRepository_OnePairWithItsStages`).
    Runtimes scoped beside the pair still face the adopt pairing question (docs/spec-workflows.md
    §3.2), whose refusal names the scope that adopts the pair alone
    (`TestBootstrapCompleteAdoptPlan_VariantBBesideAPairingQuestion_NamesThePairScopeOnly`).
  - Stand-up reads a tier's local-storage service as a managed service it leaves as it is: per
    repository, one runtime that is no stage is the dev half of every stage the repository builds
    (variant A, a stage per part), and otherwise each stage pairs with the runtime named like it,
    each dev service holding its own checkout, else with its part's dev pool named like it
    (`apistage` → `apipool`). The pair's stages put the one named like the dev half
    first, then the tier's order (`TestParseMateTier_OneDevHalfTakesEveryStageOfItsRepository`,
    `TestParseMateTier_SeveralDevServicesBesideALocalStorageService_PairByName`,
    `TestStandup_FreshTierWithALocalStorageService_SetsUpNoCodeVolume`).
  - The monorepo playbook gives every part its own dev pool and stage on one repository — in a
    Mate the plan creates them (§2.8b B-9), and a dev service with a stage per part made before a
    Mate's plan created pools reaches them through pool-migrate — and names pool-migrate
    for a repository already on a code volume (`TestPlaybookMonorepo_ContentPins_PartRules`).
  - A plan still carrying `codeVolume` is refused naming the way instead: one dev service holding
    the repository with a stage per part, then pool-migrate giving each part a dev pool of its own
    (`TestBootstrapCompletePlan_PlanCarryingCodeVolume_RefusedNamingPools`).
- Steering: every `zerops_deploy` whose target a variant-B pair names, every `zerops_dev_server`
  answer for one of its dev services (`warnings`), and lifecycle status (a `### Shared code volume`
  guidance block) carry one line — `zcp no longer sets up a shared code volume. <dev>'s pair keeps
  working on the code volume <volume>; zerops_workflow workflow="pool-migrate" service="<dev>" moves
  it onto one dev pool per part.` — a warning, never a refusal; a pair already keyed by its dev pool
  carries none (`TestDeployTool_VariantBPair_WarnsThatPoolMigrateMovesIt`,
  `TestDevServer_VariantBDevService_WarnsThatPoolMigrateMovesItsPair`,
  `TestHandleLifecycleStatus_VariantBPair_NamesPoolMigrate`).
- The pair stays one meta, keyed on the dev service the agent edits (`Hostname`). Every
  `DevServices[i]` is a further dev service of the pair with its own dev setup; the pair-keyed
  lookups resolve it to the pair, `RoleFor` names it a dev half and `SetupNameFor` its setup. A pair
  adopted again names the volume and each dev service once, every one of them live
  (`TestValidateBootstrapTargets_AdoptedCodeVolume_NamesEveryServiceOnce`).
- Deploy: a deploy from any dev service of the pair runs with `workingDir` = `mountPath` unless the
  caller names one (`ServiceMeta.WorkingDirFor`), and pre-flight reads `zerops.yml` from the same
  folder below the source's mount. In a project holding a local-storage service a `workingDir`
  under `/var/www` is a container path unless its first folder is a service's hostname, which stays
  refused as a local mount path (`TestDeployTool_FromVariantBDevService_RunsWithWorkingDirOnTheVolume`).
  The deploy of the dev half fills a volume the record names that is still empty and reloads every
  dev service's supervisor (`TestDeployTool_OfAVariantBDevHalf_FillsTheCodeVolumeOnceAndReloadsTheSupervisors`).
- Stand-up called again on a project holding a recorded variant-B pair reads the tier beside the
  records: a repository with several runtimes that are no stage, one of them the recorded pair's
  dev half, beside the tier's only local-storage service the record names, is that pair — the
  recorded dev half wherever the tier lists it, the others further dev services, every stage of the
  repository its stage; a record naming another volume, or a pair already keyed by its dev pool,
  keeps nothing (`TestParseMateTier_ARecordedVariantBPair_IsReadAsThatPair`). The adopt it runs
  refuses a target naming a service another meta records — a further dev service or stage of
  another pair — so no second meta is ever written for one runtime (docs/spec-workflows.md §8 E8;
  `TestAdoptPair_AServiceAnotherPairAlreadyNames_Refused`). The pair is adopted again with
  `codeVolume`, and once the repository is checked out its `mountPath` is read from `run.volume` of
  every dev setup of the pair in `zerops.yml` — one that does not mount the volume, mounts it
  elsewhere than the others or outside `/var/www` stops the pair. The dev half deploys from the
  checkout in `/var/www`, then every further dev service deploys its own dev setup from the same
  checkout; then the volume is filled once from that checkout (left as it is when it already holds
  this repository) and `zcp-devsup ctl reload` runs on every dev service; the pair's stages build
  from the volume after the fill
  (`TestStandup_VariantB_FillsTheVolumeOnceAndEveryDevServiceDeploysItsOwnDevSetup`).
- zcp never writes code into a volume it did not prove is a code volume. Adopt takes a local-storage
  volume that several dev setups mount for a pair's code volume only when every dev setup works in
  the mount — each `run.initCommands`/`run.start` command is a `zsc noop` or names the mount, and a
  built-in web server serves a `documentRoot` inside it (`ops.ZeropsYmlEntry.WorksIn`) — and every
  matched runtime finds a git checkout there sharing a root commit with its `/var/www`; anything
  else — nothing at the mount yet, an empty folder, or a Laravel app and worker sharing
  `/var/www/storage` — adopts as separate services. The fill writes only into an empty volume and
  refuses any other content, naming the volume
  (`TestBootstrapCompleteAdoptPlan_ASharedVolume_IsACodeVolumeOnlyWhenTheDevSetupsWorkInItAndItHoldsThisRepository`,
  `TestCodeVolume_OnlyAnEmptyVolumeIsFilledAndOnlyThisRepositoryCountsAsItsCheckout`).
- Stand-up builds every stage of a pair, variant A or B, each cross-deployed from the dev half with
  its own setup (`TestStandup_DeploysEveryStageOfAPair`).
- A pool migration clears `codeVolume` (docs/spec-workflows.md §13 PM-13). A variant-B pair moves
  one pool per part; its census and bundle read the volume's checkout. Its retire deletes every dev
  service — each delete keeps the volume and its files — then the volume last, the only step that
  loses the checkout; the platform refuses that delete while any service still mounts the volume.
  `codeVolume` rides the pool-keyed pair until the volume reads gone, then leaves it, so no migrated
  pair names a code volume
  (`TestPoolMigrateRetire_VariantB_DeletesEveryDevServiceThenTheVolumeLast`,
  `TestPoolMigrateRetire_VariantB_VolumeStillMounted_KeepsTheCodeVolume`).

#### 2.8a.5 Tier: hostname → `zeropsSetup`

Today: a tier's `services[]` entry carries `hostname`, `buildFromGit` and `zeropsSetup`; zcp's
composer writes `zeropsSetup` (the pair's stage setup) beside a credential-free `buildFromGit`
(`internal/ops/bundle/group.go` `groupRuntimeEntry`); HQ's `tierRuntimes` takes the runtimes built
from the app's HQ repo that name a `zeropsSetup` and deploys each with it
(`zeropsSetup` or hostname);
Mate's client strips both keys before it
imports empty services.

Added — no new key, only meaning: (1) a **part is its `zeropsSetup`**: the same part has the same
`zeropsSetup` in the Mate tier (preview), the stage tier and the production tier; (2) the composer
emits one runtime per `Stages[]` entry with that entry's setup; (3) a hostname is chosen once, when
the service first enters a tier, and the composer reads it back from the tier, never recomputes it;
(4) a change of the set of parts is a targeted "add/remove service X"
proposal to the existing tiers, the mechanism of today's `scaling=<hostname>`, merged by a person.

How zcp does it:

- Naming. The reconcile reads the Stage and Small Production tiers on `main` (`bundle.HeldRuntimes`:
  each runtime with a `buildFromGit` and a `zeropsSetup`) and the composer gives a part the hostname
  a tier holds for its repository and setup — the Stage tier's when people renamed it in one; a tier
  zcp owns (§10.10) holds no name, since it is zcp's own composition. A part no tier holds is
  named once, after itself: its stage's stem (`apistage` → `api`, `webstage` → `web`), the dev
  half's for a pair with no stage — every part alike, never the repository's name for one of them.
  A new name that a managed
  service, a Box a runtime reads (docs/spec-workflows.md §8 PS-3) or another part already has is
  never changed on a guess: the part is left out of the group tiers and the composer warns.
- What each runtime is. Every runtime entry of every tier is typed, scaled and reached from
  the service it stands for: a dev service from itself; a part from its stage as the live project
  runs it (`GroupStage.ServiceType`, its scale and access), else — its stage not running — from
  its setup's `run.base`, a dev service's scale and access standing in (a pool's never do); a part
  nothing types is left out of every tier and said. Never from a dev half that serves another
  part. A pair keyed by
  its dev pool is written on the AI Agent tier as that pool beside its stage, as zcp imports a
  part's pool (knowledge theme pools-and-box, "A part's dev pool"): `swarm@1`, `minContainers: 0`,
  the pool's live `maxContainers` and vertical scale, its service variables with `sshIsolation`
  as written and its orchestrator's tokens left out, no `buildFromGit` and no `zeropsSetup` (its
  image is deployed from a lease, never by an import — the comment above it names the setup) and
  no credential: a lease of a repository in HQ reaches it through zcp's relay
  (docs/spec-workflows.md §8 DP-16) and the pool's `GIT_TOKEN` stays the person's.
  A dev-service pair is written the same way, a pool per stage, where `main` of its repository in
  HQ holds the pool setup of every one of its parts: how a part is
  developed is a fact of its repository, never of the composing Mate's project, and the composing
  Mate's own dev service stays as it runs. The pair is written as pools whole or not at all, and
  only when all of these hold. Its record is not keyed by a dev pool and keeps no code volume. It
  has a stage, every stage's hostname ends in `stage` and every stage has a recorded setup: a
  stage's setup is never guessed. Every stage exists in the project, running or stopped: a pool's
  part is typed, scaled and reached from its stage alone, and a stopped stage still holds all
  three, where a dev service stands in for a stage that does not exist, so a pair with a stage gone
  is written as it runs. The project runs no service named like any part's pool
  (`webstage` → `webpool`): one running is a pool-migrate in flight, and that pair is written as it
  runs until the retire re-keys it. `main`'s zerops.yaml and `.zcp/dev-servers.json` were read at
  its head in HQ, never from the dev checkout (it may stand on a branch), and only for a pair that
  passes the conditions before. Only a `main` HQ answered proves it holds no pool setup: an empty
  repository, or one without `main`, holds none and the pair is written as it runs; a `main` that
  could not be read (HQ or Git failing, the pass cut short) fails the pass — nothing is composed,
  proposed or kept for that head, and the pass says it retries on the next. And the one setup check
  (`ops.CheckPoolSetups`, the check the stand-up runs) names nothing `main` lacks for any part: the
  pool setup named after its pool, the stage setup the stage records, the pool setup's
  `zcp-devsup run`, its install line and preview ports with `httpSupport`, and `previewPortBase`.
  Each part's pool then takes from the dev service what pool-migrate gives a pool made from it: its
  vertical scale (never its container counts, so the pool's maximum is the template's default 3),
  its subdomain, its service variables and `sshIsolation` admitting the composing zcp service; its
  comment names `main`'s setup named after the pool. The composer warns once per pair so written,
  naming the pair, its pools and the repository. Otherwise the pair is written as it runs.
  A utility that is a Mate's own dev tooling — a mail catcher, a database admin UI, by its
  hostname or its repository's name — stays on the AI Agent tier; Stage and Small Production
  leave it out and say so.
  So does what serves the Mate itself, read from what the project shows rather than a name: a
  runtime — a utility, or a stage of a pair — wired to the address of a service of the Mate's own
  project no group environment runs (a pair's dev service or dev pool, the Mate's own
  infrastructure such as zcp, a utility already so wired; never an address of the app's own parts),
  and a volume only such a runtime can be using (§10.10, *What serves the Mate*). Those rules are
  zcp's fallback: what serves only this Mate is the person's to say, and their choice wins both ways
  (*Choice*, below). A Zerops Box goes on each tier where an entry the tier carries reads it, so one
  only such a runtime reads stays on the AI Agent tier with it (docs/spec-workflows.md §8 PS-3).
- Choice. `zerops_workflow action="group-recipe" mateOnly={"<host>": true}` records, at the
  person's word, that a utility or a stage of a pair (by the Mate's hostname) serves only this Mate:
  the AI Agent tier carries it, Stage and Small Production leave it out with a volume only it uses,
  whatever zcp's rules read; `{"<host>": false}` carries it in every tier, whatever the address rule
  or the dev-tooling rule read. The agent asks the person when a runtime looks like it serves them
  rather than the app — their cloud agent, their own tooling — and never guesses (the
  mate-application playbook says so, which a Mate's AGENTS.md points at). zcp keeps the choices, merged over earlier ones, in
  `<stateDir>/hq/group-recipe-choices.json`, which every zcp of the Mate shares, written under the
  recipe pass lock, and every pass composes with them — zcp's start, the pass a service change asks
  for, the agent's own; a record that cannot be read fails the pass rather than compose without it.
  The call then runs the pass and answers with the choices and its outcome. A dev half (only the AI
  Agent tier carries it already), a managed service (it goes with the runtimes that use it), a Box,
  what the recipe carries on no tier, and a hostname the project does not run are refused, naming
  each, and nothing is recorded. Where the person said nothing and the address rule alone kept a
  runtime on the AI Agent tier, the pass says so in one line naming the runtimes, the rule and both
  calls (`bundle.mateRuleLine`), so the person learns the choice is theirs; the owner decided that the person says what serves only a Mate. `TestBuildGroupRecipe_PersonsMateOnlyChoice_WinsOverZcpsRules`,
  `TestGroupRecipe_TheMateOnlyChoice_IsKeptAndEveryPassHonoursIt`,
  `TestGroupRecipe_TheMateOnlyChoice_RefusesADevHalf`.
- Proposal. `zerops_workflow action="group-recipe" service=<host>` (the host a stage, or a runtime a
  tier names) adds that part's composed entry to each tier file on `main` that carries a service of
  its repository and lacks the part — after the repository's last service — or removes the part's
  entry, its comment lines and its blank separator from each tier still carrying it; a dev half is
  never removed, a tier carrying no service of the repository is left alone, and a tier the text
  edit cannot read is refused and named. A tier's service names its part by its repository and the
  setup a group environment builds for it (`bundle.GroupEnvironmentSetup`, the composer's own rule):
  a stage deploying Mate's preview variant of a setup — which the AI Agent tier keeps, while Stage and
  Small Production build the setup it extends — is that setup's part, so a part gained with a variant
  is one proposal over every tier, and a stage that only switched to its variant changes no part; a
  dev half keeps its own setup (`TestGroupRecipeService_PreviewVariant_IsThePartOfTheSetupItExtends`). Every other line stays as `main` has it. The change is
  titled `Mate: <host> in the group recipe`; the Mate keeps one recipe proposal open at a time —
  its one change there opened naming no branch, zcp's choice, since proposals touch the same tier
  files — as for scaling. The reconcile's line names the call for each part `main`'s tiers lack or still carry,
  unless that part's change is the open one. A one-part app's tiers carry its part, so nothing is
  said or proposed.
- Removal. `zerops_delete` deletes a service only with the person's explicit consent: a call
  whose `consent` does not name the service is refused (`CONSENT_REQUIRED`) and deletes nothing,
  so the agent asks first. With it, a stage of a pair with several leaves `Stages[]` and its
  answer's `recipe` names the call; a pair's only stage keeps today's behaviour.
- Managed service. A managed service the Mate's project runs and a tier on `main` lacks takes the same
  call, for any app, one part or several: `service=<host>` adds the composer's entry, that tier's
  policy applied (Small Production's HA variant where the type has one), to each tier file on
  `main` lacking it, among the services of its priority ahead of every runtime, every other line as
  `main` has it. Whether a tier lists the service is what the tier reads as YAML, and an edit that
  would read back as anything but `main` plus that service is refused. It only adds: a managed
  service a tier carries and the project does not is left. The reconcile's line names the call for
  each one, names a tier no edit can take as one to add the service to by hand, and says, as the
  proposal's answer does, that merging the change imports the service into every stage or
  production environment made from those tiers, which is why it waits for a person to merge it
  unless none is made from them. A proposal a person closed unmerged is not named again until a later recipe change of the Mate's
  merges. The call for a service the project no longer runs, while its proposal is the open change,
  withdraws it: the change is brought to `main`'s tree.
- Tier header. Every tier says it also takes a managed service the Mate's project gained; an app
  with more than one part — parts counted per repository, so a monorepo of pool-keyed pairs is one
  app of several parts — says it also takes a part's service added or removed. A tier zcp composes
  for a Mate says zcp proposes the whole file anew while it stays exactly as written, and ends its
  header with zcp's mark. A header names no landing rule — who lands zcp's proposals is HQ's,
  and a file on main outlives it (§10.10, `TestGroupTierHeader_SaysWhatZcpProposesIntoAMarkedTier`).

zcp `TestBuildGroupRecipe_OnePart_ComposesAsBeforeStages`, `TestBuildGroupRecipe_ThreeParts_OneRuntimePerStage`,
`TestBuildGroupRecipe_PartHostnames_HeldByTheTier`, `TestBuildGroupRecipe_PoolKeyedMonorepo_PoolAndStagePerPart`,
`TestBuildGroupRecipe_Polyglot_EachRuntimeFromItsOwnPart`, `TestBuildGroupRecipe_DevOnlyUtilities_OnlyInTheAIAgentTier`, `TestTierService_AddOrRemove_OnlyThatService`,
`TestGroupRecipeService_OnePart_ProposesNoTierEdit`, `TestGroupRecipeService_AddThenRemovePart_OnlyThatService`,
`TestDeleteTool_StageOfSeveral_LeavesThePairAndNamesTheRecipeCall`, `TestTierService_AddInRank_ManagedFirst`,
`TestGroupRecipeService_ManagedAdded_ProposedIntoEveryTierLackingIt`,
`TestGroupRecipeService_ManagedAlreadyInATier_LeavesThatTier`,
`TestGroupRecipeService_DatabaseAndCache_EachProposedInTurn`,
`TestTierService_AddInRank_ReadsTheTierAsYAML`,
`TestGroupRecipeService_ManagedDeclined_ReminderStopsUntilALaterMerge`,
`TestGroupRecipeService_ManagedNoTierCanTakeIt_SaysToAddByHand`,
`TestGroupRecipeService_ManagedDeleted_WithdrawsItsOpenChange`.

#### 2.8a.6 The receipt on an HQ change (only N > 1)

**zcp writes no receipt.** The shape below is HQ's stored key and the fixture's. A writer sends it
with the change edit,
`PATCH /api/mate/changes/<repo>/<number>`, body `{"receipt": …}`, **only when the pair has more
than one stage** — a single-part change never carries one. HQ stores the last one whole and returns it on
the change record as `receipt` (`null` when none). HQ's review and Mate's review show it; merge warns
on it and never blocks.

```json
{ "at": "2026-10-14T21:10:00Z", "parts": [
  { "setup": "web", "previewHostname": "webstage", "deploy": "ok", "deployedAt": "2026-10-14T21:06:00Z",
    "workspaceClean": true, "deployedCommit": "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", "verified": true },
  { "setup": "api", "previewHostname": "apistage", "deploy": "ok", "deployedAt": "2026-10-14T21:07:00Z",
    "workspaceClean": false, "verified": true },
  { "setup": "worker", "previewHostname": "workerstage", "deploy": "failed", "deployedAt": "2026-10-14T21:08:00Z",
    "workspaceClean": true, "verified": false } ] }
```

- `parts` follows `Stages[]`; `deploy` is `ok|failed|none` (zcp's last deploy of that preview in the
  work session); `verified` is a passing verify after that deploy; `deployedCommit` is the dev HEAD
  and is present **only** for `deploy: ok` with `--workspace-state clean` (SHAPES C14).
  `deployedAt` is that deploy's time (its success for `ok`), absent for `deploy: none`.
  A build still running when the poll timed out is not finished: `none`, never `failed`; once it
  finishes, the record that settles it carries that attempt's commit and workspace state.
- No "changed / unchanged" (REVIEW 6). "Preview older than the change" is the reader comparing
  `deployedCommit` with the change's `head`, which HQ already holds.
- Order: HQ accepts the key first; zcp writes it after.

#### 2.8a.7 Shared fixtures

**One source, in zcp** (zcp produces every value): `internal/tools/testdata/mate-contract/`.

| File | Shows |
|---|---|
| `envelope.single-pair.today.json` | today's envelope for one pair — the bytes zcp marshals today |
| `envelope.single-pair.json` | the same pair after step (3): `stages[1]`, one dev server named `appdev` |
| `envelope.three-parts.json` | `appdev` + `webstage`/`apistage`/`workerstage`, three dev servers, one crashed |
| `envelope.pool-pair.json` | a pair keyed by its dev pool (`spec-workflows.md` §13 PM-10): `apppool` (a pool, no `mode`) carrying `stages[1]`, the stage `appstage`, the pool's lease in `leases[]`; the bytes zcp computes from the re-keyed pair |
| `envelope.lease-status.json` | the envelope a `zerops_lease action=status` result carries, each lease naming its group's `repo` (two repositories); `devpool-3`, read, with `activity` and `unsaved`; `stoppool-2` of a stopped pool, unverified, its `activity` only `acquiredAt` and why it was not read, no `unsaved`; the bytes the real handler renders, its read times the call's own (`TestMateContractFixtures_LeaseStatus_RenderedByTheHandler`) |
| `dev-server.one-server.json` | a `zerops_dev_server` result for one server, with its `envelope` key; top-level `running` ≡ the reported entry's `status == "running"` (§2.8a.3) |
| `dev-server.three-servers-one-crashed.json` | the same for three servers |
| `dev-server.running-http-404.json` | a running api with no root route: `running` true from the supervisor, its probe's 404 only in `healthStatus`, `reason` and the entry's `verify` |
| `dev-server.status-starting.json` | a status of a server whose port is still awaited (`startPhase: awaiting_port`): `running` false, `reason: "starting"` with when to read it again, its `newLog` with an error line first, the entry's `status: "starting"` |
| `dev-servers.declaration.json` | the §2.8a.1 example, `admin` with `autostart: false` |
| `hq-change.receipt.json` | an HQ change record carrying the §2.8a.6 receipt |

- zcp: `TestMateContractFixtures_*` (`internal/tools/mate_contract_fixtures_test.go`) parses every
  file strictly against the contract shapes, asserts the values, and marshals today's single-pair
  envelope from `workflow.StateEnvelope` byte for byte. The lane that emits a shape switches its
  case to render through the real handler (`docs/spec-testing-architecture.md`).
- Mate: a byte copy with `SOURCE.json`
  `{zcpCommit, files: {name: sha256}}`, refreshed only by its sync script (copies from
  `../zcp`, rewrites `SOURCE.json`). Its case recomputes every hash — a hand edit of the
  copy fails — and decodes every file through Mate's consumers (the envelope schema, the card
  payload decoder), asserting values.
- Direction: a wire change lands in zcp first with its fixtures; the Mate lane runs the sync and
  lands its decoder (field order above).


### 2.8b The birth of a pooled part (B-1…B-10)

A **pooled part** is a part whose development runs in a dev pool: the tier or the plan names its pool and its stage, paired by
name (`<part>pool`, `<part>stage`; only `workflow.ParseMateTier` reads the pairing). Its birth is zcp's (the owner's
decision: birth by commit deploy from zcp). These rows add to contract 2 without a bump: the status
file's version stays 1, and the verdict is a field an older reader ignores.

| # | The fact | Owned by |
|---|---|---|
| B-1 | **Birth by commit deploy.** zcp commit-deploys `main`'s head, as the Mate's HQ holds it when the run reads it, to every pool and stage of a pooled part that has no build, all at once: a new app version named `zcp <sha12>`, the commit's deploy source uploaded to it — the commit fetched from HQ by zcp, what its root `.deployignore` leaves out left out, as HQ's rule for a deploy source says — then build and deploy with the commit's zerops.yaml and the part's setup. A version of the same commit waiting, building, deploying or active is never built twice; one left `UPLOADING` by a cut upload is deleted by the next run and the deploy submitted again. A Zerops call answering 5xx or 429, or not at all, is tried three times, then fails with its last answer and its class. No container holds a checkout for it, no lease is taken and no stage is a checkout host. A pool or stage that has a build is never written; its row is read from the version it runs. After birth the stage is the agent's test bench (`spec-workflows.md` DP-8) | zcp `ops.CommitDeploy`, the stand-up |
| B-2 | **Who runs it.** zcp's Mate service runs the stand-up at its start in a project that holds a Swarm pool, for the pooled parts only: before the Mate's server launches, a first look bounded to 15 s (the live services, HQ's tier) writes the verdict `building` where a pooled part has no build, so the server never reads a pooled tier without a verdict, and the build then runs beside the server; a tier with no pooled part, nothing to build, a run already going or a first look that failed writes nothing; the agent's `zerops_standup` runs the same stand-up, the classic pairs with it. One run per project at a time: a second — the boot run beside the agent's call, either way round — starts nothing and answers the first's start and its verdict so far | zcp |
| B-3 | **The application is HQ's answer.** The stand-up takes the application from HQ, never from the Mate server's state file | zcp, HQ |
| B-4 | **Bearer.** Each pool's bearer reference is written at adoption, before any deploy, by its one writer (`spec-workflows.md` DP-3), and resolves with the pool's first successful build. A pool's row is done only once it resolves; one still unresolved after the run's bounded asks is failed, naming `<pool>_LEASE_TOKEN` | zcp `ops.EnsureBearer` |
| B-5 | **The verdict.** zcp writes it to the status file's `standup` section as `verdict`: `rows`, one per pool and stage of a pooled part — `hostname`, `kind` (`pool`, `stage`), `commit`, `state` (`building`, `done`, `failed`), `appVersionId`, `processId`, `class` (`unreachable`, `refused`, `failed`), `cause` (the source's own words) and, on a failed row, `next` (its one next step) — and the `headline`, derived from the rows alone: `failed` when any row failed, `done` when every row is done, `ready` (ready to work: every pool is done, so its leases work) when every pool is, else `building`. A row still building when the run's wait ends is failed as `unreachable`, naming the wait, and when zcp restarts during a run, every row of it that is not final ends `failed`, `unreachable`, "zcp restarted during the stand-up": a run never ends with a row in flight. The section's `state` follows the headline: `running` while the run lives, then `done` only when the headline is `done` and no classic half failed, else `failed`, its `error` naming every row that is not done. The Mate server reads the verdict as written and never derives a row or the headline itself | zcp `ops.StandupRowOf`, `mate.StandupHeadlineOf`; the Mate server reads |
| B-6 | **Where the verdict shows.** The Mate server reads the verdict from zcp's status file as written and never derives a row or the headline itself. The public `/setup.json` carries only the headline, as the stand-up step's state; the rows go only over the signed-in feed | the Mate server |
| B-7 | **One starter.** While a verdict exists, the Mate server starts no stand-up of its own and does not hold the composer; without one, today's path applies (§10.10) | the Mate server |
| B-8 | **The action on a failed verdict.** When the headline is `failed` and no row is `building`, members who may operate the Mate get one action that starts an agent turn carrying the verdict, on the conversation's own agent; it starts at most one turn per verdict | the Mate server |
| B-9 | **Born by the plan.** A part planned in a Mate is born pooled by the same run as B-1…B-8: the plan's discover step ensures the repository (one for all parts of the plan), the seed (B-10), the import and the pool pair's record, then runs the stand-up's pooled build from the plan's parts instead of the tier's; one run per project (B-2's lock), the same verdict (B-5). While the plan's stand-up waits for builds, the call's progress and the bootstrap status name each building row's process, app version, phase and elapsed time. A plan outside a Mate is unchanged | zcp, the plan route |
| B-10 | **The seed.** On a `main` that is HQ's empty first commit, zcp opens the seed as the repository's first change (a commit with no parent, from the one pool template, holding exactly two files) and asks HQ to land it (`POST /api/mate/changes/:repo/:n/land`); HQ's Core lands it with no person, recorded as merged by Core with the Mate its author. zcp never pushes `main`. A repository with no `main` (a person made it empty) is never seeded: HQ lands a first change only onto its own empty first commit, so the plan is refused before any change is opened, naming a person as the one who writes `main`. The seed is written only onto a change opened for it: when HQ answers the open with another open change of the Mate (its default change of code), or with a seed change whose head holds other files, the plan is refused naming that change and writes nothing onto it. A plan sent again finds its own open seed change and asks on it, its head kept when it is the seed already; whatever HQ answers to the ask, zcp reads `main` again and judges it as below, closes the seed change when `main` was written by another (the seed's head not in its whole history), and a `main` still without the setups refuses the plan with HQ's reason, nothing imported. A plan sent onto a `main` holding code closes, the same way, each open seed change of the Mate in the repository that an earlier run left there (the Mate's open changes titled the seed's, as HQ's state lists them); HQ answering a close `change_not_open` (the seed landed beneath a later change, or was closed meanwhile) settles it and fails nothing. `zerops.yaml` gives each part a pool setup named after its pool (`<part>pool`: `build.base` and `run.base` the part's type, `run.ports` `previewPortBase + 1` … `previewPortBase + maxContainers` each with `httpSupport: true`, `run.prepareCommands` the one `zcp-devsup` install line, `start: zcp-devsup run`; nothing baked, a branch's install is its dev server's `prepare`) and a stage setup named after the part (`<part>`: `build.base` and `run.base` the type, `build.deployFiles: ./`, and `run.start: zsc noop --silent` for a runtime class that takes a start command). `.zcp/dev-servers.json` is `{"version": 1, "previewPortBase": 4000, "servers": []}`; a part's server entry is added by a lease's first `zerops_dev_server action=start`, carrying `service: <pool>` (`spec-workflows.md` DP-22). Nothing else: no README, no `.gitignore`, no application file. A `main` holding the parts' setups is written nothing; a `main` holding code without them is refused before any import, naming each missing setup and the declaration | zcp, the pool template |

Before any commit deploy the stand-up checks that `main` holds each pooled part's pool setup with the one setup check
(`ops.CheckPoolSetups` over `main`'s zerops.yaml and `.zcp/dev-servers.json`, read from HQ); a miss fails the part's
unbuilt rows `refused` with the check's words, and a pool or stage that has a build is never checked.

---

### 2.9 Updates in the product — one reader, one verb

The web client knows **nothing about versions** except the floor it needs to sign in
(the "Update required" refusal; _The sign-in floor_ below). Everything
else about "is there a newer Mate" is answered by zcp and merely displayed:

1. **zcp reads.** `zcp mate status --json` (§2.1a) is the only place that compares an installed
   mate with the stable manifest.
2. **The server relays.** The mate server runs it through a narrow spawner with its own
   timeout and error types at start (`--local`), on the keep job's `cancel` (`--local`, §2.1d) and
   on demand; the answer becomes the descriptor field `update: {installed, latest, available}` on
   `/.well-known/t3/environment` and rides the existing `serverConfig` the client already holds. A
   missing `zcp` (a standalone server) leaves the field absent, and the client shows nothing.
3. **The client shows, quietly.** `available: true` renders as one muted line on the Mate card in
   the projects overview ("0.8.0 · 0.8.1 available") and the same line in the thread header's
   environment menu — a status, never a banner, never dismissable, never stored. The upstream
   "Server versions differ" banner and its localStorage dismissals are deleted:
   they encode "client and server ship in one box", which this product does not.
4. **One verb: Update.** Next to that line. It calls `zerops.mate.update` — a Zerops-zone RPC
   gated by `exec:operate`, shaped like upstream's server self-update RPC — whose handler runs
   `zcp mate update --json` through that spawner, returns its JSON, and then the client waits for the
   socket to come back exactly as "Restart to install" does today. What
   the update came to is the server it comes back as: the descriptor's version and its `bootId`
   against those the update was pressed with. Another version is updated; the version it left on
   another boot is an update that did not take, said, with _Update_ offered again; the same boot is
   a server not restarted yet, still updating. A server that names no boot is no answer on the
   version it left. `bootId` is a value of the server process, new at every start and never
   persisted, optional on the wire — older servers send none — and not part of the zcp↔mate
   contract (C-1…C-12): zcp neither writes nor reads it. Running threads stop; the client says so before
   the click. `zcp mate update` starts the keep job (§2.1d), which asks the Mate and restarts its unit
   onto the version to run, or, when only zcp is behind, the container. When zcp itself is
   behind (`zcp.behind`, §2.1a) the server's own update is a container restart instead: the Mate
   drains its work, then runs `zcp self restart --json` (step 7).
5. **One check: Check for updates.** The status behind the line is the server's last read of it —
   at start, on the keep job's `cancel`, or on demand — so a release just published is invisible
   until one of those.
   The Mate menu therefore always offers "Check for updates"
   wherever `capabilities.mateUpdate` is true; it calls `zerops.mate.checkUpdate` (read scope,
   never operate), whose handler runs `zcp mate status --json` — the same reader, which
   reads the release source on every call — stores the answer as the descriptor's `update` and returns it, so
   the line and the verb repaint at once. The client still compares nothing (MU-1).
6. **Capabilities gate every Zerops RPC.** A surface that needs a server-side feature reads it
   from the descriptor's `capabilities` and never sends the RPC when the flag is absent — the
   version-skew rule the thread commands already follow. The data console is `capabilities.dataConsole`;
   without it the Data surface shows one line ("This Mate doesn't include the data console yet")
   with the update line and verb from step 3–4 beside it, instead of a defect from an older server.
7. **A restart is the update.** A container restart installs both halves: the zcp service's
   `run.initCommands` run `install.sh` (the latest zcp release), then `zcp init`, then `zcp init
   nginx`. The Mate unit is not boot-started: `zcp init`'s Mate step starts it once, at the end, and
   its start job starts the version to run (§2.1a, §2.1c). zcp says when one would change something:
   `zcp mate status --json` answers `zcp: {installed, latest, behind}`, `restartNeeded` and `why`
   (§2.1a), and the server relays them. The Mate server owns the idle check and the trigger: with
   `restartNeeded` true it drains its work, then runs **`zcp self restart --json`**. zcp owns
   everything that touches the platform from the container: the verb asks the platform to restart
   the service the container is (`PUT /service-stack/{serviceId}/restart`, with the project's
   `ZCP_API_KEY`, both read from the live env store as `zcp lease release` reads them) and, as soon
   as the platform accepted it, exits 0 with exactly `{"processId":"…"}`, never waiting for the
   process that stops it. Outside a container, without a key, or refused by the platform, it exits 1
   with `{"error":"…"}`, one line written for the person the Mate shows it to ("not running
   in a Zerops container, so there is no service to restart", "the platform refused the restart:
   …"), and `"refused":true` beside it exactly when the restart certainly did not happen: nothing to
   restart, no key, no client, or the platform's own 4xx (401, 403, 404, 429, or a rejection
   carrying the API's error code). A timeout, a dropped connection, the platform's 5xx or an error
   zcp cannot classify carries no `refused` and says "could not confirm the restart: …" — the
   platform may have taken it, so the Mate waits out its grace. Without `--json` it answers the same
   in one line of text. It is nothing Mate-shaped, so it
   is not gated on `ZCP_MATE_ENABLED`. The Mate sends it only to `zcp version` ≥ 9.226.0, the
   release that ships the verb: an older zcp, not knowing it, would start serve mode.

**The sign-in floor.** The GUI's minimum supported Mate server version is explicit and independent of its own package version.
**Release rule:** the hosted client's floor never exceeds the Mate version the fork's published
release manifest serves (`stable.json`, §2.1c), the version a pre-connection restart installs; a
floor is raised only after the release carrying that version is published as the latest one. Every
floor verdict — below the floor, and whether a restart helps because the descriptor's
`update.latest` reaches the floor — comes from the client's one version
comparison (MU-1); below the floor with no restart that reaches it, the Mate is shown as needing a
newer zcp and no verb is offered (the restart verdict from slice 0.9a). Version mismatches show both
the actual server version and the required minimum. Restart uses the platform service restart API
with the user's current account, sends one confirmed request, and then checks the server version; it
never repeatedly restarts on an uncertain response. No manually paired fallback exists.

### Invariants

| ID | Invariant |
|---|---|
| MU-1 | The client compares versions in exactly one place, the sign-in floor; the update line and verb render only from the descriptor's `update` field, and an update's outcome reads only the descriptor's version and `bootId` for equality with those it was pressed with. |
| MU-2 | `zerops.mate.update` is offered only inside a Zerops project with `zcp` on PATH and requires `exec:operate`; a failing `zcp mate update` is a successful RPC carrying its JSON, never a transport error. |
| MU-3 | The descriptor's `update` field is absent, never fabricated, when `zcp` cannot be run; the card then shows the installed version alone. |
| MU-4 | A restart is the update, and `zcp self restart` is how the Mate asks for one (step 7). `zcp mate status --json` always carries `zcp: {installed, latest, behind}` (`latest` `""` and `behind` false while the release is unknown; a dev build is behind only with no active dev pin and a release newer than its base) and `restartNeeded`, and `why` only when it is true; the restart is needed when zcp is behind or the running Mate answers its updater readiness with exactly 404 while a newer Mate is out. `zcp self restart --json` restarts the container's own service and answers exactly `{"processId":"…"}` once the platform accepted it; outside a container, without a key or refused, it exits 1 with `{"error":"…"}` and restarts nothing, plus `"refused":true` only when the restart certainly did not happen (no container, key or client, or the platform's 4xx) — never for a timeout, a dropped connection or a 5xx, which say the restart is unconfirmed. A service Zerops marked FAILED is refused with an error saying it updates once it is stopped and started again from outside (the Mate's Restart in the web, or Zerops), and the keep job keeps that attempt as it keeps an accepted one. `TestRunMateStatus_ZCPRelease_BehindFromTheCachedLatest`, `TestRunMateStatus_RestartNeeded_ZCPBehindOrMateWithoutUpdater`, `TestRunMateStatus_Text_ZCPBehind_OneAddedLine`, `TestBehind_ReleaseAndDevBuilds_OnlyAnOlderReleaseIsBehind`, `TestRunSelfCmd_Restart_RestartsTheContainersOwnService`, `TestRunSelfCmd_Restart_RefusesOutsideAContainer`, `TestRunSelfCmd_Restart_RefusedOnlyWhenDefinite`, `TestKeepsAttempt_FailedServiceRefusal_KeepsTheRecord`, `TestCLIDispatch_SelfIsAVerb`. |

### 2.10 A paired Mac — Mate's mechanism, zcp's guidance

A Mate can pair the user's Mac to build, run and drive macOS and iOS apps on it over the project VPN.
**Mate owns the mechanism**: the `mac` command on the agents' PATH (`~/.local/bin/mac`), the script a
Mac runs to pair, and what `mac status --json` prints. **zcp owns the guidance**: `zerops_workflow
action="mac-pair"` runs `mac status --json` on every call (bounded, 90 s — it reaches the Mac over ssh
and asks macOS for permissions) and answers with the one next step for the user, keeping no state.

`mac status --json` exits 0 with one object whenever it could tell the state:

```json
{
  "stage": "not-paired | unreachable | needs-permissions | ready",
  "pairCommands": ["zcli vpn up <projectId>", "ssh zcp mac host | sh"],
  "removeCommand": "ssh zcp mac host --remove | sh",
  "mac": null | {"name", "user", "address", "port", "macos"?, "agentDevice"?, "node"?},
  "missing": [] | ["Accessibility", "Screen Recording"]
}
```

`not-paired` carries no `mac`; `unreachable` is a paired Mac that does not answer over ssh;
`needs-permissions` is a Mac that answers while macOS has not given the agent-device daemon (`node`)
the permissions in `missing`; `ready` is everything working. zcp repeats `pairCommands` and
`removeCommand` verbatim and never composes one — the project and the host in them are Mate's.
A non-zero exit, output that is not this object, or an unknown stage is `mac` failing, reported with
its stderr and never guessed into a stage.

Presence of `mac` on PATH is the capability: a Mate that ships it answers `--json`, and a Mate without
it predates Mac pairing (zcp says it arrives with a Mate update). It is a capability probe, not a fact
of contract 1 (§2.8) — no contract bump either way. The action is a Mate's: with `ZCP_MATE_ENABLED`
off it refuses and never runs `mac` (§2.0).

| ID | Invariant |
|---|---|
| MC-1 | mac-pair's answer is derived from `mac status --json` alone, per stage, with the Mate's commands repeated verbatim; a failing, unreadable or hanging `mac` is an error naming it, a missing `mac` is "arrives with a Mate update", and outside a Mate `mac` never runs. `TestMacPair_GuidesEachStage_FromMateStatus`, `TestWorkflowTool_MacPairReachesItsHandler`. |

### 2.11 Org mode — the Chief

A **Chief** is a person's Mate across the organization: at most one per person, and an organization
holds as many as it has people who registered one. It lives in its own otherwise empty project, which
that person registers as their Chief in HQ; how it links, what the registration is and what it may
read are §3.11's. zcp serves it in **org mode**.

**Switching it on.** `zcp serve` reads the Mate's state at start from the file its server keeps (§2.8);
a state whose `chief` names this Mate's own project turns org mode on (`runtime.Info.Chief`); no state
heard is no Chief. A registration made or lost later restarts zcp: the Mate's server restarts
it when the `chief` of its link's `state` frame changes (§3.11), so org mode follows the registration
both ways. No service variable switches it and no credential of its own exists.

**No write, ever; HQ is the only door.** Org mode's only door to the organization is HQ's Chief read,
`POST /api/mate/chief/read` (§3.11), presented the Chief's project key, the key every Mate presents
to HQ. Org mode builds no Zerops client, and the Chief read only reads. HQ asks Zerops' private API
(`GetMemberProjects`) under its own identity for the registered person's projects and services, so
the Chief sees exactly what its person sees. No person token exists anywhere on this path: HQ keeps
none for it and none enters the container. HQ's refusal is shown once, in HQ's words: never retried
with another credential, and never answered by reading Zerops another way. The Chief reads the
allowlist of §3.11 — the organization's projects and their services with their state and ports,
each list whole — and nothing else: never a variable, a log, a token list, a cost or any other read
a Zerops credential could make. The organisation's people are not read: Zerops' private API has no
members method; the list returns when Zerops delivers one. Every change is the person's: their
client makes it with their own Zerops token after their click.

**Tool surface.** Look: `zerops_org_overview` (the projects its person reaches, grouped as `mate`,
`production`, `stage`, `other`, `chief`; no members). Its
`projectsVisibleToPerson` count is the number of projects the Chief's person reaches, not the
organization's total. `zerops_org_services` reads a project's services, their type, their state
and their ports. A project outside the person's reach is absent: a tool
naming it answers that no such project is among the projects the person reaches, never whether
it exists. The Chief asks nobody on the person's behalf and uses no other credential; the person
asks an Admin themselves, in Mate or in Zerops. Act:
`zerops_org_propose_service` (`restart` |
`start` | `stop`), `zerops_org_propose_env`, `zerops_org_propose_message`,
`zerops_org_propose_domain`, `zerops_org_propose_access`. Also `zerops_knowledge`, and in a Mate's
container `zerops_publish_page` (§5.9) and `zerops_list_mates` / `zerops_message_mate` (§6.10), which
reach only its person's own Mates. The single-project surface acts on the empty own project and
is not registered. A project's kind: the Chief registration; a Mate is tagged `mate` and runs a `zcp@*`
service, its name the part of the project name after the last `" - "`; production and stage are
named `"<application> - production"` / `"- stage"`. HQ is the configured shared origin (§3.5),
not a project kind; a service named `hq` does not classify its project as HQ. Org results
carry no envelope (§1.4): they describe the organization, not this project's lifecycle.

**The proposal contract.** An act tool runs nothing. Its result is one JSON document; the client
draws it as a card the person approves or declines, and executes an approved one itself:

```json
{
  "proposal": {
    "id": "<uuid>",
    "kind": "service.restart | service.start | service.stop | env.set | env.reveal | service.create | release.offer | mate.message | domain.add | access.set | mcp.add",
    "summary": "Restart medusa in Contoso - production",
    "reason": "<the agent's why>",
    "projectId": "…", "projectName": "…",
    "serviceId": "…", "serviceName": "…",
    "params": {}
  },
  "note": "Nothing has run. The person approves or declines it on the card, which carries your reason and how it ends: never restate it or ask them to approve, and don't propose it again unless they ask. You are not told the outcome: read the state again before saying it ran."
}
```

`serviceId`/`serviceName` are present for the service kinds, a service-scoped `env.set` and
`domain.add`. For `access.set` naming an application, `projectName` is the application's name and
`projectId` its first project.
`params` per kind: `{}` for the service kinds; `env.set` →
`{"scope": "project" | "service", "key", "value", "secret": bool}`; `mate.message` →
`{"to": {"projectId", "mateName"}, "words", "attachment"?: {"title", "text"}}`, where the top-level
project is the addressed Mate's; `domain.add` → `{"domains": ["contoso.com", "www.contoso.com"],
"port": 9000}`, the domains put on one routing to the service's port, summarised "Put <project> on
<domains>". Every field is validated against what the organization holds (the project, its own
non-system service, a state the action fits, a valid key, a real Mate other than the Chief; for a
domain, a port of the service that serves the web — routed HTTP or an `http`/`https` scheme, never
a Mate's own `zcp@` container, the ports Mate's addresses sheet offers — and names that are
lowercase fully qualified domain names, a leading `*.` allowed for every subdomain of one, each
once, without scheme, port or path) and refused with a
plain error otherwise. An approved `domain.add` is the client's own add-domain as the person; its
card then prints the DNS records the domains need, as their addresses do (a wildcard's beside its
`_acme-challenge` CNAME), for the person to add at their DNS provider. `access.set` →
`{"projects": [{"id", "name"}], "people": [{"person", "level"}]}`: the project named, or every
production, stage and Mate of the application named (an application's People in Mate change them
at once; never the Chief's own project), and each person once, at `full` (`ADMIN`), `read` (`READ_ONLY`), `none`
(`NO_ACCESS`, hidden) or `remove` (their own grant dropped, their organization role decides),
summarised "Access to <place>: <person> <level>, …". An approved `access.set` is the client's
own `set-project-access` as the approving person, written on each member's own list
(`PUT /client-user/{id}/roles`), the Mates a member owns left theirs; Zerops refuses it, in its own
words, to a person who may not change access. There is
no delete, no token, no invitation and no scaling kind.

**The reverse direction — a Mate asks its owner's Chief** by message, as it asks any of its
person's Mates (§6.10): `zerops_list_mates` names the Chief among them, `zerops_message_mate` sends
the ask with no card, and the Chief's answer comes back as a new message. No proposal kind asks the
Chief.

**A Mate proposes in its application's environments.** In project mode, in a Mate's container
(`ZCP_MATE_ENABLED`), `zerops_propose_service {action, project, service, reason}` (`restart` |
`start` | `stop`) and `zerops_propose_env {project, scope, service?, key, value, reason, restart?}` propose
the Chief's `service.*` and `env.set` kinds for the Mate's application's stage or production, which
its token does not reach. Each checks the target against HQ's observation (§10.8) under the
Mate's observe credential, one request at a time: `environments` names the stage and production
(by project id, name, kind or title), then `status` of that one environment its services. The
proposal is the shape above, its `projectName` the environment's title as Mate's menu shows it
("Production"; a stage by its own name, or "Stage" where HQ made the name). `env.set` carries a
plain value only, `secret: false`. A running process reads a changed value only after a restart,
so its params carry `restart`: the running services the card restarts once the value
is set — a service's own value's service, then the hostnames the Mate names in `restart` (the
readers of a project value), checked against that environment's `status` as the secret ask's are —
and `notRunning`, the named ones that run nothing now, which read it when they next start; a name
the environment lacks is refused, and so is a running service with no deployed version (a
database, cache or storage: HQ's `status` carries no type), which reads no value. The card says before the press what it restarts, adds the
running readers the vault's references name, restarts one at a time with the person's own access,
and settles "KEY is in the vault · app restarted"; a failed restart offers Try again and Finish
without restart. Both fields are optional both ways: a card older than them sets the value and
restarts nothing. The note names what the card restarts.

**A Mate asks for a secret in stage or production**. `zerops_propose_env {project, scope,
service?, key, reason, secret: true, restart?}` takes no value: one given is refused ("a secret is
never in the call"), as are a key that is not a variable name, a name Mate's vault never lists and a
reason holding a value (§5.8's rules), each before anything is observed and never repeated. zcp
checks the environment as above and, when a service is named or `restart` names hostnames, that
environment's `status` (a service's own secret restarts that service). Only a running service is
restarted: one that runs nothing (stopped, or waiting for its first deploy) is named in
`notRunning`, and its next start or release reads the value. A Mate below `SecretAskMateVersion`
(`internal/mate/manifest.go`), whose engine opens no card for it, is refused before anything is
observed, the agent told that Mate asks once it updates. It answers no proposal but env request's
`requested` shape, so Mate's engine opens a `secret` ask on the call (§5.8), its own kind so a
client that cannot write that project offers nothing:
`{"requested": {"key", "label", "why", "scope": "shared" | "service", "serviceHostname"?,
"sensitive": true, "environment": {"kind": "stage" | "production", "projectId", "name"},
"restart"?: [{"serviceId", "hostname"}], "notRunning"?: ["…"], "generate"?}, "nextActions"}`. `generate`, for a random secret, is
one preprocessor directive the card expands in the person's browser at their press, exactly as zParser
does: `<@generateRandomString(<N>)>` (N 16…1024: N random bytes, each mapped onto zParser's 65-character
alphabet) or `<@generateRandomInt(<min>, <max>)>` (uniform in [min, max]); anything else — a modifier, a
literal beside it, `generateRandomBytes` — is refused. With it the card shows no field, only what it will
make; the value is written sensitive, never shown and never sent to the engine. The card takes the value typed or
generated, writes it hidden into that environment's vault with the person's own access, restarts the
`restart` services through their own restart, and tells the engine saved — naming the project it
wrote to, which must be the one asked for — or declined; the agent hears `saved to <environment> ·
<vault>/<KEY>`, with `; <service> didn't restart: <reason>` when a restart failed and the person
finished without it, or `declined`. Nothing is minted at HQ and nothing is
recorded there. The agent asks this way whenever stage or production needs a secret — before a
release whose `zerops.yml` reads one that environment lacks, after a crash naming a missing value —
never by sending the person to a Vault page. A refused or unavailable observation answers in
`zerops_observe`'s words; nothing is minted at HQ, and the decision is HQ's as for any card.
A standalone zcp that observes through a configured HQ never registers them: no client draws its
cards.

**A Mate proposes an MCP server for its agents.** In project mode, in a Mate's container
(`ZCP_MATE_ENABLED`), `zerops_propose_mcp {name, title?, why, transport, url? | command + args?, auth,
fields?, scopes?, docsUrl?, scope?}` proposes `mcp.add`: connecting a server for every agent the
Mate runs. The agent never handles a key: a field is named and labelled, and the person types its
value on the card. It reads nothing and runs nothing; the result is the shape above from this
project, its definition apart from its credential fields, since they may later live at different
scopes (a server the organization defines, each Mate signing in itself):

```json
{
  "proposal": {
    "id": "<uuid>", "kind": "mcp.add", "summary": "Set up Linear", "reason": "<the agent's why>",
    "projectId": "<this project's id>", "projectName": "<this project's name>",
    "params": {
      "scope": "mate | project | org",
      "server": {
        "name": "linear", "title": "Linear",
        "transport": {"type": "http | sse", "url": "…"} | {"type": "stdio", "command": "npx", "args": ["…"]},
        "auth": {"kind": "none | header | oauth | oauth-app", "scopes": ["…"]},
        "docsUrl": "…"
      },
      "credentials": [{"key": "LINEAR_API_KEY", "label": "Linear API key", "placement": "env | header", "headerName": "Authorization", "prefix": "Bearer "}]
    }
  },
  "note": "Nothing is set up yet. The person adds it on the card and types any key there: …"
}
```

`name` is a key every agent's config takes (letters, digits, `-`, `_`, at most 64), never `zerops`;
`title` defaults to it. `scope` defaults to `mate`; only `mate` is set up so far, and for `project`
or `org` the note and the card say only this Mate is set up for now. `oauth`, `oauth-app` and
`header` are a remote server's; `header` needs a header field; scopes are OAuth's. An `env` field is
a command's environment variable named `key`; a `header` field is a remote server's header
(`headerName`, else `key`), its value after `prefix`. Anything that looks like a secret value — a
provider's token prefix, a long run of letters and digits that is not a UUID, a password or a
key-named query value in an address, a key-named flag with a literal value — is refused with a
plain error saying to declare a field instead. The client adds an approved server for all the
Mate's agents as the person (`mcp.servers.add`), signs in where it needs to, draws the card's state
from the server list, and tells the agent with the person's next message which agents have it.

**A Mate proposes a reveal.** Agents never read a secret's value: zcp answers names, and masks a
secret even on an explicit read. In a Mate's container (`ZCP_MATE_ENABLED`, project mode),
`zerops_propose_reveal {environment, project?, service?, key, reason}` proposes `env.reveal`: the
person sees the value on the card, on their own screen, read with their own access. `environment`
is `stage` or `production` (checked against HQ's observation as above: `environments`, then that
environment's `status` only when a `service` is named; `project` picks one by id or name) or `mate`,
this Mate's own project, read nowhere (`projectName` "This Mate", `serviceName` the hostname, no
`serviceId`: the card finds the service in its own vault). `params` → `{"scope": "project" |
"service", "key"}`, summarised "Show <key> from <environment title | this Mate>". It never carries
a value: a key that is not a variable name (`KEY=value`) is refused without repeating it, as are a
name Mate's vault never lists, a reason over one line or 280 characters, and a reason holding the
key followed by `=` or `:` and something, or a token-like run (twenty or more token characters with
letters and digits); each before anything is read. Its note says nothing is shown to the agent,
now or later, and that it is not told the person revealed it; the client tells the agent nothing
of a reveal. The person's client reveals through Zerops' own reveal (sudo step-up included), shows
the value only in the card for 30 seconds or until the person leaves, never stores it, and records
the reveal at HQ (who, which key, when) before showing it.

**A Mate proposes toward a release.** Production stays the person's: no tool lets a Mate create a
service there or release. In a Mate's container (`ZCP_MATE_ENABLED`, project mode) it asks for
them, each ask a proposal of the shape above with no `serviceId`, its `reason` one line of at most
280 characters. A client older than a kind shows its result as a plain tool row, so the client
ships before a zcp that sends it.

- `zerops_propose_create_service {services, reason}` proposes `service.create`: creating services
  the application's production lacks, raised when a change the Mate merges or is about to hand over
  needs one, before it proposes the release. `services` are hostnames, each once. zcp checks each
  against the production tier of the recipe on `main` (`GET /api/mate/recipe/production`, the
  project key, §10.8) and then against HQ's observation as above (`environments`, then
  production's `status`): one the tier does not declare is refused before anything is observed
  ("cache is not declared for production", with what it declares), one production already has
  after ("db is already in production"); so are a recipe with no production tier on `main` and an
  application with no production yet, whose first release makes it whole. `params` →
  `{"services": ["db"], "recipeHead": "<the recipe head the tier was read at>"}`, `projectId`
  production's, `projectName` "Production", summarised "Create <hostnames> in Production". It
  carries hostnames only, never the import: the card reads what it creates from HQ's own plan and
  checks it against the person's permission to release. Its note adds to read production's
  `status` before saying a service is there or proposing the release.
- `zerops_propose_release {reason}` proposes `release.offer`: that the person release what waits.
  The agent calls it only when the person asks to release or to put what is merged live; after a
  merge it says the change is merged and waits for a release, and raises no card by itself.
  What waits is HQ's to say: `GET /api/mate/release-offer` (the project key; no observation), read at the ask. While HQ names none — nothing waits, or an
  HQ older than it — the tool answers "Nothing waits for release" and proposes nothing. `params` →
  `{"head", "suggestion"}` as HQ gave them: `head` pins the ask, so the card of an ask made at one
  head is replaced once HQ offers another, and `suggestion` is the version HQ would release it as.
  `projectId` is the Mate's own project and `projectName` its application's name (this project's
  while HQ names none), summarised "Release <suggestion> to production".

A release's outcome is read, never assumed: `zerops_observe action=deployments` lists HQ's jobs, and
one that did not go out carries `reason` and `evidence.processes[].error`. The release ask's note
and the release handoff (§10.10) say so, and that when a release did
not go out because a service could not be created, the Mate says so in plain words, names the
service and proposes the creation again rather than telling the person to run the deploy again.

**Agent context.** In org mode the managed `AGENTS.md` section is the Chief's
(`agents_chief.md`) in place of the single-project doctrine: who it is, the organization, its
tools, that acting is proposing and the person decides, that project work goes to that project's
Mate, and to answer short. In a Mate's container the Chief messages its person's own Mates with
`zerops_message_mate` (§6.10), no card: one message per Mate, several in the same turn, never waiting
or polling; each answer wakes it as a new message, and once every asked Mate has answered it gives the
person one summary of what each did, what is left and what waits for review. A colleague's Mate,
which direct messages cannot reach, gets a `mate.message` proposal. A Mate's message to the Chief is
its own run, and the answer it ends with goes back to that Mate automatically. Outside a Mate's
container, where no message tools register, project work goes as a `mate.message` proposal. A Mate's own section (`agents_application.md`)
says stage and production are read with `zerops_observe` and changed only through the
`zerops_propose_*` tools the person approves, never by sending the person to a Vault page; each
tool's description says how it works (a secret asked with `zerops_propose_env secret=true`, shown
with `zerops_propose_reveal`, never printed or pasted; `zerops_propose_create_service`;
`zerops_propose_release` with where a release's outcome is read), and the mate-application playbook
the section points at says the rest, asking the person's Chief with `zerops_message_mate` for what
neither its project nor `zerops_observe` shows included. The shared section tells a Mate to connect a tool with
`zerops_propose_mcp` and never to ask the person to paste a key in chat.

| ID | Invariant |
|---|---|
| OM-1 | Org mode makes no write call to the Zerops API: it builds no Zerops client, and its one door, HQ's Chief read, only reads. `TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite`. |
| OM-2 | An act tool returns a proposal of the shape above, validated against the organization, and changes nothing. `TestProposeOrgAction_Shape`, `TestProposeOrgAction_ValidatesAgainstTheOrg`. |
| OM-3 | Org mode registers the org surface only, and its `AGENTS.md` section is the Chief's. `TestNewOrg_RegistersTheOrgSurfaceOnly`, `TestBuildAgentsMD_Chief`. |
| OM-4 | zcp org mode reads the allowlist of §3.11 through HQ's `POST /api/mate/chief/read` with the Chief's project key; HQ reads Zerops' private API under its own identity, not a kept token of the registered person, and no person token enters the container; an HQ refusal is shown once, in HQ's words. `TestChiefAccess_TheChiefHQNamesDecidesWhetherThisProjectServesOrgMode`, `TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite`. |
| OM-5 | A Mate asks its person's Chief only by message: `zerops_list_mates` names the peer the Mate's state names as its person's Chief, and no tool proposes asking it. `TestListMates_PersonsChiefAmongThem_IsNamedTheChief`. |
| OM-6 | `zerops_org_propose_domain` returns a `domain.add` proposal of the shape above for the organization's project, onto one of its service's web ports, and changes nothing. `TestProposeOrgAction_DomainAdd_Shape`, `TestProposeOrgAction_DomainAdd_RefusesWhatCannotGoLive`, `TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite`. |
| OM-7 | `zerops_org_propose_access` returns an `access.set` proposal of the shape above for the organization's project or application, each person once, at a level Mate gives, and changes nothing. `TestProposeOrgAction_AccessSet_Shape`, `TestProposeOrgAction_AccessSet_RefusesWhatCannotBeGiven`, `TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite`. |
| OM-8 | A Mate's `zerops_propose_service` and `zerops_propose_env` return a `service.*` or `env.set` proposal of the shape above for its application's stage or production, checked against HQ's observation of that one environment — an `env.set` carrying the running services its card restarts and the named ones that run nothing — and change nothing; only a Mate's project mode registers them. `TestProposeAppAction_Shape`, `TestProposeAppAction_ReadsOnlyTheTarget_OneEnvironment`, `TestProposeAppAction_Refuses`, `TestAppPropose_OverMCP_ProposesFromObservationAndRunsNothing`, `TestServer_AppProposeToolGating`. |
| OM-8a | A Mate's `zerops_propose_env secret=true` answers env request's `requested` shape with the environment and the running services its card restarts, always sensitive, never a value: a value, a key holding one or a reason holding one is refused before anything is observed and never repeated; `generate` passes only the directives the card makes as zParser does; a Mate below `SecretAskMateVersion` is refused. `TestProposeSecretAsk_Shape`, `TestProposeSecretAsk_RefusesAValue`, `TestProposeSecretAsk_RefusesWhatIsNotThere`, `TestSecretGenerate_AllowList`, `TestSecretGenerate_ExpandsAsTheCardMakesIt`, `TestCarriesSecretAsk`. |
| OM-9 | A Mate's `zerops_propose_mcp` returns an `mcp.add` proposal of the shape above from its own project, never carrying a secret value, and runs nothing; only a Mate's project mode registers it. `TestProposeMcp_Shape`, `TestProposeMcp_RefusesSecretsAndWhatCannotConnect`, `TestMcpPropose_OverMCP_ProposesAndRunsNothing`, `TestServer_AppProposeToolGating`. |
| OM-10 | A Mate's `zerops_propose_reveal` returns an `env.reveal` proposal of the shape above naming a key in its application's stage or production or its own project, never a value: a key or reason holding one is refused before anything is read; only a Mate's project mode registers it. `TestProposeReveal_Shape`, `TestProposeReveal_RefusesAValue`, `TestProposeReveal_RefusesWhatIsNotThere`, `TestRevealPropose_OverMCP_ProposesANameNeverAValue`, `TestServer_AppProposeToolGating`. |
| OM-11 | A Mate's `zerops_propose_create_service` returns a `service.create` proposal of the shape above for its application's production: hostnames the production tier on `main` declares and production's observed status lacks, with the recipe head the tier was read at, never an import; anything else is refused by the hostname. It creates nothing; only a Mate's project mode registers it. `TestProposeCreateService_DeclaredAndMissing_Shape`, `TestProposeCreateService_Refuses`, `TestAppPropose_CreateService_OverMCP_ProposesWhatProductionLacks`, `TestServer_AppProposeToolGating`. |
| OM-12 | A Mate's `zerops_propose_release` returns a `release.offer` proposal of the shape above carrying the `head` and `suggestion` of HQ's `releaseOffer`, and answers "Nothing waits for release" while HQ names none. It releases nothing; only a Mate's project mode registers it. `TestClient_ReleaseOffer_DecodesWhatWaitsAndNone`, `TestProposeRelease_WhatWaits_ShapeAndNothing`, `TestAppPropose_Release_OverMCP_ProposesWhatWaitsOrSaysNothingDoes`, `TestServer_AppProposeToolGating`. |

## 3. Trust and credentials

This section states the credentials the `zcp` container holds: what zcp and the Mate's server
present, to whom, and what they never forward. The trust and credential model between a person, a
Mate, HQ and Zerops is HQ's: `zeropsio/mate-hq:docs/architecture/identity-and-access.md`.

### 3.1 The environment rule

One explicit signal: `T3CODE_ZEROPS_PROJECT_ID` set and non-empty ⇒ Zerops mode; nothing else
votes. Every Zerops-specific behaviour keys off the server config's Zerops section, never a
re-derivation of the rule. `zcp init`
sets it from `runtime.Info.ProjectID` (§2.3); a laptop, a desktop build or a plain `mate serve` never
has it and keeps every upstream behaviour untouched.

### 3.3 Credentials

Each credential below is the only one of its kind. None is written to a log, a URL, an error body or
a span attribute.

| Credential                | Bound to                                                         | Who holds it                                | Lives           | Used for                                                                                   |
| ------------------------- | ---------------------------------------------------------------- | ------------------------------------------- | --------------- | ------------------------------------------------------------------------------------------ |
| Project key `ZCP_API_KEY` | one project at `BASIC_USER` or `ADMIN`, organisation `NO_ACCESS` | the `zcp` container (sensitive variable)    | until re-minted | everything Zerops-side inside the container; the HQ link; the container's own project read |
| Provider (AI) logins      | the Mate's current owner                                         | the container                               | the provider's  | agent runs; signed out on hand-over                                                        |

- **The project key, `ZCP_API_KEY`** — Zerops' own integration token on the project's `zcp` service,
  a sensitive service variable (`envIsolation: service`, C-14). Held by Zerops and the `zcp`
  container; zcp and the Mate's server read it there, zcp at each call. Lives as the platform keeps it; a regenerate
  changes its value. Opens the Zerops API for its project (zcp's work, §8.1's flag write, the
  Mate's own project read, §3.13) and, presented to HQ, proves "the Mate of project P in organisation
  O". HQ links only a key scoped to exactly one project: its Zerops record names exactly one
  organisation, organisation role `NO_ACCESS`, and exactly one project grant at `BASIC_USER` or
  `ADMIN`; nothing else in the record is judged. Any other key names no single project and is
  refused `key_too_wide` ("narrow the key"). HQ also refuses a key it can read in clear: it reads
  the project's and its non-system services' variables with the presented key itself, and a key
  found there in clear is refused `key_not_sensitive` ("make it a
  sensitive service variable"). The key passes HQ in memory at each first use — the
  link's first frame, a zcp call's header. When Zerops refuses the key with `401` to the Mate's
  server, the server re-reads `ZCP_API_KEY` from its env store; an unchanged value is reported to HQ
  in one link frame (`key-refused`), and HQ at once closes the link `key_refused` and revokes the
  project's Git credential. A changed value is a reconnect with the new key (newest wins, §3.5). zcp
  passes on a `401` it meets by asking HQ to recheck: `POST /api/mate/key-recheck` with the key in
  its usual header. HQ does not trust the caller; it runs one `/user/info` with that key at once (at most one
  recheck per key per minute) and on a definite refusal acts as for `key-refused`. A `403` does not
  trigger that recheck.
- **The profile's observe credential** — a narrow personal authorization issued by HQ,
  delivered by Mate into `~/.zcp/hq/observe-credential`, and read by zcp at every use.
  Only the Observe route accepts it, presented with the current own-project native key (§3.6).
  Same-profile processes share this authority; mode `0600` does not isolate processes of the same
  Unix user.
- **The Mate's Git credential** — HQ-issued. HQ sends it down the Mate's link; the Mate's server
  keeps it in its memory only, for the `gitFingerprint` its hello reports, and writes no file: the
  agent's shell runs as the same Unix user, so a file of that user's would put it in the agent's
  reach. After a server restart it holds none until HQ sends one on the next link (HQ sends one to
  a link reporting no fingerprint). zcp neither reads nor names it, and what Git presents to HQ
  stays the project key through `zcp hq git-credential` (§2.8): neither a dev service nor a pool
  holds a credential, since their git reaches HQ only through the
  relay, which presents the project key over the session (§2.8, §6.8). It opens HQ's git for the applications that hold the Mate's project, fetch,
  and push only to the project's own change branches (the rules of §10.10, *A Mate's git*); no HQ
  API, no Zerops. HQ sends one on every link where none is live for the project, and whenever the
  project enters an application; it sends `null` (revoked) when the project leaves it. Lives until
  HQ revokes it or replaces it with a new one over the link. Why the project key is never stored there: a
  dev service or pool runs the application's code and its dependencies, and `ZCP_API_KEY` there
  would give a malicious dependency project-wide Zerops write; the relay hands it to git for one
  request, over the session, and stores nothing.

### 3.5 A Mate starts and links

- The Mate's server dials out; HQ never dials in. HQ's origin is the server's production default,
  overridden in development by `T3CODE_ZEROPS_HQ_ORIGIN`; nothing discovers HQ. The server re-reads
  `ZCP_API_KEY` from the live env store before every connect, so a regenerated key is presented on
  the next one.
- zcp has no HQ default of its own: it reads `T3CODE_ZEROPS_HQ_ORIGIN`, else the default the
  installed Mate declares (`mate.hqOrigin` in its package.json, written from the server's constant
  at release; `mate.HQIdentity`). The server holds the single production default and puts the
  resolved origin into the environment of every process it starts; zcp's unit supervisor starts the
  server and a plain shell is none of its children, so both read the declaration. A container
  without a Mate has no installed Mate whose declaration it could read, so it addresses HQ only
  when its own environment carries `T3CODE_ZEROPS_HQ_ORIGIN`; zcp sets that variable nowhere (§6.7).
- The key travels in the link's first frame after the TLS upgrade and in zcp's header (§3.6), never
  in a URL.
- HQ links every project that runs a Mate — one it holds as a Mate or as a devstage; a plain
  project (S27) runs no Mate server and only calls. Otherwise HQ closes the link with the reason
  (`not_a_mate`, `key_refused`, `key_too_wide`, `key_not_sensitive`). The service id
  the Mate names is a label: the key carries no service, and two Mates in one project would be one
  trust zone anyway.
- **One link per project, newest wins.** A new link for the project closes the older one with a
  distinct close code (`superseded`). The Mate treats `superseded` as expected, resetting its
  reconnect backoff.
- Liveness is the connection: native IPv6, JSON ping/pong and the optional advertised silence
  deadline as in §2.8. A Mate reconnects with random backoff and gets a fresh state.
  A link HQ closed refusing the key (`4401`, `4403` with HQ's code) is not redialled on that
  backoff: the same hello hears the same answer. The Mate dials again once its env store changed
  (where a new `ZCP_API_KEY` lands), when it restarts, or after 15 minutes for what the container
  cannot see (HQ holding the project as a Mate again, the token edited in Zerops).
- On every link HQ sends the Mate's state and the Mate's Git credential on the terms of §3.3.

### 3.6 zcp's calls and git

- zcp's HTTP calls to HQ's Mate API — its state, repositories, changes, the recipe,
  the AI key, GitHub read tokens per call — present `ZCP_API_KEY` in a header on
  every call. A refusal carries the link's code: a key Zerops refuses is `key_refused` on both transports (`401` here, close `4401` on the link); a
  missing header is `project_key_required`.
- `zerops_observe` uses the fixed profile credential with `Authorization: Observe` on
  `POST /api/mate/observe`, alongside `X-Api-Key` from the current runtime
  `Identity.Key` (`ZCP_API_KEY`): the existing own-project native key proves the
  parent, never independent observation authority. Both are hidden from model inputs,
  errors and header tracing. Identity is resolved again at each tool invocation and
  the child file is read at each request. Missing, malformed, expired, revoked or
  refused authorization fails visibly without a child-only or project-only fallback.
  Setup guidance points to normal ZCP authorization and the Mate connection; no
  extra Observe consent, credential mint or runtime flag is introduced. The configured
  HQ/source identifies where to call; HQ owns the token's source/application/organisation binding and
  rejects changed placement or mismatched objects for direct HTTP and standalone zcp
  alike. Tool availability and the Mate flag confer no authority.
- git from the `zcp` container (zcp's scratch repository for a recipe proposal) presents the key the
  same way, as a header scoped to HQ's host, never stored in a git config or a URL.
- git from a Mate's dev service reaches HQ only through a relayed call — `zcp git` (§6.8) or zcp's
  own wiring (`ops.RelayedSSH`) — answered from the `zcp` container with the project key under
  `mate`, over its ssh session only, never written into the dev service (§2.8).
  A project without a Mate uses the temporary project-key exception in §6.7.
- `zcli push` of a self-deploy on the dev service runs over SSH with the key only in that command's
  environment, never `zcli login` (§10.10). The key is sent over SSH's standard input
  (`IFS= read -r ZEROPS_TOKEN && export ZEROPS_TOKEN && exec zcli push …`), so it is on no command
  line; it sits in zcli's environment, readable by the same user, for the push's duration.

### 3.11 The Chief

- A Chief is an ordinary Mate, linked by its own `ZCP_API_KEY` like any other; it has no credential
  of its own. The registration carries no key and no person travels in the Mate's header: HQ finds
  the live registration by the Mate's project key, and its person is the person the Chief belongs to.
- zcp reads its Chief from the Mate's state file at start and serves org mode (§2.11). HQ sends the
  Mate a new `state` when the registration changes, and the Mate's server restarts zcp when its
  `chief` changes, so a Chief that loses its registration or its person's access leaves org mode and
  a newly registered one enters it. Rotation does not wait for background work; work running at
  rotation is lost.
- zcp's org-mode reads go to HQ: `POST /api/mate/chief/read` with the Chief's project key, body
  exactly `{"read":"projects"|"services"}`. HQ asks Zerops' private API (`GetMemberProjects`) under
  its own identity for the registered person's projects and services; no person token exists on this
  path (§2.11), so the Chief sees exactly what its person sees; a project outside that reach is
  absent. Each answer is the whole list, complete or refused, never paged: `projects` is
  `{projects:[{id,name,status,tags}]}`, the organisation's projects the person reaches; `services` is
  `{services:[{id,projectId,name,status,type,category,ports:[{port,protocol,scheme,portRouting,httpRouting}]}]}`,
  every service of those projects, the platform's own among them, `ports` always present and as
  Zerops holds them. The allowlist is the organisation overview — projects, services with their
  state and ports. The organisation's people are not read (§2.11). Never environment variables,
  token lists or anything else a Zerops read could carry (non-sensitive variables read in clear, other Mates' keys among them). A
  read joins the allowlist only by the owner's decision written here. HQ's refusal `{code,reason?}`
  is shown once, in HQ's words (the route's states: mate-hq `spec/protocol.md`, Chief):

  | HQ answers                                                                                        | What the Chief's tool says                                                 |
  | ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
  | `403 forbidden` `not_chief`                                                                       | this project is no person's Chief now                                      |
  | `403 forbidden` with the person's permission reason (`not_mate_operator`, `not_active_member`, `org_not_found` — the organisation Zerops does not have, …) | the Chief's person may not operate this Chief now, with the reason |
  | `503 …`, `502 zerops_unexpected_answer`, `403 zerops_refused`, `404`, any other                   | as every HQ call: HQ unavailable, or HQ's refusal with its code and reason |

### 3.13 The Mate server in Zerops mode

- **No door of its own.** `bootstrapMethods` is the ticket only; no pairing, no Zerops token, no
  throwaway, no startup credential: nothing is minted at boot and the boot output names HQ as the
  way in. Outside a Zerops project every upstream seam stays as it was (§3.1).
- **No cookie.** No browser-session cookie is ever issued; `POST /api/auth/browser-session` refuses
  (`403 operation_forbidden` `browser_session_unsupported`).
- **Any origin, one ticket.** The WebSocket upgrade accepts any `Origin`: the socket carries no
  ambient credential (no cookie, above), and HQ's ticket is single-use, lives 60 s and is bound to no
  browser origin, so an origin check guards nothing while it would refuse a Mate opened from a
  preview or a development origin. The first frame's ticket is the only admission; no ticket or a
  refused one closes `4401`. The container no longer serves a client bundle or a sign-in page at
  `/mate/`.
- **Scopes.** A connection opened by HQ's ticket gets the standard client scopes
  plus `exec:operate` (`exec.run`: one command, `args` handed to the spawner, no shell, a non-zero
  exit a successful RPC; default timeout 60 s, cap 600 s, each stream truncated past 1 MiB). A
  project member already has a shell through code-server and the agent, so the scope adds no
  access.
- **No list in the Mate.** A Mate keeps no member list of its organisation, makes no member search
  and holds no member subscription at Zerops. With its project key the Mate reads only its own
  project (`GET /project/{own}`, at each Zerops websocket connect) and the organisation's `process`
  frames (one subscription, `clientId eq <organisation>`), for its own work. A failed own-project read (a timeout, a `5xx`, a
  `401`, a `400 projectNotFound`) keeps the project last read and is retried before the next
  websocket connect; a closed websocket reconnects with backoff.
- **What the server holds.** No session store, no HMAC key, no Zerops person token, no member list:
  its own project as last read with the project key, the open connections with their role and
  grant, the tickets acknowledged and not yet used (60 s), the project key read from the env store at
  each connect and each read, and the Git credential HQ sent its link, in memory only.

### Invariants

| ID    | Invariant |
| ----- | --------- |
| MS1-1 | `T3CODE_ZEROPS_PROJECT_ID` set and non-empty is the only signal for Zerops mode; nothing else votes. mate EV3. |
| MS1-5 | The WS upgrade admits any `Origin` that presents a valid ticket in its first frame; no ticket or a refused one closes `4401`, whatever the origin. |
| MS1-6 | In Zerops mode no browser-session cookie is ever issued. |
| MS1-7 | `exec:operate` is granted only to a connection HQ's ticket opened, and a failing command is a successful RPC. |
| MS1-8 | Outside a Zerops project every seam above is inert. |
| TC-3  | The key travels only in the link's first frame or zcp's header, never in a URL, and no log line carries it. Test to be written (HQ link, zcp client). |
| TC-10 | The dev service stores no git credential; its git reaches HQ only through the relay, which carries the project key over its ssh session only and is never written into the dev service (§2.8). `TestRelayOnly_ADevServicesPushToHQIsAnsweredThroughTheRelayWithTheProjectKey`. |
| TC-11 | zcp's Chief reads its organisation through HQ with its project key, and HQ reads Zerops' private API for it under its own identity, not the registered person's kept token; the client reads Zerops with the person's token (§2.11, §3.11). `TestOrgMode_OverMCP_LooksAndProposesWithoutAWrite`; mate-hq `spec/protocol.md`, Chief (S19c). |
| TC-18 | When the `chief` of the link's `state` frame changes, the Mate's server restarts zcp with the background-work loss of §3.11. Test to be written (Mate server). |
| TC-21 | When the Mate sends `key-refused`, HQ drops the key and closes the link. When zcp asks `POST /api/mate/key-recheck`, HQ verifies the key itself, at most once per key per minute, and cuts it off on a definite refusal. zcp `internal/hq/keyrecheck.go`. Test to be written (Mate server, zcp client, HQ link). |

---

## 5. What zcp gives a Mate's surfaces

### 5.6 Browser surface

One browser, in the container, owned by zcp. The agent drives it through `zerops_browser` (a
bounded batch; §0 rule 3); the user sees the same browser two ways, both through the mate server
(§0 rule 2), and zcp knows nothing about either viewer.

- **The card.** A `zerops_browser` call with `screenshot: true` returns the PNG as a second MCP
  content block; Claude Code forwards it (measured: ~300 KB base64 at the default viewport).
- **Whose call it is.** The daemon's frames carry no call (`{type, seq, data, metadata}`) and
  `default.target` is agent-browser's own, so zcp names the tool call that holds the browser in
  `/run/zcp-browser/default.call`, beside the stream's port, and Mate puts the live frames on that
  call's card only. The file is `{"callId","client","since","targetId"}`: `callId` is the calling
  client's own tool-call id from the `tools/call` request's `_meta`, verbatim — Claude Code sends
  its `tool_use` id (`toolu_…`) as `claudecode/toolUseId` (recorded from Claude Code 2.1.326);
  `client` is the `clientInfo.name` of the client's `initialize` (`claude-code`); `since` is when
  the call took the browser, in ms since the epoch; `targetId` is the CDP target of the call's own
  tab, the one its setup opened, absent when agent-browser's answer names none (0.39.1's `window
  new`, a `fresh-context` call). A call holds the browser from the moment it holds the
  container-wide browser lock and has its tab until its tabs are closed or, on a recycle, the
  browser stopped. zcp writes the file once the call has its tab, as a private file (`0600`)
  beside it renamed over it, so a reader sees one whole file, and removes it as the call's release
  ends, however the call ended — clean, a deadline, a cancel, a wedge or a recycle — before the
  next call can take the lock: two calls never share it. A call that takes the lock first removes
  whatever is there, so a file left by a zcp that died mid-call lasts until the next call takes
  the lock or the service ends (`/run/zcp-browser` is its `RuntimeDirectory`; the daemon's idle
  exit, the hour's age). No file means nobody is driving or the client sent no id: zcp never
  publishes a guessed or generated id, a client whose `_meta` names its call under no key zcp
  knows publishes none (zcp logs the keys it sent), and `zerops_verify`'s render check publishes
  none. Only Claude Code is known to send one; every other agent's card keeps its
  placeholder until its key is recorded. `targetId` stays on the call's own tab while the walk
  follows the call's popups (§2.6b): every tab active in the call's turn is the call's, so every
  frame streamed while the file names the call is that call's, whichever of its tabs it shows.

### 5.7 Data surface

One data console, in the container, owned by zcp.

- **The process.** The mate server spawns `zcp studio console serve` on the first request that
  needs it, one child per server, with stdin and stdout as pipes; the process contract — ready
  line, loopback bind, bearer, stdin-EOF shutdown — is `spec-dataconsole.md` §4, not restated here.
- **Read-only.** `--allow-writes` is not passed, no write token is minted, and every
  mutating route is refused by the console itself.
- **Credentials.** A service's plain values come from the Zerops API; a value the API withholds
  from the Mate's project-scoped key (a password, a secret key) is read from the container's live
  env store as `<hostname>_<key>`, each time a connection to the service is built, so a database
  added later works without a restart of anything; a withheld key the connection never reads
  refuses nothing. A service whose `envIsolation` does not admit the container answers
  `no_credentials` (`spec-dataconsole.md` §7.2), naming the service, never a value.
- **Two hosts, two processes.** The Zerops Studio extension keeps spawning its own console for the
  code-server surface. Nothing is shared between them — not the process, not the port, not the
  tokens.

This stays inside §0 rule 3. zcp learns nothing about mate here: the console is a CLI subcommand
mate spawns, not a layer zcp grows for it, and it is not configured through `zcp init` or the unit
contract (§2.8). Every zcp the fleet runs has the `console` command; one without it fails to start
the console like any other cause, and reads `unavailable`.

### 5.8 The vault — a project's variables

A Zerops project's variables, in the client: one **Shared** vault per project (the project's env,
named so because "project" is HQ's word) and one per service (its own variables), each value plain
or sensitive. A service's zerops.yml `run.envVariables` is what its container reads; the vault holds
the values those entries reference.

- **The Mate asks for a value only the person has.** `zerops_env action=request`
  takes the key, a `label` (what the value is, in people's words) and a one-line `why` (both
  required), the vault (`project=true` for Shared, else `serviceHostname`) and `sensitive`. It
  writes nothing and answers at once: `requested` (key, label, why, scope, sensitive) or
  `alreadySet`, telling the agent to say only why, in one sentence. Mate's engine records a `vault` request on that call,
  open after the turn ends, and the conversation reads as waiting on the person until it is
  answered. The person's client writes the value to the vault as them; the engine is told only
  saved or declined, records who and when, and the agent hears `saved to <vault>/<KEY>` or
  `declined` in a run of its own, a wake joining the run that asked. The value never enters the
  engine's records, a log, the agent's context or HQ. A `set` answer names the vault its `stored`
  keys went to in the request's own fields (`scope`, `serviceHostname`), so an open ask for a key
  the agent stored itself matches it by key and vault; a `set` that zcp answers as an error, its
  write process FAILED or CANCELED, closes no ask. zcp `TestEnvSet_AStoredKeysAnswerNamesTheVaultItsRequestNamed`.
  The why is at most 280 characters, the engine's own cap, so the person reads all zcp takes — what the value
  is for and where they find it — and `zerops_origin`'s why takes the same.
- **Replacing a wrong saved value, asking once, restarting after the save.** A key the vault holds answers `alreadySet`, and that answer
  names the way to replace it: `replace=true` with `replaceWhy`, one line on what is wrong with the
  saved value. Then `requested` carries `replace: true` and a why of `<why> — replacing the saved
  value: <replaceWhy>`; the person's card already writes over a held value (`open` with `held`), and the saved value stays until they save. The engine's `request-opened`
  opens no second card for a vault ask whose key and vault match a card still open.
  A Mate's zcp records each ask it opens (`<stateDir>/vault-asks/`, never
  a value) and, while it is open, answers a request for that key and vault as any request,
  `requested: {…}`, plus `alreadyAsked` (when the open ask was made, RFC 3339), its next step
  telling the agent the person's prompt is already open and not to ask again. No version gate: a
  client older than the dedupe draws a second card for a repeat (it draws one from any
  finished answer without `alreadySet`) until it updates, accepted as mild. An ask counts as
  answered once the vault holds the key written since it was asked (another row, or the held row's
  `lastUpdate` later); `again=true` asks after a message said declined. The save writes the vault
  from the person's client and the engine words the `saved` message, so zcp never sees it. From
  `CardRestartMateVersion` on the card then restarts the running readers from the
  person's client, the `saved` answer naming those it restarted (`restarted`) and one that did not
  and why (`notRestarted`); a restart that fails offers Try again and Finish without restart.
  `requested` names the `readers` and `restartAfterSave`, the live runtimes restarted after the
  save: by the card, the agent told to restart nothing itself, or — below that version — by the
  agent with `zerops_manage action="restart"` once the message says saved. The service running
  the session is never among them, nor a service holding an unreferenced key only by its name. Detail: spec-zerops-env-lifecycle §7. Additive both ways: an
  older mate ignores `replace`, `alreadyAsked` and the other new fields.
- **A secret for stage or production.** `zerops_propose_env secret=true` (§2.11)
  answers the same `requested` shape with an `environment` (`kind`, `projectId`, `name`) and the
  `restart` services, so the engine opens a `secret` ask, the vault ask's sibling: its card writes into that
  environment's vault with the person's own access to its project, restarts the named services,
  and answers saved or declined. A card open for a key and vault matches only an ask for the same
  environment, a `set` answer, which names this Mate's vault, never settles one, and neither does a
  saved answer naming another project. An engine older than it opens no ask for the call, so zcp
  refuses the ask below `SecretAskMateVersion`.
- **The organization's AI key, for an app.** In a Mate, `zerops_env action=ai-key`
  asks HQ for the organization's key as the Mate (`POST /api/mate/ai-key`, one bounded try) and
  sets it in the Shared vault: the key sensitive under the variable HQ names (`NEBIUS_API_KEY`),
  its base URL plain beside it (`NEBIUS_BASE_URL`), restarting their readers unless `skipRestart`.
  It answers the provider and the names only; the key never enters the answer, an error, a log or
  the agent's context. HQ's `404 no_key` answers that an Owner or Admin sets it in Mate's settings
  as the organization's Model key; HQ not answering or refusing the key sets nothing and says so.
  Outside a Mate the action does not exist. A Mate's AGENTS.md has an AI feature read the two as
  `OPENAI_API_KEY: ${NEBIUS_API_KEY}` and `OPENAI_BASE_URL: ${NEBIUS_BASE_URL}`.

### 5.8a The Mate's own vault

A Mate has a vault of its own: values only the Mate reads — a test sign-in, a key for its own
tools. No app reads them, they never travel to a stage or production, and they survive a rebuild of
the Mate's container. An agent that must sign in somewhere to check its work, with an identity it
cannot make itself (Google, GitHub, Zerops), needs a credential that belongs to the agent, not to
the app: the app's vault is the wrong home (apps read it, and it is listed as the app's value), and
a file in the container's home is lost on a rebuild. The person gives a value on the same ask card
as any other vault value, sees it listed in the Vault, and can change or remove it there.

- **Where it is stored.** No new storage: the user variables of the Mate's own container service —
  the `zcp` service of its project, the service zcp runs in — minus the hidden names below. The
  platform holds a service's variables through a restart and a redeploy (the project key lives
  there), and writes a changed one into the container's live env store
  (`mate.LiveEnvStorePath`) a few seconds after the write (5–10 s measured,
  `spec-zerops-env-lifecycle.md` §5), with no restart; a process started earlier keeps the env it
  started with, the Mate's server and every agent it runs among them (MD-8). The project isolates
  its services' variables (C-14): the container reads theirs, and no service is handed the
  container's — an app reads one only where its own zerops.yaml names it as
  `${<container>_KEY}`, which nothing writes.
- **The hidden names.** Two rules keep a name out of the Mate's own vault (C-15). The first holds
  in every vault: the names Mate and zcp keep for themselves, `ZCP_*`, `MATE_*`, `GITEA_*` and
  `GIT_TOKEN` (§5.8, `ops.IsVaultHidden`). The second holds in the Mate's own vault only: the
  container's own machinery, which Mate and zcp wrote on the container for themselves under other
  names — prefix `T3CODE_` (the Mate server's launch environment), the exact name
  `VSCODE_PASSWORD`, and suffix `_LEASE_TOKEN` (zcp's pool bearer references, `<pool>_LEASE_TOKEN`)
  (`ops.IsOwnVaultHidden`). In the Shared vault and in an app's vault the names of the second rule
  are ordinary. A hidden name is never listed, never asked for, never written or removed through
  `mate=true`, and never handed out by `zcp with` or `{{env:KEY}}`. What does list is what a
  person or the Mate put there for the Mate's tools.
- **The wire does not change.** `zerops_env action=request mate=true` answers the service ask of
  §5.8, `requested: {key, label, why, scope: "service", serviceHostname: <the container's
  hostname>, sensitive}`, which the Mate's engine already passes on as the ask `{kind: "vault",
  scope: {kind: "service", hostname}}`: no new ask kind, no new scope, no new field, no version
  guard. The client tells the Mate's own vault by the service (`isZcpService`), never by a hostname
  literal and never by a flag from zcp. zcp's record of the open ask is the service ask's
  (`service.<hostname>.<KEY>.json`).
- **The target.** `mate=true` is `zerops_env`'s third target, beside `serviceHostname` and
  `project=true`, and the one way zcp names the Mate's own vault: for `get`, `set`, `delete` and
  `request` it resolves to the service zcp runs in. It is offered only where asks are (a Mate's
  zcp, §5.8): elsewhere the schema does not list it and a call with it is refused. With
  `project=true`, or with a `serviceHostname` that is another service, it is refused as invalid
  usage, and so it is on `ai-key` and `generate-dotenv`, which take no vault to name. `get`
  answers keys only, the hidden names left out; `set`, `delete` and `request` refuse a hidden
  name before anything is written or asked, and `set` and `delete` refuse, by its place and
  never by its text, an entry whose key is not a key's name (a leading space would carry a
  hidden name past the rule; a `delete` entry is the name alone). A call that names the
  container's hostname by hand, without `mate=true`, is a call on that service's variables as it
  was before this section: every name listed, written and removed, the readers looked for, the
  words of §5.8.
- **Nothing restarts, and the words say how the value is used.** No service is restarted for a
  `mate=true` key and none is named to restart: `set`, `delete` and `request` plan no restart, read
  no service's deployed entries for it, and `requested` carries no `readers` and no
  `restartAfterSave`. No answer says that nothing reads the key or that it belongs in
  `run.envVariables`: the next step of a `set`, of `requested` and of `alreadySet` says the value
  is the Mate's alone and no app reads it, that a process started earlier keeps its env, and how
  it is used — `{{env:<KEY>}}` in a `zerops_browser` command for a page, `zcp with <KEY> --
  <command>` for a command — and a `set` and `requested` say that it is in this container a few
  seconds after the write with no restart. They say how a command gets the value: `zcp with` puts
  it in the command's environment, so the command is a script or program that reads it there, or
  `sh -c '…"$KEY"…'` in single quotes; a `$KEY` on the same line as `zcp with` is expanded by the
  calling shell, which is the one that does not have it, and arrives empty.
- **One reader of the live store.** `zcp with` and `zerops_browser` take a value from the
  container's live env store through one function (`ops.LiveEnvValues`), read at the call, with
  one set of refusals, each made before anything runs and none carrying a value: what stands
  where a key's name belongs is not one (letters, digits and `_`, no leading digit; never
  echoed, since it may be a `KEY=value`); a hidden name of either rule; a key the store does not
  hold; a store that cannot be read (only the system's error on the file is quoted, never one
  from its content). It serves any other key the store holds, so a project's value is used the
  same way. Both run in every zcp container, and their words follow where zcp runs: in a Mate a
  key the store does not hold is not saved yet (an open ask waits for the person), was saved
  moments ago, or was never asked (`zerops_env action="request" mate=true`); in a container that
  is no Mate, which has neither that ask nor `mate=true`, it is a variable this service does not
  have, set with `zerops_env action="set" serviceHostname=<this service>` or by the person
  where the project's values are set.
- **`zcp with <KEY>[,<KEY>...] [--] <command> [<args>...]`** runs one command with the named
  values (`cmd/zcp/with.go`). It sets the named keys in the command's environment on top of the
  caller's and replaces itself with the command (`execve` on unix; elsewhere it runs the command
  as a child and exits with its code): stdin, stdout, stderr, signals and the exit code are the
  command's. zcp writes nothing to stdout itself and runs before CLI telemetry, as `zcp run` and
  `zcp git` do (§6.8, §6.9). A refusal, and a call with no command, is one line on stderr with
  exit 2. Its other codes are a shell's: 127 for a command that is not found, 126 for one that is
  there and cannot be run — a file that is not executable, by its path or the first of its name
  on `PATH`, a directory, or one the system refuses to start. An executable script with no `#!`
  line, which the system refuses as a program, is run with `/bin/sh` as a shell runs it.
- **`{{env:KEY}}` in a `zerops_browser` command** (`internal/ops/browser_env.go`,
  `browser_script.go`, `browser_redact.go`, `browser_match.go`) is how a value reaches a page,
  where sign-ins happen. In any argument of a `commands` element, every `{{env:KEY}}` — a whole
  argument or inside one — is replaced with the value the store holds under KEY, once: a value
  that itself holds placeholder text is not read again, and the call's `url` is not searched.
  A command is read as agent-browser reads it (agent-browser 0.38, `clean_args`): its global
  flags, and the argument after one that takes a value, are taken out wherever they stand, so a
  flag is never taken for the command's name nor a flag's value for one of its arguments. Where
  an argument is JavaScript the page runs — an `eval` step's source, all its arguments joined
  with a space as agent-browser joins them; the argument after a `wait`'s `--fn` or `-f`, unless
  `--url` or `--load` makes it another wait; the value of `--restore-check-fn` — the placeholder
  stands inside a quoted string, single, double or backtick, and the value is escaped so that
  the string is exactly the value (the backslash, the three quotes, `$`, line ends and control
  characters); anywhere else it stands as it is. The whole call is refused before the browser
  is asked for: by the store's reader (above); for a placeholder in JavaScript that is not plain
  content of a string literal — in code, after a backslash, inside a template's `${…}`, in a
  comment, which is read as a page reads a classic script (`//` and `/* */`; `<!--` anywhere in
  code; `-->` that begins a line, after nothing but white space and comments; `#!` at the very
  start; a line ending at LF, CR, U+2028 or U+2029) — or where zcp cannot tell (after a `/` in
  code that opens no comment and may open a regular expression; in a `'` or `"` string that
  runs into a line end; in an `eval -b` source, which is base64), since the page's own syntax
  error would quote a piece of the value; and for a value longer than 4096 bytes
  (`browserValueMaxLen`), which is more than the search below is bounded for. The result never holds a substituted value where zcp can tell:
  - **The echo is the input.** A step names its command as it was written, byte for byte: zcp
    puts the caller's own command back by its place in the batch and never searches it. Every
    text zcp itself composes about a step — one that got no answer, one it did not run — names
    the command as written too, so neither the length of a value nor a way of escaping it
    matters there.
  - **Text from the browser side is searched.** What agent-browser and the page gave back —
    each step's result (every string and every number, and every key below the names
    agent-browser itself wraps a result in: an eval's value, a storage's keys) and error, and
    agent-browser's stderr — has each substituted value of at least six bytes replaced by its
    placeholder before zcp reads or composes anything from it: `steps`, `errorsOutput`,
    `consoleOutput`, `networkOutput`, `message` and the `fullResult` file are made from that.
    Values that overlap in a text are replaced together.
  - **The value as a browser hands it back.** A value is searched for as it is stored and as a
    page returns it once it was typed in: trimmed of the whitespace around it (an e-mail field
    trims), with its line breaks removed (a one-line field strips them), with every line break
    as LF (a textarea) and as CRLF (a submitted form), and each of its lines on its own (a
    snapshot gives one text per line) — each of these where it is at least six bytes.
  - **The forms found.** The characters of any of those in order, each one as itself; as a JSON
    or JavaScript string escapes it — `\"`, `\\`, `\/`, `\n` and the other short escapes, `\'`,
    `` \` `` and `\$` as an eval was given them, `\uXXXX` for any character with each hex digit
    in either case (a surrogate pair where it takes one), `\xHH` for one up to U+00FF — and so
    again inside JSON, up to three levels in all (JSON inside a JSON string inside a JSON
    string), a quote, a slash, an apostrophe, a backtick and a dollar escaped again or not at
    each level. Each byte of any of these stands as itself; as its `%HH` escape in either hex
    case, a space also as `+` — every percent-encoding of the value, whichever characters the
    encoder chose to escape; or as the HTML entity of its character: named for `&`, `<`, `>`,
    `"`, `'` and the no-break space (`&amp;`, `&lt;`, `&gt;`, `&quot;`, `&apos;`, `&nbsp;`), or
    numeric, decimal or hexadecimal with any number of leading zeros (`&#39;`, `&#039;`,
    `&#x27;`), for any character. A hex digit of an escape (`\uXXXX`, `\xHH`) is a byte of it
    that is not itself searched as an entity, since no encoder writes one so.
  - **Not redacted.** A form under six bytes in text from the browser side (it would be a
    common fragment of ordinary output — a short word, a number — and redacting it would shred
    the result while protecting nothing a guess does not find); any transformation not listed —
    base64, a hash, a change of letter case, another normal form of Unicode, another encoding,
    one of the listed ones applied where the list has it once (a percent-encoded entity, a byte
    percent-encoded twice, a fourth level of JSON), the value reordered or cut inside a line;
    a value the page kept — in storage or a cookie of the warm context — that a later call
    reads without naming its key, since a call knows only the values it substituted, also
    after a browser restart, as the kept sign-in carries it into the next browser (§2.6c)
    and onto the container's disk; and a
    screenshot, which is pixels — a password field shows dots, and a secret typed into a
    visible field is in the picture.
  - **The search is bounded, and what was not searched is not returned.** The text is read
    once: every reading of it that is still the beginning of a value is followed, and readings
    that have come to the same place are one. Its work is counted — a step per character read,
    per reading a character moves and per reading it makes — and one call has a hundred
    million steps (`browserRedactBudget`), a second or two of work; an ordinary megabyte of
    output takes a few million, and only a value that repeats one character against a text of
    that character comes near the bound. When the search does not finish, the result withholds: every step
    keeps its command as written and whether it succeeded, a failed one says its error is
    withheld, there is no step result, no `errorsOutput`, `consoleOutput` or `networkOutput`,
    and `message` says that text from the browser side was withheld and how to read it (fewer
    or shorter values, less output); whether the browser goes back warm is still read from the
    answer as it came (a CDP wedge, a spent fork budget). A capture that was cut at its cap —
    agent-browser's stderr at a megabyte — loses its end from where a form of a value may have
    begun: a reading of one still under way, or an escape the cut left unfinished that may have
    spelled a value's first character (a `%`, a `%` and one hex digit, a `&` and the beginning
    of an entity without its `;`), so no part of a value the cut went through remains.

  The tool says the screenshot rule where a screenshot is asked for (its `screenshot`
  parameter) and the rest where commands are written (its `commands` parameter): that the
  value comes back as the placeholder, what is not hidden (a value under six characters, one
  the page changed beyond trimming and escaping, one the page kept that a later call reads
  without naming it, even after a browser restart), what is refused before the browser opens,
  and that in JavaScript the placeholder goes inside a quoted string. Its description, at
  Claude Code's 4096-unit cap, only points there.
- **Checking behind a sign-in.** A Mate's AGENTS.md has one short block for it
  (`templates/agents_own_vault.md`, Mate-gated like the other blocks of §2.0): work behind a
  sign-in is checked signed in with an account made for testing; when the person said they check
  sign-ins themselves the agent asks for no account; it never asks for the person's own sign-in,
  never in the chat, and never prints a value; and it reads the playbook
  `zerops://playbooks/sign-in-check` before asking for a test account. The playbook
  (`internal/knowledge/playbooks/sign-in-check.md`) carries the procedure: where the app owns its
  users the agent makes a test user itself; where the identity comes from elsewhere it asks for an
  account made for testing — one with access to nothing that matters — with `zerops_env
  action="request" mate=true`, each thing the sign-in takes as a value of its own (an e-mail as
  `TEST_EMAIL`, not sensitive; a password as `TEST_PASSWORD`), says so in the why, uses a value in
  a page with `{{env:KEY}}` and in a command with `zcp with` — a program that reads its
  environment, or `sh -c` in single quotes, never a `$KEY` on `zcp with`'s own line — and never
  prints, logs or commits it.

### 5.9 Pages — the agent shows a page in the conversation

A Mate's agent shows a chart, a comparison, a plan or a gallery as a self-contained HTML page the
person sees inline, above its final reply. The page crosses the seam as a file, never as tool text:
a page with its pictures inlined runs to megabytes, and a tool result reaches the model.

- **zcp.** `zerops_publish_page` (Mate-gated, §2.0) takes a `title` and the page as `html` or as a
  `path` in the project. Every local picture an `<img src>` in the markup or a CSS `url()` in a
  `<style>` element or `style` attribute names is inlined as a `data:` URI, and every local font an
  `@font-face` names (WOFF2, WOFF, TTF, OTF, by its magic bytes); a script's, a comment's or a CSS
  comment's text is never read, since a bundle's `url(` or a template's `<img src="${x}">` is code.
  A file must be a regular file in the project — its directories' links resolved, never a link
  itself, a FIFO or a file outside it. A picture must be at most 40 million pixels
  (`ops.PageMaxPixels`, checked before it is decoded; a GIF's frames counted together) and decode whole as PNG, JPEG, GIF, WebP or
  BMP — inlined re-encoded from its pixels (WebP and BMP as PNG), so nothing but the picture rides
  along — or parse whole as SVG, entities a DOCTYPE declares passed over, never expanded; a file
  that is no picture is refused, so a secret renamed `.png` never rides along, and a path that is
  not there is named. A picture in a format nothing in zcp decodes (AVIF, ICO), a reference that
  names no file (`{{ logo }}`) and every remote resource are listed back as ones that will not load
  (`page.wontLoad`). The page, inlined, is at most 8 MiB (`ops.PageMaxBytes`), every file read
  counted against it as it is read. It is kept as `<stateDir>/pages/page-<digest>.html` for a week
  (`ops.PageKeepFor`); the result names the file (`page.file`, with `id`, `title`, `bytes`) and how
  to look at the page first.
- **mate.** Recording the call on its engine, the server reads `page.file` once into its own asset
  store — opened without following links, only a regular file named `page-*.html` in the session's
  own `.zcp/state/pages`, at most 8 MiB — so the page outlives zcp's copy.

Additive both ways: an older mate shows the call as any zcp call, and an older zcp has no tool.

### 5.10 The origin — the agent asks where a repository's code is kept

When the person gives a repository to develop, the Mate's agent asks where its code is kept, the
sibling of §5.8's ask for a value. Two answers: **kept on GitHub** — HQ mounts the repository
through its GitHub App and mirrors it both ways, alone pushing there — or **kept in Mate** — the
repository is only followed, a fetch-only `upstream`. Either way the checkout's `origin` is the
repository in HQ (*HQ alone pushes to GitHub*, below). A repository outside GitHub, or one HQ
reaches with a person's token, is only ever followed. The person picks in HQ themselves; no token
crosses the conversation.

- **zcp.** `zerops_origin action=request` (Mate-gated, §2.0) takes `url`, the repository's https
  address (canonical, `topology.CanonicalRepoURL`); `repo`, the repository in HQ the pair will
  wire — its dev hostname (§6, `hqRepoNameOf`); and a one-line `why`. A URL carrying a credential,
  a query or a fragment is refused, and no refusal echoes the URL; an address past `owner/name`,
  a page inside the repository, is refused asking for the repository's address. No input takes a
  token. It writes nothing and reads the Mate's state (§2.8) once: `connected` (`origin`; `repo`, the
  HQ repository holding it), nothing asked, when `url` already is the origin of `repo` in HQ's
  `repos`, or else of another repository of the application, which the answer names as where the
  code comes from: in that repository's own working copy when it is one of this project's
  services (`FindServiceMeta`), else fetched into `repo`'s from HQ's
  `/git/<orgId>/<appId>/<name>.git` — also when the person keeps that repository's code on GitHub:
  HQ's `main` is GitHub's default branch while the two follow each other — since an application's
  repository can be a sibling Mate's and name no host here — compared by
  `topology.SameRepoURL` (canonical form, any case). Otherwise, and from an HQ older than `repos`,
  `requested` (`origin`: `url`, `repo`, `why`), telling the agent to say only why, in one sentence,
  and end its turn. Both answers say how the code comes in, all of it in the pair's dev service
  through `zcp git <host>` (§6.8), which runs git in the checkout zcp's record names — a
  code-volume pair's volume — never on the mount: first `main` from the checkout's origin, a
  checkout holding only HQ's seed moving onto it, one sharing history merging it, one with work
  sharing none keeping that work on its branch `own-history-<commit>` and moving onto it — each read
  the decision rests on an assignment chained with `&&` (`n=$(zcp git <host> rev-list --count HEAD)
  && …`), so a failed call stops the take before anything is decided
  (`TestOriginTake_SubstitutionsFailClosedThroughZcpGit`); then per pick — kept on
  GitHub, the checkout's origin stays its repository in HQ (below), never GitHub and never a second
  remote, and work goes on a branch of the agent's own, pushed to that origin, its change opened
  after the push (`open-change`): HQ puts the branch on GitHub and opens its pull request, and the
  person merges it in Mate, so neither the agent's own `gh pr create` nor a push of its own to
  GitHub, which `zcp git` refuses (§6.8), is where the work ends; kept in Mate, the same shell adds
  the repository as `upstream` with its push URL `no_push`. A `connected` answer gives the way HQ's
  `repos[].origin.mode` says the code is kept (`github` or `follow`); from an HQ older than
  `mode`, it gives both and has the agent ask the person in one sentence. No answer has the agent
  point `origin` itself. A `requested` answer also names how the agent takes the ask back. A
  `requested` answer for a GitHub address says that, connected, HQ takes the repository's `main`
  from GitHub, so the agent never clones it in and merges it, nor merges HQ's initial commit into
  its history, and that its own setup goes in after it as an ordinary change — on GitHub a pull
  request, merged by squash; declined, code still to come in goes in as a branch of its own
  (*An existing repository brought in*, below). A repository followed outside GitHub, whose `main`
  HQ does not take, comes in that way too while `main` holds only HQ's initial commit
  (`git checkout -b <branch> upstream/main`), and `git merge upstream/main` takes its new work in
  only after that. The tool's description says the ask comes before a repository's code does.
- **Withdraw.** `zerops_origin action=withdraw` takes `url` (required: the address as it was asked,
  checked and canonicalised as a request's) and `repo` (optional: the HQ repository the ask named,
  a hostname). The agent withdraws an ask it no longer needs — it re-planned, the repository was
  retired, it asked by mistake — instead of telling the person in chat that a card is outdated (the
  tool description and every `requested` answer say so). It writes nothing and asks HQ nothing; it
  answers `{"withdrawn":{"origin":{"url":"<url>","repo":"<repo>"}},"nextActions":"…"}` —
  `withdrawn.origin.repo` is the repository the withdrawn ask named (never `connected.repo`, the one
  already holding an address), absent without `repo` — and Mate closes the card of that ask, or of
  every ask naming the address when `repo` is absent. A later `connected` answer for the same address
  closes an open ask too (`connected.origin.url`). zcp `TestOriginWithdraw_AnswersWhatItWithdrew`,
  `TestOriginWithdraw_BadInput_Refused`, `TestOriginRequest_NamesTheWithdraw`,
  `TestAnnotations_OriginTool`.
- **HQ alone pushes to GitHub.** Every checkout's `origin` is its repository in HQ,
  however the person keeps the code; the container asks HQ for GitHub tokens that read, never for
  one that can write; and HQ's mirror is the only thing that pushes to GitHub — `main` to its
  default branch while the two follow each other, each open change's branch, each described
  change's pull request and its squash-merge. `origin.mode` `github` still means what it meant to
  HQ: the repository is connected through the organisation's GitHub app and HQ mirrors it.
  - **zcp asks `read` alone.** `GET /api/mate/git-credential?host=github.com&path=…&access=read`
    is the one form zcp sends (`hq.Client.GitHubCredential`), from a relayed `zcp git` call and
    from `zcp hq git-credential` alike (§6.8, *The helper* below). HQ refuses a Mate `write` and
    `workflows` (`409 git_credential_refused`, reason `read_only_remote`) and answers a request
    carrying no `access` with a read-only token.
  - **A push to GitHub is refused in the container.** `zcp git <host> push …` whose git asks for a
    GitHub repository is answered nothing and told why, with no token asked of HQ (§6.8, *Tokens
    and access*).
  - **An older zcp, until it updates** (§2.1d, §2.9: the keep job restarts an idle container onto
    a newer zcp under the organization's policy; with automatic updates off, a person's update or
    restart does). Its pair kept on GitHub still has GitHub as its checkout's
    origin and its guidance still says to push there. Its relayed push asks `write` or
    `workflows`, HQ refuses, and it says `zcp git: github.com/<owner>/<name> is read-only for this
    Mate: HQ keeps it connected with a person's token and alone pushes there, so this push cannot
    be answered`, then git's `fatal: could not read Username for 'https://github.com/…': terminal
    prompts disabled`, exit 128 — words written for a token-connected remote, true in what they
    refuse and wrong in why. Nothing is lost: the work is still in the checkout, and pushed to
    its repository in HQ by address under a branch name GitHub does not hold yet (`zcp git <host>
    push https://<HQ>/git/<org>/<app>/<repo>.git <branch>:<new branch>`, which that zcp answers
    with the Mate's credential) and opened as a change, it reaches GitHub through HQ's mirror —
    never under the name of a branch that container pushed to GitHub itself (*The move*). Its `zcp hq git-credential`, which sends no `access`, receives a read-only token:
    fetches go on, and a push with it is refused by GitHub itself (403). Fetch, clone, ls-remote
    and lazy fetches are unchanged in both.
  - **The move.** The first pass of an updated zcp — the Mate's start for every wired pair, then
    develop's start — points such a checkout's `origin` back at HQ (below). The remote-tracking
    refs under `origin/` are GitHub's until the next `git fetch origin`; HQ's `main` is GitHub's
    default branch while they follow each other. **HQ pushes on only to branches it put on GitHub.**
    A branch it found there and did not make — one this container pushed to GitHub itself, as
    every change opened from such a checkout was — is followed forward (commits added on GitHub
    come into the change) and never pushed on: its change stays open, but once the agent pushes
    that branch to `origin` the change's head moves in HQ alone, HQ marks the branch diverged
    there, and GitHub's branch and its pull request stop following. So such work is finished under
    a new branch name pushed to `origin`, with a new change opened for it (`open-change`) — HQ
    puts that branch on GitHub and opens its pull request — and the agent tells the person the
    earlier pull request is superseded. A branch not pushed anywhere yet, and a change whose
    branch HQ put on GitHub, are unaffected. The agent reads it in the hq-git playbook, which
    AGENTS.md's git host paragraph points at, and, once, at the first develop start after the move (*The move is told once*, below).
- **The wiring is the one writer of `origin`.** Every pass that reads the Mate's state records on
  the pair how its repository's code is kept (`HQRepoRef.origin {mode, url}`; `mode` `none` for a
  repository HQ alone keeps, `mode` empty — unknown — from an HQ that names no repositories, so
  status does not ask it again), and the wiring and every pass point the checkout's `origin` at
  its repository in HQ whichever of `github`, `follow` or `none` the record says
  (`hqPairOrigin`, `hqPairOriginTarget`, over ssh, config only), as `git-push-setup` set it; a
  record that does not say leaves it. A checkout whose `origin` an older zcp pointed at GitHub is
  moved. **The move is told once.** A zcp update restarts the container, and the Mate's own
  start, whose lines reach only its log, usually moves the checkout before any develop start
  runs; a develop start that then found `origin` on HQ would say nothing. So the pass that moves
  it — the Mate's start, develop's start, or a rewire whose `git-push-setup` replaced a GitHub
  origin — records the move on the pair (`HQRepoRef.originMoved {from, to}`, in the pair's own
  record file, so it outlives the restart) and says nothing itself, and the next develop start
  says it and clears it in one hold of the record (`tellHQOriginMoved`): once, whichever pass
  moved it, by one of two starts running at once; a move that cannot be recorded is said by the
  pass rather than never, and a pair no longer kept on GitHub, or keyed by its dev pool since,
  is cleared unsaid. The line: its code is still on GitHub, which HQ mirrors both ways and alone
  pushes to; fetch `origin`, then push the branch to `origin`; work on a branch pushed to GitHub
  from the container is finished under a new branch name, as a new change, the earlier pull
  request superseded (*The move*, above); `zcp git` refuses a push to GitHub. The hq-git
  playbook, which AGENTS.md's git host paragraph points at, says the same of such a branch, and develop's start tells
  per repository kept on GitHub how it is worked on every time (below). The
  `credential.https://github.com.helper` lines that zcp wrote into such a checkout
  (`hq.GitHubHelperConfig`) are left where they are — unsetting them cannot tell zcp's emptied
  helper list from a person's own — and are harmless there: the helper they name answers a read
  token and nothing more, and a relayed call clears every persisted helper for itself. Never a
  second remote: what zcp needs of HQ it reaches at HQ's own address
  (`ops.BuildMainSyncCommand`), and what it needs of GitHub — its default branch for a
  realignment — by GitHub's address in a fetch's argv. A rewire's `git-push-setup` points `origin`
  at HQ, keeping the origin it replaces as `zerops-original-origin`, and the wiring removes that
  remote when it names where `origin` points or, kept on GitHub, the GitHub repository HQ mirrors.
  zcp `TestHQPairOriginTarget_EveryWayTheCodeIsKept_IsItsRepositoryInHQ`,
  `TestRepositoryPass_ACheckoutPointedAtGitHubMovesToHQ`,
  `TestMateStart_MovesACheckoutOffGitHub_DevelopStartTellsItOnce`,
  `TestRepositoryPass_ARewiredPairKeptOnGitHubHasNoSecondRemote`.
- **Where the agent reads the way.** Status names it on the dev half — `hqRepo=`, `origin.mode=`,
  `origin=` — from the record; the origin is read from the Mate's state (§2.8); with no state heard, `origin.mode=unknown`. Beside it, `checkoutOrigin=` is the checkout's origin
  as status reads it over ssh now, bounded at 5 s and beside the read of the repository's state;
  while it differs from where the wiring points it
  (a checkout an older zcp pointed at GitHub), it adds `(pending: develop's start points it at <url>)`,
  the URL its repository in HQ. `origin=` is the origin HQ connects to the repository — the GitHub
  repository it mirrors or follows — never the checkout's remote, which `checkoutOrigin=` names.
  Status names every remote without the credentials a URL may carry (`***@`).
  Develop's start, which runs the pass, tells per repository kept on GitHub that HQ mirrors it, that
  its checkout's origin is its repository in HQ, and how the work goes. The hq-git playbook,
  which AGENTS.md's git host paragraph points at for a repository kept on GitHub or a pending
  origin move, says the same for `origin.mode=github`, and that `checkoutOrigin` names a move still
  pending. zcp
  `TestRenderStatus_ADevHalfSaysHowItsRepositorysCodeIsKept`,
  `TestDevelopStart_SaysHowEachRepositorysCodeIsKept`,
  `TestStatus_AWiredPairNotReadYetAsksHQHowItsCodeIsKept`,
  `TestStatus_SaysWhatTheCheckoutsOriginIsNow`, `TestStatus_NamesNoCredentialOfTheCheckoutsOrigin`,
  `TestStatus_TheCheckoutsOriginIsReadBoundedBesideTheRepository`,
  `TestStatus_AnHQOlderThanRepositoriesIsAskedOnce`.
- **A repository HQ mirrors to GitHub.** Once the organisation's
  GitHub app connects a repository — its remote `via: "app"`, which the Mate's state names origin
  `github` — HQ mirrors it itself (mate-hq protocol, GitHub *Main* and *Change branches*): `main` to
  GitHub's default branch, each open change's head to its branch there, each described change's
  pull request, squash-merged on GitHub when the person merges it in Mate. A sync by hand leaves GitHub's default branch holding commits `main` lacks, and HQ then moves
  neither side (`divergedSince`). AGENTS.md's git host paragraph says HQ alone pushes to GitHub, and
  the hq-git playbook it points at that such a repository is never synced by hand — no push of its default branch, no merged change
  copied onto it, a note saying to being out of date — since a merged change reaches GitHub as its
  pull request. The repository pass, in its changes' turn, says once per repository it turns to
  (`hqMirrorPass`): that HQ mirrors it to GitHub now (owner/name) and alone pushes there, the
  checkout's origin staying its repository in HQ, and to drop any note that says to push to GitHub
  or to sync it, when the remote is the app's and the agent was not told so of that GitHub
  repository; and, while the remote reads `divergedSince`, that the two diverged, with the git
  commands of the one change that brings them back in step (protocol *Realignment*): a branch
  `realign-github` from GitHub's tip — the checkout's origin being HQ, fetched by GitHub's address
  to `refs/remotes/github/default` (`zcp git <host> fetch <GitHub address>
  +HEAD:refs/remotes/github/default`, which answers the address in the fetch's argv with a read
  token) — with HQ's `main`, fetched
  from HQ's address to `refs/remotes/hq/main`, merged in; pushed to HQ by its address and never to
  GitHub, every command of it through `zcp git`, in a lease by its alias, whose own git over ssh
  holds no credential for GitHub or for HQ's address — HQ
  realigns only onto the squash of a change whose head it pushed to the change's branch on GitHub
  itself, and never pushes a branch there it did not make — then opened and described; merged in
  Mate, HQ squash-merges its pull request on GitHub and moves `main` onto that squash. Never a
  force, never a rebase. What was said is kept per repository
  (`<stateDir>/hq/repos/<appId>/<repo>.json`, 0600; `workflow.TellHQRepo`), written in the hold it
  is decided in, so passes running at once say it once; a mirror the state no longer names, and a
  remote in step again, are forgotten, so a mirror connected again or a later divergence is said
  anew. The application's repositories with their remotes (`GET /api/apps/:appId/repos`, which a
  Mate reads with its project key; `hq.Client.AppRepos`) are read only while the state names a
  repository the pass turns to `github`; unanswered, the pass says nothing and reads again on the
  next. zcp `TestHQPass_GitHubMirror_SaidOnceWhenHQMirrorsTheRepository`,
  `TestHQPass_GitHubMirror_DivergenceSaidOnceWithTheRealignment`, `TestTellMirror_SaysEachOnce`,
  `TestClient_AppRepos_ReadsEachRepositorysRemote`, `TestClient_AppRepos_ReadsEveryPage`.
- **The helper.** `zcp hq git-credential get` answers HQ's own host with the project key under the
  user `mate` (§2.8), and `https://github.com` with what `GET
  /api/mate/git-credential?host=github.com&path=<owner>/<repo>[.git]&access=read` (as the Mate)
  answers at that call — a read token, for a repository HQ mounts or a followed one HQ holds a
  token for, never one that can push — written to git's pipe only: never cached, never logged,
  never on disk.
  HQ's refusals (`not_mounted`, `no_access`, `app_not_installed`) and an HQ that does not answer
  within the helper's bound are said on stderr and answered with nothing, so git never sends an
  empty password; a request without `path` (no `useHttpPath`) names the two config lines. The
  helper stays for sessions on the older guidance and for the checkouts an older zcp wrote it
  into; guidance runs git that talks to GitHub in the dev service through `zcp git <host>` (§6.8),
  which answers github.com for the record's origin and the URLs its own argv names — a fetch from a
  GitHub address writes no helper. zcp `TestAnswerGit_GitHubRepository_AsksHQForAReadToken`.
- **The order.** Ask, pick, then the code. HQ takes the origin's history onto a repository whose
  `main` is still its untouched seed — no other ref holds it, no record of HQ's names it — as
  `main`'s new tip, so the origin's history gains no commit of HQ's. A pair's checkout holding only
  the seed, as the wiring cut it, moves onto `main` (`git reset --keep`); a checkout with work of
  its own merges `main` in, and git refuses by name a `main` it shares no history with: two
  unrelated histories are never joined. The existing-repository guidance asks first when the
  person gives an address, naming the call (`zerops_origin action="request" url=… repo=… why=…`).
- **An existing repository brought in.** HQ seeds every repository it gives a Mate with one initial
  commit of the empty tree on `main`; merging an existing history onto it leaves `main` holding
  commits GitHub never had, which a later GitHub link reads as diverged. An existing history
  becomes `main` as it is:
  - **Code on GitHub** is connected first — `zerops_origin action="request"` naming its GitHub
    address — and HQ, following GitHub, takes the repository's `main` from it in place of an
    unwritten root, so `main` starts as GitHub's `main`; the agent
    takes it into the checkout as the answer says and never clones it in and merges it.
  - **Code from anywhere else** — a bundle, another host, a GitHub repository HQ cannot connect
    (no GitHub App) — is fetched onto a branch of its own, pushed exactly as it is, and its change
    opened from that branch; merged onto the unwritten root, HQ makes that history `main` with no
    merge commit (mate-hq, a change of unrelated history onto an unwritten `main`). It is never
    merged or rebased onto `main`'s initial commit. The checkout's `main`, holding only that
    commit, then moves onto it (`git checkout -B main origin/main`).
  - **A pair wired holding it.** A checkout that already holds an existing history when zcp wires
    it keeps that history as its `main` exactly as it is — the wiring joins nothing onto HQ's
    unwritten root (§10.10, `ops.BuildBaseBranchCommand`) — and the agent pushes it on a branch
    and opens its change, as for code from anywhere else.
  - **Either way** the Mate's own setup — the dev setups in `zerops.yaml`, `.zcp/dev-servers.json`
    — goes in afterwards as an ordinary change on a branch cut from that `main`; on a repository
    kept on GitHub that change is a pull request, merged by squash. HQ's initial commit is never
    merged into imported history.
  Where the agent reads it: the hq-git playbook, which AGENTS.md's git host paragraph (a Mate's,
  always in context) points at when an existing history or a bundle comes in, and
  `zerops_origin`'s answers and description; the existing-repository develop guidance
  (`develop-first-deploy-existing-repo`, step 1) names the ask in its call form and nothing more,
  since it renders outside a Mate too and the first-deploy guidance sits at the response size cap.
  The atom lint names `zerops_origin` only in its call form — its action first, one the tool takes
  (`content.AcceptedOriginActions`) — and refuses `--allow-unrelated-histories` in any atom. zcp
  `TestDevelopExistingRepo_RunsTheReposOwnDevFlow`,
  `TestOriginRequest_WritesNothing_NamesTheRepositoryThePairWillWire`,
  `TestOriginRequest_RepositoryOutsideGitHub_IsOnlyFollowed`, `TestAnnotations_OriginTool`,
  `TestAtomAuthoringLint_FiresOnKnownViolations`, `TestAtomLintAcceptedOriginActionsMatchTheTool`.

The Mate draws the origin request and tells the agent how it ended. This behavior is included
since contract 2; the current `MinimumMateVersion` also requires Observe credential delivery
(§2.1c).
zcp `TestOriginRequest_WritesNothing_NamesTheRepositoryThePairWillWire`,
`TestOriginRequest_URLWithCredentials_Refused`, `TestOriginRequest_RefusalNeverEchoesTheURL`,
`TestOriginRequest_AddressPastTheRepository_RefusedAskingForTheRepositoryAddress`, `TestOriginRequest_OriginAlreadyConnected_AnswersConnected`,
`TestOriginRequest_OriginConnectedToTheRepository_AnswersConnected`,
`TestOriginRequest_OriginConnectedUnderAnotherRepository_NamesThatRepository`,
`TestOriginRequest_OriginConnectedUnderAnotherProjectsRepository_FetchesItFromHQ`,
`TestOriginRequest_OlderHQWithoutRepos_Asks`, `TestOriginRequest_RepositoryOutsideGitHub_IsOnlyFollowed`,
`TestSameRepoURL`, `TestServer_ObserveToolGating`,
`TestAnnotations_OriginTool`, `TestDesiredRelease_RefusesInvalidManifest`, `TestDevelopExistingRepo_RunsTheReposOwnDevFlow`,
`TestOriginFromHQ_CheckoutHoldingOnlyTheSeedMovesOntoHQMain`,
`TestAnswerGit_AGitHubRepositoryGetsTheTokenHQMintsForIt`, `TestAnswerGit_AnHQThatNeverAnswersIsGivenUp`,
`TestGitCredential_AnswersOnlyTheLinkedHQ`, `TestRunHQGitCredential`.

### 5.11 The mirror ask — the agent asks the person to connect a repository to GitHub

The reverse of §5.10: the code already lives in a repository of the Mate's application in HQ, and
the person wants it on GitHub — a GitHub copy, GitHub pull requests from Mate's repository,
"connect to GitHub". The agent asks; the person picks the GitHub account and a repository there in
Mate, or has HQ make a new empty private one on that account in the same press (where the account
granted the organisation's GitHub app repository creation), and connects it through HQ, which then
mirrors it (§5.10, *A repository HQ mirrors to GitHub*). No
token or address crosses the conversation.

- **Request.** `zerops_mirror action=request` (Mate-gated, §2.0) takes `repo`, the repository in
  HQ by its name in the application — a dev hostname — and an optional one-line `why` the card
  shows. It writes nothing. It reads the Mate's state (§2.8) once for the application, then the
  application's repositories with their remotes (`GET /api/apps/:appId/repos`, the project key;
  `hq.Client.AppRepos`). A `repo` the application does not have, or the recipe repository (`group`), is refused naming the ones it has;
  a Mate in no application, or an HQ that does not answer, is a failure that asks nothing. A
  repository whose remote is connected and valid, through the organisation's GitHub app, answers
  `{"connected":{"mirror":{"repo":"<repo>","url":"<remote>"}},"nextActions":"…"}`: nothing is
  asked, and the agent tells the person so if they asked. Any other (no remote, an invalid one, or one
  through a person's token, which HQ only follows and never mirrors) answers
  `{"requested":{"mirror":{"repo":"<repo>","why":"<why>"}},"nextActions":"…"}` (`why` absent when
  none was given), telling the agent to say one sentence — that it asked the person to connect
  `repo` to GitHub, in the card above — never to push to GitHub itself, how to take the ask back,
  and to end its turn.
- **Withdraw.** `zerops_mirror action=withdraw` takes `repo` (required). The agent withdraws an
  ask it no longer needs instead of telling the person in chat that a card is outdated. It writes
  nothing and asks HQ nothing; it answers
  `{"withdrawn":{"mirror":{"repo":"<repo>"}},"nextActions":"…"}`.
- **mate.** The engine opens a mirror ask on a `requested` answer and draws a card holding the
  GitHub account and repository picker for `repo`, which also offers a new private repository named
  like `repo` where HQ may make one (HQ `POST …/remote/app/new`); the person connects through HQ, and the card
  settles once HQ shows the repository's remote. A `withdrawn` answer closes the card of the ask
  naming `repo`; a `connected` answer draws none.
- **Where the agent reads it.** AGENTS.md's git host paragraph names the call for a repository
  whose status says `origin.mode=none`. The atom lint names `zerops_mirror` only in its call form
  (`content.AcceptedMirrorActions`).

The first Mate that draws the mirror card is 0.16.114; `MinimumMateVersion` is at or above it.

### Invariants

| ID | Invariant |
|---|---|
| MF-18 | A page the agent publishes is self-contained or refused: its local pictures and fonts are inlined only when they are regular files in the project that are what they are named as — a picture of at most 40 million pixels that decodes whole, re-encoded from its pixels; a font by its magic bytes; script, comment and CSS comment text is never read; a format zcp cannot decode, a reference that names no file and a remote resource are named as ones that will not load; a missing file is named; the page inlined stays within 8 MiB; the result names the kept file the Mate server takes. `TestPublishPage_RealImages_InlinedAsDataURIs`, `TestPublishPage_BytesBehindAPicture_NeverRideAlong`, `TestPublishPage_Refusals`, `TestPublishPage_AFifo_RefusedWithoutWaiting`, `TestPublishPage_ScriptsAndTemplates_Publish`, `TestPublishPage_PicturesAndFonts_ByFormat`, `TestPublishPage_FontsAndHugePictures_Refused`, `TestPublishPage_AnimatedGIF_PixelsCountedAcrossFrames`, `TestPublishPage_LinkedDirectories_ResolvedBeforeTheProjectCheck`, `TestPublishPage_RemoteAndDataReferences_LeftAndNamed`, `TestPublishPageResult_NamesTheFileTheMateTakes`, `TestServer_PublishPageToolGating`. |
| MF-19 | A value of the Mate's own vault is the Mate's alone (§5.8a): `mate=true` names the service zcp runs in and nothing else, only in a Mate; its ask is the service ask on the container's hostname, with no new wire; no service is restarted or named to restart for its key, and the answers say how the value is used; a hidden name of either rule (C-15) is neither listed, asked for, written nor removed there, while the second rule's names stay ordinary in every other vault; `zcp with` hands the named values to one command and `{{env:KEY}}` in a `zerops_browser` command to one page, both through one reader of the live store that refuses a missing key and a hidden name before anything runs and prints no value; a browser result names each command as it was written, byte for byte, as does every text zcp composes about a step, and carries the placeholder where a substituted value came back from the browser side in a listed form; a placeholder in JavaScript outside a quoted string and a value over 4096 bytes refuse the call before the browser opens, and text the bounded search did not finish is withheld, never returned. zcp `TestEnvTool_MateTrue_NamesTheServiceZcpRunsIn`, `TestEnvTool_MateTrue_RefusedWithAnotherTargetOrOutsideAMate`, `TestEnvTool_Request_OwnVault_IsTheServiceAskAndSaysHowToUseTheValue`, `TestEnvTool_Get_OwnVault_LeavesTheHiddenNamesOut`, `TestEnvChange_OwnVault_RestartsNothing`, `TestEnvChange_OwnVault_RefusesAHiddenName`, `TestEnvTool_OwnVault_HidesTheContainersOwnMachinery`, `TestEnvTool_TheContainersMachineryNames_StayOrdinaryInSharedAndAnAppsVault`, `TestWith_RunsTheCommandWithTheNamedValues`, `TestWith_AMissingKey_RunsNothingAndSaysWhatToDo`, `TestWith_AHiddenName_IsHandedToNoCommand`, `TestWith_NoRefusal_PrintsAValue`, `TestBrowserBatch_EnvPlaceholder_ReachesThePageAsTheValue`, `TestBrowserBatch_EnvPlaceholder_RefusedBeforeTheBrowserOpens`, `TestBrowserBatch_EnvPlaceholder_TheResultKeepsThePlaceholder`, `TestBrowserBatch_EnvPlaceholder_APercentEncodedValueIsFoundWhicheverCharactersWereEscaped`, `TestBrowserBatch_EnvPlaceholder_AValueInNestedJSONOrHTMLIsFound`, `TestBrowserBatch_EnvPlaceholder_AStepWithNoAnswerIsNamedAsWritten`, `TestBrowserBatch_EnvPlaceholder_TheEchoIsTheCommandAsWritten`, `TestBrowserBatch_EnvPlaceholder_InAnEvalTheQuotedStringIsExactlyTheValue`, `TestBrowserBatch_EnvPlaceholder_OverlappingValuesAreReplacedTogether`, `TestBrowserBatch_EnvPlaceholder_AnEvalsValueIsSearchedKeysAndNumbersToo`, `TestBrowserBatch_EnvPlaceholder_AMissingKeyIsWordedByWhereZcpRuns`, `TestBrowserBatch_EnvPlaceholder_AValueTheBrowserNormalizedIsFound`, `TestBrowserBatch_EnvPlaceholder_InJavaScriptOutsideAQuotedStringIsRefused`, `TestBrowserBatch_EnvPlaceholder_PageOwnedKeysOfAnyStepAreSearched`, `TestBrowserBatch_EnvPlaceholder_AnyCharacterAsAUnicodeEscapeOrANumericEntityIsFound`, `TestBrowserBatch_EnvPlaceholder_AValueOverTheLengthLimitIsRefused`, `TestBrowserBatch_EnvPlaceholder_ASearchThatCannotFinishWithholdsTheText`, `TestBrowserBatch_EnvPlaceholder_ACaptureCutAtItsCapKeepsNoPartOfAValue`, `TestBrowserBatch_EnvPlaceholder_ACaptureCutByTheStepsOwnCapKeepsNoPartOfAValue`, `TestBrowserBatch_EnvPlaceholder_JavaScriptIsReadPastAgentBrowsersGlobalFlags`, `TestBrowserBatch_EnvPlaceholder_AfterAComment_APageReadsIsRefusedInCode`, `TestBrowserBatch_EnvPlaceholder_ACutKeepsNoUnfinishedEscapeOfAValuesFirstCharacter`, `TestBrowserBatch_EnvPlaceholder_AWithheldAnswerStillEndsAWedgedBrowser`, `TestBrowserBatch_EnvPlaceholder_AFirstCharacterWithAnyLeadingZerosIsFound`, `TestWith_AScriptWithNoInterpreterLine_RunsWithSh`, `TestEnvTool_MateTrue_RefusedOnAnActionThatNamesNoVault`, `TestWith_WordsFollowWhereZcpRuns`, `TestWith_ACommandThatCannotBeRun_ExitsAsAShellDoes`, `TestBuildAgentsMD_Container_ASignInIsCheckedWithAnAccountFromTheMatesOwnVault`. |

---

## 6. Git

On Zerops the thread's cwd (`/var/www`) is not itself a repository: each repository is a mounted
**dev service**, with its own `.git` living on that service's own disk, reachable only over SSH. The
absolute invariant is that **no git process ever runs against the sshfs mount** (measured: a full
turn costs 12.7 s over the mount against 1.37 s over a multiplexed SSH connection).

### 6.1 The repository set

A repository is a `fuse.sshfs` mount whose mountpoint is a direct child of
`/var/www` (`<host>:/var/www /var/www/<host> fuse.sshfs …`, one line per mounted dev service,
measured) and whose bounded probe succeeds: `stat` with a 2 s timeout, the same check
zcp runs before it reports a service mounted. The `/var/www/<host>` layout is one of the two
conventions mate reads from the container (§0 touchpoints).

A repository may also be a **lease mount**: `/var/www/<pool>-<number>` (mount source `<alias>:/var/www`,
`<alias>` the lease alias of §2.8a.3 `leases[]`) is a direct child of `/var/www` like any dev mount, so it
is a repository by the same rule and the same bounded probe; it drops out of the set when its lease is
released and the mount goes. **The name alone says it is a lease's**: a Zerops service
hostname is lowercase alphanumeric with no dash, and zcp names a lease `<pool>-<number>`
(`^[a-z][a-z0-9]{0,39}-[0-9]+$`).

zcp guarantees every mounted dev service's `/var/www/<host>` is already a git repository by the
time mate would look — bootstrap runs the HEAD guarantee (GLC-1), adopt preserves existing history or creates a snapshot (GLC-7)
(`docs/spec-workflows.md`'s Git Lifecycle section).

### 6.2 The SSH master connection

zcp's managed `~/.ssh/config` block sets the connection options for `Host *` and the `ControlPath`
template. The Mate server pins the same options and the same `ControlPath`, so its git reuses the
master connection zcp holds, and a change to zcp's template changes that.

### 6.3 The isolation unit and leases

**The isolation unit is a service or a leased container.** A part's dev pool (`docs/spec-workflows.md`
§8 "Dev pools (DP)") gives each branch a pool container of its own, leased through `zerops_lease`: one
`/var/www`, one checkout, its own processes and preview port — the same unit a dev service is, so
the Mate allows no worktree. Which conversation a lease is bound to is the Mate's own record, never a
field of zcp's.

zcp holds one set of leases for every conversation in the container, and gives the Mate these inputs.
Each `zerops_lease` result and each envelope's `leases[]` (§2.8a.3) name a lease's alias, pool, branch,
`repo` and `acquiredAt`. A lease's group is its repository and its branch, never the branch alone
(`docs/spec-workflows.md` §8 DP-17): two apps of one Mate both on `main` hold leases of one branch name.
One `zerops_lease action=acquire pools=[…]` leases a container per pool for the same branch
(`docs/spec-workflows.md` §8 DP-17, DP-23) and lists them in `pools` order, and `action=release
branch=…` releases them together. A lease that comes back `reused: true` — the conversation's branch
already held on that pool — is a lease held already, which binds nothing. A pool hands a released
container out again under the same alias, with a new `acquiredAt`.

A lease never decides where a conversation's sessions run: the agent's session keeps opening in the
Mate workspace, because zcp keeps its state in the session's working directory (`.zcp/state`: the
lease records, the work session, the ssh aliases it rewrites from them). A session opened in the lease
mount would lose every lease record ("No lease record" on release), write `.zcp/state` into the leased
checkout, and rewrite the ssh aliases from an empty set. The agent reaches the lease the way it reaches
a dev service: files under `/var/www/<alias>`, commands over `ssh <alias>`.
Because every session opens in the workspace, the workspace `CLAUDE.md` zcp writes imports each pair's
repository `CLAUDE.md` (`@<host>/CLAUDE.md`), so a repository's own instructions load at session start;
a lease's do not — they load once the agent reads a file in its lease (`docs/spec-workflows.md` §5.1).

**Release on archive and on completion.** When the person archives a conversation bound to a lease,
Mate runs `zcp lease release --branch <branch> --repo <repo> --json` (MA-6, `docs/spec-workflows.md` §8
DP-35) for the bound lease's repository and branch. Its answer is DP-35's: exit 0 lists in `released[]`
every lease it released (a branch release takes every lease of the branch); exit 3 — a lease holds
unsaved work, or another live zcp session shares it — is a refusal that keeps the leases; anything
else is a failure. zcp itself releases a branch's leases once its change in HQ is merged or closed
(DP-34), on its HQ pass and its service's look. The service's look asks HQ nothing: it reads the
Mate's changes from the file the Mate's server keeps from its link's `state` frames
(`~/.zcp/hq/state.json`, §2.8), at its service's start and each time the file is replaced, never on a
timer. Idleness never releases (§0 rule 4).

### 6.5 What stays on the mount, and why

The sshfs mount is still where the agent, the editor and every non-git tool read and write files —
writes through it are write-through (a create/edit/delete made on zcp is visible to a host-side
`git status`/`git diff` immediately, measured) and a host-side change reaches the
mount's file **content** and **path lookups** just as fast. What lags is **directory listings and
stale entries** specifically — a fixed 20 s `dcache_timeout` sshfs default the product mount does not
override, because weakening it costs 3–27× on `git status`. No watcher works over the mount either:
`inotify` never fires for a host-side change (only for a write made *through* the mount itself), so
anything that needs to know about host-side change must poll over SSH rather than watch.

**Identity, remotes and push stay zcp's.** mate never runs `git init`, never touches a remote, never
commits or pushes — those are zcp's workflow, reached from mate only through the agent's
zcp tools and `zcp git` (§6.8), the same as any other zcp mutation.

### Invariants

| ID | Invariant |
|---|---|
| MG-7 | Every script zcp runs over ssh for the repository's own work — `zcp run`, `zcp git` plain and relayed, a lease's `prepare`, the supervisor zcp starts in a lease — first moves its own shell into a transient scope of its own (`zcp-run-`, `zcp-git-`, `zcp-prepare-`, `zcp-devsup-` and its pid), with `TasksMax=infinity` and `CollectMode=inactive-or-failed`, out of the platform agent's service and its task limit of 300 (§6.9 *Its own scope*); where none can be had the script runs as before, its output, exit code and environment unchanged and nothing of the attempt shown. zcp `TestBuildServiceRunCommand_MovesItsShellIntoAScopeOfItsOwn`, `TestBuildServiceRunCommand_NoScopeToBeHad_RunsAsBefore`, `TestOwnScope_EveryScriptForTheRepositorysWork_StartsInItsOwnScope`. |

### 6.6 A Mate's HQ access is its project key

A Mate reaches its HQ with the project's `ZCP_API_KEY` and nothing else (§3.3, §3.5, §3.6): the
server's link presents it in its first frame, and zcp presents it in a header on every call — a pair
wired to its repository (§10.10, *A Mate's git*), a change described, a GitHub read token minted
for git (§5.10), the recipe proposed and its AI Agent tier read for the stand-up, a repository the tier
names checked before HQ is asked for it, whether this project is the Chief (§2.11). zcp reads the key from the
live env store at each call, its Zerops calls included, so a regenerated key is the one it presents,
without a restart of zcp or of the agent that started it. A refusal is HQ's word on
the key (`key_refused`, `key_too_wide`, `key_not_sensitive`, `not_a_mate`), said once and asked again
on the bounded backoff of §0 rule 4; a call HQ or Zerops does not answer is retried the same way.
A dev service stores no Git credential: a relayed call — `zcp git` or zcp's own wiring — presents
the key over its ssh session only (§2.8, §3.3, §10.10).

**One Mate per project.** A Mate is a `zcp@1` service, and HQ keys a Mate
by its project — its record, its link, its key entry, its overview — so a project holds one. The
service a link names is a label (§3.5): a project key carries no service, and a second `zcp`
service of the project would only supersede the first's link, visibly. So the client refuses *Set up
Mate* and *Finish setup* on a project holding several zcp services, naming them, and a Mate's tier
that declares one; inside a Mate's container `zerops_import` refuses a document that declares a
`zcp@` service, naming it, and imports nothing.

`GITEA_TOKEN`, a variable older containers may still carry, stays masked on every value dump and out
of every recipe tier; zcp reads no Gitea variable.

### 6.7 Projects without a Mate: HQ plain Git

Decision (the owner): the project key passes through HQ as a temporary exception until the Zerops business API arrives; zcp never stores or logs it beyond the existing ZCP_API_KEY handling.

Decision: zcp of a project without a Mate sends the header `Authorization: Project <ZCP_API_KEY>` on every project route and Git HTTP request (git http.extraHeader), enrolls via GET /api/project/self and POST /api/project/repos, names the repo after the dev service hostname, and leaves an existing external origin unchanged. A project that has a Mate keeps the existing Mate mode.

Decision: on 401 key_refused zcp calls POST /api/project/key-recheck once and reports the refusal plainly; no retry loop.

**Which key passes.** HQ admits at this door a key scoped to exactly one
project, the rule §3.3 states for a Mate's key: its Zerops record names exactly one organisation,
organisation role `NO_ACCESS`, and exactly one project grant at `BASIC_USER` or `ADMIN`; nothing
else in the record is judged. The key Zerops issues to a `zcp` service made through its
development-container setup (the integration token `zcp-<project>`) is admitted as issued:
`GET /api/project/self` and `POST /api/project/repos` answer 200, `git push` through HQ succeeds
and `POST /api/project/key-recheck` answers 204. A key with grants on more than one project, or
with an organisation role, is refused `403 key_too_wide`: nothing is enrolled; bootstrap
completes; the pass reports the refusal once per pair in HQ's words (`HQ project repository:
project self: hq refused: 403 key_too_wide`); the checkout keeps its own commit and gets no
remote. HQ checks that a key is not readable in clear (`key_not_sensitive`) only for a
Mate's key, not at this door (HQ's protocol, Plain project calls).

The repository pass selects this mode when `runtime.Info.MateEnabled` is false and the
container has HQ's launch origin, project ID and key. Before enrollment it reads the
checkout's actual origin as well as its recorded remote; an external origin is left alone
without creating an HQ repository. `GET /api/project/self?projectId=...` returns
`{orgId,projectId,appId}`; `POST /api/project/repos` carries `{name,projectId}` and returns
`{appId,name}`. The project ID is an assertion, never a selector for another identity.
The remote is `<HQ-origin>/git/<encoded-orgId>/<encoded-appId>/<encoded-name>.git`,
with each segment encoded as `encodeURIComponent`.

The plain door keeps the seeded-main join, which establishes shared history without losing
checkout files: a plain project pushes `main` itself, with no change for HQ to land, so its `main`
must descend from HQ's seed. A Mate's wiring never joins (§10.10); `ops.BuildProjectRepositoryCommand`
alone asks for the join, `TestBuildProjectRepositoryCommand_JoinsTheSeed`.
The pass itself pushes nothing. After the join, a checkout that held
only zcp's own initial commit has HQ's seed commit as its history; zcp's commit is no longer in
it. `main` moves on HQ only when someone runs `zcp hq git push` from the mounted checkout (seen:
two pushes of `main` landed while the project was alone in its application).
The remote is recorded without a Mate `ServiceMeta.HQ` record: plain Git storage does
not create a Mate change, recipe or release. HQ owns ref permissions, including the
difference between isolated and shared applications; zcp neither invents a branch policy
nor retries a refused publication.
`GitPushState` is not written and reads `unconfigured`, so for such a pair:
`git-push-setup` still asks for a remote and a personal access token and answers
`INVALID_PARAMETER` without one, and HQ issues no such token; `zerops_deploy
strategy="git-push"` answers `PREREQUISITE_MISSING`; launch-production stops at
`source-control-required` with blocker `git-push-unconfigured`; pool-migrate refuses `no-origin`
(`spec-workflows.md` §13 PM-9); export answers `compose-ready` with a `buildFromGit` naming HQ's
address and no warning, a bundle the platform's import refuses (`spec-workflows.md` §12.5).

Plain passes share Mate wiring's pair lock and 30-second-to-10-minute backoff.
A refused key stops further HQ calls for that pass, including further key rechecks.
A seeded-main refusal names its state and remedy; the remedy uses `zcp hq git fetch`
for authenticated reads. Subsequent passes inspect a digest of the checkout's HEAD,
branch refs, origin and Git status and do not repeat enrollment or setup until that
input or the HQ identity changes. Mate wiring currently retains only its timed
backoff for these refusals.

On the mounted checkout, `zcp hq git <git-arguments>` runs native Git with the live
project key in an origin-scoped `http.extraHeader` environment entry. Setup supplies
that header to its remote Git process through SSH stdin. Neither path writes the header
into `.git/config`, copies the key into `GIT_TOKEN`, or creates a credential file.
Inherited Git tracing is removed before the key reaches a Git subprocess. External
remotes receive no HQ header. The wrapper redacts the exact key from stdout and
stderr, including configuration listings. The key and origin are read again on each invocation.

HTTP refusals preserve HQ's status, code and reason: missing/refused keys (`401`),
wide keys, non-plain placement or Zerops refusal (`403`), project removal
(`404`), invalid repository names (`400`), and unavailable/unanswered HQ or Zerops
(`503`). A key recheck is one request with no recursive recheck; even a successful
recheck does not replay the original operation. Project HTTP calls do not follow
redirects or retry `503` responses. Native Git cannot expose HQ's JSON refusal;
after an authentication failure naming this exact HQ origin, `zcp hq git` reads
project self once to obtain that verdict and apply the same recheck rule. It
preserves Git's exit code and never repeats the Git command. That verdict read and
its recheck share a 15-second deadline. The wrapper refuses in Mate mode, whose
git reaches HQ through `zcp git` with the project key (§6.8, §2.8), and forwards cancellation as SIGTERM with a
five-second cleanup window. Output retains only a suffix that could begin the
key; ordinary lines pass through immediately, while an ambiguous partial key
waits for more output or exit.

Witnesses: `TestProjectClient_*`, `TestReconcileHQRepositories_PlainProject_*`,
`TestRunGit_*`, and `TestGitEnv_DoesNotExposeAuthorizationThroughInheritedTracing`.

### 6.8 A dev repository's git runs in its dev service

`zcp git <host> [-c <key>=<value>]... [-C <dir under /tmp/>] <subcommand> [<args>...]` runs one
git command in the dev service `host`, over ssh, in its checkout (the directory zcp's record says
its deploys run in, else `/var/www`; a lease's `/var/www`) or a cleaned `-C` path strictly below
`/tmp/` (a dev-side worktree) — never in the zcp container, never on its sshfs mount
(`cmd/zcp/git.go`, `internal/ops/git_brokered.go`). The remote command is `/bin/sh -c '<script>'`
whatever the login shell; the script unsets git's tracing, TLS, askpass and redirecting variables
and runs git with `GIT_TERMINAL_PROMPT=0`, every argument ShellQuoted; before git, its shell moves
into a scope of its own, `zcp-git-<pid>.scope`, as `zcp run`'s does (§6.9 *Its own scope*), since a
hook runs the repository's checks. stdout, stderr and the exit
code are git's (255 is ssh's own failure); `zcp git` prints nothing to stdout itself and runs before
CLI telemetry. stdin is git's only when the argv makes git read it — a standalone `-`, `--stdin…`,
`-F -`, a file option or operand naming `/dev/stdin`, `/dev/fd/0` or `/proc/self/fd/0`, or a
subcommand that reads it by its nature (`BrokeredGitReadsStdin`, one rule for plain and relayed
calls) — so any other call's remote reads nothing and a caller reading its stdin on, such as a
loop over hosts, keeps it. Ctrl-C or SIGTERM stops the ssh client with exit 130, saying the remote
git may still be running and its lock clears when it finishes. Refused with exit 2 before any ssh
call: a loopback or the container's own name, a name that is no hostname or lease alias, a call
outside a container, a stage (naming its dev), a pool-keyed pair (naming the lease route), a lease
alias the lease check refuses (below), and any leading option but `-c` and one `-C`. Exit 1 is a
record zcp could not read.

**Leases.** A lease alias `<pool>-<n>` is asked about before any ssh call (docs/spec-workflows.md
§8 DP-2): `ops.LeaseTargetChecked` over the API client and orchestrators `zcp init`'s lease restore
builds, one renew of the record's lease. Only zcp's own lease records make an alias reachable, so
one it holds no record of is refused without asking anyone. A lost lease is refused naming the
release that drops its record; an unverified one — the orchestrator did not answer, or this zcp has
no orchestrator or API client to ask with — naming why and the status that asks again; a parked one
(DP-28) naming the unpark. Each is a refusal, exit 2, in the check's own words: nothing reached the
container. A lease the orchestrator gives zcp runs git in its checkout, `/var/www` of its
container, reached by the alias; in a Mate container a lease of a pool pair wired to HQ is relayed,
answered from that pool pair's record.

**Relayed or plain.** In a Mate container every subcommand of a pair wired to an HQ repository, or
of a lease of such a pool pair, is relayed, wired through or not (`isRelayedGit`): its checkout is a `blob:none` partial clone
(§10.10, *A Mate's git*), so `checkout`, `show`, `log -p`, `worktree add` or a hook may fetch
contents lazily and ask for a credential again and again. Every other call is the plain
passthrough, with no helper. A relayed call also refuses, with exit 2, git's `credential`
subcommands, a stdin over 1 MiB, and a user `-c` (named, never its value) under `credential.`,
`http.`, `url.`, `protocol.`, `remote.`, `include.`, `includeif.`, `uploadpack.` or `alias.`, or
`core.sshCommand`, `core.askPass`, `core.gitProxy`, `core.fsmonitor`, `fetch.parallel` or
`submodule.fetchJobs`, matched in any letter case.

**The relay** (`internal/ops/git_brokered_relay.go`). The script keeps the session's stdin as
descriptor 3 (answers) and a dup of its stderr as 4 (frames, so a hook's `2>/dev/null` swallows
none), with copies of both at 8 and 9. A helper whose 3 or 4 is not the same file as its copy
(`test -ef` on `/dev/fd`, where the system has it) answers nothing at once and writes no frame: a
hook pointed it elsewhere, or closed it and git reused the number — on Linux git-remote-http's
sockets take it without close-on-exec, and a frame written there would go onto the wire while the
helper waited for an answer that never comes. A stdin the argv asks git to read is sent first and staged whole into
`/tmp/zcp-stdin.XXXXXX` before git starts; any other call's git reads `/dev/null`, so 3 carries
answers only. git runs with `-c credential.helper=`, clearing every persisted helper, then zcp's
one-line get-only POSIX helper, `credential.useHttpPath=true`, and `gc.autoDetach=false` and
`maintenance.autoDetach=false` — a detached auto-gc keeps 3 and 4 and would hold the session —
under `GIT_ALLOW_PROTOCOL` (https, HQ's scheme, `file` only for an absolute `.bundle` path in
argv), which overrides every `protocol.*` key in the checkout's config. The helper takes the call's
lock, `mkdir /tmp/zcp-relay-<nonce>.lock`, giving up 90 s by the clock (`date +%s`) after it began,
and writes `ZCPCRED-<nonce> <pid> <protocol> <host> <path>` on 4. zcp takes a frame out of ssh's
stderr only at a line's start with this call's nonce, never relays it, and writes exactly one
answer line per frame with a readable id: `<pid> <host> <user> <password>`, `<pid> <host> - -`
(declined), or `<pid> - - -` (malformed); a line over 64 KiB is relayed as git's output, never
parsed, and a frame grown past it is answered as malformed. The helper takes only an answer
echoing its pid and host, hands git nothing for a decline, so git never sends an empty password,
and frees the lock when it ends or is signalled. A call says at most 32 distinct lines.

When git exits the script writes `ZCPEND-<nonce>` on stdout, then `ZCPDONE-<nonce> <exit>` on 4,
and the call ends there: zcp answers no further frame, closes 3 so a waiting helper frees the lock,
revokes the call's tokens at once, and returns the marker's exit code. git's stdout passes through
a pipe of zcp's own, copied to the caller at the caller's pace with ssh waiting behind it and the
end mark taken out, so no byte git wrote is lost however slowly the caller reads. A session still
open a second after both the marker arrived and every byte before the end mark reached the caller
is ended: a process a hook or alias left running may hold descriptors 3 and 4, or stdout too, and
what it writes to stdout after the end mark is dropped. Once ssh ended, zcp reads the stdout pipe
while it gives more or the caller is still being written to, and closes one silent for 2 s; a call
the caller stopped waits at most 2 s for it. A caller whose reader went away ends the copy, which
closes the pipe: ssh dies of SIGPIPE writing to it, as it did writing there itself, and the remote
git with its session. `zcp git` catches SIGPIPE and never ignores it — an ignored disposition
survives exec, a caught one is reset in every child — so its own write fails with EPIPE while ssh
keeps the default; the call still revokes its tokens, and returns git's exit code when the marker
came first, else ssh's failure (`TestHandleSIGPIPE_*`).

**A relay frame is answered only from zcp-side records and the call's own argv — never from the
dev checkout's config, which the dev service writes, nor from `ServiceMeta.RemoteURL`, written from
the checkout's live origin: HQ mints for any repository the application mounts.** HQ's own origin,
from the Mate's identity, is answered for any path with the Mate's project key under `mate` (§2.8).
`https://github.com` is answered, for reads alone, for the record's HQ origin, which zcp writes
from HQ's state (while it names none, HQ's live origin of the record's repository, read once), and
for a URL a fetch, pull, push, ls-remote or clone names as a positional argument. A remote name, a
`url.insteadOf` rewrite and a URL anywhere else in argv count for nothing; everything else is
declined and said once.

**Tokens and access.** For a GitHub repository zcp asks HQ, as the Mate (§6.6), `GET
/api/mate/git-credential?host=github.com&path=<owner>/<name>&access=read` with the path in
HQ's canonical form (lower-cased, no `.git`; a path that is no `owner/name` is declined unasked),
once per repository per call, and answers git with that token as often as git asks. `read` is the
only access zcp asks, for a fetch, clone, ls-remote, a lazy fetch and zcp's own relayed scripts:
HQ alone pushes to GitHub (§5.10, *HQ alone pushes to GitHub*). A call whose subcommand is `push`
is answered for HQ only. When git asks, inside it, for a GitHub repository the call would answer a
read for, zcp answers nothing, asks HQ for no token, and says once why, in words chosen by the
repository. For the one HQ mirrors the pair's code to (`BrokeredGitRelay.Mirror`: the record's HQ
origin while its mode is `github`, or the live origin read in its place) it says what to do
instead — push the branch to `origin`, its repository in HQ (`zcp git <host> push origin
<branch>`), and open its change; HQ puts the change's branch on GitHub and opens its pull request,
unless GitHub already holds a branch of that name HQ did not put there, when the work goes under a
new branch name; GitHub's default branch moves only by a merge; a checkout whose `origin` still
names GitHub is moved by develop's start. For any other repository — a URL in the push's argv, an
origin only followed — it says that zcp gives no token that pushes to GitHub and that HQ pushes
only to the repository it mirrors, and names no push to `origin`: nothing HQ does would carry it
there (`TestBrokeredGit_Relay_PushToAGitHubRepositoryHQDoesNotMirror_ToldNoPushToOrigin`,
`TestBrokeredGitRelayFor_OnlyAnOriginKeptOnGitHub_IsTheMirror`). git ends on the declined
credential (`could not read Username`, exit 128). The push runs as the one ssh call it is: nothing reads the checkout's config or commits
first (`TestBrokeredGit_Relay_PushToGitHub_RefusedWithoutAskingHQ`). zcp cannot tell a push's own
request from a read git starts inside it, so such a read is refused with it; a checkout whose
origin is HQ fetches its lazy contents from HQ. A push a hook starts inside a call that is no push
gets the call's read token, and GitHub refuses it. Each token is revoked when the call ends, also
when it was cancelled — `DELETE
/installation/token` at GitHub's API on a detached context, best effort, a failure one stderr line
naming the repository, never the token. The Mate's HQ credential is never revoked. No credential
reaches ssh's argv, the caller's stdout or stderr, or a file the call leaves in the dev service.

**Residual risk.** Code the call starts in the dev service — a hook, an alias, what a hook runs —
inherits descriptors 3 and 4 and can ask, while the call lasts, for what the call is answered with:
a read token for the pair's GitHub repository, and the Mate's HQ credential, which covers every HQ
repository of the application (new exposure for a pair not wired through). zcp asks HQ only for
GitHub tokens that read; what HQ answers a request for any other access is HQ's to state.

**What stays.** `zcp hq git-credential` and the helpers persisted in a checkout's `.git/config`
keep working, for HQ and for reads from GitHub (§5.10, *The helper*); a relayed call clears them
for itself, so there they are inert. Agent guidance names `zcp git <host> …` for every git command of a Mate's dev repository —
the develop, delivery and pool-migrate atoms, the AGENTS.md git host, and the words zcp gives
about changes, origins, branch refusals and their remedies — found by the record's repository
(`repoDevHost`; a recipe may name it otherwise), the agent's worktrees below `/tmp/wt-<branch>` in
the dev service; a lease's git stays `ssh <alias> "cd /var/www && …"`, and a project without a Mate
keeps `zcp hq git` on its mounted checkout (§6.7), where `zcp git` has no Mate identity to answer
with: a text it reads too keeps the commands it gave before `zcp git` — the git-push refusal and
pool-migrate's work branch byte for byte, gated on `runtime.Info.MateEnabled`; an atom, which has
no axis for a Mate, with its Mate's form in an "In a Mate" paragraph of its own
(`TestGitPushToHQ_PlainProject_KeepsTheCheckoutPushWords`,
`TestPoolMigrate_WorkBranch_FromChangeCurrentOrUpstream`,
`TestAgentGitContent_OutsideAMateKeepsItsCommands`). A Mate's branch refusal of a history of its own
carries the commit zcp's command read in the service (`ZCP_BASE_NOT_SEED <commit>`), so its remedy
names `zcp git <host> branch -f own-history-<commit> <commit>` literally
(`TestBaseBranchRefusalRemedy_Mate_NamesTheCommitLiterally`); a plain project's remedy is unchanged
(`TestBaseBranchRefusalRemedy_PlainProject_Unchanged`). No agent-facing text runs git on the mount,
in the agent's own shell, in a zcp-side worktree, in a `$(git …)` substitution or a `$(zcp git …)`
that fails open, nor tells it to delete `index.lock` or force or prune a worktree
(`TestNoMountGitInAgentContent`, `TestAgentGitTexts_RenderedCommandsRunInTheService`). A binary
older than `zcp git` given one starts an MCP server and hangs: only a downgrade, or a revert of the
release that shipped `zcp git` after the guidance that names it, brings that back. In a Mate container code-server's user settings carry
`"git.enabled": false`, so its Git never opens the `/var/www/<host>` mounts (§2.0). A remote side
killed before its traps run can leave an empty `/tmp/zcp-relay-<nonce>.lock` or a
`/tmp/zcp-stdin.XXXXXX` holding the caller's own stdin, never a credential; a stopped call's
`index.lock` clears when its git ends, and nothing tells an agent to delete it. After a `zcp git`
that changes files, the mount's directory listings can show the old state for up to the 20 s of
§6.5.

Witnesses: `TestBrokeredGit_Relay_RemoteAddedInCheckoutConfig_Declined`,
`TestBrokeredGit_Relay_RemoteURLRecordIgnored`, `TestBrokeredGit_Relay_NoSecretInArgvStdoutStderr`,
`TestBrokeredGit_Relay_PartialClone_LazyShowCheckoutWorktreeAnswered`,
`TestBrokeredGit_Relay_PersistedHelperNeverRuns`,
`TestBrokeredGit_Relay_HelperDescriptorsNotTheSessions_AnswersNothingAtOnce`,
`TestBrokeredGit_Relay_DoneMarker_DetachedHolderDoesNotHoldTheCall`,
`TestBrokeredGit_Relay_DoneMarker_SlowReaderGetsEveryByte`,
`TestBrokeredGit_Relay_DoneMarker_StdoutHolderBoundedByTheGrace`,
`TestBrokeredGit_Relay_CallerStdoutGone_EndsWithGitsExitCode`,
`TestRelayStdout_EndMark_TakenOutWhereverReadsSplitIt`,
`TestBrokeredGit_Relay_CancelledCall_StillRevokesOnDetachedContext`,
`TestBrokeredGitFrames_EveryFrameWithReadableId_GetsExactlyOneAnswer`,
`TestBrokeredGit_StdinReaders_PlumbingStdinPathsAndSeparatedValues`,
`TestBrokeredGit_Relay_PushToGitHub_RefusedWithoutAskingHQ`,
`TestBrokeredGit_Relay_Realignment_GitHubsTipReadByAddress_PushedToHQ`,
`TestBrokeredGit_Access_NonPushCallAsksRead`,
`TestBrokeredGit_Access_PushInsideNonPushCall_GetsReadAndFails`, `TestRunGitCmd_*` (leases:
`TestRunGitCmd_LeaseAlias_NoRecord_Refused`, `TestRunGitCmd_LeaseAlias_NotVouched_RefusedBeforeSSH`,
`TestRunGitCmd_LeaseAlias_Parked_RefusedNamingTheUnpark`,
`TestRunGitCmd_LeaseAlias_Owned_RunsInTheLeaseCheckout`; plain stdin:
`TestRunGitCmd_PlainCall_StdinOnlyWhenGitReadsIt`), and
`TestConfigureVSCode_MateTurnedOff_GitSettingRemoved`; the relay tests run
the real script against real git under `/bin/sh` and, where installed, dash and busybox `sh`.

### 6.9 A command runs in its service

`zcp run <host> [--] <command> [<args>...]` runs one command in the service `host`, over ssh
(`cmd/zcp/run.go`, `internal/ops/service_run.go`) — where the code's runtime and dependencies are.
The same command started in the zcp container on the sshfs mount runs on zcp, reads every file over
the network and takes the cores and memory the Mate itself runs on; where the mount holds
`node_modules` it works, slowly, and nothing says it ran in the wrong place.

- **Where.** In the folder of the service the caller stands in: zcp mounts `host`'s `/var/www` at
  `/var/www/<host>`, so a call from `/var/www/<host>/<rel>` runs in the service's `/var/www/<rel>`
  (`ops.ServiceRunDir`). From anywhere else it runs in the service's checkout, as §6.8 finds it.
- **Which host.** The host is resolved once, for both commands (`gitTargetOf`): `zcp run` refuses,
  with exit 2 and before any ssh call, what `zcp git` refuses — a loopback or the container's own
  name, a name that is no hostname or lease alias, a call outside a container, a stage, a
  pool-keyed pair, and a lease alias the lease check refuses (§6.8 *Leases*) — in its own words,
  and a call with no command.
- **The command.** The remote command is `/bin/sh -c '<script>'` whatever the login shell: the
  script changes into the folder and runs the argv, every argument ShellQuoted, so a shell builtin
  is a command too and a shell line goes in as `sh -c '<line>'`. stdin, stdout and stderr are the
  command's, byte for byte, and the exit code is its own (255 is ssh's own failure, 1 a record zcp
  could not read); the script's shell stays to report it, so a command a signal ended is its
  128+n — 137 for one the kernel killed for memory — never ssh's 255; `zcp run` prints nothing to stdout itself and runs before CLI telemetry. stdin
  always reaches the command, as ssh's does, so a heredoc or a pipe feeds it and a caller reading
  its own stdin on — a loop over hosts — gives the call `</dev/null`. A reader of the output that
  goes away (`zcp run <host> … | head -1`) ends the call with 141 and no line of zcp's, as it ends
  any command writing into a pipe.
- **Stopping.** Ctrl-C or SIGTERM stops the ssh client with exit 130, saying the command may still
  be running: a time limit belongs on the command, under the caller's own limit (`zcp run <host>
  timeout 540 …` under a 600 s one). A long-running
  dev server is `zerops_dev_server`'s, never a `zcp run`.
- **No credential.** The call is the plain passthrough in every container, relaying nothing: git
  against the Mate's HQ is `zcp git`'s (§6.8).
- **Its own scope.** An ssh session into a Zerops container is served by the platform's agent, so
  everything it runs lands in the agent's systemd service, `zerops-zembed.service`, whose task limit
  is systemd's default, 15 % of the container's `pids.max`: 300 of 2000, while the service's own
  processes run under the container's 2000 alone. A repository's checks run their stages at once,
  each with thread pools sized to the cores, and pass 300: the kernel refuses their threads and
  processes (EAGAIN), and stages fail at random and pass on a rerun. So the script's shell first
  moves itself into a transient systemd scope of its own, `zcp-run-<pid>.scope`
  (`ops.ownScopeStep`): `sudo -n busctl call` of systemd's `StartTransientUnit` with the shell's
  own pid — the call `systemd-run --scope` makes for itself — `TasksMax=infinity`, since a scope
  gets the default 300 too and the container's 2000 stays the only bound, and
  `CollectMode=inactive-or-failed`, so the unit is gone once its last process ends. The shell itself
  moves, so the command is the process it was: its user, environment, folder, descriptors, session
  and signals (`sudo` resets the environment, so nothing of the command runs under it; only `sudo`
  and `busctl` run as root, reading `/dev/null` and writing nothing). systemd moves the pid when it
  runs the job, after it answered, so the shell waits until its own cgroup names the scope, at most
  a second. Where `sudo -n` needs a password, `sudo`, `busctl` or the system bus is missing, or
  systemd refuses, the command runs where it is, as before, with no word of it. The same step, each
  with its own name, starts every script zcp runs over ssh for the repository's own work: `zcp
  git`'s, plain and relayed (`zcp-git-`; a hook runs the checks), a lease's `prepare`
  (`zcp-prepare-`) and the supervisor zcp starts in a lease with the dev servers under it
  (`zcp-devsup-`; docs/spec-workflows.md §8 DP-13, DP-14). zcp's own reads and writes over ssh, a
  deploy's `zcli push` and the relayed git zcp runs for itself stay where they are: a process or two
  each.

### 6.10 A Mate messages its person's other Mates

A Mate lists its person's other Mates and asks one of them for something in its project, with no
click per message. "Its person's" means the same owner: both Mates' HQ records name the same,
non-null owner in the same organisation. A Mate is never its own peer, and automated Mates are
neither listed nor reachable. zcp lists and sends; HQ decides who may be reached, stores and
delivers each message, and makes the answer. zcp keeps no record of a message. The Chief is one of
its person's Mates: a Mate asks it this way, and it hands project work to its person's other Mates
this way, one message per Mate, several in one turn.

**The tools.** Both register in a Mate's container that can address its HQ (a project key, a
project and HQ's origin), the Chief's included, and neither registers anywhere else
(`internal/tools/mate_messages.go`).

- `zerops_list_mates`, no input, lists the peers: each one's name and project, a plain state and
  its `projectId`. The state is the first of these that holds: "needs an update" (it has not
  advertised `identity.messages`), "asleep" (its link to HQ is closed), "not signed in" (no login
  of it has the owner as its present signer), else "awake". HQ's peers do not mark the person's
  Chief, so zcp names the peer whose project the `chief` of the Mate's state (§2.8) names as "your
  person's Chief"; no state heard marks none.
- `zerops_message_mate` takes `{ to, words, attachmentTitle?, attachmentText? }`. `to` is a
  Mate's name, matched case-insensitively against the peers, or its `projectId`. When it
  names none or several, nothing is sent and the answer lists the candidates. Otherwise it sends
  and answers one JSON document, as the proposal tools do, in the one text block:
  `{ "message": { "id", "chainId", "hop", "to": { "projectId", "projectName", "mateName" },
  "delivery" }, "note" }`. The Mate's client reads `message` to draw the ask and, until an answer
  whose `inReplyTo` is its `id` arrives, the wait for it. `note` tells the agent where the message
  went and when the answer comes back: as a new message after its turn, never by waiting or
  polling. A refusal is an error result in words that say what to do.

**The routes,** under `Authorization: Project <ZCP_API_KEY>`; the key names the sending Mate.

- `GET /api/mate/peers` → `200 { "mates": [{ "projectId", "projectName", "mateName", "awake",
  "receives", "signedIn" }] }`, by Mate name, then project name. A caller with no owner gets
  `{ "mates": [] }`.
- `POST /api/mate/messages` with `{ "to": <projectId>, "words": <1–8000 characters>,
  "attachment"?: { "title": <1–200 characters>, "text": <1–40000 bytes> } }` → `201 { "id",
  "chainId", "hop", "to", "delivery" }`. `delivery` is, first that holds: `on-update` when the
  receiver has not advertised `identity.messages`, `on-wake` when its link is closed, `on-sign-in`
  when no login of it has the owner as its present signer, else `now`. Refusals are `{ code,
  reason }`: `400 invalid/to|words|attachment`, `413 too_large` (a body over 64 KiB),
  `403 forbidden/automated` (the caller is an automated Mate, whose peers list is empty too),
  `404 not_found/mate_not_found` (no Mate there, another person's, automated, or ownerless, alike
  so nothing leaks), `409 conflict/self`, `409 conflict/cannot_receive` (the caller's own latest
  overview does not advertise `identity.messages`, so it could not take the answer),
  `409 conflict/too_many_hops`, and `429 too_many_requests/messages_per_hour`.

**What HQ and the Mate server do with it.** A Mate that takes messages says so in its overview,
`identity.messages = { "protocol": 1 }`, and only such a link gets `message` frames. HQ re-checks
the owner and sends `{ "type": "message", "message": { id, chainId, hop, kind: "ask" | "answer",
inReplyTo?, from, words, attachment?, end?, sentAt } }` when the link is open, and again after each
later link opening for every message not yet taken, oldest first; an asleep receiver is never
started for one. The receiving Mate records the message durably and reports `message-taken`, works
it as the next run of its one conversation (never steered into a run already going), attributed to
the sending Mate and never to the person, spending its own agent login only when that login's
signer is the owner, and reports `message-ended` with `answered`, `failed`, `stopped` or
`declined`, and for an ask the run's last answer (at most 8000 characters). HQ answers
`message-ack`, and the Mate repeats every unacknowledged `message-ended` on each new link. From an
ask's end HQ makes the answer automatically, `kind: "answer"`, to the asker, which wakes it as its
next run; an answer is always delivered. A message is a colleague's request, never the person's
approval. A chain is an ask, its answer and every ask sent from a run a message started: HQ alone
attributes hops, from the open-message window it records between `message-taken` and
`message-ended` (at most 24 hours), so the container cannot forge them. An ask past
`HQ_MATE_MESSAGE_HOPS` (default 4) is refused, and asks are limited per owner to
`HQ_MATE_MESSAGES_PER_HOUR` (default 60), counted across all their Mates; answers count toward
neither.

## 8. Agent sign-in

A person signs an agent CLI in from inside Mate, and the Mate server owns that sign-in. What zcp
shares with it is the flag of §8.1 and the agent configs zcp writes.

### 8.1 The agent sign-in flag

The flags are `ZCP_AGENT_OAUTH_<S>`, `ZCP_AGENT_TOKEN_<S>` and `ZCP_AGENT_AUTH_TYPE_<S>`, `<S>` one
of `CLAUDE_CODE`, `CODEX`, `ANTIGRAVITY`, `GROK` or `CURSOR` (zcp's mapping `ops.AgentOAuthEnvKey`,
`internal/ops/agent_oauth.go`), read from the platform env store.

The OAuth flag is a row on the zcp service, written non-sensitive `true`, because the GUI's flag
read path redacts sensitive entries (`spec-welcome-mode.md §4.2`). Every writer applies one rule:
absent → create, a non-sensitive `true` → nothing, any other value or a sensitive row → replaced.
The writers are the Mate server, through the Zerops API with the project key, and zcp's `agent
mark-oauth` for the VS Code panel. The flag is written only after the agent CLI's own status says
signed in, so a planted or stale credential file never sets it. Sign-out deletes the OAuth flag and
an `oauth` auth-type row, never a token's.

The flag decides what is signed in, for every surface, as in the Zerops GUI and the VS Code panel.
The five-value state is the welcome panel's matrix, `spec-welcome-mode.md §3`, verbatim.

What zcp writes for agents: the `zerops` MCP server in OpenCode's `~/.config/opencode/opencode.json`,
adding `zerops_*` as allowed only where it does not overrule the user's permission rules, and the
project root's `AGENTS.md` in `instructions` (`TestOpenCode_*`); and zcp pre-approves the Zerops
tools in Cursor's project config.

### Invariants

| ID | Invariant |
|---|---|
| MA-1 | The feed's `state` equals the welcome panel's matrix for every combination of flag/credential; credential files are probed for presence only. |
| MA-3 | The OAuth flag is written non-sensitive and a legacy sensitive row is replaced (`migrated:true`); sign-out deletes it and an `oauth` auth-type row, never a token's. mate ZS3, ZS4, ZS5. |

## 10. zcp and HQ

### 10.5 A project's tags

A Zerops project carries only the exact `mate` marker; every Mate metadata fact lives in Mate HQ,
keyed by project id. ZCP filters `project.tags` in create-and-import YAML at the API boundary,
retaining only that marker and reporting that dropped facts must be stored in HQ. Platform tag
transport decoding remains native.

### 10.8 What zcp calls in HQ

**A Mate's side** (the project key in a header, §3.6, §6.6): its own state (the Mate's state (§2.8): its record,
the application HQ holds it in and that application's name, its changes there, its repositories
with each one's origin address, `repos`, which an older HQ omits; `production` and `gitPushes`
are not carried. The closing note handing over a change points to Review without another HQ
read; production setup belongs to the project's card, whose available actions Mate owns); a repository of
its application, made or joined (`POST /api/mate/repos`); its changes in a repository, one open per
branch it names beside its one change opened naming none — an HQ that keeps them so says
`openPerBranch: true` in the Mate's state and lists every open change there — or, from an older HQ,
one open at a time (`POST /api/mate/changes`, `PATCH …/:repo/:n`, `POST …/attachments`); its failing
checks (`GET /api/mate/changes/:repo/:n/checks`) and a check's log (`GET
/api/mate/changes/:repo/:n/checks/:check/log`), shown by `zerops_check_log` as HQ answers, without
zcp's judgement; a tier of the recipe
(`GET /api/mate/recipe/:tier`); the organization's AI key (`POST /api/mate/ai-key`, §5.8); a Chief's
reads of its organisation as its registered person (`POST /api/mate/chief/read`, §3.11); a read
token for a GitHub repository at that call (`GET
/api/mate/git-credential?host=github.com&path=…&access=read`, §5.10 — never one that can write);
git over HTTPS at `/git/<orgId>/<appId>/<repo>.git` (the organisation is the project's Zerops `clientId`; the address without it answers 404) with the Mate's Git credential (§3.3) or its project key (§2.8), which today takes a Mate's push only
on the branch of its own open change, only forward — zcp's guidance already pushes a branch of the
agent's own, which takes the relaxed rule (any branch name; `main` and tags HQ's) on HQ's
side. §10.10 is what zcp does with it.

**A profile's application observation.** `zerops_observe` needs no SSH. It is available
in a Mate's enabled container and in standalone zcp with an explicitly configured HQ
and source project. Every action uses `POST /api/mate/observe` with the profile's
Observe child credential and current own-project native parent proof (§3.3, §3.6).
The fixed model inputs are `action`, `projectId`,
`serviceId`, `processId`, `versionId`, `limit` (1–100, default 100), and optional
`from`/`till` for log reads; no token,
application/person ID, URL, API path or command is accepted.

| Action | Target inputs | Facts |
|---|---|---|
| `environments` (default) | none required | Permission-filtered stage/production catalog and sibling Mate metadata |
| `status` | projectId | Services and actually active versions |
| `deployments` | optional projectId | Visible HQ deployment jobs; requests are separate from platform runtime |
| `processes` / `process` | projectId; processId for detail | Platform processes, with direct ownership-checked detail and nullable native versionId |
| `versions` / `version` | projectId; serviceId for list, versionId for detail | Exact service version facts |
| `logs` | projectId, serviceId; optional from/till | Complete native service log records |
| `build_logs` | projectId, versionId; optional from/till | Exact-version pipeline build/preparation records, never a builder's latest unrelated log |

HQ owns the typed facts and raw log record contract in `spec/protocol.md`. zcp
requires the matching `action`, `appId`, nullable `projectId`, and
`coverage:{source,observedAt,freshness,scope,truncated,logWindow?}` envelope with the action's
bounded array; malformed, oversized or mismatched envelopes fail. It returns JSON
as text with nil `structuredContent`. Authorized log records retain every native
field and unchanged content, including sensitive source data; zcp does not scan,
redact or slice it. zcp never adds either credential to output, errors or header traces, and failed
or invalid responses do not expose raw bodies. A refusal keeps only a code this route answers
(`observation_refused`, `too_many_requests`, `observation_unavailable`, `observation_output_too_large`,
`too_large`, `invalid`, `credential_required`, `escalation_unavailable`, `zerops_refused`,
`zerops_unexpected_answer`, `zerops_unanswered`, `zerops_unavailable`, `unavailable`) and its fixed
reason; any other code reads `observation_refused`. Responses are at most 512 KiB and
lists at most 100 records; resource limits fail explicitly rather than edit records.
An HQ `413 observation_output_too_large` asks for a lower `limit` or narrower
`from`/`till` window; a single record over the output budget cannot be returned.
No redirect forwards either credential. Each observation attempts once, including a
`503`, within 45 seconds; unavailable and denied reads remain visible failures.
HQ validates current native `read_project` for each target and `observe_mate` for
sibling metadata, with object ownership checked before projection. The token cannot
write, release, retry a deployment, approve, or create a review plan. Own-project
native tools retain their existing credentials and behavior.

Log `from`/`till` boundaries are native record IDs or ISO timestamps, each a
nonempty scalar of at most 256 printable ASCII characters, forwarded unchanged.
Other actions reject these inputs. Every page is independently authorized by HQ.
`coverage.logWindow` retains HQ's `rawCount`, `rawLimit`, `rawWindowFull`, and native
boolean `nextPage`; `false` remains false. Complete records retain the IDs used for
continuation. To inspect a failed build, follow its process `versionId` or exact
version-list ID to `version` and `build_logs`; an older active runtime version is a
separate fact and never substitutes for the failed attempt.

After successful `environments` authorization, the result also carries `changes` and
`changesAsLastRead`: the existing own-project known-change projection, refreshed
with that project's own authority when a recorded change or leased branch needs it.
This metadata read neither authorizes observation nor substitutes for its token.
A change merged since the last pass reads merged; if HQ does not answer the metadata
read, its standing is explicitly as last read. The read does not consume the next
notification. Before merge advice, the agent uses current change status or says it
is unknown; absent or last-read changes cannot establish current status
(`TestObserve_Environments_CurrentChanges`). The final text, including metadata,
remains subject to the output bound.

Tests: `TestReadObserveCredential_*`, `TestObserveClient_*`, `TestObserve_*`,
`TestServer_ObserveToolGating`,
`TestServer_ObserveStandalone_ConfiguredProfileStillRequiresCredential`,
`TestAnnotations_ObserveTool`; HQ's strict observation tests own authorization,
projection and native object checks.

### 10.10 zcp inside a Mate

As soon as a dev pair exists, zcp gives it a repository in the application HQ holds the Mate in and
puts the pair's checkout on a `main` that descends from that repository's `main` (a fresh history
cannot merge into a `main` holding code), or, while that `main` is HQ's unwritten root, on its own
history as it is; a pair's work reaches `main` by plain git on a branch of the
agent's own, landed by a merge (*A Mate's git*, below). A pooled part's repository is ensured by the
plan (§2.8b B-9), one repository for all parts of the plan. It proposes the application's recipe — the whole app as the three tiers, composed by
one policy table (`bundle`), every runtime keeping its `buildFromGit` and `zeropsSetup` pair, its
`buildFromGit` the pair's repository in HQ (`https://<HQ>/git/<orgId>/<appId>/<repo>.git`; the stand-up
reads the repository's name from its last segment), no secret's value in any file — to the
application's recipe repository in HQ, `group`, and only what its `main` lacks, a tier directory at
a time: a group's first recipe whole, a tier `main` lacks on any later pass, and nothing once
`main` has every tier — no change opened. A recipe repository HQ made with its application has no
`main` until a recipe lands: the first recipe is then a root commit on the Mate's change, which Core
lands as the first `main`. A proposal is the Mate's own change in `group`, titled
exactly "Mate: the group's import files" with no description (the client knows a proposal by that
title), pushed from a scratch repository of zcp's own on the `zcp` container (`hq.Scratch`: the
credential reaches git through its environment only, as an Authorization header scoped to HQ). Its
branch holds `main`'s tree with the missing files written over it, so it only ever adds, and only
moves forward: a changed composition is a commit on its head, with `main` as a second parent once
`main` has moved, so the same change follows the project — while its branch still holds what zcp last
pushed there (*A change zcp no longer holds*, below); a move of `main` only in a tier the change writes
over (a scale landing on it) takes `main` in with the change's tree unchanged, since both sides changed
that file and HQ would judge the change in conflict, once per such move; a change `main` has overtaken — the tiers
it proposed landed from another Mate's — is brought to `main`'s tree, adds nothing, and Core closes
it. Nothing about the proposal is stored but the tree zcp last pushed on each change: each pass composes it anew and finds the change in the
Mate's state (§2.8), so passes run one at a time on the Mate, across every zcp it
runs (an exclusive lock, `<stateDir>/hq/group-recipe.lock`, taken once a pass knows it has something
to do — a Mate linked to its HQ with a pair whose repository is there — and waited for within the call
and at most two minutes; a pass that does not get it changes nothing and says another pass is
running; the start pass is the server's background work, which `Server.Stop` ends and waits for): two at
once both read `main` and the Mate's state from before either change existed, so the second
proposed onto an empty `main` after Core had landed the first, a root commit on a change of its own
that HQ judged `unrelated` and could never land
(`TestGroupRecipePass_TwoAtOnceOntoUnbornMain_OpensOneChange`,
`TestGroupRecipePass_AnotherPassRunning_ChangesNothing`, `TestGroupRecipePass_NothingToDo_WritesNothing`,
`TestServerStop_StartPassRunning_WaitsForIt`); a push from outside a pass — a proposal pushed
again for Core to judge, a targeted proposal — that races it on the branch ends in a refused push and the next pass's work. It runs once at each zcp start in a Mate (which opens no proposal again that a person closed unmerged — a closed duplicate an earlier merge of the Mate's superseded, composed against a `main` from before that merge landed or holding nothing `main` lacks, is no decline: `TestGroupRecipeAtStart_RacedDuplicateClosed_IsNoDecline`), on every bootstrap close, adopt pass and develop start, right after the repository
reconcile, after a tool call that changes what the recipe reads, by the start pass's rules (*The recipe follows a service
change*, below), and when the agent asks (`zerops_workflow action="group-recipe"`), which answers with the change or that
`main` already carries every tier; Core lands a proposal by itself, adds or edits alike, and a person
merges one whose merge would import, stop or start a service in a stage or production environment
made from a tier (SPEC §3.2c). Core judges a proposal once, on its push, so a Mate's HQ
pass (the repository reconcile's changes' turn) answers what it judged of the Mate's own open
proposal, as the Mate's state names it (`changes[].mergeability`, and `changes[].verdict` — Core's
refusal to land it at its head, an HQ field a newer HQ names): in `conflict` with `main` — a sibling's
tier landed first — the recipe reconcile runs again, so the branch takes `main` in and Core lands it
or closes it as empty (bootstrap, adopt and develop start leave it to the reconcile that follows them); refused
`head_moved` — a push arrived while Core judged — it is pushed once more, its tree unchanged, to be
judged again, once per change (`<stateDir>/hq-recipe-repushed/<appId>-<number>`), so a refusal that
persists never loops; refused for anything else — `recipe_changes_environments`, `files_past_bound`, a git
failure — it waits for a person, who lands or closes it in HQ: zcp never pushes it again for it, and
the pass's report names the change, its address and the refusal (`hq_recipe_judged.go`;
`TestHQPass_RecipeChangeJudged`). The recipe, and the stage and production projects its
tiers create, are named after the application: the slug of its name as HQ gives it
(`MateState.appName`, by the group-slug rule main's client named groups with — lowercase letters,
digits and dashes, starting with a letter, 2 to 30 long), its id while HQ names it nothing. The
recipe's names — the repository `group`, the proposal's title, each tier's path — are
the fork's shared recipe contract's, mirrored in `internal/hq/recipe.go`. `group` is reserved: HQ answers a
repository of that name with the recipe repository whoever asks, so zcp wires no pair to it — a dev
half named `group` is told to rename the service, a recipe naming it to fix the recipe — and the
recipe leaves such a pair out rather than wait on it. zcp's own key is never handed out: no `ZCP_API_KEY` in a
build-integration secret, `GITEA_TOKEN` masked on every value dump, the one `zcli push` of a
self-deploy given the key through its environment, over SSH standard input (§3.6), and never
`zcli login`. `hq_recipe.go`,
`hq_recipe_inputs.go`, `internal/hq/scratch.go`, `internal/hq/recipe.go`;
`TestGroupRecipe_ProposedAsTheMatesChange`, `TestGroupRecipe_ProposedOntoAnUnbornMain`, `TestGroupRecipeAtStart_ReopensNothingAPersonDeclined`, `TestGroupRecipe_ProposesOnlyWhatMainLacks`,
`TestGroupRecipe_FollowsTheProjectOnTheSameChange`, `TestGroupRecipe_MainMovedUnderAnOpenProposal`,
`TestGroupRecipe_AProposalMainOvertookAddsNothing`, `TestGroupRecipe_OpensNothingMainAlreadyHas`,
`TestGroupRecipe_WaitsForAnUnwiredPair`, `TestGroupRecipe_ProposesTheLiveProject`,
`TestHandleGroupRecipe_Table`, `TestGroupRecipe_NamedAfterTheApplication`, `TestRecipeSlug`,
`TestWireHQPair_TheRecipeRepositoryIsNoServicesRepository`, `TestGitEnv_CarriesTheCredentialScopedToHQ`
— against a fake HQ that serves real git with HQ's ref rules. A Mate joining from the recipe has its parts and no live proof: _Add Mate_ imports
the AI Agent tier with the first Mate's hostnames, every Mate of an application reaches all of its
repositories in HQ (a hostname names one repository there), and the repository reconcile puts the
checkout on `main` — a checkout with no commit of its own takes `main`'s tree. The adopt-time
reconcile runs where the metas are complete.

**Published-schema validation before proposal.** Every generated tier and the exact final bytes
of a targeted scaling or service proposal are checked against the cached published Zerops import
schema before opening or pushing the recipe change. A failure names the service, tier file,
instance and schema paths, and correction; no failing tier is proposed. A missing or
uncompilable publication says "validation unavailable", never "invalid recipe". The source URL
and schema digest identify the authority; the embedded catalog floor cannot substitute for it.
The existing ownership check still leaves agent- or person-edited proposals untouched. Nested
scaling, object storage size and unversioned types follow the publication, with no zcp field
list. Placement and closure keep their existing composition rules; `zerops_import` keeps its
API-only validation contract (`schema-integration.md` §4;
`TestGroupRecipeScaling_FlatMinCPU_RefusesBeforeProposal`,
`TestGroupRecipe_FlipShape_ProposesAgainstPublishedSchema`,
`TestGroupRecipe_SchemaUnavailable_ReportsUnavailableWithoutProposal`).

**The recipe follows the project.** The tiers describe the Mate's project as it really is —
each part its dev pool (or dev service) and its stage, every runtime typed, scaled and named from
its own part, a Mate's own dev tooling on the AI Agent tier alone (§2.8a.5) — and the reader takes
the project as it runs: every pair keyed by its dev pool as that pool (its type, setup, scale and
service variables, never its orchestrator's `API_TOKEN`/`ADMIN_TOKEN`) beside its stage as it runs,
never the retired dev service standing in for it, every dev-service pair whose repository's `main`
holds the pool setup of every one of its parts as a pool per stage (§2.8a.5), and a pool keying no
pair left out and said. A pair's zerops.yaml — which names a stage's setup nothing records, what
each build reads and a part's type when its stage does not run — is read from the dev checkout on
the mount (`zerops.yaml`, else `zerops.yml`), else from main of the pair's repository in HQ. Main
is read for every pair whose mount holds no zerops.yaml, and for every dev-service pair that passes
§2.8a.5's conditions on its record and the live project, to tell whether main holds its pool
setups: its head asked by `ls-remote`, only the tip fetched (`Scratch.RemoteHead`,
`Scratch.FetchTip`), its zerops.yaml and `.zcp/dev-servers.json` kept per head under
`.zcp/state/hq/main-setup/` once every file read at that head answered, so a large monorepo is read
once per move of main. Only a main HQ answered proves it holds no zerops.yaml; a main that could
not be read fails the pass, where it stands in for a mount without one as where it decides a
dev-service pair's pools: the pass composes and writes nothing and says it retries on the next. Never from a mount of a pool, whose hostname reaches a random
container.

- **What serves the Mate.** A tier made for a group environment carries the app, never
  the Mate's own infrastructure. What serves only this Mate is first the person's to say: a utility
  or a stage they marked (`mateOnly`, §2.8a.5 *Choice*) stays on the AI Agent tier with a volume
  only it uses, and one they cleared is carried in every tier, whatever the rules below read —
  a runtime wired to its pair's own stage is kept out only by the person's mark. Where they said nothing, zcp decides by its rules and says so once
  (`bundle.mateRuleLine`). Beside its dev tooling (§2.8a.5), what serves the Mate itself is
  read from what the project shows, never from a list of names: a runtime whose own variables name
  the public address of a service of the Mate's own project that no group environment runs, written
  out — `<hostname>-<label>[-<port>].<region>.zerops.app` (`zerops.dev` on devel), the label the
  first of the project's subdomain host — is wired to that Mate's project, which no stage or
  production is, whether it is a utility or a stage of a pair, built from the pair's repository by
  a setup of its own (a pair's dev half is never read so: its dev loop is the Mate's by design, and
  a pair with no stage runs its part on it). The addresses that count are those of a pair's dev
  service or dev pool beside its stages; of the Mate's own infrastructure, a service the recipe
  carries on no tier (zcp, core: a hostname the project runs that is no pair's half, managed
  service, utility or Box); and of a utility already wired so, so a stage naming that utility is
  wired too. An address of anything else — another part's stage, a managed service, a Box, a
  utility not wired so, the Mate's dev tooling among them — is the app's own wiring and wires
  nothing: a monorepo's `webstage` naming `apistage`'s public address stays in Stage and Small
  Production, as do a utility naming a stage and a stage naming the Mate's mail catcher. And a
  volume (local or shared storage) that nothing a group
  environment runs mounts or names — no setup a part builds there, with the setups it extends, no
  carried part's or the project's variable, no utility those environments carry; a dev half's
  setup and variables and a wired stage's are no such thing — while such a runtime runs beside it,
  is that runtime's — or a runtime the person marked. Both stay on the AI Agent tier, Stage and
  Small Production leave them out, and the pass says so. A stage left out so takes no name in the group
  environments, so a runtime they carry whose setup reads it refuses the recipe, naming it
  (docs/spec-workflows.md §8 PS-3a). A runtime
  naming only its own address, another project's or one through the subdomain variables
  (`${zeropsSubdomainHost}` names whichever project reads it) reaches every tier. Named limits: a
  volume beside a part whose zerops.yaml was not read stays everywhere, since
  what that part mounts is unknown; a database, cache or object storage only the Mate's own runtime
  uses still reaches the group tiers, because a runtime may read one through variables the platform
  hands it without a reference, so nothing naming it is no proof nothing uses it; and a secret zcp
  can only generate is no signal, since any app's third-party key reads the same
  (`bundle.mateOwnHosts`, `bundle.wiredUtilities`, `bundle.markMateWiredStages`,
  `bundle.markMateOwnVolumes`, `tools.readRecipeChoices`).
- **zcp's own tiers.** Every tier zcp composes carries its mark, the header's last line,
  `# zcp-recipe: <the Mate's project id> sha256=<digest of the file without that line>`
  (`bundle.StampTier`), announced by the paragraph above it: any edit of any line — a value, a
  comment, a blank line — leaves the file stamped and not intact (`bundle.TierStamp`). A tier on
  main is zcp's own when its mark is this Mate's and intact, or — a tier written before the mark —
  when every commit of main's history that changed the file is Core's squash of one of this Mate's
  recipe proposals: authored `<the Mate's project id>@mate.hq.invalid` (`hq.SquashAuthorEmail`, HQ's
  squash author) with the subject of one of zcp's titles and the change's number
  (`Scratch.History`). Anything else — a person's change landed over it, another Mate's zcp,
  another Mate's mark, a mark the file no longer matches — is the group's alone.
- **Proposed anew.** Once main carries every tier, a tier of zcp's own whose shape differs from
  what this Mate composes now (`bundle.TierShape`: each part by its repository, setup and type, any
  other service by its hostname and type — a pool migration, a part added or removed, a runtime
  retyped; a scale, a variable, a comment or a part's hostname is no shape) is written over main's on
  the Mate's recipe change, the same title, only that tier's `import.yaml`, marked. Core lands it by
  itself unless its merge would import, stop or start a service in a stage or production environment
  made from a tier, which waits for a person with production rights to accept it on the project's
  page, whose card lists it as a change row worded "Changes how … is built". The agent says so in
  one line with what accepting does (Stage at once, Production on its next release) and names no other
  place or button (`recipeLanding`, `recipeHeldForPerson`,
  `TestHQPass_RecipeChangeJudged`); the AI Agent tier proposed
  anew after a pool migration changes no environment. What main lacks still goes first and alone: a
  tier HQ sees for the first time imports nothing, so that change always lands by itself. A person
  who closes such a proposal unmerged is not asked again until a later recipe change of this Mate's
  merges (`recipeDeclined`). The header zcp writes is no shape, and main's file keeps the mark of its
  own text, so a header zcp words otherwise neither proposes a tier on main anew nor makes it the
  group's. A tier zcp owns holds no names for the composer (its names are zcp's own rule, and holding them would keep a
  name an older rule gave), zcp's own targeted edit of it (a part, a managed service, a scale) keeps
  its mark, and the part and managed-service steers stay quiet beside a tier proposed anew.
- **A change zcp no longer holds.** The Mate's recipe change is zcp's only while its
  branch holds what zcp last pushed there: zcp keeps that tree per change
  (`<stateDir>/hq-recipe-pushed/<appId>-<number>.json`), and a branch whose head holds another — a
  commit of the Mate's agent, or of a person in HQ — is theirs. No pass pushes onto it again,
  whether the project changed since or not: the reconcile names the change and says it is theirs on
  every pass while it stays open, and a scaling or service proposal is refused with the change's
  address. While the branch is zcp's, a pass pushes only when what it composes
  differs from that tree, never because the branch differs from the composition. Who authored a
  commit tells nothing — the agent's commits carry zcp's own robot identity — and neither does its
  subject. A change no record names — pushed by a zcp from before the record, or handed over from a
  Mate that is gone — is zcp's while every tier file it changes beyond its merge base with main
  carries an intact zcp mark (any Mate's, `Scratch.MergeBase`), and is recorded from then on; a
  record that cannot be written leaves the change read as someone else's, which pushes nothing over
  anything. Their edit lands by HQ's rule as they leave it, and a tier it edited is no tier of
  zcp's own once on main. Named limit: a change an older zcp pushed into a tier its mark does not
  cover — a targeted edit of the group's tier — reads as someone else's; closed in HQ, a new call
  proposes afresh (`hq_recipe_respect.go`).
- **A tier edit of the Mate's own.** Every recipe call of zcp's proposes only what it
  composes from the Mate's project — a tier main lacks, its own tier anew, one host's scale with the
  tier's floors applied, a part, a managed service — so none can make a tier say what the Mate does
  not run: production leaner than its floors, a service's settings, an address of its own. HQ takes
  a Mate's push in `group` as in any repository of its application: on a branch outside `mate/`
  unless another Mate's open change holds it, and under `mate/` only on the branch of an open change
  of its own. HQ's open route sets the recipe repository apart in nothing either
  (`POST /api/mate/changes`), and zcp's fake HQ keeps both rules.
  `zerops_workflow action="group-recipe" branch=<branch> title=<title>` opens the Mate's change in
  `group` for the agent's own edit: the branch is the agent's — never `main`, a tag, a full ref or
  `mate/`, which HQ refuses (`protected_ref`) — and the title is its own, never one zcp gives its
  proposals (the additive one's, a part's, a scale's), since zcp reads such a change, and the squash
  it lands, as its own. Under the recipe pass lock, a recipe change of the Mate's already open is
  answered — the edit goes on its branch, and once the agent's commit is on it the change is the
  agent's (*A change zcp no longer holds*) — and nothing else is opened; otherwise HQ opens the
  change on the branch (on an HQ that binds its own, on that one, with the push's refspec). The
  answer names the change, its address, the recipe repository's address and how to clone it in the
  agent's own shell (`zcp hq git-credential`), and where to push — after the answer: HQ judges a
  recipe change on each push to its branch, so a branch pushed
  before the change opened waits for one more push. Core lands it by the landing rule whatever its
  title or branch (which tiers a Mate may propose over is
  zcp's rule, not HQ's), and HQ counts a recipe change ready without a description. zcp writes
  nothing on it: a recipe pass and a targeted
  proposal find it as the Mate's other recipe change and leave it as it is, and a tier it lands is
  the group's — its squash carries the agent's title, and an edit of a marked tier breaks the mark —
  so no pass proposes zcp's composition over it again, a project that changed shape included. Every
  recipe call that finds nothing to propose over the tiers on main names this call
  (`hq_recipe_edit.go`; `TestGroupRecipeEdit_AMergedTierCorrectedOnTheMatesOwnChange_ZcpLeavesItAlone`,
  `TestGroupRecipeEdit_TheMatesRecipeChangeOpen_GoesOnIt`, `TestGroupRecipeEdit_Refusals`,
  `TestGroupRecipe_NothingToPropose_NamesTheTierEdit`). Named limits: the change is not in the change
  ledger, so describe-change does not reach it — its title is what the person reviews by; a
  scaling proposal of a host the edited tier names still composes the floors, and is made only when
  the agent asks.
- **The recipe follows a service change.** A second Mate, a stage and a production
  are made from the tiers, so the recipe pass runs again after a tool call that changes what the
  recipe reads, without the agent asking. A call asks for a
  pass once it answers without an error when it is `zerops_import` or `zerops_delete` (the services
  a tier creates), `zerops_env` `set`, `delete` or `ai-key` (what a tier carries, and which services
  a runtime reads), pool-migrate's `retire` (a pair keyed by its pool), or a `zerops_deploy` or
  `zerops_deploy_batch` that reaches a stage (the type the stage runs as). A deploy or a
  `zerops_workflow` call asks for a pass whatever it answers when it moved what zcp's records hold
  of the pairs: a pair added, finished, re-keyed or wired, its repository, its setup or a stage's
  setup. A deploy writes back the setup it resolved before it builds, so a deploy that failed can
  move it. A scale and public access ask for none: they change no tier's shape, so no tier main
  carries is proposed anew for them; a scale has its own proposal (`group-recipe
  scaling=<host>`); the next pass carries both into a tier main lacks. A change made outside zcp's
  tools — a person in the vault, the Zerops GUI — reaches the recipe on the next pass. The server
  tells the follower of every tool call as it starts and, once the handler returned, whether it
  failed; nothing of the pass reaches a tool's answer. The pass runs as the server's background
  work, under the server's own context, which outlives the call's and which `Server.Stop` ends. It
  waits until the calls have been quiet for 30 s, and at most 3 min after a burst's first call, so a
  burst is one pass. A call that lands while a pass runs is one more pass after it, never a second
  beside it. A pass still waiting when the server stops is dropped; zcp's next start is a pass. It
  is the start pass: nobody asked for it, so a proposal a person closed unmerged is not opened
  again, and it takes the Mate's one lock like every other pass. What it says goes to the log once:
  a pass that says what the last one said — a pair the recipe still waits for, a tier still refused
  (`spec-workflows.md` §8 PS-3a) — says nothing. A pass over an unchanged project pushes nothing,
  and a pass over a main that carries the composition opens and pushes nothing. While the Mate's
  recipe change proposes a tier anew, a pass that composes that tier in the same shape keeps the
  body the change holds: that change may wait for a person (`recipe_changes_environments`), every
  push has Core judge it again and moves it in the person's list, and a scale the agent tried or a
  variable set again is no reason a person can act on. A new shape is composed anew. A tier main
  lacks follows every value, since no environment was made from it. Nothing changes a tier on its
  own: the composer sorts every list it reads, so the order the platform answers in never moves a
  tier; a scale is the configured autoscaling bounds, which move only when someone scales; a secret
  is written as a generator or left out, never its value; zcp's own credentials and a pool's or a Box's tokens
  are never carried. Named limits: the dev checkout's zerops.yaml is read as the pass finds it,
  uncommitted edits included; a pass a server stop dropped waits for the next start
  (`hq_recipe_follow.go`, `hq_recipe_own.go`, `server.followRecipe`;
  `TestRecipeFollower_AfterACall_FollowsWhatTheRecipeReads`, `TestRecipeFollower_ABurst_IsOnePass`,
  `TestRecipeFollower_ACallDuringAPass_IsOneMorePass`, `TestRecipeFollower_ServerStopped_RunsNoPass`,
  `TestRecipeFollower_AnOutcome_SaidOnce`, `TestNewRecipeFollower_OutsideAMate_FollowsNothing`,
  `TestGroupRecipeFollow_ReopensNothingAPersonDeclined`, `TestGroupRecipeFollow_TheMatesOneChange`,
  `TestGroupRecipeFollow_MainCarriesTheRecipe_OpensAndPushesNothing`,
  `TestGroupRecipe_ATierProposedAnew_IsRewrittenOnlyForItsShape`,
  `TestBuildGroupRecipe_AnyInputOrder_SameBytes`,
  `TestFollowRecipe_ToolCall_ToldAsItStartsAndOnceItAnswered`, `TestNew_RecipeFollower_OnlyInAMate`).
- **Required until it composes.** The group recipe is what _Add Mate_, the group's stage and its
  production are built from, so a Mate's setup is not done while a tier main lacks cannot be
  proposed — a part whose stage setup nothing records and no zerops.yaml tells
  (`bundle.GroupRecipeBlockers`). The pass makes it a required step naming, per part, the stage,
  why, and the way out: `zerops_workflow action="set-default-setup" targetService=<stage>
  setup=<dev setup> stageSetup=<…>` with the setups its zerops.yaml declares to choose from, or,
  with none read, the code with its zerops.yaml in the dev checkout (on main of its repository in HQ
  for a pool's part) or one deploy of the stage, which records its setup. It rides the bootstrap
  response (`required`, and its message), a develop start's result, the agent's `group-recipe`
  answer, and the head of status (`→ REQUIRED:`, from zcp's record `.zcp/state/hq/group-recipe.json`,
  no HQ call); every recipe pass tries again — each zcp start, bootstrap close, adopt pass,
  develop start and service change — and the step is gone on the pass that proposes the tiers or finds main carrying
  them. A recipe the composer
  refuses because a tier reads a service it does not carry (`spec-workflows.md` §8 PS-3a) is a
  required step too, whatever main carries: nothing is proposed while the refusal stands, so the
  group's recipe goes stale. Its step is the composer's refusal word for word — per tier the
  runtime, the setup, the variable and the service, and the way out: carry the service in that
  tier, or stop the setup reading it — then the call that tries again
  (`zerops_workflow action="group-recipe"`, or the next develop start). Every step is the verdict
  of the last pass that decided one, never a mark of its own: a pass that composes against main
  writes the record afresh — a recipe that composes, or that the composer refuses for anything
  else, drops the closure step — and a pass that finds no pair left to compose from (none
  bootstrapped, none wired) clears it, since no pass could check a step again; a pass stopped
  before it composes (HQ not answering, a pair still waiting for its repository) leaves the record
  as the last deciding pass wrote it (`TestGroupRecipe_TierClosure_RefusalIsARequiredStep`,
  `TestGroupRecipe_TierClosure_StepGoneOnceItComposes`,
  `TestGroupRecipe_NonClosureComposeError_NoClosureStep`,
  `TestGroupRecipe_TierClosure_NothingLeftToCompose_StepGone`).
- **Named limits.** A tier proposed anew names each part after itself, so a part an older rule named
  after its repository is renamed in it; on a stage or production made from the
  old tier that merge would change the environment, so it waits for a person, who weighs it. Core
  lands a tier proposed anew by itself only when its merge changes no environment made from a tier
  (`land_recipe`), so one that would import, stop or
  start a service there waits for a person even when it touches only zcp's own files
  (`recipe_changes_environments`). Only the Mate whose mark a tier carries proposes it anew: a tier a
  Mate wrote stays as it is when that Mate is gone, until a person changes it.

`hq_recipe_own.go`, `hq_recipe_required.go`, `hq_recipe_main_yaml.go`, `hq_recipe_respect.go`,
`hq_recipe_follow.go`, `internal/ops/bundle/group_shape.go`, `internal/ops/bundle/group_mate_own.go`;
zcp `TestGroupRecipeInputs_PoolDevHalf_CarriedAsThePartsDevPool`,
`TestGroupRecipeInputs_PoolDevHalf_EveryPoolKeyingAPairIsCarried`,
`TestGroupRecipeInputs_SplitPoolPairs_EveryPartItsPoolAndStage`, `TestGroupRecipeInputs_ZeropsYAMLFromHQ`,
`TestTierStamp_NamesTheMateAndHoldsOnlyWhileUntouched`, `TestTierShape_ReadsWhatTheTierCreates`,
`TestScratch_History`, `TestGroupRecipe_ItsOwnTiersFromBeforeAPoolMigration_ProposedAnew`,
`TestGroupRecipe_ATierAPersonOrAnotherMateWrote_IsNeverProposedOver`,
`TestGroupRecipe_ItsOwnTiersWithTheSameShape_StayAsTheyAre`,
`TestGroupRecipe_ItsOwnTiersUnderAnOlderHeader_StayItsOwnAndAsTheyAre`,
`TestGroupRecipeBlockers_NameWhatIsMissingAndTheStageItIsFor`,
`TestGroupRecipe_TheMateWebShape_RequiredUntilItComposes`, `TestRecipeRequiredStep_NamesTheWayOut`,
`TestRenderStatus_RequiredStepsHeadTheStatus`, `TestBuildGroupRecipe_MateOwnServices_OnlyInTheAIAgentTier`,
`TestGroupRecipe_TheMatesOwnServices_StayOnTheAIAgentTier`,
`TestGroupRecipe_AChangeSomeoneElseEdited_IsNeverPushedOver`,
`TestGroupRecipeScaling_AChangeSomeoneElseEdited_IsNeverPushedOver`,
`TestGroupRecipe_PushesOnlyWhenItsCompositionChanged`, `TestScratch_MergeBase`.

**What a tier carries.** A tier is the Mate's project as it runs, written for the
person who reads and edits it in the recipe repository: two-space YAML, each service opening on its
`hostname` and `type`, a header naming the tier, the Mate it was written from and that it is the
group's to edit. The project block carries the recipe's name (the AI Agent tier keeps the Mate's
own), the live `corePackage`, and the project's user-set variables in its `vault:` — and each
runtime its own in its `vault:`; a secret-shaped name and a generated secret written
`{value, sensitive: true}`; the platform's own and the control plane's (zcp's key and agents, the git token,
the launch token) are never written. The composer decides each variable
unattended and fails closed: a value of `${name}` references alone is wiring, kept as written, and
any other value is kept only when no signal marks it secret — the platform's flag, a masked read, a
credential's name (a PIN, a seed, a passphrase and a webhook's address included, since no shape
tells those from a word), a secret's shape — and it has a narrow config shape (a number, size or
version, a phrase of words, an email or a mailbox, a URL or path with no credential or fragment and
a query of plain settings, a glob, a cron line, an image, a list of these, flags built from flag
words that name no credential), or when its name is public by design (`NEXT_PUBLIC_*`,
`*_PUBLISHABLE_KEY`) with no SECRET, PASSWORD or PRIVATE beside it, the value is no private or
vendor key, and the platform neither flags nor masks it. Everything else stays out of the file. A
secret the app makes and reads itself — a name ending in SECRET (not a client's or a webhook's, and
in no vendor's format), SALT, PEPPER or KEY_BASE, or `APP_KEY`/`APP_KEYS` — and a service's own
sign-in to its own UI — one basic-auth pair of a plain-word user under a name whose last word is AUTH
with UI before it, mailpit's `MP_UI_AUTH`, whose UI shows every mail the app sends (sign-up links,
password resets) on a public subdomain and, left out, would answer anyone — is written as
`<@generateRandomString(<N>)>`, N its live length (16 to 1024; 32 when masked), a `user:password`
keeping only a plain-word user: any random value serves it, and each environment makes its own
(`bundle.recipeOwnUISignIn`, `TestBuildGroupRecipe_AUIsOwnSignIn_StaysGenerated`); a pair under
any other name may be a third party's, and is set by hand.
Every other secret a person set by hand — a vendor's key, an OAuth client's ID or secret, a
password (a person signs in with it or another service checks it: `SMTP_PASS` is the mail
provider's, and no name tells the one from the other), an opaque value — no random value serves,
and a made-up one fails where it is used: a Google OAuth client's ID and secret written as random
characters answer Google's sign-in with `invalid_client`. So the tier leaves it out of the vault it was in —
the environment holds no such variable until a person sets it, and `zerops_env action="request"`
asks for it rather than reading it as already set — and the header names it right after what the
tier is: `Set before this environment can work: GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET
in the project; OPENAI_API_KEY in runtime "app". A person set each by hand in the Mate "…"; a tier
carries no such value and makes none up, so each is left out here.` A tier that leaves nothing out
says nothing of it. An empty secret stays empty, written sensitive: there was nothing to hide or to
make up. A variable left out names no service, so it adds no read and removes none that the closure
check (PS-3a) sees (`bundle.unsetParagraph`, `TestBuildGroupRecipe_HandSetValue_LeftOutAndNamed`,
`TestBuildGroupRecipe_NothingHandSet_HeaderSaysNothing`). The flag alone is not the rule: older
secrets read back not sensitive, and a project variable's flag never persisted. A recipe's order
is designed when it is written, from what each runtime's build reads: priority is dependency order,
since the platform creates each priority group only after the one before it is created and deployed,
and the broker deploys an environment's runtimes — and a Mate's stand-up its halves — in the same
order: the managed services at 10, a runtime no build reads at 1, and a runtime another's build reads
one above the longest chain of runtimes whose builds read it — a read being a `${host_key}` in any
setup's build variables of its zerops.yaml, directly, through a runtime variable the build lifts
(`${RUNTIME_X}`, from the setup's run variables or its own service variables) or through project
variables (`bundle.BuildReadValues`, `bundle.ReferencedHosts`). What a runtime reads only once it runs
orders nothing, so services with no build-time read between them share a priority — but for another
service's public address, which each tier orders on top (below); runtimes whose builds read each
other share a rank. The import format has no field for the reads themselves, so
the tier carries only the priorities they give, and its header says what they mean; the stand-up reads them
again from the same zerops.yaml. A managed service keeps its live scale (Small Production's
production profile replaces a profile-bearing one's), an object storage its size and its policy when
the platform names it (a custom policy's document can hold a secret, so it stays out, said), a type
with no HA variant stays single-node there; a standalone runtime built from a public repository — no
pair records it, no dev/stage sibling runs beside it (mailpit) — is written as it runs on every
tier, its build URL — a runtime's too — bare of any credential and absent when the URL carried one;
a finished pair the repository pass will still wire, or a dev/stage pair zcp has not adopted, holds
the recipe back, while a pair no pass will wire (its own remote, an unfinished bootstrap) is left
out, said; the group environments' values name their own runtimes (`http://appstage:9000` →
`http://app:9000`). A public address of a service of the Mate's own project written out in a
value (`https://appstage-3901-5195.prg1.zerops.app`, the label the first of the project's
subdomain host) names that Mate's service from every project a tier makes, and renamed with its
part a host that exists nowhere. So every tier — the AI Agent tier too, since a project made
from it gets a label of its own — writes it as the address the service has in the environment the
tier makes: `https://<host>-${zeropsSubdomainHost}-<port>.` and the domain as written, `<host>` the
tier's name for the service after the rename (appstage → app), no `-<port>` where the address
names none (port 80), and a path or query after it kept. `${zeropsSubdomainHost}` is the project's
label, given to its services and variables from the project's creation, before any service is
built: an address written with it resolved on the first boot in a service variable and a project
variable alike, on a project with no subdomain enabled
(`https://app-${zeropsSubdomainHost}-8080.prg1.zerops.app` reads `https://app-396e-8080.prg1.zerops.app`;
the label is not always a number). So the address needs no service created before its reader and is
written the same way in a project variable. Never as `${zeropsSubdomain}` or
`${<host>_zeropsSubdomain}`: `zeropsSubdomain` is the whole `https://` address with no slash after
it, one line for each HTTP port the service serves, and no variable names a single port
(`TestE2E_SubdomainLifecycle`, `TestE2E_Import_Box_FinishesSetup`), so a reference written for a
setup of one HTTP port becomes two lines once the setup gains a second. A tier writes the address so only where its service builds a setup
whose HTTP ports — any number of them — include the one the address names, and the value writes it
`https://` with no port after it. Anything else — a setup declaring another HTTP port or none, a
service whose setup the recipe does not read (a utility, a managed service, a Box, a dev pool), a
bare host or another scheme — is kept as written and the pass says so, naming the variable, the
address, the tiers and why, never guessed. A reference a person wrote — `${zeropsSubdomain}` of the
value's own service, `${<host>_zeropsSubdomain}` of another the tier carries — to a service whose
setup declares several HTTP ports is kept as written and said the same way, naming the form to write
for the one meant. A `${…}` in an import's `vault:` under `#zeropsPreprocessor=on` is stored as
written, at project and service level, sensitive or not (a `<@…>` still expands); a service's
zeropsSubdomain exists from its first build, subdomain access on or not; a service's own
`${zeropsSubdomain}` resolves on its first boot; and another service's `${A_zeropsSubdomain}` — in a
service variable, a setup's variables or a project variable, which resolves per service — resolves
in B only when A was deployed before B's container started: else B reads the literal text and keeps
it until it restarts. The order services are written in decides nothing; their priorities do — A at
9, its reader at 1, resolved on the first boot. So every tier creates
the service a runtime reads through a reference a person wrote before the runtime: each runtime of a
tier that another reads through `${<host>_zeropsSubdomain}` — in the runtime's own variables, or in
the build or run variables of the setup the tier builds for it — gets a higher priority than every
runtime reading it, on top of the ranks the build reads give, a runtime lifted lifting what its build
reads above it in turn; a managed service or a Box is created first anyway. The address the tier
writes adds no such read. Two runtimes that read each other, directly or through their builds, cannot both come first:
those reads are ordered and kept whatever comes of them, said when no priority orders them. A
project variable is read by every service, so no priority puts the service it names before all of
them: a person's `${<host>_zeropsSubdomain}` there is said (`bundle.orderSubdomainReads`). An
address of a service the tier does not carry is kept as it is:
an address written out reads nothing (docs/spec-workflows.md §8 PS-3) and a reference the rewrite
writes names only what the tier carries, so the rewrite adds no read the closure check (PS-3a) or
the Box rule would see.
The address is as public as its subdomain, so its label — `1df2` as often as `3901` — never makes
a value a secret the tier generates; a value the platform flags sensitive or reads back masked is
generated as before, never rewritten or said (`bundle.tierAddresses`, `bundle.mateAddresses`;
`TestBuildGroupRecipe_FlipServer_NamesTheEnvironmentsOwnStage`,
`TestBuildGroupRecipe_MateAddress_WrittenAsTheEnvironmentsReference`,
`TestBuildGroupRecipe_MateAddress_SensitiveNeverRewrittenOrSaid`,
`TestBuildGroupRecipe_MateAddress_RewriteAndReadsAgree`,
`TestBuildGroupRecipe_SubdomainReference_TheServiceItNamesComesFirst`,
`TestBuildGroupRecipe_SubdomainReference_ACycleKeepsOneSideAsWritten`,
`TestBuildGroupRecipe_SubdomainReference_InAProjectVariable_Said`,
`TestGroupRecipe_AnAddressOfTheMatesProject_IsTheEnvironmentsReference`). A string a YAML 1.1
reader would reinterpret (`yes`, `on`, `1:30`) is quoted.
A read that decides what a tier carries fails the pass rather than propose a tier with a gap `main`
would keep (`TestGroupPriorities_*`, `TestBuildGroupRecipe_PriorityFollowsBuildReads`, `TestBuildReads_OnlyWhatTheBuildReads`, `TestRecipeSecret_Rule`, `TestBuildGroupRecipe_FailsClosed`,
`TestBuildGroupRecipe_MedusaGolden`, `TestComposeGroupRecipeInputs_ReadsTheLiveProject`,
`TestComposeGroupRecipeInputs_WaitsOnlyForWhatALaterPassBrings`).

**Mate's preview pane frames a Mate's dev and stage; production keeps the app's framing.** The
pane draws a service's page in a frame on
Mate's origins — `https://mate.zerops.io`, which the desktop shell loads too, and the container's
own `/mate/` on its subdomain, the zcp service's `zeropsSubdomain` (`runtime.Info.SubdomainOrigin`,
`*.zerops.dev` on devel) — so a page that sends `X-Frame-Options` (no value of it names another
origin) or a `frame-ancestors` leaving them out reads "refused to connect" there. The stage builds the setup production builds,
so an allowance in that setup would reach production. A Mate's AGENTS.md (`agents_preview.md`,
behind the gate, §2.0) says, in a few lines: the pane frames dev and stage from exactly Mate's
origins, rendered from the container — the hosted origin and the container's own subdomain origin,
the hosted one alone where the service has no subdomain; where the app allowed only itself, their
`frame-ancestors` is `'self'` plus those origins, never a wildcard origin; production keeps the
app's strict framing; and the agent reads the playbook `zerops://playbooks/preview-pane` when a
page there reads "refused to connect" or before changing an app's framing headers or the stage's
setup for it. The playbook (`internal/knowledge/playbooks/preview-pane.md`, static, so it names
"Mate's origins, as AGENTS.md names them") says the rest: dev and stage send no `X-Frame-Options`
and widen the app's own `frame-ancestors` by exactly Mate's origins, never by a wildcard such as
`https://*.zerops.app`, which every app on the shared domain matches; a page that forbids framing on purpose (`X-Frame-Options: DENY`, `frame-ancestors 'none'`, or code
or a comment saying it is never framed) stays as it is and the agent tells the person the pane cannot
show it, opening it up only when they ask; the dev setup may carry the allowance where the stage
builds another; the stage deploys **Mate's preview variant** of its setup — a setup that `extends`
it and changes only the framing (a static service's `run.routing.headers`, else a
`run.envVariables` value the app's header code reads) — and the setup it extends stays as the app
wrote it. A setup lets the
pane frame the app when its own `run` holds a `frame-ancestors` naming `https://mate.zerops.io`, or
a bare source list naming Mate's origin set (that origin beside a Mate container's: the container's
exact `https://` origin on a Zerops subdomain, or the `https://*.zerops.app` wildcard setups
name); a setting naming Mate's address alone is the app's own
(`bundle.WithoutMateFraming`). Beside Mate's hosted origin every Zerops subdomain origin leaves
production's framing: a Mate's project is never production's. The AI Agent tier keeps the variant
on the stage half; every group environment builds the setup it extends, named after it
(`groupEnvironmentSetup`). When the zerops.yaml read for a pair parses but does not declare its
stage's recorded setup, every tier carrying that stage is withheld; the tiers on main stay as they are and
the pass composes again once that file declares the setup. For a pool-keyed pair the file is main's,
so a variant on a change's branch arrives with the merge. A variant of a page that forbids framing frames nothing and only points
the app's addresses and cookies at the Mate's stage, so a setup that extends one declared setup is
the variant also when its own values name an address on a Zerops subdomain — written out or with
the project's subdomain variables among its labels, never a bare `${zeropsSubdomain}` — which no
group environment has, while one that carries neither — production over a shared base — is the stage's
own (`TestBuildGroupRecipe_PoolMonorepoPreviewVariants_StageAndProductionBuildTheBases`). A framed stage setup that extends no other is built framed, and the pass
says so (MB-40).

**A Mate's git.** A Mate's code reaches its
repository by plain git, as on a laptop: the agent brings `main` up to date, cuts a branch of its
own, commits and pushes it, and `main` moves only by a merge — a person's in HQ, or GitHub's for a
repository kept there (§5.10). zcp never commits, opens a change or pushes for the agent: a stage
deploy deploys and delivers nothing, and no deploy response carries a `pullRequest`. Only a
dev/stage pair can be a Mate's: the dev half is the checkout, the stage half the verified basis a
production is promoted from. A part a Mate plans is born pooled (§2.8b B-9): zcp's classic route
refuses a plan that gives a Mate a runtime other than a part's dev pool beside its one stage
(`<part>pool`, `<part>stage`, `bootstrapMode` standard), naming that plan to re-submit — and says the
rule before any plan: a Mate's classic discover step states that each part is a dev pool beside its
stage, and both it and the refusal say that a dev service with its stage the project already has
keeps working untouched (`hq_pair.go`).

**A Mate is HQ's by what it is, not by whether HQ answers.** Being a Mate
(`runtime.Info.MateEnabled`) is what makes HQ the authority; the project key is only how zcp reaches
it — and the converse holds: outside a Mate the key gives no pair a Mate's HQ repository and composes
no group recipe — that container's repositories take the plain-project door (§6.7)
(`TestReconcileHQRepositories_OutsideAMate_GivesNoPairANewRepository`). A Mate whose HQ does not
answer yet, or refuses its key, plans the same pairs, refuses
launch-production and hands its release to the person, as one HQ answers does — it never falls back
to classic simple plans, a production project of its own or a pushed tag. Whatever needs HQ to
answer says it cannot reach HQ and why (`hq.ErrNoIdentity`), and launch-production's next step
says whether the work reached `main` is HQ's to say. A pair whose HQ repository is on record keeps
its release and its closing hand-off HQ's outside a Mate too. A container that is not a Mate keeps
zcp's own route whatever files it holds. And in a Mate the repository, its credential and what
builds from it are HQ's: `zerops_workflow` refuses `git-push-setup` and `build-integration` before
touching anything (`hqRepositoryActions`) and says what zcp does instead; zcp's own wiring confirms
through `confirmGitPushSetupContainer`, and a remote of the user's own stays theirs
(`hqPairNeedsRepository`). `TestHandleLaunchProduction_AMateRefusesBeforeAnyStep`,
`TestHandleRelease_HQHandsOffBeforeLegacyPreflight`, `TestHandleRelease_AKeyOutsideAMateKeepsTheSourceRelease`,
`TestSessionAnnotations_HandoffOnlyWithAnOpenChange`, `TestMateRepositoryActions_RefuseAndPointAtHQ`,
`TestMateRepositoryActions_ZcpStillWiresThePair`.

- **The repository** is `<appId>/<name>` in the application HQ holds the Mate in, the name the dev
  hostname (or the one a recipe names). zcp asks for it — `POST /api/mate/repos {name}`, idempotent,
  `main` born with one commit of the empty tree — and wires the pair: its origin points at
  `https://<HQ>/git/<orgId>/<appId>/<repo>.git` with no credential in the dev service (*The credential* below), a shallow checkout
  completed (a dev service that deploys `./` from a recipe holds the build's depth-1 clone, and
  HQ's receive-pack refuses a shallow push, so every repository in HQ stays complete: the
  history is fetched with `--unshallow` from `zerops-original-origin`, the origin the sync kept,
  never from HQ, which holds none of it; a failure is refused by name with that fetch as its
  remedy, the pair not wired —
  `TestReconcileHQRepositories_AShallowCloneIsCompletedFromWhereItCame`), the checkout put on a `main` that
  descends from HQ's (`ops.BuildBaseBranchCommand`: a pair fresh from `zcp init` takes `main`'s
  tree; a pair with history of its own against HQ's unwritten root — `main` one parentless commit
  of the empty tree, as HQ makes it — keeps that history as its `main` exactly as it is, no commit
  made and the root never joined in, and the change the agent pushes from it lands as `main`
  (mate-hq: a change of unrelated history merged onto the unwritten root becomes `main`, with no
  merge commit); anything else is refused
  with its remedy — two unrelated histories of code are never joined, the pair's own is kept on a
  branch named after its commit, which the refusal carries (`ZCP_BASE_NOT_SEED <commit>`, read in
  the service) and the remedy names with its start point, `zcp git <host> branch -f
  own-history-<commit> <commit>`, so the remedy runs again as it is (§6.8)), the pair's record `ServiceMeta.HQ {appId, repo}`, and `origin` where the code is kept (§5.10). There is no Mate
  branch. A remote that is not this HQ's is the user's own and left alone. One repository per pair
  is enforced by that record; a pair the stand-up adopted is wired to the recipe's name, and wired
  again to it.
  Decision: A Mate's first checkout of an HQ repository fetches commits and trees with full
  ancestry and downloads file contents only as they are needed. The wiring's initial fetch uses
  `--filter=blob:none`; checkout and later historical reads use Git's promisor remote through the
  existing HQ credential helper. Recipe clones are still unshallowed, from the origin they came
  from (above). A lease checkout of an HQ repository fetches the same way, `blob:none` with full ancestry,
  and keeps a credential hint for HQ's host that tells a plain git needing old contents to run it
  as `zcp git <alias> …`. A pool's warm clone of an HQ repository fetches the same way (`spec-workflows.md` §8 DP-32);
  missing-`.git` reconstruction keeps its existing fetch policy until separately qualified (`TestBuildBaseBranchCommand_FirstFetch_DefersBlobsWithFullAncestry`).
- **One repository per monorepo.** A monorepo's every part gets its own dev pool and its own stage, nothing sharing a
  container, all on ONE repository in HQ. In a Mate the plan creates each part's pool and stage on
  that one repository (§2.8b B-9); a dev service with a stage per part made earlier keeps working,
  and pool-migrate still moves it onto pools when a person wants; a dev service per part is never
  the end state (`TestPlaybookMonorepo_ContentPins_PartRules`).
  zcp refuses the split itself, by root commit — two checkouts sharing a root commit whose tree is
  not the empty tree are one repository (`ops.ReadRepositoryRoots`, one read-only SSH round trip,
  `GIT_OPTIONAL_LOCKS=0`); a root of the empty tree is no evidence, since HQ seeds every
  repository's `main` with one and `zcp init` makes one in every fresh checkout, the same commit in
  checkouts initialised in the same second:
  - The repository pass, before it asks HQ for a NEW repository for a pair (a pair not wired yet;
    a wired one is wired again to its record's), compares the pair's dev checkout with the checkouts
    of the pairs already wired — read when the pair's turn comes, so a pair this pass wired counts —
    and on a shared root asks HQ for nothing: the pair stays unwired, and its report line names both
    dev services, the wired pair's repository and the way out — one dev service with a stage per
    part (the wired pair takes the other's stages, its work goes onto a branch of that repository),
    then `zerops_workflow workflow="pool-migrate" service=<the wired dev>`. The attempt is
    recorded and backs off as any unwired one, and the group recipe waits for the pair as for any
    pair the pass will still wire. A checkout that cannot be read, or holds no root carrying code,
    is no evidence (the pair is wired as before), and a pair keyed by its dev pool is never compared
    — a pool holds no checkout of its own (`hq_one_repository.go`;
    `TestReconcileHQRepositories_ADevHoldingAWiredPairsRepository_GetsNoRepositoryOfItsOwn`).
  - A Mate's explicit bootstrap plan (classic or adopt) pairing two existing dev services whose
    checkouts share such a root is refused before the parts read and before anything is recorded,
    with the plan made from it: the first of them keeps every stage of the pairs holding its
    repository, the others leave the plan, and pool-migrate is named for once bootstrap closes. A
    dev service the plan creates holds no checkout yet and is not read
    (`TestBootstrapPlan_InAMate_DevServicesHoldingOneRepository_PlannedAsOnePair`).
  - All parts of a Mate's plan share the repository the plan names (§2.8b B-9); parts of different
    repositories are planned in separate bootstraps.
- **The agent's work** goes by plain git from the dev half's checkout, guided by AGENTS.md's git
  host paragraph and the hq-git playbook it points at: `git checkout main && git fetch origin && git merge --ff-only origin/main` (a
  `main` that will not fast-forward: `git branch <name>`, then `git reset --keep origin/main`), `git
  checkout -b <branch>`, commit, `git push origin <branch>`; never a push of `main`, never a force.
  HQ takes a Mate's push on any branch but `main`, a tag, or a branch another Mate's open change
  holds; no force, no deletion. The agent opens its change from its branch, pushed before or after:
  `zerops_workflow action="open-change" branch=<branch> title=<…>` — `POST /api/mate/changes`
  `{repo, title, branch}` binds the change to the branch; HQ refuses with `{code, reason}` —
  `409 conflict/branch_taken`, `409 conflict/protected_ref` (`main`, a full ref, `mate/`),
  `400 invalid/invalid_ref`, and, past five open changes of the Mate in the repository,
  `409 conflict/too_many_open` — and zcp reads the reason (too many: it names the open ones from the
  Mate's state, offers folding the Mate's own first — *What one change holds*, below — labelling a handed
  one "handed to you", and asks the person to merge or close one; folding comes first where merging is limited too
  (`TestTooManyOpen_OffersFoldingFirstWhetherOrNotMergingIsLimited`,
  `TestTooManyOpen_LabelsAHandedChange`)). A change is a branch: an open change is
  (application, repository, branch), never a pair's. zcp keeps each change it knows of in its change
  ledger, one file per change (`<stateDir>/hq/changes/<appId>/<repo>/<number>.json`, 0600, a write
  lock per change; `workflow.HQChange`), so the zcp of every conversation and every subagent of one
  writes different files, and nothing is a session's "current" change. A change file that does not
  decode (a write cut short) is listed by no reader; the next write of that change moves it aside as
  `<number>.json.corrupt` and writes the change afresh, so it never fails a write for good
  (`TestHQChangeLedger_UndecodableFileIsMovedAsideOnTheNextWrite`). A call names its change: by
  `branch`, or by `service=` a lease alias — its record's repository on this Mate's HQ
  (`hq.RepoOfURL`) and its branch, never `main` — or a pair, which names the repository; with
  neither, the one repository of the Mate's wired pairs and HQ leases, pairs that share one counting
  once (`resolveChangeTarget`, `TestResolveChange_LeaseAliasNamesRepoAndBranch`,
  `TestResolveChange_PairsSharingARepositoryAreOne`). The `service` an open-change or
  describe-change answer names is the pair or lease alias the call named; else the repository's
  only pair; else the part whose dev pool leases the change's branch — never the repository's first
  pair: a branch leased in several parts names none and lists their pools in `parts`, and a branch
  no lease holds names none, since zcp records no part's directory to judge a change's paths by
  (`changeService`, `TestChangeService_NamesOnePartOrNone`,
  `TestChangeService_MonorepoNamesThePartOfItsBranch`). HQ answers the change already open on the
  branch named, `created: false` — it stays, and its work goes on by a push and a describe — and a
  second branch opens a change of its own (`TestOpenChange_SecondBranchOpensItsOwnChange`,
  `TestOpenChange_SameBranchAnswered`, `TestOpenChange_FromLeaseAlias`,
  `TestOpenChange_TooManyOpen_NamesThem`,
  `TestOpenChange_PairsSharingARepositoryOpenAChangePerBranch`). An older HQ — no `openPerBranch` —
  answers its one open change whatever branch is named: zcp keeps that change on its own branch,
  records nothing for the branch asked for, and says the work goes onto the open one, how to take its
  branch and bring the work onto it (work cut from `main` is no fast-forward of it)
  (`TestOpenChange_OldHQAnswersTheOpenOne_RecordsNothingForTheAskedBranch`,
  `TestOpenChange_AnOpenChangeTakesTheWorkOntoItsBranch`). An HQ from before changes were bound to
  a named branch ignores `branch` and opens the change on `mate/<mateId>/<n>`, naming none: zcp
  names and keeps that branch and says to push the work there
  (`TestOpenChange_AnHQBeforeNamedBranchesNamesTheBranchItBound`). The Mate's state open-change
  reads first also says what became of the changes the ledger keeps in the repository: its answer
  forgets each one merged, closed or no longer listed and says it once (`landed`), so a describe
  naming no branch never refuses on a change merged since the last pass
  (`TestOpenChange_ForgetsTheChangesTheStateItReadNamesSettled`). An open HQ did not answer is
  checked before it is answered (check-before-mutate). HQ's own refusal, any 4xx, and a try that surely never reached
  HQ — no connection, refused, HQ's own 503 — opened nothing and say so. Any other failure — the
  answer timed out or the connection dropped once HQ had the request, a balancer's 502 or 504, a
  500 — answers from the Mate's state (§2.8): a change open for the branch there is the answer,
  kept in the ledger and answered as an existing open, the answer first saying HQ's never arrived.
  A state older than a revision on the unanswered write cannot identify its change. With none
  established there, whether a change was opened is unknown and the
  answer says so — never "Nothing was opened" — with the call again, which HQ answers by the open
  change on the branch, never a second one (`openUnanswered`,
  `TestOpenChange_AnswerLost_AnswersTheChangeItOpened`,
  `TestOpenChange_AnswerAndStateLost_SaysUnknownNeverNothing`). A describe needs no such read: it
  puts the same words again, and HQ has no read of a change's words (*A describe waits out an HQ
  that is away*, below). On such an older HQ, while the
  change is open, develop's start says the work goes on its branch rather than on one cut from
  `main`, and how to take it. The pairs' records an earlier zcp wrote (`ServiceMeta.HQ {change,
  branch, changeDescription}`) move into the ledger at every read of it, one change however many
  pairs share its repository, and are cleared (`TestHQChangeLedger_MigrationFromPairsDedupsAcrossPairs`);
  released zcp recorded the Mate's local branch `mate/<mateId>` under the same key, which moves in
  as its change's `mate/<mateId>/<n>`, and the take step renames that local branch, which git
  cannot keep beside the change's
  (`TestUpgrade_AnOpenChangeTheReleasedZcpRecordedKeepsStatusAndPushWorking`). In a
  repository whose code is kept on GitHub (`origin.mode` `github`, read from the Mate's state (§2.8)
  at the call and recorded, the pair's record when no state was heard) the branch is pushed to
  origin, HQ, like any other, and the answer adds that HQ alone pushes to GitHub: it puts the
  branch there and opens its pull request once the change is described — unless GitHub already
  holds a branch of that name HQ did not put there, when the work goes under a new branch name, as
  a new change — the agent opens none and never pushes there, and the person merges it in Mate
  (`TestOpenChange_InARepositoryKeptOnGitHubThePullRequestIsHQs`). `zerops_deploy strategy="git-push"` from a pair
  wired to HQ, to any remote, refuses before git runs — the pair holds no credential, its git
  reaches HQ through `zcp git` alone — and says that way. The person reviews a change by its description:
  `zerops_workflow action="describe-change"` sets it on the pair's open change (MB-34).
- **What one change holds.** This is the
  rule's one home: AGENTS.md's git host block words it once for the agent, as *What one change
  holds*, and every other text that sends work to a branch or a change refers to it by that name;
  the refusal past five open changes and the close-out note repeat only the fold or add steps their
  own answer needs, and never the conditions. A change is what a person reviews, merges and would
  revert as one: work is split by blast radius, never by topic — three unrelated small fixes on
  three screens belong together. Opening a change is the default: work no open change of the Mate's takes opens its own, work the agent tells the person
  is done is in a change by then, and needing a change of its own never by itself stops, defers or
  drops work — work asked for together that needs two changes gets both, with one sentence saying
  why there are two. A run of asks is the person's stream of asks — bigger, move this,
  no, put it back, sort these by date, add a toast — and not a develop work session; its work goes
  into one change: each ask pushed to that change's branch, the change described once at the end of
  the agent's turn, as a list, one item per ask. The next ask joins the Mate's open change when all
  four hold: (1) the change is the Mate's own — not another Mate's, not one handed to it, not one
  made for a task; (2) it is still open and not being merged, nobody has commented on it or asked
  to merge it as far as the agent knows, no other open change builds on it, and no other
  conversation or subagent of the Mate's is working on it; (3) the ask continues that change's run
  of asks, or is unrelated interface work while the change was updated within about the last two
  hours — develop's start and status show each open change's last update — while a follow-up to
  the change joins however long it has waited, so no change stays open forever; (4) its blast
  radius stays in the interface: what a person sees and how the interface behaves on the client,
  with no change to data, server, wire, auth, security, billing, deploy configuration or
  dependencies — copy, layout, styling, the order something is shown in, a toggle, a toast, a
  default size, what opens or shows first, keyboard or focus handling, a small interface fix (one
  of the cases above was a one-line sort order, which belonged with the polish around it). An ask
  that undoes or redoes an earlier one stays in the change, so its diff is the net result:
  separate changes would leave a later change partly reverting an earlier, unmerged one. Anything
  else gets a change of its own: data writes, a schema or migration; server, protocol or wire
  contracts; auth, security or billing; deploy or infrastructure configuration; dependencies; a
  cross-cutting refactor; anything large on its own; an urgent fix for production; work the person
  asked to ship or revert by itself; work in another repository. An ask that goes into a change of
  its own leaves the run of asks going on in the open one. No size bound splits a run of asks, and
  there is never one growing change per person: work beyond the interface always splits out, and
  unrelated work after two quiet hours starts fresh. A change is still cut from another open
  change's branch only when its work needs that change's code (MB-46). The agent does not read the
  change before adding: describe-change reads the Mate's state fresh and refuses a change merged
  or closed meanwhile, and AGENTS.md gives the way on — a branch from an up-to-date `main`, the
  new commits cherry-picked onto it, its change opened. Before asking for review the agent folds
  two or more open changes of its own that the rule would have made one, by the same conditions as
  joining: it merges their branches into one of them, describes that one as a list, one item per
  ask, naming the changes it folds ("Includes #21, #22"), closes the others with close-change
  (below), and tells the person in one sentence which changes it folded into which — never a
  change someone commented on or asked to merge as far as it knows, another Mate's, a handed one
  or a task's. When the person says to keep pieces of work apart, or to put them together, that
  wins. Each push that changes a described change's diff leaves it a draft until it is described
  again, and a person's
  Merge refuses a head that moved since they looked (`head_moved`), so nobody merges what they did
  not see; HQ squashes a change into one commit on `main`, which is why work beyond the interface
  stays apart. The develop start of a plain dev service adds that work added to the open change
  spares a branch switch in the checkout its dev server runs (MB-46); the dev-pool atom, that a
  push to a change's branch moves it, a draft until described again, that work added to it goes on
  in a lease of its branch, leased again if it was released, that during a run of asks the stage
  check comes once, before the end-of-turn describe, and that subagents on one piece open no change
  of their own — the lead merges their branches into one and opens that one's change — while an
  independent piece opens its own (`docs/spec-workflows.md` DP-33). The Mate's state carries none
  of a change's comments, its ask to merge or what it builds on (mate-hq protocol.md, the link
  state's `changes`), so (2) is the agent's to judge from what it knows — the person's words, the
  branches it cut itself — and zcp selects no words by them; it does know a handed change and when
  HQ last changed each change (`updatedAt`): develop's start labels a handed one "handed to you"
  and every open one with its last update ("updated 2 h ago"), status labels each open change's
  last update the same way, and the refusal past five open changes labels a handed one too (below)
  (`TestChangeUpdatedWords_SaysHowLongAgoHQLastChangedIt`). zcp's other words point at the rule:
  develop's start (below); open-change's answer and develop's start beside a production or other
  Mates (*What the application has*, below); the refusal past five open changes, which offers
  folding first; and the develop close's note naming a lease branch no change proposes, which also
  names the Mate's own open changes in that repository — never a handed one — as where the work may
  go instead: pushed from its lease, merged as `origin/<branch>` where the open change's branch is
  checked out, pushed, described again, and the branch's leases released. Pool-migrate's retired
  answer offers none: a migration branch carries the pool's setup, deploy configuration, so it
  always gets a change of its own (`docs/spec-workflows.md` DP-37). Unchanged: HQ's model — a
  branch holds one change, at most five open per Mate per repository, merged one at a time; an
  automated Mate's one change per task; the recipe repository's proposal (above); another Mate's
  changes and a handed one, never added to or folded; the dev checkout's safety (MB-46) (MB-53).
- **A Mate closes its own abandoned or folded change** (a folded one: *What one change holds*,
  above): `zerops_workflow
  action="close-change" branch=<branch>` — or `service=` a lease of it; never inferred, not even as
  the only open one — closes the Mate's own open change on that branch without merging,
  `POST /api/mate/changes/:repo/:n/close`, its branch and the leases on it kept, the ledger
  forgetting it; HQ shows it closed by its Mate in the change's conversation. A change on the branch
  that is not this Mate's own is not in its state and is never asked for. Only an HQ that keeps an
  open change per branch (`openPerBranch`) is asked; an older one, one that answers the route 404 or
  405 (no close for a Mate yet), and any other 403 all get the same answer: nothing was closed, a person
  closes it in its review, by its link. A refusal of this Mate's access — its key (`401
  key_refused` and the other key codes), Zerops, or its project confirmed gone — read from its state
  or from the close, is a known answer: it names that cause, as describe-change does (one
  derivation), says nothing was closed and that calling again will not help until the access is
  back, never advises a retry, and never says HQ lacks the close operation
  (`TestCloseChange_RefusedKey_SaysSoAndNoRetry`, `TestCloseChange_OwnByBranch`,
  `TestCloseChange_OldHQ_SaysPersonCloses`, `TestCloseChange_NamesItsBranch`,
  `TestClient_CloseChange_PostsTheCloseOfTheMatesOwnChange`). The refusal past five open changes
  names it, after folding the Mate's own (*What one change holds*), beside asking the person to
  merge or close one.
- **An application that limits who merges.** HQ can keep merging in an application, and releasing
  with it, to some of its people; the Mate's state then carries `mergeLimited: true` (§2.8) and
  names none of them, so a Mate never knows whether the person it works for is one. Where the
  state last heard says so, zcp tells the agent nothing that sends that person to merge or
  release. Every answer that names who merges, reviews or releases — open-change (its change,
  a handed one, the GitHub note, the refusal past five open changes), describe-change, a change's
  standing in a pass (behind `main`, a draft), the closing note that hands a change over, the
  release handoff with its open changes and `zerops_propose_release`'s note, launch-production's
  refusal, the notes of a repository kept on GitHub (`zerops_origin`, develop's start, the
  realignment) and a dev pool's part with nothing on `main` — names "someone who merges here"
  in the person's place, and the answers the agent hands work over by add one rule, written once
  (`internal/tools/hq_merge_limited.go`): only some people merge and release in this application
  and the person may not be one of them; the agent never tells them to merge or release it
  themselves; when the work is ready it tells them, in plain words, that the change is ready and
  that they press "Ask to merge" on it in Mate, and the people who merge there take it from there;
  a change that waits for a merge is normal, not a problem to solve, and work goes on meanwhile.
  The closing note names the change by its link and no Review to press; the refusal past five open
  changes asks nobody to merge and offers folding the Mate's own, closing an abandoned one or
  waiting. Where nothing is
  limited — no `mergeLimited`, an older HQ, or no state heard — every answer is word for word
  what it was. Only words change: how branches and changes are made does not, and zcp decides no
  permission from the field. AGENTS.md, the atoms and a tool's description cannot read the
  state, so where they name who merges or releases they name a person, never the Mate's own:
  "a person merges the change in Mate", "waits for a release" (MB-51).
- **What the application has.** The Mate's state says how the application's production is run
  (`production`), whether it has a Stage (`hasStage`) and how many Mates it holds (`mates`, §2.8),
  and zcp words four answers by them, each sentence written once
  (`internal/tools/hq_application_has.go`) — nothing is hardcoded for a prototype or a live
  project, and nothing is refused or decided by them. **No production** (a `mates` and no
  `production`): describe-change's answer says never
  to call merged work live, deployed or released, and to tell the person, when the work is ready,
  that merging keeps it on `main`, where later work and other Mates build on it, and that nothing
  is published until they put the project live in Mate — "until the project is put live" where the
  application limits who merges; a merged change's note repeats the first. **A production**,
  however it is run: open-change's answer says to keep each change to one run of asks, anything
  beyond the interface in a change of its own (*What one change holds*, above), and to call
  out in its description, under
  a plain heading, what could hurt production — data changes, setup or environment changes,
  anything a rollback would not undo; develop's start says to keep each change to one run of
  asks, anything beyond the interface in a change of its own. **A production HQ releases to** (`zerops`, `git`):
  describe-change's answer and a merged change's note say a merge does not publish to it, a
  release does, to say so when reporting the work, and never to call it live before a release.
  **A production run outside Mate** (`outside`): the same two say instead that Mate releases
  nothing to it and cannot tell what a merge publishes there — a merge may reach production by
  itself — so the agent never tells the person merged work is live, and never that it is not: it
  says the work is on `main`, and that whoever runs production knows when it goes out. **A
  production of a kind zcp does not know**: nothing is said about production, neither of these
  nor "no production". **A Stage**: describe-change's answer and a merged change's note say that
  after a merge the application's Stage runs what is on `main`, where it is checked before it goes
  live — the last part left out beside a production run outside Mate, where a merge may be live
  first. **More than one Mate**: develop's start says other Mates work in the application too — new
  work starts from an up-to-date `main`, `main` moves, each change stays one run of asks,
  anything beyond the interface in a change of its own. The words name no one who merges or releases, so they read beside
  the limit's rule. From an HQ that sends none of the three every answer is word for word what it
  was; that clause is said beside a production or other Mates and nowhere else
  (`TestApplicationWords_SayWhatAChangeHoldsBesideAProductionOrOtherMates`) (MB-52).
- **What became of the change** (merged with `mergedSha`, closed, or closed with `landedWith`) is
  read from the Mate's state (§2.8) by every `describe-change` and reconcile pass: HQ sends it on
  the link when the change settles. A state older than zcp's own write of a change does not settle
  or forget it. A merged change's note tells the agent to bring
  `main` up to date and cut the next work's branch from it; it carries no CI line (§10.8). A change
  HQ closed because another change's landing on `main` carried every commit of it — a change built
  on it merged first, and that squash holds its work — carries `landedWith`, that change's number
  in the same repository (an integer, present only on such a closed change, whose `mergedSha` and
  `landedHead` stay null; absent on every other change and from an HQ older than it). It is said as
  "change #N was merged as part of #M", all of its work on `main` through #M's merge, nothing lost
  and nothing to do again — never as closed without merging, which tells a Mate none of its work is
  on `main` — and takes the merged change's note, its leases and stages included (MB-49). The
  ledger forgets a change once it is no longer open. A change the state no longer lists is read by
  its window: an HQ that keeps an open change per branch lists every open one, so one missing settled
  past the window and is said as no longer open, never as closed without merging; an older HQ lists
  only the Mate's newest ten in the repository, so one missing while ten newer are listed may be an
  open one that fell out, and stays kept, unknown — only one missing from a window that is not full,
  or newer than its oldest, reads as closed (`TestLearnChangeLanding_ReadsAMissingChangeByTheStatesWindow`). The pass's change turn runs after its pairs', per
  repository, once a pair's turn read the state (or at develop's start) — and, with no pair to take
  a turn (work only in leases), at every pass while the ledger or an HQ lease names a repository; it
  turns to each repository the ledger, a wired pair or an HQ lease names, so a change handed there
  is taken (`TestHQPass_ChangesTakeTheirTurnWithNoWiredPair`) — and holds each change with
  no wait, leaving one a describe holds to the next pass. A change of another application than the
  one HQ holds the Mate in stays there, said once. A merged change's note names its branch, the
  leases zcp holds on it with the release that frees them (`zerops_lease action=release
  branch=<branch>`) — never released by the pass — and nothing of a stage: a stage keeps the last
  work deployed to it until someone deploys again (DP-8)
  (`TestHQPass_MergedChangeNamesItsLeasesAndStages`). An open change's standing, as the Mate's state
  names it from an HQ that judges it (`ready`, `mergeability`, `behind`), is said only when it
  differs from what the agent was last told (the ledger's `told`): a draft with a push to describe;
  behind `main` and still merging cleanly — merge `origin/main` in where the branch is checked out,
  check it, push, sync the group's leases, describe it again, or leave it for the person to merge on
  top of what it was not checked with; no longer merging cleanly — merge `origin/main` in and
  resolve, never a rebase, never a force (`TestHQPass_OtherChangeBehindAfterMerge_ToldOnce`,
  `TestHQPass_ConflictToldWithMergeNotRebase`, `TestHQChangeStanding_SaysOnlyWhatChanged`).
  A change HQ moved itself carries `corePush` (`{ at, from, head, landed, squash }`, optional:
  absent on a change whose branch HQ never moved, and from an HQ older than it): right after a
  squash HQ brings `main` into each open change built on the squashed change's branch by one merge
  commit of its own (author `Core <core@hq.invalid>`), a fast-forward of that branch from `from`
  to `head`, which keeps a described change described (mate-hq protocol.md, "A stacked change after
  a squash", "A description across a push"). The agent's branch is still at `from`, so its next
  push is refused as `non_fast_forward`. While `corePush.head` is the change's head, the pass says
  so once, through the same `told` ledger (its `coreHead`, never said again for that commit) and
  before any other standing, since each ends in a push: "HQ brought main into #N after #L merged:
  fetch and fast-forward `<branch>` before your next push", with `zcp git <host> fetch origin &&
  zcp git <host> merge --ff-only origin/<branch>` where the branch is checked out in the
  repository's dev service — its checkout, or its `/tmp/wt-<branch>` worktree there (`-C`) — or,
  for a branch leases hold, `ssh <alias> "cd /var/www && git fetch origin && git merge --ff-only
  origin/<branch>"` in each lease of it; a push past HQ's commit, or a change without the field, reads as
  before. zcp does not read the agent's checkouts for it (they are other containers, reached only
  over ssh), so the line is said once even where the branch was fast-forwarded already — a
  fast-forward there is a no-op (`TestHQChangeStanding_CorePushAtHead_SaidOnce`,
  `TestHQPass_CorePushAtHead_ToldOnce`).
  Develop's start, on an HQ that keeps an open change per branch, lists each repository's open
  changes by number and branch with their standing and last update, a handed one labelled handed, and says new
  work goes onto an open change of the Mate's only where *What one change holds* (above) says so,
  pushed to its branch, and otherwise on a branch of its own cut from an
  up-to-date `main`, opening its own change
  (`TestDevelopStart_ListsTheOpenChangesOnAnHQThatKeepsOnePerBranch`,
  `TestOpenChangesOf_MarksAHandedChangeAsHanded`). The list is every open change
  the Mate's state names in the repository, recorded in the ledger or not — one opened by another
  zcp, or naming no branch (HQ's default change) — since each counts toward the open changes HQ
  keeps a Mate there (`TestDevelopStart_ListsEveryOpenChangeOfTheMate_RecordedOrNot`). Status names no change on a
  pair: its Changes section is one line per repository — each change zcp's ledger keeps, by number
  and branch, the leases on its branch, an open one's last update ("updated 2 h ago"), an old one labelled (no lease holds it, untouched in HQ for
  days; a label, never an act), and each branch leases hold with no change yet. Each change's
  `ready` label means its head is described, not permission to merge: while HQ has not judged
  mergeability (including an older HQ omitting it), a ready change also says `checking mergeability`.
  HQ's conflict, empty, already-merged and unrelated verdicts say `not mergeable` with the reason;
  a clean verdict keeps the ready label (`TestStatus_ReadyChangeNamesHQMergeability`). Failing or
  pending checks for the head add `GitHub's checks failed` or `GitHub's checks running`. Each change's
  standing is the Mate's state as its link last heard it (§2.8): status asks HQ nothing, and says
  so when the link is down or nothing was heard, even when no change or leased branch is recorded,
  so app-level HQ problems remain readable after changes settle. A merge since the last pass reads merged while the pair backoff holds
  the pass off: a merged change names the leases still on its branch, to release, and that new work
  starts from `main`; a closed or no longer listed one is said so. The read writes nothing (neither
  the ledger nor what was told), so the pass still says a merge or a standing once, with what to do.
  With no state heard — or outside a Mate, where status reads nothing of HQ — the section is the
  ledger's standing as told, labelled as zcp's record
  (`TestStatus_OneBlockPerRepository`, `TestStatus_ChangesReadFresh_AsLastReadWhileHQIsDown`,
  `TestStatusHQChanges_OutsideAMate_AsLastRead`,
  `TestRenderStatus_ChangesBlockPerRepository`).

  Decision: Normal status and develop results report the app's unresolved HQ problems even with no open change, without a person pasting an error or a fabricated lifecycle envelope.
  Decision: zcp shows HQ's finding as given (what, where, rule, next actor/action); it never derives a second verdict.
  Decision: Each normal status reads the Mate's state as the link last heard it, so a new finding appears without an open change.

  Status carries optional app `problems`, change `problems` and group `recipeValidation.problems`
  from that same self read into its prose. Develop's existing HQ pass reports the same findings,
  including when no repository needs work; an error result keeps its leaf payload and gains no
  lifecycle envelope. The shared renderer preserves HQ's code, what, proven location, rule,
  next actor/action/target and optional evidence as structured text; identical findings repeated
  across the wire fields appear once. It neither interprets validation state nor creates another
  verdict, timer, poll, task or cache. Omitted findings add no section and preserve today's output
  (`TestHQProblems_NoOpenChange_StatusAndDevelopReportHQFinding`,
  `TestStatus_AnHQOlderThanRepositoriesIsAskedOnce`,
  `TestHQProblems_PresentProblems_EachStatusReadsNewFindings`,
  `TestHQProblems_RepositoriesWithoutProblems_EachStatusReadsNewFindings`,
  `TestHQProblems_NoRepositories_DevelopStillReportsHQFinding`,
  `TestHQProblems_InvalidGroupHead_StatusReportsValidationFinding`,
  `TestHQProblems_OmittedProblems_StatusOutputUnchanged`,
  `TestClient_Self_Problems_DecodesHQFindingsAndOlderHQ`).

  `zerops_lease action=status` names each group's
  change (repository@branch, once however many leases it has) from zcp's ledger and lease records,
  its standing read the same way — once, inside a Mate, only when a lease group of an HQ repository
  is in view; as last read while HQ does not answer (`TestLeaseGroupChanges_NamesEachGroupsChange`).
  A group's change HQ brought `main` into (`corePush` at its head) is named the way the pass says
  it, with the fast-forward over ssh in each of the group's leases — a view, so it stands while
  HQ's commit is the head (`TestLeaseGroupChanges_CorePushAtHead_SaysItInEachLease`). A lease
  sync whose push origin refuses as non-fast-forward names HQ's push as the cause when the open
  change on the lease's branch carries it at its head — the Mate's state read once, for that
  refusal only — and says to merge origin's branch in over ssh in the lease (a fast-forward when
  it has no commits of its own since), never a rebase, then sync again
  (`TestLeaseSyncRefusal_AfterHQsPush_NamesTheCause`). The agent's own pushes on the mounted
  checkout are plain git zcp never sees, so they get no such refusal of zcp's.
  A lease on a branch whose change merged — the pass told it and forgot the change, or no pass ran
  since — is never "no change yet", which invites a new change on merged work: lease status says
  "change #N merged — release this lease (`zerops_lease action=release branch=<b>`), or start new
  work from main", and status names it as a merged change, its leases to release
  (`TestLeaseStatus_MergedBranchSaysSo_NotNoChangeYet`). A lease release's result names each
  released branch's change the same way — its standing read once, inside a Mate; as last read while
  HQ does not answer — a merged one naming the branch's leases still held with the branch release,
  so the agent never calls a change open that merged since the last pass
  (`TestLeaseRelease_ResultNamesTheReleasedBranchsChange`); it names no stage. A lease on a branch whose change was closed
  still reads "no change yet" (opening again there is a new change). The launch refusal and the
  release handoff name every open change once per repository, however many pairs share it — a
  release takes `main` only (`TestLaunchProduction_ListsEveryOpenChangeOnce`,
  `TestHandleRelease_NamesTheOpenChangesItLeavesOut`). The release handoff reads the Mate's state
  once from the file (§2.8, inside a Mate, only when the ledger keeps a change) and leaves out a change
  it names merged, closed or no longer listed, so a change merged since the last pass is never
  named as left out; it writes nothing, so the pass still says the merge once. Unread — outside a
  Mate, or no state heard — it names the ledger's, as last read
  (`TestHandleRelease_LeavesOutTheChangesHQSaysSettled`).
- **A change handed to the Mate.** A person hands an open change whose Mate is gone to another
  Mate of the application; HQ keeps its branch — one its Mate named, or `mate/<old>/<n>` — and
  the Mate's state (§2.8) lists it with `branch` and `handed: true`. The next pass keeps it in the
  ledger with that branch (`hqTakeHanded`, after it learned what became of the changes it kept) and
  says once how to take the branch — fetched when its Mate pushed to it, cut from `main` when
  nothing is there — and to push to it, never force. `open-change` reads the Mate's state first and
  answers a handed change on the branch named, opening none; another branch opens its own change,
  the answer naming the handed one, which stays open on its branch
  (`TestHandedChange_AnotherBranchOpensItsOwnChange`). An older HQ hands a change only to a Mate
  holding no open change in the repository, so there `open-change` answers the handed change
  whatever branch is named. The group recipe's writer moves a
  handed proposal on the branch HQ names for it (`hq.Change.Branch`). zcp
  `TestHandedChange_IsThePairsAndTheWorkGoesOnItsBranch`,
  `TestGroupRecipe_AHandedProposalMovesOnItsOwnBranch`.
- **The checkout's `main`.** A pass changes the checkout in one case only: a local `main` that is
  only zcp's empty seed — a checkout wired before branches of the agent's own kept it, the work on
  another branch, so `git merge --ff-only origin/main` failed — is put on HQ's `main`, fetched from
  HQ's address over ssh (`ops.BuildMainSyncCommand`): moved by ref while the checkout is elsewhere,
  checked out while it is on `main`, refused by git with nothing moved when an uncommitted file is in
  the way, the next step named. A `main` with commits HQ's lacks is never moved; when no other ref
  holds them, the pass names the step that keeps them on a branch and puts `main` on HQ's — except
  a history of its own against HQ's unwritten root (`ops.MainFirstHistory`), the repository's first
  history, whose step is to push it as it is on a branch of the agent's own and open its change,
  which HQ makes `main`, never to carry it onto that root; the hq-git playbook and the
  git-push start guidance keep such a `main` as it is too, rather than resetting it onto the root.
  A `main` HQ's has or descends from is left to the agent's fast-forward. Develop's start runs the pass for every pair outside the
  backoff (`hqPassNow`); bootstrap and adopt keep it. zcp
  `TestMainSync_ASeedOnlyMainTakesHQsMainAndNothingOfTheAgentsMoves`,
  `TestDevelopStart_AMainThatIsOnlyZcpsSeedTakesHQsMain`,
  `TestDevelopStart_AMainWithCommitsOfItsOwnIsLeftWithItsStepNamed`,
  `TestMainSync_AFirstHistoryOnHQsUnwrittenRootIsPushedAsItIs`,
  `TestDevelopStart_AFirstHistoryOnHQsUnwrittenRootIsPushedAsItIs`.
- **HQ not answering, fast.** A describe's and the git helper's HQ call connects within 5 s,
  TLS included, and is answered within 30 s (`hq.Client.Bounded`; `Once` sends it once, so the
  client's own 20 s wait on a 503 never stacks under the caller's tries). git has no connect setting — curl would wait 300 s — so
  zcp's own git against HQ (the wiring's fetch, the push proof) is bounded by a 10 s stall passed per
  command, with `timeout` 4 min (`ops.HQGitBound`) as a safety net under the SSH runner's own 5 min.
  A describe that meets HQ not serving tries again on its own pace (`hqRetry`) and otherwise keeps
  its words and asks to be called again. A pair record an earlier zcp left with `hq.pending` reads as
  before and loses the key on its next write.
- **One process at a time on a checkout.** Every step that runs git on a pair's checkout — a
  stand-up's wiring, a describe, a pass — holds the pair's lock (`workflow.LockPair`, a file lock in
  the state directory keyed by the pair's dev hostname) for its whole sequence, and reads the pair's
  record afresh once it holds it. A pass leaves a pair held elsewhere to its next turn, recording no
  attempt; an agent's step waits up to 2 min and then runs no git and says why.
- **A Mate HQ now holds in another application** is wired there on its next pass: the repository of
  the same name in the new application, made if new, `origin` pointed at it and the checkout put on
  its `main`; its change in the old application stays where it is.
- **The credential.** A dev service holds none. The wiring points origin at the repository and
  records the pair as pushing there (`wireHQOrigin`) — no `git-push-setup`, no token, no probe — and
  every git zcp runs there against HQ (the base-branch fetch, the `main` check) is a relayed script
  (`ops.RelayedSSH`), answered with the project key over the session, as an agent's `zcp git` is.
  A dev pool holds no Git credential either: zcp's git in a lease of a repository on HQ — the
  checkout, a sync's push, the release guard's fetch — runs as a relayed script, and the agent's
  runs through `zcp git <alias>` (`spec-workflows.md` §8 DP-12, DP-16). A pool's warm clone of a repository on HQ
  runs as a relayed script that a zcp process of its own holds until the clone ends (DP-32).
- **A Mate moved to another HQ.** A wired pair whose record names its own repository
  (`/git/[<org>/]<app>/<repo>.git` of its HQ record) on another host than the HQ the Mate links to now
  was wired before the move; any other remote is the person's and is never moved. At the Mate's service start, and at
  every repository pass, its checkout's `origin` is pointed at the repository's address on the
  current HQ while it still names the retired one (an origin anywhere else is not moved), git's
  helper for the retired host goes and HQ's is written for the current one, and the record follows.
  A pair keyed by its dev pool has no checkout to point: only its record follows.
  A start that could not finish — the dev service stopped, HQ not answering — is taken again on the
  pass's backoff; each command on a dev service is bounded
  to 15 s. A pair a refused push marked keeps the pass's backoff: the pass wires it again. The project key
  is never written into the dev service or a pool: they run the application's code and its dependencies. The
  helper persisted in `.git/config` for HQ's host answers nothing in the dev service and, for
  sessions on the older guidance, a shell in the zcp container from `zcp hq git-credential`,
  which answers HQ's host with the project key — and, for github.com, with
  HQ's per-call read token (§5.10). Guidance runs a dev repository's git through `zcp git <host>`
  (§6.8), whose relayed call clears both helpers for itself.
- A push to HQ is watched for no build and offers no integration, and a wired pair's direct deploys
  are never redirected to a push. The develop session's auto-close note hands the person the open
  change's link and the next step while the pair has one; a stand-up's closes without it.

`hq_pair.go`, `hq_change.go`, `hq_change_description.go`, `hq_describe_retry.go`, `hq_retry.go`,
`hq_wiring.go`, `hq_dev_git.go`, `service/service.go`, `hq_pair_lock.go`, `deploy_git_push.go`, `workflow/pair_lock.go`,
`ops/hq_base_branch.go`, `ops/lease_pool_credential.go`, `ops/pool_credential_record.go`, `internal/hq/changes.go`, `internal/hq/gitcredential.go`;
`TestReconcileHQRepositories_*`, `TestHQPass_LearnsAMergeWithoutChangingTheCheckout`,
`TestAWiredPairDeploysDirectlyAndIsNeverSentToPush`, `TestGitPushToHQ_RefusedForPlainGitOnABranch`,
`TestBuildBaseBranchCommand_*`,
`TestBaseBranchRefusalRemedy_TheNextAttemptWires`, `TestHQGit_*`, `TestHQRetry_TriesOnlyWhileHQCannotServe`,
`TestHQNotServingWords`, `TestUpsertServiceMeta_LegacyPendingDelivery_IsDropped`,
`TestClient_Bounded_EndsATryHQDoesNotAnswer`, `TestAPassSkipsAPairHeldElsewhere`, `TestLockPair`,
`TestDescribeChange_WaitsWhileItsChangeIsHeld`, `TestDescribeChange_ByBranch`, `TestDescribeChange_SeveralOpenNoBranch_Refused`, `TestClient_*`, `TestGitCredential_*`,
`TestPersistedCredentialHelper_*`, `TestReconcileHQRepositories_RenewsThePoolsCredentialAfterTheMateEnrollsAgain`,
`TestHQPass_PoolDevHalf_RenewsOnPoolNoSSH`, `TestHQPass_PoolDevHalf_TakesOverThePoolsCredentialRecord` — against a fake HQ that serves real git with HQ's ref rules.

**A new Mate stands up from its recipe in one call.** Adding a Mate to an application —
whose recipe is `0 — AI Agent/import.yaml` on `main` of the application's recipe repository in HQ,
`group` — runs like this.

Who holds what:

- **The person's browser, as the person**, reads the recipe and builds the project: it creates the
  project, registers the Mate, imports the whole tier in one import (the `zcp` container at the top
  `priority`, `project: envIsolation: service@zcp`), lowers the container's token (`BASIC_USER` on
  its project, and on nothing else), drops the one-time delegation.
- **HQ** keeps the recipe on `main` of the recipe repository and gives each pair its repository;
  the Mate reaches both with its project key (§6.6).
- **zcp**, with its lowered key, stands the Mate's development up in its own
  project and nowhere else — the group's stage and production are not its, and it reads no tier but
  `0 — AI Agent`.
- **The model** acts only on its signer's message, and is the backup.

The order:

1. The browser reads the recipe as the person.
2. It creates the project and registers the Mate at its HQ.
3. It imports the whole tier in one import: the `zcp` container at the top `priority`, the managed
   services, and the runtimes — dev halves `startWithoutCode: true` (running and empty, the
   checkout target), stage halves with no build (`READY_TO_DEPLOY`), public-build utilities with
   their build, which the platform runs — under `project: envIsolation: service@zcp`. The platform
   creates them in priority order and isolates every service's variables but the container's. The
   container imports nothing, so it verifies nothing.
4. It lowers the container's token, drops the delegation and grants the broker, and the sign-in is
   offered.
5. The Mate's server links to HQ with the project key (§3.5); the key in zcp's header is what the
   stand-up reads the recipe and the recipe's repositories with (§3.6).
6. The person signs the agent in meanwhile; the Mate's server keeps who did (`zeropsio/mate-hq:docs/architecture/agent-runtime.md`).
7. The Mate's server sends "Stand up development of the project." into the main conversation as
   the person its HQ names as the stand-up's asker (`standupRequestedBy`, written in the same write
   as the Mate's record), once that person signed an agent in here. Until the
   link to HQ brings the Mate nobody is known to have asked, and nothing is settled on that; HQ's
   record naming nobody settles it as none at once. No client sends it. While it waits,
   `/setup.json` says why where the server knows: `not_linked`, with the link's reason (§3.5, C-7). The Mate's AGENTS.md sends that message to the standup playbook and `zerops_standup` first.
   Since the person never typed it, the playbook has the agent say first, in plain words, what it
   is about to set up and roughly how long that takes (each app built for development and for the
   preview, minutes per build), then give one short line per step, the fallback's included
   (`TestBuildAgentsMD_Container_StandUpRoutesToTheTool`).
   The tool:
   - takes the project's one stand-up (§2.8b B-2) and reads the recipe under the Mate's key, the
     application from HQ's answer, never from the Mate's state file (§2.8b B-3); an HQ that does not
     answer is waited on, bounded (2 min, asked every 10 s), and a refusal stops the run at once with
     HQ's words (zcp `internal/tools/standup.go`, `internal/hq/client.go`);
   - waits, bounded, for the runtimes (5 min: every dev half
     running and answering SSH, every stage half created; it reads the project again at each of the
     project's pushes, a service's or a process's (ZEROPS-079, ZEROPS-078), never on a timer —
     `TestStandup_TheRuntimesAreReadAgainOnlyOnAPush`), and for whatever the import still has in
     flight on them and on the managed services;
   - reads the tier from `main` of the recipe repository of the application HQ holds the Mate in
     (the application as HQ answers it, `GET /api/mate/recipe/mate` for the tier);
   - pairs the runtimes that build from the application's repositories in HQ
     (`<HQ>/git/<orgId>/<appId>/<repo>`), per repository, by zcp's naming —
     the stage half ends in `stage`, its dev half is the repository's other runtime or the one named
     like it — with each half's `zeropsSetup` (else its hostname) and the pair's priority, the
     higher of its halves'. A stage no dev runtime takes pairs with its part's dev pool: the tier's
     Swarm pool named after it (`apistage` → `apipool`, the name pool-migrate gives a part's pool),
     written with no build (or one from the stage's repository) as the group recipe writes a part's
     pool (§10.10); a pool is never a container half, and one no stage takes is skipped as `pool`.
     Managed services and public builds are skipped, and so, by name, is a runtime with no partner
     or another application's repository (`workflow.ParseMateTier`). A tier with no pair of either
     kind is refused, its error naming both kinds and every service with its reason.
     A legacy HTTPS reference whose exact path is `/git/<appId>/<repo>` and names this
     Mate's own application identifies that repository regardless of the HQ host or port
     it names; stand-up reads and wires the repository through the current HQ. Only this
     own-application legacy path bypasses the current-host check; other paths retain
     current-host matching, including organisation-qualified references.

     Decision: A Mate added after HQ migration stands up the migrated application's existing dev/stage pair.
     Decision: A recipe reference to another application's repository is never treated as this application's repository.

     Pinned through `TestStandup_HQMigrationDecisions` and `TestParseMateTier_HQMigrationDecisions`;
   - adopts each pair exactly as the adopt route records it, plus the tier's setups
     (`workflow.AdoptPair`), mounts the dev half and wires it, through the reconcile's own wiring, to
     the repository of the recipe's name in HQ — refused first when HQ does not have that
     repository (git's own `info/refs` answering not found), since HQ makes what it is asked for.
     A part's dev pool and its stage are recorded as the pair keyed by the pool, the shape the pool
     migration's re-key leaves (docs/spec-workflows.md §13 PM-10; `workflow.AdoptPoolPair`:
     `devPool` with the pool's service id, no `migratedFrom`), and its repository in HQ is recorded
     on it over no ssh — the HQ record, the remote and `main` as the wiring records them, after the
     same existence check — since a pool holds no checkout of its own and each lease checks its
     branch out from the repository (DP-16). A pool is never waited on as running: it has no
     container until a lease asks for one. Each pool's bearer reference is written at adoption
     (§2.8b B-4). The pooled part is then built by commit deploy (§2.8b B-1): the zerops.yaml at
     `main`'s head must name each service's setup, and every pool and stage with no build takes that
     commit at once, beside the classic halves' deploys; a pool or stage with a build is never
     written. Each build's failure carries the process's reason, the version's status and its build
     log's last lines. Once the builds end, `main` is read again, and a head that moved during the
     run is said (what was built is the head read at the start, and `main` is ahead of it);
   - deploys each classic half as soon as what it needs stands, at most five at once, over two calls. What
     orders the halves is what each build reads of the others, read from each half's own setup in
     its pair's zerops.yaml as the recipe writer reads it: a storefront's stage build pre-rendering
     from its API's stage waits for that stage, and nothing else waits. Every dev half starts at
     once — a dev build installs dependencies — unless its build reads another dev half; the first
     call answers once they stand, its stages queued; on the model's second call, a stage is
     cross-deployed from its dev half — never alongside it, since both read the dev half and one
     source's deploys serialise their git work and upload on its git lock in goroutine order, a
     self-deploy first dropping its SSH session under the cross-deploy — once what its build reads stands. It ships the dev half's
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
   - starts no dev server — no dev command is recorded anywhere zcp reads — and reads, per dev half,
     the dev server its supervisor runs (`running`, `did not come up`), else names the
     `zerops_dev_server` call with the port its dev setup declares (`not started`; `unread` when the
     supervisor did not answer);
   - answers one entry per service: adopted, wired, deployed with its URL, queued, or failed with
     zcp's failure classification, the dev server's state, and the model's next step. A pooled
     part's pool and stage answer their verdict row (§2.8b B-5), its `next` the way on, and the
     answer's next step follows the headline: `done`, every pooled stage runs `main` and is the
     Mate's preview; `ready`, the pools lease and a stage that is not built has no preview yet, so
     nothing tells the person it is up; `building` or `failed`, each row names its state, cause and
     step, and `zerops_standup` again continues and never builds twice. No lease is named: a lease
     never holds `main` (`spec-workflows.md` DP-3), and a part's work goes through develop, each
     branch in a lease of its own.
8. The model starts the dev servers, verifies, and tells the person development is up, with each dev
    half's address.
9. It calls `zerops_standup` again in the same turn, which builds the stages and waits for them. A
    stage never called for stays `READY_TO_DEPLOY`, as the import left it: nothing breaks, and any
    later call, or a deploy by hand, builds it.

The stand-up's section of the status file (`ZCP_STATUS_FILE`, `standup`) is one stand-up over its
two calls: `running` from the first call's start until the second has returned, never `done` between
them. The section names the zcp MCP server running it (`process`: its PID and start time, the
handle the work sessions use); a `running` section whose process is provably gone — the PID absent,
or reused by a process with another start time — is a stand-up that died, and the Mate server's
setup step says `failed` (`process_gone`). Nothing ages a section: no heartbeat, no staleness
window. A first call that leaves the stages queued ends with the section still `running`, in the
`stage` phase, its stages `build`/`pending`; a second call of the same MCP server goes on with that
section — the same `startedAt`, the same halves — and ends it `done` or `failed`; a call of another
process starts afresh. Every call stamps its own start (`callStartedAt`), so the Mate server's relay
matches the section to the call now running it — the stage call's card shows the stage phase, and a
stage call whose process died is judged as its own. zcp never ends a carried section by itself: it cannot see the agent's turn
end, and the Mate server can. A stand-up whose own turn ended while its section still waits in the
`stage` phase, no half running, ended without the second call: the server's setup step says
`failed` (`stage_not_built`) — development stands, the stages were not built — and a later call
that builds them moves the section on, its own `done` or `failed` again zcp's word.
`TestStandup_WritesItsProgressForTheRunCard`, `TestStandup_TheRecordNeverSaysDoneBetweenItsCalls`,
`TestStandupStatus_CarriedByItsProcess`.
While a batch deploys, each target's step (`build`, `deploy`, then `verify` once its process has
ended and its deploy has not returned) and the process behind it are read from the project's live
processes when the batch starts and again at each process push (ZEROPS-078), never on a timer: one
reader per call for every batch, on the call's one push session — the one the wait for the
runtimes opened, or opened by the first batch. `TestStandupTrackDeploys_NamesTheStepAndItsProcess`.

The model is the backup:

- A refusal before anything is touched — HQ refusing the key or not answering it, no application (HQ's state saying
  none, or HQ refusing the tier `mate_not_in_app`), no tier on `main`, a tier past HQ's read bound
  (`413 too_large`), a tier that does not read or holds no pair of either kind (dev/stage or
  pool/stage) — is an error result naming the adopt route.
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
`TestBuildAgentsMD_Container_StandUpRoutesToTheTool`, `TestServer_StandupToolGating`.

**A stand-up stops at dev.** A new Mate's stand-up done by the model — the fallback,
when `zerops_standup` stops short — deploys the dev halves it adopted (their build installs
the packages), starts their dev servers, verifies them, and names the stage as the next step the
person can ask for: no promotion, so no commit, change or push unasked. zcp tells a
recipe-born Mate's empty services from deployed ones, so their
first deploy opens develop's first-deploy branch — which promotes each standard pair to its stage.
So in a Mate, develop start leaves the
stage half of an adopted pair whose dev half never deployed out of the session's scope
(spec-workflows D2f): nothing is asked of the agent, the session closes on the dev halves, and the
next task's session, its dev half deployed, deploys the stage as before. A pair a Mate created for
a task keeps deploying its stage on its first deploy — "build a todo app" is
still the whole prompt (`TestHandleDevelopBriefing_MateStandUp_LeavesTheStageOut`,
`TestScenario_S14_StageLeftOutOfScope`).

**A change's description.** A change opens with a title of
what it holds and nothing more, while the person reviews it by what it says. So the
Mate writes it: `zerops_workflow action="describe-change" branch=… description=…` — markdown, at
most 20000 characters and no NUL, which no text in HQ keeps — sets the body of the open change on that
branch (`PATCH /api/mate/changes/:repo/:n {body}`); `service=` a lease alias names its branch, and
with neither the repository's only open change is the one — several open is a refusal listing each
one's `branch=`, number and title, never the newest (`TestDescribeChange_ByBranch`,
`TestDescribeChange_SeveralOpenNoBranch_Refused`, `TestResolveChange_SeveralOpen_RefusesListingEach`,
`TestResolveChange_NeverTheNewest`) — and replaces what was there; a `title=…` given with it retitles
the change in the same edit. The words are kept with the change in the ledger first, so none are
lost while HQ does not answer: they wait for the change they were written for. A describe holds its
change (`workflow.HoldHQChange`), never its pair, so a pass never forgets the change while a describe
writes onto it (`TestDescribeChange_WaitsWhileItsChangeIsHeld`). Words never reach a change they were not
written for: a change on record that merged or closed before they reached it is named to the Mate
and nothing is kept, and words kept for a change that is gone are dropped rather than inherited by
the next one. Describing never opens a change, and with none open there is nothing to describe yet.
A describe that lands answers the change's `labels` — ready or draft, its mergeability and whether
it is behind `main`, as status labels them (`changeLabels`) — from HQ's answer to the write itself,
which carries the whole change; an answer without `ready` comes from an older HQ and is read as
ready. Being the write's own answer, it needs no second read, and the agent reports the change it
described this turn from it (the agent guidance's *Reporting a change's status*) unless it pushed
to that change since. Its message says a push that changes the change's diff makes it a draft again,
and that HQ keeps it ready when it can tell the diff is unchanged, and it ends saying the
description is for the review, not the agent's reply: the agent still answers the person in a few
lines of its own — what it did, where to see it, what waits on them. An agent that read "they
review it by these words" as its reply ended its turn on the describe and said nothing, twice in a
row, so a twelve-minute run showed nothing done (2026-11-22,
`TestDescribeChange_TheAnswerSaysTheDescriptionIsNotTheReply`). Refused, kept and not-landed
answers carry no labels.
A describe waits out an HQ that is away for a moment: HQ's Core rolls for about 20 s,
and a describe that met the roll once left the change a draft. Its calls are tried again, a growing
wait apart for about a minute in all, while HQ is not reached (refused, reset, no connection within
the bound) or answers 502, 503 or 504 — never on HQ's own refusal, a 4xx or a 500, nor on a try HQ
connected for and left unanswered; when the minute is spent, the answer says plainly that the
description did not land and the change is still a draft, the words kept — or, when a try may have
reached HQ (a connection dropped after the words were sent, a balancer's 502 or 504), that it may
not have landed, since HQ has no read of a change's words to check. A try HQ took and left
unanswered past its bound is not tried again, and is said the same way — the words may not have
landed — never as HQ not taking them, since HQ may have put them on (`describeAwayOf`,
`TestDescribeAwayOf`, `TestDescribeNotLanded_Unanswered`). With no change on record, the
read of the Mate's own state that names the open one is tried the same way, and HQ staying away is
said as not knowing, never as "no change is open". `hq_describe_retry.go`; `TestDescribeChange_WaitsOutARollingDeploy`, `TestDescribeTransient`;
`hq_change_description.go`; `TestDescribeChange`, `TestDescribeChange_WaitsWhileItsChangeIsHeld`, `TestDescribeChange_ByBranch`, `TestDescribeChange_SeveralOpenNoBranch_Refused`,
`TestDescribeChange_Refusals`, `TestWorkflowTool_DescribeChangeReachesItsHandler`.

- **A change asks for review only when it is ready.** A change
  asks the person for nothing — no composer line, no *Review* on its menu row, no waiting face, no
  merge step — until the Mate marks it ready by describing it: HQ records the head the description
  was written at, and the change
  is ready (`HqChange.ready`) while that is its head. A description with words sets it — an old zcp's
  `PATCH {body}` included — an empty one clears it, a title alone leaves it. On current HQ a push
  carries readiness to its new head only if HQ successfully verifies that the change's own verbatim
  diff is unchanged; a changed or unreadable diff makes it a draft again. Older HQ may mark a moved
  head draft even when the diff is unchanged. A client reads a change from an older HQ, which sends no
  `ready`, as ready. Words HQ did not take are kept on the pair only so the pictures they show stay
  kept; the answer asks the agent to call `describe-change` again, and nothing else re-sends them.
  Each repository's change gets its own title: the describe call takes `title` — what this
  repository's change does, never the work session's task, which is the same line in every
  repository the session touched — cut after the last word that fits 120
  characters, with "…"; zcp gives a change no title of its own. A describe holds the pair's
  checkout, so two describes from two chats never interleave. The client asks for review only while
  the Mate rests, too: not while a turn runs or helpers it started still work in any of its chats; a
  watch loop alone is not work. `TestDescribeChange`, `TestDescribeChange_WaitsWhileItsChangeIsHeld`, `TestDescribeChange_ByBranch`, `TestDescribeChange_SeveralOpenNoBranch_Refused`.

**A change's pictures.** The pictures that prove a change
are the screenshots `zerops_browser` takes, and each is kept as one of the Mate's pictures
(`workflow.KeepPicture`, under the state dir) and named in the tool's result — `screenshot.picture`,
`shot-3`, an id never given twice, with how long it is kept (`screenshot.keptFor`). The bound is
time and bytes, not a count: a picture is kept for a week after it is taken
(`workflow.PictureKeepFor`) — a change's "before" is often taken hours, or a whole step, ahead of
its description —
while the store holds at most 256 MiB (`workflow.PictureStoreBytes`), past which the oldest go
first (counting the files on disk); the newest is always kept, and every one a kept description
names outlives both — a pair record that does not parse is skipped, and nothing is pruned while the
pairs' directory cannot be read. A picture already on a change outlives its file: its entry keeps the
address HQ serves it at while that change is a pair's current one (90 days at most), so a change made
a draft by a push is described again with the same picture; only another change needs the file, and
a refusal lists only the pictures the change being described can show. Reading the store writes
nothing. One store
per state dir, shared under its own lock by every zcp process of the Mate (its conversation,
helpers, subagents). A description shows one as `![what it shows](shot-3)`: `describe-change` sends each picture
it names to HQ as the change's attachment (`POST /api/mate/changes/:repo/:n/attachments`, the PNG as
the body, at most 20 MiB, once per change, the address remembered with the picture) and publishes it
as `<img alt width height src>` at `https://<HQ>/api/apps/<appId>/changes/<repo>/<n>/attachments/<id>`,
which the client reads as the person — the width and height the picture's shape at 720 pixels wide
at most, so a reader reserves its box before the bytes arrive. Never a body with a broken picture: a
picture the Mate does not keep is refused before anything is written — the refusal names the
pictures it does keep and how long one is kept; one pruned while the words went on is refused the
same way and the words are not kept — and one HQ refuses for what it is (over 20 MiB, `413
too_large`, or no raster picture, `400 invalid` reason `not_raster`) writes nothing and drops the
words. Only those refusals are about the picture. HQ's word on the Mate's key (`401 key_refused`,
`403 key_too_wide`, `key_not_sensitive`, `not_a_mate`), a Zerops refusal or a project confirmed gone
is about its access, which can come back: nothing is written, the words and their pictures are kept
for the next describe, and the answer names that refusal. Any other refusal, and HQ refusing for
now, keeps them the same way. A picture's file kept only as another change's address is one the change being described
cannot show. `TestDescribeChange_Pictures`, `TestKeepBrowserPicture`,
`TestKeepPicture_*`, `TestDescribeChange_MissingPictureNamesWhatIsKept`, `TestPictureRefs`.

**A merge may come at any moment.** A change appears in
its Mate's conversation as soon as it opens, and the person may merge it while the agent still
works. A pass reads what became of each change zcp keeps from the Mate's own state and tells the
agent: merged, bring `main` up to date and cut the next work's branch from it, the leases on its
branch named for release; closed, nothing of it is on `main`; and
of the others, once each, those now behind `main` or no longer merging cleanly, with a merge of
`origin/main`, never a rebase. A pass never changes the checkout and never releases a lease. A
closing session hands over a change only while the ledger keeps an open one in a repository it
deployed into. Production guidance reads the Mate's changes from HQ; an unreadable HQ is
a visible unknown, with a manual ask again. The recipe's additive and scaling paths keep Core's
empty-change rule: nothing beyond `main` opens nothing.
`TestReconcileHQRepositories_TellsTheMateWhatBecameOfItsChange`,
`TestHQPass_LearnsAMergeWithoutChangingTheCheckout`, `TestSessionAnnotations_HandoffOnlyWithAnOpenChange`,
`TestProductionKnowsMergedWorkThePairHoldsNoRecordOf`, `TestProductionGuidance_HQUnavailableIsUnknown`,
`TestGroupRecipe_OpensNothingMainAlreadyHas`, `TestGroupRecipeScaling_ProposesFromMainsHeadEveryTierFresh`.

**A Mate's production is its application's.** launch-production creates its own production project on a user-owned remote and stages
its own token; it has no case for a group's production, which the person adds from the projects page
and which runs what a release tag on the group repo lists. So in a Mate
(`runtime.Info.MateEnabled`), HQ answering or not, `handleLaunchProduction` refuses before scope,
state or any mutation, with the blocker `wired_mate_production_is_the_groups` and a next step that says what is
true of this Mate's own pairs — never "nothing open" read as "merged": every recorded change is first
read from the Mate's state as its link last heard it (§2.8), then one still open is named (even if its local
number was lost) — "merge `<host>`'s change #N on `<repo>` first" — a merge HQ holds, the Mate's
own change merged or landed with another (`landedWith`, MB-49), points at the projects page, and no
change at all or one closed without merging, its work not on `main`, says the work goes to `main`
first, a branch of the agent's own pushed and merged; with HQ not answering or refusing the key, it
says so and that the answer is HQ's, to ask again once HQ answers. A container
that is not a Mate keeps the classic route. The `idle-launch-entry` atom, which offers the workflow, is
not served in a Mate (`mate: [outside]` on the envelope's `mate` key, §1.1). `hq_pair.go`;
`TestHandleLaunchProduction_AMateRefusesBeforeAnyStep`,
`TestLaunchProduction_AMateKeepingItsCodeInHQRefusesAndSaysWhatIsTrue`,
`TestIdleGuidance_IG1_MateBootstrappedDeployed_NoLaunchNoRouteMenu`. No zcp tool tags an HQ release; Core does so for the person.
Outside HQ, `action="release"` keeps tagging the pair's own checkout remote.

**An agent's release request in HQ.** `zerops_workflow action="release"` returns
`status="release-person-required"` when this container is a Mate, or the named pair has a
recorded HQ repository. This happens before service/bootstrap requirements, git-push prerequisites,
freshness checks, tag reads or tag writes; `releaseVersion` does not authorize an agent to tag HQ.
In a Mate the result tells the agent to propose the release with `zerops_propose_release` (§2.11),
whose card the person reviews and releases from, and that they can also release in Mate themselves;
outside a Mate, which has neither that tool nor `zerops_observe`, it tells the agent to tell the
person. A Mate's pipeline note says Core delivers each approved release to production, creating the
services its production tier declares first, and that once the person releases, the agent reads
`zerops_observe action=deployments` for how it ended: when a service could not be created it says
so, names the service and proposes the creation again (`zerops_propose_create_service`), never
telling the person to run the deploy again. A Mate's application guidance also says what HQ's
`production_effect_approval_required` (and a preparation problem named `approval`) means: the
approval is the release itself, which creates what production lacks first; Mate has no
separate control to approve, so the agent proposes the release or the creation and never sends the
person to approve anything on a page. `ProdLaunches` is not evidence of production setup or
release readiness in HQ. The handoff conditionally points to _Add production_ when missing and _Merge_ for any
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

**Variables, strict.** zcp acts as if the platform's strict env isolation were on,
though Zerops still injects every value today: a service reads only what its zerops.yml references.
Because `KEY: ${KEY}` reaches the app as the literal text, an entry is named
what the app reads and the value it references carries another name (`APP_KEY: ${APP_KEY_SECRET}`)
until Zerops resolves same-name references. `zerops_env set` takes `sensitive` (by the key's name
when left out) on both scopes and restarts only the runtime services whose deployed entries read
the key — none, and it says the key needs a reference and a deploy instead. A deploy's preflight
fails a `${NAME}` nothing resolves. No tool output carries a sensitive value; the person's values
live in the Vault (§5.8).

### 10.11 Release and production in a Mate

HQ Core delivers each approved release to the application's production. Mate shows whether the
application has a production; an agent cannot infer its absence from `zerops_observe`. That tool
lists only environments with a Zerops project and keeps the required `projectId` observation wire.
A stage or production that Mate delivers to a Git repository is not listed there. The release
handoff directs the person to Mate to review and release; the agent never asks for a repository URL
or token. Mate's refusals of standalone `git-push-setup`, `build-integration`, `launch-production`
and source release remain unchanged (§2.8 keeps the launch and credential contracts).

### Invariants

| ID    | Invariant                                                                                                                                                                                                                                                                                                                                                                                         |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MB-14 | zcp hands its key to no forge and no app container, and writes no workflow into a Mate's repositories in HQ (deploys from HQ are HQ's). zcp `workflow_build_integration_citoken_test.go`, `deploy_ssh_test.go`.                                                                                                                                             |
| MB-26 | A Mate's code reaches HQ by plain git on a branch of the agent's own, never a push of `main` and never a force; a stage deploy of a wired pair deploys and delivers nothing — no commit, change or push — a `strategy="git-push"` deploy from a pair wired to HQ, to any remote, refuses before git runs, and a wired pair's direct deploys are never redirected to a push; a pair's checkout is put on a `main` that descends from HQ's — or, against HQ's unwritten root, holds its own history as it is, that root never joined in — and two unrelated histories of code are never joined (the pair's own is kept on a branch); a group's stage and production build the stage half's setup — the one a deploy of the stage half recorded, else the one setup the pair's zerops.yaml declares beside the dev one, else its only setup — and a tier that would build a stage setup nothing names is withheld until one does, never given the dev setup; zcp writes no workflow file — Core deploys the application's environments from the repository's commits (`zeropsio/mate-hq:docs/architecture/development-and-delivery.md`). zcp `TestAWiredPairDeploysDirectlyAndIsNeverSentToPush`, `TestGitPushToHQ_RefusedForPlainGitOnABranch`, `TestGitPushFromAPairWiredToHQ_RefusedWhateverTheRemote`, `TestBuildBaseBranchCommand_DescendsFromMain`, `TestBaseBranchRefusalRemedy_TheNextAttemptWires`, `TestBuildGroupRecipe_GroupEnvironmentsBuildTheStageHalfsSetup`, `TestBuildGroupRecipe_StageSetup_ResolvedOrWithheld`, `TestComposeGroupRecipeInputs_JoinerWithoutStageSetup_BuildsTheYAMLsOtherSetup`. |
| MB-31 | In a Mate, HQ answering or not, launch-production refuses before it reads a scope or writes anything, and its next step names only a change the Mate's last-heard state says is still open; a merged one, or one that landed with another (MB-49), points at the projects page, and none, or one closed without merging, its work not on `main`, says the work goes to `main` first by a branch of the agent's own; with HQ not answering or refusing the key it says the answer is HQ's. A container that is not a Mate keeps its route. zcp `TestHandleLaunchProduction_AMateRefusesBeforeAnyStep` (the handler, Mate and not, linked and not: no SSH read, admin client, staged token or state file before the refusal), `TestLaunchProduction_AMateKeepingItsCodeInHQRefusesAndSaysWhatIsTrue` (the next step). |
| MB-33 | A Mate proposes only the recipe tiers `main` of the application's recipe repository in HQ lacks: a tier directory `main` has any file in is left whole, the group's first recipe lands whole, a top-level file `main` has is never proposed. The proposal is the Mate's own change in `group`, titled exactly "Mate: the group's import files" with no description, its branch `main`'s tree with the missing files written over it, so it only adds; it moves forward only — a changed composition on its head, with `main` as a second parent once `main` moved — and one `main` has overtaken is brought to `main`'s tree and adds nothing. A group whose `main` has every tier gets no change opened, and the agent's `group-recipe` answers that `main` already carries every tier. Every `buildFromGit` is the pair's repository in HQ. zcp `TestMissing_ATierTheRepositoryHas_IsLeftWhole`, `TestGroupRecipe_ProposedAsTheMatesChange`, `TestGroupRecipe_ProposedOntoAnUnbornMain`, `TestGroupRecipeAtStart_ReopensNothingAPersonDeclined`, `TestGroupRecipe_ProposesOnlyWhatMainLacks`, `TestGroupRecipe_FollowsTheProjectOnTheSameChange`, `TestGroupRecipe_MainMovedUnderAnOpenProposal`, `TestGroupRecipe_AProposalMainOvertookAddsNothing`, `TestGroupRecipe_OpensNothingMainAlreadyHas`, `TestHandleGroupRecipe_Table`. Narrowed by MB-44: once `main` has every tier, a tier zcp wrote from this Mate and nobody changed since is proposed anew, on the same change title, when the Mate's project changes shape. |
| MB-34 | A Mate's description of its change is its change's body in HQ: set on the open change of the branch named — by branch or a lease alias, or the repository's only open one; several open with none named is refused, listing each — on record or named by the Mate's state, never opened by describing — kept with the change in the ledger while HQ does not answer, with an ask to describe again, and never put on a change it was not written for (one that merged or closed first is named to the Mate and nothing is kept; words kept for a change that is gone are dropped). The pictures it names are the Mate's own kept screenshots, attached to the change once each and published as `<img alt width height src>` at HQ's address; a picture not kept for that change is refused before anything is written, and one HQ will never take writes nothing and drops the words (HQ refusing for now keeps them). zcp `TestDescribeChange`, `TestDescribeChange_WaitsWhileItsChangeIsHeld`, `TestDescribeChange_ByBranch`, `TestDescribeChange_SeveralOpenNoBranch_Refused`, `TestDescribeChange_Refusals`, `TestWorkflowTool_DescribeChangeReachesItsHandler`, `TestDescribeChange_Pictures`, `TestKeepBrowserPicture`. |
| MB-35 | A new Mate stands up from its application's AI Agent tier, read from HQ, in one call: every pair built from the application's repositories in HQ is read by zcp's naming, not its setups' names, adopted as the adopt route records it with the tier's setups, its checkout put on `main` of the repository the tier names (never one HQ would have to make), and deployed every dev half at once, the first call answering once they stand with the stages queued, and on the second call each stage from its dev half once what its build reads stands (with no reads, every stage above it by priority); a stage never called for stays `READY_TO_DEPLOY` — with nothing committed, pushed or proposed; the runtimes the browser is still importing are waited for, bounded; a refusal before anything is touched names the adopt route, a pair that fails stops alone with the model's next step and holds only the halves whose builds read it, which say what they waited for, and a second call skips what is done. Registered in a Mate only, and a Mate's AGENTS.md sends "Stand up development of the project." to it first, through the standup playbook. zcp `TestStandup_StandsUpEveryPairFromTheRecipe`, `TestStandupAfter_EveryDevHalfStartsAtOnce`, `TestStandupReads_FromTheRecipe`, `TestStandupReads_EveryRouteTheWriterCounts`, `TestStandup_ReturnsOnceDevelopmentIsUp`, `TestStandup_ASecondCallContinuesAndSkipsWhatIsDone`, `TestStandup_TheModelIsTheBackup`, `TestStandup_WaitsForTheRuntimesTheBrowserIsImporting`, `TestParseMateTier`, `TestParseMateTier_PairsOnlyRepositoriesOfThisApplication`, `TestParseRecipeImportShape_RolesFollowTheHostnameConvention`, `TestAdoptPair_RecordsWhatTheAdoptRouteRecords`, `TestServer_StandupToolGating`, `TestBuildAgentsMD_Container_StandUpRoutesToTheTool`; integration `TestStandup_OverMCP_AMateInNoApplicationIsRefusedAndNothingIsTouched`. |
| MB-37 | A group recipe writes each search engine at no less than 2 GB `minRam` and 0.5 GB `minFreeRamGB` on every tier and never lowers a value; an unread scale writes no block; `profileOverrides` and the free-memory buffer are carried; a scaling proposal is the Mate's change in the recipe repository in HQ, written over `main`, rewriting only that host's block in every tier naming it; it refuses a block it cannot splice safely, moves forward on the same change for the same host, brings a stale one to `main`'s tree when nothing differs, and is refused while the Mate's additive proposal is open there, which in turn leaves an open scaling proposal alone; the steer speaks only after a change that finished. `group_floors_test.go`, `host_scaling_test.go`, `group_profile_overrides_test.go`, `scale_test.go`; zcp `TestGroupRecipeScaling_SteersAndProposesOneHostsBlock`, `TestGroupRecipeScaling_ProposesFromMainsHeadEveryTierFresh`, `TestGroupRecipeScaling_OneOpenChangeInTheRecipeRepository`, `TestScratch_File`. |
| MB-38 | A Mate reads from its first turn, even on an empty project, that its code and changes live in its application's HQ, bootstrap comes first and ZCP wires the pair's repository once the service stands, Core deploys the application's environments, and the person is never asked for a GitHub or other repository, URL or token. The block is Mate-only; a plain container or local install reads none of it. zcp `TestBuildAgentsMD_Container_GitHostSaysWhereTheCodeLives`. |
| MB-39 | A person may merge an open HQ change while the agent works; a pass tells the agent what became of it — merged, bring `main` up to date and cut the next branch from it; closed, nothing of it is on `main` — and never changes the checkout; work meant for a change merged or closed meanwhile starts from an up-to-date `main` (MB-53); a closing session hands over only an open change; production guidance reads merged history from HQ, and a failed read stays unknown with a manual ask again (§10.10). zcp `TestReconcileHQRepositories_TellsTheMateWhatBecameOfItsChange`, `TestHQPass_LearnsAMergeWithoutChangingTheCheckout`, `TestSessionAnnotations_HandoffOnlyWithAnOpenChange`, `TestProductionKnowsMergedWorkThePairHoldsNoRecordOf`, `TestProductionGuidance_HQUnavailableIsUnknown`, `TestHQPass_MergedChangeNamesItsLeasesAndStages`. |
| MB-40 | Mate's preview pane frames a Mate's dev and stage, never the application's production: a Mate's AGENTS.md names Mate's exact origins and the `'self'` list and points at the preview-pane playbook, which has dev and stage drop `X-Frame-Options` and widen the app's own `frame-ancestors` by Mate's origins through Mate's preview variant of the stage's setup, never that setup; the group recipe's AI Agent tier keeps the variant and every group environment builds the setup it extends; a setting naming Mate's address alone is no framing. The allowance names Mate's exact origins — the hosted client and the container's own subdomain — never a zerops.app wildcard, and a page that forbids framing on purpose is the person's to open up (§10.10). `TestBuildAgentsMD_Container_PreviewPaneFramesDevAndStage`, `TestBuildAgentsMD_Container_PreviewPaneNamesMatesExactOrigins`, `TestDetectFrom_SubdomainOrigin`, `TestWithoutMateFraming_ExactContainerOrigin`, `TestBuildGroupRecipe_PreviewVariant_GroupEnvironmentsBuildTheSetupItExtends`, `TestBuildGroupRecipe_PreviewVariant_PartsAreNamedByTheSetupTheyBuild`, `TestBuildGroupRecipe_PreviewVariant_ASettingNamingMateIsNoFraming`, `TestBuildGroupRecipe_PoolMonorepoPreviewVariants_StageAndProductionBuildTheBases`, `TestGroupSetup_GSUndeclared_WithholdsTiersUntilDeclared`, `TestNamesZeropsSubdomainAddress_ReferencesInLabels_Count`. |
| MB-41 | A Mate holds an open change per branch: open-change names its change by branch, or by a lease alias or pair (pairs sharing a repository are one); a second branch opens its own change, the same branch is answered, a handed change is answered only for its branch, and past five open the refusal names them and offers folding the Mate's own first (MB-53). zcp keeps each change in its ledger, one file per change, never on a pair, moving the pairs' older records in once. Against an HQ that keeps one open change per repository (no `openPerBranch`) the answered change is kept on its own branch and nothing is recorded for the branch asked for; open-change forgets, and says once, a kept change the state it read names settled (§10.10); an open whose answer was lost is answered by the Mate's state read again — the change open for the branch, or "unknown", never "Nothing was opened" (§10.10). zcp `TestHQChangeLedger_OneFilePerChange`, `TestHQChangeLedger_ConcurrentWritersKeepEachOthersFields`, `TestHQChangeLedger_MigrationFromPairsDedupsAcrossPairs`, `TestResolveChange_LeaseAliasNamesRepoAndBranch`, `TestResolveChange_PairsSharingARepositoryAreOne`, `TestResolveChange_NeverTheNewest`, `TestOpenChange_SecondBranchOpensItsOwnChange`, `TestOpenChange_SameBranchAnswered`, `TestOpenChange_FromLeaseAlias`, `TestOpenChange_OldHQAnswersTheOpenOne_RecordsNothingForTheAskedBranch`, `TestOpenChange_TooManyOpen_NamesThem`, `TestOpenChange_PairsSharingARepositoryOpenAChangePerBranch`, `TestOpenChange_ForgetsTheChangesTheStateItReadNamesSettled`, `TestOpenChange_AnswerLost_AnswersTheChangeItOpened`, `TestOpenChange_AnswerAndStateLost_SaysUnknownNeverNothing`, `TestHandedChange_AnotherBranchOpensItsOwnChange`, `TestLearnChangeLanding_ReadsAMissingChangeByTheStatesWindow`. |
| MB-42 | What zcp says of a Mate's several open changes: a merged one's note names the leases on its branch, for the agent to release; another's standing — behind `main`, conflicting, a draft — is said once, when it differs from what was told, with a merge of `origin/main`, never a rebase; develop's start lists every open change the Mate's state names in the repository, recorded or not, a handed one labelled handed and each with its last update, and sends new work onto an open change or a branch of its own by MB-53; status shows one Changes line per repository, each change's standing read from the Mate's last-heard state (a merged one naming its leases to release), and no pair names a change; lease status names each group's change, and a lease on a merged change's branch says it merged — release it, or start new work from main — never "no change yet"; a lease release's result names each released branch's change the same way; the launch refusal and release handoff name each open change once, the handoff leaving out one the Mate's state, read once, names settled. AGENTS.md: one change per branch, and new work onto an open change or a new branch by *What one change holds* (MB-53); the dev-pool atom opens and describes a branch's change from a lease and covers parallel work — an independent piece opens its own change, subagents on one piece open none and the lead opens the change of their merged branch (§10.10). zcp `TestHQPass_MergedChangeNamesItsLeasesAndStages`, `TestHQPass_OtherChangeBehindAfterMerge_ToldOnce`, `TestHQPass_ConflictToldWithMergeNotRebase`, `TestHQChangeStanding_SaysOnlyWhatChanged`, `TestDevelopStart_ListsTheOpenChangesOnAnHQThatKeepsOnePerBranch`, `TestDevelopStart_ListsEveryOpenChangeOfTheMate_RecordedOrNot`, `TestStatus_OneBlockPerRepository`, `TestStatus_ChangesReadFresh_AsLastReadWhileHQIsDown`, `TestRenderStatus_ChangesBlockPerRepository`, `TestLeaseGroupChanges_NamesEachGroupsChange`, `TestLeaseStatus_MergedBranchSaysSo_NotNoChangeYet`, `TestLeaseRelease_ResultNamesTheReleasedBranchsChange`, `TestLaunchProduction_ListsEveryOpenChangeOnce`, `TestHandleRelease_NamesTheOpenChangesItLeavesOut`, `TestHandleRelease_LeavesOutTheChangesHQSaysSettled`, `TestBuildAgentsMD_Container_GitHostOneChangePerBranch`, `TestDevelopDevPoolAtom_OpensTheBranchsChangeFromALease`. |
| MB-46 | How a Mate keeps its open changes few. AGENTS.md, and for the rest the hq-git playbook it points at: new work is cut from another open change's branch only when it needs that change's code, saying it builds on #N — HQ squashes each change into one commit on `main`, so once the base merges the change on it can conflict with `main`, and merging `main` in is a push that leaves it a draft until described again; no change opens only to record another's merge or to update a plan, roadmap or log, which goes into the change doing the work or the next one that has work in it; a file every change appends to makes each open change conflict once another merges, so each change gets an entry of its own. The develop start of a pair wired to HQ: work *What one change holds* (MB-53) adds to the open change also spares a branch switch on the dev service, since its `/var/www` is what its dev server runs and what deploys read, so another branch checked out there changes what runs; another open change's branch — merging `main` in, folding a change into it — is updated in a worktree the dev service holds below `/tmp/wt-<branch>` (`zcp git <host> worktree add --detach /tmp/wt-<branch> origin/<branch>`, merged and pushed with `-C /tmp/wt-<branch>`, then `worktree remove`) — it shares the checkout's origin, and every git command runs in the dev service through `zcp git` (§6.8), never on the mount; `/tmp/wt-` is a namespace no older session's zcp-side `/tmp/<branch>` worktree takes — then pushed and removed. That worktree is no second served checkout: no dev server runs it, no deploy reads it, and it outlives no push, so it leaves §6.3's isolation unit as it is: a dev service's work stays on the one checkout it serves. zcp `TestBuildAgentsMD_Container_GitHostOneChangePerBranch`, `TestDevelopGitPushStartFromRemoteAtom_WiredPairRunsGitInItsDevService`. |
| MB-47 | A change HQ moved itself (mate-hq: right after a squash, HQ brings `main` into each open change built on the squashed change's branch by one merge commit authored `Core <core@hq.invalid>`, a fast-forward that keeps a described change described) carries `corePush` `{ at, from, head, landed, squash }` in the Mate's state, decoded as optional: an older HQ, or a change without it, reads as before. While `corePush.head` is the change's head, the pass says once, through the `told` ledger (MB-42) and before any other standing, "HQ brought main into #N after #L merged: fetch and fast-forward `<branch>` before your next push", with `zcp git <host> fetch origin && zcp git <host> merge --ff-only origin/<branch>` where the branch is checked out in the repository's dev service (its checkout or its `/tmp/wt-<branch>` worktree there) or over ssh in each lease of it; lease status names it for the group's change, read now or as last told; a lease sync refused as non-fast-forward names it as the cause, the Mate's state read once for that refusal. zcp never reads the agent's checkouts for it, so it is said once even where the branch was fast-forwarded already (§10.10). zcp `TestHQChangeStanding_CorePushAtHead_SaidOnce`, `TestHQPass_CorePushAtHead_ToldOnce`, `TestLeaseGroupChanges_CorePushAtHead_SaysItInEachLease`, `TestLeaseSyncRefusal_AfterHQsPush_NamesTheCause`. |
| MB-48 | A repository HQ mirrors to GitHub through the organisation's app is never synced by hand: the hq-git playbook says HQ pushes `main` there itself and a merged change reaches GitHub as its pull request, so never a push of GitHub's default branch nor a merged change copied onto it; the repository pass says once, through a told record per repository, when a repository's remote becomes the app's (`GET /api/apps/:appId/repos`, `via: "app"`) — its GitHub repository, that HQ alone pushes there, drop a note that says to push to GitHub or to sync it — and once per `divergedSince` that HQ and GitHub diverged, with the git commands of the one change that brings them back in step: a branch from GitHub's tip, fetched by GitHub's address with a read token, with HQ's `main` merged in, pushed to HQ, never to GitHub, squash-merged on GitHub when merged in Mate (§5.10). zcp `TestHQPass_GitHubMirror_SaidOnceWhenHQMirrorsTheRepository`, `TestHQPass_GitHubMirror_DivergenceSaidOnceWithTheRealignment`, `TestTellMirror_SaysEachOnce`, `TestClient_AppRepos_ReadsEachRepositorysRemote`, `TestClient_AppRepos_ReadsEveryPage`. |
| MB-49 | A change HQ closed because another change's landing carried every commit of it is on `main`, never "closed without merging" (mate-hq: a change built on it merged first, so that squash holds its work; HQ closes it and names the carrier). It carries `landedWith`, the carrier's number in the same repository, in the Mate's state — the link's `state.json` (§2.8) — decoded as optional: absent on every other change and from an older HQ, read only on a `closed` change, whose `mergedSha` and `landedHead` are null. Every reader that asks whether a change's work reached `main` takes it as it takes a merged one: the pass says once "change #N was merged as part of #M", all of its work on `main` through #M's merge, nothing lost and nothing to do again, with the merged note's next steps — `main` brought up to date and the next work's branch cut from it, the leases still on its branch — and forgets it; status, lease status and a lease release's result say "merged as part of #M" where they say "merged", with the same release-or-start-from-`main` words, never "no change yet"; the launch refusal points at the projects page (MB-31); a recipe proposal that landed with another change is no decline, and one landing after a closed proposal reopens the question as a merge does (§10.10). Its leases are released on completion as a merged or closed change's are (DP-34), the line saying it "was merged as part of #M". Only the check that an earlier merge superseded a closed recipe proposal reads `mergedSha`, so a change that landed with another, having none, is not among those merges. zcp `TestClient_Self_Changes_DecodeLandedWith`, `TestReconcileHQRepositories_TellsTheMateWhatBecameOfItsChange`, `TestObserve_Environments_CurrentChanges`, `TestLaunchProduction_AMateKeepingItsCodeInHQRefusesAndSaysWhatIsTrue`, `TestReleaseCompletedLeases_ByTheBranchsChange`. |
| MB-50 | HQ alone pushes to GitHub (§5.10 *HQ alone pushes to GitHub*, §6.8 *Tokens and access*): every wired pair's checkout's `origin` is its repository in HQ whichever way the person keeps the code, and a checkout an older zcp pointed at GitHub is moved back by the pass — the Mate's start or develop's — and the next develop start says once, from the move kept on the pair's record, as the hq-git playbook does, that HQ pushes on only to branches it put on GitHub — work on a branch the container pushed there itself is finished under a new branch name pushed to `origin`, as a new change, the earlier pull request superseded; zcp asks HQ for a GitHub token with `access=read` alone, from a relayed `zcp git` call and `zcp hq git-credential` alike, never `write` or `workflows` and never with no `access`; a `zcp git` push whose git asks for a GitHub repository is refused in zcp's words, no token asked, naming the push to `origin` only for the repository HQ mirrors the pair's code to; the realignment reads GitHub's default branch by its address with that read token and pushes to HQ. zcp `TestHQPairOriginTarget_EveryWayTheCodeIsKept_IsItsRepositoryInHQ`, `TestRepositoryPass_ACheckoutPointedAtGitHubMovesToHQ`, `TestMateStart_MovesACheckoutOffGitHub_DevelopStartTellsItOnce`, `TestBrokeredGit_Relay_PushToGitHub_RefusedWithoutAskingHQ`, `TestBrokeredGit_Relay_PushToAGitHubRepositoryHQDoesNotMirror_ToldNoPushToOrigin`, `TestBrokeredGit_Relay_Realignment_GitHubsTipReadByAddress_PushedToHQ`, `TestAnswerGit_GitHubRepository_AsksHQForAReadToken`, `TestDivergedWords_InALease_GitHubIsFetchedThroughZcpGit`. |
| MB-51 | Where the Mate's state says its application limits who merges (`mergeLimited`, §2.8), zcp never has the agent send its person to merge or release: each answer that names who merges, reviews or releases names someone who merges there, and the answers that hand work over add the one rule — the person may not be one of them, the agent tells them the change is ready and that they press "Ask to merge" on it in Mate, a change waiting for a merge is normal and work goes on (§10.10 *An application that limits who merges*). Without the field every answer is unchanged; the texts that cannot read the state — AGENTS.md, the atoms, `zerops_propose_release`'s description — name who merges or releases as a person, never as the Mate's own. Words only, so no test: the shared rules admit no case that asserts copy. zcp `internal/tools/hq_merge_limited.go`. |
| MB-52 | zcp words its guidance by what the Mate's state says its application has (`production`, `hasStage`, `mates`, §2.8), at four moments and nowhere else — a change opened, a change described and ready, develop's start, a change merged: with no production (said only beside a `mates`, which an older HQ does not send) merged work is never called live, deployed or released and the person is told merging keeps it on `main` and nothing is published until the project is put live; with a production each change stays one run of asks, anything beyond the interface in a change of its own, and its description calls out what could hurt production; where HQ releases to it (`zerops`, `git`) a merge does not publish, a release does; where it runs outside Mate (`outside`) zcp says Mate cannot tell what a merge publishes there, and the agent calls merged work neither live nor not live; a kind zcp does not know says nothing about production; with a Stage, the application's Stage runs `main` after a merge and is where it is checked; with more than one Mate new work starts from an up-to-date `main` that moves, each change one run of asks (§10.10 *What the application has*). Without the three every answer is unchanged, and no tool refuses or decides by them. Words only, but for when the clause on what a change holds is said: zcp `internal/tools/hq_application_has.go`, `TestApplicationWords_SayWhatAChangeHoldsBesideAProductionOrOtherMates`. |
| MB-53 | A change is what a person reviews, merges and would revert as one, split by blast radius and never by topic (§10.10 *What one change holds*): the person's run of asks goes into one change, described once a turn as a list, one item per ask, its undos and redos included so its diff is the net result; the next ask joins the Mate's open change only while it is the Mate's own, still open, with no comment, no ask to merge, nothing built on it and no other conversation or subagent of the Mate's working on it, the ask continues that change's run of asks or is unrelated interface work while the change was updated within about two hours, and its blast radius stays in the interface — what a person sees and how it behaves on the client; anything beyond it, anything large on its own, an urgent production fix, work to ship or revert by itself and another repository open their own; no size bound splits a run of asks, and never one growing change per person; the Mate folds its own such changes by the same conditions before review, closing the folded ones and telling the person; the person's word to keep apart or put together wins. AGENTS.md states it once and every other text refers to it. What zcp says by state: develop's start and status show each open change's last update; the develop close's note offers the Mate's own open changes in the lease's repository, never a handed one, and pool-migrate's retired answer offers none; the refusal past five open changes offers folding first, merging limited or not, and labels a handed change; open-change and develop's start say each change is one run of asks, anything beyond the interface apart, beside a production or other Mates only; develop's start labels a handed change. The Mate's state carries no comments, ask to merge or `buildsOn`, so nothing is selected by them. zcp `TestUnproposedLeaseBranches_OffersOnlyTheMatesOwnOpenChangesInTheRepository`, `TestPoolMigrateRetire_InAMate_OffersNoOpenChangeForTheMigrationBranch`, `TestTooManyOpen_OffersFoldingFirstWhetherOrNotMergingIsLimited`, `TestTooManyOpen_LabelsAHandedChange`, `TestApplicationWords_SayWhatAChangeHoldsBesideAProductionOrOtherMates`, `TestOpenChangesOf_MarksAHandedChangeAsHanded`, `TestChangeUpdatedWords_SaysHowLongAgoHQLastChangedIt`. |
| MB-43 | A Mate closes its own open change by branch, or a lease of it, never inferred: closed without merging in HQ, its branch and leases kept, forgotten by zcp; another Mate's is never asked for; an HQ without `openPerBranch`, or without the close route, is not asked or refuses, and the answer says a person closes it in its review (§10.10). zcp `TestCloseChange_OwnByBranch`, `TestCloseChange_OldHQ_SaysPersonCloses`, `TestCloseChange_NamesItsBranch`, `TestClient_CloseChange_PostsTheCloseOfTheMatesOwnChange`, `TestWorkflowTool_CloseChangeReachesItsHandler`. |
| MB-44 | A Mate's recipe describes its project as it is: the AI Agent tier gives each part its dev pool (swarm@1, `minContainers: 0`, its live maximum and scale, `sshIsolation` as written, no build, no setup, no credential) and its stage, and a dev-service pair whose repository's `main` in HQ holds the pool setup of every one of its parts a pool per stage (the dev service's vertical scale, the template's maximum 3, `sshIsolation` admitting the composing zcp service) while the composing Mate keeps its dev service — an application whose `main` holds no pool setup keeps its dev services; every runtime of every tier is typed, scaled and reached from its own part (its stage as it runs, else its setup's `run.base`) and named after it (`apistage` → `api`); a Mate's own dev tooling stays on the AI Agent tier; a pair's zerops.yaml is read from main of its repository in HQ when the mount has none. A tier zcp wrote from this Mate and nobody changed since (its mark intact and this Mate's, or only Core's squashes of this Mate's recipe proposals in its history) is proposed anew when its shape changes, and HQ lands it by itself unless its merge would change an environment made from a tier; one a person or another Mate wrote or changed never is; one of the same shape stays, and so does one whose header an older zcp worded otherwise. A recipe that cannot be proposed is a required step on the bootstrap response, a develop start and status, tried again on every pass. What serves the Mate itself — a runtime, a utility or a stage of a pair, the person marked (`mateOnly`) or, where they said nothing, wired to the address of a service of its own project no group environment runs (a dev half, its own infrastructure, a utility so wired; never another part's stage), a volume only such a runtime can be using — stays on the AI Agent tier, and one the person cleared is carried everywhere; a recipe change whose branch holds a commit zcp did not write is the Mate's or a person's, and zcp pushes nothing onto it (`TestBuildGroupRecipe_MateOwnServices_OnlyInTheAIAgentTier`, `TestBuildGroupRecipe_MateWiredStage_OnlyInTheAIAgentTier`, `TestBuildGroupRecipe_StageWiring_DecidesItsTiers`, `TestBuildGroupRecipe_MateWiring_OnlyAnAddressNoGroupEnvironmentRuns`, `TestBuildGroupRecipe_PersonsMateOnlyChoice_WinsOverZcpsRules`, `TestGroupRecipe_TheMateOnlyChoice_IsKeptAndEveryPassHonoursIt`, `TestGroupRecipe_AChangeSomeoneElseEdited_IsNeverPushedOver`, `TestGroupRecipe_PushesOnlyWhenItsCompositionChanged`). zcp `TestBuildGroupRecipe_PoolKeyedMonorepo_PoolAndStagePerPart`, `TestBuildGroupRecipe_Polyglot_EachRuntimeFromItsOwnPart`, `TestBuildGroupRecipe_DevOnlyUtilities_OnlyInTheAIAgentTier`, `TestGroupRecipe_ItsOwnTiersFromBeforeAPoolMigration_ProposedAnew`, `TestGroupRecipe_ATierAPersonOrAnotherMateWrote_IsNeverProposedOver`, `TestGroupRecipe_ItsOwnTiersWithTheSameShape_StayAsTheyAre`, `TestGroupRecipe_ItsOwnTiersUnderAnOlderHeader_StayItsOwnAndAsTheyAre`, `TestGroupRecipe_TheMateWebShape_RequiredUntilItComposes`. |
| MB-45 | A Mate's recipe follows a service change: after an import, a delete, a variable set, deleted or put in the vault as the organization's AI key, a pool migration's retire, or a deploy that reaches a stage — each answered without an error — and after any deploy or workflow call that moved zcp's records of the pairs, the recipe pass runs again off the call's path, once the calls have been quiet for 30 s (3 min at most from a burst's first), one pass per burst and one more for a call during a pass; it reopens no proposal a person closed unmerged, updates the Mate's one open recipe change, pushes nothing over an unchanged project and opens nothing over a main that carries the composition; a tier proposed anew is pushed again only for a new shape; its line goes to the log once and never into a tool's answer; a scale and public access ask for none. zcp `TestRecipeFollower_AfterACall_FollowsWhatTheRecipeReads`, `TestRecipeFollower_ABurst_IsOnePass`, `TestRecipeFollower_ACallDuringAPass_IsOneMorePass`, `TestRecipeFollower_ServerStopped_RunsNoPass`, `TestRecipeFollower_AnOutcome_SaidOnce`, `TestNewRecipeFollower_OutsideAMate_FollowsNothing`, `TestGroupRecipeFollow_ReopensNothingAPersonDeclined`, `TestGroupRecipeFollow_TheMatesOneChange`, `TestGroupRecipeFollow_MainCarriesTheRecipe_OpensAndPushesNothing`, `TestGroupRecipe_ATierProposedAnew_IsRewrittenOnlyForItsShape`, `TestBuildGroupRecipe_AnyInputOrder_SameBytes`, `TestFollowRecipe_ToolCall_ToldAsItStartsAndOnceItAnswered`, `TestNew_RecipeFollower_OnlyInAMate`. |
