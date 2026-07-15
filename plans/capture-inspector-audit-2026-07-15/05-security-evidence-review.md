# Capture Inspector audit — Security and evidence review

## Threat model used

- Captures are local plaintext and may contain secrets in bodies.
- Provider credentials must not be persisted in header fields.
- Browser access is loopback-only but still requires capability, Origin/Host, and explicit reveal gates.
- Capture directories may be malformed, moved, copied, symlinked, or modified after initial inspection.
- Manifest hashes provide internal consistency, not authenticity against an attacker able to rewrite both evidence and manifest.

## Verdict

**FAIL / NO-GO.** The browser's primary access controls are strong, but canonical identity/path validation and several forensic integrity claims are unsound.

## Positive security evidence

### Credential exclusion

A structural scan parsed 3,648 provider records from all four real captures. Observed header names were limited to the documented request/response allowlists. There were zero authorization, proxy-authorization, cookie, set-cookie, API-key, token, or equivalent credential-header occurrences.

Canonical capture directories and inventoried files were mode `0700` and `0600` respectively.

Evidence: `tmp/capture-inspector-audit-2026-07-15/credential-header-structural-scan.log`.

### Browser access controls

Existing tests plus independent probes verified:

- non-loopback listen addresses rejected;
- Host must resolve to loopback;
- capability cookie is HttpOnly and SameSite=Strict;
- launch capability is one-time;
- current URL and observed back navigation contain no launch token;
- exact Origin is required for reveal;
- plaintext endpoints return 403 before reveal;
- invalid finalized captures return no plaintext detail;
- strict CSP, no external assets, no inline style/event-handler dependency;
- hostile captured markup remains escaped and does not execute;
- prompt, thinking, tool-argument, tool-result, and model-text sentinels are absent from pre-reveal metadata APIs.

Tagged synthetic and real-corpus Playwright runs reported no page or console errors.

### Read-only operation

CLI, HTTP metadata, and reveal-gated browser traversal changed neither bytes nor recorded mode/size/mtime for 107 canonical files. The real browser run reproduced all expected Cards/Flow/Split counts.

## Confirmed security/evidence findings

### `INT-004` — Blocker — core reader accepts symlink-parent evidence

Lexical path cleaning plus final-component `Lstat` does not prevent a parent directory symlink. `InspectSession` can therefore validate and derive a capture from files outside the declared session root. This breaks canonical containment and lets CLI inspection present external bytes as the declared capture's evidence.

The web projection later applies a stricter resolver, so the demonstrated path does not directly bypass browser reveal. The shared canonical reader is nevertheless unsafe and other callers can rely on its false validity.

### `INT-001` — Blocker — hashes do not bind evidence to capture identity

Provider and lifecycle records from another capture were copied as regular files and correctly inventoried. Inspection accepted them under a different manifest ID. Hash consistency alone therefore proves only that the manifest names those bytes, not that record identity agrees with the capture being reported.

### `WEB-001` — Blocker — stale cached integrity claim after tampering

A same-size canonical mutation with restored mtime leaves the cache key unchanged. The cached primary view continues to report integrity valid while fresh validation fails. Detail endpoints correctly re-hash, but users can still be shown a false green integrity state and stale derived metadata.

### `WEB-002` — Major — duplicate pinned/root ID crosses directory identity

Index metadata can come from the root session while view/detail comes from a pinned session with the same ID. This is an identity-confusion bug at the API boundary, even though both paths are local.

### `WEB-003` — Minor — manifest symlink is followed during root discovery

A symlinked `manifest.json` outside the root is indexed. Raw detail remains constrained later, but false metadata can enter the capture list.

### `SEC-001` — Minor — reveal parser accepts trailing JSON

A valid reveal object followed by a second JSON value succeeds. Capability and Origin are still required; the correction is strict EOF enforcement after the first decoded object.

### Forensic false claims

`LIFE-002`, `INT-002`, `INT-003`, `CORR-001`, `CORR-002`, `CORR-003`, `PROJ-001`, and `EVID-001` are security/evidence defects even where they are not access-control bypasses: they can label incomplete, mismatched, lossy, duplicated, or weakly joined evidence as complete/exact/proven.

## Boundedness and availability

Request bodies, page sizes, artifact/detail reads, scan depth, scan directory count, and scan capture count have explicit bounds. However, initial projection still loads complete JSONL files, complete response entities, and gzip-decoded entities into memory. A safe 250 MB/1 GB or decompression-bomb test was not executed. This remains an unmeasured local availability area and must not be represented as proven safe.

## Required correction pattern

- Resolve and verify the real session root and every path component without symlink traversal; reject symlink manifests.
- Bind manifest, provider, lifecycle, MCP, and provenance capture IDs.
- Revalidate integrity before serving a green cached claim; preserve detail re-hash.
- Detect pinned/root ID ambiguity before returning either index or detail.
- Decode exactly one reveal object and require EOF.
- Add explicit projection/decompression size budgets before claiming large-capture safety.
