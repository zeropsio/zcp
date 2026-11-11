package hq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

// ErrMateStateAbsent means the server has not written a state file here.
var ErrMateStateAbsent = errors.New("the Mate server has not written its state")

// ErrMateStateUnheard means the server has not yet heard a state from HQ.
var ErrMateStateUnheard = errors.New("the Mate server has not heard its state from HQ")

// MateStateSource is the server's last state and current link standing.
// A watch signals replacement; callers read again after each signal.
type MateStateSource interface {
	Read() (MateStateDocument, error)
	Watch(context.Context) (<-chan struct{}, error)
}

// ReadMateState reads the source selected for this identity.
func (id Identity) ReadMateState() (MateStateDocument, error) {
	if id.State == nil {
		return MateStateDocument{}, ErrMateStateAbsent
	}
	return id.State.Read()
}

// MateStateDocument is the file the server keeps from the link's last state.
// Mate is nil before the server has heard a state.
type MateStateDocument struct {
	Link    string
	Reason  string
	HeardAt string
	Mate    *MateState
}

// Heard returns the state last heard, or why there is no state to use.
func (d MateStateDocument) Heard() (MateState, error) {
	if d.Mate == nil {
		if d.Reason != "" {
			return MateState{}, fmt.Errorf("%w: %s", ErrMateStateUnheard, d.Reason)
		}
		return MateState{}, ErrMateStateUnheard
	}
	return *d.Mate, nil
}

// Standing reports when the last heard state may predate the current link.
func (d MateStateDocument) Standing() string {
	if d.Link == "linked" {
		return ""
	}
	line := "The Mate's link to HQ is not up"
	if d.Reason != "" {
		line += " (" + d.Reason + ")"
	}
	if d.Mate == nil {
		return line + "; no state has been heard."
	}
	line += "; this is its last heard state"
	if d.HeardAt != "" {
		line += " from " + d.HeardAt
	}
	return line + "."
}

// MateStatePath is the file the server replaces on each link state.
func MateStatePath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "hq", "state.json")
}

// MateStateLockPath is held by the supervisor for the server's lifetime.
func MateStateLockPath() string { return filepath.Join(filepath.Dir(MateStatePath()), "state.lock") }

type fileMateStateSource struct {
	path, projectID string
}

// NewFileMateStateSource reads the file written for projectID.
func NewFileMateStateSource(path, projectID string) MateStateSource {
	return fileMateStateSource{path: path, projectID: projectID}
}

func (s fileMateStateSource) Watch(ctx context.Context) (<-chan struct{}, error) {
	return WatchReplacedFile(ctx, s.path)
}

func (s fileMateStateSource) Read() (MateStateDocument, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return MateStateDocument{}, ErrMateStateAbsent
	}
	if err != nil {
		return MateStateDocument{}, fmt.Errorf("read the Mate's state: %w", err)
	}
	doc, err := decodeMateState(raw, s.projectID)
	if err != nil {
		return MateStateDocument{}, fmt.Errorf("the Mate's state is unreadable: %w", err)
	}
	held, err := stateLockHeld(filepath.Join(filepath.Dir(s.path), "state.lock"))
	if err != nil {
		return MateStateDocument{}, fmt.Errorf("read the Mate server's lock: %w", err)
	}
	if !held {
		doc.Link, doc.Reason = "not-linked", "no_server"
	}
	return doc, nil
}

func decodeMateState(raw []byte, projectID string) (MateStateDocument, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return MateStateDocument{}, fmt.Errorf("decode document: %w", err)
	}
	var doc MateStateDocument
	if err := json.Unmarshal(fields["link"], &doc.Link); err != nil || (doc.Link != "linked" && doc.Link != "not-linked") {
		return MateStateDocument{}, errors.New("link must be linked or not-linked")
	}
	if reason, ok := fields["reason"]; ok {
		if err := json.Unmarshal(reason, &doc.Reason); err != nil || bytes.Equal(bytes.TrimSpace(reason), []byte("null")) {
			return MateStateDocument{}, errors.New("reason must be a string")
		}
	}
	if heardAt, ok := fields["heardAt"]; ok {
		if err := json.Unmarshal(heardAt, &doc.HeardAt); err != nil {
			return MateStateDocument{}, errors.New("heardAt must be a string or null")
		}
	}
	mateRaw, ok := fields["mate"]
	if !ok {
		return MateStateDocument{}, errors.New("mate is missing")
	}
	if strings.TrimSpace(string(mateRaw)) == "null" {
		return doc, nil
	}
	var required map[string]json.RawMessage
	if err := json.Unmarshal(mateRaw, &required); err != nil || required == nil {
		return MateStateDocument{}, errors.New("mate must be an object or null")
	}
	for _, key := range []string{"projectId", "appId", "repos", "changes", "chief"} {
		if _, ok := required[key]; !ok {
			return MateStateDocument{}, fmt.Errorf("mate.%s is missing", key)
		}
	}
	if bytes.Equal(bytes.TrimSpace(required["projectId"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(required["repos"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(required["changes"]), []byte("null")) {
		return MateStateDocument{}, errors.New("mate projectId, repos and changes must not be null")
	}
	var state MateState
	if err := json.Unmarshal(mateRaw, &state); err != nil {
		return MateStateDocument{}, fmt.Errorf("decode mate: %w", err)
	}
	if state.ProjectID == "" || (projectID != "" && state.ProjectID != projectID) {
		return MateStateDocument{}, errors.New("mate.projectId does not name this project")
	}
	if state.Chief != nil && state.Chief.ProjectID == "" {
		return MateStateDocument{}, errors.New("mate.chief.projectId must be a project ID")
	}
	doc.Mate = &state
	return doc, nil
}

// LockMateState waits until no supervisor holds path and keeps it until release.
// The descriptor is close-on-exec, so the server cannot inherit the lock.
func LockMateState(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mate state lock: create directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("mate state lock: open: %w", err)
	}
	if err := lockMateState(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("mate state lock: take: %w", err)
	}
	return func() { _ = f.Close() }, nil
}

func stateLockHeld(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	return probeMateStateLock(f)
}
