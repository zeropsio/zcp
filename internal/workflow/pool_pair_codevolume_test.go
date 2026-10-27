package workflow

import (
	"reflect"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// A variant-B pair (docs/spec-mate.md §2.8a.4a) keeps its repository on a code
// volume several dev services mount. The pool migration moves it like any
// pair, and the code volume rides the pool-keyed pair until the volume reads
// gone: the retire deletes every dev service, then the volume, and only then
// is CodeVolume cleared (docs/spec-workflows.md §13 PM-13).

// devKeyedVariantB is webdev editing the repository on vol, apidev running the
// api part from it, each part with its own stage.
func devKeyedVariantB() *ServiceMeta {
	m := devKeyedMultiPart()
	m.Hostname = "webdev"
	m.ServesHTTPByHost = map[string]bool{"webdev": true, "webstage": true}
	m.PublicAccess = map[string]topology.PublicAccessRecord{"webdev": {Intent: topology.PublicAccessSubdomain}}
	m.CodeVolume = &topology.CodeVolume{Hostname: "vol", MountPath: "/var/www/ws",
		DevServices: []topology.DevServiceRef{{Hostname: "apidev", SetupName: "dev-api"}}}
	return m
}

func variantBMoves() []PoolPartMove {
	return []PoolPartMove{
		{Stage: "webstage", Pool: "webpool", PoolServiceID: "web-pool-id", PoolSetup: "webpool"},
		{Stage: "apistage", Pool: "apipool", PoolServiceID: "api-pool-id", PoolSetup: "apipool"},
	}
}

// TestMigratePairToPool_VariantB_CarriesTheCodeVolume pins a single part's
// move of a variant-B pair: the pool-keyed pair holds the code volume as the
// dev's pair held it — a copy, never the same record — so the dev services
// still resolve to the pair until they are gone, and the revert brings it
// back to the dev-keyed pair.
func TestMigratePairToPool_VariantB_CarriesTheCodeVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dev := devKeyedPair()
	dev.CodeVolume = &topology.CodeVolume{Hostname: "vol", MountPath: "/var/www/ws",
		DevServices: []topology.DevServiceRef{{Hostname: "workerdev", SetupName: "dev-worker"}}}
	if err := WriteServiceMeta(dir, dev); err != nil {
		t.Fatal(err)
	}
	got, err := MigratePairToPool(dir, appPoolMove())
	if err != nil {
		t.Fatalf("MigratePairToPool of a variant-B pair: %v", err)
	}
	if got.Hostname != "apppool" || !got.DevIsPool() || !reflect.DeepEqual(got.CodeVolume, dev.CodeVolume) {
		t.Fatalf("moved pair = %+v, want it keyed by the pool holding the code volume %+v", got, dev.CodeVolume)
	}
	if got.CodeVolume == dev.CodeVolume {
		t.Error("the moved pair shares the dev pair's code volume record")
	}
	if m, _ := FindServiceMeta(dir, "workerdev"); m == nil || m.Hostname != "apppool" {
		t.Errorf("the further dev service resolves to %+v, want the pool-keyed pair until it is gone", m)
	}
	back, err := RevertPoolPair(dir, appPoolMove())
	if err != nil {
		t.Fatalf("RevertPoolPair: %v", err)
	}
	if back.Hostname != "appdev" || back.DevIsPool() || !reflect.DeepEqual(back.CodeVolume, dev.CodeVolume) {
		t.Fatalf("reverted pair = %+v, want it keyed by appdev with its code volume", back)
	}
}

// TestMigratePairToPools_VariantB_TheFirstPartKeepsTheCodeVolume pins the
// split of a variant-B pair: the code volume rides one pair only — the first
// part's — so every dev service resolves to exactly one pair; the merge back
// carries it to the dev-keyed pair again.
func TestMigratePairToPools_VariantB_TheFirstPartKeepsTheCodeVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dev := devKeyedVariantB()
	if err := WriteServiceMeta(dir, dev); err != nil {
		t.Fatal(err)
	}
	split, err := MigratePairToPools(dir, "webdev", variantBMoves())
	if err != nil {
		t.Fatalf("MigratePairToPools of a variant-B pair: %v", err)
	}
	if len(split) != 2 || !reflect.DeepEqual(split[0].CodeVolume, dev.CodeVolume) || split[1].CodeVolume != nil {
		t.Fatalf("split = %+v / %+v, want the code volume on webpool's pair alone", split[0], split[1])
	}
	if m, _ := FindServiceMeta(dir, "apidev"); m == nil || m.Hostname != "webpool" {
		t.Errorf("apidev resolves to %+v, want webpool's pair", m)
	}
	merged, err := RevertPoolPairs(dir, "webdev", variantBMoves())
	if err != nil {
		t.Fatalf("RevertPoolPairs: %v", err)
	}
	if merged.Hostname != "webdev" || !reflect.DeepEqual(merged.CodeVolume, dev.CodeVolume) {
		t.Fatalf("merged pair = %+v, want webdev's with its code volume", merged)
	}
}

// TestSettleMigratedCodeVolume_DevServicesThenTheVolume pins how the retire
// forgets a variant-B pair's code: once every dev service reads gone the pair
// names none of them — the volume still recorded — and once the volume reads
// gone CodeVolume is cleared. Only pairs moved from the dev change; a pool
// with no pair is skipped.
func TestSettleMigratedCodeVolume_DevServicesThenTheVolume(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, devKeyedVariantB()); err != nil {
		t.Fatal(err)
	}
	other := &ServiceMeta{Hostname: "docsdev", Mode: topology.PlanModeDev, BootstrappedAt: "t",
		CodeVolume: &topology.CodeVolume{Hostname: "docsvol", MountPath: "/var/www/d", DevServices: []topology.DevServiceRef{{Hostname: "docsapidev"}}}}
	if err := WriteServiceMeta(dir, other); err != nil {
		t.Fatal(err)
	}
	if _, err := MigratePairToPools(dir, "webdev", variantBMoves()); err != nil {
		t.Fatal(err)
	}
	pools := []string{"webpool", "apipool", "gonepool"}

	if err := SettleMigratedCodeVolume(dir, "webdev", pools, false); err != nil {
		t.Fatalf("settle the dev services: %v", err)
	}
	web, _ := ReadServiceMeta(dir, "webpool")
	if web == nil || web.CodeVolume == nil || web.CodeVolume.Hostname != "vol" || web.CodeVolume.MountPath != "/var/www/ws" || len(web.CodeVolume.DevServices) != 0 {
		t.Fatalf("webpool's pair = %+v, want the volume recorded and no dev service named", web)
	}
	if m, _ := FindServiceMeta(dir, "apidev"); m != nil {
		t.Errorf("apidev still resolves to %+v once it is gone", m)
	}

	if err := SettleMigratedCodeVolume(dir, "webdev", pools, true); err != nil {
		t.Fatalf("settle the volume: %v", err)
	}
	if web, _ := ReadServiceMeta(dir, "webpool"); web == nil || web.CodeVolume != nil || !web.DevIsPool() {
		t.Fatalf("webpool's pair = %+v, want CodeVolume cleared once the volume is gone", web)
	}
	if kept, _ := ReadServiceMeta(dir, "docsdev"); kept == nil || !reflect.DeepEqual(kept.CodeVolume, other.CodeVolume) {
		t.Errorf("another pair's code volume changed: %+v", kept)
	}
}
