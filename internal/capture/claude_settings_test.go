package capture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeSettingsPatch_InstallAndRestorePreservesUnrelatedConfiguration(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	original := `{
  "model": "opus",
  "env": {
    "ANTHROPIC_BASE_URL": "https://gateway.example/base",
    "KEEP_ME": "yes"
  }
}
`
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	installed := map[string]string{
		"ANTHROPIC_BASE_URL": "http://127.0.0.1:43210",
		EnvSessionID:         "capture-1",
		EnvSessionDir:        "/private/capture-1",
	}
	patch, err := InstallClaudeCaptureSettings(path, installed)
	if err != nil {
		t.Fatalf("InstallClaudeCaptureSettings() error = %v", err)
	}
	settings := readSettingsObject(t, path)
	env := settings["env"].(map[string]any)
	for key, want := range installed {
		if env[key] != want {
			t.Errorf("installed env[%s] = %v, want %q", key, env[key], want)
		}
	}
	if env["KEEP_ME"] != "yes" || settings["model"] != "opus" {
		t.Fatalf("unrelated settings changed: %+v", settings)
	}

	// Simulate an unrelated user edit while capture is active.
	settings["theme"] = "dark"
	writeSettingsObject(t, path, settings, 0o640)
	warnings, err := RestoreClaudeCaptureSettings(path, patch)
	if err != nil {
		t.Fatalf("RestoreClaudeCaptureSettings() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("restore warnings = %v", warnings)
	}
	restored := readSettingsObject(t, path)
	restoredEnv := restored["env"].(map[string]any)
	if restoredEnv["ANTHROPIC_BASE_URL"] != "https://gateway.example/base" || restoredEnv["KEEP_ME"] != "yes" {
		t.Fatalf("restored env = %+v", restoredEnv)
	}
	if _, ok := restoredEnv[EnvSessionID]; ok {
		t.Fatalf("capture session ID remains after restore: %+v", restoredEnv)
	}
	if _, ok := restoredEnv[EnvSessionDir]; ok {
		t.Fatalf("capture session dir remains after restore: %+v", restoredEnv)
	}
	if restored["theme"] != "dark" {
		t.Fatalf("concurrent edit was lost: %+v", restored)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("settings mode = %#o, want original 0640", got)
	}
}

func TestClaudeSettingsPatch_PrepareIsNonMutatingAndApplyUsesCompareAndSwap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	original := map[string]any{"model": "opus", "env": map[string]any{"ANTHROPIC_BASE_URL": "https://gateway.example"}}
	writeSettingsObject(t, path, original, 0o600)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original settings: %v", err)
	}
	patch, err := PrepareClaudeCaptureSettings(path, map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:43210"})
	if err != nil {
		t.Fatalf("PrepareClaudeCaptureSettings() error = %v", err)
	}
	afterPrepare, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prepared settings: %v", err)
	}
	if string(afterPrepare) != string(before) {
		t.Fatal("PrepareClaudeCaptureSettings changed the settings file before the restore patch could be journaled")
	}
	concurrent := readSettingsObject(t, path)
	concurrent["env"].(map[string]any)["ANTHROPIC_BASE_URL"] = "https://user-changed.example"
	writeSettingsObject(t, path, concurrent, 0o600)
	if err := ApplyClaudeCaptureSettings(path, patch); err == nil || !strings.Contains(err.Error(), "changed before") {
		t.Fatalf("ApplyClaudeCaptureSettings() error = %v, want compare-and-swap conflict", err)
	}
	got := readSettingsObject(t, path)
	if got["env"].(map[string]any)["ANTHROPIC_BASE_URL"] != "https://user-changed.example" {
		t.Fatalf("concurrent owned value was overwritten: %+v", got)
	}
}

func TestClaudeSettingsPatch_RestoreDoesNotOverwriteConflictingUserValue(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	writeSettingsObject(t, path, map[string]any{"env": map[string]any{}}, 0o600)
	patch, err := InstallClaudeCaptureSettings(path, map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:43210"})
	if err != nil {
		t.Fatalf("InstallClaudeCaptureSettings() error = %v", err)
	}
	settings := readSettingsObject(t, path)
	settings["env"].(map[string]any)["ANTHROPIC_BASE_URL"] = "https://user-new-gateway.example"
	writeSettingsObject(t, path, settings, 0o600)

	warnings, err := RestoreClaudeCaptureSettings(path, patch)
	if err != nil {
		t.Fatalf("RestoreClaudeCaptureSettings() error = %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "ANTHROPIC_BASE_URL") {
		t.Fatalf("warnings = %v, want ownership conflict", warnings)
	}
	restored := readSettingsObject(t, path)
	if got := restored["env"].(map[string]any)["ANTHROPIC_BASE_URL"]; got != "https://user-new-gateway.example" {
		t.Fatalf("conflicting user value overwritten: %v", got)
	}
}

func TestClaudeSettingsPatch_NewFileRemovedWhenNoUserConfigurationRemains(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	patch, err := InstallClaudeCaptureSettings(path, map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:43210"})
	if err != nil {
		t.Fatalf("InstallClaudeCaptureSettings() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("installed settings missing: %v", err)
	}
	if _, err := RestoreClaudeCaptureSettings(path, patch); err != nil {
		t.Fatalf("RestoreClaudeCaptureSettings() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("new empty settings file remains after restore: %v", err)
	}
}

func readSettingsObject(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	return object
}

func writeSettingsObject(t *testing.T, path string, object map[string]any, mode os.FileMode) {
	t.Helper()
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}
