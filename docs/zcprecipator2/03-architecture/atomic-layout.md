# atomic-layout.md — per-topic file tree

**Purpose**: the final atomic content tree that replaces monolithic `internal/content/workflows/recipe.md` (3,438 lines, 60+ named `<block>` regions). Every atom is ≤ 300 lines, names exactly one audience (main agent OR one sub-agent role, never both), carries zero version anchors, and declares (where applicable) its author-runnable pre-attest form per principle P1. Transmitted-brief atoms live under `briefs/`; dispatcher-facing text never appears in a `briefs/` atom (per P2). §3 contains the **current-block → new-atom(s)** mapping table with line-range citations.

Legend:
- **audience**: `main` | `scaffold-sub` | `feature-sub` | `writer-sub` | `code-review-sub` | `editorial-review-sub`
- **line-bound**: upper bound of resulting atom in lines; actual line count may be smaller
- atoms referenced by multiple audiences live under `principles/` and are pointer-included by the consuming brief (stitching-time concatenation, not transmitted-twice)

---

## 1. Directory tree

```
internal/content/workflows/recipe/
├── README.md                                    [audience: none — architecture overview, not loaded by agents, 120 lines max]
├── phases/
│   ├── research/
│   │   ├── entry.md                             [main,     90]   — step-entry: plan.Research schema + SymbolContract computation
│   │   ├── symbol-contract-derivation.md        [main,    120]   — how to compute SymbolContract from Research.Targets + managed services
│   │   └── completion.md                        [main,     60]   — attestation predicate: plan.Research populated, SymbolContract ready
│   ├── provision/
│   │   ├── entry.md                             [main,    100]   — step-entry: goal of provision phase
│   │   ├── import-yaml/
│   │   │   ├── standard-mode.md                 [main,    110]   — single-framework standard mode shape
│   │   │   ├── static-frontend.md               [main,     90]   — static+runtime dual-mode shape
│   │   │   ├── dual-runtime.md                  [main,    120]   — dual-runtime URL-var pattern
│   │   │   ├── workspace-restrictions.md        [main,     50]   — workspace-level envIsolation rules
│   │   │   └── framework-secrets.md             [main,     60]   — generateRandomString + preprocessor
│   │   ├── import-services-step.md              [main,     60]   — 2. zerops_import call shape
│   │   ├── mount-dev-filesystem.md              [main,     60]   — 3. zerops_mount per codebase
│   │   ├── git-config-container-side.md         [main,    110]   — 3a. single container-side SSH call: config + init + initial commit
│   │   ├── git-init-per-codebase.md             [main,     50]   — 3b. multi-codebase fan-out
│   │   ├── env-var-discovery.md                 [main,    120]   — 4. zerops_discover call; env catalog interpretation
│   │   ├── provision-attestation.md             [main,     70]   — attest predicate: services RUNNING, mounts present
│   │   └── completion.md                        [main,     50]   — attestation summary
│   ├── generate/
│   │   ├── entry.md                             [main,     90]   — step-entry: framing, container state during generate
│   │   ├── scaffold/
│   │   │   ├── entry.md                         [main,     80]   — substep-entry: dispatch N scaffolds (1 for minimal, 3 for showcase)
│   │   │   ├── where-to-write-single.md         [main,     60]   — single-codebase: write inline from main
│   │   │   ├── where-to-write-multi.md          [main,     60]   — multi-codebase: dispatch one sub-agent per hostname
│   │   │   ├── completion.md                    [main,     60]   — attest: all codebases scaffolded + pre-ship passed
│   │   │   └── dev-server-host-check.md         [main,     40]   — appdev dev-server must bind 0.0.0.0 (if hasBundlerDevServer)
│   │   ├── app-code/
│   │   │   ├── entry.md                         [main,     70]   — substep-entry
│   │   │   ├── execution-order-minimal.md       [main,     40]   — minimal tier: write in order
│   │   │   ├── dashboard-skeleton-showcase.md   [main,     60]   — showcase tier: health dashboard
│   │   │   └── completion.md                    [main,     40]
│   │   ├── smoke-test/
│   │   │   ├── entry.md                         [main,     60]   — on-container smoke test guidance
│   │   │   ├── on-container-smoke-test.md       [main,     90]   — SSH-side curl + commit sequence
│   │   │   └── completion.md                    [main,     40]
│   │   ├── zerops-yaml/
│   │   │   ├── entry.md                         [main,     70]   — substep-entry: schema pointer + file-level invariants
│   │   │   ├── env-var-model.md                 [main,    140]   — cross-service auto-inject semantics; self-shadow trap; decision flow
│   │   │   ├── dual-runtime-consumption.md      [main,    200]   — yaml-half + source-code-half for dual-runtime URL baking
│   │   │   ├── setup-rules-dev.md               [main,    110]   — dev setup rules (dev-only build steps, run.start)
│   │   │   ├── setup-rules-prod.md              [main,     90]   — prod setup rules
│   │   │   ├── setup-rules-worker.md            [main,     90]   — worker setup (no HTTP, queue group)
│   │   │   ├── setup-rules-static-frontend.md   [main,     70]   — serve-only-dev override for static-frontend
│   │   │   ├── seed-execonce-keys.md            [main,     90]   — `bootstrap-seed-v1` static key vs per-deploy migrate key
│   │   │   ├── comment-style-positive.md        [main,     60]   — ASCII-only `#` comments; one hash per line; positive form
│   │   │   └── completion.md                    [main,     40]
│   │   └── completion.md                        [main,     70]   — generate step complete: all substeps attested, pre-deploy checklist
│   ├── deploy/
│   │   ├── entry.md                             [main,     80]   — step-entry: framing; execution order by recipe type
│   │   ├── deploy-dev.md                        [main,    110]   — substep: dev deployment flow (all targets)
│   │   ├── start-processes.md                   [main,     70]   — substep: dev-server spawn per codebase
│   │   ├── verify-dev.md                        [main,     80]   — substep: dev verification
│   │   ├── init-commands.md                     [main,     90]   — substep: migrate + seed via execOnce
│   │   ├── subagent.md                          [main,     40]   — substep-entry for feature sub-agent dispatch (showcase only)
│   │   ├── snapshot-dev.md                      [main,     50]   — substep: re-deploy to bake feature changes (showcase only)
│   │   ├── feature-sweep-dev.md                 [main,     90]   — substep: verify features exercise managed services
│   │   ├── browser-walk-dev.md                  [main,    140]   — substep: zerops_browser walks each feature (showcase only)
│   │   ├── cross-deploy-stage.md                [main,     90]   — substep: cross-deploy dev → stage
│   │   ├── verify-stage.md                      [main,     70]   — substep: stage verification
│   │   ├── feature-sweep-stage.md               [main,     80]   — substep: exercise all features on stage
│   │   ├── readmes.md                           [main,     80]   — substep-entry for writer sub-agent dispatch
│   │   └── completion.md                        [main,     80]   — deploy step complete attestation
│   ├── finalize/
│   │   ├── entry.md                             [main,    100]   — step-entry: envComments input shape
│   │   ├── env-comment-rules.md                 [main,    140]   — per-env comment authoring rules (positive allow-list for depth + ratio + voice)
│   │   ├── project-env-vars.md                  [main,     60]   — projectEnvVariables input shape
│   │   ├── service-keys-showcase.md             [main,     80]   — service key inventory (showcase only)
│   │   ├── review-readmes.md                    [main,     60]   — post-generate-finalize review
│   │   └── completion.md                        [main,     50]
│   └── close/
│       ├── entry.md                             [main,     80]   — step-entry: framing + constraints
│       ├── code-review.md                       [main,     70]   — substep-entry for code-review sub-agent dispatch
│       ├── close-browser-walk.md                [main,    100]   — substep: final stage browser walk (showcase only)
│       ├── export-on-request.md                 [main,     80]   — export + publish are user-request-only; NextSteps empty
│       └── completion.md                        [main,     50]
├── briefs/
│   ├── scaffold/
│   │   ├── mandatory-core.md                    [scaffold-sub,   80]   — file-op sequencing + SSH-only + tool-use policy (pointer-includes principles atoms)
│   │   ├── symbol-contract-consumption.md       [scaffold-sub,   90]   — how to read the injected SymbolContract JSON + fix-recurrence rules
│   │   ├── framework-task.md                    [scaffold-sub,  140]   — what the scaffold sub-agent does (framework-agnostic task shape)
│   │   ├── pre-ship-assertions.md               [scaffold-sub,  110]   — positive allow-list of pre-ship assertions (inline SSH chain, no committed file)
│   │   ├── completion-shape.md                  [scaffold-sub,   60]   — return payload structure + byte budget
│   │   ├── api-codebase-addendum.md             [scaffold-sub,   80]   — api-specific addendum (health endpoint, CORS, DB + seed)
│   │   ├── frontend-codebase-addendum.md        [scaffold-sub,   80]   — frontend-specific addendum (0.0.0.0 bind, VITE_API_URL, allowedHosts)
│   │   └── worker-codebase-addendum.md          [scaffold-sub,   80]   — worker-specific addendum (queue group, SIGTERM drain)
│   ├── feature/
│   │   ├── mandatory-core.md                    [feature-sub,    80]   — file-op sequencing + SSH-only + tool-use policy
│   │   ├── symbol-contract-consumption.md       [feature-sub,    90]   — cross-codebase contract usage (routes, DTOs, NATS subjects/queues)
│   │   ├── task.md                              [feature-sub,   180]   — single-author feature implementation across all mounts
│   │   ├── diagnostic-cadence.md                [feature-sub,    50]   — positive cadence rule: max 5 bash/min; probe taxonomy
│   │   ├── ux-quality.md                        [feature-sub,    80]   — contract discipline + install-package verification + UX standards
│   │   └── completion-shape.md                  [feature-sub,    60]   — return payload structure
│   ├── writer/
│   │   ├── mandatory-core.md                    [writer-sub,     70]   — file-op sequencing + tool-use policy
│   │   ├── fresh-context-premise.md             [writer-sub,     80]   — "you have no memory of the run"; input source declaration
│   │   ├── canonical-output-tree.md             [writer-sub,    100]   — positive allow-list: every file path the writer may touch
│   │   ├── content-surface-contracts.md         [writer-sub,    260]   — six content surfaces (per-codebase README / CLAUDE.md / IG / gotchas / env README / root README); per-surface boundary + reader contract
│   │   ├── classification-taxonomy.md           [writer-sub,    160]   — framework-invariant / framework×platform / framework-quirk / scaffold-decision / operational / self-inflicted
│   │   ├── routing-matrix.md                    [writer-sub,    120]   — explicit (classification × surface) routing rules
│   │   ├── citation-map.md                      [writer-sub,    160]   — authoritative platform topic list; citation requirement per gotcha
│   │   ├── manifest-contract.md                 [writer-sub,    120]   — ZCP_CONTENT_MANIFEST.json shape + routed_to enum
│   │   ├── self-review-per-surface.md           [writer-sub,    130]   — positive allow-list of pre-return checks per surface
│   │   └── completion-shape.md                  [writer-sub,     70]   — return payload (file byte counts + manifest summary)
│   ├── code-review/
│   │   ├── mandatory-core.md                    [code-review-sub, 70]
│   │   ├── task.md                              [code-review-sub, 180]   — framework-expert scan + silent-swallow antipattern + feature-coverage
│   │   ├── manifest-consumption.md              [code-review-sub, 80]    — reads ZCP_CONTENT_MANIFEST.json; verifies routing honesty dimensions
│   │   ├── reporting-taxonomy.md                [code-review-sub, 60]    — CRIT / WRONG / STYLE conventions + inline-fix policy
│   │   └── completion-shape.md                  [code-review-sub, 50]
│   └── editorial-review/
│       ├── mandatory-core.md                    [editorial-review-sub,  70]   — file-op + tool-use policy (Read + Grep + Glob primary; no Bash/SSH)
│       ├── porter-premise.md                    [editorial-review-sub,  80]   — "you ARE the porter"; reader-first mental model; no authorship investment
│       ├── surface-walk-task.md                 [editorial-review-sub, 120]   — ordered walk of all 7 surfaces (root/env README/env import.yaml/IG/KB/CLAUDE.md/zerops.yaml)
│       ├── single-question-tests.md             [editorial-review-sub, 100]   — one test per surface (from spec-content-surfaces.md §Per-surface test cheatsheet)
│       ├── classification-reclassify.md         [editorial-review-sub, 140]   — re-run spec's 7-class taxonomy independently; report writer-vs-reviewer delta
│       ├── citation-audit.md                    [editorial-review-sub, 100]   — spec §Citation map enforcement; every matching-topic gotcha cites
│       ├── counter-example-reference.md         [editorial-review-sub, 160]   — spec §Counter-examples from v28 (anti-pattern library for reviewer pattern-matching)
│       ├── cross-surface-ledger.md              [editorial-review-sub,  90]   — running fact-ledger across surfaces; flag cross-surface duplication
│       ├── reporting-taxonomy.md                [editorial-review-sub,  80]   — CRIT (wrong-surface) / WRONG (boundary + fabrication + uncited) / STYLE + inline-fix policy
│       └── completion-shape.md                  [editorial-review-sub,  60]   — return payload: CRIT/WRONG/STYLE counts + reclassification delta + per-surface walk summary
└── principles/
    ├── where-commands-run.md                    [any,             90]   — SSH-only container-execution boundary; positive form
    ├── file-op-sequencing.md                    [any,             60]   — Read-before-Edit; batch-read before first Edit
    ├── tool-use-policy.md                       [any,             70]   — base permit + forbid lists (role overrides declared in briefs/)
    ├── symbol-naming-contract.md                [any,            240]   — SymbolContract schema + fix-recurrence rule list + consumption conventions
    ├── todowrite-mirror-only.md                 [any,             60]   — TodoWrite is a mirror, not a plan
    ├── fact-recording-discipline.md             [any,            130]   — when + how to record_fact; scope + routed_to; classification moment
    ├── platform-principles/
    │   ├── 01-graceful-shutdown.md              [any,             80]
    │   ├── 02-routable-bind.md                  [any,             60]
    │   ├── 03-proxy-trust.md                    [any,             60]
    │   ├── 04-competing-consumer.md             [any,             90]
    │   ├── 05-structured-creds.md               [any,            100]
    │   └── 06-stripped-build-root.md            [any,             70]
    ├── dev-server-contract.md                   [any,             80]   — dev-server tool interface + error-class taxonomy
    ├── comment-style.md                         [any,             60]   — positive ASCII-only style for YAML comments
    ├── visual-style.md                          [any,             50]   — positive ASCII-only convention for any agent-authored text
    └── canonical-output-paths.md                [any,             80]   — positive declaration of every file path the system may write
```

**Atom count**: **96 atoms** (86 original + 10 editorial-review atoms added per research-refinement 2026-04-20). Upper bound at 300 lines each → 28,800 lines if every atom maxed; realistic expectation ~7,400 lines total (editorial-review adds ~1,000 lines on top of the ~6,500-line baseline), still a ~1.7× reduction from the 3,438-line monolith when deduplication is applied.

**Atomicity checks**:
- No atom lists >1 audience. `principles/` atoms carry `audience: any` as a pointer-include marker; they are consumed by briefs via stitch-time concatenation, not transmitted as dispatcher-and-sub-agent mix.
- No atom exceeds 300 lines. The three largest (content-surface-contracts @ 260, symbol-naming-contract @ 240, dual-runtime-consumption @ 200) are expected maxes; if step-4 simulation pressures any of them past 300, they split.
- No atom contains version anchors. Build-time grep guard (per P6) fails the build if matches.

---

## 2. Audience separation (principle P2 enforcement)

| Tree prefix | Audience | Stitched by |
|---|---|---|
| `phases/` | main | `recipe_guidance.go buildSubStepGuide` — delivered at step-entry + substep-complete returns |
| `briefs/scaffold/` | scaffold-sub | `buildScaffoldDispatchBrief(SymbolContract, codebase)` — transmitted as Agent prompt |
| `briefs/feature/` | feature-sub | `buildFeatureDispatchBrief(SymbolContract, features)` — transmitted as Agent prompt |
| `briefs/writer/` | writer-sub | `buildWriterDispatchBrief(plan, factsLogPath, citationMap)` — transmitted as Agent prompt |
| `briefs/code-review/` | code-review-sub | `buildCodeReviewDispatchBrief(plan, manifestPath)` — transmitted as Agent prompt |
| `briefs/editorial-review/` | editorial-review-sub | `buildEditorialReviewDispatchBrief(plan, manifestPath, factsLogPath)` — transmitted as Agent prompt; dispatched at close.editorial-review substep (showcase gated, minimal discretionary) |
| `principles/` | pointer-include only | concatenated at stitch time into the consuming atom's output; never transmitted standalone |

**DISPATCH.md** (outside `internal/content/workflows/recipe/`): human-facing dispatch composition guidance. Lives at `docs/zcprecipator2/DISPATCH.md`. Not readable by the Go emission layer. Covers: how to compose a scaffold dispatch from `briefs/scaffold/*`, how to interpolate `SymbolContract`, how to handle single-codebase vs multi-codebase branching. This is the P2 physical-separation artifact — the ONE place dispatcher instructions live.

---

## 3. Current block → new atom(s) mapping

The left column lists every named `<block>` currently in `internal/content/workflows/recipe.md` with its line-range citation. The right column lists the atoms it decomposes into. "→ split" = the current block maps to ≥2 atoms; "→ principle" = moved into `principles/`; "→ delete" = no longer required under new architecture.

### Block → atom decomposition

| Current block | Lines | → new atom(s) |
|---|---|---|
| `provision-framing` | 159–163 | → `phases/provision/entry.md` |
| `import-yaml-standard-mode` | 165–194 | → `phases/provision/import-yaml/standard-mode.md` |
| `import-yaml-static-frontend` | 196–211 | → `phases/provision/import-yaml/static-frontend.md` |
| `import-yaml-workspace-restrictions` | 211–217 | → `phases/provision/import-yaml/workspace-restrictions.md` |
| `import-yaml-framework-secrets` | 217–233 | → `phases/provision/import-yaml/framework-secrets.md` |
| `import-yaml-dual-runtime` | 233–257 | → `phases/provision/import-yaml/dual-runtime.md` |
| `provision-schema-inline` | 259–292 | → split: `phases/provision/entry.md` (field overview) + `phases/provision/import-yaml/standard-mode.md` (canonical example) |
| `import-services-step` | 294–304 | → `phases/provision/import-services-step.md` |
| `mount-dev-filesystem` | 306–317 | → `phases/provision/mount-dev-filesystem.md` |
| `git-config-mount` | 319–345 | → split: `phases/provision/git-config-container-side.md` (main-agent) + `principles/where-commands-run.md` (pointer: SSH-only rule) |
| `git-init-per-codebase` | 347–351 | → `phases/provision/git-init-per-codebase.md` |
| `env-var-discovery` | 353–375 | → `phases/provision/env-var-discovery.md` |
| `provision-attestation` | 377–384 | → split: `phases/provision/provision-attestation.md` + `phases/provision/completion.md` |
| `container-state` | 390–409 | → `phases/generate/entry.md` |
| `where-to-write-files-single` | 411–420 | → `phases/generate/scaffold/where-to-write-single.md` |
| `where-to-write-files-multi` | 422–444 | → `phases/generate/scaffold/where-to-write-multi.md` |
| `what-to-generate-showcase` | 446–462 | → `phases/generate/app-code/dashboard-skeleton-showcase.md` (showcase); `phases/generate/app-code/execution-order-minimal.md` (minimal) |
| `two-kinds-of-import-yaml` | 462–473 | → `phases/provision/entry.md` (cross-reference; disambiguation belongs up-phase) |
| `execution-order` | 473–493 | → `phases/generate/app-code/execution-order-minimal.md` |
| `generate-schema-pointer` | 495–507 | → `phases/generate/zerops-yaml/entry.md` |
| `zerops-yaml-header` | 507–528 | → `phases/generate/zerops-yaml/entry.md` |
| `dual-runtime-url-shapes` | 530–567 | → `phases/generate/zerops-yaml/dual-runtime-consumption.md` |
| `dual-runtime-consumption` | 567–657 | → `phases/generate/zerops-yaml/dual-runtime-consumption.md` (this is the atom max-line driver — already sized at 200 to fit) |
| `project-env-vars-pointer` | 657–663 | → cross-reference to `phases/finalize/project-env-vars.md` |
| `dual-runtime-what-not-to-do` | 663–672 | → folded positively into `phases/generate/zerops-yaml/setup-rules-dev.md` (positive form per P8) |
| `setup-dev-rules` | 672–690 | → `phases/generate/zerops-yaml/setup-rules-dev.md` |
| `serve-only-dev-override` | 690–705 | → `phases/generate/zerops-yaml/setup-rules-static-frontend.md` |
| `dev-dep-preinstall` | 705–711 | → folded into `phases/generate/zerops-yaml/setup-rules-dev.md` |
| `dev-server-host-check` | 711–715 | → `phases/generate/scaffold/dev-server-host-check.md` |
| `setup-prod-rules` | 717–727 | → `phases/generate/zerops-yaml/setup-rules-prod.md` |
| `worker-setup-block` | 727–738 | → `phases/generate/zerops-yaml/setup-rules-worker.md` + `principles/platform-principles/04-competing-consumer.md` |
| `shared-across-setups` | 738–746 | → `phases/generate/zerops-yaml/env-var-model.md` |
| `env-example-preservation` | 746–754 | → `principles/symbol-naming-contract.md` (contract rule) + `briefs/scaffold/pre-ship-assertions.md` |
| `framework-env-conventions` | 754–762 | → `phases/generate/zerops-yaml/env-var-model.md` |
| `dashboard-skeleton` | 762–790 | → `phases/generate/app-code/dashboard-skeleton-showcase.md` |
| **`scaffold-subagent-brief`** (336 lines) | 790–1125 | → **split 8 ways**: `briefs/scaffold/mandatory-core.md` + `briefs/scaffold/symbol-contract-consumption.md` + `briefs/scaffold/framework-task.md` + `briefs/scaffold/pre-ship-assertions.md` + `briefs/scaffold/completion-shape.md` + addenda (api/frontend/worker) + `principles/platform-principles/*` (six atoms). Dispatcher instructions embedded in source (L843–889) → extracted to `docs/zcprecipator2/DISPATCH.md` (never transmitted). |
| `asset-pipeline-consistency` | 1127–1140 | → `briefs/scaffold/framework-task.md` (UX-quality subsection) |
| `code-quality` | 1140–1151 | → `briefs/scaffold/framework-task.md` |
| `init-script-loud-failure` | 1151–1175 | → `briefs/scaffold/framework-task.md` |
| `client-code-observable-failure` | 1175–1246 | → `briefs/scaffold/framework-task.md` + `briefs/scaffold/frontend-codebase-addendum.md` |
| `pre-deploy-checklist` | 1246–1263 | → `phases/generate/completion.md` |
| `on-container-smoke-test` | 1263–1299 | → `phases/generate/smoke-test/on-container-smoke-test.md` |
| `comment-anti-patterns` | 1301–1316 | → `principles/comment-style.md` (positive form — ASCII-only) |
| `completion` (generate) | 1316–1326 | → `phases/generate/completion.md` |
| **Fragment Quality Requirements** (non-block header) | 1327–1415 | → `briefs/writer/content-surface-contracts.md` + `briefs/writer/self-review-per-surface.md` |
| `deploy-framing` | 1417–1423 | → `phases/deploy/entry.md` |
| `fact-recording-mandatory` | 1423–1466 | → `principles/fact-recording-discipline.md` |
| `deploy-execution-order` | 1468–1488 | → `phases/deploy/entry.md` |
| `deploy-core-universal` | 1488–1555 | → `phases/deploy/deploy-dev.md` + `phases/deploy/start-processes.md` |
| Two execOnce keys subsection | 1555–1581 | → `phases/generate/zerops-yaml/seed-execonce-keys.md` |
| `deploy-api-first` | 1581–1612 | → `phases/deploy/deploy-dev.md` (dual-runtime API-first ordering) |
| `deploy-asset-dev-server` | 1612–1636 | → `phases/deploy/start-processes.md` |
| `deploy-worker-process` | 1636–1652 | → `phases/deploy/start-processes.md` + `briefs/scaffold/worker-codebase-addendum.md` |
| `deploy-target-verification` | 1652–1675 | → `phases/deploy/verify-dev.md` + `phases/deploy/verify-stage.md` |
| **`dev-deploy-subagent-brief`** (154 lines) | 1675–1828 | → **split 6 ways**: `briefs/feature/mandatory-core.md` + `briefs/feature/symbol-contract-consumption.md` + `briefs/feature/task.md` + `briefs/feature/diagnostic-cadence.md` + `briefs/feature/ux-quality.md` + `briefs/feature/completion-shape.md`. Dispatcher lines → DISPATCH.md. |
| Feature pre-ship subsection | 1724–1830 | → `briefs/feature/task.md` + pre-ship block into `briefs/feature/completion-shape.md` |
| `where-commands-run` | 1830–1872 | → `principles/where-commands-run.md` |
| `feature-sweep-dev` | 1874–1918 | → `phases/deploy/feature-sweep-dev.md` |
| `dev-deploy-browser-walk` | 1918–2024 | → `phases/deploy/browser-walk-dev.md` |
| `browser-command-reference` | 2026–2057 | → `phases/deploy/browser-walk-dev.md` (subsection: command vocabulary) |
| `stage-deployment-flow` | 2057–2127 | → `phases/deploy/cross-deploy-stage.md` |
| `reading-deploy-failures` | 2127–2149 | → `phases/deploy/entry.md` (subsection: failure-class decoder) |
| `feature-sweep-stage` | 2149–2187 | → `phases/deploy/feature-sweep-stage.md` |
| `common-deployment-issues` | 2187–2205 | → **deleted** — principle P8 forbids broad enumerated-prohibition lists; specific cases fold into per-substep positive guidance |
| **`readme-with-fragments`** (184 lines) | 2205–2388 | → **rewritten to v8.94 shape**: `briefs/writer/*` (all writer atoms). Minimal-tier writer uses the same atomic tree with tier-conditional sections. |
| **`content-authoring-brief`** (347 lines) | 2390–2736 | → **split 10 ways**: `briefs/writer/mandatory-core.md` + `fresh-context-premise.md` + `canonical-output-tree.md` + `content-surface-contracts.md` + `classification-taxonomy.md` + `routing-matrix.md` + `citation-map.md` + `manifest-contract.md` + `self-review-per-surface.md` + `completion-shape.md`. |
| `deploy-completion` | 2738–2749 | → `phases/deploy/completion.md` |
| `env-comment-rules` | 2760–2815 | → `phases/finalize/env-comment-rules.md` |
| `env-comments-example` | 2817–2873 | → `phases/finalize/env-comment-rules.md` (canonical-example subsection) |
| `showcase-service-keys` | 2873–2889 | → `phases/finalize/service-keys-showcase.md` |
| `project-env-vars` | 2889–2943 | → `phases/finalize/project-env-vars.md` |
| `review-readmes` | 2943–2952 | → `phases/finalize/review-readmes.md` |
| `comment-voice` | 2952–3019 | → `principles/comment-style.md` + `phases/finalize/env-comment-rules.md` (voice-specific rules) |
| `finalize-completion` | 3019–3031 | → `phases/finalize/completion.md` |
| Close § (non-block) + Constraints | 3031–3050 | → `phases/close/entry.md` |
| **`code-review-subagent`** (109 lines) | 3050–3158 | → **split 5 ways**: `briefs/code-review/mandatory-core.md` + `task.md` + `manifest-consumption.md` + `reporting-taxonomy.md` + `completion-shape.md` |
| `close-browser-walk` | 3160–3191 | → `phases/close/close-browser-walk.md` |
| `export-publish` | 3193–3288 | → `phases/close/export-on-request.md` |
| `close-completion` | 3288–3303 | → `phases/close/completion.md` |
| Minimal-tier sections (non-block, L3304–3330+) | 3304–3438 | → folded into tier-branched atoms (`where-to-write-single.md`, `execution-order-minimal.md`, `dashboard-skeleton-showcase.md`) + each phase's `entry.md` handles tier branching inline |

### Unmapped / deleted

| Current artifact | Disposition | Reason |
|---|---|---|
| Dispatcher instructions inside `scaffold-subagent-brief` L843–889 | → `docs/zcprecipator2/DISPATCH.md` | P2: dispatcher lives outside transmission |
| Dispatcher instructions inside `dev-deploy-subagent-brief` | → `docs/zcprecipator2/DISPATCH.md` | P2 |
| Version anchors scattered throughout (v25, v8.85, v33, ...) | → deleted | P6: archive holds version history |
| Internal check-name references in briefs (e.g. "the `writer_manifest_honesty` check catches X") | → replaced with "the pre-attest command for the manifest consistency check is …" | P2: no internal vocabulary in transmitted briefs |
| Go-source file references in briefs (e.g. `internal/workflow/recipe_templates.go`) | → deleted | P2 |
| `common-deployment-issues` enumerated-prohibition list | → deleted | P8 |

### New role — editorial-review (no current-block predecessor)

`briefs/editorial-review/*` is a **genuinely new role** with no current-block origin. Its 10 atoms are first authored under the refinement pass (2026-04-20) rather than decomposed from an existing `recipe.md` block. Source of truth for atom content:

- [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) §Six content surfaces → `surface-walk-task.md` + `single-question-tests.md`
- [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) §Fact classification taxonomy → `classification-reclassify.md`
- [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) §Counter-examples from v28 → `counter-example-reference.md`
- [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) §Citation map → `citation-audit.md`
- `principles/where-commands-run.md`, `principles/file-op-sequencing.md`, `principles/tool-use-policy.md` → pointer-includes via `mandatory-core.md`

The role closes a gap the spec itself prescribes (spec line 317-319: *"Editorial review (human or agent) walks each surface and applies the one-question test per item; removes anything that fails"*). Not dispatched in the current system; writer self-review absorbs the role, collapsing author and judge.

---

## 4. SymbolContract — derivation + plumbing

The new `SymbolContract` plan-field carries the cross-codebase contract computed once per recipe. Lives in `plan.Research.SymbolContract` (structured JSON). Consumed by scaffold + feature + writer briefs.

### Schema (conceptual — full type goes in `internal/workflow/recipe_plan.go`)

```go
type SymbolContract struct {
    // Env var names per service kind (db, cache, queue, storage, search).
    // Keys are the PLATFORM-provided names the container sees.
    EnvVarsByKind map[string]map[string]string  // {"db": {"host":"DB_HOST","port":"DB_PORT","user":"DB_USER","pass":"DB_PASS","name":"DB_NAME"}}

    // Cross-codebase route paths: declared once, read by all codebases.
    HTTPRoutes map[string]string   // {"status":"/api/status","items":"/api/items",...}

    // NATS subject + queue naming — producer publishes to subject, consumer subscribes with queue.
    NATSSubjects map[string]string // {"job_dispatch":"jobs.dispatch"}
    NATSQueues   map[string]string // {"workers":"workers"}

    // Hostname conventions — one per target.
    Hostnames []HostnameEntry      // [{role:"api",dev:"apidev",stage:"apistage"},...]

    // DTO interface names that must match across codebases.
    DTOs []string

    // Fix-recurrence rules — a list of "scaffold-phase MUST-DO" items that
    // close past recurrence classes. Each rule has a positive form + a
    // pre-attest runnable command.
    FixRecurrenceRules []FixRule
}

type FixRule struct {
    ID            string  // e.g. "nats-separate-user-pass"
    PositiveForm  string  // "pass user + pass as separate ConnectionOptions fields"
    PreAttestCmd  string  // "grep -n 'nats://.*:.*@' src/**/*.{ts,js} && exit 1 || exit 0"
    AppliesTo     []string // hostname roles this rule applies to (e.g. ["api","worker"])
}
```

### Seeded FixRecurrenceRules (v20–v34 recurrence surface)

| ID | Positive form | Applies to |
|---|---|---|
| `nats-separate-creds` | pass user + pass as separate ConnectionOptions fields; `servers` is `${queue_hostname}:${queue_port}` only | api, worker |
| `s3-uses-api-url` | S3 client `endpoint` is `process.env.storage_apiUrl` (https://) not `storage_apiHost` (http redirect) | api |
| `s3-force-path-style` | S3 client `forcePathStyle: true` | api |
| `routable-bind` | HTTP servers bind `0.0.0.0` not `localhost` | api, frontend |
| `trust-proxy` | Express/Fastify `set trust proxy 1` or equivalent for L7 balancer IP forwarding | api |
| `graceful-shutdown` | worker + api register SIGTERM → drain → exit; api calls `app.enableShutdownHooks()` if Nest | api, worker |
| `queue-group` | NATS subscribers declare `queue: '<contract.NATSQueues[role]>'` | worker |
| `env-self-shadow` | no `key: ${key}` lines in `run.envVariables` | any |
| `gitignore-baseline` | `.gitignore` contains `node_modules`, `dist`, `.env`, `.DS_Store`, framework-specific cache dirs | any |
| `env-example-preserved` | framework-scaffolder's `.env.example` kept if present | any |
| `no-scaffold-test-artifacts` | no `preship.sh` / `.assert.sh` / self-test shell scripts committed | any |
| `skip-git` | framework scaffolders invoked with `--skip-git` OR `ssh {hostname} "rm -rf /var/www/.git"` after scaffolder returns | any |

Each rule's `PreAttestCmd` is executable against the scaffold sub-agent's mount via SSH. Scaffold sub-agent's `completion-shape.md` includes "Before returning, run each `PreAttestCmd` for rules matching your hostname role. Non-zero exit = fix before return."

### Plumbing

- `recipe_plan.go` adds `SymbolContract`.
- `research-completion` step computes the contract from `plan.Research.Targets` + managed services + tier. The computation lives in `internal/workflow/symbol_contract.go` (a new file, ≤200 lines, tested independently).
- `recipe_guidance.go buildScaffoldDispatchBrief` interpolates `{{.SymbolContract | toJSON}}` into `briefs/scaffold/symbol-contract-consumption.md` at stitch time, producing byte-identical JSON across all N scaffold dispatches.
- Feature + writer briefs interpolate a subset of the contract (feature: HTTPRoutes + DTOs + NATS*; writer: full contract for citation consistency).

---

## 5. Atomic-layout invariants audit

| Invariant | Status |
|---|---|
| Every atom ≤ 300 lines | ✅ tree sized within bounds; largest atoms (content-surface-contracts 260, symbol-naming-contract 240, dual-runtime-consumption 200, editorial-review counter-example-reference 160, editorial-review classification-reclassify 140) are intentional expected maxes |
| Every atom names exactly one audience | ✅ audience column declared per atom; `principles/` is pointer-included not multi-audience; editorial-review atoms all `editorial-review-sub` |
| `briefs/` atoms never contain dispatcher text | ✅ dispatcher text extracted to `docs/zcprecipator2/DISPATCH.md` (editorial-review dispatch composition added to DISPATCH.md in rollout-sequence C-12) |
| No version anchors in any atom | ✅ build-time grep guard (principle P6); current source has ~80 version-anchor matches, all go to archive; editorial-review `counter-example-reference.md` cites v28 concrete anti-patterns by **behavior description**, not version anchor (e.g. "NestJS setGlobalPrefix shipped as Zerops gotcha" not "v28 apidev gotcha #5") |
| Every current block has a disposition (split / move / delete) | ✅ §3 table covers every block in recipe.md; unmapped items flagged in §3.2; editorial-review atoms genuinely new per §3.3 |
| Negative enumeration-based prohibitions refactored to positive allow-lists (P8) | ✅ `common-deployment-issues` deleted; `dual-runtime-what-not-to-do` folded positively; `comment-anti-patterns` → `comment-style.md` positive form; `close-browser-walk` "what to avoid" section → positive allow-list; visual-style forbidden list → `visual-style.md` positive form; editorial-review reporting-taxonomy declares positive form (CRIT/WRONG/STYLE semantics + when to inline-fix) not forbidden-verdict enumeration |
| Every atom's audience matches exactly one of {main, scaffold-sub, feature-sub, writer-sub, code-review-sub, editorial-review-sub, any} | ✅ |

---

## 6. Stitching conventions (guidance.go rewrite surface)

Each stitching function composes a response from atoms. Summary of the emission surface (implementation detail lives outside step 3 scope):

| Stitcher | Output | Inputs |
|---|---|---|
| `buildStepEntry(phase)` | step-entry guide | `phases/<phase>/entry.md` + substep entries + applicable principles |
| `buildSubStepCompletion(phase, substep)` | substep-complete return | `phases/<phase>/<substep>/completion.md` (attest predicate) + next substep's entry (if any) + applicable principles |
| `buildScaffoldDispatchBrief(contract, codebase)` | Agent prompt | `briefs/scaffold/mandatory-core.md` + `symbol-contract-consumption.md` with contract JSON + `framework-task.md` + `pre-ship-assertions.md` + `completion-shape.md` + role-specific addendum (api/frontend/worker) + required platform principles + Prior Discoveries block |
| `buildFeatureDispatchBrief(contract, features)` | Agent prompt | `briefs/feature/*` + relevant platform principles + Prior Discoveries block |
| `buildWriterDispatchBrief(plan, factsPath)` | Agent prompt | `briefs/writer/*` + canonical citation map + Prior Discoveries block |
| `buildCodeReviewDispatchBrief(plan)` | Agent prompt | `briefs/code-review/*` + Prior Discoveries block + manifest path |
| `buildEditorialReviewDispatchBrief(plan, factsLogPath, manifestPath)` | Agent prompt | `briefs/editorial-review/mandatory-core.md` + `porter-premise.md` + `surface-walk-task.md` + `single-question-tests.md` + `classification-reclassify.md` + `citation-audit.md` + `counter-example-reference.md` + `cross-surface-ledger.md` + `reporting-taxonomy.md` + `completion-shape.md` + pointer-include `principles/where-commands-run.md` + `principles/file-op-sequencing.md` + `principles/tool-use-policy.md` + interpolate `{factsLogPath, manifestPath}` (verbatim pointers — editorial reviewer reads both). Prior Discoveries block is **NOT** included (per P7 + porter-premise — reviewer carries no authorship investment; fresh reader of the deliverable only) |

Stitch-time concatenation performs zero content transformation — atoms are concatenated verbatim with `\n---\n` separators. Interpolation uses Go templates with explicit named fields (`.SymbolContract`, `.Features`, `.FactsLogPath`).

---

## 7. Tier branching under the atomic layout

Per README constraint: minimal and showcase are first-class tier flows sharing atoms. Branching is declared per atom (not per brief). Tier-conditional inclusion is handled by:

- `phases/generate/app-code/dashboard-skeleton-showcase.md` (included when `plan.Tier == Showcase`)
- `phases/generate/app-code/execution-order-minimal.md` (included when `plan.Tier == Minimal`)
- `phases/deploy/subagent.md` + `phases/deploy/snapshot-dev.md` + `phases/deploy/browser-walk-dev.md` (showcase only — substeps gated by `isShowcase` in `recipe_substeps.go`)
- `phases/generate/scaffold/where-to-write-single.md` vs `where-to-write-multi.md` (multi-codebase gated)
- `briefs/scaffold/*` dispatched only when `multiCodebase`; minimal-single-codebase tier writes scaffold inline from main (no sub-agent dispatch)
- `briefs/editorial-review/*` + `phases/close/editorial-review.md` — dispatched at close.editorial-review (showcase: gated substep; minimal: ungated-discretionary matching close.code-review, default-on)

Tier-conditional **atoms**, not tier-conditional **blocks-within-atoms**: the atom is indivisible. Each atom either lands for this tier or it doesn't. That keeps each atom a single audience + single concern.

---

## 8. Blast-radius verification

An atom change has bounded consequences:

| Atom | Consumers |
|---|---|
| `principles/where-commands-run.md` | every brief + `phases/provision/git-config-container-side.md` + `phases/generate/smoke-test/on-container-smoke-test.md` |
| `principles/platform-principles/04-competing-consumer.md` | `briefs/scaffold/worker-codebase-addendum.md` + `phases/generate/zerops-yaml/setup-rules-worker.md` + `briefs/feature/task.md` |
| `phases/generate/zerops-yaml/env-var-model.md` | main only (delivered at substep entry) |
| `briefs/scaffold/symbol-contract-consumption.md` | scaffold-sub only |
| `briefs/writer/content-surface-contracts.md` | writer-sub only |
| `briefs/editorial-review/single-question-tests.md` | editorial-review-sub only |
| `briefs/editorial-review/classification-reclassify.md` | editorial-review-sub only |
| `briefs/editorial-review/counter-example-reference.md` | editorial-review-sub only |

Each atom's blast radius is inspectable by grep for its filename across `recipe_guidance.go` + other atom bodies (via pointer-includes). A change to a `principles/` atom affects exactly the consumers who `{{ include "principles/X.md" }}` — a static count that can be printed at dispatch time.

---

## 9. Non-atom artifacts under the new architecture

| Artifact | Path | Purpose |
|---|---|---|
| `DISPATCH.md` | `docs/zcprecipator2/DISPATCH.md` | Human-facing dispatch composition guide (P2) |
| Topic registry | `internal/workflow/atom_manifest.go` | Replaces `recipe_topic_registry.go` with atom path manifest |
| Facts log | `/tmp/zcp-facts-{sessionID}.jsonl` (unchanged) | Substrate; fact schema adds `RouteTo` field per P5 |
| Content manifest | `<mount-root>/ZCP_CONTENT_MANIFEST.json` (unchanged) | Substrate; schema adds every `routed_to` dimension |
| SymbolContract JSON | inlined into scaffold + feature + writer dispatch prompts | New — P3 carrier |

---

## 10. Open layout-level questions (deferred to step 4 + 5)

1. **Should `content-surface-contracts.md` at 260 lines split?** Seven surfaces (per spec-content-surfaces.md) × ~35-40 lines each = 245-280. Refinement 2026-04-20 strongly recommends splitting given editorial-review atoms consume the same per-surface specs + v28 counter-examples — split into seven atoms (one per surface, ~60-80 lines each) that writer AND editorial-review both pointer-include. Deferred to implementation C-4 decision per user preference.
2. **Does `symbol-naming-contract.md` at 240 lines need to split?** Contract schema + fix-recurrence rules + consumption conventions — three logical sections. Step-4 may split into `symbol-contract-schema.md` + `fix-recurrence-rules.md` + `contract-consumption-conventions.md`.
3. **How does `briefs/writer/citation-map.md` + `briefs/editorial-review/citation-audit.md` acquire their topic list?** Current recipe.md hardcodes a list. New architecture: the citation map is computed from `plan.Research.managedServices` + `internal/knowledge/guides/` index + spec-content-surfaces.md §Citation map (8 topic areas). Implementation is a `buildCitationMap()` helper shared by writer + editorial-review stitchers. Step-4 validates.
4. **Tier-branching in atoms named `-minimal.md` vs `-showcase.md`** — is duplication acceptable? For app-code (two different content skeletons) yes. For setup-rules (overlap between showcase and minimal except worker) prefer one atom with conditional section. Step-4 simulates both.
5. **Editorial-review dispatch ordering relative to code-review at close** — sequential (editorial → code-review → browser-walk) vs parallel (editorial + code-review dispatched together, browser-walk waits on both). Refinement recommends **sequential editorial-first**: editorial catches wrong-surface items that require content revision; code-review then scans framework-correctness on revised content. Wall cost: +8–10 min vs parallel. Deferred to implementation C-7.5 + C-5 stitcher decision.
6. **Should editorial-review receive Prior Discoveries block?** Refinement recommendation: **NO**. Prior Discoveries carries scaffold + feature recorded facts — useful for a reviewer *of the process* but contaminating for a reviewer *of the deliverable*. Editorial reviewer's porter-premise requires reading the shipped content cold. The `factsLogPath` + `manifestPath` are provided as *pointers* the reviewer may open if needed, not as pre-stitched context. Deferred to C-7.5 stitcher signature decision.
