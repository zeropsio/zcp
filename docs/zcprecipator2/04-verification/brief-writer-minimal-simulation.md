# brief-writer-minimal-simulation.md

**Purpose**: cold-read simulation of composed minimal-writer composition. Delivery path is main-inline (default per data-flow-minimal.md §5a).

## 1. Ambiguities

| # | Locus | Ambiguity | Severity |
|---|---|---|---|
| A1 | `fresh-context-premise.md, main-inline-aware` "remember: your content is still authored fresh-context" | Main has the full deploy context in its session — it literally has memory of what went wrong. The atom asks main to voluntarily "go cold against the facts log." Unverifiable discipline. | **medium** — This is the core tension of main-inline writer path for minimal. Per data-flow-minimal.md §11 item 1+2, this is a step-4 verification concern. **Verdict**: the atom is correct to flag it; the enforcement comes from the author-runnable pre-attest aggregate + the classification-taxonomy making memory-biased gotchas harder to route. Honest: main cannot truly simulate fresh-context — but the checks catch the output regardless of process. |
| A2 | `canonical-output-tree.md, tier=minimal` "(If dual-runtime minimal:) also `/var/www/apidev/README.md`..." | Parenthetical conditional. Stitcher needs to emit without parenthetical when single-codebase, and with hostname list when dual-runtime. | low — stitcher branches. |
| A3 | `content-surface-contracts.md, tier=minimal` "No `≥3 net-new gotchas beyond predecessor` requirement" | The retired predecessor-floor check survives under "knowledge_base_exceeds_predecessor (delete)" per check-rewrite.md §15 — but only the minimal-tier branch is tagged for retirement. Showcase also retires it (check-rewrite says delete, not tier-specific). Consistent messaging needed. | low — writer brief for showcase also dropped the predecessor-floor language per `brief-writer-showcase-composed.md`. Both tier writers are aligned. |
| A4 | `self-review-per-surface.md, tier=minimal` — "(Dual-runtime minimal: iterate over both hostnames; add cross-README dedup)" | The aggregate as written iterates only `appdev`. Stitcher must emit the full hostname list based on `.Hostnames`. | low — stitcher. |
| A5 | Recipe-slug interpolation in fact-log path | `sessionID` is dynamic; how does stitcher know it? The workflow state carries it. | low — stitcher reads from workflow state. |
| A6 | `content-surface-contracts.md` tier-filter prose "Showcase-only content checks are tier-filtered OUT for minimal" | Implies there's a check-tier-gate somewhere. The code is in the Go layer (`recipe_substeps.go` tier-gated checks). The brief does not name the Go code directly (good — P2) but the note is abstract. | low — OK; the statement is correct; check-rewrite.md §5 records tier-gating. |

## 2. Contradictions

| # | A | B | Resolution |
|---|---|---|---|
| C1 | "Default is main-inline (Path A)" | `briefs/writer/*` atoms use phrasing like "before returning, run self-review..." which implies sub-agent return semantics | In main-inline mode "returning" means "before attesting deploy.readmes complete." Stitcher can substitute phrasing ("before attesting" in main-inline mode). Same pattern as minimal scaffold's audience-adaptation concern. |
| C2 | `classification-taxonomy.md` taxonomy is same as showcase | `fresh-context-premise.md, main-inline-aware` says "honest: main cannot truly simulate fresh-context but the checks catch the output" | Not a contradiction. The taxonomy is what matters, not the author's mental state. Process-inference doesn't change routing rules. |

## 3. Impossible-to-act-on

| # | Instruction | Why | Fix |
|---|---|---|---|
| I1 | `self-review-per-surface.md` aggregate uses `zcp check manifest-honesty` | Requires shim CLI (same concern across all briefs). | Deferred to rewrite runtime. |
| I2 | "Go cold against the facts log" when main has full memory | Unenforceable discipline. | Acknowledged in atom (A1). Enforcement shifts to the pre-attest checks on the output (not the process). |

## 4. Defect-class cold-read scan

| Class | Ships? | Evidence |
|---|---|---|
| v17 SSHFS-write-not-exec | No | pointer |
| v21 framework-token-purge | No | atomization + tier-filtering |
| v22 NATS creds | N/A for most minimals (no NATS) |
| v22 queue-group | N/A (no worker) |
| v25 substep-bypass | No — main attests via workflow normally |
| v28 debug-agent-writes-content | **Partially mitigated** — A1's concern. In main-inline path for minimal, the same agent that debugged the deploy is writing the content. Fresh-context is aspirational here, enforced only by output checks. |
| v28 writer-DISCARD-override | No — manifest + check covers |
| v28 33% genuine gotchas | Mitigated — taxonomy + authenticity + citation-map still apply |
| v29 ZCP_CONTENT_MANIFEST missing | No — manifest-contract requires file |
| v30 worker SIGTERM | N/A |
| v33 phantom output tree | No — canonical-output-tree positive form; single/dual-codebase hostnames only |
| v33 paraphrased env folder names | No — Go-template emission at finalize |
| v33 Unicode box-drawing | No — visual-style pointer |
| v34 manifest-content-inconsistency | No — routing-matrix enumerates all pairs; aggregate includes `zcp check manifest-honesty` |
| v34 cross-scaffold env-var | Trivially N/A for single-codebase minimal; contract applies for dual-runtime minimal |
| v34 convergence architecture | No — runnable pre-attest aggregate |

## 5. P7 verdict

| Criterion | Status |
|---|---|
| Cold-reader finishes without unresolvable contradictions | **PASS with 1 important caveat** — A1 (main-inline writer cannot truly be fresh-context; enforcement is output-side). This is the primary escalation trigger per data-flow-minimal.md §11. |
| Author-runnable pre-attest | PASS |
| Every applicable v20-v34 defect class has a prevention mechanism | PASS (with the A1 caveat) |
| No dispatcher text (P2) | PASS (atoms adapted to main-inline via stitch-time phrasing substitution; OR atoms are audience-neutral enough) |
| No version anchors (P6) | PASS |
| No internal check vocabulary | PASS |
| No Go-source paths | PASS |

**Net**: passes conditional on audience-adaptation of atoms and acknowledgement that main-inline fresh-context is aspirational. The enforcement moves from process ("you have no memory") to output (pre-attest aggregate + content-manifest honesty).

## 6. Escalation trigger check (RESUME decision #1)

Per data-flow-minimal.md §11 item 2: "Minimal writer Path A (main-inline): does `briefs/writer/*` stitch sensibly for main? Any dispatcher-implying verbs ('dispatch', 'return') are P2 violations when main is the reader."

Audit: verbs flagged in the composition text — "return" appears in completion-shape; "before returning" in self-review; "you are a sub-agent" is absent (good). In main-inline mode these verbs need stitch-time substitution ("before attesting" / "at completion of this substep"). This is a concrete stitcher implementation concern, not an architectural blocker.

**Escalation recommendation**: do NOT commission a minimal run specifically to verify this. Stitcher-level text adaptation is a local concern resolvable by:
1. Adding `AtomAudienceMode ∈ {inband, dispatched}` to the stitching contract.
2. Having `briefs/writer/*` atoms declare their audience-mode conditional phrasing.
3. Verifying in step-4 simulation that both modes produce a sensible composition — done here.

## 7. Proposed edits

- Stitcher: audience-mode-aware phrasing substitution for main-inline consumption.
- Simplify A1 acknowledgement: "fresh-context is an authoring discipline — your output is checked regardless."
- Ensure stitcher handles hostname list interpolation for single vs dual-runtime minimal.
