// Tests for: tools/hq_pair.go — a Mate's dev/stage pair keeps its code in HQ,
// run against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// fillCodeVolume makes the lab's pair variant B the way ops.FillCodeVolume
// does: its checkout, .git included, copied onto a code volume mounted at
// /var/www/dify, recorded on the pair. Returns the volume's directory.
func (l *hqLab) fillCodeVolume() string {
	l.t.Helper()
	copied := filepath.Join(l.t.TempDir(), "volume")
	if err := os.CopyFS(copied, os.DirFS(l.pair)); err != nil {
		l.t.Fatal(err)
	}
	volume := filepath.Join(l.pair, "dify")
	if err := os.Rename(copied, volume); err != nil {
		l.t.Fatal(err)
	}
	if err := workflow.UpsertServiceMeta(l.stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
		m.CodeVolume = &topology.CodeVolume{Hostname: "code", MountPath: "/var/www/dify"}
		return nil
	}); err != nil {
		l.t.Fatal(err)
	}
	return volume
}

// TestAWiredPairDeploysDirectlyAndIsNeverSentToPush: nothing builds a Mate's
// services from its change, so a wired pair's direct deploy proceeds and is
// never told to push instead — its code reaches HQ by the agent's plain git.
func TestAWiredPairDeploysDirectlyAndIsNeverSentToPush(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}},
		BootstrapSession: "test", BootstrappedAt: "2026-10-02", FirstDeployedAt: "2026-10-02T09:00:00Z",
		GitPushState: topology.GitPushConfigured, RemoteURL: "https://hq.example/git/a1/appdev.git",
		BuildIntegration: topology.BuildIntegrationActions,
		HQ:               &workflow.HQRepoRef{AppID: "a1", Repo: "appdev"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"appdev", "appstage"} {
		if r := repoDeliveryRedirect(stateDir, target, "", false); r != nil {
			t.Errorf("%s: a wired pair's direct deploy must proceed", target)
		}
		if w := repoDeliveryDivergenceWarning(context.Background(), stateDir, target, deployShipped{}); w != "" {
			t.Errorf("%s: no push-by-hand warning on a wired pair, got %q", target, w)
		}
	}
}
