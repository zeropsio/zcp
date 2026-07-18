## Verdict summary

The register is strong, but its theme order is not a good implementation order.

I would use:

1. Immediate harmful-truth containment.
2. Structural enforcement and delivery seams.
3. Remaining retrieval/content correctness.
4. Duplication reduction where it removes contradictions or measurable payload cost.
5. Residual spec hygiene.

So: neither “all T1 first” nor “all T3 first.” Fix the dangerous live claims immediately—dirty-tree deployment, destructive READY_TO_DEPLOY recovery, Valkey auth, credential scope, destructive database advice—then establish the enforcement ratchet before processing the long tail.

T3 cannot prevent most T1 regressions. Regexes and fixture tests will not prove Valkey auth, scale limits, or what `zcli --no-git` ships. Those require single ownership, derivation from executable policy, or upstream provenance. Enforcement-first only protects structural defects such as missing axes, dangling references, dead allowlists, and unreachable delivery.

The biggest architectural recommendation is to treat this as a small corpus compiler: typed indexes, graph validation, production delivery witnesses, and structured diagnostics. That replaces most proposed one-off drift tests without turning them into one opaque mega-test.

## Answers 1–6

### 1. Is T1→T6 sane?

As review taxonomy, yes. As priority order, no.

My priority bands would be:

- P0: T1.1, T1.2, T1.3, T1.7, T1.9, and T2.2. These can expose secrets, wipe source, break deployment, provoke destructive recreation, or strand a required recovery.
- P1: T3 structural ratchets, T2 launch/retrieval delivery, recipe viability, and T6’s ES-lag and `.git`-suffix gaps. Those operational gaps are more consequential than most duplication cleanup.
- P2: remaining factual corrections and selector/co-render defects.
- P3: duplication, stale prose, and spec cleanup without behavioral impact.

Several findings are miscategorized or have the wrong proposed shape:

- T1.4 is primarily a selector/delivery defect: the wrong route receives the atom. Fix the route model and test the resulting render, not merely the prose.
- T1.8 is two defects: overdelivery from an axis-free atom and a duplicated tool contract. First make `zerops_knowledge` honest, then delete or narrowly scope the atom’s restatement.
- T1.9 belongs under ownership/composition, not point-content correction. One policy must own READY_TO_DEPLOY recovery and branch on build history.
- T1.10 needs an executable classification policy. Do not arbitrarily choose which of two prose claims survives; code should classify platform-owned envs and content should reflect that result.
- T1.13 should derive every recovery hint from `ValidateHostname` or its typed constraint, not copy `40` into another string.
- T2.7 should delete `runtimeRecipeHints`, not add a drift test to preserve it. Derive runtime associations from recipe metadata and companion import YAML.
- T2.10 should narrow the API contract. `stage` is a service role, not a bootstrap plan mode, and the code has no meaningful stage-filtered briefing. Reject or remove the affordance instead of inventing behavior.
- T3.9 is a Markdown/tokenization problem. Escaping individual table pipes would be content churn.
- T4.7 should default to deleting the phantom SSOT and stale tests unless a real production consumer is being added now.

### 2. Launch handlers or `launchStatus`?

Use `launchStatus` long-term.

The typed enum already exists in [types.go](/Users/macbook/Documents/Zerops-MCP/zcp/internal/topology/types.go:239), and its comment explicitly anticipates `launchStatus:` atom filtering. The missing field in [envelope.go](/Users/macbook/Documents/Zerops-MCP/zcp/internal/workflow/envelope.go:19) is unfinished architecture, not a reason to institutionalize handler-by-ID delivery.

The target shape:

- Add `LaunchStatus` to `StateEnvelope`.
- Add `LaunchStatuses` to `AxisVector`, parsing, matching, cross-reference comparison, and completeness classification.
- Construct a real launch envelope for every handler status.
- Route invariant/static guidance through `Synthesize`.
- Append dynamic blocker details—hostnames, API errors, target-specific recovery—from the handler.
- Test production response constructors, not synthetic scenario-only envelopes.

Likely assignments are:

- `launch-intro` → `scope-prompt`
- HA assessment → scope/preview state where the choice is actionable
- classification atoms → `classify-prompt`
- source-control catalog → `source-control-required`
- pipeline configuring → `configuring-pipeline`

For T2.2, flipping the catalog to `reference:true` is reasonable only because the current response labels it optional depth. But the systemic fix is a typed atom-fetch constructor that refuses non-reference IDs; making the literal happen to resolve is insufficient.

### 3. Reflection completeness or generated dispatch?

Use a reflection-backed, test-only classification registry. Do not generate runtime matching.

Classify every `AxisVector` field exactly once as:

- envelope-scoped,
- service-scoped, or
- render modifier.

Then assert via reflection that the registry exactly equals the struct fields. Reuse that registry in the co-render comparison tests where practical.

Generating the actual matcher is over-engineering because the axes have heterogeneous semantics: bootstrap-pointer checks, “any managed service” predicates, derived deploy state, per-service matching, and scalar modifiers. A generated generic matcher would either obscure those semantics or grow a second mini-language.

For broader corpus integrity, use one architecture with separate named validators:

| Current proposal | Better systemic mechanism |
|---|---|
| Recipe URI parity test | Kind-aware canonical resolver, table-tested across every recipe |
| Hint-map drift test | Delete the map; derive the index from recipe metadata/import YAML |
| Matcher corpus-drift test | Recipe metadata schema plus canonical aliases and real-corpus intent cases |
| Reachability test | Production delivery witnesses per content unit |
| Allowlist liveness | Suppression entries report hit counts; zero-hit suppressions fail |
| Go-literal URI lint | Typed `AtomRef`/fetch constructor, plus a narrow ban on raw literals |
| Axis completeness | Reflection-backed axis classification registry |

This should be a `CorpusSnapshot` plus validators producing structured diagnostics, not one enormous `TestEverything`. Package-local adapters can feed the snapshot so production dependency layering remains intact.

### 4. What mechanism fits the shared classification protocol?

Use a dedicated inline fragment/partial tier.

A shared pull reference is wrong: this is load-bearing interaction protocol and should not depend on another agent fetch. A “shared atom” manually selected by two handlers is a fragment disguised as a delivery unit; it acquires fake axes, reachability obligations, and handler coupling.

The fragment should:

- have no phase, axes, URI, or independent delivery identity;
- be included by thin export and launch atoms;
- expand before lint and rendering;
- reject missing includes and cycles;
- be required to have at least two consumers;
- preserve source mapping in diagnostics.

This mechanism is justified by a roughly 100-line, already-drifted duplicated block. It should not become the tool for deduplicating every repeated sentence.

### 5. Slice per theme or per finding?

Neither by default. Slice by mechanism and authority boundary.

Good batch boundaries:

- T3.1–4 and T3.8–10: lint rule registry, self-tests, suppression accounting.
- T3.5 plus local dangling dependencies: cross-atom delivery graph.
- T2.6–8: recipe discovery metadata/index, although viability lint should remain separate.
- T4.2: bootstrap composition.
- T4.3–4 and T6.3: develop close/delivery composition.
- T4.1, T1.10, and the five-bucket spec drift: classification protocol/policy.
- T5: update the relevant spec inside each behavioral slice; do not save all specs for one late rewrite.

Bad batch boundaries:

- all of T1 in one content commit,
- all of T2 in one delivery commit,
- all duplication in one rewrite,
- all specs at the end.

### 6. What is still missing?

Beyond the six critic gaps, I would add:

- Agent-facing example conformance. Parse internal `zerops_*` tool-call examples across atoms, knowledge, templates, and error hints and validate fields/enums against the actual tool schemas.
- Trust-boundary review. Strapi content and platform-derived values are inserted into agent instructions; publish/render paths need prompt-injection and Markdown/tool-call escaping analysis.
- URI stability. Renamed/deleted guides need aliases or tombstones, with a no-dangling-citation rule across the whole corpus.
- Runtime corpus identity. A response/build should expose a corpus revision or manifest hash so support can determine what content a user actually received.
- Transition coverage. Reachability should prove not merely that a status can render an atom, but that a production state transition can reach that status.
- Compatibility coverage for persisted state from older releases. New axes and status enums should not make old state silently broaden or lose guidance.

## Resequenced slice plan

Specs should move with their owning slice. Each slice starts with its failing invariant or production-response test.

1. `local-deploy-source-truth` — fix T1.1, stale allowlist text, and local golden.
2. `ready-to-deploy-recovery-owner` — consolidate T1.9 and add failed-history/no-override coverage.
3. `axis-contract-registry` — reflection completeness and the corpus-integrity test chassis.
4. `launch-status-plumbing` — envelope/axis/parser/matcher support for the already-existing enum.
5. `launch-production-delivery` — migrate static launch guidance to synthesis; fix T2.1/T2.2; production witness tests.
6. `canonical-knowledge-resolver` — document-kind dispatch and all-recipe URI/recipe equivalence.
7. `knowledge-mode-contract` — make `mode=` recipe-only or otherwise explicitly supported; fix T1.8 and misleading schema text.
8. `knowledge-search-contract` — structured zero-hit recovery and complete five-mode tool description.
9. `recipe-discovery-index` — remove `runtimeRecipeHints`, normalize composite service types, derive associations from structured corpus data.
10. `recipe-matcher-metadata` — canonical framework names/aliases and real-corpus intent coverage.
11. `recipe-publishability` — viable body/YAML requirements, companion import validation, no self-skipping calibration.
12. `atom-lint-registry` — T3.1–4 and T3.8–10, including rule teeth and suppression liveness.
13. `atom-dependency-graph` — co-render enforcement, no content dependencies from reference sources, and fixes for dangling local/container edges.
14. `learned-once-reachability` — HTTP diagnostics, steady-state env pointers, and other content stranded by obsolete gating.
15. `git-push-setup-contract` — T1.7 across export/launch/setup, owned by the actual probe-first service-scoped behavior.
16. `bootstrap-composition` — route-specific verify narrative, close ordering, and true same-render duplication.
17. `develop-close-composition` — broken/unconfigured wording, retired close mode, implicit-webserver coverage, duplicate close variants.
18. `classification-protocol` — fragment tier, executable platform-env policy, five-bucket reconciliation.
19. Four independent micro-slices: hostname constraint derivation; export command examples; ES-lag agent surface; `.git` canonicalization agent surface.
20. `spec-hygiene` — only residual dead enums, archived-plan citations, stale signatures, and line-range removal.

Keep externally synchronized work out of an unattended repo-only run unless the credentials and PR authority are explicitly provided:

- Strapi queue: NestJS auth/key fixes, matcher metadata, truncated recipe repair.
- zerops-docs queue: database mutability, scaling/resource limits, dangling grammar references.

## Drops

I would not drop any HIGH safety or deploy-correctness finding. I would drop or reshape this churn:

- Drop seven bespoke drift-test designs; retain their invariants under the corpus-integrity architecture.
- Drop expansion of `SingleOwnerFacts` as a general numeric-fact database. It is another hand-maintained drift surface.
- Drop exact byte-for-byte URI/recipe parity as the contract; assert canonical resolver routing and required envelope layers instead.
- Drop a standalone Go-source atom-URI scanner after typed reference construction exists.
- Drop implementation of speculative `mode=stage` behavior. Narrow the contract.
- Drop wiring `runtime_resources.go` merely to justify its SSOT comment. Delete it unless a real generator consumes it.
- Drop maintenance of spec line-number ranges; cite symbols or sections.
- Defer pure editorial repetition such as extra “confirm with user” wording unless it changes action ordering, creates contradiction, or materially affects the measured payload budget.
- Do not deduplicate universal safety facts merely because they appear in independently fetched documents; self-contained pull documents sometimes require repetition. Deduplicate only where the copies co-render or have already drifted.

No files were changed.
