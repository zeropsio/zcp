package ops

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// failureSignals returns the ordered classifier pattern library. First match
// wins, so order entries narrowest → broadest within a phase.
//
// The library is built once per process (sync.Once); regex compilation is
// non-trivial and the library is read-only at runtime.
//
// Adding a new signal:
//  1. Pick a stable id ("phase:short-name") so SignalsMatched is grep-able.
//  2. Restrict by Phase (and Strategy / APICode where applicable) before
//     adding broad log patterns — keeps false positives down.
//  3. Write a unit test in deploy_failure_test.go that pins the new signal
//     against a representative log/error sample.
//
// The library does NOT try to enumerate every possible failure — it covers
// the common "agent burns a turn parsing logs" cases. When no signal matches,
// the baseline falls back to a phase-level classification (see
// baselineForPhase) so the response always carries a Category.
func failureSignals() []failureSignal {
	signalsOnce.Do(func() {
		signalsCache = buildSignalLibrary()
	})
	return signalsCache
}

var (
	signalsOnce  sync.Once
	signalsCache []failureSignal
)

func buildSignalLibrary() []failureSignal {
	return []failureSignal{
		// =================================================================
		// BUILD phase
		// =================================================================
		{
			id:         "build:command-not-found",
			phases:     []DeployFailurePhase{PhaseBuild},
			logRegex:   regexp.MustCompile(`(?:command not found|: not found|No such file or directory)`),
			requireLog: true,
			build:      buildCommandNotFound,
		},
		{
			id:         "build:npm-package-missing",
			phases:     []DeployFailurePhase{PhaseBuild},
			logRegex:   regexp.MustCompile(`npm (?:ERR! )?(?:404|notarget|enoent)`),
			requireLog: true,
			build:      buildNpmMissing,
		},
		{
			id:         "build:module-not-found",
			phases:     []DeployFailurePhase{PhaseBuild},
			logRegex:   regexp.MustCompile(`Cannot find module ['"][^'"]+['"]`),
			requireLog: true,
			build:      buildModuleNotFound,
		},
		{
			id:            "build:go-module-error",
			phases:        []DeployFailurePhase{PhaseBuild},
			logSubstrings: []string{"go: cannot find module", "missing go.sum entry"},
			requireLog:    true,
			build:         buildGoModule,
		},
		{
			id:         "build:composer-missing",
			phases:     []DeployFailurePhase{PhaseBuild},
			logRegex:   regexp.MustCompile(`(?:Could not find package|composer\.json: not found|Class\s+["'][^"']+["']\s+not found)`),
			requireLog: true,
			build:      buildComposerMissing,
		},
		{
			id:         "build:oom-killed",
			phases:     []DeployFailurePhase{PhaseBuild},
			logRegex:   regexp.MustCompile(`(?:Killed\b|Out of memory|OOMKilled)`),
			requireLog: true,
			build:      buildOOMKilled,
		},

		// =================================================================
		// PREPARE phase
		// =================================================================
		{
			id:         "prepare:missing-sudo",
			phases:     []DeployFailurePhase{PhasePrepare},
			logRegex:   regexp.MustCompile(`(?:ERROR: Unable to lock database|apt-get install.*permission denied|apk add.*permission denied|must be root|Operation not permitted)`),
			requireLog: true,
			build:      prepareMissingSudo,
		},
		{
			id:         "prepare:php-extension-missing",
			phases:     []DeployFailurePhase{PhasePrepare},
			logRegex:   regexp.MustCompile(`(?i)(?:ERROR: unable to select packages:\s*php\d*-\S+|E: Unable to locate package php\d*-\S+|E: Unable to locate package php-\S+|No package php\d*-\S+ available)`),
			requireLog: true,
			build:      preparePHPExtension,
		},
		{
			id:         "prepare:wrong-pkg-name",
			phases:     []DeployFailurePhase{PhasePrepare},
			logRegex:   regexp.MustCompile(`(?:ERROR: unable to select packages|E: Unable to locate package|No package matching)`),
			requireLog: true,
			build:      prepareWrongPackage,
		},
		{
			id:         "prepare:var-www-missing",
			phases:     []DeployFailurePhase{PhasePrepare},
			logRegex:   regexp.MustCompile(`(?:/var/www[^\s:]*: (?:No such file or directory|not a directory|cannot access))`),
			requireLog: true,
			build:      prepareVarWwwMissing,
		},

		// =================================================================
		// INIT phase (DEPLOY_FAILED — runtime container started but crashed)
		// =================================================================
		{
			id:         "init:port-in-use",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`(?:EADDRINUSE|address already in use|bind: address already in use)`),
			requireLog: true,
			build:      initPortInUse,
		},
		{
			id:         "init:module-not-found",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`Cannot find module ['"][^'"]+['"]`),
			requireLog: true,
			build:      initModuleNotFound,
		},
		{
			id:         "init:db-connection-refused",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`(?:connection refused.*5432|could not connect to server|ECONNREFUSED.*(?:5432|3306|6379|27017)|MongoNetworkError)`),
			requireLog: true,
			build:      initDBConnRefused,
		},
		{
			id:         "init:db-auth-failed",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`(?:password authentication failed|FATAL: password|Access denied for user|MongoServerError: Authentication failed)`),
			requireLog: true,
			build:      initDBAuthFailed,
		},
		{
			id:         "init:missing-env-var",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`(?:undefined env var|env var .* not set|Environment variable .* required|process\.env\.[A-Z_]+ is undefined)`),
			requireLog: true,
			build:      initMissingEnvVar,
		},
		{
			id:         "init:migration-failed",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`(?:Migration .* failed|migrations.*pending|artisan migrate.*(?:failed|error))`),
			requireLog: true,
			build:      initMigrationFailed,
		},
		{
			id:         "init:build-path-baked",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`/(?:build|tmp/build)/source[^\s]*`),
			requireLog: true,
			build:      initBuildPathBaked,
		},
		{
			id:         "init:permission-denied",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`(?:EACCES|permission denied)`),
			requireLog: true,
			build:      initPermissionDenied,
		},
		{
			id:         "init:oom-killed",
			phases:     []DeployFailurePhase{PhaseInit},
			logRegex:   regexp.MustCompile(`(?:OOMKilled|Killed\s+(?:node|python|ruby|php|java)|JavaScript heap out of memory)`),
			requireLog: true,
			build:      initOOMKilled,
		},

		// =================================================================
		// TRANSPORT phase (no build was triggered; source/SSH/zcli failed)
		// =================================================================
		{
			id:            "transport:ssh-killed",
			phases:        []DeployFailurePhase{PhaseTransport},
			logSubstrings: []string{"signal: killed"},
			requireLog:    true,
			build:         transportSSHKilled,
		},
		{
			id:         "transport:ssh-unreachable",
			phases:     []DeployFailurePhase{PhaseTransport},
			logRegex:   regexp.MustCompile(`(?:connection refused|no route to host|connect: network is unreachable|Could not resolve hostname)`),
			requireLog: true,
			build:      transportSSHUnreachable,
		},
		{
			id:         "transport:zcli-tty-required",
			phases:     []DeployFailurePhase{PhaseTransport},
			logRegex:   regexp.MustCompile(`(?:allowed only in interactive terminal|requires a terminal|tty required)`),
			requireLog: true,
			build:      transportZCLITTYRequired,
		},
		{
			id:         "transport:zcli-auth-failed",
			phases:     []DeployFailurePhase{PhaseTransport},
			logRegex:   regexp.MustCompile(`(?:invalid token|unauthorized|401|forbidden|403)`),
			strategies: []string{string(topology.StrategyPushDev)},
			requireLog: true,
			build:      transportZCLIAuth,
		},
		{
			id:         "transport:git-auth-failed",
			phases:     []DeployFailurePhase{PhaseTransport},
			strategies: []string{topology.StrategyPushGit},
			logRegex:   regexp.MustCompile(`(?:Authentication failed|Permission denied \(publickey\)|fatal: could not read Username|terminal prompts disabled)`),
			requireLog: true,
			build:      transportGitAuth,
		},
		{
			id:         "transport:git-token-missing",
			phases:     []DeployFailurePhase{PhaseTransport},
			strategies: []string{topology.StrategyPushGit},
			apiCode:    platform.ErrGitTokenMissing,
			build:      transportGitTokenMissing,
		},

		// =================================================================
		// PREFLIGHT phase
		// =================================================================
		{
			id:         "preflight:dm2-self-deploy-narrow",
			phases:     []DeployFailurePhase{PhasePreflight},
			apiCode:    platform.ErrInvalidZeropsYml,
			logRegex:   regexp.MustCompile(`deployFiles must be \[\.\]`),
			requireLog: true,
			build:      preflightDM2,
		},
		{
			id:      "preflight:invalid-zerops-yaml",
			phases:  []DeployFailurePhase{PhasePreflight},
			apiCode: platform.ErrInvalidZeropsYml,
			build:   preflightInvalidZeropsYml,
		},
		{
			id:      "preflight:prerequisite-missing",
			phases:  []DeployFailurePhase{PhasePreflight},
			apiCode: platform.ErrPrerequisiteMissing,
			build:   preflightPrerequisite,
		},
	}
}

// =============================================================================
// build phase builders
// =============================================================================

func buildCommandNotFound(match string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassBuild,
		LikelyCause:     fmt.Sprintf("buildCommands referenced a binary that doesn't exist in the build container (%q).", match),
		SuggestedAction: "Check buildCommands typos. Install the binary via prepareCommands (with sudo) or pick a base image that ships it.",
		Signals:         []string{"build:command-not-found"},
	}
}

func buildNpmMissing(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassBuild,
		LikelyCause:     "npm could not resolve a package (404 / notarget / enoent).",
		SuggestedAction: "Verify package name + version in package.json. Re-run `npm install` locally to regenerate package-lock.json, then commit.",
		Signals:         []string{"build:npm-package-missing"},
	}
}

func buildModuleNotFound(match string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassBuild,
		LikelyCause:     fmt.Sprintf("Build step referenced a module not in dependencies (%s).", match),
		SuggestedAction: "Add the module to package.json/composer.json/go.mod and reinstall, or check the build step's working directory.",
		Signals:         []string{"build:module-not-found"},
	}
}

func buildGoModule(match string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassBuild,
		LikelyCause:     "Go module path mismatch or stale go.sum.",
		SuggestedAction: "Run `go mod tidy` locally and commit go.sum. If using a private repo, ensure GOPRIVATE is set in build env.",
		Signals:         []string{"build:go-module-error"},
	}
}

func buildComposerMissing(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassBuild,
		LikelyCause:     "Composer could not find a package or class.",
		SuggestedAction: "Verify composer.json + composer.lock are committed. Add `composer install --no-dev` to buildCommands if not already present.",
		Signals:         []string{"build:composer-missing"},
	}
}

func buildOOMKilled(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassBuild,
		LikelyCause:     "Build container OOM-killed during compile/install.",
		SuggestedAction: "Increase build container RAM via zerops.yaml `build.containerOptions` or split heavy install steps. Webpack/Vite builds frequently need 2-4 GB.",
		Signals:         []string{"build:oom-killed"},
	}
}

// =============================================================================
// prepare phase builders
// =============================================================================

func prepareMissingSudo(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "prepareCommands ran a privileged operation without sudo (containers run as the zerops user).",
		SuggestedAction: "Prefix every package install in run.prepareCommands with `sudo` (e.g. `sudo apk add --no-cache ...`).",
		Signals:         []string{"prepare:missing-sudo"},
	}
}

func prepareWrongPackage(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "Package manager couldn't find a requested package — likely wrong name for the runtime base distro.",
		SuggestedAction: "Check the package's exact name on Alpine/Debian (e.g. `php84-pdo_pgsql` on Alpine, not `php-pgsql`).",
		Signals:         []string{"prepare:wrong-pkg-name"},
	}
}

func preparePHPExtension(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "PHP extension package name mismatch — Alpine PHP extensions use the version prefix (e.g. `php84-ctype`, not `php-ctype`).",
		SuggestedAction: "Replace `php-<ext>` with `php<major><minor>-<ext>` matching your php@N.N runtime. Built-in extensions (json, tokenizer since PHP 8.0) don't need installing.",
		Signals:         []string{"prepare:php-extension-missing"},
	}
}

func prepareVarWwwMissing(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "prepareCommands referenced /var/www before deploy files arrived (prepareCommands run BEFORE the artifact extraction).",
		SuggestedAction: "Use `addToRunPrepare` in zerops.yaml to ship needed files to /home/zerops/ during prepare. /var/www is empty until init.",
		Signals:         []string{"prepare:var-www-missing"},
	}
}

// =============================================================================
// init phase builders
// =============================================================================

func initPortInUse(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "Application tried to bind a port that's already in use.",
		SuggestedAction: "Check run.start AND run.initCommands — only one should bind the port. Confirm run.ports[] matches the bound port.",
		Signals:         []string{"init:port-in-use"},
	}
}

func initModuleNotFound(match string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     fmt.Sprintf("Runtime crashed because a module is missing at start (%s).", match),
		SuggestedAction: "Add the module to deployFiles (so it ships to runtime) OR install in run.initCommands. Build-container node_modules/vendor doesn't auto-carry to runtime.",
		Signals:         []string{"init:module-not-found"},
	}
}

func initDBConnRefused(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "Application could not reach its database — DB service may be down or env var points at the wrong host/port.",
		SuggestedAction: "Run `zerops_discover` to confirm the DB service is RUNNING. Check env vars for ${db_*} refs (zerops_env). Inter-service traffic uses the hostname directly, not localhost.",
		Signals:         []string{"init:db-connection-refused"},
	}
}

func initDBAuthFailed(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "Database rejected credentials.",
		SuggestedAction: "Check env vars referencing ${db_user}/${db_password} via zerops_env. Managed DB services own the credentials — copy them into the app's env vars rather than hard-coding.",
		Signals:         []string{"init:db-auth-failed"},
	}
}

func initMissingEnvVar(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "Application requires an env var that isn't set on the runtime container.",
		SuggestedAction: "Set via `zerops_env action=set` (recommended) or via zerops.yaml `run.envVariables`. Use ${peer_var} refs for cross-service values.",
		Signals:         []string{"init:missing-env-var"},
	}
}

func initMigrationFailed(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "Database migration failed during init.",
		SuggestedAction: "Check DB connectivity first (env vars + DB service status). If the migration runs in run.initCommands, ensure the DB is RUNNING before deploy. For dependent failures, fix and redeploy — migrations resume from the last successful one.",
		Signals:         []string{"init:migration-failed"},
	}
}

func initBuildPathBaked(match string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     fmt.Sprintf("Build-time cache baked a /build/source path (%s) into runtime artifacts; the build container's path doesn't exist at runtime.", match),
		SuggestedAction: "Move cache commands like `php artisan config:cache` from buildCommands to run.initCommands so caches resolve runtime paths (/var/www).",
		Signals:         []string{"init:build-path-baked"},
	}
}

func initPermissionDenied(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "Runtime tried to write to a path the zerops user can't access.",
		SuggestedAction: "Use `addToRunPrepare` to chown directories during prepare phase, or write under /home/zerops/ which is writable.",
		Signals:         []string{"init:permission-denied"},
	}
}

func initOOMKilled(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassStart,
		LikelyCause:     "Runtime container OOM-killed.",
		SuggestedAction: "Scale up memory: `zerops_scale serviceHostname=<svc> minRam=<higher>`. For Node.js, also consider `--max-old-space-size`.",
		Signals:         []string{"init:oom-killed"},
	}
}

// =============================================================================
// transport phase builders
// =============================================================================

func transportSSHKilled(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassNetwork,
		LikelyCause:     "SSH process killed (most often OOM on the source container).",
		SuggestedAction: "Scale up the source container: `zerops_scale serviceHostname=<source> minRam=2`. Then retry the deploy.",
		Signals:         []string{"transport:ssh-killed"},
	}
}

func transportSSHUnreachable(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassNetwork,
		LikelyCause:     "Source container unreachable over SSH (connection refused / no route).",
		SuggestedAction: "Verify the source service is RUNNING via `zerops_discover service=<source>`. If it's not, start it via `zerops_manage action=start`.",
		Signals:         []string{"transport:ssh-unreachable"},
	}
}

func transportZCLITTYRequired(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassConfig,
		LikelyCause:     "zcli prompted for terminal input while running over SSH (no TTY).",
		SuggestedAction: "ZCP issues this internally — if you see this, file a bug. As a workaround, retry the deploy; transient state in zcli's config can trigger a one-time prompt.",
		Signals:         []string{"transport:zcli-tty-required"},
	}
}

func transportZCLIAuth(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassCredential,
		LikelyCause:     "zcli login token rejected.",
		SuggestedAction: "Regenerate ZCP_API_KEY in the Zerops GUI (Settings → Personal Tokens) and update the project env / .env file. Then restart ZCP.",
		Signals:         []string{"transport:zcli-auth-failed"},
	}
}

func transportGitAuth(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassCredential,
		LikelyCause:     "Git remote rejected the push (auth failed / permission denied).",
		SuggestedAction: "For container env: confirm GIT_TOKEN is set + has push scope to the repo. For local env: confirm SSH key is in ssh-agent or HTTPS credentials are cached.",
		Signals:         []string{"transport:git-auth-failed"},
	}
}

func transportGitTokenMissing(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassCredential,
		LikelyCause:     "GIT_TOKEN env var missing on the source container.",
		SuggestedAction: "Configure via `zerops_workflow action=\"strategy\" strategies={<svc>:\"push-git\"}` — the strategy flow walks through GIT_TOKEN setup.",
		Signals:         []string{"transport:git-token-missing"},
	}
}

// =============================================================================
// preflight phase builders
// =============================================================================

func preflightInvalidZeropsYml(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassConfig,
		LikelyCause:     "zerops.yaml rejected by the platform validator.",
		SuggestedAction: "Read apiMeta for field-level reasons. Most rejections are missing/misnamed setup, deployFiles type mismatch, or unknown base image.",
		Signals:         []string{"preflight:invalid-zerops-yaml"},
	}
}

func preflightDM2(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassConfig,
		LikelyCause:     "Self-deploy with narrower-than-`[.]` deployFiles destroys the source container's working tree (DM-2 invariant).",
		SuggestedAction: "Set `build.deployFiles: [.]` for the self-deploy setup. To cherry-pick build output, use cross-deploy (sourceService != targetService) or strategy=git-push.",
		Signals:         []string{"preflight:dm2-self-deploy-narrow"},
	}
}

func preflightPrerequisite(_ string) *topology.DeployFailureClassification {
	return &topology.DeployFailureClassification{
		Category:        topology.FailureClassConfig,
		LikelyCause:     "A pre-flight prerequisite was missing (zerops.yaml file, GIT_TOKEN, committed code, etc.).",
		SuggestedAction: "Read the error suggestion field — it names the specific missing prerequisite and the fix.",
		Signals:         []string{"preflight:prerequisite"},
	}
}
