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

Same filter rule: classification ∈ {`platform-invariant`,
`intersection`, `scaffold-decision (config|code)`} → record. Other
classes → discard (framework-quirk / library-metadata / self-inflicted)
or anchor at code site (operational facts not surfaceable).

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

