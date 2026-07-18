# Run-36 validation — first prod dogfood after v9.78.0 fix-pack

**Date:** 2026-05-10
**Run dir:** [`docs/zcprecipator3/runs/36/`](../docs/zcprecipator3/runs/36/)
**Substrate (intended):** v9.78.0 — 2 engine + 3 substrate fixes closing
[run-35 audience-model gaps](run-35-vs-jetstream.md). Commit `87fccb6b`.
**Substrate (actual):** v9.77.0 (or earlier) — see headline.
**Codex-verified:** binary-version diagnosis confirmed via `codex:codex-rescue`.

---

## Headline

**CRITICAL REGRESSION — run-36 cannot validate v9.78.0 because v9.78.0
never ran.** The binary the dev container used to compose briefs and
render templates was pre-v9.78.0 (every shipped fix is missing from the
artifacts; every v9.78.0 marker string is absent from the subagent logs).
This is a build/deploy pipeline issue, not a fix-pack design issue.
Recommend **re-build + re-deploy zcp on the dev container, then re-run
the dogfood**. Detailed audience-model analysis is not actionable
against this run — the rendered output is essentially run-35 with
stochastic variance.

---

## Engine fix verification matrix — 0/2 landed

| Surface | Expected (v9.78.0) | Run-36 actual | Verdict |
|---|---|---|---|
| `apidev/README.md` L1 | `# Zerops x NestJS Showcase API` | `# nestjs-showcase — api` | **NOT LANDED** — pre-v9.78.0 template format |
| `appdev/README.md` L1 | `# Zerops x NestJS Showcase Frontend` | `# nestjs-showcase — app` | **NOT LANDED** |
| `workerdev/README.md` L1 | `# Zerops x NestJS Showcase Worker` | `# nestjs-showcase — worker` | **NOT LANDED** |
| `environments/README.md` L1 (root) | `# NestJS Showcase Recipe` | `# nestjs-showcase` | **NOT LANDED** |
| Root porter-meta line between cover image and tier list | "Offered in examples for the whole development lifecycle…" | absent — cover image goes directly to tier-list bullets | **NOT LANDED** |

All 4 codebase + root titles still carry the v9.77.0 `# {SLUG} — {HOSTNAME}` /
`# {SLUG}` template format. The hardcoded porter-meta lifecycle line
that v9.78.0 baked into `root_readme.md.tmpl` is absent.

---

## Substrate fix verification matrix — 0/3 landed

| Substrate fix | Expected (v9.78.0 brief teaching) | Run-36 actual | Verdict |
|---|---|---|---|
| Tier README intro lead pattern | `**Tier name** environment <verb> <audience purpose>` (jetstream-GOOD) | All 6 tiers lead `<TierName> tier — <descriptors>` (pre-v9.78.0 substrate shape, run-35-class) | **NOT LANDED** |
| Tier yaml head mirrors README intro | semantic mirror of README intro body | All sampled (tier-3, tier-0, tier-5) dive into project-envVariables-block explanation, not README-intro mirror | **NOT LANDED** |
| Per-service yaml comments lead with imperative + role | `Deploy/Spin up X, used by Y for Z` (or acceptable MIX `Single-node X — used by Y for Z`) | Mixed — db comment improved (now `Single-instance Postgres — used by api and worker for items + processed-events tables…` — acceptable MIX shape, but stochastic improvement, not substrate-fix landing) | **NOT LANDED** (substrate teaching never reached agent) |

Subagent log corroboration:
- 0 hits for `Lead pattern` across all 20 subagent JSONL logs
- 0 hits for `TierName`
- 0 hits for `Deploy/Spin up`
- 0 hits for `imperative + role`
- 0 hits for `jetstream-GOOD`

The env-content brief composed by the engine on the dev container
contained none of the v9.78.0 substrate teaching. The agent could not
have complied with substrate it never saw.

---

## Why the binary diagnosis is conclusive

Templates and substrate briefs are bundled into the binary via
`//go:embed all:content` in `internal/recipe/briefs.go`. They cannot
diverge from what the binary was compiled with. Yet:

1. **Templates on disk at HEAD show v9.78.0 content** — verified
   `git show v9.78.0:internal/recipe/content/templates/codebase_readme.md.tmpl`
   contains `# Zerops x {NAME}{ROLE_LABEL}` and the same path on
   `main` matches.
2. **Run-36 rendered output shows v9.77.0 content** — `# nestjs-showcase — api`
   (slug-stem + hostname) is the v9.77.0 template shape; v9.78.0 templates
   would render `# Zerops x NestJS Showcase API`.
3. **Subagent briefs show no v9.78.0 substrate strings** — every Lead-pattern /
   TierName / Deploy/Spin up / jetstream-GOOD marker is absent from the
   composed brief that the env-content subagent read.
4. **Run-35 and run-36 produce identical L1 patterns** — both renderings
   come from the same binary version.
5. **Timing** — v9.78.0 commit at 2026-05-10T07:41:45 UTC; env-content
   subagent dispatched at 2026-05-10T09:35:00 UTC. ~2h gap, but the
   binary was never re-built+re-deployed in that window.

No alternative explanation survives the evidence:
- Agent override / post-processing — would still leave at least some
  v9.78.0 substrate strings in the brief content read by subagents.
  None present.
- Engine ran v9.78.0 but rendered fallback templates — engine renders
  from embedded templates only; there's no fallback path.
- Some non-content substrate path — content is embed-bundled; there's
  no separate path the engine could have read it from.

Codex verdict (independent re-check):
> "VERDICT: CONFIRMED — run-36 was produced by a pre-v9.78.0 binary…
> The claim stands: this is a build/deploy pipeline issue. v9.78.0
> shipped in the codebase but the dev container binary was never
> rebuilt, so run-36 exercised none of the five fixes."

---

## What's measurable on this run (and what isn't)

**Not actionable** without v9.78.0 actually running:
- Engine fix counters (V3 title × 4 surfaces; root porter-meta line) — all
  fail by definition; would all pass if the binary were correct.
- Substrate fix counters (tier intro lead pattern; yaml head mirror;
  per-service comment lead) — agent never saw the substrate; failure
  doesn't tell us if substrate teaching works at runtime.
- Side-by-side voice comparison vs jetstream — same shape as run-35;
  measuring it again is duplicate work.
- "Three still-loose items" measurements — same shape as run-35.

**Stochastic-only delta vs run-35** (informational, not load-bearing):

| Surface | Run-35 | Run-36 | Note |
|---|---|---|---|
| Tier-3 db yaml comment | descriptor + tradeoff lead ("Single-instance Postgres — restoring from snapshot still means downtime…") | descriptor + role + relationship lead ("Single-instance Postgres — used by the api and worker for items + processed-events tables…") | Coincidentally moves toward acceptable MIX shape — but agent had identical brief, so this is variance, not substrate-fix landing |
| Tier intro voice | TY2-class operational-engineer | TY2-class operational-engineer | unchanged |
| KB sibling shape | 3 distinct shapes | not re-measured (same brief, same outcome class expected) | n/a |

**Counter measurements skipped** — run-36 is essentially run-35 from
the engine + substrate perspective. Re-running the 8 counters would
duplicate run-35's table without adding signal. If the user wants the
numbers regardless, I can run them in a follow-up — but they would
not change the diagnosis.

---

## Recommended next step — rebuild + redeploy, then re-run

**Path A (recommended): re-deploy v9.78.0 binary, re-run dogfood.**

Steps:
1. Build zcp from `main` (or `v9.78.0` tag) and deploy the new binary
   to the dev container that hosts zcprecipator workspaces.
2. Confirm via a smoke test — `zcp version` on the container should
   show v9.78.0 (or commit `87fccb6b`+).
3. Re-trigger the prod dogfood. The fresh run will exercise all 5
   v9.78.0 fixes.
4. Validate against this plan's job spec.

The audience-model gaps from run-35 vs Jetstream remain the bar; the
v9.78.0 fix-pack design hasn't been falsified — it just hasn't been
exercised. The TY2 voice axis, root porter-meta line, V3 title shape:
all still need to be tested against the new substrate, not the old one.

**Path B (NOT recommended): iterate on top of run-36 as if v9.78.0 had
landed.** Would burn iteration cost on shaping fixes for problems
v9.78.0 already solves on paper. Wait for a clean v9.78.0 run before
deciding any next-iteration moves.

---

## Operational hygiene flag

The dev container's zcp binary is the single load-bearing dependency
between source and prod dogfood. Today there's no automated check that
the running binary matches the latest commit before a dogfood is paid
for. **Suggest adding a pre-flight check** (e.g. zcprecipator handler
emits engine-version into `plan.json` at `start`; validation reports
read it; mismatched-version dogfoods fail loudly before payment).
This run cost a full prod dogfood that produced essentially zero new
signal because of the silent version drift.

---

## Verdict

**v9.78.0 is unvalidated.** Run-36 is a no-op against the fix-pack
because the binary running zcprecipator3 on the dev container was a
pre-v9.78.0 build. None of the 5 fixes were exercised. Re-build +
re-deploy the binary, re-run the dogfood, then validate. The 5 fixes
themselves are well-anchored in source (templates + substrate +
human_name.go + assemble.go all updated; pinning tests
`TestAssembleCodebaseREADME_V3TitleShape` /
`TestAssembleRootREADME_V3TitleShape` /
`TestAssembleRootREADME_LifecycleFramingLine` exist) so confidence in
the fix-pack design is unchanged.
