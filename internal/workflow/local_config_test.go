// Tests for: workflow/local_config.go — local dev config persistence.
package workflow

import "testing"

func TestLocalConfig_ReadWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	config := &LocalConfig{Port: 3000, EnvFile: ".env"}

	if err := WriteLocalConfig(dir, config); err != nil {
		t.Fatalf("WriteLocalConfig: %v", err)
	}

	got, err := ReadLocalConfig(dir)
	if err != nil {
		t.Fatalf("ReadLocalConfig: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil config")
	}
	if got.Port != 3000 {
		t.Errorf("Port = %d, want 3000", got.Port)
	}
	if got.EnvFile != ".env" {
		t.Errorf("EnvFile = %s, want .env", got.EnvFile)
	}
}

func TestLocalConfig_ReadNonexistent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	got, err := ReadLocalConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent config")
	}
}
