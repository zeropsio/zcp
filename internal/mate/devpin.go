package mate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The dev pin is the one permission for a hand-placed build to stay
// (docs/spec-mate.md §2.1a): one record per container, covering whichever
// dev builds are there, a dev Mate in current and a zcp binary no release
// names. Without an active pin a dev build stays only until a release newer
// than its base is known. `zcp mate pin` is its one writer; VersionToRun and
// zcp's reading of its own release are its readers.

// DevPinDefault is how long a pin placed without --for lasts.
const DevPinDefault = 24 * time.Hour

// DevPinMaximum is the longest pin: a pin always ends.
const DevPinMaximum = 7 * 24 * time.Hour

// DevPin is the stored pin, its times in RFC 3339 UTC.
type DevPin struct {
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
	Reason string    `json:"reason"`
}

// DevPinPath is where the pin lives, beside the versions it covers.
func DevPinPath() string { return filepath.Join(Prefix(), "dev-pin.json") }

// NewDevPin is the pin placed at now for d with reason, or why it is refused:
// no reason, or a duration that is not positive or longer than DevPinMaximum.
func NewDevPin(now time.Time, d time.Duration, reason string) (DevPin, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return DevPin{}, errors.New("a dev pin needs a reason (--reason)")
	case d <= 0:
		return DevPin{}, fmt.Errorf("a dev pin lasts a positive time, not %s", d)
	case d > DevPinMaximum:
		return DevPin{}, fmt.Errorf("a dev pin lasts at most %s, not %s", DevPinMaximum, d)
	}
	since := now.UTC().Truncate(time.Second)
	return DevPin{Since: since, Until: since.Add(d), Reason: reason}, nil
}

// ActiveDevPin is the pin active at now. A pin that ended, is missing or
// cannot be read is no pin.
func ActiveDevPin(now time.Time) (DevPin, bool) {
	raw, err := os.ReadFile(DevPinPath())
	if err != nil {
		return DevPin{}, false
	}
	var pin DevPin
	if err := json.Unmarshal(raw, &pin); err != nil || !now.Before(pin.Until) {
		return DevPin{}, false
	}
	return pin, true
}

// WriteDevPin stores pin in place of any earlier one: written under a
// temporary name and renamed, so a crash leaves the old record or the new one.
func WriteDevPin(pin DevPin) error {
	body, err := json.Marshal(pin)
	if err != nil {
		return fmt.Errorf("encode dev pin: %w", err)
	}
	if err := os.MkdirAll(Prefix(), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", Prefix(), err)
	}
	tmp := DevPinPath() + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("write dev pin: %w", err)
	}
	if err := os.Rename(tmp, DevPinPath()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("record dev pin: %w", err)
	}
	return syncPrefix()
}

// RemoveDevPin ends the pin; with none there is nothing to do.
func RemoveDevPin() error {
	if err := os.Remove(DevPinPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove dev pin: %w", err)
	}
	return nil
}
