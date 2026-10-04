package mate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
)

// The Mate server keeps each login's signer in ~/.mate/signed-in.json.
// Before it starts, zcp seeds an absent store once from HQ's project record.

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

// SignInsSeededPath records the seed attempt: empty for success or no need,
// otherwise the failure reason retained until an explicit reset.
func SignInsSeededPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "state", "mate-sign-ins-seeded")
}

// SeedSignIns writes an absent store from HQ's login-to-user map once.
// A present store is preserved without reading HQ. A failed attempt keeps its
// reason in markPath and is never automatically retried; removing the marker
// explicitly permits another attempt on the next launch.
func SeedSignIns(storePath, markPath string, readSigners func() (map[string]string, error), now time.Time) (bool, error) {
	if exists(storePath) {
		return false, mark(markPath)
	}
	if raw, err := os.ReadFile(markPath); err == nil {
		if len(raw) != 0 {
			return false, errors.New(string(raw))
		}
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("read the sign-in seed attempt: %w", err)
	}
	signers, err := readSigners()
	if err != nil {
		return false, recordSeedFailure(markPath, fmt.Errorf("read the Mate's signers from HQ: %w", err))
	}
	signIns := make(map[string]SignIn)
	for key, user := range signers {
		if isSignerKey(key) && user != "" && len(user) <= signerUserMax {
			signIns[key] = SignIn{By: user, At: now.UnixMilli()}
		}
	}
	if len(signIns) == 0 {
		return false, mark(markPath)
	}
	if err := writeAtomically(storePath, signIns); err != nil {
		return false, recordSeedFailure(markPath, err)
	}
	return true, mark(markPath)
}

// recordSeedFailure keeps a failed attempt's reason so only an explicit reset
// can ask HQ and write the store again.
func recordSeedFailure(path string, reason error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.Join(reason, fmt.Errorf("record the failed sign-in seed: %w", err))
	}
	if err := os.WriteFile(path, []byte(reason.Error()), 0o600); err != nil {
		return errors.Join(reason, fmt.Errorf("record the failed sign-in seed: %w", err))
	}
	return reason
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

func mark(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mark the sign-ins seeded: %w", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("mark the sign-ins seeded: %w", err)
	}
	return nil
}

// writeAtomically writes the store whole or not at all, as the server does.
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
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write the sign-ins: %w", err)
	}
	return nil
}
