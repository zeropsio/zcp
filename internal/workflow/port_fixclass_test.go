package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestDeriveFixClass_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		class        topology.FailureClass
		signals      []string
		wantKind     FixClassKind
		wantEscalate bool
	}{
		{
			name:     "build+command-not-found→add prepare commands",
			class:    topology.FailureClassBuild,
			signals:  []string{"build:command-not-found"},
			wantKind: FixAddPrepareCommands,
		},
		{
			name:         "build+oom-killed→escalation NOT a fix",
			class:        topology.FailureClassBuild,
			signals:      []string{"build:oom-killed"},
			wantKind:     FixEscalate,
			wantEscalate: true,
		},
		{
			name:     "build no recognized signal→inspect",
			class:    topology.FailureClassBuild,
			signals:  []string{"build:something-unmapped"},
			wantKind: FixInspect,
		},
		{
			name:     "start+db-connection-refused→wire dep refs",
			class:    topology.FailureClassStart,
			signals:  []string{"init:db-connection-refused"},
			wantKind: FixWireDepRefs,
		},
		{
			name:     "start+missing-env→set env",
			class:    topology.FailureClassStart,
			signals:  []string{"init:missing-env-var"},
			wantKind: FixSetEnv,
		},
		{
			name:     "start+migration-failed→move to init commands",
			class:    topology.FailureClassStart,
			signals:  []string{"init:migration-failed"},
			wantKind: FixMoveToInitCommands,
		},
		{
			name:     "start no recognized signal→inspect",
			class:    topology.FailureClassStart,
			signals:  nil,
			wantKind: FixInspect,
		},
		{
			name:     "config→fix glue yaml",
			class:    topology.FailureClassConfig,
			signals:  nil,
			wantKind: FixGlueYAML,
		},
		{
			name:     "credential→git-push-setup",
			class:    topology.FailureClassCredential,
			signals:  nil,
			wantKind: FixGitPushSetup,
		},
		{
			name:     "network→retry",
			class:    topology.FailureClassNetwork,
			signals:  nil,
			wantKind: FixRetry,
		},
		{
			name:     "verify→inspect",
			class:    topology.FailureClassVerify,
			signals:  nil,
			wantKind: FixInspect,
		},
		{
			name:     "other→inspect",
			class:    topology.FailureClassOther,
			signals:  nil,
			wantKind: FixInspect,
		},
		{
			name:         "oom takes precedence over command-not-found when both present",
			class:        topology.FailureClassBuild,
			signals:      []string{"build:command-not-found", "build:oom-killed"},
			wantKind:     FixEscalate,
			wantEscalate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DeriveFixClass(tt.class, tt.signals)
			if got.Kind != tt.wantKind {
				t.Fatalf("DeriveFixClass(%q,%v).Kind = %q, want %q", tt.class, tt.signals, got.Kind, tt.wantKind)
			}
			if got.Escalate != tt.wantEscalate {
				t.Fatalf("DeriveFixClass(%q,%v).Escalate = %v, want %v", tt.class, tt.signals, got.Escalate, tt.wantEscalate)
			}
			if got.Guidance == "" {
				t.Fatalf("DeriveFixClass(%q,%v) returned empty guidance", tt.class, tt.signals)
			}
			// Base derivation never flags the import-override tax — that is a
			// distinct, opt-in derivation (DeriveImportOverrideFixClass).
			if got.RequiresImportOverride {
				t.Fatalf("DeriveFixClass(%q,%v) should not flag RequiresImportOverride", tt.class, tt.signals)
			}
		})
	}
}

func TestDeriveFixClass_PrefersGlueOverImport(t *testing.T) {
	t.Parallel()
	// The config-class guidance must steer toward glue-zerops.yaml and warn
	// against import.yaml edits to an existing hostname (the override tax).
	got := DeriveFixClass(topology.FailureClassConfig, nil)
	if got.Kind != FixGlueYAML {
		t.Fatalf("config class Kind = %q, want %q", got.Kind, FixGlueYAML)
	}
	if got.RequiresImportOverride {
		t.Fatalf("glue-yaml fix must not require import override")
	}
}

func TestDeriveImportOverrideFixClass_FlagsOverrideTax(t *testing.T) {
	t.Parallel()
	got := DeriveImportOverrideFixClass(topology.FailureClassStart, []string{"init:db-connection-refused"})
	if !got.RequiresImportOverride {
		t.Fatalf("DeriveImportOverrideFixClass must set RequiresImportOverride")
	}
	if got.Guidance == "" {
		t.Fatalf("DeriveImportOverrideFixClass must carry guidance")
	}
	// The override-cost warning must be present in the prepended guidance.
	if !strings.Contains(got.Guidance, "override=true") || !strings.Contains(got.Guidance, "iteration") {
		t.Fatalf("override guidance must warn about override=true + the iteration cost; got %q", got.Guidance)
	}
}
