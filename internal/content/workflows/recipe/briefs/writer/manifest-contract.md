# Manifest contract

Before returning, write `ZCP_CONTENT_MANIFEST.json` at the recipe output root. The manifest is the structured contract between your role and the step above you. Your return prose is advisory; the manifest is load-bearing.

Path: `{{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json`.

---

## Shape

```json
{
  "version": 1,
  "facts": [
    {
      "fact_title": "<exact Title from FactRecord>",
      "classification": "framework-invariant|intersection|framework-quirk|scaffold-decision|operational|self-inflicted",
      "routed_to": "content_gotcha|content_intro|content_ig|content_env_comment|claude_md|zerops_yaml_comment|scaffold_preamble|feature_preamble|discarded",
      "override_reason": ""
    }
  ]
}
```

Field semantics:

- `fact_title` — copy the FactRecord.Title value character-for-character from the facts log. The completeness check matches on exact title.
- `classification` — one of the six taxonomy classes (atom: classification-taxonomy.md). `intersection` is the short name for "framework × platform intersection".
- `routed_to` — one of the nine route values (atom: routing-matrix.md). Empty or unrecognized values fail the routing-honesty check.
- `override_reason` — required and non-empty when a default-discarded classification (framework-quirk, self-inflicted) is routed to anything other than `discarded`. Empty string is acceptable for every other cell; blank for default-discarded cells when routed elsewhere fails the consistency check.

---

## Rules

One entry per distinct fact. For every FactRecord.Title value in the facts log whose `scope` is `content`, `both`, or unset, emit exactly one manifest entry. FactRecords with `scope=downstream` are skipped — they are scratch knowledge for the next sub-agent, not publishable content.

One `routed_to` value per entry. Every fact appears on exactly one surface. Cross-surface re-authoring is handled by cross-reference prose in the other surfaces, not by a second manifest entry.

Honor the recorded route when present. FactRecord.RouteTo may be set by the recording sub-agent at record time. The default posture is to adopt that value directly. Overriding the recorded route is permitted when your classification differs; record your reason in `override_reason` so the reviewer can audit the deviation.

Default-discarded consistency. Classifications `framework-quirk` and `self-inflicted` default-route to `discarded`. Routing them anywhere else requires a non-empty `override_reason`. The canonical override reframes the fact: "reframed from scaffold-internal bug to porter-facing symptom with concrete failure mode and platform-mechanism citation".

Empty `facts: []` with a non-empty facts log fails the completeness check. A writer that emits an empty manifest to bypass the routing-honesty checks trivially fails this dimension.

---

## Manifest path and file-write

The manifest lives at `{{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json`. Fixed location; the honesty check reads it from that path. Do not emit copies under other names or nested directories.

Write with the `Write` tool. File must be valid JSON parseable by `jq empty`. Trailing whitespace, BOM bytes, and multi-document YAML-style separators break the parse. ASCII-only content.

---

## Routing honesty — the all-dimensions rule

The honesty check walks every (routed_to × published-surface) cell and fails when the published content contradicts the manifest. This means:

- A fact with `routed_to: discarded` must not appear (by stem-token overlap) on any published surface.
- A fact with `routed_to: claude_md` must appear in at least one codebase's CLAUDE.md and must NOT appear as a gotcha bullet in any README.
- A fact with `routed_to: zerops_yaml_comment` must appear as a comment in the codebase's zerops.yaml and must NOT appear as a gotcha bullet.
- A fact with `routed_to: content_ig` must appear as an H3 integration-guide item and must NOT duplicate into a gotcha bullet in the same README (the IG/gotcha distinctness rule).
- A fact with `routed_to: content_gotcha` must appear in exactly one codebase's README knowledge-base fragment; other codebases cross-reference.
- A fact with `routed_to: content_env_comment` must appear in the env-comment-set payload for at least one env tier.
- A fact with `routed_to: scaffold_preamble` or `feature_preamble` must not appear on any published surface.

The above is positive routing enforcement in all directions — not just the discarded-vs-gotcha direction. The writer's responsibility is to make the manifest match the content; the content match the manifest; and the override reasons match the reclassification you actually performed.
