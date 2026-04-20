package checks

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

func manifest(entries []ContentManifestFact) *ContentManifest {
	return &ContentManifest{Version: 1, Facts: entries}
}

func kbReadme(stems []string) string {
	var sb strings.Builder
	sb.WriteString("# X\n\n<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n## Gotchas\n")
	for _, s := range stems {
		sb.WriteString("- **")
		sb.WriteString(s)
		sb.WriteString("** — body text\n")
	}
	sb.WriteString("<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	return sb.String()
}

func TestCheckManifestHonesty_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		m          *ContentManifest
		readmes    map[string]string
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "nil manifest returns nil",
			m:          nil,
			wantStatus: "",
		},
		{
			name:       "no discarded facts passes",
			m:          manifest([]ContentManifestFact{{FactTitle: "A", RoutedTo: "content_gotcha"}}),
			readmes:    map[string]string{"api": kbReadme([]string{"A", "B"})},
			wantStatus: "pass",
		},
		{
			name:       "discarded fact not in readme passes",
			m:          manifest([]ContentManifestFact{{FactTitle: "Secret Internal Fact", RoutedTo: "discarded"}}),
			readmes:    map[string]string{"api": kbReadme([]string{"NATS queue group needed"})},
			wantStatus: "pass",
		},
		{
			name:       "discarded fact leaks into readme fails",
			m:          manifest([]ContentManifestFact{{FactTitle: "NATS queue group needed", RoutedTo: "discarded"}}),
			readmes:    map[string]string{"api": kbReadme([]string{"NATS queue group needed"})},
			wantStatus: "fail",
			wantDetail: []string{"NATS queue group needed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckManifestHonesty(context.Background(), tt.m, tt.readmes)
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
			check := findCheck(shim, "writer_manifest_honesty")
			if check == nil {
				t.Fatalf("expected writer_manifest_honesty, got %+v", shim)
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

func writeFactsLog(t *testing.T, path string, titles []string) {
	t.Helper()
	for _, tt := range titles {
		if err := ops.AppendFact(path, ops.FactRecord{
			Type:  ops.FactTypeGotchaCandidate,
			Title: tt,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
}

func TestCheckManifestCompleteness_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		m            *ContentManifest
		logTitles    []string
		useFactsPath bool
		wantStatus   string
		wantDetail   []string
	}{
		{
			name:         "nil manifest returns nil",
			m:            nil,
			useFactsPath: true,
			wantStatus:   "",
		},
		{
			name:         "empty factsLogPath passes (skip)",
			m:            manifest(nil),
			useFactsPath: false,
			wantStatus:   "pass",
			wantDetail:   []string{"skipped"},
		},
		{
			name:         "all logged facts in manifest passes",
			m:            manifest([]ContentManifestFact{{FactTitle: "A"}, {FactTitle: "B"}}),
			logTitles:    []string{"A", "B"},
			useFactsPath: true,
			wantStatus:   "pass",
		},
		{
			name:         "log contains titles missing from manifest fails",
			m:            manifest([]ContentManifestFact{{FactTitle: "A"}}),
			logTitles:    []string{"A", "B", "C"},
			useFactsPath: true,
			wantStatus:   "fail",
			wantDetail:   []string{"2 distinct"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := ""
			if tt.useFactsPath {
				path = filepath.Join(t.TempDir(), "facts.jsonl")
				if len(tt.logTitles) > 0 {
					writeFactsLog(t, path, tt.logTitles)
				}
			}
			got := CheckManifestCompleteness(context.Background(), tt.m, path)
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
			check := findCheck(shim, "writer_manifest_completeness")
			if check == nil {
				t.Fatalf("expected writer_manifest_completeness, got %+v", shim)
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

func TestLoadContentManifest(t *testing.T) {
	t.Parallel()
	// Missing file surfaces as an error, not a pass — callers decide graceful.
	if _, err := LoadContentManifest(t.TempDir()); err == nil {
		t.Error("expected error on missing manifest")
	}
}
