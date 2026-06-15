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
func buildSubagentPrompt(plan *Plan, in RecipeInput) (string, error) {
	return buildSubagentPromptForPhase(plan, nil, in, "", "", nil)
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

// isMultiFileBriefKind reports whether the kind composes via the
// multi-file path (index.md + part-*.md under
// `<outputRoot>/.briefs/<kind>-<codebase>-<unixnano>/`). Run-31 Fix #1
// closure — codebase-content / env-content / refinement carry the
// volume that historically blew the Read-tool token cap; the others
// stay on the single-file path.
func isMultiFileBriefKind(kind BriefKind) bool {
	//exhaustive:ignore — single-file kinds (scaffold/feature/finalize/claudemd-author) fall through to the false return.
	switch kind {
	case BriefCodebaseContent, BriefEnvContent, BriefRefinement:
		return true
	}
	return false
}

// composePromptHeaderAndFooter renders the recipe-level context
// (header) and closing notes (footer) the dispatch path normally
// concatenates around the engine brief body. For multi-file briefs the
// header is rendered into index.md above the "Read order" section and
// the footer is rendered below it; the legacy single-file path
// concatenates them around brief.Body.
func composePromptHeaderAndFooter(plan *Plan, kind BriefKind, cb Codebase, codebase string, currentPhase Phase) (header, footer string) {
	var hb strings.Builder
	writePromptHeader(&hb, plan, kind, cb)
	writePromptRecipeContext(&hb, plan, kind, cb)
	hb.WriteString("\n---\n\n")
	hb.WriteString("# Engine brief — ")
	hb.WriteString(string(kind))
	hb.WriteString("\n\n")

	var fb strings.Builder
	fb.WriteString("\n---\n\n")
	writePromptCloseFooter(&fb, kind, codebase, currentPhase)
	return hb.String(), fb.String()
}

// buildSubagentDispatchForPhase returns either an inline `prompt` (for
// single-file kinds; legacy path) or an `indexPath` (for multi-file
// kinds; new run-31 path). Exactly one is non-empty. outputRoot is
// required for multi-file kinds; an empty outputRoot returns an error
// for those.
func buildSubagentDispatchForPhase(plan *Plan, parent *ParentRecipe, in RecipeInput, currentPhase Phase, mountRoot, outputRoot string, facts []FactRecord) (prompt, indexPath string, err error) {
	if plan == nil {
		return "", "", errors.New("buildSubagentDispatch: nil plan")
	}
	kind := BriefKind(in.BriefKind)
	if !isMultiFileBriefKind(kind) {
		p, pErr := buildSubagentPromptForPhase(plan, parent, in, currentPhase, mountRoot, facts)
		return p, "", pErr
	}
	// Multi-file path.
	if outputRoot == "" {
		return "", "", errors.New("multi-file brief requires outputRoot")
	}
	var cb Codebase
	if requiresCodebase(kind) {
		if in.Codebase == "" {
			return "", "", fmt.Errorf("buildSubagentDispatch: %s kind requires codebase", kind)
		}
		found := false
		for _, c := range plan.Codebases {
			if c.Hostname == in.Codebase {
				cb, found = c, true
				break
			}
		}
		if !found {
			return "", "", fmt.Errorf("codebase %q not in plan", in.Codebase)
		}
	}
	header, footer := composePromptHeaderAndFooter(plan, kind, cb, in.Codebase, currentPhase)
	var brief Brief
	//exhaustive:ignore — isMultiFileBriefKind already filtered single-file kinds; default arm catches mismatches.
	switch kind {
	case BriefCodebaseContent:
		brief, err = buildCodebaseContentBriefMultiFileWithFraming(plan, cb, parent, facts, outputRoot, header, footer)
	case BriefEnvContent:
		brief, err = buildEnvContentBriefMultiFileWithFraming(plan, parent, facts, outputRoot, header, footer)
	case BriefRefinement:
		// runDir == outputRoot — both name the run-output root the
		// stitched README/import.yaml/zerops.yaml/CLAUDE.md files land
		// under, which is what renderRefinementStitchedPointerBlock
		// emits as Read-targets for the refinement sub-agent.
		// Production parity: workflow.go::Session.BuildBrief threads
		// `Session.OutputRoot` as runDir into BuildRefinementBrief on
		// the single-file path; the multi-file path now mirrors that.
		brief, err = buildRefinementBriefMultiFileWithFraming(plan, parent, outputRoot, facts, outputRoot, header, footer)
	default:
		return "", "", fmt.Errorf("multi-file dispatch: unknown kind %q", kind)
	}
	if err != nil {
		return "", "", err
	}
	return "", brief.IndexPath, nil
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
		BriefRefinement, BriefRefinement2:
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

	brief, err := buildBriefForKind(plan, parent, kind, cb, mountRoot, facts, FeaturePass(in.FeaturePass))
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
	case BriefFeature, BriefFinalize, BriefEnvContent, BriefRefinement, BriefRefinement2:
		return false
	}
	return false
}

// phaseForBriefKind maps a BriefKind to the Phase it belongs to, so the
// dispatch handler can refuse a build-subagent-prompt call whose
// briefKind doesn't match the session's current phase (run-52 Fix 2 —
// the keystone that closes the run-51 dispatch-before-enter cascade).
//
// The mapping is 1:1 except for the two kinds that share a phase:
// claudemd-author dispatches alongside codebase-content under
// PhaseCodebaseContent, and refinement2 is the cross-surface audit pass
// that runs during PhaseRefinement (there is no PhaseRefinement2). The
// bool return is false for unknown kinds so the caller can no-op rather
// than refuse a kind it can't classify.
func phaseForBriefKind(kind BriefKind) (Phase, bool) {
	switch kind {
	case BriefScaffold:
		return PhaseScaffold, true
	case BriefFeature:
		return PhaseFeature, true
	case BriefCodebaseContent, BriefClaudeMDAuthor:
		return PhaseCodebaseContent, true
	case BriefEnvContent:
		return PhaseEnvContent, true
	case BriefFinalize:
		return PhaseFinalize, true
	case BriefRefinement, BriefRefinement2:
		return PhaseRefinement, true
	}
	return "", false
}

func buildBriefForKind(plan *Plan, parent *ParentRecipe, kind BriefKind, cb Codebase, mountRoot string, facts []FactRecord, featurePass FeaturePass) (Brief, error) {
	switch kind {
	case BriefScaffold:
		var resolver *Resolver
		if mountRoot != "" {
			resolver = &Resolver{MountRoot: mountRoot}
		}
		return BuildScaffoldBriefWithResolver(plan, cb, parent, resolver)
	case BriefFeature:
		return BuildFeatureBrief(plan, featurePass)
	case BriefFinalize:
		return BuildFinalizeBrief(plan)
	case BriefCodebaseContent:
		return BuildCodebaseContentBrief(plan, cb, parent, facts)
	case BriefClaudeMDAuthor:
		return BuildClaudeMDBrief(plan, cb)
	case BriefEnvContent:
		return BuildEnvContentBrief(plan, parent, facts)
	case BriefRefinement:
		// Run-17 §9 — runDir is what `renderRefinementStitchedPointerBlock`
		// uses to render the per-tier / per-codebase Read-targets the
		// sub-agent inspects (`<runDir>/README.md`,
		// `<runDir>/environments/<tier-folder>/README.md`, …). Production
		// dispatch threads `Session.OutputRoot` here via
		// `workflow.go::Session.BuildBrief` (single-file path) and via
		// `buildSubagentDispatchForPhase` (multi-file path). This static
		// composition entry has no outputRoot in scope — `buildBriefForKind`
		// is called from the legacy single-file `buildSubagentPromptForPhase`
		// path used by unit tests and by `cmd/zcp-recipe-sim` (which calls
		// `BuildRefinementBrief` directly with its own absDir). Empty
		// runDir here suppresses the stitched-output pointer block;
		// the brief still carries atoms + rubric.
		return BuildRefinementBrief(plan, parent, "", facts)
	case BriefRefinement2:
		// Run-41 — same outputRoot story as BriefRefinement.
		// Production dispatch threads Session.OutputRoot via
		// Session.BuildBrief. This static-composition entry has no
		// outputRoot in scope; empty runDir suppresses the stitched-
		// output pointer block (audit checklist still lands).
		return BuildRefinement2Brief(plan, parent, "", facts)
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
	case BriefRefinement2:
		fmt.Fprintf(b, "You are the refinement-2 (cross-surface audit) sub-agent for the %s recipe.\n", plan.Slug)
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
	b.WriteString("It is NOT a shell command. There is no Skill named `zerops_recipe` —\n")
	b.WriteString("calling it via the Skill tool will fail with `Unknown skill`. The\n")
	b.WriteString("brief uses backtick shorthand `zerops_recipe action=X slug=Y` to\n")
	b.WriteString("refer to an MCP invocation; do not run it via Bash.\n\n")
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

	// Run-32 F-53 — recipe-authoring MCP scope. Pre-empts sub-agents
	// reaching for `zerops_workflow workflow=develop` (porter-facing
	// dev-loop runner) from inside recipe authoring. The MCP tool
	// description at `internal/tools/workflow.go` advertises `develop`
	// as a public workflow visible to any agent; the brief never names
	// the boundary between recipe-authoring tools and porter-facing
	// tools. Run-32 feature-backend hit `PREREQUISITE_MISSING` on a
	// `zerops_workflow workflow=develop` call.
	b.WriteString("\n## Recipe-authoring MCP scope\n\n")
	b.WriteString("The recipe-authoring MCP is `zerops_recipe` only. Use it for fragment\n")
	b.WriteString("recording (`record-fragment`), fact recording (`record-fact`), phase\n")
	b.WriteString("progression (`complete-phase`), self-validation, and dispatch helpers.\n")
	b.WriteString("Other `mcp__zerops__*` tools are platform / porter facing:\n\n")
	b.WriteString("- `zerops_import` / `zerops_subdomain` / `zerops_env` / `zerops_logs` /\n")
	b.WriteString("  `zerops_events` / `zerops_deploy` / `zerops_dev_server` — platform\n")
	b.WriteString("  operations the recipe needs, allowed.\n")
	b.WriteString("- `zerops_workflow` (any `workflow=` value: `develop`, `bootstrap`,\n")
	b.WriteString("  `adopt`) — porter-facing high-level workflow runner. Recipe-authoring\n")
	b.WriteString("  agents do NOT invoke this; if you find yourself reaching for\n")
	b.WriteString("  `workflow=develop`, you're trying to run the porter's dev loop, which\n")
	b.WriteString("  is not a recipe-authoring action.\n")
	b.WriteString("- `zerops_browser` — browser walk + screenshot for feature-phase\n")
	b.WriteString("  verification, allowed.\n\n")
	b.WriteString("When unsure, `zerops_recipe` is the recipe-authoring tool; everything\n")
	b.WriteString("else is platform infrastructure or porter-facing.\n")

	// Run-46 dogfood — the scaffold-phase main agent picked
	// `subagent_type="claude"` on its dispatch (FleetView system-reminder
	// frames it as the default when no agent name is typed). The Claude
	// Code 2.1.119 VSCode-native harness defaults that subagent_type to
	// worktree isolation; the worktree-create then fails on the recipe-
	// authoring outputRoot (`/var/www/zcprecipator/<slug>/`) because that
	// path isn't a git repo (`Cannot create agent worktree: not in a git
	// repository and no WorktreeCreate hooks are configured.`). Run-45
	// happened to pick `general-purpose` autonomously and shipped; the
	// engine never told either run which type to use — relied on luck.
	// Recipe-authoring sub-agents share fragment-store + facts.jsonl +
	// `.refinement-2-manifest.json` state with the main agent via the
	// `zerops_recipe` MCP; isolated worktrees break that shared state
	// outright. `general-purpose` has the same tool surface (`*`) and
	// runs inline. Make the directive explicit so the next dispatch can't
	// silently land on the worktree-isolated default.
	b.WriteString("\n## Sub-agent dispatch — subagent_type\n\n")
	b.WriteString("Agent-tool dispatch MUST pass `subagent_type=\"general-purpose\"`.\n")
	b.WriteString("Do NOT use `subagent_type=\"claude\"` even when FleetView frames\n")
	b.WriteString("it as the default — that subagent_type defaults to worktree\n")
	b.WriteString("isolation, which fails on the non-git recipe-authoring\n")
	b.WriteString("outputRoot. Sub-agents share fragment-store + facts.jsonl state\n")
	b.WriteString("with this main agent via `zerops_recipe`; isolated worktrees\n")
	b.WriteString("would break that shared state. `general-purpose` has the same\n")
	b.WriteString("tool surface (`*`) and runs inline.\n")
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
		b.WriteString("When you've refined every fragment where you can cite the\n")
		b.WriteString("violated rule (from `derived_rules.md`), the exact fragment,\n")
		b.WriteString("and the preserving edit, terminate. The main agent then\n")
		b.WriteString("dispatches the refinement-2 (cross-surface audit) sub-agent\n")
		b.WriteString("via `build-subagent-prompt briefKind=refinement2`, and only\n")
		b.WriteString("after BOTH sub-agents have run does `complete-phase\n")
		b.WriteString("phase=refinement` close the run. Each `record-fragment\n")
		b.WriteString("mode=replace` you fire at this phase is wrapped in a\n")
		b.WriteString("snapshot/restore primitive — if a post-replace validator\n")
		b.WriteString("surfaces a new violation, the engine reverts the fragment\n")
		b.WriteString("to its pre-refinement body. Per-fragment edit cap is 1\n")
		b.WriteString("attempt; do NOT loop.\n")
	case BriefRefinement2:
		b.WriteString("When you've walked every defect class in `audit_checklist.md`\n")
		b.WriteString("against the full set of stitched surfaces and emitted the\n")
		b.WriteString("fenced JSON findings block, terminate. You do NOT call\n")
		b.WriteString("`record-fragment` or `complete-phase` — diagnosis-only.\n")
		b.WriteString("The main agent reads the findings block, applies fixes\n")
		b.WriteString("per-finding (or accepts as known), and then closes\n")
		b.WriteString("refinement via `complete-phase phase=refinement`.\n")
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
