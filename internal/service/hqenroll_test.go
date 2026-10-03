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
