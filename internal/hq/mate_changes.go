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

// MateChangesPath is where the Mate's server keeps the changes of the last
// `state` frame HQ sent over its link (the frame's MateChanges: the
// application, its Mate's changes and openPerBranch), beside the Git
// credential. The server writes it in one rename on every frame and once at
// its start; zcp only reads it, and never asks HQ for what it holds.
func MateChangesPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "hq", "mate-changes.json")
}

// ReadMateChanges is the Mate's changes as the link's last `state` frame
// brought them, from the file at path, read at each use. No file is no frame
// yet: a state with no application, so no change of the Mate's is finished.
func ReadMateChanges(path string) (MateState, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return MateState{}, nil
	}
	if err != nil {
		return MateState{}, fmt.Errorf("read the link's changes: %w", err)
	}
	var state MateState
	if err := json.Unmarshal(raw, &state); err != nil {
		return MateState{}, fmt.Errorf("read the link's changes: %w", err)
	}
	return state, nil
}
