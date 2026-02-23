//go:build e2e

// Tests for: e2e — subdomain URL construction via zerops_subdomain enable.
//
// Verifies that zerops_subdomain enable returns correct subdomainUrls for services
// with different port configurations (80, 3000, 8080).
//
// Prerequisites:
//   - ZCP_API_KEY set
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_SubdomainEnableUrls -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestE2E_SubdomainEnableUrls(t *testing.T) {
	h := newHarness(t)
	s := newSession(t, h.srv)

	suffix := randomSuffix()

	tests := []struct {
		name     string
		hostname string
		port     int
		wantPort string // "-{port}" suffix in URL, empty for port 80
	}{
		{
			name:     "port 3000",
			hostname: "zcpsd3k" + suffix,
			port:     3000,
			wantPort: "-3000",
		},
		{
			name:     "port 8080",
			hostname: "zcpsd8k" + suffix,
			port:     8080,
			wantPort: "-8080",
		},
		{
			name:     "port 80",
			hostname: "zcpsd80" + suffix,
			port:     80,
			wantPort: "", // port 80 omits port suffix
		},
	}

	// Build import YAML for all test services.
	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("services:\n")
	for _, tt := range tests {
		fmt.Fprintf(&yamlBuilder, `  - hostname: %s
    type: nodejs@22
    minContainers: 1
    enableSubdomainAccess: true
    ports:
      - port: %d
        protocol: TCP
        httpSupport: true
`, tt.hostname, tt.port)
	}

	// Collect all hostnames for cleanup.
	allHostnames := make([]string, len(tests))
	for i, tt := range tests {
		allHostnames[i] = tt.hostname
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, allHostnames...)
	})

	// Step 1: Import all services.
	t.Log("Step 1: Import test services")
	importText := s.mustCallSuccess("zerops_import", map[string]any{
		"content": yamlBuilder.String(),
	})
	var importResult struct {
		Processes []struct {
			ProcessID string `json:"processId"`
			Status    string `json:"status"`
		} `json:"processes"`
	}
	if err := json.Unmarshal([]byte(importText), &importResult); err != nil {
		t.Fatalf("parse import result: %v", err)
	}
	for _, proc := range importResult.Processes {
		if proc.Status != "FINISHED" {
			t.Errorf("import process %s status = %s, want FINISHED", proc.ProcessID, proc.Status)
		}
	}

	// Step 2: Wait for all services to be ready.
	t.Log("Step 2: Waiting for services to be ready")
	for _, tt := range tests {
		waitForServiceReady(s, tt.hostname)
	}

	// Step 3: Verify subdomainUrls from zerops_subdomain enable for each service.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Enable subdomain — the response should contain subdomainUrls.
			enableText := s.mustCallSuccess("zerops_subdomain", map[string]any{
				"serviceHostname": tt.hostname,
				"action":          "enable",
			})
			var enableResult struct {
				SubdomainUrls []string `json:"subdomainUrls"`
			}
			if err := json.Unmarshal([]byte(enableText), &enableResult); err != nil {
				t.Fatalf("parse enable result: %v", err)
			}

			// Verify subdomainUrls are present.
			if len(enableResult.SubdomainUrls) == 0 {
				t.Fatal("expected subdomainUrls from enable response")
			}

			url := enableResult.SubdomainUrls[0]
			t.Logf("  subdomainUrl: %s", url)

			// URL must start with https:// and contain hostname.
			if !strings.HasPrefix(url, "https://") {
				t.Errorf("URL should start with https://, got: %s", url)
			}
			if !strings.Contains(url, tt.hostname) {
				t.Errorf("URL should contain hostname %q, got: %s", tt.hostname, url)
			}

			// Verify port suffix pattern.
			if tt.wantPort != "" {
				if !strings.Contains(url, tt.wantPort+".") {
					t.Errorf("URL should contain port suffix %q, got: %s", tt.wantPort, url)
				}
			} else {
				// Port 80 should NOT have port suffix.
				if strings.Contains(url, "-80.") {
					t.Errorf("port 80 URL should NOT contain -80 suffix, got: %s", url)
				}
			}

			// Verify zerops_discover does NOT include subdomainUrls.
			discoverText := s.mustCallSuccess("zerops_discover", map[string]any{
				"service": tt.hostname,
			})
			if strings.Contains(discoverText, "subdomainUrls") {
				t.Error("zerops_discover should NOT include subdomainUrls field")
			}
		})
	}

	// Step 4: Delete all test services.
	t.Log("Step 4: Deleting test services")
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
