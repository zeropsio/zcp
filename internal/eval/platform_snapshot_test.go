package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestCollectBehavioralPlatformEvidence_DirectPositiveStateAndFreshProcesses(t *testing.T) {
	t.Parallel()

	runStart := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	failedReason := "build failed"
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "stale", Name: "stale-search", Status: platform.ServiceStatusFailed}}).
		WithServicesDirect([]platform.ServiceStack{
			{
				ID: "app-1", Name: "appdev", ProjectID: "project-1", Status: platform.ServiceStatusActive,
				SubdomainAccess: true,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
				Ports: []platform.Port{{Port: 3000, Scheme: "http"}},
			},
		}).
		WithProjectProcesses([]platform.Process{
			{ID: "old", ActionName: "stack.build", Status: platform.ProcessStatusFailed, Created: "2026-07-15T09:00:00Z"},
			{
				ID: "fresh", ActionName: "stack.build", Status: platform.ProcessStatusFailed,
				Created: "2026-07-15T10:01:00Z", FailReason: &failedReason,
				ServiceStacks: []platform.ServiceStackRef{{ID: "app-1", Name: "appdev"}},
				AppVersion:    &platform.ProcessAppVersion{Status: platform.BuildStatusBuildFailed},
			},
		})
	scenario := &Scenario{Verification: &VerificationConfig{
		ExpectedServices:  []ExpectedService{{Hostname: "appdev", Status: []string{platform.ServiceStatusActive}, Type: "nodejs@*"}},
		NoFailedProcesses: true,
	}}

	snapshot, findings := CollectBehavioralPlatformEvidence(
		context.Background(), scenario, "project-1", client, nil, "", runStart,
	)

	if snapshot.FormatVersion != PlatformSnapshotFormat1 || snapshot.ProjectID != "project-1" {
		t.Fatalf("snapshot identity = %+v", snapshot)
	}
	if len(snapshot.Services) != 1 || snapshot.Services[0].Hostname != "appdev" || snapshot.Services[0].Type != "nodejs@22" || !snapshot.Services[0].SubdomainEnabled {
		t.Fatalf("snapshot services = %+v", snapshot.Services)
	}
	if len(snapshot.Processes) != 1 || snapshot.Processes[0].ID != "fresh" || snapshot.Processes[0].AppVersionStatus != platform.BuildStatusBuildFailed {
		t.Fatalf("snapshot processes = %+v", snapshot.Processes)
	}
	if len(findings) != 1 || findings[0].Check != "no_failed_processes" || !strings.Contains(findings[0].Message, "fresh") {
		t.Fatalf("findings = %+v", findings)
	}
	if len(snapshot.VerificationFindings) != 1 || snapshot.VerificationFindings[0] != findings[0] {
		t.Fatalf("snapshot findings = %+v, returned = %+v", snapshot.VerificationFindings, findings)
	}
	if client.CallCounts["ListServicesDirect"] != 1 || client.CallCounts["GetProjectProcessesDirect"] != 1 {
		t.Fatalf("direct call counts = %+v", client.CallCounts)
	}
	if client.CallCounts["ListServices"] != 0 || client.CallCounts["SearchProcesses"] != 0 {
		t.Fatalf("snapshot used ES-backed reads: %+v", client.CallCounts)
	}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	for _, forbidden := range []string{"ports", "customAutoscaling", "activeAppVersion", "environment", "credential"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("snapshot leaked non-allowlisted field %q: %s", forbidden, encoded)
		}
	}
}

func TestCollectBehavioralPlatformEvidence_QueryFailuresAreDiagnosticsNotEmptyTruth(t *testing.T) {
	t.Parallel()

	client := platform.NewMock().
		WithError("ListServicesDirect", errors.New("service read failed")).
		WithError("GetProjectProcessesDirect", errors.New("process read failed"))
	scenario := &Scenario{Verification: &VerificationConfig{
		ExpectedServices:  []ExpectedService{{Hostname: "appdev", Status: []string{platform.ServiceStatusActive}}},
		NoFailedProcesses: true,
	}}

	snapshot, findings := CollectBehavioralPlatformEvidence(
		context.Background(), scenario, "project-1", client, nil, "", time.Now(),
	)
	if len(snapshot.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want service and process errors", snapshot.Diagnostics)
	}
	if snapshot.Services != nil || snapshot.Processes != nil {
		t.Fatalf("failed reads must stay unknown (nil), got services=%v processes=%v", snapshot.Services, snapshot.Processes)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want two explicit query failures", findings)
	}
	if findings[0].Check != "platform_query" || findings[1].Check != "no_failed_processes" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestWritePlatformSnapshot_RoundTripPrivately(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := PlatformSnapshot{
		FormatVersion:        PlatformSnapshotFormat1,
		ProjectID:            "project-1",
		ObservedAt:           time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC),
		Services:             []PlatformSnapshotService{},
		Processes:            []PlatformSnapshotProcess{},
		Diagnostics:          []PlatformSnapshotDiagnostic{},
		VerificationFindings: []VerificationFinding{},
	}
	if err := WritePlatformSnapshot(dir, want); err != nil {
		t.Fatalf("WritePlatformSnapshot: %v", err)
	}
	path := filepath.Join(dir, "platform-snapshot.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var got PlatformSnapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if got.FormatVersion != want.FormatVersion || got.ProjectID != want.ProjectID || !got.ObservedAt.Equal(want.ObservedAt) {
		t.Fatalf("snapshot round trip = %+v, want %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %#o, want 0600", info.Mode().Perm())
	}
}
