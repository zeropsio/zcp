# Capture Inspector — lessons, completion roadmap, and definition of done

Date: 2026-07-13
Status: MVP implemented; authoritative contract is `docs/spec-capture-inspector.md`

## Focus decision

Do **not** fix the observed eval, user-sim, workflow-guidance, recipe-menu, or
tool-response issues in the capture branch. They are evidence and future
regression cases. The current focus is to finish the capture/inspection system
so those issues can later be diagnosed and fixed systematically.

Canonical raw evidence must stay independent from any grader. Findings are a
backlog produced from evidence, not transformations applied to evidence.

## What the first three runs taught us

### A causal chain is the primary product, not an aggregate score

The weather run proved the required chain:

```text
visible user intent
→ provider tool-use with bootstrapMode=dev
→ exact MCP tools/call
→ MCP INVALID_PARAMETER result
→ exact provider tool_result
→ model receives route-specific guidance
→ provider tool-use with bootstrapMode=simple
```

A transcript or retrospective alone did not explain this. The inspector must
make this chain routine and evidence-linked.

### HTTP response end is not the action boundary

Claude Code can act on streamed SSE before `message_stop` or upstream EOF. MCP
calls were observed before provider response-end records. Timelines must use SSE
content-block evidence and MCP framing, never only exchange-end timestamps.

### One eval creates multiple process/invocation roles

Fresh scenario, user-sim resumes, retrospective, and accidental post-completion
turns each spawn MCP processes and provider traffic. Per-PID files preserve the
truth, but an operator needs explicit invocation/phase labels to distinguish:

- scenario agent,
- user-sim classifier/replies,
- resume turn N,
- retrospective,
- unrelated/late work.

Do not hide idle or unexpected streams; label them.

### Source ownership needs a non-invasive side channel

Exact current-corpus atom matching maps static guidance such as
`bootstrap-mode-prompt.md`. It cannot fully identify dynamically assembled text
from Go code such as `bootstrap_guide_assembly.go`, and it is not historical
proof when capture and inspector builds differ.

The finished system needs capture-only composition provenance that does not add
one byte to the model-visible MCP/provider payload.

### Context size must be inspectable by contribution

Observed costs include repeated recipe YAML, full tools/list payloads, large
workflow responses, successful dev-server logs, built-in tool schemas, system
blocks, and cumulative message history. Provider cache usage means wire bytes,
model-visible context, and billed uncached tokens are different quantities.
The inspector must report them separately and avoid inventing cost claims.

### Completeness is part of every answer

No derived view is trustworthy unless it first reports:

- manifest/raw-file hashes,
- body/stream terminal hashes,
- queue gaps,
- provider and every MCP terminal status,
- clean/partial/unclean lifecycle,
- parser limitations such as unsupported content encoding/framing.

## Product invariants

1. **Raw is canonical and immutable.** Provider JSONL, per-process MCP JSONL,
   eval transcript, and lifecycle records are never replaced by parsed views.
2. **Derived views are disposable.** Timelines, context views, source matches,
   and summaries can be regenerated from raw evidence and carry parser version.
3. **No observer effect.** Capture must not alter model payloads, MCP stdout,
   route guidance, tool results, permissions, or agent control flow.
4. **No silent loss.** Overflow, disk failure, unsupported encoding, abrupt
   termination, or incomplete streams are visible before interpretation.
5. **Credential headers are structurally absent.** Bodies remain plaintext raw
   evidence and therefore require explicit warnings, private permissions, and
   local retention controls.
6. **Every derived claim links back to evidence.** File, sequence/range,
   exchange/process, stream/decoded offset, timestamp, bytes, and equality
   basis are available.
7. **Eval findings are not auto-fixes.** The system records and compares them;
   changing ZCP/eval behavior is a later workflow with its own approval.

## Target operator workflow

One command should run an eval with capture enabled, without temporary binaries,
manual MCP JSON, duplicate servers, scp choreography, or hand-linked paths:

```bash
zcp eval behavioral run \
  --id weather-dashboard-classic-dev \
  --capture raw
```

Equivalent explicit wrapper syntax may remain available:

```bash
zcp capture raw --label weather-dashboard-classic-dev -- \
  zcp eval behavioral run --id weather-dashboard-classic-dev
```

The eval runner must recognize the active capture session and register the
current executable as the same `zerops` MCP server exactly once.

Inspection should support focused, read-only views:

```bash
zcp capture inspect <session> --view summary
zcp capture inspect <session> --view timeline
zcp capture inspect <session> --view context --turn 6
zcp capture inspect <session> --view sources --tool-call 4
zcp capture inspect <session> --format json
```

The concrete CLI may evolve; raw storage must not depend on these views.

## Target artifact bundle

```text
<capture-session>/
├── manifest.json
├── raw/
│   ├── provider.jsonl
│   ├── mcp/
│   │   └── zcp-<pid>.jsonl
│   └── lifecycle.jsonl          # eval/Claude invocation phase markers
├── eval/
│   ├── scenario.md
│   ├── task-prompt.txt
│   ├── transcript.jsonl
│   ├── retrospective.jsonl
│   ├── self-review.md
│   └── meta.json
└── derived/                     # regenerable, optional
    ├── summary.json
    ├── timeline.md
    ├── context.json
    └── sources.json
```

Current prototype paths can be migrated later; do not move raw files merely to
match this sketch before the integration contract is proven.

## Execution roadmap (MVP portions implemented; follow-on items remain)

### 0. Finish reliability semantics

- Distinguish hash validity from capture completeness; never print
  `Integrity: OK` for a partial/unclean provider or MCP stream.
- Validate manifest format version and file/status consistency.
- Reject unsupported provider content encodings explicitly; support identity,
  gzip, and legacy gzip magic.
- Test SIGTERM/SIGKILL, signaled child exit, missing terminal records,
  cancellation, queue overflow, recorder write failure, and disk exhaustion.
- Ensure an MCP capture failure cannot be hidden by a simultaneous
  `context.Canceled` return.
- Measure proxy/recorder overhead and assert no protocol backpressure under
  representative eval payloads.

### 1. Integrate capture into the behavioral runner

- Add opt-in `--capture raw` to single-scenario and suite runs.
- Generate/use one same-name `zerops` MCP configuration for the current binary;
  prevent the duplicate-tool observer effect by construction.
- Remove manual remote binary/config/scp setup from normal operation.
- Preserve agent exit separately from capture completeness and eval outcome.
- Keep release/install commands outside this prototype workflow.

### 2. Link eval artifacts and invocation phases

- Finalize manifest with suite/scenario/session IDs and relative eval artifacts.
- Emit capture-only lifecycle markers before/after each Claude invocation:
  fresh scenario, resume/user-sim iteration, retrospective.
- Carry invocation ID/phase into MCP records through environment metadata.
- Correlate provider exchanges to invocation windows without modifying provider
  request bodies or headers.
- Show unexpected extra turns/processes rather than filtering them.

### 3. Complete model-context reconstruction

For every provider request, expose:

- model and request/exchange ID,
- system blocks and cache-control boundaries,
- tool definitions grouped by built-in/MCP server,
- message roles and content block types,
- newly added content since the previous request,
- exact MCP result → provider tool_result mappings,
- bytes and provider-reported token/cache usage,
- source evidence links.

Default views should not print hidden thinking content; show block type/size and
raw evidence location unless an authorized raw-file review is explicitly
requested.

### 4. Add capture-only composition provenance

Instrument ZCP rendering/composition to emit a side-channel trace containing:

- rendered output SHA-256,
- ordered source owners (atom ID/file or dynamic renderer symbol),
- output byte spans where deterministically available,
- capture build/commit,
- tool/workflow phase metadata.

The production MCP result text must remain byte-identical with capture on/off.
Add a test that compares payload hashes in both modes.

### 5. Make inspection usable at suite scale

- Views/filters for invocation, exchange, tool, result size, error, source, and
  incomplete evidence.
- Machine-readable JSON alongside concise terminal/Markdown output.
- Run index across captures with scenario, commit, model, status, duration,
  completeness, and artifact path.
- Compare two captures by evidence-backed deltas (context/tool/source), without
  assigning a semantic pass/fail unless an external grader does so.
- Local retention/prune/export commands with plaintext warnings.

### 6. Exercise the eval corpus

After steps 0–4:

1. rerun the classic/adoption/recipe baselines;
2. run recovery and develop-loop scenarios;
3. run the full behavioral suite with capture;
4. collect recurring findings by source owner and causal pattern;
5. only then prioritize ZCP guidance/tool/eval fixes.

The initial baseline evidence remains:

- `weather-dashboard-classic-dev` — guidance changed a correct dev decision;
- `recipe-first-deploy-race-adopt` — correct wait/adopt, late intent guidance,
  unnecessary env discovery, user-sim continuation;
- `greenfield-node-postgres-dev-stage` — clean recipe path, repeated YAML,
  verbose successful dev-server result, dynamic source-attribution gap.

## MVP definition of done

The capture/inspector MVP is ready for routine eval work when all are true:

- [x] A developer can run one scenario or the suite with `--capture raw` and no
      manual MCP/proxy setup.
- [x] The resulting directory is self-contained: raw provider/MCP, eval
      artifacts, invocation phases, build metadata, and terminal status.
- [x] Abrupt/partial/lost/unsupported evidence cannot be displayed as complete.
- [x] `inspect timeline` reconstructs model→MCP→model-context causality with raw
      links.
- [x] `inspect context` shows exactly what changed between model requests and
      where the added content came from.
- [x] Static atoms and dynamic guidance assembly have capture-only provenance
      without payload changes.
- [x] Multiple Claude/MCP processes are labeled by eval phase.
- [x] Representative multi-megabyte eval-like capture shows acceptable overhead
      and no gaps.
- [x] Classic, adoption, and recipe baseline findings can be reproduced from a
      clean run without bespoke capture/setup scripts.

## Explicitly later

- Fixing the observed ZCP guidance, tools, scenarios, or user-sim behavior.
- ClickHouse or another shared analytical store.
- Stable long-term derived schema/API.
- Browser UI.
- Automated semantic grading inside the recorder/inspector.

Those become justified only after the integrated CLI workflow and context/source
views are routinely useful across the eval suite.
