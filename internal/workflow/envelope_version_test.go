package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// VER-STATES — A stage carries its ACTIVE version on a Mate's envelope only,
// with a commit only when a commit deploy named it.
// witness: internal/workflow/compute_envelope.go | if !rt.MateEnabled || read == nil { → if read == nil {
func TestStageVersion_VERStates_ActiveVersionOnly(t *testing.T) {
	t.Parallel()
	// The recorded rows (e2e/recordings/project-service-stack.json): an
	// ACTIVE CLI-built version and a startWithoutCode placeholder.
	const (
		activeID      = "REC0000000000000000032"
		placeholderID = "REC0000000000000000044"
		newerID       = "REC0000000000000000099"
		devID         = "REC0000000000000000098"
		created       = "2026-01-01T00:00:00Z"
		commit        = "0123456789abcdef0123456789abcdef01234567"
		sha256Commit  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		newerCommit   = "89abcdef0123456789abcdef0123456789abcdef"
		candidateHash = "fedcba9876543210fedcba9876543210fedcba98"
	)
	active := &platform.ActiveAppVersionDigest{ID: activeID, Created: created, Source: "CLI", Built: true}
	placeholder := &platform.ActiveAppVersionDigest{ID: placeholderID, Created: created, Source: platform.AppVersionSourceNone}
	for _, tc := range []struct {
		name   string
		mate   bool
		active *platform.ActiveAppVersionDigest
		names  map[string]string
		err    error
		want   *StageVersion
	}{
		{"named by a commit deploy", true, active, map[string]string{activeID: "zcp " + commit}, nil,
			&StageVersion{ID: activeID, Commit: commit, At: created}},
		{"named by a commit deploy of a SHA-256 commit", true, active, map[string]string{activeID: "zcp " + sha256Commit}, nil,
			&StageVersion{ID: activeID, Commit: sha256Commit, At: created}},
		{"named by a Review build's candidate", true, active, map[string]string{activeID: "zcp review " + candidateHash}, nil,
			&StageVersion{ID: activeID, At: created}},
		{"named by its commit outside a commit deploy", true, active, map[string]string{activeID: commit}, nil,
			&StageVersion{ID: activeID, At: created}},
		{"a newer version building or failed above the active one", true, active,
			map[string]string{newerID: "zcp " + newerCommit, activeID: "zcp " + commit}, nil,
			&StageVersion{ID: activeID, Commit: commit, At: created}},
		{"no active version", true, nil, map[string]string{}, nil, nil},
		{"the startWithoutCode placeholder", true, placeholder, map[string]string{placeholderID: ""}, nil, nil},
		{"zerops does not answer", true, active, nil, errors.New("app version search: 503 Service Unavailable"), nil},
		{"zerops refuses", true, active, nil, errors.New("app version search: 403 Forbidden"), nil},
		{"the search does not return the active version yet", true, active, map[string]string{newerID: "zcp " + newerCommit}, nil, nil},
		{"not a Mate", false, active, map[string]string{activeID: "zcp " + commit}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := WriteServiceMeta(dir, &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeStandard,
				Stages: []topology.StageRef{{Hostname: "appstage"}}, BootstrapSession: "s", BootstrappedAt: "2026-10-02"}); err != nil {
				t.Fatal(err)
			}
			node := platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}
			client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{
				{ID: "dev-id", Name: "appdev", Status: StatusActive, ServiceStackTypeInfo: node,
					ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: devID, Created: created, Source: "CLI", Built: true}},
				{ID: "stage-id", Name: "appstage", Status: StatusActive, ServiceStackTypeInfo: node, ActiveAppVersion: tc.active},
			}).WithProject(&platform.Project{ID: "proj-1", Name: "demo"})
			answers := map[string]map[string]string{"dev-id": {devID: "zcp " + newerCommit}, "stage-id": tc.names}
			read := func(_ context.Context, serviceID string) (map[string]string, error) {
				return answers[serviceID], tc.err
			}
			rt := runtime.Info{InContainer: true, ServiceName: "zcp", ProjectID: "proj-1", MateEnabled: tc.mate}

			base, err := ComputeEnvelope(context.Background(), client, dir, "proj-1", rt, fixedTime)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ComputeEnvelope(context.Background(), client, dir, "proj-1", rt, fixedTime, WithVersionNames(read))
			if err != nil {
				t.Fatalf("an envelope with a stage version read: %v", err)
			}
			want := base
			want.Services = slices.Clone(base.Services)
			for i := range want.Services {
				if want.Services[i].Hostname == "appstage" {
					want.Services[i].Version = tc.want
				}
			}
			if !reflect.DeepEqual(got, want) {
				gotJSON, gotErr := json.Marshal(got)
				wantJSON, wantErr := json.Marshal(want)
				t.Errorf("envelope differs beyond the stage's version:\n got %s %v\nwant %s %v", gotJSON, gotErr, wantJSON, wantErr)
			}
		})
	}
}
