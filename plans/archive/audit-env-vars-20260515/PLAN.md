# Plan — Fix root causes of env-var confusion

**Date:** 2026-05-16
**Inputs:** AUDIT.md (model α/β + 5 contradictions + 10 gaps), ADDENDUM-live-runs.md (run1+run2 empirical evidence), Run 1 agent retrospective (2026-05-16, in `<run1-a>` reply).
**Premise:** Pre-production — structural corrections, no backwards-compat shims. Root causes, not triggers.

---

## A. What the Run 1 retrospective changed

Three things I had wrong / didn't know before agent's answer:

| What I assumed | What's actually true |
|---|---|
| HOSTNAME-bisect was a confident isolation | Agent NEVER isolated HOSTNAME individually — bisected 3-at-once + pattern-matched "HOSTNAME = reserved-sounding" to attribute. Their memory file is **over-confident**. |
| Atom is silent on `connectionString` shape | Atom **actively pushes the wrong way**: *"prefer it over assembling hostname:port:user:password:dbName"* — directly contradicting reality |
| `zerops_discover` doesn't expose connectionString shape | It already does: `"value":"postgresql://${user}:${password}@${hostname}:${port}"` (no `/dbName`) is right there. Agent never read the value field — scanned keys only. |

This shifts the priority ranking: **atom-level factual lies cost more than tool-level visibility gaps**, because the agent's mental model is being actively misdirected.

---

## B. Root-cause map (revised)

```
RUN1 + RUN2 OBSERVED FRICTION
            │
            ├── ~20-25 wasted deploys / session
            │
       ┌────┴────┬─────────────┬────────────────┐
       ▼         ▼             ▼                ▼
    RC1       RC2           RC3              RC4
  Atom       Discover      No reserved-     Model α/β
  actively   value field   name validation  conceptual
  misdirects unread        anywhere         mismatch
       │         │             │                │
       │         │             │                ├─→ explained in main AUDIT.md
       │         │             │                │   (foundational atom rewrite)
       │         │             │                │
       │         │             ▼                │
       │         │       Empty BUILD_FAILED     │
       │         │       <10s, no diagnostic    │
       │         │       hint, agent bisects    │
       │         │                              │
       │         ▼                              │
       │   Structured flag + warning            │
       │   in zerops_discover response          │
       │                                        │
       ▼                                        │
   Atom factuality edit                         │
   (one-line lie → multi-line truth)            │
                                                │
                                          PRIMARY ROOT CAUSE
                                          (3 atom edits + new
                                          foundational atom)
```

**RC1 (atom factual lie about `connectionString`)** — cheapest to fix, highest direct impact per session.
**RC2 (discover value unread)** — structural fix needed (renderer + flag), medium impact.
**RC3 (no reserved-name validation)** — must verify FIRST (Phase 0), then preflight implementation.
**RC4 (Model α/β)** — foundational atom rewrite from main audit. Highest leverage long-term but largest scope.

---

## C. Implementation phases — verify-first, structural, capped at 5 files

### Phase 0 — VERIFY (mandatory; never trust over-confident memory)

**Goal:** Empirically determine which env-var keys actually cause `BUILD_FAILED`. The agent's "HOSTNAME is reserved" is a hypothesis, not a verified fact.

**Method:** Provision a tiny nodejs@24 probe service in eval-zcp (`startWithoutCode: true`), then individually test:

| Probe | run.envVariables content | Predicted outcome | Actual |
|---|---|---|---|
| P1 (baseline) | `(empty)` | OK | TBD |
| P2 | `HOSTNAME: 0.0.0.0` only | FAIL if reserved | TBD |
| P3 | `PORT: 3000` only | OK (suspected user space) | TBD |
| P4 | `NODE_ENV: production` only | OK (suspected user space) | TBD |
| P5 | `hostname: example` (lowercase) | FAIL if cross-shadow | TBD |
| P6 | `PATH: /custom/bin` | suspected FAIL | TBD |
| P7 | `USER: foo` | suspected FAIL | TBD |
| P8 | `HOME: /custom` | suspected FAIL | TBD |
| P9 | `db_password: ${db_password}` (self-shadow) | OK (literal `${var}`, no fail) | TBD |
| P10 | quoted: `HOSTNAME: "0.0.0.0"` | distinguish parse vs. semantic reject | TBD |

For each P that FAILs: capture the platform's actual error response — `deploy_logs`, `events`, `failureClassification` payload. The goal is to find the **machine-readable signal** the platform already emits that ZCP could surface in `rejectedEnvKeys`.

**Output of Phase 0:**
- Verified list of reserved keys (subset of: HOSTNAME, PATH, USER, HOME, hostname, …)
- The actual error shape from platform (does it tell us *why*? what field carries the rejection?)
- One-page report `plans/audit-env-vars-20260515/VERIFY-reserved-names.md`

**Files touched:** 0 source files. Just a verification report.

**Gate:** No code changes proceed until Phase 0 confirms or refutes the hypotheses.

---

### Phase 1 — Atom factual fixes (after Phase 0 confirms)

**Goal:** Remove every demonstrably false line from atoms; add what reality requires.

| File | Change | Why |
|---|---|---|
| `internal/content/atoms/develop-first-deploy-env-vars.md` | EDIT lines 44-45 — remove *"prefer it over assembling"* for Postgres `connectionString`; replace with type-specific guidance | Atom currently pushes agents toward the broken `${db_connectionString}` for Prisma; verified via run1 retrospective + live `zerops_discover` value |
| `internal/content/atoms/develop-first-deploy-env-vars.md` | ADD Postgres-specific subsection: shape of `connectionString` (no `/dbName`), Prisma/Drizzle/sqlx compose pattern with worked YAML example | Worked example > prohibition; mirrors the actual fix both agents converged on |
| `internal/content/atoms/develop-reserved-env-names.md` (NEW) | List Phase-0-verified reserved keys with symptom (BUILD_FAILED <10s, empty logs) and rule (don't put these in `run.envVariables`) | Agent has zero prior cue today; this saves 4-10+ deploys per first-time encounter |
| `internal/content/examples/gotcha_pass_platform_invariant_env_shadow.md` | EDIT line 11 — symptom string: *"empty string at runtime"* → *"the literal string `${db_hostname}`"* | Matches `env_shadow.go:9-14` mechanism + matches what build-stack envs actually show in `printenv` (audit verification) |

**File count:** 3 atom edits + 1 new atom = **4 files** (under cap).

**Pin:** Add a test that asserts the symptom string in `gotcha_pass_*` matches the canonical mechanism string in `internal/ops/env_shadow.go` — drift detector.

---

### Phase 2 — Foundational atom — the α/β model rewrite

**Goal:** Replace Model α teaching with Model β as primary. Cross-service vars auto-inject; `run.envVariables` is for renames + mode flags only.

| File | Change | Why |
|---|---|---|
| `internal/content/atoms/develop-env-var-model.md` (NEW) | Foundational atom for develop-active phase. States auto-inject as the platform's mechanism. `run.envVariables` lines are RENAMES (different left-side key) + mode flags (NODE_ENV, APP_ENV) only. Anchor each rule with a 1-line worked example. | Replaces the implicit Model α that 7 atoms collectively teach. Cite the public-docs guide L49-83 + `env_shadow.go:9-14` as authoritative. |
| `internal/content/atoms/develop-env-var-channels.md` | EDIT — clarify that the three "channels" are SET-TIME, not VISIBILITY mechanisms. Drop the implication that declaration is required for visibility. | Currently the atom positions service-level / run.envVariables / build.envVariables as if they're three places you put values to make them visible; reality is one mechanism (auto-inject) with three set/override surfaces. |
| `internal/content/atoms/develop-first-deploy-env-vars.md` | EDIT — open with "Most cross-service values already in `process.env` via auto-inject under `<hostname>_<key>` form. Write a `run.envVariables` line only when…" with two bullets: rename to framework-conventional name, or compose a partial value (Postgres URL with `/dbName`). | Currently this atom is the strongest Model α reinforcement; this edit makes it the strongest Model β teacher. |

**File count:** 1 new atom + 2 edits = **3 files**.

**Pin:** AST contract test ensures every other env atom either references `develop-env-var-model` (when they discuss values reaching the app) OR explicitly limits scope to set-time semantics. Prevents future drift back to Model α.

---

### Phase 3 — Tool-level: surface what the platform already knows

**Goal:** Don't teach agents to do platform-level diagnosis; let the platform surface it. Even with perfect atoms, agents miss text; structured fields don't get missed.

| File | Change | Why |
|---|---|---|
| `internal/ops/deploy_validate.go` | Add `CheckReservedEnvNames` — pre-deploy validator that rejects user envs matching the Phase-0-verified reserved set. Returns structured error `ErrReservedEnvName` with `rejectedKeys: [...]`. Wires into existing `deployPreFlight` chain. | Empty-build-log diagnosis is the agent's worst friction; preflight rejection is cost: 0 deploys, 1 informative error. |
| `internal/ops/env_generate.go` (discover response shape) | When emitting Postgres / MariaDB `connectionString`, attach `"completenessFlags":{"includesDbName": false}` + `"warning":"connectionString does not include database name; for Prisma/Drizzle/sqlx, compose explicitly: postgresql://${user}:${password}@${hostname}:${port}/${dbName}"`. | Agent's own suggestion: structured boolean a renderer can promote into a warning, not just a string in a sea of strings. |
| `internal/ops/deploy_failures.go` (or wherever build-failed events are processed) | When build fails in <10s with empty logs, attach diagnostic hint to `DeployResult.FailureClassification`: *"Build container exited before producing logs. Likely yaml-level rejection — check run.envVariables for reserved names (HOSTNAME, PATH, …) or syntax errors."* | The platform's "empty logs <10s" is the symptom; ZCP can interpret it. Reduces agent's blind bisect to a guided check. |

**File count:** 3 ops edits = **3 files**.

**Pin:** 
- `TestCheckReservedEnvNames_*` — table-driven, each Phase-0-verified key + a control set of allowed keys.
- `TestDiscoverPostgres_AttachesConnectionStringWarning` — discover output for Postgres includes the structured flag.
- `TestDeployFailure_EmptyLogsAttachesYamlHint` — short-circuit build failure attaches the diagnostic hint.

---

### Phase 4 — Test pins (last; locks down everything)

| File | Pin |
|---|---|
| `internal/content/atoms_lint.go` or new test | Forbid the strings *"prefer it over assembling"* and *"empty string at runtime"* in `internal/content/atoms/**` — catches future drift back to the old text. |
| `internal/content/atoms_references_test.go` | Assert `develop-env-var-model.md` is referenced by every atom touching env-var values. |
| `internal/ops/env_shadow.go` + `gotcha_pass_*` | AST-pin: atom symptom string matches code mechanism string verbatim. |

**File count:** 3 test files.

---

## D. Sequencing + capped scope

| Phase | Files | Verify gate | Time estimate |
|---|---|---|---|
| 0 — Verify | 0 (report only) | Empirical proof of reserved-name set + connectionString shape | 1-2 hours probing eval-zcp |
| 1 — Atom factual | 4 | Atom-lint passes; gotcha pin passes | 1-2 hours editing |
| 2 — α/β rewrite | 3 | Atom-reference pin passes; new model atom is canonical | 3-4 hours writing + cross-ref |
| 3 — Tool-level | 3 | All three new tests pass; no regression in `deploy_validate`/`env_generate` test suite | 4-6 hours implementing |
| 4 — Test pins | 3 | All new pins green; no drift detector trips | 1 hour |

**Total:** 13 files across 4 phases + Phase-0 verification gate. Each phase is independently verifiable; abort if any phase regresses.

---

## E. What this plan deliberately does NOT do

| Considered | Rejected because |
|---|---|
| Symbol-contract-style auto-detection of dbName drop in app code | Too invasive for the surface — atom + structured flag is enough; ZCP doesn't statically analyze user app code |
| Auto-rewrite `${db_connectionString}` → `postgresql://${db_user}:...` in user's yaml | Magic; pre-prod principle says correct the model, not paper over it |
| Atom-level enumeration of *every* framework's URL-shape need (Drizzle, sqlx, SQLAlchemy, Sequelize, …) | Phase 1 worked example is enough; agents extrapolate well from one good example |
| Documenting `envIsolation` mode behavior in detail | Not surfaced in either run; addressed in main audit B3 as deferred F4 |
| Atom on inter-runtime HTTP wiring (D3 from addendum) | Run 2 agent guessed correctly with zero atom support; defer |
| `RUNTIME_*` / `BUILD_*` cross-phase access atom | Not surfaced in runs; defer to main audit F-list |
| Reorganizing the atom corpus into foundational/operational/reference (audit §G proposal) | Out of scope; this plan is targeted fixes, not reorganization |

These are tracked in `plans/audit-env-vars-20260515/AUDIT.md` for future passes.

---

## F. Open question that blocks Phase 1

**`HOSTNAME` — actually reserved, or is something else?** Run 1 agent admitted he never isolated HOSTNAME. Phase 0 must answer this before we write the reserved-names atom. If it turns out HOSTNAME is fine and something else (PORT? combination?) was the actual failure, Phase 1's new atom changes content but not shape.

Alternative hypotheses to test in Phase 0:
- **Combination effect** — e.g., `HOSTNAME` + `PORT` together but neither alone
- **Value-driven** — `0.0.0.0` parses as something special (IPv4 literal); maybe any IP-like value would fail
- **Number-vs-string** — `PORT: 3000` (number) vs `PORT: "3000"` (string) — yaml type coercion at recipe → import boundary
- **Two-layer interaction** — maybe `HOSTNAME` only fails when build is also doing prisma generate (because prisma reads `HOSTNAME` at codegen time)
- **Pre-processor** — `${HOSTNAME}` ref expansion gone wrong in some build step

Run 1's empirical evidence was:
- baseline empty envVars: PASS
- DATABASE_URL only: PASS
- DATABASE_URL + NODE_ENV + HOSTNAME + PORT: FAIL
- agent removed 3 at once; never tested individual

So we know the set {NODE_ENV, HOSTNAME, PORT} together with DATABASE_URL fails. We don't know which member(s).

Phase 0 enumerates this.

---

## G. Why this plan is the right shape for ZCP

- **Verify-first** ([Karel's clean-code rule, root-cause discipline](../../CLAUDE.md)) — agent over-confident memory is a structural risk; we lock it in only after empirical proof.
- **Three layers** (atom factual → atom conceptual → tool-level) — addresses RC1 + RC2 + RC3 + RC4 each at the correct layer; no symptom-patching at a wrong layer.
- **Pre-production posture** — no backwards-compat shims (e.g., we don't keep the *"prefer it over assembling"* line "in case some agent relies on it"); structural correction.
- **5-file phase cap** — every phase is independently reviewable + revertible.
- **Pin everything** — the test pins prevent regression; atom-lint catches drift back to the bad text.

The agent's run1 retrospective also surfaced a meta-point worth bookmarking: agent memory files (`/home/zerops/.claude/projects/-var-www/memory/feedback_*.md`) can carry **over-confident verbalizations** of incomplete debugging. These can be useful signal but can't be treated as ground truth. That's a separate concern — possibly a future memory-discipline atom for agents to follow — but out of scope here.

---

## H. What we'd ask the Run 2 agent if we could

Karel said run-2 agent retrospective isn't available. Bookmarking the questions that would have validated Phase 0 sequencing:

1. *"You suspected `${zeropsSubdomainHost}` wouldn't resolve and removed it. Bisecting which line of run.envVariables was the actual culprit — did you ever try removing only NODE_ENV (keeping HOSTNAME and PORT)?"* — directly tests whether multi-service projects ALSO hit the same combined-set failure or whether single-service vs multi-service differs.

2. *"In your zerops.yaml you wrote both `MEILI_API_KEY: ${search_masterKey}` and `MEILI_HOST: http://${search_hostname}:${search_port}`. Manual composition for MEILI_HOST suggests you knew composition was an option. Why didn't you compose DATABASE_URL the same way?"* — directly probes Model α/β inconsistency within the same session.

If run-2 agent comes back online, these would tighten Phase 0 + Phase 2.
