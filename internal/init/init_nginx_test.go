package init_test

import (
	"os"
	"path/filepath"
	"slices"
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
	zcpinit.SetNginxLogFiles(nil)
	t.Cleanup(func() { zcpinit.ResetNginxLogFiles() })
	zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
	t.Cleanup(func() { zcpinit.ResetNginxOwner() })
	const password = "alnum123token"
	t.Setenv("VSCODE_PASSWORD", password)
	t.Setenv("ZCP_Z3_ENABLED", "1")

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
		name     string
		contains string
	}{
		{"has worker_processes", "worker_processes auto;"},
		{"has raw password in cookie map", password},
		{"has login page", "/zcp-login"},
		{"has auth endpoint with raw password", "/zcp-auth/" + password},
		{"has logout endpoint", "/zcp-logout"},
		{"has cookie set with raw password", "__zcp_auth=" + password},
		{"has proxy pass", "proxy_pass http://127.0.0.1:8081"},
		{"has CSP header", "frame-ancestors"},
		{"has websocket upgrade", "proxy_set_header Upgrade"},
		{"publishes z3 under its base path", "location /z3/ {"},
		{"reaches the container's readiness even with auth on", "location = /z3/healthz {"},
		{"closes code-server's proxy door to the z3 port", "location ~ ^/(abs)?proxy/3773(/|$) {"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(content, tt.contains) {
				t.Errorf("nginx.conf should contain %q", tt.contains)
			}
		})
	}
}

// The dashboard embeds the editor in a cross-site iframe (app.zerops.io is
// site zerops.io; the editor subdomain is site zerops.app, PSL-listed), so
// every cookie the editor host sets there is a third-party cookie. Safari
// (ITP) and Chromium private modes drop third-party Set-Cookie unless it is
// Partitioned (CHIPS); without it the /zcp-auth → 302 / → /zcp-login loop
// never terminates inside the embed. The logout clear must be emitted BOTH
// partitioned and unpartitioned: a clear only removes a cookie whose
// Partitioned attribute matches, and pre-CHIPS jars hold unpartitioned ones.
func TestRunNginx_AuthCookiePartitioned(t *testing.T) {
	// Not parallel — mutates package-level vars.
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nginx.conf")
	zcpinit.SetNginxOutputPath(outputPath)
	t.Cleanup(func() { zcpinit.ResetNginxOutputPath() })
	zcpinit.SetNginxDirs([]string{filepath.Join(tmpDir, "log")})
	t.Cleanup(func() { zcpinit.ResetNginxDirs() })
	zcpinit.SetNginxLogFiles(nil)
	t.Cleanup(func() { zcpinit.ResetNginxLogFiles() })
	zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
	t.Cleanup(func() { zcpinit.ResetNginxOwner() })
	const password = "alnum123token"
	t.Setenv("VSCODE_PASSWORD", password)

	if err := zcpinit.RunNginx(); err != nil {
		t.Fatalf("RunNginx() error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read nginx.conf: %v", err)
	}
	content := string(data)

	tests := []struct {
		name     string
		contains string
	}{
		{
			"auth set is partitioned",
			"__zcp_auth=" + password + "; Path=/; HttpOnly; SameSite=None; Secure; Partitioned; Max-Age=86400",
		},
		{
			"logout clears partitioned jar",
			"__zcp_auth=; Path=/; HttpOnly; SameSite=None; Secure; Partitioned; Max-Age=0",
		},
		{
			"logout clears unpartitioned jar",
			"__zcp_auth=; Path=/; HttpOnly; SameSite=None; Secure; Max-Age=0",
		},
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
	zcpinit.SetNginxLogFiles(nil)
	t.Cleanup(func() { zcpinit.ResetNginxLogFiles() })
	zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
	t.Cleanup(func() { zcpinit.ResetNginxOwner() })
	// VSCODE_PASSWORD not set.
	t.Setenv("ZCP_Z3_ENABLED", "1")

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
		// z3 and readiness never depended on the container password —
		// they render identically whether or not auth is configured.
		{"still publishes z3", "location /z3/ {", true},
		{"still answers readiness", "location = /z3/healthz {", true},
		{"still closes the proxy door to the z3 port", "location ~ ^/(abs)?proxy/3773(/|$) {", true},
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
	zcpinit.SetNginxLogFiles(nil)
	t.Cleanup(func() { zcpinit.ResetNginxLogFiles() })
	zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
	t.Cleanup(func() { zcpinit.ResetNginxOwner() })
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
		// Best practice: worker-owned 0755, not world-writable 0777.
		if perm := info.Mode().Perm(); perm != 0o755 {
			t.Errorf("%s perms = %o, want 0755", d, perm)
		}
	}
}

func TestRunNginx_CacheDirInDefaults(t *testing.T) {
	t.Parallel()
	// nginx caching writes to /var/cache/nginx; init must create + own it.
	dirs := zcpinit.DefaultNginxDirs()
	if !slices.Contains(dirs, "/var/cache/nginx") {
		t.Errorf("default nginx dirs must include /var/cache/nginx, got %v", dirs)
	}
}

func TestRunNginx_PreExistingLogFilesGet0644(t *testing.T) {
	// Not parallel — mutates package-level vars.
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "log")
	logFile := filepath.Join(logDir, "error.log")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("seed log dir: %v", err)
	}
	// Simulate apt's restrictive 0640 install.
	if err := os.WriteFile(logFile, []byte("x"), 0o640); err != nil {
		t.Fatalf("seed log file: %v", err)
	}

	zcpinit.SetNginxOutputPath(filepath.Join(tmpDir, "nginx.conf"))
	t.Cleanup(func() { zcpinit.ResetNginxOutputPath() })
	zcpinit.SetNginxDirs([]string{logDir})
	t.Cleanup(func() { zcpinit.ResetNginxDirs() })
	zcpinit.SetNginxLogFiles([]string{logFile})
	t.Cleanup(func() { zcpinit.ResetNginxLogFiles() })
	zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
	t.Cleanup(func() { zcpinit.ResetNginxOwner() })

	if err := zcpinit.RunNginx(); err != nil {
		t.Fatalf("RunNginx() error: %v", err)
	}

	info, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("log file perms = %o, want 0644 (owner-write, others-read)", perm)
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
	zcpinit.SetNginxLogFiles(nil)
	t.Cleanup(func() { zcpinit.ResetNginxLogFiles() })
	zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
	t.Cleanup(func() { zcpinit.ResetNginxOwner() })
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
	zcpinit.SetNginxLogFiles(nil)
	t.Cleanup(func() { zcpinit.ResetNginxLogFiles() })
	zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
	t.Cleanup(func() { zcpinit.ResetNginxOwner() })
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
