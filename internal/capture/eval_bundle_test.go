package capture

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBundleEvalScenario_CopiesKnownArtifactsPrivatelyAndExactly(t *testing.T) {
	t.Parallel()

	sessionDir := t.TempDir()
	sourceDir := t.TempDir()
	scenarioPath := filepath.Join(sourceDir, "weather.md")
	outDir := filepath.Join(sourceDir, "results")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	fixtures := map[string][]byte{
		scenarioPath:                              []byte("---\nid: weather\n---\nDeploy it.\n"),
		filepath.Join(outDir, "task-prompt.txt"):  []byte("Deploy it.\n"),
		filepath.Join(outDir, "transcript.jsonl"): []byte("{\"type\":\"system\"}\n"),
		filepath.Join(outDir, "meta.json"):        []byte("{\"ok\":true}\n"),
	}
	for path, data := range fixtures {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	paths, err := BundleEvalScenario(sessionDir, "suite-1", "weather", scenarioPath, outDir)
	if err != nil {
		t.Fatalf("BundleEvalScenario() error = %v", err)
	}
	want := []string{
		"eval/suite-1/weather/meta.json",
		"eval/suite-1/weather/scenario.md",
		"eval/suite-1/weather/task-prompt.txt",
		"eval/suite-1/weather/transcript.jsonl",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("bundled paths = %v, want %v", paths, want)
	}
	for source, data := range fixtures {
		name := filepath.Base(source)
		if source == scenarioPath {
			name = "scenario.md"
		}
		destination := filepath.Join(sessionDir, "eval", "suite-1", "weather", name)
		got, err := os.ReadFile(destination)
		if err != nil {
			t.Fatalf("read bundled %s: %v", destination, err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("bundled %s changed bytes", destination)
		}
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatalf("stat bundled %s: %v", destination, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("bundled %s mode = %#o, want 0600", destination, info.Mode().Perm())
		}
	}
}

func TestBundleEvalScenario_RejectsEscapingIdentity(t *testing.T) {
	t.Parallel()

	_, err := BundleEvalScenario(t.TempDir(), "../suite", "weather", "", t.TempDir())
	if err == nil {
		t.Fatal("BundleEvalScenario() accepted escaping eval run ID")
	}
}
