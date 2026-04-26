# Plan: Atom Corpus Context-Optimization — 2026-04-26

> **Reader contract.** This plan is self-contained. A Claude Code instance
> opens the repo, reads this file, and can execute end-to-end. No prior-
> conversation context is required. Cite this file by path when starting.

> **NOTE (2026-04-26 live verification).** §17 below carries the verified
> empirical numbers. This problem statement and §15.5 carried stale
> figures from a single-service measurement — the actual fixtures in the
> test are TWO-service shapes (`appdev` + `appstage`) and the per-service
> render duplication is the dominant overflow driver (~10 KB of the
> overflow). Corrected numbers and the resulting phasing change are in §17.

## 1. Problem

The MCP `tools/call` response carries the runtime guidance the LLM acts on
each turn. Claude Code's MCP STDIO runtime caps single tool responses at
~32 KB chars; **on overflow, Claude Code spills the response to a scratch
file** (`~/.claude/projects/.../tool-results/mcp-*.txt`) **and the agent
reads only the first ~3 KB** — losing 90 %+ of the guidance. This failure
mode is documented at `internal/workflow/dispatch_brief_envelope.go:8-22`
(the v35 incident retrospective). The status / develop-briefing pipeline in
this repo composes its guidance by joining matched atom bodies — and two
representative develop-active envelope shapes already overflow:

- **`develop_first_deploy_standard_container`** (nodejs@22 dev+stage pair,
  bootstrapped, never-deployed) — `Synthesize` matches **25 atoms**, joined
  body is **40 228 bytes**, MCP wire frame **43 256 bytes** (~10.5 KB
  over the 32 KB cap). Plan §4 originally cited 19 atoms / 29 988 B —
  that was a SINGLE-service measurement; the actual fixture
  (`corpus_coverage_test.go:270`) carries TWO services. See §17.
- **`develop_first_deploy_implicit_webserver_standard`** (php-nginx@8.4
  dev+stage pair, bootstrapped, never-deployed) — **27 atoms**,
  **43 447 bytes** body, **46 647 bytes** wire frame (~13.9 KB over).

Both shapes hit production (real users with standard-mode
first-deploy projects). Pinning test
`internal/workflow/corpus_coverage_test.go::TestCorpusCoverage_OutputUnderMCPCap`
explicitly **allowlists** these two fixtures via `knownOverflowFixtures`
(line 700) at the body-join metric (40 228 B / 43 447 B — these match
live measurement) — the gate enforces the 28 KB soft cap forward but
acknowledges these two as known overflows pending this trim plan. The
companion test `TestCorpusCoverage_KnownOverflows_StillOverflow`
ensures the allowlist can only shrink: when a trim brings either fixture
under the ceiling, the companion fails and forces removal from the
allowlist.

## 2. Goal

Trim the corpus so every fixture in `coverageFixtures()` stays under the
**28 KB soft cap** (4 KB margin below the 32 KB MCP cap), while preserving
every load-bearing fact the LLM acts on. Acceptance:

- `knownOverflowFixtures` map empties.
- `TestCorpusCoverage_OutputUnderMCPCap` passes for every fixture.
- `TestCorpusCoverage_KnownOverflows_StillOverflow` passes (vacuously, if
  the map is empty — it `t.Skip`s).
- All `MustContain` phrase assertions in `TestCorpusCoverage_RoundTrip`
  continue to hold (no load-bearing fact dropped).
- `TestSynthesize_*`, `TestCorpusCoverage_CompactionSafe`,
  `TestAtomAuthoringLint`, `TestAtomReferenceFieldIntegrity`,
  `TestAtomReferencesAtomsIntegrity` all stay green.
- `make lint-local` clean.
- A representative envelope ideally lands under 24 KB (8 KB margin)
  to absorb future feature growth.

## 3. Mental model — what travels to the LLM each turn

### 3.1 Pipeline

```
StateEnvelope ──► Synthesize(envelope, corpus) ──► []MatchedRender
                                                         │
                                                         ▼
                                                  RenderStatus(Response)
                                                         │
                                                         ▼
                                                  textResult(...) ──► MCP tool response
```

`StateEnvelope` carries `Phase`, `Environment`, `Services[]` (with per-
service `Mode`, `Strategy`, `Trigger`, `RuntimeClass`, `Status`,
`Bootstrapped`, `Deployed`, `StageHostname`), `WorkSession`, `Recipe`,
`Bootstrap`, `IdleScenario`. Each atom in the corpus declares an
`AxisVector` in YAML frontmatter naming which envelopes it should fire on.

`Synthesize` (in `internal/workflow/synthesize.go`):
1. Filter atoms by envelope-wide axes (`phases`, `environments`, `routes`,
   `steps`, `idleScenarios`).
2. Filter atoms with service-scoped axes (`modes`, `strategies`,
   `triggers`, `runtimes`, `deployStates`, `serviceStatuses`) by checking
   whether ANY service in the envelope satisfies EVERY declared axis
   (per-service conjunction).
3. Sort matches by `priority` ascending, then atom ID ascending (stable).
4. Per atom: build a per-render `strings.Replacer` from the matched
   service's `{hostname}`, `{stage-hostname}`, plus
   `env.Project.Name` → `{project-name}`. Reject any leftover
   `{placeholder}` not in the allowlist.
5. Return `[]MatchedRender{AtomID, Body, Service}`.

`RenderStatus` (in `internal/workflow/render.go`) joins bodies with
`\n\n---\n\n` separators inside a markdown skeleton (`## Status`, phase,
services, plan, guidance).

### 3.2 Corpus location and shape

- Atoms: `internal/content/atoms/*.md` (76 files as of this snapshot).
- Embedded via `//go:embed` in `internal/content/content.go`.
- Loaded once and cached: `workflow.LoadAtomCorpus()` in
  `internal/workflow/synthesize.go`.

### 3.3 What does NOT travel through atoms (out of scope here)

- Recipe authoring guidance (`internal/workflow/recipe_*.go` →
  `internal/content/workflows/recipe.md`).
- v3 recipe pipeline (`internal/recipe/`) — separate atom tree at
  `internal/recipe/atoms/`, do NOT touch.
- `zerops_guidance` topic registry — predicate-filtered topics, separate
  surface.
- CLAUDE.md template (`internal/content/templates/claude_{shared,
  container, local}.md`) — written into the project repo by `zcp init`,
  delivered statically not per-turn.
- Tool descriptions (`internal/tools/*.go::mcp.AddTool`) — read once at
  MCP init; governed by `description_drift_test.go`.
- Spec docs (`docs/spec-*.md`) — never delivered to the LLM.

## 4. Empirical baseline (snapshot 2026-04-26)

### 4.1 Corpus totals

```
76 atom files
117 394 bytes total
  3 132 lines total
```

### 4.2 Per-phase distribution (approximate; some atoms list multiple)

| Phase | Atoms |
|---|---|
| `develop-active` | 48 |
| `bootstrap-active` | 18 |
| `idle` | 5 |
| `strategy-setup` | 5 |
| `develop-closed-auto` | 2 |
| `export-active` | 1 |
| `recipe-active` | 0 *(out of scope)* |

### 4.3 Top-15 largest atoms by bytes

```
2067  develop-first-deploy-asset-pipeline-container.md
2208  develop-first-deploy-scaffold-yaml.md
2303  develop-deploy-modes.md
2328  develop-dev-server-triage.md
2357  develop-dynamic-runtime-start-container.md
2364  bootstrap-provision-rules.md
2387  develop-platform-rules-container.md
2667  develop-first-deploy-env-vars.md
2713  bootstrap-route-options.md
2749  develop-platform-rules-local.md
2926  bootstrap-env-var-discovery.md
3051  develop-first-deploy-write-app.md
3506  develop-verify-matrix.md
3844  strategy-push-git-trigger-actions.md
6983  export.md  ← outlier; export-active phase only, never co-fires with develop-active
```

### 4.4 The two overflow envelopes — atoms fired (VERIFIED 2026-04-26)

The earlier listings here covered a single-service envelope. The actual
test fixtures (`corpus_coverage_test.go:270, :301`) carry TWO services
each (`appdev` + `appstage`). Per-service-axis atoms render ONCE PER
MATCHING SERVICE — six atoms render twice. The `*` marks per-service
duplication.

#### `first_deploy_standard_container` (40 228 B joined, 25 atoms)

```
   3398  develop-verify-matrix                          (envelope)
*  2844  develop-first-deploy-write-app                 appstage
*  2834  develop-first-deploy-write-app                 appdev
*  2487  develop-first-deploy-env-vars                  appstage
*  2483  develop-first-deploy-env-vars                  appdev
   2177  develop-deploy-modes                           (envelope)
   1930  develop-platform-rules-container               (envelope)
*  1877  develop-first-deploy-scaffold-yaml             appstage
*  1875  develop-first-deploy-scaffold-yaml             appdev
   1810  develop-dynamic-runtime-start-container        appdev
   1747  develop-api-error-meta                         (envelope)
   1567  develop-http-diagnostic                        (envelope)
   1251  develop-deploy-files-self-deploy               (envelope)
   1246  develop-platform-rules-common                  (envelope)
   1212  develop-knowledge-pointers                     (envelope)
   1184  develop-env-var-channels                       (envelope)
*  1122  develop-first-deploy-intro                     appdev
*  1122  develop-first-deploy-intro                     appstage
*  1042  develop-first-deploy-verify                    appstage
*  1040  develop-first-deploy-verify                    appdev
    970  develop-auto-close-semantics                   (envelope)
    799  develop-change-drives-deploy                   (envelope)
*   795  develop-first-deploy-execute                   appstage
*   793  develop-first-deploy-execute                   appdev
    455  develop-first-deploy-promote-stage             appdev
```

#### `first_deploy_implicit_webserver_standard` (43 447 B joined, 27 atoms)

```
   3398  develop-verify-matrix                          (envelope)
*  2844  develop-first-deploy-write-app                 appstage
*  2834  develop-first-deploy-write-app                 appdev
*  2487  develop-first-deploy-env-vars                  appstage
*  2483  develop-first-deploy-env-vars                  appdev
   2177  develop-deploy-modes                           (envelope)
   1930  develop-platform-rules-container               (envelope)
*  1877  develop-first-deploy-scaffold-yaml             appstage
*  1875  develop-first-deploy-scaffold-yaml             appdev
   1773  develop-first-deploy-asset-pipeline-container  appdev
   1747  develop-api-error-meta                         (envelope)
*  1624  develop-implicit-webserver                     appstage
*  1618  develop-implicit-webserver                     appdev
   1567  develop-http-diagnostic                        (envelope)
   1251  develop-deploy-files-self-deploy               (envelope)
   1246  develop-platform-rules-common                  (envelope)
   1212  develop-knowledge-pointers                     (envelope)
   1184  develop-env-var-channels                       (envelope)
*  1122  develop-first-deploy-intro                     appdev
*  1122  develop-first-deploy-intro                     appstage
*  1042  develop-first-deploy-verify                    appstage
*  1040  develop-first-deploy-verify                    appdev
    970  develop-auto-close-semantics                   (envelope)
    799  develop-change-drives-deploy                   (envelope)
*   795  develop-first-deploy-execute                   appstage
*   793  develop-first-deploy-execute                   appdev
    455  develop-first-deploy-promote-stage             appdev
```

**Per-service duplication aggregate** (one of two paths to fix; see §17.3):

| Atom (renders 2×) | Per-render | 2× total | % of `standard` overflow |
|---|---|---|---|
| `develop-first-deploy-write-app` | 2 834 B | 5 668 B | 14 % |
| `develop-first-deploy-env-vars` | 2 483 B | 4 970 B | 12 % |
| `develop-first-deploy-scaffold-yaml` | 1 875 B | 3 752 B | 9 % |
| `develop-first-deploy-intro` | 1 122 B | 2 244 B | 6 % |
| `develop-first-deploy-verify` | 1 040 B | 2 080 B | 5 % |
| `develop-first-deploy-execute` | 793 B | 1 586 B | 4 % |
| **subtotal** | | **20 300 B** | **50 %** |

If `Synthesize` rendered envelope-scoped sections once and per-service
sections per host, **collapsing these six to one render each (with
`{hostname}` replaced by `appdev|appstage` enumeration) recovers ~10 KB
without trimming a single byte of guidance**. See §17.3 for the cost-
benefit and the three implementation options.

**Both envelopes share 23 of their atom-renders.** Trimming any shared
atom helps both. Required cut to clear cap **with margin** (target ≤ 24 KB
joined body, ~28 KB wire frame): ~16 KB from `standard_container`,
~19.5 KB from `implicit_webserver`. Per-service collapse (option A in
§17.3) gets >50 % of the standard-fixture cut for free.

## 5. Four-axis trim taxonomy

These are the four lenses to apply to every candidate trim. A finding
usually fits one but may straddle two.

### Axis 1 — Content that shouldn't exist as atoms at all

Drop entirely. Criteria:

- **Already in CLAUDE.md template.** `claude_shared.md` (52 lines) ships
  in every project repo on `zcp init` — facts there are delivered
  statically and don't need atom restating.
- **General-knowledge.** What HTTP 200 means, how `npm install` works,
  what `0.0.0.0` binds to. The LLM's training carries this; restating
  burns bytes for nothing.
- **Verifiable at runtime via tool call.** Listing scaffold structure
  ("the tree looks like..."), enumerating env-var keys (`zerops_discover
  includeEnvs=true` already returns them), reading file contents
  (`Read /var/www/.../zerops.yaml`). If the agent could just call a
  read-only tool, the atom is teaching the wrong layer.

### Axis 2 — Per-atom prose verbosity

Shrink in place. Criteria:

- **Setup / preamble paragraphs** that don't carry an action ("In container
  env, the LLM operates against a Zerops-managed runtime. The mount is
  at..."). The agent reads this every turn for no decision benefit.
- **Worked examples beyond N=1** when the rule is the same. "One-shot
  commands over SSH" listing artisan + npm install + curl localhost —
  the rule fires from one example.
- **Defensive prose.** "note that...", "remember that...", "important:",
  "be careful to...", "do NOT forget" — emphasis the LLM doesn't act on.
- **Inverse-restatement pairs.** "Edit on the mount" + "Never `ssh
  hostname cat`" — the negative form repeats the positive.
- **Sentence-fragment headings** under bullet headings. Bullet → sentence
  → another bullet → "this means..." — the explanatory sentence is often
  the bullet again.

### Axis 3 — LLM-optimization rewrite (preserve facts, denser form)

Re-shape the same content. Criteria:

- **Tabular data in prose.** Mode × runtime × env matrices are usually
  3-5 rows; a markdown table is denser than 3-5 paragraphs.
- **Sequential steps in prose.** "First do X, then Y, finally Z" → numbered
  list (`1. X` etc.) is half the bytes for the same instruction.
- **Branching logic in prose.** "If A, then X; otherwise if B, then Y;
  otherwise Z" → decision tree as a 3-line structure.
- **Long inline code blocks for a single command.** A 10-line example
  for one `zerops_deploy` call is rarely necessary; a one-line code
  fence with `targetService=...` plus the rule is usually enough.

### Axis 4 — Cross-atom duplicates

Pick a canonical home, cross-link. Criteria for "duplicate":

- The fact appears verbatim or near-verbatim in 2+ atoms.
- The fact is independent of any axis the duplicating atoms differ on
  (e.g. "`deploy = new container`" doesn't depend on mode / strategy /
  runtime — it's a platform invariant).
- One of the atoms could replace its restatement with a one-liner +
  `references-atoms` cross-link.

Suspected duplicates (verify with the methodology in §6.3):

- "`deploy = new container`, only `deployFiles` persists" — likely in
  `develop-platform-rules-common`, `develop-deploy-files-self-deploy`,
  `develop-first-deploy-scaffold-yaml`.
- "`sudo apk add` / `sudo apt-get install` for `prepareCommands`" —
  likely in `develop-platform-rules-container` and
  `develop-first-deploy-write-app`.
- SSHFS mount path semantics (`/var/www/{hostname}/`) — likely in
  `develop-platform-rules-container` and several `develop-first-deploy-*`
  atoms.
- HTTP probe / `curl localhost:{port}` examples — likely in
  `develop-http-diagnostic` and `develop-verify-matrix`.
- `${hostname_VARNAME}` env-ref syntax — likely in
  `develop-env-var-channels` and `develop-first-deploy-env-vars`.

## 6. Methodology

### 6.1 Re-measure baseline before starting

```bash
# Per-file sizes, sorted, top-15.
wc -c internal/content/atoms/*.md | sort -n | tail -16 | head -15

# Corpus totals.
wc -c internal/content/atoms/*.md | tail -1

# Per-phase counts (approximate — atoms can list multiple phases).
for ph in idle bootstrap-active develop-active develop-closed-auto recipe-active strategy-setup export-active; do
  printf "%-22s %d\n" "$ph" "$(grep -l "^phases:.*${ph}" internal/content/atoms/*.md 2>/dev/null | wc -l)"
done

# Per-envelope joined size for every coverage fixture (uses the size-gate
# test instrumentation; no need to write a probe).
go test ./internal/workflow/ -run TestCorpusCoverage_OutputUnderMCPCap -count=1 -v 2>&1 | grep -E "fixture.*bytes"
```

### 6.2 List which atoms fire for an overflow envelope

Add a temporary `cmd/atomsize_probe/main.go` (delete after measurement; it
references `internal/` packages so it must live under `cmd/`). **Build in
a git worktree to avoid colliding with parallel work in
`internal/workflow/`**:

```bash
git worktree add ../zcp-trim-verify HEAD
cd ../zcp-trim-verify
mkdir -p cmd/atomsize_probe
# write main.go below
go build -o /tmp/atomsize_probe ./cmd/atomsize_probe/
/tmp/atomsize_probe
cd - && git worktree remove ../zcp-trim-verify --force
```

```go
package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func main() {
	corpus, _ := workflow.LoadAtomCorpus()
	// Two-service envelope — matches the actual fixture in
	// corpus_coverage_test.go:270 (NOT a single-service shape;
	// see §17.1 for why this matters).
	env := workflow.StateEnvelope{
		Phase: workflow.PhaseDevelopActive, Environment: workflow.EnvContainer,
		Services: []workflow.ServiceSnapshot{
			{Hostname: "appdev", TypeVersion: "nodejs@22",
				RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
				StageHostname: "appstage", Strategy: topology.StrategyUnset,
				Bootstrapped: true, Deployed: false},
			{Hostname: "appstage", TypeVersion: "nodejs@22",
				RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
				Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
		},
	}
	bodies, _ := workflow.SynthesizeBodies(env, corpus)
	joined := strings.Join(bodies, "\n\n---\n\n")
	rendered := workflow.RenderStatus(workflow.Response{Envelope: env, Guidance: bodies})
	matches, _ := workflow.Synthesize(env, corpus)
	type e struct{ id, host string; size int }
	var es []e
	for _, m := range matches {
		host := ""
		if m.Service != nil { host = m.Service.Hostname }
		es = append(es, e{m.AtomID, host, len(m.Body)})
	}
	sort.Slice(es, func(i, j int) bool { return es[i].size > es[j].size })
	fmt.Printf("%d atom-renders\n", len(matches))
	fmt.Printf("synthesize_bodies_join: %d B (corpus_coverage_test metric)\n", len(joined))
	fmt.Printf("render_status_markdown: %d B (text field on wire)\n", len(rendered))
	fmt.Printf("approx_wire_frame:      %d B (add ~1.4 KB for JSON-RPC envelope)\n", len(rendered)+1400)
	fmt.Printf("per-atom (host shows duplication):\n")
	for _, x := range es { fmt.Printf("  %5d  %-50s  %s\n", x.size, x.id, x.host) }
}
```

For the precise wire-frame measurement (not just approximation), see the
full probe in §17.5.

### 6.3 Find duplicates

Pick a candidate fact, grep across atoms:

```bash
# "deploy = new container" — likely a platform invariant duplicated.
grep -lE "new container|replaces the container|deployFiles.*persists" \
  internal/content/atoms/*.md

# Sudo for prepareCommands.
grep -lE "sudo apk|sudo apt-get|prepareCommands.*sudo" \
  internal/content/atoms/*.md

# SSHFS mount path semantics.
grep -lE "/var/www/\{hostname\}|SSHFS mount" internal/content/atoms/*.md

# Env-ref syntax.
grep -lE '\$\{[a-z]+_[A-Z]+|hostname_VARNAME' internal/content/atoms/*.md
```

A fact in ≥2 atoms with no axis-justified reason is a duplicate target.

### 6.4 Verify a trim is safe

After every individual atom edit:

```bash
go test ./internal/workflow/ -run "TestCorpusCoverage|TestSynthesize|TestAtom" -count=1
go test ./internal/content/ -count=1                # atom_lint covers prose rules
make lint-local                                     # atom-tree gates + golangci
```

The fast inner loop. Full `go test ./... -short -count=1` between phases.

## 7. Phased execution

> **REVISED 2026-04-26.** Original ordering put Axis 4 (cross-atom dups)
> first because it appeared to be the highest leverage. Live verification
> (§17) showed per-service render duplication is actually the dominant
> driver (~50 % of overflow). The new Phase 1 collapses that duplication
> via Option B from §17.3. The original Axis 4 / Axis 2 / Axis 3 / Axis 1
> phases follow as Phases 2-5. The historical ordering is preserved in
> the §17.8 changelog for context.

Each phase ships independently; verify gate between every phase. No
half-removed atoms (per CLAUDE.md "Phased refactors — verify each phase
before continuing; no half-finished states"). After every phase, re-run
the §17.5 probe to confirm wire-frame is dropping toward target.

### Phase 0 — Calibration (no tree-breaking)

Establish the metrics infrastructure before touching content. See §17.7
for full rationale.

1. **0a — Wire-frame info log.** Add a `t.Logf("fixture %q wire frame: %d B", name, size)` call to `TestCorpusCoverage_OutputUnderMCPCap` per fixture (NO assertion). Computes size from `RenderStatus(...)` + the JSON-RPC envelope-shape literal from §17.5. Lets future phases watch the trend.
2. **0b — Allowlist entries carry both metrics.** Extend `knownOverflowFixtures` rationale strings to record both `bodies_join` and `mcp_wire_jsonrpc_frame` numbers. Today they only carry body-join.
3. **0c — Multi-pair stretch fixture.** Add `develop_first_deploy_two_runtime_pairs_standard` to `developCoverageFixtures()` (4 services: `appdev/appstage` + `apidev/apistage`, all nodejs@22 standard, never-deployed). **Measure its body-join with the §17.5 probe FIRST**; only add to `knownOverflowFixtures` if it actually exceeds the 28 KB body-join cap (companion `KnownOverflows_StillOverflow:795-813` reads body-join, not wire-frame; allowlisting a fixture that fits would fail the companion immediately). Per-service-duplication scaling predicts > 60 KB body-join; confirm before allowlisting.

**Verify**: full test suite stays green. No assertions added.

### Phase 1 — Per-service render-duplication kill (Option B from §17.3)

The dominant lever. Collapses ~10-12 KB by splitting six duplicating
atoms along the natural rules-vs-commands seam.

For each of `develop-first-deploy-write-app`, `-env-vars`,
`-scaffold-yaml`, `-intro`, `-verify`, `-execute`:

1. **Read the atom and its scenario pins** (`scenarios_test.go`,
   `corpus_coverage_test.go::MustContain`).
2. **Identify the rules-vs-commands seam.** Rules: SSHFS semantics,
   bind-0.0.0.0 reasoning, env-var conventions, framework defaults.
   Commands: `ssh {hostname} "..."`, `zerops_deploy
   targetService="{hostname}"`, `zerops_verify
   serviceHostname="{hostname}"`.
3. **Split into two atoms**:
   - `<original-id>-rules` — envelope-scoped (no service axis;
     `phases: [develop-active]`, `deployStates: [never-deployed]`).
     Renders 1×. Carries the rules/concepts.
   - `<original-id>-cmds` — per-service-scoped (keeps the original
     service axes). Renders per matching service. Small body, just the
     imperative tool-call lines with `{hostname}` substitution.
4. **Update `references-atoms`** in any atom pointing at the original ID
   to point at the appropriate half. Run `TestAtomReferencesAtomsIntegrity`.
5. **Update `MustContain`** in coverage fixtures so each pinned phrase
   asserts against whichever half now carries it.
6. **Per-atom fact inventory** (per §16.2): for each fact in the
   original atom, label KEEP (with the new `MustContain` pin), MOVE
   (to which split half), or DROP (with §5 axis justification).
   Commit the inventory in the same commit as the split.
7. **Run §6.4 verify gate after each atom split**: `go test
   ./internal/workflow/ -run "TestCorpusCoverage|TestSynthesize|TestAtom"
   -count=1` plus `make lint-local`.

**Target: 10-12 KB recovered across both overflow fixtures.**

**Fallback gate**: after all six splits, re-measure with §17.5 probe.
If the standard fixture's wire-frame is still > 32 KB, escalate to
Option A (structural `Synthesize` change + multi-host placeholder
grammar). Pivot decision happens HERE, not earlier — Option B is the
default because it's corpus-only.

### Phase 2 — Cross-atom duplicates (Axis 4) — original Phase 1

Now that per-service duplication is gone, true cross-atom dups are the
next highest-leverage cut. Single-digit KB recovery but cheap.

1. Use §6.3 grep methodology to enumerate facts appearing in ≥2 atoms.
   **Re-verify each candidate against the current corpus** — original
   §15.3 spot-checks were inaccurate (see §17.8 caveat).
2. Verified candidates as of 2026-04-26 (re-verify before acting):
   - SSHFS mount path `/var/www/{hostname}/` — in 18 atoms.
     Canonical home: `develop-platform-rules-container` (already owns
     "platform invariants - container").
   - `deploy = new container, deployFiles persists` — in
     `develop-platform-rules-common` and `develop-push-dev-workflow-dev`.
     Canonical home: `develop-platform-rules-common`.
   - `${hostname_VARNAME}` env-ref syntax — in
     `develop-first-deploy-scaffold-yaml`, `develop-first-deploy-verify`,
     `export.md`. Canonical home: `develop-env-var-channels` (more
     topical).
3. For each duplicate: pick canonical home (lowest priority, broadest
   axis, or the topical atom). Trim from non-canonical sites; replace
   with a one-liner + `references-atoms: [canonical-id]`.
4. After each duplicate removed: run §6.4 verify.

**Target: 2-4 KB additional recovery.**

### Phase 3 — LLM-optimization rewrites (Axis 3) — original Phase 3

Identify 2-3 atoms whose content is genuinely tabular or sequential.
Rewrite in dense form. Verified candidates:

- **`develop-verify-matrix`** (3 398 B, envelope-scoped, fires 1×) —
  embeds a literal `Agent(model="sonnet", prompt="""...""")` block at
  lines 33-73 (~1 500 B of meta-instructions for a sub-agent). Move
  via the **generalized** dispatch-brief-atom pattern per §17.4 — NOT
  via direct reuse (recipe-bespoke namespace + scoping). Per-turn
  payload then ships only the protocol summary; the full embedded
  prompt is fetched on-demand. ~1 500 B saved per turn.
- **`develop-deploy-modes`** — mode-by-mode matrix in prose; markdown
  table is denser. ~600 B.
- **`develop-platform-rules-container`** — bullet lists where each
  bullet is a paragraph; tighten bullet-to-rule ratio. ~700 B.

**Target: 2-4 KB recovered.**

### Phase 4 — Atoms that shouldn't exist (Axis 1) — original Phase 4

Most invasive; do last when prior phases have given the most signal.

For each candidate atom or section, check:
1. Does the same fact live in `claude_shared.md`?
2. Is it general LLM knowledge (HTTP basics, npm)?
3. Could the agent verify by tool call?

**CAVEAT**: original §15.2 spot-checks were inaccurate — `apiMeta` is
NOT in `claude_shared.md` (verified `grep -c "apiMeta" claude_shared.md`
= 0). Re-verify each candidate against current template + atom
contents, do NOT trust the §15.2 list as a vetted target list.

If yes to any check: delete the atom (or the section). Update
`coverageFixtures` `MustContain` if the dropped phrase was pinned.
Update any `references-atoms` pointers.

**Target: 1-3 KB recovered.**

### Phase 5 — Allowlist removal

Once both `develop_first_deploy_standard_container` and
`develop_first_deploy_implicit_webserver_standard` (and the new
`develop_first_deploy_two_runtime_pairs_standard` from Phase 0c) measure
under the 28 KB body-join cap:

```bash
go test ./internal/workflow/ -run TestCorpusCoverage_KnownOverflows_StillOverflow -count=1
```

When this **fails** for a fixture (vacuous skip → fixture fits), edit
`internal/workflow/corpus_coverage_test.go` and remove that entry from
`knownOverflowFixtures`. The size-gate test then enforces every fixture
for every commit. Repeat per fixture until the map is empty.

**Acceptance** (mirrors §9): all fixtures green under
`TestCorpusCoverage_OutputUnderMCPCap`; map empty; `make lint-local`
clean; representative envelopes ideally land under 24 KB body-join
(8 KB margin for future feature growth).

## 8. Test guardrails (catch regressions)

| Test | What it guards | Failure mode |
|---|---|---|
| `TestCorpusCoverage_RoundTrip` | Each fixture's `MustContain` phrases — load-bearing facts. | Trim deleted a fact the agent needs. Restore or re-pin. |
| `TestCorpusCoverage_OutputUnderMCPCap` | 28 KB soft cap per fixture (allowlisted overflows excluded). | A trim made another fixture grow past the cap (rare; usually only relevant when adding atoms). |
| `TestCorpusCoverage_KnownOverflows_StillOverflow` | The two overflow fixtures STAY over the cap until the trim is real. | Trim brought one under the cap — remove the entry from the allowlist. |
| `TestCorpusCoverage_CompactionSafe` | `Synthesize` is byte-deterministic for a fixed envelope. | Non-deterministic order or content snuck in (very unlikely from prose edits). |
| `TestSynthesize_AxisFiltering` and friends | Axis logic still picks the right atoms. | Frontmatter axis edit broke filtering. |
| `TestSynthesize_PlaceholderSubstitution` / `_UnknownPlaceholderErrors` | `{hostname}`/`{stage-hostname}` substitution + reject unknown `{X}` tokens. | New placeholder accidentally introduced; not in the allowlist. |
| `TestAtomAuthoringLint` (in `internal/content/atoms_lint.go`) | Forbidden patterns: spec IDs (DM-*, KD-*, etc.), handler-internals verbs, invisible-state field names, plan-doc paths. | Edit reintroduced forbidden wording. |
| `TestAtomReferenceFieldIntegrity` | Every `references-fields` frontmatter entry resolves to a Go struct field via AST. | Renamed/dropped a referenced field, or a typo in the frontmatter. |
| `TestAtomReferencesAtomsIntegrity` | Every `references-atoms` entry resolves to an existing atom ID. | Cross-atom rename without updating the link. |
| `internal/workflow/scenarios_test.go` | Per-phase atom-ID set assertions for the canonical scenarios. | Trim accidentally changed which atoms fire for a scenario. |

## 9. Acceptance criteria (for "done")

- All tests in §8 green at every commit.
- `knownOverflowFixtures` map empty.
- Every coverage fixture's joined output ≤ 28 KB.
- Two stretch fixtures (the formerly-allowlisted ones) ≤ 24 KB ideally.
- `make lint-local` clean.
- One commit per phase (or finer); commit messages cite atom IDs cut and
  bytes recovered.

## 10. Out of scope (DO NOT touch)

- `internal/recipe/` and any atoms therein — owned by recipe team
  (Aleš); separate scope.
- `internal/content/atoms/recipe-*.md` if any exist — same.
- The `dispatch-brief-atom` action in `internal/tools/workflow.go` —
  recipe-adjacent surface; reusing the mechanism for runtime atoms is
  acceptable as a **design discussion** (Phase 3 candidate) but DO NOT
  silently extend it without explicit user approval.
- `internal/content/templates/claude_*.md` — different lifecycle
  (rendered into project repo by `zcp init`); governed by
  `internal/tools/description_drift_test.go`.
- `internal/tools/*.go` tool descriptions — different surface; governed
  by the same drift lint.
- `docs/spec-*.md` — never delivered to LLM at runtime.

## 11. Anti-patterns (don't do these)

- **Don't ratchet the cap.** Lowering the MCP cap, raising the soft
  ceiling, or growing the `knownOverflowFixtures` allowlist are all
  admissions of failure. Trim the corpus instead.
- **Don't move bytes to another surface.** Sticking the same prose into
  `claude_shared.md` or a tool description shifts the cost — the LLM
  still pays per turn for the static rules + tool init. The right fix is
  cutting bytes, not relocating them.
- **Don't paraphrase load-bearing phrases.** `MustContain` assertions
  catch this; respect the pinned phrases. If a phrase MUST change,
  update both the atom AND the assertion in the same commit.
- **Don't trim an atom you don't fully understand.** Read the full atom
  body, the surrounding atoms (its `references-atoms`), and the
  scenarios that pin it before cutting. If unclear, trim something else.
- **Don't merge phases.** Verify each phase's gate is green before
  starting the next. A failed gate halfway through reveals which class
  of trim broke; merging hides the diagnosis.
- **Don't add a new atom while trimming.** This plan is purely
  subtractive on the corpus. Net additions belong in their own plan.

## 12. First moves for the fresh instance

1. Read this plan end to end.
2. Run §6.1 baseline measurements; confirm the numbers in §4 still
   match (corpus may have grown / shrunk since 2026-04-26).
3. Run §6.2 atomsize probe for the
   `develop_first_deploy_standard_container` envelope to see the
   current top contributors.
4. Optional but recommended: run a single Codex adversarial-review pass
   on the top-10 atoms with the four-axis prompt below — gives a second
   opinion on trim targets before you start cutting.
5. Pick Phase 1. Find one duplicate (the `deploy = new container` fact
   is a high-confidence starting point per §5/Axis 4). Trim it from
   non-canonical sites. Verify §6.4 gate. Commit. Repeat.

### 12.1 Suggested Codex prompt for a quick second-opinion pass

```
node "$HOME/.claude/plugins/cache/openai-codex/codex/1.0.4/scripts/codex-companion.mjs" \
  adversarial-review --wait --scope working-tree \
  "Read internal/content/atoms/develop-{verify-matrix,first-deploy-write-app,
   first-deploy-env-vars,deploy-modes,platform-rules-container,
   first-deploy-scaffold-yaml,dynamic-runtime-start-container,
   api-error-meta,http-diagnostic,deploy-files-self-deploy}.md.
   For each: identify (1) duplicates with other atoms in the same
   directory, (2) prose verbosity that could shrink without losing the
   action the agent must take, (3) sections that could be a denser
   table/checklist instead of prose, (4) content that's already in
   internal/content/templates/claude_shared.md or is general LLM
   knowledge. Estimate bytes recoverable per atom. Cite line numbers."
```

(One-shot; no agent dispatch needed.)

## 13. Appendix — useful greps

```bash
# Where does atom X get referenced from atoms?
grep -lE "references-atoms.*X|X" internal/content/atoms/*.md

# Where does the corpus reference a Go struct field?
grep -E "^references-fields" internal/content/atoms/*.md | sort -u

# Atoms that fire on a single phase / environment / runtime axis.
grep -lE "^phases: \[develop-active\]" internal/content/atoms/ | wc -l
grep -lE "^environments: \[container\]" internal/content/atoms/*.md | wc -l
grep -lE "^runtimes: \[dynamic\]" internal/content/atoms/*.md | wc -l

# Total bytes after a trim phase.
wc -c internal/content/atoms/*.md | tail -1
```

---

## 15. Pre-baked second-opinion analysis (Codex, 2026-04-26)

This section bakes in an independent corpus inspection so the fresh
instance has concrete trim targets to start from. Re-run §12.1 if you
want a fresh second opinion against the current corpus state.

### 15.1 Top trim targets (ranked by recoverable bytes)

1. **`develop-verify-matrix`** (3 398 B → ~1 300 B; **~2 100 B saved**).
   The embedded verify-agent prompt is prose-heavy. A compact verdict
   table (non-web → `zerops_verify`; web+subdomain → HTTP GET subdomain
   URL; mixed → both) collapses ~60% of the body. Optionally migrate
   the full Agent() Task prompt to a dispatch-brief-atom fetched on
   demand (see Phase 3 note about confirming dispatch-brief reuse).

2. **`develop-first-deploy-env-vars`** (2 483 B → ~1 280 B;
   **~1 200 B saved**). Carries a static catalog of managed-service
   env-var names that the atom's own instructions tell the agent to
   discover at runtime via `zerops_discover service={host}
   includeEnvs=true`. The catalog is both derivable at runtime AND
   prone to drift when the platform adds a new managed service.

3. **`develop-first-deploy-write-app`** (2 834 B → ~1 930 B;
   **~900 B saved**). Contains explanations the LLM already knows
   (why bind `0.0.0.0`, why `npm install` populates `node_modules`,
   why relative paths matter). The scaffold checklist can compress
   to a numbered list.

4. **`develop-platform-rules-container`** (1 930 B → ~1 230 B;
   **~700 B saved**). Orientation preamble re-read every turn ("In a
   container environment, the LLM operates against a Zerops-managed
   runtime…") should be flat bullets. SSHFS mount path rules duplicate
   `develop-first-deploy-intro`.

5. **`develop-deploy-modes`** (2 177 B → ~1 580 B; **~600 B saved**).
   Mode-comparison prose can become a 3-column table
   (mode / source / deployFiles mutability).

### 15.2 Axis 1 — content that shouldn't exist as atoms

- `develop-first-deploy-write-app.md` ~lines 22–38: "Most frameworks
  listen on 127.0.0.1 by default… change to 0.0.0.0" and "npm install
  / composer install creates node_modules / vendor" — general
  programming knowledge.
- `develop-first-deploy-env-vars.md` ~lines 16–45: the
  `MANAGED_SERVICES_HOST` / `DB_HOST` / `REDIS_HOST` / etc. static
  catalog — the atom itself instructs the agent to call
  `zerops_discover includeEnvs=true`.
- `develop-http-diagnostic.md` ~lines 8–15: "HTTP 200 means success,
  5xx means server error, 502/503 means…" — LLM general knowledge.
- `develop-api-error-meta.md` ~lines 12–28: "JSON error shape has
  `message`, `meta`, `status`…" — already in
  `internal/content/templates/claude_shared.md` ~lines 31–40 (verify
  before cutting; line numbers in the template may have shifted).

### 15.3 Axis 4 — cross-atom duplicates (canonical-home assignments)

| Fact | Atoms carrying it | Canonical home |
|---|---|---|
| `deployFiles` / new-container semantics | `develop-platform-rules-container`, `develop-deploy-files-self-deploy`, `develop-first-deploy-scaffold-yaml`, `develop-change-drives-deploy`, `develop-push-dev-deploy-container` | `develop-deploy-files-self-deploy` |
| SSHFS mount path (`/var/www/{hostname}/`) | `develop-platform-rules-container`, `develop-first-deploy-intro`, `develop-first-deploy-write-app`, `develop-dynamic-runtime-start-container` | `develop-platform-rules-container` |
| `${hostname_VARNAME}` env-ref syntax | `develop-env-var-channels`, `develop-first-deploy-env-vars`, `develop-first-deploy-scaffold-yaml` | `develop-env-var-channels` |
| HTTP `curl localhost:{port}` probe | `develop-http-diagnostic`, `develop-verify-matrix` | `develop-http-diagnostic` |
| `sudo apk add` / `sudo apt-get install` for `prepareCommands` | `develop-platform-rules-common`, `develop-first-deploy-write-app` | `develop-platform-rules-common` |

After picking the canonical home: in non-canonical atoms, replace the
restated fact with a one-liner pointer plus
`references-atoms: [<canonical-id>]` in the frontmatter. Run
`TestAtomReferencesAtomsIntegrity` to verify the link resolves.

### 15.4 Axis 3 — before/after sketch (`develop-verify-matrix`)

**Before** (~800 bytes for the decision logic):

> When verifying a first deploy, you should consider whether the
> service has a subdomain enabled. If it does, you should use the
> subdomainUrl from the envelope to make an HTTP request. If it does
> not have a subdomain, use zerops_verify. For services that have both
> internal ports and a subdomain, you should do both checks. Note that
> the subdomain takes time to propagate…

**After** (~180 bytes):

```
Verify: no-subdomain → zerops_verify service={host}
        subdomain    → GET subdomainUrl (expect 200, retry 3×/10s)
        both         → run both
        propagation lag: up to 60s; retry before failing
```

### 15.5 Estimated total reduction (SUPERSEDED — see §17)

The original numbers in this section came from the same single-service
measurement that produced §4's stale figures. The verified two-service
baseline + per-service-duplication insight changes both the starting
point and the available levers. §17.3 carries the corrected reduction
budget for each of the three implementation paths (structural
synthesizer change, per-atom collapse-friendly rewrite, classic per-
atom trim).

**Original estimates (KEPT FOR HISTORY — do not act on these):**

| Envelope | "Before" (single-service) | "After" (estimated) | Note |
|---|---|---|---|
| `develop_first_deploy_standard_container` | 29 988 B | ~22 400 B | Both numbers are single-service; actual fixture is 40 228 B. |
| `develop_first_deploy_implicit_webserver_standard` | 31 574 B | ~23 900 B | Same metric error. |

---

## 16. Codex review findings (must address)

An independent Codex pass on this plan flagged three substantive issues
that change how the work must be executed. They are NOT optional polish.

### 16.1 (HIGH) Sizing metric must measure the wire payload, not just joined bodies

`§4` and `§15` quote bytes from joined `SynthesizeBodies` output (29 988
and 31 574). The `knownOverflowFixtures` map quotes higher numbers
(40 228 and 43 447) — same envelopes, different metric. The 32 KB MCP
cap applies to the **full tool response**: `textResult(RenderStatus(
Response{Envelope, Plan, Guidance: BodiesOf(matches)}))`. That includes
the `## Status` markdown skeleton, services list, plan render, and any
JSON envelope wrapping at the wire.

**Action before any trim:** add a baseline acceptance gate that
serializes the actual MCP `tools/call` response shape for both
overflow fixtures (call `RenderStatus` on the same `Response` the
handler builds, measure `len(textResult(...).Content[0].Text)`) and
asserts under 32 KB. Re-anchor §15.5 estimates from THAT metric.
Without this, a fresh executor can pass the body-size cap test while
the real wire still overflows.

### 16.2 (MEDIUM) Per-trim fact inventory required (MustContain pins are too sparse)

`§8` treats `MustContain` as the preservation contract, but each
fixture pins only ~3–10 phrases while 19–20 atoms fire. A trim can
delete operational content that's not pinned and keep the suite green.
`§7` Phase 4 also explicitly permits updating `MustContain` when an
atom is deleted — opening a hole where the trim and the pin update
ride together with no independent witness.

**Action:** before editing any atom, write a fact inventory for that
atom — bullet list of every operational instruction the atom carries,
with one of three labels per fact: `KEEP` (pin a new `MustContain`
phrase that asserts it survives), `MOVE` (cite the canonical-home atom
that will absorb it), `DROP` (cite the §5 axis justifying removal).
Commit the inventory alongside the atom edit so a reviewer can verify
what was preserved/moved/dropped.

### 16.3 (MEDIUM) Multi-service standard envelope not pre-emptively covered

`§4` envelopes both have ONE service pair (one runtime, dev+stage).
Per `internal/workflow/synthesize.go:84-110`, atoms with service-scoped
axes render **once per matching service**. A project with two
standard-mode service pairs (e.g. `appdev/appstage` + `apidev/apistage`,
both nodejs@22 first-deploy) doubles the per-service atom output.
Even after this trim closes the single-pair gap with margin, the
two-pair shape can overflow again silently — `coverageFixtures()` has
no fixture testing it.

**Action:** add a stretch fixture
`develop_first_deploy_two_service_pairs_standard` to
`internal/workflow/corpus_coverage_test.go`'s `developCoverageFixtures()`
or `matrixCoverageFixtures()` (pick the file by topical fit). Measure
the wire-payload size for it as part of the §16.1 acceptance gate. If
it overflows even after the single-pair trim, either (a) trim further
to fit, OR (b) escalate to a synthesis design change: render
envelope-scoped guidance ONCE per envelope and per-service guidance
per matching service (the per-service guidance is the smaller fraction
— most atom bodies are envelope-scoped operational rules, not per-host
deploy commands). Option (b) is the structural fix; option (a) is the
content fix; the stretch fixture forces that decision rather than
leaving it implicit.

### 16.4 Plan-execution order amendment

The original phasing in §7 starts with duplicate removal. Given §16.1
and §16.2, the corrected first move is:

0. **Phase 0 — Calibration.** Implement the wire-payload acceptance
   gate (§16.1). Re-measure both overflow fixtures via the new metric.
   Add the multi-service stretch fixture (§16.3) and measure it.
   Update §15.5 estimates from the new baseline. Decide single-pair
   trim only vs. structural multi-service fix BEFORE starting any
   subtractive work.

Phases 1–5 follow as documented, with the §16.2 fact-inventory
discipline applied to every touched atom.

---

## 17. Live verification (2026-04-26) — supersedes §4 + §15.5 numbers

This section records the empirical measurements taken against the live
zcp container in eval-zcp project + a worktree-isolated probe binary
exercising the production code path (`SynthesizeBodies` → `RenderStatus`
→ `Response{}` JSON marshal → MCP `CallToolResult` → JSON-RPC frame +
newline). Before any work begins, re-confirm these numbers with the §17.5
probe — they may shift if the corpus or render pipeline changed.

### 17.1 Wire-frame measurements (the metric that actually matters)

The 32 KB MCP cap applies to the **JSON-RPC frame on the stdio wire**, NOT
the joined atom bodies. Three layered metrics matter; track all three:

| Metric | What it is | When it matters |
|---|---|---|
| `synthesize_bodies_join` | `strings.Join(bodies, "\n\n---\n\n")` | What `corpus_coverage_test.go` measures today (`OutputUnderMCPCap` + `KnownOverflows_StillOverflow`). |
| `render_status_markdown` | `RenderStatus(Response{Env, Guidance, Plan})` — adds `## Status\n` + Phase + Services + 2-space indentation per Guidance line. | The `text` field of `mcp.TextContent`. |
| `mcp_wire_jsonrpc_frame` | `{"jsonrpc":"2.0","id":N,"result":{"content":[{"type":"text","text":"<rendered>"}]}}` + `\n` | The actual byte count Claude Code's stdio reader sees. **This is what hits the cap.** |

Verified numbers (probe in worktree, build from `bff387a5`):

| Fixture | bodies-join | render | wire-frame | over 32 KB by |
|---|---|---|---|---|
| `develop_first_deploy_standard_container` (2-svc, actual fixture) | **40 228 B** | 41 831 B | **43 256 B** | **+10 488 B** |
| `develop_first_deploy_implicit_webserver_standard` (2-svc, actual) | **43 447 B** | 45 128 B | **46 647 B** | **+13 879 B** |
| `develop_first_deploy_standard` (single-service, hypothetical) | 30 019 B | 31 177 B | 32 243 B | **+525 B** (already at cap edge) |

**Translations between metrics**:
- `render_status_markdown` = `bodies_join` + `~1 600 B` (header, services
  list, indentation).
- `mcp_wire_jsonrpc_frame` = `render_status_markdown` + `~1 400 B` (JSON
  string-escaping of newlines/quotes + JSON-RPC envelope + framing newline).
- For practical purposes: **add ~3 KB to the bodies-join number to get the
  wire frame**.

The single-service number (32 243 B wire) is **already 525 B over the cap**
— meaning even after we fix the two-service overflow, an envelope with a
single standard-mode runtime will still spillover. The trim budget must
take this seriously: any future addition of even one new envelope-scoped
atom in the develop-active first-deploy set risks pushing the single-
service shape past the cap.

### 17.2 Cap behavior (NOT a hard error — silent degradation)

What happens when the wire frame exceeds Claude Code's stdio cap is
documented in this codebase (`internal/workflow/dispatch_brief_envelope.go:8-22`,
`docs/zcprecipator2/implementation-notes.md:1552`):

> "the `complete substep=feature-sweep-stage` response weighed 71,720
> chars, exceeding the runtime's ~32 KB MCP tool-response token cap.
> The harness spilled the payload to a scratch file
> `/home/zerops/.claude/projects/.../tool-results/mcp-zerops-zerops_workflow-*.txt`.
> The main agent only read the first ~3 KB of the spillover, losing
> the wire contract."

So at **43 256 B** wire frame:
1. Claude Code MCP STDIO transport spills the response to a tool-results
   scratch file.
2. The agent receives a stub pointing at the file path.
3. The agent reads only the first ~3 KB of the file.
4. **~93 % of the runtime guidance is silently lost**, including
   `develop-verify-matrix` (the verify-agent contract), all per-service
   first-deploy operational rules, deploy-mode rules, etc.

The agent has no error to react to — it just acts on the partial guidance
it received and is much more likely to make wrong moves (skip the verify
step, miss SSHFS path rules, miss env-var conventions). This matches the
v35 incident's downstream behavior exactly.

### 17.3 The dominant lever — three implementation paths

**Per-service render duplication accounts for ~50 % of the standard
fixture's overflow** (see §4.4 table). Six atoms render once per matching
service in the dev/stage pair.

**Critical constraint** (verified against the six atoms): the duplicated
bodies contain HOST-SPECIFIC TOOL-CALL ARGUMENTS, not just paths:

| Atom | Host-specific surface |
|---|---|
| `develop-first-deploy-write-app:13,45,51,57,60` | `/var/www/{hostname}/` (path) + `ssh {hostname} "..."` (command) |
| `develop-first-deploy-env-vars:12,61` | `zerops_discover service="{hostname}"` (tool arg) |
| `develop-first-deploy-execute:13` | `zerops_deploy targetService="{hostname}"` (tool arg) |
| `develop-first-deploy-verify:14` | `zerops_verify serviceHostname="{hostname}"` (tool arg) |
| `develop-first-deploy-intro:24,28` | `zerops_deploy targetService=<hostname>` + `zerops_verify serviceHostname=<hostname>` |
| `develop-first-deploy-scaffold-yaml` | (no host-specific commands — collapses cleanly) |

The agent acts on each tool-call argument as a separate operation —
`zerops_deploy targetService="appdev"` AND `zerops_deploy
targetService="appstage"` are two distinct calls. Rendering once with
"`{hostname}=appdev|appstage`" enumeration **would lose the per-host
operation contract**. This rules out the naive "collapse identical
bodies" framing my first draft of this section had.

The placeholder allowlist today is `{hostname}`, `{stage-hostname}`,
`{project-name}` (`docs/spec-knowledge-distribution.md:236-247`,
`internal/workflow/synthesize.go:94-99`). No multi-host placeholder
grammar exists.

#### Option A — Structural synthesizer change (highest leverage, highest cost)

Two sub-changes, must ship together:

**A.1**: Split each duplicating atom into TWO atoms — one envelope-scoped
("write to the SSHFS mount; here are the rules"; renders once) and one
per-service-scoped ("for THIS service: `zerops_deploy
targetService=`appdev`...`"; renders per host but is small). The split
follows a content boundary that already exists: rules/concepts vs.
imperative tool calls.

**A.2**: Optionally introduce a multi-host placeholder grammar
(e.g. `{for-each-host: zerops_deploy targetService="{hostname}"}` that
`Synthesize` expands inline at render time) so per-service tool-call
arguments collapse into one render with N expanded lines. Spec the
grammar before implementing.

Neither A.1 nor A.2 alone breaks the existing `{hostname}` substitution;
A.1 splits content along a natural seam, A.2 adds a new grammar without
changing the existing single-host one. Together they recover ~7-10 KB
without trimming any guidance.

**Existing tests that must change**:
- `internal/workflow/synthesize_test.go::TestSynthesize_*` — the "one
  render per matching service" pin (around `synthesize_test.go:730-758`)
  becomes "one render per (atom, matching-service) tuple", with new
  cases for the envelope-scoped half of each split atom.
- `internal/workflow/scenarios_test.go` — per-phase atom-ID set
  assertions need the new atom IDs added.
- `internal/workflow/corpus_coverage_test.go::coverageFixtures()` —
  `MustContain` phrases re-pinned against whichever half of the split
  carries the asserted phrase.
- `internal/content/atoms_lint_test.go` — verify the split atoms still
  pass the authoring contract (no spec-IDs, no handler verbs, etc.).

**Estimated reduction**: ~7-10 KB standard, ~10-13 KB implicit-webserver.
**Estimated effort**: 2-3 days (atom splits + optional grammar +
extensive test updates).
**Risk**: high blast radius; every atom-rendering test re-baselines.
But it unblocks 3+-service envelopes (currently catastrophic).

#### Option B — Atom rewrites that fold concepts into envelope, push commands to small per-service atoms

Keep `Synthesize` unchanged. Rewrite the six duplicating atoms in place
so each becomes a small per-host stub (containing only the imperative
tool calls — usually <300 B per host) that points at a new envelope-
scoped concept atom (rules/rationale/path semantics — renders once,
maybe ~1.5-2 KB).

Mechanically: `develop-first-deploy-write-app` (2.8 KB × 2 = 5.7 KB)
becomes `develop-first-deploy-write-app-rules` (envelope-scoped, ~2 KB,
renders 1×) + `develop-first-deploy-write-app-cmds-{hostname}` (per-
host, ~300 B, renders 2×). Net: 2 KB + 600 B = 2.6 KB vs. the 5.7 KB
double-render. ~3 KB recovered per atom × 5 affected atoms ≈ 12 KB.

**Less invasive on `Synthesize`**: no rendering-pipeline change.
**More invasive on the corpus**: 5 new envelope atoms + 5 reshaped per-
host atoms. `references-atoms` cross-links must be updated. The atom
authoring contract test (`TestAtomAuthoringLint`) plus reference-
integrity tests apply unchanged.

**Estimated reduction**: ~10-12 KB standard, similar implicit-webserver.
**Estimated effort**: 1-2 days (corpus-only edits, no Go changes).
**Risk**: medium; mostly atom-content work + cross-link maintenance.

#### Option C — Classic per-atom trim (Phase 2 of the original plan)

Stay with per-service rendering. Trim each of the six atoms by 30-40 %
each so 2× the trimmed size still fits. This is what the original §15
estimates assumed (without acknowledging the duplication). To recover
~10 KB you have to trim ~5 KB of source content across six atoms —
mostly the per-host operational instructions that the agent actually
needs.

**Estimated reduction**: ~5-7 KB after aggressive trim, possibly insufficient.
**Estimated effort**: 2-3 days, fact-by-fact careful edits.
**Risk**: high — actual operational guidance loss; harder to verify
nothing critical was dropped.

**Recommendation**: pursue Option B (corpus-only, no synthesizer change,
respects existing placeholder contract) unless the multi-pair stretch
fixture from §17.7 forces a structural fix at which point pivot to
Option A. Option C is what the original plan assumed and falls short of
the budget needed (single-service shape is already at cap edge — see
§17.1).

### 17.4 The `dispatch-brief-atom` PATTERN — generalize, do not reuse

A solution to **the same overflow class** shipped as `Cx-BRIEF-OVERFLOW`
(F-1 close from the v35 incident) — but it is **NOT directly reusable**
for develop-active atoms. It needs generalization.

Today's mechanism (verified):

- `internal/workflow/dispatch_brief_envelope.go:57-68` — `envelopeForLargeBrief`
  is hard-scoped to `step == RecipeStepDeploy && subStep == SubStepReadmes &&
  isShowcase(plan)`. Other paths fall through to inline.
- `internal/tools/workflow.go:75-80` — the `AtomID` field on
  `WorkflowInput` is documented as "Fully-qualified dot-path (e.g.
  'briefs.writer.manifest-contract')" — recipe atom namespace, not
  `internal/content/atoms/*.md` IDs.
- `internal/tools/workflow.go:528-558` — `handleDispatchBriefAtom`
  retrieves via `LoadAtomBodyRendered(...)` with recipe render context;
  it does NOT consult a `StateEnvelope` and does NOT do `{hostname}`
  substitution.

**To extend the pattern to develop-active atoms** the cost items are:

1. **Atom namespace generalization** — `AtomID` lookup must accept
   either a recipe dot-path OR a runtime-corpus atom ID
   (`develop-verify-matrix`, etc.). Either union the namespaces or add
   an `atomNamespace` discriminator to the tool input.
2. **Per-envelope placeholder rendering** — the runtime atom body must
   be rendered with the caller's `StateEnvelope` `{hostname}` /
   `{stage-hostname}` substitution at retrieval time. The recipe path
   doesn't carry an envelope; the runtime path needs one passed in (or
   stored in the active session).
3. **New scoping gate** — `envelopeForLargeBrief` is recipe-substep
   gated. A runtime equivalent needs its own gate at the
   `RenderStatus`/`Synthesize` boundary that detects when the composed
   guidance exceeds the threshold and emits an envelope pointing at
   ungated atom IDs.
4. **Stitch ordering invariant** — recipe path has `writerBriefBodyAtomIDs()`
   pinning the atom sequence so envelope and inline produce byte-identical
   output. A runtime equivalent needs the equivalent of
   `dispatch_brief_envelope_test.go::TestEnvelopeAtoms_StitchToFullBrief`
   for the runtime atom set.
5. **Tests** — equivalents to `dispatch_brief_envelope_test.go:111-151`
   for the new envelope shape + the runtime stitch invariant.

The pattern is right; the implementation is recipe-bespoke. Phase 3 of
the original plan (Axis 3) mentions reusing dispatch-brief-atom for
`develop-verify-matrix` — that's still a sensible direction, but score
it as "build a runtime variant of the pattern", not "call the existing
function". The Phase 3 effort estimate (~2-4 KB recovery) should
include generalization cost from this list.

### 17.5 Probe used for these measurements

The probe was built and run from a git worktree (`../zcp-trim-verify` at
`bff387a5`) so it didn't conflict with parallel work in progress on
`internal/tools/`, `internal/workflow/envelope.go`, etc.

```go
// cmd/atomsize_probe/main.go (build with: go build -o /tmp/atomsize_probe ./cmd/atomsize_probe/)
// Runs SynthesizeBodies + RenderStatus + JSON-RPC marshal pipeline for
// the two overflow fixtures + a single-service hypothetical, prints the
// three layered metrics + per-atom contributions.
```

To re-run after a trim: rebuild the probe in a worktree (so a half-merged
parallel refactor in `internal/workflow/` doesn't break your build),
`/tmp/atomsize_probe`, compare numbers to the table in §17.1.

For end-to-end live verification (probe doesn't catch wire-side
surprises), the procedure that worked here:

1. SSH to the `zcp` container (`ssh zcp` — host key checking disabled
   per CLAUDE.local.md).
2. `cat <init+initialized+tools/call> | timeout 10s zcp serve > /tmp/resp.ndjson`
   with `sleep 6` between request batch and EOF (otherwise the server
   shuts down before flushing the response).
3. Per-line byte size + JSON `result.content[0].text` length give you
   the actual wire numbers.

Live container measurement on idle envelope (no overflow scenario yet):
init response 206 B, status response 3 162 B JSON line / 2 965 B text.
JSON-RPC overhead matches the probe model (~197 B between text and
line). Confidence the probe = wire is high.

### 17.6 Coincidental finding — orphan meta on eval-zcp

Surface only — not in scope of this plan. The eval-zcp project's zcp
container has `/var/www/.zcp/state/services/probe.json` for a service
named `probe` that no longer exists in the live API. This is exactly
the G4 scenario `plans/open-findings-resolution-2026-04-26.md` addresses.
Useful as a real-world test fixture once that work lands.

### 17.7 Phase 0 amendment — calibration without breaking the tree

The §16.4 prescription "implement the wire-payload acceptance gate" as a
hard assertion would break the tree on Phase 0 (the wire frame is at 43
KB; a < 32 KB hard gate fails immediately). Corrected sequence:

1. **Phase 0a**: Add a `mcp_wire_jsonrpc_frame` measurement to
   `corpus_coverage_test.go` as an info-level `t.Logf` on every fixture
   in `coverageFixtures()`. No assertion.
2. **Phase 0b**: Extend `knownOverflowFixtures` map entries to carry
   wire-frame number alongside the body-join number. Same allowlist
   pattern; companion `KnownOverflows_StillOverflow` test stays.
3. **Phase 0c**: Add a multi-pair stretch fixture (per §16.3) — call it
   `develop_first_deploy_two_runtime_pairs_standard`. **Measure its
   actual `bodies_join` size with the §17.5 probe BEFORE adding to
   `knownOverflowFixtures`** — the companion test
   `KnownOverflows_StillOverflow` (`corpus_coverage_test.go:795-813`)
   asserts each allowlisted fixture currently exceeds the 28 KB
   bodies-join cap, so adding a fixture under the cap fails the
   companion immediately. Record the measured `bodies_join` AND
   `mcp_wire_jsonrpc_frame` numbers in the rationale string. Per-
   service duplication scaling predicts > 60 KB body-join for two pairs;
   confirm before allowlisting.
4. **Phases 1+**: Pick A or B from §17.3. As wire-frame numbers cross
   under 32 KB, remove fixtures from the allowlist (the ratchet test
   forces this).

Don't try to land a hard 32 KB wire-frame gate before the structural
fix — the gate is the OUTCOME of the trim, not a precondition.

### 17.8 Plan-execution amendments — applied to §7

The amendments below have been folded into §7 directly. Listed here as
the changelog so a reader who knows the original §7 sees what moved.

- **Per-service-duplication kill is now Phase 1** (was implicit / not
  acknowledged in original §7). Original Phase 1 (cross-atom dups)
  becomes Phase 2; Phases 3-5 unchanged.
- **Phase 0 (calibration) inserted** — wire-frame info log, two-metric
  allowlist entries, multi-pair stretch fixture. None of these break
  the tree.
- **§15.2 / §15.3 caveats inlined** — Phase 2 + Phase 4 now carry an
  explicit "re-verify against current corpus" step because three §15
  spot-checks did not survive `grep` (apiMeta not in claude_shared.md;
  "deploy = new container" duplicate appears in 2 atoms not 5;
  `${hostname_VARNAME}` is in different atoms than claimed).
- **§16.2 fact-inventory discipline inlined into Phase 1 step 6** —
  every split atom needs KEEP/MOVE/DROP labels for each fact, committed
  alongside the split.
- **Option B chosen as Phase 1 default**, with explicit fallback gate to
  Option A only if standard fixture's wire-frame is still > 32 KB after
  all six splits. Pivot decision happens at one specific point, not
  scattered.

---

## 14. Provenance

This plan was prepared after a session that established the empirical
overflow numbers in §4, the size-gate test machinery referenced in §8,
and the four-axis trim taxonomy in §5. The fixtures, allowlist, and
companion ratchet test all exist in the codebase as of 2026-04-26.
A separate audit document (`docs/audit-instruction-delivery-synthesis-2026-04-26.md`)
discusses the full runtime instruction-delivery surface; this plan
focuses narrowly on corpus-byte reduction. No prior context beyond what
this file states is required to execute.
