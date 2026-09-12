package observer

import (
	"testing"
	"time"
)

var goldenCreatedAt = time.Date(2026, 9, 11, 10, 56, 30, 0, time.UTC)

// TestRender_Golden pins render.go's plain-text rendering (§7.5's shape,
// stable and golden-tested) for each of the observation's four rendered
// shapes: ok with findings, ok with no findings, unparsed, and error.
//
// Golden deliberately updated (FIX2.md round 2, "two wording items the API
// slice could not reach"): the header line's quote count now reads
// "quotes found N/M" (N verified, M total) instead of "N of M quotes
// unverified" — this changes every case's header, format 1 and format 2
// alike, since Render's header is common to both.
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
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · quotes found 1/2\n" +
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
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · quotes found 1/1\n" +
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
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · quotes found 0/0\n" +
				"Clean run: agent reached the goal with no friction.\n" +
				"Goal: yes — service healthy\n" +
				"Checks: agree with verdict passed\n" +
				"Findings: none\n" +
				"Self-review: yes\n",
		},
		{
			name: "unparsed",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: statusUnparsed,
				Raw: "the agent did fine overall, nothing structured to report",
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · quotes found 0/0\n" +
				"Observer answer could not be parsed\n" +
				"the agent did fine overall, nothing structured to report\n",
		},
		{
			name: "error",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: statusError,
				Error: "observer timed out after 5m0s",
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · quotes found 0/0\n" +
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

// TestRender_Format2Golden pins render.go's format-2 rendering (§7.5 item
// 4): outcome first, headline, story, findings with surface/anchor/span/
// causedVerdict, judged checks, warnings — distinguished from format 1
// purely by Story being non-nil (format 1 never sets it).
func TestRender_Format2Golden(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
		want string
	}{
		{
			name: "format 2: problem with a stuck story, a judged check and a warning",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: "ok",
				Outcome:  OutcomeProblem,
				Headline: "Agent called the forbidden override on zerops_import.",
				Story: &Story{
					Task: "diagnose and fix the failing api service", Expected: "investigate without a forbidden override",
					Did: "called zerops_import with override=true", Stuck: &Stuck{From: 12, To: 54, What: "kept retrying the same forbidden call"},
					Ending: EndingFinished,
				},
				Goal: Goal{Reached: "no", Why: "the service never became healthy"},
				Checks: Checks{
					Verdict: "failed", Agree: true,
					Judged: []JudgedCheck{{ID: "liveness/api/marker", Correct: true, Why: "the probe genuinely never found the marker"}},
				},
				Findings: []Finding{
					{
						Severity: "high", Owner: "agent", Surface: "tool:zerops_import", Anchor: "forbidden call zerops_import{override=true}",
						Title: "called forbidden override despite scenario rule",
						What:  "The agent called zerops_import with override=true, which the scenario forbids.",
						Evidence: []Evidence{
							{Step: 5, Quote: "forbidden call zerops_import{override=true}", Verified: true},
						},
						Span: &Span{From: 12, To: 54}, CausedVerdict: true,
						LookAt: "zerops_import override handling",
						Fix:    "check the never-list before issuing a destructive call",
					},
				},
				SelfReview: SelfReview{Accurate: "yes"},
				Warnings:   []string{"dropped judged check \"no_such_check\": not a failed or blocked check of this run"},
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · quotes found 1/1\n" +
				"Outcome: problem\n" +
				"Agent called the forbidden override on zerops_import.\n" +
				"Task: diagnose and fix the failing api service\n" +
				"Expected: investigate without a forbidden override\n" +
				"Did: called zerops_import with override=true\n" +
				"Stuck: steps #12–#54 — kept retrying the same forbidden call\n" +
				"Ending: finished\n" +
				"Goal: no — the service never became healthy\n" +
				"Checks: agree with verdict failed\n" +
				"Verdict right:\n" +
				"  ✓ correct liveness/api/marker — the probe genuinely never found the marker\n" +
				"Findings:\n" +
				"1. [high · agent · tool:zerops_import] called forbidden override despite scenario rule\n" +
				"   The agent called zerops_import with override=true, which the scenario forbids.\n" +
				"   Anchor: \"forbidden call zerops_import{override=true}\"\n" +
				"   Evidence: #5 \"forbidden call zerops_import{override=true}\" ✓\n" +
				"   Span: steps #12–#54\n" +
				"   Caused verdict: yes\n" +
				"   Look at: zerops_import override handling\n" +
				"   Fix: check the never-list before issuing a destructive call\n" +
				"Self-review: yes\n" +
				"Warnings:\n" +
				" - dropped judged check \"no_such_check\": not a failed or blocked check of this run\n",
		},
		{
			name: "format 2: clean run, no findings, no stuck, no warnings",
			obs: Observation{
				Model: "claude-sonnet-5", CreatedAt: goldenCreatedAt, Status: "ok",
				Outcome:  OutcomeOK,
				Headline: "OK — agent reached the goal with no friction.",
				Story: &Story{
					Task: "diagnose the service", Expected: "investigate without changing anything",
					Did: "looked at it, changed nothing", Stuck: nil, Ending: EndingFinished,
				},
				Goal:       Goal{Reached: "yes", Why: "service healthy"},
				Checks:     Checks{Verdict: "passed", Agree: true},
				SelfReview: SelfReview{Accurate: "yes"},
			},
			want: "Observer · claude-sonnet-5 · 2026-09-11T10:56Z · quotes found 0/0\n" +
				"Outcome: ok\n" +
				"OK — agent reached the goal with no friction.\n" +
				"Task: diagnose the service\n" +
				"Expected: investigate without changing anything\n" +
				"Did: looked at it, changed nothing\n" +
				"Ending: finished\n" +
				"Goal: yes — service healthy\n" +
				"Checks: agree with verdict passed\n" +
				"Findings: none\n" +
				"Self-review: yes\n",
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
