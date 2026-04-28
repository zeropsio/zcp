package recipe

import (
	"context"
	"strings"
	"testing"
)

// Run-16 §6.8 / §10.5a #17 — calibration tests for the cross-surface
// duplication validator. Topic-name-overlap matcher chosen over pure
// Jaccard; calibration evidence: against the run-15 corpus, the matcher
// catches both real R-15-6 dups while sparing 5+ representative non-dup
// IG/KB pairs from run-15's apidev/appdev/workerdev codebases.

func TestValidateCrossSurfaceDuplication_CatchesR15_6_XCacheDup(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "apidev", Role: RoleAPI}},
		Fragments: map[string]string{
			"codebase/apidev/integration-guide": `### 6. Custom response headers across origins

When the SPA reads X-Cache via fetch from a peer subdomain, the browser
strips it.`,
			"codebase/apidev/knowledge-base": `- **Custom response headers across origins** — Browsers strip every non-CORS-safelisted response header from cross-origin JS reads.`,
		},
	}
	vs := validateCrossSurfaceDuplication(context.Background(), plan)
	if !containsCode(vs, "cross-surface-duplication") {
		t.Errorf("expected cross-surface-duplication notice for X-Cache topic in IG + KB; got %+v", vs)
	}
}

func TestValidateCrossSurfaceDuplication_CatchesR15_6_DuplexHalfDup(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "appdev", Role: RoleFrontend}},
		Fragments: map[string]string{
			"codebase/appdev/integration-guide": `### 5. Streamed proxy bodies need duplex half

SvelteKit +server.ts proxies that stream multipart bodies must declare duplex.`,
			"codebase/appdev/knowledge-base": `- **Streamed proxy bodies need duplex half** — Same-origin proxy needs duplex: 'half' on Node 20+.`,
		},
	}
	vs := validateCrossSurfaceDuplication(context.Background(), plan)
	if !containsCode(vs, "cross-surface-duplication") {
		t.Errorf("expected cross-surface-duplication notice for duplex topic in IG + KB; got %+v", vs)
	}
}

func TestValidateCrossSurfaceDuplication_SparesNonDupPairs(t *testing.T) {
	t.Parallel()
	// Representative non-dup IG/KB pairs from run-15. Topics share at
	// most one common content word.
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "apidev", Role: RoleAPI}},
		Fragments: map[string]string{
			"codebase/apidev/integration-guide": `### 4. Set the API_URL env variable

The SPA needs API_URL to reach the api codebase.

### 5. Health endpoint

A /healthz endpoint pinned to the L7 readiness check.`,
			"codebase/apidev/knowledge-base": `- **execOnce keys per deploy** — The first container to win the race runs the body.
- **Per-tier mode flips** — Tier 5 promotes managed services to HA where the family supports it.
- **CORS exposes peer subdomain aliases** — Cross-origin fetch strips non-safelisted headers.`,
		},
	}
	vs := validateCrossSurfaceDuplication(context.Background(), plan)
	for _, v := range vs {
		if v.Code == "cross-surface-duplication" {
			t.Errorf("non-dup pair flagged as duplicate: %+v", v)
		}
	}
}

// Run-16 reviewer D-5 — slotted IG path. Pre-fix the validator read
// only the legacy `codebase/<h>/integration-guide` single-fragment id
// which is empty for run-16 recipes; the validator was inert. The fix
// merges slotted IG fragments the same way assemble.go does at stitch.
func TestValidateCrossSurfaceDuplication_CatchesDupAcrossSlottedIG(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "apidev", Role: RoleAPI}},
		Fragments: map[string]string{
			"codebase/apidev/integration-guide/2": "### 2. Custom response headers across origins\n\nbody.",
			"codebase/apidev/integration-guide/3": "### 3. Trust the L7 proxy\n\nbody.",
			"codebase/apidev/knowledge-base":      `- **Custom response headers across origins** — body.`,
		},
	}
	vs := validateCrossSurfaceDuplication(context.Background(), plan)
	if !containsCode(vs, "cross-surface-duplication") {
		t.Errorf("validator should fire against slotted IG fragments (D-5 closure); got %+v", vs)
	}
}

func TestValidateCrossSurfaceDuplication_NoFragments_NoNotice(t *testing.T) {
	t.Parallel()
	plan := &Plan{Codebases: []Codebase{{Hostname: "api", Role: RoleAPI}}}
	if vs := validateCrossSurfaceDuplication(context.Background(), plan); len(vs) != 0 {
		t.Errorf("empty fragments → no notice; got %+v", vs)
	}
}

func TestValidateCrossRecipeDuplication_CatchesParentTopicReuse(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI}},
		Fragments: map[string]string{
			"codebase/api/integration-guide": "### 2. Trust the reverse proxy\n\nbody.",
		},
	}
	parent := &ParentRecipe{
		Slug: "minimal",
		Codebases: map[string]ParentCodebase{
			"api": {README: "### 2. Trust the reverse proxy\n\nparent body."},
		},
	}
	vs := validateCrossRecipeDuplication(context.Background(), plan, parent)
	if !containsCode(vs, "cross-recipe-duplication") {
		t.Errorf("parent topic re-author should produce cross-recipe-duplication notice; got %+v", vs)
	}
}

// Run-16 reviewer D-5 — parent-recipe duplication must fire against
// slotted IG fragments too. The pre-fix lookup read the legacy single
// fragment id directly, which is empty for run-16 default (slotted)
// recipes, so parent overlap went silently undetected.
func TestValidateCrossRecipeDuplication_CatchesDupAcrossSlottedIG(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI}},
		Fragments: map[string]string{
			"codebase/api/integration-guide/2": "### 2. Trust the reverse proxy\n\nbody.",
			"codebase/api/integration-guide/3": "### 3. Bind to 0.0.0.0\n\nbody.",
		},
	}
	parent := &ParentRecipe{
		Slug: "minimal",
		Codebases: map[string]ParentCodebase{
			"api": {README: "### 2. Trust the reverse proxy\n\nparent body."},
		},
	}
	vs := validateCrossRecipeDuplication(context.Background(), plan, parent)
	if !containsCode(vs, "cross-recipe-duplication") {
		t.Errorf("validator should fire against slotted IG fragments (D-5 closure); got %+v", vs)
	}
}

func TestValidateCrossRecipeDuplication_NilParent_NoNotice(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI}},
		Fragments: map[string]string{"codebase/api/integration-guide": "### 2. Trust\n"},
	}
	if vs := validateCrossRecipeDuplication(context.Background(), plan, nil); len(vs) != 0 {
		t.Errorf("nil parent → no notice; got %+v", vs)
	}
}

func TestTopicsOverlap_SharedKeywords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
	}{
		// Real R-15-6 dups
		{"Custom response headers across origins", "Custom response headers across origins", true},
		{"Streamed proxy bodies need duplex half", "Streamed proxy bodies need duplex 'half'", true},
		// Distinct but partially-overlapping topics — single shared word
		// should NOT trip the matcher.
		{"Trust the reverse proxy", "Trust the L7 balancer", false},
		// Semantically distinct
		{"Set the API_URL env variable", "Health endpoint", false},
		// Empty + edge cases
		{"", "trust", false},
		{"a", "the", false},
	}
	for _, tc := range cases {
		got := topicsOverlap(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("topicsOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestTopicWords_StripsStopwordsAndShortTokens(t *testing.T) {
	t.Parallel()
	words := topicWords("The Custom response headers across origins")
	got := strings.Join(words, " ")
	want := "custom response headers origins"
	if got != want {
		t.Errorf("topicWords: got %q, want %q", got, want)
	}
}

func TestNotice_DedupSeverity(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI}},
		Fragments: map[string]string{
			"codebase/api/integration-guide": "### 2. Custom response headers across origins\n",
			"codebase/api/knowledge-base":    "- **Custom response headers across origins** — body",
		},
	}
	vs := validateCrossSurfaceDuplication(context.Background(), plan)
	for _, v := range vs {
		if v.Code != "cross-surface-duplication" {
			continue
		}
		if v.Severity != SeverityNotice {
			t.Errorf("cross-surface-duplication should be Notice severity (heuristic backstop); got %d", v.Severity)
		}
	}
}
