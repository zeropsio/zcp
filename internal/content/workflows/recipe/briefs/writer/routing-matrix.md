# Routing matrix

Every fact has exactly ONE `routed_to` value in the manifest. The writer emits that value. The honesty check reads across all (routed_to × published-surface) pairs and compares what the manifest claims to what the published content actually contains.

This atom enumerates the matrix explicitly because a single-dimension honesty check (discarded-vs-gotcha only) misses five of six routing dimensions. A fact routed to `claude_md` can still leak into a gotcha bullet; a fact routed to `content_ig` can still appear as a gotcha bullet; and so on. The matrix below declares, for every cell, either the routing is allowed (and when) or the routing requires an `override_reason`.

---

## The `routed_to` enum

Writer-emitted values in `ZCP_CONTENT_MANIFEST.json`:

| Value | Meaning |
|---|---|
| `content_gotcha` | Appears in a README knowledge-base fragment as a gotcha bullet. |
| `content_intro` | Appears in a README intro fragment as paraphrase. |
| `content_ig` | Appears in a README integration-guide fragment as an H3 item. |
| `content_env_comment` | Appears in the `env-comment-set` payload for an env `import.yaml`. |
| `claude_md` | Appears in a codebase's CLAUDE.md operational section. |
| `zerops_yaml_comment` | Appears as an ASCII `#` comment in the codebase's `zerops.yaml`. |
| `scaffold_preamble` | Consumed by future scaffold dispatches; not published content. |
| `feature_preamble` | Consumed by future feature dispatches; not published content. |
| `discarded` | Dropped; no content surface. |

`scaffold_preamble` and `feature_preamble` are downstream-only. They do not appear on any reader-facing surface and the writer does not author content from them.

---

## The routing cells

For every classification × surface pair, the table below marks the routing as Allowed, Allowed-with-reason, or Not-allowed. "Reason" means an `override_reason` in the manifest is required when routing this class to this destination.

| Classification \ routed_to | content_gotcha | content_intro | content_ig | content_env_comment | claude_md | zerops_yaml_comment | discarded |
|---|---|---|---|---|---|---|---|
| framework-invariant | Allowed + citation | Allowed (paraphrase) | Allowed (principle-level) | Allowed | Allowed | Allowed | Not-expected |
| framework × platform | Allowed + citation | Not-allowed | Allowed | Allowed | Allowed | Allowed | Not-expected |
| framework-quirk | Reason + reframe | Not-allowed | Reason | Not-allowed | Reason | Not-allowed | Allowed (default) |
| scaffold-decision (YAML choice) | Not-allowed | Not-allowed | Not-allowed | Not-allowed | Not-allowed | Allowed | Not-expected |
| scaffold-decision (code principle) | Not-allowed | Not-allowed | Allowed | Not-allowed | Not-allowed | Allowed | Not-expected |
| scaffold-decision (operational) | Not-allowed | Not-allowed | Not-allowed | Not-allowed | Allowed | Not-allowed | Not-expected |
| operational | Not-allowed | Not-allowed | Not-allowed | Not-allowed | Allowed | Not-allowed | Not-expected |
| self-inflicted | Reason + reframe | Not-allowed | Reason | Not-allowed | Reason | Not-allowed | Allowed (default) |

Reading a cell: "Allowed" means the writer may route a fact of this class to this destination without a reason field. "Reason" means the `override_reason` field must be non-empty and must reframe the fact; routing the raw self-inflicted or framework-quirk text is not acceptable. "Not-allowed" means the honesty check will flag any manifest entry that makes this pairing.

---

## Enforcement dimensions

Given the writer emits one `routed_to` per fact, the honesty check walks every published surface and confirms:

- A fact routed to `content_gotcha` appears as a gotcha bullet in exactly one codebase's README knowledge-base fragment.
- A fact routed to `content_intro` appears in the intro fragment as paraphrase; exact-stem match is not required.
- A fact routed to `content_ig` appears as an H3 item in the integration-guide fragment of at least one codebase's README, carrying a fenced code block.
- A fact routed to `content_env_comment` appears in the env-comment-set payload for at least one env tier.
- A fact routed to `claude_md` appears in at least one codebase's CLAUDE.md, and does NOT appear as a gotcha bullet in any README.
- A fact routed to `zerops_yaml_comment` appears as a `#` comment in the codebase's `zerops.yaml`, and does NOT appear as a gotcha bullet.
- A fact routed to `discarded` does NOT appear on any published surface. Similarity is measured on stem tokens; cross-surface restatements in different words still trip if the content-bearing tokens overlap.

A fact routed to `scaffold_preamble` or `feature_preamble` is downstream-only — it should not appear in the published content set, and the writer's manifest tracks it only so the routing honesty check can confirm the absence on publishable surfaces.

---

## Single-routing rule

Every fact appears in exactly one manifest entry and every manifest entry has exactly one `routed_to` value. A fact whose content is useful across multiple surfaces picks the primary surface and other surfaces cross-reference. Example: NATS credential format is routed_to `content_gotcha` and placed in the API codebase's README; the worker codebase's README has a one-line "See apidev/README.md §Gotchas for NATS credential format" cross-reference rather than a second gotcha bullet.

A rewrite of the same fact into two different stems is still one fact. The honesty check tokenizes both stems and compares set-overlap. Diverging vocabulary does not create a new fact.
