# Flow-Eval Friction Report — Consolidation Battery (12 rounds / ~60 live runs)

**Method:** 12 rounds (~60 live runs on real Zerops) → 63 raw findings logged in
`plans/flow-eval-battery-2026-06-04.md` → 17-agent Workflow fan-out verifying each top
finding against the ACTUAL ZCP code (single owner + classify real-bug/guidance-fix/by-design/
already-fixed/out-of-scope + precise fix). Observation only — fixes ship in SEPARATE plans below.

## 1. Verdikt

Consolidation is **healthy and ships clean**: 59/62 functional PASS, **0 regressions** from the
just-landed R1/R5/R6/R7 + dev-only + MCP-resource removal, R7 export+launch verified live-clean.
**No REAL_BUG is a handler-logic deadlock from the new code.** Every confirmed bug is a
**single-owner drift where the TELL (atom / rendered guidance / schema-doc) was never brought to
parity with a CHECK or STATE that is already correct.** Dominant theme end-to-end:
**state-or-convention drifting from live reality** — an atom example promises behavior the handler
refuses (adopt), a snapshot bool says `deployed:false` for a recipe that is ACTIVE+serving (F11),
env/call-shape examples hard-code params the schema dropped (F27/F54), a terminal-launch recovery
can't read its own persisted pipeline (F43). Fix surface = **atom/guidance edits + a handful of
S/M handler stamps**, not redesign.

## 2. Ranked fix table (impact-per-effort)

| finding | class | sev | eff | one-line fix | owner |
|---|---|---|---|---|---|
| **F1+F55** | GUIDANCE_FIX | LOW | S | adopt worked example → a case that actually auto-derives (kills ~18× recurrence) | `bootstrap-adopt-discover.md` |
| **F27+F53** | GUIDANCE_FIX | MED | S | env example → `project=true variables=[...]`; regen 2 goldens | 2 bootstrap atoms + `bootstrap_guide_assembly.go:300` |
| **F11** | REAL_BUG | MED | S | stamp `FirstDeployedAt` for fresh recipe `buildFromGit` target → `deployed:true` | `bootstrap_outputs.go::writeBootstrapOutputs` |
| **F36** | REAL_BUG | MED | S | surface `SSHExecError.Output` in git-push probe failure | `workflow_git_push_setup.go:384-393` |
| **F19** | GUIDANCE_FIX | MED | S | add `run.start` exec / `env KEY=VAL` line | `develop-reserved-env-names.md` |
| **F9** | GUIDANCE_FIX | MED | S | add php `build.base = php@X` (not composite) line | `develop-implicit-webserver.md` |
| **F22** | GUIDANCE_FIX | MED | S | carve `buildFromGit` verify-exception into close atom | `bootstrap-verify.md` |
| **F57** | GUIDANCE_FIX | MED | S | add client-side-URL-must-be-public-subdomain failure sentence | `guides/environment-variables.md` |
| **F16** | RECIPE_KNOWLEDGE | MED | S | add dev/prod `zerops.yaml` block (self vs cross-deploy) | `recipes/nextjs-ssr-hello-world.md` (sync push) |
| **F54** | GUIDANCE_FIX | MED | M | add `compose-ready` to axis allow-list + 2 doc tables + new atom | `atom.go:331` + export atoms |
| **F35** | REAL_BUG | MED | M | reject planless `discover`-complete at source; fix misleading "Start bootstrap first" message | `engine.go::BootstrapComplete` |
| **F43** | GUIDANCE_FIX | LOW | M | add `pipelineSummary` to launch recovery envelope + atom | `launch_status_recovery.go` + atom |

### No ZCP action
| finding | class | reason |
|---|---|---|
| **F7** | BY_DESIGN | Real friction, correct-but-dense. Largest develop-active measured 20 KB / 22 atoms; `ComposeUnderBudget` wired but never fires (24 KB > 20.2 KB). Lever = deferred OPEN backlog `r1-context-relevance-below-cap.md` (design pass, not a bug). |
| **F25** | BY_DESIGN | "Next: deploy" is the auto-close GATE hint (keyed on `needsDeploy`, no successful deploy recorded), NOT the `deployed` bool or edit-type. Agent misread. |
| **F51** | ALREADY_FIXED | `setup-git-push-container.md:46` already warns SSHFS/host-shell push fails. "shallow clone" mechanism is factually wrong (it's `git init`, not clone). |
| **F2+F15** | ALREADY_FIXED | Close-mode gate surfaced via priority-1 `develop-strategy-review.md` DECISION heading + verify/deploy "auto-close is OFF" Note. *(Recurrence-despite-fix reinforces F7: the note exists but is lost in the wall.)* |
| **F17** | ALREADY_FIXED | Two-phase bootstrap signal loud on 4 surfaces (`kind` field, discovery Message, priority-1 atom, tool schema). |

## 3. Root-cause clusters

**(a) Adopt false-promise (F1+F55 — dominant ~18×, deterministic):** `bootstrap-adopt-discover.md:36`
promises "plan is derived for you", but the worked example uses `scope=["appdev","appstage"]` —
the same-type pair that deterministically hits `ErrAdoptPairingChoice`. Line 38 conditions the
promise in prose, but the EXAMPLE contradicts the sentence above it. Handler refusal + proactive
OS-qualified templates + discover-sourced composite types are all **by-design-correct and pinned**.
The fix is the atom example only. LOW/S — but highest frequency.

**(b) META "keyed on state-bool/convention not live reality" (F11, F27+F53, F54; F25 refuted):**
the cluster the consolidation meant to kill, leaking in 3 real places — F11 recipe meta never
gets `FirstDeployedAt` (TELL fixed Wave-5, STATE not brought to parity); F27 env example renders
the obsolete CLI form the schema dropped; F54 handler returns `compose-ready` the knowledge layer
never learned. All single-owner S/M.

**(c) Recovery/resume (F35, F43):** F35 planless `discover`-complete advances with no plan then
deadlocks at provision (only `reset` escapes) + wrong "Start bootstrap first" message. F43 launch
recovery envelope carries no pipeline summary so `action=status` can't say which CD was configured.

**(d) Cross-surface tell-gaps (F36, F22, F19, F9, F57):** true rule exists but on the wrong
surface, or a handler swallows a diagnostic (F36 git-push probe discards git stderr).

## 4. Recommended next plans (priority order)

1. **`atom-tell-parity-sweep`** — batch the LOW-friction single-owner drifts sharing the
   "TELL drifted from CHECK/reality" root: F1+F55 (dominant), F27+F53, F22, F19, F9. All S-effort
   atom edits + 2 golden regens; highest impact-per-effort, no handler risk.
2. **`bootstrap-recipe-deployed-state-parity`** — F11: stamp `FirstDeployedAt` for recipe
   `buildFromGit` targets so the envelope stops lying about `deployed:false`. Self-contained S-fix + pin.
3. **`git-push-probe-diagnostic-surfacing`** — F36: surface `SSHExecError.Output` (reuse
   `transport:git-auth-failed` regexes to classify token/repo/network/SSO).
4. **`bootstrap-discover-planless-deadlock`** — F35: reject planless `discover`-complete at
   `engine.go::BootstrapComplete`, fix the "Start bootstrap first" misnomer; 6 unit tests + regression.
5. **`export-compose-ready-knowledge-propagation`** — F54 (axis allow-list + doc tables + new atom)
   + F43 (launch `pipelineSummary` recovery field + atom): the two "handler shipped a terminal/state
   the knowledge layer never learned" findings.

## Action items (Karel, non-code)
- **Stray prod project** from `launch-with-existing-cicd` — eval agent couldn't delete (LaunchKey
  lacks delete perm). Delete from dashboard, or provide a delete-capable token + project name.
- **`existing-simple-mode-node-add-endpoint` fixture** reliably fails seed (api pre-deploy) — eval-infra fix.
- **Rotate** the GitHub PATs + Zerops tokens pasted into the transcript / `.zcp/manual/cred.txt`.
- Un-run (no creds): `launch-to-existing-prod-project` (needs an existing prod project + token).
