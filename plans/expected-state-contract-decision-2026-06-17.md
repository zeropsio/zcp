# Expected-state contract for launch/deploy — decision doc

> **STAV 2026-06-17 (implementováno, necommitnuto, branch `fix/eval-gitpush-launchprod-feedback`):**
> Shipnuto §3 (1)(2)(3) + §4(a) jako standalone single-owner fixy; pure derivery v
> `topology/`. Framework (Precondition/Contract) ZÁMĚRNĚ nepostaven (§2). Detaily:
> - ✅ **F5-label** — `bundle.ResolvedManagedType` jeden owner (composer i preview); preview ukazuje `postgresql:ha@16`, žádný protimluv. Pinnuto HA + non-HA. Živé schéma potvrdilo type-form.
> - ✅ **F4b** — `topology.RecommendDelivery` (matice pinnuta, 9 cel) + napojeno do 3 sites; host-only `recommendIntegrationForRemoteURL` smazán; `recommendedIntegrationWhy` přidán.
> - ✅ **F4a** — `launchTargetSetupName` deleguje na `resolveLaunchSetupName` (single owner); lživý comment opraven. ZÁMĚRNĚ NEDĚLÁNO: edge (c) yaml-block read + spekulativní `PreferProdSetupBasis` (current chování bezpečné: bez stage mety → SetupProvenanceHint se ptá; over-engineering).
> - ✅ **§4(a)** — `reconcileZeropsYAMLSetup` (launch adoptuje sole setup; export zůstává striktní). Pinnuto adopt + ambiguity-reject.
> - ⛔ **F5-durability** — DROPNUTO do `plans/backlog/rejected/` (mrtvá cesta, code-proven). App-HA-readiness UŽ pokryto atomem `launch-ha-assessment`.
> - 🚫 **§4(b) launch origin self-heal** — NENÍ defekt (export taky nepřepíše probe-proven URL; block vs warn = reverzibilita). Neměněno.
> - Gates: `go test ./... -short` PASS · `make lint-local` 0 issues · `-race` PASS (topology/ops/tools).

**Date:** 2026-06-17 · **Status:** decision for owner. NO implementation shipped.
**Inputs:** my code-grounding + a 6-agent workflow (ground→synth→3 adversarial lenses)
+ an independent Codex gpt-5.5 pass. All three converged. Reconciled against the
prior `plans/zcp-goal-contracts-concept-2026-06-09.md`.

---

## 0. TL;DR

The owner's instinct — *"don't keep reactively straightening shallow repos; declare
the EXPECTED production state and tell the LLM what's expected, safe-by-default"* — is
**right and already documented**: it is the `promote-to-prod` **goal contract** in
`zcp-goal-contracts-concept-2026-06-09.md` (§2–§3). That bigger redesign is specced,
"Adopt amended" by Codex, **not built**.

Three independent heads agree on the near-term move:

> **Ship the four launch/deploy items as standalone single-owner fixes NOW. Do NOT
> build a `Precondition/Contract` framework to host them — the consumer (the
> promote-to-prod goal contract) is designed but unbuilt, and building the framework
> before its consumer is exactly the anti-pattern the concept doc §4 warns against
> (and the owner's own guardrail #2: lightweight, not a framework).**

The correct shape: put the **pure derivers** in `topology/expected_state.go`
(stdlib-only, no I/O — so both the TELL-renderer and the CHECK-gate import the *same*
function ⇒ single-owner). Leave the existing scattered preflight probes where they
are. When goal-contracts phase 4 (launch onto shared envelope) lands, these derivers
slot in as requirement checks — no rework, no speculative framework.

---

## 1. Reframe vs. reality — what the code actually says

Two of the four items are **largely shipped or near-trivial**; one is a **likely dead
path**; one is genuinely new. Grounded against code (file:line verified):

| Item | Premise (as carried in) | Reality in code | Real remaining work |
|---|---|---|---|
| **F4a** prefer stage>dev setup | "launch always promotes DEV" | **FALSE — already stage-first.** `resolveLaunchSetupName` (launch_promotables.go:171): `ProdSetupName → StageSetupName → PrimarySetupName → legacy "prod"`, pinned `launch_setup_name_p5_test.go:87`. `stageRecommendationBlock` (workflow_launch_production.go:1301) already recommends creating a stage first. | (a) **Consolidate** the duplicate `launchTargetSetupName` (workflow_launch_production.go:1093) into `resolveLaunchSetupName` — the duplicate **drops `ProdSetupName`** from its cascade ⇒ the gate's announced setup can diverge from the bundle's actual setup. (b) **New edge:** probe source-yaml `setup:` blocks (`hasSetupNamed`) so a `setup:stage` block with no stamped `StageSetupName` counts as stage-exists instead of falling to dev-promoted. |
| **F4b** delivery decision matrix | new decision matrix + recommend git+CI/CD | derived family is fine (`launchDeliveryFamily` = 1-input pass-through of `meta.BuildIntegration`). No **recommendation** owner exists; `recommendIntegrationForRemoteURL` (git_push_setup.go:841) is host-only + defaults to actions. | **One new pure deriver** `topology.RecommendDelivery(DeliveryInputs)` — additive, feeds recommendation alongside the kept derived family. **Recommendation, never a gate.** |
| **F5** durability demotion warning | "warn earlier than develop-active" | **FALSE premise + likely DEAD PATH.** No demotion warning exists *anywhere*. Bootstrap simple→standard emits only **runtime** snapshots (dev+stage pair); managed deps go through `Resolution=EXISTS` (validate.go:467) = *leave the live service alone*; `CREATE` **rejects** if the hostname already exists live (validate.go:463). So you cannot re-import/demote a live HA DB via bootstrap. A planned-vs-live demote compare would fire on a healthy HA DB nobody is changing = clutter. | **SCOPE FIRST.** Prove a path that re-provisions a live managed dep at a lower variant. If none → **drop to `plans/backlog/rejected/`** (dead path). App-HA-readiness "seed awareness" = surface existing `launch-ha-assessment.md` one phase earlier (atom placement), not a struct. |
| **F5** DB preview label | single+HA contradiction | **TRUE — 1-site display bug.** Composer is correct (`rules.go:62` resolves `postgresql:ha@18` via `WithDeploymentVariant`, omits `mode`). Only the preview drifts: `launchBundlePreviewFrom` (workflow_launch_production.go:1592) hand-pairs raw `Type:m.Type` + separate `Mode:"HA"`; `launch_ready_consent_test.go:91` **pins the contradiction**. | **Clean win.** `topology.ResolvedManagedLabel(rawType, ha)` == `WithDeploymentVariant(rawType, VariantForHA(ha))`; preview calls it; drop standalone `Mode`; **rewrite the bug-pinning test**. Must take the `keepNonHA`-resolved `ha bool` (`!keepNonHA[host]`) — pin BOTH the HA row and a deliberate NON_HA row. |

---

## 2. Why NOT the unified framework now (the convergent verdict)

Both the over-engineering lens and the does-it-unify lens returned **needs-rework** on
the grand contract, for the same reasons:

1. **The four items share no mechanism that needs sharing.** F5-label is a 3-line
   display fix; F4a is consolidation of shipped behavior; F4b is one pure deriver;
   F5-durability is new behavior on a probably-dead path. Wrapping them in
   `Precondition/Contract/State/Severity` + 5 scenario builders + ~16 rows is a
   framework grown to host four unrelated changes — guardrail #2 violation.
2. **The headline "up-front TELL" has no consumer today.** Every current message is
   *failure-triggered* (emitted only on a failed check). TELL and CHECK are already
   co-located per case (the switch emits message+severity together). Pinning a "Tell
   derived, not literal" pins a surface nothing reads yet. The genuine drift is narrow:
   (a) the duplicate setup resolver, (b) the preview-vs-emit label split — both fixed
   standalone.
3. **One of the "3 reactive patches" does not fit one contract.** `dirty-tree` is a
   **WARN** on deploy (deploy_git_push.go:240) but an intentional **BLOCK** on launch
   (launch_source_control_gate.go:652). A shared single-Tell + "never blocks" remedy
   would either regress the launch block or need a per-row severity flag = the
   framework creep the proposal itself disavows.

The pure derivers DO belong in `topology/` (stdlib, dual-importer) — that part is
right and is what we ship. The `Precondition/Contract` struct + builders is **deferred
to the goal-contract redesign**, where a real agent-facing up-front-TELL consumer is
designed.

---

## 3. What to ship now (recommended)

Four independent, minimal, single-owner changes. Each its own RED→GREEN, each
verifiable alone.

**(1) F5-label — clean single-owner-drift fix.**
`topology.ResolvedManagedLabel(rawType string, ha bool) string` (== composer
resolution). `launchBundlePreviewFrom` calls it, drops standalone `Mode`. Rewrite
`launch_ready_consent_test.go:91` to assert resolved label (`postgresql:ha@18`, no
sibling `mode`) on a `keepNonHA=false` row AND a `keepNonHA=true` (→ non-HA) row.

**(2) F4b — `RecommendDelivery` pure deriver (additive).**
`topology.RecommendDelivery(DeliveryInputs) DeliveryDecision{Recommended, Why}`, keyed
on `GitPushState × BuildIntegration × VerifiedAt × HasStage × RemoteHost` (all on
ServiceMeta). Keep `launchDeliveryFamily` as the DERIVED family for the bundle; add
the recommendation alongside it. Recommendation, never a gate. Vocabulary fix:
`BuildIntegration{none,webhook,actions}` is NOT `GitPushState`; "git-push without CI"
= `none + configured`, not a 4th enum. **Owner's "git-push to prod" resolved by
pipeline-first invariant (§10): prod = actions/webhook; agent self-deploy to prod is
not an option.** Matrix:

| Inputs | Recommend | Why |
|---|---|---|
| `configured`, `actions`, verified | actions | earned+verified — strongest signal; promote same model |
| `configured`, `actions`, unverified | actions | matches source intent; Tell notes unverified |
| `configured`, `webhook` | webhook | source delivers via Zerops webhook; don't force a CI rewrite |
| `configured`, `none`, has-stage | actions (webhook if gitlab) | best-practice CI-on-push for the stage applies |
| `configured`, `none`, no-stage | actions + create-stage-first nudge | full git+token+CI path; cross-link F4a |
| not `configured` | git-push-setup FIRST | integration without git-push configured is incoherent |
| RemoteHost gitlab (any CI cell) | webhook | GitHub Actions registers a Zerops webhook; GitLab doesn't the same way |

**(3) F4a — consolidate + one edge.**
Fold `launchTargetSetupName` into `resolveLaunchSetupName` (kill the
`ProdSetupName`-dropping divergence). Add `topology.PreferProdSetupBasis(hasStage,
prov) (basis, recommendStageFirst, tell)` as the single owner the existing
`stageRecommendationBlock` + `SetupProvenanceHint` render from. New edge: read
source-yaml `setup:` blocks so a `setup:stage` block counts as stage-exists.

**(4) F5-durability — SCOPE, then fix-or-drop.**
Code-prove whether any bootstrap/re-provision path re-imports a live managed dep at a
lower variant. If yes → one warn at `completePlanWithTargets`, reconcile-toward-live,
never block (HA immutability = delete+recreate = data loss). If no → `plans/backlog/
rejected/durability-demotion-bootstrap.md` with "Why rejected: EXISTS deps untouched,
CREATE rejects live hostname — demote path unreachable at this layer." App-HA-readiness
= surface `launch-ha-assessment.md` one phase earlier (atom placement decision).

---

## 4. Latent reject-healthy-state bugs found (separate triage — owner decides)

The reject-healthy lens surfaced real defects that exist **regardless** of the
contract. These violate guardrail "never reject a healthy real state" / "check
parallel paths". NOT part of the four items — triage individually:

- **`verifyZeropsYAMLSetup` hard-aborts** the whole bundle when the resolved SetupName
  is absent from the yaml body (ops/bundle/launch.go:109). A user with one
  hand-written `setup: production` block (cascade resolves `prod`) is healthy yet
  slammed. → reconcile-and-adopt: single setup block present but name-mismatched →
  adopt it + report.
- **Launch origin-divergence hard-blocks while export self-heals** (parallel-path
  defect). A working moved/mirror remote that differs from `meta.RemoteURL` pushes
  fine; launch should default to trusting the live working origin + refresh meta +
  WARN, matching the export path — not deadlock. (Caveat P-LP-10: don't silently pick
  live for the *recipe-template-fallback* case; the nuance is meta-identity + live
  alignment.)
- **Transport error → `StateUnknown` must be universal**, not per-row. Any SSH/REST
  probe (token, shallow, session-auth, origin-sync, meta read) can fail on transport;
  it must resolve to retry/skip, never the state-fail blocker. This is the F2-split
  regression class the launch gate already learned (gate.go:223).
- **HTTPS-only rejects a working SSH remote** (deploy key) — caller-side policy, not a
  builder check; reconcile or WARN, don't slam.
- **Container/local branch asymmetry** — local BLOCKS on undetectable branch, container
  defaults `main`. Unify toward default+report.

---

## 5. Relationship to the goal-contracts concept (2026-06-09)

This decision **finishes a leaf of that migration without front-running it**:

- The "expected production state" the owner describes = the `promote-to-prod` goal
  contract requirement set (concept §3.2): `promotables-selected`,
  `source-control-ready`, `env-classified`, `bundle-valid`, etc.
- The four derivers we ship now (`ResolvedManagedLabel`, `RecommendDelivery`,
  `PreferProdSetupBasis`, + maybe `DurabilityDemotion`) become the **first concrete
  requirement-checks** that contract assembles when concept phase 4 lands.
- Shipping them as topology pure functions now (single-owner TELL==CHECK) is exactly
  the shape the contract needs — so this is down-payment, not throwaway.
- We do NOT build the `GoalContractResponse` envelope / `Precondition` struct here —
  that is concept phases 1–4, an owner-level redesign decision (concept §8).

---

## 6. Open decisions for the owner

1. **Ship the four standalone now** (this doc §3), or hold for the goal-contract
   redesign? Recommended: ship the four — they're independently valuable and unblock
   the eval friction today.
2. **F5-durability:** authorize the scope-probe, then fix-or-drop by evidence?
   (Recommended: yes — drop if the demote path is proven unreachable.)
3. **Latent reject-healthy bugs (§4):** fix the two cheap parallel-path ones now
   (verifyZeropsYAMLSetup reconcile-and-adopt; launch origin self-heal parity), or
   backlog? They're correctness, not polish.
4. **`prodDelivery` override input** on `zerops_workflow` — add it so the user can
   override the F4b recommendation at launch, or keep launch delivery derived-only and
   require re-running `build-integration` during develop? (Recommended: derived-only +
   recommendation; add override only if eval shows a real need.)
