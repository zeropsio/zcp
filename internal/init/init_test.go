// Tests for: init package — zcp init subcommand.
package init_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
)

func TestRun_GeneratesCLAUDEMD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Zerops") {
		t.Error("CLAUDE.md should mention Zerops")
	}
	if !strings.Contains(content, "zerops.yml") {
		t.Error("CLAUDE.md should mention zerops.yml")
	}
}

func TestRun_GeneratesMCPConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "mcp-config.json"))
	if err != nil {
		t.Fatalf("read mcp-config.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "zcp") {
		t.Error("mcp-config.json should reference zcp")
	}
}

func TestRun_GeneratesSSHConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "zerops") {
		t.Error("ssh config should mention zerops")
	}
}

func TestRun_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Run twice.
	if err := zcpinit.Run(dir); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if err := zcpinit.Run(dir); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	// Files should still exist and be valid.
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md after second run: %v", err)
	}
	if !strings.Contains(string(data), "Zerops") {
		t.Error("CLAUDE.md should still contain Zerops after second run")
	}
}

func TestRun_ReportsSteps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// All three files should exist.
	files := []string{
		filepath.Join(dir, "CLAUDE.md"),
		filepath.Join(dir, ".claude", "mcp-config.json"),
		filepath.Join(dir, ".ssh", "config"),
	}
	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}
}
