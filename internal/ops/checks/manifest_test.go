package checks

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
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

// TestCheckManifestHonesty_Table exercises the existing
// (discarded → gotcha) dimension after C-8's expansion to 6 pairs.
// The target row shifts from the aggregate `writer_manifest_honesty`
// to the dimension-specific `writer_manifest_honesty_discarded_as_gotcha`.
// Other dimensions are exercised in TestCheckManifestHonesty_AllDimensions
// below.
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
			check := findCheck(shim, "writer_manifest_honesty_discarded_as_gotcha")
			if check == nil {
				t.Fatalf("expected writer_manifest_honesty_discarded_as_gotcha, got %+v", shim)
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

// TestCheckManifestHonesty_AllDimensionsReturnSixRows pins C-8's
// 6-row emission shape. Every (routed_to × surface) dimension declared
// in honestyDimensions is expected to appear in the output in stable
// order, regardless of pass/fail. A regression that removes or reorders
// a dimension breaks this test; a manifest-honesty gate consumer
// relying on the row count stays sharp.
func TestCheckManifestHonesty_AllDimensionsReturnSixRows(t *testing.T) {
	t.Parallel()
	got := CheckManifestHonesty(context.Background(),
		manifest([]ContentManifestFact{{FactTitle: "A", RoutedTo: "content_gotcha"}}),
		map[string]string{"api": kbReadme([]string{"NATS queue group needed"})})
	want := []string{
		"writer_manifest_honesty_discarded_as_gotcha",
		"writer_manifest_honesty_claude_md_as_gotcha",
		"writer_manifest_honesty_integration_guide_as_gotcha",
		"writer_manifest_honesty_zerops_yaml_comment_as_gotcha",
		"writer_manifest_honesty_env_comment_as_gotcha",
		"writer_manifest_honesty_any_as_intro",
	}
	if len(got) != len(want) {
		t.Fatalf("row count: got %d, want %d (rows: %+v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("row[%d]: got %q, want %q", i, got[i].Name, w)
		}
	}
}

// TestCheckManifestHonesty_PerDimensionLeakage pins each non-discarded
// routing dimension's failure surface: a fact routed to claude_md /
// content_ig / zerops_yaml_comment / content_env_comment that leaks
// into the knowledge-base gotcha fragment fails only its own
// dimension's row (cross-surface duplication). The other 5 rows pass.
func TestCheckManifestHonesty_PerDimensionLeakage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		routedTo    string
		wantFailRow string
	}{
		{"claude_md_leaks_to_gotcha", "claude_md", "writer_manifest_honesty_claude_md_as_gotcha"},
		{"content_ig_leaks_to_gotcha", "content_ig", "writer_manifest_honesty_integration_guide_as_gotcha"},
		{"zerops_yaml_comment_leaks_to_gotcha", "zerops_yaml_comment", "writer_manifest_honesty_zerops_yaml_comment_as_gotcha"},
		{"content_env_comment_leaks_to_gotcha", "content_env_comment", "writer_manifest_honesty_env_comment_as_gotcha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckManifestHonesty(context.Background(),
				manifest([]ContentManifestFact{{FactTitle: "NATS queue group needed", RoutedTo: tt.routedTo}}),
				map[string]string{"api": kbReadme([]string{"NATS queue group needed"})})
			for _, row := range got {
				if row.Name == tt.wantFailRow {
					if row.Status != "fail" {
						t.Errorf("%s: expected fail, got %q (%s)", row.Name, row.Status, row.Detail)
					}
					continue
				}
				if row.Status != "pass" {
					t.Errorf("%s: expected pass, got %q (%s)", row.Name, row.Status, row.Detail)
				}
			}
		})
	}
}

// TestCheckManifestHonesty_AnyAsIntroDimension pins the intro-leakage
// dimension. A fact routed to any non-intro non-empty class that
// appears near-verbatim in the intro fragment fails the any_as_intro
// row. Intro-routed facts leaking into intro (correct routing) do NOT
// fail.
func TestCheckManifestHonesty_AnyAsIntroDimension(t *testing.T) {
	t.Parallel()

	readmeWithIntro := "# apidev\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:intro# -->\n" +
		"This recipe ships a NATS queue group required to avoid double-processing every scheduled job.\n" +
		"<!-- #ZEROPS_EXTRACT_END:intro# -->\n"

	fail := CheckManifestHonesty(context.Background(),
		manifest([]ContentManifestFact{{
			FactTitle: "NATS queue group required to avoid double-processing every scheduled job",
			RoutedTo:  "claude_md",
		}}),
		map[string]string{"api": readmeWithIntro})
	introRow := findCheck(toShim(fail), "writer_manifest_honesty_any_as_intro")
	if introRow == nil || introRow.Status != "fail" {
		t.Fatalf("expected fail on non-intro fact leaking to intro: %+v", introRow)
	}

	// Intro-routed fact in intro passes (correct routing).
	pass := CheckManifestHonesty(context.Background(),
		manifest([]ContentManifestFact{{
			FactTitle: "NATS queue group required to avoid double-processing every scheduled job",
			RoutedTo:  "content_intro",
		}}),
		map[string]string{"api": readmeWithIntro})
	introRow = findCheck(toShim(pass), "writer_manifest_honesty_any_as_intro")
	if introRow == nil || introRow.Status != "pass" {
		t.Fatalf("expected pass on intro-routed fact in intro: %+v", introRow)
	}
}

// toShim is a tiny test helper that converts the workflow.StepCheck
// slice returned by the predicate into the local stepCheckShim form
// findCheck expects. Kept here so the test body reads as intent, not
// glue.
func toShim(rows []workflow.StepCheck) []stepCheckShim {
	out := make([]stepCheckShim, 0, len(rows))
	for _, r := range rows {
		out = append(out, stepCheckShim{Name: r.Name, Status: r.Status, Detail: r.Detail})
	}
	return out
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
