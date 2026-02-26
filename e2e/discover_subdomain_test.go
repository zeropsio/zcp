//go:build e2e

// Tests for: e2e — subdomain URL construction via zerops_subdomain enable.
//
// Multi-runtime test that deploys code and verifies HTTP 200 on returned
// subdomain URLs. Proves URLs actually work, not just that they look right.
//
// Test matrix: nodejs@22 (port 3000), php-nginx@8.4 (port 80), go@1 (port 8080).
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - zcli installed and in PATH
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_SubdomainEnableUrls -v -timeout 900s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createMinimalPHPApp creates a temp directory with a minimal PHP app and zerops.yml.
func createMinimalPHPApp(t *testing.T, hostname string) string {
	t.Helper()
	dir := t.TempDir()

	zeropsYML := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: php-nginx@8.4
      buildCommands:
        - echo "build done"
      deployFiles: ./
    run:
      base: php-nginx@8.4
      documentRoot: public
      ports:
        - port: 80
          httpSupport: true
`, hostname)

	if err := os.MkdirAll(filepath.Join(dir, "public"), 0o755); err != nil {
		t.Fatalf("mkdir public: %v", err)
	}

	indexPHP := `<?php
if ($_SERVER['REQUEST_URI'] === '/health') {
    http_response_code(200);
    echo 'ok';
    exit;
}
echo 'hello from e2e php test';
`

	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(zeropsYML), 0o644); err != nil {
		t.Fatalf("write zerops.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "public", "index.php"), []byte(indexPHP), 0o644); err != nil {
		t.Fatalf("write index.php: %v", err)
	}

	gitInit(t, dir)
	return dir
}

// createMinimalGoApp creates a temp directory with a minimal Go app and zerops.yml.
func createMinimalGoApp(t *testing.T, hostname string) string {
	t.Helper()
	dir := t.TempDir()

	zeropsYML := fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: go@1
      buildCommands:
        - go build -o app ./main.go
      deployFiles: ./app
    run:
      base: go@1
      ports:
        - port: 8080
          httpSupport: true
      start: ./app
`, hostname)

	mainGo := `package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "hello from e2e go test")
	})
	fmt.Println("listening on 8080")
	http.ListenAndServe(":8080", nil)
}
`

	goMod := fmt.Sprintf("module %s\n\ngo 1.22\n", hostname)

	if err := os.WriteFile(filepath.Join(dir, "zerops.yml"), []byte(zeropsYML), 0o644); err != nil {
		t.Fatalf("write zerops.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	gitInit(t, dir)
	return dir
}

// gitInit initializes a git repo with an initial commit in the given directory.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s (%v)", args, string(out), err)
		}
	}
}

// assertSubdomainURL checks that a URL returned by zerops_subdomain enable is well-formed.
func assertSubdomainURL(t *testing.T, url, hostname string) {
	t.Helper()
	if url == "" {
		t.Fatal("empty subdomain URL")
	}
	if strings.HasSuffix(url, ".") {
		t.Errorf("URL ends with dot (truncated): %s", url)
	}
	if !strings.Contains(url, ".zerops.app") {
		t.Errorf("URL missing .zerops.app domain: %s", url)
	}
	if !strings.HasPrefix(url, "https://") {
		t.Errorf("URL not HTTPS: %s", url)
	}
	if !strings.Contains(url, hostname) {
		t.Errorf("URL should contain hostname %q: %s", hostname, url)
	}
}

func TestE2E_SubdomainEnableUrls(t *testing.T) {
	// Check zcli is available.
	if _, err := exec.LookPath("zcli"); err != nil {
		t.Skip("zcli not in PATH — skipping subdomain URL E2E test")
	}

	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()

	// Login zcli with the same token.
	zcliLogin(t, h.authInfo.Token)

	type testCase struct {
		name     string
		hostname string
		port     int
		wantPort string // "-{port}" suffix in URL, empty for port 80
		appDir   string // set after app creation
	}

	tests := []testCase{
		{
			name:     "nodejs port 3000",
			hostname: "zcpsdnj" + suffix,
			port:     3000,
			wantPort: "-3000",
		},
		{
			name:     "php-nginx port 80",
			hostname: "zcpsdph" + suffix,
			port:     80,
			wantPort: "", // port 80 omits port suffix
		},
		{
			name:     "go port 8080",
			hostname: "zcpsdgo" + suffix,
			port:     8080,
			wantPort: "-8080",
		},
	}

	// Create app directories for each runtime.
	tests[0].appDir = createMinimalApp(t, tests[0].hostname)
	tests[1].appDir = createMinimalPHPApp(t, tests[1].hostname)
	tests[2].appDir = createMinimalGoApp(t, tests[2].hostname)

	// Collect all hostnames for cleanup.
	allHostnames := make([]string, len(tests))
	for i, tt := range tests {
		allHostnames[i] = tt.hostname
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, allHostnames...)
	})

	step := 0

	// --- Step 1: Import all services ---
	step++
	logStep(t, step, "zerops_import (3 runtimes: nodejs, php-nginx, go)")
	importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    enableSubdomainAccess: true
    ports:
      - port: 3000
        protocol: TCP
        httpSupport: true
  - hostname: %s
    type: php-nginx@8.4
    minContainers: 1
    enableSubdomainAccess: true
    ports:
      - port: 80
        protocol: TCP
        httpSupport: true
  - hostname: %s
    type: go@1
    minContainers: 1
    enableSubdomainAccess: true
    ports:
      - port: 8080
        protocol: TCP
        httpSupport: true
`, tests[0].hostname, tests[1].hostname, tests[2].hostname)

	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": importYAML,
	})
	var importResult struct {
		Processes []struct {
			ProcessID string `json:"processId"`
			Status    string `json:"status"`
		} `json:"processes"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(importText), &importResult); err != nil {
		t.Fatalf("parse import result: %v", err)
	}
	t.Logf("  Import: %s", importResult.Summary)
	for _, proc := range importResult.Processes {
		if proc.Status != "FINISHED" {
			t.Errorf("import process %s status = %s, want FINISHED", proc.ProcessID, proc.Status)
		}
	}

	// --- Step 2: Wait for all services to be ready ---
	step++
	logStep(t, step, "waiting for all services to be ready")
	for _, tt := range tests {
		waitForServiceReady(s, tt.hostname)
		t.Logf("  %s ready", tt.hostname)
	}

	// --- Step 3: Deploy each service ---
	step++
	logStep(t, step, "deploying all services")
	for _, tt := range tests {
		t.Logf("  Deploying %s from %s", tt.hostname, tt.appDir)
		deployText := s.mustCallSuccess("zerops_deploy", map[string]any{
			"targetService": tt.hostname,
			"workingDir":    tt.appDir,
		})
		var deployResult struct {
			Status      string `json:"status"`
			BuildStatus string `json:"buildStatus"`
		}
		if err := json.Unmarshal([]byte(deployText), &deployResult); err != nil {
			t.Fatalf("parse deploy result for %s: %v", tt.hostname, err)
		}
		if deployResult.Status != "DEPLOYED" {
			t.Errorf("%s deploy status = %s, want DEPLOYED", tt.hostname, deployResult.Status)
		}
		t.Logf("  %s: status=%s buildStatus=%s", tt.hostname, deployResult.Status, deployResult.BuildStatus)
	}

	// --- Step 4: Wait for RUNNING status ---
	step++
	logStep(t, step, "verifying all services RUNNING")
	for _, tt := range tests {
		var svcStatus string
		for i := 0; i < 20; i++ {
			discoverText := s.mustCallSuccess("zerops_discover", map[string]any{
				"service": tt.hostname,
			})
			var disc struct {
				Services []struct {
					Status string `json:"status"`
				} `json:"services"`
			}
			if err := json.Unmarshal([]byte(discoverText), &disc); err != nil {
				t.Fatalf("parse discover for %s: %v", tt.hostname, err)
			}
			if len(disc.Services) > 0 {
				svcStatus = disc.Services[0].Status
				if svcStatus == "RUNNING" || svcStatus == "ACTIVE" {
					break
				}
			}
			time.Sleep(5 * time.Second)
		}
		if svcStatus != "RUNNING" && svcStatus != "ACTIVE" {
			t.Fatalf("%s status = %s, want RUNNING or ACTIVE", tt.hostname, svcStatus)
		}
		t.Logf("  %s: %s", tt.hostname, svcStatus)
	}

	// --- Step 5: Enable subdomain and verify URLs ---
	step++
	logStep(t, step, "zerops_subdomain enable + URL format verification")
	for i := range tests {
		tt := &tests[i]
		enableText := s.mustCallSuccess("zerops_subdomain", map[string]any{
			"serviceHostname": tt.hostname,
			"action":          "enable",
		})
		var enableResult struct {
			SubdomainUrls []string `json:"subdomainUrls"`
		}
		if err := json.Unmarshal([]byte(enableText), &enableResult); err != nil {
			t.Fatalf("parse enable result for %s: %v", tt.hostname, err)
		}

		if len(enableResult.SubdomainUrls) == 0 {
			t.Fatalf("%s: expected subdomainUrls from enable response", tt.hostname)
		}

		url := enableResult.SubdomainUrls[0]
		t.Logf("  %s subdomainUrl: %s", tt.hostname, url)

		// URL format assertions — these catch the truncated URL bug.
		assertSubdomainURL(t, url, tt.hostname)

		// Verify port suffix pattern.
		if tt.wantPort != "" {
			if !strings.Contains(url, tt.wantPort+".") {
				t.Errorf("%s: URL should contain port suffix %q, got: %s", tt.hostname, tt.wantPort, url)
			}
		} else {
			if strings.Contains(url, "-80.") {
				t.Errorf("%s: port 80 URL should NOT contain -80 suffix, got: %s", tt.hostname, url)
			}
		}

		// Wait for enable process if one was returned.
		var enableProc struct {
			Process *struct {
				ID string `json:"id"`
			} `json:"process"`
		}
		if err := json.Unmarshal([]byte(enableText), &enableProc); err == nil && enableProc.Process != nil && enableProc.Process.ID != "" {
			t.Logf("  Waiting for enable process %s", enableProc.Process.ID)
			waitForProcess(s, enableProc.Process.ID)
		}
	}

	// --- Step 6: Verify HTTP reachability on each subdomain URL ---
	step++
	logStep(t, step, "HTTP health checks on all subdomain URLs")
	for _, tt := range tests {
		enableText := s.mustCallSuccess("zerops_subdomain", map[string]any{
			"serviceHostname": tt.hostname,
			"action":          "enable",
		})
		var enableResult struct {
			SubdomainUrls []string `json:"subdomainUrls"`
		}
		if err := json.Unmarshal([]byte(enableText), &enableResult); err != nil || len(enableResult.SubdomainUrls) == 0 {
			t.Fatalf("%s: cannot get subdomain URL for health check", tt.hostname)
		}

		healthURL := enableResult.SubdomainUrls[0] + "/health"
		// PHP-Nginx serves from documentRoot — /health maps to public/index.php
		// which checks REQUEST_URI. For simplicity, just use the base URL for PHP.
		if tt.port == 80 {
			healthURL = enableResult.SubdomainUrls[0] + "/health"
		}

		t.Logf("  Polling %s", healthURL)
		code, ok := pollHTTPHealth(healthURL, 5*time.Second, 90*time.Second)
		if !ok {
			t.Fatalf("%s: health check failed (last status=%d), want 200", tt.hostname, code)
		}
		t.Logf("  %s: HTTP %d OK", tt.hostname, code)
	}

	// --- Step 7: Delete all test services ---
	step++
	logStep(t, step, "deleting all test services")
	for _, tt := range tests {
		deleteText := s.mustCallSuccess("zerops_delete", map[string]any{
			"serviceHostname": tt.hostname,
			"confirm":         true,
		})
		procID := extractProcessID(t, deleteText)
		waitForProcess(s, procID)
		t.Logf("  Deleted %s", tt.hostname)
	}
}
