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
	t.Setenv("ZCP_MATE_ENABLED", "1")

	err := zcpinit.RunNginxWithRotationPath(filepath.Join(tmpDir, "logrotate-nginx"))
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
		{"publishes mate under its base path", "location /mate/ {"},
		{"reaches the container's readiness even with auth on", "location = /mate/healthz {"},
		{"closes code-server's proxy door to the mate port", "location ~ ^/(abs)?proxy/3773(/|$) {"},
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

	if err := zcpinit.RunNginxWithRotationPath(filepath.Join(tmpDir, "logrotate-nginx")); err != nil {
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
	t.Setenv("ZCP_MATE_ENABLED", "1")

	err := zcpinit.RunNginxWithRotationPath(filepath.Join(tmpDir, "logrotate-nginx"))
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
		// mate and readiness never depended on the container password —
		// they render identically whether or not auth is configured.
		{"still publishes mate", "location /mate/ {", true},
		{"still answers readiness", "location = /mate/healthz {", true},
		{"still closes the proxy door to the mate port", "location ~ ^/(abs)?proxy/3773(/|$) {", true},
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

	err := zcpinit.RunNginxWithRotationPath(filepath.Join(tmpDir, "logrotate-nginx"))
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

func TestRunNginx_LegacyLogFilesPreserved(t *testing.T) {
	// Not parallel — mutates package-level overrides.
	for _, tt := range []struct {
		name string
		mode os.FileMode
	}{
		{"legacy distribution permissions", 0o640},
		{"inaccessible legacy log", 0o000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			logDir := filepath.Join(tmpDir, "legacy-logs")
			if err := os.Mkdir(logDir, 0o755); err != nil {
				t.Fatal(err)
			}
			logFile := filepath.Join(logDir, "error.log")
			if err := os.WriteFile(logFile, []byte("historical log\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(logFile, tt.mode); err != nil {
				t.Fatal(err)
			}
			zcpinit.SetNginxOutputPath(filepath.Join(tmpDir, "nginx.conf"))
			t.Cleanup(zcpinit.ResetNginxOutputPath)
			zcpinit.SetNginxDirs([]string{filepath.Join(tmpDir, "cache")})
			t.Cleanup(zcpinit.ResetNginxDirs)
			zcpinit.SetNginxLogFiles([]string{logFile})
			t.Cleanup(zcpinit.ResetNginxLogFiles)
			zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
			t.Cleanup(zcpinit.ResetNginxOwner)
			if err := zcpinit.RunNginxWithRotationPath(filepath.Join(tmpDir, "nginx")); err != nil {
				t.Fatalf("RunNginx: %v", err)
			}
			info, err := os.Stat(logFile)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != tt.mode {
				t.Errorf("legacy log mode = %o, want %o", info.Mode().Perm(), tt.mode)
			}
			// Restore read access only after verifying init left the mode alone.
			if err := os.Chmod(logFile, 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(logFile)
			if err != nil || string(data) != "historical log\n" {
				t.Errorf("legacy log changed: %q, %v", data, err)
			}
			if slices.Contains(zcpinit.DefaultNginxDirs(), "/var/log/nginx") {
				t.Error("init must not manage obsolete log directories")
			}
		})
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

	if err := zcpinit.RunNginxWithRotationPath(filepath.Join(tmpDir, "logrotate-nginx")); err != nil {
		t.Fatalf("first RunNginx() error: %v", err)
	}
	first, _ := os.ReadFile(outputPath)

	if err := zcpinit.RunNginxWithRotationPath(filepath.Join(tmpDir, "logrotate-nginx")); err != nil {
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

	if err := zcpinit.RunNginxWithRotationPath(filepath.Join(tmpDir, "logrotate-nginx")); err != nil {
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

// Not parallel: nginx paths and ownership are package-level test overrides.
func TestRunNginx_FileRotationRetired(t *testing.T) {
	for _, tt := range []struct {
		name       string
		policy     string
		blocked    bool
		badConfig  bool
		wantError  string
		wantPolicy bool
	}{
		{name: "existing policy", policy: "create 0640 www-data adm\n"},
		{name: "absent policy"},
		{name: "removal failure", blocked: true, wantError: "nginx log rotation", wantPolicy: true},
		{name: "render failure preserves policy", policy: "create 0640 www-data adm\n", badConfig: true, wantError: "nginx config", wantPolicy: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			policy := filepath.Join(tmpDir, "nginx")
			if tt.blocked {
				if err := os.Mkdir(policy, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(policy, "child"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if tt.policy != "" {
				if err := os.WriteFile(policy, []byte(tt.policy), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			unrelated := filepath.Join(tmpDir, "other-service")
			if err := os.WriteFile(unrelated, []byte("unrelated policy\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(tmpDir, "nginx.conf")
			if tt.badConfig {
				output = filepath.Join(tmpDir, "missing", "nginx.conf")
			}
			zcpinit.SetNginxOutputPath(output)
			t.Cleanup(zcpinit.ResetNginxOutputPath)
			zcpinit.SetNginxDirs([]string{filepath.Join(tmpDir, "cache")})
			t.Cleanup(zcpinit.ResetNginxDirs)
			zcpinit.SetNginxLogFiles(nil)
			t.Cleanup(zcpinit.ResetNginxLogFiles)
			zcpinit.SetNginxOwner(os.Geteuid(), os.Getegid())
			t.Cleanup(zcpinit.ResetNginxOwner)
			err := zcpinit.RunNginxWithRotationPath(policy)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("RunNginx: %v", err)
				}
				if err := zcpinit.RunNginxWithRotationPath(policy); err != nil {
					t.Fatalf("repeat RunNginx: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("RunNginx error = %v, want %q", err, tt.wantError)
			}
			_, err = os.Stat(policy)
			if tt.wantPolicy {
				if err != nil {
					t.Errorf("policy must remain: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Errorf("obsolete policy must be absent, stat = %v", err)
			}
			data, err := os.ReadFile(unrelated)
			if err != nil || string(data) != "unrelated policy\n" {
				t.Errorf("unrelated policy changed: %q, %v", data, err)
			}
			if tt.badConfig {
				data, err := os.ReadFile(policy)
				if err != nil || string(data) != tt.policy {
					t.Errorf("failed render changed policy: %q, %v", data, err)
				}
			}
		})
	}
}
