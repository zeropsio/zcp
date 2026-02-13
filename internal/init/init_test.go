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
	if !strings.Contains(content, "zerops_workflow") {
		t.Error("CLAUDE.md should mention zerops_workflow")
	}
}

func TestRun_GeneratesMCPConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "zcp") {
		t.Error(".mcp.json should reference zcp")
	}
}

func TestRun_GeneratesSSHConfig(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	err := zcpinit.Run(dir)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		t.Fatalf("read ssh config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "zerops") {
		t.Error("ssh config should mention zerops")
	}
}

func TestRun_GeneratesSettingsLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	err := zcpinit.Run(dir)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "mcp__zerops__*") {
		t.Error("settings.local.json should contain mcp__zerops__* permission")
	}
}

func TestRun_GeneratesCLAUDEMD_StrongInstructions(t *testing.T) {
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
	required := []string{"MANDATORY", "zerops_workflow", "zerops_knowledge", "zerops_context"}
	for _, keyword := range required {
		if !strings.Contains(content, keyword) {
			t.Errorf("CLAUDE.md should contain %q", keyword)
		}
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
	if !strings.Contains(string(data), "zerops_workflow") {
		t.Error("CLAUDE.md should still contain zerops_workflow after second run")
	}
}

func TestRun_ReportsSteps(t *testing.T) {
	// Not parallel — mutates HOME env var.
	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	err := zcpinit.Run(dir)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// All four files should exist.
	files := []string{
		filepath.Join(dir, "CLAUDE.md"),
		filepath.Join(dir, ".mcp.json"),
		filepath.Join(dir, ".claude", "settings.local.json"),
		filepath.Join(homeDir, ".ssh", "config"),
	}
	for _, f := range files {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}
}
