package content

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

func TestBuildClaudeMD_Container_InjectsHostname(t *testing.T) {
	t.Parallel()
	out, err := BuildClaudeMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("BuildClaudeMD: %v", err)
	}
	if !strings.Contains(out, "ZCP control-plane container `zcp`") {
		t.Errorf("hostname not injected:\n%s", out)
	}
	if strings.Contains(out, "{{.SelfHostname}}") {
		t.Errorf("template var should be resolved, got raw {{.SelfHostname}}:\n%s", out)
	}
}

func TestBuildClaudeMD_Container_HasContainerFacts(t *testing.T) {
	t.Parallel()
	out, _ := BuildClaudeMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	for _, want := range []string{
		"# Zerops",
		"/var/www/{hostname}/",
		"SSHFS",
		"Read", "Edit", "Write",
		"Three entry points",
		"Don't guess",
		"intent` is your one-line proposal",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("container CLAUDE.md missing %q", want)
		}
	}
}

func TestBuildClaudeMD_Container_NoLocalLeak(t *testing.T) {
	t.Parallel()
	out, _ := BuildClaudeMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	for _, forbidden := range []string{
		"developer machine",
		"zcli vpn up",
		"working directory is the source of truth",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("container CLAUDE.md leaked local content %q", forbidden)
		}
	}
}

func TestBuildClaudeMD_Local_HasLocalFacts(t *testing.T) {
	t.Parallel()
	out, _ := BuildClaudeMD(runtime.Info{InContainer: false})
	for _, want := range []string{
		"# Zerops",
		"developer machine",
		"zerops_deploy",
		"zerops.yaml",
		"zcli vpn up",
		"Three entry points",
		"Don't guess",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("local CLAUDE.md missing %q", want)
		}
	}
}

func TestBuildClaudeMD_Local_NoContainerLeak(t *testing.T) {
	t.Parallel()
	out, _ := BuildClaudeMD(runtime.Info{InContainer: false})
	for _, forbidden := range []string{
		"/var/www/",
		"SSHFS",
		"ZCP control-plane container",
		"{{.SelfHostname}}",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("local CLAUDE.md leaked container content %q", forbidden)
		}
	}
}

func TestBuildClaudeMD_Deterministic(t *testing.T) {
	t.Parallel()
	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	a, _ := BuildClaudeMD(rt)
	b, _ := BuildClaudeMD(rt)
	if a != b {
		t.Error("BuildClaudeMD not deterministic for same Info")
	}
}

func TestBuildClaudeMD_DevelopFirst(t *testing.T) {
	t.Parallel()
	out, _ := BuildClaudeMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	devIdx := strings.Index(out, "1. **Develop**")
	bootIdx := strings.Index(out, "2. **Bootstrap**")
	recipeIdx := strings.Index(out, "3. **Recipe")
	if devIdx < 0 || bootIdx < 0 || recipeIdx < 0 {
		t.Fatalf("missing one of the three entry-point headers: develop=%d bootstrap=%d recipe=%d\n%s",
			devIdx, bootIdx, recipeIdx, out)
	}
	if devIdx >= bootIdx || bootIdx >= recipeIdx {
		t.Errorf("entry points out of order: develop=%d, bootstrap=%d, recipe=%d",
			devIdx, bootIdx, recipeIdx)
	}
}

// TestClaudeShared_NoEnvLeak — architecture invariant: the shared body
// must not contain env-specific content. Drift here would re-introduce
// the "or local" branching this refactor eliminates.
func TestClaudeShared_NoEnvLeak(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("claude_shared.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	forbidden := []string{
		"/var/www/",
		"SSHFS",
		"developer machine",
		"working directory is the source of truth",
		"zcli vpn up",
		"{{.SelfHostname}}",
	}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Errorf("claude_shared.md must not contain env-specific %q", f)
		}
	}
}

func TestClaudeContainer_HasHostnameTemplate(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("claude_container.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !strings.Contains(body, "{{.SelfHostname}}") {
		t.Error("claude_container.md must reference {{.SelfHostname}} template var")
	}
}

func TestClaudeLocal_NoContainerPaths(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("claude_local.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	forbidden := []string{"/var/www/", "SSHFS", "{{.SelfHostname}}"}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Errorf("claude_local.md must not contain container-specific %q", f)
		}
	}
}
