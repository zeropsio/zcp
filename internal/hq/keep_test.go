package hq

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
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
				Keep(ctx, a.attempt, a.attempt, KeepOptions{Retry: 2 * time.Millisecond, RetryMax: 8 * time.Millisecond, Recheck: 50 * time.Millisecond})
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
	go Keep(t.Context(), a.attempt, a.attempt, KeepOptions{Recheck: time.Hour, Log: func(line string) { said <- line }})

	select {
	case line := <-said:
		if want := "enrolled with https://hq.example; HQ was not told its key's id: hq refused: 409 key_not_its_own"; line != want {
			t.Errorf("said %q, want %q", line, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Keep said nothing")
	}
}

// calls records which of Keep's two attempts ran, in order: the whole
// enrollment (the member list read to find the official HQ) or the recheck
// of the kept one (its HQ alone).
type calls struct {
	mu   sync.Mutex
	made []string
	// recheck answers each recheck in turn, the last one staying.
	recheck []error
	done    chan struct{}
	want    int
}

func (c *calls) record(kind string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.made = append(c.made, kind)
	if len(c.made) == c.want {
		close(c.done)
	}
}

func (c *calls) enroll(context.Context) (Result, error) {
	c.record("enroll")
	return Result{HQ: "https://hq.example"}, nil
}

func (c *calls) rechecked(context.Context) (Result, error) {
	c.record("recheck")
	c.mu.Lock()
	defer c.mu.Unlock()
	rechecks := 0
	for _, kind := range c.made {
		if kind == "recheck" {
			rechecks++
		}
	}
	if err := c.recheck[min(rechecks-1, len(c.recheck)-1)]; err != nil {
		return Result{}, err
	}
	return Result{HQ: "https://hq.example"}, nil
}

// TestKeep_RediscoversOnlyWhenHQRefusesOrStaysSilent: once enrolled, Keep
// asks only the kept enrollment's HQ (R6). It reads the member list for the
// official HQ again only with no enrollment kept, when HQ no longer knows
// the credential (401 mate_credential_required, or it names another
// project), or when HQ has not answered for Rediscover — an HQ that lost the
// anchor answers 503 for good; a deploy's handover answers it for seconds.
func TestKeep_RediscoversOnlyWhenHQRefusesOrStaysSilent(t *testing.T) {
	t.Parallel()
	silent := &UnavailableError{Code: "not_active"}
	tests := []struct {
		name       string
		recheck    []error
		rediscover time.Duration
		want       []string
	}{
		{"an enrollment HQ knows", []error{nil}, time.Hour, []string{"recheck", "recheck", "recheck", "recheck"}},
		{"no enrollment kept", []error{ErrNotEnrolled, nil}, time.Hour, []string{"recheck", "enroll", "recheck", "recheck"}},
		{"HQ refuses the credential", []error{nil, &RefusedError{Status: 401, Code: "mate_credential_required"}, nil}, time.Hour, []string{"recheck", "recheck", "enroll", "recheck"}},
		{"a credential HQ holds for another project", []error{nil, ErrOtherProject, nil}, time.Hour, []string{"recheck", "recheck", "enroll", "recheck"}},
		{"HQ silent within the bound", []error{nil, silent}, time.Hour, []string{"recheck", "recheck", "recheck", "recheck"}},
		{"HQ silent past the bound", []error{nil, silent}, time.Millisecond, []string{"recheck", "recheck", "enroll", "recheck"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &calls{recheck: tt.recheck, done: make(chan struct{}), want: len(tt.want)}
			go Keep(t.Context(), c.enroll, c.rechecked, KeepOptions{
				Retry: 20 * time.Millisecond, RetryMax: 20 * time.Millisecond,
				Recheck: 5 * time.Millisecond, Rediscover: tt.rediscover,
			})
			select {
			case <-c.done:
			case <-time.After(5 * time.Second):
				t.Fatal("Keep stopped attempting")
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			if got := c.made[:len(tt.want)]; !slices.Equal(got, tt.want) {
				t.Errorf("attempts = %v, want %v", got, tt.want)
			}
		})
	}
}

// countingZerops counts the member-list reads it passes on.
type countingZerops struct {
	Zerops
	mu      sync.Mutex
	members int
}

func (c *countingZerops) ListOrgMembers(ctx context.Context, org string) ([]platform.OrgMember, error) {
	c.mu.Lock()
	c.members++
	c.mu.Unlock()
	return c.Zerops.ListOrgMembers(ctx, org)
}

// TestRecheck_AsksTheKeptHQAloneNeverTheMemberList: a recheck asks the HQ
// the kept enrollment names whether it still knows the credential as this
// project's, and never reads the org's member list (R6); what it answers is
// what Keep decides on.
func TestRecheck_AsksTheKeptHQAloneNeverTheMemberList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enroll  bool
		arrange func(r *rig)
		want    func(error) bool
	}{
		{"HQ knows the credential", true, func(*rig) {}, func(err error) bool { return err == nil }},
		{"HQ revoked it", true, func(r *rig) { r.hq.credential = map[string]string{} },
			func(err error) bool { return refusedAs(err, "mate_credential_required") }},
		{"HQ holds it for another project", true, func(r *rig) {
			for credential := range r.hq.credential {
				r.hq.credential[credential] = "P_OTHER"
			}
		}, func(err error) bool { return errors.Is(err, ErrOtherProject) }},
		{"nothing kept", false, func(*rig) {}, func(err error) bool { return errors.Is(err, ErrNotEnrolled) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			if tt.enroll {
				mustEnroll(t, r)
			}
			tt.arrange(r)
			counting := &countingZerops{Zerops: r.enroller.Zerops}
			e := r.enroller
			e.Zerops = counting
			got, err := e.Recheck(context.Background())
			if !tt.want(err) {
				t.Fatalf("Recheck error = %v", err)
			}
			if err == nil && got.HQ != r.hqURL {
				t.Errorf("HQ = %q, want %q", got.HQ, r.hqURL)
			}
			if counting.members != 0 {
				t.Errorf("read the member list %d times, want never", counting.members)
			}
		})
	}
}
