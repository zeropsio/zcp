package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFactsLog_AppendAndRead_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "zcp-facts-abc.jsonl")

	rec := FactRecord{
		Substep:     "deploy.deploy-dev",
		Codebase:    "workerdev",
		Type:        FactTypeGotchaCandidate,
		Title:       "module: nodenext + raw ts-node",
		Mechanism:   "ts-node -r tsconfig-paths/register",
		FailureMode: "Cannot find module './app.module.js'",
		FixApplied:  "Switch tsconfig to module: commonjs",
		Evidence:    "12:35:26 ts-node failed",
	}
	if err := AppendFact(path, rec); err != nil {
		t.Fatalf("AppendFact: %v", err)
	}

	got, err := ReadFacts(path)
	if err != nil {
		t.Fatalf("ReadFacts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].Title != rec.Title || got[0].Type != rec.Type {
		t.Errorf("round-trip mismatch: got %+v", got[0])
	}
	if got[0].Timestamp == "" {
		t.Error("timestamp should be set by AppendFact")
	}
}

func TestFactsLog_AppendMultipleRecords_Ordered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "zcp-facts-ord.jsonl")

	titles := []string{"first", "second", "third"}
	for _, title := range titles {
		if err := AppendFact(path, FactRecord{
			Substep:  "deploy.init-commands",
			Codebase: "apidev",
			Type:     FactTypeVerifiedBehavior,
			Title:    title,
		}); err != nil {
			t.Fatalf("AppendFact %q: %v", title, err)
		}
	}
	got, err := ReadFacts(path)
	if err != nil {
		t.Fatalf("ReadFacts: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 records, got %d", len(got))
	}
	for i, r := range got {
		if r.Title != titles[i] {
			t.Errorf("[%d] Title=%q, want %q", i, r.Title, titles[i])
		}
	}
}

func TestFactsLog_RequiresTypeAndTitle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "zcp-facts-val.jsonl")

	for name, rec := range map[string]FactRecord{
		"missing type":     {Title: "x"},
		"missing title":    {Type: FactTypeGotchaCandidate},
		"empty everything": {},
	} {
		if err := AppendFact(path, rec); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestFactsLog_RejectsUnknownType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "zcp-facts-type.jsonl")
	err := AppendFact(path, FactRecord{Type: "random-nonsense", Title: "x"})
	if err == nil {
		t.Fatal("expected rejection for unknown record type")
	}
	if !strings.Contains(err.Error(), "unknown fact type") {
		t.Errorf("error should name the problem: %v", err)
	}
}

func TestFactsLog_FactLogPath_UsesSessionID(t *testing.T) {
	t.Parallel()
	path := FactLogPath("sess-xyz")
	if !strings.Contains(path, "sess-xyz") {
		t.Errorf("path should contain session id: %s", path)
	}
	if !strings.HasSuffix(path, ".jsonl") {
		t.Errorf("path should be jsonl: %s", path)
	}
}

func TestFactsLog_ReadsMissingFileAsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got, err := ReadFacts(filepath.Join(dir, "does-not-exist.jsonl"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing file should yield 0 records, got %d", len(got))
	}
}

func TestFactsLog_AllTypesAccepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "all-types.jsonl")
	types := []string{
		FactTypeGotchaCandidate,
		FactTypeIGItemCandidate,
		FactTypeVerifiedBehavior,
		FactTypePlatformObservation,
		FactTypeFixApplied,
		FactTypeCrossCodebaseContract,
	}
	for _, ft := range types {
		if err := AppendFact(path, FactRecord{Type: ft, Title: "t"}); err != nil {
			t.Errorf("type %q rejected: %v", ft, err)
		}
	}
}

// TestFactsLog_AllScopesAccepted — v8.96 Theme B invariant. The four valid
// scopes ("" = legacy content-only default, "content", "downstream", "both")
// must round-trip through AppendFact without rejection. C-0 substrate pin:
// any future commit that tightens or expands the enum must update this test
// deliberately.
func TestFactsLog_AllScopesAccepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "all-scopes.jsonl")
	scopes := []string{
		"", // legacy default — treated as content-only per v8.96 comment
		FactScopeContent,
		FactScopeDownstream,
		FactScopeBoth,
	}
	for _, sc := range scopes {
		if err := AppendFact(path, FactRecord{
			Type:  FactTypeVerifiedBehavior,
			Title: "scope=" + sc,
			Scope: sc,
		}); err != nil {
			t.Errorf("scope %q rejected: %v", sc, err)
		}
	}
	got, err := ReadFacts(path)
	if err != nil {
		t.Fatalf("ReadFacts: %v", err)
	}
	if len(got) != len(scopes) {
		t.Fatalf("want %d records, got %d", len(scopes), len(got))
	}
	for i, rec := range got {
		if rec.Scope != scopes[i] {
			t.Errorf("record[%d] Scope=%q, want %q", i, rec.Scope, scopes[i])
		}
	}
}

// TestFactsLog_RejectsUnknownScope — v8.96 Theme B invariant. A typo'd
// scope (e.g. "downsteam") must return a validation error naming the valid
// values. Silent acceptance would cause downstream-scope facts to default
// to content-lane-only routing, invisibly starving downstream sub-agents.
func TestFactsLog_RejectsUnknownScope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "scope-typo.jsonl")
	err := AppendFact(path, FactRecord{
		Type:  FactTypeGotchaCandidate,
		Title: "typo guard",
		Scope: "downsteam", // deliberate typo — missing the 'r'
	})
	if err == nil {
		t.Fatal("expected rejection for typo'd scope value")
	}
	if !strings.Contains(err.Error(), "unknown scope") {
		t.Errorf("error should name the problem: %v", err)
	}
	// Helpful message must enumerate the valid values so a caller can
	// self-correct without reading source.
	for _, want := range []string{"content", "downstream", "both"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name valid value %q: %v", want, err)
		}
	}
}

// TestFactsLog_ScopeRoundTrip — v8.96 Theme B invariant. The Scope field
// must survive marshal + unmarshal through the jsonl write/read cycle.
// Guards against accidental `json:"-"` or rename regression.
func TestFactsLog_ScopeRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "scope-rt.jsonl")
	rec := FactRecord{
		Type:  FactTypeCrossCodebaseContract,
		Title: "DB_PASS naming",
		Scope: FactScopeDownstream,
	}
	if err := AppendFact(path, rec); err != nil {
		t.Fatalf("AppendFact: %v", err)
	}
	got, err := ReadFacts(path)
	if err != nil {
		t.Fatalf("ReadFacts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].Scope != FactScopeDownstream {
		t.Errorf("Scope round-trip lost value: got %q, want %q", got[0].Scope, FactScopeDownstream)
	}
}

// TestFactsLog_AllRouteTosAccepted — P5 routing enum. Every FactRouteTo*
// constant plus the legacy default (empty string) round-trips through
// AppendFact without rejection. Keeps in sync with atomic-layout.md §4
// SymbolContract plumbing + C-8 writer_manifest_honesty dimensions.
func TestFactsLog_AllRouteTosAccepted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "all-routeto.jsonl")
	routes := []string{
		"", // legacy default — "not yet routed"
		FactRouteToContentGotcha,
		FactRouteToContentIntro,
		FactRouteToContentIG,
		FactRouteToContentEnvComment,
		FactRouteToClaudeMD,
		FactRouteToZeropsYAMLComment,
		FactRouteToScaffoldPreamble,
		FactRouteToFeaturePreamble,
		FactRouteToDiscarded,
	}
	for _, r := range routes {
		if err := AppendFact(path, FactRecord{
			Type:    FactTypeVerifiedBehavior,
			Title:   "routeTo=" + r,
			RouteTo: r,
		}); err != nil {
			t.Errorf("routeTo %q rejected: %v", r, err)
		}
	}
	got, err := ReadFacts(path)
	if err != nil {
		t.Fatalf("ReadFacts: %v", err)
	}
	if len(got) != len(routes) {
		t.Fatalf("want %d records, got %d", len(routes), len(got))
	}
	for i, rec := range got {
		if rec.RouteTo != routes[i] {
			t.Errorf("record[%d] RouteTo=%q, want %q", i, rec.RouteTo, routes[i])
		}
	}
}

// TestFactsLog_RejectsUnknownRouteTo — P5 routing enum. A typo'd route
// (e.g. "content_gocha") must return a validation error naming the valid
// values. Silent acceptance would let the manifest-honesty check's
// downstream consumer defeat itself by comparing against a typo.
func TestFactsLog_RejectsUnknownRouteTo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "routeto-typo.jsonl")
	err := AppendFact(path, FactRecord{
		Type:    FactTypeGotchaCandidate,
		Title:   "routeTo typo guard",
		RouteTo: "content_gocha", // deliberate typo — missing 't'
	})
	if err == nil {
		t.Fatal("expected rejection for typo'd routeTo value")
	}
	if !strings.Contains(err.Error(), "unknown routeTo") {
		t.Errorf("error should name the problem: %v", err)
	}
	// Helpful message must enumerate the valid values so a caller can
	// self-correct without reading source.
	for _, want := range []string{"content_gotcha", "claude_md", "discarded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name valid value %q: %v", want, err)
		}
	}
}

// TestFactsLog_RouteToRoundTrip — RouteTo survives marshal + unmarshal
// through the jsonl write/read cycle. Regression guard for accidental
// `json:"-"` or rename.
func TestFactsLog_RouteToRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "routeto-rt.jsonl")
	rec := FactRecord{
		Type:    FactTypeCrossCodebaseContract,
		Title:   "DB env-var naming",
		Scope:   FactScopeDownstream,
		RouteTo: FactRouteToClaudeMD,
	}
	if err := AppendFact(path, rec); err != nil {
		t.Fatalf("AppendFact: %v", err)
	}
	got, err := ReadFacts(path)
	if err != nil {
		t.Fatalf("ReadFacts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].RouteTo != FactRouteToClaudeMD {
		t.Errorf("RouteTo round-trip: got %q, want %q", got[0].RouteTo, FactRouteToClaudeMD)
	}
	if got[0].Scope != FactScopeDownstream {
		t.Errorf("Scope round-trip broken by RouteTo addition: got %q", got[0].Scope)
	}
}

// TestFactsLog_LegacyRecordWithoutRouteTo — records predating the P5
// addition (no RouteTo field in JSON) must deserialize cleanly. The
// zero-value empty string maps to the legacy "not yet routed" default.
func TestFactsLog_LegacyRecordWithoutRouteTo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	// Hand-craft a legacy-shape JSONL line (no routeTo key present).
	legacyLine := `{"ts":"2025-11-01T12:00:00Z","type":"verified_behavior","title":"legacy record"}` + "\n"
	if err := os.WriteFile(path, []byte(legacyLine), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	got, err := ReadFacts(path)
	if err != nil {
		t.Fatalf("ReadFacts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].RouteTo != "" {
		t.Errorf("legacy record should have empty RouteTo (zero-value default); got %q", got[0].RouteTo)
	}
	if !IsKnownFactRouteTo(got[0].RouteTo) {
		t.Error("empty RouteTo should be accepted by IsKnownFactRouteTo (legacy default)")
	}
}

// TestIsKnownFactRouteTo_Exported — the exported helper reports true for
// every enumerated value + the empty-string default, and false for
// unknown values. Used by downstream manifest-honesty validator (C-8).
func TestIsKnownFactRouteTo_Exported(t *testing.T) {
	t.Parallel()
	valid := []string{
		"",
		FactRouteToContentGotcha,
		FactRouteToContentIntro,
		FactRouteToContentIG,
		FactRouteToContentEnvComment,
		FactRouteToClaudeMD,
		FactRouteToZeropsYAMLComment,
		FactRouteToScaffoldPreamble,
		FactRouteToFeaturePreamble,
		FactRouteToDiscarded,
	}
	for _, s := range valid {
		if !IsKnownFactRouteTo(s) {
			t.Errorf("IsKnownFactRouteTo(%q) = false, want true", s)
		}
	}
	invalid := []string{
		"content_gocha",
		"clude_md",
		"readme",
		"unknown",
	}
	for _, s := range invalid {
		if IsKnownFactRouteTo(s) {
			t.Errorf("IsKnownFactRouteTo(%q) = true, want false", s)
		}
	}
}

func TestFactsLog_JSONLFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "jsonl.jsonl")
	if err := AppendFact(path, FactRecord{Type: FactTypeGotchaCandidate, Title: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := AppendFact(path, FactRecord{Type: FactTypeGotchaCandidate, Title: "second"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), string(raw))
	}
	for i, line := range lines {
		var rec FactRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d not valid JSON: %v", i, err)
		}
	}
}
