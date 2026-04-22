package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// discardDefaultClassifications is the set of classification values whose
// default routing is "discarded". Routing them anywhere else requires a
// non-empty override_reason explaining why the default doesn't apply.
// Reframing a scaffold-internal bug into a porter-facing symptom with a
// concrete failure mode is the canonical override path.
var discardDefaultClassifications = map[string]bool{
	"framework-quirk": true,
	"library-meta":    true,
	"self-inflicted":  true,
}

// checkWriterContentManifest is the deploy-step post-author enforcement
// for the writer subagent's content-classification contract. It runs
// four sub-checks:
//
//	A. Manifest presence + parse — file exists and is valid JSON.
//	B. Classification consistency — every fact classified framework-quirk /
//	   library-meta / self-inflicted with `routed_to != "discarded"` must
//	   carry a non-empty override_reason.
//	C. Manifest honesty (opschecks.CheckManifestHonesty) — for each fact
//	   routed to "discarded", no published gotcha stem may Jaccard-match
//	   the fact title at or above the honesty threshold.
//	D. Manifest completeness (opschecks.CheckManifestCompleteness) —
//	   every distinct FactRecord.Title in the facts log must have exactly
//	   one manifest entry. Guards against the deceptive-empty-manifest
//	   attack (writer emits {"facts":[]} to bypass B + C trivially).
//
// factsLogPath resolves to ops.FactLogPath(sessionID). The empty string
// indicates test context or a nil resolver — sub-check D passes with a
// skip note in that case.
func checkWriterContentManifest(ctx context.Context, projectRoot string, readmesByHost map[string]string, factsLogPath string) []workflow.StepCheck {
	path := filepath.Join(projectRoot, opschecks.ManifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return []workflow.StepCheck{{
			Name:   "writer_content_manifest_exists",
			Status: statusFail,
			Detail: fmt.Sprintf(
				"content manifest missing at %s — the content-authoring subagent must Write ZCP_CONTENT_MANIFEST.json at the recipe root before returning (see recipe.md content-authoring-brief §'Return contract'). The manifest reports classification + routing for every recorded fact so the deploy-step checker can enforce DISCARD-class routing.",
				path,
			),
		}}
	}
	var manifest opschecks.ContentManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return []workflow.StepCheck{
			{Name: "writer_content_manifest_exists", Status: statusPass},
			{
				Name:   "writer_content_manifest_valid",
				Status: statusFail,
				Detail: fmt.Sprintf(
					"content manifest invalid JSON at %s: %v. Required shape: {\"version\":1,\"facts\":[{\"fact_title\":...,\"classification\":...,\"routed_to\":...,\"override_reason\":\"\"}]}.",
					path, err,
				),
			},
		}
	}

	checks := make([]workflow.StepCheck, 0, 7)
	checks = append(checks,
		workflow.StepCheck{Name: "writer_content_manifest_exists", Status: statusPass},
		workflow.StepCheck{Name: "writer_content_manifest_valid", Status: statusPass},
	)
	checks = append(checks, checkManifestClassificationConsistency(manifest)...)
	checks = append(checks, checkManifestCitationsPresent(manifest)...)
	checks = append(checks, opschecks.CheckManifestHonesty(ctx, &manifest, readmesByHost)...)
	checks = append(checks, opschecks.CheckManifestCompleteness(ctx, &manifest, factsLogPath)...)
	return checks
}

// citationRequiredRoutes is the set of manifest routed_to values whose
// entries MUST carry at least one citation with a guide_fetched_at
// timestamp. Currently the two reader-facing per-codebase surfaces
// (content_gotcha + content_ig) — they are where v38 editorial-review
// surfaced the folk-doctrine CRIT class, and where "did you look up
// the guide?" is the load-bearing question.
//
// Extend with content_env_comment / zerops_yaml_comment if future
// editorial-review runs surface fabrication on those surfaces at
// comparable rates.
var citationRequiredRoutes = map[string]bool{
	"content_gotcha": true,
	"content_ig":     true,
}

// checkManifestCitationsPresent (v39 Commit 4, Sub-check E) enforces
// that every manifest fact routed to a reader-facing per-codebase
// surface carries at least one citation with a non-empty
// guide_fetched_at timestamp. The check turns the judgment call "is
// this bullet folk-doctrine?" into a file-existence question:
// did the writer call zerops_knowledge before publishing the bullet,
// or not?
//
// The check does NOT validate the citation's Topic against a known-
// guides list — that would require a topic registry wire-hop at check
// time and couples the check to the knowledge package's internal
// state. A non-empty Topic + non-empty GuideFetchedAt is sufficient
// evidence the lookup happened; if the agent recorded a made-up topic
// name the knowledge pipeline would have returned "unknown topic" and
// the agent would not have had a guide to paraphrase from.
func checkManifestCitationsPresent(m opschecks.ContentManifest) []workflow.StepCheck {
	var failures []string
	for _, entry := range m.Facts {
		if !citationRequiredRoutes[entry.RoutedTo] {
			continue
		}
		if hasCitationWithTimestamp(entry.Citations) {
			continue
		}
		failures = append(failures, fmt.Sprintf(
			"%q (routed_to=%s)",
			entry.FactTitle, entry.RoutedTo,
		))
	}
	if len(failures) == 0 {
		return []workflow.StepCheck{{
			Name:   "writer_manifest_citations_present",
			Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "writer_manifest_citations_present",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%d manifest entries routed to reader-facing surfaces carry no citation with a guide_fetched_at timestamp: %s. Every gotcha + IG-item bullet must paraphrase from an authoritative zerops_knowledge guide — call zerops_knowledge(topic=<...>) for the relevant topic BEFORE shipping the bullet, and record {topic, guide_fetched_at} on the manifest entry under citations[]. If the bullet's subject has no matching guide, cite the adjacent guide closest in mechanism (e.g. use 'env-var-model' for any cross-service env-var fact) so the lookup is still recorded.",
			len(failures), strings.Join(failures, "; "),
		),
	}}
}

// hasCitationWithTimestamp returns true iff at least one citation in
// the slice carries both a Topic and a GuideFetchedAt timestamp. Used
// by checkManifestCitationsPresent to grade each entry.
func hasCitationWithTimestamp(citations []opschecks.ManifestCitation) bool {
	for _, c := range citations {
		if strings.TrimSpace(c.Topic) != "" && strings.TrimSpace(c.GuideFetchedAt) != "" {
			return true
		}
	}
	return false
}

// checkManifestClassificationConsistency (Sub-check B) enforces that
// facts with a default-discard classification (framework-quirk,
// library-meta, self-inflicted) either route to "discarded" OR carry a
// non-empty override_reason. A missing reason means the writer silently
// overrode the default without explaining why — this is how v29 shipped
// healthCheck-bare-GET as an apidev gotcha despite its framework-quirk
// classification.
//
// "Keep" disposition per check-rewrite.md §17 — lives in the tools
// package because it operates purely on the in-memory manifest struct
// the server-side checker loads; shim invocation is not anticipated for
// this sub-check.
func checkManifestClassificationConsistency(m opschecks.ContentManifest) []workflow.StepCheck {
	var failures []string
	for _, entry := range m.Facts {
		if !discardDefaultClassifications[entry.Classification] {
			continue
		}
		if entry.RoutedTo == "discarded" {
			continue
		}
		if strings.TrimSpace(entry.OverrideReason) != "" {
			continue
		}
		failures = append(failures, fmt.Sprintf(
			"fact %q classified %s but routed to %s without override_reason",
			entry.FactTitle, entry.Classification, entry.RoutedTo,
		))
	}
	if len(failures) == 0 {
		return []workflow.StepCheck{{
			Name:   "writer_discard_classification_consistency",
			Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "writer_discard_classification_consistency",
		Status: statusFail,
		Detail: "manifest inconsistencies: " + strings.Join(failures, "; ") + ". Either route these facts to 'discarded' OR supply a non-empty override_reason explaining why the default classification doesn't apply (e.g. 'reframed from scaffold-internal bug to porter-facing symptom with concrete failure mode').",
	}}
}
