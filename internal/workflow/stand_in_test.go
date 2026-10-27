package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestComputeEnvelope_PoolMigrateStandIn: inside a Mate, a dev service
// pool-migrate holds as its repository's stand-in — the dev half of a pair
// serving several parts (more than one stage), the step on the way to a dev
// pool per part, and every dev service of a variant-B pair — carries
// standIn {for: pool-migrate} on its snapshot, from the plan that made the
// pair until the retire re-keys it onto its pools; a pair of one stage, a
// pair keyed by its dev pool, every stage, and every snapshot outside a Mate
// carry none (docs/spec-workflows.md §13 PM-14).
func TestComputeEnvelope_PoolMigrateStandIn(t *testing.T) {
	t.Parallel()
	pair := func(stages ...string) *ServiceMeta {
		m := &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeStandard, BootstrapSession: "s", BootstrappedAt: "2026-10-27"}
		for _, s := range stages {
			m.Stages = append(m.Stages, topology.StageRef{Hostname: s})
		}
		return m
	}
	variantB := pair("webstage", "apistage")
	variantB.CodeVolume = &topology.CodeVolume{Hostname: "code", MountPath: "/var/www/code", DevServices: []topology.DevServiceRef{{Hostname: "apidev"}}}
	poolKeyed := pair("webstage")
	poolKeyed.Hostname, poolKeyed.DevPool = "webpool", &DevPoolRef{ServiceID: "svc-webpool"}
	tests := []struct {
		name string
		meta *ServiceMeta
		mate bool
		want []string
	}{
		{"several parts in a Mate", pair("webstage", "apistage"), true, []string{"appdev"}},
		{"several parts outside a Mate", pair("webstage", "apistage"), false, nil},
		{"one part in a Mate", pair("appstage"), true, nil},
		{"variant B in a Mate", variantB, true, []string{"apidev", "appdev"}},
		{"keyed by its dev pool", poolKeyed, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := WriteServiceMeta(dir, tt.meta); err != nil {
				t.Fatal(err)
			}
			services := make([]platform.ServiceStack, 0, 2+len(tt.meta.Stages))
			services = append(services, runtimeService(tt.meta.Hostname), runtimeService("apidev"))
			for _, s := range tt.meta.Stages {
				services = append(services, runtimeService(s.Hostname))
			}
			mock := platform.NewMock().WithServices(services).WithProject(&platform.Project{ID: "p1", Name: "p"})
			env, err := ComputeEnvelope(context.Background(), mock, dir, "p1", runtime.Info{InContainer: true, ServiceName: "zcp", MateEnabled: tt.mate}, fixedTime)
			if err != nil {
				t.Fatalf("ComputeEnvelope: %v", err)
			}
			var got []string
			for _, s := range env.Services {
				if s.StandIn == nil {
					continue
				}
				if s.StandIn.For != topology.StandInPoolMigrate {
					t.Errorf("%s: standIn = %+v, want for pool-migrate", s.Hostname, s.StandIn)
				}
				got = append(got, s.Hostname)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("stand-ins = %v, want %v", got, tt.want)
			}
		})
	}
}

// runtimeService is a running bun service named host.
func runtimeService(host string) platform.ServiceStack {
	return platform.ServiceStack{ID: "svc-" + host, Name: host, Status: platform.ServiceStatusActive,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3", ServiceStackTypeCategoryName: "USER"}}
}

// TestServiceSnapshot_StandInRidesBesideTheParts: on the wire standIn is
// {"for":"pool-migrate"}, after stages[] and devServers[] in the dev
// service's snapshot (docs/spec-mate.md §2.8a.3), and absent otherwise.
func TestServiceSnapshot_StandInRidesBesideTheParts(t *testing.T) {
	t.Parallel()
	snap := ServiceSnapshot{
		Hostname: "appdev", WireStages: []topology.StageWire{{Hostname: "webstage"}},
		DevServers: []topology.DevServerState{{Name: "web", Status: "running"}},
		StandIn:    &topology.StandIn{For: topology.StandInPoolMigrate},
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	at := strings.Index(text, `"standIn":{"for":"pool-migrate"}`)
	if at < 0 || at < strings.Index(text, `"devServers"`) || at < strings.Index(text, `"stages"`) {
		t.Errorf("standIn does not ride after stages and devServers: %s", text)
	}
	snap.StandIn = nil
	raw, err = json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "standIn") {
		t.Errorf("a snapshot that is no stand-in carries the key: %s", raw)
	}
}
