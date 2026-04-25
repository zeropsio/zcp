package recipe

import (
	"path/filepath"
	"testing"
)

func TestFactRecord_RequiredFields(t *testing.T) {
	t.Parallel()

	// Facts are structured at record time, not classified at consume time
	// (plan §5 P4). Records missing any required field are rejected at
	// Append — the writer never sees a half-typed fact.
	required := []string{"topic", "symptom", "mechanism", "surfaceHint", "citation"}

	for _, missing := range required {
		t.Run("missing="+missing, func(t *testing.T) {
			t.Parallel()
			f := FactRecord{
				Topic:       "cross-service-env-autoinject",
				Symptom:     "503 on deploy",
				Mechanism:   "self-shadow trap",
				SurfaceHint: "platform-trap",
				Citation:    "env-var-model",
			}
			switch missing {
			case "topic":
				f.Topic = ""
			case "symptom":
				f.Symptom = ""
			case "mechanism":
				f.Mechanism = ""
			case "surfaceHint":
				f.SurfaceHint = ""
			case "citation":
				f.Citation = ""
			}
			if err := f.Validate(); err == nil {
				t.Errorf("expected Validate() error for missing %q", missing)
			}
		})
	}
}

func TestFactsLog_AppendRejectsIncomplete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	// Missing citation — must be rejected at Append (not silently written).
	incomplete := FactRecord{
		Topic: "x", Symptom: "y", Mechanism: "z",
		SurfaceHint: "platform-trap",
	}
	if err := log.Append(incomplete); err == nil {
		t.Fatal("Append(incomplete) should have errored")
	}

	records, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records in log after rejected Append, got %d", len(records))
	}
}

func TestFactsLog_AppendAndRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	records := []FactRecord{
		{
			Topic: "cross-service-env-autoinject", Symptom: "503", Mechanism: "shadow",
			SurfaceHint: "platform-trap", Citation: "env-var-model", Author: "scaffold",
		},
		{
			Topic: "rolling-deploys", Symptom: "dropped traffic", Mechanism: "SIGTERM",
			SurfaceHint: "porter-change", Citation: "rolling-deploys", Author: "scaffold",
		},
	}
	for _, r := range records {
		if err := log.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(records) {
		t.Fatalf("Read: len = %d, want %d", len(got), len(records))
	}
	for i := range records {
		if got[i].Topic != records[i].Topic {
			t.Errorf("record %d: Topic = %q, want %q", i, got[i].Topic, records[i].Topic)
		}
		if got[i].RecordedAt == "" {
			t.Errorf("record %d: RecordedAt should be auto-filled", i)
		}
	}
}

// TestFactRecord_AcceptsEnrichedFields — run-11 gap U-2. v3 schema now
// carries failureMode/fixApplied/evidence/scope (the v2 shape that ran-10
// agents reached for naturally). Fields round-trip through facts.jsonl
// and surface to V-1's classifier so self-inflicted shape can be
// detected from fixApplied + failureMode.
func TestFactRecord_AcceptsEnrichedFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	rec := FactRecord{
		Topic:       "deployignore-filters-build-artifact",
		Symptom:     "Cannot find module /var/www/dist/main.js looping every 2s",
		Mechanism:   "deployignore filters the deploy bundle, not just the source upload",
		SurfaceHint: "platform-trap",
		Citation:    "deploy-files",
		FailureMode: "Cannot find module /var/www/dist/main.js",
		FixApplied:  "removed dist from .deployignore",
		Evidence:    "deploy log line 12:35; runtime log loop 12:36-12:55",
		Scope:       "content",
	}
	if err := log.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].FailureMode != rec.FailureMode {
		t.Errorf("FailureMode round-trip: got %q, want %q", got[0].FailureMode, rec.FailureMode)
	}
	if got[0].FixApplied != rec.FixApplied {
		t.Errorf("FixApplied round-trip: got %q, want %q", got[0].FixApplied, rec.FixApplied)
	}
	if got[0].Evidence != rec.Evidence {
		t.Errorf("Evidence round-trip: got %q, want %q", got[0].Evidence, rec.Evidence)
	}
	if got[0].Scope != rec.Scope {
		t.Errorf("Scope round-trip: got %q, want %q", got[0].Scope, rec.Scope)
	}
}

// TestFactRecord_OptionalEnrichedFields — backward-compat: callers that
// don't set the v3 enriched fields still validate + persist correctly.
// No existing call breaks.
func TestFactRecord_OptionalEnrichedFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))

	rec := FactRecord{
		Topic:       "cross-service-env-autoinject",
		Symptom:     "503 on first deploy",
		Mechanism:   "self-shadow trap",
		SurfaceHint: "platform-trap",
		Citation:    "env-var-model",
	}
	if err := log.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := log.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	if got[0].FailureMode != "" || got[0].FixApplied != "" || got[0].Evidence != "" {
		t.Errorf("enriched fields should be empty when not set: %+v", got[0])
	}
}

func TestFactsLog_FilterByHint(t *testing.T) {
	t.Parallel()

	all := []FactRecord{
		{Topic: "a", Symptom: "s", Mechanism: "m", SurfaceHint: "platform-trap", Citation: "c"},
		{Topic: "b", Symptom: "s", Mechanism: "m", SurfaceHint: "porter-change", Citation: "c"},
		{Topic: "c", Symptom: "s", Mechanism: "m", SurfaceHint: "platform-trap", Citation: "c"},
	}

	filtered := FilterByHint(all, "platform-trap")
	if len(filtered) != 2 {
		t.Fatalf("FilterByHint: len = %d, want 2", len(filtered))
	}
	for _, r := range filtered {
		if r.SurfaceHint != "platform-trap" {
			t.Errorf("FilterByHint returned record with hint %q", r.SurfaceHint)
		}
	}
}
