# Completion shape

You return a single structured payload when the surface walk is complete. The payload is the exclusive channel for findings; the caller receiving your payload drives substep completion. Do not split findings across multiple tool calls.

## Payload fields

- `surfaces_walked` — a list of surface instances visited, named by their path on the mount. Every instance the surface-walk-task atom enumerated for this tier appears here, present or absent. An instance whose expected file was missing is included with a note.
- `surfaces_skipped` — any surface instance the walk did not visit, with a reason. An empty list is the expected case.
- `findings_by_severity` — three counts: `CRIT`, `WRONG`, `STYLE`. Counts are **post-inline-fix** — a finding that was inline-fixed counts in the severity it was fixed at. A separate field records the pre-fix counts.
- `findings_by_severity_before_inline_fix` — the same three counts measured before any inline-fix was applied. A reader comparing the two fields sees how much of the deliverable the reviewer repaired inline versus reported.
- `per_surface_findings` — one entry per flagged item. Each entry carries the surface instance path, the severity, the one-question test outcome (pass/fail), a short prose description, and the disposition (`inline-fixed`, `fix-recommended`, `suggestion`).
- `reclassification_delta_table` — one row per published gotcha and Integration-Guide item, with columns `item_path`, `writer_said`, `reviewer_said`, `final`. Rows where `writer_said == reviewer_said` may be omitted; the count of omitted rows is reported separately.
- `citation_coverage` — a percentage computed per the citation-audit atom: matching-topic gotchas cited, over total matching-topic gotchas. Include the raw numerator and denominator alongside the percentage.
- `cross_surface_ledger` — the table the cross-surface-ledger atom builds, with every row's severity classification.
- `inline_fixes_applied` — one entry per `Edit` the reviewer performed. Each entry carries the file path, the severity it addressed, and a short before/after snippet showing the change.

Return the payload as soon as the walk is complete. Do not call any workflow tool to signal completion; the caller takes the payload and completes the substep.
