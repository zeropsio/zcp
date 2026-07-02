// Package telemetry is the ZCP anonymous usage telemetry client core: consent
// resolution (config.go), install identity (install.go), the emit pipeline —
// queue, batch, send, spool (client.go, spool.go) — and the storm guard
// (storm.go). Spec: docs/spec-telemetry.md §2, §3, §5.
package telemetry

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	telemetryDirName        = "telemetry"
	installFileName         = "install.json"
	installInternalFileName = "install-internal.json"
)

// installFile is the on-disk shape of ~/.zcp/telemetry/install.json (spec
// §2): {"installId": "<uuid>", "disclosedAt": "<RFC3339>", "disabled": false}.
type installFile struct {
	InstallID   string `json:"installId"`
	DisclosedAt string `json:"disclosedAt,omitempty"`
	Disabled    bool   `json:"disabled"`
}

// installFilePath returns the install file path under
// homeDir/.zcp/telemetry. internalChannel selects install-internal.json —
// internal channels (internal_dev, internal_eval) never share the external
// install-id namespace, so internal traffic can never pollute external
// install counts (spec §2).
func installFilePath(homeDir string, internalChannel bool) string {
	name := installFileName
	if internalChannel {
		name = installInternalFileName
	}
	return filepath.Join(homeDir, ".zcp", telemetryDirName, name)
}

// loadInstallFile reads and parses path.
//
//   - exists=false, err=nil: the file is simply absent — a pre-disclosure
//     candidate (spec §3.1 rule 4).
//   - exists=true, err!=nil: the file is present but unreadable/unparseable
//     — spec §3.1 "install-file read/write errors → disabled for the
//     process", never crash.
//   - exists=true, err=nil: f is the parsed install file.
func loadInstallFile(path string) (f installFile, exists bool, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return installFile{}, false, nil
		}
		return installFile{}, true, fmt.Errorf("read install file %s: %w", path, readErr)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return installFile{}, true, fmt.Errorf("parse install file %s: %w", path, err)
	}
	return f, true, nil
}

// writeInstallFileAtomic writes f to path via tmp-then-rename, creating the
// parent dir 0700 if needed. os.CreateTemp already opens the temp file 0600
// (spec §2 file modes), so the renamed final file inherits that mode.
func writeInstallFileAtomic(path string, f installFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal install file: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".install-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp install file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp install file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp install file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename install file: %w", err)
	}
	return nil
}

// newUUIDv4 generates a random UUIDv4 string via crypto/rand (spec §2 — no
// new dependency).
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// stampDisclosure mints a fresh install identity and persists it as the
// process's first disclosed install file (spec §3.3): fresh installId +
// disclosedAt, disabled=false.
func stampDisclosure(path string, now time.Time) (installFile, error) {
	id, err := newUUIDv4()
	if err != nil {
		return installFile{}, fmt.Errorf("mint install id: %w", err)
	}
	f := installFile{
		InstallID:   id,
		DisclosedAt: now.UTC().Format(time.RFC3339),
	}
	if err := writeInstallFileAtomic(path, f); err != nil {
		return installFile{}, fmt.Errorf("write disclosure: %w", err)
	}
	return f, nil
}
