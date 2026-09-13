package eval

import (
	"testing"
)

// TestReadTranscriptToolUses_MCPAndBash_Normalised pins FM-59: every
// tool_use block in transcript.jsonl is read — MCP tools (name normalised
// by stripping the "mcp__<server>__" prefix) AND the agent's own Bash/Read/…
// tool_use blocks (left as-is). Non-tool_use content (plain text) is
// ignored, and a malformed line is skipped rather than aborting the read.
func TestReadTranscriptToolUses_MCPAndBash_Normalised(t *testing.T) {
	t.Parallel()
	uses, present := ReadTranscriptToolUses("testdata/transcript-sample.jsonl")
	if !present {
		t.Fatal("present = false, want true (fixture file exists and is parseable)")
	}
	if len(uses) != 2 {
		t.Fatalf("len(uses) = %d, want 2 (one MCP tool_use, one Bash tool_use); got %+v", len(uses), uses)
	}

	deploy := uses[0]
	if deploy.Tool != "zerops_deploy" {
		t.Errorf("uses[0].Tool = %q, want %q (mcp__zerops__ prefix stripped)", deploy.Tool, "zerops_deploy")
	}
	if deploy.Input["hostname"] != "api" || deploy.Input["strategy"] != "rebuild" {
		t.Errorf("uses[0].Input = %+v, want hostname=api strategy=rebuild", deploy.Input)
	}
	if deploy.At.IsZero() {
		t.Error("uses[0].At is zero, want the fixture's timestamp")
	}

	bash := uses[1]
	if bash.Tool != "Bash" {
		t.Errorf("uses[1].Tool = %q, want %q (non-MCP tool name left as-is)", bash.Tool, "Bash")
	}
	if bash.Input["command"] != "ls -la /var/www" {
		t.Errorf("uses[1].Input[command] = %q, want %q", bash.Input["command"], "ls -la /var/www")
	}
}

// TestReadTranscriptToolUses_MissingFile_NotPresent pins that a missing
// transcript.jsonl reports present=false (the "no transcript" case FM-59's
// toolArg blocked-rows depend on), never a fatal error.
func TestReadTranscriptToolUses_MissingFile_NotPresent(t *testing.T) {
	t.Parallel()
	uses, present := ReadTranscriptToolUses("testdata/does-not-exist-transcript.jsonl")
	if present {
		t.Error("present = true, want false for a missing file")
	}
	if uses != nil {
		t.Errorf("uses = %+v, want nil", uses)
	}
}
