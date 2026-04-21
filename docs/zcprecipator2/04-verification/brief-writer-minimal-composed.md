# brief-writer-minimal-composed.md

**Role**: writer (per-codebase README + CLAUDE.md + manifest)
**Tier**: minimal
**Delivery**: per data-flow-minimal.md §5a, minimal's default is **Path A — main-inline** (main consumes the writer atoms in-band at deploy.readmes). Path B — dispatch writer sub-agent — is supported optionally.

**Source atoms** (same tree as showcase writer per atomic-layout.md §7; tier-conditional sections handle deltas):

```
briefs/writer/mandatory-core.md
briefs/writer/fresh-context-premise.md
briefs/writer/canonical-output-tree.md     (hostname interpolation: single appdev for minimal)
briefs/writer/content-surface-contracts.md (tier-conditional: drops "≥3 net-new gotchas beyond predecessor" for minimal; drops worker-specific section)
briefs/writer/classification-taxonomy.md
briefs/writer/routing-matrix.md
briefs/writer/citation-map.md
briefs/writer/manifest-contract.md
briefs/writer/self-review-per-surface.md
briefs/writer/completion-shape.md
+ pointer-include principles/where-commands-run.md
+ pointer-include principles/file-op-sequencing.md
+ pointer-include principles/tool-use-policy.md
+ pointer-include principles/comment-style.md
+ pointer-include principles/visual-style.md
+ interpolate {sessionID, factsLogPath, plan.Research}
```

Interpolations (example): `{{.SessionID}} = <new session>`, `{{.FactsLogPath}} = /tmp/zcp-facts-<session>.jsonl`, `{{.Hostnames}} = [appdev]` (single-codebase) or `[appdev, apidev]` (dual-runtime minimal), `{{.Plan.Tier}} = minimal`, `{{.Framework}} = NestJS` (or whichever).

---

## Composed brief (main consumes this at deploy.readmes substep — Path A default)

```
You are authoring reader-facing content with NO memory of the deploy debug rounds. This note applies even when main is consuming the composition in-band: reader-facing content must be framed from the reader's perspective, not the author's debug experience.

Session ID: <interpolated>.
Facts log: /tmp/zcp-facts-<session>.jsonl.

--- [briefs/writer/mandatory-core.md] ---

(Same as showcase writer mandatory-core: tools permitted/forbidden, file-op sequencing.)

--- [principles/where-commands-run.md] ---

(Same positive-form atom.)

--- [briefs/writer/fresh-context-premise.md, main-inline-aware] ---

Inputs you HAVE:

1. The facts log at /tmp/zcp-facts-<session>.jsonl — `cat` it. Each line is a FactRecord. Scope filters: `content` (or unset) → published content; `downstream` → skip; `both` → consider. If RouteTo is set, honor unless you document override_reason.

2. Mount paths — read-only for scaffold/feature-produced files, read/write for files you own.

3. mcp__zerops__zerops_knowledge — see Citation Map.

4. SymbolContract (interpolated) — use for citation consistency.

When main consumes this in-band (default for minimal), remember: your content is still authored fresh-context. The fact that YOU lived the debug rounds doesn't change the rule — write what a porter wants to read, not what you remember. Prefer fact-log-sourced gotchas over memory-sourced ones.

Inputs you do NOT have when writing (treat as mental discipline in main-inline path): the dispatched-session transcript, the debug-heavy internal state. Go cold against the facts log.

--- [briefs/writer/canonical-output-tree.md, tier=minimal] ---

The ONLY files you write:

- `/var/www/appdev/README.md` and `/var/www/appdev/CLAUDE.md`.
- (If dual-runtime minimal:) also `/var/www/apidev/README.md` + `/var/www/apidev/CLAUDE.md`.
- `/var/www/ZCP_CONTENT_MANIFEST.json`.

Out of scope for direct write (handled by Go-template emission at finalize): root recipe README, env READMEs, env import.yaml files. Emit env comments via the env-comment-set payload in your completion message; main applies at finalize.

Any path not in the "ONLY files you write" list is out of scope. The publish CLI ignores them.

--- [briefs/writer/content-surface-contracts.md, tier=minimal] ---

Six content surfaces (same as showcase):

| # | Surface | Reader | Shape |
|---|---|---|---|
| 1 | Per-codebase README intro fragment | Recipe-page browser | 1–3 lines, plain text |
| 2 | Per-codebase README integration-guide fragment | Porter bringing their own app | H3 per platform-forced change + code block |
| 3 | Per-codebase README knowledge-base fragment | Dev hitting platform failure | `### Gotchas` H3 with `- **symptom** — mechanism + evidence` bullets |
| 4 | Per-codebase CLAUDE.md | Operator of THIS repo | Plain markdown, Dev Loop / Migrations / Container Traps / Testing + ≥2 custom; ≥1200 bytes |
| 5 | Per-codebase zerops.yaml `#` comments | Deploy-config reader | ASCII `#`, one per line |
| 6 | Env `import.yaml` comments via env-comment-set payload | Env-tier chooser | Per-env per-service; WHY not WHAT |

Minimal-tier deltas:

- No "≥3 net-new gotchas beyond predecessor" requirement (the predecessor-floor check is being retired; authenticity bar replaces it).
- Authenticity bar: each gotcha either names a platform mechanism or describes a failure mode with evidence.
- Cross-README gotcha uniqueness applies IF more than one codebase (dual-runtime minimal). Single-codebase minimal trivially satisfies.
- No worker-specific knowledge-base requirements (minimal has no separate-codebase worker).
- Showcase-only content checks (comment_specificity, knowledge_base_exceeds_predecessor, knowledge_base_authenticity_showcase-level, integration_guide_code_adjustment, integration_guide_per_item_code) are tier-filtered OUT for minimal.

--- [briefs/writer/classification-taxonomy.md] ---

(Same 7-class taxonomy: invariant, intersection, framework-quirk, library-meta, scaffold-decision, operational, self-inflicted. DISCARD default for framework-quirk / library-meta / self-inflicted.)

--- [briefs/writer/routing-matrix.md] ---

(Same `routed_to` enum: content_gotcha / content_intro / content_ig / content_env_comment / claude_md / zerops_yaml_comment / scaffold_preamble / feature_preamble / discarded. Same matrix rules.)

Tier note: minimal's routing is simpler in practice (fewer facts, fewer surfaces, single codebase), but the matrix rules apply identically. v34 manifest-content-inconsistency is structurally closed here too.

--- [briefs/writer/citation-map.md] ---

(Same citation map. Fewer topics are usually applicable for minimal — typically env-var-model, init-commands, readiness-health-checks. Object-storage / rolling-deploys / http-support / deploy-files apply when the minimal recipe uses those services.)

--- [briefs/writer/manifest-contract.md] ---

(Same JSON schema. Every distinct FactRecord.Title with scope != downstream gets exactly one manifest entry. Default-discard classifications routed non-discarded need override_reason.)

--- [briefs/writer/self-review-per-surface.md, tier=minimal] ---

Before attesting (or returning if dispatched), for each item you wrote answer the surface test. Any "no" → remove; do NOT rewrite.

Author-runnable pre-attest aggregate (tier=minimal):

    # manifest exists + parseable
    test -f /var/www/ZCP_CONTENT_MANIFEST.json && jq empty /var/www/ZCP_CONTENT_MANIFEST.json

    # every fact has routed_to populated (new check)
    jq '[.facts[] | select(.routed_to==null or .routed_to=="")] | length' /var/www/ZCP_CONTENT_MANIFEST.json | grep -qE '^0$'

    # discard classification consistency
    jq '[.facts[] | select(.classification=="framework-quirk" or .classification=="library-meta" or .classification=="self-inflicted") | select(.routed_to != "discarded") | select(.override_reason == "")] | length' /var/www/ZCP_CONTENT_MANIFEST.json | grep -qE '^0$'

    # manifest honesty (all routing dimensions, per P5)
    zcp check manifest-honesty --mount-root=/var/www/

    # per-codebase fragment presence (iterate {{.Hostnames}})
    for h in appdev; do
      grep -q '#ZEROPS_EXTRACT_START:intro' /var/www/$h/README.md &&
      grep -q '#ZEROPS_EXTRACT_START:integration-guide' /var/www/$h/README.md &&
      grep -q '#ZEROPS_EXTRACT_START:knowledge-base' /var/www/$h/README.md || { echo FAIL: fragments in $h; exit 1; }
    done

    # CLAUDE.md floor
    for h in appdev; do
      test $(wc -c < /var/www/$h/CLAUDE.md) -ge 1200 || { echo FAIL: $h CLAUDE.md under 1200 bytes; exit 1; }
    done

    # canonical-output-tree
    ! find /var/www -maxdepth 2 -type d -name 'recipe-*'

(Dual-runtime minimal: iterate over both hostnames; add cross-README dedup via `zcp check cross-readme-dedup`.)

--- [briefs/writer/completion-shape.md] ---

Return:

1. Files written + byte counts.
2. Classification summary (fact counts per class).
3. Self-review per-surface answers.
4. env-comment-set JSON payload (6 env stages — 0 AI Agent, 1 Remote CDE, 2 Local, 3 Stage, 4 Small Production, 5 HA Production). Per-env service comments for `appdev`/`appstage` (plus `apidev`/`apistage` if dual-runtime) + any managed service (`db`, `redis`, etc. as applicable) + a project-level comment. Comments explain decisions (scale, HA mode, minContainers), not field meaning.
5. Pre-attest aggregate exit code (must be 0).

--- [principles/comment-style.md + principles/visual-style.md] ---

(Same — ASCII-only.)
```

**Composed byte-budget**: ~9 KB (smaller than showcase writer — single codebase, fewer surfaces, no worker-specific requirements, no predecessor-floor language).

**Tier-conditional sections that FIRE in this composition**:
- canonical-output-tree hostname list → single (or dual-runtime) hostnames only
- content-surface-contracts → drops showcase-only requirements (≥3 net-new, worker-specific rules)
- self-review aggregate → iterates minimal's hostnames
- completion-shape → env-comment-set payload covers minimal's service set

**Tier-conditional sections that are FILTERED OUT**:
- Worker-specific knowledge-base rules (queue-group + SIGTERM) — minimal has no worker
- Showcase-only gate checks (comment_specificity, predecessor-floor)
- Cross-README dedup (unless dual-runtime minimal)
