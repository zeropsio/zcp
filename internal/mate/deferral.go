package mate

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Deferral is the keep job's record of a restart the running Mate answered
// not now (docs/spec-mate.md §2.1d): since the first not now of an unbroken
// row, the Mate version running then, what the question asked for (the Mate
// version to run, or "zcp <version>"), and the Mate's own words on why,
// opaque to zcp. `zcp mate status` reads it as the updater's "postponed"
// phase while From is the installed version.
type Deferral struct {
	Since string `json:"since"`
	From  string `json:"from"`
	For   string `json:"for"`
	Why   string `json:"why,omitempty"`
}

// deferralWhyLimit bounds Why in bytes: it travels wherever the Mate's update
// record does (its descriptor, its clients, HQ).
const deferralWhyLimit = 300

// zcpAsked prefixes a Deferral's For when the question was for a zcp release.
const zcpAsked = "zcp "

// ZCPAsked is what a question for zcp release version asks for.
func ZCPAsked(version string) string { return zcpAsked + version }

// Deferred is the deferral to keep after the Mate, at version running,
// answered not now to a question for asked at now, given the one on record (nil when
// none). A deferral for the same running Mate keeps its Since, so a row of
// not nows reads as one wait since its first; another running Mate starts a
// new row.
func Deferred(prev *Deferral, now time.Time, running, asked, why string) Deferral {
	since := now.UTC().Format(time.RFC3339)
	if prev != nil && prev.From == running && prev.Since != "" {
		since = prev.Since
	}
	return Deferral{Since: since, From: running, For: asked, Why: cutWhy(why)}
}

// SpeaksFor says the deferral holds for a Mate of version installed: one
// recorded while another version ran says nothing about it.
func (d *Deferral) SpeaksFor(installed string) bool {
	return d != nil && installed != "" && d.From == installed
}

// KeepRun is what a keep run that asked nothing learned: the Mate version
// running, whether the release list was read, the Mate version it could not
// stage ("" none), and zcp's latest release ("" unread).
type KeepRun struct {
	Running     string
	ReleaseRead bool
	Unstaged    string
	ZCPLatest   string
}

// Over says a run that asked nothing knows the deferral no longer holds:
// another version runs, or the run read everything the question depended on
// and found nothing to ask. A run that failed to look knows nothing, so the
// deferral and its Since stay.
func (d *Deferral) Over(run KeepRun) bool {
	if d == nil {
		return false
	}
	if d.From != run.Running {
		return true
	}
	if strings.HasPrefix(d.For, zcpAsked) && run.ZCPLatest == "" {
		return false
	}
	return run.ReleaseRead && run.Unstaged == ""
}

// KeepRecord is the keep job's own memory in ~/.zcp/mate/fuse.json: the zcp
// release it already restarted the container for and the zcp build installed
// then, its version and build time (one attempt per installed build and
// release), the running Mate's not now to the last question, and the last
// restart it caused.
type KeepRecord struct {
	ZCPRestartedFor       string    `json:"zcpRestartedFor,omitempty"`
	ZCPRestartedFrom      string    `json:"zcpRestartedFrom,omitempty"`
	ZCPRestartedFromBuilt string    `json:"zcpRestartedFromBuilt,omitempty"`
	Deferred              *Deferral `json:"deferred,omitempty"`
	LastRestart           *Restart  `json:"lastRestart,omitempty"`
}

// Attempted says the record holds a container restart for a zcp release.
func (r KeepRecord) Attempted() bool {
	return r.ZCPRestartedFor != "" || r.ZCPRestartedFrom != "" || r.ZCPRestartedFromBuilt != ""
}

// WithoutAttempt is the record with its zcp restart attempt forgotten; the
// deferral and the last restart are other facts and stay.
func (r KeepRecord) WithoutAttempt() KeepRecord {
	return KeepRecord{Deferred: r.Deferred, LastRestart: r.LastRestart}
}

// KeepWay is how a keep run asks: the instance of the keep job's systemd
// template that runs it.
type KeepWay string

const (
	// WayAutomatic asks with the organization's policy.
	WayAutomatic KeepWay = "automatic"
	// WayAttended asks without it: a person's or an operator's update.
	WayAttended KeepWay = "attended"
	// WayForced is an operator's forced update (KeepQuestion.Force).
	WayForced KeepWay = "forced"
)

// KeepStep is the step of a keep run that restarts (docs/spec-mate.md §2.1d).
type KeepStep string

const (
	// StepNotThere restarts the unit of a server that is not there.
	StepNotThere KeepStep = "not_there"
	// StepMate restarts the unit for another Mate version to run.
	StepMate KeepStep = "mate"
	// StepZCP restarts the container for a newer zcp.
	StepZCP KeepStep = "zcp"
)

// Restart is a restart a keep run causes, kept as the record's last restart
// until the next one: the run's time, its step, what it is for (the Mate
// version to run, or ZCPAsked; nothing for StepNotThere), what it restarts
// from (the Mate version running, or the one installed for StepNotThere,
// and for StepZCP the installed zcp too), the way it asked, and the
// blockers a forced update went past.
type Restart struct {
	At         string   `json:"at"`
	Step       KeepStep `json:"step"`
	For        string   `json:"for,omitempty"`
	From       string   `json:"from,omitempty"`
	Way        KeepWay  `json:"way"`
	Overridden []string `json:"overridden,omitempty"`
}

// KeepQuestion is the keep job's "may I restart you now?" to the running
// Mate and what it keeps of the answer. The keep job wires the loopback
// updater and its record (WithRecord); each field is one effect. Close is an
// operator's forced update's own close of intake (CloseForUpdate); Record
// keeps a restart as the last one.
type KeepQuestion struct {
	Drain     func(version string, automatic bool) (UpdateReadiness, error)
	Readiness func() (UpdateReadiness, error)
	Close     func(version string) (ended <-chan struct{}, err error)
	Save      func(*Deferral) error
	Record    func(Restart) error
	Cancel    func()
	Now       func() time.Time
}

// WithRecord is q keeping what it learns in rec, written whole by write after
// each change: saving or clearing the deferral changes only rec's deferral,
// and recording a restart only its last restart.
func (q KeepQuestion) WithRecord(rec *KeepRecord, write func(KeepRecord) error) KeepQuestion {
	q.Save = func(d *Deferral) error {
		rec.Deferred = d
		return write(*rec)
	}
	q.Record = func(r Restart) error {
		rec.LastRestart = &r
		return write(*rec)
	}
	return q
}

// KeepAnswer is the keep job's answer to a restart it wants
// (KeepQuestion.Decide): yes, Ended when a forced yes holds the Mate's intake
// closed by a drain request still open (nil otherwise), the not now's
// reason, and a record that could not be written.
type KeepAnswer struct {
	Yes     bool
	Ended   <-chan struct{}
	NotNow  error
	SaveErr error
}

// Decide answers r, a restart of the Mate running running, before that
// restart is asked for: StepNotThere asks nothing, a server that is not
// there holding no work to ask about; every other step asks, naming version,
// for r.For, the way r.Way says (Force, or Ask). A yes records r at the
// run's time, with the blockers a forced yes went past, as the last restart;
// a record that cannot be written never turns it into a no.
func (q KeepQuestion) Decide(prev *Deferral, r Restart, version, running string) KeepAnswer {
	var a KeepAnswer
	switch {
	case r.Step == StepNotThere:
		a.Yes = true
	case r.Way == WayForced:
		f := q.Force(prev, version, running, r.For)
		a = KeepAnswer{Yes: f.Yes, Ended: f.Ended, NotNow: f.NotNow, SaveErr: f.SaveErr}
		r.Overridden = f.Overridden
	default:
		a.Yes, a.NotNow, a.SaveErr = q.Ask(prev, version, running, r.For, r.Way == WayAutomatic)
	}
	if a.Yes {
		r.At = q.Now().UTC().Format(time.RFC3339)
		a.SaveErr = errors.Join(a.SaveErr, q.Record(r))
	}
	return a
}

// overridablePrefix starts the Mate's words for a blocker that is no live
// work: a restart would not resume this conversation's native session
// (docs/spec-mate.md §2.1d, "The question").
const overridablePrefix = "native resume "

// Overridable says an operator's forced update may restart past blocker:
// after an optional leading "<id>: ", it starts with "native resume ". Every
// other blocker, an unknown one included, is live work.
func Overridable(blocker string) bool {
	if strings.HasPrefix(blocker, overridablePrefix) {
		return true
	}
	_, rest, found := strings.Cut(blocker, ": ")
	return found && strings.HasPrefix(rest, overridablePrefix)
}

// workBlockers is the blockers that are live work.
func workBlockers(blockers []string) []string {
	var work []string
	for _, b := range blockers {
		if !Overridable(b) {
			work = append(work, b)
		}
	}
	return work
}

// Forced is an operator's forced update's answer: yes, the blockers it forced
// past (none when the ordinary question said yes), Ended when it holds the
// Mate's intake closed by a drain request still open (nil otherwise), the not
// now's reason, and a record that could not be written.
type Forced struct {
	Yes        bool
	Overridden []string
	Ended      <-chan struct{}
	NotNow     error
	SaveErr    error
}

// Force is an operator's forced update of the Mate running running, naming
// version, for asked (docs/spec-mate.md §2.1d). A Mate that is ready or
// names no blocker gets the attended Ask. One blocker that is live work, or
// no readiness answer, is not now with nothing closed and nothing recorded.
// Only overridable blockers: intake is closed, readiness read once more after
// the close is written, and live work or no answer then cancels as a not now;
// otherwise yes, clearing a deferral on record as Ask's yes does.
func (q KeepQuestion) Force(prev *Deferral, version, running, asked string) Forced {
	first, err := q.Readiness()
	if err != nil {
		return Forced{NotNow: fmt.Errorf("readiness not read: %w", err)}
	}
	if first.Ready || len(first.Blockers) == 0 {
		yes, notNow, saveErr := q.Ask(prev, version, running, asked, false)
		return Forced{Yes: yes, NotNow: notNow, SaveErr: saveErr}
	}
	if work := workBlockers(first.Blockers); len(work) > 0 {
		return Forced{NotNow: fmt.Errorf("live work: %s", strings.Join(work, "; "))}
	}
	ended, err := q.Close(version)
	if err != nil {
		q.Cancel()
		return Forced{NotNow: err}
	}
	second, err := q.Readiness()
	if err != nil {
		q.Cancel()
		return Forced{NotNow: fmt.Errorf("readiness not read after the close: %w", err)}
	}
	if work := workBlockers(second.Blockers); len(work) > 0 {
		q.Cancel()
		return Forced{NotNow: fmt.Errorf("live work after the close: %s", strings.Join(work, "; "))}
	}
	f := Forced{Yes: true, Overridden: second.Blockers, Ended: ended}
	if prev != nil {
		f.SaveErr = q.Save(nil)
	}
	return f
}

// Ask asks the Mate running running, naming version, for a restart that is
// for asked. Yes clears a deferral on record; a record that cannot be written
// never turns a yes into a no. A not now the Mate answered (protocol 1) is
// recorded with its words on why before the cancel that reopens, so the
// Mate's re-read on cancel finds it; a drain without an answer
// (ErrNotAnswering, a deadline, an unusable payload) decodes no protocol and
// records nothing. notNow is the drain's refusal; saveErr a record that could not be
// written.
func (q KeepQuestion) Ask(prev *Deferral, version, running, asked string, automatic bool) (yes bool, notNow, saveErr error) {
	drain, err := q.Drain(version, automatic)
	if err == nil {
		if prev != nil {
			saveErr = q.Save(nil)
		}
		return true, nil, saveErr
	}
	if drain.Protocol == 1 {
		d := Deferred(prev, q.Now(), running, asked, q.why(drain))
		saveErr = q.Save(&d)
	}
	q.Cancel()
	return false, err, saveErr
}

// Settle ends a run that asked nothing: a deferral the run knows is over is
// cleared and the Mate told, whose cancel route re-reads its status; one the
// run cannot judge stays untouched.
func (q KeepQuestion) Settle(prev *Deferral, run KeepRun) error {
	if !prev.Over(run) {
		return nil
	}
	err := q.Save(nil)
	q.Cancel()
	return err
}

// why is the Mate's own words on a not now: the drain answer's why, else the
// blockers of one readiness read, else "".
func (q KeepQuestion) why(drain UpdateReadiness) string {
	if drain.Why != "" {
		return drain.Why
	}
	readiness, err := q.Readiness()
	if err != nil {
		return ""
	}
	return strings.Join(readiness.Blockers, "; ")
}

// UpdaterStatus is `zcp mate status`'s "updater" key (docs/spec-mate.md
// §2.1a), in the shape the Mate server reads.
type UpdaterStatus struct {
	Protocol       int    `json:"protocol"`
	Phase          string `json:"phase"`
	RunningVersion string `json:"runningVersion"`
	FailedVersion  string `json:"failedVersion,omitempty"`
	PostponedSince string `json:"postponedSince,omitempty"`
	PostponedFor   string `json:"postponedFor,omitempty"`
	PostponedWhy   string `json:"postponedWhy,omitempty"`
}

// PhasePostponed is the updater phase while the running Mate's not now is on
// record.
const PhasePostponed = "postponed"

// UpdaterStatusOf is the updater key for the versions the container holds
// and the keep job's deferral (nil none): protocol 1, the version current
// names, a failed version newer than it, and phase "idle", or "postponed"
// with since, for and why while the deferral speaks for the installed Mate.
// nil with no Mate installed.
func UpdaterStatusOf(v Versions, d *Deferral) *UpdaterStatus {
	if v.Current == "" {
		return nil
	}
	u := &UpdaterStatus{Protocol: 1, Phase: "idle", RunningVersion: v.Current}
	if VersionOlder(v.Current, v.Failed) {
		u.FailedVersion = v.Failed
	}
	if d.SpeaksFor(v.Current) {
		u.Phase = PhasePostponed
		u.PostponedSince, u.PostponedFor, u.PostponedWhy = d.Since, d.For, d.Why
	}
	return u
}

// cutWhy cuts why to deferralWhyLimit bytes on a character boundary.
func cutWhy(why string) string {
	if len(why) <= deferralWhyLimit {
		return why
	}
	cut := deferralWhyLimit
	for cut > 0 && !utf8.RuneStart(why[cut]) {
		cut--
	}
	return why[:cut]
}
