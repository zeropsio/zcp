// Tests for: ops/closed_off.go — whether a Mate's project is closed off, read from Zerops.
package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestProjectClosedOff: a project is closed off when Zerops says so — its
// envIsolation's first word is `service` (the container recipe's own
// `service service@zcp` too) and no ZCP_API_KEY is left project-wide, where
// isolation does not reach. A read that fails, or one that has not caught up
// with the project's birth (no envIsolation yet), is "not known", never
// "open" or "closed".
func TestProjectClosedOff(t *testing.T) {
	t.Parallel()
	isolation := func(content string) platform.ProjectEnvVar {
		return platform.ProjectEnvVar{ID: "e-iso", Key: "envIsolation", Content: content, Type: platform.ProjectEnvSystem}
	}
	key := platform.ProjectEnvVar{ID: "e-key", Key: "ZCP_API_KEY", Content: "k", Type: platform.ProjectEnvUser}
	tests := []struct {
		name    string
		env     []platform.ProjectEnvVar
		readErr error
		want    bool
		wantErr bool
	}{
		{"isolated per service", []platform.ProjectEnvVar{isolation("service")}, nil, true, false},
		{"the container recipe's own isolation", []platform.ProjectEnvVar{isolation(" service service@zcp ")}, nil, true, false},
		{"open", []platform.ProjectEnvVar{isolation("none")}, nil, false, false},
		{"isolated with the key still project-wide", []platform.ProjectEnvVar{isolation("service"), key}, nil, false, false},
		{"a read that has not caught up", []platform.ProjectEnvVar{key}, nil, false, true},
		{"a read that failed", nil, errors.New("api down"), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithProjectEnv(tt.env)
			if tt.readErr != nil {
				mock = mock.WithError("GetProjectEnv", tt.readErr)
			}
			got, err := ProjectClosedOff(context.Background(), mock, "proj-1")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want an error: %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("closed off = %v, want %v", got, tt.want)
			}
		})
	}
}
