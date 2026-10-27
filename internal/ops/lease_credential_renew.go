package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// A lease that is held keeps its pool's HQ credential from the acquire that
// took it, and HQ sends the Mate a new Git credential on every link it opens,
// revoking the one before (docs/spec-workflows.md §8 DP-16). The Mate's own
// service and the repository pass renew the pools once the Mate's file
// changes; a lease's git that HQ refuses before then is not left to them: the
// call that met the refusal writes the Mate's current credential to the pool
// and tries once more (renewLeasePoolCredential). An acquire of the held
// branch is reused and writes nothing, so it is never what zcp points at.

// gitCredentialRefused reports whether err is git asking for a credential the
// session had none of, or the remote refusing the one it presented — the
// failures classifyCheckoutFailure names as credential ones.
func gitCredentialRefused(err error) bool {
	var sshErr *platform.SSHExecError
	if !errors.As(err, &sshErr) {
		return false
	}
	return containsAny(strings.ToLower(sshErr.Output),
		"could not read username", "could not read password", "terminal prompts disabled",
		"authentication failed", "invalid username or password", "invalid credentials",
		"error: 401", "error: 403")
}

// leaseCredentialRenewal is what renewLeasePoolCredential did for a lease's
// pool.
type leaseCredentialRenewal struct {
	// tried: zcp is a Mate's, which writes the pool's HQ credential.
	tried bool
	// renewed: the Mate's current credential was written to the pool and a
	// fresh session waited for, so one more try presents it.
	renewed bool
	// why says what kept it from being renewed.
	why string
}

// renewLeasePoolCredential writes the Mate's current Git credential to the
// pool of the lease rec — HQ refused what a session there presented — under
// the pool's credential lock, and waits for a fresh session in the leased
// container to read it. It writes whatever zcp's record says: HQ refusing it
// says the record is not what the pool holds, or what it holds was revoked.
// A write that fails marks the record (poolCredentialRecord.Refused), so the
// Mate's service, the repository pass and the next acquire write it. Outside a
// Mate nothing is tried: zcp writes no pool credential there.
func renewLeasePoolCredential(ctx context.Context, d LeaseDeps, rec leaseRecord) leaseCredentialRenewal {
	if d.PoolCredential.Read == nil {
		return leaseCredentialRenewal{}
	}
	r := leaseCredentialRenewal{tried: true}
	credential, ok := d.PoolCredential.Read()
	if !ok || credential == "" {
		r.why = "the Mate's server holds no Git credential from HQ — it is not linked to HQ yet, or HQ revoked it and has not sent another (its /mate/setup.json says why)"
		return r
	}
	unlock, err := lockPoolCredential(ctx, d.StateDir, rec.PoolHostname)
	if err != nil {
		r.why = err.Error()
		return r
	}
	defer unlock()
	if err := writePoolHQCredential(ctx, d.Client, d.StateDir, rec.PoolHostname, rec.PoolServiceID, credential); err != nil {
		markPoolCredentialRefused(d.StateDir, rec.PoolHostname, rec.PoolServiceID)
		r.why = fmt.Sprintf("its %s could not be written (%v)", PoolHQCredentialEnvKey, err)
		return r
	}
	poolCredentialState{pool: rec.PoolHostname, fingerprint: hq.GitCredentialFingerprint(credential), wrote: true}.awaitSession(ctx, d, rec)
	r.renewed = true
	return r
}

// markPoolCredentialRefused marks pool's record (of the pool service
// serviceID) refused, keeping what it says otherwise. The caller holds the
// pool's credential lock. Best-effort: an unwritten mark leaves the record as
// it was.
func markPoolCredentialRefused(stateDir, pool, serviceID string) {
	rec, _ := readPoolCredential(stateDir, pool)
	rec.PoolServiceID, rec.Refused = serviceID, true
	_ = storePoolCredential(stateDir, pool, rec)
}

// leaseCredentialRenewals renews each pool at most once in one call, however
// many of its leases meet HQ's refusal; safe for the sync's concurrent
// members.
type leaseCredentialRenewals struct {
	mu   sync.Mutex
	done map[string]*leaseCredentialOnce
}

type leaseCredentialOnce struct {
	once sync.Once
	r    leaseCredentialRenewal
}

func newLeaseCredentialRenewals() *leaseCredentialRenewals {
	return &leaseCredentialRenewals{done: map[string]*leaseCredentialOnce{}}
}

// renew is renewLeasePoolCredential for rec's pool, run once per pool.
func (rs *leaseCredentialRenewals) renew(ctx context.Context, d LeaseDeps, rec leaseRecord) leaseCredentialRenewal {
	rs.mu.Lock()
	o, ok := rs.done[rec.PoolHostname]
	if !ok {
		o = &leaseCredentialOnce{}
		rs.done[rec.PoolHostname] = o
	}
	rs.mu.Unlock()
	o.once.Do(func() { o.r = renewLeasePoolCredential(ctx, d, rec) })
	return o.r
}

// heldLeaseHQCredentialSuggestion is what a held lease's git on this Mate's
// HQ says once HQ refused its pool's credential (why) and the call renewed it
// or could not (r); again is the call to make once the pool holds one HQ
// takes. It never says to acquire again — an acquire of a held branch is
// reused and writes nothing — and asks no one for a token.
func heldLeaseHQCredentialSuggestion(why string, r leaseCredentialRenewal, again string) string {
	const develop = `zerops_workflow action="start" workflow="develop"`
	head := "The repository is on this Mate's HQ, whose git takes only the user `mate` with this Mate's HQ credential, and " + why + ". "
	const tail = " It is no user's token, so never ask for one or make one up"
	switch {
	case r.renewed:
		return head + "zcp wrote this Mate's current HQ credential to the pool's " + PoolHQCredentialEnvKey +
			" in this call and tried once more, and HQ refused that too: HQ revoked it and sends the Mate's server a new one over its link. " +
			"The Mate's own service writes the new one to the pool within seconds of its arrival, and " + develop +
			" renews it from the Mate's file at once; then " + again + "." + tail
	case r.tried:
		return head + "zcp could not renew it in this call: " + r.why + ". The Mate's own service writes it to the pool at its next look once it can, and " +
			develop + " renews it at once; then " + again + "." + tail
	}
	return head + "zcp outside a Mate writes no HQ credential on the pool, so nothing here renews it: work on this repository in the dev service of the pair wired to it, or from the Mate's own zcp." + tail
}

// hqRefusalWhy is what HQ's refusal of the pool's credential was, in the
// words the checkout's classification uses.
func hqRefusalWhy(err error) string {
	var sshErr *platform.SSHExecError
	if errors.As(err, &sshErr) && containsAny(strings.ToLower(sshErr.Output), "could not read username", "could not read password", "terminal prompts disabled") {
		return "the container's session had no " + PoolHQCredentialEnvKey + ": the pool's secret is not set (or is empty), or had not reached the session yet"
	}
	return "HQ refused the pool's " + PoolHQCredentialEnvKey + " for that user: it is not this Mate's current HQ credential"
}

// heldLeaseCredentialRefusal is the refusal of a held lease's git HQ refused
// the pool's credential of (cause), saying what the call did about it (r) and
// the call to make once the pool holds one HQ takes (again).
func heldLeaseCredentialRefusal(cause error, message string, r leaseCredentialRenewal, again string) error {
	pe := platform.NewPlatformError(platform.ErrPrerequisiteMissing, message, heldLeaseHQCredentialSuggestion(hqRefusalWhy(cause), r, again))
	pe.Cause = cause
	return pe
}
