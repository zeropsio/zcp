package hq

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// attempts answers each attempt in turn, the last one staying, and records
// when each was made.
type attempts struct {
	mu      sync.Mutex
	answers []error
	made    []time.Time
	done    chan struct{}
	want    int
	// keyUnnamed is each enrollment's KeyUnnamed.
	keyUnnamed string
}

func (a *attempts) attempt(context.Context) (Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.made = append(a.made, time.Now())
	if len(a.made) == a.want {
		close(a.done)
	}
	err := a.answers[min(len(a.made)-1, len(a.answers)-1)]
	if err != nil {
		return Result{}, err
	}
	return Result{HQ: "https://hq.example", Changed: len(a.made) == 1, KeyUnnamed: a.keyUnnamed}, nil
}

func TestKeep_RetriesWhatIsNotYetPossible_RechecksWhatHolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		answers []error
		// wantGapAtLeast is the shortest gap expected between the last two
		// attempts: a retry's backoff after failures, the recheck after a
		// success.
		wantGapAtLeast time.Duration
	}{
		{"no official HQ yet, then enrolled", []error{&NoHQError{Official: Official{Verdict: VerdictNone}}, nil, nil}, 40 * time.Millisecond},
		{"no Mate record in HQ yet, then enrolled", []error{&RefusedError{Status: 403, Code: "not_a_mate"}, nil, nil}, 40 * time.Millisecond},
		{"HQ unreachable, again and again", []error{errors.New("dial tcp: refused")}, 4 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &attempts{answers: tt.answers, done: make(chan struct{}), want: 3}
			ctx, cancel := context.WithCancel(context.Background())
			ended := make(chan struct{})
			go func() {
				defer close(ended)
				Keep(ctx, a.attempt, KeepOptions{Retry: 2 * time.Millisecond, RetryMax: 8 * time.Millisecond, Recheck: 50 * time.Millisecond})
			}()
			select {
			case <-a.done:
			case <-time.After(5 * time.Second):
				t.Fatal("Keep stopped attempting: a refusal must never end it")
			}
			cancel()
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("Keep outlived its context")
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			if gap := a.made[2].Sub(a.made[1]); gap < tt.wantGapAtLeast {
				t.Errorf("gap before the third attempt = %s, want at least %s", gap, tt.wantGapAtLeast)
			}
		})
	}
}

func TestKeep_SaysWhyHQWasNotToldTheKeysID(t *testing.T) {
	t.Parallel()
	said := make(chan string, 1)
	a := &attempts{answers: []error{nil}, keyUnnamed: "hq refused: 409 key_not_its_own", done: make(chan struct{}), want: 1}
	go Keep(t.Context(), a.attempt, KeepOptions{Recheck: time.Hour, Log: func(line string) { said <- line }})

	select {
	case line := <-said:
		if want := "enrolled with https://hq.example; HQ was not told its key's id: hq refused: 409 key_not_its_own"; line != want {
			t.Errorf("said %q, want %q", line, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Keep said nothing")
	}
}
