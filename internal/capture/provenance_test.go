package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordCompositionFromEnvironment_WritesExactOutputAndComponentSpans(t *testing.T) {
	// non-parallel: capture opt-in is process environment.
	sessionDir := t.TempDir()
	t.Setenv(EnvSessionID, "capture-provenance")
	t.Setenv(EnvSessionDir, sessionDir)
	output := "static atom\n\n---\n\ndynamic yaml"
	components := []CompositionComponent{
		{Kind: "atom", Owner: "bootstrap-mode-prompt", Start: 0, End: len("static atom")},
		{Kind: "dynamic", Owner: "workflow.formatRecipeImportYAMLForGuide", Start: len("static atom\n\n---\n\n"), End: len(output)},
	}
	recorded, err := RecordCompositionFromEnvironment(CompositionRecord{Surface: "bootstrap.guide", Output: output, Components: components})
	if err != nil {
		t.Fatalf("RecordCompositionFromEnvironment() error = %v", err)
	}
	if !recorded {
		t.Fatal("RecordCompositionFromEnvironment() recorded = false")
	}
	paths, err := filepath.Glob(filepath.Join(sessionDir, "provenance", "zcp-*.jsonl"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("provenance files = %v, %v", paths, err)
	}
	records, err := ReadCompositionRecords(paths[0])
	if err != nil {
		t.Fatalf("ReadCompositionRecords() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	record := records[0]
	sum := sha256.Sum256([]byte(output))
	if record.OutputSHA256 != hex.EncodeToString(sum[:]) || record.OutputBytes != len(output) || len(record.Components) != 2 {
		t.Fatalf("composition record = %+v", record)
	}
	for index, component := range record.Components {
		componentSum := sha256.Sum256([]byte(output[component.Start:component.End]))
		if component.SHA256 != hex.EncodeToString(componentSum[:]) {
			t.Errorf("component[%d] hash = %s", index, component.SHA256)
		}
	}
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatalf("stat provenance: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("provenance mode = %#o, want 0600", info.Mode().Perm())
	}
}

func TestRecordCompositionFromEnvironment_DisabledWithoutCompleteOptIn(t *testing.T) {
	t.Setenv(EnvSessionID, "")
	t.Setenv(EnvSessionDir, "")
	recorded, err := RecordCompositionFromEnvironment(CompositionRecord{Surface: "test", Output: "unchanged"})
	if err != nil || recorded {
		t.Fatalf("RecordCompositionFromEnvironment() = (%v, %v), want disabled", recorded, err)
	}
}
