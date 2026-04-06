package workflow

import "strings"

// jsPackageManagerCommands are substrings that signal a JS/TS build pipeline
// in RecipePlan.Research.BuildCommands. When a non-JS primary runtime carries
// one of these in its build commands, the recipe needs multi-base handling.
var jsPackageManagerCommands = []string{"npm ", "npm\t", "pnpm ", "yarn ", "bun "}

// jsRuntimePrefixes identifies runtime service-types that already include a
// JS runtime natively — multi-base knowledge is irrelevant for these because
// node/bun/deno are the primary runtime.
var jsRuntimePrefixes = []string{"nodejs@", "bun@", "deno@"}

// needsMultiBaseGuidance reports whether the recipe's primary runtime is
// non-JS yet its build pipeline invokes a JS package manager (npm/pnpm/yarn/bun).
// When true, the agent must use multi-base in build AND — if dev needs
// interactive JS tooling at runtime — install it via zsc install in
// run.prepareCommands. This is the trigger for injecting the multi-base
// knowledge snippet into generate-step guidance.
func needsMultiBaseGuidance(plan *RecipePlan) bool {
	if plan == nil || plan.RuntimeType == "" {
		return false
	}
	// Primary runtime is already a JS runtime — no second base needed.
	for _, prefix := range jsRuntimePrefixes {
		if strings.HasPrefix(plan.RuntimeType, prefix) {
			return false
		}
	}
	// Build commands include a JS package manager invocation.
	for _, cmd := range plan.Research.BuildCommands {
		for _, marker := range jsPackageManagerCommands {
			if strings.Contains(cmd, marker) {
				return true
			}
		}
	}
	// BuildBases with more than one entry — agent already decided multi-base,
	// but may not understand the run-container asymmetry. Inject anyway.
	if len(plan.BuildBases) > 1 {
		return true
	}
	return false
}

// multiBaseGuidance returns the conditional knowledge snippet explaining
// the build-vs-run multi-base asymmetry, zsc install for run-container
// secondary runtimes, and the startWithoutCode trap. Injected at generate
// step when needsMultiBaseGuidance fires.
func multiBaseGuidance() string {
	return `## Multi-Base Runtime (this recipe needs it)

Your framework's build pipeline invokes a JS package manager, but the primary
runtime is not a JS runtime. This triggers a build/run asymmetry that is easy
to get wrong.

### Build container: native multi-base

Set ` + "`build.base`" + ` to a list — both runtimes are fully installed with all
binaries on ` + "`PATH`" + `:

` + "```yaml" + `
build:
  base:
    - {primary-runtime}@{version}    # e.g. php@8.4, python@3.12, ruby@3.3
    - nodejs@22                      # or bun@1, deno@2
  buildCommands:
    - composer install               # or the equivalent for your runtime
    - npm ci && npm run build        # JS pipeline runs here, writes to public/build/
` + "```" + `

### Run container: single base + sudo -E zsc install

` + "`run.base`" + ` is a single runtime — you cannot pass a list. Secondary runtimes
at runtime come from ` + "`sudo -E zsc install <type>@<version>`" + ` in ` + "`run.prepareCommands`" + `
(cached into the runtime image once, not re-run per container start).

**` + "`sudo -E`" + ` is mandatory** — ` + "`zsc install`" + ` modifies system paths and needs
root privileges. The ` + "`-E`" + ` flag preserves environment variables (critical for
any env-dependent post-install steps).

### The prod vs dev divergence

**Prod** (end-user production target): multi-base in BUILD only. Build runs
npm/vite, compiled assets land in ` + "`deployFiles`" + ` (e.g. ` + "`public/build/`" + `), runtime
container serves the static bytes. No JS runtime needed at run time.

**Dev** (agent's iteration container): inverse pattern. The agent iterates
inside the runtime container (SSHFS edits, live server, ` + "`npm run dev`" + `, HMR,
running tests). Node must exist at runtime, which means:

` + "```yaml" + `
- setup: dev
  run:
    base: {primary-runtime}@{version}
    prepareCommands:
      - sudo -E zsc install nodejs@22  # or bun@1, deno@2 — whatever dev needs
` + "```" + `

Dev's BUILD side can be minimal — no need to bake assets there when the
whole point of dev is live source editing.

### The startWithoutCode trap

When provision created ` + "`appdev`" + ` with ` + "`startWithoutCode: true`" + `, the container
spun up with ONLY its service type (primary runtime). No zerops.yaml has been
applied yet → no ` + "`sudo -E zsc install`" + ` has run → Node/bun/deno is NOT available.

If you SSH into appdev right now expecting ` + "`npm`" + ` or ` + "`node`" + `, you will get
` + "`command not found`" + `. The secondary runtime only becomes available after the
first deploy runs a zerops.yaml whose ` + "`run.prepareCommands`" + ` installs it.

Chicken-and-egg: you cannot ` + "`npm install`" + ` to generate a lockfile before first
deploy if the dev container has no Node. Options:
1. Let the first build install it (multi-base build + ship assets in deployFiles) — works when dev doesn't need interactive JS tooling.
2. Skip the lockfile dependency in initial build commands (` + "`npm install`" + ` instead of ` + "`npm ci`" + `) so the first build is tolerant.
3. Generate the lockfile locally before the recipe run (not always possible).

**Always ` + "`sudo -E zsc install`" + `** — never bare ` + "`zsc install`" + `. The command
requires root and the ` + "`-E`" + ` flag preserves environment variables.

Reference: https://docs.zerops.io/references/zsc#install
`
}
