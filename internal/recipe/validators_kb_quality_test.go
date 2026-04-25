package recipe

import (
	"context"
	"testing"
)

// TestValidateKB_RejectsCitedGuideParaphrase — run-11 gap V-2. A KB
// bullet that just paraphrases the cited guide adds no porter-facing
// teaching beyond what zerops_knowledge already returns. Detected by
// Jaccard overlap of bullet body vs. guide key-phrase set.
func TestValidateKB_RejectsCitedGuideParaphrase(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **L7 balancer routes subdomain traffic** — the L7 balancer " +
		"terminates SSL and routes traffic to the subdomain hostname on " +
		"configurable ports; binding 0.0.0.0 not localhost is required " +
		"for the container network. The subdomain DNS resolves through " +
		"the L7 balancer. Cited guide: `http-support`.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-bullet-paraphrases-cited-guide") {
		t.Errorf("expected kb-bullet-paraphrases-cited-guide, got %+v", vs)
	}
}

// TestValidateKB_AcceptsCitedGuideExtension — bullet citing http-support
// with new content beyond the guide (a recipe-specific intersection)
// passes. The guide body's tokens make up < 50% of the bullet's
// vocabulary.
func TestValidateKB_AcceptsCitedGuideExtension(t *testing.T) {
	t.Parallel()

	body := []byte("# codebase/api\n" +
		"\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n" +
		"### Gotchas\n" +
		"\n" +
		"- **Subdomain registration is two-step** — Per the http-support " +
		"guide the L7 balancer requires explicit subdomain activation; " +
		"after `zerops_subdomain action=enable` the route propagates " +
		"asynchronously and a 502 for ~10 seconds is normal. Watch the " +
		"Zerops dashboard subdomain card for state=ACTIVE before declaring " +
		"the deploy green. Worker codebases skip this step entirely.\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-bullet-paraphrases-cited-guide") {
		t.Errorf("expected NO paraphrase violation for extension bullet, got %+v", vs)
	}
}
