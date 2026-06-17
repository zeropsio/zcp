package topology

import "testing"

// TestRecommendDelivery_Matrix pins every cell of the production-delivery
// recommendation matrix (F4b). RecommendDelivery is the SINGLE owner of the
// recommendation so the git-push-setup response, the build-integration
// walkthrough, and the launch first-release rendering cannot drift on which
// family they advise. Recommendation, never a gate.
func TestRecommendDelivery_Matrix(t *testing.T) {
	t.Parallel()
	const gh = "https://github.com/me/app"
	const gl = "https://gitlab.com/me/app"

	tests := []struct {
		name        string
		in          DeliveryInputs
		wantFamily  BuildIntegration
		wantGitPush bool
		wantStage   bool
	}{
		{
			name:        "not configured (github) → actions, needs git-push first",
			in:          DeliveryInputs{GitPushState: GitPushUnconfigured, RemoteURL: gh},
			wantFamily:  BuildIntegrationActions,
			wantGitPush: true,
		},
		{
			name:        "not configured (gitlab) → webhook, needs git-push first",
			in:          DeliveryInputs{GitPushState: GitPushUnconfigured, RemoteURL: gl},
			wantFamily:  BuildIntegrationWebhook,
			wantGitPush: true,
		},
		{
			name:       "configured + actions + verified → actions",
			in:         DeliveryInputs{GitPushState: GitPushConfigured, BuildIntegration: BuildIntegrationActions, Verified: true, RemoteURL: gh},
			wantFamily: BuildIntegrationActions,
		},
		{
			name:       "configured + actions + unverified → actions (carry intent)",
			in:         DeliveryInputs{GitPushState: GitPushConfigured, BuildIntegration: BuildIntegrationActions, Verified: false, RemoteURL: gh},
			wantFamily: BuildIntegrationActions,
		},
		{
			name:       "configured + webhook → webhook (don't force CI rewrite)",
			in:         DeliveryInputs{GitPushState: GitPushConfigured, BuildIntegration: BuildIntegrationWebhook, RemoteURL: gh},
			wantFamily: BuildIntegrationWebhook,
		},
		{
			name:       "configured + none + has-stage (github) → actions, no stage nudge",
			in:         DeliveryInputs{GitPushState: GitPushConfigured, BuildIntegration: BuildIntegrationNone, HasStage: true, RemoteURL: gh},
			wantFamily: BuildIntegrationActions,
			wantStage:  false,
		},
		{
			name:       "configured + none + has-stage (gitlab) → webhook",
			in:         DeliveryInputs{GitPushState: GitPushConfigured, BuildIntegration: BuildIntegrationNone, HasStage: true, RemoteURL: gl},
			wantFamily: BuildIntegrationWebhook,
			wantStage:  false,
		},
		{
			name:       "configured + none + no-stage (github) → actions + create-stage-first",
			in:         DeliveryInputs{GitPushState: GitPushConfigured, BuildIntegration: BuildIntegrationNone, HasStage: false, RemoteURL: gh},
			wantFamily: BuildIntegrationActions,
			wantStage:  true,
		},
		{
			name:       "configured + none + no-stage (gitlab) → webhook + create-stage-first",
			in:         DeliveryInputs{GitPushState: GitPushConfigured, BuildIntegration: BuildIntegrationNone, HasStage: false, RemoteURL: gl},
			wantFamily: BuildIntegrationWebhook,
			wantStage:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RecommendDelivery(tt.in)
			if got.Recommended != tt.wantFamily {
				t.Errorf("Recommended = %q, want %q", got.Recommended, tt.wantFamily)
			}
			if got.NeedsGitPush != tt.wantGitPush {
				t.Errorf("NeedsGitPush = %v, want %v", got.NeedsGitPush, tt.wantGitPush)
			}
			if got.SuggestStageFirst != tt.wantStage {
				t.Errorf("SuggestStageFirst = %v, want %v", got.SuggestStageFirst, tt.wantStage)
			}
			if got.Why == "" {
				t.Error("Why must never be empty — it IS the agent-facing tell")
			}
		})
	}
}
