package hq

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/runtime"
)

// ChangesPath is where the Mate's server keeps the changes of the last
// `state` frame HQ sent over its link, beside the Git credential. The server
// replaces it whole, in one rename, on every frame; zcp only reads it, and
// never asks HQ for what it holds.
func ChangesPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "hq", "changes.json")
}

// MateChanges is the file at ChangesPath: exactly the `state` frame's
// MateChanges (mate packages/shared/src/hqChanges.ts) — the application HQ
// holds the Mate in, nil for none, the Mate's changes there, and whether HQ
// keeps an open change per branch.
type MateChanges struct {
	AppID         *string      `json:"appId"`
	AppName       *string      `json:"appName"`
	Changes       []MateChange `json:"changes"`
	OpenPerBranch bool         `json:"openPerBranch,omitempty"`
}

// ReadMateChanges is the Mate's changes as the link's last `state` frame
// brought them, from the file at path, read at each use. No file is no frame
// yet: no application, so no change of the Mate's is finished.
func ReadMateChanges(path string) (MateChanges, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return MateChanges{}, nil
	}
	if err != nil {
		return MateChanges{}, fmt.Errorf("read the link's changes: %w", err)
	}
	var frame MateChanges
	if err := json.Unmarshal(raw, &frame); err != nil {
		return MateChanges{}, fmt.Errorf("read the link's changes: %w", err)
	}
	return frame, nil
}
