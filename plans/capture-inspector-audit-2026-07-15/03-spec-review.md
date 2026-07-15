# Capture Inspector audit — independent Spec review

Lane: implementation fidelity to `docs/spec-capture-inspector.md`, with `docs/spec-architecture.md` as the boundary authority. Style/tooling findings are intentionally excluded.

## Verdict

**FAIL / NO-GO.** Core forensic claims can be marked valid, complete, exact, or causally proven without the evidence required by the authoritative spec.

## Confirmed violations

### `LIFE-002` — Blocker — late loss can finalize as complete

Spec §2.3: **“No silent loss.”**

Spec §2.4: a partial capture **“can never be rendered as integrity `OK`.”**

`Runtime.close` checks proxy/control capture errors before draining those components. A late drain-time error is omitted from terminal status and manifest finalization. The deterministic probe observed `complete` with no returned error.

### `INT-001` — Blocker — manifest identity is not bound to provider/lifecycle identity

Spec §5 and §7 define one capture-window identity. `inspectionFiles` copies the manifest ID into the report, while provider/lifecycle readers validate only their own internal consistency. `InspectSession` never compares those embedded IDs with the manifest ID.

Regular provider/lifecycle files copied from a different capture, with correctly recomputed manifest hashes, were accepted as `valid`/`complete` under the declaring manifest's ID.

### `INT-002` — Blocker — terminal-only provider and MCP streams are complete

Spec §7 requires canonical session/process lifetimes, and §2.3 requires incomplete streams to be visible.

`validateInspectionRecordSequence` checks contiguous sequence numbers and the final kind but not the required first kind (`internal/capture/inspect_provider.go:279–297`). Both provider and MCP terminal-only fixtures were accepted with `Integrity.Valid=true` and `Integrity.Complete=true`.

### `INT-003` — Blocker — open lifecycle scopes remain integrity complete

Spec §5.1 says missing lifecycle/bind evidence marks eval annotation incomplete.

`inspectLifecycleFile` emits warnings for runs, scenarios, and invocations with no terminal marker (`internal/capture/inspect_lifecycle.go:146–160`). `InspectSession` computes completeness only from stream terminal statuses (`internal/capture/inspect.go:340–345`) and ignores those open scopes. A lifecycle containing `eval.run.start` followed by a complete stream end returned integrity complete.

### `INT-004` — Blocker — core inspection follows symlinked parent directories

Spec isolation I6 requires symlink-safe paths. `resolveInspectionPath` performs lexical containment only; final-file `Lstat` does not reject a symlink in a parent component. A manifest under one session referenced provider/lifecycle files through a symlinked directory and was accepted as valid evidence for the declaring session.

The browser projection's separate `resolveFile` is stricter, so this primarily affects core/CLI inspection; it is still a canonical-reader violation.

### `CORR-001` — Blocker — `exact` propagation is a lossy text comparison, not byte equality

Handoff §7.4 defines exact as **“provider-visible result bytes equal the observed MCP result.”** Spec §8.2 calls `EXACT` solid.

`inspectMCPResult` concatenates text blocks and discards non-text result content (`internal/capture/inspect_mcp.go:277–300`). `providerToolResultText` independently flattens provider content (`internal/capture/inspect_provider.go:379–403`). `correlateToolEvidence` then compares only those strings and `isError` (`internal/capture/inspect.go:535`).

A fixture with MCP text plus an image and provider text only was reported `ProviderResultStatus="exact"` with four matching text bytes. The missing non-text evidence was silently discarded.

### `CORR-002` — Major — an unfinished SSE tool block becomes a completed derived tool use

Spec §2.3 names incomplete streams as visible loss. `parseProviderSSEToolUses` finalizes every remaining block at EOF (`internal/capture/inspect_provider.go:493–505`) exactly as if `content_block_stop` had been observed.

An SSE stream containing start plus input delta but no stop produced one derived tool use. The projection layer separately knows the block is `incomplete`, creating contradictory core/projection semantics.

### `CORR-003` — Major — argument mismatch can become a causal tool execution and an exact-basis graph edge

The core correlation falls back from unequal arguments to the first unused same-name MCP call (`internal/capture/inspect.go:479–548`). The result may still be labeled exact if flattened result text matches. The projection records `name-order`, but `buildEdges` unconditionally emits `jsonrpc-and-argument-equality` (`internal/captureinspector/internal/projection/graph.go:61`).

A same-name/different-argument fixture produced a causal correlation with `ArgumentsEqual=false`, propagation `exact`, and a graph edge explicitly asserting argument equality.

### `PROJ-001` — Major — canonical coordinates do not produce unique MCP message IDs

Spec §8.1: **“Projection IDs are deterministic from canonical coordinates.”** Identity must also be unambiguous.

MCP call/notification IDs use only file plus `SeqStart` (`internal/captureinspector/internal/projection/mcp.go:146`). Multiple newline-framed messages in one raw chunk share that coordinate and ID even though their stream offsets differ.

All four real captures reproduce duplicates: 2, 2, 12, and 2 duplicate-ID groups respectively. Generic graph-edge deduplication can then collapse distinct messages.

### `EVID-001` — Major — projected metrics have no evidence references

Spec §2.5: **“Every derived claim has evidence.”** The `Metric` type has an `Evidence` field, but metric factories never populate it.

The synthetic fixture had 102 known metrics with no coordinates. Each of the four real captures had 112 known metrics and zero metric evidence arrays. Textual values such as `raw`, `byte-equality`, or `derived-exact` state a basis but do not identify a file, range, offset, exchange, or timestamp.

### `WEB-001` — Blocker — cached view can continue to assert valid integrity after tampering

Spec §8 says a finalized bundle failing hash validation opens only as invalid. A same-size mutation with restored mtime bypasses the terminal-view cache key, leaving the primary view at `integrity.valid=true` even though fresh inspection rejects it. Detail endpoints re-hash, but the top-level forensic claim remains false.

### `WEB-002` — Major — pinned/root duplicate ID produces split-brain capture identity

Spec §8.1 says duplicate IDs make evidence identity ambiguous and root discovery must fail loudly.

Root-only duplicates do fail. When a pinned session shares an ID with a root session, `captureEntries` keeps root index metadata while `sessionPath` resolves detail to the pinned directory. The probe observed different labels for one displayed ID rather than an ambiguity error.

### `WEB-003` — Minor — root discovery follows a symlinked `manifest.json`

`ScanRoot` avoids symlink directories but checks each manifest with `os.Stat` and then reads it. A symlinked manifest outside the root was indexed. Later file resolution limits raw-file escape, but discovery itself violates the stated symlink-safe boundary and can inject false index metadata.

### `SEC-001` — Minor — reveal confirmation accepts a second JSON value

The reveal decoder disallows unknown fields but performs only one `Decode`. A body containing the valid confirmation followed by a second JSON object receives 204 and sets the reveal cookie. Authentication and exact Origin remain required, so this is bounded parser hardening rather than a reveal bypass.

## Conforming areas

- Four-corpus read-only proof passed.
- Credential headers were absent in 3,648 real provider records.
- Partial MCP `(n, err)` behavior and accepted prefixes were exact.
- Disabled MCP transcript was byte-identical to base.
- Invalid selected detail files are re-hashed before plaintext reads.
- Host, Origin, capability, reveal, CSP, no-inline-style, and hostile-markup tests passed.
- Metadata taint for prompt, thinking, arguments, result, and model text stayed absent before reveal.
- Real Cards/Flow/Split counts exactly reproduced the handoff acceptance table.

Those conforming areas do not make the failed forensic invariants shippable.
