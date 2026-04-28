# Parent-recipe deduplication

When this recipe inherits from a parent (the brief includes a parent
pointer block), Read the parent's published surfaces FIRST and treat
parent-covered topics as DONE.

For each candidate IG item or KB bullet:

1. Read parent's `codebase/<h>/README.md` (or equivalent surface).
2. If the parent already covers the topic with the same teaching →
   skip in your output; cross-reference parent instead.
3. If the parent covers the topic but THIS recipe extends it (different
   service, different scenario) → write the delta only.
4. If the parent doesn't cover it → author normally.

Parent + child READMEs read together as a per-codebase guide; topic
duplication compounds and bores the porter.

R-15-6 closure: cross-recipe duplication is checked by the
`validateCrossRecipeDuplication` Notice validator (heuristic similarity
backstop). Primary closure is structural: this brief gives you the
parent pointer at authoring time so you can dedup before write, not
catch dups post-hoc.
