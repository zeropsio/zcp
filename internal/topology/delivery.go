package topology

import "strings"

// DeliveryInputs is the resolved per-pair state the production-delivery
// recommendation keys on — all five fields read from one ServiceMeta, so the
// recommendation derives from the SAME source the launch source-control gate's
// earn-probe reads (no tell≠check drift). It is NOT a choice the agent makes;
// it is facts about the pair.
type DeliveryInputs struct {
	GitPushState     GitPushState
	BuildIntegration BuildIntegration
	Verified         bool   // meta.BuildIntegrationVerifiedAt != "" (earned proof)
	HasStage         bool   // meta.StageHostname != ""
	RemoteURL        string // host detection: gitlab → webhook, else actions
}

// DeliveryDecision is the recommendation RecommendDelivery returns. Recommended
// is the advised CI family; Why is the agent-facing tell (never empty).
// NeedsGitPush and SuggestStageFirst are advisory cross-links, never gates —
// the agent may wire any family in any order.
type DeliveryDecision struct {
	Recommended       BuildIntegration
	Why               string
	NeedsGitPush      bool // GitPushState != configured — set up git-push before any family is meaningful
	SuggestStageFirst bool // configured + none + no stage — CI-on-push is most valuable against a stage
}

// RecommendDelivery is the SINGLE owner of the production-delivery
// recommendation (F4b). The best-practice default is everything through
// git + tokens + CI/CD; the host picks the family (GitHub → Actions, the agent
// can land the workflow file + repo secret via `gh`; GitLab → the dashboard
// OAuth webhook, no equivalent CLI-driven secret wiring). An already-earned or
// declared family is carried forward — never rewrite a working path the user
// chose. Recommendation, never a gate.
//
// Vocabulary discipline: BuildIntegration{none,webhook,actions} is orthogonal
// to GitPushState. "Plain git-push without CI" is BuildIntegration=none +
// GitPushState=configured, not a fourth family. Production delivery is
// pipeline-first (a tag/push triggers a build) — the agent never self-deploys
// to prod from its own machine.
func RecommendDelivery(in DeliveryInputs) DeliveryDecision {
	isGitLab := strings.Contains(strings.ToLower(in.RemoteURL), "gitlab")
	ciFamily := BuildIntegrationActions
	ciWhy := "GitHub Actions — the agent can land the workflow file + repo secret via `gh` without leaving the terminal"
	if isGitLab {
		ciFamily = BuildIntegrationWebhook
		ciWhy = "the Zerops dashboard OAuth webhook — GitLab has no equivalent CLI-driven secret wiring"
	}

	// 1. git-push not configured — no family is meaningful until push works.
	if in.GitPushState != GitPushConfigured {
		return DeliveryDecision{
			Recommended:  ciFamily,
			NeedsGitPush: true,
			Why:          "configure git-push first (build integration requires GitPushState=configured), then wire " + ciWhy,
		}
	}

	// 2. an integration is already declared/earned — carry it forward.
	switch in.BuildIntegration {
	case BuildIntegrationActions:
		why := "source pair already delivers via GitHub Actions — promote the same model"
		if !in.Verified {
			why = "GitHub Actions is declared but not yet verified — finish/verify it; matches the source intent"
		}
		return DeliveryDecision{Recommended: BuildIntegrationActions, Why: why}
	case BuildIntegrationWebhook:
		return DeliveryDecision{
			Recommended: BuildIntegrationWebhook,
			Why:         "source pair already delivers via the Zerops webhook — promote the same; don't force a CI rewrite the user didn't choose",
		}
	case BuildIntegrationNone:
		// No managed CI yet — recommend wiring it (step 3 below).
	}

	// 3. BuildIntegration=none + git-push configured — recommend wiring CI.
	d := DeliveryDecision{Recommended: ciFamily}
	if in.HasStage {
		d.Why = "git-push works and a stage exists — wire CI/CD to build-on-git-push for the stage (" + ciWhy + ")"
	} else {
		d.SuggestStageFirst = true
		d.Why = "git-push works but there's no CI and no stage — recommend the full git + token + CI/CD path and create a stage first (CI-on-push is most valuable against a stage); " + ciWhy
	}
	return d
}
