# brief-writer-showcase-simulation.md

**Purpose**: cold-read simulation of composed writer-showcase brief.

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | `canonical-output-tree.md` — "The ONLY files you write" list uses hostname literals (apidev, appdev, workerdev). For a different showcase recipe (e.g. `laravel-showcase`), the list would differ. | Interpolation path is implicit; the stitcher must inject `.Hostnames` iteration. Unclear from the atom text. | low — fix in stitcher: emit `for h in {{range .Hostnames}}{{.dev}}{{end}}` expansion |
| A2 | `fresh-context-premise.md` — "Inputs you do NOT have: the run transcript, the main agent's context, memory of what went wrong" | Reader asks: so what about the SymbolContract, which IS interpolated? The premise says "no memory" but the contract is a structured input. No contradiction but the line between "no memory" and "here is the contract" could confuse. | low — add clarifying sentence: "The SymbolContract is a structured input, not memory; it names the decisions scaffold/feature already locked in." |
| A3 | `routing-matrix.md` — "A fact routed to `content_intro` appears in the intro fragment as paraphrase" | Intro is 1-3 lines of plain prose. Paraphrasing a detailed fact into an intro isn't always possible. What if the fact is "Meilisearch 1.20 requires forced-path-style" — that doesn't belong in intro. | medium — the matrix implies every fact routes SOMEWHERE; some facts may not fit any route except `discarded`. Make that explicit: "if no surface fits, route to `discarded` with override_reason." |
| A4 | `manifest-contract.md` — "routed_to enum" includes 9 values | `scaffold_preamble` and `feature_preamble` are routing destinations for facts recorded during scaffold/feature that the writer sees but shouldn't publish. How does the writer handle them? Leave alone? Write them to manifest as routed_to=scaffold_preamble? That's fine but not obvious. | medium — clarify: "facts with scope=downstream are not in your input anyway (facts log filter); facts you see with RouteTo already set to `scaffold_preamble` or `feature_preamble` should be recorded in the manifest as-is (source of truth) and NOT surfaced in any content surface." |
| A5 | `self-review-per-surface.md` pre-attest command `zcp check manifest-honesty --mount-root=/var/www/` | Assumes the `zcp` CLI is installed in the SSHFS mount's PATH on zcp orchestrator. If the writer sub-agent runs it, it needs PATH access. | low — the zcp binary IS on the orchestrator by definition; it's the agent's own binary. No impossibility. |
| A6 | `content-surface-contracts.md` row 6 "Env `import.yaml` comments (via env-comment-set payload)" | Writer emits payload via completion-shape JSON; does not write `env*/import.yaml` directly. But the surface is in the table row 6. Is this writer's output surface or main-agent's? | medium — add one-liner: "you produce the payload; main-agent applies it at finalize. The SURFACE (what the reader sees) is authored by you; the WRITE mechanism is main-agent." |

## 2. Contradictions

| # | A | B | Resolution |
|---|---|---|---|
| C1 | `canonical-output-tree.md`: "The ONLY files you write" lists 6 READMEs/CLAUDE.mds + manifest. | `self-review-per-surface.md` pre-attest command greps for fragment markers in /var/www/$h/README.md where $h ∈ {apidev, appdev, workerdev}. | consistent. No contradiction. |
| C2 | `content-surface-contracts.md` showcase tier requires ≥3 net-new gotchas beyond predecessor. | `fresh-context-premise.md`: "You have no memory of the run, no transcript." | **partial contradiction** — the predecessor-floor check needs comparison with the previous recipe's README. Writer doesn't have access to that; it's a server-side check against a pinned predecessor blob. The brief's "≥3 net-new" requirement is thus unverifiable author-side. The gate will fail and the writer iterates blindly. | Resolution: `knowledge_base_exceeds_predecessor` is flagged for DELETE in check-rewrite.md §15 — so this contradiction is resolved by the check's removal. Update brief: "quality targets ≥3 authentic gotchas per codebase; the previous system's predecessor-floor is being retired." |
| C3 | `routing-matrix.md`: "A fact routed to `claude_md` must NOT appear in any README knowledge-base fragment." | `classification-taxonomy.md`: "operational" facts default to CLAUDE.md — and the writer might have a worker gotcha about graceful shutdown that is ALSO operational ("how to drain on SIGTERM is both a platform gotcha AND a repo operational note"). | ambiguity not contradiction — same fact would be split into two entries in the manifest? Or routed once to content_gotcha with a duplicate line in CLAUDE.md? Per the matrix the fact gets one entry, one route. Writer must choose primary surface. Add guidance: "when a fact spans surfaces, pick the primary surface per classification taxonomy; copies in other surfaces are allowed only if they serve the other surface's distinct reader test." |

## 3. Impossible-to-act-on

| # | Instruction | Why | Fix |
|---|---|---|---|
| I1 | `content-surface-contracts.md` showcase tier "≥3 net-new gotchas beyond the predecessor" | Writer cannot access the predecessor's README at dispatch time. | See C2 resolution — remove the predecessor-floor requirement; keep the authenticity bar. |
| I2 | `self-review-per-surface.md` aggregate assumes `zcp` binary in PATH | Requires CLI shim from check-rewrite.md §18 to be built. Until then, `zcp check manifest-honesty` fails as "command not found." | Only impossible pre-implementation. At runtime of the rewrite, the shim is in place. Deferred issue; not a P7 blocker. |

## 4. Defect-class cold-read scan

| Class | Ships? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No | where-commands-run pointer (though writer rarely uses SSH) |
| v21-framework-token-purge | No | atomization — framework content is per-codebase, not cross-codebase-shared |
| v25 substep-bypass | No | writer dispatched via Agent, not zerops_workflow |
| v28 debug writes content | **No** — `fresh-context-premise.md` atom is the structural fix | |
| v28 writer-kept-despite-DISCARD | No — `manifest-contract.md` enforces override_reason + author-runnable pre-attest checks the discard consistency |
| v28 33% genuine gotchas | Mitigated — classification taxonomy, routing matrix, authenticity rule, citation map |
| v29 env-README-factual-drift | N/A — env READMEs are Go-template emitted at finalize, not writer territory |
| v29 ZCP_CONTENT_MANIFEST missing | No — manifest-contract atom mandates the file |
| v33 phantom output tree | **No** — `canonical-output-tree.md` atom is the positive-form allow-list (P8); declares the ONLY valid paths. Any non-listed path is out-of-scope. v33 class closed. |
| v33 paraphrased env folder names | **No** — env folder names are Go-template emitted; writer produces an env-comment-set PAYLOAD, not the folders. |
| v33 Unicode box-drawing | No | visual-style pointer |
| v34 manifest ↔ content inconsistency (DB_PASS) | **No** — `routing-matrix.md` enumerates EVERY (routed_to × surface) pair; `manifest-contract.md` requires override_reason for non-default routings; pre-attest runs `manifest-honesty` across all dimensions. | v34 closure — primary coverage. |
| v34 convergence architecture | No | pre-attest runnable per check; gate becomes confirmation |

## 5. P7 verdict

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | **PASS** — C2 resolved by predecessor-floor deletion; C3 resolved by cross-surface-fact guidance |
| Author-runnable pre-attest for each check | PASS — self-review atom has explicit aggregate |
| Every applicable v20-v34 defect class has a prevention mechanism | PASS |
| No dispatcher text (P2) | PASS |
| No version anchors (P6) | PASS |
| No internal check vocabulary transmitted | PASS (the pre-attest commands DO name `writer_content_manifest_exists` etc. via shim — but as runnable-check names, not internal server vocabulary. The atom uses `zcp check manifest-honesty` — the shim's CLI name — which IS transmitted, but as an author-runnable command, not internal implementation detail.) |
| No Go-source paths | PASS |

**Net**: passes conditional on the four medium-severity clarifications in §1 (A1 hostname interpolation, A3 "route to discarded if no fit," A4 downstream-preamble handling, A6 "you produce payload, main applies").

## 6. Proposed edits

- Explicit hostname interpolation in canonical-output-tree.md.
- Add "if no surface fits, route to discarded with override_reason" to routing-matrix.md.
- Add "scaffold_preamble / feature_preamble facts: record in manifest as-is; do NOT surface" note to manifest-contract.md.
- Add "you produce the env-comment-set payload; main-agent writes it at finalize" clarifier.
- Drop predecessor-floor wording; update "showcase tier requires" to "showcase tier targets ≥3 authentic gotchas per codebase."
- Add cross-surface-fact guidance to routing-matrix.md.
