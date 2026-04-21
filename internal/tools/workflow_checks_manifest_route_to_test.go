package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runManifestRouteToCheck invokes checkManifestRouteToPopulated and converts
// its output to the test-local shim type.
func runManifestRouteToCheck(t *testing.T, manifestPath string) []workflowStepCheckShim {
	t.Helper()
	checks := checkManifestRouteToPopulated(manifestPath)
	out := make([]workflowStepCheckShim, 0, len(checks))
	for _, c := range checks {
		out = append(out, workflowStepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
	}
	return out
}

// writeManifestAtPath writes raw body to a new tempdir's
// ZCP_CONTENT_MANIFEST.json and returns the full path. Distinct from the
// shared writeManifest helper (which takes a pre-existing projectRoot) so
// the table-driven cases here control the dir lifetime per sub-test.
func writeManifestAtPath(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ZCP_CONTENT_MANIFEST.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func TestManifestRouteToPopulated_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		manifest   string
		wantStatus string
		wantDetail []string
	}{
		{
			name: "all facts routed passes",
			manifest: `{
  "version": 1,
  "facts": [
    {"fact_title":"DB pass env var","classification":"platform-invariant","routed_to":"content_gotcha","override_reason":""},
    {"fact_title":"NATS queue group","classification":"platform-invariant","routed_to":"claude_md","override_reason":""}
  ]
}`,
			wantStatus: "pass",
		},
		{
			name: "empty routed_to fails (v34 class)",
			manifest: `{
  "version": 1,
  "facts": [
    {"fact_title":"DB pass env var","classification":"platform-invariant","routed_to":"","override_reason":""}
  ]
}`,
			wantStatus: "fail",
			wantDetail: []string{"DB pass env var"},
		},
		{
			name: "missing routed_to field fails",
			manifest: `{
  "version": 1,
  "facts": [
    {"fact_title":"NATS creds","classification":"platform-invariant"}
  ]
}`,
			wantStatus: "fail",
			wantDetail: []string{"NATS creds"},
		},
		{
			name: "unknown enum value fails",
			manifest: `{
  "version": 1,
  "facts": [
    {"fact_title":"Storage bucket","classification":"platform-invariant","routed_to":"published"}
  ]
}`,
			wantStatus: "fail",
			wantDetail: []string{"Storage bucket", "published"},
		},
		{
			name: "mix of populated + empty reports only offenders",
			manifest: `{
  "version": 1,
  "facts": [
    {"fact_title":"ok","classification":"platform-invariant","routed_to":"content_gotcha"},
    {"fact_title":"bad","classification":"platform-invariant","routed_to":""}
  ]
}`,
			wantStatus: "fail",
			wantDetail: []string{"bad"},
		},
		{
			name:       "empty facts array passes (vacuous)",
			manifest:   `{"version":1,"facts":[]}`,
			wantStatus: "pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeManifestAtPath(t, tt.manifest)
			got := runManifestRouteToCheck(t, path)
			check := findCheckByName(got, "manifest_route_to_populated")
			if check == nil {
				t.Fatalf("expected manifest_route_to_populated check, got %+v", got)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
			for _, w := range tt.wantDetail {
				if !strings.Contains(check.Detail, w) {
					t.Errorf("detail missing %q; full: %s", w, check.Detail)
				}
			}
		})
	}
}

// TestManifestRouteToPopulated_MissingManifest: a manifest absent from disk
// is the upstream concern of writer_content_manifest_exists in C-5's
// content-manifest check. Pass here so we don't double-report the missing
// file on the same deploy-step failure surface.
func TestManifestRouteToPopulated_MissingManifest(t *testing.T) {
	t.Parallel()
	got := runManifestRouteToCheck(t, "/definitely/not/here/ZCP_CONTENT_MANIFEST.json")
	check := findCheckByName(got, "manifest_route_to_populated")
	if check == nil {
		t.Fatal("expected check emitted even on missing manifest")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass (missing manifest is upstream concern), got %q", check.Status)
	}
}

// TestManifestRouteToPopulated_InvalidJSON: unreadable JSON is also
// upstream concern; pass without piling onto the same failure surface.
func TestManifestRouteToPopulated_InvalidJSON(t *testing.T) {
	t.Parallel()
	path := writeManifestAtPath(t, "{not-json")
	got := runManifestRouteToCheck(t, path)
	check := findCheckByName(got, "manifest_route_to_populated")
	if check == nil {
		t.Fatal("expected check emitted even on invalid JSON")
	}
	if check.Status != "pass" {
		t.Errorf("expected pass on invalid JSON (upstream concern), got %q", check.Status)
	}
}
