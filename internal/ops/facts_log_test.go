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
