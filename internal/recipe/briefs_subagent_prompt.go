package recipe

import (
	"errors"
	"fmt"
	"strings"
)

// buildSubagentPrompt composes the full sub-agent dispatch prompt:
// engine-owned recipe-level context (slug, framework, tier, codebase
// identity, sister codebases, managed services) + the engine brief
// body verbatim + a kind-specific closing-notes footer naming the
// self-validate path.
//
// Run-13 §B2 — eliminates the hand-typed wrapper main agent typed in
// front of the brief during dispatch. The wrapper carried slug,
// framework, codebase identity, mount paths, fragment-id list, close
// criteria — all derivable from Plan + the kind. Run-12 wrapper share
// was 28-38% across sub-agent dispatches; engine-composed wrapper
// drops to under 10%, finally meeting run-12-readiness criterion 15.
//
// Recipe-specific authoring decisions that aren't Plan-derivable
// (framework-canonical pins like "Svelte+Vite static" / "NestJS+
// TypeORM" the research phase recorded) ride along via
// plan.Research.Description — agents already record those there per
// the existing research-phase teaching, so no atom extension is
// needed for the run-13 stretch.
func buildSubagentPrompt(plan *Plan, parent *ParentRecipe, in RecipeInput) (string, error) {
	return buildSubagentPromptForPhase(plan, parent, in, "", "", nil)
}

// BuildSubagentPromptForReplay is the exported entry the
// `cmd/zcp-recipe-sim` tool calls so offline replay dispatches use
// the byte-identical prompt the production handler would compose. The
// replay tool prepends a thin "REPLAY MODE" adapter (file-write
// redirect) and otherwise sends this string verbatim to the dispatched
// Agent. Keeping replay on the same composition path means a brief or
// atom edit lands identically in simulation and production — divergence
// lives only in the leading adapter, never in the engine output.
func BuildSubagentPromptForReplay(plan *Plan, parent *ParentRecipe, in RecipeInput, currentPhase Phase, mountRoot string, facts []FactRecord) (string, error) {
	return buildSubagentPromptForPhase(plan, parent, in, currentPhase, mountRoot, facts)
}

// buildSubagentPromptForPhase is buildSubagentPrompt with the session's
// current phase explicitly threaded so the BriefFeature closing footer
// can teach a defensive re-dispatch sub-agent "the session is already at
// phase=<currentPhase>; do not re-walk research/provision/scaffold."
// Run-14 §C.2 (R-13-4) — features-2 in run-13 burned ~50s re-walking
// phase transitions after a compaction-driven re-dispatch landed in a
// fresh sub-agent session.
//
// mountRoot is the recipes-mount path (typically Session.MountRoot) used
// by the scaffold-kind branch to enumerate reachable recipe slugs in the
// dispatched brief. Run-15 R-14-P-1 — run-14's stealth regression: the
// scaffold brief composer had a Resolver-aware variant
// (BuildScaffoldBriefWithResolver) but the production dispatch path
// called the legacy non-resolver entry point, so dispatched scaffold
// briefs never carried "## Recipe-knowledge slugs you may consult".
// Empty mountRoot omits the section (matches the legacy unit-test
// shape).
func buildSubagentPromptForPhase(plan *Plan, parent *ParentRecipe, in RecipeInput, currentPhase Phase, mountRoot string, facts []FactRecord) (string, error) {
	if plan == nil {
		return "", errors.New("buildSubagentPrompt: nil plan")
	}
	kind := BriefKind(in.BriefKind)
	switch kind {
	case BriefScaffold, BriefFeature, BriefFinalize,
		BriefCodebaseContent, BriefClaudeMDAuthor, BriefEnvContent,
		BriefRefinement:
		// ok
	default:
		return "", fmt.Errorf("buildSubagentPrompt: unknown briefKind %q", in.BriefKind)
	}

	// Per-codebase kinds resolve cb from the plan; phase-wide kinds (feature,
	// finalize, env-content) operate on plan-only context.
	var cb Codebase
	if requiresCodebase(kind) {
		if in.Codebase == "" {
			return "", fmt.Errorf("buildSubagentPrompt: %s kind requires codebase", kind)
		}
		found := false
		for _, c := range plan.Codebases {
			if c.Hostname == in.Codebase {
				cb, found = c, true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("codebase %q not in plan", in.Codebase)
		}
	}

	brief, err := buildBriefForKind(plan, parent, kind, cb, mountRoot, facts)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	writePromptHeader(&b, plan, kind, cb)
	writePromptRecipeContext(&b, plan, kind, cb)
	b.WriteString("\n---\n\n")
	b.WriteString("# Engine brief — ")
	b.WriteString(string(kind))
	b.WriteString("\n\n")
	b.WriteString(brief.Body)
	if !strings.HasSuffix(brief.Body, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n---\n\n")
	writePromptCloseFooter(&b, kind, in.Codebase, currentPhase)
	return b.String(), nil
}

// buildBriefForKind dispatches to the right brief composer. Mirrors
// Session.BuildBrief but operates on plan + parent directly (no Session
// dependency) so buildSubagentPrompt is callable from tests without a
// full session.
//
// mountRoot is the recipes-mount path threaded through to the scaffold
// composer's Resolver. Empty mountRoot keeps the legacy non-resolver
// shape (unit tests). Production callers pass Session.MountRoot so the
// dispatched brief carries "## Recipe-knowledge slugs you may consult"
// (run-15 R-14-P-1).
// requiresCodebase reports whether a BriefKind binds to a single codebase.
// Scaffold + codebase-content + claudemd-author dispatch per-codebase;
// feature + finalize + env-content operate on plan-wide context.
func requiresCodebase(kind BriefKind) bool {
	switch kind {
	case BriefScaffold, BriefCodebaseContent, BriefClaudeMDAuthor:
		return true
	case BriefFeature, BriefFinalize, BriefEnvContent, BriefRefinement:
		return false
	}
	return false
}

func buildBriefForKind(plan *Plan, parent *ParentRecipe, kind BriefKind, cb Codebase, mountRoot string, facts []FactRecord) (Brief, error) {
	switch kind {
	case BriefScaffold:
		var resolver *Resolver
		if mountRoot != "" {
			resolver = &Resolver{MountRoot: mountRoot}
		}
		return BuildScaffoldBriefWithResolver(plan, cb, parent, resolver)
	case BriefFeature:
		return BuildFeatureBrief(plan)
	case BriefFinalize:
		return BuildFinalizeBrief(plan)
	case BriefCodebaseContent:
		return BuildCodebaseContentBrief(plan, cb, parent, facts)
	case BriefClaudeMDAuthor:
		return BuildClaudeMDBrief(plan, cb)
	case BriefEnvContent:
		return BuildEnvContentBrief(plan, parent, facts)
	case BriefRefinement:
		// Run-17 §9 — refinement composer wired without runDir; the
		// dispatcher path passes outputRoot via Session.OutputRoot at
		// the handler boundary. Static composition (no Session)
		// passes empty runDir; the brief still carries atoms + rubric.
		return BuildRefinementBrief(plan, parent, "", facts)
	default:
		return Brief{}, fmt.Errorf("unknown briefKind %q", kind)
	}
}

func writePromptHeader(b *strings.Builder, plan *Plan, kind BriefKind, cb Codebase) {
	switch kind {
	case BriefScaffold:
		fmt.Fprintf(b, "You are the scaffold sub-agent for the `%s` codebase of the %s recipe.\n",
			cb.Hostname, plan.Slug)
	case BriefFeature:
		fmt.Fprintf(b, "You are the feature sub-agent for the %s recipe.\n", plan.Slug)
	case BriefFinalize:
		fmt.Fprintf(b, "You are the finalize sub-agent for the %s recipe.\n", plan.Slug)
	case BriefCodebaseContent:
		fmt.Fprintf(b, "You are the codebase-content sub-agent for the `%s` codebase of the %s recipe.\n",
			cb.Hostname, plan.Slug)
	case BriefClaudeMDAuthor:
		fmt.Fprintf(b, "You are the claudemd-author sub-agent for the `%s` codebase of the %s recipe.\n",
			cb.Hostname, plan.Slug)
	case BriefEnvContent:
		fmt.Fprintf(b, "You are the env-content sub-agent for the %s recipe.\n", plan.Slug)
	case BriefRefinement:
		fmt.Fprintf(b, "You are the refinement sub-agent for the %s recipe.\n", plan.Slug)
	}
	b.WriteString("Read the engine brief below verbatim and follow it; the recipe-level\n")
	b.WriteString("context above and the closing notes below the brief are wrapper notes\n")
	b.WriteString("from the engine.\n\n")
	// Run-19 prep — `zerops_recipe` is an MCP tool. Sub-agents that
	// read the brief literally have, in past dogfoods, run
	// `Bash("zerops_recipe action=foo slug=...")` because the brief
	// uses the shorthand `<tool> <action> <args>` to refer to a JSON
	// invocation. That always fails (`command not found`) and wastes a
	// dispatch round-trip. Make the contract explicit at the top of
	// every emitted prompt so the next paste-as-shell attempt doesn't
	// happen.
	b.WriteString("**Tool-call shape**: `zerops_recipe` is an **MCP tool** invoked as a\n")
	b.WriteString("JSON tool call (e.g. `{\"action\": \"record-fragment\", \"slug\": \"...\", ...}`).\n")
	b.WriteString("It is NOT a shell command. The brief uses backtick shorthand\n")
	b.WriteString("`zerops_recipe action=X slug=Y` to refer to an MCP invocation; do\n")
	b.WriteString("not run it via Bash.\n\n")
}

func writePromptRecipeContext(b *strings.Builder, plan *Plan, kind BriefKind, cb Codebase) {
	b.WriteString("## Recipe-level context\n\n")
	fmt.Fprintf(b, "- Slug: `%s`\n", plan.Slug)
	if plan.Framework != "" {
		fmt.Fprintf(b, "- Framework family: %s\n", plan.Framework)
	}
	if plan.Tier != "" {
		fmt.Fprintf(b, "- Tier: `%s`\n", plan.Tier)
	}
	if plan.Research.CodebaseShape != "" {
		fmt.Fprintf(b, "- Codebase shape: %s (%s)\n",
			plan.Research.CodebaseShape, joinHostnames(plan.Codebases))
	}
	if plan.Research.AppSecretKey != "" {
		fmt.Fprintf(b, "- Project-level secret already set: `%s`\n",
			plan.Research.AppSecretKey)
	}
	if plan.Research.Description != "" {
		b.WriteString("\n### Research-phase decisions\n\n")
		b.WriteString(plan.Research.Description)
		if !strings.HasSuffix(plan.Research.Description, "\n") {
			b.WriteByte('\n')
		}
	}

	if kind == BriefScaffold {
		b.WriteString("\n### Sister codebases\n\n")
		emittedSister := false
		for _, peer := range plan.Codebases {
			if peer.Hostname == cb.Hostname {
				continue
			}
			emittedSister = true
			fmt.Fprintf(b, "- `%s` — role=%s, runtime=%s, worker=%t\n",
				peer.Hostname, peer.Role, peer.BaseRuntime, peer.IsWorker)
		}
		if !emittedSister {
			b.WriteString("(none — single-codebase recipe)\n")
		}
	}

	// Run-21 R2-3 — filter by per-codebase ConsumesServices so a SPA
	// codebase that only consumes `${api_zeropsSubdomain}` doesn't see
	// db/cache/broker in its brief. Empty ConsumesServices (slice is
	// nil) falls back to the full list — preserves the pre-R2-3 shape
	// for codebases the engine couldn't analyze (sim path that skips
	// scaffold, codebases without on-disk yaml). Empty BUT non-nil
	// (slice has len 0 from successful parse) emits nothing — the
	// codebase consumes no managed service.
	if len(plan.Services) > 0 {
		filtered := filterConsumedServices(plan.Services, cb.ConsumesServices)
		if len(filtered) > 0 {
			b.WriteString("\n### Managed services\n\n")
			for _, svc := range filtered {
				fmt.Fprintf(b, "- `%s` (%s) — kind=%s\n", svc.Hostname, svc.Type, svc.Kind)
			}
		}
	}

	if kind == BriefScaffold {
		b.WriteString("\n## Your codebase (`")
		b.WriteString(cb.Hostname)
		b.WriteString("`)\n\n")
		fmt.Fprintf(b, "- `cb.Hostname`: `%s`\n", cb.Hostname)
		if cb.SourceRoot != "" {
			fmt.Fprintf(b, "- `cb.SourceRoot` / mount: `%s`\n", cb.SourceRoot)
		}
		fmt.Fprintf(b, "- Dev slot: `%sdev`\n", cb.Hostname)
		fmt.Fprintf(b, "- Stage slot: `%sstage`\n", cb.Hostname)
		if cb.BaseRuntime != "" {
			fmt.Fprintf(b, "- Runtime: `%s`\n", cb.BaseRuntime)
		}
		if cb.IsWorker {
			b.WriteString("- Subdomain access: NO (worker role)\n")
		} else {
			b.WriteString("- Subdomain access: yes\n")
		}
	}
}

func writePromptCloseFooter(b *strings.Builder, kind BriefKind, codebase string, currentPhase Phase) {
	b.WriteString("## Closing notes from the engine\n\n")
	switch kind {
	case BriefScaffold:
		b.WriteString("When you're ready to terminate: ensure all required fragments\n")
		b.WriteString("are recorded, and call\n\n")
		fmt.Fprintf(b, "    zerops_recipe action=complete-phase phase=scaffold codebase=%s\n\n", codebase)
		b.WriteString("to self-validate. Fix any violations in-session via\n")
		b.WriteString("`record-fragment mode=replace` (fragments) or ssh-edit (yaml\n")
		b.WriteString("file), re-call until ok:true, then terminate.\n")
	case BriefFeature:
		if currentPhase != "" {
			fmt.Fprintf(b, "Note: the recipe session is already at phase=%s. If you join\n", currentPhase)
			b.WriteString("an existing session at a later phase (defensive re-dispatch after\n")
			b.WriteString("compaction is the common cause), do NOT re-walk research /\n")
			b.WriteString("provision / scaffold — the engine refuses the transitions and\n")
			b.WriteString("the on-disk state is already correct. Resume work at the\n")
			b.WriteString("current phase.\n\n")
		}
		// Run-21 R2-7 — re-state the SSHFS-no-local-build rule at brief
		// close so the agent re-encounters it before terminating. Build
		// failures debugged on the wrong site are the leading scope-
		// creep cause at feature phase (run-21 features-2nd burned 8 min
		// in a Vite-on-SSHFS ESM-import rabbit hole).
		b.WriteString("Reminder before you terminate: build/install commands run via\n")
		b.WriteString("`ssh <hostname>dev \"cd /var/www && <cmd>\"`, NOT against the\n")
		b.WriteString("local SSHFS mount. If you see an unfamiliar build failure,\n")
		b.WriteString("check the build site FIRST.\n\n")
		b.WriteString("When you're ready to terminate: ensure per-feature commits are\n")
		b.WriteString("in place, browser-verification facts recorded for each panel\n")
		b.WriteString("you exercised, and call\n\n")
		b.WriteString("    zerops_recipe action=complete-phase phase=feature codebase=<host>\n\n")
		b.WriteString("per touched codebase to self-validate. Fix violations in-\n")
		b.WriteString("session via `record-fragment mode=replace`, re-call until\n")
		b.WriteString("ok:true, then move on.\n")
	case BriefFinalize:
		b.WriteString("When you're ready to terminate: ensure every fragment is\n")
		b.WriteString("recorded, then call `stitch-content` and `complete-phase\n")
		b.WriteString("phase=finalize`. Address any blocking violations via\n")
		b.WriteString("`record-fragment mode=replace` (codebase ids) or fragment\n")
		b.WriteString("re-record (root/env ids).\n")
	case BriefCodebaseContent:
		b.WriteString("When you're ready to terminate: ensure every codebase fragment\n")
		b.WriteString("(intro + IG slots + KB + zerops.yaml comments) is recorded,\n")
		b.WriteString("and call\n\n")
		fmt.Fprintf(b, "    zerops_recipe action=complete-phase phase=codebase-content codebase=%s\n\n", codebase)
		b.WriteString("to self-validate. Fix violations via `record-fragment\n")
		b.WriteString("mode=replace`, re-call until ok:true, then terminate.\n")
	case BriefClaudeMDAuthor:
		b.WriteString("When you're ready to terminate: record the single\n")
		fmt.Fprintf(b, "`codebase/%s/claude-md` fragment via\n\n", codebase)
		b.WriteString("    zerops_recipe action=record-fragment mode=replace\n")
		fmt.Fprintf(b, "      fragmentId=codebase/%s/claude-md\n", codebase)
		b.WriteString("      fragment=<your /init output>\n\n")
		b.WriteString("If slot-shape refusal fires (Zerops content detected),\n")
		b.WriteString("re-author without the offending tokens, then terminate.\n")
	case BriefEnvContent:
		b.WriteString("When you're ready to terminate: ensure root/intro + env/<N>/intro\n")
		b.WriteString("(N=0..5) + per-tier import-comments are recorded, then call\n\n")
		b.WriteString("    zerops_recipe action=complete-phase phase=env-content\n\n")
		b.WriteString("to self-validate. Fix violations via `record-fragment\n")
		b.WriteString("mode=replace`, re-call until ok:true, then terminate.\n")
	case BriefRefinement:
		b.WriteString("When you've refined every fragment that meets the 100%-sure\n")
		b.WriteString("threshold, call\n\n")
		b.WriteString("    zerops_recipe action=complete-phase phase=refinement\n\n")
		b.WriteString("to close the run. Each `record-fragment mode=replace` you fire\n")
		b.WriteString("at this phase is wrapped in a snapshot/restore primitive — if\n")
		b.WriteString("a post-replace validator surfaces a new violation, the engine\n")
		b.WriteString("reverts the fragment to its pre-refinement body. Per-fragment\n")
		b.WriteString("edit cap is 1 attempt; do NOT loop.\n")
	}
}

// joinHostnames renders codebase hostnames as a comma-separated list
// for the recipe-level context block.
func joinHostnames(cbs []Codebase) string {
	out := make([]string, 0, len(cbs))
	for _, c := range cbs {
		out = append(out, c.Hostname)
	}
	return strings.Join(out, ", ")
}
