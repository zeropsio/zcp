package hq

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"
)

// KeepOptions paces Keep; a zero field takes its default.
type KeepOptions struct {
	// Retry is the wait after a failed attempt, doubling up to RetryMax;
	// 5 s and 5 min.
	Retry, RetryMax time.Duration
	// Recheck is the wait after an enrollment HQ knows, before asking again;
	// 10 min.
	Recheck time.Duration
	// Log, when set, hears each outcome that differs from the one before.
	Log func(string)
	// Record, when set, hears every attempt's outcome: nil once enrolled.
	Record func(error)
}

// Keep keeps this Mate enrolled with its org's official HQ until ctx ends:
// it enrolls now, then asks again every Recheck, which enrolls anew whenever
// HQ no longer knows the credential (revoked, or another HQ became the
// official one). What is not possible yet is retried with a growing wait,
// never fatal: no official HQ, a Mate HQ holds no record of yet
// (not_a_mate: the client writes it after the project exists), HQ or Zerops
// not answering.
func Keep(ctx context.Context, attempt func(context.Context) (Result, error), opts KeepOptions) {
	opts = opts.withDefaults()
	retry := opts.Retry
	said := ""
	for {
		res, err := attempt(ctx)
		if opts.Record != nil {
			opts.Record(err)
		}
		wait, line := opts.Recheck, "enrolled with "+res.HQ
		if err != nil {
			// Up to a quarter more, so the Mates of an org do not all knock at once.
			wait = retry + rand.N(retry/4+1) //nolint:gosec // G404: jitter, not a secret
			retry = min(2*retry, opts.RetryMax)
			line = "not enrolled yet: " + describe(err)
		} else {
			retry = opts.Retry
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

func describe(err error) string {
	var noHQ *NoHQError
	if errors.As(err, &noHQ) {
		return fmt.Sprintf("no official HQ (%s)", noHQ.Official.Verdict)
	}
	if refusedAs(err, "not_a_mate") {
		return "HQ holds no record of this Mate yet"
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
	return o
}
