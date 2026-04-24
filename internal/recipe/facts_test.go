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
