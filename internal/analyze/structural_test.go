package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckGhostEnvDirs pins the F-9 surrogate: any directory under
// environments/ that isn't one of the six canonical tier folders counts
// as a ghost. Threshold is zero; v36 observed 6. Seed test establishes
// the table pattern for the rest of the structural bars.
func TestCheckGhostEnvDirs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		dirs         []string
		wantObserved int
		wantStatus   string
		wantFiles    []string
	}{
		{
			name: "all_canonical_pass",
			dirs: []string{
				"0 \u2014 AI Agent",
				"1 \u2014 Remote (CDE)",
				"2 \u2014 Local",
				"3 \u2014 Stage",
				"4 \u2014 Small Production",
				"5 \u2014 Highly-available Production",
			},
			wantObserved: 0,
			wantStatus:   StatusPass,
		},
		{
			name:         "empty_dir_pass",
			dirs:         nil,
			wantObserved: 0,
			wantStatus:   StatusPass,
		},
		{
			name: "v36_ghost_invasion_fail",
			dirs: []string{
				"0 \u2014 AI Agent",
				"1 \u2014 Remote (CDE)",
				"2 \u2014 Local",
				"3 \u2014 Stage",
				"4 \u2014 Small Production",
				"5 \u2014 Highly-available Production",
				"dev-and-stage-hypercde",
				"local-validator",
				"prod-ha",
				"remote-cde-and-stage",
				"small-prod",
				"stage-only",
			},
			wantObserved: 6,
			wantStatus:   StatusFail,
			wantFiles: []string{
				"dev-and-stage-hypercde",
				"local-validator",
				"prod-ha",
				"remote-cde-and-stage",
				"small-prod",
				"stage-only",
			},
		},
		{
			name:         "missing_environments_dir_skip",
			dirs:         []string{"__missing__"},
			wantObserved: 0,
			wantStatus:   StatusSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if len(tt.dirs) != 1 || tt.dirs[0] != "__missing__" {
				envsDir := filepath.Join(dir, "environments")
				if err := os.MkdirAll(envsDir, 0o755); err != nil {
					t.Fatalf("mkdir envs: %v", err)
				}
				for _, d := range tt.dirs {
					if err := os.Mkdir(filepath.Join(envsDir, d), 0o755); err != nil {
						t.Fatalf("mkdir %s: %v", d, err)
					}
				}
			}

			got := CheckGhostEnvDirs(dir)
			if got.Observed != tt.wantObserved {
				t.Errorf("observed=%d want=%d", got.Observed, tt.wantObserved)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status=%s want=%s", got.Status, tt.wantStatus)
			}
			if tt.wantFiles != nil {
				if len(got.EvidenceFiles) != len(tt.wantFiles) {
					t.Errorf("evidence_files len=%d want=%d (%v)", len(got.EvidenceFiles), len(tt.wantFiles), got.EvidenceFiles)
				}
			}
		})
	}
}

// TestCheckPerCodebaseMarkdown covers the F-10 surrogate: per-codebase
// README.md + CLAUDE.md must be present in the deliverable. Measured
// flatly (count found vs expected). Stranded writer output produces
// observed < expected.
func TestCheckPerCodebaseMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		codebases    []string
		files        map[string]string // rel path → body
		wantExpected int
		wantObserved int
		wantStatus   string
	}{
		{
			name:      "all_present_pass",
			codebases: []string{"apidev", "appdev", "workerdev"},
			files: map[string]string{
				"apidev/README.md":    "x",
				"apidev/CLAUDE.md":    "x",
				"appdev/README.md":    "x",
				"appdev/CLAUDE.md":    "x",
				"workerdev/README.md": "x",
				"workerdev/CLAUDE.md": "x",
			},
			wantExpected: 6,
			wantObserved: 6,
			wantStatus:   StatusPass,
		},
		{
			name:         "v36_all_stranded_fail",
			codebases:    []string{"apidev", "appdev", "workerdev"},
			files:        map[string]string{},
			wantExpected: 6,
			wantObserved: 0,
			wantStatus:   StatusFail,
		},
		{
			name:      "partial_stranded_fail",
			codebases: []string{"apidev", "appdev"},
			files: map[string]string{
				"apidev/README.md": "x",
				"appdev/CLAUDE.md": "x",
			},
			wantExpected: 4,
			wantObserved: 2,
			wantStatus:   StatusFail,
		},
		{
			name:         "no_codebases_skip",
			codebases:    nil,
			files:        nil,
			wantExpected: 0,
			wantObserved: 0,
			wantStatus:   StatusSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for rel, body := range tt.files {
				full := filepath.Join(dir, rel)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			got := CheckPerCodebaseMarkdown(dir, tt.codebases)
			if got.Threshold != tt.wantExpected {
				t.Errorf("threshold=%d want=%d", got.Threshold, tt.wantExpected)
			}
			if got.Observed != tt.wantObserved {
				t.Errorf("observed=%d want=%d", got.Observed, tt.wantObserved)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status=%s want=%s", got.Status, tt.wantStatus)
			}
		})
	}
}

// TestCheckMarkerExactForm covers F-12. A README with any ZEROPS_EXTRACT
// marker missing the trailing `#` counts as a failing file. Observed is
// the number of offending files (not the number of offending markers).
func TestCheckMarkerExactForm(t *testing.T) {
	t.Parallel()

	good := "<!-- #ZEROPS_EXTRACT_START:intro# -->\nbody\n<!-- #ZEROPS_EXTRACT_END:intro# -->\n"
	bad := "<!-- #ZEROPS_EXTRACT_START:intro -->\nbody\n<!-- #ZEROPS_EXTRACT_END:intro -->\n"

	tests := []struct {
		name         string
		files        map[string]string
		wantObserved int
		wantStatus   string
	}{
		{
			name: "all_exact_pass",
			files: map[string]string{
				"README.md":        good,
				"apidev/README.md": good,
			},
			wantObserved: 0,
			wantStatus:   StatusPass,
		},
		{
			name: "one_broken_fail",
			files: map[string]string{
				"README.md":        good,
				"apidev/README.md": bad,
			},
			wantObserved: 1,
			wantStatus:   StatusFail,
		},
		{
			name:         "empty_tree_skip",
			files:        map[string]string{},
			wantObserved: 0,
			wantStatus:   StatusSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for rel, body := range tt.files {
				full := filepath.Join(dir, rel)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			got := CheckMarkerExactForm(dir)
			if got.Observed != tt.wantObserved {
				t.Errorf("observed=%d want=%d evidence=%v", got.Observed, tt.wantObserved, got.EvidenceFiles)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status=%s want=%s", got.Status, tt.wantStatus)
			}
		})
	}
}

// TestCheckStandaloneDuplicateFiles covers the F-13 surrogate. Any file
// named INTEGRATION-GUIDE.md or GOTCHAS.md anywhere under the
// deliverable counts. Fragments live inside README.md; standalone
// duplicates are the defect.
func TestCheckStandaloneDuplicateFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		files        []string
		wantObserved int
		wantStatus   string
	}{
		{
			name:         "clean_pass",
			files:        []string{"README.md", "apidev/README.md", "apidev/CLAUDE.md"},
			wantObserved: 0,
			wantStatus:   StatusPass,
		},
		{
			name:         "v13_writer_authored_fail",
			files:        []string{"apidev/INTEGRATION-GUIDE.md", "apidev/GOTCHAS.md", "appdev/INTEGRATION-GUIDE.md"},
			wantObserved: 3,
			wantStatus:   StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, rel := range tt.files {
				full := filepath.Join(dir, rel)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			got := CheckStandaloneDuplicateFiles(dir)
			if got.Observed != tt.wantObserved {
				t.Errorf("observed=%d want=%d evidence=%v", got.Observed, tt.wantObserved, got.EvidenceFiles)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status=%s want=%s", got.Status, tt.wantStatus)
			}
		})
	}
}

// TestCheckAtomTemplateVarsBound covers B-22. Every {{.Field}} reference
// in every atom must name a field the Go render path populates. Fixture
// tree exercises pass/fail paths without touching the live atom corpus.
func TestCheckAtomTemplateVarsBound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		atoms        map[string]string
		allowed      []string
		wantObserved int
		wantStatus   string
	}{
		{
			name: "all_bound_pass",
			atoms: map[string]string{
				"writer/tree.md": "Path: {{.ProjectRoot}}/{h}\nFramework: {{.Framework}}",
			},
			allowed:      []string{"ProjectRoot", "Framework", "Slug", "Tier", "Hostnames", "EnvFolders"},
			wantObserved: 0,
			wantStatus:   StatusPass,
		},
		{
			name: "unbound_field_fail",
			atoms: map[string]string{
				"writer/tree.md": "Path: {{.FakeField}}",
			},
			allowed:      []string{"ProjectRoot"},
			wantObserved: 1,
			wantStatus:   StatusFail,
		},
		{
			name: "index_expr_scanned_pass",
			atoms: map[string]string{
				"writer/tree.md": "Folder: {{index .EnvFolders 0}}",
			},
			allowed:      []string{"EnvFolders"},
			wantObserved: 0,
			wantStatus:   StatusPass,
		},
		{
			name:         "empty_pass",
			atoms:        map[string]string{},
			allowed:      []string{"ProjectRoot"},
			wantObserved: 0,
			wantStatus:   StatusPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for rel, body := range tt.atoms {
				full := filepath.Join(dir, rel)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			allowed := make(map[string]bool)
			for _, f := range tt.allowed {
				allowed[f] = true
			}
			got := CheckAtomTemplateVarsBound(dir, allowed)
			if got.Observed != tt.wantObserved {
				t.Errorf("observed=%d want=%d evidence=%v", got.Observed, tt.wantObserved, got.EvidenceFiles)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status=%s want=%s", got.Status, tt.wantStatus)
			}
		})
	}
}

// TestExtractTemplateRefs isolates the atom-text parser so regex changes
// can be validated independent of the filesystem walk.
func TestExtractTemplateRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{"direct_field", "path: {{.ProjectRoot}}", []string{"ProjectRoot"}},
		{"index_expr", "folder: {{index .EnvFolders 0}}", []string{"EnvFolders"}},
		{"range_expr", "{{range .Hostnames}}x{{end}}", []string{"Hostnames"}},
		{"multiple", "{{.Slug}} {{.Framework}} {{.Slug}}", []string{"Slug", "Framework"}},
		{"no_refs", "plain prose", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractTemplateRefs(tt.body)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}
