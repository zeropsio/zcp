package observer

import (
	"context"
	"strings"
	"testing"
)

// TestSinkBundle_ListsAndReadsRunFiles pins §1.1's runs/<runId>/ bucket
// layout read through S1's Bundle contract: ResultsDir/CaptureScenarioMD
// (bundle.go, unchanged by this slice) must find the single
// results/<suite>/<scenario>/ directory and the optional capture
// scenario.md through a SinkBundle exactly as they do through a DirBundle.
// It also pins outcome 3: a run id outside the FM-47 grammar is refused
// before any key is built (no store call happens for that case).
func TestSinkBundle_ListsAndReadsRunFiles(t *testing.T) {
	t.Parallel()

	const runID = "final1"
	fake := newFakeObjectStore()
	metaPath := "runs/" + runID + "/results/final1/recover-failed-buildfromgit-missing-dep/meta.json"
	taskPromptPath := "runs/" + runID + "/results/final1/recover-failed-buildfromgit-missing-dep/task-prompt.txt"
	scenarioMDPath := "runs/" + runID + "/capture/20260911-100000/eval/final1/recover-failed-buildfromgit-missing-dep/scenario.md"
	_ = fake.Put(context.Background(), metaPath, []byte(`{"scenarioId":"recover-failed-buildfromgit-missing-dep"}`))
	_ = fake.Put(context.Background(), taskPromptPath, []byte("do the thing"))
	_ = fake.Put(context.Background(), scenarioMDPath, []byte("# scenario"))

	bundle, err := NewSinkBundle(context.Background(), fake, runID)
	if err != nil {
		t.Fatalf("NewSinkBundle: %v", err)
	}

	resultsDir, err := ResultsDir(bundle)
	if err != nil {
		t.Fatalf("ResultsDir: %v", err)
	}
	wantResultsDir := "results/final1/recover-failed-buildfromgit-missing-dep"
	if resultsDir != wantResultsDir {
		t.Errorf("ResultsDir = %q, want %q", resultsDir, wantResultsDir)
	}

	taskPrompt, err := LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		t.Fatalf("LoadTaskPrompt: %v", err)
	}
	if taskPrompt != "do the thing" {
		t.Errorf("LoadTaskPrompt = %q, want %q", taskPrompt, "do the thing")
	}

	suiteScenario := strings.TrimPrefix(resultsDir, "results/")
	scenarioMDRel, err := CaptureScenarioMD(bundle, suiteScenario)
	if err != nil {
		t.Fatalf("CaptureScenarioMD: %v", err)
	}
	wantScenarioMDRel := "capture/20260911-100000/eval/final1/recover-failed-buildfromgit-missing-dep/scenario.md"
	if scenarioMDRel != wantScenarioMDRel {
		t.Errorf("CaptureScenarioMD = %q, want %q", scenarioMDRel, wantScenarioMDRel)
	}
	scenarioMD, err := bundle.ReadFile(scenarioMDRel)
	if err != nil {
		t.Fatalf("ReadFile(scenarioMD): %v", err)
	}
	if string(scenarioMD) != "# scenario" {
		t.Errorf("scenario.md content = %q, want %q", scenarioMD, "# scenario")
	}

	t.Run("invalid run id refused before any key is built", func(t *testing.T) {
		t.Parallel()
		recording := &callRecordingStore{}
		if _, err := NewSinkBundle(context.Background(), recording, "Not_Valid"); err == nil {
			t.Fatal("NewSinkBundle with an invalid run id = nil error, want an error")
		}
		if recording.calls != 0 {
			t.Errorf("NewSinkBundle made %d store call(s) for an invalid run id, want 0 (refused before any network call)", recording.calls)
		}
	})
}

// callRecordingStore is an ObjectStore that counts every call — used to
// prove a rejection happens before any network call, not merely that its
// eventual result carries no data.
type callRecordingStore struct{ calls int }

func (c *callRecordingStore) Get(context.Context, string) ([]byte, error) {
	c.calls++
	return nil, nil
}

func (c *callRecordingStore) Put(context.Context, string, []byte) error {
	c.calls++
	return nil
}

func (c *callRecordingStore) Head(context.Context, string) (bool, int64, error) {
	c.calls++
	return false, 0, nil
}

func (c *callRecordingStore) List(context.Context, string) ([]string, error) {
	c.calls++
	return nil, nil
}
