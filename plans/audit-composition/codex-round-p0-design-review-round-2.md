# Codex round 2: Phase 0 PRE-WORK design review re-validation (2026-04-26)

Round type: CORPUS-SCAN re-validation (per §10.1 P0 row 1, §16.1 NEEDS-REVISION → re-run protocol)
Reviewer: Codex (round 2, fresh agent)
Plan revision commit: a0f19ece
Round 1 artifact: `plans/audit-composition/codex-round-p0-design-review.md`

> **Artifact write protocol note.** Codex's sandbox blocked the
> in-agent write to this path. Claude reconstructed this artifact
> from Codex's reported summary verbatim, augmented with grep
> verification of the named claim. The verdicts below are Codex's;
> the cross-checks (e.g. "verified phrase X appears in atom Y") are
> Claude's confirmation that Codex's claim is grounded in current code.

## Round 1 finding → round 2 disposition

| # | Round 1 finding | Round 2 verdict |
|---|---|---|
| Axis 1.1 | substring search counts comments + unrelated mentions | RESOLVED — revised plan now AST-parses scenarios_test.go for `requireAtomIDs*` arg position only |
| Axis 1.2 | knownUnpinnedAtoms self-counting (haystack contains its own declarations) | RESOLVED — allowlist moved to dedicated file `corpus_pin_density_test.go`, separate from AST haystack |
| Axis 1.3 | should parse structured pin sites | RESOLVED — uses go/parser AST, args[3:] of `requireAtomIDsContain`/`requireAtomIDsExact` |
| Axis 4.1 | _StillUnpinned reads same file containing allowlist | RESOLVED — file isolation (per Axis 1.2 fix) + AST parsing decouples from raw text |
| Axis 4.2 | missing stale-entry check for deleted/renamed atoms | RESOLVED — revised _StillUnpinned has explicit "every allowlist key must still exist in LoadAtomCorpus()" check, mirroring `KnownOverflows_StillOverflow` |
| Axis 4.3 | doesn't handle pinned-by-scenario frontmatter | EXPLICITLY-EXCLUDED — atom self-declared `pinned-by-scenario` is a forward-direction informational field; Phase 0 design does not depend on it. Future phase may extend if needed. |
| Axis 5.3 | bootstrap synthetic generator omits services | RESOLVED — revised generator emits no-svc + per-runtime-class svc variants per (env × route × step) |
| Axis 5.4 | bootstrap step set wrong | RESOLVED — `{discover, provision, close}` (atom-valid set per `internal/workflow/atom.go::"steps"`) |
| Axis 5.5 | develop generator missing Trigger loop | RESOLVED — Trigger axis added: `{TriggerUnset, TriggerActions, TriggerWebhook}` |
| Axis 5.6 | strategy-setup + export-active not implemented | RESOLVED — strategy-setup synthesised per (env × trigger) with push-git service; export-active container-only generated |
| Axis 5.7 | develop-closed-auto envelopes missing | RESOLVED — `PhaseDevelopClosed` (string `"develop-closed-auto"`) generated per env |
| Axis 5.8 | single-service develop only — multi-service Axis J case missing | RESOLVED — explicit mixed-deploy-state envelope per env (one bootstrapped+deployed service alongside one bootstrapped+undeployed service) |
| Axis 5.9 | envelopeRuntimes axis doesn't exist | RESOLVED — revised plan explicitly notes "no envelopeRuntimes generation — that axis does not exist" |
| Axis 6.3 | empty MustContain doesn't anchor pre-hygiene fire-set | NEEDS-REVISION — three pins added but pin uniqueness was not verified (see new finding below) |
| Axis 6.4 | needs explicit pins to simple-mode push-dev workflow/deploy/close atoms | NEEDS-REVISION — pins claim to anchor those three atoms but at least one phrase (`zerops_workflow action="close"`) does NOT appear in the named anchor (`develop-close-push-dev-simple`) and another (`zerops_deploy`) appears in many atoms |

## Recommended adjustments — round 2 disposition

| # | Round 1 recommendation | Round 2 verdict |
|---|---|---|
| 1 | PinDensity uses structured pin sources | RESOLVED |
| 2 | _StillUnpinned isolates allowlist + adds stale-entry check | RESOLVED |
| 3 | Bootstrap generator includes services across runtime classes | RESOLVED |
| 4 | Replace bootstrap step set with atom-valid set | RESOLVED |
| 5 | Add strategy-setup synthesis + export-active | RESOLVED |
| 6 | Add develop-closed-auto + mixed-service envelopes | RESOLVED |
| 7 | Give simple-deployed fixture non-empty MustContain | NEEDS-REVISION — pins were added but not all uniquely anchor their named target atoms |

## New findings (introduced by the revisions)

1. **Axis 6 pin verification gap.** The revised plan's
   `develop_simple_deployed_container` fixture pins `zerops_deploy`
   and `zerops_workflow action="close"` as anchoring phrases. Round 2
   verified directly via grep:
   - `zerops_workflow action="close"` does NOT appear in
     `internal/content/atoms/develop-close-push-dev-simple.md` (atom
     body lines 11-18 contain only `zerops_deploy targetService=...`
     and `zerops_verify serviceHostname=...`). The phrase IS in 4
     other atoms (`bootstrap-recipe-close`, `develop-auto-close-
     semantics`, `develop-change-drives-deploy`, `develop-closed-
     auto`), so the pin would pass for the wrong reasons.
   - `zerops_deploy` appears in many atoms, so it doesn't uniquely
     anchor `develop-push-dev-deploy-container`.
   - `push-dev` is in multiple atoms (the term is used in
     comments / discussion across the corpus).

   The fixture as-pinned would not catch a Phase 7 regression that
   silently dropped the named atoms — `RoundTrip` would still pass
   because the phrases are sourced from other atoms. The fix is to
   pin on phrases UNIQUE to each anchor atom (Claude's follow-up
   commit replaces the three phrases with grep-verified-unique
   strings: `Push-Dev Deploy Strategy — container`, `auto-starts
   with its \`healthCheck\``, `Simple-mode services auto-start on
   deploy`).

## Verdict per axis (refreshed)

### Axis 1 — TestCorpusCoverage_PinDensity design
Verdict: APPROVE. AST-based parsing of `requireAtomIDs*` arg
positions is the right shape; the round 1 substring concerns are
addressed.

### Axis 2 — knownUnpinnedAtoms allowlist composition
Verdict: APPROVE. Allowlist composition unchanged from round 1
(verified separately by Claude: AST scan returns 11 pinned + 68
unpinned, identical to the §4.2 grep-derived baseline).

### Axis 3 — Recipe-atom scope leakage (R6)
Verdict: APPROVE. Continued use of `LoadAtomCorpus` /
`ReadAllAtoms` keeps scope at `internal/content/atoms/`.

### Axis 4 — TestCorpusCoverage_PinDensity_StillUnpinned ratchet
Verdict: APPROVE. Stale-entry check + ratchet shrink-only mirror
`KnownOverflows_StillOverflow`.

### Axis 5 — §6.3 fire-set generator approach
Verdict: APPROVE. All 7 round-1 sub-findings addressed; new
generator covers idle × bootstrap (no-svc + per-runtime svc) ×
develop-active (full Cartesian + multi-service mixed) × develop-
closed-auto × strategy-setup × export-active.

### Axis 6 — develop_simple_deployed_container fixture sketch
Verdict: NEEDS-REVISION (round 2 finding above). Pins are present
but at least two of the three do not uniquely anchor their named
target atoms; the fixture would pass `RoundTrip` even if the
named atoms were dropped. Fix: replace the three phrases with
grep-verified-unique strings.

## Verdict summary

OVERALL: NEEDS-REVISION (1 of 6 axes; Axis 6 only)
Round-2 disposition: Phase 0 may begin: NO. Single blocker is
Axis 6 pin re-selection. Once unique anchoring phrases land, run
round 3 (verify-only) to confirm before substantive work begins.
