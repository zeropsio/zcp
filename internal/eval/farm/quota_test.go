package farm

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestQuotaWindow_CapsRunWindowToOrgHeadroom pins the batch quota
// preflight: before a batch the farm reads how many projects the org
// already holds and never keeps more run projects alive than fit under the
// org's project limit with QuotaHeadroom left free; with no room at all the
// batch refuses instead of failing every run on the platform's limit.
func TestQuotaWindow_CapsRunWindowToOrgHeadroom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		projects     int
		requested    int
		wantWindow   int
		wantErrMatch string
	}{
		{name: "room for the requested window", projects: 20, requested: 8, wantWindow: 8},
		{name: "requested window capped", projects: 41, requested: 8, wantWindow: 5},
		{name: "unlimited window capped", projects: 28, requested: 0, wantWindow: 18},
		{name: "exactly one slot", projects: 45, requested: 16, wantWindow: 1},
		{name: "no room refuses", projects: 46, requested: 8, wantErrMatch: "holds 46 of 50 projects"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			const clientID = "client-quota"
			f := newControllerFixture(t, clientID)
			for i := range tt.projects {
				f.account.seedProject(fmt.Sprintf("existing-%d", i))
			}

			got, err := QuotaWindow(context.Background(), f.client, clientID, tt.requested)
			if tt.wantErrMatch != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrMatch) {
					t.Fatalf("QuotaWindow error = %v, want one naming %q", err, tt.wantErrMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("QuotaWindow: %v", err)
			}
			if got.Projects != tt.projects || got.Window != tt.wantWindow {
				t.Errorf("QuotaWindow = %+v, want projects %d window %d", got, tt.projects, tt.wantWindow)
			}
		})
	}
}
