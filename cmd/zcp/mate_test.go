package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

// seedPinnedVersion lays down a versioned install at mate.PinnedVersion and
// activates it, so mate.EnsureInstalled — run for real, unstubbed — finds the
// desired version already live and reaches no network at all.
func seedPinnedVersion(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".zcp", "mate", "versions", mate.PinnedVersion)
	pkgDir := filepath.Join(dir, "node_modules", mate.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pkgDir, err)
	}
	body := `{"name":"` + mate.PackageName + `","version":"` + mate.PinnedVersion + `"}`
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	current := filepath.Join(home, ".zcp", "mate", "current")
	if err := os.Symlink(filepath.Join("versions", mate.PinnedVersion), current); err != nil {
		t.Fatalf("symlink current: %v", err)
	}
}

func TestRunMateCmd_UnknownSubcommand_Fails(t *testing.T) {
	tests := [][]string{nil, {}, {"bogus"}, {"--force"}}
	for _, args := range tests {
		if got := runMateCmd(args); got != 1 {
			t.Errorf("runMateCmd(%v) = %d, want 1", args, got)
		}
	}
}

func TestRunMateCmd_Update_NoUnitFile_SkipsRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedPinnedVersion(t, home)

	origUnitPath := mateUnitFilePath
	mateUnitFilePath = filepath.Join(t.TempDir(), "no-such-unit.service")
	t.Cleanup(func() { mateUnitFilePath = origUnitPath })

	restarted := false
	origRunner := mateRestartUnit
	mateRestartUnit = func(string) error { restarted = true; return nil }
	t.Cleanup(func() { mateRestartUnit = origRunner })

	if got := runMateCmd([]string{"update"}); got != 0 {
		t.Fatalf("runMateCmd(update) = %d, want 0", got)
	}
	if restarted {
		t.Error("no unit file present must mean no restart attempt")
	}
}

func TestRunMateCmd_Update_UnitFilePresent_Restarts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedPinnedVersion(t, home)

	unitPath := filepath.Join(t.TempDir(), "zerops@mate.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	origUnitPath := mateUnitFilePath
	mateUnitFilePath = unitPath
	t.Cleanup(func() { mateUnitFilePath = origUnitPath })

	var restartedUnit string
	origRunner := mateRestartUnit
	mateRestartUnit = func(unit string) error { restartedUnit = unit; return nil }
	t.Cleanup(func() { mateRestartUnit = origRunner })

	if got := runMateCmd([]string{"update"}); got != 0 {
		t.Fatalf("runMateCmd(update) = %d, want 0", got)
	}
	if want := "zerops@mate.service"; restartedUnit != want {
		t.Errorf("restarted unit = %q, want %q", restartedUnit, want)
	}
}

func TestRunMateCmd_Update_RestartFailure_ReturnsNonZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedPinnedVersion(t, home)

	unitPath := filepath.Join(t.TempDir(), "zerops@mate.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	origUnitPath := mateUnitFilePath
	mateUnitFilePath = unitPath
	t.Cleanup(func() { mateUnitFilePath = origUnitPath })

	origRunner := mateRestartUnit
	mateRestartUnit = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { mateRestartUnit = origRunner })

	if got := runMateCmd([]string{"update"}); got != 1 {
		t.Errorf("runMateCmd(update) = %d, want 1 when the restart fails", got)
	}
}
