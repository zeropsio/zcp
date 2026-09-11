package observer

import (
	"testing"
	"time"
)

var goldenCreatedAt = time.Date(2026, 9, 11, 10, 56, 30, 0, time.UTC)

// TestRender_Golden pins render.go's plain-text rendering (§7.5's shape,
// stable and golden-tested) for each of the observation's four rendered
// shapes: ok with findings, ok with no findings, unparsed, and error.
func TestRender_Golden(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
		want string
	}{
		{
			name: "ok with findings",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: "ok",
				Headline: "Agent fixed the build but skipped the liveness check.",
				Goal:     Goal{Reached: "partly", Why: "service builds but never answers HTTP"},
				Checks:   Checks{Verdict: "failed", Agree: true},
				Findings: []Finding{
					{
						Severity: "high", Owner: "zcp-tool", Title: "deploy preflight missed missing dep",
						What: "The build succeeded but runtime crashed on missing db.",
						Evidence: []Evidence{
							{Step: 12, Quote: "PREFLIGHT_FAILED: zerops.yaml not found", Verified: true},
							{Step: 40, Quote: "this quote is not in the record", Verified: false},
						},
						LookAt: "zerops_deploy preflight, PREFLIGHT_FAILED",
						Fix:    "surface the missing dependency in the preflight message",
					},
				},
				SelfReview: SelfReview{Accurate: "partly", Note: "claims success but liveness check failed"},
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · 1 of 2 quotes unverified\n" +
				"Agent fixed the build but skipped the liveness check.\n" +
				"Goal: partly — service builds but never answers HTTP\n" +
				"Checks: agree with verdict failed\n" +
				"Findings:\n" +
				"1. [high · zcp-tool] deploy preflight missed missing dep\n" +
				"   The build succeeded but runtime crashed on missing db.\n" +
				"   Evidence: #12 \"PREFLIGHT_FAILED: zerops.yaml not found\" ✓ · #40 \"this quote is not in the record\" ✗ unverified\n" +
				"   Look at: zerops_deploy preflight, PREFLIGHT_FAILED\n" +
				"   Fix: surface the missing dependency in the preflight message\n" +
				"Self-review: partly — claims success but liveness check failed\n",
		},
		{
			// A quote copied verbatim from a step can carry a real newline
			// (a multi-line tool result); rendered as-is it breaks the
			// Evidence line's layout (observed live: `#39 "Phase: idle
			// Services: …"` spanning two lines). Collapsed to a space for
			// display only — the stored Evidence.Quote is untouched.
			name: "evidence quote with a newline is collapsed for display",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: "ok",
				Headline: "Agent misread multi-line platform state.",
				Goal:     Goal{Reached: "no", Why: "agent never noticed the failed service"},
				Checks:   Checks{Verdict: "failed", Agree: true},
				Findings: []Finding{
					{
						Severity: "medium", Owner: "agent", Title: "missed the failed service in the snapshot",
						What: "The snapshot text spans lines; the agent read only the first.",
						Evidence: []Evidence{
							{Step: 39, Quote: "Phase: idle\nServices: api=FAILED", Verified: true},
						},
						LookAt: "zerops_discover output formatting",
						Fix:    "",
					},
				},
				SelfReview: SelfReview{Accurate: "no"},
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · 0 of 1 quotes unverified\n" +
				"Agent misread multi-line platform state.\n" +
				"Goal: no — agent never noticed the failed service\n" +
				"Checks: agree with verdict failed\n" +
				"Findings:\n" +
				"1. [medium · agent] missed the failed service in the snapshot\n" +
				"   The snapshot text spans lines; the agent read only the first.\n" +
				"   Evidence: #39 \"Phase: idle Services: api=FAILED\" ✓\n" +
				"   Look at: zerops_discover output formatting\n" +
				"Self-review: no\n",
		},
		{
			name: "ok with no findings",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: "ok",
				Headline:   "Clean run: agent reached the goal with no friction.",
				Goal:       Goal{Reached: "yes", Why: "service healthy"},
				Checks:     Checks{Verdict: "passed", Agree: true},
				Findings:   nil,
				SelfReview: SelfReview{Accurate: "yes"},
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · 0 of 0 quotes unverified\n" +
				"Clean run: agent reached the goal with no friction.\n" +
				"Goal: yes — service healthy\n" +
				"Checks: agree with verdict passed\n" +
				"Findings: none\n" +
				"Self-review: yes\n",
		},
		{
			name: "unparsed",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: "unparsed",
				Raw: "the agent did fine overall, nothing structured to report",
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · 0 of 0 quotes unverified\n" +
				"Observer answer could not be parsed\n" +
				"the agent did fine overall, nothing structured to report\n",
		},
		{
			name: "error",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: statusError,
				Error: "observer timed out after 5m0s",
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · 0 of 0 quotes unverified\n" +
				"Observer failed: observer timed out after 5m0s\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Render(tc.obs)
			if got != tc.want {
				t.Errorf("Render() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}
