# Plan: Atom Corpus Hygiene — bootstrap + develop + idle clusters (2026-04-26)

> **Reader contract.** Self-contained. A fresh Claude instance opens the
> repo, reads this file, and can execute end-to-end. Cite by path when
> starting. Sister plan: `plans/atom-corpus-context-trim-2026-04-26.md`
> (the prior fixture-cap-chase trim — references its conventions, does
> NOT depend on it being incomplete).

## 1. Problem

The prior trim (`atom-corpus-context-trim-2026-04-26.md`) was a
**fixture-cap chase**: get the two known-overflow envelopes under the
28 KB body-join soft cap. It touched 12 atoms and shipped a working
allowlist-empty state. But the broader corpus was never audited:

- **65 of 79 atoms (~82 %) were never read for redundancy / quality**
  in that pass. Only the develop-active first-deploy fire-set + a
  handful of cross-atom dup canonical homes were touched.
- **68 of 79 atoms (~86 %) have ZERO atom-ID pin** in
  `scenarios_test.go` or `corpus_coverage_test.go`. A trim that
  removes a load-bearing fact from any of them ships green — no test
  surfaces the regression.
- **27 atoms restate `/var/www/<hostname>` SSHFS-mount semantics** as
  of 2026-04-26 (post-Phase-2 of the prior trim). Phase 2 deduped one
  pair (`platform-rules-container` ↔ `first-deploy-write-app`); the
  other 25 occurrences carry on.
- **No composition audit** has ever been run: when 23 atoms render for
  a develop-active first-deploy envelope, what does the agent ACTUALLY
  see? Are the atom transitions coherent? Are there contradictions
  across atoms? Are there gaps the agent has to fill from training?
  Nobody has read the rendered output as if they were the agent.
- **Atom necessity is unaudited along TWO axes**:
  1. *Fire-set necessity* — does this atom fire on any realistic
     envelope at all? (Dead atoms.)
  2. *Content-relevance necessity* (NEW lens, validated by user-test
     2026-04-26) — when the atom DOES fire on an envelope, does its
     content actually help the agent on THAT envelope, or is it
     tangential noise? An axis filter may be loose enough to admit
     atoms whose body content is irrelevant to the envelope shape.

### 1.1 User-test evidence (2026-04-26)

A separate user-test session edited a single Go file in a running
simple-mode-deployed service. The agent received **~30 atoms of
guidance**; the user-tester (LLM running the task) judged **~5 as
relevant**, the rest as context-window pollution.

Specific irrelevance examples observed in that envelope:
- `develop-mode-expansion` (talks about expanding to standard pair) —
  irrelevant for a simple-mode service that doesn't expand.
- `develop-dev-server-triage` (dev-server reason codes) — irrelevant
  for `implicit-webserver` runtime which has no dev-server concept.
- `develop-first-deploy-*` family — irrelevant when `deployed=true`.

The §6.3 fire-set matrix would correctly report all three as "fires
on this envelope" — they DO match the envelope's axes. The miss is
on the second axis: their content is NOT relevant to the user's
task. Axis-tightness audit (new content in §5/§7) addresses this.

This is *concrete production validation* of the plan's premise:
the corpus is ~75 % excess for any given envelope shape. The
hygiene plan is not premature optimization; it's already-late
optimization.

## 2. Goal

Per-atom **content-quality** hygiene across the bootstrap + develop +
idle clusters (74 atoms: 50 develop + 18 bootstrap + 6 idle, all of
the agent-facing runtime corpus minus 5 strategy + 1 export atoms
that are partially in scope per §11). The full corpus is 79 atoms
(74 in primary scope + 5 strategy + 1 export — see §11 for what each
phase touches). Three orthogonal hygiene axes:

1. **Per-atom audit.** For each atom: is every fact load-bearing,
   correctly scoped (envelope vs service), free of redundancy with
   other atoms or static surfaces, free of general LLM training
   knowledge, and densely written?
2. **Cross-atom hygiene.** Identify and consolidate cross-atom
   duplicates (same fact restated in N atoms with no axis
   justification). Pick a canonical home; demote others to one-line
   cross-links.
3. **Aggregate composition audit.** For each representative envelope
   shape, READ the full rendered status output as the agent does.
   Flag: incoherent transitions, contradictory advice across atoms,
   redundancy that survives at the sum level, gaps the agent has to
   guess through.

Acceptance is **measurable per phase** (bytes recovered, atoms touched,
new test pins added) and culminates in a final **aggregate-coherence
review** by a fresh-eyes pass.

## 3. Mental model — three delivery surfaces

```
┌──────────────────────────────────────────────────────────────────────┐
│ PER-TURN payload (every tool response)                               │
│                                                                      │
│ • Atoms (internal/content/atoms/*.md) — synthesized per envelope.    │
│ • RenderStatus skeleton (## Status, services, plan).                 │
│ • Tool descriptions (internal/tools/*.go::Description).              │
│                                                                      │
│ → Costs the LLM context window EVERY turn. Prime trim target.        │
└─────────────────────────┬────────────────────────────────────────────┘
                          │
┌─────────────────────────┴────────────────────────────────────────────┐
│ STATIC repo template (delivered once, on `zcp init`)                 │
│                                                                      │
│ • internal/content/templates/claude_shared.md (~72 lines)            │
│ • claude_container.md, claude_local.md (env-specific shims)          │
│                                                                      │
│ → Costs the LLM context window once per session boot. Anything in    │
│   the corpus that duplicates this is wasteful: the LLM already has   │
│   it from `CLAUDE.md`.                                               │
└─────────────────────────┬────────────────────────────────────────────┘
                          │
┌─────────────────────────┴────────────────────────────────────────────┐
│ FETCH-ON-DEMAND (`zerops_knowledge`, `zerops_recipe`)                │
│                                                                      │
│ • internal/knowledge/guides/*.md (21 guides as of 2026-04-26)        │
│ • internal/knowledge/themes/*.md (4 themes: core, model, ops, svcs)  │
│ • internal/knowledge/decisions/*.md (decision-trees)                 │
│ • internal/knowledge/recipes/*.md (per-stack walkthroughs)           │
│                                                                      │
│ → Costs nothing per turn. Costs only when the agent fetches.         │
│   Right home for: catalogs, deep references, on-demand protocols.    │
└──────────────────────────────────────────────────────────────────────┘
```

**Hygiene rule:** content belongs on the surface that **minimizes the
agent's overall consumption** while keeping the fact reachable when
needed. Per-turn delivery is the most expensive — earn its slot.

## 4. Empirical baseline (snapshot 2026-04-26)

### 4.1 Corpus totals

```
79 atom files (after prior trim's 2 split-half additions)
~115 KB total
50 develop-* atoms (~68 KB)
18 bootstrap-* atoms (~25 KB)
6 idle-* atoms (~4 KB)
5 strategy-* atoms (~10 KB)
1 export.md (~7 KB; outlier, single-fixture-only)
2 -cmds atoms (per-service tool-call lines from prior trim splits)
```

### 4.2 Test-pin coverage gap

```
Atom-ID mentions per atom in (scenarios_test.go ∪ corpus_coverage_test.go):

  68 atoms: ZERO mentions  (86 %)  ← silent-trim risk
   8 atoms: 1-2 mentions   (10 %)  ← weak coverage
   3 atoms: 3+ mentions    ( 4 %)  ← strong coverage
```

Re-derive with:

```bash
for atom in internal/content/atoms/*.md; do
  id=$(basename "${atom%.md}")
  cnt=$(grep -c "$id" \
    internal/workflow/scenarios_test.go \
    internal/workflow/corpus_coverage_test.go \
    | awk -F: '{sum+=$2} END {print sum+0}')
  echo "$cnt $id"
done | sort -n
```

### 4.3 Known cross-cluster overlap territory (from probes 2026-04-26)

| Concept | Atoms carrying it | Canonical (post-prior-trim) |
|---|---|---|
| `/var/www/<hostname>` SSHFS path | **27 atoms** | platform-rules-container (only 1 pair deduped) |
| `${hostname_KEY}` env-ref syntax | 4 atoms | first-deploy-env-vars (other 3 deduped Phase 2) |
| `deploy = new container, deployFiles persists` | 3 atoms | platform-rules-common (other 2 deduped Phase 2) |
| Managed-service env-var catalog | bootstrap-env-var-discovery (canonical) + … | bootstrap-env-var-discovery (still ~2926 B; potential MOVE-TO-GUIDE) |
| Standard-mode dev+stage pair semantics | 5+ atoms | first-deploy-promote-stage (concept) + auto-close-semantics (close rule) |
| `sudo apk add` / `sudo apt-get install` `prepareCommands` | 2+ atoms | platform-rules-common (already canonical) |
| `zerops_dev_server` field shape | 2 atoms | platform-rules-container (already canonical) |
| `agent-browser` tool | 3+ atoms | verify-matrix (concept) |

### 4.4 Top-30 atoms by size (descending)

```
2926  bootstrap-env-var-discovery        (catalog; MOVABLE-TO-GUIDE candidate)
2823  develop-first-deploy-write-app     (touched by prior trim)
2749  develop-platform-rules-local       (NEVER AUDITED)
2713  bootstrap-route-options            (NEVER AUDITED)
2364  bootstrap-provision-rules          (NEVER AUDITED)
2357  develop-dynamic-runtime-start-container  (NEVER AUDITED)
2328  develop-dev-server-triage          (NEVER AUDITED)
2183  develop-first-deploy-scaffold-yaml (touched by prior trim)
2158  develop-deploy-modes               (touched by prior trim)
2067  develop-first-deploy-asset-pipeline-container (NEVER AUDITED)
1993  develop-platform-rules-container   (touched by prior trim)
1912  develop-api-error-meta             (NEVER AUDITED; load-bearing per Codex round #6)
1901  develop-ready-to-deploy            (NEVER AUDITED)
1897  bootstrap-close                    (NEVER AUDITED)
1843  develop-dynamic-runtime-start-local (NEVER AUDITED)
1827  develop-implicit-webserver         (touched by prior trim)
1747  bootstrap-resume                   (NEVER AUDITED)
1746  develop-first-deploy-asset-pipeline-local (NEVER AUDITED)
1715  develop-verify-matrix              (touched by prior trim)
1707  develop-http-diagnostic            (NEVER AUDITED)
1684  bootstrap-recipe-import            (NEVER AUDITED)
1683  develop-push-git-deploy            (NEVER AUDITED)
1645  develop-mode-expansion             (NEVER AUDITED)
1572  develop-dev-server-reason-codes    (NEVER AUDITED)
1524  develop-env-var-channels           (NEVER AUDITED)
1445  bootstrap-provision-local          (NEVER AUDITED)
1441  develop-first-deploy-intro         (touched by prior trim)
1430  develop-manual-deploy              (NEVER AUDITED)
1423  develop-deploy-files-self-deploy   (NEVER AUDITED)
1410  develop-platform-rules-common      (touched by prior trim)
```

23 of top-30 = **NEVER AUDITED**. Total never-audited bytes ≈ ~30 KB.

## 5. Ten-axis hygiene taxonomy (apply per atom)

Each atom receives one or more labels from this taxonomy. Choice of
labels drives the action.

### Axis A — LEAN
Already minimal, every fact load-bearing, no redundancy.
**Action:** none.

### Axis B — PROSE-VERBOSE
Body has defensive emphasis, hedge phrases, inverse-restatement pairs,
multi-paragraph explanations of single rules. Could shrink ~30 % with
denser form.
**Action:** rewrite in place. Use tables for matrices, numbered lists
for sequential steps, decision-tree triplets for branching logic.

### Axis C — REDUNDANT-WITHIN-CLUSTER
A fact appears verbatim or near-verbatim in 2+ atoms within the same
cluster, no axis justification (not container-vs-local, not mode-
specific). Example: SSHFS path semantics in 27 atoms.
**Action:** pick canonical home (lowest priority, broadest axis, or
topical owner); replace non-canonical occurrences with one-line cross-
links + `references-atoms` frontmatter entry.

### Axis D — REDUNDANT-CROSS-CLUSTER
Bootstrap atom restates a develop atom (or vice versa). The clusters
fire in different phases so they don't co-render, but content drift
risks accumulate when the two divergence.
**Action:** pick the cluster that owns the concept (env-var catalog =
bootstrap; verify protocol = develop). Other cluster keeps a one-line
phase-relevant note + cross-link.

### Axis E — REDUNDANT-WITH-TEMPLATE / GUIDE
Content already lives in `claude_shared.md` (delivered every session
boot via `zcp init`) or in `internal/knowledge/guides/*.md` (fetchable
on demand). Per-turn delivery is wasteful.
**Action:** drop the atom-side restatement; if reachability matters,
add a one-liner naming the static surface or guide URI.

### Axis F — GENERAL-LLM-KNOWLEDGE
Body is mostly content the LLM's training carries (HTTP basics, npm
install semantics, framework defaults, "what 0.0.0.0 binds to").
**Action:** drop the general-knowledge prose; keep only ZCP-specific
nuance (e.g. "L7 reverse-proxy — bind 0.0.0.0" stays because L7 is
ZCP-specific; "what 0.0.0.0 means" goes).

### Axis G — VERIFIABLE-AT-RUNTIME
Body teaches a catalog or shape the agent could fetch via
`zerops_discover`, `zerops_logs`, `zerops_env`, etc. The tool is the
authoritative source; the atom is a stale copy.
**Action:** replace catalog/shape with one-liner naming the tool call;
link to the Go field via `references-fields` frontmatter so the test
catches drift.

### Axis H — SPLIT-CANDIDATE
Like Phase 1 of the prior trim: atom mixes envelope-level rules + per-
service tool-call lines. Service-scoped axes cause per-service
duplication; rules half can move to envelope-scoped (use
`envelopeDeployStates` / `envelopeRuntimes` if added) or keep with
`{hostname}` → `<hostname>` literal placeholder per Phase 1 idiom.
**Action:** split into `<id>` (rules, envelope-scoped) + `<id>-cmds`
(per-service tool-call lines). Same shape as prior trim's execute /
verify splits.

### Axis I — DEAD ATOM (new — composition-audit finding)
Atom fires on no realistic envelope, OR fires only on a synthetic
envelope shape no actual user reaches. Identifiable via fire-set
analysis: walk every coverageFixture, every scenario, every
plausible runtime envelope; an atom that never appears in any
fire-set is dead.
**Action:** delete entirely.

### Axis J — AXIS-LOOSE ATOM (new, from user-test 2026-04-26 evidence)
Atom fires on envelopes where its content is NOT relevant to the
agent's likely task. The axis filter is loose enough to admit
envelopes the atom doesn't help. Examples from §1.1:
- `develop-mode-expansion` fires on simple-mode envelopes where
  expansion isn't applicable.
- `develop-dev-server-triage` fires on implicit-webserver runtimes
  with no dev-server concept.
- `develop-first-deploy-*` atoms can fire on a mixed envelope where
  ONE service is never-deployed and others are deployed; the
  first-deploy atoms still render even when the agent's task is
  about a different (already-deployed) service.

**Action:** tighten the atom's frontmatter axes. Add `runtimes:`,
`modes:`, `deployStates:` (or `envelopeDeployStates:` for once-per-
envelope content) constraints that prevent the atom from firing on
envelopes where it doesn't help. The synthesizer's existing axis
filter does the rest.

**Risk**: tightening axes can drop the atom from envelopes where it
WAS being delivered. Confirm via fire-set re-run that axis-tightening
doesn't drop the atom from any envelope where it's actually load-
bearing. Commit per §6.1 fact inventory + Codex round per §10.1
Phase 7.

## 6. Methodology

### 6.1 Per-atom audit form

Every touched atom gets a fact inventory committed alongside the edit
(addressing the prior-trim's §16.2 gap). Form:

```
Atom: <id>
Pre-edit bytes: NNNN
Post-edit bytes: NNNN
Axis labels: [B, C, ...]   (from §5)

Facts in original body:
- F1: "<short fact>"  →  KEEP (with new MustContain pin "...")
- F2: "<short fact>"  →  MOVE (to atom <other-id>)
- F3: "<short fact>"  →  DROP (axis E: in claude_shared.md:NN)
- F4: "<short fact>"  →  DROP (axis F: general LLM knowledge)
- ...
```

Commit message embeds the inventory verbatim. A reviewer can audit
"what was preserved / moved / dropped" without reading the diff.

### 6.2 Composition audit (per envelope)

For each representative envelope shape (every entry in
`coverageFixtures()`):

1. Run probe (see §6.4) → produces the rendered status text.
2. **Read it as the agent would**: top-to-bottom, no skipping.
3. Score on 4 dimensions (1-5 scale, anchors below).
4. Commit a per-envelope composition-audit document in `plans/audit-
   composition/<fixture-name>.md`. Score + 1-2 paragraph qualitative
   notes + flagged improvement targets.

After all per-atom edits land, **re-run composition audit on the same
envelopes**. Score deltas + qualitative narrative should both improve.

#### Scoring rubric

Each dimension scored 1-5 with these anchors. Two executors should
produce ±1 agreement after reading the same rendered output.

**Coherence — do atom transitions read as one document or as a bag of
disconnected snippets?**

| Score | Anchor |
|---|---|
| 5 | Reads as one cohesive document. Atom transitions are invisible — sections build on each other; cross-atom references resolve naturally. |
| 4 | Mostly cohesive. 1-2 awkward transitions where a section feels dropped in. |
| 3 | Sections are individually readable but transitions are jarring. The agent has to mentally reset between atoms. |
| 2 | Bag of snippets. Sections contradict tone, repeat orientation, or address different audiences. |
| 1 | Incoherent. Sections actively contradict each other (different recommendations, different vocabulary). |

**Density — load-bearing facts per KB.**

A "load-bearing fact" is one operational instruction the agent must
act on. Count distinct facts; divide by KB. Anchors:

| Score | Threshold |
|---|---|
| 5 | ≥ 4.0 facts/KB. Almost no prose padding. |
| 4 | 3.0-3.9 facts/KB. Lean, occasional defensive emphasis. |
| 3 | 2.0-2.9 facts/KB. Acceptable but rewriteable. |
| 2 | 1.0-1.9 facts/KB. Significant prose verbosity. |
| 1 | < 1.0 fact/KB. Mostly orientation/explanation, sparse action. |

**Redundancy — facts restated by 2+ atoms in this output.**

| Score | Threshold (per fixture render) |
|---|---|
| 5 | 0 cross-atom restated facts. |
| 4 | 1 restated fact (often platform invariant — borderline acceptable). |
| 3 | 2-3 restated facts. Cross-link should replace at least one. |
| 2 | 4-6 restated facts. |
| 1 | 7+ restated facts. Significant cross-atom dup territory. |

**Coverage-gap — facts the agent would need that aren't present.**

To score: for the envelope shape, enumerate the 5-10 most likely next
agent actions; for each, check whether the rendered output names the
tool / arg shape / decision rule needed. Count gaps.

| Score | Threshold |
|---|---|
| 5 | 0 gaps. Every plausible next-action is supported. |
| 4 | 1 gap on a low-probability next-action. |
| 3 | 1 gap on a likely next-action OR 2-3 gaps total. |
| 2 | Likely-action gap + multiple low-probability gaps. |
| 1 | Major gap (e.g. "what tool to call next" unclear). |

**Task-relevance — fraction of rendered atoms relevant to the most
likely task on this envelope** (NEW dimension from user-test 2026-04-26).

Per envelope, identify the 1-3 most likely tasks the agent would
perform (e.g. simple-mode-deployed envelope: "edit a file +
re-deploy"; first-deploy envelope: "scaffold zerops.yaml + write app
+ deploy"). For each rendered atom, judge: relevant / partially-
relevant / irrelevant / actively-noise.

| Score | Threshold |
|---|---|
| 5 | ≥ 90 % of atoms relevant. |
| 4 | 75-89 % relevant. |
| 3 | 50-74 % relevant. |
| 2 | 25-49 % relevant. |
| 1 | < 25 % relevant (user-test baseline = ~17 %; current corpus). |

This dimension catches Axis J (axis-loose atoms) at the composition
level. A score of 1-2 here flags axis-tightening targets for Phase 7.

**Re-run scoring** post-hygiene must show: coherence + density +
task-relevance non-decreasing; redundancy + coverage-gap strictly
improving (or flat-at-5).

### 6.3 Necessity check

For each atom: enumerate every envelope it fires on (using `Synthesize`
+ the existing scenarios + a generated set of plausible envelopes
covering bootstrap routes × develop strategies × runtime classes ×
modes × deploy states). If empty → DEAD (drop). If only one fixture →
flag for "marginal" review (might consolidate with another atom).

Generate the fire-set matrix via a temporary probe binary (delete after
Phase 8 per §11 / §12). The "plausible envelope" generator is the
Cartesian product of:
- `Phase ∈ {idle, bootstrap-active, develop-active, develop-closed-auto, strategy-setup, export-active}`
  (Go const names: `PhaseIdle`, `PhaseBootstrapActive`, `PhaseDevelopActive`, `PhaseDevelopClosed`, `PhaseStrategySetup`, `PhaseExportActive` — note `PhaseDevelopClosed` const name vs `"develop-closed-auto"` string value).
- `Environment ∈ {container, local}` — both must be enumerated for every phase, including idle, bootstrap, strategy-setup. (Phase 0 round 1 fix: container was hardcoded for several sub-products.)
- For phase=idle: `IdleScenario ∈ {empty, bootstrapped, adopt, incomplete, orphan}`.
- For phase=bootstrap-active: every `(Route, Step, Env)` triple from `BootstrapRoute × {discover, provision, close} × {container, local}`. The atom-valid step enum is `{discover, provision, close}` per `internal/workflow/atom.go::"steps"`; `generate` and `deploy` are workflow-only states with no atom axis. Bootstrap envelopes must also generate a service-bearing variant per `RuntimeClass` so service-scoped bootstrap atoms (e.g. `bootstrap-classic-plan-dynamic`) can match — `Synthesize` skips service-scoped atoms when no envelope service satisfies their axes (`internal/workflow/synthesize.go:61-74`).
- For phase=develop-active: every `(Mode, Strategy, Trigger, RuntimeClass, DeployState, Env)` tuple where `serviceSatisfiesAxes` accepts at least one envelope. `Trigger ∈ {unset, actions, webhook}` (`topology.TriggerUnset`/`TriggerActions`/`TriggerWebhook`) is required because strategy atoms have `triggers:` axes. Plus a multi-service mixed-deploy-state variant per environment so first-deploy atoms render against envelopes where one service is undeployed and another is deployed (Axis J risk surface).
- For phase=develop-closed-auto: per `Env`. No service axes consulted by atoms in this phase.
- For phase=strategy-setup: per `(Env, Trigger)`, with one push-git service per envelope so service-scoped strategy atoms can match.
- For phase=export-active: container-only (per `internal/content/atoms/export.md::environments: [container]`); generate at least one variant.

There is **no** `envelopeRuntimes` axis — atom frontmatter accepts `runtimes` (service-scoped) and `envelopeDeployStates` (envelope-scoped) but NOT envelope-scoped runtimes. Round 1 sketch implied otherwise; the corrected generator does not generate over a phantom axis.

```go
// cmd/atom_fire_audit/main.go (build with: go build -o /tmp/fire_audit ./cmd/atom_fire_audit/)
// Output: one line per atom_id with the list of (fixture-name | synthetic-envelope-key)
// it fires on. Atoms with empty fire-set are DEAD candidates for Phase 1.
package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func main() {
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		panic(err)
	}

	// fireMap: atom_id → ordered list of envelope keys that match it.
	fireMap := make(map[string][]string)
	for _, atom := range corpus {
		fireMap[atom.ID] = nil
	}

	// 1. Walk every coverageFixture from corpus_coverage_test.go (you may
	//    need to copy the relevant envelope structs into this binary or
	//    duplicate the fixture list here — the test package is _test.go
	//    so callers outside the package can't import it).
	for _, fx := range listCoverageFixtures() {
		matches, err := workflow.Synthesize(fx.envelope, corpus)
		if err != nil {
			continue
		}
		for _, m := range matches {
			fireMap[m.AtomID] = append(fireMap[m.AtomID], "fixture:"+fx.name)
		}
	}

	// 2. Walk plausible-envelope Cartesian product (synthetic envelopes
	//    not pinned to any specific test fixture but representative of
	//    real user states).
	for _, env := range generatePlausibleEnvelopes() {
		matches, err := workflow.Synthesize(env.envelope, corpus)
		if err != nil {
			continue
		}
		for _, m := range matches {
			fireMap[m.AtomID] = append(fireMap[m.AtomID], "synthetic:"+env.key)
		}
	}

	// Print: dead atoms first (empty fire-set), then by fire-set size ascending.
	type row struct {
		id      string
		matches []string
	}
	rows := make([]row, 0, len(fireMap))
	for id, ms := range fireMap {
		// Dedupe — atom can fire across multiple envelopes; we want unique set.
		seen := map[string]struct{}{}
		var unique []string
		for _, m := range ms {
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			unique = append(unique, m)
		}
		sort.Strings(unique)
		rows = append(rows, row{id, unique})
	}
	sort.Slice(rows, func(i, j int) bool {
		if len(rows[i].matches) != len(rows[j].matches) {
			return len(rows[i].matches) < len(rows[j].matches)
		}
		return rows[i].id < rows[j].id
	})
	fmt.Printf("%-50s  %5s  %s\n", "atom_id", "count", "fires-on")
	for _, r := range rows {
		fmt.Printf("%-50s  %5d  %s\n", r.id, len(r.matches), strings.Join(r.matches, ", "))
	}
}

type fixture struct {
	name     string
	envelope workflow.StateEnvelope
}

// listCoverageFixtures duplicates the entries from
// internal/workflow/corpus_coverage_test.go::coverageFixtures().
// Keep in sync manually — corpus_coverage_test.go is _test.go and not
// importable from cmd/.
func listCoverageFixtures() []fixture {
	// ... copy each coverageFixture() entry's Name + Envelope here.
	// Plan executor: snapshot the fixtures at the time the audit runs;
	// dont try to programmatically pull from the test package.
	return nil
}

type plausibleEnvelope struct {
	key      string
	envelope workflow.StateEnvelope
}

// generatePlausibleEnvelopes builds the Cartesian product of axis values
// described in §6.3 of the hygiene plan. Each entry is keyed by a stable
// short string so the output is deterministic + greppable.
//
// Codex round 1 (2026-04-26) corrections applied:
//   - bootstrap step set narrowed to {discover, provision, close} (atom-
//     valid set per internal/workflow/atom.go::"steps"). `generate` and
//     `deploy` are workflow-only, not atom axes.
//   - bootstrap envelopes carry service snapshots so service-scoped
//     atoms (e.g. bootstrap-classic-plan-dynamic) can fire — Synthesize
//     skips service-scoped atoms when no service satisfies axes
//     (internal/workflow/synthesize.go:61-74).
//   - bootstrap + idle + strategy enumerate both environments.
//   - develop product expanded over Trigger axis ({unset, actions, webhook}).
//   - strategy-setup synthesised per (env × trigger) with a push-git service.
//   - export-active synthesised (container-only per export.md axis).
//   - develop-closed-auto synthesised per env (PhaseDevelopClosed const
//     name; "develop-closed-auto" is the string value).
//   - multi-service mixed-deploy-state develop envelopes added — covers
//     Axis J risk where first-deploy atoms render against an envelope
//     in which one service is undeployed and others are deployed.
//   - no `envelopeRuntimes` generation — that axis does not exist
//     (internal/workflow/atom.go enums only `runtimes` + `envelopeDeployStates`).
func generatePlausibleEnvelopes() []plausibleEnvelope {
	var out []plausibleEnvelope

	envs := []workflow.Environment{workflow.EnvContainer, workflow.EnvLocal}
	runtimes := []topology.RuntimeClass{
		topology.RuntimeDynamic, topology.RuntimeStatic,
		topology.RuntimeImplicitWeb, topology.RuntimeManaged,
	}
	triggers := []topology.PushGitTrigger{
		topology.TriggerUnset, topology.TriggerActions, topology.TriggerWebhook,
	}

	// ── Idle — every (env × IdleScenario) pair.
	for _, env := range envs {
		for _, scen := range []workflow.IdleScenario{
			workflow.IdleEmpty, workflow.IdleBootstrapped, workflow.IdleAdopt,
			workflow.IdleIncomplete, workflow.IdleOrphan,
		} {
			out = append(out, plausibleEnvelope{
				key: fmt.Sprintf("idle/%s/%s", env, scen),
				envelope: workflow.StateEnvelope{
					Phase: workflow.PhaseIdle, Environment: env,
					IdleScenario: scen,
				},
			})
		}
	}

	// ── Bootstrap-active — every (env × route × step) with a no-service
	// variant (envelope-scoped atoms) AND a one-service-per-runtime variant
	// (service-scoped atoms like bootstrap-classic-plan-dynamic).
	routes := []workflow.BootstrapRoute{
		workflow.BootstrapRouteRecipe, workflow.BootstrapRouteClassic,
		workflow.BootstrapRouteAdopt, workflow.BootstrapRouteResume,
	}
	bootSteps := []string{"discover", "provision", "close"} // atom-valid set
	for _, env := range envs {
		for _, route := range routes {
			for _, step := range bootSteps {
				out = append(out, plausibleEnvelope{
					key: fmt.Sprintf("bootstrap/%s/%s/%s/no-svc", env, route, step),
					envelope: workflow.StateEnvelope{
						Phase: workflow.PhaseBootstrapActive, Environment: env,
						Bootstrap: &workflow.BootstrapSessionSummary{Route: route, Step: step},
					},
				})
				for _, rt := range runtimes {
					out = append(out, plausibleEnvelope{
						key: fmt.Sprintf("bootstrap/%s/%s/%s/svc-%s", env, route, step, rt),
						envelope: workflow.StateEnvelope{
							Phase: workflow.PhaseBootstrapActive, Environment: env,
							Bootstrap: &workflow.BootstrapSessionSummary{Route: route, Step: step},
							Services: []workflow.ServiceSnapshot{{
								Hostname: "app", TypeVersion: "nodejs@22",
								RuntimeClass: rt, Mode: topology.ModeStandard,
								Strategy: topology.StrategyUnset,
								Bootstrapped: true, Deployed: false,
							}},
						},
					})
				}
			}
		}
	}

	// ── Develop-active — Cartesian over (env × mode × strategy × trigger
	// × runtime × deployState).
	modes := []topology.Mode{
		topology.ModeDev, topology.ModeStage, topology.ModeStandard,
		topology.ModeSimple, topology.ModeLocalStage, topology.ModeLocalOnly,
	}
	strategies := []topology.DeployStrategy{
		topology.StrategyPushDev, topology.StrategyPushGit,
		topology.StrategyManual, topology.StrategyUnset,
	}
	deployStates := []bool{false, true}
	for _, env := range envs {
		for _, mode := range modes {
			for _, strat := range strategies {
				for _, trig := range triggers {
					for _, rt := range runtimes {
						for _, deployed := range deployStates {
							out = append(out, plausibleEnvelope{
								key: fmt.Sprintf("develop/%s/%s/%s/%s/%s/dep=%v",
									env, mode, strat, trig, rt, deployed),
								envelope: workflow.StateEnvelope{
									Phase: workflow.PhaseDevelopActive, Environment: env,
									Services: []workflow.ServiceSnapshot{{
										Hostname: "appdev", TypeVersion: "nodejs@22",
										RuntimeClass: rt, Mode: mode,
										Strategy: strat, Trigger: trig,
										Bootstrapped: true, Deployed: deployed,
									}},
								},
							})
						}
					}
				}
			}
		}
	}
	// Multi-service mixed deploy-state — covers Axis J risk: envelopeDeployStates
	// matches if ANY bootstrapped service has the wanted state, so first-deploy
	// atoms can fire when ONE service is undeployed despite others being deployed.
	for _, env := range envs {
		out = append(out, plausibleEnvelope{
			key: fmt.Sprintf("develop/%s/multi/mixed-deploy", env),
			envelope: workflow.StateEnvelope{
				Phase: workflow.PhaseDevelopActive, Environment: env,
				Services: []workflow.ServiceSnapshot{
					{Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						Strategy:     topology.StrategyPushDev,
						Bootstrapped: true, Deployed: true},
					{Hostname: "workerdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						Strategy:     topology.StrategyPushDev,
						Bootstrapped: true, Deployed: false},
				},
			},
		})
	}

	// ── Develop-closed-auto — single phase per env, no service axes.
	for _, env := range envs {
		out = append(out, plausibleEnvelope{
			key: fmt.Sprintf("develop-closed-auto/%s", env),
			envelope: workflow.StateEnvelope{
				Phase: workflow.PhaseDevelopClosed, Environment: env,
			},
		})
	}

	// ── Strategy-setup — push-git × env × trigger; one service per envelope.
	for _, env := range envs {
		for _, trig := range triggers {
			out = append(out, plausibleEnvelope{
				key: fmt.Sprintf("strategy-setup/%s/push-git/%s", env, trig),
				envelope: workflow.StateEnvelope{
					Phase: workflow.PhaseStrategySetup, Environment: env,
					Services: []workflow.ServiceSnapshot{{
						Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeStandard,
						Strategy:     topology.StrategyPushGit, Trigger: trig,
						Bootstrapped: true, Deployed: false,
					}},
				},
			})
		}
	}

	// ── Export-active — container-only (export.md::environments: [container]).
	out = append(out, plausibleEnvelope{
		key: "export-active/container",
		envelope: workflow.StateEnvelope{
			Phase: workflow.PhaseExportActive, Environment: workflow.EnvContainer,
			Services: []workflow.ServiceSnapshot{{
				Hostname: "appdev", TypeVersion: "nodejs@22",
				RuntimeClass: topology.RuntimeDynamic,
				Mode:         topology.ModeStandard,
				Strategy:     topology.StrategyPushDev,
				Bootstrapped: true, Deployed: true,
			}},
		},
	})

	return out
}
```

The probe is intentionally verbose. The plan executor can prune branches
once fire-sets stabilize — initial run prefers exhaustiveness.

### 6.4 Re-measurement probe (re-use the prior trim's probe)

Recreate `cmd/atomsize_probe/main.go` from the prior trim plan §6.2 +
§17.5 (the source is in git history at `c8d87406`). Add the new fixtures
the hygiene plan creates. Delete after Phase 5.

### 6.5 Verify gate (re-use the prior trim's gate)

After every individual atom edit:

```bash
go test ./internal/workflow/ \
  -run "TestCorpusCoverage|TestSynthesize|TestAtom|TestScenario" \
  -count=1
go test ./internal/content/ -count=1
make lint-local
```

After every PHASE: full `go test ./... -short -count=1 -race`.

### 6.6 Multi-layer verification (per-phase + final)

The §6.5 verify gate is necessary but insufficient — it catches
test-pinned regressions only. Hygiene work risks subtler failures
(facts dropped without test coverage, agent comprehension regressed,
composition coherence broken). Five layered checks address each class:

| Layer | What it catches | When applied |
|---|---|---|
| **L1 — Unit/test gate** (§6.5) | Pinned regressions, lint, race. | After every commit. |
| **L2 — Probe re-measurement** (§6.4) | Byte-recovery deltas; dedup confirmation; per-atom-render counts. | After every commit that edits a corpus atom. |
| **L3 — Composition audit** (§6.2) | Per-fixture aggregate quality (coherence/density/redundancy/coverage-gap). Subjective scoring with rubric anchors. | Phase 0 baseline + Phase 7 re-score. |
| **L4 — Composition cross-validation** (NEW) | Composition scoring drift between two independent reads. | Per Phase 7 fixture: TWO Codex rounds with same fixture + rubric, compare scores. ≥2 disagreement triggers rubric refinement. |
| **L5 — Live smoke test** (NEW) | Wire-frame on real MCP STDIO transport; agent recovery from compaction; per-phase envelope deliverability. | Phase 0 baseline + Phase 8 final gate. |

#### L5 — Live smoke test procedure (per prior trim §17.5)

eval-zcp container (per CLAUDE.local.md authorization) is the
playground.

```bash
# Build the patched binary on dev box.
make linux-amd

# Push to zcp container in eval-zcp project.
scp builds/zcp-linux-amd64 zcp:/tmp/zcp-hygiene
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    zcp 'cp /tmp/zcp-hygiene ~/.local/bin/zcp'

# Issue MCP STDIO call: initialize → status (idle envelope).
ssh zcp 'cat <<EOF > /tmp/mcp.json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"hygiene-smoke","version":"0"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"zerops_workflow","arguments":{"action":"status"}}}
EOF
( cat /tmp/mcp.json; sleep 6 ) | timeout 10s zcp serve 2>/dev/null > /tmp/resp.ndjson'

# Decode response.content[0].text length per line; assert under 32 KB cap.
ssh zcp 'awk "NR==2 {print length()}" /tmp/resp.ndjson'
```

Repeat for develop-active envelope (provision a runtime service if
needed per CLAUDE.local.md "When tests/evals require a fresh runtime
service, provision one yourself").

L5 has TWO assertions:
1. Wire-frame matches probe number ±1 byte (validates probe accuracy
   end-to-end).
2. Decoded `text` parses as valid markdown + contains the expected
   atom-rendered structure (`## Status`, services, plan, guidance).

Failure on L5 = ship-blocker until root-caused. The probe could be
lying about wire-frame, OR the production transport adds bytes the
probe doesn't model.

### 6.7 Eval-scenario regression check

Optional but recommended for Phase 7 / 8: run a known eval scenario
from `internal/eval/scenarios/` against the new corpus. Compare agent
moves vs the pre-hygiene baseline (capture from a `git stash` or a
worktree on prior commit).

```bash
# Run a bootstrap → first-deploy scenario end-to-end on eval-zcp.
go test ./internal/eval/ -run TestScenario_WeatherDashboardNodejs -count=1 -v
```

Document any divergence in `plans/audit-composition/eval-regression-<scenario>.md`.
A divergence is NOT automatically a regression — the new corpus may
guide the agent to a BETTER move. But unexamined divergence is a
ship-blocker until the executor evaluates it as improvement OR
regression.

## 7. Phased execution

> **Pacing rule.** ~74 in-scope atoms across 9 phases (Phase 0-8) means
> ~10 atoms per phase on average. Each phase is ~1-3 commits with
> verify-gate between. Codex protocol per phase per §10. No half-
> finished states.

> **Universal phase contract.** Every phase has explicit ENTRY,
> WORK-SCOPE, and EXIT criteria. A phase is "complete" only when its
> EXIT condition is verifiable — not when work feels done. EXIT
> conditions are gateways: the next phase MAY NOT begin until the
> prior phase's EXIT is satisfied.

### Phase 0 — Calibration

**ENTRY**: working tree clean; current corpus state matches §4
baseline within 15 % (pin-coverage gap derivation in §4.2 returns
~68 unpinned atoms ±5).

**WORK-SCOPE**: build infrastructure for measurement-driven hygiene
before touching content.

1. **Recreate `cmd/atomsize_probe/main.go`** from prior trim's source
   (git show `c8d87406:cmd/atomsize_probe/main.go`). Confirm baseline
   numbers match current corpus state.
2. **Build `cmd/atom_fire_audit/main.go`** per §6.3 source sketch.
   Generate fire-set matrix for ALL 79 atoms across the existing 30+
   coverage fixtures + plausible-envelope generator. Commit the matrix
   output as `plans/audit-composition/fire-set-matrix.md` (table only;
   not an atom).
3. **Pin-coverage gap test.** Add `TestCorpusCoverage_PinDensity` —
   asserts every atom_id from `LoadAtomCorpus()` is named as an
   argument to `requireAtomIDsContain` / `requireAtomIDsExact` in
   `internal/workflow/scenarios_test.go`. Atom-ID mentions in
   `coverageFixture.MustContain` are NOT counted as pins — those
   strings are phrase-pins, not ID-pins, and may match atom IDs
   coincidentally. Currently 68 atoms fail this — allowlist the
   failures with a `knownUnpinnedAtoms` map following the same
   ratchet pattern as prior trim's `knownOverflowFixtures`. Removing
   an entry is the verification that a pin landed.

   **File-isolation rule (Codex round 1, axis 1.2 + 4.1):** the
   `knownUnpinnedAtoms` allowlist + the two new tests live in a
   dedicated file `internal/workflow/corpus_pin_density_test.go`,
   NOT in `corpus_coverage_test.go`. The test parses the AST of
   `scenarios_test.go` to build the pinned set; the allowlist's
   own atom IDs never enter the haystack because they are in a
   different file. (The previous round 0 sketch was self-counting.)

   Test source sketch (`internal/workflow/corpus_pin_density_test.go`):

   ```go
   package workflow

   import (
       "go/ast"
       "go/parser"
       "go/token"
       "strconv"
       "testing"
   )

   // knownUnpinnedAtoms is the Phase 0 starting allowlist — atoms that
   // currently lack a scenarios_test.go pin. Each Phase 8 commit adds a
   // pin AND removes the matching entry here. Ratchet: shrink-only
   // (enforced by TestCorpusCoverage_PinDensity_StillUnpinned). Phase 8
   // EXIT empties it.
   var knownUnpinnedAtoms = map[string]string{
       "bootstrap-adopt-discover": "(Phase 0): no scenarios_test pin.",
       // ... 67 more entries — generate from the §4.2 derivation.
   }

   // pinnedAtomIDs builds the set of atom IDs that scenarios_test.go
   // pins via requireAtomIDsContain or requireAtomIDsExact. Both
   // helpers have signature (t, label, matches, wantIDs ...string), so
   // string-literal args from index 3 onward are the pinned atom IDs.
   func pinnedAtomIDs(t *testing.T) map[string]bool {
       t.Helper()
       fset := token.NewFileSet()
       f, err := parser.ParseFile(fset, "scenarios_test.go", nil, parser.ParseComments)
       if err != nil {
           t.Fatalf("parse scenarios_test.go: %v", err)
       }
       pinned := make(map[string]bool)
       ast.Inspect(f, func(n ast.Node) bool {
           ce, ok := n.(*ast.CallExpr)
           if !ok {
               return true
           }
           ident, ok := ce.Fun.(*ast.Ident)
           if !ok {
               return true
           }
           if ident.Name != "requireAtomIDsContain" && ident.Name != "requireAtomIDsExact" {
               return true
           }
           if len(ce.Args) < 4 {
               return true
           }
           for _, arg := range ce.Args[3:] {
               bl, ok := arg.(*ast.BasicLit)
               if !ok || bl.Kind != token.STRING {
                   continue
               }
               s, err := strconv.Unquote(bl.Value)
               if err != nil {
                   continue
               }
               pinned[s] = true
           }
           return true
       })
       return pinned
   }

   // TestCorpusCoverage_PinDensity asserts every loaded atom is named
   // by a scenarios_test.go pin call UNLESS allowlisted. Allowlist
   // entries ratchet shrink-only via _StillUnpinned below.
   func TestCorpusCoverage_PinDensity(t *testing.T) {
       t.Parallel()
       corpus, err := LoadAtomCorpus()
       if err != nil {
           t.Fatalf("LoadAtomCorpus: %v", err)
       }
       pinned := pinnedAtomIDs(t)

       for _, atom := range corpus {
           if _, allowed := knownUnpinnedAtoms[atom.ID]; allowed {
               continue
           }
           if !pinned[atom.ID] {
               t.Errorf("atom %q has no scenarios_test.go pin "+
                   "(requireAtomIDsContain or requireAtomIDsExact); "+
                   "add a pin OR (last resort) allowlist via knownUnpinnedAtoms",
                   atom.ID)
           }
       }
   }

   // TestCorpusCoverage_PinDensity_StillUnpinned mirrors
   // TestCorpusCoverage_KnownOverflows_StillOverflow. Two checks:
   //   (a) stale-entry — every allowlist key MUST still exist in
   //       LoadAtomCorpus(); deleting an atom requires removing its
   //       allowlist row in the same commit.
   //   (b) ratchet — every allowlist entry MUST still be unpinned;
   //       adding a pin requires removing the allowlist row in the
   //       same commit.
   func TestCorpusCoverage_PinDensity_StillUnpinned(t *testing.T) {
       t.Parallel()
       if len(knownUnpinnedAtoms) == 0 {
           t.Skip("allowlist empty — Phase 8 done")
       }
       corpus, err := LoadAtomCorpus()
       if err != nil {
           t.Fatalf("LoadAtomCorpus: %v", err)
       }
       corpusIDs := make(map[string]bool, len(corpus))
       for _, a := range corpus {
           corpusIDs[a.ID] = true
       }
       pinned := pinnedAtomIDs(t)

       for id, rationale := range knownUnpinnedAtoms {
           if !corpusIDs[id] {
               t.Errorf("knownUnpinnedAtoms lists %q but no such atom "+
                   "exists — remove the stale entry (rationale was: %s)",
                   id, rationale)
               continue
           }
           if pinned[id] {
               t.Errorf("atom %q is now pinned in scenarios_test.go "+
                   "(rationale at acknowledgement: %s) — remove from "+
                   "knownUnpinnedAtoms in the same commit that added the pin",
                   id, rationale)
           }
       }
   }
   ```
4. **Composition-audit baseline.** Run §6.2 on FIVE fixtures:
   - `develop_first_deploy_standard_container` (heavy first-deploy)
   - `develop_first_deploy_implicit_webserver_standard` (heavy first-deploy + implicit-webserver)
   - `develop_first_deploy_two_runtime_pairs_standard` (multi-pair stretch)
   - `develop_push_dev_dev_container` (post-deploy edit-loop)
   - `develop_simple_deployed_container` (NEW from user-test 2026-04-26 — single simple-mode deployed service edit task)

   Commit baseline scores in `plans/audit-composition/baseline-scores.md`.
   Score the NEW `task-relevance` dimension (per §6.2 rubric) on
   each fixture — the user-test reported < 25 % task-relevance on
   the simple-deployed fixture, so a baseline score of 1 is expected.

   If the `develop_simple_deployed_container` fixture doesn't exist
   in `coverageFixtures()`, ADD it as part of Phase 0 calibration
   (analogous to how the prior trim added the
   `develop_first_deploy_two_runtime_pairs_standard` stretch fixture).
   Use this envelope shape (mirrors the existing
   `develop_push_dev_simple_container` fixture at
   `internal/workflow/corpus_coverage_test.go:502-516` —
   `Strategy: "push-dev"` is the documented simple-mode default):

   ```go
   {
       Name: "develop_simple_deployed_container",
       Envelope: StateEnvelope{
           Phase: PhaseDevelopActive,
           Environment: EnvContainer,
           Services: []ServiceSnapshot{{
               Hostname: "weatherdash", TypeVersion: "go@1.22",
               RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
               Strategy: "push-dev", Bootstrapped: true, Deployed: true,
           }},
       },
       MustContain: []string{
           // Phrase pins anchor the pre-hygiene fire-set. Each phrase
           // is sourced from an atom that SHOULD fire on this envelope
           // (verified by the Phase 0 fire-audit run before Phase 1).
           // Phase 7 axis-tightening will REMOVE atoms that fire here
           // but shouldn't (e.g. develop-mode-expansion fires on
           // simple-mode envelopes where mode-expansion is N/A); pins
           // below stay because they're anchored on the simple-mode
           // push-dev workflow / deploy / close atoms which DO belong.
           //
           // Sources:
           //   develop-push-dev-workflow-simple → "push-dev"
           //   develop-push-dev-deploy-container → "zerops_deploy"
           //   develop-close-push-dev-simple → "zerops_workflow action=\"close\""
           "push-dev",
           "zerops_deploy",
           `zerops_workflow action="close"`,
       },
   }
   ```

   **Pin sourcing (Codex round 1, axis 6.3 + 6.4):** an empty
   `MustContain` only proves at least one atom rendered, not that the
   intended pre-hygiene fire-set was preserved. The three phrases
   above are observable phrases that must be present on this envelope
   pre-hygiene; if Phase 7 axis-tightening would silently drop the
   workflow-simple / deploy-container / close-push-dev-simple atoms
   for this envelope, `TestCorpusCoverage_RoundTrip` fails. Verify
   each phrase appears in the rendered output BEFORE adding the
   fixture (run Synthesize once locally with the fixture).

**EXIT**:
- Both probes built + run, output committed to `plans/audit-composition/`.
- `TestCorpusCoverage_PinDensity` exists, `knownUnpinnedAtoms` map
  populated to current 68-atom state.
- Baseline composition scores committed.
- Full test suite green; no assertions changed semantics (only
  scaffolding + Logf + allowlist + docs).

### Phase 1 — Dead-atom sweep

**ENTRY**: Phase 0 EXIT satisfied. Fire-set matrix committed and
identifies ≥ 0 atoms with empty fire-set across every probed envelope.

**WORK-SCOPE**: lowest-risk content reduction — drop atoms that fire
on no realistic envelope.

1. Read `plans/audit-composition/fire-set-matrix.md` from Phase 0.
2. For each atom with **fire-set = ∅**: confirm via probe + scenarios
   test + COMPUTE-ENVELOPE WALK (see RISK below), then delete the
   atom.
3. For each atom with **fire-set = 1 fixture**: read the atom + the
   fixture; flag "merge candidate" for Phase 7 review (record in
   `plans/audit-composition/merge-candidates.md`; do NOT merge here).

**Codex round** before each delete: read-only adversarial review.
"This atom claims to fire on no envelope. Verify: (a) walk
ComputeEnvelope in `internal/workflow/compute_envelope.go` for every
plausible state — could a real user reach an envelope that satisfies
this atom's axes? (b) check git log for the atom's authoring intent
— was it created for a path that's since been removed? Cite file:line."

**RISK — fixture-only fire-set vs live envelope.** A coverageFixture
absence doesn't prove a live envelope can't fire the atom. The probe's
synthetic envelope generator (§6.3) covers Cartesian products of axis
values, but real ComputeEnvelope output may have constraints those
synthetic envelopes don't capture (e.g. specific `IdleScenario` only
emitted when X). Codex round MUST confirm by walking ComputeEnvelope,
not just by trusting the fire-set matrix.

**EXIT**:
- All atoms with fire-set = ∅ deleted (ratchet via fire-set re-run).
- `plans/audit-composition/merge-candidates.md` committed listing
  fire-set = 1 atoms for Phase 7 review.
- Test suite green; pin-density allowlist may have shrunk if a deleted
  atom was on it.

**Target**: 0-3 atoms deleted. Sweep is risk-mitigation, not byte-recovery.

### Phase 2 — Cross-atom dedup (axis C, axis D)

**ENTRY**: Phase 1 EXIT satisfied. Fire-set matrix reflects post-Phase-1
state.

**WORK-SCOPE**: highest-leverage cluster work. The 27-atom SSHFS-path
restatement is the headline target.

1. **SSHFS path semantics**: canonical home `develop-platform-rules-
   container`. For each of the 25 non-canonical atoms: confirm the
   atom's fire-set co-fires with platform-rules-container on at least
   one common envelope; replace the SSHFS prose with a one-line cross-
   link + `references-atoms`. For atoms that DON'T co-fire (e.g.
   bootstrap-* atoms — different phase), keep a TIGHT (~1 line)
   restatement and document why duplication is justified.
2. **Re-verify all axis C / axis D candidates from §4.3** — those are
   the 2026-04-26 snapshot; corpus may have moved. Use methodology
   from §6.3 of prior trim:
   ```bash
   grep -lE "<fact-pattern>" internal/content/atoms/*.md
   ```
3. **Per-dedup commit**: one commit per concept, with the fact-
   inventory form from §6.1.

**Codex round** before each canonical-home selection: "These N atoms
restate <fact>. Which is the topical / lowest-priority / broadest-
axis canonical home? Cite reasoning."

**EXIT**:
- All §4.3 candidates re-verified and acted on (or documented as
  "duplication justified by axis").
- `plans/audit-composition/dedup-log.md` committed listing every fact
  deduped + canonical home + non-canonical atoms updated.
- Probe re-run shows body-join recovery on at least 2 of the 4
  baseline fixtures.

**Target**: 3-6 KB body recovery across multiple fixtures.

### Phase 3 — Static-template + knowledge-guide moves (axis E)

**ENTRY**: Phase 2 EXIT satisfied. dedup-log records the canonical-home
choices.

**WORK-SCOPE**: drop atom-side content already delivered by
`claude_shared.md` or existing knowledge guides. Per-turn cost goes to
zero for these facts.

1. Read `internal/content/templates/claude_shared.md` end-to-end.
2. For each atom touched in Phase 2 (and the top-30 from §4.4): grep
   each load-bearing phrase against `claude_shared.md` and
   `internal/knowledge/guides/*.md`. Hit → axis E candidate.
3. Per-candidate Codex round (per memory rule "Codex pre-baked claims
   need grep before trust"): verify the static surface ACTUALLY
   contains the fact (don't trust prior plan §15.2 — those claims
   were factually wrong on 3 of 5 candidates).
4. **Don't add to `claude_shared.md`** — that's also per-turn-paid.
   New static facts go elsewhere or stay in atoms.

**EXIT**: every axis E candidate either dropped (with grep-confirmed
target surface citation in commit message) or rejected with reason.
Probe shows monotone or improved body-join.

**Target**: 1-3 KB body recovery.

### Phase 4 — General-knowledge tighten (axis F)

**ENTRY**: Phase 3 EXIT satisfied.

**WORK-SCOPE**: drop content the LLM already knows from training.
Subtler than axes C / E because the line between "general" and
"ZCP-specific" requires judgment.

1. For each top-30 atom: re-read the body asking "would Claude know
   this without ZCP context?". Yes → drop or tighten to ZCP-specific
   nuance.
2. Examples from prior trim: `npm install creates node_modules`
   (general — drop), `push-dev creates /var/www/.git false signal`
   (ZCP-specific — keep), `0.0.0.0 vs localhost` (general framing —
   drop; "L7 reverse-proxy needs 0.0.0.0" — keep).
3. Per-candidate Codex round: "Is this general programming knowledge
   or ZCP-specific? If unsure, keep."

**EXIT**: per-atom commit messages cite the kept ZCP-specific nuance
where general framing was dropped, so the rationale survives review.

**Target**: 1-2 KB body recovery.

### Phase 5 — Verifiable-at-runtime moves (axis G)

**ENTRY**: Phase 4 EXIT satisfied.

**WORK-SCOPE**: drop catalogs and field shapes the agent could fetch
via tool calls.

1. Survey atoms for: env-var catalogs (already mostly handled), port
   lists, log-shape examples, response-field enumerations.
2. For each: confirm the same data is reachable via `zerops_discover` /
   `zerops_logs` / `zerops_env` / `zerops_knowledge`. Replace atom
   prose with a one-liner naming the tool call.
3. Where applicable, add `references-fields` frontmatter so a future
   field rename breaks the build.

**EXIT**: every dropped catalog has a one-liner pointing at the tool
that returns the same data, so the agent has a path to the facts.

**Target**: 1-2 KB body recovery.

### Phase 6 — Per-atom prose tightening (axis B)

**ENTRY**: Phase 5 EXIT satisfied.

**WORK-SCOPE**: riskiest phase — prose rewrites can lose facts.
Sequenced last so prior phases have removed all reasonably-removable
content; what remains IS load-bearing and just needs density.

1. For each top-30 atom NOT yet at LEAN status: rewrite per §5 axis B
   recipe (tables / numbered lists / decision-tree triplets).
2. Per-atom Codex round: "Verify the rewrite preserves every load-
   bearing fact. List any fact missing or mutated."
3. Per-atom fact-inventory commit (§6.1).

**EXIT**: every rewritten atom has a committed fact-inventory + Codex
review notes. No silent fact loss.

**Target**: 2-4 KB body recovery.

### Phase 7 — Necessity rationalization (axis I + axis J + composition pass)

**ENTRY**: Phase 6 EXIT satisfied.
`plans/audit-composition/merge-candidates.md` (from Phase 1) lists the
single-fixture-fire-set atoms.

**WORK-SCOPE**: three sub-passes — axis tightening, marginal-atom
merges, aggregate composition re-score.

1. **Axis-tightness audit (axis J, NEW from user-test)**: for each
   atom that the Phase 0 baseline composition audit's `task-relevance`
   dimension scored ≤ 2 on at least one fixture: read the atom's
   frontmatter axes; identify the envelopes where it fires but is
   irrelevant; tighten axes (`runtimes:`, `modes:`, `deployStates:`,
   `envelopeDeployStates:`) to exclude those envelopes.

   Re-run §6.3 fire-set after tightening; confirm the atom is NOT
   dropped from envelopes where it WAS load-bearing (would be a
   regression). Per-atom Codex round per §10.1 Phase 7 PER-EDIT.

   Examples from user-test:
   - `develop-mode-expansion`: tighten to exclude `modes:[simple]`
     (mode expansion N/A on simple).
   - `develop-dev-server-triage`: tighten to exclude
     `runtimes:[implicit-webserver, static, managed]`.
   - First-deploy-* family: confirm `envelopeDeployStates:` axis
     properly excludes them when ALL services in envelope are
     deployed (not just the primary one).

2. **Marginal-atom merges (axis I)**: for each atom flagged in Phase
   1 step 3 (single-fixture fire-set): could it merge into another
   atom firing on the same fixture? If yes and the merged atom stays
   under 2 KB, merge. Decompose the merged atom's content via the
   §6.1 fact inventory.
   - **RISK CHECK BEFORE EACH MERGE**: confirm the source and target
     atoms have OVERLAPPING axis sets. Merging a `runtimes:[dynamic]`
     atom into a `runtimes:[]` (wildcard) atom changes axis-filtering —
     facts that only applied to dynamic now fire for static too. If
     axis sets disagree, do NOT merge; keep as a marginal atom.
3. **Re-run composition audit** (§6.2) on the four heavy-fire
   fixtures + the simple-mode-deployed user-test fixture (per §4.4
   addition). Compare to baseline scores. Coherence + density +
   task-relevance should improve; redundancy + coverage-gap should
   drop.
4. Commit composition-audit deltas as
   `plans/audit-composition/post-hygiene-scores.md`.

**EXIT**: composition scores documented; axis-tightening + merges
accompanied by Codex round confirming axis-filtering preserved AND
the user-test envelope's task-relevance score moved to ≥ 4.

**Target**: 1-2 atom files removed via merge; 5+ atoms axis-tightened;
aggregate composition + task-relevance improvement.

### Phase 8 — Pin-coverage closure + cleanup

**ENTRY**: Phase 7 EXIT satisfied.

**WORK-SCOPE**:

1. **Add MustContain pins** for every atom that survived without one.
   New `TestCorpusCoverage_PinDensity` allowlist (`knownUnpinnedAtoms`)
   should empty. Pin one load-bearing phrase per atom — short enough
   to be stable across edits, specific enough to catch fact loss.
   - **Brittleness mitigation**: pick phrases that contain ZCP-specific
     vocabulary (tool names, axis values, frontmatter keys) rather
     than generic English. A pin on "Run zerops_deploy" is more
     stable than a pin on "Make sure to deploy your code."
2. **Delete `cmd/atomsize_probe/main.go`** + `cmd/atom_fire_audit/`.
3. **Final Codex round**: full-diff adversarial review per prior trim
   plan §17.5 pattern.

**EXIT**:
- `knownUnpinnedAtoms` empty. `TestCorpusCoverage_PinDensity` enforces
  every atom has a pin going forward.
- Probe binaries deleted. Tree clean.
- Final Codex round documented; PLAN COMPLETE marker committed in
  `plans/audit-composition/final-review.md`.

## 8. Test guardrails

Augments the prior trim's §8 table.

| Test | What it guards | Failure mode |
|---|---|---|
| `TestCorpusCoverage_PinDensity` (NEW) | Every atom_id (filter to `internal/content/atoms/` only — recipe atoms out of scope per R6) is named as a wantID arg to `requireAtomIDsContain` / `requireAtomIDsExact` in `scenarios_test.go` (parsed via AST, not substring search — Codex round 1 fix). MustContain phrase pins are NOT counted. Allowlist `knownUnpinnedAtoms` (in `corpus_pin_density_test.go`, isolated from the AST haystack) carries entries the executor hasn't pinned yet; ratchet pattern from prior trim's `knownOverflowFixtures`. | An unpinned atom exists; either add a `requireAtomIDsContain` arg in scenarios_test.go OR remove from `knownUnpinnedAtoms` after pinning. |
| `TestCorpusCoverage_RoundTrip` | Existing pins (augmented in Phase 8). | A trim deleted a fact the agent needs. Restore or re-pin. |
| `TestCorpusCoverage_OutputUnderMCPCap` | 28 KB body-join cap (allowlist already empty post-prior-trim). | Don't grow the allowlist; trim further. |
| `TestSynthesize_*`, `TestAtom*`, scenarios | (Same as prior trim §8.) | (Same.) |
| `TestComposition_*` (NEW, optional) | Per-envelope composition score ≥ baseline. | Aggregate quality regressed. |

## 9. Acceptance criteria

> **Final ship-gate**: §15.3 G1-G8 are the *operationalized* form of
> these criteria. This section names the goals; §15.3 names the
> verifiable artifacts that prove them. A ship-claim without §15.3
> evidence is incomplete.

- All §8 tests green at every commit.
- `knownUnpinnedAtoms` map empty at end of Phase 8.
- Composition-audit scores (§6.2) on the four baseline envelopes
  improve OR stay flat on coherence + density; redundancy + coverage-
  gap strictly improve.
- Net byte recovery: target ~8-12 KB body across the four heavy-fire
  fixtures (rough; track per-phase). **Baseline reference**: post-prior-
  trim measurements at plan draft time —
  - `develop_first_deploy_standard_container`: 26145 B body / 28342 B wire
  - `develop_first_deploy_implicit_webserver_standard`: 27752 B / 30070 B
  - `develop_first_deploy_two_runtime_pairs_standard`: 28636 B / 31257 B
  - `develop_first_deploy_standard_single_service`: 26037 B / 28122 B

  The 8-12 KB target reads as ~30-45 % of remaining body across these
  four — defensible because: (a) ~82 % of the corpus is unaudited so
  the marginal-byte cost of unexamined dups is high, (b) the prior trim
  cut ~15 KB from a higher baseline so the marginal-byte cost of
  remaining waste is lower (we're past the easy wins), (c) target is
  body-join, not wire-frame; wire-frame additions from Phase 6
  rewrites typically save more than body-join reflects.
- All atoms in fire-set-matrix have non-empty fire-set (no dead atoms).
- `make lint-local` clean. `go test ./... -count=1 -race` clean.
- Commit messages cite atom IDs touched + bytes recovered + axis
  label(s) applied + fact-inventory inline.
- Final aggregate-coherence review committed in
  `plans/audit-composition/post-hygiene-scores.md`.

## 10. Codex collaboration protocol

Codex is a **partner**, not just a reviewer. The protocol is designed
around what Codex does best (corpus-wide analysis, fresh-eyes
adversarial review, large-context cross-correlation) vs what Claude
does best (per-edit orchestration, tool execution, stateful editing).
See §10.5 for the work-economics principles that drive round selection.

Round types:

- **CORPUS-SCAN** (high-leverage, run once per phase or even once per
  plan): big-picture multi-file analysis. E.g. "find every cross-
  atom dup in the corpus", "score the redundancy heat-map across
  all 79 atoms", "composition-audit all 4 baseline fixtures
  independently". Output is a structured artifact that drives many
  per-edit decisions downstream.
- **PRE-WORK** (one per phase): approach validation before substantial
  work. Asks Codex to find design holes BEFORE Claude commits to a
  path.
- **PER-EDIT** (gated, not mandatory): per-significant-change fact-
  preservation review. Used when the change risks dropping facts
  silently. SKIPPED for low-risk mechanical edits Claude can self-
  verify (e.g. axis switches that don't touch body content).
- **POST-WORK** (one per phase): "find what I missed" completeness
  validation after the phase's work commits. Different framing from
  PRE-WORK (gap-finding, not approach-validation).
- **FINAL-VERDICT** (one per plan): SHIP/NO-SHIP gate per §15.3 G7.

### 10.1 Per-phase round matrix

The matrix below names each round + WHO consumes the output (which
phase / which work unit / which tracker row). Output without a
consumer = wasted Codex effort and not in the matrix.

| Phase | Round type | Purpose | Output → consumed by |
|---|---|---|---|
| Phase 0 | CORPUS-SCAN | Pin-coverage gap derivation review: validate `TestCorpusCoverage_PinDensity` design + `knownUnpinnedAtoms` baseline list. Catch recipe-atom scope leakage (R6). | Phase 0 tracker: row per atom in initial allowlist. |
| Phase 0 | CORPUS-SCAN | Composition baseline scoring on the 4 heavy-fire fixtures per §6.2 rubric. Codex reads each rendered status output, scores 4 dimensions × 4 fixtures = 16 scores. | `plans/audit-composition/baseline-scores.md` (filled by Codex output verbatim). |
| Phase 0 | POST-WORK | Validate fire-set matrix: walk `ComputeEnvelope` for atoms reported as fire-set=∅ in §6.3 probe output. Confirm or reject "dead" status. | Phase 1 tracker: pre-populates "candidate state" column. |
| Phase 1 | PRE-WORK | "Of these N candidate dead atoms, which truly are dead vs which look-dead because the envelope generator missed a state?" | Per-delete decision in Phase 1 tracker. |
| Phase 1 | PER-EDIT | (Optional, only for atoms with non-trivial git-history archaeology) git-log walk: was the atom created for a path since removed? | Justification in tracker `notes` column. |
| Phase 2 | CORPUS-SCAN | Full-corpus cross-atom dup hunt. Codex reads ALL 79 atoms, identifies every fact appearing in 2+ atoms, ranks by recoverable bytes, suggests canonical home per dup. Output: a single structured deliverable. | `plans/audit-composition/dedup-candidates.md`. Phase 2 tracker rows derive from this artifact. |
| Phase 2 | PER-EDIT | (Only for canonical-home selection that has > 2 viable candidates) "Pick the topical/lowest-priority/broadest-axis home." | Tracker `notes` for that dup row. |
| Phase 2 | POST-WORK | "Find every cross-atom dup remaining post-Phase 2. Was any axis-justified dup incorrectly merged (introduced a regression)?" | Tracker re-validation; defers to Phase 8 if any found. |
| Phase 3 | CORPUS-SCAN | Read `claude_shared.md` + every `internal/knowledge/guides/*.md`; for each, list the facts it contains; cross-correlate against the corpus; output every (atom-fact, static-surface-fact) match pair. | `plans/audit-composition/axis-e-candidates.md`. |
| Phase 3 | PER-EDIT | (Per memory rule "Codex pre-baked claims need grep before trust") Per axis-E candidate: confirm with grep that the static surface ACTUALLY contains the fact. | Tracker `final state` column ("confirmed" / "rejected"). |
| Phase 4 | PRE-WORK | "For each top-30 atom, identify general-LLM-knowledge candidates. Mark each as DROP / KEEP-AS-ZCP-SPECIFIC / NUANCE-PRESERVE." | `plans/audit-composition/axis-f-candidates.md`; per-row Phase 4 tracker. |
| Phase 4 | POST-WORK | "Re-read every Phase 4 commit. Did any drop ZCP-specific nuance disguised as general knowledge?" | Tracker re-validation. |
| Phase 5 | CORPUS-SCAN | "Find every catalog / field-shape / port-list / response-enumeration in the corpus. Cross-reference against `zerops_*` tool surface to confirm fetchability." | `plans/audit-composition/axis-g-candidates.md`. |
| Phase 5 | POST-WORK | "Re-grep for catalogs/field-shapes still in atoms post-Phase 5. Any we missed?" | Tracker re-validation. |
| Phase 6 | PRE-WORK | "For each top-30 atom NOT-yet-LEAN, identify the densification path: TABLE / NUMBERED-LIST / DECISION-TREE / TIGHTEN-IN-PLACE / NO-CHANGE." | `plans/audit-composition/axis-b-candidates.md`. |
| Phase 6 | PER-EDIT | (MANDATORY — riskiest phase) Per-atom rewrite review. Codex reads the diff, lists every fact missing/mutated. | Fact inventory commit per §6.1 cites this round. |
| Phase 6 | POST-WORK | "Diff every Phase 6 atom edit cumulatively. Any atom whose post-edit body is missing a load-bearing fact from pre-edit body?" | Tracker re-validation. |
| Phase 7 | PER-EDIT | Per-merge axis-set overlap check (R4 mitigation). | Tracker `notes` ("axis-overlap-confirmed" / "merge-rejected"). |
| Phase 7 | CORPUS-SCAN | Composition cross-validation per §6.6 L4 — Codex independently scores the same 4 fixtures using the same rubric. | `plans/audit-composition/post-hygiene-scores-codex.md` (alongside the executor's `post-hygiene-scores.md`). Compare; ≥2 disagreement triggers rubric refinement. |
| Phase 8 | CORPUS-SCAN | Final adversarial diff sweep across the full plan range. | `plans/audit-composition/final-review.md`. |
| Phase 8 | FINAL-VERDICT | SHIP/NO-SHIP gate per §15.3 G7 contract from §10.3. | Determines whether plan is shippable. |

### 10.2 Per-round invariants (every Codex invocation)

- Cite `file:line` for every claim.
- Read-only (Codex makes no edits; that's the executor's job).
- Out of scope outside the plan's stated surface.
- "Skeptical framing — catch what I missed."
- For PRE-WORK: prompt asks Codex to verify the *approach*, not just
  validate the work-already-done.
- For PER-EDIT: prompt includes the diff or the proposed atom edit
  inline; Codex reads the change in context.
- For POST-WORK: prompt asks "what did the executor MISS?" — the
  framing forces gap-finding, not approval.
- For CORPUS-SCAN: prompt produces a STRUCTURED ARTIFACT (markdown
  table or list) that Claude commits to `plans/audit-composition/`
  and uses as the source of truth for downstream decisions. The
  output's value is in being CONSUMED, not just produced.

### 10.3 Final SHIP verdict (G7 in §15.3)

The final Codex round before declaring PLAN COMPLETE has a specific
contract:

```
Read all 9 phase trackers in plans/audit-composition/. Read every
commit in this plan's range (git log <plan-baseline>..HEAD). Read
the final composition scores. Read the L5 smoke-test output. Read
the eval-regression docs.

Verdict: SHIP / NO-SHIP / SHIP-WITH-NOTES
- SHIP: every G1-G8 in §15.3 is verifiably satisfied. No deferred
  followups OR all deferrals have documented justification.
- NO-SHIP: some G1-G8 fails. Name which.
- SHIP-WITH-NOTES: G1-G8 pass but you found a NEW concern not
  addressed by the plan. Describe.

If verdict is anything other than SHIP, the executor MUST address
before claiming PLAN COMPLETE.
```

A SHIP verdict is required for §15.3 G7 to be satisfied.

### 10.4 Memory rule (carry forward)

Codex's CURRENT-code citations with `file:line` are usually right;
pre-baked memory claims (like prior plan §15.2) are often wrong.
Verify every "X exists at Y" claim with grep before acting. The
auto-memory `feedback_codex_verify_specific_claims.md` codifies this
— ensure it's loaded (P6 in §17 prereq).

### 10.5 Work-economics principles (Claude × Codex efficiency)

The protocol's design constraint: **every Codex round must produce
output that gets consumed**. A round that runs without a downstream
consumer is wasted compute (Codex side) AND wasted attention (Claude
side reading the result with no decision driven by it).

#### Where Codex is highest-leverage
- **Corpus-wide pattern discovery.** Reading 79 atoms + 21 guides + 4
  themes + claude_shared.md and finding all (atom-fact, surface-
  fact) match pairs. Claude doing this manually = many small greps,
  high error rate, slow. Codex doing this once = one structured
  output that drives 20+ Phase 3 decisions.
- **Adversarial fresh-eyes review.** Claude's biases accumulate
  during a phase ("I just trimmed atom X, of course atom Y is fine
  too"). Codex starts each round with no investment in prior
  decisions — its NO-SHIP verdict is more credible than Claude's
  self-approval.
- **Cross-correlation across files.** "Find every atom that
  references `references-atoms: [develop-deploy-modes]` and verify
  the cross-link still resolves to a meaningful section after
  deploy-modes was rewritten." Codex reads N atoms in one pass.
- **Composition scoring with rubric.** Subjective scoring
  benefits from independent application of the rubric. Codex
  applies rubric to each fixture without remembering the executor's
  bias.

#### Where Codex is LOW-leverage (Claude does it directly)
- **Single-file mechanical edits.** "Change `deployStates:` to
  `envelopeDeployStates:`" — Claude can verify alone via test
  re-run. No Codex round needed.
- **Tool execution.** Running `go test`, `make lint-local`,
  `git status`. Codex doesn't help.
- **Probe re-measurement.** Building + running the probe binary +
  reading numeric output. Mechanical.
- **Tracker updates.** Filling rows in markdown tables.
  Bookkeeping, not analysis.
- **Routine grep verification of memory rule.** Single-line greps
  to verify Codex's prior claim. Codex grepping its own claim is a
  loop; Claude greps directly.

#### Round-skipping decision rule

Before invoking a Codex round, ask:

1. **Does this round produce a STRUCTURED ARTIFACT** that drives
   downstream decisions? (If not, skip.)
2. **Is there a CONSUMER** identified in §10.1 for this round's
   output? (If not, the round is decorative — skip.)
3. **Could Claude self-verify with a single grep / test re-run?**
   (If yes for the specific decision, skip the per-edit round —
   keep the corpus-scan rounds intact.)
4. **Has Codex already answered this question in a recent round?**
   (Don't re-ask the same question; cite the prior round's output.)

The matrix in §10.1 is the *upper bound* on rounds — fewer is fine
when 1-4 above justify skipping. The matrix in §10.1 is the *lower
bound* on rounds — never fewer than what §10.1 mandates for
corpus-scan and final-verdict rounds.

#### Pre-flight estimation

Total Codex rounds budgeted (matrix-implied):
- 5 CORPUS-SCAN rounds (one per major analysis surface).
- 4 PRE-WORK rounds (Phases 1, 4, 6 + Phase 0's design-review).
- ~30 PER-EDIT rounds (mostly Phase 6 prose tightening — riskiest).
- 6 POST-WORK rounds (one per Phase 0-5 + 7).
- 1 FINAL-VERDICT round.

Total: ~46 Codex invocations across ~74 atoms touched.
Average ~6 rounds per phase. Median round duration: 1-3 min for
PER-EDIT, 5-10 min for CORPUS-SCAN. Total Codex compute: ~3-5
hours across the plan execution.

If the executor finds Codex consistently producing rounds that
don't get consumed → the plan's protocol has gaps. Update §10.1
mid-execution rather than continuing to waste rounds. Document the
update in `plans/audit-composition/protocol-amendments.md`.

## 11. Out of scope

- `internal/recipe/` and `internal/recipe/atoms/*.md` — owned by
  recipe team; separate plan. The new `TestCorpusCoverage_PinDensity`
  filters its iteration to `internal/content/atoms/` ONLY (see R6 in
  §12).
- `internal/content/atoms/recipe-*.md` if any exist — same.
- `internal/content/templates/claude_*.md` — different lifecycle (per-
  session-boot delivery). Adding bytes here is an anti-pattern; only
  changing them when a corpus drop migrates a fact AWAY from per-turn.
- `internal/tools/*.go` tool descriptions — drift-tested separately
  (`description_drift_test.go`). Don't move bytes here.
- `docs/spec-*.md` — never delivered to LLM.

### Strategy-* / export.md scope rule (clarified)

`internal/content/atoms/strategy-*.md` (5 atoms, ~10 KB) and
`internal/content/atoms/export.md` (~7 KB single outlier) are **PARTIALLY
in scope**:

- **IN-SCOPE** for Phase 0 (fire-set matrix + pin-density test must
  cover them — they're real atoms).
- **IN-SCOPE** for Phase 1 (dead-atom sweep — a strategy or export
  atom firing on no envelope is still dead).
- **IN-SCOPE** for Phase 2 ONLY when a cross-atom dedup naturally
  touches them (e.g. a develop atom and a strategy atom both restate
  the same fact). The strategy atom IS edited in that case.
- **OUT-OF-SCOPE** for Phases 3-7 (no proactive trim of strategy or
  export atoms beyond what Phase 1-2 forced). They're lower-priority
  envelopes; their dedicated hygiene pass is a follow-up plan.

The "opportunistically" phrasing from earlier draft was ambiguous;
this rule replaces it. When in doubt: in scope for Phases 0-2, out of
scope for Phases 3-7.

### Idle-* atoms scope

`internal/content/atoms/idle-*.md` (6 atoms) ARE in scope. They're the
entry points to bootstrap and develop, fire on `Phase=idle`, and
their content directly drives the agent's first move per session.
Audit identical to bootstrap-* and develop-* atoms.

## 12. Anti-patterns + risks (don't do these / watch for these)

(Inherits all of prior trim §11 plus:)

### Anti-patterns

- **Don't bloat lean atoms.** Some bootstrap atoms are 500-700 B,
  doing one thing well. The hygiene pass shouldn't push them past 1
  KB by adding cross-links or premature abstractions.
- **Don't merge atoms across phases.** Splits + merges in same atom
  are confusing; respect the phasing.
- **Don't add a new atom file** unless it's a §H split (rules + cmds
  pair) sanctioned by the methodology. Net-new topics belong in their
  own plan.
- **Don't skip the fact inventory** (§6.1). Prior trim's §16.2 gap was
  exactly this.
- **Don't trust pre-baked Codex claims** without grep. The memory
  feedback `feedback_codex_verify_specific_claims.md` was forged in
  the prior trim — apply it religiously here.
- **Don't move bytes to `claude_shared.md` or tool descriptions.**
  Both are per-turn-paid; relocating doesn't reduce cost. Knowledge
  guides ARE the right destination.
- **Don't ratchet the body cap.** Stay at 28 KB; if a trim phase
  pushes a fixture over, fix it within the phase.

### Risks (named explicitly so each phase can mitigate)

- **R1 — MustContain brittleness from over-pinning.** Phase 8 adds 68+
  pins. If pins are too specific (full sentences), every prose tweak
  fails a test. Mitigation: pin SHORT, ZCP-VOCABULARY-DENSE phrases
  (tool names, axis values, frontmatter keys) rather than full
  sentences. Reviewable rule: "a pin should survive a re-wording of
  the surrounding sentence."
- **R2 — Composition-audit subjectivity.** §6.2 1-5 scoring is
  rubric-anchored but still judgment-based. Mitigation: two-executor
  cross-check on the four baseline fixtures before treating scores as
  ground truth. If scores diverge by ≥ 2 on any dimension, refine the
  rubric anchor before continuing.
- **R3 — Phase 1 dead-atom deletion vs live envelope.** Coverage
  fixtures + plausible-envelope generator may not capture every real
  ComputeEnvelope output. An atom flagged DEAD by the matrix could
  fire on a real user envelope. Mitigation: per-delete Codex round
  walks `compute_envelope.go` semantics, not just the matrix.
- **R4 — Phase 7 merges destroying axis-filtering.** Different atoms
  exist BECAUSE different envelopes need different content. Merging
  collapses the axis distinction. Mitigation: per-merge axis-set
  comparison (see Phase 7 RISK CHECK) — only merge when source and
  target have overlapping axes; otherwise keep separate.
- **R5 — Pin-density allowlist becoming permanent.** Phase 0 creates
  the allowlist with ~68 entries. Phase 8 must empty it. Without
  per-phase ratchet, the allowlist drifts into a documented-defect
  status quo (the prior trim's §11 anti-pattern verbatim).
  Mitigation: every commit that adds a pin removes the allowlist
  entry in the same commit (mirror prior trim's
  `knownOverflowFixtures` ratchet).
- **R6 — Recipe team's recipe atoms accidentally touched.** §11
  excludes `internal/recipe/`. The pin-density gate must NOT iterate
  over recipe atoms. Mitigation: the new `TestCorpusCoverage_PinDensity`
  scope-filters to `internal/content/atoms/` only.

## 13. First moves for the fresh instance

**Step 0 — prereq verification (MANDATORY)**: walk every row of §17
prereq checklist (P1-P11). If any fail, STOP and ask user. Do NOT
skip prereq verification "because the project looks set up" — this
catches missing memory, missing Codex tooling, missing VPN, stale
git state, AND ensures the executor knows what's missing before
sinking effort.

**Step 1 — read plan + sister context**:
1. This plan end-to-end (yes, all 1500+ lines).
2. `plans/atom-corpus-context-trim-2026-04-26.md` — sections §11
   (anti-patterns) + §17 (live-verification probe + memory rules).
3. `CLAUDE.md` + `CLAUDE.local.md` (project conventions + auth).
4. `~/.claude/projects/.../memory/MEMORY.md` (auto-memory) — confirm
   `feedback_codex_verify_specific_claims.md` is loaded.

**Step 2 — corpus baseline check**:
```bash
wc -c internal/content/atoms/*.md | sort -n | tail -30
ls internal/content/atoms/*.md | wc -l
git log --oneline -5
```
Run §4.2 pin-coverage-gap derivation; confirm "68 unpinned" still
holds (or update baseline).

If corpus has shifted by > 15 % or atom count moved by > 5, STOP and
ask the user — the corpus shifted since this plan was drafted; the
plan §4 baselines are stale.

**Step 3 — initialize tracker directory**:
```bash
mkdir -p plans/audit-composition
```
Create empty `phase-0-tracker.md` per §15.1 schema; commit
("scaffold: phase 0 tracker, hygiene plan execution begins").

**Step 4 — Phase 0 PRE-WORK Codex round**:
"Review the §8 test design + §6.3 fire-set generator approach. Catch
design holes." (per §10.1 Phase 0 row.)

**Step 5 — Begin Phase 0 work**: build probe + fire-audit + pin-
density test + baseline composition scores. Per §7 Phase 0 WORK-SCOPE.
Update `phase-0-tracker.md` per row as work lands.

**Step 6 — Phase 0 EXIT verification**:
- §15.2 EXIT check on tracker (no PENDING rows; commits cited).
- §7 Phase 0 EXIT criteria (all four sub-bullets).
- §6.6 L1+L2 verify gate green.
- Run Phase 0 POST-WORK Codex round per §10.1.

Only when Step 6 fully green: enter Phase 1.

**Pause/resume**: per §16.2. Trackers are the system of record;
fresh sessions resume from where last tracker leaves off.

## 15. Completeness machinery — per-phase trackers + ship-gate

The §7 EXIT criteria are necessary but verbal — a sloppy executor
could mark a phase complete without actually visiting every atom in
scope. The completeness machinery turns "did you visit every atom" /
"did every fact get classified" / "did every Codex round happen" into
*verifiable git-committed artifacts*.

### 15.1 Per-phase tracker file contract

Every phase commits a tracker file in `plans/audit-composition/`
named `phase-N-tracker.md` (where N is the phase number). The
tracker is the *single source of truth* for that phase's
completeness.

**Tracker schema** (markdown table, one row per work unit):

```markdown
# Phase N tracker — <phase title>

Started: <ISO date>
Closed: <ISO date | "open">

| # | work unit | initial state | final state | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | <atom-id or fact name> | <baseline> | <kept/modified/deleted/deferred> | <commit hash> | <APPROVE/NEEDS-REVISION/N-A> | <one-line rationale> |
```

The schema varies per phase but the contract is invariant:
- One row per work unit. Phase 1 = one row per atom in scope.
  Phase 2 = one row per dedup candidate fact. Phase 6 = one row per
  top-30 atom + axis label. Etc.
- Every row's "final state" is non-empty before phase EXIT.
- Every row that took action (modified / deleted) cites its commit hash.
- Every row whose action required Codex review cites the round outcome.

**Tracker rendering example** (Phase 1):

```markdown
# Phase 1 tracker — Dead-atom sweep

Started: 2026-04-NN
Closed: 2026-04-NN

| # | atom | fire-set size | final state | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | bootstrap-classic-plan-static | 3 (fixtures) | KEPT | – | – | not a dead-atom candidate |
| 2 | develop-ready-to-deploy | 0 | DELETED | abc1234 | APPROVE | confirmed via ComputeEnvelope walk; no plausible envelope satisfies axes |
| 3 | develop-mode-expansion | 1 (single fixture) | MARKED-MERGE | – | – | candidate for Phase 7 review (single-fixture fire-set) |
| ... | ... | ... | ... | ... | ... | ... |
```

A reviewer (or a later session of the same executor) reads the tracker
and instantly sees: "row 27 says state=PENDING and commit=– — this
phase is NOT actually closed."

### 15.2 EXIT enforcement via tracker

A phase is "shippable" only when the tracker satisfies:

1. Every row has a non-empty final state (no PENDING).
2. Every row that took action cites a commit hash (no orphan edits).
3. Every row whose phase required a Codex round cites the round
   outcome (no skipped reviews).
4. The "Closed:" date is filled in.

The next phase MAY NOT ENTER until prior phase's tracker satisfies
all four. The Phase N+1 ENTRY criteria from §7 should be read AFTER
verifying the tracker — they're additional, not replacements.

### 15.3 Final ship-gate procedure

Phase 8 EXIT is plan-level shippability. The executor MUST satisfy
all of these before declaring "PLAN COMPLETE":

| # | gate | how verified |
|---|---|---|
| G1 | All 9 phases (0-8) have closed trackers per §15.2. | Read every `phase-N-tracker.md`; confirm Closed: date + no PENDING rows. |
| G2 | `knownUnpinnedAtoms` map is empty. | grep `corpus_coverage_test.go`. |
| G3 | All 5 baseline composition fixtures (4 original + simple-deployed user-test) re-scored at Phase 7. Coherence + density + task-relevance non-decreasing; redundancy + coverage-gap strictly improving. The simple-deployed fixture's task-relevance must reach ≥ 4 (was baseline ~1). | Read `plans/audit-composition/post-hygiene-scores.md`; compare to baseline. |
| G4 | `make lint-local` clean. `go test ./... -count=1 -race` clean. | Run both, paste output in final commit message. |
| G5 | L5 live smoke test (§6.6) passes on idle + develop-active envelopes. | ssh zcp; issue MCP calls; assert wire-frame within probe ±1 + decoded text valid markdown. |
| G6 | Eval-scenario regression check (§6.7) — at least one scenario from `internal/eval/scenarios/` re-run; divergences documented. | `plans/audit-composition/eval-regression-*.md` exists. |
| G7 | Final Codex VERDICT round returns SHIP. | See §10 expanded protocol — Codex must explicitly write "VERDICT: SHIP" in its review. |
| G8 | Probe binaries deleted. | `ls cmd/` shows only `zcp/`. |

**Ship-gate failure mode**: if ANY G1-G8 fails, the plan is NOT
shippable. The executor either:
- Remediates within current phase's scope (re-open the relevant
  tracker, complete the missed work, re-run the gate).
- Documents a deferred follow-up in `plans/audit-composition/deferred-followups.md`
  with a justification for why this hygiene cycle ships without it
  (e.g. "G6 eval-scenario regression deferred — eval scenarios for
  this runtime aren't yet authored; tracking issue #NNN").

A self-claim of "SHIP COMPLETE" without G1-G8 evidence in the final
commit message is a process failure. Treat the prior trim's
self-congratulatory final commit as a CAUTIONARY example: it claimed
"plan complete" but later audit found §16.2 fact-inventory
requirement skipped + §17.7 wire-frame hard gate not landed. This
plan's ship-gate exists to make THAT not repeat.

## 16. Failure recovery + pause/resume

### 16.1 What to do when a phase mid-flight breaks

Failure modes during a phase:
- A Codex round returns NEEDS REVISION → ADDRESS the concerns;
  re-run the round; do NOT proceed until APPROVE.
- A test fails after a commit → STOP; root-cause; either revert the
  commit (`git reset --soft HEAD~1`, fix, re-commit) OR fix in a
  follow-up commit. NEVER force-skip.
- A merge breaks axis-filtering (R4) → revert the merge; document in
  the phase tracker as "MERGE-REJECTED with reason"; treat as kept.
- The executor finds an issue mid-phase the plan didn't anticipate
  → STOP; document in `plans/audit-composition/phase-N-issues.md`;
  run a Codex investigation round; decide path: fix in current
  phase / defer to follow-up plan / abandon work.

NEVER:
- Mark a phase complete with known-failing tests "to come back later".
- Commit work that fails the verify gate "because it'll pass after
  the next change".
- Skip a Codex round "because the change seems trivial".

### 16.2 Pause / resume protocol

Hygiene plan is multi-day work. The executor MAY pause at any point.
Resume protocol for a fresh Claude session:

1. Read this plan end-to-end.
2. `git log --oneline ^<plan-baseline-commit> HEAD` — see what's done.
3. `ls plans/audit-composition/` — read every tracker file; identify
   the highest phase with state ≠ "Closed".
4. If that phase's tracker has rows with PENDING state, RESUME there
   — do the pending rows; close the tracker; verify gate; commit.
5. If that phase's tracker is closed, ENTER the next phase per §7.
6. Do NOT skip earlier phases that look "obviously done" — verify
   the tracker exists + is closed first.

Trackers are the system of record. The executor cannot rely on
reading commit messages alone — `git log` shows commits but doesn't
show "did Codex round actually happen for atom X". Trackers do.

## 17. Self-containment prereq checklist

Before Phase 0 step 1, the executor MUST verify all of these are
satisfied. If any fail, STOP and ask the user.

| # | prereq | how verified | failure → |
|---|---|---|---|
| P1 | Working tree clean. | `git status` returns empty. | commit / stash before proceeding |
| P2 | Branch is current with origin. | `git fetch && git status` says "up to date". | `git pull --rebase origin main` |
| P3 | Prior trim plan exists at expected path. | `ls plans/atom-corpus-context-trim-2026-04-26.md` succeeds. | abort — sister plan missing means context drift |
| P4 | Prior trim's probe source reachable in git. | `git show c8d87406:cmd/atomsize_probe/main.go \| head -3` shows source. | check git history — commit hash may have changed; find new hash via `git log --oneline -- cmd/atomsize_probe/` |
| P5 | CLAUDE.md + CLAUDE.local.md readable. | `ls CLAUDE.md CLAUDE.local.md` succeeds. | abort — running without project conventions is unsafe |
| P6 | Auto-memory accessible. | Read `~/.claude/projects/.../memory/MEMORY.md`. Look for `feedback_codex_verify_specific_claims.md` entry. | If memory not loaded, the "verify Codex pre-baked claims" rule won't fire; cannot proceed safely |
| P7 | `codex:codex-rescue` subagent available. | Test with a tiny invocation: `Agent(subagent_type:"codex:codex-rescue", prompt:"echo hello")`. | abort — Codex collaboration protocol is mandatory; without it the plan is unsafe |
| P8 | VPN + SSH to eval-zcp container works. | `ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null zcp 'echo ok'`. | L5 smoke test impossible; document in §17 deferred followup OR fix VPN before starting |
| P9 | `make lint-local` works on baseline. | Run it; expect 0 issues. | If lint already failing, hygiene work conflates with lint debt; resolve first |
| P10 | `go test ./... -short -count=1 -race` green on baseline. | Run it. | Same as P9 — resolve before starting hygiene work |
| P11 | Empirical baseline §4 still matches reality. | Run §4.2 derivation script. Expect ~68 unpinned atoms ±5 and 79 atoms total ±5. | If counts diverge by > 15 %, corpus shifted; STOP, ask user, possibly re-baseline plan §4 |

Plan execution may begin only when P1-P11 all pass.

## 14. Provenance

Drafted 2026-04-26 after the `atom-corpus-context-trim-2026-04-26.md`
trim shipped. The trim closed the byte-cap gap on overflow envelopes
(allowlist empty) but left ~82 % of the corpus un-audited. User asked
for a comprehensive content-quality hygiene pass covering bootstrap +
develop + idle clusters: per-atom necessity, composition coherence,
redundancy elimination, prose density.

Drafted in collaboration with Codex (round #1 strategy input was
abandoned after stagnating 10+ minutes without content output —
provenance kept here as a known limitation; the Phase 0 fire-set
matrix in §6.3 + §7 captures the equivalent per-cluster analysis
at execution time, which is a stronger guarantee than inlining a
2026-04-26 Codex snapshot that would stale. Round #2 review of the
draft returned VERDICT: NEEDS REVISION on 5 specific concerns —
fire-audit source sketch, composition rubric anchors, phase entry/
exit criteria, expanded risks, strategy-* scope clarity — all 5
addressed before commit).

**Ultrathink revision** (2026-04-26 same day, post-initial-commit):
user audit identified 6 gaps: completeness-forcing mechanism,
multi-layer verification, Codex-as-validator (not just reviewer),
failure recovery, final ship-gate procedure, self-containment
prereq checklist. All 6 addressed via new §15-§17 plus expanded
§6.6 + §10. Plan grew from ~1130 to ~1500 lines; the additions are
not padding, they're the machinery that turns "the plan SHOULD be
followed" into "the plan ENFORCES being followed via committed
artifacts and Codex SHIP verdict gate."

This plan does NOT supersede `atom-corpus-context-trim-2026-04-26.md`;
the trim plan documents the byte-cap-chase work and ships a closed
allowlist. This plan is the follow-up content-quality work.
