//go:build live

// Live-network audit: run with `go test -tags live -run TestLiveAllTypesAudit
// ./internal/schema/`. Skipped in the default (offline) suite. Fetches the real
// platform schema and round-trips every service type / run.base / build.base
// through the catalog + topology — the durable check that catches a managed
// type missing from topology.managedServicePrefixes (which the embedded coverage
// pins cannot, since a missing managed type just never enters ManagedBaseNames).
// It is what surfaced `mysql` being absent from the managed list.
package schema

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestLiveAllTypesAudit fetches the LIVE platform schema (canonical host =
// eval-zcp's prg1) and round-trips EVERY service type / run.base / build.base
// through the schema catalog + topology. The load-bearing checks:
//   - HasServiceType/HasRunBase/HasBuildBase must ACCEPT every real type — this
//     catches the mode-capability guard false-rejecting an actual platform type.
//   - every type classifies as exactly one of managed/runtime/storage (+utility),
//     and CanonicalBaseName is decoration-free.
// Throwaway live-network audit (not committed); run with -run TestLiveAllTypesAudit.
func TestLiveAllTypesAudit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := FetchSchemas(ctx, "") // "" → CanonicalAPIHost (api.app-prg1.zerops.io)
	if err != nil {
		t.Fatalf("live fetch failed: %v", err)
	}

	var rejectedTypes, rejectedRun, rejectedBuild, decorated, unclassified []string
	managed, runtime, objStore, shStore, utility := 0, 0, 0, 0, 0

	for _, ty := range s.ImportYml.ServiceTypes {
		if !s.HasServiceType(ty) {
			rejectedTypes = append(rejectedTypes, ty) // a REAL type the catalog rejects = bug
		}
		if base := topology.CanonicalBaseName(ty); strings.ContainsAny(base, "/:") {
			decorated = append(decorated, ty+" -> "+base)
		}
		switch {
		case topology.IsObjectStorageType(ty):
			objStore++
		case topology.IsSharedStorageType(ty):
			shStore++
		case topology.IsManagedService(ty):
			managed++
		case topology.IsUtilityType(ty):
			utility++
		case topology.IsRuntimeType(ty):
			runtime++
		default:
			unclassified = append(unclassified, ty)
		}
	}
	for _, b := range s.ZeropsYml.RunBases {
		if !s.HasRunBase(b) {
			rejectedRun = append(rejectedRun, b)
		}
	}
	for _, b := range s.ZeropsYml.BuildBases {
		if !s.HasBuildBase(b) {
			rejectedBuild = append(rejectedBuild, b)
		}
	}

	fmt.Printf("\n===== LIVE ALL-TYPES AUDIT (canonical host) =====\n")
	fmt.Printf("import types: %d  (managed=%d runtime=%d object-storage=%d shared-storage=%d utility=%d)\n",
		len(s.ImportYml.ServiceTypes), managed, runtime, objStore, shStore, utility)
	fmt.Printf("run.base: %d   build.base: %d\n", len(s.ZeropsYml.RunBases), len(s.ZeropsYml.BuildBases))

	report := func(title string, items []string) {
		sort.Strings(items)
		fmt.Printf("%s: %d\n", title, len(items))
		for _, it := range items {
			fmt.Println("   " + it)
		}
		if len(items) > 0 {
			t.Errorf("%s: %d (see above)", title, len(items))
		}
	}
	report("REJECTED real service types (catalog false-reject = BUG)", rejectedTypes)
	report("REJECTED real run.bases", rejectedRun)
	report("REJECTED real build.bases", rejectedBuild)
	report("CanonicalBaseName left decoration", decorated)
	report("UNCLASSIFIED types", unclassified)
}
