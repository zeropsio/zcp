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
		name         string
		input        FailureInput
		wantCategory topology.FailureClass
		wantSignal   string
		wantInCause  string
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyDeployFailure(tc.input)
			assertClassification(t, got, tc.wantCategory, tc.wantSignal, tc.wantInCause)
		})
	}
}

func TestClassifyDeployFailure_Prepare(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		input        FailureInput
		wantCategory topology.FailureClass
		wantSignal   string
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
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyDeployFailure(tc.input)
			assertClassification(t, got, tc.wantCategory, tc.wantSignal, "")
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
			name: "zcli-auth-failed-push-dev",
			input: FailureInput{
				Phase:    PhaseTransport,
				Strategy: "push-dev",
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
