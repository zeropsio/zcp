package workflow

import (
	"strings"
	"testing"
	"time"
)

// TestBuildSubagentBrief_WriterReturnsStitchedBrief is the anchor test
// for Cx-SUBAGENT-BRIEF-BUILDER. Post-Cx-1 the writer atom corpus no
// longer carries ghost env paths, standalone file prescriptions, or
// root-README content; the stitched writer brief must still include
// every surviving atom body. The function's output is what the main
// agent forwards to Task — any missing atom means a missing teaching.
func TestBuildSubagentBrief_WriterReturnsStitchedBrief(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlanForBrief()
	got, err := BuildSubagentBrief(plan, SubagentRoleWriter, "/tmp/zcp-facts-test.jsonl", "")
	if err != nil {
		t.Fatalf("BuildSubagentBrief: %v", err)
	}
	if got.Role != SubagentRoleWriter {
		t.Errorf("role=%q want %q", got.Role, SubagentRoleWriter)
	}
	if got.Description == "" || !strings.Contains(strings.ToLower(got.Description), "readme") {
		t.Errorf("description must mention readme; got %q", got.Description)
	}
	if got.PromptSHA == "" || len(got.PromptSHA) != 64 {
		t.Errorf("promptSHA must be a hex-encoded 256-bit hash; got %q", got.PromptSHA)
	}
	if got.Prompt == "" {
		t.Fatal("prompt is empty")
	}
	// No unresolved template literals survive the render path.
	if strings.Contains(got.Prompt, "{{") || strings.Contains(got.Prompt, "}}") {
		t.Errorf("prompt contains unresolved {{...}} tokens:\n%s", got.Prompt)
	}
	// Post-Cx-1: writer atoms must never mention standalone dead files
	// nor slug-named env folders. The stitched prompt inherits that
	// property.
	for _, forbidden := range []string{"INTEGRATION-GUIDE.md", "GOTCHAS.md", "ai-agent", "remote-dev", "small-prod"} {
		if strings.Contains(got.Prompt, forbidden) {
			t.Errorf("prompt leaks Cx-1-forbidden token %q", forbidden)
		}
	}
	// Core writer atoms must all be present. Cheap substring signal per atom.
	for _, marker := range []string{
		"Mandatory core",
		"Fresh-context premise",
		"Canonical output tree",
		"Content surface contracts",
		"Self-review per surface",
		"Completion shape",
	} {
		if !strings.Contains(got.Prompt, marker) {
			t.Errorf("prompt missing atom marker %q — writer corpus not fully stitched", marker)
		}
	}
	// Facts log path flows through.
	if !strings.Contains(got.Prompt, "/tmp/zcp-facts-test.jsonl") {
		t.Errorf("prompt must carry the factsLogPath interpolation; got:\n%s", got.Prompt)
	}
}

// TestBuildSubagentBrief_DeterministicHash: two builds against the
// same plan + inputs must produce byte-identical prompts + hashes.
// Non-determinism here would defeat the dispatch-guard SHA compare.
func TestBuildSubagentBrief_DeterministicHash(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlanForBrief()
	a, err := BuildSubagentBrief(plan, SubagentRoleWriter, "/tmp/x.jsonl", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildSubagentBrief(plan, SubagentRoleWriter, "/tmp/x.jsonl", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.PromptSHA != b.PromptSHA {
		t.Errorf("two builds produced different hashes:\na=%s\nb=%s", a.PromptSHA, b.PromptSHA)
	}
	if a.Prompt != b.Prompt {
		t.Error("two builds produced different prompt bytes")
	}
}

// TestBuildSubagentBrief_UnknownRoleRejected: a role keyword the
// handler doesn't recognise is INVALID_PARAMETER territory. The
// error message must name the accepted role set so the caller knows
// how to recover.
func TestBuildSubagentBrief_UnknownRoleRejected(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlanForBrief()
	_, err := BuildSubagentBrief(plan, "bogus", "", "")
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
	msg := err.Error()
	for _, want := range []string{"writer", "editorial-review", "code-review", "bogus"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must mention %q; got %q", want, msg)
		}
	}
}

// TestBuildSubagentBrief_NilPlanRejected: a build against a nil plan
// would produce a template-studded prompt. The caller must surface an
// error so the agent can recover (by starting a session first).
func TestBuildSubagentBrief_NilPlanRejected(t *testing.T) {
	t.Parallel()
	_, err := BuildSubagentBrief(nil, SubagentRoleWriter, "", "")
	if err == nil {
		t.Fatal("expected error for nil plan")
	}
	if !strings.Contains(err.Error(), "recipe plan") {
		t.Errorf("error must mention recipe plan; got %q", err.Error())
	}
}

// TestResolveSubagentRoleFromDescription covers the description-to-role
// lookup the dispatch guard relies on. v37's writer dispatch description
// was "Author recipe READMEs + CLAUDE.md + manifest" — no literal
// "writer" token — the resolver must still classify it as writer.
func TestResolveSubagentRoleFromDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc     string
		wantRole string
	}{
		{"Author recipe READMEs + CLAUDE.md + manifest", SubagentRoleWriter},
		{"author READMEs", SubagentRoleWriter},
		{"Recipe writer sub-agent", SubagentRoleWriter},
		{"Editorial review of recipe content", SubagentRoleEditorialReview},
		{"editorial-review pass", SubagentRoleEditorialReview},
		{"Code review of the recipe scaffold", SubagentRoleCodeReview},
		{"code-review sub-agent", SubagentRoleCodeReview},
		{"Arbitrary developer Task", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := ResolveSubagentRoleFromDescription(tt.desc)
		if got != tt.wantRole {
			t.Errorf("ResolveSubagentRoleFromDescription(%q) = %q, want %q", tt.desc, got, tt.wantRole)
		}
	}
}

// TestVerifySubagentDispatch_RefusesParaphrasedPrompt: the main agent
// builds a brief, then submits a Task with a paraphrased prompt. The
// guard detects the SHA mismatch and returns ok=false with a remediation
// sentence naming the correct recovery (call build-subagent-brief
// then pass the prompt verbatim).
func TestVerifySubagentDispatch_RefusesParaphrasedPrompt(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlanForBrief()
	built, err := BuildSubagentBrief(plan, SubagentRoleWriter, "/tmp/x.jsonl", "")
	if err != nil {
		t.Fatal(err)
	}
	state := &RecipeState{
		LastSubagentBrief: map[string]SubagentBriefRecord{
			SubagentRoleWriter: {
				Role:        SubagentRoleWriter,
				Description: built.Description,
				PromptSHA:   built.PromptSHA,
				BuiltAt:     time.Now().UTC().Format(time.RFC3339),
				PromptSize:  len(built.Prompt),
			},
		},
	}
	// Paraphrase: drop the first line of the prompt.
	paraphrased := strings.SplitN(built.Prompt, "\n", 2)[1]
	role, ok, reason := VerifySubagentDispatch(state, built.Description, paraphrased)
	if role != SubagentRoleWriter {
		t.Errorf("role=%q want %q", role, SubagentRoleWriter)
	}
	if ok {
		t.Fatal("paraphrased prompt must not be accepted")
	}
	for _, want := range []string{"prompt SHA", "build-subagent-brief", "role=writer"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason must mention %q; got %q", want, reason)
		}
	}
}

// TestVerifySubagentDispatch_AcceptsVerbatimPrompt: the happy path —
// Task prompt is the byte-for-byte output of the most recent brief
// build. Guard returns ok=true and no remediation text.
func TestVerifySubagentDispatch_AcceptsVerbatimPrompt(t *testing.T) {
	t.Parallel()
	plan := testShowcasePlanForBrief()
	built, err := BuildSubagentBrief(plan, SubagentRoleWriter, "/tmp/x.jsonl", "")
	if err != nil {
		t.Fatal(err)
	}
	state := &RecipeState{
		LastSubagentBrief: map[string]SubagentBriefRecord{
			SubagentRoleWriter: {
				Role:        SubagentRoleWriter,
				Description: built.Description,
				PromptSHA:   built.PromptSHA,
				BuiltAt:     time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	role, ok, reason := VerifySubagentDispatch(state, built.Description, built.Prompt)
	if role != SubagentRoleWriter {
		t.Errorf("role=%q want %q", role, SubagentRoleWriter)
	}
	if !ok {
		t.Errorf("verbatim prompt rejected: %s", reason)
	}
	if reason != "" {
		t.Errorf("reason must be empty on accept; got %q", reason)
	}
}

// TestVerifySubagentDispatch_NonGuardedDescriptionPassesThrough: a
// Task with a description that doesn't match any guarded role is
// accepted even if no brief was built. The guard only engages on
// writer/editorial/code-review dispatches.
func TestVerifySubagentDispatch_NonGuardedDescriptionPassesThrough(t *testing.T) {
	t.Parallel()
	role, ok, reason := VerifySubagentDispatch(nil, "Generic developer Task", "some prompt")
	if role != "" {
		t.Errorf("role must be empty for non-guarded description; got %q", role)
	}
	if !ok {
		t.Errorf("non-guarded description must pass the guard; reason=%q", reason)
	}
}

// TestVerifySubagentDispatch_NoBuildCallYet: a Task dispatch for a
// guarded role with NO prior build-subagent-brief call is refused.
// Surfaces the main-agent-forgot-the-workflow case.
func TestVerifySubagentDispatch_NoBuildCallYet(t *testing.T) {
	t.Parallel()
	role, ok, reason := VerifySubagentDispatch(nil, "Author recipe READMEs + CLAUDE.md + manifest", "some prompt")
	if role != SubagentRoleWriter {
		t.Errorf("role=%q want %q", role, SubagentRoleWriter)
	}
	if ok {
		t.Error("dispatch with no build-subagent-brief call must be refused")
	}
	if !strings.Contains(reason, "no build-subagent-brief") {
		t.Errorf("reason must name the missing prerequisite; got %q", reason)
	}
}

// testShowcasePlanForBrief returns a showcase recipe plan sized like
// nestjs-showcase — three runtime codebases, managed services — so
// the brief stitcher has realistic context to interpolate.
func testShowcasePlanForBrief() *RecipePlan {
	return &RecipePlan{
		Framework: "nestjs",
		Tier:      "showcase",
		Slug:      "nestjs-showcase",
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, SharesCodebaseWith: "api"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
}
