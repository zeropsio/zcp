package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

// stepCheckShim mirrors workflow.StepCheck for test-local assertions that
// don't pull in the full workflow package surface.
type stepCheckShim struct {
	Name   string
	Status string
	Detail string
}

// findCheck returns a pointer to the first check with the given name, or
// nil if absent.
func findCheck(checks []stepCheckShim, name string) *stepCheckShim {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

func TestCheckEnvRefs_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		entry             *ops.ZeropsYmlEntry
		discoveredEnvVars map[string][]string
		liveHostnames     []string
		wantStatus        string
		wantDetail        []string
	}{
		{
			name:       "nil entry returns nil (no surface)",
			entry:      nil,
			wantStatus: "",
		},
		{
			name: "entry with no envVariables returns nil",
			entry: &ops.ZeropsYmlEntry{
				EnvVariables: map[string]string{},
			},
			wantStatus: "",
		},
		{
			name: "all refs resolve passes",
			entry: &ops.ZeropsYmlEntry{
				EnvVariables: map[string]string{
					"DB_HOST":     "${db_hostname}",
					"DB_PASSWORD": "${db_password}",
				},
			},
			// ValidateEnvReferences strips the `<hostname>_` prefix from
			// the raw reference; discoveredEnvVars keys the stripped var
			// name. `${db_hostname}` → lookup discoveredEnvVars["db"] for
			// "hostname". Mirror that here.
			discoveredEnvVars: map[string][]string{
				"db": {"hostname", "password"},
			},
			liveHostnames: []string{"db", "apidev"},
			wantStatus:    "pass",
		},
		{
			name: "unresolved hostname fails",
			entry: &ops.ZeropsYmlEntry{
				EnvVariables: map[string]string{
					"DB_HOST": "${missing_hostname}",
				},
			},
			discoveredEnvVars: map[string][]string{},
			liveHostnames:     []string{"apidev"},
			wantStatus:        "fail",
			wantDetail:        []string{"missing_hostname"},
		},
		{
			name: "unresolved env var fails",
			entry: &ops.ZeropsYmlEntry{
				EnvVariables: map[string]string{
					"DB_HOST": "${db_nonexistent}",
				},
			},
			discoveredEnvVars: map[string][]string{
				"db": {"hostname", "password"},
			},
			liveHostnames: []string{"db"},
			wantStatus:    "fail",
			wantDetail:    []string{"nonexistent"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckEnvRefs(context.Background(), "apidev", tt.entry, tt.discoveredEnvVars, tt.liveHostnames)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			if tt.wantStatus == "" {
				if len(shim) != 0 {
					t.Errorf("expected nil/empty result, got %+v", shim)
				}
				return
			}
			check := findCheck(shim, "apidev_env_refs")
			if check == nil {
				t.Fatalf("expected apidev_env_refs check, got %+v", shim)
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
