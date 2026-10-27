package tools

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestPoolRetireDevState_DeletionStatuses pins that the retire reads a dev's
// deletion the way a lease release reads a pool's (DP-20): both the plain and
// the service-stack spellings of "being deleted" and "deleted".
func TestPoolRetireDevState_DeletionStatuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status string
		want   string
	}{
		{platform.ServiceStatusActive, poolRetireDevLive},
		{"DELETING", poolRetireDevDeleting},
		{"SERVICE_DELETING", poolRetireDevDeleting},
		{"DELETED", poolRetireDevGone},
		{"SERVICE_DELETED", poolRetireDevGone},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			svc := platform.ServiceStack{ID: "svc-dev", Name: "appdev", Status: tt.status}
			mock := platform.NewMock().WithService(&svc)
			got, err := poolRetireDevState(context.Background(), mock, "svc-dev")
			if err != nil {
				t.Fatalf("poolRetireDevState: %v", err)
			}
			if got != tt.want {
				t.Errorf("status %s: got %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}
