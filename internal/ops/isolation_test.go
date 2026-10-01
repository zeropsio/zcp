package ops

import (
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestProjectIsolation reads envIsolation's mode from the project's
// variables: the platform may follow "service" with per-service exceptions,
// and a project that does not carry the variable says nothing.
func TestProjectIsolation(t *testing.T) {
	t.Parallel()
	iso := func(v string) []platform.ProjectEnvVar {
		return []platform.ProjectEnvVar{{Key: "zeropsSubdomainHost", Content: "x"}, {Key: "envIsolation", Content: v, Type: platform.ProjectEnvSystem}}
	}
	tests := []struct {
		name       string
		envs       []platform.ProjectEnvVar
		want       string
		wantClosed bool
	}{
		{"closed off", iso("service"), IsolationService, true},
		{"closed off with an exception", iso("service service@zcp"), IsolationService, true},
		{"open", iso("none"), "none", false},
		{"not said", []platform.ProjectEnvVar{{Key: "zeropsSubdomainHost", Content: "x"}}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ProjectIsolation(tt.envs); got != tt.want {
				t.Errorf("ProjectIsolation = %q, want %q", got, tt.want)
			}
			if got := ProjectClosedOff(tt.envs); got != tt.wantClosed {
				t.Errorf("ProjectClosedOff = %v, want %v", got, tt.wantClosed)
			}
		})
	}
}
