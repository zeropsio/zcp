package telemetry

import (
	"testing"
	"time"
)

func TestEnable_NoExistingFile_MintsIDAndStampsDisclosure(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	if err := Enable(home, false, now); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	f, exists, err := loadInstallFile(installFilePath(home, false))
	if err != nil || !exists {
		t.Fatalf("loadInstallFile: exists=%v err=%v", exists, err)
	}
	if f.InstallID == "" {
		t.Error("InstallID not minted")
	}
	if f.Disabled {
		t.Error("Disabled = true, want false after Enable")
	}
	if f.DisclosedAt != now.UTC().Format(time.RFC3339) {
		t.Errorf("DisclosedAt = %q, want %q", f.DisclosedAt, now.UTC().Format(time.RFC3339))
	}
}

func TestEnable_ExistingDisabledFile_ClearsDisabledKeepsIdentity(t *testing.T) {
	home := t.TempDir()
	path := installFilePath(home, false)
	seed := installFile{InstallID: "seed-id", DisclosedAt: "2026-01-01T00:00:00Z", Disabled: true}
	if err := writeInstallFileAtomic(path, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Enable(home, false, time.Now()); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	f, _, err := loadInstallFile(path)
	if err != nil {
		t.Fatalf("loadInstallFile: %v", err)
	}
	if f.Disabled {
		t.Error("Disabled = true, want false after Enable")
	}
	if f.InstallID != "seed-id" {
		t.Errorf("InstallID = %q, want unchanged %q", f.InstallID, "seed-id")
	}
	if f.DisclosedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("DisclosedAt = %q, want unchanged", f.DisclosedAt)
	}
}

func TestEnable_InternalChannel_WritesInternalFileOnly(t *testing.T) {
	home := t.TempDir()
	if err := Enable(home, true, time.Now()); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if _, exists, _ := loadInstallFile(installFilePath(home, false)); exists {
		t.Error("external install.json must not be written for internal channel Enable")
	}
	if _, exists, _ := loadInstallFile(installFilePath(home, true)); !exists {
		t.Error("install-internal.json must be written for internal channel Enable")
	}
}

func TestDisable_ExistingFile_SetsDisabledKeepsInstallID(t *testing.T) {
	home := t.TempDir()
	path := installFilePath(home, false)
	seed := installFile{InstallID: "keep-me", DisclosedAt: "2026-01-01T00:00:00Z"}
	if err := writeInstallFileAtomic(path, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Disable(home, false); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	f, _, err := loadInstallFile(path)
	if err != nil {
		t.Fatalf("loadInstallFile: %v", err)
	}
	if !f.Disabled {
		t.Error("Disabled = false, want true after Disable")
	}
	if f.InstallID != "keep-me" {
		t.Errorf("InstallID = %q, want preserved %q", f.InstallID, "keep-me")
	}
}

func TestDisable_NoExistingFile_WritesDisabledRecord(t *testing.T) {
	home := t.TempDir()
	if err := Disable(home, false); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	f, exists, err := loadInstallFile(installFilePath(home, false))
	if err != nil || !exists {
		t.Fatalf("loadInstallFile: exists=%v err=%v", exists, err)
	}
	if !f.Disabled {
		t.Error("Disabled = false, want true")
	}
}

func TestInstallIDOf_ExistingFile_ReturnsID(t *testing.T) {
	home := t.TempDir()
	path := installFilePath(home, false)
	if err := writeInstallFileAtomic(path, installFile{InstallID: "the-id"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	id, err := InstallIDOf(home, false)
	if err != nil {
		t.Fatalf("InstallIDOf: %v", err)
	}
	if id != "the-id" {
		t.Errorf("id = %q, want %q", id, "the-id")
	}
}

func TestInstallIDOf_NoFile_ReturnsError(t *testing.T) {
	home := t.TempDir()
	if _, err := InstallIDOf(home, false); err == nil {
		t.Fatal("InstallIDOf: expected error when no install file exists yet")
	}
}
