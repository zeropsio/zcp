# Codex plan review — local-storage-catalog-drift

Verdict: **REVISE**, incorporated on 2026-08-29.

The independent review required these corrections before BUILD:

1. Separate composite-variant syntax admissibility from legacy mode/HA
   capability. Local Storage may carry `:single` while
   `ServiceSupportsMode` and `SupportsHAVariant` remain false. Add the schema
   matcher to S1 and pin exact-single acceptance plus HA rejection.
2. Refresh embedded schema before downstream consumers use it for HA
   capability. The order is now S1 → S3 → S2. Pin Local Storage volume fields
   in the refreshed zerops.yaml schema.
3. Cover both mounted and unmounted Local Storage dependencies. Unmounted
   guidance must name `run.volume.hostname`, never DB connection variables.
4. Treat the GitHub Environment branch restriction as an operator gate, add an
   in-workflow `main` ref defense, and do not overclaim that a unit test proves
   external repository settings.
5. Exclude Local Storage from authoring's managed HA-family briefing and assert
   its exact vertical-scaling YAML shape.
6. Add adapter-level Data Console proof that Local Storage resolves to the
   unsupported file path rather than a DB/object descriptor.

All findings were accepted. The plan, register, briefs, verification trace,
and promoted contracts were updated before implementation.
