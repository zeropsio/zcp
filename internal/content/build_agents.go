package content

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/runtime"
)

// BuildAgentsMD composes the env-rendered AGENTS.md body from embedded
// templates: agents_shared.md (env-agnostic body) plus exactly one
// env-specific preamble (agents_container.md or agents_local.md). User
// contexts splice agents_onboarding.md between the preamble and shared
// routing body. Authoring contexts omit onboarding and append an
// agents_authoring.md block describing the recipe-authoring surface.
//
// The authoring block is gated on the SAME rt.Authoring flag as the MCP
// server's authoring tool registration (internal/server, both fed by
// runtime.Detect), so the agent context and the tool surface cannot
// drift: end users (gate off) get no recipe-authoring guidance and no
// authoring tools; maintainers (gate on) get both. The block is
// env-agnostic so it composes cleanly into either preamble.
//
// Container preamble carries a {{.SelfHostname}} template var, resolved
// to rt.ServiceName at composition time. The composed output is wrapped
// in <!-- ZCP:BEGIN/END --> markers by the caller (init.generateAgentContext).
//
// Render is install-time: zcp init detects rt.InContainer and freezes
// the env into the disk file. Subsequent zcp serve runs use
// RefreshAgentContext to re-render the marked section in BOTH envs
// (local and container) so a long-lived install doesn't drift past the
// build's template version. Env + authoring mode are stable per install;
// toggling ZCP_AUTHORING (or moving envs) means the next zcp serve
// refresh re-renders the block accordingly.
//
// AGENTS.md is the cross-tool canonical context file consumed by Codex,
// Cursor, Gemini, Antigravity, and ~17 other agents on the agents.md
// Linux Foundation standard. Claude Code consumes CLAUDE.md, which the
// Claude adapter writes as a thin @AGENTS.md include wrapper — so both
// agents see the same content from one source.
// guided enables the always-on guided block (user-only mode). It is project
// CONFIG resolved by the caller (init from the --guided flag, the serve-time
// refresh from projectcfg) and passed in — deliberately NOT a runtime.Info
// field, which is env/container detection only. Mutually exclusive with
// authoring: the block is appended only on `guided && !rt.Authoring`.
func BuildAgentsMD(rt runtime.Info, guided bool) (string, error) {
	shared, err := GetTemplate("agents_shared.md")
	if err != nil {
		return "", fmt.Errorf("read agents_shared.md: %w", err)
	}

	var preamble string
	if rt.InContainer {
		tmpl, err := GetTemplate("agents_container.md")
		if err != nil {
			return "", fmt.Errorf("read agents_container.md: %w", err)
		}
		preamble = strings.ReplaceAll(tmpl, "{{.SelfHostname}}", rt.ServiceName)
		// What this container reaches beyond its own project, said once, so
		// an agent does not have to dump its environment to discover it.
		//
		// Outside a Mate the block says how to LOOK rather than what is
		// there: reach is a property of the container's own token, and any
		// flag here mirroring it would be a second copy free to drift from it.
		// A Mate's token reaches no other project; its application's stage
		// and production are HQ's to show (zerops_observe, docs/spec-mate.md
		// §10.8), so its block names that tool instead.
		//
		// The stand-up and git-host blocks are a Mate's too: only there is
		// zerops_standup a tool (docs/spec-mate.md D32), and only a Mate
		// delivers its code to HQ. Only a Mate's conversation draws a page
		// its agent publishes (zerops_publish_page, docs/spec-mate.md §5.9).
		for _, block := range []struct {
			include bool
			name    string
		}{
			{rt.MateEnabled, "agents_standup.md"},
			{!rt.MateEnabled, "agents_group.md"},
			{rt.MateEnabled, "agents_application.md"},
			{rt.MateEnabled, "agents_git_host.md"},
			{rt.MateEnabled, "agents_pages.md"},
			{rt.MateEnabled, "agents_live_facts.md"},
		} {
			if !block.include {
				continue
			}
			tmpl, err := GetTemplate(block.name)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", block.name, err)
			}
			preamble += "\n\n" + strings.TrimSpace(tmpl)
		}
	} else {
		tmpl, err := GetTemplate("agents_local.md")
		if err != nil {
			return "", fmt.Errorf("read agents_local.md: %w", err)
		}
		preamble = tmpl
	}

	var onboardingBlock string
	if !rt.Authoring {
		onboarding, err := GetTemplate("agents_onboarding.md")
		if err != nil {
			return "", fmt.Errorf("read agents_onboarding.md: %w", err)
		}
		onboardingBlock = strings.TrimSpace(onboarding) + "\n\n"
	}

	body := "# Zerops\n\n" +
		strings.TrimSpace(preamble) + "\n\n" +
		onboardingBlock +
		strings.TrimSpace(shared) + "\n"

	if rt.Authoring {
		authoring, err := GetTemplate("agents_authoring.md")
		if err != nil {
			return "", fmt.Errorf("read agents_authoring.md: %w", err)
		}
		body += "\n" + strings.TrimSpace(authoring) + "\n"
	}

	// Guided block — USER-ONLY, mutually exclusive with authoring. The
	// `&& !rt.Authoring` is mandatory: an authoring-context AGENTS.md must
	// never carry the guided block (pinned by
	// TestBuildAgentsMD_AuthoringExcludesGuided).
	if guided && !rt.Authoring {
		guided, err := GetTemplate("agents_guided.md")
		if err != nil {
			return "", fmt.Errorf("read agents_guided.md: %w", err)
		}
		body += "\n" + strings.TrimSpace(guided) + "\n"
	}

	return body, nil
}

// BuildClaudeWrapper returns the body content of CLAUDE.md's
// ZCP-managed section: an @AGENTS.md include that pulls the canonical
// body into Claude's context (Claude Code's native @-include syntax).
//
// The wrapper is intentionally minimal today — the corpus + boot shim
// content is agent-neutral after the genericization in commit d0f8a449,
// so there's nothing Claude-specific to add here. The deltas section is
// reserved for future Claude-only guidance (e.g. an explicit
// `run_in_background=true` example block) that would mislead non-Claude
// agents if it lived in AGENTS.md.
//
// Caller wraps the returned body in <!-- ZCP:BEGIN/END --> markers.
func BuildClaudeWrapper() string {
	return "@AGENTS.md\n"
}
