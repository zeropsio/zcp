# Cross-surface ledger

As you walk the seven surfaces, you maintain a running ledger of every factual claim you encounter. The ledger is the mechanism that makes cross-surface consistency checkable: one fact, one surface, with cross-references from everywhere else. Multiple bodies of the same fact — even when phrased similarly — is the structural enabler of drift.

## Ledger shape

The ledger is a table you build as you walk. One row per distinct factual claim. Each row carries:

- `claim_id` — a short stable identifier you pick (a hyphenated phrase: `worker-queue-group`, `forcepathstyle-on-s3`, `self-shadow-prohibition`).
- `claim_one_line` — the claim in one sentence, phrased in your own words. This is the reviewer's normalised form of the claim; the ledger compares claims on their substance, not on the prose the writer chose.
- `surfaces_with_body` — the list of surface instances where the fact BODY appears (not where the fact is cross-referenced). A "body" is prose that states the mechanism, the symptom, or the fix in its own right; a cross-reference is a pointer like "see `apidev/GOTCHAS.md` on self-shadow" that does not restate the claim.
- `canonical_surface` — the surface where the claim SHOULD live per the classification-reclassify atom's routing rules. Populate this column based on the claim's class (platform-invariant → `GOTCHAS.md` with citation; scaffold-decision → `zerops.yaml` comment or IG; operational → `CLAUDE.md`; and so on).
- `divergences` — phrasings or values that differ across surfaces for the SAME claim. An example: one surface says `forcePathStyle: true` applies because MinIO is path-style; another surface attributes it to VPC routing. The claim is the same (fact about `forcePathStyle`); the mechanisms attributed differ.

## Building the ledger

When you land on a new item during the surface walk:

1. Form the claim in one sentence. If the item is purely a pointer or cross-reference, note that — it does not add a row to the ledger; it is a correct use of cross-reference.
2. Search the ledger for an existing row with a matching claim. "Matching" means the same mechanism or the same fix is being described, even if the surface chose different words. Use judgement — the ledger is checking substance, not exact prose.
3. If an existing row matches, add the current surface to `surfaces_with_body` and note any divergence in `divergences`.
4. If no row matches, create a new row. Populate `canonical_surface` from the classification-reclassify rules.
5. Continue the walk.

## Severity rules

At completion, classify each row's severity:

- **One surface with body, others cross-reference or silent** — correct shape. No finding.
- **Multiple surfaces with body, no divergence** — the same fact restated word-for-word in multiple places. Finding at STYLE severity: the duplication is a structural hazard for future drift even when the restatements agree today. Canonical surface keeps the body; other surfaces become cross-references.
- **Multiple surfaces with body, with divergence** — the same fact stated differently across surfaces. Finding at CRIT severity: the deliverable is internally inconsistent. A porter reading one surface gets one answer; another surface gives a contradicting answer; there is no mechanism to tell the porter which is right. The severity and exact inline-fix policy come from the reporting-taxonomy atom.
- **Body is on the wrong surface** — the claim appears on a surface that is not the canonical one, even if only one surface carries the body. This is the wrong-surface class from the classification-reclassify atom; severity and handling come from the reporting-taxonomy atom.

## Output

The ledger enters your return payload as the cross-surface audit table. One entry per row, with the severity classification and the recommended canonical surface. Divergences appear as sub-entries — concrete quoted prose showing what the surfaces say differently, so the caller receiving your payload can act on the divergence without re-walking the deliverable.
