package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// seedL1Meta writes a configured, first-deployed pair — the L1 (repo
// delivery) state of the ladder — carrying the ZCP-managed integration
// that consumes its pushes. The redirect is keyed on that integration:
// only a declared one actually rebuilds the build target from a push.
func seedL1Meta(t *testing.T, stateDir, host string, mode topology.Mode, stage string, integration topology.BuildIntegration) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         host,
		Mode:             mode,
		StageHostname:    stage,
		GitPushState:     topology.GitPushConfigured,
		BuildIntegration: integration,
		RemoteURL:        "https://github.com/example/app.git",
		FirstDeployedAt:  "2026-06-10T10:00:00Z",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestRepoDeliveryRedirect_L1DirectDeploy pins the terminal-act rule
// (spec-git-delivery-target §2, Karel-confirmed): a direct deploy on an
// L1 pair redirects to the push call — with the break-glass escape and
// its guarantees named, never a dead end.
func TestRepoDeliveryRedirect_L1DirectDeploy(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	seedL1Meta(t, stateDir, "weather", topology.PlanModeSimple, "", topology.BuildIntegrationWebhook)

	res := repoDeliveryRedirect(stateDir, "weather", "", false)
	if res == nil {
		t.Fatal("L1 direct deploy must redirect to push delivery")
	}
	body := extractText(res)
	for _, want := range []string{
		"push-delivery-required",
		`"deliveryState":"repo"`,
		`"strategy":"git-push"`,
		"breakGlass",
		"source of truth",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("redirect missing %q; got: %s", want, body)
		}
	}
}

// TestRepoDeliveryRedirect_Passthroughs pins every proceed case: push
// delivery itself, break-glass, L0 (unconfigured), first-deploy bypass,
// and no-meta targets (recipe sessions / other gates own those).
func TestRepoDeliveryRedirect_Passthroughs(t *testing.T) {
	t.Parallel()

	t.Run("push strategy proceeds", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		seedL1Meta(t, stateDir, "weather", topology.PlanModeSimple, "", topology.BuildIntegrationWebhook)
		if res := repoDeliveryRedirect(stateDir, "weather", deployStrategyGitPush, false); res != nil {
			t.Errorf("push delivery must proceed; got: %s", extractText(res))
		}
	})
	t.Run("break-glass proceeds", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		seedL1Meta(t, stateDir, "weather", topology.PlanModeSimple, "", topology.BuildIntegrationWebhook)
		if res := repoDeliveryRedirect(stateDir, "weather", "", true); res != nil {
			t.Errorf("break-glass must proceed; got: %s", extractText(res))
		}
	})
	t.Run("L0 unconfigured proceeds", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname: "weather", Mode: topology.PlanModeSimple,
			FirstDeployedAt:  "2026-06-10T10:00:00Z",
			BootstrapSession: "test", BootstrappedAt: "2026-06-10",
		}); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
		if res := repoDeliveryRedirect(stateDir, "weather", "", false); res != nil {
			t.Errorf("L0 must keep the artifact flow; got: %s", extractText(res))
		}
	})
	t.Run("first deploy bypass proceeds", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname: "weather", Mode: topology.PlanModeSimple,
			GitPushState:     topology.GitPushConfigured,
			RemoteURL:        "https://github.com/example/app.git",
			BootstrapSession: "test", BootstrappedAt: "2026-06-10",
		}); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
		if res := repoDeliveryRedirect(stateDir, "weather", "", false); res != nil {
			t.Errorf("first deploy (D2a) must proceed direct; got: %s", extractText(res))
		}
	})
	t.Run("no meta proceeds", func(t *testing.T) {
		t.Parallel()
		if res := repoDeliveryRedirect(t.TempDir(), "ghost", "", false); res != nil {
			t.Errorf("meta-less target must fall through to other gates; got: %s", extractText(res))
		}
	})
}

// TestRepoDeliveryRedirect_PairTargetsRedirectToo pins that the stage
// half of an L1 pair is covered (cross-deploy dev→stage was the other
// direct path; stage builds from the repo now).
func TestRepoDeliveryRedirect_PairTargetsRedirectToo(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	seedL1Meta(t, stateDir, "appdev", topology.PlanModeStandard, "appstage", topology.BuildIntegrationActions)

	res := repoDeliveryRedirect(stateDir, "appstage", "", false)
	if res == nil {
		t.Fatal("L1 stage target must redirect (stage builds from the repo)")
	}
	body := extractText(res)
	if !strings.Contains(body, `"buildTarget":"appstage"`) {
		t.Errorf("redirect should name the stage build target; got: %s", body)
	}
	if !strings.Contains(body, `"targetService":"appdev"`) {
		t.Errorf("recommended push must originate from the dev half; got: %s", body)
	}
}

// TestRepoDeliveryRedirect_NoIntegrationProceeds pins the case the
// redirect used to deadlock. Measured on a live Mate: a standard pair
// with git-push configured but NO ZCP-managed integration had its
// dev→stage promotion refused with push-delivery-required; the push the
// refusal recommended then answered "nothing rebuilds appstage — re-run
// the deploy with breakGlass=true", i.e. two round trips back to the
// refused call, with an outage escape as the only route through a
// routine promotion. A push only delivers where an integration consumes
// it, so with `none` (or the bootstrap's empty default) the direct
// deploy IS the delivery and proceeds.
func TestRepoDeliveryRedirect_NoIntegrationProceeds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		integration topology.BuildIntegration
	}{
		{name: "declared none", integration: topology.BuildIntegrationNone},
		{name: "bootstrap empty", integration: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			seedL1Meta(t, stateDir, "appdev", topology.PlanModeStandard, "appstage", tc.integration)

			if res := repoDeliveryRedirect(stateDir, "appstage", "", false); res != nil {
				t.Errorf("no integration consumes the push — promotion must proceed; got: %s", extractText(res))
			}
			if res := repoDeliveryRedirect(stateDir, "appdev", "", false); res != nil {
				t.Errorf("no integration consumes the push — self-deploy must proceed; got: %s", extractText(res))
			}
		})
	}
}

// TestBatchRepoDeliveryRedirect pins that zerops_deploy_batch honours
// the same terminal-act rule as the single deploy — it bypassed the gate
// entirely, so a batch was a silent route around push delivery. Batch
// targets carry no breakGlass field, so the refusal must name the single
// deploy as the escape rather than dead-ending the agent.
func TestBatchRepoDeliveryRedirect(t *testing.T) {
	t.Parallel()

	t.Run("integration declared refuses and names both routes", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		seedL1Meta(t, stateDir, "appdev", topology.PlanModeStandard, "appstage", topology.BuildIntegrationWebhook)

		res := batchRepoDeliveryRedirect(stateDir, []ops.DeployBatchTarget{
			{TargetService: "cache"},
			{SourceService: "appdev", TargetService: "appstage"},
		})
		if res == nil {
			t.Fatal("an L1 batch entry must refuse — batch was bypassing push delivery")
		}
		body := extractText(res)
		for _, want := range []string{"push-delivery-required", `"strategy":"git-push"`, "breakGlass"} {
			if !strings.Contains(body, want) {
				t.Errorf("batch refusal missing %q; got: %s", want, body)
			}
		}
	})

	t.Run("no integration proceeds", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		seedL1Meta(t, stateDir, "appdev", topology.PlanModeStandard, "appstage", topology.BuildIntegrationNone)

		if res := batchRepoDeliveryRedirect(stateDir, []ops.DeployBatchTarget{
			{SourceService: "appdev", TargetService: "appstage"},
		}); res != nil {
			t.Errorf("no integration consumes the push — batch must proceed; got: %s", extractText(res))
		}
	})
}

// TestRepoDeliveryDivergenceWarning pins the break-glass aftermath flag.
func TestRepoDeliveryDivergenceWarning(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	seedL1Meta(t, stateDir, "weather", topology.PlanModeSimple, "", topology.BuildIntegrationWebhook)

	warn := repoDeliveryDivergenceWarning(stateDir, "weather")
	if !strings.Contains(warn, "AHEAD of the configured repo") || !strings.Contains(warn, `strategy="git-push"`) {
		t.Errorf("divergence warning must name the reconcile push; got: %q", warn)
	}
	if w := repoDeliveryDivergenceWarning(t.TempDir(), "ghost"); w != "" {
		t.Errorf("no warning without L1 meta; got %q", w)
	}
}
