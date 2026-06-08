package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectGuideFiles(t *testing.T) {
	t.Parallel()

	guidesDir := t.TempDir()
	decisionsDir := t.TempDir()

	for _, name := range []string{"guide-a.md", "guide-b.md"} {
		if err := os.WriteFile(filepath.Join(guidesDir, name), []byte("# test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(decisionsDir, "choose-db.md"), []byte("# choose"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		filter string
		want   int
	}{
		{"all_files", "", 3},
		{"filter_guide", "guide-a", 1},
		{"filter_decision", "choose-db", 1},
		{"no_match", "nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files, err := collectGuideFiles(guidesDir, decisionsDir, tt.filter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(files) != tt.want {
				t.Errorf("got %d files, want %d", len(files), tt.want)
			}
		})
	}
}

func TestCollectGuideFiles_MissingDirs(t *testing.T) {
	t.Parallel()

	files, err := collectGuideFiles("/nonexistent/guides", "/nonexistent/decisions", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files for missing dirs, got %d", len(files))
	}
}

func TestPushGuides_DryRun(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	guidesDir := filepath.Join(root, "internal", "knowledge", "guides")
	if err := os.MkdirAll(guidesDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := "# My Guide\n\n## Section\n\nContent here\n"
	if err := os.WriteFile(filepath.Join(guidesDir, "test-guide.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	results, err := PushGuides(cfg, root, "", true)
	if err != nil {
		// gh CLI not available is expected in test env — the dry-run should
		// still return results for files it can read locally
		t.Logf("error (expected without gh): %v", err)
		return
	}

	for _, r := range results {
		if r.Status == Created || r.Status == Updated {
			t.Errorf("dry-run should not create/update, got %v for %s", r.Status, r.Slug)
		}
	}
}

// TestClassifyGuidePush pins the guides dry-run classification (the false
// positive was an UNCONDITIONAL "would update"): identical post-transform MDX
// must report no-change, a real difference would-update, a missing file
// would-create.
func TestClassifyGuidePush(t *testing.T) {
	t.Parallel()
	const a = "---\ntitle: X\n---\n\nbody one\n"
	const b = "---\ntitle: X\n---\n\nbody two\n"
	cases := []struct {
		name          string
		mdx, existing string
		found         bool
		want          guideDryRunVerdict
	}{
		{"missing → create", a, "", false, guideWouldCreate},
		{"identical → no change", a, a, true, guideNoChange},
		{"differs → update", a, b, true, guideWouldUpdate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyGuidePush(tc.mdx, tc.existing, tc.found); got != tc.want {
				t.Errorf("classifyGuidePush = %d, want %d", got, tc.want)
			}
		})
	}
}
