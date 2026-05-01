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

// TestNounPhraseSlugCitation_CatchesRun19DominantShapes — Run-20 V1.
// Run-19 caught zero hits on the colon-prefixed regex above because
// the agent rephrased to noun-phrase shapes that the prior regex
// missed entirely (apidev/README.md: 11 "The Zerops `<slug>`
// reference covers …"; workerdev/README.md: 2 "see `zerops_knowledge`
// guide `<slug>`"). Each of the four extension regex MUST catch its
// targeted shape; legitimate inline prose without backticked slugs
// still passes.
func TestNounPhraseSlugCitation_CatchesRun19DominantShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want bool
	}{
		// Run-19 apidev shape: "The Zerops `<slug>` reference …"
		{"The Zerops `<slug>` reference (apidev shape)",
			"The Zerops `rolling-deploys` reference covers SIGTERM handling.",
			true},
		// Run-19 workerdev shape: "see `zerops_knowledge` guide `<slug>`"
		{"see `zerops_knowledge` guide `<slug>` (workerdev shape)",
			"For more, see `zerops_knowledge` guide `managed-services-nats`.",
			true},
		// Variant: "per/cite `zerops_knowledge` guide `<slug>`"
		{"Per `zerops_knowledge` guide `<slug>` (verb variant)",
			"Per `zerops_knowledge` guide `env-var-model`, the alias resolves on start.",
			true},
		// Variant: "see guide `<slug>`"
		{"see guide `<slug>` (short variant)",
			"To learn more, see guide `http-support`.",
			true},
		// Reference/guide noun + verb covers/documents/explains
		{"`<slug>` reference covers …",
			"The `rolling-deploys` reference covers drain ordering.",
			true},
		{"`<slug>` guide explains …",
			"The `managed-services-nats` guide explains JetStream usage.",
			true},
		{"`<slug>` reference documents …",
			"The `env-var-model` reference documents alias shadowing.",
			true},
		// Legitimate inline-prose citations — no backticks around the
		// slug, full mechanism words: must NOT match.
		{"Inline prose without backticked slug — legitimate",
			"see the rolling-deploys guide on Zerops docs", false},
		{"Inline prose with mechanism phrase — legitimate",
			"the env-var-model guide covers cross-service auto-injection", false},
		{"Plain code reference (`foo`) — legitimate",
			"the `process.env.FOO` env var resolves at runtime", false},
		{"Backticked slug without citation framing — legitimate",
			"set `S3_REGION` so MinIO accepts uploads", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			match, got := hasNounPhraseSlugCitation(tc.body)
			if got != tc.want {
				t.Errorf("hasNounPhraseSlugCitation on %q: got match=%v (text=%q), want=%v",
					tc.body, got, match, tc.want)
			}
		})
	}
}

// TestNounPhraseSlugCitation_RefusalSurfaces — pins that the run-20
// V1 refusal fires through every porter-facing surface: KB bullets,
// IG slot bodies, and yaml-comment surfaces (codebase + env). The
// prep-doc redirect message must steer toward mechanism citation,
// not slug citation.
func TestNounPhraseSlugCitation_RefusalSurfaces(t *testing.T) {
	t.Parallel()

	body := "The Zerops `rolling-deploys` reference covers SIGTERM ordering."
	wantSubstrings := []string{"noun-phrase slug citation", "mechanism, not by slug", "Spec §216"}

	t.Run("kbBullet", func(t *testing.T) {
		t.Parallel()
		refusals := kbBulletAuthoringRefusals("**Stem**", body)
		if len(refusals) == 0 {
			t.Fatalf("kbBulletAuthoringRefusals returned no refusals")
		}
		joined := strings.Join(refusals, " | ")
		for _, want := range wantSubstrings {
			if !strings.Contains(joined, want) {
				t.Errorf("kb refusal missing %q; got %q", want, joined)
			}
		}
	})
	t.Run("igSlot", func(t *testing.T) {
		t.Parallel()
		refusals := igSlotAuthoringRefusals(body, nil)
		if len(refusals) == 0 {
			t.Fatalf("igSlotAuthoringRefusals returned no refusals")
		}
		joined := strings.Join(refusals, " | ")
		for _, want := range wantSubstrings {
			if !strings.Contains(joined, want) {
				t.Errorf("ig refusal missing %q; got %q", want, joined)
			}
		}
	})
	t.Run("commentSurface", func(t *testing.T) {
		t.Parallel()
		refusals := commentSurfaceSlugCitationRefusals(body, "codebase zerops.yaml comment")
		if len(refusals) == 0 {
			t.Fatalf("commentSurfaceSlugCitationRefusals returned no refusals")
		}
		joined := strings.Join(refusals, " | ")
		for _, want := range wantSubstrings {
			if !strings.Contains(joined, want) {
				t.Errorf("comment refusal missing %q; got %q", want, joined)
			}
		}
	})
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
