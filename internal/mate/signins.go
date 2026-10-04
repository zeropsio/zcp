package mate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
)

// The Mate server keeps each login's signer in ~/.mate/signed-in.json.
// zcp seeds an absent store after enrollment with HQ: once when HQ names
// signers, again on a later start while HQ named none.

// SignIn is one login's sign-in as the server's store keeps it: who, and
// when (epoch ms).
type SignIn struct {
	By string `json:"by"`
	At int64  `json:"at"`
}

// signerKeyOther is a login other than a driver's default: `<driver>-<slug>`
// (apps/server/src/zerops/zeropsLoginIds.ts).
var signerKeyOther = regexp.MustCompile(`^(claudeAgent|codex)-[a-zA-Z0-9_-]+$`)

// The server's bounds on a login's id and a user's (zeropsLoginIds.ts,
// zeropsSignIns.ts).
const (
	signerKeyMax  = 64
	signerUserMax = 128
)

// SignInsPath is the server's sign-in store under the container's home.
func SignInsPath() string {
	return filepath.Join(runtime.HomeDir(), ".mate", "signed-in.json")
}

// SignInsSeededPath records the seed's outcome, without a credential.
func SignInsSeededPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "state", "mate-sign-ins-seeded")
}

// SignInsSeedStatus is one completed seed input. Input is an opaque fingerprint
// of the enrollment, never its credential. A failed input is not tried again.
// Start names the process start that recorded it; an "empty" answer is final
// for that start only.
type SignInsSeedStatus struct {
	State string    `json:"state"`
	Input string    `json:"input,omitempty"`
	Start string    `json:"start,omitempty"`
	At    time.Time `json:"at"`
	Error string    `json:"error,omitempty"`
}

// thisStart identifies this process's start, so an "empty" seed is read again
// by the next start and never twice by the same one.
var thisStart = strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)

// ReadSignInsSeedStatus reads the completed attempt, including legacy markers.
func ReadSignInsSeedStatus(path string) (SignInsSeedStatus, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SignInsSeedStatus{}, fmt.Errorf("read the sign-in seed attempt: %w", err)
	}
	if len(raw) == 0 {
		return SignInsSeedStatus{State: "seeded"}, nil
	}
	if !json.Valid(raw) {
		// The previous writer kept only a failure's reason, before enrollment.
		return SignInsSeedStatus{State: "failed", Error: string(raw)}, nil
	}
	var status SignInsSeedStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return SignInsSeedStatus{}, fmt.Errorf("decode the sign-in seed attempt: %w", err)
	}
	if status.State == "" {
		return SignInsSeedStatus{}, errors.New("sign-in seed attempt has no state")
	}
	return status, nil
}

// SeedSignIns is the one-time seed without a changing enrollment input.
func SeedSignIns(storePath, markPath string, readSigners func() (map[string]string, error), now time.Time) (bool, error) {
	return SeedSignInsForEnrollment(storePath, markPath, "", readSigners, now)
}

// SeedSignInsForEnrollment preserves any existing store, reads HQ at most once
// per failed enrollment input, and retains the outcome. Success remains final
// even if the store is later removed. A changed enrollment is new input; a
// restart or recheck of the same credential is not. Legacy failures have no
// input and therefore permit one attempt with an enrolled credential. HQ
// answering without signers is final for this start only: the next start
// reads HQ once more.
func SeedSignInsForEnrollment(storePath, markPath, input string, readSigners func() (map[string]string, error), now time.Time) (bool, error) {
	status := SignInsSeedStatus{Input: input, Start: thisStart, At: now.UTC()}
	finish := func(state string, reason error) error {
		status.State = state
		if reason != nil {
			status.Error = reason.Error()
		}
		if err := saveSeedStatus(markPath, status); err != nil {
			return errors.Join(reason, err)
		}
		return reason
	}
	if exists(storePath) {
		return false, finish("preserved", nil)
	}
	kept, err := ReadSignInsSeedStatus(markPath)
	if err == nil {
		switch kept.State {
		case "failed":
			if kept.Input == input {
				return false, errors.New(kept.Error)
			}
		case "empty":
			if kept.Start == thisStart {
				return false, nil
			}
		default:
			return false, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	signers, err := readSigners()
	if err != nil {
		return false, finish("failed", fmt.Errorf("read the Mate's signers from HQ: %w", err))
	}
	signIns := make(map[string]SignIn)
	for key, user := range signers {
		if isSignerKey(key) && user != "" && len(user) <= signerUserMax {
			signIns[key] = SignIn{By: user, At: now.UnixMilli()}
		}
	}
	if len(signIns) == 0 {
		return false, finish("empty", nil)
	}
	if err := writeAtomically(storePath, signIns); err != nil {
		if errors.Is(err, fs.ErrExist) && exists(storePath) {
			return false, finish("preserved", nil)
		}
		return false, finish("failed", err)
	}
	return true, finish("seeded", nil)
}

func saveSeedStatus(path string, status SignInsSeedStatus) error {
	raw, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encode the sign-in seed outcome: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("record the sign-in seed outcome: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sign-in-seed-*")
	if err != nil {
		return fmt.Errorf("record the sign-in seed outcome: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("record the sign-in seed outcome: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("record the sign-in seed outcome: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("record the sign-in seed outcome: %w", err)
	}
	return nil
}

// isSignerKey is a login the server records a signer for: a driver's
// default (`claude-code`, `codex`) or another login's own id.
func isSignerKey(key string) bool {
	return key == "claude-code" || key == "codex" || (len(key) <= signerKeyMax && signerKeyOther.MatchString(key))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// writeAtomically publishes an absent store whole, without replacing a store
// the server wrote while HQ was answering.
func writeAtomically(path string, signIns map[string]SignIn) error {
	raw, err := json.Marshal(signIns)
	if err != nil {
		return fmt.Errorf("encode the sign-ins: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("write the sign-ins: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".signed-in-*.json")
	if err != nil {
		return fmt.Errorf("write the sign-ins: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write the sign-ins: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write the sign-ins: %w", err)
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		return fmt.Errorf("write the sign-ins: %w", err)
	}
	return nil
}
