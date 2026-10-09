package hq

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// ErrOtherProject is HQ answering the kept credential for another project:
// not this Mate's, so it is enrolled anew.
var ErrOtherProject = errors.New("hq holds the credential for another project")

// NotThisProjectsMate is HQ's refusal of a zcp service other than the one
// the Mate's record names: a project holds one Mate.
const NotThisProjectsMate = "not_this_projects_mate"

// KeepOptions paces Keep; a zero field takes its default.
type KeepOptions struct {
	// Retry is the wait after a failed attempt, doubling up to RetryMax;
	// 5 s and 5 min.
	Retry, RetryMax time.Duration
	// Recheck is the wait after an enrollment HQ knows, before asking again;
	// 10 min.
	Recheck time.Duration
	// NewMateRetryMax caps the wait between a new Mate's enrollments — with
	// nothing kept to recheck — while its HQ does not answer; 60 s.
	NewMateRetryMax time.Duration
	// Log, when set, hears each outcome that differs from the one before.
	Log func(string)
	// Record, when set, hears every attempt's outcome: nil once enrolled.
	Record func(error)
}

// Keep keeps this Mate enrolled with its org's official HQ until ctx ends.
// It asks the kept enrollment's HQ alone whether it still knows the
// credential (recheck), now and every Recheck; it enrolls — reading the org's
// member list for the official HQ — only with no enrollment kept, when HQ no
// longer knows the credential (revoked, or another project's), or when the
// member list names an official HQ other than the kept one (moved). That list
// is read while the kept HQ does not answer, on the retry's backoff: an HQ
// that lost the anchor answers 503 for good, a deploy's handover for seconds,
// and only the anchor tells the two apart — never how long the silence
// lasted. While the anchor names the kept HQ still, the kept HQ is asked
// again on the backoff, however long it stays silent. A new Mate's
// enrollment — nothing kept to recheck — is tried again on the backoff capped
// at NewMateRetryMax, so a Mate born in an outage enrolls within a minute of
// HQ answering. What is not possible yet is retried with a growing wait,
// never fatal: no official HQ, a Mate HQ holds no record of yet (not_a_mate:
// the client writes it after the project exists), HQ or Zerops not
// answering. One answer ends it, from any attempt — a recheck, an
// enrollment, a new Mate's: HQ refusing this container because another zcp
// service of its project is the Mate (NotThisProjectsMate) — said once and
// recorded, never asked again until zcp starts anew.
func Keep(ctx context.Context, enroll, recheck func(context.Context) (Result, error), moved func(context.Context) (bool, error), opts KeepOptions) {
	opts = opts.withDefaults()
	retry := opts.Retry
	said := ""
	// enrolling: the next attempt is an enrollment; newMate: it is one with
	// nothing kept to recheck.
	enrolling, newMate := false, false
	// backoff is the next retry's wait: up to a quarter more, so the Mates of
	// an org do not all knock at once.
	backoff := func() time.Duration {
		wait := retry + rand.N(retry/4+1) //nolint:gosec // G404: jitter, not a secret
		retry = min(2*retry, opts.RetryMax)
		return wait
	}
	for {
		attempt := recheck
		if enrolling {
			attempt = enroll
		}
		res, err := attempt(ctx)
		if opts.Record != nil {
			opts.Record(err)
		}
		if refusedAs(err, NotThisProjectsMate) {
			if opts.Log != nil {
				opts.Log("not enrolled: " + describe(err) + "; this one stops enrolling")
			}
			return
		}
		wait, line := opts.Recheck, "enrolled with "+res.HQ
		if res.KeyUnnamed != "" {
			line += "; HQ keeps no key id for it: " + res.KeyUnnamed
		}
		var unavailable *UnavailableError
		switch {
		case err == nil:
			enrolling, retry = false, opts.Retry
		case enrolling && newMate && errors.As(err, &unavailable):
			// A new Mate's: tried again soon, never on the outage clock.
			wait, line = min(backoff(), opts.NewMateRetryMax), "HQ not answering: "+describe(err)
		case enrolling:
			wait, line = backoff(), "not enrolled yet: "+describe(err)
		case needsEnrollment(err):
			// Enrolled anew at once: the kept enrollment no longer holds.
			enrolling, newMate = true, errors.Is(err, ErrNotEnrolled)
			wait, line = 0, "enrolling anew: "+describe(err)
		default:
			// The kept HQ did not answer: the anchor says whether it is still the one.
			switch elsewhere, anchorErr := moved(ctx); {
			case anchorErr != nil:
				wait, line = backoff(), "HQ not answering: "+describe(err)+"; the official HQ not read: "+anchorErr.Error()
			case elsewhere:
				enrolling, newMate = true, false
				wait, line = 0, "enrolling anew: the org names another official HQ"
			default:
				wait, line = backoff(), "HQ not answering: "+describe(err)
			}
		}
		if line != said && opts.Log != nil {
			opts.Log(line)
		}
		said = line
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// needsEnrollment is a recheck saying the kept enrollment no longer holds:
// none is kept, or its HQ no longer knows the credential as this project's.
func needsEnrollment(err error) bool {
	return errors.Is(err, ErrNotEnrolled) || errors.Is(err, ErrOtherProject) ||
		refusedAs(err, "mate_credential_required")
}

// Recheck asks the HQ the kept enrollment names — and only it, never the
// member list — whether it still knows the credential as this project's, and
// tells it the id of this container's key where that changed (nameKey).
// ErrNotEnrolled with no enrollment kept for this project.
func (e Enroller) Recheck(ctx context.Context) (Result, error) {
	kept, found, err := LoadEnrollment(e.Path)
	if err != nil {
		return Result{}, err
	}
	if !found || kept.ProjectID != e.ProjectID {
		return Result{}, ErrNotEnrolled
	}
	hq := hqClient{http: e.HTTP, address: kept.HQ}
	projectID, err := hq.whoami(ctx, kept.Credential)
	if err != nil {
		return Result{}, fmt.Errorf("ask %s: %w", kept.HQ, err)
	}
	if projectID != e.ProjectID {
		return Result{}, ErrOtherProject
	}
	return Result{HQ: kept.HQ, KeyUnnamed: e.nameKey(ctx, hq, kept)}, nil
}

// Moved is whether the org's member list names an official HQ other than
// the one the kept enrollment is with — the one answer that moves a Mate
// whose kept HQ does not answer. No official HQ, or an unclear one, moves it
// nowhere: there is nothing to enroll with. ErrNotEnrolled with no
// enrollment kept for this project.
func (e Enroller) Moved(ctx context.Context) (bool, error) {
	kept, found, err := LoadEnrollment(e.Path)
	if err != nil {
		return false, err
	}
	if !found || kept.ProjectID != e.ProjectID {
		return false, ErrNotEnrolled
	}
	official, err := e.official(ctx)
	var noHQ *NoHQError
	if errors.As(err, &noHQ) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return official.Address != kept.HQ, nil
}

func describe(err error) string {
	var noHQ *NoHQError
	if errors.As(err, &noHQ) {
		return fmt.Sprintf("no official HQ (%s)", noHQ.Official.Verdict)
	}
	if refusedAs(err, "not_a_mate") {
		return "HQ holds no record of this Mate yet"
	}
	if refusedAs(err, NotThisProjectsMate) {
		return "another zcp service of this project is its Mate"
	}
	return err.Error()
}

func (o KeepOptions) withDefaults() KeepOptions {
	if o.Retry == 0 {
		o.Retry = 5 * time.Second
	}
	if o.RetryMax == 0 {
		o.RetryMax = 5 * time.Minute
	}
	if o.Recheck == 0 {
		o.Recheck = 10 * time.Minute
	}
	if o.NewMateRetryMax == 0 {
		o.NewMateRetryMax = time.Minute
	}
	return o
}
