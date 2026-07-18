# NEW-SESSION PROMPT — ZCP correctness audit vs Zerops env ground-truth → fundamental-fix plan

> Paste the section below into a fresh Claude Code session in the `zcp` repo.
> It is self-contained (does not rely on any prior conversation).

---

## Mission

A **ground-truth specification of how Zerops environment variables actually behave** was just established and **verified live on the real platform**: `docs/spec-zerops-env-lifecycle.md`. Your job is a **deep, fundamental correctness audit of ZCP against that ground truth** — sweep the whole env-and-adjacent surface (tools, knowledge corpus, atoms, env-handling code, verify/subdomain/deploy, specs), find **everywhere ZCP's behavior, code, guidance, or assumptions are WRONG**, read the **actual code** to confirm each, verify against the live platform + tests, and produce a **multi-step implementation plan that fixes the root causes** — fully utilizing **parallel agents + Codex**.

This is NOT a surface pass. Go to the fundamental level. Every "X is wrong" must carry: (a) the code location (`file:line`), (b) the ground-truth line it violates, (c) a live or test confirmation (or an explicit "inferred, needs verification"). Treat ZCP's own docs/guides as **circular evidence** — never the basis for a claim.

## Read first (authoritative context, in order)
1. `docs/spec-zerops-env-lifecycle.md` — **THE ground truth.** Env = two scopes / two type-enums; precedence total order `system > yaml-baked run.envVariables > service-userData(secret) > project`; propagation via the `zerops-zembed` daemon (`/etc/zerops-zembed/env.json`, in-place ~5–10s, no restart; running PID1 keeps boot env); service-env API returns ~2% of container env (yaml-baked + project + cross-service aliases all invisible); `envIsolation` is **source-side & directional**; default `service` mode does NOT auto-inject siblings. Evidence-tagged + live-verified. All PENDING cells are now closed (incl. PENDING-3: secret `content` is API privilege-gated — admin reads verbatim, read-only reads `REDACTED`).
2. `plans/env-shadow-httpport-utility-recipe-2026-05-27.md` — the **F1/F2/F3** plan (already Codex-re-verified). **Fold these in; do NOT re-derive.**
3. `docs/spec-env-handling.md` — ZCP's EXISTING env model (local-`.env` rendering). **Suspect:** it encodes platform-behavior ASSUMPTIONS (§4 precedence "zerops.yaml > project … matching runtime"; service-env exclusion rationale) that must be reconciled against the ground truth.
4. `.zcp/manual/mailpit.txt` — the originating incident (added mailpit, mail silently didn't send).
5. `.claude/agent-memory/platform-verifier/verified-facts.md` — the live-verified env facts (precedence, isolation directionality, propagation timings, plaintext-secret-in-env.json, etc.).
6. `CLAUDE.md` + `CLAUDE.local.md` — invariants, TDD, 4-layer architecture + depguard, operating rules.

## Known fundamental errors to START from (verify + fold into the plan — don't re-discover)
1. **Env-isolation model error (likely the biggest blast radius).** ZCP's corpus teaches cross-service vars "auto-inject in every mode" — but the platform **default `envIsolation=service` does NOT auto-inject siblings** (only legacy `none` does, source-side). eval-zcp + the mailpit project are both legacy `none`, which masked this. **Audit every atom/guide/code-path that assumes cross-service auto-injection** and ask: *what breaks for a user on a DEFAULT (service-isolation) project?* Suspects: atoms (`develop-env-var-model`/`develop-l`, `develop-platform-rules-common`, `develop-env-var-channels`, `develop-first-deploy-env-vars`), env-ref validation (`CheckEnvRefs`, `ops.ValidateEnvReferences`), dotenv generation (`env_plan.go`, `env_generate.go`), the knowledge guide `environment-variables.mdx`, and `spec-env-handling.md`'s precedence model. Are any of these env-mode-aware at all?
2. **F1 — cross-layer env shadow.** Project env silently shadowed by a service yaml-baked literal; no detection; `zerops_env set` overclaims "env values are live". Verified: yaml-baked is invisible to **every** service-env API endpoint (~2%) and is **un-overridable at service scope** (platform rejects the duplicate key). ⇒ **deploy-preflight reading `zerops.yaml run.envVariables` vs project envs is the ONLY correct detector**; warning is single-path "edit the yaml + redeploy".
3. **F2 — HTTP-port-blind verify/subdomain.** `ResolveSubdomainURL`/verify use `Ports[0]`, ignore `HTTPSupport` — and `HTTPSupport` is **post-enable routing state, not yaml intent** (do NOT use it in auto-enable eligibility — see the `deploy_subdomain.go` invariant). Fix = cross-port probe fallback; never empty; never touch auto-enable.
4. **F3 — utility-recipe retrieval gap.** `zerops_knowledge "mailpit"` returns the antonym; the `recipe-mailpit` URL is unfindable; `engine.go` has no mailpit alias. (Two real recipe repos exist — `zeropsio/recipe-mailpit` vs `zerops-recipe-apps/mailpit-app`; the latter is recipe-generation = Aleš's scope, mention only.)

## Scope checklist — audit each surface vs ground truth
- **Tools + response messaging:** `zerops_env` (set/get/generate-dotenv — the "live" overclaim; does it model isolation?), `zerops_verify` (http_root port), `zerops_subdomain`, `zerops_deploy` + `deploy_preflight`, `zerops_discover`, `zerops_import`, `zerops_knowledge`.
- **Knowledge corpus:** `internal/knowledge/guides/*` (esp. `environment-variables.mdx` — **ZCP's own prose round-tripped upstream; gitignored, `sync push guides`**), `internal/knowledge/themes/*`, `engine.go` retrieval/aliases.
- **Atoms:** `internal/content/atoms/develop-*env*` + any atom asserting env behavior, isolation, cross-service refs, or precedence.
- **Env-handling code:** `internal/ops/{env_plan,env_generate,env_shadow,env_refs,classify_envs}.go`, `internal/ops/checks/env_*`, `internal/tools/{env,deploy_preflight}.go`.
- **Verify/subdomain:** `internal/ops/{verify*,subdomain}.go`, `internal/tools/{subdomain,deploy_subdomain}.go`, `internal/eval/probe.go`.
- **Specs:** reconcile `spec-env-handling.md` against `spec-zerops-env-lifecycle.md`; cite where the old one is now provably wrong.

## Method (mandatory)
- **Read the actual code** — never infer behavior from names. `file:line` for every finding.
- **Verify, don't assume.** Live platform = highest authority. eval-zcp is legacy `none`; for default-`service`-mode behavior use a **throwaway project** (ask the operator for a project-create token + VPN if needed — they did this before). Distinguish verified vs inferred explicitly.
- **Team of agents:** `Explore` for blast-radius sweeps, `general-purpose` for deep reads, `platform-verifier` for live proof. Launch in parallel; don't poll.
- **Codex** via `~/bin/codex-helper.sh --cd <repo> < brief` (background) for deep code root-cause analysis AND an adversarial red-team of the drafted plan (it already caught real overclaims last round — use it again on the plan).
- Respect TDD (RED→GREEN, all four layers), architecture (4-layer + depguard, new shared types → `topology/`), and CLAUDE.local: **no `make install` / `make release` without explicit ask; plan-fidelity (no silent scope cuts); backward-compat at the ZCP↔user-files seam.**

## Deliverable
A multi-step, phased implementation plan (`plans/<slug>-<date>.md`) that:
- Enumerates every confirmed defect: code location + ground-truth violation + evidence.
- Sequences by leverage + dependency (the env-isolation model error likely has the widest surface — atoms + guidance + code; weigh doing it as its own phase).
- Per phase: root cause, fix design, full blast radius (all call sites), test strategy per layer, **the pinned tests + atom goldens that change**, atom/guide edits (note guide edits ship via `sync push guides`).
- Folds in F1/F2/F3 (re-verified — reference, don't re-derive).
- Flags backward-compat seams (published product: user `CLAUDE.md`/`.mcp.json`/`.zcp/state`/permission allowlists must keep working).
- Ends with: phase order + eval gates (`flow-eval`), effort (LOC + days), and a concrete gate question.

Present the plan for approval **before** implementing. Be skeptical, go deep, verify everything — the goal is fundamental correctness, not symptom patches.
