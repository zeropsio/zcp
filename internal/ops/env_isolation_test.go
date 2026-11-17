package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// witness: internal/ops/env_isolation.go | return strings.Join(append(rules, grant), " "), false, false → return grant, false, false
func TestAdmitContainerToProjectEnv_ProjectNotAdmittingTheContainer_GainsItKeepingEveryOtherRule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, before, after string
		changed, refused    bool
	}{
		{name: "default isolation gains the container", before: "service", after: "service service@zcp", changed: true},
		{name: "other grants and blocks are kept", before: "service service@api -service@worker", after: "service service@api -service@worker service@zcp", changed: true},
		{name: "already admitted is not written", before: "service service@zcp", after: "service service@zcp"},
		{name: "open project admits the container", before: "none -service@api", after: "none -service@api"},
		{name: "a block on the container is kept and refused", before: "none -service@zcp", after: "none -service@zcp", refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithProjectEnv([]platform.ProjectEnvVar{
				{ID: "env-ssh", Key: "sshIsolation", Content: "vpn service@zcp", Type: platform.ProjectEnvSystem, Editable: true},
				{ID: "env-iso", Key: "envIsolation", Content: tc.before, Type: platform.ProjectEnvSystem, Editable: true},
			})
			got, err := AdmitContainerToProjectEnv(context.Background(), client, "proj", "zcp")
			if (err != nil) != tc.refused {
				t.Fatalf("err = %v, refused want %v", err, tc.refused)
			}
			if got.Changed != tc.changed {
				t.Fatalf("Changed = %v, want %v", got.Changed, tc.changed)
			}
			envs, _ := client.GetProjectEnv(context.Background(), "proj")
			if envs[1].Content != tc.after || envs[0].Content != "vpn service@zcp" {
				t.Fatalf("project env = %q / %q, want envIsolation %q and sshIsolation untouched", envs[1].Content, envs[0].Content, tc.after)
			}
		})
	}
}
