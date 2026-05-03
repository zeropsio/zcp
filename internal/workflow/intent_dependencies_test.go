package workflow

import (
	"slices"
	"testing"
)

// testPHPNginxType is the recipe-side type token for PHP+nginx implicit-
// webserver — used across CompareStacks fixtures here. Local const avoids
// repeating the literal 5+ times (goconst threshold).
const testPHPNginxType = "php-nginx"

// testRuntimeTokens enumerates runtime-class tokens that must NEVER appear
// in StackMismatch.Extras (Extras counts dependency families only).
var testRuntimeTokens = []string{"python", "nodejs", "bun", "go", testPHPNginxType}

func TestExtractIntentDependencies_NormalizesPostgresAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		intent string
		want   []string
	}{
		{"node app with postgres", []string{"postgresql"}},
		{"Laravel + PostgreSQL backend", []string{"postgresql"}},
		{"using pgsql for storage", []string{"postgresql"}},
	}
	for _, tt := range tests {
		got := ExtractIntentDependencies(tt.intent)
		if !slices.Equal(got, tt.want) {
			t.Errorf("intent %q: got %v, want %v", tt.intent, got, tt.want)
		}
	}
}

func TestExtractIntentDependencies_RecognizesValkeyRedisMariaDBMySQL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		intent string
		want   []string
	}{
		{"valkey cache", []string{"valkey"}},
		{"redis backed sessions", []string{"valkey"}},    // redis → valkey family
		{"mysql for the dashboard", []string{"mariadb"}}, // mysql → mariadb family
		{"mariadb with replication", []string{"mariadb"}},
		{"laravel + mariadb + valkey", []string{"mariadb", "valkey"}},
	}
	for _, tt := range tests {
		got := ExtractIntentDependencies(tt.intent)
		if !slices.Equal(got, tt.want) {
			t.Errorf("intent %q: got %v, want %v", tt.intent, got, tt.want)
		}
	}
}

func TestExtractIntentDependencies_EmptyAndNoMatches(t *testing.T) {
	t.Parallel()
	if got := ExtractIntentDependencies(""); got != nil {
		t.Errorf("empty intent: want nil, got %v", got)
	}
	if got := ExtractIntentDependencies("just a simple node app"); got != nil {
		t.Errorf("no dependency tokens: want nil, got %v", got)
	}
}

func TestRecipeServiceTypes_ParsesImportYAML(t *testing.T) {
	t.Parallel()
	yaml := []byte(`services:
  - hostname: appdev
    type: php-nginx@8.4
  - hostname: db
    type: postgresql@18
  - hostname: cache
    type: valkey@7.2
`)
	got := RecipeServiceTypes(yaml)
	want := []string{testPHPNginxType, "postgresql", "valkey"}
	if !slices.Equal(got, want) {
		t.Errorf("RecipeServiceTypes: got %v, want %v", got, want)
	}
}

func TestRecipeServiceTypes_StripsVersion(t *testing.T) {
	t.Parallel()
	yaml := []byte(`services:
  - hostname: db
    type: postgresql@18
`)
	got := RecipeServiceTypes(yaml)
	want := []string{"postgresql"}
	if !slices.Equal(got, want) {
		t.Errorf("RecipeServiceTypes: got %v, want %v", got, want)
	}
}

func TestRecipeServiceTypes_MalformedYAMLReturnsNil(t *testing.T) {
	t.Parallel()
	got := RecipeServiceTypes([]byte("this is not yaml at all  invalid"))
	if len(got) > 0 {
		t.Errorf("malformed yaml should return nil/empty, got %v", got)
	}
}

func TestCompareStacks_ContradictedDeps(t *testing.T) {
	t.Parallel()
	// User wants MariaDB (MySQL family), recipe ships PostgreSQL — same DB
	// slot, different family. Recipe should drop.
	mismatch := CompareStacks(
		[]string{"mariadb"},                      // user intent
		[]string{testPHPNginxType, "postgresql"}, // recipe stack
	)
	if !mismatch.HasContradiction() {
		t.Fatal("expected Contradicted: mariadb wanted, postgresql provided")
	}
	if got := mismatch.Contradicted[0]; got.Wanted != "mariadb" || got.Got != "postgresql" {
		t.Errorf("Contradicted: got %+v, want {mariadb,postgresql}", got)
	}
}

func TestCompareStacks_MissingDeps(t *testing.T) {
	t.Parallel()
	// User wants Valkey (cache slot), recipe has Postgres (different slot,
	// no cache at all). Recipe should demote.
	mismatch := CompareStacks(
		[]string{"valkey"},
		[]string{testPHPNginxType, "postgresql"},
	)
	if mismatch.HasContradiction() {
		t.Errorf("expected no contradiction (different slots), got %v", mismatch.Contradicted)
	}
	if !mismatch.HasMissing() {
		t.Fatal("expected MissingFromRecipe: valkey not provided")
	}
}

func TestCompareStacks_ExtrasOnlyDependencyFamilies(t *testing.T) {
	t.Parallel()
	// User asked for nothing specific; recipe over-provisions S3 + Meilisearch.
	// Runtime types (php-nginx) MUST NOT appear in Extras — they're the
	// recipe's runtime, not an over-provisioned dependency.
	mismatch := CompareStacks(
		nil,
		[]string{testPHPNginxType, "postgresql", "object-storage", "meilisearch"},
	)
	if mismatch.HasContradiction() || mismatch.HasMissing() {
		t.Errorf("no user constraints should produce no Contradicted/Missing")
	}
	wantExtras := map[string]bool{"postgresql": true, "object-storage": true, "meilisearch": true}
	if len(mismatch.Extras) != len(wantExtras) {
		t.Fatalf("Extras count: got %d (%v), want %d (%v)", len(mismatch.Extras), mismatch.Extras, len(wantExtras), wantExtras)
	}
	for _, ex := range mismatch.Extras {
		if !wantExtras[ex] {
			t.Errorf("unexpected extra: %q (runtime types must not appear)", ex)
		}
	}
}

func TestCompareStacks_RuntimeNeverFlaggedAsExtra(t *testing.T) {
	t.Parallel()
	// Regression for python-hello-world recipe: agent saw "Over-provisions:
	// adds [python] not in your intent" — but python WAS the user's runtime,
	// not a dependency. Runtime tokens must be silently dropped from Extras.
	mismatch := CompareStacks(
		[]string{"postgresql"}, // user mentioned Postgres
		[]string{"python", "postgresql"},
	)
	for _, ex := range mismatch.Extras {
		if slices.Contains(testRuntimeTokens, ex) {
			t.Errorf("runtime token %q must not appear in Extras", ex)
		}
	}
}

func TestCompareStacks_FullStackMatch_NoMismatch(t *testing.T) {
	t.Parallel()
	// User wants Laravel + Postgres + Valkey; recipe provides exactly that.
	mismatch := CompareStacks(
		[]string{"postgresql", "valkey"},
		[]string{testPHPNginxType, "postgresql", "valkey"},
	)
	if mismatch.HasContradiction() {
		t.Errorf("matching stack: no contradiction expected, got %v", mismatch.Contradicted)
	}
	if mismatch.HasMissing() {
		t.Errorf("matching stack: no missing expected, got %v", mismatch.MissingFromRecipe)
	}
	// php-nginx is a runtime, not in dependencyAliases — appears as extra.
	// That's fine; only conflict-group dependencies trigger drops/demotes.
}
