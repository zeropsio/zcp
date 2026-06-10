package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func seedEvidenceMeta(t *testing.T, stateDir string) {
	t.Helper()
	if err := WriteServiceMeta(stateDir, &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		StageSetupName:   "prod",
		PrimarySetupName: "appdev",
		BootstrapSession: "t",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestRecordVerifiedSetup_RoundTrip pins the F4 durable evidence: a verify
// pass recorded against either pair half lands in the pair-keyed sidecar
// and survives independently of any work session.
func TestRecordVerifiedSetup_RoundTrip(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	seedEvidenceMeta(t, stateDir)

	ev := VerifiedSetupEvidence{
		SetupName:      "prod",
		TargetHostname: "appstage",
		VerifiedAt:     "2026-06-10T12:00:00Z",
		Summary:        "healthy",
	}
	if err := RecordVerifiedSetup(stateDir, "appstage", ev); err != nil {
		t.Fatalf("RecordVerifiedSetup: %v", err)
	}

	// Pair-keyed: the sidecar lives under the canonical dev-half hostname.
	if _, err := os.Stat(filepath.Join(stateDir, "services", "appdev.verified.json")); err != nil {
		t.Fatalf("sidecar not at pair-keyed path: %v", err)
	}

	got, err := ReadVerifiedSetups(stateDir, "appdev")
	if err != nil {
		t.Fatalf("ReadVerifiedSetups: %v", err)
	}
	if got["prod"].VerifiedAt != ev.VerifiedAt || got["prod"].TargetHostname != "appstage" {
		t.Errorf("evidence round-trip mismatch: %+v", got["prod"])
	}

	// Latest-wins per setup.
	ev2 := ev
	ev2.VerifiedAt = "2026-06-10T13:00:00Z"
	if err := RecordVerifiedSetup(stateDir, "appdev", ev2); err != nil {
		t.Fatalf("RecordVerifiedSetup #2: %v", err)
	}
	got, _ = ReadVerifiedSetups(stateDir, "appstage")
	if got["prod"].VerifiedAt != ev2.VerifiedAt {
		t.Errorf("latest evidence must win: %+v", got["prod"])
	}
}

// TestRecordVerifiedSetup_NoMetaNoop pins that evidence without an anchor
// (unknown hostname / no meta) is a silent no-op, and an empty setup name
// records nothing.
func TestRecordVerifiedSetup_NoMetaNoop(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := RecordVerifiedSetup(stateDir, "ghost", VerifiedSetupEvidence{SetupName: "prod", VerifiedAt: "t"}); err != nil {
		t.Fatalf("no-meta record must not error: %v", err)
	}
	seedEvidenceMeta(t, stateDir)
	if err := RecordVerifiedSetup(stateDir, "appdev", VerifiedSetupEvidence{SetupName: "", VerifiedAt: "t"}); err != nil {
		t.Fatalf("empty-setup record must not error: %v", err)
	}
	got, _ := ReadVerifiedSetups(stateDir, "appdev")
	if len(got) != 0 {
		t.Errorf("expected no evidence, got %+v", got)
	}
}
