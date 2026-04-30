package recipe

import (
	"strings"
	"testing"
)

// TestSlugTrailingCitation_CatchesAllVerbVariants pins the run-19 prep
// extension. Run-18 shipped 9 instances of `Cite: <slug>` trailing form
// on workerdev/README.md (see docs/zcprecipator3/runs/18/workerdev/
// README.md:128, 138, 144, 148-160). The original §3.1 check 3 regex
// only matched `See: <slug> guide` — agent learned to evade `See:` and
// shipped `Cite: <slug>` instead. Catalog drift signature.
//
// New regex must catch See/Cite/Per/Ref/cf colon-prefixed forms with or
// without trailing `guide` keyword and with or without backticks around
// the slug. Inline prose ("see the http-support guide" — no colon)
// must not false-positive: those are legitimate per spec §216.
func TestSlugTrailingCitation_CatchesAllVerbVariants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want bool
	}{
		{"See: <slug> guide. (run-17 form)", "...the env shadow trap. See: env-var-model guide.", true},
		{"Cite: <slug>. (run-18 worker form)", "...avoid double-auth. Cite: managed-services-nats.", true},
		{"Cite: `<slug>`. (run-18 worker backticked)", "...avoid double-auth. Cite: `managed-services-nats`.", true},
		{"Cite: `<slug>`, `<slug2>`. (multi-slug)", "...drain ordering. Cite: `rolling-deploys`, `managed-services-nats`.", true},
		{"Per: <slug>", "...as detailed. Per: rolling-deploys.", true},
		{"Ref: <slug>", "...details. Ref: env-var-model.", true},
		{"cf: <slug>", "...alternative. cf: object-storage.", true},
		{"see the X guide (inline prose, no colon — legitimate)", "see the http-support guide for how the public subdomain wires", false},
		{"the X guide explains (inline prose — legitimate)", "the env-var-model guide covers cross-service auto-injection", false},
		{"Note: this is X (Note isn't a citation verb)", "Note: the recipe ships with hash routing", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := slugTrailingCitationRE.MatchString(tc.body)
			if got != tc.want {
				t.Errorf("slugTrailingCitationRE on %q: got match=%v, want=%v",
					tc.body, got, tc.want)
			}
		})
	}
}

// TestSlugTrailingCitation_RefusalMessage pins that the refusal message
// names the offending pattern intelligibly so the agent reauthors
// correctly. The earlier message hard-coded "See: <slug> guide." — now
// the regex catches Cite/Per/Ref/cf too, so the message text generalizes.
func TestSlugTrailingCitation_RefusalMessage(t *testing.T) {
	t.Parallel()

	body := "...avoid the double-auth path. Cite: `managed-services-nats`."
	refusals := kbBulletAuthoringRefusals("**Bullet stem**", body)
	if len(refusals) == 0 {
		t.Fatalf("expected at least one refusal, got none")
	}
	found := false
	for _, r := range refusals {
		if strings.Contains(r, "trailing citation") || strings.Contains(r, "Cite:") || strings.Contains(r, "See:") || strings.Contains(r, "agent's `zerops_knowledge` tool slug") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("refusal message must reference the trailing-citation anti-pattern; got: %v", refusals)
	}
}
