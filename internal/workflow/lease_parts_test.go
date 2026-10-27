package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
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

// TestComputeEnvelope_LeaseCarriesLastKnownDevServers: every envelope's lease
// carries its last-known dev servers and when they were read
// (devServersReadAt) from zcp's record of the last read of its supervisor —
// not only the call that read them; WithParts keeps them, and a call that
// reads them fresh puts its own read and time in their place.
func TestComputeEnvelope_LeaseCarriesLastKnownDevServers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLeaseRecordFile(t, dir, "devpool-3", `{"poolHostname":"devpool","containerNumber":3,"containerHostname":"node-id-3.runtime.devpool.zerops",`+
		`"lease":"lease-secret","branch":"feat-x","acquiredAt":"2026-10-23T12:00:00Z",`+
		`"devServers":[{"name":"web","port":3000,"url":"https://devpool-abc1-9003.prg1.zerops.app","status":"running","readAt":"2026-10-27T11:00:00Z"}],`+
		`"devServersReadAt":"2026-10-27T11:00:00Z"}`)
	held := LeaseSnapshot{Alias: "devpool-3", Pool: "devpool", Branch: "feat-x", ContainerHostname: "node-id-3.runtime.devpool.zerops", State: "held"}
	for name, reader := range map[string]func(context.Context) ([]LeaseSnapshot, error){
		"the reader answered":         func(context.Context) ([]LeaseSnapshot, error) { return []LeaseSnapshot{held}, nil },
		"the records stood in for it": func(context.Context) ([]LeaseSnapshot, error) { return nil, context.DeadlineExceeded },
	} {
		env, err := ComputeEnvelope(context.Background(), nil, dir, "", runtime.Info{InContainer: true, ServiceName: "zcp"}, fixedTime, WithLeaseReader(reader))
		if err != nil {
			t.Fatalf("%s: ComputeEnvelope: %v", name, err)
		}
		for _, wire := range []StateEnvelope{env, env.WithParts(nil)} {
			got := wire.LeaseList()
			if len(got) != 1 || len(got[0].DevServers) != 1 || got[0].DevServers[0].URL != "https://devpool-abc1-9003.prg1.zerops.app" ||
				got[0].DevServersReadAt != "2026-10-27T11:00:00Z" {
				t.Errorf("%s: leases = %+v, want the last-known dev server and its read time", name, got)
			}
		}
		fresh := time.Date(2026, 10, 27, 12, 30, 0, 0, time.UTC)
		got := env.WithParts(map[string][]topology.DevServerState{"devpool-3": {{Name: "web", Status: "crashed", ReadAt: fresh}}}).LeaseList()
		if len(got) != 1 || got[0].DevServers[0].Status != "crashed" || got[0].DevServersReadAt != "2026-10-27T12:30:00Z" {
			t.Errorf("%s: a fresh read = %+v, want it and its own read time", name, got)
		}
	}
}
