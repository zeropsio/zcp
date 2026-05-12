package recipe

import (
	"context"
	"strings"
	"testing"
)

// validators_kb_citation_url_g1_test.go — Run-44 G1 substrate pin.
//
// Forensics (plans/run-43-validation.md §"F3 substrate-internal
// contradiction"): refinement-2 correctly caught worker KB #1 + #2's
// wrong-path citation (`docs.zerops.io/zerops-yaml/specification
// #rolling-deploys` vs the canonical `docs.zerops.io/features/
// scaling-ha`). The main agent ACTed and rewrote the body to use the
// canonical URL. Refinement-1's snapshot/restore wrapper reverted
// twice because the `kb-citation-missing` validator at
// validators_codebase.go:144-156 fires bare-substring on the slug-
// stem ("rolling-deploys"); the canonical URL does NOT contain the
// slug-stem substring, so the validator false-fires on the FIX.
// Substrate fighting itself; wrong URL ships.
//
// Fix: extend `kb-citation-missing` to ALSO pass when the body
// contains the canonical URL for the topic's guide. Source the
// canonical URL from the `citationURL*` constants in
// `briefs_refinement2.go`.
//
// False-positive guard (per codex): anchored URL match — the matched
// URL ends at a non-path character (whitespace, `)`, `#`, or EOF) AND
// starts after a scheme-like prefix (`https://`, `http://`, or `(`).
// Substring `features/scaling-ha` MUST NOT match
// `features/scaling-ha-advanced`.

// TestValidator_KB_CitationByCanonicalURL — KB body cites the
// canonical URL (not the slug-stem). Validator MUST pass.
func TestValidator_KB_CitationByCanonicalURL(t *testing.T) {
	t.Parallel()

	body := []byte(`<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **SIGTERM drain too short** — workers crashing on the next deploy.
  See [zero-downtime deploys with multi-container setups](https://docs.zerops.io/features/scaling-ha)
  for the timing contract.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(), "codebases/worker/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-citation-missing") {
		t.Errorf("kb-citation-missing fired on a canonical-URL citation; substrate-internal contradiction. violations=%+v", vs)
	}
}

// TestValidator_KB_CitationCanonicalURLFalsePositiveGuard — body
// contains a prefix-extending URL (`features/scaling-ha-advanced`)
// but NOT the exact canonical URL. Validator MUST fire (the prefix
// match was not the topic guide).
func TestValidator_KB_CitationCanonicalURLFalsePositiveGuard(t *testing.T) {
	t.Parallel()

	body := []byte(`<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **SIGTERM drain too short** — workers crashing. The
  [advanced scaling article](https://docs.zerops.io/features/scaling-ha-advanced)
  is a prefix-extending URL, not the canonical citation.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(), "codebases/worker/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	// SIGTERM is a CitationMap topic that maps to "rolling-deploys"
	// guide; canonical URL is `docs.zerops.io/features/scaling-ha`.
	// The body has only the prefix `features/scaling-ha-advanced`,
	// which does NOT pass the anchored-match guard, so the validator
	// MUST fire. The body deliberately avoids the literal stem
	// `rolling-deploys` so the URL check is the load-bearing path.
	if !containsCode(vs, "kb-citation-missing") {
		t.Errorf("expected kb-citation-missing on prefix-extended URL false-positive; got %+v", vs)
	}
}

// TestValidator_KB_CitationByGuideStemStillPasses — pre-G1 behavior
// preserved. A KB body citing via the guide-id stem (the validator's
// original substring check) still passes.
func TestValidator_KB_CitationByGuideStemStillPasses(t *testing.T) {
	t.Parallel()

	body := []byte(`<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **SIGTERM drain too short** — workers crashing. See the
  ` + "`rolling-deploys`" + ` guide for the timing contract.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(), "codebases/worker/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-citation-missing") {
		t.Errorf("kb-citation-missing fired on legitimate guide-stem citation; got %+v", vs)
	}
}

// TestValidator_KB_NoCitationStillFails — body has neither slug-stem
// nor canonical URL. Validator MUST fire (preserves the original
// kb-citation-missing behavior).
func TestValidator_KB_NoCitationStillFails(t *testing.T) {
	t.Parallel()

	body := []byte(`<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
### Gotchas

- **SIGTERM drain too short** — workers crashing. No citation here at all.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`)
	vs, err := validateCodebaseKB(context.Background(), "codebases/worker/README.md", body, SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "kb-citation-missing") {
		t.Errorf("expected kb-citation-missing on no-citation body; got %+v", vs)
	}
}

// TestCitationGuideURL_AllGuidesHaveURLs — every distinct guide ID in
// CitationMap MUST have an entry in CitationGuideURL. This pin keeps
// the parallel map honest: adding a new topic→guide row in
// CitationMap must also extend CitationGuideURL.
func TestCitationGuideURL_AllGuidesHaveURLs(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, guide := range CitationMap {
		if seen[guide] {
			continue
		}
		seen[guide] = true
		if _, ok := CitationGuideURL[guide]; !ok {
			t.Errorf("guide %q in CitationMap has no canonical URL in CitationGuideURL", guide)
		}
	}
}

// TestCitationGuideURL_MatchesBriefConstants — the rolling-deploys
// entry MUST match citationURLRollingDeploys; the env-var-model entry
// MUST match citationURLEnvVarModel; etc. Two named-constant sources
// must stay in lockstep so refinement-2's brief and the validator's
// accept-list agree on the canonical URL.
func TestCitationGuideURL_MatchesBriefConstants(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		guide string
		want  string
	}{
		{"rolling-deploys", citationURLRollingDeploys},
		{"init-commands", citationURLInitCommands},
		{"object-storage", citationURLObjectStorage},
		{"env-var-model", citationURLEnvVarModel},
		{"http-support", citationURLHTTPSupport},
		{"deploy-files", citationURLDeployFiles},
		{"readiness-health-checks", citationURLReadinessChecks},
	}
	for _, p := range pairs {
		got, ok := CitationGuideURL[p.guide]
		if !ok {
			t.Errorf("CitationGuideURL[%q] missing", p.guide)
			continue
		}
		if !strings.EqualFold(got, p.want) {
			t.Errorf("CitationGuideURL[%q] = %q, want %q", p.guide, got, p.want)
		}
	}
}
