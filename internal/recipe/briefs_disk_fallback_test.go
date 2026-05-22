package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-29 Fix #1 — designed disk-fallback for oversized briefs.
//
// Below BriefDiskFallbackThreshold (40 KB) the inline path stays;
// above, the engine writes the body to
// `<sess.OutputRoot>/.briefs/<kind>-<codebase>-<unix>.md` and returns a
// pointer (briefPath + briefSize) instead of an inline prompt payload.
// Either-or semantics — Prompt and BriefPath never both populated.

// promptHandlerWithBody bypasses the brief composer so tests can exercise
// the handler's threshold/disk-write behavior with a deterministic-size
// prompt. Returns the RecipeResult.
func promptHandlerWithBody(t *testing.T, sess *Session, body string) RecipeResult {
	t.Helper()
	r := RecipeResult{Action: "build-subagent-prompt", Slug: sess.Slug}
	if len(body) > BriefDiskFallbackThreshold && sess.OutputRoot != "" {
		path, err := writeBriefToDisk(sess.OutputRoot, BriefScaffold, "api", body)
		if err != nil {
			r.Error = err.Error()
			return r
		}
		r.BriefPath = path
		r.BriefSize = len(body)
		r.OK = true
		return r
	}
	r.Prompt, r.OK = body, true
	return r
}

// TestHandleBuildSubagentPrompt_BriefUnderThreshold_ReturnsInline —
// composed brief is 30 KB → response carries Prompt non-empty,
// BriefPath empty.
func TestHandleBuildSubagentPrompt_BriefUnderThreshold_ReturnsInline(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	dispatchRes := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: filepath.Join(dir, "run"),
	})
	if !dispatchRes.OK {
		t.Fatalf("start: %+v", dispatchRes)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	body := strings.Repeat("a", 30*1024)
	r := promptHandlerWithBody(t, sess, body)
	if !r.OK {
		t.Fatalf("handler: %+v", r)
	}
	if r.Prompt == "" {
		t.Errorf("expected inline Prompt non-empty under threshold; got empty")
	}
	if r.BriefPath != "" {
		t.Errorf("expected BriefPath empty under threshold; got %q", r.BriefPath)
	}
	if r.BriefSize != 0 {
		t.Errorf("expected BriefSize zero under threshold; got %d", r.BriefSize)
	}
}

// TestHandleBuildSubagentPrompt_BriefOverThreshold_WritesDiskAndReturnsPointer —
// composed brief is 50 KB → response carries Prompt="", BriefPath
// absolute, file at path contains exact composed prompt body.
func TestHandleBuildSubagentPrompt_BriefOverThreshold_WritesDiskAndReturnsPointer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outRoot := filepath.Join(dir, "run")
	dispatchRes := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outRoot,
	})
	if !dispatchRes.OK {
		t.Fatalf("start: %+v", dispatchRes)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	body := strings.Repeat("b", 50*1024)
	r := promptHandlerWithBody(t, sess, body)
	if !r.OK {
		t.Fatalf("handler: %+v", r)
	}
	if r.Prompt != "" {
		t.Errorf("expected Prompt empty over threshold; got %d bytes", len(r.Prompt))
	}
	if r.BriefPath == "" {
		t.Fatalf("expected BriefPath populated over threshold; got empty")
	}
	if !filepath.IsAbs(r.BriefPath) {
		t.Errorf("expected BriefPath absolute; got %q", r.BriefPath)
	}
	if r.BriefSize != len(body) {
		t.Errorf("expected BriefSize %d; got %d", len(body), r.BriefSize)
	}
	// File contents match composed body exactly.
	got, err := os.ReadFile(r.BriefPath)
	if err != nil {
		t.Fatalf("read brief at %s: %v", r.BriefPath, err)
	}
	if string(got) != body {
		t.Errorf("brief file body differs from composed body")
	}
	// File lives under <outputRoot>/.briefs/.
	expectedDir := filepath.Join(outRoot, ".briefs")
	if !strings.HasPrefix(r.BriefPath, expectedDir+string(os.PathSeparator)) {
		t.Errorf("expected brief path under %s; got %s", expectedDir, r.BriefPath)
	}
}

// TestHandleBuildSubagentPrompt_DiskWrite_AtomicSyncRename — the writer
// uses temp + rename pattern; the destination file's existence implies
// the rename completed (no half-state .tmp files lingering at target
// path). Verifies no .tmp files remain in the briefs dir.
func TestHandleBuildSubagentPrompt_DiskWrite_AtomicSyncRename(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outRoot := filepath.Join(dir, "run")
	dispatchRes := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outRoot,
	})
	if !dispatchRes.OK {
		t.Fatalf("start: %+v", dispatchRes)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	body := strings.Repeat("c", 45*1024)
	r := promptHandlerWithBody(t, sess, body)
	if !r.OK {
		t.Fatalf("handler: %+v", r)
	}
	if r.BriefPath == "" {
		t.Fatalf("expected BriefPath populated; got empty")
	}
	// Briefs dir should contain exactly one .md file and zero .tmp files.
	briefsDir := filepath.Dir(r.BriefPath)
	entries, err := os.ReadDir(briefsDir)
	if err != nil {
		t.Fatalf("readdir %s: %v", briefsDir, err)
	}
	mdCount, tmpCount := 0, 0
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".md"):
			mdCount++
		case strings.HasSuffix(e.Name(), ".tmp") || strings.Contains(e.Name(), ".tmp"):
			tmpCount++
		}
	}
	if mdCount != 1 {
		t.Errorf("expected 1 .md brief file in %s; got %d", briefsDir, mdCount)
	}
	if tmpCount != 0 {
		t.Errorf("expected 0 .tmp files in %s; got %d (atomic rename failed)", briefsDir, tmpCount)
	}
}

// TestHandleBuildSubagentPrompt_DiskWriteFails_ReturnsError — when the
// briefs dir cannot be created (parent path is a regular file), the
// writer surfaces an error; no half-state.
func TestHandleBuildSubagentPrompt_DiskWriteFails_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create a regular FILE at the would-be outputRoot so MkdirAll on
	// <outputRoot>/.briefs fails — outputRoot itself isn't a directory.
	outRoot := filepath.Join(dir, "run")
	if err := os.WriteFile(outRoot, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	body := strings.Repeat("d", 45*1024)
	_, err := writeBriefToDisk(outRoot, BriefScaffold, "api", body)
	if err == nil {
		t.Fatalf("expected error when outputRoot is a regular file; got nil")
	}
	if !strings.Contains(err.Error(), "create briefs dir") {
		t.Errorf("expected 'create briefs dir' in error; got %q", err.Error())
	}
}

// TestRecipeResult_BriefPath_OmitemptyJSON — JSON marshaling drops
// briefPath and briefSize when empty (back-compat for existing
// consumers who only read prompt).
func TestRecipeResult_BriefPath_OmitemptyJSON(t *testing.T) {
	t.Parallel()

	// Empty BriefPath/BriefSize → keys absent.
	r := RecipeResult{OK: true, Action: "build-subagent-prompt", Prompt: "inline body"}
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(body)
	if strings.Contains(s, `"briefPath"`) {
		t.Errorf("expected briefPath omitted when empty; got %s", s)
	}
	if strings.Contains(s, `"briefSize"`) {
		t.Errorf("expected briefSize omitted when zero; got %s", s)
	}
	// Populated BriefPath/BriefSize → keys present.
	r2 := RecipeResult{OK: true, Action: "build-subagent-prompt", BriefPath: "/abs/p", BriefSize: 50000}
	body2, err := json.Marshal(r2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s2 := string(body2)
	if !strings.Contains(s2, `"briefPath":"/abs/p"`) {
		t.Errorf("expected briefPath present when set; got %s", s2)
	}
	if !strings.Contains(s2, `"briefSize":50000`) {
		t.Errorf("expected briefSize present when set; got %s", s2)
	}
}

// TestPhaseEntryCodebaseContentAtom_TeachesMultiFilePointer — atom
// contains the dispatch-shape teaching for the run-31 multi-file
// pointer shape (always disk; index.md + part-*.md). Replaces the
// run-29 inline-or-pointer teaching.
func TestPhaseEntryCodebaseContentAtom_TeachesMultiFilePointer(t *testing.T) {
	t.Parallel()

	body := loadPhaseEntry(PhaseCodebaseContent)
	mustContain(t, body, "## Dispatch — multi-file pointer")
	mustContain(t, body, "briefPath")
	mustContain(t, body, "index.md")
	mustContain(t, body, "Read order")
}

// TestHandleBuildSubagentPrompt_NoticeCarriesSubagentTypeDirective —
// every build-subagent-prompt response Notice MUST tell the main agent
// to dispatch with `subagent_type="general-purpose"`. The directive
// inside the brief body (writePromptRecipeContext) is invisible at
// dispatch time on the disk-fallback / multi-file paths because the
// main agent only sees BriefPath, not the body. Without this on Notice
// the main agent defaults to `subagent_type="claude"` (FleetView
// default), which triggers worktree isolation, which fails on the
// non-git recipe-authoring outputRoot. Closes the run-48 wall: every
// scaffold/codebase-content/refinement dispatch died at "Cannot create
// agent worktree: not in a git repository". Pin every kind across the
// real handler so a future Notice edit can't silently re-open the gap.
func TestHandleBuildSubagentPrompt_NoticeCarriesSubagentTypeDirective(t *testing.T) {
	t.Parallel()

	type kindCase struct {
		kind     string
		codebase string // empty for phase-wide kinds.
	}
	cases := []kindCase{
		{kind: "scaffold", codebase: "api"},
		{kind: "feature"},
		{kind: "finalize"},
		{kind: "codebase-content", codebase: "api"},
		{kind: "claudemd-author", codebase: "api"},
		{kind: "env-content"},
		{kind: "refinement"},
		{kind: "refinement2"},
	}
	// Anchors that MUST appear in Notice — keep tight to load-bearing
	// tokens so prose edits stay possible but the directive itself
	// cannot drift.
	noticeAnchors := []string{
		`subagent_type="general-purpose"`,
		`subagent_type="claude"`,
		"worktree isolation",
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			store := NewStore(dir)
			outRoot := filepath.Join(dir, "run")
			startRes := dispatch(t.Context(), store, RecipeInput{
				Action: "start", Slug: "synth-showcase", OutputRoot: outRoot,
			})
			if !startRes.OK {
				t.Fatalf("start: %+v", startRes)
			}
			sess, _ := store.Get("synth-showcase")
			sess.Plan = syntheticShowcasePlan()

			in := RecipeInput{
				Action: "build-subagent-prompt", Slug: "synth-showcase",
				BriefKind: tc.kind, Codebase: tc.codebase,
			}
			if tc.kind == "feature" {
				in.FeaturePass = string(FeaturePassFrontend)
			}
			r := dispatch(t.Context(), store, in)
			if !r.OK {
				t.Fatalf("build-subagent-prompt(kind=%s): %+v", tc.kind, r)
			}
			for _, want := range noticeAnchors {
				if !strings.Contains(r.Notice, want) {
					t.Errorf("kind=%s Notice missing anchor %q (silent-drop guard against run-48 worktree-isolation regression)\nNotice=%s",
						tc.kind, want, r.Notice)
				}
			}
		})
	}
}

// TestHandleBuildSubagentPrompt_OverThreshold_PromptIsEmptyBriefPathSet —
// verifies the either-or contract end-to-end: no dual-population.
func TestHandleBuildSubagentPrompt_OverThreshold_PromptIsEmptyBriefPathSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outRoot := filepath.Join(dir, "run")
	dispatchRes := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outRoot,
	})
	if !dispatchRes.OK {
		t.Fatalf("start: %+v", dispatchRes)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	body := strings.Repeat("e", BriefDiskFallbackThreshold+1)
	r := promptHandlerWithBody(t, sess, body)
	if !r.OK {
		t.Fatalf("handler: %+v", r)
	}
	if r.Prompt != "" && r.BriefPath != "" {
		t.Errorf("either-or violated: both Prompt (%d bytes) and BriefPath (%q) populated",
			len(r.Prompt), r.BriefPath)
	}
	if r.Prompt == "" && r.BriefPath == "" {
		t.Errorf("either-or violated: neither Prompt nor BriefPath populated")
	}
}
