package mate_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// recordLog is a keep run on literals that keeps its record: the drain says
// yes, or not now with notNow as why; readiness answers the next of reads;
// the clock reads 07:38:50 an hour east of UTC; each write keeps the record's
// JSON and fails with writeErr.
type recordLog struct {
	notNow   string
	reads    [][]string
	writeErr error
	rec      mate.KeepRecord
	written  []string
}

func (l *recordLog) question() mate.KeepQuestion {
	return mate.KeepQuestion{
		Drain: func(string, bool) (mate.UpdateReadiness, error) {
			if l.notNow != "" {
				return mate.UpdateReadiness{Protocol: 1, BootID: "b1", Why: l.notNow}, errNotIdle
			}
			return mate.UpdateReadiness{Protocol: 1, Drained: true, BootID: "b1"}, nil
		},
		Readiness: func() (mate.UpdateReadiness, error) {
			blockers := l.reads[0]
			l.reads = l.reads[1:]
			return mate.UpdateReadiness{Protocol: 1, BootID: "b1", Blockers: blockers}, nil
		},
		Close:  func(string) (<-chan struct{}, error) { return make(chan struct{}), nil },
		Cancel: func() {},
		Now:    func() time.Time { return time.Date(2026, 11, 27, 7, 38, 50, 0, time.FixedZone("UTC+1", 3600)) },
	}.WithRecord(&l.rec, func(r mate.KeepRecord) error {
		body, err := json.Marshal(r)
		if err != nil {
			return err
		}
		l.written = append(l.written, string(body))
		return l.writeErr
	})
}

// record is the record's JSON as last written, "" when nothing was.
func (l *recordLog) record() string {
	if len(l.written) == 0 {
		return ""
	}
	return l.written[len(l.written)-1]
}

// KD-UNWRITABLE — A yes whose record cannot be written still restarts.
// witness: internal/mate/deferral.go | return true, nil, saveErr → return saveErr == nil, err, saveErr
func TestKeepAsk_KDUnwritable_YesWithUnwritableRecord_StillYes(t *testing.T) {
	t.Parallel()
	l := recordLog{writeErr: errors.New("read-only file system")}
	l.rec.Deferred = &mate.Deferral{Since: "2026-11-16T08:00:00Z", From: "0.16.239", For: "0.16.240"}
	a := l.question().Decide(l.rec.Deferred, mate.Restart{Step: mate.StepMate, For: "0.16.240", From: "0.16.239", Way: mate.WayAutomatic}, "0.16.240", "0.16.239")
	if !a.Yes {
		t.Fatal("a yes turned into a no because the record could not be written")
	}
	if a.SaveErr == nil {
		t.Fatal("the failed write is not reported")
	}
}

// KD-LAST — Every restart the keep job causes is recorded as the last one
// before it is asked for, with the run's time in UTC, its step, what it is
// for, what it restarts from, the way it asked and the blockers a forced
// update went past; a not now is no restart and records none.
// witness: internal/mate/deferral.go | r.Overridden = f.Overridden →
func TestKeepDecide_KDLast_EachRestartStep_RecordsLastRestart(t *testing.T) {
	t.Parallel()
	const zcpFrom = "0.16.239, zcp v9.265.0"
	tests := []struct {
		name    string
		log     recordLog
		restart mate.Restart
		version string
		yes     bool
		want    string
	}{
		{
			name:    "the server is not there",
			restart: mate.Restart{Step: mate.StepNotThere, From: "0.16.239", Way: mate.WayAutomatic},
			yes:     true,
			want:    `{"lastRestart":{"at":"2026-11-27T06:38:50Z","step":"not_there","from":"0.16.239","way":"automatic"}}`,
		},
		{
			name:    "another Mate version",
			restart: mate.Restart{Step: mate.StepMate, For: "0.16.240", From: "0.16.239", Way: mate.WayAutomatic},
			version: "0.16.240",
			yes:     true,
			want:    `{"lastRestart":{"at":"2026-11-27T06:38:50Z","step":"mate","for":"0.16.240","from":"0.16.239","way":"automatic"}}`,
		},
		{
			name:    "a newer zcp",
			restart: mate.Restart{Step: mate.StepZCP, For: "zcp v9.267.0", From: zcpFrom, Way: mate.WayAttended},
			version: "0.16.239",
			yes:     true,
			want:    `{"lastRestart":{"at":"2026-11-27T06:38:50Z","step":"zcp","for":"zcp v9.267.0","from":"0.16.239, zcp v9.265.0","way":"attended"}}`,
		},
		{
			name:    "a newer zcp forced past a resume blocker",
			log:     recordLog{reads: [][]string{{resumeBlocker}, {resumeBlocker}}},
			restart: mate.Restart{Step: mate.StepZCP, For: "zcp v9.267.0", From: zcpFrom, Way: mate.WayForced},
			version: "0.16.239",
			yes:     true,
			want:    `{"lastRestart":{"at":"2026-11-27T06:38:50Z","step":"zcp","for":"zcp v9.267.0","from":"0.16.239, zcp v9.265.0","way":"forced","overridden":["c1: native resume cursor is missing or disagrees"]}}`,
		},
		{
			name:    "a newer zcp the Mate answers not now",
			log:     recordLog{notNow: "c2: active run"},
			restart: mate.Restart{Step: mate.StepZCP, For: "zcp v9.267.0", From: zcpFrom, Way: mate.WayAutomatic},
			version: "0.16.239",
			want:    `{"deferred":{"since":"2026-11-27T06:38:50Z","from":"0.16.239","for":"zcp v9.267.0","why":"c2: active run"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			l := tt.log
			a := l.question().Decide(nil, tt.restart, tt.version, "0.16.239")
			if a.Yes != tt.yes {
				t.Fatalf("yes = %v, want %v (not now: %v)", a.Yes, tt.yes, a.NotNow)
			}
			if got := l.record(); got != tt.want {
				t.Fatalf("record = %s, want %s", got, tt.want)
			}
		})
	}
}

// KD-LAST-KEPT — The last restart is its own fact: forgetting a zcp restart
// attempt, clearing a deferral, and a run that finds another zcp build
// installed after a restart for zcp each keep it; only the next restart
// replaces it.
// witness: internal/mate/deferral.go | return KeepRecord{Deferred: r.Deferred, LastRestart: r.LastRestart} → return KeepRecord{Deferred: r.Deferred}
func TestKeepRecord_KDLastKept_AttemptOrDeferralGone_KeepsLastRestart(t *testing.T) {
	t.Parallel()
	last := &mate.Restart{At: "2026-11-27T06:38:50Z", Step: mate.StepZCP, For: "zcp v9.267.0", From: "0.16.239, zcp v9.265.0", Way: mate.WayAutomatic}
	attempt := mate.KeepRecord{ZCPRestartedFor: "v9.267.0", ZCPRestartedFrom: "v9.265.0", ZCPRestartedFromBuilt: "2026-11-25T10:00:00Z", LastRestart: last}
	deferral := &mate.Deferral{Since: "2026-11-27T07:00:00Z", From: "0.16.239", For: "0.16.240", Why: "c2: active run"}
	const want = `{"lastRestart":{"at":"2026-11-27T06:38:50Z","step":"zcp","for":"zcp v9.267.0","from":"0.16.239, zcp v9.265.0","way":"automatic"}}`
	tests := []struct {
		name   string
		record func(t *testing.T) string
	}{
		{"a forgotten attempt", func(t *testing.T) string {
			t.Helper()
			body, err := json.Marshal(attempt.WithoutAttempt())
			if err != nil {
				t.Fatal(err)
			}
			return string(body)
		}},
		{"a cleared deferral", func(t *testing.T) string {
			t.Helper()
			l := recordLog{rec: mate.KeepRecord{Deferred: deferral, LastRestart: last}}
			_ = l.question().Settle(l.rec.Deferred, mate.KeepRun{Running: "0.16.240"})
			return l.record()
		}},
		{"a run that finds another zcp build installed", func(t *testing.T) string {
			t.Helper()
			l := recordLog{rec: attempt}
			l.rec.Deferred = deferral
			l.rec = l.rec.WithoutAttempt()
			_ = l.question().Settle(l.rec.Deferred, mate.KeepRun{Running: "0.16.239", ReleaseRead: true, ZCPLatest: "v9.267.0"})
			return l.record()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.record(t); got != want {
				t.Fatalf("record = %s, want %s", got, want)
			}
		})
	}
}

// KD-RESET — Forgetting a zcp restart attempt keeps the deferral.
// witness: internal/mate/deferral.go | return KeepRecord{Deferred: r.Deferred, LastRestart: r.LastRestart} → return KeepRecord{LastRestart: r.LastRestart}
func TestKeepRecordWithoutAttempt_KDReset_KeepsDeferral(t *testing.T) {
	t.Parallel()
	d := &mate.Deferral{Since: "2026-11-16T08:00:00Z", From: "0.16.239", For: "zcp v9.267.0"}
	r := mate.KeepRecord{ZCPRestartedFor: "v9.266.0", ZCPRestartedFrom: "v9.265.0", ZCPRestartedFromBuilt: "2026-11-17T10:00:00Z", Deferred: d}
	got := r.WithoutAttempt()
	if got.Attempted() || got.Deferred == nil || got.Deferred.Since != "2026-11-16T08:00:00Z" {
		t.Fatalf("record = %+v, want no attempt and the deferral since 2026-11-16T08:00:00Z", got)
	}
}

// forceLog is a KeepQuestion for an operator's forced update on literals: each
// readiness read answers the next of reads, and every effect appends to
// events.
type forceLog struct {
	reads  [][]string
	events []string
	saved  []*mate.Deferral
}

func (l *forceLog) question() mate.KeepQuestion {
	return mate.KeepQuestion{
		Drain: func(string, bool) (mate.UpdateReadiness, error) {
			l.events = append(l.events, "drain")
			return mate.UpdateReadiness{Protocol: 1, Drained: true, BootID: "b1"}, nil
		},
		Readiness: func() (mate.UpdateReadiness, error) {
			l.events = append(l.events, "readiness")
			blockers := l.reads[0]
			l.reads = l.reads[1:]
			return mate.UpdateReadiness{Protocol: 1, BootID: "b1", Blockers: blockers}, nil
		},
		Close: func(string) (<-chan struct{}, error) {
			l.events = append(l.events, "close")
			return make(chan struct{}), nil
		},
		Save: func(d *mate.Deferral) error {
			l.events = append(l.events, "save")
			l.saved = append(l.saved, d)
			return nil
		},
		Cancel: func() { l.events = append(l.events, "cancel") },
		Now:    func() time.Time { return time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC) },
	}
}

const resumeBlocker = "c1: native resume cursor is missing or disagrees"

// KD-FORCE-WORK — A forced update whose readiness names any blocker other
// than `native resume …`, an unknown one included, neither closes intake nor
// restarts, and names that blocker.
// witness: internal/mate/deferral.go | if !Overridable(b) { → if strings.Contains(b, "active run") {
func TestKeepForce_KDForceWork_UnknownBlocker_NeitherClosesNorRestarts(t *testing.T) {
	t.Parallel()
	l := forceLog{reads: [][]string{{resumeBlocker, "terminal:t1/term-1"}}}
	d := &mate.Deferral{Since: "2026-11-20T06:26:57Z", From: "0.16.269", For: "0.16.279"}
	f := l.question().Force(d, "0.16.279", "0.16.269", "0.16.279")
	if f.Yes {
		t.Fatal("a forced update restarted past a terminal")
	}
	if got := fmt.Sprint(l.events); got != "[readiness]" {
		t.Fatalf("effects = %s, want one readiness read and nothing closed, cancelled or recorded", got)
	}
	if f.NotNow == nil || !strings.Contains(f.NotNow.Error(), "terminal:t1/term-1") {
		t.Fatalf("not now = %v, want it to name terminal:t1/term-1", f.NotNow)
	}
}

// KD-FORCE-RESUME — A forced update whose readiness names only
// `native resume …` blockers, with or without a leading `<id>: `, closes
// intake, reads readiness again and says yes, clearing the deferral.
// witness: internal/mate/deferral.go | _, rest, found := strings.Cut(blocker, ": ") → _, rest, found := strings.Cut(blocker, ":")
func TestKeepForce_KDForceResume_OnlyResumeBlockers_ClosesAndSaysYes(t *testing.T) {
	t.Parallel()
	resume := []string{resumeBlocker, "native resume is unsupported"}
	l := forceLog{reads: [][]string{resume, resume}}
	d := &mate.Deferral{Since: "2026-11-20T06:26:57Z", From: "0.16.269", For: "0.16.279", Why: resumeBlocker}
	f := l.question().Force(d, "0.16.279", "0.16.269", "0.16.279")
	if !f.Yes || f.Ended == nil {
		t.Fatalf("answer = %+v, want yes holding the drain request open", f)
	}
	if got := fmt.Sprint(l.events); got != "[readiness close readiness save]" {
		t.Fatalf("effects = %s, want read, close, read again, clear the deferral", got)
	}
	if len(l.saved) != 1 || l.saved[0] != nil {
		t.Fatalf("saved = %v, want the deferral cleared", l.saved)
	}
}

// KD-FORCE-REREAD — A forced update that finds live work on the readiness
// read after its close cancels and says not now.
// witness: internal/mate/deferral.go | if work := workBlockers(second.Blockers); len(work) > 0 { → if work := workBlockers(first.Blockers); len(work) > 0 {
func TestKeepForce_KDForceReread_WorkAfterClose_CancelsNotNow(t *testing.T) {
	t.Parallel()
	l := forceLog{reads: [][]string{{resumeBlocker}, {resumeBlocker, "c2: active run"}}}
	f := l.question().Force(nil, "0.16.279", "0.16.269", "0.16.279")
	if f.Yes {
		t.Fatal("a forced update restarted past a run accepted while intake closed")
	}
	if got := fmt.Sprint(l.events); got != "[readiness close readiness cancel]" {
		t.Fatalf("effects = %s, want the close cancelled after the second read", got)
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
