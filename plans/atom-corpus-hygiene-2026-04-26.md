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
- **Atom necessity is unaudited.** Some atoms might fire on no realistic
  envelope (dead atoms). Some might be marginal — the agent could
  reasonably proceed without them. Some might be load-bearing but
  carry only one or two facts amid 1+ KB of prose padding.

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

## 5. Six-axis hygiene taxonomy (apply per atom)

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

**Re-run scoring** post-hygiene must show: coherence + density
non-decreasing; redundancy + coverage-gap strictly improving (or
flat-at-5).

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
- `Environment ∈ {container, local}`
- For phase=idle: `IdleScenario ∈ {empty, bootstrapped, adopt, incomplete, orphan}`
- For phase=bootstrap-active: every `(Route, Step)` pair from `BootstrapRoute × {discover, provision, generate, deploy, close}`
- For phase=develop-active: every `(Mode, Strategy, Trigger, RuntimeClass, DeployState, Bootstrapped)` tuple where `serviceSatisfiesAxes` accepts at least one envelope

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
func generatePlausibleEnvelopes() []plausibleEnvelope {
	var out []plausibleEnvelope

	// Idle envelopes — one per IdleScenario.
	for _, scen := range []workflow.IdleScenario{
		workflow.IdleEmpty, workflow.IdleBootstrapped, workflow.IdleAdopt,
		workflow.IdleIncomplete, workflow.IdleOrphan,
	} {
		out = append(out, plausibleEnvelope{
			key: "idle/" + string(scen),
			envelope: workflow.StateEnvelope{
				Phase: workflow.PhaseIdle, Environment: workflow.EnvContainer,
				IdleScenario: scen,
			},
		})
	}

	// Bootstrap-active — every (route, step) pair.
	for _, route := range []workflow.BootstrapRoute{
		workflow.BootstrapRouteRecipe, workflow.BootstrapRouteClassic,
		workflow.BootstrapRouteAdopt, workflow.BootstrapRouteResume,
	} {
		for _, step := range []string{"discover", "provision", "generate", "deploy", "close"} {
			out = append(out, plausibleEnvelope{
				key: "bootstrap/" + string(route) + "/" + step,
				envelope: workflow.StateEnvelope{
					Phase: workflow.PhaseBootstrapActive, Environment: workflow.EnvContainer,
					Bootstrap: &workflow.BootstrapSessionSummary{Route: route, Step: step},
				},
			})
		}
	}

	// Develop-active — Cartesian over (mode, strategy, runtime, deployState, env).
	envs := []workflow.Environment{workflow.EnvContainer, workflow.EnvLocal}
	modes := []topology.Mode{
		topology.ModeDev, topology.ModeStage, topology.ModeStandard,
		topology.ModeSimple, topology.ModeLocalStage, topology.ModeLocalOnly,
	}
	strategies := []topology.DeployStrategy{
		topology.StrategyPushDev, topology.StrategyPushGit,
		topology.StrategyManual, topology.StrategyUnset,
	}
	runtimes := []topology.RuntimeClass{
		topology.RuntimeDynamic, topology.RuntimeStatic,
		topology.RuntimeImplicitWeb, topology.RuntimeManaged,
	}
	deployStates := []bool{false, true}
	for _, env := range envs {
		for _, mode := range modes {
			for _, strat := range strategies {
				for _, rt := range runtimes {
					for _, deployed := range deployStates {
						out = append(out, plausibleEnvelope{
							key: fmt.Sprintf("develop/%s/%s/%s/%s/dep=%v",
								env, mode, strat, rt, deployed),
							envelope: workflow.StateEnvelope{
								Phase: workflow.PhaseDevelopActive, Environment: env,
								Services: []workflow.ServiceSnapshot{{
									Hostname: "appdev", TypeVersion: "nodejs@22",
									RuntimeClass: rt, Mode: mode, Strategy: strat,
									Bootstrapped: true, Deployed: deployed,
								}},
							},
						})
					}
				}
			}
		}
	}

	// Strategy-setup + export-active — small surfaces, one each per relevant axis.
	// (Add per executor judgment based on which atoms exist there.)

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
   asserts every atom_id from `LoadAtomCorpus()` is mentioned at
   least once in either `scenarios_test.go::wantIDs` OR
   `corpus_coverage_test.go::MustContain`. Currently 68 atoms fail
   this — allowlist the failures with a `knownUnpinnedAtoms` map
   following the same ratchet pattern as prior trim's
   `knownOverflowFixtures`. Removing an entry is the verification
   that a pin landed.

   Test source sketch (pin to `internal/workflow/corpus_coverage_test.go`):

   ```go
   // knownUnpinnedAtoms is the Phase 0 starting allowlist — atoms that
   // currently lack any MustContain or scenarios pin. Each Phase 8
   // commit adds a pin AND removes the matching entry here. Ratchet:
   // the map can only shrink. Phase 8 EXIT empties it.
   var knownUnpinnedAtoms = map[string]string{
       "bootstrap-adopt-discover":      "(Phase 0): no MustContain, no scenarios_test mention.",
       // ... 67 more entries — generate from the §4.2 derivation.
   }

   // TestCorpusCoverage_PinDensity asserts every loaded atom has at
   // least one pin (MustContain phrase OR scenarios_test atom-ID
   // mention) UNLESS it's in the knownUnpinnedAtoms allowlist. The
   // allowlist ratchets shrink-only via TestCorpusCoverage_PinDensity_AllowlistOnlyShrinks.
   func TestCorpusCoverage_PinDensity(t *testing.T) {
       t.Parallel()
       corpus, err := LoadAtomCorpus()
       if err != nil { t.Fatalf("LoadAtomCorpus: %v", err) }

       // Build the union of pinned IDs from both test files.
       scenariosBytes, err := os.ReadFile("scenarios_test.go")
       if err != nil { t.Fatalf("read scenarios_test.go: %v", err) }
       coverageBytes, err := os.ReadFile("corpus_coverage_test.go")
       if err != nil { t.Fatalf("read corpus_coverage_test.go: %v", err) }
       haystack := string(scenariosBytes) + string(coverageBytes)

       for _, atom := range corpus {
           // Only enforce on internal/content/atoms/ (R6 — recipe atoms out of scope).
           if _, allowlisted := knownUnpinnedAtoms[atom.ID]; allowlisted {
               continue
           }
           if !strings.Contains(haystack, atom.ID) {
               t.Errorf("atom %q has no MustContain or scenarios_test mention; either add a pin or allowlist it (last resort).",
                   atom.ID)
           }
       }
   }

   // TestCorpusCoverage_PinDensity_StillUnpinned mirrors
   // TestCorpusCoverage_KnownOverflows_StillOverflow — every entry in
   // knownUnpinnedAtoms must STILL be unpinned. Adding a pin AND
   // forgetting to remove the allowlist entry fails this test —
   // forces the cleanup.
   func TestCorpusCoverage_PinDensity_StillUnpinned(t *testing.T) {
       t.Parallel()
       if len(knownUnpinnedAtoms) == 0 {
           t.Skip("allowlist empty — Phase 8 done")
       }
       // Read both test files; for every allowlisted atom, assert
       // its ID does NOT appear in the haystack (still unpinned).
       // Same shape as KnownOverflows_StillOverflow.
   }
   ```
4. **Composition-audit baseline.** Run §6.2 on the four heavy-fire
   fixtures (`develop_first_deploy_standard_container`,
   `develop_first_deploy_implicit_webserver_standard`,
   `develop_first_deploy_two_runtime_pairs_standard`,
   `develop_push_dev_dev_container`). Commit baseline scores in
   `plans/audit-composition/baseline-scores.md`.

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

### Phase 7 — Necessity rationalization (axis I + composition pass)

**ENTRY**: Phase 6 EXIT satisfied.
`plans/audit-composition/merge-candidates.md` (from Phase 1) lists the
single-fixture-fire-set atoms.

**WORK-SCOPE**: fold marginal atoms into siblings; verify aggregate
coherence.

1. **Marginal-atom merges**: for each atom flagged in Phase 1 step 3
   (single-fixture fire-set): could it merge into another atom firing
   on the same fixture? If yes and the merged atom stays under 2 KB,
   merge. Decompose the merged atom's content via the §6.1 fact
   inventory.
   - **RISK CHECK BEFORE EACH MERGE**: confirm the source and target
     atoms have OVERLAPPING axis sets. Merging a `runtimes:[dynamic]`
     atom into a `runtimes:[]` (wildcard) atom changes axis-filtering —
     facts that only applied to dynamic now fire for static too. If
     axis sets disagree, do NOT merge; keep as a marginal atom.
2. **Re-run composition audit** (§6.2) on the four heavy-fire
   fixtures. Compare to baseline scores. Coherence + density should
   improve; redundancy + coverage-gap should drop.
3. Commit composition-audit deltas as
   `plans/audit-composition/post-hygiene-scores.md`.

**EXIT**: composition scores documented; merges accompanied by Codex
round confirming axis-filtering preserved.

**Target**: 1-2 atom files removed via merge; aggregate composition
quality improvement.

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
| `TestCorpusCoverage_PinDensity` (NEW) | Every atom_id (filter to `internal/content/atoms/` only — recipe atoms out of scope per R6) has at least one MustContain pin OR scenarios_test mention. Allowlist `knownUnpinnedAtoms` carries entries the executor hasn't pinned yet; ratchet pattern from prior trim's `knownOverflowFixtures`. | An unpinned atom exists; either add a pin or remove from `knownUnpinnedAtoms` after pinning. |
| `TestCorpusCoverage_RoundTrip` | Existing pins (augmented in Phase 8). | A trim deleted a fact the agent needs. Restore or re-pin. |
| `TestCorpusCoverage_OutputUnderMCPCap` | 28 KB body-join cap (allowlist already empty post-prior-trim). | Don't grow the allowlist; trim further. |
| `TestSynthesize_*`, `TestAtom*`, scenarios | (Same as prior trim §8.) | (Same.) |
| `TestComposition_*` (NEW, optional) | Per-envelope composition score ≥ baseline. | Aggregate quality regressed. |

## 9. Acceptance criteria

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

Mandatory per-phase invocations. Mirrors the prior trim's protocol but
expanded:

| Phase | Codex round purpose |
|---|---|
| Phase 0 | Verify the pin-coverage gap test design (false-positive risks). |
| Phase 1 | Per-delete: confirm fire-set is genuinely empty. |
| Phase 2 | Per-canonical-home selection: confirm choice is topically + scope-wise correct. |
| Phase 3 | Per axis-E candidate: confirm `claude_shared.md`/guide ACTUALLY contains the fact (memory rule: pre-baked claims need grep). |
| Phase 4 | Per-axis-F candidate: confirm "general LLM knowledge" framing isn't dropping ZCP-specific nuance. |
| Phase 5 | Per axis-G candidate: confirm the tool surface returns the data shape we're claiming. |
| Phase 6 | Per-atom rewrite: fact-preservation review. |
| Phase 7 | Per-merge: confirm merged atom stays coherent + under size soft-target. |
| Phase 8 | Final adversarial diff sweep. |

**Per-prompt invariants** (every Codex round):
- Cite file:line for every claim.
- Read-only.
- Out of scope outside the trim plan's surface.
- "Skeptical framing — catch what I missed."

**Memory rule** (carry forward): Codex's CURRENT-code citations with
file:line are usually right; pre-baked memory claims (like prior plan
§15.2) are often wrong. Verify every "X exists at Y" claim with grep
before acting.

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

1. Read this plan end-to-end.
2. Read `plans/atom-corpus-context-trim-2026-04-26.md` §11 (anti-
   patterns) + §17 (live-verification probe + memory rules).
3. Read `CLAUDE.md` + `CLAUDE.local.md` (project conventions + auth).
4. Pull current corpus state:
   ```bash
   wc -c internal/content/atoms/*.md | sort -n | tail -30
   ls internal/content/atoms/*.md | wc -l
   git log --oneline -5
   ```
5. Run §4.2 pin-coverage-gap derivation; confirm "68 unpinned" still
   holds (or update baseline).
6. Run Phase 0 Codex round: "Review the §8 test design + §6.3 fire-
   set generator approach. Catch design holes."
7. Start Phase 0. Build probe + fire-audit + pin-density test +
   baseline scores.

If something significantly diverges from the §4 baseline (corpus has
grown / shrunk by > 15 %, or atom count moved by > 5), STOP and ask
the user — the corpus shifted since this plan was drafted.

## 14. Provenance

Drafted 2026-04-26 after the `atom-corpus-context-trim-2026-04-26.md`
trim shipped. The trim closed the byte-cap gap on overflow envelopes
(allowlist empty) but left ~82 % of the corpus un-audited. User asked
for a comprehensive content-quality hygiene pass covering bootstrap +
develop + idle clusters: per-atom necessity, composition coherence,
redundancy elimination, prose density.

Drafted in collaboration with Codex (round #1: strategy input — see
session transcript for the prompt). The Codex round's per-cluster
redundancy maps and per-atom classification table were intended to be
inlined here, but Codex was still reading the 74-file scope at draft
time. The fresh-instance executor should run Phase 0 step 4 (baseline
composition audit) which captures the same per-cluster analysis from
ground truth at execution time — that's a stronger guarantee than
inlining 2026-04-26 Codex findings that may stale.

This plan does NOT supersede `atom-corpus-context-trim-2026-04-26.md`;
the trim plan documents the byte-cap-chase work and ships a closed
allowlist. This plan is the follow-up content-quality work.
