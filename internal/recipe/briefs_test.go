package recipe

import (
	"strings"
	"testing"
)

func TestBriefCompose_ScaffoldUnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for _, cb := range plan.Codebases {
		t.Run(cb.Hostname, func(t *testing.T) {
			t.Parallel()
			brief, err := BuildScaffoldBrief(plan, cb, nil)
			if err != nil {
				t.Fatalf("BuildScaffoldBrief: %v", err)
			}
			if brief.Bytes > ScaffoldBriefCap {
				t.Errorf("scaffold brief for %s: %d bytes exceeds %d cap",
					cb.Hostname, brief.Bytes, ScaffoldBriefCap)
			}
			if !strings.Contains(brief.Body, "# Scaffold brief — "+cb.Hostname) {
				t.Error("missing scaffold brief header")
			}
			if !strings.Contains(brief.Body, "Platform obligations") {
				t.Error("missing platform obligations section")
			}
		})
	}
}

func TestBriefCompose_FeatureUnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if brief.Bytes > FeatureBriefCap {
		t.Errorf("feature brief: %d bytes exceeds %d cap", brief.Bytes, FeatureBriefCap)
	}
	for _, cb := range plan.Codebases {
		if !strings.Contains(brief.Body, cb.Hostname) {
			t.Errorf("feature brief missing codebase %q in symbol table", cb.Hostname)
		}
	}
	for _, svc := range plan.Services {
		if !strings.Contains(brief.Body, svc.Hostname) {
			t.Errorf("feature brief missing service %q in symbol table", svc.Hostname)
		}
	}
}
