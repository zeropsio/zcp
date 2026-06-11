package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/sync"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handlePortCapture is Stage B (Phase 4) — capture + publish of a measured port.
// It is gated on a feasible, harden-scored FitCeiling: the loop must have reached
// a working, scored deployment first (action="harden" sets ps.FitCeiling). The
// handler converts the PortSession's working topology + measured FitCeiling into
// a recipe.Plan (the curated-recipe shape — a single self-referential glue-repo
// snapshot, NOT a framework scaffold), emits the HONORED-tier subset to the
// recipe output dir via the existing recipe emit verbs, authors the recipe-level
// rich-content fragments (description / features / takeover-guide / knowledge-
// base) from the FitCeiling evidence into the app README, and drives the curated
// two-channel publish (app/glue → zerops-recipe-apps; envs + metadata →
// zeropsio/recipes) via internal/sync.
//
// Channel (NOT the self-service .zerops-recipe/ folder — that's a separate
// channel): app → zerops-recipe-apps/<slug> (CreateRecipeRepo + PushAppSource),
// recipe envs → zeropsio/recipes (PublishRecipe --software). The rich content is
// fragments (paired #ZEROPS_EXTRACT_START:NAME# markers) in the app README, the
// SAME machinery framework recipes use (recipe.SubstituteFragmentMarkers).
//
// OQ-1 defer: when FitCeiling.GlueRepo.BuildFromGitReady is false the glue repo
// isn't committed/reachable yet, so a published import's buildFromGit would not
// resolve. The handler EMITS the recipe output but DEFERS the publish, saying so
// — it never fails on the OQ-1 unresolved case.
//
// Publishes are dry-run-able (dryRun flag threaded from the agent input) so the
// path is exercised end-to-end in tests without a live GitHub write.
func handlePortCapture(input WorkflowInput, stateDir string) *mcp.CallToolResult {
	if stateDir == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Port workflow requires a state directory",
			"Ensure ZCP is configured with a state directory before capturing the port."), WithRecoveryStatus())
	}

	ps, err := workflow.LoadPortSession(stateDir, os.Getpid())
	if err != nil {
		return convertError(err, WithRecoveryStatus())
	}
	if ps == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			"No active port session for this process",
			`Start the port flow with action="start" workflow="port", run the deploy-debug loop, then action="harden" to score the FitCeiling before action="capture".`), WithRecoveryStatus())
	}

	// GATE — capture is feasible only on a harden-scored, feasible FitCeiling.
	if ps.FitCeiling == nil || !ps.FitCeiling.Feasible {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Port capture requires a feasible, scored FitCeiling",
			`Run action="harden" workflow="port" with portRubric={...} to measure the FitCeiling first. Capture refuses an infeasible port (the build/boot/serve gate did not pass) — read whatDoesnt + the honored-tier reasons, fix via action="iterate", then re-harden.`), WithRecoveryStatus())
	}
	fc := *ps.FitCeiling

	plan := portSessionToPlan(ps, fc)

	// Convention: stateDir = {projectRoot}/.zcp/state/. The recipe output lands
	// under the state tree (port-recipes/<slug>) so it never pollutes the repo;
	// projectRoot is recovered two levels up for sync.LoadConfig (.sync.yaml).
	projectRoot := portProjectRootFromStateDir(stateDir)
	outputDir := filepath.Join(stateDir, "port-recipes", plan.Slug)
	emitted, emitErr := emitPortRecipe(plan, fc, outputDir)
	if emitErr != nil {
		return convertError(emitErr, WithRecoveryStatus())
	}

	resp := map[string]any{
		"status":      "port-capture",
		"phase":       string(workflow.PhasePortActive),
		"slug":        plan.Slug,
		"outputDir":   outputDir,
		"emitted":     emitted,
		"glueRepoUrl": plan.GlueRepoURL,
	}

	// OQ-1 defer: glue repo not buildFromGit-ready → emit only, defer publish.
	if !fc.GlueRepo.BuildFromGitReady {
		resp["published"] = false
		resp["deferred"] = true
		resp["guidance"] = "Port recipe EMITTED to outputDir, but the publish is DEFERRED: FitCeiling.glueRepo.buildFromGitReady is false, so the glue repo isn't committed/reachable yet and a published import's buildFromGit would not resolve. Commit the glue zerops.yaml to " + plan.GlueRepoURL + " (chain via git-push-setup), set buildFromGitReady, re-harden, then re-run action=\"capture\". Nothing was pushed."
		return jsonResult(resp)
	}

	pub := portPublisher(projectRoot, plan, fc, outputDir, bool(input.PortPublishDryRun))
	resp["published"] = true
	resp["publish"] = pub
	resp["dryRun"] = bool(input.PortPublishDryRun)
	resp["guidance"] = "Port recipe captured + published. App/glue → " + pub.AppRepo + " (zerops-recipe-apps); recipe envs + metadata → the recipes catalog. The published tiers are the HONEST honored subset of the FitCeiling — excluded tiers were dropped with their reason. Review the PR(s) before merge."
	return jsonResult(resp)
}

// portSessionToPlan converts a measured PortSession into the recipe.Plan the
// emit verbs consume. The result is the CURATED-recipe shape: a single
// self-referential glue-repo snapshot (Plan.GlueRepoURL set, so every runtime +
// utility service's buildFromGit points at the one port glue repo — the D6
// override), NOT a framework scaffold (no dev/stage pairs, no per-codebase repos).
//
//   - Slug derives from the target (lowercased, slug-safe).
//   - Each PortPlan runtime becomes a Codebase; the FIRST runtime serves HTTP
//     (RoleAPI), the rest are workers/services. BaseRuntime is the runtime token
//     verbatim (it came from the agent's research, already platform-valid).
//   - Each MANAGED dependency becomes a Service: object-storage → ServiceKindStorage,
//     everything else → ServiceKindManaged (topology classifies). Self-run /
//     constraint deps are NOT emitted as managed services (they ride the glue repo
//     or surface as constraints) — only the managed catalog maps cleanly.
func portSessionToPlan(ps *workflow.PortSession, fc workflow.FitCeiling) *recipe.Plan {
	slug := portSlug(ps.Plan.Target)
	plan := &recipe.Plan{
		Slug:        slug,
		Name:        portHumanName(ps.Plan.Target),
		GlueRepoURL: fc.GlueRepo.URL,
		Research: recipe.ResearchResult{
			Description: portShortDescription(ps.Plan, fc),
		},
	}

	for i, rt := range ps.Plan.Runtimes {
		host := slug
		if len(ps.Plan.Runtimes) > 1 {
			host = fmt.Sprintf("%s-%d", slug, i+1)
		}
		role := recipe.RoleAPI // first runtime serves HTTP (C3 was honored)
		if i > 0 {
			role = recipe.RoleWorker
		}
		plan.Codebases = append(plan.Codebases, recipe.Codebase{
			Hostname:    host,
			Role:        role,
			BaseRuntime: rt,
		})
	}

	for _, dep := range ps.Plan.Dependencies {
		if dep.Mapping != workflow.DepMappingManaged || dep.ManagedType == "" {
			continue
		}
		svc := recipe.Service{
			Hostname: portDepHostname(dep),
			Type:     dep.ManagedType,
			// MEASURED topology: SupportsHA reflects what the port actually proved
			// ran HA (fc.AchievableHA.ManagedHADeps), and ModeMeasured opts the
			// emitter out of the capability-table fallback so an unmeasured dep is
			// NOT force-promoted to HA at tier 5 (the honest-port contract).
			SupportsHA:   managedSupportsHA(fc, dep.ManagedType),
			ModeMeasured: true,
		}
		if topology.IsObjectStorageType(dep.ManagedType) || topology.IsSharedStorageType(dep.ManagedType) {
			svc.Kind = recipe.ServiceKindStorage
		} else {
			svc.Kind = recipe.ServiceKindManaged
		}
		plan.Services = append(plan.Services, svc)
	}
	return plan
}

// managedSupportsHA reports whether the FitCeiling's AchievableHA names the
// managed type among the HA-running deps — the honest, MEASURED HA fact, not a
// family-table assumption. Used so the emitted tier-5 yaml flips a dep to HA mode
// only when the port actually proved it ran HA.
func managedSupportsHA(fc workflow.FitCeiling, managedType string) bool {
	for _, ha := range fc.AchievableHA.ManagedHADeps {
		if ha == managedType || topology.CanonicalBaseName(ha) == topology.CanonicalBaseName(managedType) {
			return true
		}
	}
	return false
}

// portEmittedTier is one honored tier's emitted artifact paths.
type portEmittedTier struct {
	Level      int    `json:"level"`
	Label      string `json:"label"`
	ImportYAML string `json:"importYaml"`
	Readme     string `json:"readme"`
}

// emitPortRecipe writes the HONORED-tier subset of the FitCeiling to outputDir —
// `environments/<order> — <Name>/import.yaml` + a per-env README — via the
// existing recipe emit verbs (EmitDeliverableYAML + recipe.TierAt), and writes
// the app README carrying the recipe-level rich-content fragments. Only honored
// tiers are emitted (the honest-subset contract — "don't ship a tier whose guide
// you can't honor"). Returns the per-tier emitted-path list + the app README path.
func emitPortRecipe(plan *recipe.Plan, fc workflow.FitCeiling, outputDir string) (map[string]any, error) {
	envRoot := filepath.Join(outputDir, "environments")
	var tiers []portEmittedTier

	for _, ht := range fc.HonoredTiers {
		if !ht.Honored {
			continue
		}
		level := int(ht.Level)
		tier, ok := recipe.TierAt(level)
		if !ok {
			return nil, fmt.Errorf("port capture: honored tier %d has no recipe.Tier", level)
		}
		yaml, err := recipe.EmitDeliverableYAML(plan, level)
		if err != nil {
			return nil, fmt.Errorf("port capture: emit tier %d: %w", level, err)
		}
		tierDir := filepath.Join(envRoot, tier.Folder)
		if mkErr := os.MkdirAll(tierDir, 0o755); mkErr != nil {
			return nil, fmt.Errorf("port capture: mkdir %q: %w", tierDir, mkErr)
		}
		importPath := filepath.Join(tierDir, "import.yaml")
		if wErr := os.WriteFile(importPath, []byte(yaml), 0o600); wErr != nil {
			return nil, fmt.Errorf("port capture: write import.yaml: %w", wErr)
		}
		readmePath := filepath.Join(tierDir, "README.md")
		readme := portEnvReadme(plan, tier)
		if wErr := os.WriteFile(readmePath, []byte(readme), 0o600); wErr != nil {
			return nil, fmt.Errorf("port capture: write env README: %w", wErr)
		}
		tiers = append(tiers, portEmittedTier{
			Level: level, Label: ht.Label, ImportYAML: importPath, Readme: readmePath,
		})
	}

	appReadmePath := filepath.Join(outputDir, "README.md")
	appReadme, missing, err := portAppReadme(plan, fc)
	if err != nil {
		return nil, fmt.Errorf("port capture: assemble app README: %w", err)
	}
	if wErr := os.WriteFile(appReadmePath, []byte(appReadme), 0o600); wErr != nil {
		return nil, fmt.Errorf("port capture: write app README: %w", wErr)
	}

	return map[string]any{
		"tiers":            tiers,
		"appReadme":        appReadmePath,
		"missingFragments": missing,
	}, nil
}

// portAppReadme renders the curated app-repo README carrying the recipe-level
// rich-content fragments — the SAME paired-marker machinery framework recipes
// use (recipe.SubstituteFragmentMarkers). The fragment bodies
// (description / features / takeover-guide / knowledge-base) are authored HERE
// from the measured FitCeiling evidence (whatRuns / whatDoesnt / honored tiers /
// achievable HA / unresolved constraints) so the published recipe carries the
// honest port story, not a fabricated framework narrative. Returns the rendered
// body + any markers whose fragment body was empty (none in practice — the
// handler authors all four).
func portAppReadme(plan *recipe.Plan, fc workflow.FitCeiling) (string, []string, error) {
	const tpl = "# " + "{NAME}" + "\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:description# -->\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:description# -->\n\n" +
		"## Features\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:features# -->\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:features# -->\n\n" +
		"## Takeover guide\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:takeover-guide# -->\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:takeover-guide# -->\n\n" +
		"## Knowledge base\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n" +
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"

	body := strings.ReplaceAll(tpl, "{NAME}", plan.Name)
	fragments := map[string]string{
		"root/description":    portFragmentDescription(plan, fc),
		"root/features":       portFragmentFeatures(fc),
		"root/takeover-guide": portFragmentTakeoverGuide(plan, fc),
		"root/knowledge-base": portFragmentKnowledgeBase(fc),
	}
	return recipe.SubstituteFragmentMarkers(body, fragments, "root")
}

// portPublishFunc is the curated-publish seam. The default
// (livePortPublishRecipe) drives the real internal/sync lifecycle; tests swap it
// for a hermetic stub so they never make a live `gh` call (the real sync funcs
// hit GitHub for existence/template reads even in dry-run). Initialized var —
// allowed by TestNoCrossCallHandlerState (it carries no per-call state; tests
// restore it via t.Cleanup).
type portPublishFunc func(projectRoot string, plan *recipe.Plan, fc workflow.FitCeiling, outputDir string, dryRun bool) portPublishResult

var portPublisher portPublishFunc = livePortPublishRecipe

// livePortPublishRecipe drives the curated two-channel publish via internal/sync.
// dryRun threads end-to-end so the path is exercised without a live write.
func livePortPublishRecipe(projectRoot string, plan *recipe.Plan, fc workflow.FitCeiling, outputDir string, dryRun bool) portPublishResult {
	cfg, cfgErr := sync.LoadConfig(projectRoot)
	if cfgErr != nil {
		return portPublishResult{Error: cfgErr.Error()}
	}
	out := portPublishResult{
		AppRepo: cfg.Push.Recipes.Org + "/" + plan.Slug + "-app",
		DryRun:  dryRun,
	}

	create, err := sync.CreateRecipeRepo(cfg, plan.Slug, "app", dryRun)
	if err != nil {
		out.Error = "create-repo: " + err.Error()
		return out
	}
	out.CreateRepo = create.Status.String()

	// PushAppSource needs a .git in the source dir; in dry-run it never touches
	// the dir, so the emitted outputDir is a safe stand-in for the glue source.
	push, err := sync.PushAppSource(cfg, plan.Slug, "app", outputDir, dryRun)
	if err != nil {
		out.Error = "push-app: " + err.Error()
		return out
	}
	out.PushApp = push.Status.String()

	publish, err := sync.PublishRecipe(cfg, plan.Slug, filepath.Join(outputDir, "environments"), sync.PublishOpts{
		PrettyName:  plan.Name,
		Software:    plan.Name,
		Description: portShortDescription(toPortPlan(plan), fc),
		Tags:        portTags(plan),
	}, dryRun)
	if err != nil {
		out.Error = "publish: " + err.Error()
		return out
	}
	out.Publish = publish.Status.String()
	out.PublishDiff = publish.Diff
	return out
}

// portPublishResult is the agent-facing publish summary.
type portPublishResult struct {
	AppRepo     string `json:"appRepo"`
	DryRun      bool   `json:"dryRun"`
	CreateRepo  string `json:"createRepo,omitempty"`
	PushApp     string `json:"pushApp,omitempty"`
	Publish     string `json:"publish,omitempty"`
	PublishDiff string `json:"publishDiff,omitempty"`
	Error       string `json:"error,omitempty"`
}

// --- pure rendering helpers (fragment bodies + slug/name shaping) ---

// portSlug lowercases + slug-safes the OSS target name.
func portSlug(target string) string {
	s := strings.ToLower(strings.TrimSpace(target))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// portHumanName title-cases the slug-stem for the recipe display name.
func portHumanName(target string) string {
	t := strings.TrimSpace(target)
	if t == "" {
		return ""
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

// portDepHostname derives a managed service hostname from a dependency. Uses the
// declared token (e.g. "postgresql" → "db" is too lossy; keep the family stem so
// `${<host>_*}` refs are legible).
func portDepHostname(dep workflow.PortDependency) string {
	base := dep.Declared
	if base == "" {
		base = dep.ManagedType
	}
	return portSlug(base)
}

// portShortDescription is the one-line recipe description (Strapi + intro).
func portShortDescription(plan workflow.PortPlan, fc workflow.FitCeiling) string {
	return fmt.Sprintf("%s, ported to Zerops — runs at up to the %s tier.",
		portHumanName(plan.Target), highestHonoredLabel(fc))
}

// portFragmentDescription authors the recipe-level description fragment from the
// measured FitCeiling — what the port actually runs, honestly.
func portFragmentDescription(plan *recipe.Plan, fc workflow.FitCeiling) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s, ported to run on Zerops. ", plan.Name)
	if len(fc.WhatRuns) > 0 {
		fmt.Fprintf(&b, "Verified working: %s. ", strings.Join(fc.WhatRuns, "; "))
	}
	fmt.Fprintf(&b, "The highest honored deployment tier is **%s**.", highestHonoredLabel(fc))
	return b.String()
}

// portFragmentFeatures lists the honored deployment tiers as the recipe's
// features (the honest-subset contract — only tiers whose rubric prerequisites
// were met).
func portFragmentFeatures(fc workflow.FitCeiling) string {
	var b strings.Builder
	for _, ht := range fc.HonoredTiers {
		if !ht.Honored {
			continue
		}
		fmt.Fprintf(&b, "- **%s** — deployment tier honored.\n", ht.Label)
	}
	if b.Len() == 0 {
		return "No deployment tier was honored."
	}
	return strings.TrimRight(b.String(), "\n")
}

// portFragmentTakeoverGuide explains what a porter is taking over — the working
// topology + the excluded tiers (with reasons) so they know the ceiling.
func portFragmentTakeoverGuide(plan *recipe.Plan, fc workflow.FitCeiling) string {
	var b strings.Builder
	b.WriteString("This recipe is a port of foreign OSS — the glue repo carries the build/run wiring.\n\n")
	b.WriteString("**Working topology:**\n")
	for _, cb := range plan.Codebases {
		fmt.Fprintf(&b, "- runtime `%s` (%s)\n", cb.Hostname, cb.BaseRuntime)
	}
	for _, svc := range plan.Services {
		fmt.Fprintf(&b, "- managed `%s` (%s)\n", svc.Hostname, svc.Type)
	}
	excluded := excludedTiers(fc)
	if len(excluded) > 0 {
		b.WriteString("\n**Excluded tiers (not honored):**\n")
		for _, ht := range excluded {
			fmt.Fprintf(&b, "- %s — %s\n", ht.Label, ht.Reason)
		}
	}
	if note := hardBandChoreographyNote(fc); note != "" {
		b.WriteString("\n**Cross-service init ordering (HARD band):** ")
		b.WriteString(note)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// portChoreographyNote is the retry-until-ready cross-service init-ordering note
// the HARD-band honesty content surfaces. It mirrors the recon constraint
// (workflow.constraintCrossServiceOrdering) without importing an unexported
// const: a HARD-band port's working deployment depends on this idiom, so the
// porter must know it even when the loop did not record an explicit ordering
// constraint (the band itself is the honesty trigger). Plan §12.3 + §8.
const portChoreographyNote = "Zerops has no native readiness barrier between services. Cross-service init steps (e.g. a DB-init → migrate → downstream-migration chain) are sequenced with `zsc execOnce <key> --retryUntilSuccessful`, and `zsc scale ram max` pre-boosts RAM so a startup spike doesn't OOM before the autoscaler reacts. This is the in-band choreography the deployment relies on — re-establish it if you change the topology."

// hardBandChoreographyNote returns the retry-until-ready choreography note for a
// HARD-band FitCeiling, or "" otherwise. HARD band is the honesty trigger: the
// note is surfaced regardless of whether an explicit ordering constraint was
// recorded (the recon cross-service-ordering axis records one when the agent
// flagged it, but many-runtime HARD ports rely on the same idiom without it).
func hardBandChoreographyNote(fc workflow.FitCeiling) string {
	if fc.Band != workflow.BandHard {
		return ""
	}
	return portChoreographyNote
}

// portFragmentKnowledgeBase surfaces the honesty residue — the loop-discovered
// unresolved constraints (the HARD-band no-signal class) + the throughput-vs-HA
// breakdown — so the porter knows what's fragile.
func portFragmentKnowledgeBase(fc workflow.FitCeiling) string {
	var b strings.Builder
	if note := hardBandChoreographyNote(fc); note != "" {
		b.WriteString("**Cross-service init ordering (HARD band):** ")
		b.WriteString(note)
		b.WriteString("\n\n")
	}
	if len(fc.UnresolvedConstraints) > 0 {
		b.WriteString("**Unresolved constraints discovered during the port:**\n")
		for _, c := range fc.UnresolvedConstraints {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteByte('\n')
	}
	if fc.AchievableHA.AppContainers > 0 {
		fmt.Fprintf(&b, "App runtime scaled to %d container(s) during the HA probe (throughput axis).\n",
			fc.AchievableHA.AppContainers)
	}
	if len(fc.AchievableHA.ManagedHADeps) > 0 {
		fmt.Fprintf(&b, "Managed dependencies proven HA-replicated: %s.\n",
			strings.Join(fc.AchievableHA.ManagedHADeps, ", "))
	}
	if len(fc.AchievableHA.NotHA) > 0 {
		fmt.Fprintf(&b, "Surfaces that could NOT reach HA replication: %s.\n",
			strings.Join(fc.AchievableHA.NotHA, ", "))
	}
	if b.Len() == 0 {
		return "No unresolved constraints recorded; the port reached its measured ceiling cleanly."
	}
	return strings.TrimRight(b.String(), "\n")
}

// portEnvReadme renders a per-tier env README. Minimal + honest — names the tier
// (only honored tiers reach this, so it is honored by construction).
func portEnvReadme(plan *recipe.Plan, tier recipe.Tier) string {
	return fmt.Sprintf("# %s — %s\n\nThe **%s** deployment tier of the %s port. This tier's rubric prerequisites were met during the port.\n",
		plan.Name, tier.Label, tier.Label, plan.Name)
}

// portTags derives recipe tags from the runtimes (the base-runtime family stems).
func portTags(plan *recipe.Plan) string {
	seen := map[string]bool{}
	var tags []string
	for _, cb := range plan.Codebases {
		fam := cb.BaseRuntime
		if i := strings.IndexByte(fam, '@'); i > 0 {
			fam = fam[:i]
		}
		if fam == "" || seen[fam] {
			continue
		}
		seen[fam] = true
		tags = append(tags, fam)
	}
	sort.Strings(tags)
	return strings.Join(tags, ",")
}

// excludedTiers returns the not-honored ladder rungs in order.
func excludedTiers(fc workflow.FitCeiling) []workflow.HonoredTier {
	var out []workflow.HonoredTier
	for _, ht := range fc.HonoredTiers {
		if !ht.Honored {
			out = append(out, ht)
		}
	}
	return out
}

// highestHonoredLabel returns the label of the highest honored tier, or a
// no-tier marker when none is honored (should not happen past the feasible gate).
func highestHonoredLabel(fc workflow.FitCeiling) string {
	label := "none"
	for _, ht := range fc.HonoredTiers {
		if ht.Honored {
			label = ht.Label
		}
	}
	return label
}

// toPortPlan re-derives a minimal PortPlan from the recipe.Plan for the
// description helper (which is keyed on the OSS target name the plan carries as
// its human name). Kept tiny — the description only needs Target.
func toPortPlan(plan *recipe.Plan) workflow.PortPlan {
	return workflow.PortPlan{Target: plan.Name}
}

// portProjectRootFromStateDir recovers the project root from the state dir per
// the {projectRoot}/.zcp/state/ convention (two levels up). Used to locate
// .sync.yaml for sync.LoadConfig. Falls back to "." when stateDir is too shallow
// (LoadConfig then uses DefaultConfig — the pinned curated orgs).
func portProjectRootFromStateDir(stateDir string) string {
	if stateDir == "" {
		return "."
	}
	// .zcp/state → up two for the project root.
	root := filepath.Dir(filepath.Dir(stateDir))
	if root == "" || root == "." || root == string(filepath.Separator) {
		return "."
	}
	return root
}
