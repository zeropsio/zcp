//go:build unix

package ops

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// callRunner records the call its Acquire was asked to hold the browser for.
type callRunner struct {
	held BrowserCall
}

func (*callRunner) LookPath() (string, error) { return "/fake/agent-browser", nil }

func (c *callRunner) Acquire(ctx context.Context, _ string) (func(bool) (string, error), bool, string, error) {
	c.held = browserCallOf(ctx)
	return func(bool) (string, error) { return "", nil }, false, "", nil
}

func (*callRunner) Run(context.Context, string, time.Duration) (string, string, bool, error) {
	return `[]`, "", false, nil
}

// dirNames lists dir's entries by name.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A call whose client sent its tool-call id names itself as the browser's
// holder for the Mate: the id verbatim, the client, since when and its own
// tab, private to its user, with nothing left beside it.
// witness: internal/ops/browser_call.go | holder := browserCallHolder{CallID: call.ID, Client: call.Client, Since: since.UnixMilli(), TargetID: target} → holder := browserCallHolder{CallID: call.Client, Client: call.Client, Since: since.UnixMilli(), TargetID: target}
func TestPublishBrowserCall_ACallWithAnID_NamesItsIDClientAndTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "default.call")
	since := time.UnixMilli(1763650000123)
	call := BrowserCall{ID: "toolu_019MULyG55DPdWbaXBcYaPxi", Client: "claude-code"}

	if _, err := publishBrowserCall(path, call, "16EF8560F05C773909A6D37A1C0402C1", since); err != nil {
		t.Fatalf("publish: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := `{"callId":"toolu_019MULyG55DPdWbaXBcYaPxi","client":"claude-code","since":1763650000123,"targetId":"16EF8560F05C773909A6D37A1C0402C1"}`
	if string(data) != want {
		t.Errorf("the file holds %s, want %s", data, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the file must be its user's alone: %v %v", info.Mode(), err)
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Errorf("the directory must hold the one file, holds %v", names)
	}
}

// A call whose client sent no id publishes nothing, and the file a call left
// when zcp died in its turn is gone: no file means nobody the Mate can name
// holds the browser, never a guess.
// witness: internal/ops/browser_call.go | if call.ID == "" { → if false {
func TestPublishBrowserCall_ACallWithoutAnID_PublishesNoneAndClearsAStaleOne(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "default.call")
	if err := os.WriteFile(path, []byte(`{"callId":"toolu_dead","client":"claude-code","since":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := publishBrowserCall(path, BrowserCall{Client: "codex"}, "T1", time.Now()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("no file may name a call while one without an id holds the browser: %v", err)
	}
}

// The call's release withdraws its file, and withdrawing after a stop
// already removed the runtime directory with it is no failure.
// witness: internal/ops/browser_call.go | withdraw := func() { withdrawBrowserCall(path) } → withdraw := func() {}
func TestPublishBrowserCall_Withdraw_RemovesTheFile(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "zcp-browser")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "default.call")
	withdraw, err := publishBrowserCall(path, BrowserCall{ID: "toolu_01", Client: "claude-code"}, "T1", time.Now())
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	withdraw()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the released call's file must be gone: %v", err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	withdraw()
}

// The file is replaced in one rename: a link at its path is replaced, never
// written through, and nothing is left beside it, so a reader sees one whole
// file, the last call's or this one's.
// witness: internal/ops/browser_call.go | if err := os.Rename(tmp.Name(), path); err != nil { → if err := os.WriteFile(path, data, 0o600); err != nil {
func TestPublishBrowserCall_ReplacesThePathInOneRename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "default.call")
	elsewhere := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(elsewhere, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, path); err != nil {
		t.Fatal(err)
	}

	if _, err := publishBrowserCall(path, BrowserCall{ID: "toolu_02", Client: "claude-code"}, "T2", time.UnixMilli(5)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if data, _ := os.ReadFile(elsewhere); string(data) != "kept" {
		t.Errorf("the publish wrote through the link: %q", data)
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		t.Errorf("the path must be the call's own file: %v %v", info, err)
	}
	if names := dirNames(t, dir); len(names) != 2 {
		t.Errorf("the publish left a file beside its own: %v", names)
	}
}

// A batch hands the agent's call to the browser's turn, which publishes it
// as the holder.
// witness: internal/ops/browser.go | browserRun.Acquire(withBrowserCall(ctx, input.Call), isolation) → browserRun.Acquire(ctx, isolation)
func TestBrowserBatch_HandsTheCallToTheBrowsersTurn(t *testing.T) {
	// Not parallel: overrides the package's browser runner and scale.
	runner := &callRunner{}
	defer OverrideBrowserRunnerForTest(runner)()
	defer OverrideBrowserScaleForTest(func(context.Context) error { return nil })()
	call := BrowserCall{ID: "toolu_03", Client: "claude-code"}

	if _, err := BrowserBatch(t.Context(), BrowserBatchInput{URL: "https://appdev.example.invalid", Call: call}); err != nil {
		t.Fatalf("BrowserBatch: %v", err)
	}
	if runner.held != call {
		t.Errorf("the browser's turn holds %+v, want %+v", runner.held, call)
	}
}
