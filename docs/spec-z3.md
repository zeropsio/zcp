# Spec: z3 — Zerops Code

z3 is a fork of the open-source T3 Code that rides inside the `zcp` container. Its server runs
next to nginx and code-server and spawns the coding agent (Claude Code, Codex) with ZCP's MCP
tools attached; its client — web, desktop, mobile — is the product surface a Zerops user signs
into. Because the agent operates the project through the same `zerops_*` tools an agent in a
terminal uses, z3's UI is not a second control plane: it is a **reader** of what those tools
report. That reading contract is what this spec owns.

- Delivery (how the z3 bundle gets into the container, the `/z3/` base path, healthz) —
  a later section, not yet written.
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

A workflow-aware tool result's text ends with exactly one fenced code block whose info string
is `json zcp-envelope`:

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

### 1.2 The reducer rule

1. Scan the tool result's text for lines that consist solely of the opening fence. The match is
   **line-anchored**: a fence mentioned mid-line (prose describing this format) is text.
2. The **last** complete block wins. A transcript may concatenate several tool results; the
   newest state is the last envelope in it.
3. A block whose body does not parse is **ignored** — the reducer keeps its previous state
   rather than adopting a malformed one. Same for an unterminated block.

### 1.3 Which tools carry it

| Tool | Result | Carries |
|---|---|---|
| `zerops_workflow action="status"` | rendered markdown | yes — the canonical lifecycle carrier (P4 recovery primitive) |
| `zerops_workflow workflow="develop" action="start"` | rendered markdown | yes — seeds a new thread's strip without a second call |
| `zerops_workflow action="close"` | terse text | yes — a fresh envelope, so the strip sees the transition |
| every other mutating tool | JSON | **no** — see §1.5 |

The first two route through `tools.statusResult`, which renders and appends from the *same*
envelope, so the markdown and the machine-readable state can never describe different moments.
`close` computes a fresh one through `tools.withFreshEnvelope` after the mutation.

Error results carry **no** envelope. An error is a leaf payload (`spec-workflows.md` P4);
appending state to one would let a reducer read a failed call as fresh truth.

Envelope computation is an addendum and is **total**: a failure appends nothing, leaves the
tool's own result untouched, and reports to stderr (JSON-only stdout).

### 1.4 Size

The block is small next to the guidance it trails: **140 B** for an idle envelope, **~1.7 KB**
for four services plus a work session with deploy/verify attempts. The synthesized guidance is
held under `workflow.ComposeBodyBudget` (24 KB), which sits below the 28 KB soft cap and the
**32 KB MCP tool-response cap** precisely to leave room for the scaffold `RenderStatus` adds —
so the envelope fits inside existing headroom and needs no budget of its own.
`TestAppendEnvelope_BlockSizeBudget` pins it.

### 1.5 Why not `structuredContent`, and why most tools still don't carry it

The Go MCP SDK marshals a non-nil typed handler output (a handler's second return value) into
the JSON-RPC result's `structuredContent` field, *alongside* the text content. **Claude Code
replaces the model-facing tool result with `structuredContent` when it is present** — the text
block never reaches the model. Routing the envelope that way would silently strip every atom of
guidance a workflow result renders. Measured live; recorded in
`../z3/docs/internals/zerops/verified.md`, section "S6 PROVE".

So the typed-output slot stays empty at every handler, guarded by
`TestNoStructuredContentOnToolResults` (which checks named handlers *and* the closures handed
to `mcp.AddTool`).

The same measurement constrains the remaining tools. `zerops_deploy`, `zerops_verify`,
`zerops_import`, `zerops_mount` and the bootstrap actions return `jsonResult` — the result text
is one JSON document, not prose. Appending a markdown fence to it would stop it parsing as
JSON, which is not an additive change. Carrying the envelope there needs a decided mechanism
(an `envelope` field on each response struct, or a second `mcp.Content` block), not an append;
until then the strip advances on `status` / `start` / `close`, and a thread reopened after
compaction is correct after one `status` call.

---

## 2. Delivery

Not yet specified.
