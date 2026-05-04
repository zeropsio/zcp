package recipe

import (
	"fmt"
	"os"
	"path/filepath"
)

// RefinementClosedMarkerName is the on-disk sentinel that signals
// `complete-phase phase=refinement` returned ok for this recipe run.
// Used by `zcp sync recipe export` (a separate process from the
// MCP server hosting the recipe Session) to gate export on
// refinement closure. Run-23 F-26.
const RefinementClosedMarkerName = ".refinement-closed"

// writeRefinementCloseMarker creates an empty sentinel file under
// outputRoot indicating refinement has closed. Idempotent — repeat
// calls overwrite the existing marker. Empty outputRoot is a no-op
// (in-memory tests construct sessions without an on-disk root).
func writeRefinementCloseMarker(outputRoot string) error {
	if outputRoot == "" {
		return nil
	}
	dst := filepath.Join(outputRoot, RefinementClosedMarkerName)
	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create refinement-close marker: %w", err)
	}
	return f.Close()
}

// IsRefinementClosed reports whether the recipe run at outputRoot has
// closed the refinement phase (by checking for the close marker).
// Used by the export gate to refuse export when refinement has not
// completed. Returns false when outputRoot is empty or the marker is
// absent.
func IsRefinementClosed(outputRoot string) bool {
	if outputRoot == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(outputRoot, RefinementClosedMarkerName))
	return err == nil
}
