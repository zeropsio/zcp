# Raw capture prototype — incremental live-learning plan

Status: completed; superseded by `docs/spec-capture-inspector.md`
Owner goal: observe real Claude/ZCP information flow before designing storage, inspector, or eval integration

## Scope boundary

"All communication" in this prototype means the boundaries that shape the
agent's model context:

1. Claude Code ↔ Anthropic HTTP/SSE bytes.
2. Claude Code ↔ ZCP MCP stdio JSON-RPC bytes.
3. Existing Claude Code stream-json behavioral transcript, linked from the run.
4. Child-process lifecycle and capture completeness.

It does not yet include ZCP ↔ Zerops REST, shell-command network traffic,
packet/TLS capture, ClickHouse, a browser UI, or semantic grading.

Canonical captures are plaintext internal developer evidence. Provider auth,
cookies, and database credentials are structurally omitted from recorded
headers; body bytes are retained as observed.

## Slice S1 — provider raw recorder

- [x] Add `zcp capture raw --label <label> -- <command>`.
- [x] Bind an ephemeral loopback-only fixed-upstream Anthropic proxy.
- [x] Record session lifecycle, request bodies, response chunks, safe headers,
      byte counts, and SHA-256 hashes as append-only JSONL outside the repo by
      default.
- [x] Preserve SSE incremental delivery and child exit code.
- [x] Record failures honestly; no semantic analyzer, redactor, UI, or DB.
- [x] Synthetic tests: exact request/response reconstruction, credential-header
      exclusion, fixed destination, incremental SSE.

Gate S1: a benign Claude call on the eval container produces reconstructable
provider bytes with no capture gap.

## Slice S2 — MCP raw recorder

- [x] Propagate capture session/directory through the wrapped process tree.
- [x] Add an opt-in ZCP stdio observer that records initialize, tools/list,
      tools/call, results, progress, cancellation, and framing without changing
      stdout or normal `zcp serve` behavior.
- [x] Use one per-process file and explicit completeness metadata.
- [x] Synthetic exact-frame and disabled-by-default tests.

Gate S2: a benign ZCP MCP call can be reconstructed at both MCP and provider
boundaries.

## Slice S3 — first live behavioral capture

- [x] Run `weather-dashboard-classic-dev` on the `zcp` eval container through
      the raw capture wrapper.
- [x] Pull provider capture, MCP capture, existing transcript, self-review, and
      run metadata locally.
- [x] Write an observations note beside the run: what is provable, missing
      evidence, first context/tool-result findings, and the smallest next view.

Gate S3: stop and review evidence before adding recipe/adopt variants, stable
schemas, ClickHouse, UI, source traces, or automated metrics.

## S3 review decision — guidance regression found

- Keep `weather-dashboard-classic-dev` unchanged as a regression probe.
- The run proved Claude first selected `bootstrapMode=dev`, then changed to
  `simple` only after route-specific ZCP guidance prioritized durable URL
  outcome over the user's iteration signal.
- Treat this as a ZCP guidance defect to fix later, not as a scenario wording
  defect. The valid raw capture is the causal regression baseline.
- Proceed with capture inspection before changing guidance so the same causal
  chain can be checked without one-off scripts.

## Slice S4 — minimal capture manifest

- [x] Write a private `manifest.json` with prototype format version, session,
      label, command, capture build metadata, provider origin, lifecycle,
      child exit, completeness, and relative raw-file inventory.
- [x] Create it before child start and durably finalize it after raw recorders
      close; manifest failure must be visible without changing child exit.
- [x] Tests cover permissions, terminal state, non-zero child exit, and MCP file
      discovery without embedding file contents.

Gate S4: one capture directory is self-describing without consulting terminal
output or an eval-specific path convention.

## Slice S5 — read-only causal timeline

- [x] Add `zcp capture inspect <session-dir>` as an explicitly prototype,
      local-only view; it never mutates canonical raw files.
- [x] Validate terminal hashes/gaps before interpretation.
- [x] Decode provider content encoding + SSE and MCP NDJSON while retaining
      source file, sequence, offsets, timestamps, and byte sizes.
- [x] Correlate provider tool-use → MCP `tools/call` → MCP result → subsequent
      provider `tool_result` by exact name/argument/content evidence.
- [x] Render the weather run's `dev → INVALID_PARAMETER → guidance → simple`
      chain from raw capture without scenario-specific rules.

Gate S5: the valid weather capture can be reviewed causally without bespoke
Python, while every derived row links back to raw evidence.

## Slice S6 — offline guidance source attribution

- [x] Match exact rendered result spans against the inspector binary's embedded
      atom corpus without modifying MCP/provider payloads.
- [x] Attach candidate atom ID/file matches to each MCP result and label them as
      current-corpus exact matches, not historical proof when build commits
      differ.
- [x] Prove the weather result containing the mode guidance points to
      `bootstrap-mode-prompt.md`.

Gate S6: the captured guidance defect links from model-visible bytes to its
current source owner without introducing observer effect.

## Slice S7 — second live scenario: natural adoption race

The existing recipe-route scenario is explicitly local-mode and must not be
forced through the `zcp` container. Use the canonical container scenario
`recipe-first-deploy-race-adopt` next: its prompt naturally asks to connect ZCP
to a dashboard-created recipe project without naming the adopt route.

- [x] Capture the full behavioral run with manifest-enabled provider/MCP raw
      files and the same-name `zerops` MCP override (no duplicate tool set).
- [x] Inspect whether the model reads live `activity`, waits for the in-flight
      build, then adopts without import/redeploy or zcli escape.
- [x] Save the generated causal inspection and a short observation note before
      selecting or authoring a container recipe-route scenario.

Gate S7: adoption behavior and its guidance sources are understood from raw
causal evidence, not only the retrospective transcript.

## Slice S8 — third live scenario: natural recipe route

- [x] Capture canonical `greenfield-node-postgres-dev-stage`; its visible prompt
      requests Node + Postgres with dev/stage but does not name a route.
- [x] Inspect recipe selection, bootstrap plan/import, first deploy, dev-server,
      stage delivery, verify, and any retry-causing guidance.
- [x] Record findings and source owners before adding another scenario or
      changing guidance.

Gate S8: the first empirical matrix contains classic, adoption, and recipe-route
runs under the same raw/manifest/inspector contract.
