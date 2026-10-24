package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// A lease holding work that is not pushed need not be released to stop
// running (docs/spec-workflows.md §8 DP-28): park stops its container through
// the orchestrator with the lease, which keeps the reservation and the
// container's disk — /var/www and $HOME; /tmp too, except where the base empties it at boot (an Ubuntu base does) — and
// ends its processes; unpark starts it again (live-verified 2026-10-23). A
// stopped container answers nothing: ssh fails (255, no route to host, after
// ~6 s), the orchestrator's exec is refused container-not-running, and zcp's
// sshfs mount blocks the first access for ~42 s before it errors. So park
// unmounts the lease before it stops the container, and anything that reads
// /var/www/<alias> fails at once instead of hanging.
//
// The record says the lease is parked (parked, parkedAt): dev servers,
// deploy, verify and sync read only records, so they refuse a parked lease
// naming the unpark, and zcp init does not mount it. A parked lease is still
// held, so its container keeps its hostname and no pool hands it out: the
// branch's other leases stay wired to it (DP-26) — their dev servers reach
// nothing on it until the unpark — and nothing is rewired. Unpark starts the
// container, waits for its ssh, mounts the lease and makes sure its supervisor
// runs (DP-13): a container never reset runs its run.start again on every
// start, a reset one does not, so the supervisor is started only when none
// answers, and either way it gets the branch's sibling overlay as an
// acquire's checkout sends it (DP-22). A release of a parked lease starts it
// first so the release guard can read it (DP-4), and stops it again when the
// release is then refused; a forced release resets it as it stands, stopped.

// LeaseParkCall and LeaseUnparkCall are the zerops_lease calls that park and
// unpark alias.
func LeaseParkCall(alias string) string { return "zerops_lease action=park lease=" + alias }

// LeaseUnparkCall is the zerops_lease call that unparks alias.
func LeaseUnparkCall(alias string) string { return "zerops_lease action=unpark lease=" + alias }

// leaseSSHReadyEvery is the default LeaseDeps.SSHReadyEvery.
const leaseSSHReadyEvery = time.Second

// leaseStartedSSHWait bounds the wait for a started container's ssh, which
// answers ~65 ms after the start returns (live, 2026-10-23).
const leaseStartedSSHWait = time.Minute

// The orchestrator's lockState while it stops or starts a container: work in
// progress under the reservation, which a holder's stop and start keep.
const (
	leaseLockStopping = "stopping"
	leaseLockStarting = "starting"
)

// powerTransition: the orchestrator is stopping or starting the container.
func powerTransition(c platform.SwarmContainer) bool {
	return c.LockState == leaseLockStopping || c.LockState == leaseLockStarting
}

// parkedContainer is a held container that is stopped and kept, or being
// stopped or started: nothing answers on it.
func parkedContainer(c platform.SwarmContainer) bool {
	return powerTransition(c) || (c.LockState == leaseLockReserved && c.Status == platform.SwarmContainerStopped)
}

// parkedKeeps is what a parked lease keeps and how it runs again.
func parkedKeeps(alias string) string {
	return fmt.Sprintf("it keeps its reservation, /var/www and $HOME (an Ubuntu base empties /tmp at every start), nothing runs in it and nothing answers on it; %s starts it again", LeaseUnparkCall(alias))
}

// parkedView is the view of a lease whose container its orchestrator lists
// stopped (or stopping, or starting) and reserved: parked, whoever stopped it.
func parkedView(r leaseRecord, c platform.SwarmContainer) LeaseView {
	alias := r.alias()
	v := r.view(LeaseStateParked)
	if r.Parked {
		v.ParkedAt = r.ParkedAt
	}
	switch {
	case powerTransition(c):
		v.Note = fmt.Sprintf("the orchestrator is %s its container right now", c.LockState)
	case r.Parked:
		v.Note = "parked: its container is stopped; " + parkedKeeps(alias)
	default:
		v.Note = fmt.Sprintf("its container is stopped though zcp did not park it (it was stopped outside zcp); %s starts it and mounts it", LeaseUnparkCall(alias))
	}
	return v
}

// startedSinceParkedNote is said of a lease zcp parked whose container runs
// again: something started it outside zcp, so it is not mounted and nothing
// made sure its supervisor runs.
func startedSinceParkedNote(alias string) string {
	return fmt.Sprintf("zcp parked it, but its container runs again (started outside zcp); %s mounts it and makes sure its supervisor runs", LeaseUnparkCall(alias))
}

// LeaseParkedError refuses what (a dev-server call, a deploy, a sync) on a
// parked lease: its container is stopped, so nothing answers on it.
func LeaseParkedError(lease LeaseTargetInfo, what string) error {
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s is parked: its container is stopped, so %s cannot reach it", lease.Alias, what),
		fmt.Sprintf("Start it first: %s (it keeps its /var/www and $HOME)", LeaseUnparkCall(lease.Alias)))
}

// leaseContainerFor asks the orchestrator, with the lease, whose the
// container rec records is (readLeaseOwnership) for a park or an unpark
// (verb), refusing — nothing changed — a lease zcp no longer holds (gone,
// free, another holder's), named with the release that drops its record; one
// the orchestrator does not answer about; and one whose container it is
// stopping or starting right now. Only a container the orchestrator gives the
// lease is parked or unparked, so neither ever touches another holder's
// workspace.
func leaseContainerFor(ctx context.Context, d LeaseDeps, rec leaseRecord, verb string) (leaseVerdict, error) {
	alias := rec.alias()
	v := readLeaseOwnership(ctx, d, rec)
	switch {
	case v.own == leaseUnverified:
		return leaseVerdict{}, unverifiedLeaseRefusal(alias, verb+"ed", v)
	case v.own.lost():
		return leaseVerdict{}, lostLeaseRefusal(alias, verb, v.why)
	case powerTransition(v.container):
		return leaseVerdict{}, platform.NewPlatformError(platform.ErrPrerequisiteMissing,
			fmt.Sprintf("Lease %s was not %sed: the orchestrator is %s its container right now", alias, verb, v.container.LockState),
			fmt.Sprintf("zerops_lease action=status shows it once that is done; %s it then if it still needs it", verb))
	}
	return v, nil
}

// leaseLostOnCall is a park or an unpark (verb) whose stop or start the
// orchestrator refused held-by-other: the lease ended outside zcp, so its
// record is dropped as lost with its mount and alias stanza, and the view
// says so — never "retry in a moment".
func leaseLostOnCall(ctx context.Context, d LeaseDeps, rec leaseRecord, verb string) (LeaseView, error) {
	v, err := dropLostLease(ctx, d, rec, heldByOtherWhy)
	if err != nil {
		return LeaseView{}, fmt.Errorf("%s %s: %w", verb, rec.alias(), err)
	}
	v.Note = joinNotes(fmt.Sprintf("not %sed", verb), v.Note, siblingsMayBeWired(d.StateDir, rec))
	return v, nil
}

// lostLeaseRefusal refuses a park or an unpark (verb) of a lease zcp no longer
// holds, saying why.
func lostLeaseRefusal(alias, verb, why string) error {
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s cannot be %sed: %s; zcp does not re-lease it", alias, verb, why),
		"zerops_lease action=release lease="+alias+" drops the record")
}

// LeasePark parks the lease addressed by alias: it unmounts the lease, then
// stops its container through the orchestrator with the lease, and records it
// parked. A mount that will not unmount refuses the park, nothing stopped: a
// stopped container's mount hangs whatever reads it. A container already
// stopped is only recorded. A stop the orchestrator refuses leaves the lease
// held and mounted again, the refusal saying why.
func LeasePark(ctx context.Context, d LeaseDeps, alias string) (LeaseView, error) {
	rec, err := loadLeaseRecord(d.StateDir, alias)
	if err != nil {
		return LeaseView{}, err
	}
	verdict, err := leaseContainerFor(ctx, d, rec, "park")
	if err != nil {
		return LeaseView{}, err
	}
	rp, c := verdict.rp, verdict.container
	if err := d.Host.unmountLease(ctx, d, rec); err != nil {
		return LeaseView{}, platform.NewPlatformError(platform.ErrUnmountFailed,
			fmt.Sprintf("Lease %s was not parked: zcp unmounts it before it stops its container, and the unmount failed (%v)", alias, err),
			fmt.Sprintf("A stopped container's mount hangs whatever reads it: unmount it with sudo fusermount -uz %s, then park again: %s", filepath.Join(mountBase, alias), LeaseParkCall(alias)))
	}
	var notes []string
	switch {
	case c.Status == platform.SwarmContainerStopped && rec.Parked:
		notes = append(notes, "it was parked already")
	case c.Status == platform.SwarmContainerStopped:
		notes = append(notes, "its container was stopped already (outside zcp), so zcp stopped nothing and recorded it parked")
	default:
		if _, err := rp.api.Stop(ctx, rec.ContainerID, rec.Lease); err != nil {
			if isHeldByOther(err) {
				return leaseLostOnCall(ctx, d, rec, "park")
			}
			out := leaseOrchestratorError(ctx, d, rp, "park "+alias, err)
			remount := "its mount is back"
			if note := d.Host.mountLease(ctx, d, rec); note != "" {
				remount = note
			}
			return LeaseView{}, withNote(out, "the lease stays held and running; "+remount)
		}
	}
	if !rec.Parked {
		rec.Parked, rec.ParkedAt = true, d.now().UTC().Format(time.RFC3339)
		if err := writeLeaseRecord(d.StateDir, rec); err != nil {
			return LeaseView{}, fmt.Errorf("park %s: its container is stopped and still reserved, but its record could not be written (status reports it parked from the orchestrator): %w", alias, err)
		}
	}
	v := rec.view(LeaseStateParked)
	v.ParkedAt = rec.ParkedAt
	notes = append(notes, "parked: its container is stopped, its processes (dev servers, supervisor) ended and "+filepath.Join(mountBase, alias)+" is unmounted; "+parkedKeeps(alias))
	if siblings := siblingRecords(d.StateDir, rec); len(siblings) > 0 {
		notes = append(notes, fmt.Sprintf("the branch's other leases (%s) stay wired to its container, which keeps its hostname while it is parked; their dev servers reach nothing on it until it is unparked", strings.Join(aliasesOf(siblings), ", ")))
	}
	v.Note = joinNotes(notes...)
	return v, nil
}

// LeaseUnpark unparks the lease addressed by alias: it starts its container
// through the orchestrator with the lease (none when it runs already), records
// it running, waits for its ssh, mounts it and makes sure its supervisor runs
// with the branch's sibling overlay. Once the container runs, nothing fails
// the unpark: an ssh that does not answer, a mount or a supervisor that fails
// is a note, and the same call again does what is left.
func LeaseUnpark(ctx context.Context, d LeaseDeps, alias string) (LeaseView, error) {
	rec, err := loadLeaseRecord(d.StateDir, alias)
	if err != nil {
		return LeaseView{}, err
	}
	verdict, err := leaseContainerFor(ctx, d, rec, "unpark")
	if err != nil {
		return LeaseView{}, err
	}
	rp, c := verdict.rp, verdict.container
	var notes []string
	if c.Status == platform.SwarmContainerActive {
		if !rec.Parked {
			notes = append(notes, "its container was running already, so zcp started nothing")
		}
	} else if _, err := rp.api.Start(ctx, rec.ContainerID, rec.Lease); err != nil {
		if isHeldByOther(err) {
			return leaseLostOnCall(ctx, d, rec, "unpark")
		}
		return LeaseView{}, leaseOrchestratorError(ctx, d, rp, "unpark "+alias, err)
	}
	if rec.Parked {
		rec.Parked, rec.ParkedAt = false, ""
		if err := writeLeaseRecord(d.StateDir, rec); err != nil {
			return LeaseView{}, fmt.Errorf("unpark %s: its container runs again, but its record could not be written, so zcp still treats it as parked: %w", alias, err)
		}
	}
	v := rec.view(LeaseStateHeld)
	// ssh reaches the container through its alias: put back a stanza another
	// call or a redeploy lost.
	notes = append(notes, d.Host.refreshAliases(ctx, d))
	if d.SSH != nil {
		if err := waitStartedLease(ctx, d, alias); err != nil {
			v.State = LeaseStateUnmounted
			v.Note = joinNotes(append(notes, fmt.Sprintf("its container runs again but did not answer ssh within %s (%v), so zcp neither mounted it nor made sure its supervisor runs; %s does both once it answers",
				d.startedSSHWait(), err, LeaseUnparkCall(alias)))...)
			return v, nil
		}
	}
	if note := d.Host.mountLease(ctx, d, rec); note != "" {
		v.State = LeaseStateUnmounted
		notes = append(notes, note)
	}
	if d.SSH != nil {
		notes = append(notes, superviseUnparked(ctx, d.SSH, d.StateDir, alias))
	}
	v.Note = joinNotes(notes...)
	return v, nil
}

// startedSSHWait is how long a started container's ssh is waited for.
func (d LeaseDeps) startedSSHWait() time.Duration {
	return cmp.Or(d.StartedSSHWait, leaseStartedSSHWait)
}

// waitStartedLease waits until the started container behind alias answers
// ssh, within startedSSHWait.
func waitStartedLease(ctx context.Context, d LeaseDeps, alias string) error {
	wait := d.startedSSHWait()
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	cfg := sshReadyConfig{interval: cmp.Or(d.SSHReadyEvery, leaseSSHReadyEvery), timeout: wait, command: "true"}
	if err := waitSSHReady(ctx, d.SSH, alias, cfg); err != nil {
		return fmt.Errorf("wait for ssh: %w", err)
	}
	return nil
}

// superviseUnparked makes sure the supervisor of an unparked lease runs —
// started only when none answers (a container never reset ran its run.start
// at the start; a reset one did not) — and hands it the branch's sibling
// overlay, computed from the checkout's declaration and zcp's records as an
// acquire's checkout computes it: the group may have changed while the lease
// was parked. A declaration zcp cannot read sends no overlay, so the
// supervisor keeps the one it kept in its state. It answers a note, "" when
// all went well; nothing in it fails the unpark.
func superviseUnparked(ctx context.Context, ssh SSHDeployer, stateDir, alias string) string {
	read, note := readLeaseCheckout(ctx, ssh, alias, "so zcp sent its supervisor no sibling overlay (it keeps the one it had)")
	var env LeaseSiblingEnv
	var envArgs []string
	wireNote := ""
	if note == "" {
		env, envArgs, wireNote = checkoutOverlay(stateDir, alias, read)
	}
	r, err := ensureSupervisorIn(ctx, ssh, alias, leaseWorkDir, leaseSupervisorWait, false, envArgs)
	refused := ""
	if err == nil && r.envErr != nil {
		refused = overlayRefusedNote(alias, env, r.envErr)
	}
	return joinNotes(note, wireNote, leaseSupervisorNote(alias, r, err), refused)
}

// leaseWake is what a release did to read a parked lease: whether it started
// the container.
type leaseWake struct {
	started bool
}

// note is what the release says of it.
func (w leaseWake) note() string {
	if !w.started {
		return ""
	}
	return "zcp started its parked container to check it for unsaved work"
}

// wakeParkedForGuard starts the container of a parked lease so the release
// guard can read it, and waits for its ssh. v is the release's own verdict,
// asked of the orchestrator right before (readLeaseOwnership): only a
// container it gives the lease, stopped, is started. A lease that is not
// parked by its record and a zcp without ssh (it checks nothing) start
// nothing either: the guard and the release then say what became of it. A
// start refused held-by-other is returned as it is (the lease ended outside
// zcp; the caller drops it as lost); any other start the orchestrator will not
// make refuses the release; one whose ssh does not answer refuses it too, with
// the container started (the caller stops it again, reparkAfterRefusal).
func wakeParkedForGuard(ctx context.Context, d LeaseDeps, rec leaseRecord, v leaseVerdict) (leaseWake, error) {
	var w leaseWake
	c := v.container
	if !rec.Parked || d.SSH == nil || v.own != leaseOurs || c.LockState != leaseLockReserved || c.Status == platform.SwarmContainerActive {
		return w, nil
	}
	alias := rec.alias()
	if _, err := v.rp.api.Start(ctx, rec.ContainerID, rec.Lease); err != nil {
		if isHeldByOther(err) {
			return w, err
		}
		return w, parkedReleaseRefusal(alias, "zcp could not start its container to check it for unsaved work ("+errorNote(leaseOrchestratorError(ctx, d, v.rp, "start "+alias, err))+")")
	}
	w.started = true
	if err := waitStartedLease(ctx, d, alias); err != nil {
		return w, parkedReleaseRefusal(alias, fmt.Sprintf("zcp started its container to check it for unsaved work, but it did not answer ssh within %s (%v)", d.startedSSHWait(), err))
	}
	return w, nil
}

// parkedReleaseRefusal refuses the release of a parked lease zcp could not
// read, saying why.
func parkedReleaseRefusal(alias, why string) error {
	return platform.NewPlatformError(platform.ErrPrerequisiteMissing,
		fmt.Sprintf("Lease %s is parked and %s, so it was not released", alias, why),
		fmt.Sprintf("%s starts it, then release it; or release with force=true to discard whatever it holds: zerops_lease action=release lease=%s force=true", LeaseUnparkCall(alias), alias))
}

// reparkAfterRefusal is a release's refusal once it started a parked lease's
// container (w): the container is stopped again, so the lease stays parked as
// it was, and the refusal says so — or, when zcp could not stop it, that it
// runs now, with its record saying so and the call that parks it again. The
// caller's context may be what ended the release; the stop runs all the same.
func reparkAfterRefusal(ctx context.Context, d LeaseDeps, rec leaseRecord, w leaseWake, cause error) error {
	if !w.started {
		return cause
	}
	text, _ := repark(context.WithoutCancel(ctx), d, rec)
	return withNote(cause, text)
}

// reparkMembers is a branch release's refusal once it started the containers
// of some of its parked leases: each is stopped again (repark), and the
// refusal says what became of each.
func reparkMembers(ctx context.Context, d LeaseDeps, members []*releaseMember, cause error) error {
	var texts []string
	for _, m := range members {
		if !m.wake.started {
			continue
		}
		if text, _ := repark(context.WithoutCancel(ctx), d, m.rec); text != "" {
			texts = append(texts, m.rec.alias()+": "+text)
		}
	}
	return withNote(cause, strings.Join(texts, "; "))
}

// repark stops again the container of a parked lease that a release started
// and did not release, and says what became of it; parked reports that the
// lease stays parked. It asks the orchestrator first, fresh
// (readLeaseOwnership): a container that is no longer this lease's — or whose
// stop it refuses held-by-other — means the lease ended outside zcp meanwhile,
// and its record is dropped with its alias and mount, saying so; one it is
// stopping or starting right now is left alone, and nothing is said of it.
func repark(ctx context.Context, d LeaseDeps, rec leaseRecord) (text string, parked bool) {
	alias := rec.alias()
	v := readLeaseOwnership(ctx, d, rec)
	var err error
	switch {
	case v.own.lost():
		return reparkDropped(ctx, d, rec, v.why), false
	case v.own == leaseUnverified:
		err = v.err
	case powerTransition(v.container):
		return "", false
	case v.container.Status == platform.SwarmContainerStopped:
		return "zcp stopped its container again, so it stays parked", true
	default:
		_, serr := v.rp.api.Stop(ctx, rec.ContainerID, rec.Lease)
		switch {
		case serr == nil:
			return "zcp stopped its container again, so it stays parked", true
		case isHeldByOther(serr):
			return reparkDropped(ctx, d, rec, heldByOtherWhy), false
		}
		err = leaseOrchestratorError(ctx, d, v.rp, "stop "+alias, serr)
	}
	text = fmt.Sprintf("zcp could not stop its container again (%s), so it runs now: %s parks it again", errorNote(err), LeaseParkCall(alias))
	rec.Parked, rec.ParkedAt = false, ""
	if werr := writeLeaseRecord(d.StateDir, rec); werr != nil {
		text += fmt.Sprintf(" (its record still says parked: %v)", werr)
	}
	return text, false
}

// reparkDropped drops the record of a lease a re-park found no longer this
// lease's — why says so — with its alias and mount, and says so.
func reparkDropped(ctx context.Context, d LeaseDeps, rec leaseRecord, why string) string {
	text := "its lease ended outside zcp meanwhile — " + why + " — so zcp dropped its record, ssh alias and mount"
	if _, err := dropLostLease(ctx, d, rec, why); err != nil {
		text = "its lease ended outside zcp meanwhile — " + why + " — but its record could not be dropped: " + err.Error()
	}
	return text
}

// releasedParkedNote is what a release says of a parked lease it did not
// start (forced, or a zcp without ssh) and handed back to the pool reset: a
// reset leaves a stopped container stopped.
func releasedParkedNote(rec leaseRecord, guard leaseGuardResult, v LeaseView) string {
	if !rec.Parked || guard.woken || !releasedReset(v) {
		return ""
	}
	return "it was parked: a reset leaves a stopped container stopped, and the pool's next acquire starts it"
}

// withNote adds text to err's message ("" adds nothing), keeping its code and
// suggestion.
func withNote(err error, text string) error {
	if text == "" || err == nil {
		return err
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		return fmt.Errorf("%w (%s)", err, text)
	}
	out := *pe
	out.Message += "; " + text
	return &out
}
