// Tests for: server/instructions.go — runtime-only MCP init payload.
package server

import (
	"strings"
	"testing"
)

// MCP init is now runtime-only. Static project rules (Three entry
// points, intent rule, env preamble) live in CLAUDE.md (env-rendered
// at zcp init). These tests pin the runtime/static separation.

func TestBuildInstructions_Empty_WhenNothingApplies(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(RuntimeContext{})
	if out != "" {
		t.Errorf("expected empty MCP init when no runtime context, got %q", out)
	}
}

func TestBuildInstructions_AdoptionNoteOnly(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(RuntimeContext{AdoptionNote: "Adopted: appdev"})
	if out != "Adopted: appdev" {
		t.Errorf("got %q, want %q", out, "Adopted: appdev")
	}
}

func TestBuildInstructions_StateHintOnly(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(RuntimeContext{StateHint: "Active recipe session: foo"})
	if out != "Active recipe session: foo" {
		t.Errorf("got %q", out)
	}
}

func TestBuildInstructions_BothJoinedByBlankLine(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(RuntimeContext{
		AdoptionNote: "Adopted: appdev",
		StateHint:    "Active recipe session: foo",
	})
	want := "Adopted: appdev\n\nActive recipe session: foo"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// Architecture invariant: MCP init must not contain static project
// rules. Static rules live in CLAUDE.md (the strong-adherence surface).
// Drift here would re-introduce the duplication this refactor
// eliminates.
func TestBuildInstructions_NoStaticRulesLeak(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(RuntimeContext{
		AdoptionNote: "Adopted: appdev",
		StateHint:    "Active recipe session: foo",
	})
	forbidden := []string{
		"Three entry points",
		"workflow=\"develop\"",
		"workflow=\"bootstrap\"",
		"/var/www/",
		"SSHFS",
		"Don't guess",
	}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("MCP init must not contain %q (belongs in CLAUDE.md): %s", f, out)
		}
	}
}

func TestBuildInstructions_FitsIn2KB(t *testing.T) {
	t.Parallel()
	// Even with rich state hint + adoption, MCP init stays under the MCP
	// protocol 2KB instructions budget.
	rc := RuntimeContext{
		AdoptionNote: strings.Repeat("Adopted: ", 30) + "appdev",
		StateHint:    strings.Repeat("Active session: ", 30) + "details",
	}
	out := BuildInstructions(rc)
	const limit = 2048
	if len(out) > limit {
		t.Errorf("instructions = %d bytes, must be under %d", len(out), limit)
	}
}

func TestBuildInstructions_Deterministic(t *testing.T) {
	t.Parallel()
	rc := RuntimeContext{AdoptionNote: "x", StateHint: "y"}
	a := BuildInstructions(rc)
	b := BuildInstructions(RuntimeContext{AdoptionNote: "x", StateHint: "y"})
	if a != b {
		t.Errorf("BuildInstructions not deterministic for same RuntimeContext: %q vs %q", a, b)
	}
}

func TestComposeStateHint_EmptyStateDir_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := ComposeStateHint("", 1234); got != "" {
		t.Errorf("expected empty hint for empty stateDir, got %q", got)
	}
}

func TestComposeStateHint_NonexistentStateDir_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	// Real path that doesn't exist; ListSessions soft-fails.
	if got := ComposeStateHint(t.TempDir()+"/nonexistent", 1234); got != "" {
		t.Errorf("expected empty hint when no sessions, got %q", got)
	}
}
