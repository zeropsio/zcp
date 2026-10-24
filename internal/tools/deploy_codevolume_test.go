package tools

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestFillCodeVolumeAfterDeploy_PoolKeyedPair_NeverReachesThePool: a migrated
// variant-B pair is keyed by its pool and still names its code volume until
// the volume reads gone (docs/spec-workflows.md §13 PM-13). A deploy to the
// pool — the image step's — fills nothing and reloads nothing: the pool's
// hostname reaches a random container of it (§8 PS-3). A dev-keyed variant-B
// pair still fills its volume after its dev half deployed.
func TestFillCodeVolumeAfterDeploy_PoolKeyedPair_NeverReachesThePool(t *testing.T) {
	t.Parallel()
	volume := &topology.CodeVolume{Hostname: "vol", MountPath: "/var/www/ws",
		DevServices: []topology.DevServiceRef{{Hostname: "apidev", SetupName: "dev-api"}}}
	tests := []struct {
		name    string
		meta    *workflow.ServiceMeta
		target  string
		wantSSH bool
	}{
		{
			name: "a pool-keyed pair still naming its code volume",
			meta: &workflow.ServiceMeta{
				Hostname: "webpool", Mode: topology.PlanModeStandard, BootstrappedAt: "2026-10-01T00:00:00Z",
				Stages:     []topology.StageRef{{Hostname: "webstage", SetupName: "web"}},
				CodeVolume: volume,
				DevPool:    &workflow.DevPoolRef{ServiceID: "svc-webpool", MigratedFrom: &workflow.MigratedDev{Hostname: "webdev"}},
			},
			target: "webpool",
		},
		{
			name: "a dev-keyed variant-B pair",
			meta: &workflow.ServiceMeta{
				Hostname: "webdev", Mode: topology.PlanModeStandard, BootstrappedAt: "2026-10-01T00:00:00Z",
				Stages:     []topology.StageRef{{Hostname: "webstage", SetupName: "web"}},
				CodeVolume: volume,
			},
			target:  "webdev",
			wantSSH: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if err := workflow.WriteServiceMeta(stateDir, tt.meta); err != nil {
				t.Fatal(err)
			}
			ssh := &recordingSSH{}
			fillCodeVolumeAfterDeploy(context.Background(), ssh, stateDir, tt.target, &ops.DeployResult{})
			ssh.mu.Lock()
			calls := len(ssh.calls[tt.target])
			total := len(ssh.calls)
			ssh.mu.Unlock()
			switch {
			case tt.wantSSH && calls == 0:
				t.Fatalf("no fill reached %s", tt.target)
			case !tt.wantSSH && total > 0:
				t.Fatalf("ssh reached %v after a deploy to the pool", ssh.calls)
			}
		})
	}
}
