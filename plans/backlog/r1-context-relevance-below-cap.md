# R1 — context-driven below-cap relevance demotion (the relevance half)

**Status:** open / deferred (the budget-ceiling half shipped 2026-06-04)
**Surfaced by:** R1 consolidation (`plans/zcp-consolidation-2026-06-04.md`), Codex reshape.

## What shipped (R1, this session)
- Route-menu carries the import YAML only on the best-fit recipe (`keepBestFitImportYAMLOnly`) — the biggest single noise source.
- `ComposeUnderBudget` budget backstop: deterministic priority-desc demotion to a self-contained head when the synthesized payload exceeds `ComposeBodyBudget` (24 KB), wired into the develop/status + bootstrap-guide render paths. Inert on the current corpus (all fixtures under the cap) — a runtime safety net for oversized live payloads, not a behavior change.

## What's deferred (the relevance half — needs design + golden regen)
The composer today demotes only at the HARD cap. The plan's relevance lever —
demote BELOW the cap by decision-context (first-deploy ⇒ scaffold atoms full;
iterate ⇒ those go to head; scope=1 ⇒ multi-service atoms head) — is NOT shipped.
It needs:

1. **DecisionContext owner** (`turnKind / inScopeCount / route / managedDeps`)
   derived from the envelope. Cheap, but plumbing-without-a-consumer until the
   below-cap demotion uses it — so it ships WITH the demotion, not before.
2. **Head-span primitive** — `<!-- head -->…<!-- /head -->` author-marked
   decision-lever spans (parsed like the existing axis markers), so a demoted
   atom shows its REAL lever, not the heuristic first-paragraph the backstop uses.
   The reshape explicitly says do NOT author these across all atoms up front —
   migrate the top-noise atoms first.
3. **Pull-contract resolution.** The substitution-free pull retrieval
   (`zerops_knowledge uri="zerops://atoms/<id>"`, `LookupReferenceAtomBody`) only
   resolves `reference:true` atoms — an inline atom's deferred tail is NOT
   pullable (its body carries `{hostname}` placeholders no envelope-less fetch can
   substitute). A below-cap demotion that promises "pull the full body" would
   404. Either (a) keep heads self-contained (no tail pointer, as the backstop
   does), or (b) extend the pull path to render a demoted inline atom's tail —
   a real design decision.
4. **Golden regeneration.** Below-cap demotion CHANGES current output (iterate
   turns render scaffold atoms as heads), so every scenario/coverage golden
   regenerates. This is the long pole the reshape flagged.

## Trigger to promote
A flow-eval shows the agent over-reading on a narrow turn (the Wave-6 "stopped
reading at ~7 KB for a 3-line task" signal recurs), OR the corpus grows enough
that fixtures approach the cap and below-cap trimming becomes load-bearing.
