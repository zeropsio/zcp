# Codex wide-perspective critique of the flow-eval leaf-fix plan (2026-06-05)

Codex (gpt-5.5, xhigh) reviewed `flow-eval-friction-report-2026-06-05.md` +
`flow-eval-fix-master-plan-2026-06-05.md` against the live code. Verdict + the durable insights
(preserved because Karel is redirecting toward a deeper response-delivery audit — the leaf-fix
plan is now an INPUT, not the focus).

**Top-line:** "Change the plan before shipping. It fixes many leaf defects, but it under-reacts
to two root signals: the dominant adopt friction belongs partly in the handler, and 'guidance
exists' is not the same as 'the agent can use it.'"

**Root the bottom-up plan missed (the key line):** *"Information contract includes REACHABILITY
and TIMING. A correct fact buried below the fold is not a correct tell, and a boolean that was
true before the last mutation is not live truth."* → unifies F7/R1 (reachability) + F11/F60 (timing).

## The six positions
1. **F1 — change the plan; atom-only too weak.** Handler has live hostnames + types at
   `adopt.go:135`, refuses ALL two-same-bare-type pairs at `:158`. Over-cautious for exact
   `appdev`/`appstage`. Invariant should be "only high-confidence standard suffix pairs auto-derive;
   ambiguous still require choice." Auto-derive `PlanModeStandard` when both match `<base>dev`+
   `<base>stage` + share canonical bare type, each host's live type; keep `ErrAdoptPairingChoice`
   otherwise; document explicit-plan override. Split `adopt_autoderive_test.go:152` into
   `suffix_pair_auto_derives_standard` + `non_suffix_same_type_refuses_with_templates`.
   *(Note: the removal-history at adopt.go:32-39 killed the BROAD heuristic — `frontend-app`+
   `frontend-app-prod` misclassification; Codex's NARROW exact-suffix version dodges that exact case.)*
2. **"Already fixed" recurrence — no-action wrong.** F2/F15, F51, F17 recurring despite existing
   guidance ⇒ guidance not EFFECTIVE (buried in develop-active wall, F7). Promote
   `plans/backlog/r1-context-relevance-below-cap.md` now: surface close-mode/git-push/dev-server
   blocks in the decision HEAD. F17 also needs a structured `nextCall` field on the route-menu
   response (recurred despite `kind` + schema text).
3. **META under-scoped.** Plan fixes F11 only. **F60 is a REAL timing bug:**
   `work_session.go:624 serviceAutoCloseReady` checks latest-deploy-success + latest-verify-pass
   but NOT that verify happened AFTER the deploy — a pre-deploy verify satisfies a post-deploy gate
   after the deploy killed the dev server. Fix: require verify-pass at-or-after latest successful
   deploy; unit test (verify t1, deploy t2 → not ready). F34/F61 = source/target contract:
   `build-integration` is source-pair-keyed but user language is target/pipeline-keyed; surface
   "source appdev, build target appstage" as a hard confirmation; production verification routes
   through launch status/pipeline summary, not build-integration.
   **P2 tightening:** stamping `FirstDeployedAt` from `strings.Contains(importYAML,"buildFromGit")`
   REPEATS the stored-proxy anti-pattern → derive from parsed recipe shape + live `DiscoveredStatuses`
   RUNNING/ACTIVE.
4. **P2/P4 — no code conflict** (bootstrap_outputs.go:75/171 vs engine.go:459 + workflow_bootstrap.go:134,
   adjacent not overlapping). Land P4 with a handler-boundary test: omitted `plan` rejects but
   explicit `plan:[]` still commits classic managed-only discover.
5. **Eval protocol — 2-3 LLM runs are smoke, not proof.** Add DETERMINISTIC rendered-output
   assertions per guidance fix (correct atom selected, bad example absent, required instruction in
   the FIRST decision block, structured response carries the next call). Live eval then proves
   fresh-transcript ERGONOMICS, not mechanism correctness.
6. **Scope.** P1 isn't "pure tell" (atom + prod Go hint + guide sync + goldens) → split F57 guide
   sync. P5 `compose-ready` allow-list is symptom-level: if `exportStatus` is owned by topology,
   the atom-axis allow-list should DERIVE from that owner / be pinned by a set-comparison test —
   hand-adding one enum while leaving `variant-prompt` drift is the exact copy-drift the plan claims
   to kill.

## Three changes before shipping (Codex)
1. Replace F1 atom-only with handler auto-derive for exact `*dev`/`*stage` pairs + explicit-plan override.
2. Promote R1 below-cap relevance now + structured route-menu `nextCall` (F17).
3. META follow-up before DoD: F60 temporal-ordering now; F34/F61 source-vs-target/pipeline contract classified+planned.
