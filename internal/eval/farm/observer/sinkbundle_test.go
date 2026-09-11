package observer

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
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

// sinkbundleRequiredMetaJSON, sinkbundleTaskPrompt and
// sinkbundleTranscriptJSONL are the three required inputs (§7.2) for a
// minimal but real run, reused verbatim from testdata/sample-run so this
// fixture stays independent of (and doesn't drift from) the DirBundle
// fixture.
const (
	sinkbundleRequiredMetaJSON = `{
  "scenarioId": "sample-scenario",
  "suiteId": "20260911-104838",
  "mode": "two-shot-resume",
  "startedAt": "2026-09-11T10:48:38.622532575Z",
  "duration": "8m3.598s",
  "sessionId": "sess-1",
  "model": "claude-sonnet-5",
  "task": {
    "mode": "required",
    "result": "failed",
    "frozenAt": "2026-09-11T10:56:08.364056886Z"
  }
}`
	sinkbundleTaskPrompt      = "Diagnose and fix the failing service."
	sinkbundleTranscriptJSONL = `{"type":"system","subtype":"init","cwd":"/home/zerops/work","session_id":"sess-1","model":"claude-sonnet-5"}
{"type":"assistant","message":{"content":[{"type":"text","text":"Looked at it; nothing more to do."}]}}
`
	// sinkbundleCleanModelAnswer has no findings — a clean run's answer is
	// valid per the prompt's own rule ("A clean run has no findings").
	sinkbundleCleanModelAnswer = `{
		"headline": "Agent inspected the service and made no changes.",
		"goal": {"reached": "no", "why": "the service never became healthy"},
		"checks": {"agree": true, "why": ""},
		"findings": [],
		"selfReview": {"accurate": "yes", "note": ""}
	}`
)

// TestSinkBundle_MissingOptionalFileIsNotRecorded pins the sink.go fix
// (farm.ErrObjectNotFound now satisfies errors.Is(err, fs.ErrNotExist)):
// a bucket-backed run missing its three optional files
// (self-review.md, platform-snapshot.json, verification.json) must observe
// exactly like a local-directory bundle missing the same files — status
// "ok", every optional section rendered "(not recorded)" — never
// status "error" from a loader mistaking a 404 for a real failure.
func TestSinkBundle_MissingOptionalFileIsNotRecorded(t *testing.T) {
	const runID = "final3"
	const resultsDir = "results/20260911-104838/sample-scenario"

	fake := newFakeObjectStore()
	prefix := "runs/" + runID + "/" + resultsDir + "/"
	if err := fake.Put(context.Background(), prefix+"meta.json", []byte(sinkbundleRequiredMetaJSON)); err != nil {
		t.Fatal(err)
	}
	if err := fake.Put(context.Background(), prefix+"task-prompt.txt", []byte(sinkbundleTaskPrompt)); err != nil {
		t.Fatal(err)
	}
	if err := fake.Put(context.Background(), prefix+"transcript.jsonl", []byte(sinkbundleTranscriptJSONL)); err != nil {
		t.Fatal(err)
	}
	// self-review.md, platform-snapshot.json and verification.json are
	// deliberately never Put — that absence is what this test is about.

	bundle, err := NewSinkBundle(context.Background(), fake, runID)
	if err != nil {
		t.Fatalf("NewSinkBundle: %v", err)
	}

	// The loaders themselves must treat the 404 as absence, not failure.
	if _, err := LoadVerification(bundle, resultsDir); err != nil {
		t.Errorf("LoadVerification over a bucket bundle with no verification.json: %v, want nil (absence, not an error)", err)
	}
	if selfReview, err := LoadSelfReview(bundle, resultsDir); err != nil || selfReview != "" {
		t.Errorf("LoadSelfReview over a bucket bundle with no self-review.md = (%q, %v), want (\"\", nil)", selfReview, err)
	}
	if snap, err := LoadPlatformSnapshot(bundle, resultsDir); err != nil || snap != nil {
		t.Errorf("LoadPlatformSnapshot over a bucket bundle with no platform-snapshot.json = (%+v, %v), want (nil, nil)", snap, err)
	}

	tmp := t.TempDir()
	claudePath := obsWriteCannedClaude(t, tmp, sinkbundleCleanModelAnswer)
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID:      runID,
		Model:      "claude-sonnet-5",
		ClaudePath: claudePath,
		OAuthToken: "test-token",
		Timeout:    time.Minute,
		Environ:    os.Environ,
	})
	if obs.Status != "ok" {
		t.Fatalf("obs.Status = %q (obs.Error = %q), want %q — a bucket bundle missing only optional files must still observe", obs.Status, obs.Error, "ok")
	}
}
