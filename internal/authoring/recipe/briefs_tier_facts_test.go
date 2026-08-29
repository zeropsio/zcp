package recipe

import (
	"slices"
	"testing"
)

func TestManagedFamiliesByHA_LocalStorage_ExcludedFromHAFamilies(t *testing.T) {
	t.Parallel()
	for _, families := range [][]string{managedFamiliesByHA(true), managedFamiliesByHA(false)} {
		if slices.Contains(families, "local-storage") {
			t.Errorf("Local Storage appeared in managed HA family table: %v", families)
		}
	}
}
