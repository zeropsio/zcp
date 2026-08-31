package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/z3"
)

// seedPinnedVersion lays down a versioned install at z3.PinnedVersion and
// activates it, so z3.EnsureInstalled — run for real, unstubbed — finds the
// desired version already live and reaches no network at all.
func seedPinnedVersion(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".zcp", "z3", "versions", z3.PinnedVersion)
	pkgDir := filepath.Join(dir, "node_modules", z3.PackageName)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pkgDir, err)
	}
	body := `{"name":"` + z3.PackageName + `","version":"` + z3.PinnedVersion + `"}`
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	current := filepath.Join(home, ".zcp", "z3", "current")
	if err := os.Symlink(filepath.Join("versions", z3.PinnedVersion), current); err != nil {
		t.Fatalf("symlink current: %v", err)
	}
}

func TestRunZ3Cmd_UnknownSubcommand_Fails(t *testing.T) {
	tests := [][]string{nil, {}, {"bogus"}, {"--force"}}
	for _, args := range tests {
		if got := runZ3Cmd(args); got != 1 {
			t.Errorf("runZ3Cmd(%v) = %d, want 1", args, got)
		}
	}
}

func TestRunZ3Cmd_Update_NoUnitFile_SkipsRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedPinnedVersion(t, home)

	origUnitPath := z3UnitFilePath
	z3UnitFilePath = filepath.Join(t.TempDir(), "no-such-unit.service")
	t.Cleanup(func() { z3UnitFilePath = origUnitPath })

	restarted := false
	origRunner := z3RestartUnit
	z3RestartUnit = func(string) error { restarted = true; return nil }
	t.Cleanup(func() { z3RestartUnit = origRunner })

	if got := runZ3Cmd([]string{"update"}); got != 0 {
		t.Fatalf("runZ3Cmd(update) = %d, want 0", got)
	}
	if restarted {
		t.Error("no unit file present must mean no restart attempt")
	}
}

func TestRunZ3Cmd_Update_UnitFilePresent_Restarts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedPinnedVersion(t, home)

	unitPath := filepath.Join(t.TempDir(), "zerops@z3.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	origUnitPath := z3UnitFilePath
	z3UnitFilePath = unitPath
	t.Cleanup(func() { z3UnitFilePath = origUnitPath })

	var restartedUnit string
	origRunner := z3RestartUnit
	z3RestartUnit = func(unit string) error { restartedUnit = unit; return nil }
	t.Cleanup(func() { z3RestartUnit = origRunner })

	if got := runZ3Cmd([]string{"update"}); got != 0 {
		t.Fatalf("runZ3Cmd(update) = %d, want 0", got)
	}
	if want := "zerops@z3.service"; restartedUnit != want {
		t.Errorf("restarted unit = %q, want %q", restartedUnit, want)
	}
}

func TestRunZ3Cmd_Update_RestartFailure_ReturnsNonZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	seedPinnedVersion(t, home)

	unitPath := filepath.Join(t.TempDir(), "zerops@z3.service")
	if err := os.WriteFile(unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatalf("seed unit file: %v", err)
	}
	origUnitPath := z3UnitFilePath
	z3UnitFilePath = unitPath
	t.Cleanup(func() { z3UnitFilePath = origUnitPath })

	origRunner := z3RestartUnit
	z3RestartUnit = func(string) error { return os.ErrPermission }
	t.Cleanup(func() { z3RestartUnit = origRunner })

	if got := runZ3Cmd([]string{"update"}); got != 1 {
		t.Errorf("runZ3Cmd(update) = %d, want 1 when the restart fails", got)
	}
}
