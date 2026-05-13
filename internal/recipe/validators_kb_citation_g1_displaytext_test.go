package recipe

import (
	"context"
	"testing"
)

// validators_kb_citation_g1_displaytext_test.go — Run-46 Item 5.
//
// Run-45 forensic — apidev IG #4 shipped:
//
//	See [zero-downtime deploys with multi-container setups](https://docs.zerops.io/zerops-yaml/specification#initcommands-)
//
// The display text names rolling-deploys (the canonical friendly for
// the rolling-deploys guide); the URL is the init-commands canonical.
// The wrong-URL form-(b) citation passed the existing
// kbBodyCitesGuideURL anchored-match check because the URL was a valid
// init-commands citation — but the bullet's stated topic and display
// text were rolling-deploys.
//
// Item 5 closes the loop with a display↔URL agreement check + a
// best-effort URL↔topic check:
//
//  1. When a markdown link's URL matches a known guide URL, the display
//     text MUST be the friendly display name for THAT guide.
//  2. When the bullet's topic is unambiguously detectable, the URL MUST
//     match the canonical URL for the topic.
//
// Multi-topic bullets escape the topic check (DetectBulletTopic returns
// "" on ambiguity) — only hard-fail unambiguous mis-citations.

// TestCitationDisplayURLTopic_DisplayMismatchURL — the run-45 apidev
// IG #4 fixture. Display names rolling-deploys; URL is init-commands.
// Validator MUST fire.
func TestCitationDisplayURLTopic_DisplayMismatchURL(t *testing.T) {
	t.Parallel()
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Rolling deploys via the L7 balancer** — multi-container deploys drain.
  See [zero-downtime deploys with multi-container setups](https://docs.zerops.io/zerops-yaml/specification#initcommands-)
  for the timing contract.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-citation-display-mismatch") {
		t.Errorf("expected kb-citation-display-mismatch on display-URL drift; got %+v", vs)
	}
}

// TestCitationDisplayURLTopic_TopicMismatchURL — bullet's primary topic
// is unambiguously rolling-deploys (multiple anchors); URL is the
// init-commands canonical. Validator MUST fire on the topic check.
func TestCitationDisplayURLTopic_TopicMismatchURL(t *testing.T) {
	t.Parallel()
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **SIGTERM and rolling-deploys** — minContainers≥2 enables zero-downtime
  rolling-deploys.
  See the ` + "`init-commands`" + ` guide for the timing contract.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	// The bullet's PRIMARY topic is rolling-deploys (SIGTERM + minContainers
	// + rolling-deploys all converge); the citation form-(a) names
	// init-commands. Topic check fires.
	if !containsCode(vs, "kb-citation-topic-mismatch") {
		t.Errorf("expected kb-citation-topic-mismatch on unambiguous topic mis-cite; got %+v", vs)
	}
}

// TestCitationDisplayURLTopic_MultiTopicAmbiguousAllows — bullet
// touches both rolling-deploys + init-commands topics; either citation
// passes (topic check returns "" for ambiguity).
func TestCitationDisplayURLTopic_MultiTopicAmbiguousAllows(t *testing.T) {
	t.Parallel()
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **execOnce + rolling deploys** — execOnce keys are scoped per-deploy;
  multi-container deploys with minContainers≥2 trade slight overlap for
  zero-downtime. See the ` + "`init-commands`" + ` guide.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-citation-topic-mismatch") {
		t.Errorf("multi-topic bullet should not hard-fail topic check; got %+v", vs)
	}
}

// TestCitationDisplayURLTopic_CorrectDisplayURLPasses — display text
// matches the URL's friendly name. No validator fires.
func TestCitationDisplayURLTopic_CorrectDisplayURLPasses(t *testing.T) {
	t.Parallel()
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **SIGTERM drain too short** — workers crashing on the next deploy.
  See [zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha)
  for the timing contract.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-citation-display-mismatch") {
		t.Errorf("correct display-URL pairing should not fire mismatch; got %+v", vs)
	}
	if containsCode(vs, "kb-citation-topic-mismatch") {
		t.Errorf("correct display-URL pairing should not fire topic mismatch; got %+v", vs)
	}
}

// TestDetectBulletTopic_AmbiguousReturnsEmpty — multi-topic body returns
// "" so callers don't hard-fail on multi-topic bullets.
func TestDetectBulletTopic_AmbiguousReturnsEmpty(t *testing.T) {
	t.Parallel()
	bullet := "**execOnce + rolling deploys** — execOnce keys are scoped per-deploy; multi-container deploys with minContainers≥2."
	if got := DetectBulletTopic(bullet); got != "" {
		t.Errorf("ambiguous multi-topic bullet should return \"\"; got %q", got)
	}
}

// TestDetectBulletTopic_UnambiguousReturnsTopic — bullet has a clear
// rolling-deploys focus; helper returns the canonical topic id.
func TestDetectBulletTopic_UnambiguousReturnsTopic(t *testing.T) {
	t.Parallel()
	bullet := "**SIGTERM and rolling deploys** — minContainers≥2 enables rolling-deploys; SIGTERM drain handles the cutover."
	if got := DetectBulletTopic(bullet); got != "rolling-deploys" {
		t.Errorf("unambiguous rolling-deploys bullet returned %q, want \"rolling-deploys\"", got)
	}
}

// TestFriendlyDisplayName_KnownGuidesResolve — every guide id with a
// canonical URL has a friendly display name registered, so the
// display-text validator can resolve every URL.
func TestFriendlyDisplayName_KnownGuidesResolve(t *testing.T) {
	t.Parallel()
	for _, guide := range []string{
		"rolling-deploys", "init-commands", "object-storage",
		"env-var-model", "http-support", "deploy-files",
		"readiness-health-checks",
	} {
		if got := FriendlyDisplayName(guide); got == "" {
			t.Errorf("FriendlyDisplayName(%q) returned empty; every known guide must have a registered friendly name", guide)
		}
	}
}
