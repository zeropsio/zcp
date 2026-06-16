package content

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

func TestBuildAgentsMD_Container_InjectsHostname(t *testing.T) {
	t.Parallel()
	out, err := BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatalf("BuildAgentsMD: %v", err)
	}
	if !strings.Contains(out, "ZCP control-plane container `zcp`") {
		t.Errorf("hostname not injected:\n%s", out)
	}
	if strings.Contains(out, "{{.SelfHostname}}") {
		t.Errorf("template var should be resolved, got raw {{.SelfHostname}}:\n%s", out)
	}
}

func TestBuildAgentsMD_Container_HasContainerFacts(t *testing.T) {
	t.Parallel()
	out, _ := BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	for _, want := range []string{
		"# Zerops",
		"/var/www/{hostname}/",
		"SSHFS",
		"Read", "Edit", "Write",
		"Route every user turn",
		"Don't guess",
		"`intent` = one-line proposal",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("container AGENTS.md missing %q", want)
		}
	}
}

// TestBuildAgentsMD_Container_RunOnServiceRule pins the always-on
// edit-vs-run topology rule in the container shim: build/test/framework
// commands run INSIDE the service over SSH at the container-internal
// `/var/www`, distinct from EDITING via the host-view mount
// `/var/www/{hostname}/`. Before this, the rule lived only in
// first-deploy-gated develop atoms (develop-first-deploy-write-app,
// develop-platform-rules-container) and vanished from the steady-state
// deployed edit loop — exactly where agents iterate `npm run build` —
// so they ran it in the control-plane shell or against the mount path,
// where the runtime + deps don't exist. Single owner: this boot shim
// (env-shaped paths belong in the shim, not atoms — see CLAUDE.md).
func TestBuildAgentsMD_Container_RunOnServiceRule(t *testing.T) {
	t.Parallel()
	out, _ := BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	for _, want := range []string{
		"ssh {hostname}",    // the run handle (literal placeholder, not substituted)
		"cd /var/www && ",   // container-internal run path, distinct from the mount
		"npm run build",     // the canonical forgotten command
		"zerops_dev_server", // long-running dev servers are the one exception
	} {
		if !strings.Contains(out, want) {
			t.Errorf("container AGENTS.md missing run-on-service rule fragment %q", want)
		}
	}
	// Must state WHY commands run on the service, not in the agent's shell.
	if !strings.Contains(out, "not on this host") {
		t.Error("container AGENTS.md must explain WHY: the runtime + deps live in the service container, not on this host")
	}
}

// TestBuildAgentsMD_Container_EphemeralToolInstall pins the always-on
// "you can install tools on the service container" affordance. The agent
// runs as `zerops` with passwordless sudo on the live container (Alpine
// `apk` / Debian `apt-get`), so a missing CLI is an ad-hoc install away,
// not a blocker or a forced redeploy. The install is ephemeral (gone on
// the next deploy = fresh container); durable tooling belongs in
// `prepareCommands`. Without this the agent stalls or works around a
// "command not found" instead of just installing the tool. Affordance
// owner: this shim (always-on); durable-install depth stays in the
// develop-platform-rules-common atom.
func TestBuildAgentsMD_Container_EphemeralToolInstall(t *testing.T) {
	t.Parallel()
	out, _ := BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	for _, want := range []string{
		"sudo apk add",    // ephemeral install command shape (Alpine)
		"prepareCommands", // durable pointer — survive-redeploy installs go in zerops.yaml
		"ephemeral",       // the lost-on-redeploy caveat
	} {
		if !strings.Contains(out, want) {
			t.Errorf("container AGENTS.md missing ephemeral-tool-install affordance fragment %q", want)
		}
	}
}

func TestBuildAgentsMD_Container_NoLocalLeak(t *testing.T) {
	t.Parallel()
	out, _ := BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	for _, forbidden := range []string{
		"Developer machine",
		"zcli vpn up",
		"Working dir = source of truth",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("container AGENTS.md leaked local content %q", forbidden)
		}
	}
}

func TestBuildAgentsMD_Local_HasLocalFacts(t *testing.T) {
	t.Parallel()
	out, _ := BuildAgentsMD(runtime.Info{InContainer: false})
	for _, want := range []string{
		"# Zerops",
		"Developer machine",
		"zerops_deploy",
		"zerops.yaml",
		"zcli vpn up",
		"Route every user turn",
		"Don't guess",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("local AGENTS.md missing %q", want)
		}
	}
}

func TestBuildAgentsMD_Local_NoContainerLeak(t *testing.T) {
	t.Parallel()
	out, _ := BuildAgentsMD(runtime.Info{InContainer: false})
	for _, forbidden := range []string{
		"/var/www/",
		"SSHFS",
		"ZCP control-plane container",
		"{{.SelfHostname}}",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("local AGENTS.md leaked container content %q", forbidden)
		}
	}
}

func TestBuildAgentsMD_Deterministic(t *testing.T) {
	t.Parallel()
	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
	a, _ := BuildAgentsMD(rt)
	b, _ := BuildAgentsMD(rt)
	if a != b {
		t.Error("BuildAgentsMD not deterministic for same Info")
	}
}

func TestBuildAgentsMD_DevelopFirst(t *testing.T) {
	t.Parallel()
	out, _ := BuildAgentsMD(runtime.Info{InContainer: true, ServiceName: "zcp"})
	devIdx := strings.Index(out, "- `develop`")
	bootIdx := strings.Index(out, "- `bootstrap`")
	if devIdx < 0 || bootIdx < 0 {
		t.Fatalf("missing a workflow-detail bullet: develop=%d bootstrap=%d\n%s", devIdx, bootIdx, out)
	}
	if devIdx >= bootIdx {
		t.Errorf("workflow detail bullets out of order: develop=%d, bootstrap=%d", devIdx, bootIdx)
	}
	// The v2 `workflow="recipe"` bullet is dead (the dispatcher hard-blocks
	// it) — it must NOT appear as a workflow in any mode.
	if strings.Contains(out, "- `recipe` — self-contained pipeline") {
		t.Errorf("dead `workflow=recipe` bullet present in workflow detail:\n%s", out)
	}
}

// TestBuildAgentsMD_AuthoringGate — the recipe-authoring guidance is
// present iff rt.Authoring, mirroring the MCP tool-registration gate
// (single owner: runtime.Info.Authoring). docs/spec-authoring-boundary.md.
func TestBuildAgentsMD_AuthoringGate(t *testing.T) {
	t.Parallel()
	for _, env := range []runtime.Info{
		{InContainer: true, ServiceName: "zcp"},
		{InContainer: false},
	} {
		on, _ := BuildAgentsMD(runtime.Info{InContainer: env.InContainer, ServiceName: env.ServiceName, Authoring: true})
		off, _ := BuildAgentsMD(env)

		for _, want := range []string{"Recipe authoring (maintainer mode)", "zerops_recipe", "zerops_port"} {
			if !strings.Contains(on, want) {
				t.Errorf("InContainer=%v authoring ON: missing %q", env.InContainer, want)
			}
			if strings.Contains(off, want) {
				t.Errorf("InContainer=%v authoring OFF: leaked authoring content %q — end users must not see it", env.InContainer, want)
			}
		}
		// Bootstrap route=recipe CONSUMPTION is universal — present in
		// BOTH modes (it is NOT authoring; gating it would break end-user
		// recipe deploys).
		if !strings.Contains(off, `route="recipe"`) {
			t.Errorf("InContainer=%v authoring OFF: bootstrap route=recipe consumption guidance missing — that's a universal capability", env.InContainer)
		}
	}
}

// TestAgentsShared_NoAuthoringLeak — the shared body must carry NO
// recipe-authoring surface; that content lives only in the gated
// agents_authoring.md block. Pins the de-mention so authoring tool
// names can't creep back into the always-rendered body.
func TestAgentsShared_NoAuthoringLeak(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("agents_shared.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	for _, f := range []string{"zerops_recipe", "zerops_port", "- `recipe`"} {
		if strings.Contains(body, f) {
			t.Errorf("agents_shared.md must not contain authoring surface %q (belongs in gated agents_authoring.md)", f)
		}
	}
}

// TestAgentsAuthoring_EnvAgnostic — the authoring block is appended to
// BOTH the container and local AGENTS.md, so it must carry no
// env-specific content (else container paths would leak into a local
// maintainer's file, and vice versa).
func TestAgentsAuthoring_EnvAgnostic(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("agents_authoring.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	for _, f := range []string{"/var/www/", "SSHFS", "Developer machine", "zcli vpn up", "{{.SelfHostname}}"} {
		if strings.Contains(body, f) {
			t.Errorf("agents_authoring.md must be env-agnostic; found %q", f)
		}
	}
}

// TestAgentsShared_NoEnvLeak — architecture invariant: the shared body
// must not contain env-specific content. Drift here would re-introduce
// the "or local" branching this refactor eliminates.
func TestAgentsShared_NoEnvLeak(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("agents_shared.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	forbidden := []string{
		"/var/www/",
		"SSHFS",
		"Developer machine",
		"Working dir = source of truth",
		"zcli vpn up",
		"{{.SelfHostname}}",
	}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Errorf("agents_shared.md must not contain env-specific %q", f)
		}
	}
}

func TestAgentsContainer_HasHostnameTemplate(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("agents_container.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !strings.Contains(body, "{{.SelfHostname}}") {
		t.Error("agents_container.md must reference {{.SelfHostname}} template var")
	}
}

// TestAgentsContainer_MountClaimConditional asserts the mount-claim text
// is conditional on bootstrap/adopt provision completing, not asserted as
// ambient state. Phase 2.3 of plans/eval-review-20260518-subset/fix-plan.md:
// the previous unconditional claim ("Service code SSHFS-mounted at ...")
// burned several eval sessions where agents read AGENTS.md, did `ls
// /var/www/`, saw only AGENTS.md, and concluded the environment was
// broken. The actual mount materializes only after the bootstrap/adopt
// workflow's provision step closes (workflow_bootstrap.go::autoMountTargets).
func TestAgentsContainer_MountClaimConditional(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("agents_container.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !strings.Contains(body, "After bootstrap or adopt") {
		t.Error("agents_container.md must frame mount claim as conditional on bootstrap/adopt completion")
	}
	if !strings.Contains(body, "hasn't been bootstrapped") {
		t.Error("agents_container.md must tell the agent what to do when mount is empty (run adopt route)")
	}
}

func TestAgentsLocal_NoContainerPaths(t *testing.T) {
	t.Parallel()
	body, err := GetTemplate("agents_local.md")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	forbidden := []string{"/var/www/", "SSHFS", "{{.SelfHostname}}"}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Errorf("agents_local.md must not contain container-specific %q", f)
		}
	}
}
