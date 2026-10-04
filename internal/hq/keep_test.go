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
				Keep(ctx, a.attempt, a.attempt, stays, KeepOptions{Retry: 2 * time.Millisecond, RetryMax: 8 * time.Millisecond, Recheck: 50 * time.Millisecond})
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
	go Keep(t.Context(), a.attempt, a.attempt, stays, KeepOptions{Recheck: time.Hour, Log: func(line string) { said <- line }})

	select {
	case line := <-said:
		if want := "enrolled with https://hq.example; HQ was not told its key's id: hq refused: 409 key_not_its_own"; line != want {
			t.Errorf("said %q, want %q", line, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Keep said nothing")
	}
}

// stays is an anchor read that names the kept HQ still.
func stays(context.Context) (bool, error) { return false, nil }

// calls records which of Keep's reads ran, in order: the whole enrollment
// (the member list read to find the official HQ), the recheck of the kept one
// (its HQ alone), or the anchor read asking whether the member list names
// another HQ than the kept one.
type calls struct {
	mu   sync.Mutex
	made []string
	// recheck answers each recheck in turn, the last one staying; enrollment
	// each enrollment, likewise, none enrolling; moved each anchor read, none
	// naming another HQ.
	recheck    []error
	enrollment []error
	moved      []error
	done       chan struct{}
	want       int
}

// errMoved, as an anchor read's answer, is the member list naming another HQ.
var errMoved = errors.New("moved")

func (c *calls) record(kind string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.made = append(c.made, kind)
	if len(c.made) == c.want {
		close(c.done)
	}
}

// answer is the nth answer of kind, the last one staying; none is nil.
func (c *calls) answer(kind string, answers []error) error {
	c.record(kind)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(answers) == 0 {
		return nil
	}
	n := 0
	for _, made := range c.made {
		if made == kind {
			n++
		}
	}
	return answers[min(n-1, len(answers)-1)]
}

func (c *calls) enroll(context.Context) (Result, error) {
	if err := c.answer("enroll", c.enrollment); err != nil {
		return Result{}, err
	}
	return Result{HQ: "https://hq.example"}, nil
}

func (c *calls) rechecked(context.Context) (Result, error) {
	if err := c.answer("recheck", c.recheck); err != nil {
		return Result{}, err
	}
	return Result{HQ: "https://hq.example"}, nil
}

func (c *calls) anchor(context.Context) (bool, error) {
	err := c.answer("anchor", c.moved)
	if errors.Is(err, errMoved) {
		return true, nil
	}
	return false, err
}

// TestKeep_EnrollsAnewOnlyOnHQsRefusalOrAnotherOfficialHQ: once enrolled,
// Keep asks only the kept enrollment's HQ (R6). It enrolls anew only with no
// enrollment kept, when HQ no longer knows the credential (401
// mate_credential_required, or it names another project), or when the org's
// member list names an official HQ other than the kept one — read while the
// kept HQ does not answer, on the retry's backoff. However long the kept HQ
// stays silent, an anchor that still names it keeps it: silence is not an
// answer that the HQ moved.
func TestKeep_EnrollsAnewOnlyOnHQsRefusalOrAnotherOfficialHQ(t *testing.T) {
	t.Parallel()
	silent := &UnavailableError{Code: "not_active"}
	noHQ := &NoHQError{Official: Official{Verdict: VerdictNone}}
	tests := []struct {
		name       string
		recheck    []error
		enrollment []error
		moved      []error
		want       []string
	}{
		{"an enrollment HQ knows", []error{nil}, nil, nil, []string{"recheck", "recheck", "recheck", "recheck"}},
		{"no enrollment kept", []error{ErrNotEnrolled, nil}, nil, nil, []string{"recheck", "enroll", "recheck", "recheck"}},
		{"HQ refuses the credential", []error{nil, &RefusedError{Status: 401, Code: "mate_credential_required"}, nil}, nil, nil, []string{"recheck", "recheck", "enroll", "recheck"}},
		{"a credential HQ holds for another project", []error{nil, ErrOtherProject, nil}, nil, nil, []string{"recheck", "recheck", "enroll", "recheck"}},
		{"HQ silent, the anchor naming it still", []error{nil, silent}, nil, nil, []string{"recheck", "recheck", "anchor", "recheck", "anchor", "recheck", "anchor"}},
		{"HQ silent, the anchor unreadable", []error{nil, silent}, nil, []error{errors.New("read org members: 503")}, []string{"recheck", "recheck", "anchor", "recheck", "anchor"}},
		{"HQ silent, the anchor naming another HQ", []error{nil, silent, nil}, nil, []error{errMoved}, []string{"recheck", "recheck", "anchor", "enroll", "recheck"}},
		// The HQ the anchor names now cannot serve the enrollment yet: enrolled again on the
		// backoff, the kept one never rechecked in between.
		{"another HQ, not answering yet", []error{nil, silent, nil}, []error{silent, nil}, []error{errMoved}, []string{"recheck", "recheck", "anchor", "enroll", "enroll", "recheck"}},
		{"an enrollment refused otherwise, enrolling again", []error{ErrNotEnrolled}, []error{noHQ}, nil, []string{"recheck", "enroll", "enroll", "enroll"}},
	}
	// A new Mate — nothing kept to recheck — whose HQ does not answer tries again on the usual
	// backoff, capped at a minute: an hour's backoff here is cut to NewMateRetryMax.
	t.Run("nothing kept and HQ down, enrolling at most once a minute", func(t *testing.T) {
		t.Parallel()
		want := []string{"recheck", "enroll", "enroll", "enroll"}
		c := &calls{recheck: []error{ErrNotEnrolled}, enrollment: []error{silent}, done: make(chan struct{}), want: len(want)}
		go Keep(t.Context(), c.enroll, c.rechecked, c.anchor, KeepOptions{
			Retry: time.Hour, RetryMax: time.Hour, NewMateRetryMax: 5 * time.Millisecond,
			Recheck: time.Hour,
		})
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			t.Fatal("a new Mate waited past NewMateRetryMax to enroll again")
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if got := c.made[:len(want)]; !slices.Equal(got, want) {
			t.Errorf("attempts = %v, want %v", got, want)
		}
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &calls{recheck: tt.recheck, enrollment: tt.enrollment, moved: tt.moved, done: make(chan struct{}), want: len(tt.want)}
			go Keep(t.Context(), c.enroll, c.rechecked, c.anchor, KeepOptions{
				Retry: 20 * time.Millisecond, RetryMax: 20 * time.Millisecond,
				Recheck: 5 * time.Millisecond,
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

// TestMoved_IsTheMemberListNamingAnotherHQ: what moves a Mate whose kept HQ
// does not answer is the org's member list naming another official HQ, never
// how long the kept one has been silent. No official HQ, or an unclear one,
// moves it nowhere: there is nothing to enroll with.
func TestMoved_IsTheMemberListNamingAnotherHQ(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enroll  bool
		arrange func(r *rig)
		want    bool
		wantErr func(error) bool
	}{
		{"the anchor names the kept HQ", true, func(*rig) {}, false, nil},
		{"the anchor names another HQ", true, func(r *rig) {
			r.zerops.members[1].FullName = anchorPrefix + "hq2:https://elsewhere.example"
		}, true, nil},
		{"no anchor", true, func(r *rig) { r.zerops.members = r.zerops.members[:1] }, false, nil},
		{"the member list unreadable", true, func(r *rig) { r.enroller.OrgID = "org-unknown" }, false,
			func(err error) bool { return err != nil }},
		{"nothing kept", false, func(*rig) {}, false, func(err error) bool { return errors.Is(err, ErrNotEnrolled) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			if tt.enroll {
				mustEnroll(t, r)
			}
			tt.arrange(r)
			got, err := r.enroller.Moved(context.Background())
			if tt.wantErr == nil && err != nil || tt.wantErr != nil && !tt.wantErr(err) {
				t.Fatalf("Moved error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Moved = %v, want %v", got, tt.want)
			}
		})
	}
}

// One Mate per project: HQ refusing this container because another zcp
// service of its project is the Mate ends Keep from any attempt — a kept
// Mate's recheck, its enrollment anew, a new Mate's enrollment once its HQ
// answers again — said once, recorded, and never asked again in a loop.
func TestKeep_NotThisProjectsMate_SaysSoOnceAndStops(t *testing.T) {
	t.Parallel()
	refused := &RefusedError{Status: 409, Code: NotThisProjectsMate}
	revoked := &RefusedError{Status: 401, Code: "mate_credential_required"}
	silent := &UnavailableError{Code: "not_active"}
	tests := []struct {
		name            string
		recheck, enroll []error
		want            []string
	}{
		{"refused rechecking", []error{refused}, []error{nil}, []string{"recheck"}},
		{"refused enrolling a kept Mate anew", []error{revoked}, []error{refused}, []string{"recheck", "enroll"}},
		{"refused enrolling a new Mate once HQ answers", []error{ErrNotEnrolled}, []error{silent, refused}, []string{"recheck", "enroll", "enroll"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var made, said []string
			var recorded []error
			attempt := func(kind string, answers []error) func(context.Context) (Result, error) {
				asked := 0
				return func(context.Context) (Result, error) {
					made = append(made, kind)
					asked++
					return Result{}, answers[min(asked, len(answers))-1]
				}
			}
			ended := make(chan struct{})
			go func() {
				defer close(ended)
				Keep(t.Context(), attempt("enroll", tt.enroll), attempt("recheck", tt.recheck), stays, KeepOptions{
					Retry: time.Millisecond, RetryMax: time.Millisecond, NewMateRetryMax: time.Millisecond,
					Recheck: time.Millisecond,
					Log:     func(line string) { said = append(said, line) },
					Record:  func(err error) { recorded = append(recorded, err) },
				})
			}()
			select {
			case <-ended:
			case <-time.After(5 * time.Second):
				t.Fatal("Keep went on after HQ said another service is the project's Mate")
			}
			if !slices.Equal(made, tt.want) {
				t.Errorf("attempts = %v, want %v", made, tt.want)
			}
			if len(recorded) != len(tt.want) || !errors.Is(recorded[len(recorded)-1], refused) {
				t.Errorf("recorded %v, want the refusal last", recorded)
			}
			want := "not enrolled: another zcp service of this project is its Mate; this one stops enrolling"
			if len(said) == 0 || said[len(said)-1] != want || slices.Index(said, want) != len(said)-1 {
				t.Errorf("said %q, want %q once, last", said, want)
			}
		})
	}
}
