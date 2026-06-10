package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestParseMeta_FoldsLegacyGitPushCloseMode pins the one-way lazy
// migration (spec-git-delivery-target §3/§9): a meta file written by an
// older binary with closeDeployMode="git-push" reads back as auto — the
// delivery mechanism now derives from GitPushState, close-mode records
// done-ness ownership only. Write-side folds too, so the next persist
// completes the migration without a sweep.
func TestParseMeta_FoldsLegacyGitPushCloseMode(t *testing.T) {
	t.Parallel()

	meta, err := parseMeta([]byte(`{"hostname":"appdev","closeDeployMode":"git-push","gitPushState":"configured"}`))
	if err != nil {
		t.Fatalf("parseMeta: %v", err)
	}
	if meta.CloseDeployMode != topology.CloseModeAuto {
		t.Errorf("legacy git-push close-mode must fold to auto on read; got %q", meta.CloseDeployMode)
	}

	// Write-side: persisting a meta still carrying the legacy value folds
	// it before it hits disk.
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname:        "appdev",
		CloseDeployMode: topology.CloseModeGitPush,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	got, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got.CloseDeployMode != topology.CloseModeAuto {
		t.Errorf("write path must fold legacy value; got %q", got.CloseDeployMode)
	}

	// Manual is NOT folded — user ownership survives verbatim.
	manual, err := parseMeta([]byte(`{"hostname":"appdev","closeDeployMode":"manual"}`))
	if err != nil {
		t.Fatalf("parseMeta: %v", err)
	}
	if manual.CloseDeployMode != topology.CloseModeManual {
		t.Errorf("manual must survive the fold untouched; got %q", manual.CloseDeployMode)
	}
}

// TestResolve_ConfiguredDrivesGitPushDelivery pins the ladder pivot
// (spec-git-delivery-target §2, Karel-confirmed 2026-06-10): once
// GitPushState=configured, PUSH is the terminal act for auto AND unset
// close-modes alike — the S5 orthogonality cell ("configured push can
// coexist with auto self-deploy close") is deliberately superseded.
// Manual still yields the loop; the first-deploy bypass still applies.
func TestResolve_ConfiguredDrivesGitPushDelivery(t *testing.T) {
	t.Parallel()

	base := ServiceSnapshot{
		Hostname:      "appdev",
		Mode:          topology.ModeStandard,
		GitPushState:  topology.GitPushConfigured,
		StageHostname: "appstage",
		Deployed:      true,
	}

	for _, cm := range []topology.CloseDeployMode{topology.CloseModeAuto, topology.CloseModeUnset} {
		snap := base
		snap.CloseDeployMode = cm
		intent := Resolve(snap, []ServiceSnapshot{snap})
		if intent.Delivery != DeployDeliveryGitPush {
			t.Errorf("closeMode=%q + configured must resolve git-push delivery (ladder); got %q", cm, intent.Delivery)
		}
		if intent.BuildTarget != "appstage" {
			t.Errorf("closeMode=%q: push must build the stage half; got %q", cm, intent.BuildTarget)
		}
	}

	// Manual yields the whole loop.
	manual := base
	manual.CloseDeployMode = topology.CloseModeManual
	if got := Resolve(manual, []ServiceSnapshot{manual}); got.Delivery != DeployDeliveryManual {
		t.Errorf("manual must stay manual; got %q", got.Delivery)
	}

	// First deploy bypass (D2a) survives: no commits/remote exist yet.
	fresh := base
	fresh.CloseDeployMode = topology.CloseModeAuto
	fresh.Deployed = false
	if got := Resolve(fresh, []ServiceSnapshot{fresh}); got.Delivery != DeployDeliveryDirect {
		t.Errorf("first deploy must bypass to direct; got %q", got.Delivery)
	}

	// L0: unconfigured stays artifact-direct.
	l0 := base
	l0.CloseDeployMode = topology.CloseModeAuto
	l0.GitPushState = topology.GitPushUnconfigured
	if got := Resolve(l0, []ServiceSnapshot{l0}); got.Delivery != DeployDeliveryDirect {
		t.Errorf("unconfigured must stay direct; got %q", got.Delivery)
	}

	// Simple mode (self-target) in L1: push delivery, build target = self.
	simple := ServiceSnapshot{
		Hostname:        "weather",
		Mode:            topology.ModeSimple,
		CloseDeployMode: topology.CloseModeAuto,
		GitPushState:    topology.GitPushConfigured,
		Deployed:        true,
	}
	intent := Resolve(simple, []ServiceSnapshot{simple})
	if intent.Delivery != DeployDeliveryGitPush {
		t.Errorf("simple L1 must deliver via push; got %q", intent.Delivery)
	}
	if intent.BuildTarget != "weather" || !intent.RequiresAsyncAck {
		t.Errorf("simple L1 push builds self asynchronously; got target=%q ack=%v", intent.BuildTarget, intent.RequiresAsyncAck)
	}
}
