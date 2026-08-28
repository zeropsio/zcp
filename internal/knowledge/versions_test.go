// Tests for: schema-derived briefing catalog views
// (internal/knowledge/catalog_view.go, versions_format.go, versions.go).
//
// The briefing functions consume the schema-derived catalog (*schema.Schemas),
// not the deleted live stack-types API. Fixtures are built either from the real
// committed schema (schema.Embedded) for "real catalog" grouping cases, or from
// a small constructed *schema.Schemas for targeted [B] / Build-only /
// version-check outcomes.
package knowledge

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

// constructedCatalog returns a small, deterministic catalog: one runtime base
// that is also a build base (nodejs -> [B]), one runtime-only base (nginx),
// one managed base built from mode-encoded composite types (postgresql), object
// + shared storage, and a build-only base (php) with no matching runtime type.
func constructedCatalog() *schema.Schemas {
	return &schema.Schemas{
		ImportYml: &schema.ImportYmlSchema{ServiceTypes: []string{
			"alpine/nodejs@22", "ubuntu/nodejs@22", "nginx@1.22",
			"postgresql:single@18", "postgresql:ha@18",
			"object-storage", "shared-storage",
		}},
		ZeropsYml: &schema.ZeropsYmlSchema{
			BuildBases: []string{"nodejs@22", "php@8.4", "php@8.1"},
			RunBases:   []string{"nodejs@22"},
		},
	}
}

// osLegendCatalogWant is the exact OS-identifier legend line both catalog
// renderers must emit for osLegendCatalog(): nodejs ships both alpine/ and
// ubuntu/ (no exception), deno is ubuntu-only, docker is alpine-only.
const osLegendCatalogWant = "Identifiers: runtime = `<os>/<tech>@<ver>` (e.g. ubuntu/nodejs@22, alpine/nodejs@22) — every runtime below exists as alpine/ and ubuntu/ except ubuntu-only: deno; alpine-only: docker. Managed = `<tech>:single@<ver>` or `<tech>:ha@<ver>`. A bare `<tech>@<ver>` is legacy (import resolves it to ubuntu, zerops.yaml to alpine) — always write the prefix."

// osLegendCatalog returns a catalog with one both-OS runtime (nodejs), one
// ubuntu-only runtime (deno), one alpine-only runtime (docker), and a
// mode-encoded managed base (postgresql) — enough to derive a non-trivial
// exception clause in the OS-identifier legend line from schema data alone.
func osLegendCatalog() *schema.Schemas {
	return &schema.Schemas{
		ImportYml: &schema.ImportYmlSchema{ServiceTypes: []string{
			"alpine/nodejs@22", "ubuntu/nodejs@22",
			"ubuntu/deno@2", "alpine/docker@26",
			"postgresql:single@18", "postgresql:ha@18",
		}},
		ZeropsYml: &schema.ZeropsYmlSchema{
			BuildBases: []string{"nodejs@22"},
			RunBases:   []string{"nodejs@22"},
		},
	}
}

// --- OS-identifier legend ---

// TestFormatStackList_OSLegend pins that FormatStackList emits the
// OS-identifier legend as the line immediately following the heading — before
// the "Pick a concrete version…" paragraph and the Runtime/Managed lines —
// and that its except-clause is derived from the schema (deno ubuntu-only,
// docker alpine-only; nodejs, seeing both prefixes, is not excepted).
func TestFormatStackList_OSLegend(t *testing.T) {
	t.Parallel()

	result := FormatStackList(osLegendCatalog())
	lines := strings.Split(result, "\n")

	if len(lines) < 2 || !strings.HasPrefix(lines[0], "## Available Service Stacks") {
		t.Fatalf("expected heading as first line, got: %q", lines[0])
	}
	if lines[1] != osLegendCatalogWant {
		t.Errorf("expected OS legend right after the heading, got line 2:\n%q\nwant:\n%q", lines[1], osLegendCatalogWant)
	}
	if got := strings.Count(result, "Identifiers: runtime ="); got != 1 {
		t.Errorf("expected exactly one legend line, got %d occurrences in:\n%s", got, result)
	}
	legendIdx := strings.Index(result, osLegendCatalogWant)
	runtimeIdx := strings.Index(result, "Runtime:")
	if legendIdx < 0 || runtimeIdx < 0 || legendIdx >= runtimeIdx {
		t.Errorf("legend (idx %d) must appear before the Runtime line (idx %d):\n%s", legendIdx, runtimeIdx, result)
	}
}

// TestFormatServiceStacks_OSLegend mirrors TestFormatStackList_OSLegend for
// the briefing renderer: legend right after the heading, before the
// "[B]=…" line and the Runtime line.
func TestFormatServiceStacks_OSLegend(t *testing.T) {
	t.Parallel()

	result := FormatServiceStacks(osLegendCatalog())
	lines := strings.Split(result, "\n")

	if len(lines) < 2 || !strings.HasPrefix(lines[0], "## Service Stacks") {
		t.Fatalf("expected heading as first line, got: %q", lines[0])
	}
	if lines[1] != osLegendCatalogWant {
		t.Errorf("expected OS legend right after the heading, got line 2:\n%q\nwant:\n%q", lines[1], osLegendCatalogWant)
	}
	if got := strings.Count(result, "Identifiers: runtime ="); got != 1 {
		t.Errorf("expected exactly one legend line, got %d occurrences in:\n%s", got, result)
	}
	legendIdx := strings.Index(result, osLegendCatalogWant)
	runtimeIdx := strings.Index(result, "Runtime:")
	if legendIdx < 0 || runtimeIdx < 0 || legendIdx >= runtimeIdx {
		t.Errorf("legend (idx %d) must appear before the Runtime line (idx %d):\n%s", legendIdx, runtimeIdx, result)
	}
}

// TestFormatStackList_OSLegend_NoExceptionWhenAllBothOS pins that the
// except-clause is omitted entirely when every runtime in the catalog has
// both OS prefixes — the legend must not print a dangling "except" with
// nothing after it.
func TestFormatStackList_OSLegend_NoExceptionWhenAllBothOS(t *testing.T) {
	t.Parallel()

	s := &schema.Schemas{
		ImportYml: &schema.ImportYmlSchema{ServiceTypes: []string{
			"alpine/nodejs@22", "ubuntu/nodejs@22",
			"alpine/python@3.12", "ubuntu/python@3.12",
		}},
		ZeropsYml: &schema.ZeropsYmlSchema{RunBases: []string{"nodejs@22"}},
	}
	result := FormatStackList(s)
	want := "Identifiers: runtime = `<os>/<tech>@<ver>` (e.g. ubuntu/nodejs@22, alpine/nodejs@22) — every runtime below exists as alpine/ and ubuntu/. Managed = `<tech>:single@<ver>` or `<tech>:ha@<ver>`. A bare `<tech>@<ver>` is legacy (import resolves it to ubuntu, zerops.yaml to alpine) — always write the prefix."
	if !strings.Contains(result, want) {
		t.Errorf("expected no except-clause when every runtime has both OS prefixes, got:\n%s", result)
	}
	if strings.Contains(result, "except") {
		t.Errorf("unexpected except-clause in:\n%s", result)
	}
}

// --- FormatStackList Tests ---

func TestFormatStackList_Groups(t *testing.T) {
	t.Parallel()

	result := FormatStackList(schema.Embedded())

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "Available Service Stacks") {
		t.Error("missing header")
	}
	if !strings.Contains(result, "Runtime:") {
		t.Error("missing Runtime category")
	}
	if !strings.Contains(result, "Managed:") {
		t.Error("missing Managed category")
	}
	// Active concrete versions, newest marked (latest), no rolling tag.
	if !strings.Contains(result, "nodejs@24 (latest) · 22 · 20") {
		t.Errorf("expected concrete-leaf notation for nodejs, got: %s", result)
	}
	if strings.Contains(result, "nodejs@latest") || strings.Contains(result, "@{") {
		t.Errorf("rolling tag or brace notation leaked into presentation: %s", result)
	}
	// Managed bases are grouped under Managed (canonicalized to bare base).
	if !strings.Contains(result, "postgresql@18 (latest) · 17 · 16 · 14") {
		t.Errorf("expected postgresql managed line, got: %s", result)
	}
}

func TestFormatStackList_Empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schemas *schema.Schemas
	}{
		{"nil", nil},
		{"empty importYml", &schema.Schemas{ImportYml: nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FormatStackList(tt.schemas)
			if result != "" {
				t.Errorf("expected empty string, got: %q", result)
			}
		})
	}
}

// --- FormatVersionCheck Tests ---

func TestFormatVersionCheck_AllValid(t *testing.T) {
	t.Parallel()

	result := FormatVersionCheck("nodejs@22", []string{"postgresql@18"}, constructedCatalog())

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "Version Check") {
		t.Error("missing header")
	}
	// All valid — checkmarks, no warnings.
	if !strings.Contains(result, "✓") {
		t.Error("expected checkmark for valid types")
	}
	if strings.Contains(result, "⚠") {
		t.Errorf("expected no warnings for valid types, got: %s", result)
	}
}

func TestFormatVersionCheck_InvalidVersion(t *testing.T) {
	t.Parallel()

	// nodejs@99 is not an available version of the known base nodejs.
	result := FormatVersionCheck("nodejs@99", []string{"postgresql@18"}, constructedCatalog())

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "⚠") {
		t.Error("expected warning for invalid version nodejs@99")
	}
	// Warning lists the available versions of the known base.
	if !strings.Contains(result, "not found. Available:") || !strings.Contains(result, "nodejs@22") {
		t.Errorf("expected suggestion of valid nodejs versions, got: %s", result)
	}
}

func TestFormatVersionCheck_UnknownBase(t *testing.T) {
	t.Parallel()

	result := FormatVersionCheck("ruby@3", nil, constructedCatalog())

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "⚠") {
		t.Error("expected warning for unknown base type ruby")
	}
	if !strings.Contains(result, "unknown type") {
		t.Errorf("expected 'unknown type' for a base absent from the catalog, got: %s", result)
	}
}

func TestFormatVersionCheck_Empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schemas *schema.Schemas
	}{
		{"nil", nil},
		{"empty importYml", &schema.Schemas{ImportYml: nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := FormatVersionCheck("nodejs@22", []string{"postgresql@18"}, tt.schemas)
			if result != "" {
				t.Errorf("expected empty string, got: %q", result)
			}
		})
	}
}

func TestFormatVersionCheck_BareNameNormalized(t *testing.T) {
	t.Parallel()

	// "postgresql" without version — should normalize to the latest available
	// version and pass rather than warn.
	result := FormatVersionCheck("nodejs@22", []string{"postgresql"}, constructedCatalog())

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	// No warning naming postgresql — the bare name resolved to postgresql@18.
	if strings.Contains(result, "⚠") && strings.Contains(result, "postgresql") {
		t.Errorf("bare 'postgresql' should normalize to an available version and pass, got: %s", result)
	}
	if !strings.Contains(result, "✓") {
		t.Error("expected checkmarks for valid types")
	}
}

func TestFormatVersionCheck_BareRuntimeNormalized(t *testing.T) {
	t.Parallel()

	// "nodejs" without version — should normalize to an available version.
	result := FormatVersionCheck("nodejs", nil, constructedCatalog())

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if strings.Contains(result, "⚠") {
		t.Errorf("bare 'nodejs' should normalize to an available version and pass, got: %s", result)
	}
}

// ValidateServiceTypes tests were removed in W6 of plans/api-validation-plumbing.md.
// The Zerops API validator is authoritative for every rule this package used
// to duplicate (type existence, mode enum, mode-required-for-managed,
// objectStoragePolicy enum); coverage for those error shapes now lives in
// internal/platform/zerops_errors_test.go (APIMeta plumbing) and
// internal/platform/zerops_validate_test.go (ValidateZeropsYaml surface).

// --- FormatServiceStacks Tests ---

func TestFormatServiceStacks_Empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schemas *schema.Schemas
	}{
		{"nil", nil},
		{"empty importYml", &schema.Schemas{ImportYml: nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if result := FormatServiceStacks(tt.schemas); result != "" {
				t.Errorf("expected empty string, got: %q", result)
			}
		})
	}
}

func TestFormatServiceStacks_BuildRunCrossReference(t *testing.T) {
	t.Parallel()

	result := FormatServiceStacks(constructedCatalog())

	// nodejs is both a runtime base and a build base — marked [B].
	if !strings.Contains(result, "nodejs@22 [B]") {
		t.Errorf("nodejs should show [B] (also a build base), got: %s", result)
	}
	// nginx is runtime-only (not in BuildBases) — no [B].
	if strings.Contains(result, "nginx@1.22 [B]") {
		t.Error("nginx should not show [B] (not a build base)")
	}
	// postgresql is a managed service — no [B].
	if strings.Contains(result, "postgresql@18 [B]") {
		t.Error("postgresql should not show [B] (managed service)")
	}
}

func TestFormatServiceStacks_UnmatchedBuildVersions(t *testing.T) {
	t.Parallel()

	result := FormatServiceStacks(constructedCatalog())

	// php is a build base with no matching runtime type — surfaced as Build-only.
	if !strings.Contains(result, "Build-only:") {
		t.Errorf("should have Build-only section for build-only bases, got: %s", result)
	}
	if !strings.Contains(result, "php@8.4 (latest) · 8.1") {
		t.Errorf("should show php build versions in compact brace notation, got: %s", result)
	}
	// nodejs is both runtime and build — marked [B], not Build-only.
	if !strings.Contains(result, "nodejs@22 [B]") {
		t.Error("nodejs should show [B]")
	}
}

func TestFormatServiceStacks_CategoryOrdering(t *testing.T) {
	t.Parallel()

	result := FormatServiceStacks(constructedCatalog())

	runtimeIdx := strings.Index(result, "Runtime:")
	managedIdx := strings.Index(result, "Managed:")
	sharedIdx := strings.Index(result, "Shared storage:")
	objectIdx := strings.Index(result, "Object storage:")

	if runtimeIdx < 0 || managedIdx < 0 || sharedIdx < 0 || objectIdx < 0 {
		t.Fatalf("missing category sections: runtime=%d, managed=%d, shared=%d, object=%d",
			runtimeIdx, managedIdx, sharedIdx, objectIdx)
	}
	if runtimeIdx >= managedIdx {
		t.Errorf("Runtime (%d) should appear before Managed (%d)", runtimeIdx, managedIdx)
	}
	if managedIdx >= sharedIdx {
		t.Errorf("Managed (%d) should appear before Shared storage (%d)", managedIdx, sharedIdx)
	}
	if sharedIdx >= objectIdx {
		t.Errorf("Shared storage (%d) should appear before Object storage (%d)", sharedIdx, objectIdx)
	}
}

func TestFormatServiceStacks_StorageBuckets(t *testing.T) {
	t.Parallel()

	// Runtime-only catalog: no managed, no storage — those lines must be absent.
	runtimeOnly := &schema.Schemas{
		ImportYml: &schema.ImportYmlSchema{ServiceTypes: []string{"nodejs@22"}},
		ZeropsYml: &schema.ZeropsYmlSchema{RunBases: []string{"nodejs@22"}},
	}
	result := FormatServiceStacks(runtimeOnly)

	if !strings.Contains(result, "nodejs@22") {
		t.Error("should contain the runtime base")
	}
	if strings.Contains(result, "Managed:") {
		t.Error("should not emit a Managed line when there are no managed services")
	}
	if strings.Contains(result, "Shared storage:") || strings.Contains(result, "Object storage:") {
		t.Error("should not emit storage lines when there is no storage type")
	}
}

func TestFormatServiceStacks_NilSchema(t *testing.T) {
	t.Parallel()

	if result := FormatServiceStacks(nil); result != "" {
		t.Errorf("expected empty string for nil schema, got: %q", result)
	}
}

func TestCompactBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bv   baseVersions
		want string
	}{
		{"single", baseVersions{base: "nodejs", versions: []string{"22"}}, "nodejs@22"},
		{"versionless", baseVersions{base: "static"}, "static"},
		{"multi-concrete-leaves", baseVersions{base: "nodejs", versions: []string{"18", "20", "22"}}, "nodejs@22 (latest) · 20 · 18"},
		{"drops-family-and-rolling", baseVersions{base: "bun", versions: []string{"1", "1.2", "1.2.2", "1.3", "1.3.9", "canary", "latest"}}, "bun@1.3.9 (latest) · 1.2.2"},
		{"rolling-only-shows-rolling", baseVersions{base: "rust", versions: []string{"stable", "nightly", "canary"}}, "rust@stable · nightly · canary"},
		{"versionless-base", baseVersions{base: "object-storage"}, "object-storage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compactBase(tt.bv)
			if got != tt.want {
				t.Errorf("compactBase(%v) = %q, want %q", tt.bv, got, tt.want)
			}
		})
	}
}

// --- canonicalization / dedup ---

// TestCatalogView_CanonicalizesAndDedups pins the base-canonicalization +
// dedup contract: OS-prefixed runtime variants collapse to one bare base, and
// mode-encoded managed composites collapse to one bare managed base.
//
// Assertions are scoped to the Runtime:/Managed: catalog lines, not the whole
// FormatStackList output — the OS-identifier legend line (right after the
// heading) legitimately mentions "ubuntu/nodejs@22" as a worked example and
// "<tech>:single@<ver>"/"<tech>:ha@<ver>" as the managed identifier form; a
// whole-output scan would false-positive on the legend itself.
func TestCatalogView_CanonicalizesAndDedups(t *testing.T) {
	t.Parallel()

	result := FormatStackList(constructedCatalog())
	runtimeLine := catalogSectionLine(t, result, "Runtime:")
	managedLine := catalogSectionLine(t, result, "Managed:")

	// alpine/nodejs@22 + ubuntu/nodejs@22 collapse to a single nodejs@22.
	if !strings.Contains(runtimeLine, "nodejs@22") || strings.Contains(runtimeLine, "alpine/nodejs") || strings.Contains(runtimeLine, "ubuntu/nodejs") {
		t.Errorf("OS-prefixed runtime variants should collapse to bare nodejs@22, got: %s", runtimeLine)
	}
	if strings.Contains(runtimeLine, "nodejs@{") {
		t.Errorf("duplicate nodejs@22 variants should dedup to a single version, got: %s", runtimeLine)
	}
	// postgresql:single@18 + postgresql:ha@18 collapse to a single postgresql@18.
	if !strings.Contains(managedLine, "postgresql@18") || strings.Contains(managedLine, "postgresql:single") || strings.Contains(managedLine, "postgresql:ha") {
		t.Errorf("mode-encoded managed composites should collapse to bare postgresql@18, got: %s", managedLine)
	}
}

// catalogSectionLine returns the single line in result starting with prefix
// (e.g. "Runtime:", "Managed:"), failing the test if there isn't exactly one.
func catalogSectionLine(t *testing.T, result, prefix string) string {
	t.Helper()
	var found string
	n := 0
	for line := range strings.SplitSeq(result, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = line
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly one %q line, found %d in:\n%s", prefix, n, result)
	}
	return found
}

// TestFormatVersionCheck_StorageVersionless pins that versionless storage
// services validate as ✓ rather than "unknown type" — storage has no entry in
// the version index, so FormatVersionCheck must accept the kind directly
// (alias-aware: object-storage / shared-storage / seaweedfs).
func TestFormatVersionCheck_StorageVersionless(t *testing.T) {
	t.Parallel()
	s := &schema.Schemas{
		ImportYml: &schema.ImportYmlSchema{ServiceTypes: []string{"object-storage", "shared-storage", "seaweedfs@3", "nodejs@22"}},
		ZeropsYml: &schema.ZeropsYmlSchema{RunBases: []string{"nodejs@22"}, BuildBases: []string{"nodejs@22"}},
	}
	out := FormatVersionCheck("", []string{"object-storage", "shared-storage", "seaweedfs@3"}, s)
	for _, want := range []string{"✓ `object-storage`", "✓ `shared-storage`", "✓ `seaweedfs@3`"} {
		if !strings.Contains(out, want) {
			t.Errorf("valid storage version-check missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "unknown type") {
		t.Errorf("valid storage services wrongly reported unknown:\n%s", out)
	}

	// A bogus storage version is NOT in the schema → must NOT emit ✓ off the
	// kind alone (regression guard for the schema-membership gate).
	bogus := FormatVersionCheck("", []string{"seaweedfs@99"}, s)
	if strings.Contains(bogus, "✓ `seaweedfs@99`") {
		t.Errorf("bogus storage version wrongly accepted:\n%s", bogus)
	}
}

// TestFormatVersionCheck_ModeOnIncapableBase pins that a `:mode` on a base that
// does not take a mode (runtime / object-storage) is NOT accepted on the
// version-check path — mirrors the catalog matcher's mode-capability guard so
// both existence paths agree (`nodejs:ha@22` must not get a ✓).
func TestFormatVersionCheck_ModeOnIncapableBase(t *testing.T) {
	t.Parallel()
	s := &schema.Schemas{
		ImportYml: &schema.ImportYmlSchema{ServiceTypes: []string{"alpine/nodejs@22", "object-storage", "postgresql:single@18", "postgresql:ha@18"}},
		ZeropsYml: &schema.ZeropsYmlSchema{RunBases: []string{"alpine/nodejs@22"}, BuildBases: []string{"nodejs@22"}},
	}
	out := FormatVersionCheck("nodejs:ha@22", []string{"object-storage:ha", "postgresql:single@18"}, s)
	// Reject both the literal request AND its canonical collapse — without the
	// guard, canonRequest strips `:ha` and the line would print `✓ nodejs@22`,
	// so asserting only the literal would miss a guard removal.
	for _, bad := range []string{"✓ `nodejs:ha@22`", "✓ `nodejs@22`", "✓ `object-storage:ha`", "✓ `object-storage`"} {
		if strings.Contains(out, bad) {
			t.Errorf("mode on a mode-incapable base wrongly accepted (%q):\n%s", bad, out)
		}
	}
	// A valid managed mode-encoded request resolves (shown canonicalized to the
	// bare base — writeVersionLine prints the normalized form).
	if !strings.Contains(out, "✓ `postgresql@18`") {
		t.Errorf("valid managed mode-encoded type should be accepted:\n%s", out)
	}
}
