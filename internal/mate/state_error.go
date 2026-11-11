package mate

import (
	"errors"
	"fmt"

	"github.com/zeropsio/zcp/internal/hq"
)

// StateReadError names an outdated installed server when it has written no
// state, so a person can bring it to the minimum this zcp drives.
func StateReadError(err error) error {
	if !errors.Is(err, hq.ErrMateStateAbsent) {
		return err
	}
	if version, readErr := InstalledVersion(); readErr == nil && BelowFloor(version) {
		return fmt.Errorf("%w; installed Mate %s is below the minimum %s: run zcp mate update", err, version, MinimumMateVersion)
	}
	return err
}
