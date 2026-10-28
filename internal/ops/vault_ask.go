package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// VaultAsk is an ask a request opened in Mate (docs/spec-mate.md §5.8):
// Mate's engine keeps it open until the person saves or declines it, and
// keys it by the call that asked, so a second request for the same key and
// vault would open a second prompt. zcp keeps this record of its own to
// refuse that second request while the ask is open. It holds the key, the
// vault, when it was asked and the row the vault held under the key then —
// never a value.
type VaultAsk struct {
	Key             string          `json:"key"`
	Label           string          `json:"label"`
	Scope           EnvRequestScope `json:"scope"`
	ServiceHostname string          `json:"serviceHostname,omitempty"`
	AskedAt         time.Time       `json:"askedAt"`
	HeldID          string          `json:"heldId,omitempty"`
	HeldChanged     time.Time       `json:"heldChanged,omitzero"`
}

// vaultAsksDir holds one record per open ask, by key and vault.
const vaultAsksDir = "vault-asks"

// NewVaultAsk is the record of the ask a request's answer opens, at now.
func NewVaultAsk(req *EnvRequestResult, now time.Time) VaultAsk {
	return VaultAsk{
		Key: req.Key, Label: req.Label, Scope: req.Scope, ServiceHostname: req.ServiceHostname,
		AskedAt: now.UTC(), HeldID: req.HeldID, HeldChanged: req.HeldChanged,
	}
}

// AnsweredBy reports whether the vault, as req read it, holds the key written
// since the ask: a row where there was none, another row (a set replaces the
// row), or the held row changed since (a value replaced in place keeps its
// id). The person saved it, or the agent set it — either way the ask is done.
func (a VaultAsk) AnsweredBy(req *EnvRequestResult) bool {
	if req.HeldID == "" {
		return false
	}
	return a.HeldID == "" || req.HeldID != a.HeldID || req.HeldChanged.After(a.HeldChanged)
}

// vaultAskPath is the record of the ask for req's key in req's vault. A key
// is letters, digits and `_`, a hostname letters and digits, so the name is
// a plain file name.
func vaultAskPath(stateDir string, req *EnvRequestResult) string {
	name := "shared." + req.Key + ".json"
	if req.Scope == EnvRequestScopeService {
		name = "service." + req.ServiceHostname + "." + req.Key + ".json"
	}
	return filepath.Join(stateDir, vaultAsksDir, name)
}

// ReadVaultAsk returns the open ask for req's key and vault, nil when there
// is none. A record that does not parse is no record: it is removed.
func ReadVaultAsk(stateDir string, req *EnvRequestResult) (*VaultAsk, error) {
	path := vaultAskPath(stateDir, req)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the open ask: %w", err)
	}
	var ask VaultAsk
	if err := json.Unmarshal(data, &ask); err != nil || ask.Key != req.Key {
		_ = os.Remove(path)
		return nil, nil
	}
	return &ask, nil
}

// ClaimVaultAsk records ask as the open ask for its key and vault, unless one
// is recorded already: then it returns that one and records nothing. The
// record is written whole beside the target and hard-linked into place, so a
// reader never sees half of one and two requests racing for one key claim it
// once.
func ClaimVaultAsk(stateDir string, req *EnvRequestResult, ask VaultAsk) (*VaultAsk, error) {
	path := vaultAskPath(stateDir, req)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create the open asks directory: %w", err)
	}
	data, err := json.Marshal(ask)
	if err != nil {
		return nil, fmt.Errorf("encode the open ask: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return nil, fmt.Errorf("create the open ask: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write the open ask: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close the open ask: %w", err)
	}
	// A record that does not parse is removed on read, so the link is tried
	// once more.
	for range 2 {
		err := os.Link(tmpPath, path)
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("record the open ask: %w", err)
		}
		held, err := ReadVaultAsk(stateDir, req)
		if err != nil || held != nil {
			return held, err
		}
	}
	return nil, fmt.Errorf("record the open ask: %s keeps a record that does not parse", path)
}

// ForgetVaultAsk removes the record of the ask for req's key and vault: it
// was answered, or the agent asks again after a decline.
func ForgetVaultAsk(stateDir string, req *EnvRequestResult) error {
	if err := os.Remove(vaultAskPath(stateDir, req)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("forget the open ask: %w", err)
	}
	return nil
}
