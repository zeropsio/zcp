package recipe

import (
	"strings"
	"testing"
)

// briefs_refinement2_friendly_labels_g6_test.go — Run-44 G6 substrate pin.
//
// Run-43 validation §"Substrate priority 4 — missing-citation pre-
// resolved replacement": refinement-2's `missing-citation` action is
// `add-citation` with no concrete replacement. Main agent has to
// compose the citation prose, avoid slug-stem-leak (V3 warning), and
// match canonical URL. Three pitfalls per finding.
//
// Fix: pin the citation-map block in briefs_refinement2.go carries the
// friendly labels per topic family — labels the audit emits VERBATIM
// as the `suggestedReplacement` form-(b) markdown link. Main agent
// pastes the link into the KB/IG body and the citation lands clean.
//
// The friendly labels themselves are not new (they shipped with the
// citation-map block under V3 slug-stem-leak guidance); the G6 fix is
// (a) keeping them coupled with canonical URLs in the brief, and (b)
// telling the audit-LLM to emit them as suggestedReplacement (G2's
// audit_checklist.md edit). This test pins (a).

// TestCitationMap_FriendlyLabelsPresent — every topic family in the
// brief's citation map MUST list a friendly display label as the
// form-(b) link text, AND that label MUST be paired with the canonical
// URL.
func TestCitationMap_FriendlyLabelsPresent(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Per-topic friendly-label assertions.
	pairs := []struct {
		topic    string
		friendly string
		url      string
	}{
		{
			topic:    "rolling-deploys",
			friendly: "zero-downtime deploys with multi-container setups",
			url:      citationURLRollingDeploys,
		},
		{
			topic:    "init-commands",
			friendly: "zsc execOnce + per-deploy key model",
			url:      citationURLInitCommands,
		},
		{
			topic:    "object-storage",
			friendly: "S3-compatible storage on the MinIO backend",
			url:      citationURLObjectStorage,
		},
		{
			topic:    "env-var-model",
			friendly: "per-key env shape and cross-service aliases",
			url:      citationURLEnvVarModel,
		},
		{
			topic:    "http-support",
			friendly: "Zerops L7 balancer + subdomain access",
			url:      citationURLHTTPSupport,
		},
		{
			topic:    "deploy-files",
			friendly: "deploy-files tilde syntax + static runtime",
			url:      citationURLDeployFiles,
		},
		{
			topic:    "readiness-health-checks",
			friendly: "readiness + health checks",
			url:      citationURLReadinessChecks,
		},
	}
	for _, p := range pairs {
		// The friendly label MUST be present in the brief body.
		if !strings.Contains(brief.Body, p.friendly) {
			t.Errorf("citation map missing friendly label for topic %q: %q", p.topic, p.friendly)
			continue
		}
		// AND the URL MUST also be present (pairing pinned by drift
		// test TestBuildRefinement2Brief_CitationMapURLsAreStable).
		if !strings.Contains(brief.Body, p.url) {
			t.Errorf("citation map missing URL for topic %q: %q", p.topic, p.url)
		}
	}
}

// TestRefinement2Brief_MissingCitation_PreResolvedSuggestedReplacement —
// the audit_checklist.md missing-citation rule MUST instruct the audit
// to emit `suggestedReplacement` as a concrete form-(b) markdown link
// (using the citation map's friendly label + canonical URL). This
// closes the run-43 substrate-priority-4 gap where the main agent had
// to compose the citation by hand and tripped slug-stem-leak / wrong-
// URL-form regressions.
func TestRefinement2Brief_MissingCitation_PreResolvedSuggestedReplacement(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Locate the missing-citation defect class section.
	idx := strings.Index(brief.Body, "## Defect class: missing-citation")
	if idx < 0 {
		t.Fatal("missing-citation header missing")
	}
	tail := brief.Body[idx:]
	next := strings.Index(tail[1:], "\n## Defect class:")
	var chunk string
	if next > 0 {
		chunk = tail[:next+1]
	} else {
		chunk = tail[:min(len(tail), 5000)]
	}
	// G6 anchors — section MUST direct the audit to emit suggestedReplacement
	// as a form-(b) markdown link with the friendly label + canonical URL.
	for _, want := range []string{
		"suggestedReplacement",
		"form-(b) markdown link",
		"copy verbatim",
	} {
		if !strings.Contains(chunk, want) {
			t.Errorf("missing-citation section missing G6 suggestedReplacement anchor %q; chunk:\n%s", want, chunk)
		}
	}
}
