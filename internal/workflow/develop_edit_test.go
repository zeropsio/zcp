package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// witness: internal/workflow/compute_envelope.go | return func(c *envelopeConfig) { c.edit = true } → return func(c *envelopeConfig) { c.edit = false }
func TestEditEnvelope_IG2_DoesNotReadOrWriteWorkFile(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		edit      bool
		wantError bool
	}{
		{"edit ignores corrupt deploy session", true, false},
		{"deploy reads corrupt deploy session", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := workSessionPath(dir, os.Getpid())
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			const before = "{corrupt deploy session"
			if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
				t.Fatal(err)
			}
			var options []EnvelopeOption
			if tt.edit {
				options = append(options, WithEditPhase())
			}
			env, err := ComputeEnvelope(context.Background(), nil, dir, "", runtime.Info{}, time.Time{}, options...)
			if (err != nil) != tt.wantError {
				t.Fatalf("envelope error = %v, want error %v", err, tt.wantError)
			}
			if tt.edit && (env.Phase != PhaseDevelopEdit || env.WorkSession != nil) {
				t.Errorf("edit envelope = %+v", env)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != before {
				t.Errorf("work file = %q, error %v", after, err)
			}
		})
	}
}

// witness: internal/workflow/render.go | for _, copy := range copies { → for _, copy := range copies[:1] {
func TestEditCopies_IG3_RendersEveryCopyAndStep(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		copies []WhereCopy
		want   []string
	}{
		{"one copy", []WhereCopy{{Name: "pool-1", Branch: "one", Step: &EditStep{Action: "commit", Copy: "pool-1"}}}, []string{"pool-1", "commit"}},
		{"both copies", []WhereCopy{{Name: "pool-1", Branch: "one", Step: &EditStep{Action: "commit", Copy: "pool-1"}}, {Name: "pool-2", Branch: "two", Step: &EditStep{Action: "push", Copy: "pool-2"}}}, []string{"pool-1", "commit", "pool-2", "push"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderEditCopies(WhereFacts{Copies: tt.copies})
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("briefing %q lacks %q", got, want)
				}
			}
		})
	}
}

// witness: internal/workflow/compute_envelope.go | HasLiveCode:  svc.HasDeployedCode(), → HasLiveCode:  false,
func TestEditLiveCode_IG4_ProjectsTheActiveVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		active *platform.ActiveAppVersionDigest
		want   bool
	}{
		{"absent", nil, false},
		{"empty placeholder", &platform.ActiveAppVersionDigest{ID: "v1", Source: platform.AppVersionSourceNone}, false},
		{"real deploy", &platform.ActiveAppVersionDigest{ID: "v2", Source: "CLI", Built: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := platform.ServiceStack{Name: "dev", ActiveAppVersion: tt.active}
			if got := buildOneSnapshot(svc, nil, nil).HasLiveCode; got != tt.want {
				t.Errorf("has live code = %v, want %v", got, tt.want)
			}
		})
	}
}

// witness: internal/content/atoms/develop-edit.md | phases: [develop-edit] → phases: [develop-active]
func TestEditBriefing_IG5_SelectsEditGuidance(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("load atoms: %v", err)
	}
	for _, tt := range []struct {
		name    string
		phase   Phase
		runtime topology.RuntimeClass
		want    bool
	}{
		{"classic edit", PhaseDevelopEdit, topology.RuntimeDynamic, true},
		{"pool edit", PhaseDevelopEdit, topology.RuntimePool, true},
		{"deploy session", PhaseDevelopActive, topology.RuntimeDynamic, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{Phase: tt.phase, Environment: EnvContainer, Mate: true,
				Services: []ServiceSnapshot{{Hostname: "dev", RuntimeClass: tt.runtime}}}
			matches, err := Synthesize(env, corpus)
			if err != nil {
				t.Fatalf("synthesize: %v", err)
			}
			if got := atomFired(matches, "develop-edit"); got != tt.want {
				t.Errorf("edit guidance = %t, want %t", got, tt.want)
			}
		})
	}
}

// witness: internal/content/atoms/develop-dev-pool.md | phases: [develop-active, develop-edit] → phases: [develop-active]
func TestEditBriefing_IG6_SelectsThePoolLoop(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("load atoms: %v", err)
	}
	for _, tt := range []struct {
		name    string
		runtime topology.RuntimeClass
		want    bool
	}{
		{"pool", topology.RuntimePool, true},
		{"classic", topology.RuntimeDynamic, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{Phase: PhaseDevelopEdit, Environment: EnvContainer, Mate: true,
				Services: []ServiceSnapshot{{Hostname: "dev", RuntimeClass: tt.runtime}}}
			matches, err := Synthesize(env, corpus)
			if err != nil {
				t.Fatalf("synthesize: %v", err)
			}
			if got := atomFired(matches, "develop-dev-pool"); got != tt.want {
				t.Errorf("pool guidance = %t, want %t", got, tt.want)
			}
		})
	}
}
