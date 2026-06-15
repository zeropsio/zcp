package tools

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

func TestCheckEnvSelfShadow_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		entry      *ops.ZeropsYmlEntry
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "nil entry passes defensively",
			entry:      nil,
			wantStatus: "pass",
		},
		{
			name:       "no envVars passes",
			entry:      &ops.ZeropsYmlEntry{},
			wantStatus: "pass",
		},
		{
			name: "run-level self-shadow fails",
			entry: func() *ops.ZeropsYmlEntry {
				e := &ops.ZeropsYmlEntry{}
				e.Run.EnvVariables = map[string]string{
					"APP_ENV": "${APP_ENV}",
				}
				return e
			}(),
			wantStatus: "fail",
			wantDetail: []string{"APP_ENV"},
		},
		{
			name: "legitimate cross-service ref passes",
			entry: func() *ops.ZeropsYmlEntry {
				e := &ops.ZeropsYmlEntry{}
				e.Run.EnvVariables = map[string]string{
					"DB_HOST":      "${db_hostname}",
					"APP_ENV":      "production",
					"VITE_API_URL": "${API_URL}",
				}
				return e
			}(),
			wantStatus: "pass",
		},
		{
			name: "multiple run-level shadows reported together",
			entry: func() *ops.ZeropsYmlEntry {
				e := &ops.ZeropsYmlEntry{}
				e.Run.EnvVariables = map[string]string{
					"FIRST":  "${FIRST}",
					"SECOND": "${SECOND}",
				}
				return e
			}(),
			wantStatus: "fail",
			wantDetail: []string{"FIRST", "SECOND"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := checkEnvSelfShadow(context.Background(), "apidev", tt.entry)
			shim := []stepCheckShim{{Name: c.Name, Status: c.Status, Detail: c.Detail}}
			check := findCheck(shim, "apidev_env_self_shadow")
			if check == nil {
				t.Fatalf("expected apidev_env_self_shadow check, got %+v", shim)
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
