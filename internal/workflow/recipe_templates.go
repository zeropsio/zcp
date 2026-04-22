package workflow

import (
	"fmt"
	"strings"
)

// RecipeAppRepoBase is the GitHub org where recipe app repos live.
const RecipeAppRepoBase = "https://github.com/zerops-recipe-apps/"

// Environment tier definitions with folder names (em-dash U+2014).
// IntroLabel is sentence-cased with acronyms preserved (used in extract bold text).
var envTiers = []struct {
	Index      int
	Folder     string
	Suffix     string
	Label      string
	IntroLabel string
}{
	{0, "0 \u2014 AI Agent", "agent", "AI Agent", "AI agent"},
	{1, "1 \u2014 Remote (CDE)", "remote", "Remote (CDE)", "Remote (CDE)"},
	{2, "2 \u2014 Local", "local", "Local", "Local"},
	{3, "3 \u2014 Stage", "stage", "Stage", "Stage"},
	{4, "4 \u2014 Small Production", "small-prod", "Small Production", "Small production"},
	{5, "5 \u2014 Highly-available Production", "ha-prod", "Highly-available Production", "Highly-available production"},
}

// EnvTierCount returns the number of environment tiers.
func EnvTierCount() int { return len(envTiers) }

// EnvFolder returns the folder name for an environment index.
func EnvFolder(envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}
	return envTiers[envIndex].Folder
}

// CanonicalEnvFolders returns the six tier folder names in order
// (0 — AI Agent through 5 — Highly-available Production). Exported
// so the atom render path (LoadAtomBodyRendered) can populate
// `{{.EnvFolders}}` references without importing envTiers directly.
// The analyze harness mirrors this list at
// internal/analyze.CanonicalEnvFolders so external tooling stays in
// sync; changing the list here requires updating that copy.
func CanonicalEnvFolders() []string {
	out := make([]string, len(envTiers))
	for i := range envTiers {
		out[i] = envTiers[i].Folder
	}
	return out
}

// BuildFinalizeOutput generates all recipe repo files and returns them as a map.
// Keys are relative paths (e.g., "0 — AI Agent/import.yaml").
// Values are file content strings.
func BuildFinalizeOutput(plan *RecipePlan) map[string]string {
	files := make(map[string]string)

	// Main recipe README (for zeropsio/recipes).
	files["README.md"] = GenerateRecipeREADME(plan)

	// Per-environment files (for zeropsio/recipes).
	for i := range envTiers {
		folder := envTiers[i].Folder
		files[folder+"/import.yaml"] = GenerateEnvImportYAML(plan, i)
		files[folder+"/README.md"] = GenerateEnvREADME(plan, i)
	}

	// Per-codebase README scaffolds. A target owns its own README iff EITHER:
	//   (a) it's a non-worker runtime target, OR
	//   (b) it's a worker with SharesCodebaseWith == "" (separate codebase).
	// Shared-codebase workers (SharesCodebaseWith set) don't get their own
	// README — the host target owns it. This matches the "codebase count
	// rule" used by the multi-repo publish flow — see
	// docs/implementation-multi-repo-publish.md.
	for _, target := range plan.Targets {
		if !IsRuntimeType(target.Type) {
			continue
		}
		if target.IsWorker && target.SharesCodebaseWith != "" {
			continue
		}
		files[target.Hostname+"dev/README.md"] = GenerateAppREADME(plan)
	}

	return files
}

// GenerateRecipeREADME returns the main recipe README.md content.
// Matches the format used by zeropsio/recipes (with ZEROPS_EXTRACT markers,
// deploy button, cover image, and deploy-with-one-click links for each environment).
func GenerateRecipeREADME(plan *RecipePlan) string {
	var b strings.Builder

	title := titleCase(plan.Framework)
	pretty := recipePrettyName(plan.Slug, plan.Framework)
	fmt.Fprintf(&b, "# %s %s Recipe\n\n", title, pretty)

	// Intro with extract markers — list ALL managed/utility services, not just DB.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:intro# -->\n")
	fmt.Fprintf(&b, "A [%s](%s) application", title, frameworkURL(plan.Framework))
	if svcList := recipeIntroServiceList(plan); svcList != "" {
		fmt.Fprintf(&b, " %s,", svcList)
	}
	b.WriteString(" running on [Zerops](https://zerops.io) with six ready-made environment configurations")
	b.WriteString(" \u2014 from AI agent and remote development to stage and highly-available production.\n")
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:intro# -->\n\n")

	// Deploy button and cover image.
	b.WriteString("\u2b07\ufe0f **Full recipe page and deploy with one-click**\n\n")
	fmt.Fprintf(&b, "[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/light/deploy-button.svg)](https://app.zerops.io/recipes/%s?environment=small-production)\n\n", plan.Slug)
	fw := strings.ToLower(plan.Framework)
	fmt.Fprintf(&b, "![%s](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-%s.svg)\n\n", fw, fw)

	// Environment list with deploy links.
	b.WriteString("Offered in examples for the whole development lifecycle")
	b.WriteString(" \u2014 from environments for AI agents like [Claude Code](https://www.anthropic.com/claude-code)")
	b.WriteString(" or [opencode](https://opencode.ai)")
	b.WriteString(" through environments for remote (CDE) or local development")
	b.WriteString(" of each developer to stage and productions of all sizes.\n\n")

	for _, env := range envTiers {
		slug := envSlugSuffix(env.Index)
		fmt.Fprintf(&b, "- **%s** [[info]](/%s) \u2014 [[deploy with one click]](https://app.zerops.io/recipes/%s?environment=%s)\n",
			env.Label,
			envFolderURLEncoded(env.Folder),
			plan.Slug,
			slug,
		)
	}

	b.WriteString("\n---\n\n")
	fmt.Fprintf(&b, "For more advanced examples see all [%s recipes](https://app.zerops.io/recipes?lf=%s) on Zerops.\n\n", title, fw)
	b.WriteString("Need help setting your project up? Join [Zerops Discord community](https://discord.gg/zeropsio).\n")

	return b.String()
}

// GenerateEnvREADME returns the README.md for a specific environment tier.
// Matches the format used by zeropsio/recipes (with ZEROPS_EXTRACT intro marker).
//
// v8.94: expanded from a 7-line intro-only boilerplate to a 40-80-line
// tier-transition teaching README. The added sections answer the four
// questions a reader considering a tier actually has:
//
//  1. "Who is this tier for?"             → Audience
//  2. "What changed from the lower tier?" → Diff-from-predecessor
//  3. "How do I move up?"                 → Promotion path
//  4. "What's special about running this tier?" → Tier-specific ops
//
// Content is derived deterministically from envTiers metadata + plan shape.
// The v28 evidence this addresses: all six env READMEs in nestjs-showcase-v28
// were 7 lines each of template boilerplate — zero tier-transition teaching.
func GenerateEnvREADME(plan *RecipePlan, envIndex int) string {
	if envIndex < 0 || envIndex >= len(envTiers) {
		return ""
	}
	env := envTiers[envIndex]
	title := titleCase(plan.Framework)
	pretty := recipePrettyName(plan.Slug, plan.Framework)
	slug := envSlugSuffix(envIndex)

	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s \u2014 %s Environment\n\n", title, pretty, env.Label)
	fmt.Fprintf(&b, "This is %s %s environment for [%s %s (info + deploy)](https://app.zerops.io/recipes/%s?environment=%s) recipe on [Zerops](https://zerops.io).\n\n",
		aOrAn(env.Label), strings.ToLower(env.Label), title, pretty, plan.Slug, slug)

	// Environment intro with extract markers.
	b.WriteString("<!-- #ZEROPS_EXTRACT_START:intro# -->\n")
	fmt.Fprintf(&b, "**%s** %s\n", env.IntroLabel, envDescription(plan, envIndex))
	b.WriteString("<!-- #ZEROPS_EXTRACT_END:intro# -->\n\n")

	// ── Tier-transition teaching sections (v8.94) ──────────────────────

	b.WriteString("## Who this is for\n\n")
	b.WriteString(envAudience(envIndex))
	b.WriteString("\n\n")

	if envIndex == 0 {
		b.WriteString("## First-tier context\n\n")
		b.WriteString("This is the entry-level tier in the six-environment lifecycle:\n\n")
		b.WriteString("- There is no lower tier to compare against.\n")
		b.WriteString("- Every service is sized for a single developer / agent working alone.\n")
		b.WriteString("- Managed services (DB, cache, broker, storage, search) run single-replica; no HA, no redundancy.\n")
		b.WriteString("- If you need higher availability, stronger DB durability, or replicated runtimes, promote to the next tier rather than hand-tuning this one.\n")
		b.WriteString("- Each tier's `import.yaml` declares a distinct `project.name`, so deploying a later-tier template creates a NEW Zerops project. Service state (DB rows, cache entries, stored files) does NOT carry across tiers by default — promote by first exporting data from this tier's project and importing into the next, or re-seed in the new project.\n\n")
	} else {
		b.WriteString("## What changes vs the adjacent tier\n\n")
		b.WriteString(envDiffFromPrevious(envIndex))
		b.WriteString("\n\n")
	}

	if envIndex == len(envTiers)-1 {
		b.WriteString("## Terminal tier\n\n")
		b.WriteString("This is the last tier in the recipe's lifecycle. There is no higher environment to promote to:\n\n")
		b.WriteString("- HA-prod is tuned for availability under rolling deploys and transient platform incidents.\n")
		b.WriteString("- Runtime `minContainers: 2` (behind the L7 balancer) — one container drains while the other serves.\n")
		b.WriteString("- DB and cache run in HA replication mode with automatic failover.\n")
		b.WriteString("- Object storage and search engine carry the redundancy their managed types provide.\n")
		b.WriteString("- If you need even more capacity, the next step is beyond recipes: a custom-sized Zerops project with autoscaling, extra services the recipe doesn't bundle, or service types the recipe template excluded.\n")
		b.WriteString("- Graduation path: take the HA-prod import.yaml as a starting point, extend it manually for the specific workload.\n\n")
	} else {
		b.WriteString("## Promoting to the next tier\n\n")
		b.WriteString(envPromotionPath(envIndex))
		b.WriteString("\n\n")
	}

	b.WriteString("## Tier-specific operational concerns\n\n")
	b.WriteString(envOperationalConcerns(plan, envIndex))
	b.WriteString("\n")

	return b.String()
}

// envAudience returns a bullet list describing who should deploy this tier.
// Audience framing is tier-indexed because the six-environment lifecycle is
// intentionally designed around audience progression (AI agent → remote dev
// → local dev → stage → small prod → HA prod).
//
// v39 Commit 1: every bullet either (a) references a field the yaml generator
// emits (`zeropsSetup: dev`, `mode: NON_HA`, `minContainers: 2`) or (c) frames
// the tier's audience/lifecycle role. Claims that would need plan/yaml fields
// that don't exist (backup policy, tier-specific healthCheck tuning,
// per-tier dev-container images) are NOT present — see v39-commit1-bullet-audit.md.
func envAudience(envIndex int) string {
	switch envIndex {
	case 0:
		return "- An AI coding agent (Claude Code, opencode, or similar) iterating on the application from **inside** the Zerops dev container via SSH.\n" +
			"- Every codebase ships with a `zerops.yaml` `setup: dev` block that boots the container idle (no-op start).\n" +
			"- The agent drives the dev server lifecycle, runs migrations and seeds ad hoc, and hot-reloads edits through the SSHFS mount.\n" +
			"- No local toolchain on the agent's host is required — everything runs on the Zerops container.\n" +
			"- Best fit: recipes where the agent's primary artifact is committed source code plus verified end-to-end behavior."
	case 1:
		return "- A human developer using a CDE (Cloud Development Environment) — SSH plus VS Code Remote, Cursor Remote, or similar.\n" +
			"- Iteration happens **inside** the same `zeropsSetup: dev` container the AI-Agent tier uses — the tier distinction is audience (human via CDE) not container image.\n" +
			"- Managed DB / cache / broker / storage / search are provisioned in the same Zerops project — the dev container reaches them as neighbors on the project network.\n" +
			"- No local Node / PHP / Python install required on the developer's machine.\n" +
			"- Best fit: onboarding new contributors, cross-platform teams, or anyone whose local machine cannot run the stack (locked-down laptops, light Chromebooks)."
	case 2:
		return "- A developer running the app **locally** (Node / PHP / Python / Go process on their workstation).\n" +
			"- Managed services live in Zerops; local app reaches them via `zcli vpn up`.\n" +
			"- Fits pair programming, offline iteration on code, and developers who prefer their own editor / debugger / OS over a remote container.\n" +
			"- Initial setup cost: installing the runtime on the local machine, then `zcli vpn up`.\n" +
			"- Best fit: established contributors with a configured local environment who want managed data services without running DB/cache/broker locally."
	case 3:
		return "- A reviewer, product manager, or QA engineer validating a pre-production build on a live subdomain.\n" +
			"- Stage runs the **same `setup: prod` zerops.yaml** as every higher tier — same build commands, same deploy files, same runtime base.\n" +
			"- Scale is intentionally lower — runtime services run single-replica (no explicit `minContainers`; platform default of 1 applies), and managed services are `mode: NON_HA`, single-replica.\n" +
			"- Best fit: gating merges on a fresh-container deploy that exercises init commands, migrations, and service wiring together."
	case 4:
		return "- A small production deployment serving real traffic:\n" +
			"  - single-tenant SaaS with moderate daily users,\n" +
			"  - internal tools for an organization,\n" +
			"  - a minimum-viable production for an early-stage product.\n" +
			"- Runtime services run at `minContainers: 2`; DB and cache remain single-replica and run in `mode: NON_HA`.\n" +
			"- Rolling deploys swap one replica at a time — `minContainers: 2` keeps one container serving while the other rolls. DB/cache remain NON_HA, so node-level failures incur downtime until the platform restarts the affected instance.\n" +
			"- Best fit: budget-constrained production where occasional node-failure downtime is tolerable but routine deploys must not drop traffic."
	case 5:
		return "- Production traffic that must survive node failures, rolling deploys, and transient platform incidents with **zero visible downtime**.\n" +
			"- Runtime runs `minContainers: 2` with `cpuMode: DEDICATED` behind the L7 balancer; the balancer drains one container while the other serves.\n" +
			"- DB and cache run at `mode: HA` with automatic failover.\n" +
			"- Object storage and search engine carry the redundancy their managed types provide.\n" +
			"- Best fit: customer-facing production that pages someone when it goes down, SaaS with SLAs, or any workload where a deploy cannot be scheduled during a quiet window."
	}
	return ""
}

// envDiffFromPrevious summarizes the concrete changes between the current
// tier's import.yaml and the preceding tier's. Framed as a short list so a
// reader comparing two env folders on GitHub can map each bullet to a
// diff hunk.
//
// v39 Commit 1: every claim about a "change" cites a field the yaml generator
// actually emits differently between the two tiers. env 3 and env 4 emit
// identical managed-service autoscaling today, so env-4 prose describes ONLY
// the real delta (`minContainers: 2` on runtime services). env 0 and env 1
// emit identical dev-slot yaml, so env-1 prose frames the distinction as
// audience, not container/image.
func envDiffFromPrevious(envIndex int) string {
	switch envIndex {
	case 1:
		return "vs the AI agent tier:\n" +
			"- `import.yaml` shape is identical — same `project.name` pattern, same service list, same `mode: NON_HA` on managed services, same `zeropsSetup: dev` on runtime containers.\n" +
			"- The tier distinction is audience: an AI agent driving the container over SSH vs a human developer driving it through a CDE.\n" +
			"- Every `zerops.yaml` setup name is identical across envs 0 and 1.\n" +
			"- Same `zerops_import` shape, same cross-service env var references — each tier is still a distinct Zerops project with its own containers and state."
	case 2:
		return "vs the remote (CDE) tier:\n" +
			"- Runtime dev services are **not deployed** — the dev container is optional at Local.\n" +
			"- The developer's local machine is the runtime; only managed services live in Zerops.\n" +
			"- Managed services reached via `zcli vpn up` — same hostnames, same credentials.\n" +
			"- Deploy cadence: no dev redeploy loop; iteration happens against a locally running app, not a Zerops container.\n" +
			"- The dev + stage runtime pair from env 1 collapses to a single runtime service at Local — same `setup: prod` build and autoscaling, but the `{host}dev` / `{host}stage` split is gone."
	case 3:
		return "vs the local tier:\n" +
			"- Dev runtime services disappear entirely — Stage is deployment-only, no iteration container.\n" +
			"- Stage runtime containers are deployed from the `setup: prod` block in `zerops.yaml` — same build + `deployFiles` + start as production.\n" +
			"- Runtime services run single-replica (no explicit `minContainers`; platform default of 1 applies).\n" +
			"- `enableSubdomainAccess: true` stays on every non-worker runtime (same as Local), but Stage is the first tier where the subdomain is the review surface reviewers actually load.\n" +
			"- Managed services stay at their tier-3 scale (`mode: NON_HA`, single-replica) — no HA here, HA arrives at env 5.\n" +
			"- `zerops.yaml` setup name and build commands are identical to every production tier — if Stage passes, the production promotion path is a `minContainers` + `mode` flip, not a code change."
	case 4:
		return "vs the stage tier:\n" +
			"- Runtime services run at `minContainers: 2` — one replica keeps serving during a rolling deploy while the other rolls.\n" +
			"- DB and cache remain `mode: NON_HA` and single-replica — node-level failures still cause a brief outage until the platform restarts the affected instance.\n" +
			"- Same `zerops.yaml`, same setup name — the only `import.yaml` delta from stage is `minContainers: 2` on runtime services."
	case 5:
		return "vs the small production tier:\n" +
			"- `minContainers: 2` is already in place at Small Production — the HA-distinct changes are `mode: HA` on DB and cache and `cpuMode: DEDICATED` on runtime services.\n" +
			"- DB moves to `mode: HA` replication with automatic failover on node failure.\n" +
			"- Cache moves to `mode: HA` replication.\n" +
			"- Object storage carries the redundancy its managed type provides.\n" +
			"- Workers add a queue-group so the second replica does NOT double-process jobs (see the worker codebase's README §Gotchas for the exact client-library flag).\n" +
			"- Zero-downtime rolling deploys depend on the application handling SIGTERM correctly — the platform drains the old container before teardown.\n" +
			"- Same application code — the tier difference is replica counts, service modes, and the `DEDICATED` cpu mode, not build commands."
	}
	return ""
}

// envPromotionPath tells the reader what to flip when moving up one tier.
// Stays prose-level (not a code snippet) because the mechanics are "copy
// the adjacent tier's import.yaml and zerops_import it" for every recipe.
//
// v39 Commit 1: every promotion-path claim about a yaml field matches what
// GenerateEnvImportYAML(plan, envIndex+1) actually emits for the target tier.
// Claims about capabilities the generator doesn't emit (backup policy,
// per-tier healthCheck tuning, managed-service sizing growth between env 3
// and env 4) are dropped — the audit established env 3 and env 4 emit
// identical autoscaling, so "sizing grows at env 4" was false.
func envPromotionPath(envIndex int) string {
	switch envIndex {
	case 0:
		return "To move from AI Agent to Remote (CDE):\n" +
			"- Deploy the `1 — Remote (CDE)/import.yaml` via the Zerops dashboard or the deploy button (this provisions a new project for the Remote tier; it does NOT modify your AI-Agent tier project).\n" +
			"- The dev container in the new project is the same `zeropsSetup: dev` shape — the tier distinction is audience (human via CDE vs AI agent over SSH), not the container image. Managed services carry the same shape too.\n" +
			"- Each tier's YAML declares a distinct `project.name` suffix (this tier's ends `-agent`, Remote's ends `-remote`). Service hostnames match by convention, but the containers are separate — if you need data carry-over, export from this tier's project before provisioning Remote; otherwise plan to re-seed.\n" +
			"- Handoff cost: minutes — no code changes, no deploy ceremony."
	case 1:
		return "From CDE to Local is usually a step sideways, not up:\n" +
			"- If you are moving from CDE iteration to local-machine iteration, deploy `2 — Local` and stop using the dev container.\n" +
			"- Run `zcli vpn up` on your local machine to reach the managed services.\n" +
			"- Run your app locally (Node / PHP / Python / Go).\n" +
			"- If you want a pre-production review environment instead, skip Local and deploy `3 — Stage`."
	case 2:
		return "From Local to Stage — your first deployment tier:\n" +
			"- Deploy the `3 — Stage` tier alongside Local.\n" +
			"- Stage runs the same `setup: prod` zerops.yaml that every higher tier uses — so the moment Stage is green, the production promotion path is just a scale-up.\n" +
			"- Your local setup continues unchanged; Stage gives you a live subdomain to share with reviewers.\n" +
			"- First deploy will exercise init commands, migrations, and service wiring end-to-end — expect a few minutes of first-boot latency."
	case 3:
		return "From Stage to Small Production:\n" +
			"- Deploy the `4 — Small Production/import.yaml`.\n" +
			"- `zerops.yaml` itself does not change — same prod setup, same build + deploy commands.\n" +
			"- The `import.yaml` delta is a single change: `minContainers: 2` on runtime services — one replica keeps serving while the other rolls during a deploy.\n" +
			"- DB and cache stay at `mode: NON_HA` — they HA-flip at the next tier, not here.\n" +
			"- Configure `deploy.readinessCheck` on runtime services before this promotion — the rolling-deploy behavior at `minContainers: 2` assumes it; without readiness gating, traffic can reach a not-yet-ready new replica during rollover."
	case 4:
		return "From Small Production to Highly-available Production:\n" +
			"- Deploy the `5 — Highly-available Production/import.yaml`.\n" +
			"- Runtime services continue at `minContainers: 2`; the HA-distinct changes are `mode: HA` on DB and cache and `cpuMode: DEDICATED` on runtime services. Note: managed-service `mode` is immutable after creation, so the HA flip happens in a new project — the Small-Production services are not modified in place.\n" +
			"- Automatic failover on DB and cache once the HA project is live.\n" +
			"- Expect the cutover from the Small-Production project to the HA project to involve a data export/import — schedule during low-traffic hours and plan the DNS/client swap.\n" +
			"- Once the HA project is serving, subsequent deploys within it are graceful by the same rolling mechanics as Small Production.\n" +
			"- Verify the worker codebase's queue-group configuration BEFORE the cutover (double-processing risk on `minContainers > 1`, which already applies at Small Production)."
	}
	return ""
}

// envOperationalConcerns names tier-specific behaviors a reader running that
// tier needs to know about on day one. Cited from zerops_knowledge guides
// where a guide exists for the mechanism.
//
// v39 Commit 1: signature accepts `plan` so env-2's managed-service hostname
// list can be derived from `plan.Targets` instead of hardcoding `db, cache,
// queue, storage, search`. v38 editorial-review CRIT #3 ("Stage hits the same
// DB as dev on tiers 0-2") is dropped — each tier declares a distinct
// `project.name` so each tier has its own DB project. See
// v39-commit1-bullet-audit.md for the full cluster classification.
func envOperationalConcerns(plan *RecipePlan, envIndex int) string {
	switch envIndex {
	case 0:
		return "- SSH into the dev container and drive the app process yourself.\n" +
			"- `zerops.yaml` `setup: dev` ships a no-op start (`zsc noop --silent`) — the container idles until you run the dev server.\n" +
			"- Edits made on the SSHFS mount from zcp are live on the container's filesystem; there is no build loop.\n" +
			"- `initCommands` do NOT fire automatically at this tier — run migrations and seeds ad-hoc over SSH.\n" +
			"- See `zerops_knowledge topic=init-commands` for how `zsc execOnce` behaves across iterations."
	case 1:
		return "- The CDE expects a persistent SSH session — avoid opening and closing connections in a loop.\n" +
			"- Use `zerops_dev_server` (or your IDE's Remote extension) to keep the dev process alive across multiple file edits.\n" +
			"- Managed-service credentials arrive as OS env vars at deploy time.\n" +
			"- Restart the dev process after credential rotation or after provisioning a new managed service.\n" +
			"- See `zerops_knowledge topic=env-var-model` for the three-level env-var resolution (project / service / zerops.yaml)."
	case 2:
		hostnames := managedServiceHostnameList(plan)
		return "- `zcli vpn up` establishes the tunnel to managed services — run it once per session.\n" +
			"- Managed-service hostnames (" + hostnames + ") resolve through the VPN.\n" +
			"- Connection strings in your local `.env` should use the same `${hostname}_*` shape the zerops.yaml uses.\n" +
			"- When adding a new service to the recipe, bring the VPN down and back up so DNS refreshes.\n" +
			"- Your local app will fail cryptically if the VPN is down; add a startup check that verifies DB connectivity before serving."
	case 3:
		return "- Stage deploys come from local or CI — there is no Zerops dev container at this tier to cross-deploy from.\n" +
			"- Stage has no long-lived dev process; every deploy produces a fresh container.\n" +
			"- Each tier's `project.name` is distinct (`{slug}-stage` for this tier vs `{slug}-agent` / `{slug}-remote` / `{slug}-local` for lower tiers) — Stage has its own Zerops project with its own DB and state.\n" +
			"- `readinessCheck` and `healthCheck` windows live in each codebase's `zerops.yaml` and are writer-authored per framework, not env-tier-tuned — if stage deploys are flaky, inspect the codebase's `zerops.yaml` before blaming the application.\n" +
			"- See `zerops_knowledge topic=readiness-health-checks` for the three probe types and their failure semantics."
	case 4:
		return "- Rolling deploys at `minContainers: 2` keep one replica serving while the other rolls, but traffic can still land on a not-yet-ready replica unless `readinessCheck` is configured on runtime services — add `deploy.readinessCheck` with an `httpGet` probe to each codebase's `zerops.yaml` to gate traffic handoff.\n" +
			"- Schedule any `mode`-related changes (HA promotion, managed-service mode flips) during low-traffic windows — those require a new project and a data cutover.\n" +
			"- Keep `zerops_logs` windows narrow to control retention cost.\n" +
			"- Cache is single-replica (`mode: NON_HA`) — a cache restart flushes warm data. Design the app to tolerate cold cache."
	case 5:
		return "- Rolling deploys are zero-downtime BUT require the application to handle SIGTERM correctly.\n" +
			"- Consumers must drain in-flight work before exit — see the worker codebase's README §Gotchas for the concrete drain sequence.\n" +
			"- Queue-group consumers MUST set the client-library's group flag; without it, two replicas process every message twice.\n" +
			"- DB/cache HA failover is automatic but carries a brief write-blocking window — don't mistake it for an outage.\n" +
			"- Session state should NOT live in process memory — use cache or DB so a rolling replacement doesn't drop user sessions.\n" +
			"- See `zerops_knowledge topic=rolling-deploys` for the SIGTERM timing contract and `zerops_knowledge topic=http-support` for L7 balancer behavior during container swap."
	}
	return ""
}

// managedServiceHostnameList returns a human-readable, comma-separated list of
// managed-service hostnames in the plan (non-runtime targets). Used by
// envOperationalConcerns(envIndex=2) so the VPN-hostname bullet reflects the
// actual plan rather than a hardcoded set. Returns a generic fallback if the
// plan has no managed services (shouldn't happen for showcase tiers; defensive
// for hello-world / minimal recipes).
func managedServiceHostnameList(plan *RecipePlan) string {
	if plan == nil {
		return "your plan's managed-service hostnames"
	}
	names := make([]string, 0, len(plan.Targets))
	for _, t := range plan.Targets {
		if IsRuntimeType(t.Type) {
			continue
		}
		names = append(names, "`"+t.Hostname+"`")
	}
	if len(names) == 0 {
		return "your plan's managed-service hostnames"
	}
	return strings.Join(names, ", ")
}

// recipePrettyName derives a display name from the slug by stripping the framework prefix.
// "laravel-minimal" → "Minimal", "bun-hello-world" → "Hello World", "django-showcase" → "Showcase".
func recipePrettyName(slug, framework string) string {
	prefix := strings.ToLower(framework) + "-"
	name := strings.TrimPrefix(slug, prefix)
	words := strings.Split(name, "-")
	for i, w := range words {
		if w != "" {
			words[i] = titleCase(w)
		}
	}
	return strings.Join(words, " ")
}

// aOrAn returns "an" for vowel-starting words, "a" otherwise.
func aOrAn(s string) string {
	if len(s) == 0 {
		return "a"
	}
	switch s[0] {
	case 'A', 'E', 'I', 'O', 'U', 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}

// envSlugSuffix returns the URL-safe environment slug for deploy links.
func envSlugSuffix(envIndex int) string {
	switch envIndex {
	case 0:
		return "ai-agent"
	case 1:
		return "remote-cde"
	case 2:
		return "local"
	case 3:
		return "stage"
	case 4:
		return "small-production"
	case 5:
		return "highly-available-production"
	}
	return ""
}

// envFolderURLEncoded returns the URL-encoded folder name for README links.
func envFolderURLEncoded(folder string) string {
	// Replace spaces and em-dash for URL encoding.
	r := strings.NewReplacer(" ", "%20", "\u2014", "%E2%80%94", "(", "(", ")", ")")
	return r.Replace(folder)
}

// frameworkURL returns a reasonable URL for a framework name.
func frameworkURL(framework string) string {
	urls := map[string]string{
		"laravel":   "https://laravel.com",
		"django":    "https://djangoproject.com",
		"rails":     "https://rubyonrails.org",
		"nestjs":    "https://nestjs.com",
		"nextjs":    "https://nextjs.org",
		"nuxt":      "https://nuxt.com",
		"bun":       "https://bun.sh",
		"deno":      "https://deno.com",
		"express":   "https://expressjs.com",
		"hono":      "https://hono.dev",
		"elysia":    "https://elysiajs.com",
		"flask":     "https://flask.palletsprojects.com",
		"fastapi":   "https://fastapi.tiangolo.com",
		"spring":    "https://spring.io",
		"phoenix":   "https://phoenixframework.org",
		"gin":       "https://gin-gonic.com",
		"fiber":     "https://gofiber.io",
		"echo":      "https://echo.labstack.com",
		"actix":     "https://actix.rs",
		"axum":      "https://github.com/tokio-rs/axum",
		"svelte":    "https://svelte.dev",
		"sveltekit": "https://svelte.dev/docs/kit",
		"react":     "https://react.dev",
		"vue":       "https://vuejs.org",
		"angular":   "https://angular.dev",
		"astro":     "https://astro.build",
		"remix":     "https://remix.run",
		"adonis":    "https://adonisjs.com",
		"koa":       "https://koajs.com",
	}
	if u, ok := urls[strings.ToLower(framework)]; ok {
		return u
	}
	return "https://zerops.io"
}

// recipeIntroServiceList builds the "connected to X, Y, and Z" phrase for the
// recipe intro. Minimal recipes mention only the DB. Showcase recipes list all
// managed/utility services so the intro reflects the full recipe capability.
func recipeIntroServiceList(plan *RecipePlan) string {
	var names []string
	// DB from research.
	if plan.Research.DBDriver != "" && plan.Research.DBDriver != recipeDBNone {
		names = append(names, dbDisplayName(plan.Research.DBDriver))
	}
	// Additional services from plan targets (non-runtime, non-DB).
	for _, t := range plan.Targets {
		if IsRuntimeType(t.Type) {
			continue
		}
		kind := serviceTypeKind(t.Type)
		if kind == kindDatabase {
			continue // already covered by DBDriver
		}
		names = append(names, serviceIntroLabel(t.Type))
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return "connected to " + names[0]
	}
	return "connected to " + strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// serviceIntroLabel returns a human-readable label for a service type in the intro.
func serviceIntroLabel(serviceType string) string {
	base, _, _ := strings.Cut(strings.ToLower(serviceType), "@")
	switch base {
	case "valkey", "keydb":
		return "[Valkey](https://valkey.io/) (Redis-compatible)"
	case svcMeilisearch:
		return "[Meilisearch](https://www.meilisearch.com/)"
	case "elasticsearch":
		return "[Elasticsearch](https://www.elastic.co/)"
	case "qdrant":
		return "[Qdrant](https://qdrant.tech/)"
	case "typesense":
		return "[Typesense](https://typesense.org/)"
	case "object-storage":
		return "S3-compatible object storage"
	case "shared-storage":
		return "shared storage"
	case "nats":
		return "[NATS](https://nats.io/)"
	case "kafka":
		return "[Kafka](https://kafka.apache.org/)"
	case "mailpit":
		return "[Mailpit](https://mailpit.axllent.org/)"
	}
	return base
}

// dbDisplayName returns a display name for a DB driver.
func dbDisplayName(driver string) string {
	switch driver {
	case svcPostgreSQL, "pgsql":
		return "[PostgreSQL](https://www.postgresql.org/)"
	case "mysql", svcMariaDB:
		return "[MariaDB](https://mariadb.org/)"
	case "mongodb":
		return "[MongoDB](https://www.mongodb.com/)"
	}
	return driver
}

// Import YAML generation is in recipe_templates_import.go.

// envDescription returns a description for an environment tier, dynamically including
// the services present in the plan. Matches the style used by zeropsio/recipes.
func envDescription(plan *RecipePlan, envIndex int) string {
	switch envIndex {
	case 0:
		desc := "environment provides a development space for AI agents to build and version the app."
		if svc := buildServiceIncludesList(plan, envIndex); svc != "" {
			desc += "\n" + svc
		}
		return desc
	case 1:
		desc := "environment allows developers to build the app **within Zerops** via SSH, supporting the full development lifecycle without local tool installation."
		if svc := buildServiceIncludesList(plan, envIndex); svc != "" {
			desc += "\n" + svc
		}
		return desc
	case 2:
		return "environment supports local app development using zCLI VPN for database access, while ensuring valid deployment processes using a staged app in Zerops."
	case 3:
		return "environment uses the same configuration as production, but runs on a single container with lower scaling settings."
	case 4:
		return "environment offers a production-ready setup optimized for moderate throughput."
	case 5:
		return "environment provides a production setup with enhanced scaling, dedicated resources, and HA components for improved durability and performance."
	}
	return ""
}

// buildServiceIncludesList returns "It includes a dev service..., a staging service, and a database."
// based on targets in the plan. All targets appear in all environments.
// For dual-runtime recipes, each non-worker runtime gets its own dev+stage mention.
func buildServiceIncludesList(plan *RecipePlan, envIndex int) string {
	var parts []string

	for _, target := range plan.Targets {
		if IsRuntimeType(target.Type) && !target.IsWorker {
			if envIndex <= 1 {
				label := target.Hostname
				parts = append(parts,
					fmt.Sprintf("a %s dev service with the code repository and necessary development tools", label),
					fmt.Sprintf("a %s staging service", label),
				)
			}
		} else if !IsRuntimeType(target.Type) {
			parts = append(parts, dataServiceIncludesLabel(target.Type))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return "It includes " + naturalJoin(parts) + "."
}

// dataServiceIncludesLabel returns the prose label for a non-runtime service
// in the env description, derived from its type's canonical kind.
func dataServiceIncludesLabel(serviceType string) string {
	switch serviceTypeKind(serviceType) {
	case kindDatabase:
		return "a low-resource database"
	case kindCache:
		return "a cache store"
	case kindStorage:
		return "an object storage"
	case kindSearchEngine:
		return "a search engine"
	case kindMessaging:
		return "a messaging service"
	case kindMailCatcher:
		return "a mail catcher"
	}
	return "a service"
}

// naturalJoin joins parts with commas and "and" before the last element.
// ["a", "b", "c"] → "a, b, and c"; ["a", "b"] → "a and b"; ["a"] → "a".
func naturalJoin(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}
