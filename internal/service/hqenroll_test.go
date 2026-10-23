package service

import (
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

// White-box: the enroller keepEnrolled builds over the container's live
// environment names the container's own project and zcp service — a project
// holds one Mate, and HQ refuses any service but the one its record names.
func TestMateEnroller_NamesTheContainersProjectAndService(t *testing.T) {
	t.Parallel()
	env := map[string]string{"projectId": "p-mate", "serviceId": "svc-zcp"}

	got := mateEnroller(func(key string) string { return env[key] }, nil)

	if got.ProjectID != "p-mate" || got.ServiceID != "svc-zcp" {
		t.Errorf("enroller = project %q service %q, want p-mate svc-zcp", got.ProjectID, got.ServiceID)
	}
	if got.Path != hq.EnrollmentPath() {
		t.Errorf("path = %q, want %q", got.Path, hq.EnrollmentPath())
	}
}

// White-box: the HQ address the container's live environment names is the
// enroller's, so a Mate is pointed at an HQ by its launch environment; unset,
// the enroller names none and the org's anchor names the HQ.
func TestMateEnroller_TakesTheHQAddressFromTheLiveEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"set", map[string]string{"projectId": "p-mate", "ZCP_MATE_HQ_ADDRESS": "https://hq-staging.example"}, "https://hq-staging.example"},
		{"unset", map[string]string{"projectId": "p-mate"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mateEnroller(func(key string) string { return tt.env[key] }, nil)
			if got.Address != tt.want {
				t.Errorf("address = %q, want %q", got.Address, tt.want)
			}
		})
	}
}
