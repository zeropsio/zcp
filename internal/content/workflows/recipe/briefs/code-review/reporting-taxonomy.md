# reporting-taxonomy

Every finding carries a severity tier and a fix disposition. Three tiers, one symptom-only escape hatch.

## Severity tiers (positive form)

- **CRIT** — the code is broken in a way that surfaces at runtime: a missing controller route the feature list declares, a silent-swallow in an init-phase script, a worker subscription without the contract's queue group, a cross-codebase env-var-name mismatch, a manifest claim that ships user-facing content on the wrong surface. CRIT findings must be resolved before the review returns.
- **WRONG** — the implementation diverges from the plan or from a framework-correctness expectation, but the app still works: an orphan `data-feature` attribute not in the declared feature list, a missing content-type check after a fetch, a legacy framework reactive pattern where a modern rune is available, a manifest metadata inconsistency the user never reads. WRONG findings are flagged; fix is recommended but not inline.
- **STYLE** — quality improvement with no behavioral impact: variable names, whitespace, dead comments, import ordering. STYLE findings are flagged as suggestions; never inline-fixed.

One escape hatch:

- **SYMPTOM** — observed behavior with a possible platform-level cause you cannot diagnose from source alone: a console error mentioning a wrong service URL, a CORS failure, a deploy-layer issue. Report the symptom with exact evidence; do NOT propose platform fixes. Shape: "appstage console shows `Failed to fetch https://...`. Platform root cause unclear — investigate from broader context."

## Inline-fix policy

- **CRIT** — if the fix is a bounded edit of at most five lines in at most two files (for example renaming `DB_PASSWORD` to `DB_PASS` on a single line; adding the missing queue group option; adding `throw` to one catch block), you MAY apply the fix inline. Read the file first, apply the edit, then continue the review. Any CRIT larger than the five-line / two-file bound is flagged, not fixed.
- **WRONG** — flag with file path, line number, and one-sentence summary. Do not edit.
- **STYLE** — flag only.
- **SYMPTOM** — report only.

Every finding includes file path + line number + a one-sentence summary. Inline-applied fixes additionally include a one-line description of the edit.
