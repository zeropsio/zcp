package workflow

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// standUpTarget is a pair a Mate's stand-up adopts: an existing standard pair
// whose setups its recipe names.
func standUpTarget() BootstrapTarget {
	return BootstrapTarget{Runtime: RuntimeTarget{
		DevHostname:      "medusadev",
		ExplicitStage:    "medusastage",
		Type:             "nodejs@22",
		BootstrapMode:    topology.PlanModeStandard,
		IsExisting:       true,
		PrimarySetupName: "medusadev",
		StageSetupName:   "medusaprod",
	}}
}

// TestAdoptPair_RecordsWhatTheAdoptRouteRecords pins that a pair the stand-up
// adopts is recorded exactly as the adopt route's close records it — one
// pair-keyed meta, standard, adopted (no bootstrap session), bootstrapped,
// the deploy dimensions at their zero values, public access auto — so every
// gate and envelope reads it the same way; only the setups, which the recipe
// names, are added.
func TestAdoptPair_RecordsWhatTheAdoptRouteRecords(t *testing.T) {
	t.Parallel()

	routeState := filepath.Join(t.TempDir(), ".zcp", "state")
	eng := NewEngine(routeState, EnvContainer, nil)
	if _, err := eng.BootstrapStartWithRoute("p1", "adopt the pair", BootstrapRouteAdopt, ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	routeTarget := standUpTarget()
	routeTarget.Runtime.PrimarySetupName, routeTarget.Runtime.StageSetupName = "", ""
	if _, err := eng.BootstrapCompletePlan([]BootstrapTarget{routeTarget}, nil, nil); err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Complete every step the route still has open; the adopt route closes
	// itself once provision is done.
	for {
		resp, err := eng.BootstrapStatus()
		if err != nil || resp.Current == nil {
			break
		}
		step := resp.Current.Name
		if _, err := eng.BootstrapComplete(context.Background(), step, "Attestation for "+step+" step completed ok", nil); err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
	}
	want, err := ReadServiceMeta(routeState, "medusadev")
	if err != nil || want == nil {
		t.Fatalf("the adopt route wrote no meta: %v", err)
	}

	standUpState := t.TempDir()
	if err := AdoptPair(standUpState, standUpTarget(), want.BootstrappedAt); err != nil {
		t.Fatalf("AdoptPair: %v", err)
	}
	got, err := ReadServiceMeta(standUpState, "medusadev")
	if err != nil || got == nil {
		t.Fatalf("AdoptPair wrote no meta: %v", err)
	}
	if got.PrimarySetupName != "medusadev" || got.StageSetupName != "medusaprod" {
		t.Errorf("setups = %q/%q, want the recipe's medusadev/medusaprod", got.PrimarySetupName, got.StageSetupName)
	}
	got.PrimarySetupName, got.StageSetupName = "", ""
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stand-up meta = %+v\nadopt route meta = %+v", got, want)
	}
	if !got.IsAdopted() || !IsKnownService(standUpState, "medusastage") {
		t.Error("the pair must read as adopted, and its stage half as known (the deploy gate)")
	}
}

// TestAdoptPair_KeepsWhatAnEarlierPassRecorded: adopting again is a no-op on
// everything the pair has earned since — its wiring, its first deploy, a
// setup somebody chose — so a second stand-up call continues rather than
// starting the pair over.
func TestAdoptPair_KeepsWhatAnEarlierPassRecorded(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	earlier := &ServiceMeta{
		Hostname:         "medusadev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "medusastage",
		CloseDeployMode:  topology.CloseModeUnset,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://hq.acme.example/git/a1/medusadev.git",
		BuildIntegration: topology.BuildIntegrationNone,
		BootstrappedAt:   "2026-09-30",
		FirstDeployedAt:  "2026-09-30T10:00:00Z",
		PrimarySetupName: "chosen",
		HQ:               &HQRepoRef{AppID: "a1", Repo: "medusadev", Branch: "mate/p1"},
	}
	if err := WriteServiceMeta(stateDir, earlier); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := AdoptPair(stateDir, standUpTarget(), "2026-10-01"); err != nil {
		t.Fatalf("AdoptPair: %v", err)
	}
	got, _ := ReadServiceMeta(stateDir, "medusadev")
	if got.BootstrappedAt != "2026-09-30" || got.FirstDeployedAt == "" || got.GitPushState != topology.GitPushConfigured ||
		got.RemoteURL == "" || got.HQ == nil || got.PrimarySetupName != "chosen" || got.StageSetupName != "medusaprod" {
		t.Errorf("a second adoption lost what the pair earned: %+v", got)
	}
}

// TestAdoptPair_Refusals: only a dev/stage pair is adopted this way, and a
// stage half already recorded as a service of its own is not silently folded
// into a pair — two metas for one runtime break the pair-keyed invariant.
func TestAdoptPair_Refusals(t *testing.T) {
	t.Parallel()
	t.Run("no stage half", func(t *testing.T) {
		t.Parallel()
		target := standUpTarget()
		target.Runtime.ExplicitStage = ""
		target.Runtime.BootstrapMode = topology.PlanModeDev
		if err := AdoptPair(t.TempDir(), target, "2026-09-30"); err == nil {
			t.Error("a runtime with no stage half is no pair to adopt")
		}
	})
	t.Run("the stage is its own service already", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := WriteServiceMeta(stateDir, NewServiceMeta("medusastage", topology.PlanModeDev)); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := AdoptPair(stateDir, standUpTarget(), "2026-09-30"); err == nil {
			t.Error("a stage half adopted on its own must be named, not folded into the pair")
		}
	})
}
