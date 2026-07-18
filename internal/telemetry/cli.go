package telemetry

import (
	"fmt"
	"time"
)

// Enable clears the install file's disabled flag and stamps disclosure if
// this install has never been disclosed before (spec §3.5 "enable — clears
// disabled, stamps disclosure"). Unlike Resolve's automatic pre-disclosure
// stamp (spec §3.3, which withholds telemetry for the REST of that
// process), Enable is a direct, explicit user action — the caller is
// expected to show the disclosure notice (PrintDisclosureNotice) alongside
// this call, since consent is happening right now, not "starting next run".
func Enable(homeDir string, internalChannel bool, now time.Time) error {
	path := installFilePath(homeDir, internalChannel)
	f, exists, err := loadInstallFile(path)
	if err != nil {
		return fmt.Errorf("read install file: %w", err)
	}
	if !exists {
		id, err := newUUIDv4()
		if err != nil {
			return fmt.Errorf("mint install id: %w", err)
		}
		f = installFile{InstallID: id}
	}
	f.Disabled = false
	if f.DisclosedAt == "" {
		f.DisclosedAt = now.UTC().Format(time.RFC3339)
	}
	if err := writeInstallFileAtomic(path, f); err != nil {
		return fmt.Errorf("write install file: %w", err)
	}
	return nil
}

// Disable sets the install file's disabled flag (spec §3.5 "disable — sets
// disabled: true (keeps installId; rows purgeable on request)"). Existing
// installId/disclosedAt are preserved untouched — installId stays the
// erasure-request key even while opted out. Safe to call with no prior
// install file (pre-emptive opt-out): writes a disabled record with no id.
func Disable(homeDir string, internalChannel bool) error {
	path := installFilePath(homeDir, internalChannel)
	f, _, err := loadInstallFile(path)
	if err != nil {
		return fmt.Errorf("read install file: %w", err)
	}
	f.Disabled = true
	if err := writeInstallFileAtomic(path, f); err != nil {
		return fmt.Errorf("write install file: %w", err)
	}
	return nil
}

// InstallIDOf reads the install UUID for `zcp telemetry id` (spec §3.5 —
// "the erasure-request key").
func InstallIDOf(homeDir string, internalChannel bool) (string, error) {
	path := installFilePath(homeDir, internalChannel)
	f, exists, err := loadInstallFile(path)
	if err != nil {
		return "", fmt.Errorf("read install file: %w", err)
	}
	if !exists || f.InstallID == "" {
		return "", fmt.Errorf("no install id yet — telemetry has not been enabled on this machine")
	}
	return f.InstallID, nil
}
