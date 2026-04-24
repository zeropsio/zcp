package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-9-readiness Workstream I tests — source-code comment scanner.

func writeSourceFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestValidateSourceComments_FlagsPreShipContractReference(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/main.ts", `export function boot() {
  // pre-ship contract item 5 — remove before prod
  return 42;
}
`)
	vs, err := scanSourceCommentsAt(dir)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "source-comment-authoring-voice-leak") {
		t.Errorf("expected source-comment-authoring-voice-leak, got %+v", vs)
	}
}

func TestValidateSourceComments_FlagsScaffoldReference(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/ping.ts", "// scaffold smoke test — kept for now\n")
	vs, err := scanSourceCommentsAt(dir)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "source-comment-authoring-voice-leak") {
		t.Errorf("expected violation for 'scaffold smoke test'")
	}
}

func TestValidateSourceComments_FlagsShowcaseDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/storage.ts", "/*\n * Bucket policy is private (showcase default).\n */\n")
	vs, err := scanSourceCommentsAt(dir)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "source-comment-authoring-voice-leak") {
		t.Errorf("expected violation for 'showcase default'")
	}
}

func TestValidateSourceComments_IgnoresNodeModules(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "node_modules/foo/bar.js", "// the scaffold ships this placeholder\n")
	vs, err := scanSourceCommentsAt(dir)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "source-comment-authoring-voice-leak") {
		t.Errorf("node_modules should be skipped: %+v", vs)
	}
}

func TestValidateSourceComments_IgnoresLegitimateCausalComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSourceFile(t, dir, "src/storage.ts", `// Bucket policy is private — signed URLs give callers
// time-bounded access without exposing the bucket to the internet.
export const bucketPolicy = "private";
`)
	vs, err := scanSourceCommentsAt(dir)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("legit causal comment flagged: %+v", vs)
	}
}

func TestGateSourceCommentVoice_SkipsMissingSourceRoot(t *testing.T) {
	t.Parallel()

	// Empty SourceRoot → gate skips.
	vs := gateSourceCommentVoice(GateContext{
		Plan: &Plan{Codebases: []Codebase{{Hostname: "api", SourceRoot: ""}}},
	})
	if len(vs) != 0 {
		t.Errorf("empty SourceRoot should skip, got %+v", vs)
	}

	// Non-existent SourceRoot → gate skips (stat fails).
	vs = gateSourceCommentVoice(GateContext{
		Plan: &Plan{Codebases: []Codebase{{Hostname: "api", SourceRoot: "/does/not/exist"}}},
	})
	if len(vs) != 0 {
		t.Errorf("missing SourceRoot should skip, got %+v", vs)
	}
}

// TestGateSourceCommentVoice_FlagsLeaksAcrossCodebases — drives the
// gate through GateContext to prove the finalize wiring works.
func TestGateSourceCommentVoice_FlagsLeaksAcrossCodebases(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	apiRoot := filepath.Join(dir, "api")
	appRoot := filepath.Join(dir, "app")
	writeSourceFile(t, apiRoot, "src/main.ts", "// pre-ship contract item 5\n")
	writeSourceFile(t, appRoot, "src/App.svelte", "<!-- the scaffold ships this tab -->\n")

	vs := gateSourceCommentVoice(GateContext{
		Plan: &Plan{Codebases: []Codebase{
			{Hostname: "api", SourceRoot: apiRoot},
			{Hostname: "app", SourceRoot: appRoot},
		}},
	})
	if !containsCode(vs, "source-comment-authoring-voice-leak") {
		t.Errorf("expected voice-leak violations across both codebases, got %+v", vs)
	}
}

func TestContentAuthoring_IncludesVoiceRule(t *testing.T) {
	t.Parallel()

	body, err := readAtom("briefs/scaffold/content_authoring.md")
	if err != nil {
		t.Fatalf("readAtom: %v", err)
	}
	for _, anchor := range []string{
		"Voice",
		"porter",
		"never another recipe author",
		"we chose",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("content_authoring.md missing voice-rule anchor %q", anchor)
		}
	}
}
