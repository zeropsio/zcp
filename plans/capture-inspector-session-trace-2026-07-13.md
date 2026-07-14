# Capture Inspector — human session trace plan

Status: Session Story and causal Flow Map MVP implemented; browser-scale H5 verification remains

## 1. Outcome

Add a human-oriented **Session Trace** view that answers, in one vertical story:

1. What prompt or new context did the model receive?
2. What did the model return, in original block order?
3. Which tool did it call and with which arguments?
4. What exact result did the tool return?
5. What version of that result entered the next provider request?
6. What happened next?

The existing Timeline remains the forensic transport/lifecycle view. Session
Trace is a separate view because hiding recorder/process noise is useful for a
human narrative but would be inappropriate in the canonical forensic timeline.

## 2. Human model

The page should read like a structured conversation, not like a packet log:

```text
Task / user prompt                                      2.4 KB
  "Recover the failed service ..."

Claude thinking                                      4.8 KB  [collapsed]

Claude called zerops_import                           320 B
  { pretty, colored, collapsible arguments }

Zerops returned DIAGNOSIS_REQUIRED                    1.1 KB  ERROR
  { pretty, colored, collapsible result }

Result entered Claude context                         1.6 KB  DIFFERENT +460 B
  exact MCP result
  + client-added <system-reminder> ...

Claude called zerops_events                           118 B
  ...
```

A phase divider separates `agent.initial`, `user-sim`, `agent.resume`, and
`retrospective`, even when phases share one Claude session ID.

### 2.1 Default Story mode

The first screen is intentionally not a complete protocol dump. It is a visual
**Story mode** that keeps only the causal spine:

```text
USER PROMPT
     ↓
CLAUDE TEXT / DECISION
     ↓
TOOL CALL ────────────────┐
     ↓                     │ one grouped operation
TOOL RESULT ──────────────┘
     ↓
CLAUDE'S NEXT ACTION
     ↓
FINAL ANSWER
```

Visible by default:

- actual user/task prompt;
- short model text immediately related to an action;
- tool name and a compact argument summary;
- tool result status, compact result summary, and KB size;
- propagation differences;
- context reset/rewrite warnings;
- final model response;
- errors, retries, and phase boundaries.

Hidden or collapsed by default:

- system prompt and repeated system bytes;
- tool schemas/catalogs;
- complete repeated conversation context;
- lifecycle and process records;
- provider SSE event rows;
- MCP initialize/list/progress protocol traffic;
- raw IDs, hashes, offsets, and evidence coordinates;
- token tables and secondary timings;
- thinking content (show only `Thinking · 4.8 KB` as a thin collapsed row);
- successful low-information housekeeping calls when they can be grouped without
  changing causality.

Nothing is discarded. Every hidden item is available through `Expand`,
`Details`, or `Open forensic evidence`.

Three explicit density levels prevent one view from serving conflicting needs:

1. **Story** — default human narrative;
2. **Detailed** — all model blocks, arguments, results, sizes, and timings;
3. **Forensic** — existing raw timeline/evidence views.

Changing density affects presentation only, never grouping or evidence.

### 2.2 Visual hierarchy

The page uses a centered vertical rail with large alternating cards, generous
spacing, and very little table chrome. Each model turn forms one visual group.
A tool proposal and its result share a colored container so the eye immediately
sees them as one operation.

Each collapsed card contains only four primary signals:

```text
[icon + actor]  action/status                  [duration]
short one-line summary                        [1.6 KB]
relative payload-size bar                     [expand ▾]
```

Visual priority:

1. red errors and retries;
2. yellow result differences/context rewrites;
3. orange tool operations;
4. blue user input;
5. violet model output;
6. muted technical metadata.

A narrow session minimap on the right shows one colored segment per turn. Large
payloads have proportionally wider markers, and errors have red markers. Clicking
a marker scrolls directly to the corresponding step.

## 3. Scope selection

Top controls:

- capture;
- eval run;
- scenario;
- invocation phase;
- Claude session;
- optional `Whole session` mode with explicit phase dividers.

Default behavior:

- open directly in `Story` density rather than the forensic Overview;
- from an eval capture: select the first scenario's primary agent session;
- from a direct capture: select the first verified Claude session;
- if identity is absent: offer an `Unattributed provider stream` without
  pretending it is a verified session;
- never merge two sessions based only on nearby timestamps.

The selected scope is represented in the URL so a trace can be bookmarked.

## 4. Trace lanes and cards

### 4.1 Vertical story rail

Use a vertically virtualized list with a narrow time/sequence rail on the left.
Cards are connected by causal edges, not merely sorted by wall time.

Color vocabulary:

| Card | Color | Meaning |
|---|---|---|
| Prompt / user message | blue | New model-visible user input |
| System/context change | neutral gray | System, tools, cache, or history boundary |
| Model text | violet | Assistant-visible prose |
| Thinking | amber, collapsed | Thinking metadata/content after reveal |
| Tool proposal | orange | Model requested a tool |
| Tool result success | green | Tool returned without error |
| Tool result error | red | Tool returned an error |
| Propagation difference | yellow | Tool result changed before next request |
| Unknown/ambiguous | dashed gray | Evidence cannot prove the link |

### 4.2 Prompt card

The prompt card uses the **actual provider request**, not only the eval artifact.
It shows:

- role and content-block types;
- exact decoded content size;
- task-prompt artifact equality/difference when an artifact exists;
- user text, image/document metadata, and tool results as distinct blocks;
- full request-context size as secondary metadata.

After the first request, show only the request-to-request message delta by
default. Repeating the entire 100–300 KB context on every turn would make the
trace unreadable. A `Show complete model context` action opens the existing
context detail.

If history shrinks or is rewritten, insert a prominent `CONTEXT RESET` or
`HISTORY REWRITTEN` divider. Nested `cache_control` changes remain transport
metadata and do not create a false reset.

### 4.3 Model response card

Reconstruct the provider response into original content-block order:

- text;
- thinking/redacted thinking;
- `tool_use`;
- `server_tool_use` and provider-native result blocks;
- unknown future block types preserved as `unsupported block`, not discarded.

Header metadata:

- model;
- provider message ID;
- exchange ID;
- stop reason;
- request start, first response byte, response end, and total duration;
- compressed wire bytes;
- decoded SSE bytes;
- model-visible content bytes;
- provider-reported input/output/cache tokens.

Per-SSE event timestamps from gzip captures remain labeled
`response-entity-end` until exact compressed-to-decoded timestamp mapping is
implemented. Block order is exact even when per-block wall time is unavailable.

### 4.4 Tool call card

Show:

- model tool name and tool-use ID;
- category: MCP, built-in client tool, or provider server tool;
- pretty arguments;
- argument bytes;
- proposal exchange;
- dispatch time and result time when available;
- duration only when both boundaries are observed;
- source/provenance owners as optional metadata.

### 4.5 Tool result card

Show two distinct payloads when applicable:

1. **Exact MCP/client result**;
2. **Provider-context tool_result** observed in a later request.

Propagation state:

- `EXACT` — text/error state match;
- `DIFFERENT` — provider result exists but differs;
- `MISSING` — completed tool result has no observed provider result;
- `AMBIGUOUS` — required result evidence is incomplete.

For `DIFFERENT`, provide a safe unified diff. The recovery capture must visibly
show the client-added `<system-reminder>` rather than saying the result is
missing.

## 5. Readability and formatting

### 5.1 Content classification

Classify a revealed payload conservatively:

1. valid JSON object/array → JSON tree and pretty source;
2. JSON string containing JSON → offer `Decode nested JSON`;
3. Markdown-like text → safe Markdown preview plus source;
4. YAML-like text → syntax-colored source;
5. shell/log/plain text → escaped monospace source;
6. binary/non-UTF-8 → bounded base64 preview and byte metadata.

Classification is a display hint, never an evidence claim. The user can switch
formatter manually.

### 5.2 JSON viewer

The JSON viewer needs:

- syntax colors for keys, strings, numbers, booleans, and null;
- indentation guides;
- collapse/expand at every object/array;
- `Collapse deeper than level N`;
- search with matching keys/values highlighted;
- copy value, copy path, and copy full raw JSON;
- arrays summarized as `[N items]` when collapsed;
- no loss of original raw text: `Pretty` and `Raw` tabs.

All highlighting must operate on escaped text or DOM text nodes. Captured HTML
must never execute.

### 5.3 Markdown and logs

- Markdown rendering disables raw HTML and external resource loading.
- Code fences receive local syntax colors only; no CDN.
- Logs preserve whitespace and support wrap/no-wrap.
- Long lines have a horizontal-scroll option.
- Every card supports `Pretty`, `Raw`, and `Evidence` tabs.

## 6. Size visualization

Every content card shows a prominent size chip using exact UTF-8 byte counts:

```text
1.1 KB result        1.6 KB in next context        +460 B added
```

Do not conflate these dimensions:

- provider compressed wire bytes;
- decoded SSE bytes;
- text/thinking/tool-input bytes;
- MCP raw JSON-RPC bytes;
- extracted MCP result text bytes;
- provider-context tool-result bytes;
- full provider request bytes.

The trace header includes a small relative-size histogram. Story mode uses size
bars rather than a numeric table so unusually large responses are visible at a
glance. Exact KB values remain printed on every card.

Filters:

- `Focus: important only` (errors, retries, differences, final actions);
- only payloads larger than `N KB`;
- only tool calls;
- only errors/differences;
- hide/show thinking;
- hide/show model text;
- hide/show context/system cards.

Unknown sizes remain `unknown`, not `0 B`.

## 7. Evidence and ordering contract

Authoritative ordering:

1. provider JSONL sequence inside an exchange;
2. decoded SSE event order and block index;
3. provider tool-use ID;
4. MCP JSON-RPC ID and stream sequence;
5. provider tool-result ID in the later request;
6. explicit lifecycle session/invocation binding.

Wall time controls visual spacing only. It never invents a causal edge.

Every card carries:

- deterministic ID;
- scope/session/invocation/exchange identity;
- correlation basis;
- raw evidence references;
- exact/different/missing/ambiguous trust state.

`Open forensic evidence` opens the existing drawer/raw record view.

## 8. Projection and API

### 8.1 Metadata-only skeleton

Add a bounded `SessionTrace` query projection:

```text
SessionTrace
├── scope identity
├── ordered phase dividers
└── TraceStep[]
    ├── kind/status/order
    ├── timing + size metadata
    ├── causal edge IDs
    ├── EvidenceRef[]
    └── ContentRef[]       # no plaintext
```

Suggested endpoint:

```text
GET /api/v1/captures/{id}/session-trace
    ?session=<id>&invocation=<id>&offset=<n>&limit=<n>
```

This endpoint is available after capability auth but returns no plaintext.

### 8.2 Reveal-gated content

Suggested endpoint:

```text
GET /api/v1/captures/{id}/trace-content?ref=<deterministic-ref>
```

It requires the reveal cookie and returns:

- exact bounded content;
- formatter candidates;
- original/decoded byte lengths;
- truncation state;
- evidence coordinates.

Content refs cover:

- actual provider request message blocks;
- complete provider response blocks;
- thinking blocks;
- MCP arguments/results;
- built-in tool arguments/results;
- provider-context tool results;
- eval task prompts for comparison.

Do not put payloads into the normal view cache or metadata response.

### 8.3 Required projection work

- Parse provider request messages into stable role/content-block summaries.
- Compute exact new-message deltas per verified session.
- Reconstruct complete response blocks from SSE deltas.
- Add provider response stop reason and decoded content totals.
- Bind tool proposals, executions, exact results, and later provider results.
- Project built-in client tools even when no eval transcript exists where raw
  provider evidence permits it.
- Keep unsupported blocks and unmatched endpoints visible.

## 9. Security and performance

- Existing loopback capability and explicit reveal gates remain mandatory.
- No plaintext in trace skeleton, URL, browser history, or metrics.
- No raw HTML execution, external images, CDN, telemetry, or network assets.
- Content previews are bounded; full large payloads use byte-range/chunk queries.
- Default trace response target: below 1 MB metadata for the real captures.
- Load card content only when expanded.
- Virtualize after 300 trace cards.
- JSON tree initially expands at most three levels and 200 nodes.
- Searching a large payload runs off the render path and can be canceled.

## 10. Delivery slices

### H0 — contract and fixtures

- Add synthetic fixtures for prompt → text, MCP success/error, built-in tool,
  thinking, nested JSON, different provider result, context rewrite/reset, and
  unknown/unmatched evidence.
- Pin trace ordering, IDs, byte dimensions, and no-plaintext metadata contract.

Gate: every trace card resolves to canonical coordinates without timestamp-only
correlation.

### H1 — trace projection

- Implement metadata-only `SessionTrace` and pagination.
- Add phase/session selectors and context-delta projection.
- Add complete provider response-block reconstruction.

Gate: weather and recovery traces produce a deterministic readable skeleton.

### H2 — reveal content and formatters

- Implement bounded `trace-content` API.
- Add JSON tree/raw viewer, safe Markdown, YAML/log display, and size chips.
- Add explicit thinking reveal behavior.

Gate: nested tool results remain readable without exposing payloads in summary
APIs.

### H3 — human story UI

- Add Session Trace tab and vertical rail.
- Add phase dividers, causal connectors, filters, response-size histogram, and
  URL-preserved selection.
- Make Session Trace the recommended human view while retaining forensic
  Timeline.

Gate: a human can explain the session without opening raw JSONL manually.

### H4 — tool propagation and differences

- Add MCP/client/provider result cards.
- Add safe unified diff for `DIFFERENT`.
- Add source/provenance links and forensic evidence drawer integration.

Gate: recovery clearly displays `DIAGNOSIS_REQUIRED` and the added system
reminder as two causally related but byte-different payloads.

### H5 — browser and scale verification

- Playwright tests for reveal, expansion, filters, JSON collapsing, diff, URL
  restore, keyboard navigation, and XSS payloads.
- Benchmarks on current captures plus synthetic 250 MB and 1 GB bundles.
- Verify metadata response, first render, expansion latency, and bounded memory.

Gate: the trace stays responsive and never changes recorder/proxy behavior.

### H6 — deterministic causal Flow Map

- Add metadata-only flow lanes for user input, model context, Claude turns, and
  tools.
- Group provider response blocks into stable turn nodes while preserving tool
  branches and exact result joins.
- Render context continuity as a request-size river with reset/rewrite states.
- Add `Cards / Flow map / Split`, sequence replay, bounded path focus, node and
  edge inspectors, and exact propagation deltas.
- Keep full context/tool/evidence drill-down in one inspector workspace with
  Back navigation; format multiline and nested JSON without nested scrollbars.
- Preserve strict CSP without inline style attributes or event handlers.
- Keep layout sequence-driven and deterministic; do not use force placement or
  wall-clock ordering.

Gate: recovery renders the import failure as a red tool node and a yellow
`DIFFERENT +460 B` edge to the exact next request, with no payload plaintext in
the flow response.

## 11. Acceptance walkthroughs

### Weather

The trace must show the exact initial task prompt, Claude's bootstrap decision,
workflow error/result, later corrective calls, and subdomain interaction in
causal order.

### Recovery

The trace must show:

- destructive import proposal;
- `DIAGNOSIS_REQUIRED` MCP error at approximately 1.1 KB;
- provider-context result at approximately 1.6 KB;
- highlighted client-added system reminder;
- subsequent events query and confirmed retry.

### Recipe

The trace must make many built-in file/shell operations readable, with collapsed
arguments/results and visible KB sizes, without drowning the user in repeated
full context.

### Adoption

The trace must separate initial, user-sim/resume, and retrospective phases while
preserving the shared Claude session identity where explicitly bound.

## 12. Definition of done

- A human can follow prompt → model output → tool call → result → next action.
- Every visible payload has an exact byte size and evidence coordinates.
- Nested JSON is colored, collapsible, searchable, and available as raw text.
- Thinking is collapsed and metadata-only until explicit reveal.
- MCP versus provider-context differences are explicit and diffable.
- Repeated full context is hidden by default; resets/rewrites are prominent.
- Unknown causality and missing values are never displayed as exact or zero.
- Metadata APIs contain no plaintext.
- Existing forensic Timeline, raw evidence, and capture invariants remain intact.
