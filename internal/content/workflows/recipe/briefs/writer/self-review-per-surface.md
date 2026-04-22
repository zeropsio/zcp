# Self-review per surface

Before returning, walk every surface you authored and apply the checklist below. An item that fails any applicable check is **removed, not rewritten** — rewrite means the item was on the wrong surface. Move it to its correct surface (or drop it entirely) and re-check.

The engine runs gate checks at `complete substep=readmes` — manifest presence + valid JSON, classification-consistency, citations-present on gotcha/IG entries, manifest-honesty, fragment markers, content-quality predicates, factual-claims. You don't need to replicate those in shell; the gates fail your completion call loud if anything slips. The checklists below are the AUTHORING-TIME logic that prevents needing a retry round — pattern-match BEFORE you publish, not after the gate refuses.

---

## Surface 1 — Per-codebase README integration-guide fragment

- 3–6 H3 items. Beyond 6 means you didn't choose ruthlessly, OR you routed a scaffold-decision as an IG item.
- Each H3 stands alone: a porter reading just that item understands the action + reason + code diff.
- Code fenced block present in every item, showing the exact change the porter copies into their own file.
- No H3 describes a recipe scaffold helper (`api.ts`, `useApi.ts`, scaffold-specific class names) as the primary teaching. The PRINCIPLE belongs here, the implementation belongs in code comments.
- Every item whose mechanism matches a Citation Map row cites the platform topic in prose AND records `{topic, guide_fetched_at}` in the manifest entry's `citations` array.

---

## Surface 2 — Per-codebase README knowledge-base fragment

- 3–6 gotcha bullets in the `### Gotchas` section. The stem names an HTTP status, a quoted error string, or a measurable wrong-state — not "it breaks".
- Every bullet classified in {framework-invariant, framework × platform intersection, scaffold-decision-reframed, framework-quirk-reframed}. Self-inflicted routed here without `override_reason` reframing is a dropped bullet, not a rewritten one.
- Every bullet on a Citation Map row cites the guide in prose AND records a citation on the manifest entry.
- No bullet is a paraphrase of an IG item in the same README (IG teaches the fix; gotcha adds the symptom + mechanism + cross-codebase context).
- No bullet repeats a fact shipped in another codebase's README — second codebase cross-references by prose instead.

---

## Surface 3 — Per-codebase CLAUDE.md

- File byte count ≥ 1200.
- Four base sections present: "Dev Loop", "Migrations" (or "Migrations & Seed"), "Container Traps", "Testing".
- At least two custom sections beyond the base four, chosen for what THIS repo actually needs (resetting dev state, log tailing, adding a managed service, driving a feature end-to-end).
- No deploy instructions inside CLAUDE.md — deploy content belongs in IG items or `zerops.yaml` comments.

---

## Surface 4 — Env `import.yaml` comments (env-comment-set payload)

- Every service block's comment explains a decision (why this service at this tier, why this scale, why this mode) rather than narrating what the YAML field does.
- Templated openings repeated word-for-word across service blocks are anti-pattern — each block's reasoning is service-specific.
- Every number in a comment matches the adjacent YAML field exactly. Use qualitative phrasing ("single-replica", "HA mode", "modest quota") when there is no number in the adjacent YAML to match — never invent a number from memory.
- Every comment line is ASCII `#` prefixed; no Unicode box-drawing, no dividers.

---

## Removal, not rewrite

This atom's load-bearing rule: when an item fails its surface's check, **remove it**. Rewriting a wrong-surface item leaves it on the wrong surface; rewriting an uncited folk-doctrine item leaves the fabricated mechanism in place with fancier wording. The gate will refuse the completion call anyway. Your fastest path to a shipped completion is to drop the failing item and either re-route it (manifest entry changes) or discard it (manifest entry marked `routed_to: discarded`).
