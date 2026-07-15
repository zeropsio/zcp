# Capture Inspector audit — Codebase design review

## Verdict

**Facade design is strong; forensic-core seam design is not yet reliable enough to ship.**

## What is well designed

1. **Compiler-private UI domain.** The facade/private projection/private web layout is simple and enforced by both Go and AST guards.
2. **CLI-owned process policy.** Signal handling and browser launch remain outside the Inspector domain.
3. **Canonical/detail separation.** Raw JSONL remains canonical; projection JSON is explicitly versioned and ephemeral.
4. **Reveal separation.** Metadata endpoints and detail endpoints are distinct rather than relying on client-side hiding.
5. **Recorder hot-path intent.** Provider recording is non-blocking with explicit gap records; MCP wrappers preserve exact delegate `(n, err)` results.
6. **Concrete manager seams.** `ManagerConfig` provides process/start/token seams that make lifecycle fault tests possible.
7. **Real-corpus UX model.** Deterministic lane layout and one fixed inspector surface work at both tested widths.

## Design concerns

### 1. `internal/capture` has too many reasons to change

The package contains provider proxying, async recording, MCP transport, lifecycle control, manager/settings mutation, manifest storage, eval bundling, provenance, source-corpus matching, protocol inspection, and renderers. Because Go dependencies are package-wide, the Inspector's canonical-reader edge also brings `content`, `platform`, `runtime`, and the Zerops SDK into its closure.

Smallest future seam: extract a dependency-light canonical format/reader/validator package while leaving hot recorder and manager adapters in `internal/capture`. Do this only with migration tests; it is not a license for a second storage format.

### 2. Protocol state is parsed more than once with divergent semantics

Provider SSE and MCP data are interpreted in both core inspection and projection/detail paths. The divergence is observable:

- projection marks an unfinished provider block `incomplete`;
- core inspection finalizes the same unfinished tool block into a correlatable tool use.

A single validated intermediate protocol event/state machine should feed both core report and projection. Detail readers can re-open bytes but should reuse the same framing/state rules.

### 3. Equality is represented by flattened display text

The same fields currently serve display previews, byte counts, and forensic equality. This allowed non-text MCP result content to disappear before `exact` was decided. Equality needs a dedicated canonical byte representation plus an explicit transformation basis; display text should be derived afterward.

### 4. Lifecycle validation is terminal-marker oriented, not state-machine oriented

Provider/MCP validators check sequence and final record; lifecycle validation records warnings without contributing to completeness. Required starts, legal transitions, nesting, identity, open scopes, and terminal state should be represented in one validator result consumed by `Integrity.Valid` and `Integrity.Complete`.

### 5. Manager ownership and error ownership are split

`Manager.On` owns process startup but its rollback helper owns only a best-effort control request. `Runtime.close` owns final status but observes component errors before it completes component shutdown. Ownership should extend through verified cleanup/final status, not end at method dispatch.

### 6. View cache identity is weaker than the claim it caches

The cache stores a cryptographic integrity result but keys raw files by mutable metadata. Either cache only non-integrity presentation, key by verified content identity, or perform a cheap current-manifest hash revalidation before returning an integrity-bearing terminal view. The cache also has no eviction and can retain old keys after repeated metadata changes.

### 7. Metric construction discards source sets

Metric factories receive aggregate numbers but not the observations/evidence used to calculate them, making empty `Metric.Evidence` the natural outcome. Aggregate helpers should return value, sample/missing counts, and a bounded deterministic evidence set (or an explicit evidence-range aggregate) together.

### 8. Platform policy is embedded in portable files

Unix flock, signals, process groups, and session creation are in untagged production files. A narrow process-lock platform adapter would both restore the Windows build and make platform behavior independently testable.

### 9. Projection identity omits the coordinate that distinguishes messages

MCP line assembly already computes `StreamOffset`; ID generation ignores it and uses only file/sequence. IDs should be generated through one coordinate constructor and uniqueness asserted over every projected entity.

## Design-level test recommendations

- Property/state-machine tests for provider, MCP, and lifecycle start/transition/end rules.
- Table tests for whole-result equality across text, multiple blocks, non-text, errors, empty content, and client-added wrappers.
- Global uniqueness assertion for every projection ID on synthetic and real corpus.
- Fault matrix where every startup/finalization operation fails in turn and ownership is checked afterward.
- Cache tests that vary bytes, size, mtime, mode, manifest, and concurrent requests independently.
- Differential tests as first-class fixtures for capture OFF, not ad hoc subprocess probes.

## Accepted design observations

- Keeping the Inspector in the main binary remains reasonable based on successful cold-runtime proof; binary-size alone does not show activation.
- A local HTTP service with loopback capability cookies is appropriate for the current developer-only threat model.
- Current-corpus source matching is correctly labeled as a candidate/current-corpus match rather than capture-time proof.
