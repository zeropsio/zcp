package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

func TestMergeVersions_Deduplicates(t *testing.T) {
	t.Parallel()

	schemas := &schema.Schemas{
		ZeropsYml: &schema.ZeropsYmlSchema{
			BuildBases: []string{"php@8.4", "nodejs@22", "go@1"},
			RunBases:   []string{"php-nginx@8.4", "nodejs@22", "static"}, // nodejs@22 is a dupe
		},
		ImportYml: &schema.ImportYmlSchema{
			ServiceTypes: []string{"php-nginx@8.4", "postgresql@16", "static"}, // php-nginx@8.4 and static are dupes
		},
	}

	versions := mergeVersions(schemas)

	set := make(map[string]bool, len(versions))
	for _, v := range versions {
		if set[v] {
			t.Errorf("duplicate version %q", v)
		}
		set[v] = true
	}

	want := []string{"php@8.4", "nodejs@22", "go@1", "php-nginx@8.4", "static", "postgresql@16"}
	for _, w := range want {
		if !set[w] {
			t.Errorf("missing expected version %q", w)
		}
	}
}

func TestMergeVersions_NilSchemas(t *testing.T) {
	t.Parallel()

	schemas := &schema.Schemas{}
	versions := mergeVersions(schemas)
	if len(versions) != 0 {
		t.Errorf("expected 0 versions from nil schemas, got %d", len(versions))
	}
}

func TestWriteSnapshot_ValidJSON(t *testing.T) {
	t.Parallel()

	snap := &Snapshot{
		Versions: []string{"go@1", "nodejs@22", "static"},
	}

	outPath := filepath.Join(t.TempDir(), "active_versions.json")
	if err := writeSnapshot(snap, outPath); err != nil {
		t.Fatalf("writeSnapshot: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var fromFile Snapshot
	if err := json.Unmarshal(data, &fromFile); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(fromFile.Versions) != 3 {
		t.Errorf("expected 3 versions, got %d", len(fromFile.Versions))
	}
}

// TestWriteSnapshot_DeterministicBytes pins the rebase-conflict fix:
// two regenerations against identical inputs MUST produce byte-identical
// files. Without this guarantee the file diverges every release and
// rebase conflicts return.
func TestWriteSnapshot_DeterministicBytes(t *testing.T) {
	t.Parallel()

	snap := &Snapshot{
		Versions: []string{"go@1", "nodejs@22", "static"},
	}
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.json")
	pathB := filepath.Join(dir, "b.json")

	if err := writeSnapshot(snap, pathA); err != nil {
		t.Fatalf("writeSnapshot A: %v", err)
	}
	if err := writeSnapshot(snap, pathB); err != nil {
		t.Fatalf("writeSnapshot B: %v", err)
	}

	bytesA, _ := os.ReadFile(pathA)
	bytesB, _ := os.ReadFile(pathB)
	if string(bytesA) != string(bytesB) {
		t.Errorf("snapshot bytes diverge between regenerations:\nA: %s\nB: %s", bytesA, bytesB)
	}
}
