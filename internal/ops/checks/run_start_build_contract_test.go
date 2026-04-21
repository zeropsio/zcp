package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

func TestCheckRunStartBuildContract_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		start      string
		wantStatus string // "" means no check emitted
		wantDetail []string
	}{
		{name: "empty start emits nothing", start: "", wantStatus: ""},
		{name: "legitimate node launcher passes silently", start: "node dist/main.js", wantStatus: ""},
		{name: "shell wrapper around build + start passes silently", start: "bash -c 'npm run build && node dist/main.js'", wantStatus: ""},
		{name: "php-fpm start passes silently", start: "php-fpm", wantStatus: ""},
		{name: "npm install prefix fails", start: "npm install && node dist/main.js", wantStatus: "fail", wantDetail: []string{"npm install"}},
		{name: "pip install prefix fails", start: "pip install -r req.txt && python app.py", wantStatus: "fail", wantDetail: []string{"pip install"}},
		{name: "case-insensitive match", start: "NPM INSTALL", wantStatus: "fail"},
		{name: "go build prefix fails", start: "go build -o bin/app && ./bin/app", wantStatus: "fail", wantDetail: []string{"go build"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := &ops.ZeropsYmlEntry{}
			entry.Run.Start = tt.start
			got := CheckRunStartBuildContract(context.Background(), "apidev", entry)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			if tt.wantStatus == "" {
				if len(shim) != 0 {
					t.Errorf("expected no check emitted, got %+v", shim)
				}
				return
			}
			check := findCheck(shim, "apidev_run_start_build_cmd")
			if check == nil {
				t.Fatalf("expected apidev_run_start_build_cmd, got %+v", shim)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q", check.Status, tt.wantStatus)
			}
			for _, w := range tt.wantDetail {
				if !strings.Contains(check.Detail, w) {
					t.Errorf("detail missing %q; full: %s", w, check.Detail)
				}
			}
		})
	}
}

// TestCheckRunStartBuildContract_NilEntry: defensive nil handling.
func TestCheckRunStartBuildContract_NilEntry(t *testing.T) {
	t.Parallel()
	got := CheckRunStartBuildContract(context.Background(), "apidev", nil)
	if len(got) != 0 {
		t.Errorf("expected no checks on nil entry, got %+v", got)
	}
}
