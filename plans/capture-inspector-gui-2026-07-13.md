# Capture Inspector GUI — evidence-first product and implementation plan

Date: 2026-07-13
Status: functional local MVP implemented on `feat/capture-raw-prototype`; scale hardening and browser automation remain

Implementation note (2026-07-14): the compiler-fenced
`internal/captureinspector/` domain, its private projection/web packages, the
embedded frontend, and `zcp capture ui` now implement the G0–G5 product
shape over finalized captures. The implementation also has a persisted-prefix
running mode, explicit plaintext/thinking gates, generic built-in and MCP tool
details, all framed MCP JSON-RPC methods, conversation metadata, exact context
request detail, dimensioned metrics, nested capture discovery, and unknown-safe
comparison. Remaining G6/hardening work is accurate per-SSE-event timestamp
projection, paged/compact indexes and 250 MB/1 GB benchmarks, sanitized fixture
corpus expansion, and a broader visual-regression matrix beyond the committed
synthetic Playwright smoke gate. These remaining items do
not sit in recorder/proxy hot paths and must preserve the invariants below.
Authoritative current contract: `docs/spec-capture-inspector.md`

## 1. Executive decision

Build the GUI as a **local, read-only, evidence-first web application embedded in
the ZCP binary**. It runs on a random loopback port and reads finalized capture
bundles without changing canonical files. It is a new presentation/query layer
above the raw provider, MCP, lifecycle, provenance, and eval evidence—not an
analyzer inserted into the proxy hot path.

The product should answer, in one place:

1. Is this capture trustworthy and complete?
2. Which eval, scenario, invocation, client session, provider exchange, and MCP
   process am I looking at?
3. What exact context did the model receive at each request, and what changed?
4. What did the model emit, which action followed, and how long did every stage
   take?
5. Did MCP arguments/results reach each boundary byte-identically?
6. Which static atom or dynamic renderer owned model-visible ZCP guidance?
7. Where are context, token, latency, retry, and result-size costs accumulating?
8. How does this run differ from another run without pretending to grade it?
9. Can every chart point and derived statement be opened at its raw evidence?

The command surface should be:

```bash
# Browse the default capture root.
zcp capture ui

# Open one finalized bundle directly.
zcp capture ui <capture-directory>

# Open the currently active window when supported.
zcp capture ui --active

# Start without launching the browser.
zcp capture ui <capture-directory> --no-open
```

The UI server must be separate from the provider proxy listener. Stopping the UI
must not stop capture, and the first release must not expose capture mutation or
shutdown controls.

## 2. Why the current inspector is a foundation, not the browser API

The current inspector correctly establishes the essential trust boundary:
manifest/hash/completeness validation precedes interpretation, raw files remain
immutable, and derived facts carry evidence coordinates. Its
`InspectionReport`, however, is intentionally documented as unstable and is too
lossy for a complete GUI:

- provider exchanges lose status, request/response sizes, headers, duration,
  TTFB, stream timing, and error details;
- provider SSE parsing retains MCP tool-use blocks but not normal text,
  thinking, stop reasons, all built-in tool calls, or exact event timing;
- tool correlation suppresses unmatched provider uses and unmatched MCP calls;
- `ProviderResultObserved bool` conflates missing and non-identical results;
- MCP inspection discards initialize, tools/list, cancellation, unknown methods,
  per-notification timing, and process lifetime;
- context deltas expose aggregate bytes but not block/message/tool contributors;
- transcript, retrospective, user-sim metadata, result cost/TTFT, rate-limit
  events, and permission denials are not projected into the report;
- the JSON output uses Go field names and must not become an accidental stable
  browser API;
- `InspectSession` materializes full files and model request bodies, which is
  acceptable for the CLI MVP but not for a full suite or long-running window;
- active manifests have no terminal inventory, and recorder buffering means a
  live view needs explicit prefix semantics rather than pretending a running
  capture is final.

Therefore the GUI requires a versioned internal derived projection and query
API. It may reuse/refactor the verified raw readers, but it must not bind its
frontend directly to `InspectionReport`.

## 3. Evidence reviewed and empirical scale

The design is grounded in four real Claude Opus container runs captured with
commit `8a6314b3`:

| Scenario | Bundle bytes | Raw records | Model requests | Request bytes across turns | Largest request | Invocations | Claude sessions | MCP calls |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| weather classic/dev | 6.8 MB | 666 | 19 | 4.5 MB | 284 KB | 2 | 1 | 15 |
| adoption race | 10.2 MB | 1,118 | 40 | 6.5 MB | 239 KB | 22 | 11 | 8 |
| recipe dev/stage | 12.1 MB | 1,374 | 31 | 8.3 MB | 317 KB | 2 | 1 | 14 |
| failed-state recovery | 9.3 MB | 985 | 26 | 6.3 MB | 294 KB | 2 | 1 | 24 |

Additional observations that shape the GUI:

- one repeated tool catalog was about 158 KB per normal Opus request;
- provider evidence occupied 89–95% of finalized bundle bytes;
- the adoption run contained ten user-sim sessions and ten resumed invocations,
  interleaved in one capture window;
- dynamic composition provenance successfully identified recipe YAML and env
  catalog renderer spans;
- a failed MCP result was present in the next provider request with
  `is_error: true`, while the current rendered timeline reported
  `provider tool_result: missing`; the GUI cannot hide this class of
  correlation ambiguity;
- one scenario exited successfully while behavior did not satisfy the scenario
  description, proving that process/eval completion is not semantic pass/fail;
- request-to-request context is mostly repeated data, so showing only total
  bytes obscures the useful question: what was newly added or invalidated?

These are single-scenario captures. A full suite can be hundreds of megabytes,
contain hundreds of model requests, and contain many concurrent or unrelated
client sessions. The UI must paginate and lazily fetch body data from the first
version.

The older `feat/anthropic-proxy-capture` UI can inform visual vocabulary (local
loopback server, tabs, detail drawer, live refresh), but should not be
cherry-picked as architecture. It analyzed/redacted on the forwarding path,
loaded one large derived object, had no canonical integrity gate or eval/MCP
hierarchy, and mixed semantic risk findings with transport evidence.

## 4. Product invariants

The GUI adds these rules to the existing capture invariants:

1. **Inspection remains read-only.** Opening, filtering, searching, comparing,
   or closing the GUI does not modify the capture bundle.
2. **Raw evidence is lazy, not omitted.** Summary APIs do not send plaintext
   bodies. An explicit reveal action fetches a bounded raw/decoded detail.
3. **Every visual datum has evidence or is labeled otherwise.** Exact facts,
   deterministic joins, heuristics, and external annotations use distinct
   visual treatments.
4. **Unknown is not zero.** Missing token usage, absent timestamps, unbound
   sessions, and unsupported client signals render as unknown/unattributed.
5. **Execution success is not semantic success.** Capture completeness, child
   exit, eval execution status, self-review, and external grading remain
   separate dimensions.
6. **Causality beats wall-clock sorting.** Per-stream sequence and explicit IDs
   define order. Cross-process timestamps aid layout but never invent an edge.
7. **No hidden correlation loss.** Unmatched, ambiguous, or differing records
   remain visible as first-class nodes.
8. **No external network dependencies.** Fonts, scripts, charts, and assets are
   embedded; the UI emits no analytics or telemetry.
9. **No implicit redaction claim.** Credential headers are structurally absent,
   but request bodies, tool inputs/results, source code, and thinking can be
   plaintext.
10. **Derived caches are disposable.** Cache identity includes capture manifest
    hash, parser format, and inspector build. Deleting the cache changes no raw
    evidence.

Before implementation, promote the GUI contract into
`docs/spec-capture-inspector.md` and remove browser UI from that spec's
non-goals. The plan remains transient; durable behavior belongs in the spec and
tests.

## 5. Trust vocabulary and visual encoding

Every node, edge, metric, and finding carries one basis:

| Basis | Meaning | Rendering |
|---|---|---|
| `raw` | directly stored field/byte count/timestamp | solid neutral badge |
| `derived-exact` | deterministic parse, hash, or byte equality | solid blue badge |
| `joined-id` | explicit lifecycle/session/request/process/tool ID join | solid green edge |
| `joined-order` | unique name+arguments+ordered-stream correlation | amber edge with basis tooltip |
| `ambiguous` | more than one legal correlation candidate | dashed amber edge; all candidates shown |
| `heuristic` | pattern such as likely retry/loop | dotted purple; disabled in strict-evidence mode |
| `external` | transcript metadata, human annotation, or grader output | outlined badge naming source |
| `unknown` | evidence missing or unsupported | gray gap; never coerced to zero |

Color is never the sole status signal. Complete/partial/error/unknown also have
text and icons. Partial time ranges and uncertain joins use hatching/dashed
outlines. Hidden thinking is represented by a collapsed block with byte/token
metadata, not rendered text.

## 6. Canonical identity and derived event graph

### 6.1 Identity hierarchy

The GUI must keep these identities separate:

```text
capture window
├── eval run (zero or more)
│   └── scenario run
│       └── invocation / phase
│           ├── client session binding
│           ├── provider exchanges
│           ├── MCP processes
│           └── eval artifacts
├── client sessions not bound to an eval
└── provider/MCP traffic that remains unattributed
```

Provider message ID identifies one model response. Client session ID identifies
a conversation. Exchange ID identifies one proxied HTTP request/response.
Invocation ID identifies one runner-owned process phase. MCP process/request IDs
identify one stdio process and JSON-RPC call. None may substitute for another.

### 6.2 Deterministic evidence references

Every projected entity gets a deterministic ID derived from canonical
coordinates, not an auto-increment database row. Examples:

```text
provider:provider.jsonl:exchange-000011
provider:provider.jsonl:exchange-000011:sse:decoded-11960
mcp:mcp/zcp-121072.jsonl:request:13
lifecycle:lifecycle.jsonl:seq:8
eval:20260713-204136:recover-failed-buildfromgit-missing-dep:meta.json
provenance:provenance/zcp-121072.jsonl:record:3:component:1
```

An evidence reference contains:

- relative canonical file;
- sequence start/end;
- stream and decoded offsets where applicable;
- observed time;
- byte length and optional hash;
- exchange/process/request identity;
- transform chain (`base64 → gzip → SSE JSON`, `stdio chunks → NDJSON`, etc.).

The detail drawer can therefore navigate both directions: derived fact → raw
records and raw record → all derived facts that cite it.

### 6.3 Projected entities

Create a versioned internal `zcp-capture-view-1` graph containing at least:

- `CaptureWindow`
- `EvidenceFile`, `IntegrityCheck`, `Gap`
- `EvalRun`, `ScenarioRun`, `Invocation`, `Artifact`
- `ClientSession`, `ClientInit`, `RateLimitEvent`, `ClientResult`
- `ProviderExchange`, `ProviderStreamEvent`, `ModelResponseBlock`
- `ContextSnapshot`, `ContextComponent`, `ContextDelta`
- `ToolDefinition`, `ToolUse`, `ToolExecution`, `ToolResult`
- `MCPProcess`, `JSONRPCMessage`, `ProgressNotification`
- `CompositionOutput`, `SourceComponent`, `SourceMatch`
- `StructuralDiagnostic`
- `EvidenceRef`, `CausalEdge`

Large plaintext is represented by a `ContentRef`; it is not copied into overview
or timeline responses.

### 6.4 Causal edges

Required edge types include:

- lifecycle invocation → client session binding;
- invocation → provider exchange by session plus explicit time boundary;
- invocation → MCP process by capture launch metadata;
- provider tool-use → MCP tools/call;
- MCP tools/call → progress notifications → MCP result;
- MCP result → provider tool_result in a later request;
- provider tool_result → context snapshot containing it;
- composition record → exact MCP result segment;
- source component → composition byte span;
- transcript event → provider message/request where explicit IDs match;
- artifact → eval/scenario/invocation owner.

A missing edge never removes either endpoint. The failed-result regression from
the recovery run must become a fixture: the provider result must render as
`observed exact`, `observed different`, or `missing`, not one boolean.

## 7. Information architecture

### 7.1 Global shell

Persistent chrome contains:

- capture picker and capture-root search;
- breadcrumb for eval/scenario/invocation/session/exchange;
- global time-range brush;
- filters for phase, model, tool, status, error, source owner, and attribution;
- integrity/completeness badge always visible;
- strict-evidence toggle (hides heuristic overlays);
- plaintext reveal state and warning;
- permalink/copy-link action using derived IDs only.

Desktop layout:

```text
┌ capture picker / breadcrumbs / filters / integrity / reveal ┐
├ hierarchy tree ┬───────────────────────────────┬─────────────┤
│ scopes/lanes   │ current view                  │ evidence    │
│                │ timeline/chart/table/content  │ drawer      │
└────────────────┴───────────────────────────────┴─────────────┘
```

The hierarchy tree and evidence drawer can collapse on smaller screens. The GUI
is primarily a desktop forensic tool, but all controls remain keyboard
accessible.

### 7.2 Capture index / home

Purpose: find a capture before loading its bodies.

Display one row/card per manifest:

- label and capture ID;
- start/end/duration;
- complete/partial/unclean/running and integrity summary;
- capture build version/commit;
- client/version and model set when derivable;
- eval/scenario names and tags;
- child exit code versus eval execution status;
- bundle disk size by file kind;
- provider exchanges, sessions, invocations, MCP processes, tool calls, errors;
- warning count and last parse compatibility warning;
- plaintext badge and path.

Filters: date, label, scenario, commit, model, status, complete only, warnings,
client adapter, and free-text IDs. Sort by recent, duration, size, requests,
errors, or scenario.

Do not recursively parse body files just to render this page. Use manifests and
an external content-addressed summary cache.

### 7.3 Overview / health

Top section is an evidence health report, not a success score:

- integrity state and checked files;
- capture/eval execution status;
- gaps, unsupported data, partial streams, unbound sessions, unattributed
  exchanges, ambiguous/missing correlations;
- raw file size composition;
- elapsed capture/eval/scenario time;
- request/session/invocation/MCP/tool counts;
- provider token mix and CLI-reported cost, clearly separated;
- top context contributors and largest tool results;
- phase duration strip;
- structural diagnostics sorted by severity and evidence basis.

Any incomplete state pins a banner above every other view. Charts remain usable
for the validated prefix but receive an `incomplete prefix` watermark.

### 7.4 Scope hierarchy explorer

Expandable tree:

```text
Eval 20260713-202225
└── recipe-first-deploy-race-adopt
    ├── agent.initial → Claude session A → MCP process 118348
    ├── user-sim.1   → Claude session B
    ├── agent.resume.1 → Claude session A → MCP process ...
    ├── ...
    └── retrospective → Claude session A → MCP process ...
Unattributed
└── provider exchange ...
```

Each node shows status, duration, request/tool count, bytes, warnings, and
concurrency. Selecting a node scopes every view without changing global
integrity totals. Unbound and unrelated branches are never hidden.

### 7.5 Unified multi-lane timeline

This is the primary diagnosis view. Use a virtualized canvas flame chart for
scale, with optional causal edges over selected events.

Lanes:

1. eval/scenario/invocation lifecycle;
2. one lane per client session;
3. provider exchanges with request upload, provider wait, and response stream
   segments;
4. model output blocks: hidden thinking, text, built-in tool-use, MCP tool-use;
5. built-in tool executions from stream-json/provider evidence;
6. MCP process lifecycle, calls, progress, results, cancellations;
7. context-injection events (tool result appears in next request);
8. provenance/source composition events;
9. rate-limit, compatibility, gap, and parser diagnostics.

Interactions:

- zoom/pan and minimap;
- absolute UTC, relative-to-scenario, and sequence-order modes;
- collapse repeated user-sim/resume pairs;
- filter to only errors/retries/ZCP calls;
- click an event to open exact arguments/result/context/evidence;
- click an edge to show join basis and all alternate candidates;
- select a range to recompute scoped metrics;
- show concurrent provider exchanges without forcing false serial order.

Provider SSE event times must map decoded offsets back to the raw chunk that
contained the event. Using response-end time for every tool-use is not
acceptable. Cross-process wall time is display assistance only; explicit
causal edges and per-stream sequence remain authoritative.

### 7.6 Causal trace / tool execution view

One row per **all** model tool-use, including Bash/Read/Write and MCP tools.
Expandable chain:

```text
model tool_use
  → Claude/local execution or MCP tools/call
  → progress events
  → execution result
  → provider tool_result
  → next model request/context snapshot
  → next model response/action
```

Columns:

- phase/session/exchange;
- tool category/server/name/action/target;
- start/end/duration;
- argument bytes/hash and equality status;
- result bytes/hash/error;
- propagation status (`exact`, `different`, `missing`, `ambiguous`);
- source/provenance owners;
- raw evidence links.

Views include call distribution, error/retry groups, large-result ranking,
result-latency histogram, and a compact causal graph for one selected chain.
Potential retry grouping is heuristic and must be labeled. Exact repeated calls
(name + canonical arguments) can be shown as deterministic duplicate groups.

### 7.7 Conversation / client view

Render the operator-visible conversation by invocation phase:

- user messages;
- assistant text;
- tool-use cards in original order;
- tool results;
- system/init/result/rate-limit events from stream-json;
- resume boundaries and user-sim replies;
- retrospective in a separately labeled lane.

Thinking blocks are collapsed by default and show only type, bytes, reported
thinking tokens, and evidence. Revealing thinking requires a second explicit
action after general plaintext reveal.

Show client init metadata without account/device identifiers:

- Claude Code version, model, cwd, permission mode;
- MCP servers and tools count;
- plugins/skills/agents count;
- session ID (optionally shortened in overview);
- result stop/terminal reason, turns, duration, TTFT, permission denials,
  compaction, and provider/CLI usage.

### 7.8 Model context explorer

Provide four synchronized panels.

**Trend panel**

- stacked request bytes by system/tools/messages/metadata+other;
- provider-reported input/cache-create/cache-read/output token series;
- request/message count and context-reset markers;
- system/tool hash changes;
- phase boundaries and compaction markers.

**Snapshot panel**

- exact request metadata and model;
- system blocks with cache-control boundaries;
- tool catalog grouped by built-in/MCP server;
- messages by role and content-block type;
- per-component wire bytes and fingerprint;
- content hidden until reveal.

**Delta panel**

- longest common message prefix;
- added/removed/replaced messages and bytes;
- system/tool schema changes;
- newly injected tool results;
- context reset/compaction versus normal append;
- exact repeated blocks and cumulative duplicate bytes.

**Contribution panel**

- treemap for latest request contributors;
- stacked area for growth over requests;
- top tool schemas by bytes;
- top MCP/built-in results by added bytes;
- source-owner contribution when provenance exists;
- unique versus repeated byte fingerprints.

Wire JSON bytes and provider tokens must never share one unlabeled axis. Byte
counts do not claim tokenizer cost. Provider cache fields are shown as reported
per request, not as unique tokens across the session.

### 7.9 MCP protocol/process view

Per process:

- PID/file/invocation/phase/status/start/end/duration;
- stdin/stdout bytes and hashes;
- initialize protocol/client/server versions and capabilities;
- tools/list count, schema bytes, catalog fingerprint, and changes;
- all JSON-RPC methods and notifications;
- tools/call table with results;
- progress count/timing;
- cancellation and I/O errors;
- unmatched request/response IDs;
- raw NDJSON frame navigator.

A process-comparison table should expose repeated initialization/tool-catalog
overhead—especially user-sim/resume runs that launch many MCP processes.

### 7.10 Source and provenance view

Two evidence classes remain visually distinct:

1. **capture-time composition proof**: output hash plus exact static/dynamic
   owner spans;
2. **current-corpus exact match**: candidate match against the inspector
   binary, with a warning when capture and inspector commits differ.

Display:

- source-owner treemap/table by matched bytes and occurrences;
- static atom versus dynamic renderer coverage;
- output span viewer with owner-colored ranges;
- repeated owner/output fingerprints across calls;
- tool/result/context locations where an owner became model-visible;
- build commit comparison and provenance gaps;
- raw provenance record and component hash checks.

Selecting `bootstrap-mode-prompt` should highlight the exact result span, the
provider request that received it, and the next model action. Selecting
`workflow.formatRecipeImportYAMLForGuide` should show every repeated recipe YAML
in the run.

### 7.11 Metrics workspace

Offer configurable cards/charts driven by the catalog in section 8:

- KPI cards with scope and denominator tooltips;
- time series / stacked areas;
- histograms and p50/p95/max tables;
- heatmaps by phase × tool/source/status;
- treemaps for context/storage/source ownership;
- concurrency chart;
- sortable detail table behind every aggregate.

There is no single health/quality score. Users can save view configuration in
browser-local state, but it must not be written into the capture bundle.

### 7.12 Raw evidence explorer

Every canonical record must be reachable:

- manifest and file inventory;
- provider/lifecycle/MCP/provenance JSONL records;
- eval artifacts;
- virtualized rows with file, sequence, kind, time, IDs, bytes, hashes;
- record-neighbor navigation;
- raw base64/hex/text/JSON views;
- decoded gzip/SSE/NDJSON view with transform breadcrumb;
- bounded copy/download after plaintext reveal;
- list of derived entities citing the selected record.

Body endpoints enforce byte/range limits and safe manifest-relative paths. The
browser never receives an arbitrary local filesystem path to fetch. Metadata/ID
search is always available; full-body search is an explicit plaintext-reveal
action, runs server-side with cancellation and limits, and is not persisted by
default.

### 7.13 Eval artifacts view

Render and link:

- scenario frontmatter, tags, area, prompt, persona, notable friction;
- task and retrospective prompts;
- transcript and retrospective stream-json;
- self-review as model-authored evidence, not truth;
- `meta.json`, user-sim turns/termination, compaction, model, durations;
- capture MCP config as configuration evidence;
- missing/extra artifacts and manifest hashes.

Execution status, child exit, and self-review are separate cards. A future
external grader can add an overlay, but it cannot rewrite raw/eval evidence or
silently become the default status.

### 7.14 Compare view

Compare two captures or two selected scenario/invocation scopes:

- capture build/client/model/config differences;
- integrity and attribution differences first;
- phase/invocation duration and count;
- provider requests, tokens, cache mix, cost, TTFT;
- context component/delta curves;
- tool sequence, errors, retries, result sizes, propagation;
- MCP process/catalog overhead;
- source owners and repeated guidance;
- structural diagnostics.

Alignment order:

1. explicit scenario/phase IDs;
2. client session/invocation identity where meaningful;
3. tool name + canonical arguments + occurrence ordinal;
4. source owner/output fingerprint;
5. unaligned remainder shown explicitly.

Never claim statistical significance from one pair. Cohort summaries show N,
median, percentiles, and missing-data count.

### 7.15 Live view (post-finalized MVP)

A running capture is always labeled `RUNNING / NOT FINALIZED`:

- read only the durable persisted prefix;
- show recorder/control status and last persisted sequence/time;
- never render `Integrity: OK` before terminal records and manifest inventory;
- tolerate partial last JSONL lines by retaining the previous valid prefix;
- transition atomically to normal finalized inspection after `off`;
- show estimated display lag, not fake real-time precision.

The current buffered writer may not expose small updates until its buffer
flushes. Any periodic flush change must remain in the writer goroutine, be
measured, and preserve non-blocking protocol calls. Do not add a synchronous UI
subscriber to the proxy/MCP hot path.

## 8. Metric catalog

### 8.1 Metric rules

- Every metric declares scope, unit, denominator, evidence basis, and missing
  count.
- `null/unknown` is distinct from numeric zero.
- Percentiles appear only with sample count; p95 on tiny N is marked weak.
- Aggregate provider usage is labeled “sum of provider-reported per-request
  values,” not unique context tokens or cost.
- Reported CLI cost is displayed only when present; never synthesize cost from
  tokens without an explicit price/version source.
- Wire bytes, decoded bytes, model-visible fragment bytes, and disk bytes are
  distinct units.
- Structural diagnostics are deterministic. Behavioral judgments require a
  human/external annotation and do not affect integrity.

### 8.2 Integrity, completeness, and attribution

| Metric | Definition / display |
|---|---|
| Capture validity | all available manifest/file/record/body/stream/hash checks passed |
| Capture completeness | terminal complete status across manifest, provider, lifecycle, and every MCP stream |
| Verified files | verified manifest files / inventoried files, plus bytes |
| Record continuity | contiguous records / expected; show first gap |
| Dropped records/bytes | sum of `capture.gap` ranges and bytes |
| Partial/unclean streams | count and list by provider/lifecycle/MCP |
| Parser compatibility | unsupported formats/encodings/framing/client signals by file/exchange |
| Exchange completion | complete/error/missing-response-end exchanges |
| MCP completion | complete/partial/unclean processes |
| Lifecycle terminal coverage | runs/scenarios/invocations with terminal marker / started |
| Session binding coverage | bound invocations / invocations requiring bind |
| Provider attribution coverage | exchanges with supported client session / eligible `/v1/messages` exchanges |
| Eval attribution coverage | provider exchanges assigned to invocation / eligible bound-session exchanges |
| MCP attribution coverage | MCP streams assigned to invocation / MCP streams |
| Tool correlation coverage | matched provider tool-uses and MCP calls, with unmatched counts on both sides |
| Argument equality coverage | exact argument matches / correlated MCP calls |
| Result propagation state | exact / different / missing / ambiguous provider tool_results |
| Provenance coverage | correlated model-visible ZCP outputs with capture-time composition / eligible outputs |
| Artifact completeness | expected and observed eval artifacts, missing/extra files |
| Warning/diagnostic count | by severity, basis, parser code, and scope |

### 8.3 Storage and traffic volume

| Metric | Definition / display |
|---|---|
| Bundle disk bytes | manifest-inventoried size, split provider/MCP/lifecycle/provenance/eval |
| Raw record count | by file and record kind |
| Provider exchange count | by path, method, status, model, session, invocation |
| Request entity bytes | terminal request body bytes |
| Response entity bytes | terminal response body bytes |
| Decoded response bytes | after supported content decoding |
| Compression ratio | decoded/entity bytes when content encoding exists |
| MCP stdin/stdout bytes | per process/invocation and total |
| Eval artifact bytes | by artifact type |
| Provenance bytes/records | per process/surface |
| Average/max exchange bytes | request and response separately |
| Byte throughput | bytes over observed stream duration; never capture overhead |
| Persisted-prefix lag | live mode: now minus last durably visible record time |
| Observer overhead | only from a controlled paired capture-on/off benchmark; never inferred from one run |
| Duplicate content bytes | repeated exact fingerprints by system/tool/message/result/source block |
| Unique content bytes | unique fingerprints within selected scope |
| Top large records | body chunks, requests, responses, MCP results, artifacts |

### 8.4 Timing and concurrency

| Metric | Definition / display |
|---|---|
| Capture/eval/scenario/invocation duration | terminal time minus start time |
| Client session observed span | last exchange start minus first exchange start |
| Request upload time | provider request end minus request start |
| Provider wait / TTFB | response start minus request end |
| First SSE byte | first response body chunk minus request end |
| Observed TTFT | first text/thinking delta chunk minus request end |
| Observed time-to-tool-use | first tool-use SSE event minus request end |
| Response stream duration | response end minus response start |
| Exchange total duration | response/error terminal minus request start |
| Tool dispatch lag | MCP/local call start minus model tool-use event |
| MCP execution duration | matching response line time minus tools/call line time |
| First progress latency | first progress notification minus call time |
| Result injection lag | provider request containing tool_result minus execution result |
| Model reaction latency | next response first event minus tool_result request start |
| End-to-end causal chain | model proposal through next model action, with stage breakdown |
| Inter-turn idle time | next exchange start minus prior response terminal within one session |
| MCP handshake time | stream start to initialize response/tools readiness |
| Process lifetime | MCP terminal minus stream start |
| Concurrency | simultaneous provider exchanges/invocations/MCP calls over time |
| Rate-limit wait | transcript rate-limit event durations when explicitly reported |
| CLI timing | result `duration_ms`, `duration_api_ms`, `ttft_ms`, `time_to_request_ms` as external client evidence |

### 8.5 Provider, model, context, token, and cache metrics

| Metric | Definition / display |
|---|---|
| Requests by model/path/status | count and model changes by phase/session |
| Provider HTTP/error rate | status-family and explicit exchange errors / terminal exchanges |
| SDK retry count | captured `X-Stainless-Retry-Count`; unknown when absent |
| Rate-limit snapshot | captured allowlisted remaining/reset headers by exchange |
| SSE event/block count | event type and content block type |
| Output block bytes | text/thinking/tool-use JSON and decoded content sizes |
| Stop reason | provider message delta / client result when present |
| Request JSON bytes | exact request entity size |
| Context component bytes | system, tools, messages, metadata/other; fragments labeled as wire JSON |
| System blocks/bytes | count, total, per-block, cache-control boundary, fingerprint |
| Tool catalog count/bytes | built-in versus each MCP server; per-tool schema bytes |
| Message count/bytes | by role and content-block type |
| Added/removed/replaced bytes | request-to-request structural delta |
| Context reset count | non-prefix replacement/compaction/phase reset |
| Context growth rate | message/request bytes over request ordinal/time |
| Largest contributors | latest/cumulative system block, schema, message, tool result, source owner |
| Repeated schema bytes | exact tool schemas retransmitted across requests/processes |
| Repeated result/guidance bytes | exact MCP/built-in result fingerprints injected repeatedly |
| Provider input tokens | `input_tokens`, preserving presence versus zero |
| Cache creation tokens | `cache_creation_input_tokens` |
| Cache read tokens | `cache_read_input_tokens` |
| Output tokens | final provider output tokens |
| Reported token mix | each category / sum of reported categories; call it mix, not cache hit rate |
| Fresh-token share | `(input + cache_create) / reported token mix`, diagnostic only |
| Bytes per reported token | wire bytes / reported tokens, explicitly not tokenizer truth |
| CLI usage/cost | stream-json result usage/modelUsage/total_cost_usd, separate from raw provider totals |
| Thinking tokens/events | client-reported thinking-token events; content hidden by default |
| Context churn | system/tool hash changes and request reset markers |

### 8.6 Tool and MCP metrics

| Metric | Definition / display |
|---|---|
| Tool-use count | all built-in and MCP proposals by name/category/server/phase |
| Execution count | local/MCP execution records, separate from proposals |
| Tool success/error/unknown | tri-state counts and error rate over known results |
| Calls by action/target | parsed exact fields where schema permits; raw fallback otherwise |
| Tool duration distribution | p50/p95/max and N by tool/phase |
| Result-size distribution | p50/p95/max bytes by tool/status/source |
| Arguments size | canonical JSON bytes and largest calls |
| Exact duplicate calls | same tool + canonical arguments fingerprint |
| Retry pattern | ordered repeated calls after error/difference; labeled heuristic unless explicit retry ID exists |
| Orphan proposals | model tool-uses with no execution match |
| Orphan executions | MCP/local calls with no provider proposal match |
| Orphan results | results with no call/proposal |
| Propagation mismatch | arguments or result differ across boundaries, with byte diff |
| MCP process count | by phase/invocation/client session |
| MCP protocol methods | initialize, tools/list, tools/call, notifications, cancel, unknown |
| Tool catalog size/change | tools/list count/bytes/fingerprint per process |
| Progress notifications | count, first/last latency, payload bytes |
| Cancellation count | requests and outcomes |
| Permission denials | client result artifact, by tool when available |
| Destructive acknowledgement count | exact `confirmDestructive` structures, no moral/risk score |
| Structural ZCP error codes | exact parsed codes such as `DIAGNOSIS_REQUIRED`, with retry chain |

### 8.7 Conversation and eval metrics

| Metric | Definition / display |
|---|---|
| User/assistant turn count | by invocation and client session |
| Invocation count | by phase (`agent.initial`, user-sim, resume, retrospective, other) |
| Resume count | explicit resume invocations |
| User-sim iterations | metadata/lifecycle count and termination reason |
| Social/no-tool turns | deterministic turns with text and no tool-use; semantic meaning not inferred |
| Model result state | success/error, stop reason, terminal reason |
| Child exit code | capture wrapper child status, distinct from eval status |
| Eval execution status | run/scenario/invocation lifecycle terminal status |
| Compaction marker | explicit meta/client evidence; context reset shown separately |
| Scenario wall time | exact eval metadata/lifecycle duration |
| Retrospective wall time | exact metadata/lifecycle duration |
| Seed/setup/cleanup time | only when markers/artifacts expose boundaries; otherwise unknown |
| Rate-limit events | count and explicit wait data |
| Model/config changes | model, client version, MCP set, permission mode, tools/plugins/skills |
| Artifact count/bytes | per scenario and type |
| Self-review presence | present/missing only; never score its sentiment |
| External semantic assessment | optional separate overlay with grader identity/version/evidence |

### 8.8 Provenance and source metrics

| Metric | Definition / display |
|---|---|
| Composition record count | by surface/process/invocation |
| Output verification | composition output hash found and verified in model-visible result |
| Component span coverage | covered output bytes / output bytes; gaps remain unknown |
| Static/dynamic bytes | component bytes grouped by kind |
| Owner occurrences/bytes | exact spans by atom ID/file or renderer symbol |
| Current-corpus match count | exact candidate matches, labeled current-build only |
| Capture-time proof count | exact composition matches from capture-time side channel |
| Build mismatch | capture commit versus inspector commit |
| Repeated owner contribution | same owner/output appearing across results/requests |
| Unattributed guidance bytes | result segments without static/dynamic provenance |
| Owner-to-action chains | subsequent model tool-use/action after owner became visible; causal sequence, not attention claim |

### 8.9 Comparison/cohort metrics

- absolute and percent delta for every compatible metric;
- added/removed/reordered tools and phases;
- aligned context byte/token series;
- source-owner contribution delta;
- error/retry/propagation delta;
- duration delta by causal stage;
- model/client/build/config differences;
- cohort N, missing N, median, p50/p95/min/max;
- no pass rate unless an explicitly imported semantic grader supplies outcomes.

## 9. Backend and query architecture

### 9.1 Layers

```text
canonical raw readers
    ↓ validate / decode
versioned evidence graph projector
    ↓ deterministic IDs + edges
bounded query/index layer
    ↓ paginated local HTTP API
embedded browser application
```

Implemented package split:

```text
internal/capture/                                  canonical recorder/read contract
internal/captureinspector/                         cold CLI-only facade
internal/captureinspector/internal/projection/     projection, metrics, compare, details
internal/captureinspector/internal/web/            loopback HTTP, auth, APIs, assets
cmd/zcp/capture_ui.go                              sole composition/lifecycle adapter
```

Avoid exporting recorder internals merely for UI convenience. Go's nested
`internal/` rule prevents core packages from importing projection/web; AST and
depguard rules allow only the exact CLI adapter to import the facade. The
inspector may read `capture` but capture hot paths never import the inspector.

### 9.2 Projection work required before UI

1. Build an exchange index with request/response/error terminal records, sizes,
   safe headers, status, and stage timestamps.
2. Map decoded provider SSE event offsets back to raw chunk sequence/time.
3. Parse all provider content blocks and preserve hidden-content policy.
4. Parse every MCP JSON-RPC message, not just tools/call and progress.
5. Parse eval stream-json/meta/scenario artifacts with source references.
6. Represent all tool proposals/calls/results, including unmatched endpoints.
7. Replace boolean propagation with explicit exact/different/missing/ambiguous.
8. Record correlation basis/candidates and deterministic diagnostics.
9. Decompose context snapshots into addressable contributors and optional token
   presence.
10. Detect append versus reset/compaction without calling every non-prefix
    request “all messages added.”
11. Add process/exchange/invocation start/end durations.
12. Add versioned lower-camel JSON tags; do not expose Go struct layout.
13. Reproduce and fix the errored provider tool_result correlation regression
    with a sanitized fixture before trusting propagation charts.

### 9.3 Query API

Version the UI-internal API even though it is not a public compatibility
promise:

```text
GET /api/v1/captures
GET /api/v1/captures/{capture}/overview
GET /api/v1/captures/{capture}/hierarchy
GET /api/v1/captures/{capture}/events?cursor=&limit=&from=&to=&lane=&...
GET /api/v1/captures/{capture}/exchanges/{id}
GET /api/v1/captures/{capture}/contexts?cursor=&...
GET /api/v1/captures/{capture}/contexts/{exchange}/delta
GET /api/v1/captures/{capture}/tools?cursor=&...
GET /api/v1/captures/{capture}/mcp/processes
GET /api/v1/captures/{capture}/sources
GET /api/v1/captures/{capture}/metrics?scope=&from=&to=
GET /api/v1/captures/{capture}/artifacts
GET /api/v1/captures/{capture}/evidence/{derived-id}
GET /api/v1/captures/{capture}/raw/{file-id}?seq=&offset=&limit=
GET /api/v1/compare?left=&right=&alignment=
GET /api/v1/live/events
```

Rules:

- cursor pagination, stable deterministic ordering;
- bounded `limit`, response bytes, preview bytes, and decode work;
- summary endpoints contain no body plaintext;
- content endpoints require explicit reveal authorization;
- API errors include parser code and evidence reference, never raw secrets by
  default;
- strict path resolution through manifest inventory;
- active-prefix APIs are separate from finalized APIs so completeness semantics
  cannot blur.

### 9.4 Index and cache

Do not start with ClickHouse or a stable analytical database schema.

Initial implementation:

- stream records and build compact metadata indices;
- retain offsets/hashes, not duplicated bodies;
- lazily decode content/details;
- cache a content-addressed summary/index outside capture directories, e.g.
  `~/.cache/zcp/capture-inspector/<manifest-hash>/<view-format>/`;
- invalidate on manifest hash, file stat/hash, projector version, or inspector
  build change;
- allow `--no-cache` and cache deletion;
- never persist a full-text plaintext search index by default.

Benchmark this on 15 MB, 250 MB, and 1 GB synthetic bundles. Introduce SQLite
or another indexed local store only if measured query/memory targets cannot be
met through compact offset indices. Keep storage behind an interface so this is
an empirical decision, not a product commitment.

### 9.5 Frontend recommendation

Run a short implementation spike, then use:

- TypeScript + Preact + Vite;
- tree-shaken ECharts modules for trends, heatmaps, treemaps, and graphs;
- a custom canvas timeline for dense multi-lane events;
- a virtualized table/list primitive for records and tool calls;
- no component framework and no CDN assets.

Commit pinned frontend source, lockfile, and generated embedded assets so normal
`go build` remains Node-free. CI rebuilds assets and fails on a diff. Add:

```text
make capture-ui-build
make capture-ui-test
make capture-ui-e2e
```

If the spike shows that Preact does not materially improve maintainability,
plain TypeScript modules are acceptable. Do not repeat the older monolithic
HTML string.

## 10. Security and plaintext handling

1. Bind only `127.0.0.1`/`::1`; reject wildcard/non-loopback addresses.
2. Generate a random UI capability token. A one-time launch URL sets a
   `HttpOnly`, `SameSite=Strict` cookie and redirects so the token does not stay
   in browser history.
3. Reject cross-origin requests and mutating methods; set strict CSP,
   `X-Content-Type-Options`, frame denial, referrer policy, and
   `Cache-Control: no-store`.
4. Serve no external scripts, fonts, maps, telemetry, or update checks.
5. Default APIs expose IDs/counts/hashes/previews only, not full bodies.
6. Require an explicit per-browser “Reveal plaintext” confirmation for content
   endpoints; thinking has an additional confirmation.
7. Never expose account/device identifiers in ordinary views.
8. Resolve only manifest-inventoried regular files beneath an approved capture
   root/session; reject symlinks, traversal, absolute paths, and unknown files.
9. Bound raw ranges and decompression to prevent memory/disk denial of service.
10. Escape all rendered content; JSON, Markdown, logs, and model text are data,
    never executable HTML.
11. Do not place plaintext in URL query/fragment, browser localStorage, service
    workers, or persistent search indices.
12. Print the plaintext warning and exact capture path on startup.
13. Keep raw download/export explicit and auditable in terminal logs, without
    adding records to the capture itself.

## 11. Performance budgets

Initial targets on a developer laptop:

- capture index from manifests: first paint under 500 ms for 1,000 manifests;
- finalized 15 MB session overview under 1 second warm / 2 seconds cold;
- 250 MB suite overview under 3 seconds with lazy body decoding;
- timeline pan/zoom at 60 fps for 10,000 visible events;
- API list page ≤200 rows and ≤1 MB response by default;
- raw/content preview ≤64 KB unless the user requests a bounded larger range;
- metadata index memory ≤2× compact projected metadata, never proportional to
  repeated request body bytes;
- no eager delivery of full request bodies to the browser;
- cancellation of abandoned decode/search requests;
- active view lag displayed and bounded only after measured recorder flushing.

Add benchmarks for projection, source matching, event queries, context deltas,
and comparison. A performance regression must not be “fixed” by dropping
unmatched/incomplete evidence.

## 12. Delivery plan

### Slice G0 — durable GUI contract and sanitized fixtures

- [ ] Promote GUI invariants, command contract, security model, and evidence
      vocabulary into `docs/spec-capture-inspector.md`.
- [ ] Define `zcp-capture-view-1` entities, evidence references, edge bases, and
      null semantics.
- [ ] Build small sanitized synthetic bundles representing:
    - complete happy path;
    - partial/unclean/gap/unsupported encoding;
    - concurrent sessions and unattributed exchange;
    - initial/user-sim/resume/retrospective hierarchy;
    - built-in and MCP tool executions;
    - errored MCP result propagated with `is_error: true`;
    - missing/different/ambiguous correlation;
    - context append, reset, and compaction;
    - static and dynamic provenance.
- [ ] Prove projection determinism and raw-hash immutability.

Gate G0: every projected fact has a resolvable evidence reference, and the
recovery correlation regression is represented correctly.

### Slice G1 — projection core and local read-only server

- [ ] Implement exchange/SSE/MCP/transcript/artifact projections.
- [ ] Add deterministic IDs, causal edges, structural diagnostics, and compact
      indices.
- [ ] Implement loopback capability authentication and safe file resolution.
- [ ] Add `zcp capture ui <session> --no-open`.
- [ ] Serve overview, hierarchy, paginated event, evidence, and raw APIs.
- [ ] Add content-addressed external summary cache.

Gate G1: one finalized capture opens with integrity, hierarchy, overview, and
raw evidence navigation; no body plaintext is returned before reveal.

### Slice G2 — GUI shell, overview, hierarchy, raw evidence

- [ ] Frontend build spike and selected embedded stack.
- [ ] Global filters/breadcrumbs/integrity banner/plaintext gate.
- [ ] Capture index and overview dashboards.
- [ ] Scope hierarchy with unattributed/unknown branches.
- [ ] Virtualized raw record/artifact explorer and evidence drawer.
- [ ] Deep links stable across projector rebuilds for unchanged raw evidence.

Gate G2: an operator can find any canonical record and understand completeness
without using the CLI or loading the entire bundle in the browser.

### Slice G3 — timeline and causal tool traces

- [ ] Event-time mapping from raw chunks through decoded SSE offsets.
- [ ] Multi-lane virtualized timeline with phase/session/process lanes.
- [ ] All-tool causal trace and exact/different/missing/ambiguous propagation.
- [ ] Progress/cancellation/error/retry visualization.
- [ ] Built-in tool and stream-json integration.
- [ ] Structural diagnostics linked to graph gaps.

Gate G3: weather mode guidance, adoption waits/user-sim loop, recipe happy path,
and recovery override retry can each be reconstructed visually with raw links.

### Slice G4 — context, tokens, MCP, and provenance

- [ ] Context trend/snapshot/delta/contribution panels.
- [ ] System/tool/message contributor parsing and duplicate fingerprints.
- [ ] Provider token/cache series and separate CLI usage/cost.
- [ ] MCP process/protocol/catalog overhead view.
- [ ] Source-owner spans, coverage, repetition, and owner-to-action navigation.
- [ ] Metric definitions/tooltips from section 8.

Gate G4: an operator can identify the repeated 158 KB tool catalog, largest
results, context resets, cache-token mix, and exact guidance source in the real
baseline captures.

### Slice G5 — multi-capture index and comparison

- [ ] Default capture-root browser and cached manifest summaries.
- [ ] Capture/scenario/invocation comparison alignment.
- [ ] Delta charts and unaligned remainder.
- [ ] Cohort summaries with N/missing/percentiles.
- [ ] Export a self-contained derived report that references but does not
      replace canonical evidence.

Gate G5: two weather captures from different builds can be compared by context,
tool sequence, mode guidance source, latency, and tokens without semantic
pass/fail inference.

### Slice G6 — live finalized transition and scale hardening

- [ ] Active-manager discovery and persisted-prefix reader.
- [ ] Explicit running/incomplete UI and last-persisted lag.
- [ ] Measured asynchronous writer flush strategy if required.
- [ ] Finalized transition without page reload or false integrity state.
- [ ] 250 MB/1 GB stress tests, cancellation, cache invalidation, retention
      visibility, and accessibility pass.

Gate G6: a live window remains non-blocking, never claims completeness early,
and transitions to the same final projection produced by offline inspection.

## 13. Test strategy

### Go unit and contract tests

- every raw record kind and protocol transform;
- safe header/body semantics and unsupported encoding;
- exact SSE event offset→chunk timestamp mapping;
- MCP method/request/result/progress/cancel parsing;
- tool correlation exact/different/missing/ambiguous and orphan preservation;
- lifecycle/session/invocation joins including concurrent sessions;
- context append/reset/compaction and optional usage fields;
- provenance span/hash/build mismatch;
- metric formulas, null denominators, and aggregation scope;
- compare alignment and unaligned remainder;
- deterministic IDs and projection output;
- cache invalidation;
- raw files hash-identical before/after every operation.

### HTTP/security tests

- loopback-only binding;
- capability auth, one-time launch, cookie flags, origin checks;
- no CORS/external assets;
- CSP/no-store headers;
- traversal, symlink, unknown file, range/decompression limits;
- HTML/script injection through model/tool/artifact content;
- summary APIs do not contain known plaintext sentinels;
- reveal and thinking gates;
- cancellation and concurrent requests.

### Frontend tests

- component/metric formatting and unknown/partial states;
- hierarchy/filter synchronization;
- timeline lane ordering and edge basis styling;
- virtualized raw navigation;
- context and comparison charts against fixtures;
- keyboard navigation, focus, text alternatives, contrast;
- Playwright end-to-end: open fixture → select invocation → causal trace → raw
  evidence → context delta → compare.

### Performance and live tests

- projection benchmarks at 15 MB, 250 MB, and 1 GB;
- 10k/100k event timeline rendering benchmark;
- bounded browser/API memory;
- partial final JSONL line and abrupt daemon death;
- queue overflow/disk failure visible during live mode;
- finalize while UI is open;
- prove UI readers never create recorder backpressure.

Real plaintext container captures remain gitignored local smoke inputs. Commit
only minimized synthetic fixtures with deliberately fake content and IDs.

## 14. Definition of done

The GUI is ready for routine use when:

- [ ] one command opens a finalized capture or capture-root index locally;
- [ ] all canonical record/file types are navigable;
- [ ] integrity/completeness is always visible and cannot be overridden by a
      successful eval exit;
- [ ] eval → scenario → invocation → session → provider/MCP hierarchy works for
      the 22-invocation adoption case;
- [ ] timeline uses exact per-stream order and accurate SSE chunk times;
- [ ] all tool proposals/calls/results—including unmatched and errored—remain
      visible with correlation basis;
- [ ] context snapshots/deltas identify system, schema, message, result, token,
      cache, and repeated-byte contributors;
- [ ] capture-time static/dynamic provenance is inspectable by exact output span;
- [ ] metrics have units, denominator, missing count, and evidence links;
- [ ] two runs can be compared without an implicit semantic score;
- [ ] raw plaintext and thinking require explicit reveal and never leave the
      loopback application through external dependencies;
- [ ] no GUI action mutates canonical capture bytes;
- [ ] normal `go build` still emits one self-contained ZCP binary;
- [ ] repository tests, frontend tests, race tests, security tests, and scale
      benchmarks pass.

## 15. Explicitly later

- shared/multi-user hosted UI;
- ClickHouse or centralized ingestion;
- packet/TLS capture;
- automatic semantic grading or model-attention claims;
- editing guidance/tools from inside the inspector;
- remote browser access or collaboration;
- automatic deletion/pruning from the first read-only GUI;
- first-class Codex/pi transport support before their raw adapters are verified;
- source-control blame/PR automation from a finding.

Those can be layered onto the evidence graph only after the local forensic GUI
is trustworthy across the full behavioral suite.
