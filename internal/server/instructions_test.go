// Tests for: server/instructions.go — static MCP instructions text.
package server

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

func TestBuildInstructions_Container_HasEnvironmentBlock(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(runtime.Info{InContainer: true})
	for _, want := range []string{
		"ZCP manages Zerops",
		`zerops_workflow action="status"`,
		"/var/www/",
		"SSHFS",
		"ssh",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("container instructions missing %q", want)
		}
	}
}

func TestBuildInstructions_Local_HasEnvironmentBlock(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(runtime.Info{InContainer: false})
	for _, want := range []string{
		"ZCP manages Zerops",
		`zerops_workflow action="status"`,
		"zcli push",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("local instructions missing %q", want)
		}
	}
	if strings.Contains(out, "/var/www/") {
		t.Error("local instructions should not mention container mount paths")
	}
}

func TestBuildInstructions_Container_WithSelfHostname(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(runtime.Info{InContainer: true, ServiceName: "zcpx"})
	if !strings.Contains(out, "'zcpx'") {
		t.Errorf("expected self-hostname reference, got:\n%s", out)
	}
}

func TestBuildInstructions_Static_NoDynamicContent(t *testing.T) {
	t.Parallel()
	// Same runtime info must always produce byte-identical output —
	// instructions are built once at init and must not drift between calls.
	rt := runtime.Info{InContainer: true, ServiceName: "zcpx"}
	a := BuildInstructions(rt)
	b := BuildInstructions(rt)
	if a != b {
		t.Error("BuildInstructions is not deterministic for identical runtime.Info")
	}
}

func TestBuildInstructions_FitsIn2KB(t *testing.T) {
	t.Parallel()
	// Claude Code v2.1.84+ imposes a 2KB limit on the instructions field.
	const limit = 2048
	for _, tc := range []struct {
		name string
		rt   runtime.Info
	}{
		{"container", runtime.Info{InContainer: true, ServiceName: "zcpx"}},
		{"local", runtime.Info{InContainer: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := BuildInstructions(tc.rt)
			if len(out) > limit {
				t.Errorf("instructions = %d bytes, must be under %d", len(out), limit)
			}
		})
	}
}

func TestBuildInstructions_DevelopEntryPrecedesStatus(t *testing.T) {
	t.Parallel()
	out := BuildInstructions(runtime.Info{InContainer: true})
	startIdx := strings.Index(out, `action="start"`)
	statusIdx := strings.Index(out, `action="status"`)
	if startIdx < 0 {
		t.Fatal(`instructions must mention action="start"`)
	}
	if statusIdx < 0 {
		t.Fatal(`instructions must mention action="status"`)
	}
	if startIdx > statusIdx {
		t.Errorf(`action="start" (%d) should appear before action="status" (%d) — develop is the primary entry, status is recovery`, startIdx, statusIdx)
	}
}
