package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInjectMCPEnvIfUnset_LiftsTokenFromMCPJSON proves the token-blind bridge:
// when ZCP_API_KEY isn't already set, the studio transport lifts it from the
// project-scoped .mcp.json env block so the extension never has to read or
// forward the secret. Non-parallel: mutates process env.
func TestInjectMCPEnvIfUnset_LiftsTokenFromMCPJSON(t *testing.T) {
	dir := t.TempDir()
	mcp := `{"mcpServers":{"zerops":{"command":"zcp","args":["serve"],"env":{"ZCP_API_KEY":"tok-123","ZCP_API_HOST":"api.example.zerops.io"}}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZCP_API_KEY", "")
	t.Setenv("ZCP_API_HOST", "")
	_ = os.Unsetenv("ZCP_API_KEY")
	_ = os.Unsetenv("ZCP_API_HOST")

	injectMCPEnvIfUnset(dir)

	if got := os.Getenv("ZCP_API_KEY"); got != "tok-123" {
		t.Errorf("ZCP_API_KEY=%q, want tok-123 (lifted from .mcp.json env block)", got)
	}
	if got := os.Getenv("ZCP_API_HOST"); got != "api.example.zerops.io" {
		t.Errorf("ZCP_API_HOST=%q, want api.example.zerops.io", got)
	}
}

// TestInjectMCPEnvIfUnset_RespectsExistingEnv proves an already-set token wins
// (env-first resolution) — the .mcp.json fallback never clobbers it.
func TestInjectMCPEnvIfUnset_RespectsExistingEnv(t *testing.T) {
	dir := t.TempDir()
	mcp := `{"mcpServers":{"zerops":{"env":{"ZCP_API_KEY":"from-file"}}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZCP_API_KEY", "from-env")

	injectMCPEnvIfUnset(dir)

	if got := os.Getenv("ZCP_API_KEY"); got != "from-env" {
		t.Errorf("ZCP_API_KEY=%q, want from-env (existing env must win)", got)
	}
}

// TestInjectMCPEnvIfUnset_NoFileIsNoOp proves a missing .mcp.json is silently
// tolerated (resolution falls through to zcli, exactly as today).
func TestInjectMCPEnvIfUnset_NoFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCP_API_KEY", "")
	_ = os.Unsetenv("ZCP_API_KEY")

	injectMCPEnvIfUnset(dir) // must not panic or error

	if got := os.Getenv("ZCP_API_KEY"); got != "" {
		t.Errorf("ZCP_API_KEY=%q, want empty (no .mcp.json to lift from)", got)
	}
}
