# brief-editorial-review-showcase-simulation.md — cold-read simulation

**Purpose**: simulate receiving the composed editorial-review brief (from [brief-editorial-review-showcase-composed.md](brief-editorial-review-showcase-composed.md)) cold — no prior context, no run-log access, no spec-content-surfaces.md in context. Document ambiguities, contradictions, and impossible-to-act-on instructions. Per principle P7.

**Role**: editorial-review
**Tier**: showcase
**Simulation stance**: fresh Opus 4.7 instance, no prior zcprecipator2 exposure

---

## 1. What reads cleanly

The composed brief establishes the role quickly. Porter-premise is load-bearing and scans as clear: "you ARE the porter; read the deliverable cold; you have not worked on this recipe." Surface-walk-task enumerates the order (root → env README → env import.yaml → per-codebase README × 3 surfaces → CLAUDE.md → zerops.yaml). Single-question-tests maps each surface to one pass/fail predicate.

The classification-reclassify section reads as an executable procedure: read fact → independently apply 7-class taxonomy → compare to writer's → increment delta. The 7 classifications are each defined in a sentence; the routing destinations are declared.

Counter-example-reference is the strongest atom — five named v28 anti-patterns with concrete examples (`api.ts` helper gotcha, `setGlobalPrefix` framework quirk, `zsc execOnce` self-inflicted, etc.). Pattern-matching against named examples is tractable.

Reporting-taxonomy + completion-shape close cleanly: CRIT/WRONG/STYLE semantics declared with inline-fix policy; return payload structure listed field-by-field.

## 2. Ambiguities flagged

### 2.1. "Cold-read" premise vs tool access

> You carry no authorship investment. Read the deliverable cold.

But the brief then says:

> For each fact in ZCP_CONTENT_MANIFEST.json... Independently apply the 7-class taxonomy.

**Ambiguity**: is the reviewer permitted to read `ZCP_CONTENT_MANIFEST.json`? The manifest is a writer-authored artifact; reading it anchors the reviewer to the writer's classification BEFORE independent reclassification.

**Proposed clarification**: the porter-premise atom + classification-reclassify atom should explicitly declare the reading order: *"Do not read ZCP_CONTENT_MANIFEST.json until AFTER you have walked the deliverable and formed your own surface-by-surface assessment. Read the manifest only for the reclassify step — read each fact's mechanism + observable behavior, form YOUR classification, then compare."* Otherwise the reviewer reads the manifest first, absorbs the writer's framing, and "independent" reclassification is contaminated.

### 2.2. Pointer-include vs full access

> interpolate {factsLogPath = /tmp/zcp-facts-{sessionID}.jsonl, manifestPath = /var/www/ZCP_CONTENT_MANIFEST.json}

The facts log is provided as a pointer but the brief doesn't say whether the reviewer SHOULD read it. Porter-premise implies the porter wouldn't have access to the facts log (it's a run-internal artifact). Classification-reclassify needs the manifest. Citation-audit needs the `zerops_knowledge` guide bodies.

**Proposed clarification**: the `mandatory-core.md` atom should declare: *"Read `zerops_knowledge` guides for citation-audit (pointer: MCP `zerops_knowledge` tool). Read ZCP_CONTENT_MANIFEST.json for classification-reclassify (after you form independent surface assessments). Do NOT read the facts log — it's a run-internal artifact a porter wouldn't have access to. The pointer is provided for audit-trail reference only."*

### 2.3. Tool-use policy inconsistency

> Your permitted tools: Read, Grep, Glob, Edit, Write, Bash (for SSH-side grep/jq/wc only; no mutation via Bash).

But editorial reviews apply inline-fixes:

> Inline-fix policy: CRIT items MUST be fixed before return. If fix requires deletion... If fix requires rewrite to correct surface, rewrite.

**Ambiguity**: Edit + Write permit file mutation but Bash-mutation is forbidden. Is `Bash: ssh {host} "cat file"` permitted (read-only shell)? What about `Bash: ssh {host} "rm orphan-file"` (SSH-side delete, not mutation of the Bash process but of the remote file)?

**Proposed clarification**: `mandatory-core.md` should declare: *"Edit + Write permitted for inline-fixing content on the SSHFS mount. Bash permitted for SSH-side read-only commands (cat, grep, jq, wc, awk, find). Bash commands that mutate remote files are forbidden — use Edit/Write via SSHFS mount instead. SSH-side deletion of editorial-scope files (e.g., orphan canonical-tree duplicates) is permitted via Bash `ssh {host} 'rm <specific-path>'` but the path MUST be in the reviewer's intended-delete set from classification-reclassify decisions."*

### 2.4. Classification for edge cases

The 7-class taxonomy gives clear tests for most cases but edge cases are under-specified:
- **Platform-invariant AND self-inflicted simultaneously**: v34 DB_PASS — the env-var name mismatch is self-inflicted (scaffolds disagreed) AND the underlying platform invariant (cross-service env vars auto-inject by specific name) is real. Does the reviewer classify as `self-inflicted` (DISCARD) or `platform-invariant` (route-to-gotcha)?
- **Intersection vs framework-quirk boundary**: the rule "does the Zerops side contribute materially to the failure mode" is subjective. "Materially" isn't defined.

**Proposed clarification**: classification-reclassify.md should include 2-3 worked edge-case examples showing the reviewer's reasoning. v34 DB_PASS as example: "self-inflicted at source (scaffolds disagreed on var name); the underlying invariant (auto-inject by platform-declared name) belongs in a SEPARATE gotcha about the general class (env-var auto-inject + SymbolContract discipline), not as a gotcha documenting THIS recipe's scaffold bug. Write the general gotcha OR cite env-var-model guide; DO NOT ship the scaffold bug as a gotcha."

### 2.5. Cross-surface ledger — cross-refs definition

> Each fact lives on ONE surface. Other surfaces cross-reference — they do not re-author.

**Ambiguity**: what counts as a "cross-reference"? A two-sentence summary with a link? A one-line mention? A pointer like "see apidev README §KB"?

**Proposed clarification**: `cross-surface-ledger.md` should declare positive form: *"A cross-reference is ≤ 2 sentences naming the destination surface + the specific section anchor, AND does NOT restate the mechanism. Example acceptable cross-ref: 'See apidev/README.md knowledge-base §2 for cross-service env var auto-injection semantics.' Example NOT acceptable (counts as duplication): 'Cross-service env vars auto-inject project-wide — see apidev/README.md knowledge-base §2.' — this restates the mechanism."*

## 3. Contradictions flagged

### 3.1. "No run log" vs citation-audit on `zerops_knowledge` guides

Porter-premise says no run log. But citation-audit requires checking gotchas against `zerops_knowledge` guide bodies (which are MCP tool responses, not run-log artifacts). These are distinct — guides are external platform docs; run log is internal session transcript. The brief conflates them by default.

**Not a contradiction but worth explicit**: `citation-audit.md` should declare: *"`zerops_knowledge` guides are external Zerops platform documentation a porter can access. Reading them is required for citation-audit. This is not 'reading the run log.'"*

## 4. Impossible-to-act-on instructions

None surfaced in cold-read. Every task has:
- Concrete artifact path
- Concrete predicate to evaluate
- Concrete reporting category

The WRONG-deferred exception ("if a WRONG fix would cascade beyond editorial scope, report without fix") is clear.

## 5. Proposed atom edits

Based on §2 ambiguities + §3 contradiction:

1. **`porter-premise.md`**: add reading-order discipline — walk + form assessment BEFORE reading manifest.
2. **`mandatory-core.md`**: clarify tool-use scope (Edit/Write permitted; Bash read-only; SSH-side delete permitted for editorial-scope).
3. **`classification-reclassify.md`**: add 2-3 worked edge-case examples (v34 DB_PASS class + intersection-vs-framework-quirk boundary case + self-inflicted-plus-platform-invariant case).
4. **`cross-surface-ledger.md`**: declare positive cross-reference form.
5. **`citation-audit.md`**: declare `zerops_knowledge` guides as external-doc-access permitted (not run-log).

All 5 are in-atom content clarifications, not structural changes. Ship as part of C-4 atom-authoring in cleanroom rollout.

## 6. Cold-read verdict

**PASS conditional on the 5 proposed clarifications in §5.** Composed brief is reviewable by a cold reader in under 3 minutes (target per P7). Task is actionable. Ambiguities are closable in-atom. No atom needs restructuring.

## 7. Defect classes cold-read catches

A cold reader walking the composed brief ASKS these questions (the questions themselves are the defect-class catches):

- "If I find a gotcha whose claim contradicts the env-var-model guide, what do I do?" → citation-audit atom answers: WRONG unless reclassified as DISCARD.
- "If the manifest says classified=platform-invariant but I read the fact and it's framework-quirk, what do I do?" → classification-reclassify answers: reclassification_delta += 1; propose DISCARD route; CRIT class.
- "If the same fact body appears on 3 surfaces, what do I do?" → cross-surface-ledger answers: duplicates += 1; pick canonical surface per routing-matrix (from writer brief, not editorial brief — editorial reads writer's routing to know canonical destination); WRONG class.
- "If a gotcha is about `api.ts` helper, what do I do?" → counter-example-reference answers: match to v28 appdev gotcha #4 scaffold-decision-disguised class; CRIT (either rewrite as framework-agnostic principle OR delete).

All four catch-points are the defect classes registry rows 8.2, 8.3, 14.1, 14.4, 15.1 target. Cold-read verifies the atoms instruct for each.
