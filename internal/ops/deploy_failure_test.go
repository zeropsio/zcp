// Tests for: ops/deploy_failure.go + deploy_failure_signals.go — the
// DeployFailureClassification pipeline. Table-driven so adding a new
// signal goes alongside its fixture in one PR.
//
// Each case names the signal id it expects to fire (or empty for the
// phase baseline) and a representative log/error sample. Coverage
// targets: every signal in failureSignals() has at least one case
// here; phase baselines have one case each.
package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

func TestClassifyDeployFailure_Build(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                     string
		input                    FailureInput
		wantCategory             topology.FailureClass
		wantSignal               string
		wantInCause              string
		wantInSuggestedAction    string
		wantNotInSuggestedAction string
	}{
		{
			name: "command-not-found",
			input: FailureInput{
				Phase:     PhaseBuild,
				Status:    platform.BuildStatusBuildFailed,
				BuildLogs: []string{"+ thisbinaryisnotreal_xy42", "/bin/sh: 1: thisbinaryisnotreal_xy42: not found"},
			},
			wantCategory: topology.FailureClassBuild,
			wantSignal:   "build:command-not-found",
			wantInCause:  "binary that doesn't exist",
		},
		{
			// R6-P1: a build command whose FILE OPERAND is missing (cp/mv
			// source absent, deployFiles referencing build output that
			// wasn't produced — e.g. nextjs `cp -r public dist` with no
			// public/) must NOT classify as build:command-not-found
			// ("install a binary"). The cp-stat case is correctly handled
			// in PREPARE phase by prepare:var-www-missing; the gap was the
			// BUILD phase, where `No such file or directory` rode the
			// command-not-found regex. Surfaced by deploy-failure-recovery-
			// hint-and-classifier-gaps Finding 2 + the nextjs build error.
			name: "build-operand-missing-not-command",
			input: FailureInput{
				Phase:     PhaseBuild,
				Status:    platform.BuildStatusBuildFailed,
				BuildLogs: []string{"+ cp -r public dist", "cp: cannot stat 'public': No such file or directory"},
			},
			wantCategory:             topology.FailureClassBuild,
			wantSignal:               "build:operand-missing",
			wantInCause:              "does not exist",
			wantNotInSuggestedAction: "Install the binary",
		},
		{
			name: "npm-package-missing",
			input: FailureInput{
				Phase:     PhaseBuild,
				Status:    platform.BuildStatusBuildFailed,
				BuildLogs: []string{"npm ERR! 404 Not Found - GET https://registry.npmjs.org/@scope%2fpkg-typo"},
			},
			wantCategory: topology.FailureClassBuild,
			wantSignal:   "build:npm-package-missing",
			wantInCause:  "npm could not resolve",
		},
		{
			// Brownfield gotcha: repo has no committed package-lock.json
			// but buildCommands runs `npm ci`. npm refuses with EUSAGE
			// before any package resolve happens. Surfaced by
			// flow-eval-local brownfield-existing-node-app suite
			// 20260507-133912 — the agent fell to the build baseline
			// and had to read zerops_events to learn the real cause.
			name: "npm-ci-missing-lockfile",
			input: FailureInput{
				Phase:  PhaseBuild,
				Status: platform.BuildStatusBuildFailed,
				BuildLogs: []string{
					"npm error code EUSAGE",
					"npm error",
					"npm error The `npm ci` command can only install with an existing package-lock.json or",
					"npm error npm-shrinkwrap.json with lockfileVersion >= 1.",
				},
			},
			wantCategory: topology.FailureClassBuild,
			wantSignal:   "build:npm-ci-missing-lockfile",
			wantInCause:  "no package-lock.json",
		},
		{
			name: "module-not-found",
			input: FailureInput{
				Phase:     PhaseBuild,
				BuildLogs: []string{"Error: Cannot find module 'express'", "Require stack:"},
			},
			wantCategory: topology.FailureClassBuild,
			wantSignal:   "build:module-not-found",
		},
		{
			name: "go-mod-tidy-needed",
			input: FailureInput{
				Phase:     PhaseBuild,
				BuildLogs: []string{"go: github.com/foo/bar@v0.1.0: missing go.sum entry"},
			},
			wantCategory: topology.FailureClassBuild,
			wantSignal:   "build:go-module-error",
		},
		{
			name: "composer-class-missing",
			input: FailureInput{
				Phase:     PhaseBuild,
				BuildLogs: []string{"PHP Fatal error: Class 'App\\Foo' not found in /build/source/index.php"},
			},
			wantCategory: topology.FailureClassBuild,
			wantSignal:   "build:composer-missing",
		},
		{
			name: "build-oom",
			input: FailureInput{
				Phase:     PhaseBuild,
				BuildLogs: []string{"webpack compiling...", "Killed"},
			},
			wantCategory: topology.FailureClassBuild,
			wantSignal:   "build:oom-killed",
		},
		{
			name: "build-baseline-no-pattern",
			input: FailureInput{
				Phase:     PhaseBuild,
				Status:    platform.BuildStatusBuildFailed,
				BuildLogs: []string{"some unrecognized build chatter"},
			},
			wantCategory: topology.FailureClassBuild,
			wantSignal:   "phase:build",
		},
		// H3a: when build fails before any logs were captured (sub-10s
		// exits), baseline must NOT direct the agent to read buildLogs —
		// they're empty. Eval evidence:
		// greenfield-fullstack-multi-runtime in suite 20260505-151844.
		{
			// VERIFY-reserved-names.md §C — empty-logs <10s case is
			// dominated by reserved-env-name rejections (HOSTNAME/Path/path
			// in run.envVariables). Baseline must point at that trap, NOT
			// at buildCommands bisection.
			name: "build-baseline-empty-logs",
			input: FailureInput{
				Phase:     PhaseBuild,
				Status:    platform.BuildStatusBuildFailed,
				BuildLogs: nil,
			},
			wantCategory:             topology.FailureClassBuild,
			wantSignal:               "phase:build",
			wantInSuggestedAction:    "reserved key in run.envVariables",
			wantNotInSuggestedAction: "Read buildLogs",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyDeployFailure(tc.input)
			assertClassification(t, got, tc.wantCategory, tc.wantSignal, tc.wantInCause)
			if tc.wantInSuggestedAction != "" && got != nil &&
				!strings.Contains(got.SuggestedAction, tc.wantInSuggestedAction) {
				t.Errorf("SuggestedAction %q must contain %q",
					got.SuggestedAction, tc.wantInSuggestedAction)
			}
			if tc.wantNotInSuggestedAction != "" && got != nil &&
				strings.Contains(got.SuggestedAction, tc.wantNotInSuggestedAction) {
				t.Errorf("SuggestedAction %q must not contain %q (logs unavailable)",
					got.SuggestedAction, tc.wantNotInSuggestedAction)
			}
		})
	}
}

func TestClassifyDeployFailure_Prepare(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                     string
		input                    FailureInput
		wantCategory             topology.FailureClass
		wantSignal               string
		wantNotInSuggestedAction string
	}{
		{
			name: "missing-sudo",
			input: FailureInput{
				Phase:     PhasePrepare,
				BuildLogs: []string{"ERROR: Unable to lock database: Permission denied"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "prepare:missing-sudo",
		},
		{
			name: "wrong-pkg-name",
			input: FailureInput{
				Phase:     PhasePrepare,
				BuildLogs: []string{"E: Unable to locate package imagemagick-dev"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "prepare:wrong-pkg-name",
		},
		{
			name: "php-extension-prefix",
			input: FailureInput{
				Phase:     PhasePrepare,
				BuildLogs: []string{"ERROR: unable to select packages: php-ctype (no such package):"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "prepare:php-extension-missing",
		},
		{
			name: "var-www-missing-during-prepare",
			input: FailureInput{
				Phase:     PhasePrepare,
				BuildLogs: []string{"cp: cannot stat '/var/www/storage': No such file or directory"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "prepare:var-www-missing",
		},
		{
			name: "prepare-baseline",
			input: FailureInput{
				Phase:     PhasePrepare,
				BuildLogs: []string{"some prepare chatter"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "phase:prepare",
		},
		// H3a parity: prepare baseline must condition SuggestedAction on
		// hasLogs same way the build baseline does (commit 410c419f).
		// Empty BuildLogs → SuggestedAction must NOT direct the agent at
		// non-existent buildLogs. Codex fresh review surfaced this as the
		// missed test gap from the original H3a sweep — the production
		// fix is correct (deploy_failure.go:143-153 already conditions),
		// just untested. This row regression-locks it.
		{
			name: "prepare-baseline-empty-logs",
			input: FailureInput{
				Phase:     PhasePrepare,
				BuildLogs: nil,
			},
			wantCategory:             topology.FailureClassStart,
			wantSignal:               "phase:prepare",
			wantNotInSuggestedAction: "Read buildLogs",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyDeployFailure(tc.input)
			assertClassification(t, got, tc.wantCategory, tc.wantSignal, "")
			if tc.wantNotInSuggestedAction != "" && got != nil &&
				strings.Contains(got.SuggestedAction, tc.wantNotInSuggestedAction) {
				t.Errorf("SuggestedAction %q must not contain %q (logs unavailable)",
					got.SuggestedAction, tc.wantNotInSuggestedAction)
			}
		})
	}
}

func TestClassifyDeployFailure_Init(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		input        FailureInput
		wantCategory topology.FailureClass
		wantSignal   string
	}{
		{
			name: "port-in-use",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"node:events:494", "Error: listen EADDRINUSE: address already in use :::3000"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:port-in-use",
		},
		{
			name: "module-not-found-runtime",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"Error: Cannot find module 'pg'", "Require stack:", "- /var/www/server.js"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:module-not-found",
		},
		{
			name: "db-conn-refused-postgres",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"could not connect to server: Connection refused (0x0000274D/10061)", "Is the server running on host \"db\" (10.0.0.5) and accepting TCP/IP connections on port 5432?"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:db-connection-refused",
		},
		{
			name: "db-auth-failed-postgres",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"FATAL: password authentication failed for user \"app\""},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:db-auth-failed",
		},
		{
			name: "missing-env-var",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"Error: Environment variable JWT_SECRET required"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:missing-env-var",
		},
		{
			name: "migration-failed",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"Migration 2026_04_01_create_users failed: column already exists"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:migration-failed",
		},
		{
			name: "build-path-baked-into-cache",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"include(/build/source/bootstrap/cache/services.php): failed to open"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:build-path-baked",
		},
		{
			name: "permission-denied-runtime",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"Error: EACCES: permission denied, mkdir '/var/log/app'"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:permission-denied",
		},
		{
			name: "init-oom-node",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"<--- JS stacktrace --->", "FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "init:oom-killed",
		},
		{
			name: "init-baseline",
			input: FailureInput{
				Phase:       PhaseInit,
				RuntimeLogs: []string{"some unrecognized init chatter"},
			},
			wantCategory: topology.FailureClassStart,
			wantSignal:   "phase:init",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyDeployFailure(tc.input)
			assertClassification(t, got, tc.wantCategory, tc.wantSignal, "")
		})
	}
}

func TestClassifyDeployFailure_Transport(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		input        FailureInput
		wantCategory topology.FailureClass
		wantSignal   string
	}{
		{
			name: "ssh-killed-oom",
			input: FailureInput{
				Phase: PhaseTransport,
				TransportErr: &platform.SSHExecError{
					Hostname: "builder",
					Output:   "compiling...",
					Err:      errors.New("signal: killed"),
				},
			},
			wantCategory: topology.FailureClassNetwork,
			wantSignal:   "transport:ssh-killed",
		},
		{
			name: "ssh-unreachable",
			input: FailureInput{
				Phase: PhaseTransport,
				TransportErr: &platform.SSHExecError{
					Hostname: "src",
					Err:      errors.New("dial tcp 10.0.0.5:22: connect: connection refused"),
				},
			},
			wantCategory: topology.FailureClassNetwork,
			wantSignal:   "transport:ssh-unreachable",
		},
		{
			name: "zcli-tty-required",
			input: FailureInput{
				Phase: PhaseTransport,
				TransportErr: &platform.SSHExecError{
					Hostname: "src",
					Output:   "✓ Parsing zerops.yml\n✗ ERR allowed only in interactive terminal",
					Err:      errors.New("exit status 1"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:zcli-tty-required",
		},
		{
			name: "zcli-auth-failed-zcli",
			input: FailureInput{
				Phase:    PhaseTransport,
				Strategy: "zcli",
				TransportErr: &platform.SSHExecError{
					Hostname: "src",
					Output:   "✗ ERR unauthorized: invalid token",
					Err:      errors.New("exit status 1"),
				},
			},
			wantCategory: topology.FailureClassCredential,
			wantSignal:   "transport:zcli-auth-failed",
		},
		{
			name: "git-auth-failed-push-git",
			input: FailureInput{
				Phase:    PhaseTransport,
				Strategy: "git-push",
				TransportErr: &platform.SSHExecError{
					Hostname: "src",
					Output:   "remote: HTTP Basic: Access denied\nfatal: Authentication failed for 'https://github.com/foo/bar.git/'",
					Err:      errors.New("exit status 128"),
				},
			},
			wantCategory: topology.FailureClassCredential,
			wantSignal:   "transport:git-auth-failed",
		},
		{
			// "Repository not found" — a distinct cause from a rejected
			// password: wrong URL OR a private repo the PAT cannot see (B6).
			name: "git-repo-not-found-push-git",
			input: FailureInput{
				Phase:    PhaseTransport,
				Strategy: "git-push",
				TransportErr: &platform.SSHExecError{
					Hostname: "appdev",
					Output:   "remote: Repository not found.\nfatal: repository 'https://github.com/foo/bar.git/' not found",
					Err:      errors.New("exit status 128"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:git-repo-not-found",
		},
		{
			// A non-fast-forward push rejection (remote has commits the
			// local push lacks). NOT auth, NOT network — the fix is to
			// integrate the remote or force-push. Pre-fix this fell through
			// to the transport baseline (category=network), which sent the
			// agent chasing connectivity/PAT instead of the real cause.
			name: "git-non-fast-forward-push-git",
			input: FailureInput{
				Phase:    PhaseTransport,
				Strategy: "git-push",
				TransportErr: &platform.SSHExecError{
					Hostname: "app",
					Output:   " ! [rejected]        main -> main (fetch first)\nerror: failed to push some refs to 'https://github.com/foo/bar'\nhint: Updates were rejected because the remote contains work that you do not\nhint: have locally.",
					Err:      errors.New("exit status 1"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:git-non-fast-forward",
		},
		{
			// F1c: a shallow/incomplete recipe clone missing a delta-base
			// object — the push aborts with "did not receive expected object".
			// NOT network/auth (p2 #2 misclassified it as network → the agent
			// chased connectivity/PAT). Must classify as config (local repo fix).
			name: "git-shallow-object-missing-push-git",
			input: FailureInput{
				Phase:    PhaseTransport,
				Strategy: "git-push",
				TransportErr: &platform.SSHExecError{
					Hostname: "appdev",
					Output:   "Enumerating objects: 5, done.\nremote: error: unpack failed: unpack-objects abnormal exit\nfatal: did not receive expected object b93b603d4f2c",
					Err:      errors.New("exit status 1"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:git-shallow-object-missing",
		},
		{
			name: "git-token-missing",
			input: FailureInput{
				Phase:    PhaseTransport,
				Strategy: "git-push",
				APIErr: platform.NewPlatformError(
					platform.ErrGitTokenMissing,
					"GIT_TOKEN missing",
					"Set via zerops_workflow git-push-setup",
				),
			},
			wantCategory: topology.FailureClassCredential,
			wantSignal:   "transport:git-token-missing",
		},
		{
			name: "transport-baseline",
			input: FailureInput{
				Phase:        PhaseTransport,
				TransportErr: errors.New("some unrecognized transport error"),
			},
			wantCategory: topology.FailureClassNetwork,
			wantSignal:   "phase:transport",
		},
		// H1: zcli's local arg validation rejects multi-setup yaml when
		// --setup is omitted. zcli reaches the platform fine, then refuses
		// to push because no setup block matches the target. Pre-fix, this
		// fell through to PhaseTransport baseline → category=network +
		// "Transport-layer error reaching the platform" — sent agents
		// chasing connectivity issues for a yaml-config error. Eval
		// evidence: cross-deploy-stage-promote-from-dev in suite
		// 20260505-151844. Same defect class as
		// `transport:zcli-auth-failed` (commit 821f6113 swept that one).
		{
			name: "zcli-setup-mismatch",
			input: FailureInput{
				Phase: PhaseTransport,
				TransportErr: &platform.SSHExecError{
					Hostname: "appdev",
					Output:   "✓ Parsing zerops.yml\n✗ ERR Cannot find corresponding setup in zerops.yaml, please select with --setup",
					Err:      errors.New("exit status 1"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:zcli-setup-mismatch",
		},
		{
			name: "zcli-unknown-base",
			input: FailureInput{
				Phase: PhaseTransport,
				TransportErr: &platform.SSHExecError{
					Hostname: "appdev",
					Output:   "✗ ERR unknown base php-nginx@8.4 — see zcli stack list",
					Err:      errors.New("exit status 1"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:zcli-unknown-runtime",
		},
		// B13: git identity errors used to fall through to the network
		// baseline because no signal recognized them. The agent then
		// chased "Transport-layer error reaching the platform" hints
		// (VPN / connectivity) when the actual fix was a git config
		// inside /var/www/.git/. Three regex variants cover the messages
		// modern git emits when user.email / user.name aren't set.
		{
			name: "git-identity-missing-auto-detect",
			input: FailureInput{
				Phase: PhaseTransport,
				TransportErr: &platform.SSHExecError{
					Hostname: "appdev",
					Output:   "[main (root-commit) abc123] deploy\nfatal: unable to auto-detect email address (got 'zerops@runtime.(none)')",
					Err:      errors.New("exit status 128"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:git-identity-missing",
		},
		{
			name: "git-identity-missing-empty-ident",
			input: FailureInput{
				Phase: PhaseTransport,
				TransportErr: &platform.SSHExecError{
					Hostname: "appdev",
					Output:   "fatal: empty ident name (for <>) not allowed",
					Err:      errors.New("exit status 128"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:git-identity-missing",
		},
		{
			name: "git-identity-missing-tell-me-who",
			input: FailureInput{
				Phase: PhaseTransport,
				TransportErr: &platform.SSHExecError{
					Hostname: "appdev",
					Output:   "*** Please tell me who you are.\nRun\n  git config --global user.email \"you@example.com\"",
					Err:      errors.New("exit status 128"),
				},
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "transport:git-identity-missing",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyDeployFailure(tc.input)
			assertClassification(t, got, tc.wantCategory, tc.wantSignal, "")
		})
	}
}

func TestClassifyDeployFailure_Preflight(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		input        FailureInput
		wantCategory topology.FailureClass
		wantSignal   string
	}{
		{
			name: "dm2-narrow-deployfiles",
			input: FailureInput{
				Phase: PhasePreflight,
				APIErr: platform.NewPlatformError(
					platform.ErrInvalidZeropsYml,
					`self-deploy setup "appdev": deployFiles must be [.] or [./]`,
					"Set deployFiles: [.] for self-deploy.",
				),
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "preflight:dm2-self-deploy-narrow",
		},
		{
			name: "invalid-yaml-baseline",
			input: FailureInput{
				Phase: PhasePreflight,
				APIErr: platform.NewPlatformError(
					platform.ErrInvalidZeropsYml,
					"yaml validation failed",
					"see apiMeta",
				),
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "preflight:invalid-zerops-yaml",
		},
		{
			name: "prerequisite-missing",
			input: FailureInput{
				Phase: PhasePreflight,
				APIErr: platform.NewPlatformError(
					platform.ErrPrerequisiteMissing,
					"zcli not in PATH",
					"install zcli",
				),
			},
			wantCategory: topology.FailureClassConfig,
			wantSignal:   "preflight:prerequisite",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyDeployFailure(tc.input)
			assertClassification(t, got, tc.wantCategory, tc.wantSignal, "")
		})
	}
}

// TestClassifyDeployFailure_NoPhase pins that classification refuses to run
// without a phase — the field is required, no guessing from logs alone.
func TestClassifyDeployFailure_NoPhase(t *testing.T) {
	t.Parallel()
	got := ClassifyDeployFailure(FailureInput{
		BuildLogs: []string{"some logs"},
	})
	if got != nil {
		t.Errorf("expected nil for missing phase, got %+v", got)
	}
}

// TestFailurePhaseFromStatus pins the platform-status → phase mapping that
// callers in tools/deploy_poll.go rely on. Drift here would silently
// skip classification on whichever status fell out.
func TestFailurePhaseFromStatus(t *testing.T) {
	t.Parallel()
	cases := map[string]DeployFailurePhase{
		platform.BuildStatusBuildFailed:          PhaseBuild,
		platform.BuildStatusPreparingRuntimeFail: PhasePrepare,
		platform.BuildStatusDeployFailed:         PhaseInit,
		platform.BuildStatusDeployed:             "",
		"":                                       "",
	}
	for status, want := range cases {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			if got := FailurePhaseFromStatus(status); got != want {
				t.Errorf("FailurePhaseFromStatus(%q) = %q, want %q", status, got, want)
			}
		})
	}
}

// TestClassifyDeployFailure_GitAuthRejected_EnumeratesClosedCauseSet pins the
// F7 enrichment. A git-push auth rejection — which BOTH the deploy path and the
// git-push-setup write-auth probe route through transportGitAuth — must name the
// closed cause-set the agent can act on (the read-only "Public repositories" PAT
// trap; SAML for org repos) and derive the re-scope link + push-minimum scope
// from the single topology owner, not a hand-authored duplicate.
func TestClassifyDeployFailure_GitAuthRejected_EnumeratesClosedCauseSet(t *testing.T) {
	t.Parallel()
	got := ClassifyDeployFailure(FailureInput{
		Phase:    PhaseTransport,
		Strategy: "git-push",
		TransportErr: &platform.SSHExecError{
			Hostname: "appdev",
			Output:   "remote: HTTP Basic: Access denied\nfatal: Authentication failed for 'https://github.com/foo/bar.git/'",
			Err:      errors.New("exit status 128"),
		},
	})
	if got == nil {
		t.Fatal("expected a classification")
	}
	if got.Category != topology.FailureClassCredential {
		t.Errorf("git-auth rejection must stay credential-class; got %s", got.Category)
	}
	// Names the likeliest cause: the read-only "Public repositories" PAT trap.
	if !strings.Contains(got.LikelyCause, "Public repositories") {
		t.Errorf("LikelyCause must name the read-only Public-repositories PAT trap; got: %s", got.LikelyCause)
	}
	// Recovery derives link + push-min scope from the topology owner + names SAML.
	for _, want := range []string{topology.GHPATSettingsURL, topology.GHPATPushMinScope, "SAML"} {
		if !strings.Contains(got.SuggestedAction, want) {
			t.Errorf("SuggestedAction missing %q; got: %s", want, got.SuggestedAction)
		}
	}
	// Still refuses to fabricate a token.
	if !strings.Contains(got.SuggestedAction, "NEVER generate") {
		t.Errorf("SuggestedAction must keep the never-fabricate contract; got: %s", got.SuggestedAction)
	}
}

// TestClassifyDeployFailure_RepoNotFound_DerivesScopeFromOwner pins that the
// repo-not-found recovery derives its PAT link + push-min scope from the same
// topology owner (the hand-authored duplicate at transportGitRepoNotFound is
// retired), so the link/scope can't drift from ghPATScopeRecommendation.
func TestClassifyDeployFailure_RepoNotFound_DerivesScopeFromOwner(t *testing.T) {
	t.Parallel()
	got := ClassifyDeployFailure(FailureInput{
		Phase:    PhaseTransport,
		Strategy: "git-push",
		TransportErr: &platform.SSHExecError{
			Hostname: "appdev",
			Output:   "remote: Repository not found.\nfatal: repository 'https://github.com/foo/bar.git/' not found",
			Err:      errors.New("exit status 128"),
		},
	})
	if got == nil {
		t.Fatal("expected a classification")
	}
	for _, want := range []string{topology.GHPATSettingsURL, topology.GHPATPushMinScope} {
		if !strings.Contains(got.SuggestedAction, want) {
			t.Errorf("SuggestedAction missing %q (must derive from topology owner); got: %s", want, got.SuggestedAction)
		}
	}
}

func assertClassification(t *testing.T, got *topology.DeployFailureClassification, wantCategory topology.FailureClass, wantSignal string, wantInCause string) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected classification, got nil")
	}
	if got.Category != wantCategory {
		t.Errorf("Category = %q, want %q", got.Category, wantCategory)
	}
	if wantSignal != "" && !slices.Contains(got.Signals, wantSignal) {
		t.Errorf("Signals %v missing %q", got.Signals, wantSignal)
	}
	if wantInCause != "" && !strings.Contains(got.LikelyCause, wantInCause) {
		t.Errorf("LikelyCause %q missing %q", got.LikelyCause, wantInCause)
	}
	if got.SuggestedAction == "" {
		t.Errorf("SuggestedAction empty for %v", got.Signals)
	}
}
