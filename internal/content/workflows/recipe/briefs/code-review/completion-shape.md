# completion-shape

Return a structured message with the following sections. Do not claim a clean pass on an audit you could not complete; an honest gap is worth more than a faked green.

## Return payload

1. **Files reviewed** — count per codebase.
2. **Findings per tier per codebase** — CRIT / WRONG / STYLE / SYMPTOM counts per codebase, each with file-path + line-number + one-sentence summary references.
3. **Inline-fixes applied** — every CRIT that was fixed under the bounded inline-fix policy, with file-path + line-number + one-line description of the edit.
4. **Feature-coverage summary** — per declared feature: evidence found on each surface the feature declares (api controller path + line, ui `data-feature` selector + file, worker subject handler + file). Call out any feature lacking observable evidence.
5. **Manifest-routing summary** — count of `(routed_to × surface)` pairs verified clean; count of pairs that flagged a drift; one line per drift with the manifest entry's `fact_title` and the surface the token-match landed on.
6. **Silent-swallow scan summary** — count of init-phase scripts reviewed, count of fetch wrappers reviewed, count of async-durable-write call sites reviewed; flagged call sites listed by file-path + line-number.
7. **Symptom reports** — any platform-level symptoms observed, with evidence, for the caller to investigate.
