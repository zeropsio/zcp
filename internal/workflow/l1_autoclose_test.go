package workflow

import (
	"os"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestL1AutoClose_PushBuildVerifyChain pins the L1 done-evidence chain
// (spec-git-delivery-target §6): a standard pair delivering via push
// auto-closes once (a) the build-watch auto-recorded the landed
// integration build on the BUILD TARGET (the §6.1 watch writes the
// success attempt the manual record-deploy bridge used to), and (b) a
// verify passed AT-OR-AFTER it. The session scope names the dev half;
// the gate evaluates the RESOLVED build target (the 2026-05-19 rule,
// now reached by every configured pair via the ladder).
func TestL1AutoClose_PushBuildVerifyChain(t *testing.T) {
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeAuto,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/app.git",
		FirstDeployedAt:  "2026-06-10T09:00:00Z",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	ws := NewWorkSession("p", "container", "test", []string{"appdev"})
	if err := SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Stage 1: push happened, watch observed the build ACTIVE and
	// auto-recorded the success attempt on the BUILD TARGET (appstage).
	buildAt := time.Now().UTC().Add(-30 * time.Second).Format(time.RFC3339)
	if err := RecordDeployAttempt(dir, "appstage", DeployAttempt{
		AttemptedAt: buildAt, SucceededAt: buildAt, Strategy: "git-push", Setup: "prod",
	}); err != nil {
		t.Fatalf("record deploy: %v", err)
	}
	loaded, err := LoadWorkSession(dir, os.Getpid())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if closed, _, _ := DeriveCloseState(dir, loaded); closed {
		t.Fatal("gate must stay open before verify")
	}

	// Stage 2: verify passes at-or-after the build → chain complete.
	verifyAt := time.Now().UTC().Format(time.RFC3339)
	if err := RecordVerifyAttempt(dir, "appstage", VerifyAttempt{
		AttemptedAt: verifyAt, PassedAt: verifyAt, Passed: true,
	}); err != nil {
		t.Fatalf("record verify: %v", err)
	}
	loaded, err = LoadWorkSession(dir, os.Getpid())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if closed, _, _ := DeriveCloseState(dir, loaded); !closed {
		t.Error("push → build-landed (auto-recorded) → verify must complete the L1 chain and auto-close")
	}
}
