# Audit — phrase classification for handleExport → atom-synthesis migration

**Step**: Phase 0b.0 of `plans/atom-corpus-verification-2026-05-02.md`.
**Date**: 2026-05-02.
**Pause point**: classification is collaborative (human approval required
before 0b.1 proceeds). Codex may be invoked for mechanical contradiction-
spotting per plan; Codex output is signal, not ground truth.

## Method

Walked `internal/tools/workflow_export.go` lines 256-433. For each of the
7 export statuses, extracted the agent-facing inline phrases that are
content-bearing (not structural keys, dynamic placeholders, or schema
echoes). Classified per the plan §0b.0 three-state rule:

- `survived-as-correct` — content-correct, relocate to atom body verbatim
  (or with cosmetic editing). Goes into `expectedSubstrings` test.
- `survived-with-question` — content questionable but worth keeping; goes
  into `expectedSubstrings` with a `TODO` comment for Phase 2 review pass.
- `dropped-pre-migration` — phrase contains an identifiable state-leak,
  outdated plan reference, or lie. Excluded from `expectedSubstrings`;
  not migrated to atom body.

Decision rule for each phrase: is the assertion universally true for
every envelope configuration the target atom's axes match (per the
procedural-form principle in plan §11.1 bullet 7)?

## Findings

### 1. scope-prompt — `scopePromptResponse` (line 273)

| # | Phrase | Class | Rationale |
|---|---|---|---|
| 1.1 | `Pick the runtime service to export` | `survived-as-correct` | Universal at scope-prompt: by definition target unknown, the handler is asking which one. |
| 1.2 | `Pass targetService=<hostname> on the next call` | `survived-as-correct` | Universal at scope-prompt; the action shape is fixed by the WorkflowInput contract. |

### 2. variant-prompt — `variantPromptResponse` (lines 282-294)

| # | Phrase | Class | Rationale |
|---|---|---|---|
| 2.1 | `is part of a <mode> pair` | `survived-as-correct` | Handler emits variant-prompt only for `ModeStandard` / `ModeLocalStage` (per `resolveExportVariant` line 224). Universal at variant-prompt. |
| 2.2 | `Pick which half of the pair to package` | `survived-as-correct` | Universal at variant-prompt — the action's whole purpose. |
| 2.3 | `variant="dev" packages the dev hostname's working tree + zerops.yaml` | `survived-as-correct` | True per `ops.BuildBundle` semantics for dev variant. |
| 2.4 | `variant="stage" packages the stage hostname's` | `survived-as-correct` | True per `ops.BuildBundle` semantics for stage variant. |
| 2.5 | `Both bundles emit Zerops scaling mode=NON_HA` | `survived-as-correct` | Pinned by spec invariant in CLAUDE.md ("Export-for-buildFromGit … `services[].mode` is the Zerops scaling enum (`HA`/`NON_HA`)"). |
| 2.6 | `destination project topology is established by ZCP's bootstrap on import, not by the bundle` | `survived-as-correct` | Pinned by same CLAUDE.md invariant ("ZCP topology (dev/simple/local-only) is destination-bootstrap concern, not import.yaml content"). |
| 2.7 | `Per plan §3.3 (revised in Phase 5)` | `dropped-pre-migration` | Plan reference. The plan named (`export-buildfromgit-2026-04-28.md`) is archived per `plans/archive/`; embedding the cite in agent-facing prose creates a rot vector and adds no actionable signal. |

### 3. scaffold-required — `scaffoldChainResponse` (lines 299-308)

| # | Phrase | Class | Rationale |
|---|---|---|---|
| 3.1 | `/var/www/zerops.yaml is missing — export cannot compose a bundle without a setup block` | `survived-with-question` | Asserts "missing"; handler actually triggers on `TrimSpace(zeropsYAMLBody) == ""` so empty-but-present also fires. Existing `scaffold-zerops-yaml.md` atom body already says "missing or empty" — the inline string is the slight lie. **Action**: relocate to atom body using the atom's existing "missing or empty" wording; expectedSubstring asserts presence of `/var/www/zerops.yaml` and `setup block` rather than the literal "is missing" phrasing. TODO in expectedSubstring for Phase 2 review of the precise wording. |
| 3.2 | `Run the scaffold-zerops-yaml atom flow to emit a minimal valid zerops.yaml` | `survived-with-question` | After 0b.6 the scaffold-zerops-yaml atom IS the rendered guidance — the self-referential "Run the X atom flow" wording is awkward for an agent reading X directly. **Action**: rephrase to actionable imperative ("Emit a minimal valid zerops.yaml from runtime-detected type/version/ports, commit, then re-call export"). expectedSubstring asserts `minimal valid zerops.yaml` (substantive content) without the meta-instruction. TODO Phase 2. |
| 3.3 | `then re-call export` | `survived-as-correct` | Universal at scaffold-required: stateless workflow, reentry is the only forward path. |

### 4. git-push-setup-required — `gitPushSetupChainResponse` (lines 317-333)

| # | Phrase | Class | Rationale |
|---|---|---|---|
| 4.1 | `Export Phase C requires GitPushState=configured` | `survived-as-correct` | True per handler invariant (line 206). The atom already carries this. |
| 4.2 | `Run the git-push-setup action to provision GIT_TOKEN/.netrc/remote URL` | `survived-as-correct` | True per `setup-git-push-container` atom semantics + existing `export-publish-needs-setup.md` content. |
| 4.3 | `re-call export` | `survived-as-correct` | Universal — same reentry rule as 3.3. |
| 4.4 | `Reason: %s` (dynamic) | (excluded) | Dynamic interpolation. The static prefix `Reason:` would migrate as structural; but the human-readable reason text comes from `len(repoURL) == 0` vs `meta.GitPushState != configured` branching in handler. Keep dynamic reason on the response as a top-level `reason` field (already there); atom body refers to "the reason in the response" generically. **Not in expectedSubstring**. |

### 5. classify-prompt — `classifyPromptResponse` (lines 365-380)

| # | Phrase | Class | Rationale |
|---|---|---|---|
| 5.1 | `Classify each project env` | `survived-as-correct` | Universal at classify-prompt: by definition unclassified envs exist. |
| 5.2 | `infrastructure / auto-secret / external-secret / plain-config` | `survived-as-correct` | Closed enum names (per `topology.SecretClassification`). Already in existing `export-classify-envs.md`. |
| 5.3 | `Inspect values via zerops_discover (includeEnvs=true, includeEnvValues=true)` | `survived-as-correct` | True per discover action shape; already in atom. |
| 5.4 | `grep against source code` | `survived-as-correct` | True per classification protocol. Already in atom. |
| 5.5 | `re-call with envClassifications={key:bucket} populated to publish` | `survived-as-correct` | True per WorkflowInput contract. Already in atom. |
| 5.6 | `plan §3.4 four-category classification protocol` | `dropped-pre-migration` | Plan reference. Same rot-vector argument as 2.7. The four bucket names already convey the protocol; the plan cite adds no agent-actionable info. |
| 5.7 | `per-env review table per amendment 12` | `dropped-pre-migration` | Plan reference / amendment cite. Same as above; the table itself is the response payload, naming the amendment in prose adds noise. |

### 6. validation-failed — `validationFailedResponse` (lines 390-403)

| # | Phrase | Class | Rationale |
|---|---|---|---|
| 6.1 | `Schema validation surfaced blocking errors against the published JSON schemas` | `survived-as-correct` | True per schema validation gate (`bundle.Errors`). Already in `export-validate.md`. |
| 6.2 | `Inspect each error's path + message` | `survived-as-correct` | True per `formatBundleErrors` shape (`path`/`message` keys). |
| 6.3 | `fix the failing field (re-classify the env, edit the live zerops.yaml, or scaffold if structurally absent)` | `survived-as-correct` | True — these are the three repair classes Phase 5 schema validation surfaces. |
| 6.4 | `re-call export with the same inputs` | `survived-as-correct` | Universal — same reentry rule as 3.3 / 4.3. |

### 7. publish-ready — `publishGuidanceResponse` (lines 408-433)

| # | Phrase | Class | Rationale |
|---|---|---|---|
| 7.1 | `Bundle composed` | `survived-as-correct` | True at publish-ready: bundle has been built, classifications accepted, validation clean. |
| 7.2 | `Write the YAMLs at repo root, commit, and push via zerops_deploy strategy="git-push"` | `survived-as-correct` | True per export-publish.md atom content (already present). The four next-step commands in the response carry the actual ssh/cat/git command shape; this prose summarizes the high-level flow. |

## Aggregate counts

| Status | survived-as-correct | survived-with-question | dropped-pre-migration |
|---|---|---|---|
| scope-prompt | 2 | 0 | 0 |
| variant-prompt | 6 | 0 | 1 |
| scaffold-required | 1 | 2 | 0 |
| git-push-setup-required | 3 | 0 | 0 |
| classify-prompt | 5 | 0 | 2 |
| validation-failed | 4 | 0 | 0 |
| publish-ready | 2 | 0 | 0 |
| **Total** | **23** | **2** | **3** |

The 3 drops are all plan-reference / amendment cites (2.7, 5.6, 5.7) —
no structural lies surfaced. The 2 questions (3.1, 3.2) are wording
nuances around the scaffold-required path's framing post-migration; both
get TODO comments in the expectedSubstring test for Phase 2 review.

## What goes into `expectedSubstrings[status]` (post-classification)

This is the input to Phase 0b.1 RED test. Per status, the test asserts
each survived substring is present in the rendered atom body. Drops are
NOT asserted (they don't migrate). Questions are asserted with TODO.

```
scope-prompt:
  - "Pick the runtime"            // 1.1 (truncated to safer substring)
  - "targetService"                // 1.1 / 1.2 — action shape
variant-prompt:
  - "pair"                         // 2.1 covers it
  - "dev"                          // 2.3 — variant name
  - "stage"                        // 2.4 — variant name
  - "NON_HA"                       // 2.5 — scaling invariant
  - "destination project"          // 2.6 / topology invariant
scaffold-required:
  - "/var/www/zerops.yaml"         // 3.1 (TODO Phase 2 — wording precision)
  - "minimal valid zerops.yaml"    // 3.2 (TODO Phase 2 — self-ref clarity)
  - "re-call"                      // 3.3
git-push-setup-required:
  - "GitPushState=configured"      // 4.1
  - "git-push-setup"               // 4.2
  - "re-call"                      // 4.3
classify-prompt:
  - "Classify"                     // 5.1
  - "infrastructure"               // 5.2
  - "auto-secret"                  // 5.2
  - "external-secret"              // 5.2
  - "plain-config"                 // 5.2
  - "zerops_discover"              // 5.3
  - "envClassifications"           // 5.5
validation-failed:
  - "Schema validation"            // 6.1
  - "path"                         // 6.2 — error shape
  - "message"                      // 6.2 — error shape
  - "re-call"                      // 6.4
publish-ready:
  - "Bundle composed"              // 7.1
  - "git-push"                     // 7.2 — action name
  - "commit"                       // 7.2
```

The substrings are deliberately loose (single words / short phrases)
rather than exact-string matches, so atom-body editorial improvements
during Phase 2 don't break the test. The contract is "these concepts
must surface in the rendered guidance for this status."

## Awaiting user approval

Plan 0b.0 is a PAUSE POINT. Do NOT proceed to 0b.1 (RED expectedSubstrings
test) without user approval of:

1. The 3-state classifications above (especially 3 drops + 2 questions).
2. The expected-substring list as the contract for the 0b.1 test.
3. Whether any phrase I marked `survived-as-correct` should actually be
   dropped, or any drop should be kept (e.g. is the plan-§3.4 cite
   actually load-bearing for some reader?).

I will not edit atoms or rewrite handleExport until this audit is
explicitly approved.
