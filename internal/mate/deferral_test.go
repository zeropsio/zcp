package mate_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// KD-SINCE — A second not now for the same running Mate keeps the first one's
// time, so "postponed since" is how long the Mate has really waited.
// witness: internal/mate/deferral.go | if prev != nil && prev.From == running && prev.Since != "" { → if false {
func TestDeferred_KDSince_SecondNotNowSameRunning_KeepsFirstSince(t *testing.T) {
	t.Parallel()
	first := mate.Deferred(nil, time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC), "0.16.212", "0.16.230", "terminal:t1/1")
	second := mate.Deferred(&first, time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC), "0.16.212", "0.16.231", "terminal:t1/1")
	if second.Since != "2026-11-16T08:00:00Z" {
		t.Fatalf("since = %q, want the first not now 2026-11-16T08:00:00Z", second.Since)
	}
	if second.For != "0.16.231" {
		t.Fatalf("for = %q, want the version the second question asked for", second.For)
	}
}

// KD-STALE — A deferral recorded for another running version does not speak
// for the installed one.
// witness: internal/mate/deferral.go | return d != nil && installed != "" && d.From == installed → return d != nil && installed != ""
func TestDeferralSpeaksFor_KDStale_AnotherRunningVersion_False(t *testing.T) {
	t.Parallel()
	d := mate.Deferral{Since: "2026-11-16T08:00:00Z", From: "0.16.212", For: "0.16.230"}
	if d.SpeaksFor("0.16.230") {
		t.Fatal("a deferral recorded while 0.16.212 ran speaks for installed 0.16.230")
	}
}

// keepLog is a KeepQuestion on literals whose effects append to events: the
// drain answers drain/drainErr, readiness answers blockers, a save fails
// with saveErr and records what it saved.
type keepLog struct {
	events []string
	saved  []*mate.Deferral
}

func (l *keepLog) question(drain mate.UpdateReadiness, drainErr, saveErr error) mate.KeepQuestion {
	return mate.KeepQuestion{
		Drain: func(string, bool) (mate.UpdateReadiness, error) {
			l.events = append(l.events, "drain")
			return drain, drainErr
		},
		Readiness: func() (mate.UpdateReadiness, error) {
			l.events = append(l.events, "readiness")
			return mate.UpdateReadiness{Protocol: 1, Blockers: []string{"terminal:t1/term-1"}}, nil
		},
		Save: func(d *mate.Deferral) error {
			l.events = append(l.events, "save")
			l.saved = append(l.saved, d)
			return saveErr
		},
		Cancel: func() { l.events = append(l.events, "cancel") },
		Now:    func() time.Time { return time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC) },
	}
}

var errNotIdle = errors.New("mate did not prove idle before drain deadline")

// KD-ORDER — After a not now the deferral is written before the cancel, so
// the Mate's status re-read on cancel finds it.
// witness: internal/mate/deferral.go | saveErr = q.Save(&d) → defer func() { saveErr = q.Save(&d) }()
func TestKeepAsk_KDOrder_NotNow_SavesBeforeCancel(t *testing.T) {
	t.Parallel()
	var l keepLog
	yes, _, _ := l.question(mate.UpdateReadiness{Protocol: 1, BootID: "b1"}, errNotIdle, nil).Ask(nil, "0.16.240", "0.16.239", "0.16.240", true)
	if yes {
		t.Fatal("a refused drain read as yes")
	}
	if got := fmt.Sprint(l.events); got != "[drain readiness save cancel]" {
		t.Fatalf("effects = %s, want the save before the cancel", got)
	}
	if d := l.saved[0]; d == nil || d.Why != "terminal:t1/term-1" || d.Since != "2026-11-18T08:00:00Z" {
		t.Fatalf("saved = %+v, want the readiness blocker since 2026-11-18T08:00:00Z", d)
	}
}

// KD-NOANSWER — A drain the Mate never answered records nothing and still
// reopens.
// witness: internal/mate/deferral.go | if drain.Protocol == 1 { → if err != nil {
func TestKeepAsk_KDNoAnswer_NotAnswering_RecordsNothing(t *testing.T) {
	t.Parallel()
	var l keepLog
	yes, _, _ := l.question(mate.UpdateReadiness{}, fmt.Errorf("drain Mate update: %w", mate.ErrNotAnswering), nil).Ask(nil, "0.16.240", "0.16.239", "0.16.240", true)
	if yes {
		t.Fatal("an unanswered drain read as yes")
	}
	if got := fmt.Sprint(l.events); got != "[drain cancel]" {
		t.Fatalf("effects = %s, want only the drain and the cancel", got)
	}
}

// KD-UNKNOWN — A run that could not read the release list keeps the
// deferral and its since and tells the Mate nothing.
// witness: internal/mate/deferral.go | return run.ReleaseRead && run.Unstaged == "" → return run.Unstaged == ""
func TestKeepSettle_KDUnknown_ReleaseListUnread_KeepsDeferral(t *testing.T) {
	t.Parallel()
	var l keepLog
	d := &mate.Deferral{Since: "2026-11-16T08:00:00Z", From: "0.16.239", For: "0.16.240", Why: "terminal:t1/term-1"}
	_ = l.question(mate.UpdateReadiness{}, nil, nil).Settle(d, mate.KeepRun{Running: "0.16.239", ReleaseRead: false, ZCPLatest: "v9.266.0"})
	if len(l.events) != 0 {
		t.Fatalf("effects = %v, want none: the run learned nothing", l.events)
	}
}

// KD-UNWRITABLE — A yes whose record cannot be written still restarts.
// witness: internal/mate/deferral.go | return true, nil, saveErr → return saveErr == nil, err, saveErr
func TestKeepAsk_KDUnwritable_YesWithUnwritableRecord_StillYes(t *testing.T) {
	t.Parallel()
	var l keepLog
	d := &mate.Deferral{Since: "2026-11-16T08:00:00Z", From: "0.16.239", For: "0.16.240"}
	yes, _, saveErr := l.question(mate.UpdateReadiness{Protocol: 1, Drained: true, BootID: "b1"}, nil, errors.New("read-only file system")).Ask(d, "0.16.240", "0.16.239", "0.16.240", true)
	if !yes {
		t.Fatal("a yes turned into a no because the record could not be written")
	}
	if saveErr == nil {
		t.Fatal("the failed write is not reported")
	}
}

// KD-RESET — Forgetting a zcp restart attempt keeps the deferral.
// witness: internal/mate/deferral.go | return KeepRecord{Deferred: r.Deferred} → return KeepRecord{}
func TestKeepRecordWithoutAttempt_KDReset_KeepsDeferral(t *testing.T) {
	t.Parallel()
	d := &mate.Deferral{Since: "2026-11-16T08:00:00Z", From: "0.16.239", For: "zcp v9.267.0"}
	r := mate.KeepRecord{ZCPRestartedFor: "v9.266.0", ZCPRestartedFrom: "v9.265.0", ZCPRestartedFromBuilt: "2026-11-17T10:00:00Z", Deferred: d}
	got := r.WithoutAttempt()
	if got.Attempted() || got.Deferred == nil || got.Deferred.Since != "2026-11-16T08:00:00Z" {
		t.Fatalf("record = %+v, want no attempt and the deferral since 2026-11-16T08:00:00Z", got)
	}
}

// KD-POSTPONED — While the deferral speaks for the installed Mate, the
// status's updater is "postponed" with since, for and why.
// witness: internal/mate/deferral.go | if d.SpeaksFor(v.Current) { → if !d.SpeaksFor(v.Current) {
func TestUpdaterStatusOf_KDPostponed_DeferralForInstalled_Postponed(t *testing.T) {
	t.Parallel()
	d := &mate.Deferral{Since: "2026-11-16T08:00:00Z", From: "0.16.239", For: "0.16.240", Why: "terminal:t1/term-1"}
	got := mate.UpdaterStatusOf(mate.Versions{Current: "0.16.239"}, d)
	want := mate.UpdaterStatus{Protocol: 1, Phase: "postponed", RunningVersion: "0.16.239", PostponedSince: "2026-11-16T08:00:00Z", PostponedFor: "0.16.240", PostponedWhy: "terminal:t1/term-1"}
	if got == nil || *got != want {
		t.Fatalf("updater = %+v, want %+v", got, want)
	}
}
