# Close — editorial-review substep entry

This substep completes when an editorial-review sub-agent has walked every surface of the shipped deliverable, applied inline fixes for correctable editorial defects, returned its structured payload, and the main agent has recorded the findings and applied any fix-recommended changes the reviewer proposed.

## What main does at this substep

1. **Compose the editorial-review dispatch brief** by stitching the atoms under `briefs/editorial-review/` with the pointer inputs the reviewer needs on demand (facts log path, content manifest path). The reviewer walks the deliverable on the mount; the brief teaches the porter premise, the surfaces to cover, the per-surface single-question tests, and the reporting taxonomy. Do NOT prepend a Prior Discoveries block — porter-premise requires a fresh-reader stance.
2. **Dispatch the editorial-review sub-agent** with the composed brief. One sub-agent per review. The reviewer is an editor — not a Zerops platform expert, not a framework expert — focused on whether the shipped content teaches a porter what they need, without scaffold-self-referential narration or classification-at-source errors.
3. **Await the return payload**. The reviewer returns a single structured payload per `completion-shape.md`: counts by severity (pre- and post-inline-fix), per-surface findings, reclassification delta table, citation coverage, cross-surface ledger, inline fixes applied. Apply every `fix-recommended` item the reviewer proposes (directly or via a small Edit on the mount). `inline-fixed` items are already applied; `suggestion` items are advisory.
4. **Attest the substep** once the reviewer has returned and every `fix-recommended` item has been applied. The attestation carries the return payload as JSON so the substep checks can parse severity counts, reclassification delta, citation coverage, and cross-surface duplicates directly.

## Attestation

The attestation is the reviewer's return payload serialized as JSON. Pass it verbatim — the substep validator parses it to populate the seven dispatch-runnable checks (`editorial_review_dispatched`, `editorial_review_no_wrong_surface_crit`, `editorial_review_reclassification_delta`, `editorial_review_no_fabricated_mechanism`, `editorial_review_citation_coverage`, `editorial_review_cross_surface_duplication`, `editorial_review_wrong_count`).

```
zerops_workflow action="complete" step="close" substep="editorial-review" attestation='{"surfaces_walked":[...],"findings_by_severity":{"CRIT":0,"WRONG":1,"STYLE":3},"findings_by_severity_before_inline_fix":{"CRIT":2,"WRONG":1,"STYLE":3},"reclassification_delta_table":[],"citation_coverage":{"numerator":8,"denominator":8},"cross_surface_ledger":[],"inline_fixes_applied":[...],"per_surface_findings":[...]}'
```

If the reviewer found nothing, the attestation still carries the zero-counts payload — `editorial_review_dispatched` requires a parseable payload with at least one surface walked. Bare "reviewed" or a non-JSON string is rejected at the substep validator.

## Scope separation — what this atom owns vs what the brief owns

This atom frames what main does at this substep. The reviewer's task instructions — the porter premise, the seven surfaces, the single-question tests, the reporting taxonomy, the return payload shape — live inside the transmitted dispatch brief, addressed to the reviewer alone. The separation is the point: each atom keeps one audience.
