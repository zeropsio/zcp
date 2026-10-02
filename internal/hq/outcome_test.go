package hq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOutcomeOf_RecordsWhatTheMateServerCanSay(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		err      error
		want     Outcome
		recorded bool
	}{
		{"enrolled", nil, Outcome{State: OutcomeEnrolled, At: at}, true},
		{"no official HQ", &NoHQError{}, Outcome{State: OutcomeNoHQ, At: at}, true},
		{
			"HQ's own refusal, with its code",
			fmt.Errorf("enroll: %w", &RefusedError{Status: 404, Code: "not_a_mate"}),
			Outcome{State: OutcomeRefused, Code: "not_a_mate", At: at},
			true,
		},
		{"HQ or Zerops not answering", errors.New("dial tcp: timeout"), Outcome{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, recorded := OutcomeOf(tt.err, at)
			if recorded != tt.recorded || got != tt.want {
				t.Fatalf("OutcomeOf = %+v, %v; want %+v, %v", got, recorded, tt.want, tt.recorded)
			}
		})
	}
}

func TestSaveOutcome_TheDocumentTheServerReads(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "hq", "outcome.json")
	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	if err := SaveOutcome(path, Outcome{State: OutcomeRefused, Code: "not_a_mate", At: at}); err != nil {
		t.Fatalf("SaveOutcome: %v", err)
	}
	if err := SaveOutcome(path, Outcome{State: OutcomeNoHQ, At: at}); err != nil {
		t.Fatalf("SaveOutcome again: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	want := map[string]any{"state": "no_hq", "at": "2026-10-02T12:00:00Z"}
	if len(doc) != len(want) || doc["state"] != want["state"] || doc["at"] != want["at"] {
		t.Fatalf("outcome document = %v; want %v (no code once there is none)", doc, want)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory holds %v (err %v); want the outcome alone, no temp file left", entries, err)
	}
}

func TestKeep_RecordsEachAttemptsOutcome(t *testing.T) {
	t.Parallel()

	a := &attempts{answers: []error{&NoHQError{}, nil}, done: make(chan struct{}), want: 2}
	seen := make(chan error, 4)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go Keep(ctx, a.attempt, KeepOptions{
		Retry:   time.Millisecond,
		Recheck: time.Hour,
		Record:  func(err error) { seen <- err },
	})
	var noHQ *NoHQError
	if first := <-seen; !errors.As(first, &noHQ) {
		t.Fatalf("first recorded = %v; want the missing HQ", first)
	}
	if second := <-seen; second != nil {
		t.Fatalf("second recorded = %v; want the enrollment", second)
	}
}
