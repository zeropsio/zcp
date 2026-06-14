package schema

import (
	"sort"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestCatalogStorageAlwaysManaged is the storage-drift tripwire. Every enum
// type whose name signals storage MUST classify as a managed storage service.
// A new storage spelling Zerops ships that topology.canonicalStorageKind does
// not yet recognize fails here after `make schema-sync` — a loud red CI, not a
// silent runtime misclassification (the bug class this plan fixed: objectstorage
// / sharedstorage / seaweedfs fell through to RuntimeDynamic).
func TestCatalogStorageAlwaysManaged(t *testing.T) {
	t.Parallel()
	for _, ty := range Embedded().ImportYml.ServiceTypes {
		low := strings.ToLower(ty)
		if !strings.Contains(low, "storage") && !strings.Contains(low, "seaweedfs") {
			continue
		}
		if !topology.IsManagedService(ty) {
			t.Errorf("storage type %q classifies non-managed — extend topology.canonicalStorageKind", ty)
		}
		if !topology.IsObjectStorageType(ty) && !topology.IsSharedStorageType(ty) {
			t.Errorf("storage type %q matches neither object- nor shared-storage predicate", ty)
		}
	}
}

// TestCatalogManagedBaseNames pins the managed-base-name set the embedded schema
// yields through topology classification. It catches (a) a regression that
// breaks managed detection (the set shrinks) and (b) documents that adding a new
// managed service type to topology is a deliberate two-place change. NOTE: a
// brand-new managed DB type Zerops adds with a novel name is NOT auto-detected —
// the schema carries existence only, not a managed discriminator (§8 (c)
// rejected). When a real new managed type lands, update topology.managedServicePrefixes
// AND this golden set in the same change.
func TestCatalogManagedBaseNames(t *testing.T) {
	t.Parallel()
	// rabbitmq removed by the platform (live schema 2026-06-10 carries no
	// rabbitmq service type); topology keeps it in the classification list
	// (classification ≠ existence — the schema owns existence).
	want := []string{
		"clickhouse", "elasticsearch", "kafka", "keydb", "mariadb",
		"meilisearch", "nats", "object-storage", "postgresql", "qdrant",
		"shared-storage", "typesense", "valkey",
	}
	gotSet := Embedded().ManagedBaseNames()
	got := make([]string, 0, len(gotSet))
	for k := range gotSet {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ManagedBaseNames drift:\n got: %v\nwant: %v\n(schema-sync changed the managed set — confirm + update topology + this golden)", got, want)
	}
}

// TestCatalogCanonicalBaseNameDecorationFree asserts every enum type reduces to
// a decoration-free base (no leftover OS prefix `/` or mode `:`), so the
// symmetric matching key is stable across spellings.
func TestCatalogCanonicalBaseNameDecorationFree(t *testing.T) {
	t.Parallel()
	for _, ty := range Embedded().ImportYml.ServiceTypes {
		base := topology.CanonicalBaseName(ty)
		if strings.ContainsAny(base, "/:") {
			t.Errorf("CanonicalBaseName(%q) = %q still carries decoration", ty, base)
		}
	}
}

// TestCatalogHasServiceType_CompositeAndBare pins the goal-3 behavior: the
// catalog accepts an authored bare form AND its composite equivalent, against
// the embedded schema.
func TestCatalogHasServiceType_CompositeAndBare(t *testing.T) {
	t.Parallel()
	s := Embedded()
	cases := []struct {
		query string
		want  bool
	}{
		{"nodejs@22", true},
		{"alpine/nodejs@22", true},
		{"ubuntu/nodejs@22", true},
		{"postgresql@16", true},
		{"postgresql:single@16", true},
		{"definitely-not-a-real-type@99", false},
		// Bogus (non-mode) `:suffix` must NOT canonicalize away and match a real
		// base — only known modes (single/ha) are stripped.
		{"nodejs:bogus@22", false},
		{"postgresql:bogus@16", false},
		{"objectstorage:bogus", false},
		// A KNOWN mode on a mode-INCAPABLE base must also be rejected: runtimes
		// and object-storage do not take a mode, so the suffix is invalid.
		{"nodejs:ha@22", false},
		{"object-storage:ha", false},
		// A known mode on a mode-capable base is accepted (managed + shared-storage).
		{"postgresql:ha@16", true},
	}
	for _, c := range cases {
		if got := s.HasServiceType(c.query); got != c.want {
			t.Errorf("HasServiceType(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

// TestCatalogSupportsHAVariant pins the strict (NOT bare-equivalence-tolerant)
// HA-capability owner: a type supports `:ha` only when the catalog carries an
// explicit `<base>:ha` entry. meilisearch ships ONLY `:single` — promoting it to
// `:ha` (the pre-fix launch composer behavior) emits a type the platform import
// rejects. Contrast with HasServiceType("postgresql:ha@16")==true above: that is
// deliberately tolerant and must NOT be used for this question.
func TestCatalogSupportsHAVariant(t *testing.T) {
	t.Parallel()
	s := Embedded()
	cases := []struct {
		query string
		want  bool
	}{
		{"postgresql@18", true},        // managed DB, ships :ha
		{"postgresql:single@18", true}, // bare/variant input canonicalizes the same
		{"mariadb@10.6", true},         // ships :ha (the launch-promote-valid case)
		{"valkey@7.2", true},           // cache, ships :ha
		{"meilisearch@1.20", false},    // ships ONLY :single — the break case
		{"meilisearch:single@1.20", false},
		{"object-storage", false}, // no deployment variant at all
		{"nodejs@22", false},      // runtime — not a managed HA type
		{"definitely-not-real@99", false},
	}
	for _, c := range cases {
		if got := s.SupportsHAVariant(c.query); got != c.want {
			t.Errorf("SupportsHAVariant(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

// TestCatalogHasBase_CompositeAndBare pins composite/bare tolerance for build
// and run bases — the false-reject `validateBuildBases`/`CheckZeropsBasesLive`
// hit against the composite-only live schema.
func TestCatalogHasBase_CompositeAndBare(t *testing.T) {
	t.Parallel()
	s := Embedded()
	if !s.HasRunBase("nodejs@22") {
		t.Error("HasRunBase(nodejs@22) = false, want true (bare run base)")
	}
	if !s.HasBuildBase("nodejs@22") {
		t.Error("HasBuildBase(nodejs@22) = false, want true (bare build base)")
	}
	if s.HasRunBase("nodejs@999") {
		t.Error("HasRunBase(nodejs@999) = true, want false (hallucinated version)")
	}
}
