package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// Run-41 — refinement-2 (cross-surface audit) brief composer.
//
// The first refinement pass walks per-fragment rules (`derived_rules.md`)
// — intra-fragment voice/structure/citation checks. Cross-surface
// defect classes (KB↔IG duplication, KB below-floor, surface-misplaced
// bullets, yaml-comment ↔ yaml-content named-constant drift,
// aspirational-as-current prose, cross-codebase named-constant drift)
// are invisible to that pass by construction: each check compares two
// or more fragments. Run-40 dogfood ([plans/run-40-validation.md])
// surfaced six such defects post-finalize (N-1, N-3, N-4, KB↔IG
// duplication on app + worker, JWT/MEILI_SEARCH_KEY aspirational
// prose) that refinement-1 ran clean over.
//
// The composer assembles only the load-bearing audit substrate from
// `docs/spec-content-surfaces.md` — phase entry (voice + dispatch
// shape) + audit checklist (the defect-class definitions). The spec
// document itself is engine-author reference; the agent never reads
// it (it's under docs/, not in `//go:embed all:content`).
//
// Output contract: diagnosis-only. The audit sub-agent emits a
// fenced JSON findings block; it does NOT call `record-fragment
// mode=replace`. The main agent reads the findings and decides per-
// finding whether to ACT, HOLD, or accept-as-known. This avoids the
// cross-rule conflict pattern that reverted refinement-1's ACT
// attempts in run-40 (slug-stem rule conflict).

// BuildRefinement2Brief composes the brief for the second refinement
// sub-agent dispatched at phase 8 (post-refinement-1, pre-finalize-
// close). Mirrors BuildRefinementBrief signature: plan, parent,
// outputRoot, facts.
//
// Brief content:
//
//  1. phase_entry — voice + output shape (JSON findings block).
//  2. audit_checklist — per-defect-class definitions with checks +
//     suggested actions.
//  3. Stitched output pointer block — paths the sub-agent reads.
//  4. Canonical-latest constants block — engine-rendered from
//     plan.NamedConstants + facts canonical topics so the audit can
//     compare on-surface values against the canonical store.
//  5. Citation map — which topics MUST cite which zerops_knowledge
//     guide; engine-rendered.
//
// No filesystem-local references leak; runDir is the published-
// recipe output root (porter-facing, deliverable-shipping path),
// not the author's working tree. Pinned by
// TestBuildRefinement2Brief_NoFilesystemReferenceLeak.
func BuildRefinement2Brief(plan *Plan, parent *ParentRecipe, runDir string, facts []FactRecord) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	parts := []string{}
	var b strings.Builder

	// Phase entry — voice + dispatch + findings JSON shape.
	if atom, err := readAtom("briefs/refinement2/phase_entry.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/refinement2/phase_entry.md")
	} else {
		return Brief{}, fmt.Errorf("read refinement2 phase_entry atom: %w", err)
	}

	// Audit checklist — per-defect-class definitions.
	if atom, err := readAtom("briefs/refinement2/audit_checklist.md"); err == nil {
		b.WriteString(atom)
		b.WriteString("\n\n")
		parts = append(parts, "briefs/refinement2/audit_checklist.md")
	} else {
		return Brief{}, fmt.Errorf("read refinement2 audit_checklist atom: %w", err)
	}

	// Stitched-output pointer block. runDir is the published-recipe
	// output root. Trailing-slash normalization mirrors
	// BuildRefinementBrief (run-40 S-3).
	if runDir != "" {
		runDir = strings.TrimRight(runDir, "/")
		b.WriteString("## Stitched output to audit\n\n")
		b.WriteString("Read each path end-to-end before running cross-surface checks. Don't `Grep`; cross-surface defects need full surfaces held in working memory.\n\n")
		b.WriteString("**Root**\n\n")
		fmt.Fprintf(&b, "- `%s/README.md` — root README (S1)\n", runDir)
		fmt.Fprintf(&b, "- `%s/environments/plan.json` — fragment store (canonical content for every fragmentId)\n", runDir)
		fmt.Fprintf(&b, "- `%s/environments/facts.jsonl` — recorded facts (audit citation source)\n", runDir)
		b.WriteString("\n**Tier environments (S2 + S3)**\n\n")
		for _, t := range Tiers() {
			fmt.Fprintf(&b, "- `%s/environments/%s/README.md` + `import.yaml`\n", runDir, t.Folder)
		}
		b.WriteString("\n**Codebases (S4 + S5 + S6 + S7)**\n\n")
		for _, cb := range plan.Codebases {
			fmt.Fprintf(&b, "- `%s/%sdev/README.md` + `zerops.yaml` + `CLAUDE.md` — IG (S4), KB (S5), CLAUDE.md (S6), yaml comments (S7)\n", runDir, cb.Hostname)
		}
		// Per-codebase dependency manifests for `aspirational-as-current`
		// framework-feature claim cross-check. The audit reads these
		// to confirm a claimed framework feature has its implementing
		// dependency in scope. The brief lists candidate paths for
		// every common manifest shape (Node, PHP, Python); the agent
		// reads whichever exist in the codebase dir.
		b.WriteString("\n**Per-codebase dependency manifests (for aspirational-as-current)**\n\n")
		for _, cb := range plan.Codebases {
			fmt.Fprintf(&b, "- `%s/%sdev/{package.json, composer.json, pyproject.toml, requirements.txt}` — read whichever exists\n", runDir, cb.Hostname)
		}
		b.WriteByte('\n')
		parts = append(parts, "stitched-output-pointer-block")
	}

	// Canonical-latest constants block — same shape the
	// refinement-1 + env-content composers render. The audit reads
	// it for cross-codebase-named-constant-drift + yaml-comment-
	// content-drift checks.
	if appendCanonicalLatestSection(&b, facts) {
		parts = append(parts, "canonical-latest-facts")
	}
	if appendNamedConstantsSection(&b, plan) {
		parts = append(parts, "named-constants")
	}

	// Citation map — extracted from spec-content-surfaces.md §
	// "Citation map". Hardcoded here as a constant block (the spec
	// doc itself isn't embedded; we inline the load-bearing rules).
	b.WriteString(citationMapBlock())
	parts = append(parts, "citation-map")

	return Brief{
		Kind:  BriefRefinement2,
		Body:  b.String(),
		Bytes: b.Len(),
		Parts: parts,
	}, nil
}

// citationMapBlock renders the topic → required-guide-citation map.
// Anchored to the seven-surface content contract. Kept inline as a
// rendered block in the brief body because the contract doc itself
// isn't embedded in the binary (//go:embed all:content covers
// internal/recipe/content/ only) and the sub-agent can't Read it
// at runtime. Spec drift is pinned by
// TestBuildRefinement2Brief_CitationMapMatchesSpec.
//
// The map carries seven heuristic topic rows that mirror the
// content-surface contract's citation table, plus two managed-
// service rows (`managed NATS broker`, `managed Meilisearch
// service`) that aren't in the contract's heuristic table but are
// stable per-service-guide pointers the contract's spirit covers.
// Adjust counter assertions in the matching test, not the rule.
//
// Citation acceptance is three-form: each topic row lists the
// canonical guide ID in backticks, the friendly display name, and
// the docs URL. A bullet passes if its body cites any of the three
// USING CITATION FRAMING (`the \`<guide-id>\` guide covers …`,
// `[friendly name](URL)`, or a bare URL — not a passing mention of
// the literal token as a noun phrase). The framing requirement
// prevents bare-backtick narration (e.g. *"Zerops's `init-commands`
// feature stamps each `key:` value"*) from being read as a real
// citation when the bullet doesn't actually point the porter at
// the guide.
//
// Run-43 P7 — citation URLs are exposed as named constants below.
// Form (b) tightening: the brief now demands the URL exactly match
// the map's URL for the topic. Run-42 dogfood evidence: workerdev
// README #5 (slug-stem-fix replacement) shipped
// `docs.zerops.io/features/env-variables#env-var-model` while the
// brief's citation map specifies
// `docs.zerops.io/zerops-yaml/specification#envvariables-`. Phase-8
// refinement-1 caught the slug-stem leak on app KB but missed the
// fabricated URL fragment on worker KB because the validator only
// checked the `docs.zerops.io` host. The constants below + the
// brief-composer test `TestBuildRefinement2Brief_CitationMapURLsAreStable`
// pin every URL to a single source of truth.

// citation-map URL constants — exposed for test pinning and brief
// rendering. Order matches the citation map row order.
const (
	citationURLRollingDeploys      = "docs.zerops.io/features/scaling-ha"
	citationURLInitCommands        = "docs.zerops.io/zerops-yaml/specification#initcommands-"
	citationURLObjectStorage       = "docs.zerops.io/services/object-storage"
	citationURLEnvVarModel         = "docs.zerops.io/zerops-yaml/specification#envvariables-"
	citationURLHTTPSupport         = "docs.zerops.io/features/access"
	citationURLDeployFiles         = "docs.zerops.io/zerops-yaml/specification#deployfiles-"
	citationURLReadinessChecks     = "docs.zerops.io/zerops-yaml/specification#readinesscheck-"
	citationURLManagedNATS         = "docs.zerops.io/services/nats"
	citationURLManagedMeilisearch  = "docs.zerops.io/services/meilisearch"
)

func citationMapBlock() string {
	return `## Citation map — topics requiring zerops_knowledge citation

When a KB bullet covers one of these topics, the body MUST cite the
named guide. Missing citations get flagged with
` + "`missing-citation`" + ` (advisory).

**Acceptable citation forms** — for each topic, a bullet passes if
its body contains ANY of these, USED AS A CITATION (pointing the
porter at the guide), not as a passing mention of the literal token:

- (a) **Canonical guide ID in citation framing** — ` + "`the \\`<guide-id>\\` guide covers …`" + `,
  ` + "`see the \\`<guide-id>\\` reference`" + `, or equivalent
  framing where the backticked guide ID is the subject of a
  sentence pointing the porter at the guide. **Bare backtick token
  use that doesn't reference the guide DOES NOT count**: a bullet
  saying *"Zerops's ` + "`init-commands`" + ` feature stamps each
  ` + "`key:`" + ` value"* mentions the feature but does not cite
  the guide — fails this form.
- (b) **Friendly display name as markdown link text** —
  ` + "`[zero-downtime deploys with multi-container setups](URL)`" + `;
  the link text spells out the friendly name verbatim AND the URL
  EXACTLY matches the topic's URL in the citation map below. Host-
  only matches (any ` + "`docs.zerops.io/<anything>`" + ` URL passes the
  bullet) DO NOT count — the URL fragment must match the map. Run-42
  worker README shipped ` + "`docs.zerops.io/features/env-variables#env-var-model`" + ` for an env-var-model citation; the map specifies
  ` + "`docs.zerops.io/zerops-yaml/specification#envvariables-`" + `. The
  host-only check passed but the link points the porter at a
  fabricated URL fragment. Fail with ` + "`missing-citation`" + ` when the
  link text claims to cite topic ` + "`X`" + ` but the URL doesn't match
  topic ` + "`X`" + `'s row in the citation map.
- (c) **Bare docs URL** — the literal ` + "`docs.zerops.io/...`" + ` URL
  for the guide, present in the bullet body — the URL must EXACTLY
  match the topic's URL in the citation map (no host-only acceptance,
  same rule as form (b)).

- ` + "`rolling-deploys`" + ` / ` + "`minContainers-semantics`" + ` / multi-container setups / minContainers≥2 / zero-downtime / SIGTERM-before-teardown / drain semantics → cite guide ID ` + "`rolling-deploys`" + ` OR friendly ` + "`zero-downtime deploys with multi-container setups`" + ` OR URL ` + "`" + citationURLRollingDeploys + "`" + `
- ` + "`init-commands`" + ` / ` + "`zsc execOnce`" + ` / ` + "`${appVersionId}`" + ` / per-deploy lock / ` + "`--retryUntilSuccessful`" + ` → cite guide ID ` + "`init-commands`" + ` OR friendly ` + "`zsc execOnce + per-deploy key model`" + ` OR URL ` + "`" + citationURLInitCommands + "`" + `
- ` + "`object-storage`" + ` / MinIO / S3 / forcePathStyle / presigned URL / ` + "`storage_*`" + ` env vars → cite guide ID ` + "`object-storage`" + ` OR friendly ` + "`S3-compatible storage on the MinIO backend`" + ` OR URL ` + "`" + citationURLObjectStorage + "`" + `
- ` + "`env-var-model`" + ` / cross-service alias / same-key shadow / ` + "`${<host>_<key>}`" + ` / envIsolation / project-level vs service-level → cite guide ID ` + "`env-var-model`" + ` OR friendly ` + "`per-key env shape and cross-service aliases`" + ` OR URL ` + "`" + citationURLEnvVarModel + "`" + `
- ` + "`http-support`" + ` / ` + "`l7-balancer`" + ` / subdomain access / ` + "`httpSupport`" + ` / VXLAN routing / TLS termination / ` + "`trust proxy`" + ` / bind 0.0.0.0 → cite guide ID ` + "`http-support`" + ` OR ` + "`l7-balancer`" + ` OR friendly ` + "`Zerops L7 balancer + subdomain access`" + ` OR URL ` + "`" + citationURLHTTPSupport + "`" + `
- ` + "`deploy-files`" + ` / ` + "`static-runtime`" + ` / tilde suffix / ` + "`./dist/~`" + ` strip-prefix / ` + "`base: static`" + ` runtime / Nginx SPA fallback → cite guide ID ` + "`deploy-files`" + ` OR ` + "`static-runtime`" + ` OR friendly ` + "`deploy-files tilde syntax + static runtime`" + ` OR URL ` + "`" + citationURLDeployFiles + "`" + `
- ` + "`readiness-health-checks`" + ` / readiness check / health check / routing gates / what routes traffic vs restarts container → cite guide ID ` + "`readiness-health-checks`" + ` OR friendly ` + "`readiness + health checks`" + ` OR URL ` + "`" + citationURLReadinessChecks + "`" + `
- managed NATS / queue groups / pub-sub → cite ` + "`managed NATS broker`" + ` (` + citationURLManagedNATS + `)
- managed Meilisearch / search keys / index admin → cite ` + "`managed Meilisearch service`" + ` (` + citationURLManagedMeilisearch + `)

A bullet covering a topic NOT in this list has no required citation;
` + "`missing-citation`" + ` does not fire.

`
}
