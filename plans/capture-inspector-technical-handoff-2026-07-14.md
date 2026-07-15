# ZCP Capture + Capture Inspector — technical handoff

Date: 2026-07-14

Snapshot commit: `c07fb09d` (`feat/capture-raw-prototype`)

Worktree: `/Users/macbook/Documents/Zerops-MCP/zcp-capture-raw`

Base before this work: `8f1ef6f2` (`v9.126.0`, `origin/main` at the time of the work)

## 0. How to read this document

This handoff records four different kinds of information and keeps them
separate:

- **Contract** — behavior pinned by the authoritative spec or tests.
- **Implementation fact** — what the current code does, including incidental
  implementation choices.
- **Empirical observation** — what was measured on the four local capture
  bundles listed below.
- **Unmeasured/open** — an area for which this work selected no implementation
  or obtained no scale/compatibility evidence.

It intentionally does not rank future work or prescribe an architecture beyond
contracts already encoded in specs/tests. Current implementation choices are
reported so they can be understood; they are not presented as the only possible
continuation.

No capability URL, auth token, provider credential, prompt body, or captured
plaintext is included in this file.

---

## 1. Snapshot summary

The branch contains a raw capture system and a local forensic browser:

1. `zcp capture raw -- <command>` starts a scoped provider proxy around one
   child process.
2. `zcp capture on|off|status` manages a persistent capture daemon and a
   reversible Claude Code provider override.
3. ZCP MCP stdio records exact accepted stdin/stdout byte chunks when the
   capture environment is present.
4. Eval runners emit capture-only lifecycle identity and bundle their artifacts.
5. `zcp capture inspect` builds a text/JSON inspection report from canonical
   evidence.
6. `zcp capture ui` serves an embedded loopback-only browser over a versioned,
   read-only projection.
7. Session Story and Causal Flow Map reconstruct prompt/context/model/tool/result
   causality without placing plaintext in their metadata skeletons.

The production implementation remains in the main `zcp` binary. Capture
Inspector is isolated behind a narrow facade and nested Go `internal/` packages;
normal MCP startup has no dependency path into it.

The canonical `/usr/local/bin/zcp` was not replaced during this work. Local UI
verification used temporary binaries under `/tmp`.

### Git sequence

```text
8a6314b3 feat(capture): add persistent raw capture inspector
13daa3b7 feat(capture): add isolated forensic inspector UI
e1eaff09 fix(capture): use page scrolling for flow map
c07fb09d fix(capture): show flow details in fixed sidebar
```

The worktree was clean at `c07fb09d` before this handoff file was added.

---

## 2. Terms and identities

These identities are intentionally distinct:

| Term | Meaning in current implementation |
|---|---|
| Capture window | One scoped `capture raw` lifetime or one persistent `capture on → off` interval. |
| Capture/session ID | ID of the capture window and directory, e.g. `capture-20260713T204136Z-...`. It is not a Claude conversation ID. |
| Claude session ID | Client conversation identity extracted from supported Claude Messages request metadata. |
| Provider exchange | One proxy-assigned request/response pair, e.g. `exchange-000003`. |
| Eval run | One suite execution. |
| Scenario run | One scenario inside an eval run. |
| Invocation | One Claude process invocation inside a scenario. |
| Phase | `agent.initial`, `user-sim.N`, `agent.resume.N`, `retrospective`, etc. |
| MCP process/stream | One ZCP MCP server process and its stdio stream file. |
| Tool use ID | Provider/client tool call identity used for explicit proposal/result matching. |
| Provider message ID | Provider response identity; not used as a Claude session ID. |

A capture window may contain multiple Claude sessions, evals, invocations, and
unrelated exchanges. An invocation may share a Claude session with resume or
retrospective invocations. Missing identity stays unattributed; it is not
reconstructed from wall-clock proximity.

For the currently supported Claude Code path, the Claude session ID is parsed
from the JSON-encoded string at `metadata.user_id`, specifically its
`session_id` member. This is adapter-specific behavior, not a provider-neutral
identity contract.

---

## 3. End-to-end component graph

### 3.1 Recording and inspection graph

```text
Claude Code
  │
  ├─ HTTP/SSE via ANTHROPIC_BASE_URL
  │      ↓
  │  capture.ProxyServer
  │      ├─ forwards to one fixed upstream origin
  │      └─ queues provider JSONL records
  │
  └─ MCP stdio to zcp serve
         ↓
     capture.WrapMCPReader / WrapMCPWriter
         └─ queues MCP JSONL records

Eval runner / workflow renderer
  ├─ lifecycle markers over Unix control socket
  ├─ capture identity through environment variables
  └─ composition provenance side-channel

Canonical capture directory
  ├─ manifest.json
  ├─ provider.jsonl
  ├─ lifecycle.jsonl
  ├─ mcp/*.jsonl
  ├─ provenance/*.jsonl
  └─ eval/** artifacts
         ↓
  capture.InspectSession
         ↓
  captureinspector/internal/projection
         ↓
  captureinspector/internal/web + embedded SPA
```

### 3.2 Inspector package graph

```text
cmd/zcp/capture_ui.go
  → internal/captureinspector                 facade
    → internal/captureinspector/internal/web  private HTTP/UI implementation
      → .../internal/projection               private projection/detail readers
        → internal/capture                    canonical reader/inspection contract
```

Only `cmd/zcp/capture_ui.go` imports the facade outside the inspector subtree.
`internal/capture`, `internal/server`, `internal/eval`, `internal/workflow`,
`internal/tools`, and `internal/ops` do not import the inspector.

---

## 4. Source map

### 4.1 CLI and process composition

| File | Responsibility |
|---|---|
| `cmd/zcp/capture.go` | Dispatch for `on`, `off`, `status`, `raw`, `inspect`, `ui`, and internal `daemon`; scoped child wrapper; inspection CLI. |
| `cmd/zcp/capture_daemon.go` | Detached daemon startup/readiness and process identity plumbing. |
| `cmd/zcp/capture_ui.go` | UI argument parsing, `--active` manager lookup, random capabilities, signals, optional browser launch. Sole inspector facade importer. |
| `cmd/zcp/eval_capture.go` | `--capture raw` interception; attach to inherited/global capture or wrap the eval in scoped capture. |
| `cmd/zcp/eval_behavioral*.go` | Behavioral eval command surfaces. |

### 4.2 Canonical capture domain

| File | Responsibility |
|---|---|
| `internal/capture/recorder.go` | Generic non-blocking JSONL record queue, sequence assignment, visible gaps, provider record schema. |
| `internal/capture/proxy.go` | Loopback provider HTTP/SSE forwarding and provider records. |
| `internal/capture/mcp.go` | MCP stdio byte recorder and transparent reader/writer wrappers. |
| `internal/capture/lifecycle.go` | Synchronously durable eval/lifecycle marker stream. |
| `internal/capture/provenance.go` | Capture-only output/component hash and span provenance. |
| `internal/capture/manifest.go` | Running/terminal manifest and file inventory hashes; unclean recovery. |
| `internal/capture/runtime.go` | Owns recorder, proxy, lifecycle, manifest, control listener, and shutdown order for one window. |
| `internal/capture/control.go` | Token-protected Unix control socket for status, lifecycle marks, and shutdown. |
| `internal/capture/manager.go` | Persistent ON/OFF/BROKEN state machine, lock, journal, daemon reconciliation. |
| `internal/capture/claude_settings.go` | Prepare/apply/restore compare-and-swap patch for Claude settings. |
| `internal/capture/eval_bundle.go` | Copies known eval artifacts into the canonical capture inventory. |
| `internal/capture/inspect*.go` | Manifest/protocol validation, provider/MCP/eval/context/provenance derivation, filters, and text rendering. |

### 4.3 Inspector domain

| File | Responsibility |
|---|---|
| `internal/captureinspector/inspector.go` | Facade: `Config`, `Start`, `Server.URL`, `LaunchURL`, `Close`. |
| `internal/captureinspector/architecture_test.go` | Import, compiler-private layout, cold-import, process-side-effect, and stdout guards. |
| `.../internal/projection/types.go` | `zcp-capture-view-1` public JSON shape inside the private domain. |
| `.../projection/build.go` | Builds finalized, running-prefix, or invalid/degraded views. |
| `.../projection/provider.go` | Provider SSE event/block projection and paged event index. |
| `.../projection/mcp.go` | Framed MCP JSON-RPC reconstruction across raw chunk boundaries. |
| `.../projection/context.go` | Exact provider request/context detail. |
| `.../projection/client.go` | Eval stream-json/client runs, conversation metadata, and built-in tools. |
| `.../projection/raw.go` | Root scan, safe file resolution, paged raw records, artifacts, and tool detail. |
| `.../projection/trace.go` | Metadata-only Session Story and opaque reveal content refs. |
| `.../projection/flow.go` | Deterministic four-lane causal flow. |
| `.../projection/metrics.go` | Dimensioned metric catalog. |
| `.../projection/graph.go` | General evidence graph edges. |
| `.../projection/compare.go` | Pairwise metric delta projection. |
| `.../internal/web/server.go` | Loopback HTTP service, auth/reveal, REST handlers, process-local view cache. |
| `.../web/assets/index.html` | Static application shell. |
| `.../web/assets/app.js` | Framework-free SPA rendering and interaction state. |
| `.../web/assets/style.css` | Embedded strict-CSP stylesheet. It is currently minified into one line. |
| `.../browsertest/` | Optional Playwright dependency and smoke harness, outside production Go dependencies. |

### 4.4 Eval and MCP integration points

| File | Responsibility |
|---|---|
| `internal/server/server.go` | Calls `NewMCPRecorderFromEnvironment`; wraps MCP transport reader/writer; closes with complete/partial status. |
| `internal/server/transport_test.go` | Pins MCP writer/stdout semantics and clean EOF status. |
| `internal/eval/capture.go` | Capture-only MCP config, lifecycle markers, artifact bundle, scoped environment. |
| `internal/eval/behavioral_capture_phases.go` | `user-sim.N` and `agent.resume.N` invocation boundaries. |
| `internal/eval/behavioral_run.go` | Initial agent and retrospective integration. |
| `internal/workflow/bootstrap_guide_provenance.go` | Calls composition provenance without changing rendered output. |

---

## 5. Canonical capture write path

### 5.1 CLI modes

Current usage is printed by `cmd/zcp/capture.go`:

```text
zcp capture on [--label <name>] [--upstream <url>] [--listen <host:port>]
zcp capture off
zcp capture status
zcp capture raw [flags] -- <command> [args...]
zcp capture inspect <session-dir> [--view summary|timeline|context|all]
                    [--eval <id>] [--scenario <id>] [--invocation <id>]
                    [--format text|json]
zcp capture ui [<session-dir>] [--root <capture-root>] [--active]
               [--listen <loopback:port>] [--no-open]
```

Default local paths and raw-mode flags:

```text
capture root:       ~/.local/state/zcp/captures
manager state/logs: ~/.local/state/zcp/capture-runtime
Claude settings:    ${CLAUDE_CONFIG_DIR:-~/.claude}/settings.json
control socket:     <os.TempDir()>/zcp-capture-<uid>/control.sock
raw overrides:      --output-dir, --listen, --upstream
```

`zcp capture ui` uses the same default capture root. A scoped `capture raw` can
select another root with `--output-dir`; persistent manager state uses the paths
above.

`capture raw`:

- creates a unique capture ID;
- starts `capture.Runtime` in the same process;
- sets `ANTHROPIC_BASE_URL` and capture environment variables for the child;
- forwards SIGINT/SIGTERM to the child's process group;
- preserves the child's shell-style signal exit convention;
- finalizes the manifest with child exit status.

`capture on`:

- starts a detached daemon;
- verifies provider and control readiness/identity;
- journals a Claude settings restore patch before applying it;
- configures new Claude processes to use the capture proxy.

There is no hot attachment to an already-running Claude process.

### 5.2 Generic record queue

`internal/capture/recorder.go` defines the shared `capture.Record` format and
record kinds:

```text
session.start / session.end / capture.gap
provider.request.start / body / end
provider.response.start / body / end
provider.exchange.error
mcp.stream.start / stdin.chunk / stdout.chunk / stdin.error / stdout.error / stream.end
```

Body bytes are base64 encoded in JSONL. Each record receives a monotonically
increasing local sequence and UTC observation time.

The recorder has one buffered channel (default capacity 512) and one writer
goroutine. Protocol-facing `Record` calls attempt a non-blocking enqueue; they do
not perform file I/O. On overflow:

- `Record` returns `ErrCaptureGap`;
- the dropped sequence range, record count, and body byte count are accumulated;
- a later `capture.gap` record is emitted when capacity exists;
- terminal `complete` is downgraded to `partial` if any gap occurred.

Close writes the terminal record, flushes the buffered writer, fsyncs, and closes
the file. Write errors are returned at close and cause runtime status downgrade.

Capture directories use mode `0700`; canonical files use `0600`.

### 5.3 Provider proxy

`internal/capture/proxy.go` implements a fixed-upstream loopback proxy:

- default listener: `127.0.0.1:0`;
- default upstream: `https://api.anthropic.com`;
- only HTTP/HTTPS upstream schemes;
- redirects are relayed and not followed;
- automatic response compression is disabled;
- request URL path/query is joined onto the configured fixed upstream;
- hop-by-hop and `Connection`-nominated headers are removed as appropriate;
- SSE response chunks are flushed to the client immediately after each write.

The request body is read into memory before the upstream request is made. It is
recorded in 64 KiB record chunks with an aggregate byte count and SHA-256. The
response is read in 32 KiB transport reads, recorded, written to the client, and
flushed for SSE. The aggregate response byte count and SHA-256 are recorded at
terminal response status.

The proxy records HTTP entity bytes observed through Go `net/http`; it is not a
packet, TLS-record, transfer-framing, or raw socket capture.

Headers are structurally allowlisted. Authorization, cookies, and arbitrary
headers are not recorded. Recorded request headers include content negotiation,
Anthropic version/beta, content type, user agent, selected client headers, and
`x-stainless-*`. Recorded response headers include content type/encoding, date,
request IDs, and rate-limit families. Request/response bodies remain plaintext
capture evidence.

Unsupported response content encoding does not alter proxy forwarding. It
becomes an explicit inspection error later. Inspection currently supports
identity and gzip, including a compatibility lane for gzip magic bytes with a
missing encoding header.

### 5.4 MCP stdio capture

`NewMCPRecorderFromEnvironment` is disabled when both required environment
values are absent. A half-configured environment is an error. If any eval scope
field is present, all four eval scope fields are required together.

The MCP recorder creates `mcp/zcp-<pid>.jsonl` with `O_EXCL` and records:

- exact successful stdin-read prefixes after the underlying read;
- exact successful stdout-write prefixes accepted by the underlying writer;
- original stream offsets and per-chunk hashes;
- non-EOF I/O errors;
- whole-stream input/output byte counts and hashes at close.

The wrappers return the delegate's exact `(n, err)` values. Capture errors are
stored separately and do not replace protocol I/O results. The server treats
`nil`, cancellation, EOF, and `server is closing: EOF` as clean completion;
other server errors produce partial MCP capture status.

The MCP transport writer passed to the SDK is a no-op closer so SDK teardown
does not close real stdout. Transport tests pin both the supplied writer and
close behavior.

### 5.5 Lifecycle stream

`lifecycle.jsonl` is a low-volume, synchronously written and fsynced side
channel. It is deliberately separate from the provider queue. Supported markers
are:

```text
lifecycle.stream.start / lifecycle.stream.end
eval.run.start / eval.run.end
eval.scenario.start / eval.scenario.end
eval.invocation.start / eval.invocation.bind / eval.invocation.end
eval.artifact
```

Marker validation requires the identity fields appropriate to each kind. A bind
requires eval run, scenario, invocation, phase, and Claude session ID. Marker
failure is printed as a capture warning by the eval runner but does not change
the evaluated agent's control flow or result.

### 5.6 Composition provenance

`RecordCompositionFromEnvironment` is active only under complete capture opt-in.
It receives the exact rendered output plus owner spans, computes:

- whole-output byte count and SHA-256;
- each component's kind, owner, start/end offsets, and SHA-256.

The output string itself has `json:"-"` and is not duplicated into provenance
JSONL. Exact text remains in provider/MCP evidence. Provenance records append to
`provenance/zcp-<pid>.jsonl`, fsync, and never return replacement output bytes.

### 5.7 Manifest and bundle

The current manifest format is `zcp-capture-1`; the inspector also contains core
compatibility for the initial prototype format where applicable.

A running manifest is written before observed work starts and initially has an
empty file inventory. Finalization inventories regular files relative to the
session root, recording kind, path, size, and SHA-256. Kinds are:

```text
provider, lifecycle, mcp, provenance, eval
```

Finalization inventories `provider.jsonl`, optional lifecycle, sorted MCP and
provenance files, and files under `eval/`. It writes terminal time/status and an
optional child exit code atomically through a temporary file and rename.

`RecoverUncleanSessionManifest` inventories the durable prefix after daemon
loss and marks the capture unclean. A valid hash inventory does not imply
completeness.

Canonical bundle shape:

```text
<capture>/
├── manifest.json
├── provider.jsonl
├── lifecycle.jsonl
├── mcp/zcp-<pid>.jsonl
├── provenance/zcp-<pid>.jsonl
└── eval/<eval-run>/<scenario-run>/**
```

### 5.8 Runtime shutdown

`capture.Runtime` owns the provider recorder, proxy, lifecycle recorder,
manifest, and control socket. Close order is bounded:

1. inspect component capture errors;
2. close control server;
3. drain/shut down provider proxy;
4. cancel runtime context;
5. close lifecycle stream;
6. close provider recorder;
7. finalize manifest.

Any component failure downgrades requested complete status to partial and is
joined into the returned error.

### 5.9 Persistent manager and Claude settings

Manager state format: `zcp-capture-manager-1`. Reconciled public states are
`OFF`, `ON`, and `BROKEN`; the journal also has internal `enabling` and `on`
phases.

The manager serializes lifecycle operations with an advisory flock. Enable:

1. rejects unreconciled existing manager/socket state;
2. refuses to infer through an existing loopback `ANTHROPIC_BASE_URL` unless
   upstream is explicit;
3. writes pending manager state;
4. starts the daemon and verifies control identity;
5. prepares the exact Claude settings patch;
6. journals that patch before applying settings;
7. atomically applies settings and commits ON state.

Installed Claude settings are:

```text
ANTHROPIC_BASE_URL
ZCP_CAPTURE_SESSION_ID
ZCP_CAPTURE_SESSION_DIR
```

Restore only changes values still equal to the ZCP-installed values. Concurrent
user edits are preserved and surfaced as warnings. Disable first restores
settings, then requests daemon shutdown, then uses identity-checked TERM/KILL
fallbacks, recovers an unclean manifest when necessary, removes owned socket and
state, and reports OFF.

`status` reconciles journal format/phase, process liveness, installed settings,
and control endpoint identity. A dead daemon with installed routing is BROKEN,
not ON.

### 5.10 Capture environment

Current environment keys:

```text
ZCP_CAPTURE_SESSION_ID
ZCP_CAPTURE_SESSION_DIR
ZCP_CAPTURE_CONTROL_SOCKET
ZCP_CAPTURE_CONTROL_TOKEN
ZCP_CAPTURE_EVAL_RUN_ID
ZCP_CAPTURE_SCENARIO_RUN_ID
ZCP_CAPTURE_INVOCATION_ID
ZCP_CAPTURE_INVOCATION_PHASE
ZCP_CAPTURE_UPSTREAM_BASE_URL       CLI upstream default override
ZCP_CAPTURE_DAEMON_TOKEN            daemon startup plumbing
ANTHROPIC_BASE_URL                  provider routing
```

Control tokens are process/control-plane data and are not provider request
fields.

---

## 6. Eval integration

`zcp eval ... --capture raw` has three paths:

1. inherited capture environment exists: run inside it;
2. persistent manager has an active capture: run inside it;
3. neither exists: recursively invoke the current executable under scoped
   `zcp capture raw` and finalize afterward.

Global capture is also discovered automatically by eval runner setup even when
the explicit flag is absent.

For capture runs, eval writes a strict MCP config containing exactly one server
named `zerops`, pointing to the current executable with `serve`. It passes
`--strict-mcp-config`; capture does not create a duplicate observer server.

The lifecycle hierarchy is:

```text
eval_run_id
└── scenario_run_id
    └── invocation_id
        ├── phase
        └── claude_session_id (after explicit bind)
```

The initial invocation starts before Claude's session ID is known. After the
stream-json `system.init.session_id` event, eval emits `invocation.bind`.
Resume/retrospective paths can bind a known session before launch. User
simulation has its own Claude sessions and `user-sim.N` phases. Resume phases
are `agent.resume.N`.

`captureMCPConfig` and the scoped environment carry eval identities only to the
MCP recorder. Provider payloads are not augmented.

Known artifact copies include scenario source, task/retrospective prompts,
transcript, retrospective stream, self-review, metadata, and available
verification artifacts. The copied files are then inventoried by the terminal
manifest.

---

## 7. Core inspection (`internal/capture`)

### 7.1 Entry points

- `InspectSession(sessionDir) (*InspectionReport, error)`
- `FilterInspection(report, InspectionFilter)`
- `RenderInspection`, `RenderInspectionSummary`, `RenderTimelineInspection`,
  `RenderContextInspection`

CLI views are `summary`, `timeline`, `context`, and `all`; output is text or
JSON. Filters select eval/scenario/invocation while preserving linked evidence.

### 7.2 Validation order

The implemented contract validates before deriving claims:

1. manifest format/lifecycle declarations and safe relative paths;
2. every inventoried file type, size, and SHA-256;
3. record sequence continuity, gaps, and terminal status;
4. provider request/response and MCP whole-stream counts/hashes;
5. explicitly supported content encoding and framing;
6. provider, MCP, lifecycle, context, source, and eval correlations.

A missing terminal record, hash mismatch, unsupported encoding, malformed
framing, or unsupported manifest format is explicit. A partial/unclean capture
can be hash-valid but never renders integrity OK.

Core inspection retains a legacy filename-discovery lane when `manifest.json`
is absent. The browser projection does not use that lane because its `Build`
entry point requires a manifest.

### 7.3 Derived report

`InspectionReport` contains:

- integrity/completeness and warnings;
- provider exchange count and unattributed count;
- Claude sessions;
- model context snapshots and lineage;
- eval run/scenario/invocation hierarchy;
- MCP streams;
- provider tool use ↔ MCP call/result ↔ provider tool result correlations;
- source/provenance matches.

Context lineage compares normalized messages while ignoring nested
`cache_control` metadata. Shorter history is a reset; changed same/larger history
is a rewrite. System and tool-schema changes are tracked independently.

Usage fields carry an explicit observed bit. A provider-reported zero is
separate from a missing field.

### 7.4 Tool correlation

Provider tool proposals are decoded from SSE block order and explicit tool-use
IDs. MCP JSON-RPC is reconstructed across arbitrary chunk boundaries. The
projection includes requests, responses, and notifications, not only tool calls.

Current tool result propagation states are:

- `exact` — provider-visible result bytes equal the observed MCP result;
- `different` — both exist but differ;
- `missing` — completed MCP result has no observed provider result;
- `ambiguous` — incomplete evidence prevents a unique claim.

`different` preserves source bytes, provider-context bytes, and signed delta.
The recovery capture currently demonstrates 1163-byte source, 1623-byte target,
and `+460 B` client-added content.

Current corpus matching and capture-time composition matching are separate.
Neither is used to overwrite canonical result content.

---

## 8. Versioned browser projection

Projection format: `zcp-capture-view-1`.

The browser consumes projection JSON, not the Go layout of `InspectionReport` as
a public API. Primary types live in
`internal/captureinspector/internal/projection/types.go`.

### 8.1 `Build` modes

`projection.Build(ctx, sessionDir)` has three paths:

#### Finalized and valid

- calls `capture.InspectSession`;
- adds manifest file declarations;
- reads provider/MCP/lifecycle/provenance records;
- projects provider events/blocks, client artifacts, hierarchy, sessions,
  contexts, MCP, tools, graph edges, metrics, and timeline;
- sorts timeline and returns a full `View`.

#### Finalized and invalid

If core inspection fails after the manifest can be read, `buildInvalidView`
returns a manifest-declaration-only view:

- `integrity.state = invalid`;
- diagnostic contains the validation error;
- manifest-declared file/size/artifact metadata remains visible;
- most derived metric values are set to null with missing count;
- plaintext and raw detail routes are disabled by the web layer.

#### Running

`buildRunningView` discovers provider, lifecycle, MCP, and provenance files and
decodes only complete JSON objects from their durable prefixes. A partial
trailing line becomes a structural diagnostic. It does not claim final hashes or
completeness and is not cached by the web server.

Running projection currently provides structural raw/timeline/provider-prefix
information only. It does not run the finalized correlation path, so sessions,
model contexts, tools, Session Story, and manifest-inventory-based detail reads
may be absent or return an error until finalization. No browser acceptance matrix
for a running capture was added in this work.

### 8.2 Main `View`

The `View` includes:

- capture and integrity summaries;
- overview counters;
- eval hierarchy, sessions, client runs, conversation metadata;
- provider exchanges, blocks, contexts;
- tools, MCP calls/processes;
- source owners, timeline, raw file/index metadata, artifacts;
- diagnostics, metrics, and general evidence edges.

`ProviderEvents` is held server-side with `json:"-"`; only its total appears in
the main view and items are returned by a paged endpoint.

The initial cross-file raw record projection is capped at 5000 records. Raw
record pages are capped at 1000. Provider event pages are capped at 2000.
Plaintext previews for raw bodies, artifacts, tool content, trace content, and
provider events are generally capped at 1 MiB; context request detail is capped
at 4 MiB.

### 8.3 Metrics and compare

A metric carries:

```text
id, name, unit, scope, nullable value, nullable denominator,
sampleCount, missingCount, evidenceBasis, description, evidence
```

Unknown is represented as null/missing, not zero. Provider usage, client timing,
client cost, request wire bytes, response wire bytes, context bytes, MCP bytes,
and derived counts remain separate dimensions. The current real views contained
more than one hundred metrics (112 were observed during implementation).

`projection.Compare` aligns metric IDs and returns left/right/delta/percent plus
missing counts and evidence basis. It is a metric comparison only; it does not
currently compare causal operation sequences or produce semantic grades.

### 8.4 Root scan and file safety

`ScanRoot`:

- resolves the root;
- does not follow directory symlinks;
- scans at most depth 6, 10,000 directories, and 1000 captures;
- reads manifest metadata without building body projections;
- supports nested eval-run directory layouts;
- sorts newest first;
- returns an error if two manifests declare the same capture ID.

The duplicate-ID behavior avoids selecting one ambiguous by-ID directory. Its
current effect is that one duplicate makes root listing/lookup fail until the
ambiguity is removed.

Detail readers accept only manifest-inventoried relative paths. `resolveFile`
rejects absolute/parent escape, symlink traversal, outside-root resolution, and
non-regular files.

### 8.5 Cache and post-projection mutation

The web server caches finalized `View` values in process memory. The cache key
contains:

- session path;
- `FormatVersion1`;
- manifest bytes;
- each declared file path plus current size, mtime, and mode.

Running views are always rebuilt. The cache map currently has no explicit entry
or byte limit.

Detail readers re-open canonical files after the view may have been cached.
`inventoriedFile` therefore rechecks the selected file's size and computes its
full SHA-256 against the manifest before returning the path. This catches
post-projection modification even if size and mtime were preserved. The reader
then opens/decodes the file again, so a large detail read currently has at least
a hash pass plus its decode/read pass.

---

## 9. Session Story

`BuildSessionTrace(sessionDir, view, TraceFilter)` produces a metadata-only
`SessionTrace` for one session and optionally one invocation.

### 9.1 Selection and ordering

- If only invocation is supplied, the lifecycle hierarchy supplies its session.
- If no scope is supplied, the first projected session is selected.
- Provider exchange order supplies the trace backbone.
- Explicit lifecycle scope supplies phase boundaries where available.
- No matching exchange is an error.

### 9.2 Step construction

Trace step kinds include phase, prompt, model text, thinking, tool, context, and
other provider blocks. Steps carry sizes, status, scope IDs, evidence refs, and
correlation basis but no plaintext body.

Prompt bodies are read server-side to classify them. The metadata output uses
generic titles and opaque content refs. Classification currently hides by
default:

- `<system-reminder>` client reminders;
- session-title generation prompts/responses;
- duplicate normalized user prompts;
- thinking;
- transport/technical blocks;
- initial technical context rewrite in the narrow initialization case.

Prompt deduplication hashes normalized content with SHA-256; the hash is not
sent as prompt text. `Story` filters `hiddenByDefault`; `Detailed` restores it.
Focus filters are Everything, Important only, and Tools only.

### 9.3 Opaque content refs

Content ref forms are:

```text
request:<base64url exchange>:<message index>:<block index>
response:<base64url exchange>:<block index>:<decoded start offset>
tool:<base64url tool id>:arguments|result|provider-result
```

`ReadTraceContent` resolves one ref only after reveal authorization. It returns a
bounded value, content kind, format candidates, truncation flag, and evidence
coordinate. Trace skeletons and flow metadata do not contain body text.

Current implementation detail relevant to scale: building a trace calls
`ReadContextDetail` for selected exchanges. `ReadContextDetail` hashes and reads
the whole `provider.jsonl` and then selects one exchange. This is repeated for
multiple exchanges. Session Trace is generated per request and is not currently
cached or paginated.

---

## 10. Causal Flow Map

`buildSessionFlow` transforms the metadata trace plus the view into deterministic
lanes, turns, nodes, edges, phases, and summary counts.

### 10.1 Lanes and turns

Fixed lane order:

```text
1 User input
2 Model context
3 Claude
4 Tools
```

Each selected provider exchange with a context snapshot becomes one turn.
Turn/node/edge IDs are deterministic strings derived from exchange/tool/step
coordinates. No force-directed or random layout is used.

### 10.2 Nodes

Per turn the builder can add:

- visible and technical prompt nodes;
- one context node;
- one aggregated model-turn node;
- zero or more tool nodes;
- a result boundary node when result context is missing, ambiguous, or outside
  selected scope.

Context node dimensions are system, tool schemas, messages, metadata/other, and
new messages. Model node dimensions are model text, thinking, tool arguments,
and response wire bytes. Tool nodes retain execution ID, propagation, timing,
result bytes, and evidence.

### 10.3 Edges and evidence basis

Current edge kinds and bases:

| Kind | Basis |
|---|---|
| `prompt-input` | provider request message |
| `provider-request` | provider exchange |
| `tool-proposal` | provider content block order |
| `context-carry` | normalized prefix equality |
| `observed-next-request` | provider exchange order; hidden by default |
| `tool-result` | explicit tool/result correlation basis from the inspection report |

Tool-result edges preserve source, target, and delta observed flags separately.
`missing` and `ambiguous` can point at explicit result-boundary nodes rather than
claim a next-context join. Phase bands use explicit lifecycle evidence when an
invocation exists and provider-session order otherwise.

### 10.4 Browser layout and interaction

`app.js` computes a deterministic lane layout with a canonical 1160-unit SVG
viewBox. CSS sets the SVG to `width: 100%; height: auto` inside
`width: min(1160px, 100%)`. The map does not own vertical or horizontal scroll;
the document scrolls through its full evidence height.

Current flow presentations:

- Cards;
- Flow map;
- Split.

Selecting a node or keyboard-selecting an edge opens a fixed right sidebar
without changing map width or document scroll. Current CSS details:

```text
summary sidebar: position fixed, z-index 45, top 66px, right/bottom 0,
                 width min(560px, 94vw), overflow auto
expanded detail: width min(760px, 96vw)
viewport <=950: top 0
```

The sidebar receives focus with `preventScroll`. It closes through its close
button or Escape. Closing a Split sidebar changes presentation back to Flow map
so an empty fixed sidebar does not remain. Sidebar content owns detail scrolling;
formatted `pre`/JSON blocks are configured not to add nested vertical
scrollbars.

Full context detail has Overview/System/Tools/Messages/Raw request tabs. Full
tool detail has Overview/Arguments/Tool result/Model context/Difference/Sources.
A Back action replaces expanded detail with the selected node summary. Opening a
canonical raw record clears the flow inspector before using the global drawer.

---

## 11. Web service and API

### 11.1 Start/lifecycle

The facade receives only explicit configuration:

```go
type Config struct {
    ListenAddr      string
    CaptureRoot     string
    SessionDir      string
    CapabilityToken string
    RevealToken     string
}
```

Importing the package starts nothing. `Start` validates an existing capture root,
optional pinned manifest, loopback listener, and non-empty capabilities before
creating an HTTP server. Context cancellation or `Close` shuts down only that
server.

`zcp capture ui` owns signal handling and browser process launch. `--active`
obtains the current session directory from the manager, closes that connection,
and then starts the read-only inspector. `--active` cannot be combined with
`--root`. The viewer does not create a missing capture root.

### 11.2 Routes

All routes except launch require the capability cookie. Reveal-gated routes also
require the reveal cookie.

| Route | Content class |
|---|---|
| `GET /launch/{token}` | One-time capability exchange, then redirect. |
| `GET /`, `/assets/{name}` | Embedded application assets. |
| `GET /api/v1/captures` | Manifest-derived capture index. |
| `GET /api/v1/captures/{id}/view` | Metadata projection; provider event payload array omitted. |
| `GET /api/v1/captures/{id}/session-trace` | Metadata-only trace/flow skeleton. |
| `GET /api/v1/captures/{id}/provider-events` | Paged event metadata. |
| `GET /api/v1/captures/{id}/records` | Paged raw record coordinates/metadata. |
| `GET /api/v1/compare` | Metric deltas. |
| `POST /api/v1/reveal` | Exact-origin JSON body `{ "confirm": "REVEAL" }`. |
| `GET .../trace-content` | Reveal-gated prompt/model/thinking/tool content. |
| `GET .../provider-event` | Reveal-gated SSE payload. |
| `GET .../raw` | Reveal-gated raw record/body preview. |
| `GET .../context` | Reveal-gated exact provider request. |
| `GET .../artifact`, `artifact-line` | Reveal-gated eval artifacts. |
| `GET .../tool` | Reveal-gated MCP or built-in tool detail. |

A finalized invalid view is returned by `/view`, but `inspectableView` returns
HTTP 409 for its detail/trace paths. Running views are allowed through that gate,
subject to the running projection limitations described above.

### 11.3 Local security

- listener must be loopback (`localhost` or loopback IP);
- every request Host must also be loopback;
- launch capability is constant-time compared and accepted once;
- capability and reveal cookies are HTTP-only and SameSite Strict;
- reveal POST requires exact `Origin == server.URL()`;
- request body is limited to 64 KiB and unknown JSON fields are rejected;
- responses set no-store, no-referrer, nosniff, frame deny, and strict CSP;
- CSP is `default-src 'self'`, explicit self script/style/connect, no objects,
  no base, no framing;
- no CDN, telemetry, or external runtime asset;
- no API writes canonical capture evidence.

The launch URL necessarily contains a capability until the one-time redirect.
The SPA then replaces browser history with `?capture=<capture-id>`; plaintext is
not placed in URL/history.

---

## 12. Frontend implementation

The frontend is framework-free HTML/CSS/JavaScript embedded with `go:embed`.
There is no production Node dependency or asset build step.

`app.js` holds one in-memory `state` object and re-renders `#app` with escaped
HTML strings. `esc()` is used for captured strings. Dynamic geometry and bars use
SVG attributes or native `<progress>` elements; generated markup contains no
inline `style` or inline event handlers.

Main tabs currently are:

```text
Session story, Capture index, Overview, Hierarchy, Timeline, Provider SSE,
Client / eval, Model context, Tools, MCP, Sources, Metrics, Artifacts, Raw,
Compare
```

Plaintext is fetched lazily after reveal. Rich rendering supports:

- multiline text with real line breaks;
- a bounded markdown-like renderer;
- syntax-colored, bounded, collapsible JSON trees;
- JSON encoded inside strings (`nested-json` candidate);
- raw source modes;
- explicit tool-result difference rendering.

The global non-flow evidence drawer is fixed at z-index 50. Flow sidebar is
z-index 45. Flow raw opening clears the sidebar before opening the global drawer;
the current UI does not intentionally stack the two detail surfaces.

The application uses native `confirm` for reveal and `alert` for request errors.
There is no client router; tab/selection state is in memory, while the selected
capture ID is reflected in query state.

---

## 13. Isolation and non-interference enforcement

### 13.1 Compiler/package boundary

Projection and web live under:

```text
internal/captureinspector/internal/
```

Go therefore prevents packages outside `internal/captureinspector` from
importing them.

### 13.2 AST architecture tests

`internal/captureinspector/architecture_test.go` scans the repository and
inspector source. It enforces:

- legacy flat `internal/captureview` and `internal/captureui` do not exist;
- only `cmd/zcp/capture_ui.go` imports the facade outside the subtree;
- inspector imports only stdlib, itself, and `internal/capture`;
- production inspector files have no `init()`;
- no package-level call initializer;
- no `os/exec` or `os/signal` production imports;
- no process-global environment/chdir/exit/stdout/stderr use;
- no direct builtin/fmt/default logger output.

The test includes synthetic violations to prove its scanners fire.

### 13.3 Depguard

`.golangci.yaml` has:

- `core-not-capture-inspector` — denies facade imports from core and all commands
  except the exact CLI adapter;
- `capture-inspector-allowlist` — strict stdlib/self/capture allowlist for the
  inspector subtree.

### 13.4 Runtime consequence

No listener, goroutine, file read, environment read, browser launch, or cache is
created by importing the facade. Runtime activation starts only through explicit
`captureinspector.Start`, reached by `zcp capture ui`.

The production binary-size comparison recorded at the isolation checkpoint was
30,889,490 bytes, +24,160 bytes (+0.08%) versus the pre-isolation same-UI build.
This is an empirical build result, not a format/API guarantee.

---

## 14. Tests and verification state

### 14.1 Unit/integration coverage relevant to capture

Canonical capture tests cover:

- exact provider request/response bytes and credential-header absence;
- early SSE forwarding;
- redirects not followed;
- hop-by-hop/connection header removal;
- queue overflow and visible gaps;
- write failure at close;
- private permissions;
- MCP exact byte transparency and whole-stream hashes;
- lifecycle validation and control auth;
- manifest running/final/unclean behavior;
- manager transaction, rollback, stale/unowned socket, dead daemon, idempotence;
- context reset/rewrite and observed-zero semantics;
- provider/MCP/result correlation;
- manifest hash mismatch before parsing;
- missing terminal, partial/unclean, unsupported format/encoding;
- source/provenance matches.

Projection/web tests cover:

- complete, invalid, partial, and running-prefix views;
- deterministic metadata-only flow;
- propagation difference and context rewrite;
- bounded initial raw projection;
- all provider SSE event/block types without payload leakage;
- MCP JSON-RPC framing across chunks;
- symlink parent escape;
- capability/reveal and summary plaintext exclusion;
- invalid detail disablement;
- post-cache tampering with preserved size/mtime;
- cache identity changes from manifest/evidence state;
- Host, Origin, traversal, and non-loopback rejection;
- no CSP-inline requirements.

### 14.2 Browser harness

Location: `internal/captureinspector/browsertest/`.

It is optional and test-only. Install/run instructions:

```bash
cd internal/captureinspector/browsertest
npm ci
npm run install-browser
cd ../../..
go test -tags=captureinspector_browser \
  ./internal/captureinspector/internal/web \
  -run TestBrowserSmoke -count=1
```

The tagged Go test creates a finalized synthetic capture, starts the real web
server, and invokes Playwright. Current assertions include:

- one-time capability leaves the current URL;
- raw detail is 403 before reveal;
- Cards/Flow/Split render;
- keyboard edge selection;
- fixed sidebar is immediately in the viewport;
- deep node selection preserves `window.scrollY` and focuses sidebar;
- Escape closes sidebar;
- Split close removes the fixed sidebar;
- map owns no horizontal/vertical scrolling;
- context detail does not execute captured hostile markup;
- no inline styles;
- no nested content scrollbars in flow detail;
- no stacked global drawers;
- no page overflow at 1024/2560;
- no page/console errors.

Screenshots produced by this harness are plaintext after reveal and are written
to a temporary/output directory excluded from git.

### 14.3 Latest verification commands

The following passed on this branch during the final UI rounds:

```bash
go test ./... -short -count=1
go test -race ./internal/captureinspector/... ./internal/server -count=1
go vet ./...
make lint-fast
./bin/golangci-lint run ./internal/captureinspector/...
node --check internal/captureinspector/internal/web/assets/app.js
node --check internal/captureinspector/browsertest/smoke.cjs
go test -tags=captureinspector_browser \
  ./internal/captureinspector/internal/web \
  -run TestBrowserSmoke -count=1 -timeout=2m
git diff --check
```

The race/vet gate preceded the last asset-only sidebar/page-scroll commits; the
last commits changed JavaScript, CSS, docs, and browser assertions, not
production Go control flow. Full short tests, fast lint, inspector lint, JS
syntax, tagged browser test, and real-capture browser traversal were rerun after
the current sidebar behavior.

A full non-fast repository-wide golangci run was not the acceptance gate.
Earlier branch runs exposed historical `contextcheck`, `noctx`, `goconst`, and
related findings outside the inspector packages; focused inspector lint is
clean.

---

## 15. Real capture corpus used for acceptance

These directories are local/gitignored evidence and are not fixtures committed
to the repository:

| Scenario directory | Capture ID | Inventoried bundle bytes | Exchanges | Claude sessions | MCP streams |
|---|---|---:|---:|---:|---:|
| `tmp/container-capture-runs/weather-dashboard-classic-dev-20260713-201710/` | `capture-20260713T201710Z-e86d3eff2115` | 6,787,744 | 21 | 1 | 2 |
| `tmp/container-capture-runs/recipe-first-deploy-race-adopt-20260713-202225/` | `capture-20260713T202225Z-b2b3c274ac8f` | 10,239,899 | 62 | 11 | 12 |
| `tmp/container-capture-runs/greenfield-node-postgres-dev-stage-20260713-202847/` | `capture-20260713T202847Z-521ce47fc17f` | 12,099,627 | 33 | 1 | 2 |
| `tmp/container-capture-runs/recover-failed-buildfromgit-missing-dep-20260713-204136/` | `capture-20260713T204136Z-43c2b5d2b55e` | 9,314,205 | 28 | 1 | 2 |

Totals: 144 provider exchanges, 14 observed Claude sessions, 18 MCP streams.
All four manifests are complete and passed local re-inspection.

Current Story-density browser counts:

| Capture | Cards | Flow nodes | Flow edges | Error nodes | Different edges | Rewrite/reset edges | Phases |
|---|---:|---:|---:|---:|---:|---:|---:|
| recovery | 52 | 81 | 104 | 1 | 1 | 1 | 2 |
| greenfield | 70 | 107 | 120 | 0 | 0 | 1 | 2 |
| recipe/adopt | 49 | 58 | 65 | 0 | 0 | 10 | 12 |
| weather | 38 | 57 | 71 | 0 | 0 | 1 | 2 |

The fixed-sidebar test selected nodes after document scroll positions of roughly
2782–5862 px across these captures and asserted no scroll jump, fixed positioning,
and focus transfer.

Observed on these bundles, main views were roughly 0.6–1.1 MiB and session trace
responses roughly 267–464 KiB. Trace generation was roughly 0.5–1.3 seconds.
These are measurements on 6.8–12.1 MiB bundles, not scale bounds.

`tmp/container-capture-runs/README.md` contains historical behavioral notes. Its
statement that an errored recovery result was rendered as missing describes a
bug found during the runs; current code distinguishes it correctly and the
recovery flow now shows an error tool node plus a `DIFFERENT +460 B` edge.

---

## 16. Behavioral findings represented by the corpus

These are observations from the captured agent runs, not Inspector verdicts:

- Weather: classic/no-managed-dependency route was selected, but mode guidance
  led the agent to submit simple mode despite an iteration intent; subdomain
  enable required a recovery call.
- Adoption: agent waited for active builds and adopted existing services without
  import/delete/redeploy; user simulation continued through ten social turns to
  max iterations.
- Recipe/greenfield: recipe import/develop/dev/stage path completed without MCP
  errors; composition provenance identified render owners.
- Recovery: fixture did not exhibit the documented missing-Postgres state; agent
  destructively replaced the service and produced a healthy Flask app instead.
  Execution completed, but that does not imply semantic success for the scenario.

Capture integrity, process execution, eval review, and semantic scenario outcome
remain separate concepts. The UI intentionally does not combine them into one
score.

---

## 17. Current implementation limits and unmeasured areas

The following are status statements. No implementation direction is selected in
this handoff.

### 17.1 Scale

- No 250 MiB or 1 GiB bundle benchmark has been run.
- Finalized `Build` reads full record streams into slices and accumulates provider
  response entities for projection.
- Context and trace readers can hash/read `provider.jsonl` repeatedly.
- `ReadToolDetail` invokes full `capture.InspectSession` again for MCP details.
- Session Trace is generated as one response, without pagination or cache.
- The process-local finalized view cache has no size/entry eviction.
- Initial raw and provider-event indexes are bounded, but all projection
  collections are not virtualized in the browser.

### 17.2 Time precision

Historical gzip response chunks do not prove per-SSE-event wire timestamps.
Projected SSE events use decoded order/offset and a timestamp basis such as
`response-entity-end`; wall clock is annotation, not causal ordering.

### 17.3 Client compatibility

- Claude Code Messages HTTP/SSE plus Claude stream-json is the verified client
  lane.
- Codex and pi do not have working capture adapters in this branch.
- Provider-neutral equivalence is not claimed.

### 17.4 Lifecycle/process behavior

- No hot attach to running Claude processes.
- `capture on` affects new processes.
- Reboot survival has no launchd/systemd supervisor.
- Clients surviving `capture off` are outside the first contract.

### 17.5 Browser/test corpus

- The committed browser fixture is finalized and synthetic.
- Running/partial/invalid captures have Go projection/server tests but no
  committed screenshot matrix.
- There is no committed sanitized multi-capture fixture set for every retry,
  parallel-tool, propagation, reset/rewrite, unmatched, or retrospective case.
- Screenshots are smoke artifacts, not pixel-golden visual regressions.

### 17.6 Current compare surface

The Compare tab aligns metrics. There is no implemented structural comparison of
session turn order, tool additions/removals, context lineage, or propagation
changes.

### 17.7 Local service behavior

- Capability launch is one-time; a browser that loses its session cookie needs a
  restarted UI process/new capability.
- Exact reveal Origin is the printed listener origin. Manually switching between
  `localhost` and `127.0.0.1` changes the origin and reveal will fail.
- Duplicate capture IDs fail the whole root scan rather than yielding per-entry
  conflicts.
- A missing capture root is an error at web `Start`; the UI command does not
  create it.

### 17.8 Evidence read races

Detail reads verify a file hash and then reopen/decode it. This prevents stable
post-projection tampering but does not provide an OS-level immutable file handle
across verification and all subsequent parsing. Canonical bundles are treated
as immutable local evidence by contract.

---

## 18. Contracts that are easy to accidentally violate

These statements are encoded in the current spec/tests and are listed as
technical constraints, not future product direction:

- Raw provider/MCP/lifecycle/provenance files are canonical; derived views are
  disposable.
- Capture must not alter provider entity bytes, MCP `(n, err)`, streaming order,
  tool results, or control flow.
- Queue overflow and capture write failures remain visible; they are not solved
  by backpressuring observed traffic.
- Credential headers are structurally absent, but body content is plaintext.
- Partial/unclean can be hash-valid but never integrity OK.
- Missing or ambiguous identity/evidence remains explicit.
- Sequence and explicit IDs establish causality; wall-clock time annotates.
- Request wire, response wire, model-context, tool result, and provider-result
  bytes are separate dimensions.
- Unknown metric values remain null/missing, not zero.
- Execution success is not semantic success.
- Summary/trace/flow metadata must not contain plaintext body content.
- Thinking is reveal-gated and hidden by default.
- Invalid finalized captures expose only degraded declaration metadata and no
  plaintext/raw details.
- Flow links are asserted only from their declared evidence basis.
- The embedded app runs under `style-src 'self'` without inline styles or event
  handlers.
- Inspector has no import/runtime activation path from normal MCP server startup.

---

## 19. Areas for which this snapshot has no selected continuation

The branch and plans mention, but do not implement or select an approach for:

- scale behavior beyond the current 6.8–12.1 MiB corpus;
- trace pagination, collection virtualization, or an external analytical store;
- cache eviction policy;
- provider/client adapters beyond Claude Code;
- exact future capture-time SSE timestamp instrumentation;
- deterministic committed fixtures for the full causal-state matrix;
- pixel-based visual regression infrastructure;
- session-level causal regression comparison;
- service supervision across reboot;
- semantic grading.

No ordering among these areas is implied here.

---

## 20. Operational entry points for another agent

### 20.1 Repository state

```bash
cd /Users/macbook/Documents/Zerops-MCP/zcp-capture-raw
git status --short
git log -5 --oneline
```

Expected feature head before this handoff document is `c07fb09d`.

### 20.2 Read first

```text
CLAUDE.md
CLAUDE.local.md (when present; it is absent in this snapshot worktree)
docs/spec-capture-inspector.md
docs/spec-architecture.md
plans/capture-inspector-technical-handoff-2026-07-14.md
plans/capture-inspector-gui-2026-07-13.md
plans/capture-inspector-session-trace-2026-07-13.md
plans/capture-inspector-v1-isolation-2026-07-13.md
plans/research/capture-inspector-roadmap-2026-07-13.md
```

The authoritative current behavior is the spec plus code/tests. Plans contain
historical design context and may describe earlier package names or intermediate
UI layouts.

### 20.3 Build a temporary binary

```bash
go build -o /tmp/zcp-capture-inspector ./cmd/zcp
```

This does not change `/usr/local/bin/zcp`.

### 20.4 Start the UI on the real local corpus

```bash
/tmp/zcp-capture-inspector capture ui \
  --root tmp/container-capture-runs \
  --no-open
```

Use the printed launch URL once. Do not place that URL in committed docs/logs.
A prior temporary UI process may exist; `/tmp/zcp-ui.pid` was used during manual
verification, but no work should depend on that PID still being live.

### 20.5 Inspect one canonical capture directly

```bash
/tmp/zcp-capture-inspector capture inspect \
  tmp/container-capture-runs/recover-failed-buildfromgit-missing-dep-20260713-204136/capture \
  --view all
```

### 20.6 Focused verification

```bash
go test ./internal/capture/... -count=1
go test ./internal/captureinspector/... ./cmd/zcp -count=1
go test -race ./internal/captureinspector/... ./internal/server -count=1
go vet ./...
make lint-fast
./bin/golangci-lint run ./internal/captureinspector/...
node --check internal/captureinspector/internal/web/assets/app.js
node --check internal/captureinspector/browsertest/smoke.cjs
git diff --check
```

Optional tagged browser test requires the install steps in
`internal/captureinspector/browsertest/README.md`.

### 20.7 Release/install constraints from local policy

No `make release`, `make release-patch`, `make install`, remote canonical binary
replacement, or `/usr/local/bin/zcp` mutation was performed for this checkpoint.
This session's operator constraint required explicit approval for those
operations. No `CLAUDE.local.md` is present in this snapshot worktree; a
receiving environment may supply its own local policy.

---

## 21. Primary references

- Authoritative contract: `docs/spec-capture-inspector.md`
- Package map: `docs/spec-architecture.md`
- GUI design/implementation history:
  `plans/capture-inspector-gui-2026-07-13.md`
- Session Story/Flow design history:
  `plans/capture-inspector-session-trace-2026-07-13.md`
- Isolation decision and acceptance:
  `plans/capture-inspector-v1-isolation-2026-07-13.md`
- Longer roadmap/research:
  `plans/research/capture-inspector-roadmap-2026-07-13.md`
- Behavioral operation:
  `eval/behavioral/README.md`
- Real local capture summary:
  `tmp/container-capture-runs/README.md`
