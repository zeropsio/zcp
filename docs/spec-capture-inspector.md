# Capture and Inspector

## 1. Purpose

ZCP capture records the two protocol boundaries needed to explain agent
behaviour:

1. the model client ↔ provider HTTP/SSE boundary; and
2. the model client ↔ ZCP MCP stdio boundary.

The raw bytes at those boundaries are canonical evidence. Inspection, session
indexing, eval grouping, context reconstruction, and source attribution are
derived views and must remain regenerable.

Capture is developer-only, local, opt-in, and plaintext. Provider authorization
and cookie header values are never recorded. Request and response bodies can
contain prompts, source code, tool inputs, and tool results and therefore live
under private user-only permissions.

## 2. Product invariants

1. **Raw is immutable.** Provider, MCP, and lifecycle JSONL files are append-only
   during capture and never rewritten by inspection.
2. **No observer effect.** The proxy does not change provider entity bytes,
   status, streaming order, MCP stdin/stdout bytes, guidance, tool results, or
   agent control flow.
3. **No silent loss.** Queue overflow, disk/write failure, unsupported framing or
   encoding, incomplete streams, daemon restart, and abrupt termination are
   visible before interpretation.
4. **Completeness and hash validity are distinct.** A partial or unclean capture
   can have valid hashes but can never be rendered as integrity `OK`.
5. **Every derived claim has evidence.** Views identify the raw file, sequence
   range, exchange/process, byte/decoded offset, timestamp, and equality basis.
6. **Credential headers are structurally absent.** Body-level identifiers and
   content remain plaintext evidence; inspection does not claim they were
   redacted.
7. **Client-specific signals are versioned observations.** If a supported client
   stops emitting an identity signal, records remain captured and are shown as
   unattributed rather than heuristically assigned.

## 3. Capture lifecycle

`zcp capture on`, `off`, and `status` form a serialized state machine. The
manager reconciles its journal against the real client configuration, daemon
control endpoint, process identity, and provider listener.

### 3.1 States

- **OFF** — no ZCP-owned provider override is installed, no owned capture daemon
  or listener exists, and no capture manifest is open.
- **ON** — the supported client configuration points at a health-checked,
  loopback-only ZCP listener owned by the recorded daemon; its capture window is
  open.
- **BROKEN** — configuration, journal, daemon, or listener disagree. `status`
  reports every disagreement and returns non-zero. `on`/`off` reconcile only
  changes demonstrably owned by ZCP.

The commands are idempotent and protected by an advisory lifecycle lock.

### 3.2 Enable transaction

1. Acquire the lifecycle lock, reconcile stale state, and durably journal the
   enabling transaction.
2. Create a private capture window and start the detached daemon.
3. Wait for a control-plane readiness response and verify daemon identity and
   the provider listener.
4. Prepare the exact compare-and-swap restore patch and journal it before any
   client setting changes.
5. Atomically install the client provider override and MCP capture coordinates.
6. Commit `ON` state.

The client configuration is never pointed at an endpoint that failed readiness.
Any pre-commit failure rolls back both configuration and daemon. Once a starter
returns a PID, the manager journals all returned ownership coordinates before
further checks. Rollback attempts authenticated control shutdown, then bounded
exit wait, identity-checked TERM, bounded wait, identity-checked KILL, and a
final exit check. The enabling journal remains `BROKEN` until exit is proven;
it is never deleted while an owned process may still be alive.

Filesystem locking, process liveness, process-group setup, and signal behavior
are platform adapters. Unix preserves session/process-group semantics. Windows
uses a native file lock and process query, and deliberately retains `BROKEN`
state rather than killing a PID when the capture-ID command identity cannot be
proved through a stable API.

### 3.3 Disable transaction

1. Acquire the lifecycle lock.
2. Restore/remove only settings that still equal ZCP's installed values. Preserve
   unrelated and concurrently edited settings.
3. If configuration restoration fails while it still points at ZCP, keep the
   proxy alive and return an error.
4. Stop accepting new traffic, bounded-drain active exchanges, close lifecycle
   and provider recorders, and finalize the manifest.
5. Stop the owned daemon and verify its process and listener are gone.
6. Commit `OFF` state.

Clients started while capture was on must be stopped before `off`; hot attach
and clients surviving disable are outside the first compatibility contract.

### 3.4 Daemon failure

A dead daemon with an installed provider override is `BROKEN`, never `ON`.
Capture fails closed: ZCP does not silently bypass an enabled recorder. A future
supervisor may restart a daemon, but every process epoch and resulting evidence
gap must remain explicit.

## 4. Client adapters

The core owns daemon/storage lifecycle. An adapter owns only reversible client
configuration and compatibility extraction.

The first adapter is Claude Code:

- provider routing uses `ANTHROPIC_BASE_URL`;
- persistent interactive routing is installed through Claude's user settings,
  not by editing shell startup files;
- ZCP eval runners also inject the active endpoint into spawned Claude
  processes because evals may use an isolated `HOME`;
- exact prior settings are restored on disable;
- the compatibility report records the observed Claude version.

Codex and pi require separate base-URL/protocol adapters and are not represented
as working merely because the Claude transport works.

## 5. Capture windows, Claude sessions, and eval scopes

One `on → off` interval is a **capture window**. It can contain multiple
interactive sessions and eval runs.

For observed Claude Code Messages requests, `metadata.user_id` is a JSON-encoded
string containing `device_id`, `account_uuid`, and `session_id`. The derived
Claude adapter parses `metadata.user_id.session_id`. Provider responses inherit
the session through the proxy exchange identity. Account and device identifiers
are not needed for grouping and are not printed in normal views.

The session signal is adapter-specific. Missing or malformed metadata produces
an unattributed exchange and a compatibility warning.

### 5.1 Eval lifecycle side channel

The eval runner emits capture-only lifecycle records to the daemon. It never
adds a provider header or body field.

Identity hierarchy:

```text
eval_run_id
└── scenario_run_id
    └── invocation_id
        ├── phase
        └── claude_session_id
```

Required phases include `agent.initial`, `user-sim.<n>`,
`agent.resume.<n>`, and `retrospective`.

For a fresh invocation the runner emits `invocation.start`, starts Claude, reads
`system.init.session_id`, and then emits `invocation.bind`. Late binding is
intentional: inspection joins already-recorded exchanges after the run. Resume
and retrospective invocations can bind the known session before launch.
`invocation.end` bounds the phase. The same Claude session may therefore have
several non-overlapping invocation phases.

MCP process evidence carries the same invocation identity through a
capture-only launch side channel. Provider and MCP protocol bytes remain
unchanged.

Missing lifecycle/bind evidence does not invalidate raw provider bytes. It marks
the eval annotation incomplete. Unrelated sessions remain visibly
`unattributed`; they are never silently folded into an eval.

## 6. Eval operation

When global capture is `ON`, ZCP eval commands discover it automatically,
inject its endpoint into all Claude subprocesses (including user simulation),
and register lifecycle scopes.

`zcp eval behavioral ... --capture raw` is a convenience contract:

- if global capture is on, attach to that window;
- otherwise create a scoped window before the eval and close it afterward;
- never start a second competing proxy.

A suite has one eval-run identity, each scenario has one scenario-run identity,
and every Claude process invocation is marked independently.

The capture bundle copies and hashes the scenario source, task prompt,
transcript, retrospective, self-review, metadata, and available verification
artifacts under an eval-owned directory. The top-level manifest inventories raw
and eval files. Logical eval views reference canonical raw sequence ranges; they
do not rewrite or delete unrelated evidence.

## 7. Canonical bundle

```text
<capture-window>/
├── manifest.json
├── provider.jsonl
├── lifecycle.jsonl
├── mcp/
│   └── zcp-<pid>.jsonl
└── eval/
    └── <eval-run-id>/
        └── <scenario-run-id>/
            ├── scenario.md
            ├── task-prompt.txt
            ├── transcript.jsonl
            ├── retrospective.jsonl
            ├── self-review.md
            └── meta.json
```

Every canonical file is regular, relative to the capture directory, hashed in
the terminal manifest, and private to the user. Derived output is optional and
regenerable.

## 8. Inspection

Inspection validates, in order:

1. supported manifest format and lifecycle consistency;
2. every inventoried file size/hash and safe relative path;
3. record sequence continuity, explicit gaps, and terminal status;
4. request/response and MCP stream byte counts and hashes;
5. declared content encoding and protocol framing;
6. only then, provider/MCP/lifecycle parsing and correlation.

Primary views are:

- summary/completeness;
- Claude sessions and unattributed exchanges;
- eval run → scenario → invocation timeline;
- model tool-use → MCP call/result → provider tool-result causality;
- model-context composition and request-to-request delta;
- exact current-corpus source matches and capture-time composition provenance.

Unsupported encoding or framing is an explicit inspection error. A finalized
bundle that fails hash, sequence, framing, or protocol validation opens only as
an `INVALID` manifest-declaration view: no raw body or plaintext detail endpoint
is enabled and derived metrics remain unknown. Because finalized views may be
cached while detail queries re-open canonical files, cache identity includes
non-restorable file status-change identity (or a content digest where the OS
lacks it), and every selected detail file is size/hash-verified against the
current manifest immediately before it is read. A same-size rewrite with a
restored mtime invalidates the cached projection; a valid cached view never
authorizes post-projection tampering. Hidden
thinking content is not printed by default; views expose its type, size, and raw
evidence location.

### 8.1 Versioned evidence projection

Browser and future query consumers use `zcp-capture-view-1`, produced by the
compiler-private `internal/captureinspector/internal/projection` package. They do
not consume the Go JSON layout of
`InspectionReport` as an API contract. The projection contains capture/eval/
session hierarchy, provider exchanges, context snapshots, MCP processes and all
framed JSON-RPC methods, causal MCP and built-in tool executions, stream-json
event metadata, source ownership, artifacts, structural diagnostics, metrics,
and raw evidence references.

Projection IDs are deterministic from canonical coordinates. Explicit graph
edges name their source/target entity and join basis. A reference names its
manifest-inventoried file and record/line range and, where available, exchange,
stream/decoded offset, byte length, and observation time. MCP result propagation
is one of `exact`, `different`, `missing`, or `ambiguous`. `exact` requires
equality of the complete canonical result structure: all content blocks and
fields plus error state, with a provider string normalized to one text block.
Display-text extraction is never the equality basis. `different` includes
client-added, omitted, or structurally changed content and is not rendered as
missing evidence. Unknown or ambiguous identity remains explicit.

Every provider SSE data event and content-block type is indexed by decoded
stream order/offset without returning its text. Existing gzip captures cannot
prove a per-event wire timestamp from compressed chunks, so the projection says
`response-entity-end` rather than presenting that time as exact. Event payload
(including thinking) is a separate reveal-gated query. Request-to-request
message lineage ignores only nested `cache_control` transport metadata and
separates a shorter history reset from a same-size/larger history rewrite.

The summary projection contains sizes, types, names, counts, statuses, and
coordinates, not provider bodies, transcript lines, tool arguments/results, or
thinking text. Those values are loaded by separate detail queries only after a
plaintext reveal gate. `GET /api/v1/captures/{id}/session-trace` returns the
metadata-only card and flow skeleton for a selected session/invocation;
`GET /api/v1/captures/{id}/trace-content` is reveal-gated and resolves one
opaque content reference.

Every projected metric has an ID, unit, scope, optional value and denominator,
sample count, missing count, evidence basis, and description. The initial
catalog exposes more than one hundred independent integrity, volume, timing,
context, token/cache, tool/MCP, client/eval, and provenance dimensions. A
missing provider usage field is `null`, never zero. Provider-reported usage,
client-reported cost/timing, exact wire bytes, and derived counts remain
distinct. There is no single health or quality score.

### 8.2 Local browser inspector

The supported entry points are:

```text
zcp capture ui
zcp capture ui <capture-directory>
zcp capture ui --root <capture-root>
zcp capture ui --active
zcp capture ui --no-open
```

The UI is embedded in the ZCP binary and uses no CDN, external asset, telemetry,
or write endpoint. The server binds only an explicitly validated loopback
address on a random port by default. It is separate from the provider proxy and
capture control socket. Capture-root discovery is bounded, does not follow
symlinks, and supports nested eval-run directories. A duplicate capture ID makes
by-ID evidence identity ambiguous, so root discovery fails loudly rather than
silently selecting one directory or inventing a canonical copy. The same rule
covers an explicitly pinned session outside the scanned root: a root entry with
the pinned ID must resolve to that same canonical directory or startup/index/
by-ID lookup fails instead of mixing two copies.

A random launch capability is accepted once and exchanged for an HTTP-only,
SameSite cookie. Host validation, exact-origin checks on reveal, CSP,
`frame-ancestors 'none'`, `nosniff`, no-referrer, and no-store headers protect
the local service. Full raw records, model context, artifacts, conversation
lines, thinking, and tool details require an explicit `REVEAL` confirmation and
a second random HTTP-only cookie. Authorization/cookie provider headers remain
structurally absent, but reveal copy warns that body content is otherwise
plaintext.

The browser exposes capture index, integrity/overview, scope hierarchy,
multi-lane timeline, provider SSE, client/eval events, model-context delta and
exact request detail, causal tools, MCP protocol, source provenance, metric
workspace, artifacts, raw evidence, and pairwise comparison. Large SSE and raw
record indexes use bounded pages. Detail previews have explicit size limits and
truncation markers. Comparison preserves unknown values and reports dimensions
independently; it is not semantic grading.

#### Session story and causal flow

The default human session inspector is a separate projection from the forensic
timeline. It selects one verified Claude session (or an explicitly labeled
unattributed stream), preserves invocation phase boundaries, and offers three
presentations over the same evidence:

- `Cards` is the readable prompt/model/tool/result story;
- `Flow map` is a deterministic four-lane causal map;
- `Split` keeps the map visible beside the selected node's exact dimensions and
  story details.

The flow lanes are user input, model context, Claude turns, and tools. Each
provider exchange forms one ordered turn. A context node reports the exact
request size and separate system, tool-schema, message, other, and newly added
message dimensions. Context-to-context edges form a visual context river;
reset and rewrite boundaries interrupt or warn on that river. Provider response
blocks are grouped into a model-turn node while tool proposals remain explicit
branches. Results rejoin only at a provider request proven by tool-use/result
correlation.

Flow edges are metadata-only and carry deterministic IDs, byte dimensions,
correlation basis, and evidence references. `EXACT` result propagation is solid,
`DIFFERENT` is yellow with the signed byte delta, and `MISSING` or `AMBIGUOUS`
is dashed. A missing bind is never drawn as a proven connection. Provider stream
sequence determines vertical ordering; wall-clock timestamps annotate nodes but
do not determine layout. The layout is lane-based rather than force-directed,
so identical evidence produces identical nodes and edges. The map viewport does
not own a vertical or horizontal scrollbar: its SVG fits the available width,
expands to the full evidence height, and leaves scrolling to the document.

Story density hides title generation, client system reminders, repeated prompts,
thinking content, and transport-only links without deleting them. Detailed
density restores those nodes. Selecting a node or edge can highlight its bounded
causal neighborhood and opens exact sizes, correlation basis, evidence
coordinates, and reveal-gated content in a fixed right sidebar. The sidebar is
inserted into the current viewport and focused without scrolling or reflowing
the map, so selection deep in a long flow is visible immediately and preserves
the document position. Sequence replay advances by projected evidence order,
not simulated wall-clock time. The map itself contains no prompt, response,
argument, result, or thinking plaintext before `REVEAL`.

Flow detail uses one inspector surface at a time. Opening full context, tool, or
evidence detail replaces the selected-node summary in that inspector and offers
a Back action; it does not stack a second drawer over the first. Opening a
canonical raw record first closes the flow inspector. The reveal-gated context
workspace separates Overview, System, Tools, Messages, and Raw request.
Multiline strings render with actual line breaks, JSON strings containing JSON
are decoded into bounded collapsible trees, tool schemas and tool arguments are
syntax-colored, and raw encoded source remains explicitly available. The
inspector owns vertical scrolling; formatted blocks do not create nested
scrollbars.

The embedded application must satisfy its strict `style-src 'self'` CSP without
inline style attributes or event handlers. Dynamic bars and coordinates use
SVG attributes or native progress elements rather than weakening CSP.

Projection caches are disposable and live outside canonical capture files (the
current implementation uses process-local memory). Inspection and UI operations
are read-only and never update the manifest or JSONL streams.

### 8.3 Running captures

A running manifest is displayed as `RUNNING / NOT FINALIZED`. Only complete JSONL
records from the durably readable provider, lifecycle, MCP, and provenance
prefix are projected. A partial trailing line is diagnosed and ignored until it
is complete. No hash-validity or completeness claim is made before terminal
manifest finalization, and polling the reader is independent of recorder queues.

### 8.4 Package and runtime isolation

Capture Inspector ships in the main `zcp` binary to keep the canonical recorder,
projection, and eval tooling on one version. It is nevertheless a cold CLI-only
domain with this dependency graph:

```text
cmd/zcp/capture_ui.go -> internal/captureinspector facade
                      -> captureinspector/internal/web
                      -> captureinspector/internal/projection
                      -> internal/capture
```

The nested `internal/` packages are compiler-private to the inspector subtree.
Only the exact capture UI CLI adapter imports the facade. `internal/server`,
`tools`, `ops`, `workflow`, `eval`, other commands, and `internal/capture` never
import the inspector. `internal/capture` deliberately remains outside this
boundary because MCP recording, workflow provenance, and eval lifecycle use it
as a shared side channel; the reverse inspector dependency is read-only.

Importing the facade has no observable runtime behavior. Inspector production
packages contain no `init()` function or package-level call initializer and do
not import process-control packages, mutate environment/process state, or access
stdout/stderr. The embedded asset variable has no initializer. A listener,
goroutine, file read, or cache is created only after explicit facade `Start`,
which is reachable from `zcp capture ui` only. Signal handling and browser
process execution stay in the CLI adapter.

Architecture AST tests, Go's nested-internal compiler rule, and matching depguard
rules enforce both directions. Existing MCP transport and stdout-purity tests
remain the behavioral non-interference gate. `zcp capture ui --active` is a
narrow CLI-only read exception: it obtains the active session directory from the
capture manager and immediately closes that connection before starting the
inspector; the inspector domain itself receives paths and cannot mutate capture
manager state. `--active` cannot be combined with `--root`, and the viewer does
not create a missing capture root or otherwise write capture evidence.

The optional `captureinspector_browser` test tag runs a test-only Playwright
harness over a synthetic finalized capture. It verifies Cards/Flow/Split,
keyboard edge selection, reveal gating, escaped hostile markup, strict CSP,
single-detail ownership, and responsive 1024/2560 px layouts. Playwright and its
browser remain outside the Go/runtime dependency graph and embedded binary.

## 9. Compatibility and non-goals

The first supported transport lane is the empirically verified Claude Code
HTTP/SSE Messages path. Provider message IDs identify one model response;
Claude session IDs identify the conversation; capture/eval IDs identify local
observation scopes. These identities must not be conflated.

The first version does not promise hot attachment, transparent survival of
`capture off` by already-running clients, packet/TLS-record capture, model
attention, semantic grading, a provider-neutral projection, or reboot survival
without an external service supervisor. `zcp-capture-view-1` is a stable local
capture projection, but its provider/client-specific fields do not claim that
unsupported clients have equivalent evidence.
