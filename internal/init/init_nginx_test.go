package init_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
)

func TestRunNginx_WithPassword(t *testing.T) {
	// Not parallel — mutates package-level vars.
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nginx.conf")
	zcpinit.SetNginxOutputPath(outputPath)
	t.Cleanup(func() { zcpinit.ResetNginxOutputPath() })
	zcpinit.SetNginxDirs([]string{filepath.Join(tmpDir, "log"), filepath.Join(tmpDir, "tmp")})
	t.Cleanup(func() { zcpinit.ResetNginxDirs() })
	t.Setenv("VSCODE_PASSWORD", "test-password-123")

	err := zcpinit.RunNginx()
	if err != nil {
		t.Fatalf("RunNginx() error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read nginx.conf: %v", err)
	}
	content := string(data)

	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test-password-123")))

	tests := []struct {
		name     string
		contains string
	}{
		{"has worker_processes", "worker_processes auto;"},
		{"has password hash in cookie map", expectedHash},
		{"has login page", "/zcp-login"},
		{"has auth endpoint", "/zcp-auth/" + expectedHash},
		{"has logout endpoint", "/zcp-logout"},
		{"has cookie set", "__zcp_auth=" + expectedHash},
		{"has proxy pass", "proxy_pass http://127.0.0.1:8081"},
		{"has CSP header", "frame-ancestors"},
		{"has websocket upgrade", "proxy_set_header Upgrade"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(content, tt.contains) {
				t.Errorf("nginx.conf should contain %q", tt.contains)
			}
		})
	}
}

func TestRunNginx_WithoutPassword(t *testing.T) {
	// Not parallel — mutates package-level vars.
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nginx.conf")
	zcpinit.SetNginxOutputPath(outputPath)
	t.Cleanup(func() { zcpinit.ResetNginxOutputPath() })
	zcpinit.SetNginxDirs([]string{filepath.Join(tmpDir, "log")})
	t.Cleanup(func() { zcpinit.ResetNginxDirs() })
	// VSCODE_PASSWORD not set.

	err := zcpinit.RunNginx()
	if err != nil {
		t.Fatalf("RunNginx() error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read nginx.conf: %v", err)
	}
	content := string(data)

	tests := []struct {
		name       string
		contains   string
		shouldFind bool
	}{
		{"has proxy pass", "proxy_pass http://127.0.0.1:8081", true},
		{"has CSP header", "frame-ancestors", true},
		{"no login page", "/zcp-login", false},
		{"no auth endpoint", "/zcp-auth/", false},
		{"no cookie map", "zcp_cookie_ok", false},
		{"no logout", "/zcp-logout", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := strings.Contains(content, tt.contains)
			if found != tt.shouldFind {
				if tt.shouldFind {
					t.Errorf("nginx.conf should contain %q", tt.contains)
				} else {
					t.Errorf("nginx.conf should NOT contain %q", tt.contains)
				}
			}
		})
	}
}

func TestRunNginx_CreatesDirectories(t *testing.T) {
	// Not parallel — mutates package-level vars.
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log", "nginx")
	tmpNginx := filepath.Join(tmpDir, "lib", "nginx", "tmp")
	zcpinit.SetNginxDirs([]string{logDir, tmpNginx})
	t.Cleanup(func() { zcpinit.ResetNginxDirs() })
	zcpinit.SetNginxOutputPath(filepath.Join(tmpDir, "nginx.conf"))
	t.Cleanup(func() { zcpinit.ResetNginxOutputPath() })

	err := zcpinit.RunNginx()
	if err != nil {
		t.Fatalf("RunNginx() error: %v", err)
	}

	dirs := []string{logDir, tmpNginx}
	for _, d := range dirs {
		info, err := os.Stat(d)
		if err != nil {
			t.Errorf("directory %s should exist: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s should be a directory", d)
		}
	}
}

func TestRunNginx_Idempotent(t *testing.T) {
	// Not parallel — mutates package-level vars.
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nginx.conf")
	zcpinit.SetNginxOutputPath(outputPath)
	t.Cleanup(func() { zcpinit.ResetNginxOutputPath() })
	zcpinit.SetNginxDirs([]string{filepath.Join(tmpDir, "log")})
	t.Cleanup(func() { zcpinit.ResetNginxDirs() })
	t.Setenv("VSCODE_PASSWORD", "idempotent-test")

	if err := zcpinit.RunNginx(); err != nil {
		t.Fatalf("first RunNginx() error: %v", err)
	}
	first, _ := os.ReadFile(outputPath)

	if err := zcpinit.RunNginx(); err != nil {
		t.Fatalf("second RunNginx() error: %v", err)
	}
	second, _ := os.ReadFile(outputPath)

	if string(first) != string(second) {
		t.Error("nginx.conf should be identical after two runs")
	}
}

func TestRunNginx_NoFakeServerBlock(t *testing.T) {
	// Not parallel — mutates package-level vars.
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nginx.conf")
	zcpinit.SetNginxOutputPath(outputPath)
	t.Cleanup(func() { zcpinit.ResetNginxOutputPath() })
	zcpinit.SetNginxDirs([]string{filepath.Join(tmpDir, "log")})
	t.Cleanup(func() { zcpinit.ResetNginxDirs() })
	t.Setenv("VSCODE_PASSWORD", "test")

	if err := zcpinit.RunNginx(); err != nil {
		t.Fatalf("RunNginx() error: %v", err)
	}

	data, _ := os.ReadFile(outputPath)
	content := string(data)

	// Should have exactly one server block (port 8080), not the fake 8081 one.
	if strings.Count(content, "listen 8080") != 1 {
		t.Error("should have exactly one server block on port 8080")
	}
	if strings.Contains(content, "listen 8081") {
		t.Error("should NOT have the fake server block on port 8081")
	}
}

func TestNginxConfig_HashComputation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		password string
		wantHash string
	}{
		{
			"known password",
			"3T1SUTUVs1)W4=_*",
			fmt.Sprintf("%x", sha256.Sum256([]byte("3T1SUTUVs1)W4=_*"))),
		},
		{
			"simple password",
			"test",
			fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(tt.password)))
			if hash != tt.wantHash {
				t.Errorf("hash mismatch: got %s, want %s", hash, tt.wantHash)
			}
		})
	}
}
