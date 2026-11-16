package workflow

import (
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestWithParts_LeasesAndStagesNameTheirPart: on the wire every lease names
// the part its pool serves — its dev server's name, else the pool-keyed
// pair's stage setup — and the stage that pair pairs with; every stage of a
// pair names its part the same way: a pool-keyed pair's from its leases' dev
// servers, else the stage's setup, else the stage hostname without `stage`. A
// lease of a pool no pair keys, of which zcp knows no dev server, names
// neither (docs/spec-mate.md §2.8a.3).
func TestWithParts_LeasesAndStagesNameTheirPart(t *testing.T) {
	t.Parallel()
	read := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	servers := func(names ...string) []topology.DevServerState {
		out := make([]topology.DevServerState, 0, len(names))
		for _, n := range names {
			out = append(out, topology.DevServerState{Name: n, Status: "running", ReadAt: read})
		}
		return out
	}
	pool := func(host, stage, setup string) ServiceSnapshot {
		return ServiceSnapshot{Hostname: host, RuntimeClass: topology.RuntimePool, DevIsPool: true, Stages: []topology.StageRef{{Hostname: stage, SetupName: setup}}}
	}
	tests := []struct {
		name       string
		services   []ServiceSnapshot
		lease      LeaseSnapshot
		wantPart   string
		wantStage  string
		wantStages map[string]string // stage hostname → part
	}{
		{
			name:     "pool-keyed pair, the lease's dev server named",
			services: []ServiceSnapshot{pool("webpool", "webstage", "webprod")},
			lease:    LeaseSnapshot{Alias: "webpool-3", Pool: "webpool", DevServers: servers("web")},
			wantPart: "web", wantStage: "webstage", wantStages: map[string]string{"webstage": "web"},
		},
		{
			name:     "pool-keyed pair, no dev server known",
			services: []ServiceSnapshot{pool("apipool", "apistage", "api")},
			lease:    LeaseSnapshot{Alias: "apipool-1", Pool: "apipool"},
			wantPart: "api", wantStage: "apistage", wantStages: map[string]string{"apistage": "api"},
		},
		{
			name:     "a pool no pair keys, its dev server known",
			services: nil,
			lease:    LeaseSnapshot{Alias: "boxpool-2", Pool: "boxpool", DevServers: servers("box")},
			wantPart: "box",
		},
		{
			name:     "a pool no pair keys, nothing known",
			services: nil,
			lease:    LeaseSnapshot{Alias: "boxpool-2", Pool: "boxpool"},
		},
		{
			name: "a dev service serving several parts, and a pair of one",
			services: []ServiceSnapshot{
				{Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage"}}},
				{Hostname: "shopdev", Stages: []topology.StageRef{{Hostname: "shopstage", SetupName: "prod"}}},
			},
			lease:      LeaseSnapshot{Alias: "boxpool-2", Pool: "boxpool"},
			wantStages: map[string]string{"webstage": "web", "apistage": "api", "shopstage": "prod"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			leases := []LeaseSnapshot{tt.lease}
			env := StateEnvelope{Services: tt.services, Leases: &leases}.WithParts(nil)
			got := env.LeaseList()[0]
			if got.Part != tt.wantPart || got.Stage != tt.wantStage {
				t.Errorf("lease part/stage = %q/%q, want %q/%q", got.Part, got.Stage, tt.wantPart, tt.wantStage)
			}
			for _, s := range env.Services {
				for _, st := range s.WireStages {
					if want, ok := tt.wantStages[st.Hostname]; ok && st.Part != want {
						t.Errorf("stage %s part = %q, want %q", st.Hostname, st.Part, want)
					}
				}
			}
		})
	}
}
