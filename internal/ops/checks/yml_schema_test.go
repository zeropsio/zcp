package checks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// testValidFields returns a minimal ValidFields allowing only the fields
// we need for these tests. Real runs derive from the live JSON schema.
func testValidFields() *schema.ValidFields {
	return &schema.ValidFields{
		Setup:  map[string]bool{"setup": true, "build": true, "run": true, "deploy": true},
		Build:  map[string]bool{"base": true, "buildCommands": true, "deployFiles": true},
		Deploy: map[string]bool{"readiness": true},
		Run:    map[string]bool{"base": true, "start": true, "ports": true},
	}
}

func writeYML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestCheckZeropsYmlFields_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		yaml       string
		vf         *schema.ValidFields
		setupDir   bool
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "nil validFields returns nil",
			vf:         nil,
			setupDir:   true,
			yaml:       "zerops:\n  - setup: dev\n",
			wantStatus: "",
		},
		{
			name:       "missing file returns nil",
			vf:         testValidFields(),
			setupDir:   false,
			wantStatus: "",
		},
		{
			name:       "valid fields passes",
			vf:         testValidFields(),
			setupDir:   true,
			yaml:       "zerops:\n  - setup: dev\n    build:\n      base: nodejs@22\n    run:\n      base: nodejs@22\n      start: node app.js\n",
			wantStatus: "pass",
		},
		{
			name:       "unknown top-level field fails",
			vf:         testValidFields(),
			setupDir:   true,
			yaml:       "zerops:\n  - setup: dev\n    verticalAutoscaling:\n      min: 1\n",
			wantStatus: "fail",
			wantDetail: []string{"verticalAutoscaling"},
		},
		{
			name:       "unknown run field fails",
			vf:         testValidFields(),
			setupDir:   true,
			yaml:       "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n      envVariableS: []\n",
			wantStatus: "fail",
			wantDetail: []string{"envVariableS"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var dir string
			if tt.setupDir {
				dir = writeYML(t, tt.yaml)
			} else {
				dir = t.TempDir() // empty dir; no zerops.yaml
			}
			got := CheckZeropsYmlFields(context.Background(), dir, tt.vf)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			if tt.wantStatus == "" {
				if len(shim) != 0 {
					t.Errorf("expected nil, got %+v", shim)
				}
				return
			}
			check := findCheck(shim, "zerops_yml_schema_fields")
			if check == nil {
				t.Fatalf("expected zerops_yml_schema_fields, got %+v", shim)
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
