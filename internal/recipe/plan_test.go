package recipe

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestWritePlan_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	plan := &Plan{
		Slug:      "synth-showcase",
		Framework: "synth",
		Tier:      "showcase",
		Research:  ResearchResult{CodebaseShape: "monolith", Description: "test"},
		Codebases: []Codebase{
			{Hostname: "apidev", Role: "api", BaseRuntime: "nodejs@22", SourceRoot: "/var/www/apidev"},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", SupportsHA: true},
		},
		Fragments: map[string]string{"root/intro": "Hello"},
	}

	if err := WritePlan(dir, plan); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}
	got, err := ReadPlan(dir)
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	if !reflect.DeepEqual(plan, got) {
		t.Fatalf("round-trip mismatch\nwant: %+v\ngot:  %+v", plan, got)
	}
}

func TestWritePlan_AtomicReplace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	first := &Plan{Slug: "x", Framework: "f1"}
	if err := WritePlan(dir, first); err != nil {
		t.Fatalf("first write: %v", err)
	}
	second := &Plan{Slug: "x", Framework: "f2"}
	if err := WritePlan(dir, second); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := ReadPlan(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Framework != "f2" {
		t.Fatalf("expected framework=f2 after replace, got %q", got.Framework)
	}
}

func TestWritePlan_EmptyOutputRoot_NoOp(t *testing.T) {
	t.Parallel()
	if err := WritePlan("", &Plan{Slug: "x"}); err != nil {
		t.Fatalf("WritePlan with empty root should be no-op, got: %v", err)
	}
}

func TestWritePlan_NilPlan_NoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WritePlan(dir, nil); err != nil {
		t.Fatalf("WritePlan with nil plan should be no-op, got: %v", err)
	}
	if _, err := ReadPlan(dir); err == nil {
		t.Fatalf("expected ReadPlan to fail when WritePlan was a no-op")
	}
}

func TestMergePlan_PersistsToDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", log, dir, nil)

	if err := mergePlan(sess, &Plan{Framework: "synth", Tier: "showcase"}); err != nil {
		t.Fatalf("mergePlan: %v", err)
	}
	got, err := ReadPlan(dir)
	if err != nil {
		t.Fatalf("ReadPlan after mergePlan: %v", err)
	}
	if got.Framework != "synth" || got.Tier != "showcase" {
		t.Fatalf("expected framework=synth tier=showcase, got %+v", got)
	}
	if got.Slug != "synth-showcase" {
		t.Fatalf("expected slug carried from session, got %q", got.Slug)
	}
}
