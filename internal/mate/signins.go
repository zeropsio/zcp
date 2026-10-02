package mate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
)

// Who signed each of the Mate's logins in (spec-mate §10.5) is the Mate
// server's own record, its sign-in store (apps/server/src/zerops/
// zeropsSignIns.ts): `~/.mate/signed-in.json`, `{"<signer key>": {"by":
// "<Zerops user id>", "at": <epoch ms>}}`. On main the record was the Mate
// project's tags, `mate:signer:{key}:{userId}`; a Mate migrated from main has
// only those, so before its server first starts zcp writes the store from
// them — once, and never again.

// SignIn is one login's sign-in as the server's store keeps it: who, and
// when (epoch ms).
type SignIn struct {
	By string `json:"by"`
	At int64  `json:"at"`
}

// signerTagPrefix starts a signer tag: `mate:signer:{key}:{userId}`.
const signerTagPrefix = "mate:signer:"

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

// SignInsSeededPath marks that zcp has seeded the store, or found no need to.
func SignInsSeededPath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "state", "mate-sign-ins-seeded")
}

// SeedSignIns writes the server's sign-in store at storePath from the Mate
// project's signer tags, which readTags reads, when there is no store and
// markPath says it never seeded: answers whether it wrote. A store present,
// or tags that name no login the server reads, write nothing; either way the
// mark is set and it never asks again. Tags it could not read leave
// everything as it was, for the next launch to ask.
func SeedSignIns(storePath, markPath string, readTags func() ([]string, error), now time.Time) (bool, error) {
	if exists(markPath) {
		return false, nil
	}
	if exists(storePath) {
		return false, mark(markPath)
	}
	tags, err := readTags()
	if err != nil {
		return false, fmt.Errorf("read the Mate's signer tags: %w", err)
	}
	signIns := signInsFromTags(tags, now)
	if len(signIns) == 0 {
		return false, mark(markPath)
	}
	if err := writeAtomically(storePath, signIns); err != nil {
		return false, err
	}
	return true, mark(markPath)
}

// signInsFromTags reads the signer tags as the server read them on main
// (ZeropsProjectSigners.parseSignerTags): a login it knows, a user id, the
// last tag winning; anything else is left out.
func signInsFromTags(tags []string, now time.Time) map[string]SignIn {
	signIns := map[string]SignIn{}
	for _, tag := range tags {
		rest, ok := strings.CutPrefix(tag, signerTagPrefix)
		if !ok {
			continue
		}
		key, user, ok := strings.Cut(rest, ":")
		if !ok || !isSignerKey(key) || user == "" || len(user) > signerUserMax {
			continue
		}
		signIns[key] = SignIn{By: user, At: now.UnixMilli()}
	}
	return signIns
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
