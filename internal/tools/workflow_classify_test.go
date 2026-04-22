package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

// TestClassifyAction_ReturnsRouteTo — v39 Commit 5a. Table-driven
// coverage of the runtime classify lookup: every FactType constant
// returns the expected default route + citation requirement, and the
// keyword-based self-inflicted / framework-quirk overrides steer
// away from publish surfaces correctly.
func TestClassifyAction_ReturnsRouteTo(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name               string
		factType           string
		keywords           string
		wantClassification string
		wantRoute          string
		wantCitation       bool
		wantOverrideReq    bool
	}{
		{
			name:               "gotcha defaults to content_gotcha + citation required",
			factType:           ops.FactTypeGotchaCandidate,
			keywords:           "",
			wantClassification: "framework-invariant",
			wantRoute:          ops.FactRouteToContentGotcha,
			wantCitation:       true,
			wantOverrideReq:    false,
		},
		{
			name:               "ig_item defaults to content_ig + citation required",
			factType:           ops.FactTypeIGItemCandidate,
			keywords:           "",
			wantClassification: "framework-invariant",
			wantRoute:          ops.FactRouteToContentIG,
			wantCitation:       true,
			wantOverrideReq:    false,
		},
		{
			name:               "cross_codebase_contract routes to content_ig",
			factType:           ops.FactTypeCrossCodebaseContract,
			keywords:           "",
			wantClassification: "framework-invariant",
			wantRoute:          ops.FactRouteToContentIG,
			wantCitation:       true,
			wantOverrideReq:    false,
		},
		{
			name:               "fix_applied pairs with gotcha surface",
			factType:           ops.FactTypeFixApplied,
			keywords:           "",
			wantClassification: "framework-invariant",
			wantRoute:          ops.FactRouteToContentGotcha,
			wantCitation:       true,
			wantOverrideReq:    false,
		},
		{
			name:               "verified_behavior → zerops_yaml_comment (no citation required)",
			factType:           ops.FactTypeVerifiedBehavior,
			keywords:           "",
			wantClassification: "framework-invariant",
			wantRoute:          ops.FactRouteToZeropsYAMLComment,
			wantCitation:       false,
			wantOverrideReq:    false,
		},
		{
			name:               "self-inflicted keyword forces discard + override required",
			factType:           ops.FactTypeGotchaCandidate,
			keywords:           "our code had a bug silently exited 0 with no output",
			wantClassification: "self-inflicted",
			wantRoute:          ops.FactRouteToDiscarded,
			wantCitation:       false,
			wantOverrideReq:    true,
		},
		{
			name:               "framework-quirk keyword forces discard + override required",
			factType:           ops.FactTypeGotchaCandidate,
			keywords:           "setGlobalPrefix controller decorator collision",
			wantClassification: "framework-quirk",
			wantRoute:          ops.FactRouteToDiscarded,
			wantCitation:       false,
			wantOverrideReq:    true,
		},
		{
			name:               "operational keyword reroutes to claude_md",
			factType:           ops.FactTypeVerifiedBehavior,
			keywords:           "truncate dev data reset iterate locally",
			wantClassification: "operational",
			wantRoute:          ops.FactRouteToClaudeMD,
			wantCitation:       false,
			wantOverrideReq:    false,
		},
		{
			name:               "scaffold-decision keyword reroutes to zerops_yaml_comment",
			factType:           ops.FactTypeIGItemCandidate,
			keywords:           "deployFiles tilde dist/~ zeropsSetup",
			wantClassification: "scaffold-decision",
			wantRoute:          ops.FactRouteToZeropsYAMLComment,
			wantCitation:       false,
			wantOverrideReq:    false,
		},
		{
			name:               "unknown fact type returns empty classification",
			factType:           "nonsense_type",
			keywords:           "",
			wantClassification: "",
			wantRoute:          "",
			wantCitation:       false,
			wantOverrideReq:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyFactRuntime(tc.factType, strings.ToLower(tc.keywords))
			if got.Classification != tc.wantClassification {
				t.Errorf("Classification = %q, want %q", got.Classification, tc.wantClassification)
			}
			if got.DefaultRouteTo != tc.wantRoute {
				t.Errorf("DefaultRouteTo = %q, want %q", got.DefaultRouteTo, tc.wantRoute)
			}
			if got.RequiresCitation != tc.wantCitation {
				t.Errorf("RequiresCitation = %v, want %v", got.RequiresCitation, tc.wantCitation)
			}
			if got.RequiresOverrideReason != tc.wantOverrideReq {
				t.Errorf("RequiresOverrideReason = %v, want %v", got.RequiresOverrideReason, tc.wantOverrideReq)
			}
			// Guidance must be non-empty for every known type.
			if tc.wantClassification != "" && got.Guidance == "" {
				t.Error("Guidance empty; expected prose explaining the taxonomy rule")
			}
		})
	}
}
