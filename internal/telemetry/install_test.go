package telemetry

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

var uuidV4Shape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUIDv4_ProducesRFC4122Version4Format(t *testing.T) {
	id, err := newUUIDv4()
	if err != nil {
		t.Fatalf("newUUIDv4: %v", err)
	}
	if !uuidV4Shape.MatchString(id) {
		t.Fatalf("newUUIDv4() = %q, want v4 UUID shape", id)
	}
}

func TestNewUUIDv4_ProducesUniqueValues(t *testing.T) {
	seen := map[string]struct{}{}
	for range 100 {
		id, err := newUUIDv4()
		if err != nil {
			t.Fatalf("newUUIDv4: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("newUUIDv4() produced duplicate %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestInstallFilePath_SelectsExternalOrInternalNamespace(t *testing.T) {
	home := "/home/x"
	ext := installFilePath(home, false)
	inv := installFilePath(home, true)
	if ext != filepath.Join(home, ".zcp", "telemetry", "install.json") {
		t.Fatalf("external path = %q", ext)
	}
	if inv != filepath.Join(home, ".zcp", "telemetry", "install-internal.json") {
		t.Fatalf("internal path = %q", inv)
	}
	if ext == inv {
		t.Fatal("external and internal install file paths must never collide")
	}
}

func TestLoadInstallFile_Missing_ReturnsNotExistsNoError(t *testing.T) {
	dir := t.TempDir()
	f, exists, err := loadInstallFile(filepath.Join(dir, "install.json"))
	if err != nil {
		t.Fatalf("loadInstallFile: unexpected error %v", err)
	}
	if exists {
		t.Fatal("exists = true for a missing file")
	}
	if f != (installFile{}) {
		t.Fatalf("f = %+v, want zero value", f)
	}
}

func TestLoadInstallFile_Corrupt_ReturnsExistsTrueAndError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "install.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	_, exists, err := loadInstallFile(path)
	if err == nil {
		t.Fatal("loadInstallFile: expected error for corrupt JSON")
	}
	if !exists {
		t.Fatal("exists = false, want true (file is present, just unparseable)")
	}
}

func TestLoadInstallFile_Valid_ParsesFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "install.json")
	want := installFile{InstallID: "abc-123", DisclosedAt: "2026-07-02T00:00:00Z", Disabled: true}
	if err := writeInstallFileAtomic(path, want); err != nil {
		t.Fatalf("writeInstallFileAtomic: %v", err)
	}
	got, exists, err := loadInstallFile(path)
	if err != nil {
		t.Fatalf("loadInstallFile: %v", err)
	}
	if !exists {
		t.Fatal("exists = false, want true")
	}
	if got != want {
		t.Fatalf("loadInstallFile() = %+v, want %+v", got, want)
	}
}

func TestWriteInstallFileAtomic_SetsDirAndFileModes(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested", "telemetry")
	path := filepath.Join(nested, "install.json")

	if err := writeInstallFileAtomic(path, installFile{InstallID: "x"}); err != nil {
		t.Fatalf("writeInstallFileAtomic: %v", err)
	}

	dirInfo, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", perm)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 0600", perm)
	}

	// No leftover tmp files.
	entries, err := os.ReadDir(nested)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want exactly 1 (no tmp leftovers): %v", len(entries), entries)
	}
}

func TestWriteInstallFileAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "install.json")
	if err := writeInstallFileAtomic(path, installFile{InstallID: "first"}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := writeInstallFileAtomic(path, installFile{InstallID: "second"}); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, _, err := loadInstallFile(path)
	if err != nil {
		t.Fatalf("loadInstallFile: %v", err)
	}
	if got.InstallID != "second" {
		t.Fatalf("InstallID = %q, want %q", got.InstallID, "second")
	}
}

func TestStampDisclosure_MintsIDAndPersistsDisclosedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "install.json")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	f, err := stampDisclosure(path, now)
	if err != nil {
		t.Fatalf("stampDisclosure: %v", err)
	}
	if !uuidV4Shape.MatchString(f.InstallID) {
		t.Fatalf("InstallID = %q, want v4 UUID shape", f.InstallID)
	}
	if f.DisclosedAt != now.UTC().Format(time.RFC3339) {
		t.Fatalf("DisclosedAt = %q, want %q", f.DisclosedAt, now.UTC().Format(time.RFC3339))
	}
	if f.Disabled {
		t.Fatal("Disabled = true, want false on fresh disclosure")
	}

	onDisk, exists, err := loadInstallFile(path)
	if err != nil || !exists {
		t.Fatalf("loadInstallFile after stamp: exists=%v err=%v", exists, err)
	}
	if onDisk != f {
		t.Fatalf("on-disk file = %+v, want %+v", onDisk, f)
	}
}
