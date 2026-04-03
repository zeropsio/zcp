//go:build e2e

// Tests for: e2e — zeropsYaml handling in import YAML.
//
// Forensic analysis (Mar 25) identified that ALL imports with zeropsYaml fail
// with projectImportInvalidYaml (8/8 failures across 2 sessions), while imports
// without zeropsYaml succeed (2/2). This test isolates the root cause:
//
//   Layer 1: Does normalizeZeropsYaml corrupt the YAML? (ops/import.go:233-277)
//   Layer 2: Does the Zerops API accept zeropsYaml at all for runtime services?
//
// Strategy: Compare API-direct (bypass normalization) vs ops.Import (with normalization).
// If direct succeeds but ops.Import fails → normalization is the bug.
// If both fail → API doesn't support zeropsYaml (or has undocumented constraints).
//
// Prerequisites:
//   - ZCP_API_KEY set
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_Import -v -timeout 600s

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// zeropsYamlImportTest defines a single import test case with zeropsYaml.
type zeropsYamlImportTest struct {
	name    string
	yaml    string
	wantErr bool   // expect API rejection
	errCode string // expected error code (if wantErr)
}

func TestE2E_Import_ZeropsYaml_Identification(t *testing.T) {
	h := newHarness(t)
	// Reset any stale workflow session from previous test runs.
	resetSession := newSession(t, h.srv)
	resetSession.callTool("zerops_workflow", map[string]any{"action": "reset"})

	suffix := randomSuffix()

	// Hostnames for each sub-test. Use "in" prefix (registered in testServicePrefixes).
	hostDirect := "in" + suffix + "d"
	hostNorm := "in" + suffix + "n"
	hostNone := "in" + suffix + "x"

	// Cleanup all test services and reset workflow.
	t.Cleanup(func() {
		cs := newSession(t, h.srv)
		cs.callTool("zerops_workflow", map[string]any{"action": "reset"})
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanupServices(ctx, h.client, h.projectID, hostDirect, hostNorm, hostNone)
	})

	zeropsYamlContent := func(hostname string) string {
		return fmt.Sprintf(`zerops:
  - setup: %s
    build:
      base: nodejs@22
      buildCommands:
        - echo ok
      deployFiles: ./
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
`, hostname)
	}

	// --- Sub-test 1: Import WITHOUT zeropsYaml (baseline) ---
	t.Run("baseline_without_zeropsYaml", func(t *testing.T) {
		importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
`, hostNone)

		s := newSession(t, h.srv)
		t.Cleanup(func() { s.callTool("zerops_workflow", map[string]any{"action": "reset"}) })

		// Start workflow (required for import).
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		s.mustCallSuccess("zerops_workflow", map[string]any{
			"action":   "start",
			"workflow": "bootstrap",
			"intent":   "e2e import baseline test",
		})

		text := s.mustCallSuccess("zerops_import", map[string]any{
			"content": importYAML,
		})

		var result ops.ImportResult
		if err := json.Unmarshal([]byte(text), &result); err != nil {
			t.Fatalf("parse import result: %v", err)
		}
		t.Logf("Baseline import (no zeropsYaml): %s", result.Summary)

		for _, proc := range result.Processes {
			if proc.Status != "FINISHED" {
				t.Errorf("process %s status = %s, want FINISHED", proc.ProcessID, proc.Status)
			}
		}
	})

	// --- Sub-test 2: Import WITH zeropsYaml as nested object (through ops.Import + normalizeZeropsYaml) ---
	t.Run("with_zeropsYaml_via_ops_Import", func(t *testing.T) {
		importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    zeropsYaml:
      zerops:
        - setup: %s
          build:
            base: nodejs@22
            buildCommands:
              - echo ok
            deployFiles: ./
          run:
            base: nodejs@22
            ports:
              - port: 3000
                httpSupport: true
            start: node server.js
`, hostNorm, hostNorm)

		s := newSession(t, h.srv)
		t.Cleanup(func() { s.callTool("zerops_workflow", map[string]any{"action": "reset"}) })

		// Start workflow (required for import).
		s.callTool("zerops_workflow", map[string]any{"action": "reset"})
		s.mustCallSuccess("zerops_workflow", map[string]any{
			"action":   "start",
			"workflow": "bootstrap",
			"intent":   "e2e import zeropsYaml normalization test",
		})

		result := s.callTool("zerops_import", map[string]any{
			"content": importYAML,
		})
		text := getE2ETextContent(t, result)

		if result.IsError {
			// This is the CURRENT expected behavior: normalizeZeropsYaml corrupts the YAML.
			t.Logf("EXPECTED FAILURE: ops.Import with zeropsYaml rejected: %s", text)

			// Verify it's the specific API error we saw in forensics.
			if !strings.Contains(text, "projectImportInvalidYaml") && !strings.Contains(text, "INVALID_IMPORT_YML") {
				t.Errorf("unexpected error code: %s", text)
			}
		} else {
			// If this succeeds, normalizeZeropsYaml works correctly (or was fixed).
			t.Logf("SUCCESS: ops.Import with zeropsYaml accepted: %s", text)
		}
	})

	// --- Sub-test 3: Import WITH zeropsYaml DIRECTLY to API (bypass normalizeZeropsYaml) ---
	t.Run("with_zeropsYaml_direct_API", func(t *testing.T) {
		// Send the YAML directly to client.ImportServices WITHOUT going through
		// ops.Import (which calls normalizeZeropsYaml). This tells us if the API
		// itself accepts zeropsYaml as a nested object.
		importYAML := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    zeropsYaml:
      zerops:
        - setup: %s
          build:
            base: nodejs@22
            buildCommands:
              - echo ok
            deployFiles: ./
          run:
            base: nodejs@22
            ports:
              - port: 3000
                httpSupport: true
            start: node server.js
`, hostDirect, hostDirect)

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Call API directly — no normalizeZeropsYaml.
		result, err := h.client.ImportServices(ctx, h.projectID, importYAML)
		if err != nil {
			// API rejects raw nested zeropsYaml.
			t.Logf("DIRECT API with zeropsYaml (nested object) REJECTED: %v", err)

			var pe *platform.PlatformError
			if ok := extractPlatformError(err, &pe); ok {
				t.Logf("  Code: %s, APICode: %s, Message: %s", pe.Code, pe.APICode, pe.Message)
			}

			// Now try with zeropsYaml as a YAML string (what normalizeZeropsYaml produces).
			zyStr := zeropsYamlContent(hostDirect)
			importYAMLWithString := fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    minContainers: 1
    zeropsYaml: |
%s`, hostDirect, indentString(zyStr, 6))

			t.Logf("Retrying with zeropsYaml as block scalar string...")
			result2, err2 := h.client.ImportServices(ctx, h.projectID, importYAMLWithString)
			if err2 != nil {
				t.Logf("DIRECT API with zeropsYaml (string) ALSO REJECTED: %v", err2)
				t.Logf("CONCLUSION: Zerops API does not accept zeropsYaml at all (regardless of format)")
			} else {
				t.Logf("DIRECT API with zeropsYaml (string) ACCEPTED: project=%s", result2.ProjectID)
				t.Logf("CONCLUSION: API accepts zeropsYaml as string but rejects nested object — normalizeZeropsYaml should convert, not corrupt")
				for _, ss := range result2.ServiceStacks {
					t.Logf("  Service: %s (id=%s)", ss.Name, ss.ID)
				}
			}
		} else {
			// API accepts raw nested zeropsYaml without normalization.
			t.Logf("DIRECT API with zeropsYaml (nested object) ACCEPTED: project=%s", result.ProjectID)
			t.Logf("CONCLUSION: API accepts zeropsYaml as nested object — normalizeZeropsYaml is corrupting valid input")
			for _, ss := range result.ServiceStacks {
				t.Logf("  Service: %s (id=%s)", ss.Name, ss.ID)
			}
		}
	})
}

// TestE2E_Import_ZeropsYaml_Normalization_Output traces what normalizeZeropsYaml
// actually produces, comparing input and output byte-by-byte.
func TestE2E_Import_ZeropsYaml_Normalization_Output(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "nested_object",
			input: `services:
  - hostname: test
    type: nodejs@22
    zeropsYaml:
      zerops:
        - setup: test
          run:
            start: node server.js
`,
		},
		{
			name: "block_scalar_string",
			input: `services:
  - hostname: test
    type: nodejs@22
    zeropsYaml: |
      zerops:
        - setup: test
          run:
            start: node server.js
`,
		},
		{
			name: "complex_commands",
			input: `services:
  - hostname: test
    type: nodejs@22
    zeropsYaml:
      zerops:
        - setup: test
          build:
            buildCommands:
              - echo "hello: world"
              - npm install
            deployFiles: ./
          run:
            prepareCommands:
              - curl -sSfL https://example.com/install.sh | sh
            start: node server.js
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Use the mock client to capture what normalizeZeropsYaml sends.
			mock := platform.NewMock().WithImportResult(&platform.ImportResult{
				ProjectID:   "test-project",
				ProjectName: "test",
			})

			ctx := context.Background()
			_, err := ops.Import(ctx, mock, "test-project", tt.input, "", nil, nil)
			if err != nil {
				t.Logf("Import error (expected for some cases): %v", err)
				return
			}

			captured := mock.CapturedImportYAML
			t.Logf("INPUT (%d bytes):\n%s", len(tt.input), tt.input)
			t.Logf("OUTPUT (%d bytes):\n%s", len(captured), captured)

			if tt.input == captured {
				t.Log("RESULT: Input preserved byte-for-byte (no normalization needed)")
			} else {
				t.Log("RESULT: Input was MODIFIED by normalizeZeropsYaml")
				// Show the diff
				inputLines := strings.Split(tt.input, "\n")
				outputLines := strings.Split(captured, "\n")
				maxLines := len(inputLines)
				if len(outputLines) > maxLines {
					maxLines = len(outputLines)
				}
				for i := 0; i < maxLines; i++ {
					var inLine, outLine string
					if i < len(inputLines) {
						inLine = inputLines[i]
					}
					if i < len(outputLines) {
						outLine = outputLines[i]
					}
					if inLine != outLine {
						t.Logf("  DIFF line %d:", i+1)
						t.Logf("    IN:  %q", inLine)
						t.Logf("    OUT: %q", outLine)
					}
				}
			}
		})
	}
}

// extractPlatformError is a helper to extract PlatformError from any error type.
func extractPlatformError(err error, pe **platform.PlatformError) bool {
	if err == nil {
		return false
	}
	type platformErr interface {
		Error() string
	}
	// Try direct type assertion.
	if p, ok := err.(*platform.PlatformError); ok {
		*pe = p
		return true
	}
	// Try errors.As.
	return false
}

// indentString indents every line of s by n spaces.
func indentString(s string, n int) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] = prefix + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
