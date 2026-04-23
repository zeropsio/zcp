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

func TestBriefCompose_WriterUnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	facts := []FactRecord{
		{
			Topic: "cross-service-env-autoinject", Symptom: "blank DB_HOST",
			Mechanism: "self-shadow", SurfaceHint: "platform-trap",
			Citation: "env-var-model",
		},
		{
			Topic: "trust-proxy-for-forwarded-ip", Symptom: "wrong client IP in logs",
			Mechanism: "L7 balancer rewrites headers", SurfaceHint: "porter-change",
			Citation: "http-support",
		},
	}
	brief, err := BuildWriterBrief(plan, facts, nil)
	if err != nil {
		t.Fatalf("BuildWriterBrief: %v", err)
	}
	if brief.Bytes > WriterBriefCap {
		t.Errorf("writer brief: %d bytes exceeds %d cap", brief.Bytes, WriterBriefCap)
	}
	for _, s := range Surfaces() {
		if !strings.Contains(brief.Body, string(s)) {
			t.Errorf("writer brief missing surface %q", s)
		}
	}
	// Facts route to the right surfaces.
	if !strings.Contains(brief.Body, "cross-service-env-autoinject — citation: env-var-model") {
		t.Error("platform-trap fact missing from writer brief")
	}
	if !strings.Contains(brief.Body, "trust-proxy-for-forwarded-ip — citation: http-support") {
		t.Error("porter-change fact missing from writer brief")
	}
}

func TestBriefCompose_WriterDeterministic(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	a, err := BuildWriterBrief(plan, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildWriterBrief(plan, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Body != b.Body {
		t.Error("writer brief composition not deterministic across calls")
	}
}
