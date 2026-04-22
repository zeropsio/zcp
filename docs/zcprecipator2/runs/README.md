# runs/ — per-run analyses + verdicts

Every commissioned zcprecipator2 run lands one folder here: `runs/vN/`. The folder is self-contained — flow traces, bars the run was measured against, verdict, analysis. This README is both the **index** of past runs and the **runbook** for analysing a fresh one.

---

## Index

| Run | Tier | Date | Verdict | Root cause summary | Folder |
|---|---|---|---|---|---|
| v38 | showcase | pending | PRE-COMMISSION | target `v8.110.0` — verification of F-17 close (engine-built subagent brief); gated on [v38 fix-stack](../plans/v38-fix-stack.md) | [`v38/`](v38/) |
| v37 | showcase | 2026-04-21 | **PAUSE** | F-17 envelope content loss: main agent paraphrases atoms when composing Task prompts, so four v8.109.0 atom-level Cx fixes had zero runtime effect | [`v37/`](v37/) |
| v36 | showcase | 2026-04-21 | **PAUSE** (revised from ACCEPT-WITH-FOLLOW-UP) | 6 systemic open defects surfaced only on deep re-read; first analysis pass failed artifact-shape-as-proxy-for-depth — drove harness build | [`v36/`](v36/) |
| v35 | showcase | 2026-04-21 | **PAUSE + engine-level defects** | six pre-rollout engine/harness/knowledge-engine defects surfaced; v35 stalled at deploy-check | [`v35/`](v35/) |
| v34 | showcase | 2026-04-20 | PROCEED (baseline) | convergence empirically refuted (4 deploy rounds); pre-rewrite baseline | [`../01-flow/flow-showcase-v34-*`](../01-flow/) (not yet migrated to this layout) |

Future rows land at the top. Every run row points at its folder; every folder's `README.md` is the authoritative TL;DR for that run.

---

## Folder contract — what every `runs/vN/` must contain

| File | Purpose | Source |
|---|---|---|
| `README.md` | TL;DR + file index + verdict one-liner | hand-written at analysis time |
| `analysis.md` | narrative post-mortem with evidence pointers | hand-written at analysis time |
| `verdict.md` | PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK decision + rationale + measurement tightenings | hand-written at analysis time |
| `calibration-bars.md` | snapshot of bars the run was measured against + run results inline + any new bars added at post-run | copied from prior run's folder, evolved |
| `rollback-criteria.md` | snapshot of T-triggers used to arbitrate the run + any measurement-definition tightenings surfaced | copied from prior run's folder, evolved |
| `flow-main.md` | main-agent session trace | `extract_flow.py` output |
| `sub-*.md` (N files) | per-subagent traces (one per subagent stream) | `extract_flow.py` output |
| `flow-dispatches/*.md` | verbatim dispatch prompts transmitted to each sub-agent | `extract_flow.py` output |
| `role_map.json` | maps subagent-ID prefix → role slug for `extract_flow.py` | hand-written from deliverable tree inspection |

If a run is clean and uneventful, `analysis.md` can be short — but it still exists. If a run produces no new defect classes, the registry isn't touched — but the bars snapshot + verdict still land.

---

## Runbook — analysing a fresh run

Assume the user has handed you a run directory (e.g. `/Users/fxck/www/zcprecipator/<slug>/<slug>-vN/`) containing `SESSIONS_LOGS/` + the deliverable tree + optionally a user-authored `TIMELINE.md`.

### Step 1 — inventory

```bash
# Inspect the raw artifacts before doing anything else.
ls /Users/fxck/www/zcprecipator/<slug>/<slug>-vN/
ls /Users/fxck/www/zcprecipator/<slug>/<slug>-vN/SESSIONS_LOGS/
ls /Users/fxck/www/zcprecipator/<slug>/<slug>-vN/SESSIONS_LOGS/subagents/
cat /Users/fxck/www/zcprecipator/<slug>/<slug>-vN/TIMELINE.md   # if present
```

**Record**: the session ID, the run date, the tag under test, how many subagent streams exist.

### Step 2 — read the user's qualitative read

If `TIMELINE.md` is present, read it first. It's the user-authored narrative — load-bearing for framing the analysis. If the user also supplied prose (in chat), capture that too before running any extractor.

### Step 3 — create role map + run folder

```bash
mkdir -p docs/zcprecipator2/runs/vN/flow-dispatches
```

Write `docs/zcprecipator2/runs/vN/role_map.json`. Map each subagent ID prefix (first 3 chars of the `agent-*.jsonl` filename after the `agent-` prefix) to a role slug. Inspect each `.meta.json` sidecar — the `description` field names the role.

Example (v35):
```json
{
  "a16": "scaffold-apidev",
  "a2c": "scaffold-appdev",
  "af6": "scaffold-workerdev",
  "ae5": "feature",
  "a28": "writer-1",
  "a5f": "writer-2-fix",
  "ac9": "writer-3-fix"
}
```

### Step 4 — extract flow traces

```bash
python3 docs/zcprecipator2/scripts/extract_flow.py \
  /Users/fxck/www/zcprecipator/<slug>/<slug>-vN \
  --tier <showcase|minimal> \
  --ref vN \
  --role-map docs/zcprecipator2/runs/vN/role_map.json \
  --out-dir /tmp/vN-analysis/flow
```

Then move the outputs into `runs/vN/`:

```bash
mv /tmp/vN-analysis/flow/flow-showcase-vN-main.md          docs/zcprecipator2/runs/vN/flow-main.md
for f in /tmp/vN-analysis/flow/flow-showcase-vN-sub-*.md; do
  bn=$(basename "$f" | sed 's/flow-showcase-vN-//')
  mv "$f" docs/zcprecipator2/runs/vN/$bn
done
mv /tmp/vN-analysis/flow/flow-showcase-vN-dispatches/*.md docs/zcprecipator2/runs/vN/flow-dispatches/
rmdir /tmp/vN-analysis/flow/flow-showcase-vN-dispatches
```

Naming convention inside the folder:
- main trace → `flow-main.md` (keep `flow-` prefix)
- subagent traces → `sub-<role>.md` (drop `flow-showcase-vN-` prefix)
- dispatches → `flow-dispatches/<slug>.md` (keep as-is)

### Step 5 — cross-stream timeline (for stats + wall times)

```bash
python3 eval/scripts/timeline.py \
  /Users/fxck/www/zcprecipator/<slug>/<slug>-vN \
  --phase --stats --no-text > /tmp/vN-analysis/timeline.log
tail -60 /tmp/vN-analysis/timeline.log   # stats block
```

Record: total wall-clock, tool-call histogram, per-source wall, errored count. These feed `analysis.md` Appendix B.

### Step 6 — targeted extraction for check-failure evidence

For any workflow-check regression the run hit, dump the `checkResult` payloads from the relevant `zerops_workflow action=complete` responses. Use the Python template from v35 analysis (scan main-session.jsonl, pair tool_use with tool_result, extract `json.loads(response).checkResult.checks`). Copy from [v35 methodology](v35/analysis.md) if needed.

### Step 7 — copy standing docs, seed evolution

```bash
# Calibration bars: start from the most recent run's snapshot.
cp docs/zcprecipator2/runs/v{N-1}/calibration-bars.md docs/zcprecipator2/runs/vN/calibration-bars.md

# Rollback criteria: same pattern.
cp docs/zcprecipator2/runs/v{N-1}/rollback-criteria.md docs/zcprecipator2/runs/vN/rollback-criteria.md
```

Amend the header of each: "Snapshot status" block updates with run-N date, status (measured / run-stopped / clean), any new bars or tightenings surfaced.

### Step 8 — write `analysis.md`

Section skeleton (mirror [`v35/analysis.md`](v35/analysis.md)):

1. **Executive summary** — one paragraph, headline cause + outcome
2. **Findings** — ordered by blast radius, each with:
   - name + class + evidence (flow-row pointers + timestamps)
   - diagnosis (one paragraph)
   - consequence chain
   - invariant touched (if any)
3. **Secondary observations** — smaller issues documented, not blocking
4. **Cross-check against HANDOFF-to-IN invariants** — 6-row table (each current invariant × status on this run)
5. **What this run does NOT tell us** — explicit scope limits
6. **Calibration-bar coverage assessment** — which bars were measurable, which weren't, what new bars fell out
7. **What this analysis commits us to** — concrete follow-ups with file pointers
8. **Appendix A — key timestamps** — chronological table
9. **Appendix B — dispatch prompt lengths + sub-agent wall times** — derived from Step 5 stats

### Step 9 — write `verdict.md`

Section skeleton (mirror [`v35/verdict.md`](v35/verdict.md)):

1. **Decision** — ONE of PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK, with one-paragraph rationale
2. **Why not <other-decisions>** — walk every T-trigger against the run; show which fire, which don't, which are ambiguous
3. **Measurement-definition tightening** — any T-trigger whose text misfired or was ambiguous during arbitration gets tightened inline
4. **New bars added to the measurement sheet** — each new bar named + motivated (catches defect F-X)
5. **What PAUSE/ROLLBACK/etc. means in practice** — halted items vs not-halted items vs gate-to-proceed
6. **Appendix — decision-path audit trail** — timestamped log of the analysis process

### Step 10 — write `README.md` (TL;DR index)

Short — one screen. Date, tier, slug, session ID, verdict one-liner, defect table (if any), file index, "what else to look at" links. Mirror [`v35/README.md`](v35/README.md).

### Step 11 — update standing docs (only when warranted)

- **`05-regression/defect-class-registry.md`** — append one row per new defect class discovered in this run. Row format matches the registry convention. Section heading `### vN — <summary line>`.
- **`HANDOFF-to-IN+1.md`** — write only if the verdict is PAUSE or ROLLBACK, or if new work has been identified for the next instance. Skip if verdict is PROCEED with no follow-up.
- **`05-regression/recipe-version-log.md`** (if it exists) — one-line metric entry. This is multi-run history; keep chronological.

### Step 12 — update the index in this file

Add a row at the top of the "Index" table above. Bump the file count / commit.

### Step 13 — commit

```bash
git add docs/zcprecipator2/runs/vN/ docs/zcprecipator2/runs/README.md
# plus any standing-doc edits from Step 11
git commit -m "docs(zcprecipator2): vN run analysis + <verdict>"
```

If the run produced code-change follow-ups (new Cx-commits), those land in separate commits per the operating rules in CLAUDE.md.

---

## What makes analysis reproducible

- **Same inputs → same artifact shapes.** `extract_flow.py` + `timeline.py` are deterministic over a given session log. Re-running them produces identical traces.
- **Same folder contract → same file set.** Every `runs/vN/` matches the folder contract above. Fresh instance reading a past run doesn't have to guess what's there.
- **Evolutionary bars.** `runs/vN/calibration-bars.md` is always seeded from `runs/v{N-1}/calibration-bars.md`. Drift is visible in `git diff`.
- **Standing vs snapshot discipline.** Standing docs (`05-regression/defect-class-registry.md`, `HANDOFF-to-I*.md`, `06-migration/*`, atom tree, checks) grow across runs but never carry a `vN` suffix. Snapshot docs (`calibration-bars.md`, `rollback-criteria.md`) live per-run.

---

## Common pitfalls

- **Don't commit flow traces + analysis in the same commit as Cx fix-stack code.** Analysis is retrospective; code is forward. Separate commits keep history readable.
- **Don't rename past runs' files to match a new convention.** The analysis narrative references specific filenames; a rename breaks cold-read.
- **Don't mix framework specifics into atom corpus.** Run-specific gotchas belong in the run's `analysis.md` or the facts log — never in `internal/content/workflows/recipe/`.
- **Don't delete a past run's folder when reorganizing.** Every past run is historical truth; even if we later decide a different folder layout is better, migrate in place rather than delete.

---

## Related tooling

- [`docs/zcprecipator2/scripts/extract_flow.py`](../scripts/extract_flow.py) — per-stream trace + dispatch capture (step 4)
- [`eval/scripts/timeline.py`](../../../eval/scripts/timeline.py) — cross-stream chronological + stats (step 5)
- `scripts/measure_calibration_bars.sh` + `scripts/extract_calibration_evidence.py` — bar evaluators (step 8 / calibration-bars.md — pending Front A work per HANDOFF-to-I5)
