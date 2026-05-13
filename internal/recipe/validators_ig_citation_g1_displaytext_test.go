package recipe

import (
	"context"
	"testing"
)

// validators_ig_citation_g1_displaytext_test.go — Run-46 Item 5
// G1-followup.
//
// The original Item 5 wiring (validators_kb_citation_g1_displaytext_test.go)
// installed the citation display↔URL↔topic agreement check on
// validateCodebaseKB. The codex review then pointed out that the
// motivating run-45 example was an IG bullet — apidev IG #4: "rolling
// deploys with multi-container setups" display text + init-commands
// canonical URL — and the validator wasn't wired to validateCodebaseIG.
//
// This file pins the IG-surface coverage. citationBlockSplits picks the
// `### N. <title>` heading delimiter for IG bodies vs the
// `- **stem**` bullet delimiter for KB bodies; both surfaces share the
// same display↔URL↔topic check.

// TestIGCitationDisplayURLTopic_DisplayMismatchURL — the run-45 apidev
// IG #4 fixture in its IG (S4) surface form. Display names rolling-
// deploys; URL is init-commands. Validator MUST fire with
// kb-citation-display-mismatch.
func TestIGCitationDisplayURLTopic_DisplayMismatchURL(t *testing.T) {
	t.Parallel()
	body := `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
### 1. Adding zerops.yaml

The runtime zerops.yaml lives at the codebase root.

### 2. Rolling-deploy timing

The L7 balancer drains containers during multi-container deploys.
See [zero-downtime deploys with multi-container setups](https://docs.zerops.io/zerops-yaml/specification#initcommands-)
for the timing contract.
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
`
	vs, err := validateCodebaseIG(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-citation-display-mismatch") {
		t.Errorf("expected kb-citation-display-mismatch on IG display-URL drift; got %+v", vs)
	}
}

// TestIGCitationDisplayURLTopic_TopicMismatchURL — IG item's primary
// topic is unambiguously rolling-deploys; the form-(a) citation names
// init-commands. Topic check MUST fire.
func TestIGCitationDisplayURLTopic_TopicMismatchURL(t *testing.T) {
	t.Parallel()
	body := `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
### 1. Adding zerops.yaml

The runtime yaml is at the codebase root.

### 2. SIGTERM and rolling-deploys

minContainers≥2 enables zero-downtime rolling-deploys.
See the ` + "`init-commands`" + ` guide for the timing contract.
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
`
	vs, err := validateCodebaseIG(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-citation-topic-mismatch") {
		t.Errorf("expected kb-citation-topic-mismatch on IG topic mis-cite; got %+v", vs)
	}
}

// TestIGCitationDisplayURLTopic_CorrectPairPasses — IG item with a
// display-text that matches the URL's friendly name fires no citation
// mismatch. Uses the http-support guide URL whose path uniquely
// identifies the guide (`docs.zerops.io/features/access`) so the test
// is independent of how lookupGuideIDFromURL resolves path-sharing
// fragments under the `/zerops-yaml/specification` stem.
func TestIGCitationDisplayURLTopic_CorrectPairPasses(t *testing.T) {
	t.Parallel()
	body := `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
### 1. Adding zerops.yaml

Place zerops.yaml at the codebase root.

### 2. Subdomain access via the L7 balancer

The platform-issued zeropsSubdomain URL routes through the L7 balancer.
See [Zerops L7 balancer + subdomain access](https://docs.zerops.io/features/access)
for the full contract.
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
`
	vs, err := validateCodebaseIG(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-citation-display-mismatch") {
		t.Errorf("correct IG display-URL pairing should not fire mismatch; got %+v", vs)
	}
	if containsCode(vs, "kb-citation-topic-mismatch") {
		t.Errorf("correct IG display-URL pairing should not fire topic mismatch; got %+v", vs)
	}
}
