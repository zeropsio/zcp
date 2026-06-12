package port

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// reconTestSchemas builds a minimal catalog covering the managed types the
// recon table cases reference (postgresql, clickhouse, kafka, valkey,
// object-storage) plus a couple of runtime types. Mirrors the embedded-floor
// shape so HasServiceType / ManagedBaseNames answer as the live catalog would.
func reconTestSchemas() *schema.Schemas {
	return &schema.Schemas{
		ImportYml: &schema.ImportYmlSchema{
			ServiceTypes: []string{
				// runtimes
				"nodejs@22", "python@3.12", "go@1",
				// managed
				"postgresql@16", "postgresql@18",
				"clickhouse@25.3",
				"kafka@3.9",
				"valkey@7.2",
				"object-storage",
			},
		},
	}
}

func TestReconClassify_ImageOnly_CraneImageLift(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:            "posthog",
		AcquisitionHint: AcquireHintImageOnly,
		Dependencies:    []string{"postgresql", "clickhouse"},
		Runtimes:        []string{"python@3.12"},
	}
	plan := ReconClassify(desc, reconTestSchemas())
	if plan.Acquisition != AcquireCraneImageLift {
		t.Fatalf("image-only descriptor must classify as crane-image-lift, got %q", plan.Acquisition)
	}
	if plan.Band == BandBail {
		t.Fatalf("image-only is IN-band via crane-lift, must NOT bail")
	}
}

func TestReconClassify_K8sRuntimeOnly_Bails(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:            "some-mesh-operator",
		AcquisitionHint: AcquireHintK8sRuntime,
		Dependencies:    []string{"postgresql"},
		Runtimes:        []string{"go@1"},
	}
	plan := ReconClassify(desc, reconTestSchemas())
	if plan.Acquisition != AcquireBail {
		t.Fatalf("k8s-runtime-orchestration descriptor must classify acquisition=bail, got %q", plan.Acquisition)
	}
	if plan.Band != BandBail {
		t.Fatalf("k8s-runtime-orchestration descriptor must classify band=bail, got %q", plan.Band)
	}
}

func TestReconClassify_SourceAllManaged_Easy(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:            "strapi",
		AcquisitionHint: AcquireHintSourceRepo,
		Dependencies:    []string{"postgresql", "object-storage"},
		Runtimes:        []string{"nodejs@22"},
	}
	plan := ReconClassify(desc, reconTestSchemas())
	if plan.Acquisition != AcquireSourceBuild {
		t.Fatalf("source-repo descriptor must classify as source-build, got %q", plan.Acquisition)
	}
	if plan.Band != BandEasy {
		t.Fatalf("source + all-managed + single runtime must be EASY, got %q", plan.Band)
	}
	// Every declared dep maps to a managed service (no self-run, no constraint).
	for _, d := range plan.Dependencies {
		if d.Mapping != DepMappingManaged {
			t.Fatalf("dep %q expected DepMappingManaged, got %q", d.Declared, d.Mapping)
		}
	}
}

func TestReconClassify_PrebuiltBinaryHint_PrebuiltBinary(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:            "vault",
		AcquisitionHint: AcquireHintBinaryURL,
		Dependencies:    nil,
		Runtimes:        []string{"go@1"},
	}
	plan := ReconClassify(desc, reconTestSchemas())
	if plan.Acquisition != AcquirePrebuiltBinary {
		t.Fatalf("binary-url hint must classify as prebuilt-binary, got %q", plan.Acquisition)
	}
}

func TestReconClassify_ClickhouseKafkaDeps_MapToManaged(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:            "analytics",
		AcquisitionHint: AcquireHintSourceRepo,
		Dependencies:    []string{"clickhouse", "kafka"},
		Runtimes:        []string{"python@3.12"},
	}
	plan := ReconClassify(desc, reconTestSchemas())
	byName := map[string]PortDependency{}
	for _, d := range plan.Dependencies {
		byName[d.Declared] = d
	}
	for _, want := range []string{"clickhouse", "kafka"} {
		d, ok := byName[want]
		if !ok {
			t.Fatalf("dependency %q missing from plan", want)
		}
		if d.Mapping != DepMappingManaged {
			t.Fatalf("dependency %q must map to managed catalog, got %q", want, d.Mapping)
		}
		if d.ManagedType == "" {
			t.Fatalf("dependency %q mapped to managed but ManagedType empty", want)
		}
	}
}

func TestReconClassify_OneSelfRun_Medium(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:            "app-with-exotic-db",
		AcquisitionHint: AcquireHintSourceRepo,
		// "cockroachdb" has no managed equivalent in the catalog → self-run.
		Dependencies: []string{"postgresql", "cockroachdb"},
		Runtimes:     []string{"go@1"},
	}
	plan := ReconClassify(desc, reconTestSchemas())
	var selfRun int
	for _, d := range plan.Dependencies {
		if d.Mapping == DepMappingSelfRun {
			selfRun++
		}
	}
	if selfRun != 1 {
		t.Fatalf("expected exactly one self-run dep, got %d", selfRun)
	}
	if plan.Band != BandMedium {
		t.Fatalf("source + one self-run dep must be MEDIUM, got %q", plan.Band)
	}
}

func TestReconClassify_MultipleSelfRun_Hard(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:            "complex",
		AcquisitionHint: AcquireHintSourceRepo,
		Dependencies:    []string{"cockroachdb", "scylladb"},
		Runtimes:        []string{"go@1"},
	}
	plan := ReconClassify(desc, reconTestSchemas())
	if plan.Band != BandHard {
		t.Fatalf("two self-run deps must be HARD, got %q", plan.Band)
	}
}

func TestReconClassify_ManyRuntimes_Hard(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:            "posthog-like",
		AcquisitionHint: AcquireHintImageOnly,
		Dependencies:    []string{"postgresql", "clickhouse", "kafka", "valkey", "object-storage"},
		Runtimes:        []string{"python@3.12", "python@3.12", "nodejs@22", "go@1"},
	}
	plan := ReconClassify(desc, reconTestSchemas())
	if plan.Band != BandHard {
		t.Fatalf("many runtimes must be HARD, got %q", plan.Band)
	}
}

// TestReconClassify_CrossServiceOrdering_RaisesBandAndConstrains: the
// cross-service-ordering axis raises an otherwise-EASY descriptor to HARD and
// records a retry-until-ready choreography constraint — NOT a bail. Strict
// cross-service runtime init ordering (the PostHog ch-init → migrate → … chain)
// is solved by `zsc execOnce --retryUntilSuccessful`, an in-band idiom, so the
// band rises but acquisition stays in-band and no constraint says "bail".
func TestReconClassify_CrossServiceOrdering_RaisesBandAndConstrains(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:                 "ordered-app",
		AcquisitionHint:      AcquireHintSourceRepo,
		Dependencies:         []string{"postgresql"}, // all-managed, single runtime → would be EASY
		Runtimes:             []string{"nodejs@22"},
		CrossServiceOrdering: true,
	}
	plan := ReconClassify(desc, reconTestSchemas())
	if plan.Band != BandHard {
		t.Fatalf("strict cross-service init ordering must raise the band to HARD, got %q", plan.Band)
	}
	// Acquisition is untouched — ordering is an init-choreography concern, not a bail.
	if plan.Acquisition != AcquireSourceBuild {
		t.Fatalf("cross-service ordering must NOT change acquisition (still source-build), got %q", plan.Acquisition)
	}
	// The recorded constraint must name the retry-until-ready FIX, not a bail.
	var found bool
	for _, c := range plan.Constraints {
		if strings.Contains(c, "retryUntilSuccessful") {
			found = true
		}
		if strings.Contains(c, "bail") {
			t.Fatalf("cross-service ordering constraint must NOT say bail, got %q", c)
		}
	}
	if !found {
		t.Fatalf("cross-service ordering must record a retryUntilSuccessful choreography constraint, got %v", plan.Constraints)
	}
}

// TestReconClassify_CrossServiceOrdering_ImageOnlyStaysCraneLift: the ordering
// axis composes with crane-image-lift acquisition (the real PostHog shape) —
// it raises the band but never flips acquisition to bail.
func TestReconClassify_CrossServiceOrdering_ImageOnlyStaysCraneLift(t *testing.T) {
	t.Parallel()
	desc := PortTargetDescriptor{
		Name:                 "posthog",
		AcquisitionHint:      AcquireHintImageOnly,
		Dependencies:         []string{"postgresql", "clickhouse", "kafka", "valkey", "object-storage"},
		Runtimes:             []string{"python@3.12"},
		CrossServiceOrdering: true,
	}
	plan := ReconClassify(desc, reconTestSchemas())
	if plan.Acquisition != AcquireCraneImageLift {
		t.Fatalf("image-only + ordering must stay crane-image-lift, got %q", plan.Acquisition)
	}
	if plan.Band == BandBail {
		t.Fatalf("cross-service ordering is in-band — must never bail, got %q", plan.Band)
	}
	if plan.Band != BandHard {
		t.Fatalf("ordering raises the band to HARD, got %q", plan.Band)
	}
}
