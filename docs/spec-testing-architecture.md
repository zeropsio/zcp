# ZCP Testing Architecture Specification

> **Scope**: The governing MAP for EVERY verification surface — which tier proves
> which behavior, and the rule that decides where a new test lands. The
> test-surface analog of CLAUDE.md's "one home per knowledge": one behavior, one
> verification home, chosen by a TIER RULE, not by convenience.
>
> **Status**: DESIGN — defines the target the consolidation
> (`plans/test-eval-consolidation-impl-2026-06-16.md`, executing the audit
> `plans/e2e-eval-surface-consolidation-2026-06-16.md`) lands against.
>
> This is a map: pointers + disciplines. It caches no test values and no
> per-file dispositions — those live in the tests + the audit. To answer "is
> behavior X covered," go to the tier this spec names + read the test.

---

## 1. The Placement Principle — one behavior, one home, chosen by a rule

Every behavior has exactly ONE verification home. The home is chosen by the
**tier rule** (§2) — "what does proving this actually require" — never by which
file the author was editing.

**The symptom this fixes:** tests land in the wrong tier by copy-paste. A
platform-free guard inherits the `e2e` build tag of the file it was pasted near
and then **never runs** — not in `go test ./...`, not in CI, only behind a
real-platform gate it doesn't need. The 2026-06-16 audit found exactly this:
`update_test.go` (subprocess binary-swap) and `orphan_cleanup_safety_test.go`
(pure `hasTestPrefix` table) were `e2e`-tagged safety guards that executed in
neither the default suite nor CI. The tier rule is the antidote: placement is a
property of the behavior (what it needs to be proved), not the editing context.

---

## 2. The tiers — and the rule that assigns them

There are now **two real-platform build tags** (down from four — `probe` deleted,
`live` folded into `api`; `plans/e2e-eval-surface-consolidation-2026-06-16.md`).
Tiers in increasing cost/coupling:

| Tier | Build tag | What it proves | Cost |
|---|---|---|---|
| **default** | *(untagged)* | everything provable OFFLINE | `go test ./...` |
| **`api`** | `//go:build api` | read-mostly REAL-platform CONTRACT | `ZCP_API_KEY`, read |
| **`e2e`** | `//go:build e2e` | MUTATING real-platform lifecycle | eval-zcp, mutates |
| **UI-drive** | *(not a build tag — Node)* | the ASSEMBLED embedded UI against a live container | puppeteer-core, mutates |
| **behavioral eval** | *(NOT a build tag)* | non-deterministic AGENT-decision quality | full agent run |

**The tier rule** (apply top-down; the first tier that can prove the behavior is its home):

- **default (untagged)** — anything provable without the live platform: units,
  schema/content lint, parser/render, safety guards (e.g. `hasTestPrefix`), the
  subprocess `update` binary-swap test. If it needs no `ZCP_API_KEY` and no
  eval-zcp, it belongs here — and ONLY here.
- **`api`** — read-mostly REAL platform: the SDK-decode / status / error-code
  CONTRACT (what the wire actually decodes to, which no mock can pin honestly),
  the live catalog + recipe audits, the export-composer audit against live
  source. The retired `live` tag's three real audits folded in here; `probe`
  (spent investigation scaffolding) was deleted. `api` asserts the SDK PROTOCOL,
  not tool behavior — which is why it is NOT foldable into `e2e`.
- **`e2e`** — MUTATING real-platform lifecycle: full ZCP handler / MCP-tool runs
  vs eval-zcp (deploy ssh+local, failure classification, bootstrap, export,
  launch single-token lifecycle, verify, subdomain, git-delivery, env-generate).
  The everything-net for deterministic platform truth. The tag also covers a
  second, independent suite on a different PLANE: the Data Console's live
  data-plane conformance harness
  (`internal/dataconsole/console/provider/conformance/`) dials the managed
  engines directly — pg/redis/s3/http/kafka/nats over the project VPN — never
  the Zerops REST API, so it needs no `ZCP_API_KEY`. Same tier semantics as
  the control-plane suite above (mutating, real backends, no retries on
  semantic assertions), just aimed at engine data instead of platform
  lifecycle. `vet-tags` already compiles it (`go vet -tags e2e ./...` walks
  the whole tree), so the one compile rot-guard covers both suites without a
  second Makefile target.
- **UI-drive** (`internal/dataconsole/uitest/`) — live-only and Node-run, outside
  the Go build-tag system entirely: a `puppeteer-core` harness drives the Data
  Console's ASSEMBLED embedded UI (code-server -> VS Code workbench -> nested
  webview iframes -> the console SPA) against a real deployed container — the one
  layer the `e2e` conformance harness above and the jsdom/HTTP test layers below
  it cannot reach, since a divergence *between* layers (SPA state green while the
  broker is actually read-only), native-control styling, or layout reflow is
  invisible to all of them. `run.js` (scenario registry), `gallery.js` (a
  state-gallery screenshot sweep), and `button-audit.js` (exhaustive control
  enumeration) are its three entry points. Every scenario asserts THREE oracles:
  O-UI (a DOM assertion in the real webview), O-ENGINE (an independent CLI over
  SSH — `psql`/`redis-cli`/`curl`/`mc` — never the console's own API), and
  O-HONESTY (the UI's claim equals engine truth: success implies applied, error
  implies unchanged, refusal implies refused and looks refused). It is never part
  of `go test ./...`, and `make vet-tags` does not compile it — `node run.js`
  against a live container is the only way to run it.
- **behavioral eval** (`eval/behavioral/`, run by `flow-eval.sh`) — the only home
  for **non-deterministic agent-decision quality**: route choice, plan shape,
  env-wiring, blocker comprehension. A markdown scenario corpus + an agent
  runner, NOT a build tag. See §6.

---

## 3. Division of labor — deterministic correctness vs agent navigation

`api`/`e2e` and behavioral eval are **complementary by design, not redundant**:

- **`api`/`e2e`** prove deterministic **handler + platform correctness** — given
  these inputs, the handler does exactly this and the platform reaches exactly
  that state. Assertable, repeatable, gating.
- **behavioral eval** proves non-deterministic **agent navigation** — does the
  agent, free-running, choose the right route / shape / wiring. Observed, not
  asserted (§4).

**The load-bearing rule:** **no e2e is deletable on an "an eval covers it" basis.**
Behavioral verification is warn-only (§4), so an agent-flow eval is NOT a
substitute for a deterministic platform pin. A behavioral scenario exercising
the same surface narrows nothing about the e2e's keep/delete decision. e2e
shrinks only by removing *deterministic* redundancy (one e2e subsuming another's
asserts), never by deferring coverage to an eval.

---

## 4. The eval-as-gate decision — observation-only, by deliberate design

Today behavioral verification is **OBSERVATION-ONLY / warn-only**. The runner's
`VerificationConfig` (`internal/eval`) writes findings to `verification.json`
without failing the run; the suite verdict propagates from the retrospective, and
**the local Claude session is the grader** (`eval/behavioral/README.md`). There
is no automated pass/fail.

**Why this is deliberate, not an oversight:** a reliable automated grader for
open-ended agent navigation is itself a hard, non-deterministic problem; a flaky
LLM-judge gate would block CI on grader noise, not on real regressions. The
chosen design keeps the human in the loop where judgment is irreducible.

**The named inversion (recorded, not lost):** the rigor gradient runs *backwards*
relative to the product's purpose — maximal rigor on handler-correctness
(`api`/`e2e`, hard-gating), minimal on the agent-success the product exists to
deliver (behavioral, warn-only). This is a conscious fork, not drift.

**Path to a hard gate — TWO prerequisites, NEITHER present today:**

1. **A reliable automated grader.** Deterministic platform assertions already
   exist via `VerificationConfig::ExpectedServices` / `NoFailedProcesses`; the
   open part is grading the non-deterministic NAVIGATION quality, not the
   platform outcome — itself the hard, non-deterministic problem above.
2. **A `gating` field on the scenario schema.** No such field is parsed
   (`scenario.go`) and zero scenarios carry one — there is NO present lever to
   flip. Making eval a hard gate first requires ADDING `gating` to the manifest
   (a future field, §7) so a scenario can declare itself enforcing.

Both are prerequisites, not present capability. When BOTH exist and the grader
is trustworthy, behavioral verification can fail the run, and §3's rule relaxes:
an eval CAN then justify retiring a deterministic e2e. Until then it cannot.

---

## 5. Anti-drift — the compile-guard catches rot it can't see; these catch the rest

`make vet-tags` compiles the `api` + `e2e` tagged files so they can't ROT at the
COMPILE level (a renamed symbol breaks the build). It is blind to **semantic**
rot: a stale string literal compiles fine. The defenses against semantic rot:

- **Scenario drift guards** (`internal/content/eval_scenario_drift_test.go`) —
  `TestNoNeverShippedLaunchVocabInEvalScenarios` (a whole never-shipped DESIGN's
  vocabulary) + `TestNoRetiredMechanicsVocabInEvalScenarios` (individual retired
  MECHANIC literals: `ZEROPS_TOKEN_STAGE`, re-supply-same-launchKey,
  `closeMode=git-push`, `.netrc`). A scenario that names dead vocab trains the
  agent against behavior the shipped handler cannot produce — and burns a
  14-17 min eval cycle with no code fix. Scoped to `eval/behavioral/scenarios{,-local}/`
  deliberately: plans/ and archived design docs legitimately name this vocab as
  history, so the whole-repo gate must not claim it.
- **Single-owner derivation** (`tell == check`) — a test's expected set DERIVES
  from the runtime owner instead of re-listing it. `knowledge_quality`'s service
  key-table derives from `knowledge.serviceNormalizer` (its `Keys()` accessor),
  so a key dropped from the normalizer can't leave a stale expectation behind.
  This is the §3.3 derive-from-code rule of `spec-knowledge-architecture.md`
  applied to the test surface.
- **Scenario-manifest convention** (§7) — the front-matter every behavioral
  scenario carries, enforced by `eval_scenario_manifest_test.go`, so the implicit
  convention can't silently lapse.

`vet-tags` catches COMPILE rot; the drift guards + single-owner derivation catch
SEMANTIC rot. Both are required — neither subsumes the other.

---

## 6. The behavioral corpus — runner, scenarios, retrospective

`eval/behavioral/scenarios/` (container-mode, run over SSH to a zcp container) +
`eval/behavioral/scenarios-local/` (local-mode, run directly on the dev Mac) are
the two scenario directories. `flow-eval.sh` (container) and `make flow-eval-local`
build+ship the binary, run the agent, and pull the retrospective; the local
Claude session reads `self-review.md` and drives analysis. Full architecture +
round-trip protocol: `eval/behavioral/README.md`.

A scenario is a markdown file: YAML front-matter (the manifest, §7) + a body used
verbatim as the agent prompt. `cmd/zcp eval behavioral {list,run,all}` parses it
via `internal/eval/scenario.go`; only scenarios carrying `retrospective:`
(`IsBehavioral()`) are behavioral. A scenario missing it is SKIPPED by `list`/`all`
but ERRORS under `run` (`RunBehavioralScenario` rejects a non-behavioral scenario,
`behavioral_run.go`) — addressing one by id is an explicit choice the runner
refuses rather than silently no-ops.

---

## 7. Scenario manifest convention — the enforced front-matter

Every behavioral scenario carries front-matter fields that `flow-eval.sh` + the
`cmd/zcp eval behavioral` runner actually rely on. `eval_scenario_manifest_test.go`
pins the universal set so the convention is enforced, not implicit — with NO new
per-file data (every field below is already universal in the corpus).

**The enforced set** (each is RELIED ON by flow-eval's own parsing — the lint
enforces only what the harness reads, never an invented field):

| Field | Relied on by |
|---|---|
| `id` | `validate()` requires it; `flow-eval.sh <id>` addresses the scenario as `<id>.md` (filename MUST equal `id`); `list` prints it |
| `description` | `list` prints it; the flow-eval descriptor-match (run-by-qualifier) matches against it |
| `seed` | `validate()` requires a valid enum (`empty\|imported\|deployed\|settled`) — drives fixture seeding |
| `tags` | `list` prints them; descriptor-match |
| `area` | `list` prints it; descriptor-match |
| `retrospective` | `IsBehavioral()` gate — a scenario WITHOUT it is invisible to `list`/`run`/`all` |

**Deferred — FUTURE fields, none parsed today:** the audit recommends a richer
curation manifest (`canonical` / `overlaps` / `last-reviewed`) to make de-dup
toward the founding 12-15 matrix mechanical and to give the drift lint an
ownership anchor. Separately, `gating` (a per-scenario enforce-vs-observe flag)
is the field the eval-as-gate path (§4) would need — it does NOT exist in
`scenario.go` and no scenario carries it; it is a prerequisite to ADD, not a
present lever. These are **NOT mass-added** here — the corpus carries ~55
scenarios and a bulk retrofit is its own curation pass. Add them as scenarios are
touched; the lint enforces only the universal set above until the curation pass
makes the richer set universal too.

---

## 8. Invariants (pin with tests)

- **I1** Every behavior has exactly one verification home, chosen by the tier
  rule (§2) — offline behavior is untagged-only, never `e2e`-tagged.
- **I2** Two real-platform build tags only (`api`, `e2e`); `vet-tags` compiles
  both. (`Makefile::vet-tags`)
- **I3** No e2e is deletable on an "an eval covers it" basis while behavioral
  verification is warn-only. (§3, §4)
- **I4** Behavioral scenarios carry the universal manifest set (§7).
  (`eval_scenario_manifest_test.go`)
- **I5** Eval scenarios name no never-shipped / retired vocab.
  (`eval_scenario_drift_test.go`)
- **I6** Test expectations over a runtime-owned set DERIVE from the owner, never
  re-list it (single-owner; e.g. `knowledge_quality` ← `serviceNormalizer`).

---

## 9. Relationship to other specs

- `spec-knowledge-architecture.md` — the knowledge-surface analog: one fact, one
  owner, computed delivery. §5's single-owner derivation + drift lints are its
  §3.3 / §5 rules applied to the TEST surface.
- `eval/behavioral/README.md` — the behavioral runner's operational home
  (round-trip protocol, failure modes); this spec governs where eval sits among
  the tiers, the README governs how it runs.
- `docs/spec-authoring-boundary.md` — the `ZCP_AUTHORING`-gated authoring domain
  has its own test surface under the same tier rule (depguard + `TestAuthoringBoundary_*`).
