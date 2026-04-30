# Eval Phase 1.5 — Develop iteration + recovery scenarios

> **Status**: Plan, awaiting approval before execution.
> **Date**: 2026-04-30
> **Predecessor**: Phase 1 closed out, see `docs/eval-phase1-closeout-2026-04-30.md` and `plans/archive/eval-phase1-bootstrap-develop-deploy-2026-04-30.md`.
> **Scope IN**: existing `seed=deployed`/`seed=imported` scenarios that exercise SPECIFIC develop-side behaviors — incremental work, recovery, dev-server lifecycle, ambiguous-state handling. NO new scenario authoring (the corpus is already there from prior development cycles; Phase 1.5 is about RUNNING + triaging them).
> **Scope OUT**: git-push setup, build-integration, mode-expansion, multi-deploy iteration tier ladder — all Phase 2.

---

## 1. Why this phase exists

Phase 1 confirmed the **bootstrap → develop → first-deploy → verify** chain works for greenfield + adopt + classic flows across runtime classes. It explicitly DIDN'T cover:

- **Resume after process death** — bootstrap session interrupted mid-step
- **Develop iterations on already-deployed services** — incremental endpoint/feature work
- **Ambiguous state recovery** — agent walks into a project mid-flow without context
- **Dev-server lifecycle edges** — start/restart/triage of `zerops_dev_server`
- **First-deploy branch in already-imported state** — fixture provisions services, scenario tests just the develop part
- **Pivot auto-close** — explicit close-mode-pivot scenarios

These all already exist as authored scenarios in `internal/eval/scenarios/` and the framework can run them as-is. The work is curate-the-list + run + triage + decide what surfaces.

---

## 2. Scenario set

Eight existing scenarios from `internal/eval/scenarios/`, all `seed=deployed` or `seed=imported`, none requiring authoring:

| # | Scenario | Seed | Wall est. | Why |
|---|---|---|---|---|
| P15-1 | `bootstrap-resume-interrupted` | empty (preseed) | ~10 min | Recovery from dead-PID bootstrap session — claim + continue |
| P15-2 | `develop-first-deploy-branch` | imported (preseed) | ~10 min | Second-pass on never-deployed → deployed transition (already-imported state) |
| P15-3 | `develop-add-endpoint` | deployed (fixture) | ~8 min | Incremental work on already-deployed Laravel app |
| P15-4 | `develop-ambiguous-state` | imported (preseed) | ~10 min | Agent walks into incomplete state, must recover |
| P15-5 | `develop-dev-not-started` | imported | ~10 min | Recovery from missed `zerops_dev_server action=start` |
| P15-6 | `develop-dev-server-container` | deployed (fixture) | ~10 min | Container-env dev-server lifecycle |
| P15-7 | `develop-dev-server-local` | deployed (fixture) | ~10 min | Local-env dev-server lifecycle (only LOCAL coverage we get this phase) |
| P15-8 | `develop-pivot-auto-close` | deployed (preseed) | ~10 min | Task-pivot triggering auto-close (post-F6 prose-drift fix) |
| P15-9 | `develop-close-mode-unset-regression` | deployed | ~10 min | `develop-strategy-review` atom firing path (close-mode unset post first-deploy) |

Total estimate: ~90 min wall (sequential), 9 scenarios.

### What's intentionally NOT in Phase 1.5

- `close-mode-git-push-setup`, `e2-build-fail-classification`, `export-deployed-service` — Phase 2
- `verify-rendered-text` — narrow primitive test, not lifecycle
- `deploy-warnings-fresh-only` — diagnostic scenario, not lifecycle
- All `weather-dashboard-*` and `greenfield-*` and `bootstrap-*-static-*-classic` — Phase 1 already covered greenfield flows

---

## 3. Run mechanics

Same as Phase 1:

```bash
# Single scenario-suite under one suite ID
ssh zcp "nohup zcp eval scenario-suite --files \
  /home/zerops/eval-scenarios/bootstrap-resume-interrupted.md,\
  /home/zerops/eval-scenarios/develop-first-deploy-branch.md,\
  /home/zerops/eval-scenarios/develop-add-endpoint.md,\
  /home/zerops/eval-scenarios/develop-ambiguous-state.md,\
  /home/zerops/eval-scenarios/develop-dev-not-started.md,\
  /home/zerops/eval-scenarios/develop-dev-server-container.md,\
  /home/zerops/eval-scenarios/develop-dev-server-local.md,\
  /home/zerops/eval-scenarios/develop-pivot-auto-close.md,\
  /home/zerops/eval-scenarios/develop-close-mode-unset-regression.md \
  > /home/zerops/phase1-5.log 2>&1 < /dev/null & echo PID=\$!"
```

With watchdog polling pattern (10-min staleness, 90 min upper bound):

```bash
LAST_SIZE=0; LAST_GROWTH=$(date +%s)
until ssh zcp "grep -q '=== Scenario suite' /home/zerops/phase1-5.log"; do
  sleep 90
  SIZE=$(ssh zcp "stat -c %s /home/zerops/phase1-5.log 2>/dev/null || echo 0")
  NOW=$(date +%s)
  if [ "$SIZE" -gt "$LAST_SIZE" ]; then LAST_SIZE=$SIZE; LAST_GROWTH=$NOW; fi
  STALE=$((NOW - LAST_GROWTH))
  [ "$STALE" -gt 600 ] && { echo "STUCK: log silent ${STALE}s"; break; }
done
```

Triage same as Phase 1:

```bash
ssh zcp "cd /var/www && zcp eval triage --suite <suite-id>"
rsync zcp:/var/www/.zcp/eval/results/<suite-id>/ /Users/macbook/Documents/Zerops-MCP-evals/$(date -u +%Y-%m-%d)/phase1-5/
```

---

## 4. Acceptance criteria

Phase 1.5 closes when:

1. **All 9 scenarios run** (sequential, one suite). Per-scenario PASS/FAIL informational; suite continues through every scenario.
2. **Triage doc identifies new findings** beyond what Phase 1 surfaced — fresh signal on develop-side behaviors.
3. **No regression** in primitives Phase 1 confirmed (zerops_dev_server, zerops_discover, zerops_import, cross-deploy, auto-close).
4. **Backlog grows or shrinks based on real signal** — entries closed if Phase 1.5 confirms a fix already landed; new entries added if novel friction surfaces.
5. **Phase 2 brief drafted** based on combined Phase 1 + 1.5 findings.

If a scenario fails GRADER but the agent succeeds, treat it like Phase 1's pattern — diagnose whether it's calibration vs. real bug vs. framework issue.

---

## 5. Triage anti-pattern guard

Phase 1 surfaced a recurring pattern: agents emit `## EVAL REPORT` failure chains WITHOUT the `Root cause:` field, so all entries land in UNCATEGORIZED. The aggregator captures the body fields (Step, Went wrong, Recovered) correctly — the bucket is the only loss.

**Don't tighten the prompt for this in Phase 1.5.** Wait for more samples first; if the pattern persists across both phases, then either:
- Tighten the prompt schema with stricter wording
- Accept UNCATEGORIZED + rely on body prose for triage

Decide after Phase 1.5 with N=11+9 = 20 reports of evidence.

---

## 6. Suggested execution order

1. **Day 1.5.0** (optional pre-flight, ~30 min): re-read each scenario file to spot any post-Phase-1 vocabulary drift (e.g., scenarios that mention atoms or actions that have since been renamed). Patch in-place; commit.
2. **Day 1.5.1**: run the 9-scenario suite (~90 min wall + watchdog notifications).
3. **Day 1.5.2**: triage doc (`/Users/macbook/Documents/Zerops-MCP-evals/<date>/PHASE1-5-TRIAGE.md`), aggregate Failure chains and Information gaps.
4. **Day 1.5.3**: backlog updates (close, add, or promote entries based on signal).
5. **Day 1.5.4**: draft Phase 2 brief (~1h) with scope from Phase 1+1.5 combined.

Total ~3-4h calendar (most is wall-clock during suite run + triage thinking).

---

## 7. Anti-goals

- **No new scenario authoring** — Phase 1.5 runs what exists. If a needed scenario doesn't exist, document the gap, defer to Phase 2.
- **No framework changes** — `scenario-suite`, `triage`, watchdog all proved out in Phase 1. Touch only if a real bug surfaces.
- **No re-running Phase 1 scenarios** — Phase 1 closed clean (in the agent-success sense); rerunning eats wall-clock without new signal.
- **No fixing platform-side issues** — if Zerops migration causes flakes during Phase 1.5, document via watchdog notifications and either re-run after settle or accept partial signal.
- **No expanding scope to Phase 2 territory** — git-push setup, build-integration, mode expansion stay parked. Each is its own plan.

---

## 8. Open questions for the user

1. **Run timing** — Zerops migration was active during Tier-2; safe to run Phase 1.5 today, or wait? (Tier-3 ran fine post-migration.)
2. **Scenario file pre-flight** — should I read all 9 scenarios for vocabulary drift before kicking off, or trust them as-is and trust the eval signal to surface drift?
3. **B5 promotion** — Phase 1 surfaced B5 (cross-type recipe pair) as HIGH backlog. Should B5 promote to its own plan in parallel with Phase 1.5, or stay parked until Phase 1.5 triage?
