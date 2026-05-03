# Decision recording — feature-phase porter_change + field_rationale

The feature phase extends scaffold-authored code/yaml. Same recording
contract as scaffold (`briefs/scaffold/decision_recording.md`) — Class
D framework × scenario items typically surface here:

- Custom response headers exposed across origins (cache panel
  X-Cache cross-origin)
- Streamed proxy bodies need `duplex: 'half'` (storage panel)
- Per-feature env-var additions (often `field_rationale`)

The codebase-content sub-agent reads your facts + on-disk source +
spec at phase 5 and synthesizes IG/KB. You record at densest context;
you do NOT author content.

## When to record

Same filter rule as scaffold: `candidateClass` ∈ {`platform-invariant`,
`intersection`, `scaffold-decision`} → record. Other classes
(`framework-quirk`, `library-metadata`, `operational`,
`self-inflicted`) → skip; no porter-facing surface. See the
Classification table in `scaffold/decision_recording.md` for
per-class surface routing.

## When in doubt

Record. The classifier auto-routes; redundant records are cheaper than
losing teaching that took 10 minutes to figure out at deploy time.

## Per-feature commits

Each feature kind (crud, cache-demo, queue-demo, storage-upload,
search-items) commits independently:

```
git commit -m 'feature(<kind>): <one-line summary>'
```

A porter scrolling git history sees the narrative shape — one commit
per feature. Don't bundle commits across feature kinds; that erases
the per-feature commit signal.

## Worked examples — feature-phase porter_change shapes

Feature phase records typically look different from scaffold —
narrower scope, scenario-tied, often a Class D shape (framework ×
feature). Two canonical examples:

### Worked example F1 — cross-origin custom headers (Class D, cache feature)

**The change you'd make in `src/main.ts`**:

```typescript
app.enableCors({
  origin: [process.env.APP_URL, process.env.APP_DEV_URL],
  credentials: true,
  exposedHeaders: ['X-Cache', 'X-Cache-Elapsed-Ms'],
});
```

**The fact you record**:

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-cors-exposed-headers",
    kind: "porter_change",
    scope: "api/code/main.ts",
    phase: "feature",
    changeKind: "code-addition",
    diff: "exposedHeaders: ['X-Cache', 'X-Cache-Elapsed-Ms']",
    why: "Browsers hide every non-CORS-safelisted response header from JS on cross-origin fetches. The cache panel's X-Cache: HIT|MISS and X-Cache-Elapsed-Ms headers are visible from curl but undefined from the SPA unless the api lists them in app.enableCors({ exposedHeaders: [...] }). Without exposedHeaders, the cache demo silently shows 'undefined' on every request — porter can't tell hit from miss. This is intersection — CORS spec + Zerops cross-origin-by-default subdomain shape.",
    candidateClass: "intersection",
    candidateHeading: "Cross-origin custom headers need exposedHeaders",
    candidateSurface: "CODEBASE_KB",
    citationGuide: ""
  }
```

**Why this Why is good**: feature-phase facts often surface to KB
(intersection class). The Why explicitly names the trigger (browsers
hide non-safelisted headers), the symptom (undefined / can't tell
hit from miss), the fix (exposedHeaders), and the classification
reason (CORS spec + Zerops subdomain shape). The synthesizer at
phase 5 has everything to author the KB bullet at 9.0 anchor shape.

### Worked example F2 — streamed proxy duplex (Class D, storage feature)

**The change you'd make in `src/storage/proxy.ts`**:

```typescript
const upstream = await fetch(s3Url, {
  method: 'PUT',
  body: req,           // streamed
  duplex: 'half',      // required when body is a stream
});
```

**The fact you record**:

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "api-fetch-stream-duplex",
    kind: "porter_change",
    scope: "api/code/storage/proxy.ts",
    phase: "feature",
    changeKind: "code-addition",
    diff: "duplex: 'half'",
    why: "Node 18+ undici fetch rejects body=stream without duplex: 'half' — the request fails with 'TypeError: RequestInit: duplex option is required when sending a body.' This applies to any streamed-body proxy (storage upload, large-file forwarding). The error is at request-build time, not at runtime, so the proxy crashes on first call.",
    candidateClass: "library-metadata",
    candidateHeading: "",
    candidateSurface: "",
    citationGuide: ""
  }
```

**Why this Why is good**: notice the candidate fields are empty.
This is library-metadata classification (Node 18+ undici quirk) —
per spec, library-metadata routes to NO surface. Recording it
preserves the teaching for code comments at the call site, but the
synthesizer at phase 5 sees the classification and discards from IG/
KB candidate sets. This is a discard-class fact recorded for
internal teaching, not for porter-facing surfaces.

If you record this with `candidateSurface: "CODEBASE_KB"` and
`candidateClass: "platform-invariant"`, the synthesizer ships a KB
bullet that's actually about Node + undici, not about Zerops. R-17
classification routing closure depends on getting this distinction
right at recording time.

The "What good vs bad Why looks like" guidance lives in
`briefs/scaffold/decision_recording.md`; cross-reference it for the
feature-phase recording. The shape is identical; only the
classification mix shifts (intersection + library-metadata are more
common at feature phase than at scaffold).
