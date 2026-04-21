# Reporting taxonomy

Every finding you produce falls into exactly one of three severities. The severity determines the finding's handling — whether it must be fixed before the deliverable ships, whether inline-fix is appropriate, whether the caller receiving your payload takes the finding and acts on it.

## The three severities

### CRIT — required revision; cannot ship as-is

A CRIT finding means the deliverable is wrong in a way a porter will act on and be misled. The published content directs the reader to do something incorrect, or hides a mechanism behind a fabricated explanation, or places content on a surface that fails the surface's single-question test in a way inline-fix cannot repair.

CRIT applies to:

- A wrong-surface item where the correct disposition is reroute or delete and inline-fix is infeasible (e.g., a gotcha that is actually a scaffold-decision belonging in a `zerops.yaml` comment — moving the content requires context the reviewer does not have).
- A fabricated-mechanism finding where the gotcha invents a platform behaviour contradicted by the matching platform topic guide.
- A factually-wrong claim the reviewer can verify against the recipe's own state (e.g., an `import.yaml` comment describing a subsystem the recipe does not use).
- A cross-surface divergence where the same claim carries different values or different mechanisms on different surfaces — the deliverable is internally inconsistent.
- A classification-reclassify delta whose reclassification reroutes the item to a different surface (wrong-surface by reclassification).

Handling: the reviewer surfaces CRIT findings to the caller receiving your payload. The caller receiving your payload takes the finding and revises the deliverable. Inline-fix by the reviewer is not appropriate for CRIT — the required revision depends on context the reviewer does not have (authorial intent, cross-codebase consistency beyond the ledger, platform-topic depth).

### WRONG — fix recommended; inline-fix where confidence is high

A WRONG finding means the deliverable has a defect that should be fixed before shipping but that a porter reading the surrounding content can often work around. The content is imperfect in a way the reviewer can often repair directly without re-deriving authorial intent.

WRONG applies to:

- A matching-topic gotcha missing its citation to the platform topic guide (the citation-audit atom covers the topic map).
- A boundary violation between adjacent surfaces where the content belongs on a sibling surface and the reviewer can identify the sibling confidently (e.g., a framework-quirk shipped as a gotcha; the gotcha is deleted and the framework-quirk nature is the justification).
- A classification-reclassify delta that does not reroute the item but disagrees on class in a way a downstream reader would notice.
- A self-inflicted item that should be discarded — the reviewer deletes the item with a short note explaining the self-inflicted classification.

Handling: inline-fix is permitted when the fix is **bounded** (under five lines changed, a single-item deletion, or adding a citation the reviewer identifies confidently from the citation-audit topic map). A bounded inline-fix is applied via `Edit`, logged in the completion payload's `inline_fixes_applied` list with a before/after snippet, and surfaced at WRONG severity with disposition `inline-fixed`. An unbounded WRONG — one where the correct fix ripples beyond a five-line edit or one where the reviewer's confidence in the fix is low — is surfaced with disposition `fix-recommended` and left for the caller receiving your payload.

### STYLE — suggestion; non-behavioural

A STYLE finding means the deliverable's substance is correct but the shape could be tighter. STYLE findings do not block the deliverable from shipping.

STYLE applies to:

- Cross-surface duplication where multiple surfaces carry the same fact body with no divergence — the duplication is a structural hazard for future drift but the deliverable is consistent today.
- Phrasing, voice, or depth concerns on a surface that passes its single-question test but could pass it more clearly.
- Formatting inconsistencies that do not affect comprehension.

Handling: STYLE findings may be inline-fixed at the reviewer's discretion when the fix is trivial (a three-line rewording, a cross-reference replacement of a duplicated body). Fixes are logged the same way as WRONG inline-fixes. STYLE findings not fixed are surfaced with disposition `suggestion` — the caller receiving your payload decides whether to act.

## Inline-fix policy — what a "bounded" inline-fix looks like

- Under five lines changed per fix.
- Does not require re-deriving authorial intent or cross-codebase context beyond the ledger the reviewer already built.
- The `Read` of the target file has already happened in this session (per the mandatory-core atom's file-op sequencing).
- Each inline-fix is logged in the completion payload with the file path, the severity it addressed, and a short before/after snippet.

Unbounded fixes — those that cross a codebase boundary, that require re-walking another surface for consistency, or that touch more than five lines — are surfaced with disposition `fix-recommended` and left for the caller receiving your payload regardless of severity.
